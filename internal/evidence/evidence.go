package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Ramanan-Bala/donegate/internal/rules"
)

// Schema is the versioned identifier for donegate evidence JSON documents.
const Schema = "donegate/evidence@1"

// Check summarizes the outcome of a single verification rule.
type Check struct {
	ID       string          `json:"id"`
	Status   string          `json:"status"` // "pass" | "warn" | "fail"
	Detail   string          `json:"detail,omitempty"`
	Findings []rules.Finding `json:"findings,omitempty"`
}

// AllowedFinding records a blocking finding waived by a human-approved session allowance.
type AllowedFinding struct {
	Rule string `json:"rule"`
	Path string `json:"path"`
	By   string `json:"by"`
	At   string `json:"at"`
}

// Evidence is the hashed verification report for a session or verify run.
type Evidence struct {
	Schema       string           `json:"schema"`
	Session      string           `json:"session"`
	Harness      string           `json:"harness"`
	Model        string           `json:"model,omitempty"`
	Repo         string           `json:"repo"`
	Base         string           `json:"base"`
	Head         string           `json:"head"`
	Tier         string           `json:"tier"`
	Verdict      string           `json:"verdict"`
	Blocks       int              `json:"blocks"`
	CreatedAt    string           `json:"created_at"`
	Checks       []Check          `json:"checks"`
	Allowed      []AllowedFinding `json:"allowed,omitempty"`
	PolicySHA256 string           `json:"policy_sha256"`
	DiffSHA256   string           `json:"diff_sha256"`
	ReportSHA256 string           `json:"report_sha256"`
}

// Input provides the raw verification outputs used to construct an Evidence report.
type Input struct {
	Session   string
	Harness   string
	Model     string
	Repo      string
	Base      string
	Head      string
	Tier      string
	Blocks    int
	RuleIDs   []string // every rule that ran
	Findings  []rules.Finding
	Allowed   []AllowedFinding
	Policy    []byte // raw policy bytes used
	Diff      []byte // raw diff text
	Now       time.Time
	ForceFail bool // gate gave up after max blocks
}

// Build constructs a hashed Evidence report from in.
func Build(in Input) *Evidence {
	idSet := make(map[string]struct{}, len(in.RuleIDs))
	for _, id := range in.RuleIDs {
		if id != "" {
			idSet[id] = struct{}{}
		}
	}

	byRule := make(map[string][]rules.Finding)
	for _, f := range in.Findings {
		idSet[f.RuleID] = struct{}{}
		byRule[f.RuleID] = append(byRule[f.RuleID], f)
	}

	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	checks := make([]Check, 0, len(ids))
	anyFail := false
	for _, id := range ids {
		fs := byRule[id]
		status := "pass"
		for _, f := range fs {
			if f.Severity == rules.Block {
				status = "fail"
				break
			}
			if f.Severity == rules.Warn {
				status = "warn"
			}
		}

		c := Check{
			ID:     id,
			Status: status,
		}
		if status != "pass" {
			c.Detail = formatDetail(fs)
			c.Findings = fs
		}
		if status == "fail" {
			anyFail = true
		}
		checks = append(checks, c)
	}

	verdict := "PASS"
	if anyFail || in.ForceFail {
		verdict = "FAIL"
	}

	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	var allowed []AllowedFinding
	if len(in.Allowed) > 0 {
		allowed = make([]AllowedFinding, len(in.Allowed))
		copy(allowed, in.Allowed)
	}

	e := &Evidence{
		Schema:       Schema,
		Session:      in.Session,
		Harness:      in.Harness,
		Model:        in.Model,
		Repo:         in.Repo,
		Base:         in.Base,
		Head:         in.Head,
		Tier:         in.Tier,
		Verdict:      verdict,
		Blocks:       in.Blocks,
		CreatedAt:    now.UTC().Format(time.RFC3339),
		Checks:       checks,
		Allowed:      allowed,
		PolicySHA256: sha256Hex(in.Policy),
		DiffSHA256:   sha256Hex(in.Diff),
	}

	hash, err := computeReportHash(e)
	if err != nil {
		panic(err)
	}
	e.ReportSHA256 = hash
	return e
}

// Verify recomputes report_sha256 and returns an error if it does not match e.ReportSHA256.
func (e *Evidence) Verify() error {
	if e == nil {
		return fmt.Errorf("verify evidence: nil evidence")
	}
	expected, err := computeReportHash(e)
	if err != nil {
		return fmt.Errorf("verify evidence: %w", err)
	}
	if e.ReportSHA256 != expected {
		return fmt.Errorf("verify evidence: report_sha256 mismatch (got %q, want %q)", e.ReportSHA256, expected)
	}
	return nil
}

// Write creates dir (0o700) and writes evidence.json and evidence.md (0o600).
func Write(dir string, e *Evidence) (jsonPath, mdPath string, err error) {
	if e == nil {
		return "", "", fmt.Errorf("write evidence: nil evidence")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("create evidence dir: %w", err)
	}

	jsonBytes, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("encode evidence: %w", err)
	}
	jsonBytes = append(jsonBytes, '\n')

	jsonPath = filepath.Join(dir, "evidence.json")
	if err := os.WriteFile(jsonPath, jsonBytes, 0o600); err != nil {
		return "", "", fmt.Errorf("write evidence json: %w", err)
	}

	mdPath = filepath.Join(dir, "evidence.md")
	if err := os.WriteFile(mdPath, []byte(Markdown(e)), 0o600); err != nil {
		return "", "", fmt.Errorf("write evidence markdown: %w", err)
	}

	return jsonPath, mdPath, nil
}

// Read loads and decodes an Evidence report from a JSON file at path.
func Read(path string) (*Evidence, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read evidence %s: %w", path, err)
	}
	var e Evidence
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("decode evidence %s: %w", path, err)
	}
	return &e, nil
}

func formatDetail(findings []rules.Finding) string {
	if len(findings) == 0 {
		return ""
	}
	if len(findings) == 1 {
		return findings[0].Message
	}
	return fmt.Sprintf("%s (+%d more)", findings[0].Message, len(findings)-1)
}

func computeReportHash(e *Evidence) (string, error) {
	cp := *e
	cp.ReportSHA256 = ""
	raw, err := json.Marshal(cp)
	if err != nil {
		return "", fmt.Errorf("marshal evidence for hash: %w", err)
	}
	return sha256Hex(raw), nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
