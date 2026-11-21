package gate

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/allow"
	"github.com/Ramanan-Bala/donegate/internal/evidence"
	"github.com/Ramanan-Bala/donegate/internal/session"
)

func TestRunAllowedTestEditStillRunsL2InGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command tests use POSIX /bin/sh")
	}
	t.Setenv("DONEGATE_HOME", t.TempDir())

	counterFile := filepath.Join(t.TempDir(), "gate-l2-counter.txt")
	passJSON := `{"numTotalTestSuites":1,"numPassedTestSuites":1,"numFailedTestSuites":0,"numTotalTests":1,"numPassedTests":1,"numFailedTests":0,"success":true,"testResults":[{"name":"src/cart.test.ts","status":"passed","assertionResults":[{"title":"works","status":"passed","failureMessages":[],"meta":{}}]}]}`
	failJSON := `{"numTotalTestSuites":1,"numPassedTestSuites":0,"numFailedTestSuites":1,"numTotalTests":1,"numPassedTests":0,"numFailedTests":1,"success":false,"testResults":[{"name":"src/cart.test.ts","status":"failed","assertionResults":[{"title":"works","status":"failed","failureMessages":["AssertionError: expected 1 to be 2"],"meta":{}}]}]}`

	testCmd := fmt.Sprintf(
		"sh -c 'echo run >> %q && if grep -q ALLOW_PASS src/cart.test.ts; then cp pass.json {json}; else cp fail.json {json}; exit 1; fi'",
		counterFile,
	)

	dir := initRepo(t, map[string]string{
		"pass.json":        passJSON + "\n",
		"fail.json":        failJSON + "\n",
		"src/cart.test.ts": "// ALLOW_PASS\n",
		".donegate.yml":    fmt.Sprintf("version: 1\ngate:\n  tier: standard\nprojects:\n  - name: web\n    root: .\n    test: %q\n", testCmd),
	})

	sess, err := session.Start(dir, "claude-code")
	if err != nil {
		t.Fatalf("session.Start: %v", err)
	}
	if err := allow.Add(sess, "T001", []string{"src/cart.test.ts"}, "alice"); err != nil {
		t.Fatalf("allow.Add: %v", err)
	}

	// 1. Allowed edit of test where project test passes -> L2 runs, verdict PASS, evidence records T001+H001 in Allowed.
	writeRepoFile(t, dir, "src/cart.test.ts", "// ALLOW_PASS\n// edited\n")
	outPass, err := Run(Options{Cwd: dir, Now: fixedNow()})
	if err != nil {
		t.Fatalf("Run pass case: %v", err)
	}
	if outPass.Block || outPass.Verdict != "PASS" {
		t.Fatalf("got Block=%v Verdict=%q Message=%q, want PASS", outPass.Block, outPass.Verdict, outPass.Message)
	}
	data, err := os.ReadFile(counterFile)
	if err != nil || strings.TrimSpace(string(data)) == "" {
		t.Fatalf("expected L2 counter file to have lines after allowed test edit, err=%v", err)
	}
	evPass, err := evidence.Read(outPass.EvidencePath)
	if err != nil {
		t.Fatalf("evidence.Read: %v", err)
	}
	if len(evPass.Allowed) == 0 {
		t.Fatalf("expected Allowed in evidence, got empty")
	}

	// 2. Allowed edit of test where project test fails -> verdict FAIL with R001.
	writeRepoFile(t, dir, "src/cart.test.ts", "// BROKEN\n")
	outFail, err := Run(Options{Cwd: dir, Now: fixedNow()})
	if err != nil {
		t.Fatalf("Run fail case: %v", err)
	}
	if !outFail.Block || outFail.Verdict != "FAIL" {
		t.Fatalf("got Block=%v Verdict=%q, want Block=true Verdict=FAIL", outFail.Block, outFail.Verdict)
	}
	if !strings.Contains(outFail.Message, "- R001 ") {
		t.Errorf("expected R001 in block message, got:\n%s", outFail.Message)
	}
	if strings.Contains(outFail.Message, "T001 src/cart.test.ts") || strings.Contains(outFail.Message, "H001 src/cart.test.ts") {
		t.Errorf("allowed T001/H001 should not appear in block message, got:\n%s", outFail.Message)
	}
}
