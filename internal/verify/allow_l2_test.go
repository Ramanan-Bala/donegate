package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/allow"
)

func TestRunAllowedL1FindingDoesNotShortCircuitL2(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command tests use POSIX /bin/sh")
	}

	passJSON := `{"numTotalTestSuites":1,"numPassedTestSuites":1,"numFailedTestSuites":0,"numTotalTests":1,"numPassedTests":1,"numFailedTests":0,"success":true,"testResults":[{"name":"src/cart.test.ts","status":"passed","assertionResults":[{"title":"works","status":"passed","failureMessages":[],"meta":{}}]}]}`
	failJSON := `{"numTotalTestSuites":1,"numPassedTestSuites":0,"numFailedTestSuites":1,"numTotalTests":1,"numPassedTests":0,"numFailedTests":1,"success":false,"testResults":[{"name":"src/cart.test.ts","status":"failed","assertionResults":[{"title":"works","status":"failed","failureMessages":["AssertionError: expected 1 to be 2"],"meta":{}}]}]}`

	setupRepo := func(t *testing.T, fixtureJSON string, exitCode int) (repoDir, baseSHA, counterFile string) {
		t.Helper()
		repoDir = t.TempDir()
		counterFile = filepath.Join(t.TempDir(), "l2-counter.txt")

		runGit(t, repoDir, "-c", "init.defaultBranch=main", "init")
		writeFile(t, repoDir, "src/cart.ts", "export const total = () => 1;\n")
		writeFile(t, repoDir, "src/cart.test.ts", "expect(total()).toBe(1);\n")
		writeFile(t, repoDir, "result.json", fixtureJSON+"\n")

		policyYAML := fmt.Sprintf(
			"version: 1\nprojects:\n  - name: app\n    root: .\n    test: sh -c 'echo run >> %q && cp result.json {json} && exit %d'\n",
			counterFile, exitCode,
		)
		writeFile(t, repoDir, ".donegate.yml", policyYAML)
		baseSHA = commitAll(t, repoDir, "base")
		return repoDir, baseSHA, counterFile
	}

	t.Run("allowed test edit runs L2 and passes when project test passes", func(t *testing.T) {
		repoDir, baseSHA, counterFile := setupRepo(t, passJSON, 0)
		writeFile(t, repoDir, "src/cart.test.ts", "expect(total()).toBe(2);\n")

		res, err := Run(Options{
			RepoDir:  repoDir,
			Base:     baseSHA,
			MaxLevel: 0,
			Allowed: []allow.Allowance{
				{Rule: "T001", Path: "src/cart.test.ts", By: "human", At: "2026-11-21T00:00:00Z"},
			},
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if res.Verdict != "PASS" {
			t.Fatalf("Verdict = %q, want PASS (findings=%+v)", res.Verdict, res.Findings)
		}
		if len(res.Findings) != 0 {
			t.Fatalf("expected 0 kept findings, got %+v", res.Findings)
		}
		if len(res.Allowed) != 1 || res.Allowed[0].RuleID != "T001" || res.Allowed[0].Path != "src/cart.test.ts" {
			t.Fatalf("expected T001 for src/cart.test.ts in Result.Allowed, got %+v", res.Allowed)
		}
		data, err := os.ReadFile(counterFile)
		if err != nil {
			t.Fatalf("expected counter file to exist (L2 must run): %v", err)
		}
		if strings.TrimSpace(string(data)) == "" {
			t.Fatalf("expected counter file to have at least one line")
		}
	})

	t.Run("allowed test edit fails with R001 when project test fails", func(t *testing.T) {
		repoDir, baseSHA, counterFile := setupRepo(t, failJSON, 1)
		writeFile(t, repoDir, "src/cart.test.ts", "expect(total()).toBe(2);\n")

		res, err := Run(Options{
			RepoDir:  repoDir,
			Base:     baseSHA,
			MaxLevel: 0,
			Allowed: []allow.Allowance{
				{Rule: "T001", Path: "src/cart.test.ts", By: "human", At: "2026-11-21T00:00:00Z"},
			},
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if res.Verdict != "FAIL" {
			t.Fatalf("Verdict = %q, want FAIL (findings=%+v)", res.Verdict, res.Findings)
		}
		if len(res.Allowed) != 1 || res.Allowed[0].RuleID != "T001" {
			t.Fatalf("expected T001 in Result.Allowed, got %+v", res.Allowed)
		}
		hasR001 := false
		for _, f := range res.Findings {
			if f.RuleID == "R001" {
				hasR001 = true
			}
		}
		if !hasR001 {
			t.Fatalf("expected R001 in Findings, got %+v", res.Findings)
		}
		data, err := os.ReadFile(counterFile)
		if err != nil || strings.TrimSpace(string(data)) == "" {
			t.Fatalf("expected L2 counter file to have run, err=%v", err)
		}
	})
}
