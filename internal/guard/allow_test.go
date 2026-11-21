package guard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/allow"
	"github.com/Ramanan-Bala/donegate/internal/policy"
	"github.com/Ramanan-Bala/donegate/internal/session"
)

func TestDecideWithAllowancesAndDonegateAllowBashGuard(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	stateDir := filepath.Join(tmp, "state")
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "allowed.test.ts"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "denied.test.ts"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := Context{
		RepoRoot: repo,
		Policy:   policy.Default(),
		Protected: map[string]string{
			"src/allowed.test.ts":                    "h1",
			"src/denied.test.ts":                     "h2",
			"src/__snapshots__/allowed.test.ts.snap": "h3",
		},
		Allowed: []allow.Allowance{
			{Rule: "T001", Path: "src/allowed.test.ts", By: "human", At: "2026-11-21T00:00:00Z"},
		},
		StateDir: stateDir,
		Home:     home,
	}

	t.Run("allows allowed test edit", func(t *testing.T) {
		dec := Decide(ToolCall{
			Tool:  "Edit",
			Paths: []string{filepath.Join(repo, "src", "allowed.test.ts")},
			Cwd:   repo,
		}, ctx)
		if !dec.Allow {
			t.Fatalf("expected Allow=true, got %+v", dec)
		}
	})

	t.Run("allows bash sed on allowed test", func(t *testing.T) {
		dec := Decide(ToolCall{
			Tool:    "Bash",
			Command: "sed -i '' 's/a/b/' src/allowed.test.ts",
			Cwd:     repo,
		}, ctx)
		if !dec.Allow {
			t.Fatalf("expected Allow=true, got %+v", dec)
		}
	})

	t.Run("still denies non-allowed test", func(t *testing.T) {
		dec := Decide(ToolCall{
			Tool:  "Edit",
			Paths: []string{filepath.Join(repo, "src", "denied.test.ts")},
			Cwd:   repo,
		}, ctx)
		if dec.Allow || dec.RuleID != "T001" {
			t.Fatalf("expected deny T001, got %+v", dec)
		}
	})

	t.Run("still denies snapshot when only T001 on test is allowed", func(t *testing.T) {
		dec := Decide(ToolCall{
			Tool:  "Edit",
			Paths: []string{filepath.Join(repo, "src", "__snapshots__", "allowed.test.ts.snap")},
			Cwd:   repo,
		}, ctx)
		if dec.Allow || dec.RuleID != "T004" {
			t.Fatalf("expected deny T004, got %+v", dec)
		}
	})

	wantReason := "donegate: only a human can approve exceptions (donegate allow); ask the user to run it in their terminal."
	bashDenyCmds := []string{
		"donegate allow T001 x",
		"donegate allow T001 src/allowed.test.ts",
		"cd x && /usr/local/bin/donegate allow all y",
		"echo hi; ./donegate allow --list",
		"false || env FOO=1 donegate allow T001 src/denied.test.ts",
		"yes | /opt/homebrew/bin/donegate allow T001 x",
	}
	for _, cmd := range bashDenyCmds {
		t.Run("bash denies "+cmd, func(t *testing.T) {
			dec := Decide(ToolCall{
				Tool:    "Bash",
				Command: cmd,
				Cwd:     repo,
			}, ctx)
			if dec.Allow || dec.RuleID != "H001" || dec.Reason != wantReason {
				t.Fatalf("got %+v, want deny H001 with reason %q", dec, wantReason)
			}
		})
	}

	t.Run("bash allows harmless echo mentioning donegate allow", func(t *testing.T) {
		dec := Decide(ToolCall{
			Tool:    "Bash",
			Command: `echo "ask user to run donegate allow"`,
			Cwd:     repo,
		}, ctx)
		if !dec.Allow {
			t.Fatalf("expected Allow=true, got %+v", dec)
		}
	})
}

func TestLoadContextForSessionAllowances(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	t.Setenv("DONEGATE_HOME", filepath.Join(tmp, "state"))

	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.name", "Tester")
	runGit(t, repo, "config", "user.email", "tester@example.com")
	if err := os.WriteFile(filepath.Join(repo, "src", "a.test.ts"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "init")

	sessA, err := session.StartWith(repo, session.StartOptions{Harness: "claude-code", HarnessSession: "sess-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.StartWith(repo, session.StartOptions{Harness: "claude-code", HarnessSession: "sess-b"}); err != nil {
		t.Fatal(err)
	}

	if err := allow.Add(sessA, "T001", []string{"src/a.test.ts"}, "human"); err != nil {
		t.Fatal(err)
	}

	ctxA, err := LoadContextFor(repo, "sess-a")
	if err != nil {
		t.Fatalf("LoadContextFor A: %v", err)
	}
	if dec := Decide(ToolCall{Tool: "Edit", Paths: []string{"src/a.test.ts"}, Cwd: repo}, ctxA); !dec.Allow {
		t.Errorf("expected session A to allow src/a.test.ts, got %+v", dec)
	}

	ctxB, err := LoadContextFor(repo, "sess-b")
	if err != nil {
		t.Fatalf("LoadContextFor B: %v", err)
	}
	if dec := Decide(ToolCall{Tool: "Edit", Paths: []string{"src/a.test.ts"}, Cwd: repo}, ctxB); dec.Allow || dec.RuleID != "T001" {
		t.Errorf("expected session B to deny src/a.test.ts with T001, got %+v", dec)
	}
}
