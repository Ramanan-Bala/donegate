package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/allow"
	"github.com/Ramanan-Bala/donegate/internal/evidence"
	"github.com/Ramanan-Bala/donegate/internal/session"
)

func TestRunAllowedTestEditPassesAndRecordsEvidence(t *testing.T) {
	t.Setenv("DONEGATE_HOME", t.TempDir())
	dir := initRepo(t, map[string]string{
		"src/other.test.ts": "test('other', () => expect(1).toBe(1));\n",
	})

	sess, err := session.StartWith(dir, session.StartOptions{
		Harness:        "claude-code",
		HarnessSession: "sess-1",
	})
	if err != nil {
		t.Fatalf("session.StartWith: %v", err)
	}

	if err := allow.Add(sess, "T001", []string{"src/cart.test.ts"}, "alice"); err != nil {
		t.Fatalf("allow.Add: %v", err)
	}

	// Edit allowed test -> PASS, allowed recorded in evidence + markdown, message never mentions it.
	writeRepoFile(t, dir, "src/cart.test.ts", "test('total', () => expect(total()).toBe(2));\n")

	out, err := Run(Options{Cwd: dir, HarnessSession: "sess-1", Now: fixedNow()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Block || out.Verdict != "PASS" {
		t.Fatalf("got Block=%v Verdict=%q Message=%q, want PASS", out.Block, out.Verdict, out.Message)
	}
	if out.Message != "" {
		t.Errorf("expected empty Message on PASS, got %q", out.Message)
	}

	ev, err := evidence.Read(out.EvidencePath)
	if err != nil {
		t.Fatalf("evidence.Read: %v", err)
	}
	if ev.Verdict != "PASS" {
		t.Errorf("ev.Verdict = %q, want PASS", ev.Verdict)
	}
	if len(ev.Allowed) == 0 {
		t.Fatalf("expected non-empty ev.Allowed")
	}
	foundT001 := false
	for _, a := range ev.Allowed {
		if a.Rule == "T001" && a.Path == "src/cart.test.ts" && a.By == "alice" {
			foundT001 = true
		}
	}
	if !foundT001 {
		t.Errorf("expected T001 allowance for src/cart.test.ts in ev.Allowed, got %+v", ev.Allowed)
	}

	mdBytes, err := os.ReadFile(filepath.Join(filepath.Dir(out.EvidencePath), "evidence.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mdBytes), "### Approved exceptions") || !strings.Contains(string(mdBytes), "src/cart.test.ts") {
		t.Errorf("expected Approved exceptions in markdown:\n%s", mdBytes)
	}

	// Edit a non-allowed test as well -> still blocked, and agent message never mentions allowed test.
	writeRepoFile(t, dir, "src/other.test.ts", "test('other', () => expect(1).toBe(2));\n")
	out2, err := Run(Options{Cwd: dir, HarnessSession: "sess-1", Now: fixedNow()})
	if err != nil {
		t.Fatalf("Run 2: %v", err)
	}
	if !out2.Block || out2.Verdict != "FAIL" {
		t.Fatalf("got Block=%v Verdict=%q, want FAIL", out2.Block, out2.Verdict)
	}
	if !strings.Contains(out2.Message, "src/other.test.ts") {
		t.Errorf("expected non-allowed test in block message, got %q", out2.Message)
	}
	if strings.Contains(out2.Message, "src/cart.test.ts") {
		t.Errorf("block message must never mention allowed test src/cart.test.ts, got %q", out2.Message)
	}
}

func TestRunMultiHarnessSessionsSeparateBaselines(t *testing.T) {
	t.Setenv("DONEGATE_HOME", t.TempDir())
	dir := initRepo(t, map[string]string{
		"src/b.test.ts": "test('b', () => expect(1).toBe(1));\n",
	})

	// Agent B starts first and modifies a source file AND a test file before Agent A starts.
	if _, err := session.StartWith(dir, session.StartOptions{
		Harness:        "claude-code",
		HarnessSession: "harness-B",
	}); err != nil {
		t.Fatalf("StartWith B: %v", err)
	}
	writeRepoFile(t, dir, "src/b_work.ts", "export const b = 1;\n")
	writeRepoFile(t, dir, "src/b.test.ts", "test('b', () => expect(1).toBe(2));\n")

	// Agent A starts after B's changes; B's changes are part of A's baseline, not A's diff.
	if _, err := session.StartWith(dir, session.StartOptions{
		Harness:        "claude-code",
		HarnessSession: "harness-A",
	}); err != nil {
		t.Fatalf("StartWith A: %v", err)
	}

	// Agent A's gate sees no diff and passes.
	outA, err := Run(Options{Cwd: dir, HarnessSession: "harness-A", Now: fixedNow()})
	if err != nil {
		t.Fatalf("Run A: %v", err)
	}
	if outA.Block || outA.Verdict != "PASS" {
		t.Fatalf("A got Block=%v Verdict=%q Message=%q, want PASS", outA.Block, outA.Verdict, outA.Message)
	}

	// Agent B's gate judges against B's baseline and catches src/b.test.ts.
	outB, err := Run(Options{Cwd: dir, HarnessSession: "harness-B", Now: fixedNow()})
	if err != nil {
		t.Fatalf("Run B: %v", err)
	}
	if !outB.Block || outB.Verdict != "FAIL" {
		t.Fatalf("B got Block=%v Verdict=%q, want FAIL", outB.Block, outB.Verdict)
	}
	if !strings.Contains(outB.Message, "src/b.test.ts") {
		t.Errorf("B message should mention src/b.test.ts, got %q", outB.Message)
	}
}
