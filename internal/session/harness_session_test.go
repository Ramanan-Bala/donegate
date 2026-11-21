package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/diff"
)

func TestCurrentForMultiHarnessSessions(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("DONEGATE_HOME", stateDir)

	dir, _ := initCommitRepo(t)

	// Agent B starts first and makes a change.
	sessB, err := StartWith(dir, StartOptions{
		Harness:        "claude-code",
		HarnessSession: "agent-B",
	})
	if err != nil {
		t.Fatalf("StartWith B: %v", err)
	}
	writeRepoFile(t, dir, "src/b_only.ts", []byte("export const b = 1;\n"))

	// Agent A starts after B's change; B's change must be in A's baseline.
	sessA, err := StartWith(dir, StartOptions{
		Harness:        "claude-code",
		HarnessSession: "agent-A",
	})
	if err != nil {
		t.Fatalf("StartWith A: %v", err)
	}

	// Verify harness pointer file was written for both.
	resolvedRepo, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	ptrB := filepath.Join(stateDir, "repos", repoKey(resolvedRepo), "harness", shortHash("agent-B"))
	rawB, err := os.ReadFile(ptrB)
	if err != nil {
		t.Fatalf("read harness pointer B: %v", err)
	}
	if strings.TrimSpace(string(rawB)) != sessB.ID {
		t.Errorf("pointer B = %q, want %q", strings.TrimSpace(string(rawB)), sessB.ID)
	}

	// Current() now points to A (most recent), while CurrentFor returns each agent's own session.
	cur, err := Current(dir)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if cur.ID != sessA.ID {
		t.Errorf("Current = %q, want %q", cur.ID, sessA.ID)
	}

	gotB, err := CurrentFor(dir, "agent-B")
	if err != nil {
		t.Fatalf("CurrentFor agent-B: %v", err)
	}
	if gotB.ID != sessB.ID {
		t.Errorf("CurrentFor(agent-B) = %q, want %q", gotB.ID, sessB.ID)
	}

	gotA, err := CurrentFor(dir, "agent-A")
	if err != nil {
		t.Fatalf("CurrentFor agent-A: %v", err)
	}
	if gotA.ID != sessA.ID {
		t.Errorf("CurrentFor(agent-A) = %q, want %q", gotA.ID, sessA.ID)
	}

	// Fallback when harnessSession is empty or unknown.
	gotFallback, err := CurrentFor(dir, "unknown-agent")
	if err != nil {
		t.Fatalf("CurrentFor unknown: %v", err)
	}
	if gotFallback.ID != sessA.ID {
		t.Errorf("CurrentFor(unknown) = %q, want fallback %q", gotFallback.ID, sessA.ID)
	}

	// Diff against A's baseline has no files; diff against B's baseline has src/b_only.ts.
	diffA, err := diff.Load(dir, gotA.Base)
	if err != nil {
		t.Fatalf("diff.Load A: %v", err)
	}
	if len(diffA.Files) != 0 {
		t.Errorf("A diff files = %+v, want empty", diffA.Files)
	}

	diffB, err := diff.Load(dir, gotB.Base)
	if err != nil {
		t.Fatalf("diff.Load B: %v", err)
	}
	if len(diffB.Files) != 1 || diffB.Files[0].Path != "src/b_only.ts" {
		t.Errorf("B diff files = %+v, want [src/b_only.ts]", diffB.Files)
	}
}
