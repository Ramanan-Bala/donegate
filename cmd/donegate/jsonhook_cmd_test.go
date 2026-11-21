package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/adapters/jsonhook"
	"github.com/Ramanan-Bala/donegate/internal/session"
)

func execCmd(t *testing.T, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := newRootCmd()
	var outBuf, errBuf bytes.Buffer
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

func TestGuardJSONFormat(t *testing.T) {
	t.Run("denies edit to existing test file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)
		if _, err := session.Start(repo, "opencode"); err != nil {
			t.Fatalf("session.Start: %v", err)
		}

		in := `{"tool":"edit","paths":["src/cart.test.ts"],"command":"","cwd":` + mustJSON(t, repo) + `}`
		stdout, stderr, err := execCmd(t, in, "guard", "--format", "json")
		if err != nil {
			t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
		}

		var got jsonhook.GuardOutput
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("invalid json stdout %q: %v", stdout, err)
		}
		if got.Allow {
			t.Fatalf("expected deny, got allow=true (%q)", stdout)
		}
		if got.Rule != "T001" {
			t.Errorf("rule = %q, want T001", got.Rule)
		}
		if got.Path != "src/cart.test.ts" {
			t.Errorf("path = %q, want src/cart.test.ts", got.Path)
		}
		if !strings.Contains(got.Reason, "T001") {
			t.Errorf("reason = %q, want to mention T001", got.Reason)
		}
	})

	t.Run("allows edit to source file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)
		if _, err := session.Start(repo, "opencode"); err != nil {
			t.Fatalf("session.Start: %v", err)
		}

		in := `{"tool":"edit","paths":["src/cart.ts"],"command":"","cwd":` + mustJSON(t, repo) + `}`
		stdout, stderr, err := execCmd(t, in, "guard", "--format", "json")
		if err != nil {
			t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
		}
		if stdout != `{"allow":true}`+"\n" {
			t.Errorf("stdout = %q, want %q", stdout, `{"allow":true}`+"\n")
		}
	})

	t.Run("denies bash snapshot update", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)

		in := `{"tool":"bash","paths":[],"command":"vitest -u","cwd":` + mustJSON(t, repo) + `}`
		stdout, _, err := execCmd(t, in, "guard", "--format", "json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var got jsonhook.GuardOutput
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("invalid json stdout %q: %v", stdout, err)
		}
		if got.Allow || got.Rule != "T004" {
			t.Errorf("got %+v, want deny T004", got)
		}
	})

	t.Run("garbage input fails open with error JSON and exit 0", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DONEGATE_HOME", t.TempDir())

		stdout, _, err := execCmd(t, "{not-valid-json", "guard", "--format", "json")
		if err != nil {
			t.Fatalf("expected nil error (exit 0), got %v", err)
		}
		var got jsonhook.GuardOutput
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("invalid json stdout %q: %v", stdout, err)
		}
		if !got.Allow {
			t.Errorf("expected allow=true on error, got %q", stdout)
		}
		if got.Error == "" {
			t.Errorf("expected non-empty error field, got %q", stdout)
		}
	})
}

func TestSessionStartJSONFormat(t *testing.T) {
	t.Run("starts session, reuses on resume, and skips outside git", func(t *testing.T) {
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)

		in1 := `{"cwd":` + mustJSON(t, repo) + `,"session":"oc-1","source":"startup"}`
		out1, stderr, err := execCmd(t, in1, "session", "start", "--format", "json", "--harness", "opencode")
		if err != nil {
			t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
		}
		var resp1 jsonhook.SessionStartOutput
		if err := json.Unmarshal([]byte(out1), &resp1); err != nil {
			t.Fatalf("invalid json %q: %v", out1, err)
		}
		if resp1.Session == "" || resp1.Reused == nil || *resp1.Reused {
			t.Fatalf("expected new session with reused=false, got %q", out1)
		}
		if !strings.Contains(resp1.Context, resp1.Session) {
			t.Errorf("context %q missing session id %q", resp1.Context, resp1.Session)
		}

		cur, err := session.Current(repo)
		if err != nil {
			t.Fatalf("session.Current: %v", err)
		}
		if cur.Harness != "opencode" || cur.HarnessSession != "oc-1" {
			t.Errorf("unexpected session metadata: %+v", cur)
		}

		// Resume with same harness session reuses it.
		in2 := `{"cwd":` + mustJSON(t, repo) + `,"session":"oc-1","source":"resume"}`
		out2, _, err := execCmd(t, in2, "session", "start", "--format", "json", "--harness", "opencode")
		if err != nil {
			t.Fatalf("unexpected error on resume: %v", err)
		}
		var resp2 jsonhook.SessionStartOutput
		if err := json.Unmarshal([]byte(out2), &resp2); err != nil {
			t.Fatalf("invalid json %q: %v", out2, err)
		}
		if resp2.Session != resp1.Session || resp2.Reused == nil || !*resp2.Reused {
			t.Errorf("expected reused session %q, got %q", resp1.Session, out2)
		}

		// Outside git repo returns {"skipped":true}.
		nonRepo := t.TempDir()
		inSkip := `{"cwd":` + mustJSON(t, nonRepo) + `,"session":"pi-1","source":"startup"}`
		outSkip, _, err := execCmd(t, inSkip, "session", "start", "--format", "json", "--harness", "pi")
		if err != nil {
			t.Fatalf("unexpected error outside git: %v", err)
		}
		if outSkip != `{"skipped":true}`+"\n" {
			t.Errorf("stdout = %q, want %q", outSkip, `{"skipped":true}`+"\n")
		}
	})

	t.Run("garbage input fails open with error JSON and exit 0", func(t *testing.T) {
		t.Setenv("DONEGATE_HOME", t.TempDir())
		stdout, _, err := execCmd(t, "not-json", "session", "start", "--format", "json", "--harness", "pi")
		if err != nil {
			t.Fatalf("expected nil error (exit 0), got %v", err)
		}
		var resp jsonhook.SessionStartOutput
		if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
			t.Fatalf("invalid json %q: %v", stdout, err)
		}
		if resp.Error == "" {
			t.Errorf("expected error field, got %q", stdout)
		}
	})
}

func TestGateJSONFormat(t *testing.T) {
	t.Run("no changes returns block false only", func(t *testing.T) {
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)
		if _, err := session.Start(repo, "opencode"); err != nil {
			t.Fatalf("session.Start: %v", err)
		}

		in := `{"cwd":` + mustJSON(t, repo) + `,"session":"oc-1"}`
		stdout, stderr, err := execCmd(t, in, "gate", "--format", "json")
		if err != nil {
			t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
		}
		if stdout != `{"block":false}`+"\n" {
			t.Errorf("stdout = %q, want %q", stdout, `{"block":false}`+"\n")
		}
	})

	t.Run("no session returns block false only", func(t *testing.T) {
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)

		in := `{"cwd":` + mustJSON(t, repo) + `}`
		stdout, _, err := execCmd(t, in, "gate", "--format", "json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if stdout != `{"block":false}`+"\n" {
			t.Errorf("stdout = %q, want %q", stdout, `{"block":false}`+"\n")
		}
	})

	t.Run("blocks on edited existing test", func(t *testing.T) {
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)
		if _, err := session.Start(repo, "opencode"); err != nil {
			t.Fatalf("session.Start: %v", err)
		}
		writeTestFile(t, repo, "src/cart.test.ts", "test('x', () => expect(1).toBe(2));\n")

		in := `{"cwd":` + mustJSON(t, repo) + `,"session":"oc-1"}`
		stdout, stderr, err := execCmd(t, in, "gate", "--format", "json")
		if err != nil {
			t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
		}

		var got jsonhook.GateOutput
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("invalid json %q: %v", stdout, err)
		}
		if !got.Block {
			t.Fatalf("expected block=true, got %q", stdout)
		}
		if got.Verdict != "FAIL" {
			t.Errorf("verdict = %q, want FAIL", got.Verdict)
		}
		if got.Attempt == nil || *got.Attempt != 1 {
			t.Errorf("attempt = %v, want 1", got.Attempt)
		}
		if got.MaxBlocks != 5 {
			t.Errorf("max_blocks = %d, want 5", got.MaxBlocks)
		}
		if !strings.Contains(got.Message, "T001") {
			t.Errorf("message = %q, want to contain T001", got.Message)
		}
		if got.Evidence == "" {
			t.Errorf("expected evidence path, got empty")
		}
	})

	t.Run("PASS on honest source change", func(t *testing.T) {
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)
		if _, err := session.Start(repo, "pi"); err != nil {
			t.Fatalf("session.Start: %v", err)
		}
		writeTestFile(t, repo, "src/cart.ts", "export const x = 2;\n")

		in := `{"cwd":` + mustJSON(t, repo) + `,"session":"pi-1"}`
		stdout, stderr, err := execCmd(t, in, "gate", "--format", "json")
		if err != nil {
			t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
		}

		var got jsonhook.GateOutput
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("invalid json %q: %v", stdout, err)
		}
		if got.Block {
			t.Fatalf("expected block=false, got %q", stdout)
		}
		if got.Verdict != "PASS" {
			t.Errorf("verdict = %q, want PASS", got.Verdict)
		}
		if got.Attempt == nil || *got.Attempt != 0 {
			t.Errorf("attempt = %v, want 0", got.Attempt)
		}
		if got.MaxBlocks != 5 {
			t.Errorf("max_blocks = %d, want 5", got.MaxBlocks)
		}
		if !strings.Contains(got.UserMessage, "donegate: verified") {
			t.Errorf("user_message = %q, want verified prefix", got.UserMessage)
		}
		if got.Evidence == "" {
			t.Errorf("expected evidence path, got empty")
		}
	})

	t.Run("garbage input fails open with error JSON and exit 0", func(t *testing.T) {
		t.Setenv("DONEGATE_HOME", t.TempDir())
		stdout, _, err := execCmd(t, "{bad", "gate", "--format", "json")
		if err != nil {
			t.Fatalf("expected nil error (exit 0), got %v", err)
		}
		var got jsonhook.GateOutput
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("invalid json %q: %v", stdout, err)
		}
		if got.Block {
			t.Errorf("expected block=false on error, got %q", stdout)
		}
		if got.Error == "" {
			t.Errorf("expected non-empty error field, got %q", stdout)
		}
	})
}
