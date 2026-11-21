package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/evidence"
	"github.com/Ramanan-Bala/donegate/internal/gate"
	"github.com/Ramanan-Bala/donegate/internal/session"
)

func runWithCapturedStderr(t *testing.T, args []string) (int, string) {
	t.Helper()
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	code := run(args)
	_ = w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	_ = r.Close()
	return code, buf.String()
}

func TestReportCmd(t *testing.T) {
	t.Run("exits 1 with clear message when no session or no evidence yet", func(t *testing.T) {
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)

		code, stderr := runWithCapturedStderr(t, []string{"report", "--repo", repo})
		if code != 1 {
			t.Errorf("no session: exit code = %d, want 1 (stderr=%q)", code, stderr)
		}
		if !strings.Contains(stderr, "no active session") {
			t.Errorf("no session: stderr = %q, want clear message", stderr)
		}

		if _, err := session.Start(repo, "manual"); err != nil {
			t.Fatalf("session.Start: %v", err)
		}
		code, stderr = runWithCapturedStderr(t, []string{"report", "--repo", repo})
		if code != 1 {
			t.Errorf("no evidence yet: exit code = %d, want 1 (stderr=%q)", code, stderr)
		}
		if !strings.Contains(stderr, "no evidence yet") {
			t.Errorf("no evidence yet: stderr = %q, want clear message", stderr)
		}
	})

	t.Run("prints text, md, and json after a gate run", func(t *testing.T) {
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)
		sess, err := session.Start(repo, "opencode")
		if err != nil {
			t.Fatalf("session.Start: %v", err)
		}
		writeTestFile(t, repo, "src/cart.test.ts", "test('x', () => expect(1).toBe(2));\n")
		if _, err := gate.Run(gate.Options{Cwd: repo}); err != nil {
			t.Fatalf("gate.Run: %v", err)
		}

		// Default text summary (via active session for --repo)
		textOut, textErr, err := execCmd(t, "", "report", "--repo", repo)
		if err != nil {
			t.Fatalf("report text failed: %v (stderr=%q)", err, textErr)
		}
		if textErr != "" {
			t.Errorf("unexpected stderr: %q", textErr)
		}
		if !strings.Contains(textOut, "donegate: FAIL") || !strings.Contains(textOut, "1 block") || !strings.Contains(textOut, "T001") {
			t.Errorf("unexpected text summary:\n%s", textOut)
		}

		// Markdown output (via explicit --session)
		mdOut, mdErr, err := execCmd(t, "", "report", "--session", sess.ID, "--md")
		if err != nil {
			t.Fatalf("report --md failed: %v (stderr=%q)", err, mdErr)
		}
		if !strings.HasPrefix(mdOut, "## donegate: FAIL") {
			t.Errorf("markdown output should start with '## donegate: FAIL', got:\n%s", mdOut)
		}

		// JSON output
		jsonOut, jsonErr, err := execCmd(t, "", "report", "--repo", repo, "--json")
		if err != nil {
			t.Fatalf("report --json failed: %v (stderr=%q)", err, jsonErr)
		}
		var ev evidence.Evidence
		if err := json.Unmarshal([]byte(jsonOut), &ev); err != nil {
			t.Fatalf("invalid evidence json: %v\n%s", err, jsonOut)
		}
		if ev.Session != sess.ID || ev.Verdict != "FAIL" || ev.Blocks != 1 {
			t.Errorf("unexpected evidence: %+v", ev)
		}
		if err := ev.Verify(); err != nil {
			t.Errorf("evidence.Verify failed: %v", err)
		}

		// Tampered evidence warns on stderr but still prints report
		evPath := filepath.Join(sess.Dir(), "evidence", "evidence.json")
		raw, err := os.ReadFile(evPath)
		if err != nil {
			t.Fatal(err)
		}
		tampered := strings.Replace(string(raw), `"verdict": "FAIL"`, `"verdict": "PASS"`, 1)
		if err := os.WriteFile(evPath, []byte(tampered), 0o600); err != nil {
			t.Fatal(err)
		}
		_, warnErr, err := execCmd(t, "", "report", "--repo", repo)
		if err != nil {
			t.Fatalf("report on tampered evidence failed: %v", err)
		}
		if !strings.Contains(warnErr, "warning:") || !strings.Contains(warnErr, "report_sha256 mismatch") {
			t.Errorf("expected hash mismatch warning on stderr, got %q", warnErr)
		}
	})
}

func TestVerifyEvidenceAndMarkdownFlags(t *testing.T) {
	t.Run("writes evidence files and prints markdown on PASS", func(t *testing.T) {
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)
		writeTestFile(t, repo, "src/cart.ts", "export const x = 2;\n")

		evDir := filepath.Join(t.TempDir(), "out-evidence")
		stdout, stderr, err := execCmd(t, "", "verify", "--base", "HEAD", "--repo", repo, "--tier", "fast", "--evidence-dir", evDir, "--md")
		if err != nil {
			t.Fatalf("verify failed: %v (stderr=%q)", err, stderr)
		}
		if !strings.HasPrefix(stdout, "## donegate:") {
			t.Errorf("expected markdown starting with '## donegate:', got:\n%s", stdout)
		}
		if !strings.HasPrefix(stdout, "## donegate: PASS") {
			t.Errorf("expected PASS markdown, got:\n%s", stdout)
		}

		jsonPath := filepath.Join(evDir, "evidence.json")
		mdPath := filepath.Join(evDir, "evidence.md")
		ev, err := evidence.Read(jsonPath)
		if err != nil {
			t.Fatalf("read evidence.json: %v", err)
		}
		if err := ev.Verify(); err != nil {
			t.Errorf("evidence.Verify failed: %v", err)
		}
		if ev.Harness != "ci" || ev.Session != "" || ev.Tier != "fast" || ev.Verdict != "PASS" {
			t.Errorf("unexpected evidence metadata: %+v", ev)
		}
		if ev.Head == "" || ev.Base == "" || ev.Repo == "" || ev.PolicySHA256 == "" || ev.DiffSHA256 == "" {
			t.Errorf("expected populated repo/base/head/hashes, got %+v", ev)
		}

		mdBytes, err := os.ReadFile(mdPath)
		if err != nil {
			t.Fatalf("read evidence.md: %v", err)
		}
		if !strings.HasPrefix(string(mdBytes), "## donegate: PASS") {
			t.Errorf("unexpected evidence.md:\n%s", string(mdBytes))
		}
	})

	t.Run("writes evidence files, prints markdown, and exits 1 on FAIL", func(t *testing.T) {
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)
		writeTestFile(t, repo, "src/cart.test.ts", "test('x', () => expect(1).toBe(2));\n")

		evDir := filepath.Join(t.TempDir(), "fail-evidence")
		stdout, _, err := execCmd(t, "", "verify", "--base", "HEAD", "--repo", repo, "--tier", "fast", "--evidence-dir", evDir, "--md")
		if err == nil {
			t.Fatal("expected error on FAIL, got nil")
		}
		if !strings.HasPrefix(stdout, "## donegate: FAIL") {
			t.Errorf("expected FAIL markdown, got:\n%s", stdout)
		}

		ev, readErr := evidence.Read(filepath.Join(evDir, "evidence.json"))
		if readErr != nil {
			t.Fatalf("expected evidence.json on FAIL: %v", readErr)
		}
		if ev.Verdict != "FAIL" {
			t.Errorf("evidence verdict = %q, want FAIL", ev.Verdict)
		}
		if _, statErr := os.Stat(filepath.Join(evDir, "evidence.md")); statErr != nil {
			t.Errorf("expected evidence.md on FAIL: %v", statErr)
		}

		// Verify exit code is 1 via run().
		code, _ := runWithCapturedStderr(t, []string{"verify", "--base", "HEAD", "--repo", repo, "--tier", "fast", "--md"})
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
	})
}
