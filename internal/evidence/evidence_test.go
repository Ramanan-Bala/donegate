package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Ramanan-Bala/donegate/internal/rules"
)

var update = flag.Bool("update", false, "update golden files")

func fixedTime() time.Time {
	return time.Date(2026, 11, 21, 14, 0, 0, 0, time.UTC)
}

func sampleInput() Input {
	return Input{
		Session: "20261121T140000Z-deadbeef",
		Harness: "claude-code",
		Model:   "claude-opus-5-5",
		Repo:    "git@github.com:you/shop.git",
		Base:    "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678",
		Head:    "e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3",
		Tier:    "standard",
		Blocks:  2,
		RuleIDs: []string{"T002", "T001", "C002"},
		Findings: []rules.Finding{
			{
				RuleID:   "C002",
				Severity: rules.Warn,
				Path:     "web/src/Cart.tsx",
				Line:     42,
				Message:  "1 new @ts-expect-error",
				Hint:     "fix the underlying type error instead of suppressing it",
			},
			{
				RuleID:   "T001",
				Severity: rules.Block,
				Path:     "src/cart.test.ts",
				Line:     10,
				Message:  "existing test file modified",
				Hint:     "revert src/cart.test.ts and fix src/cart.ts",
			},
			{
				RuleID:   "T001",
				Severity: rules.Block,
				Path:     "src/checkout.test.ts",
				Message:  "existing test file modified",
				Hint:     "revert src/checkout.test.ts",
			},
		},
		Policy: []byte("version: 1\ngate:\n  tier: standard\n"),
		Diff:   []byte("diff --git a/src/cart.test.ts b/src/cart.test.ts\n"),
		Now:    fixedTime(),
	}
}

func TestBuildStatusAggregation(t *testing.T) {
	tests := []struct {
		name       string
		ruleIDs    []string
		findings   []rules.Finding
		wantChecks []Check
	}{
		{
			name:    "all pass sorted and deduplicated",
			ruleIDs: []string{"T002", "T001", "T001"},
			wantChecks: []Check{
				{ID: "T001", Status: "pass"},
				{ID: "T002", Status: "pass"},
			},
		},
		{
			name:    "pass warn and fail with multiple findings",
			ruleIDs: []string{"T002", "C002", "T001"},
			findings: []rules.Finding{
				{RuleID: "C002", Severity: rules.Warn, Path: "a.ts", Line: 3, Message: "suppression added"},
				{RuleID: "T001", Severity: rules.Block, Path: "a.test.ts", Line: 1, Message: "first failure"},
				{RuleID: "T001", Severity: rules.Block, Path: "b.test.ts", Line: 2, Message: "second failure"},
				{RuleID: "T001", Severity: rules.Block, Path: "c.test.ts", Line: 3, Message: "third failure"},
			},
			wantChecks: []Check{
				{
					ID:     "C002",
					Status: "warn",
					Detail: "suppression added",
					Findings: []rules.Finding{
						{RuleID: "C002", Severity: rules.Warn, Path: "a.ts", Line: 3, Message: "suppression added"},
					},
				},
				{
					ID:     "T001",
					Status: "fail",
					Detail: "first failure (+2 more)",
					Findings: []rules.Finding{
						{RuleID: "T001", Severity: rules.Block, Path: "a.test.ts", Line: 1, Message: "first failure"},
						{RuleID: "T001", Severity: rules.Block, Path: "b.test.ts", Line: 2, Message: "second failure"},
						{RuleID: "T001", Severity: rules.Block, Path: "c.test.ts", Line: 3, Message: "third failure"},
					},
				},
				{ID: "T002", Status: "pass"},
			},
		},
		{
			name:    "rule only in findings is included",
			ruleIDs: []string{"T001"},
			findings: []rules.Finding{
				{RuleID: "H001", Severity: rules.Block, Path: ".donegate.yml", Message: "protected file hash mismatch"},
			},
			wantChecks: []Check{
				{
					ID:     "H001",
					Status: "fail",
					Detail: "protected file hash mismatch",
					Findings: []rules.Finding{
						{RuleID: "H001", Severity: rules.Block, Path: ".donegate.yml", Message: "protected file hash mismatch"},
					},
				},
				{ID: "T001", Status: "pass"},
			},
		},
		{
			name:    "mixed warn and block on same rule yields fail",
			ruleIDs: []string{"S001"},
			findings: []rules.Finding{
				{RuleID: "S001", Severity: rules.Warn, Path: "a.ts", Line: 1, Message: "warn first"},
				{RuleID: "S001", Severity: rules.Block, Path: "b.ts", Line: 2, Message: "block second"},
			},
			wantChecks: []Check{
				{
					ID:     "S001",
					Status: "fail",
					Detail: "warn first (+1 more)",
					Findings: []rules.Finding{
						{RuleID: "S001", Severity: rules.Warn, Path: "a.ts", Line: 1, Message: "warn first"},
						{RuleID: "S001", Severity: rules.Block, Path: "b.ts", Line: 2, Message: "block second"},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev := Build(Input{
				RuleIDs:  tc.ruleIDs,
				Findings: tc.findings,
				Now:      fixedTime(),
			})
			if ev.Schema != Schema {
				t.Errorf("Schema = %q, want %q", ev.Schema, Schema)
			}
			if ev.CreatedAt != "2026-11-21T14:00:00Z" {
				t.Errorf("CreatedAt = %q, want 2026-11-21T14:00:00Z", ev.CreatedAt)
			}
			if !reflect.DeepEqual(ev.Checks, tc.wantChecks) {
				t.Errorf("Checks = %+v, want %+v", ev.Checks, tc.wantChecks)
			}
		})
	}
}

func TestBuildVerdict(t *testing.T) {
	tests := []struct {
		name      string
		findings  []rules.Finding
		forceFail bool
		want      string
	}{
		{
			name: "no findings passes",
			want: "PASS",
		},
		{
			name: "warnings only passes",
			findings: []rules.Finding{
				{RuleID: "C002", Severity: rules.Warn, Path: "a.ts", Message: "warn"},
			},
			want: "PASS",
		},
		{
			name: "block finding fails",
			findings: []rules.Finding{
				{RuleID: "T001", Severity: rules.Block, Path: "a.test.ts", Message: "modified"},
			},
			want: "FAIL",
		},
		{
			name:      "force fail overrides passing checks",
			forceFail: true,
			want:      "FAIL",
		},
		{
			name: "force fail overrides warn-only checks",
			findings: []rules.Finding{
				{RuleID: "C002", Severity: rules.Warn, Path: "a.ts", Message: "warn"},
			},
			forceFail: true,
			want:      "FAIL",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev := Build(Input{
				RuleIDs:   []string{"T001", "C002"},
				Findings:  tc.findings,
				ForceFail: tc.forceFail,
				Now:       fixedTime(),
			})
			if ev.Verdict != tc.want {
				t.Errorf("Verdict = %q, want %q", ev.Verdict, tc.want)
			}
		})
	}
}

func TestBuildDeterministicHashes(t *testing.T) {
	in := sampleInput()
	ev1 := Build(in)

	// Same instant in a non-UTC timezone must yield identical UTC timestamp and hashes.
	inTZ := sampleInput()
	inTZ.Now = in.Now.In(time.FixedZone("IST", 5*3600+30*60))
	ev2 := Build(inTZ)

	wantPolicySum := sha256.Sum256(in.Policy)
	wantPolicyHex := hex.EncodeToString(wantPolicySum[:])
	wantDiffSum := sha256.Sum256(in.Diff)
	wantDiffHex := hex.EncodeToString(wantDiffSum[:])

	if ev1.PolicySHA256 != wantPolicyHex {
		t.Errorf("PolicySHA256 = %q, want %q", ev1.PolicySHA256, wantPolicyHex)
	}
	if ev1.DiffSHA256 != wantDiffHex {
		t.Errorf("DiffSHA256 = %q, want %q", ev1.DiffSHA256, wantDiffHex)
	}
	if ev1.ReportSHA256 == "" || len(ev1.ReportSHA256) != 64 {
		t.Errorf("ReportSHA256 = %q, want 64-char hex sha256", ev1.ReportSHA256)
	}
	if !reflect.DeepEqual(ev1, ev2) {
		t.Errorf("non-deterministic build across timezones:\nev1 = %+v\nev2 = %+v", ev1, ev2)
	}
	if err := ev1.Verify(); err != nil {
		t.Fatalf("Verify on fresh build failed: %v", err)
	}

	// Changing Now must change ReportSHA256.
	inLater := sampleInput()
	inLater.Now = fixedTime().Add(time.Second)
	evLater := Build(inLater)
	if evLater.ReportSHA256 == ev1.ReportSHA256 {
		t.Errorf("ReportSHA256 did not change when Now changed")
	}
}

func TestVerifyDetectsTampering(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Evidence)
	}{
		{
			name:   "verdict changed",
			mutate: func(e *Evidence) { e.Verdict = "PASS" },
		},
		{
			name:   "blocks changed",
			mutate: func(e *Evidence) { e.Blocks = 0 },
		},
		{
			name:   "session changed",
			mutate: func(e *Evidence) { e.Session = "tampered" },
		},
		{
			name:   "model changed",
			mutate: func(e *Evidence) { e.Model = "" },
		},
		{
			name:   "created_at changed",
			mutate: func(e *Evidence) { e.CreatedAt = "2026-11-21T14:00:01Z" },
		},
		{
			name:   "policy_sha256 changed",
			mutate: func(e *Evidence) { e.PolicySHA256 = strings.Repeat("0", 64) },
		},
		{
			name:   "diff_sha256 changed",
			mutate: func(e *Evidence) { e.DiffSHA256 = strings.Repeat("f", 64) },
		},
		{
			name:   "check status changed",
			mutate: func(e *Evidence) { e.Checks[1].Status = "pass" },
		},
		{
			name:   "finding message changed",
			mutate: func(e *Evidence) { e.Checks[1].Findings[0].Message = "harmless" },
		},
		{
			name:   "report_sha256 cleared",
			mutate: func(e *Evidence) { e.ReportSHA256 = "" },
		},
		{
			name:   "report_sha256 corrupted",
			mutate: func(e *Evidence) { e.ReportSHA256 = strings.Repeat("a", 64) },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev := Build(sampleInput())
			tc.mutate(ev)
			if err := ev.Verify(); err == nil {
				t.Fatalf("Verify succeeded after tampering (%s), want error", tc.name)
			}
		})
	}

	t.Run("nil evidence", func(t *testing.T) {
		var nilEv *Evidence
		if err := nilEv.Verify(); err == nil {
			t.Fatal("Verify on nil evidence succeeded, want error")
		}
	})
}

func TestWriteReadRoundTrip(t *testing.T) {
	ev := Build(sampleInput())
	outDir := filepath.Join(t.TempDir(), "nested", "evidence")

	jsonPath, mdPath, err := Write(outDir, ev)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if filepath.Base(jsonPath) != "evidence.json" {
		t.Errorf("jsonPath = %q, want evidence.json", jsonPath)
	}
	if filepath.Base(mdPath) != "evidence.md" {
		t.Errorf("mdPath = %q, want evidence.md", mdPath)
	}

	dirInfo, err := os.Stat(outDir)
	if err != nil {
		t.Fatalf("stat outDir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir perm = %o, want 700", perm)
	}

	for _, p := range []string{jsonPath, mdPath} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s perm = %o, want 600", filepath.Base(p), perm)
		}
	}

	rawJSON, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read raw json: %v", err)
	}
	if len(rawJSON) == 0 || rawJSON[len(rawJSON)-1] != '\n' {
		t.Errorf("evidence.json missing trailing newline")
	}
	if !strings.Contains(string(rawJSON), "\n  \"schema\":") {
		t.Errorf("evidence.json does not appear indented:\n%s", rawJSON)
	}

	// Verify exact JSON field set matches spec.
	var rawMap map[string]any
	if err := json.Unmarshal(rawJSON, &rawMap); err != nil {
		t.Fatalf("unmarshal raw json: %v", err)
	}
	wantKeys := []string{
		"schema", "session", "harness", "model", "repo", "base", "head",
		"tier", "verdict", "blocks", "created_at", "checks",
		"policy_sha256", "diff_sha256", "report_sha256",
	}
	if len(rawMap) != len(wantKeys) {
		t.Errorf("json field count = %d, want %d (keys: %v)", len(rawMap), len(wantKeys), rawMap)
	}
	for _, k := range wantKeys {
		if _, ok := rawMap[k]; !ok {
			t.Errorf("missing expected json field %q", k)
		}
	}

	rawMD, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read raw md: %v", err)
	}
	if string(rawMD) != Markdown(ev) {
		t.Errorf("evidence.md differs from Markdown(ev)")
	}

	loaded, err := Read(jsonPath)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if !reflect.DeepEqual(loaded, ev) {
		t.Errorf("round-trip mismatch:\ngot  = %+v\nwant = %+v", loaded, ev)
	}
	if err := loaded.Verify(); err != nil {
		t.Fatalf("Verify after Read failed: %v", err)
	}

	// Tampering with evidence.json on disk must be caught by Read + Verify.
	tamperedBytes := strings.Replace(string(rawJSON), `"FAIL"`, `"PASS"`, 1)
	if err := os.WriteFile(jsonPath, []byte(tamperedBytes), 0o600); err != nil {
		t.Fatalf("write tampered json: %v", err)
	}
	tampered, err := Read(jsonPath)
	if err != nil {
		t.Fatalf("Read tampered json failed: %v", err)
	}
	if err := tampered.Verify(); err == nil {
		t.Fatal("Verify on tampered evidence.json succeeded, want error")
	}
}

func TestMarkdownGolden(t *testing.T) {
	ev := Build(sampleInput())
	got := Markdown(ev)

	goldenPath := filepath.Join("testdata", "report.golden.md")
	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}

	wantBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	want := string(wantBytes)
	if got != want {
		t.Errorf("Markdown mismatch vs %s:\n--- got ---\n%s\n--- want ---\n%s", goldenPath, got, want)
	}
}

func TestMarkdownEdgeCasesAndPipeEscaping(t *testing.T) {
	tests := []struct {
		name         string
		input        Input
		wantContains []string
		wantAbsent   []string
	}{
		{
			name: "pass report omits model warnings and failures",
			input: Input{
				Session: "s1",
				Harness: "opencode",
				Repo:    "/tmp/repo",
				Base:    "1234567890abcdef",
				Head:    "fedcba0987654321",
				Tier:    "fast",
				Blocks:  0,
				RuleIDs: []string{"T001"},
				Now:     fixedTime(),
			},
			wantContains: []string{
				"## donegate: PASS ✅",
				"Tier: `fast`",
				"Harness: `opencode`",
				"`1234567..fedcba0`",
				"blocked 0 time(s)",
				"| Check | Status | Detail |",
				"| T001 | pass | |",
				"Report SHA-256: `",
			},
			wantAbsent: []string{
				"Model:",
				"### Warnings",
				"### Failures",
				"❌",
			},
		},
		{
			name: "pipe characters and newlines are escaped in table cells",
			input: Input{
				Session: "s2",
				Harness: "claude-code",
				Base:    "abc",
				Head:    "def",
				Tier:    "strict",
				Blocks:  1,
				RuleIDs: []string{"C001"},
				Findings: []rules.Finding{
					{
						RuleID:   "C001",
						Severity: rules.Block,
						Path:     "package.json",
						Line:     7,
						Message:  `script uses "vitest || true" | loosened`,
					},
				},
				Now: fixedTime(),
			},
			wantContains: []string{
				"## donegate: FAIL ❌",
				"`abc..def`",
				`| C001 | fail | script uses "vitest \|\| true" \| loosened |`,
				"### Failures",
				"- `C001` package.json:7 script uses \"vitest || true\" | loosened",
			},
			wantAbsent: []string{
				"### Warnings",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev := Build(tc.input)
			got := Markdown(ev)

			for _, sub := range tc.wantContains {
				if !strings.Contains(got, sub) {
					t.Errorf("Markdown missing %q\nfull output:\n%s", sub, got)
				}
			}
			for _, sub := range tc.wantAbsent {
				if strings.Contains(got, sub) {
					t.Errorf("Markdown unexpectedly contains %q\nfull output:\n%s", sub, got)
				}
			}
			if !strings.Contains(got, ev.ReportSHA256[:12]) {
				t.Errorf("Markdown missing 12-char report hash prefix %q\nfull output:\n%s", ev.ReportSHA256[:12], got)
			}
		})
	}
}
