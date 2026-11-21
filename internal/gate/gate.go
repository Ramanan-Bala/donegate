package gate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Ramanan-Bala/donegate/internal/allow"
	"github.com/Ramanan-Bala/donegate/internal/evidence"
	"github.com/Ramanan-Bala/donegate/internal/gitx"
	"github.com/Ramanan-Bala/donegate/internal/policy"
	"github.com/Ramanan-Bala/donegate/internal/rules"
	"github.com/Ramanan-Bala/donegate/internal/session"
	"github.com/Ramanan-Bala/donegate/internal/verify"
)

// Options configures a donegate gate run.
type Options struct {
	Cwd            string
	HarnessSession string
	Now            func() time.Time // nil -> time.Now
}

// Outcome describes the gate decision, messages, retry attempt, and evidence path.
type Outcome struct {
	Block        bool
	Message      string // shown to the agent when Block
	UserMessage  string // shown to the human, may be empty
	Verdict      string
	Attempt      int
	MaxBlocks    int
	EvidencePath string
}

type gateState struct {
	Blocks      int    `json:"blocks"`
	LastVerdict string `json:"last_verdict"`
	UpdatedAt   string `json:"updated_at"`
}

// Run evaluates the active session's changes against the snapshotted policy and updates retry state and evidence.
func Run(opts Options) (*Outcome, error) {
	sess, err := session.CurrentFor(opts.Cwd, opts.HarnessSession)
	if err != nil {
		if errors.Is(err, session.ErrNoSession) {
			return &Outcome{
				Block:       false,
				UserMessage: "donegate: no session for this repo; run `donegate session start` (or install the SessionStart hook)",
			}, nil
		}
		return nil, fmt.Errorf("gate: load current session: %w", err)
	}

	allowances, err := allow.List(sess)
	if err != nil {
		return nil, fmt.Errorf("gate: load allowances: %w", err)
	}

	mismatches, err := sess.CheckProtected()
	if err != nil {
		return nil, fmt.Errorf("gate: check protected files: %w", err)
	}
	tampered := make([]rules.Tamper, 0, len(mismatches)+1)
	for _, m := range mismatches {
		tampered = append(tampered, rules.Tamper{
			Path:     m.Path,
			Expected: m.Expected,
			Actual:   m.Actual,
		})
	}

	policyPath := filepath.Join(sess.Dir(), "policy.yml")
	policyBytes, err := sess.PolicyBytes()
	var pol *policy.Policy
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			pol = policy.Default()
			tampered = append(tampered, rules.Tamper{
				Path:     policyPath,
				Expected: sess.PolicySHA256,
				Actual:   "",
			})
		} else {
			return nil, fmt.Errorf("gate: read policy snapshot: %w", err)
		}
	} else {
		actualPolicySHA := sha256Hex(policyBytes)
		if actualPolicySHA != sess.PolicySHA256 {
			tampered = append(tampered, rules.Tamper{
				Path:     policyPath,
				Expected: sess.PolicySHA256,
				Actual:   actualPolicySHA,
			})
		}
		pol, err = sess.Policy()
		if err != nil {
			if actualPolicySHA != sess.PolicySHA256 {
				pol = policy.Default()
			} else {
				return nil, fmt.Errorf("gate: %w", err)
			}
		}
	}

	tier := pol.Gate.Tier
	if tier == "" {
		tier = "standard"
	}
	var maxLevel int
	switch tier {
	case "fast":
		maxLevel = 1
	case "standard":
		maxLevel = 2
	case "strict":
		maxLevel = 0
	default:
		return nil, fmt.Errorf("gate: unknown tier %q (expected fast, standard, or strict)", tier)
	}
	maxBlocks := pol.Gate.MaxBlocks
	if maxBlocks <= 0 {
		maxBlocks = 5
	}

	restore := make([]string, 0, len(sess.Protected))
	for p := range sess.Protected {
		if allow.Allows(allowances, "H001", p) {
			continue
		}
		restore = append(restore, p)
	}
	sort.Strings(restore)

	res, err := verify.Run(verify.Options{
		RepoDir:  sess.Repo,
		Base:     sess.Base,
		Policy:   pol,
		Tampered: tampered,
		MaxLevel: maxLevel,
		Restore:  restore,
		CacheDir: sess.Dir(),
		Allowed:  allowances,
	})
	if err != nil {
		return nil, fmt.Errorf("gate: %w", err)
	}

	statePath := filepath.Join(sess.Dir(), "gate.json")
	state, err := readGateState(statePath)
	if err != nil {
		return nil, fmt.Errorf("gate: %w", err)
	}

	if (res.Diff == nil || len(res.Diff.Files) == 0) && len(tampered) == 0 && state.LastVerdict != "FAIL" {
		return &Outcome{
			Block:   false,
			Verdict: "PASS",
		}, nil
	}

	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn().UTC()

	var (
		blocks          []rules.Finding
		warns           []rules.Finding
		allowedFindings []evidence.AllowedFinding
	)
	for _, f := range res.Findings {
		switch f.Severity {
		case rules.Block:
			blocks = append(blocks, f)
		case rules.Warn:
			warns = append(warns, f)
		}
	}
	seenAllowed := make(map[string]bool)
	for _, f := range res.Allowed {
		m, ok := allow.Match(allowances, f.RuleID, f.Path)
		if !ok {
			continue
		}
		key := f.RuleID + "\x00" + f.Path
		if seenAllowed[key] {
			continue
		}
		seenAllowed[key] = true
		allowedFindings = append(allowedFindings, evidence.AllowedFinding{
			Rule: f.RuleID,
			Path: f.Path,
			By:   m.By,
			At:   m.At,
		})
	}

	verdict := "PASS"
	if len(blocks) > 0 {
		verdict = "FAIL"
	}

	out := &Outcome{
		Verdict:   verdict,
		MaxBlocks: maxBlocks,
	}
	forceFail := false

	if verdict == "FAIL" {
		state.Blocks++
		state.LastVerdict = "FAIL"
		state.UpdatedAt = now.Format(time.RFC3339)
		out.Attempt = state.Blocks
		if state.Blocks <= maxBlocks {
			out.Block = true
			msgBlocks := sortBlockFindings(filterBlockFindings(blocks))
			out.Message = formatBlockMessage(msgBlocks, warns, state.Blocks, maxBlocks)
		} else {
			forceFail = true
			out.Block = false
		}
	} else {
		state.LastVerdict = "PASS"
		state.UpdatedAt = now.Format(time.RFC3339)
		out.Block = false
		out.Attempt = state.Blocks
	}

	if err := writeGateState(statePath, state); err != nil {
		return nil, fmt.Errorf("gate: %w", err)
	}

	var diffBytes []byte
	if res.Diff != nil {
		diffBytes, err = json.Marshal(res.Diff.Files)
		if err != nil {
			return nil, fmt.Errorf("gate: encode diff files: %w", err)
		}
	}

	ev := evidence.Build(evidence.Input{
		Session:   sess.ID,
		Harness:   sess.Harness,
		Repo:      resolveRepoIdentifier(sess.Repo),
		Base:      sess.Base,
		Head:      resolveHeadOrEmpty(sess.Repo),
		Tier:      tier,
		Blocks:    state.Blocks,
		RuleIDs:   res.RuleIDs,
		Findings:  res.Findings,
		Allowed:   allowedFindings,
		Policy:    policyBytes,
		Diff:      diffBytes,
		Now:       now,
		ForceFail: forceFail,
	})
	jsonPath, _, err := evidence.Write(filepath.Join(sess.Dir(), "evidence"), ev)
	if err != nil {
		return nil, fmt.Errorf("gate: write evidence: %w", err)
	}
	out.EvidencePath = jsonPath

	if forceFail {
		out.UserMessage = fmt.Sprintf("donegate: gave up after %d blocks; the work is NOT verified. Evidence: %s", maxBlocks, jsonPath)
	} else if verdict == "PASS" {
		out.UserMessage = fmt.Sprintf("donegate: verified (%s). Evidence: %s", countLabel(len(warns), "warning", "warnings"), jsonPath)
	}

	return out, nil
}

func readGateState(path string) (gateState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return gateState{}, nil
		}
		return gateState{}, fmt.Errorf("read gate state: %w", err)
	}
	var st gateState
	if err := json.Unmarshal(data, &st); err != nil {
		return gateState{}, fmt.Errorf("decode gate state: %w", err)
	}
	return st, nil
}

func writeGateState(path string, st gateState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode gate state: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write gate state: %w", err)
	}
	return nil
}

func filterBlockFindings(blocks []rules.Finding) []rules.Finding {
	covered := make(map[string]bool)
	for _, f := range blocks {
		if f.RuleID != "H001" && f.Path != "" {
			covered[f.Path] = true
		}
	}
	filtered := make([]rules.Finding, 0, len(blocks))
	for _, f := range blocks {
		if f.RuleID == "H001" && f.Path != "" && covered[f.Path] {
			continue
		}
		filtered = append(filtered, f)
	}
	return filtered
}

func sortBlockFindings(blocks []rules.Finding) []rules.Finding {
	out := make([]rules.Finding, len(blocks))
	copy(out, blocks)
	sort.SliceStable(out, func(i, j int) bool {
		gi, gj := ruleGroupOrder(out[i].RuleID), ruleGroupOrder(out[j].RuleID)
		if gi != gj {
			return gi < gj
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].RuleID != out[j].RuleID {
			return out[i].RuleID < out[j].RuleID
		}
		return out[i].Line < out[j].Line
	})
	return out
}

func ruleGroupOrder(ruleID string) int {
	if ruleID == "" {
		return 5
	}
	switch ruleID[0] {
	case 'T':
		return 0
	case 'C':
		return 1
	case 'S':
		return 2
	case 'R':
		return 3
	case 'H':
		return 4
	default:
		return 5
	}
}

func formatBlockMessage(blocks, warns []rules.Finding, attempt, maxBlocks int) string {
	lines := make([]string, 0, 2+len(blocks)+len(warns))
	lines = append(lines, fmt.Sprintf("donegate: not done (%s, attempt %d/%d)", countLabel(len(blocks), "failure", "failures"), attempt, maxBlocks))

	var reproduceHints []string
	seenHints := make(map[string]bool)
	collectReproduce := func(f rules.Finding) bool {
		if (f.RuleID == "R001" || f.RuleID == "R003") && strings.HasPrefix(f.Hint, "Run ") {
			if !seenHints[f.Hint] {
				seenHints[f.Hint] = true
				reproduceHints = append(reproduceHints, f.Hint)
			}
			return true
		}
		return false
	}

	for _, f := range blocks {
		omitHint := collectReproduce(f)
		lines = append(lines, formatFindingLine("- ", f, omitHint))
	}
	for _, f := range warns {
		omitHint := collectReproduce(f)
		lines = append(lines, formatFindingLine("warn ", f, omitHint))
	}
	lines = append(lines, reproduceHints...)
	lines = append(lines, "Fix these and stop again; donegate re-checks every stop.")
	return strings.Join(lines, "\n")
}

func formatFindingLine(prefix string, f rules.Finding, omitHint bool) string {
	s := fmt.Sprintf("%s%s %s", prefix, f.RuleID, f.Message)
	if !omitHint && f.Hint != "" {
		s += "; " + f.Hint
	}
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

func countLabel(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

func resolveRepoIdentifier(repoRoot string) string {
	out, err := gitx.Output(repoRoot, nil, "config", "--get", "remote.origin.url")
	if err == nil {
		if u := strings.TrimSpace(string(out)); u != "" {
			return u
		}
	}
	return repoRoot
}

func resolveHeadOrEmpty(repoRoot string) string {
	out, err := gitx.Output(repoRoot, nil, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
