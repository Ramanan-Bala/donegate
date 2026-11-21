package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/session"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitCmd(t, dir, "-c", "init.defaultBranch=main", "init")
	writeTestFile(t, dir, "src/cart.ts", "export const x = 1;\n")
	writeTestFile(t, dir, "src/cart.test.ts", "test('x', () => expect(1).toBe(1));\n")
	runGitCmd(t, dir, "add", "-A")
	runGitCmd(t, dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "init")
	return dir
}

func runGitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeTestFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", full, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

func TestGateCmd(t *testing.T) {
	t.Run("text mode blocks with exit code 1 when test modified", func(t *testing.T) {
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)
		if _, err := session.Start(repo, "manual"); err != nil {
			t.Fatalf("session.Start failed: %v", err)
		}
		writeTestFile(t, repo, "src/cart.test.ts", "test('x', () => expect(1).toBe(2));\n")

		code := run([]string{"gate", "--repo", repo})
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
	})

	t.Run("claude format outputs block JSON and exits 0", func(t *testing.T) {
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)
		if _, err := session.Start(repo, "claude-code"); err != nil {
			t.Fatalf("session.Start failed: %v", err)
		}
		writeTestFile(t, repo, "src/cart.test.ts", "test('x', () => expect(1).toBe(2));\n")

		input := `{"session_id":"s1","cwd":` + mustJSON(t, repo) + `,"hook_event_name":"Stop","stop_hook_active":false}`
		root := newRootCmd()
		var stdout, stderr bytes.Buffer
		root.SetIn(strings.NewReader(input))
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs([]string{"gate", "--format", "claude"})

		if err := root.Execute(); err != nil {
			t.Fatalf("unexpected error: %v (stderr=%s)", err, stderr.String())
		}
		if !strings.Contains(stdout.String(), `"decision":"block"`) {
			t.Errorf("expected block decision JSON, got %q", stdout.String())
		}
	})

	t.Run("claude format fails open on bad JSON", func(t *testing.T) {
		root := newRootCmd()
		var stdout, stderr bytes.Buffer
		root.SetIn(strings.NewReader("{bad"))
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs([]string{"gate", "--format", "claude"})

		if err := root.Execute(); err != nil {
			t.Fatalf("expected fail-open nil error, got %v", err)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout, got %q", stdout.String())
		}
		if !strings.Contains(stderr.String(), "donegate gate:") {
			t.Errorf("expected error on stderr, got %q", stderr.String())
		}
	})
}

func TestSessionStartClaudeFormat(t *testing.T) {
	t.Run("non-git cwd exits 0 silently", func(t *testing.T) {
		t.Setenv("DONEGATE_HOME", t.TempDir())
		nonRepo := t.TempDir()
		input := `{"session_id":"cs1","cwd":` + mustJSON(t, nonRepo) + `,"source":"startup"}`

		root := newRootCmd()
		var stdout, stderr bytes.Buffer
		root.SetIn(strings.NewReader(input))
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs([]string{"session", "start", "--format", "claude"})

		if err := root.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if stdout.Len() != 0 || stderr.Len() != 0 {
			t.Errorf("expected silent exit, got stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
	})

	t.Run("starts session and reuses on resume or compact with matching session_id", func(t *testing.T) {
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)

		runStart := func(sessionID, source string) string {
			input := `{"session_id":` + mustJSON(t, sessionID) + `,"cwd":` + mustJSON(t, repo) + `,"source":` + mustJSON(t, source) + `}`
			root := newRootCmd()
			var stdout, stderr bytes.Buffer
			root.SetIn(strings.NewReader(input))
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs([]string{"session", "start", "--format", "claude"})
			if err := root.Execute(); err != nil {
				t.Fatalf("unexpected error: %v (stderr=%s)", err, stderr.String())
			}
			return stdout.String()
		}

		out1 := runStart("cs-100", "startup")
		sess1, err := session.Current(repo)
		if err != nil {
			t.Fatalf("session.Current failed: %v", err)
		}
		if sess1.Harness != "claude-code" || sess1.HarnessSession != "cs-100" {
			t.Errorf("unexpected session metadata: %+v", sess1)
		}
		if !strings.Contains(out1, `"hookEventName":"SessionStart"`) || !strings.Contains(out1, sess1.ID) {
			t.Errorf("unexpected stdout: %q", out1)
		}

		out2 := runStart("cs-100", "resume")
		sess2, err := session.Current(repo)
		if err != nil {
			t.Fatalf("session.Current after resume failed: %v", err)
		}
		if sess2.ID != sess1.ID {
			t.Errorf("resume created new session %q, want reused %q", sess2.ID, sess1.ID)
		}
		if !strings.Contains(out2, sess1.ID) {
			t.Errorf("expected reused session ID in stdout, got %q", out2)
		}

		out3 := runStart("cs-100", "compact")
		sess3, err := session.Current(repo)
		if err != nil {
			t.Fatalf("session.Current after compact failed: %v", err)
		}
		if sess3.ID != sess1.ID {
			t.Errorf("compact created new session %q, want reused %q", sess3.ID, sess1.ID)
		}
		if !strings.Contains(out3, sess1.ID) {
			t.Errorf("expected reused session ID in stdout, got %q", out3)
		}

		_ = runStart("cs-200", "resume")
		sess4, err := session.Current(repo)
		if err != nil {
			t.Fatalf("session.Current after different session_id failed: %v", err)
		}
		if sess4.ID == sess1.ID || sess4.HarnessSession != "cs-200" {
			t.Errorf("expected new session for different session_id, got %+v", sess4)
		}
	})
}
