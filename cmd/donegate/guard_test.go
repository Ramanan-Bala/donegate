package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardCmd(t *testing.T) {
	t.Run("denies protected test file with exit 0", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", filepath.Join(tmp, "home"))
		t.Setenv("DONEGATE_HOME", filepath.Join(tmp, "state"))

		repo := filepath.Join(tmp, "repo")
		if err := os.MkdirAll(filepath.Join(repo, "__tests__"), 0o755); err != nil {
			t.Fatal(err)
		}
		testFile := filepath.Join(repo, "__tests__", "a.test.ts")
		if err := os.WriteFile(testFile, []byte("test\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		// Use home harness path so we don't even need git init here, or test non-repo harness deny.
		harnessFile := filepath.Join(tmp, "home", ".claude", "settings.json")
		input := `{"session_id":"s1","cwd":` + mustJSON(t, repo) + `,"hook_event_name":"PreToolUse","tool_name":"Edit","tool_input":{"file_path":` + mustJSON(t, harnessFile) + `}}`

		root := newRootCmd()
		var stdout, stderr bytes.Buffer
		root.SetIn(strings.NewReader(input))
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs([]string{"guard", "--format", "claude"})

		if err := root.Execute(); err != nil {
			t.Fatalf("unexpected error: %v (stderr=%s)", err, stderr.String())
		}
		if !strings.Contains(stdout.String(), `"permissionDecision":"deny"`) {
			t.Errorf("expected deny JSON on stdout, got %q", stdout.String())
		}
	})

	t.Run("fails open on bad JSON", func(t *testing.T) {
		root := newRootCmd()
		var stdout, stderr bytes.Buffer
		root.SetIn(strings.NewReader("{not-json"))
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs([]string{"guard", "--format", "claude"})

		if err := root.Execute(); err != nil {
			t.Fatalf("expected fail-open nil error, got %v", err)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout on fail-open, got %q", stdout.String())
		}
		if !strings.Contains(stderr.String(), "donegate guard:") {
			t.Errorf("expected 'donegate guard:' on stderr, got %q", stderr.String())
		}
	})

	t.Run("unsupported format exits 2", func(t *testing.T) {
		oldStderr := os.Stderr
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stderr = w
		code := run([]string{"guard", "--format", "unknown"})
		_ = w.Close()
		os.Stderr = oldStderr

		var errBuf bytes.Buffer
		_, _ = errBuf.ReadFrom(r)
		_ = r.Close()

		if code != 2 {
			t.Errorf("exit code = %d, want 2 (stderr=%q)", code, errBuf.String())
		}
	})
}

func mustJSON(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
