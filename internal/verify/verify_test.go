package verify

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/policy"
	"github.com/Ramanan-Bala/donegate/internal/rules"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", full, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

func commitAll(t *testing.T, dir, msg string) string {
	t.Helper()
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", msg)
	return strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
}

func TestRunT001EndToEnd(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "-c", "init.defaultBranch=main", "init")
	writeFile(t, dir, "src/cart.ts", "export const total = () => 0;\n")
	writeFile(t, dir, "src/cart.test.ts", "expect(total()).toBe(10);\n")
	baseSHA := commitAll(t, dir, "base")

	// Modify test file in working tree.
	writeFile(t, dir, "src/cart.test.ts", "expect(total()).toBe(0);\n")

	res, err := Run(Options{RepoDir: dir, Base: baseSHA})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res.Verdict != "FAIL" {
		t.Fatalf("Verdict = %q, want FAIL", res.Verdict)
	}
	if len(res.Findings) != 1 || res.Findings[0].RuleID != "T001" || res.Findings[0].Path != "src/cart.test.ts" {
		t.Fatalf("unexpected findings: %+v", res.Findings)
	}
}

func TestRunIgnoresWorkingTreePolicyTampering(t *testing.T) {
	tests := []struct {
		name          string
		basePolicy    string
		workingPolicy string
		wantVerdict   string
		wantSeverity  rules.Severity
		wantFindings  int
	}{
		{
			name:          "working tree .donegate.yml added with T001 off does not disable T001",
			workingPolicy: "version: 1\nrules:\n  T001: off\n",
			wantVerdict:   "FAIL",
			wantSeverity:  rules.Block,
			wantFindings:  1,
		},
		{
			name:          "working tree .donegate.yml edited to T001 off cannot weaken base policy",
			basePolicy:    "version: 1\nrules:\n  T001: block\n",
			workingPolicy: "version: 1\nrules:\n  T001: off\n",
			wantVerdict:   "FAIL",
			wantSeverity:  rules.Block,
			wantFindings:  1,
		},
		{
			name:         "base commit .donegate.yml with T001 warn yields PASS with warn finding",
			basePolicy:   "version: 1\nrules:\n  T001: warn\n",
			wantVerdict:  "PASS",
			wantSeverity: rules.Warn,
			wantFindings: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			runGit(t, dir, "-c", "init.defaultBranch=main", "init")
			writeFile(t, dir, "src/cart.test.ts", "expect(1).toBe(1);\n")
			if tc.basePolicy != "" {
				writeFile(t, dir, ".donegate.yml", tc.basePolicy)
			}
			baseSHA := commitAll(t, dir, "base")

			writeFile(t, dir, "src/cart.test.ts", "expect(1).toBe(2);\n")
			if tc.workingPolicy != "" {
				writeFile(t, dir, ".donegate.yml", tc.workingPolicy)
			}

			res, err := Run(Options{RepoDir: dir, Base: baseSHA})
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			if res.Verdict != tc.wantVerdict {
				t.Fatalf("Verdict = %q, want %q (findings: %+v)", res.Verdict, tc.wantVerdict, res.Findings)
			}
			if len(res.Findings) != tc.wantFindings {
				t.Fatalf("len(Findings) = %d, want %d (%+v)", len(res.Findings), tc.wantFindings, res.Findings)
			}
			if tc.wantFindings > 0 && res.Findings[0].Severity != tc.wantSeverity {
				t.Fatalf("Severity = %q, want %q", res.Findings[0].Severity, tc.wantSeverity)
			}
		})
	}
}

func TestRunOptionsPolicyTamperedMaxLevelAndResultFields(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "-c", "init.defaultBranch=main", "init")
	writeFile(t, dir, "src/cart.ts", "export const total = () => 0;\n")
	writeFile(t, dir, "src/cart.test.ts", "expect(total()).toBe(10);\n")
	writeFile(t, dir, ".donegate.yml", "version: 1\nrules:\n  T001: off\n")
	baseSHA := commitAll(t, dir, "base")

	writeFile(t, dir, "src/cart.test.ts", "expect(total()).toBe(0);\n")

	overridePol := policy.Default()
	overridePol.Rules["T001"] = "block"
	overridePol.Rules["S001"] = "off"

	res, err := Run(Options{
		RepoDir: dir,
		Base:    baseSHA,
		Policy:  overridePol,
		Tampered: []rules.Tamper{
			{Path: "vitest.config.ts", Expected: "abc", Actual: "def"},
		},
		MaxLevel: 1,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res.Verdict != "FAIL" {
		t.Fatalf("Verdict = %q, want FAIL", res.Verdict)
	}
	if res.Diff == nil || len(res.Diff.Files) != 1 || res.Diff.Files[0].Path != "src/cart.test.ts" {
		t.Fatalf("unexpected Diff: %+v", res.Diff)
	}
	if !sort.StringsAreSorted(res.RuleIDs) || len(res.RuleIDs) == 0 {
		t.Fatalf("RuleIDs should be sorted and non-empty: %v", res.RuleIDs)
	}
	for _, id := range res.RuleIDs {
		if id == "S001" {
			t.Errorf("RuleIDs should exclude off rule S001: %v", res.RuleIDs)
		}
	}

	var gotRules []string
	for _, f := range res.Findings {
		gotRules = append(gotRules, f.RuleID)
	}
	wantRules := []string{"H001", "T001"}
	if !reflect.DeepEqual(gotRules, wantRules) {
		t.Fatalf("finding rules = %v, want %v (%+v)", gotRules, wantRules, res.Findings)
	}
}

func TestRunL2GatingAndLazyInvocation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command tests use POSIX /bin/sh")
	}

	setupRepo := func(t *testing.T) (repoDir, baseSHA, counterFile string) {
		t.Helper()
		repoDir = t.TempDir()
		counterFile = filepath.Join(t.TempDir(), "l2-counter.txt")

		runGit(t, repoDir, "-c", "init.defaultBranch=main", "init")
		writeFile(t, repoDir, "src/cart.ts", "export const total = () => 0;\n")
		writeFile(t, repoDir, "src/cart.test.ts", "expect(total()).toBe(0);\n")

		vitestJSON := `{"numTotalTestSuites":1,"numPassedTestSuites":1,"numFailedTestSuites":0,"numTotalTests":1,"numPassedTests":1,"numFailedTests":0,"success":true,"testResults":[{"name":"src/cart.test.ts","status":"passed","assertionResults":[{"title":"works","status":"passed","failureMessages":[],"meta":{}}]}]}`
		policyYAML := fmt.Sprintf("version: 1\nprojects:\n  - name: app\n    root: .\n    test: echo run >> %q && printf '%%s\\n' '%s' > {json}\n", counterFile, vitestJSON)
		writeFile(t, repoDir, ".donegate.yml", policyYAML)

		baseSHA = commitAll(t, repoDir, "base")
		return repoDir, baseSHA, counterFile
	}

	hasRule := func(ids []string, target string) bool {
		for _, id := range ids {
			if id == target {
				return true
			}
		}
		return false
	}

	t.Run("L1 block violation stops before L2 with MaxLevel 0", func(t *testing.T) {
		repoDir, baseSHA, counterFile := setupRepo(t)
		writeFile(t, repoDir, "src/cart.test.ts", "expect(total()).toBe(1);\n")

		res, err := Run(Options{RepoDir: repoDir, Base: baseSHA, MaxLevel: 0})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if res.Verdict != "FAIL" {
			t.Fatalf("Verdict = %q, want FAIL", res.Verdict)
		}
		if len(res.Findings) != 1 || res.Findings[0].RuleID != "T001" {
			t.Fatalf("unexpected findings: %+v", res.Findings)
		}
		if _, err := os.Stat(counterFile); !os.IsNotExist(err) {
			t.Fatalf("expected counter file not to exist, got err=%v", err)
		}
		for _, id := range res.RuleIDs {
			if strings.HasPrefix(id, "R00") {
				t.Errorf("RuleIDs should not include L2 rule %q: %v", id, res.RuleIDs)
			}
		}
	})

	t.Run("honest source change with MaxLevel 0 runs L2 and includes R001 R002 R003", func(t *testing.T) {
		repoDir, baseSHA, counterFile := setupRepo(t)
		writeFile(t, repoDir, "src/cart.ts", "export const total = () => 42;\n")

		res, err := Run(Options{RepoDir: repoDir, Base: baseSHA, MaxLevel: 0})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if res.Verdict != "PASS" {
			t.Fatalf("Verdict = %q, want PASS (findings: %+v)", res.Verdict, res.Findings)
		}
		data, err := os.ReadFile(counterFile)
		if err != nil {
			t.Fatalf("expected counter file to exist: %v", err)
		}
		if strings.TrimSpace(string(data)) == "" {
			t.Fatalf("expected counter file to have lines, got empty")
		}
		for _, id := range []string{"R001", "R002", "R003"} {
			if !hasRule(res.RuleIDs, id) {
				t.Errorf("RuleIDs missing %s: %v", id, res.RuleIDs)
			}
		}
	})

	t.Run("no changes at all skips L2", func(t *testing.T) {
		repoDir, baseSHA, counterFile := setupRepo(t)

		res, err := Run(Options{RepoDir: repoDir, Base: baseSHA, MaxLevel: 0})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if res.Verdict != "PASS" {
			t.Fatalf("Verdict = %q, want PASS (findings: %+v)", res.Verdict, res.Findings)
		}
		if _, err := os.Stat(counterFile); !os.IsNotExist(err) {
			t.Fatalf("expected counter file not to exist, got err=%v", err)
		}
	})

	t.Run("MaxLevel 1 never runs L2", func(t *testing.T) {
		repoDir, baseSHA, counterFile := setupRepo(t)
		writeFile(t, repoDir, "src/cart.ts", "export const total = () => 42;\n")

		res, err := Run(Options{RepoDir: repoDir, Base: baseSHA, MaxLevel: 1})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if res.Verdict != "PASS" {
			t.Fatalf("Verdict = %q, want PASS (findings: %+v)", res.Verdict, res.Findings)
		}
		if _, err := os.Stat(counterFile); !os.IsNotExist(err) {
			t.Fatalf("expected counter file not to exist, got err=%v", err)
		}
		for _, id := range res.RuleIDs {
			if strings.HasPrefix(id, "R00") {
				t.Errorf("RuleIDs should not include L2 rule %q: %v", id, res.RuleIDs)
			}
		}
	})
}
