package main

import (
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/allow"
	"github.com/Ramanan-Bala/donegate/internal/session"
)

func TestMultiSessionCLIWorkflow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DONEGATE_HOME", t.TempDir())
	repo := initTestRepo(t)

	// Agent B starts session in claude format.
	startB := `{"session_id":"claude-B","cwd":` + mustJSON(t, repo) + `,"source":"startup"}`
	if _, stderr, err := execCmd(t, startB, "session", "start", "--format", "claude"); err != nil {
		t.Fatalf("start B: %v (stderr=%q)", err, stderr)
	}

	// Agent B makes a clean source change and a test edit.
	writeTestFile(t, repo, "src/b_only.ts", "export const b = 1;\n")
	writeTestFile(t, repo, "src/b.test.ts", "test('b', () => expect(1).toBe(1));\n")

	// Agent A starts session after B's edits.
	startA := `{"session_id":"claude-A","cwd":` + mustJSON(t, repo) + `,"source":"startup"}`
	if _, stderr, err := execCmd(t, startA, "session", "start", "--format", "claude"); err != nil {
		t.Fatalf("start A: %v (stderr=%q)", err, stderr)
	}

	// Agent B resumes later: must still reuse B's session even though A updated `current`.
	resumeB := `{"session_id":"claude-B","cwd":` + mustJSON(t, repo) + `,"source":"resume"}`
	outResumeB, _, err := execCmd(t, resumeB, "session", "start", "--format", "claude")
	if err != nil {
		t.Fatalf("resume B: %v", err)
	}
	sessB, err := session.CurrentFor(repo, "claude-B")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(outResumeB, sessB.ID) {
		t.Errorf("resume B did not reuse session %q: %q", sessB.ID, outResumeB)
	}

	// Grant allowance only to session A for src/cart.test.ts.
	sessA, err := session.CurrentFor(repo, "claude-A")
	if err != nil {
		t.Fatal(err)
	}
	if err := allow.Add(sessA, "T001", []string{"src/cart.test.ts"}, "human"); err != nil {
		t.Fatal(err)
	}

	// Guard for Agent A allows editing src/cart.test.ts; Guard for Agent B denies it!
	guardA := `{"session_id":"claude-A","cwd":` + mustJSON(t, repo) + `,"hook_event_name":"PreToolUse","tool_name":"Edit","tool_input":{"file_path":"src/cart.test.ts"}}`
	outGA, _, err := execCmd(t, guardA, "guard", "--format", "claude")
	if err != nil {
		t.Fatalf("guard A: %v", err)
	}
	if outGA != "" {
		t.Errorf("expected guard A to allow src/cart.test.ts, got %q", outGA)
	}

	guardB := `{"session_id":"claude-B","cwd":` + mustJSON(t, repo) + `,"hook_event_name":"PreToolUse","tool_name":"Edit","tool_input":{"file_path":"src/cart.test.ts"}}`
	outGB, _, err := execCmd(t, guardB, "guard", "--format", "claude")
	if err != nil {
		t.Fatalf("guard B: %v", err)
	}
	if !strings.Contains(outGB, `"permissionDecision":"deny"`) {
		t.Errorf("expected guard B to deny src/cart.test.ts, got %q", outGB)
	}

	// Agent A edits src/cart.test.ts (allowed) -> Agent A's gate passes; Agent B's gate blocks on src/cart.test.ts!
	writeTestFile(t, repo, "src/cart.test.ts", "test('x', () => expect(1).toBe(2));\n")

	gateA := `{"session_id":"claude-A","cwd":` + mustJSON(t, repo) + `,"hook_event_name":"Stop","stop_hook_active":false}`
	outGateA, _, err := execCmd(t, gateA, "gate", "--format", "claude")
	if err != nil {
		t.Fatalf("gate A: %v", err)
	}
	if strings.Contains(outGateA, `"decision":"block"`) {
		t.Errorf("expected gate A to pass, got %q", outGateA)
	}

	gateB := `{"session_id":"claude-B","cwd":` + mustJSON(t, repo) + `,"hook_event_name":"Stop","stop_hook_active":false}`
	outGateB, _, err := execCmd(t, gateB, "gate", "--format", "claude")
	if err != nil {
		t.Fatalf("gate B: %v", err)
	}
	if !strings.Contains(outGateB, `"decision":"block"`) || !strings.Contains(outGateB, "src/cart.test.ts") {
		t.Errorf("expected gate B to block on src/cart.test.ts, got %q", outGateB)
	}
}
