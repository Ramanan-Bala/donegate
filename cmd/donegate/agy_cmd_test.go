package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/session"
)

func TestSessionStartAgyFormat(t *testing.T) {
	t.Run("starts session, injects context, reuses on matching conversationId, skips outside git", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)

		in1 := `{"conversationId":"conv-1","cwd":` + mustJSON(t, repo) + `,"workspacePaths":[` + mustJSON(t, repo) + `],"hookEventName":"SessionStart"}`
		stdout, stderr, err := execCmd(t, in1, "session", "start", "--format", "agy")
		if err != nil {
			t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
		}

		cur, err := session.Current(repo)
		if err != nil {
			t.Fatalf("session.Current: %v", err)
		}
		if cur.Harness != "antigravity" || cur.HarnessSession != "conv-1" {
			t.Errorf("unexpected session metadata: %+v", cur)
		}

		var out1 struct {
			InjectSteps []struct {
				SystemMessage struct {
					SystemMessage string `json:"systemMessage"`
				} `json:"systemMessage"`
			} `json:"injectSteps"`
		}
		if err := json.Unmarshal([]byte(stdout), &out1); err != nil {
			t.Fatalf("invalid json %q: %v", stdout, err)
		}
		if len(out1.InjectSteps) != 1 || !strings.Contains(out1.InjectSteps[0].SystemMessage.SystemMessage, cur.ID) {
			t.Errorf("expected injectSteps with session %q, got %q", cur.ID, stdout)
		}

		// Same conversationId reuses the existing donegate session.
		in2 := `{"conversationId":"conv-1","workspacePaths":[` + mustJSON(t, repo) + `],"hookEventName":"SessionStart"}`
		stdout2, _, err := execCmd(t, in2, "session", "start", "--format", "agy")
		if err != nil {
			t.Fatalf("unexpected error on reuse: %v", err)
		}
		cur2, err := session.Current(repo)
		if err != nil {
			t.Fatalf("session.Current after reuse: %v", err)
		}
		if cur2.ID != cur.ID {
			t.Errorf("expected reused session %q, got %q", cur.ID, cur2.ID)
		}
		if !strings.Contains(stdout2, cur.ID) {
			t.Errorf("expected session ID %q in stdout, got %q", cur.ID, stdout2)
		}

		// Different conversationId starts a new session.
		in3 := `{"conversationId":"conv-2","cwd":` + mustJSON(t, repo) + `,"hookEventName":"SessionStart"}`
		if _, _, err := execCmd(t, in3, "session", "start", "--format", "agy"); err != nil {
			t.Fatalf("unexpected error on new conversationId: %v", err)
		}
		cur3, err := session.Current(repo)
		if err != nil {
			t.Fatalf("session.Current after new conversationId: %v", err)
		}
		if cur3.ID == cur.ID || cur3.HarnessSession != "conv-2" {
			t.Errorf("expected new session for conv-2, got %+v", cur3)
		}

		// Outside git repo outputs {}.
		nonRepo := t.TempDir()
		inSkip := `{"conversationId":"conv-3","cwd":` + mustJSON(t, nonRepo) + `,"hookEventName":"SessionStart"}`
		outSkip, errSkip, err := execCmd(t, inSkip, "session", "start", "--format", "agy")
		if err != nil {
			t.Fatalf("unexpected error outside git: %v", err)
		}
		if outSkip != "{}\n" {
			t.Errorf("stdout = %q, want %q", outSkip, "{}\n")
		}
		if errSkip != "" {
			t.Errorf("expected empty stderr outside git, got %q", errSkip)
		}
	})

	t.Run("garbage stdin fails open with {} and exit 0", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DONEGATE_HOME", t.TempDir())

		stdout, stderr, err := execCmd(t, "{bad-json", "session", "start", "--format", "agy")
		if err != nil {
			t.Fatalf("expected nil error (exit 0), got %v", err)
		}
		if stdout != "{}\n" {
			t.Errorf("stdout = %q, want %q", stdout, "{}\n")
		}
		if !strings.Contains(stderr, "donegate session start:") {
			t.Errorf("expected stderr message, got %q", stderr)
		}
	})
}

func TestGuardAgyFormat(t *testing.T) {
	t.Run("denies edit_file on existing test file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)
		if _, err := session.Start(repo, "antigravity"); err != nil {
			t.Fatalf("session.Start: %v", err)
		}

		in := `{"conversationId":"c1","cwd":` + mustJSON(t, repo) + `,"hookEventName":"PreToolUse","toolCall":{"id":"t1","name":"edit_file","args":{"Path":"src/cart.test.ts","Target":"1","Replacement":"2"}}}`
		stdout, stderr, err := execCmd(t, in, "guard", "--format", "agy")
		if err != nil {
			t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
		}

		var got struct {
			Decision string `json:"decision"`
			Reason   string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("invalid json stdout %q: %v", stdout, err)
		}
		if got.Decision != "deny" {
			t.Fatalf("decision = %q, want deny (%q)", got.Decision, stdout)
		}
		if !strings.Contains(got.Reason, "T001") {
			t.Errorf("reason = %q, want T001", got.Reason)
		}
	})

	t.Run("prints decision ask for edit_file on source file and for read_file on test file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)
		if _, err := session.Start(repo, "antigravity"); err != nil {
			t.Fatalf("session.Start: %v", err)
		}

		wantAsk := `{"decision":"ask"}` + "\n"
		inSrc := `{"conversationId":"c1","cwd":` + mustJSON(t, repo) + `,"hookEventName":"PreToolUse","toolCall":{"id":"t2","name":"edit_file","args":{"Path":"src/cart.ts","Target":"1","Replacement":"2"}}}`
		outSrc, _, err := execCmd(t, inSrc, "guard", "--format", "agy")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outSrc != wantAsk {
			t.Errorf("source file stdout = %q, want %q", outSrc, wantAsk)
		}

		inRead := `{"conversationId":"c1","cwd":` + mustJSON(t, repo) + `,"hookEventName":"PreToolUse","toolCall":{"id":"t3","name":"read_file","args":{"Path":"src/cart.test.ts"}}}`
		outRead, _, err := execCmd(t, inRead, "guard", "--format", "agy")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outRead != wantAsk {
			t.Errorf("read_file stdout = %q, want %q", outRead, wantAsk)
		}
	})

	t.Run("not inside git repo prints decision ask", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DONEGATE_HOME", t.TempDir())
		nonRepo := t.TempDir()

		in := `{"conversationId":"c1","cwd":` + mustJSON(t, nonRepo) + `,"hookEventName":"PreToolUse","toolCall":{"id":"t1","name":"edit_file","args":{"Path":"a.test.ts"}}}`
		stdout, stderr, err := execCmd(t, in, "guard", "--format", "agy")
		if err != nil {
			t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
		}
		want := `{"decision":"ask"}` + "\n"
		if stdout != want {
			t.Errorf("stdout = %q, want %q", stdout, want)
		}
	})

	t.Run("garbage stdin fails open with decision ask and exit 0", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DONEGATE_HOME", t.TempDir())

		stdout, stderr, err := execCmd(t, "{not-valid-json", "guard", "--format", "agy")
		if err != nil {
			t.Fatalf("expected nil error (exit 0), got %v", err)
		}
		want := `{"decision":"ask"}` + "\n"
		if stdout != want {
			t.Errorf("stdout = %q, want %q", stdout, want)
		}
		if !strings.Contains(stderr, "donegate guard:") {
			t.Errorf("expected stderr message, got %q", stderr)
		}
	})
}

func TestGateAgyFormat(t *testing.T) {
	t.Run("prints continue after existing test is edited", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)
		if _, err := session.Start(repo, "antigravity"); err != nil {
			t.Fatalf("session.Start: %v", err)
		}
		writeTestFile(t, repo, "src/cart.test.ts", "test('x', () => expect(1).toBe(2));\n")

		in := `{"conversationId":"c1","cwd":` + mustJSON(t, repo) + `,"hookEventName":"Stop","terminationReason":"NO_TOOL_CALL","continuationActive":false,"executionNum":1}`
		stdout, stderr, err := execCmd(t, in, "gate", "--format", "agy")
		if err != nil {
			t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
		}

		var got struct {
			Decision string `json:"decision"`
			Reason   string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("invalid json %q: %v", stdout, err)
		}
		if got.Decision != "continue" {
			t.Fatalf("decision = %q, want continue (%q)", got.Decision, stdout)
		}
		if !strings.Contains(got.Reason, "T001") {
			t.Errorf("reason = %q, want T001", got.Reason)
		}
	})

	t.Run("prints {} for USER_CANCELED even with edited test", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)
		if _, err := session.Start(repo, "antigravity"); err != nil {
			t.Fatalf("session.Start: %v", err)
		}
		writeTestFile(t, repo, "src/cart.test.ts", "test('x', () => expect(1).toBe(2));\n")

		in := `{"conversationId":"c1","cwd":` + mustJSON(t, repo) + `,"hookEventName":"Stop","terminationReason":"USER_CANCELED","executionNum":1}`
		stdout, stderr, err := execCmd(t, in, "gate", "--format", "agy")
		if err != nil {
			t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
		}
		if stdout != "{}\n" {
			t.Errorf("stdout = %q, want %q", stdout, "{}\n")
		}
		if stderr != "" {
			t.Errorf("expected empty stderr, got %q", stderr)
		}
	})

	t.Run("prints {} with no changes", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)
		if _, err := session.Start(repo, "antigravity"); err != nil {
			t.Fatalf("session.Start: %v", err)
		}

		in := `{"conversationId":"c1","cwd":` + mustJSON(t, repo) + `,"hookEventName":"Stop","terminationReason":"NO_TOOL_CALL","executionNum":1}`
		stdout, stderr, err := execCmd(t, in, "gate", "--format", "agy")
		if err != nil {
			t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
		}
		if stdout != "{}\n" {
			t.Errorf("stdout = %q, want %q", stdout, "{}\n")
		}
		if stderr != "" {
			t.Errorf("expected empty stderr, got %q", stderr)
		}
	})

	t.Run("prints {} to stdout and user message to stderr on PASS", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)
		if _, err := session.Start(repo, "antigravity"); err != nil {
			t.Fatalf("session.Start: %v", err)
		}
		writeTestFile(t, repo, "src/cart.ts", "export const x = 2;\n")

		in := `{"conversationId":"c1","cwd":` + mustJSON(t, repo) + `,"hookEventName":"Stop","terminationReason":"NO_TOOL_CALL","executionNum":1}`
		stdout, stderr, err := execCmd(t, in, "gate", "--format", "agy")
		if err != nil {
			t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
		}
		if stdout != "{}\n" {
			t.Errorf("stdout = %q, want %q", stdout, "{}\n")
		}
		if !strings.Contains(stderr, "donegate: verified") {
			t.Errorf("expected verified message on stderr, got %q", stderr)
		}
	})

	t.Run("not inside git repo prints {}", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DONEGATE_HOME", t.TempDir())
		nonRepo := t.TempDir()

		in := `{"conversationId":"c1","cwd":` + mustJSON(t, nonRepo) + `,"hookEventName":"Stop","terminationReason":"NO_TOOL_CALL","executionNum":1}`
		stdout, stderr, err := execCmd(t, in, "gate", "--format", "agy")
		if err != nil {
			t.Fatalf("unexpected error: %v (stderr=%q)", err, stderr)
		}
		if stdout != "{}\n" {
			t.Errorf("stdout = %q, want %q", stdout, "{}\n")
		}
		if stderr != "" {
			t.Errorf("expected empty stderr outside git, got %q", stderr)
		}
	})

	t.Run("garbage stdin fails open with {} and exit 0", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DONEGATE_HOME", t.TempDir())

		stdout, stderr, err := execCmd(t, "{bad", "gate", "--format", "agy")
		if err != nil {
			t.Fatalf("expected nil error (exit 0), got %v", err)
		}
		if stdout != "{}\n" {
			t.Errorf("stdout = %q, want %q", stdout, "{}\n")
		}
		if !strings.Contains(stderr, "donegate gate:") {
			t.Errorf("expected stderr message, got %q", stderr)
		}
	})
}
