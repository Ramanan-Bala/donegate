package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/policy"
	"github.com/Ramanan-Bala/donegate/internal/session"
)

func TestDecide(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	stateDir := filepath.Join(tmp, "state")
	repo := filepath.Join(tmp, "repo")
	outside := filepath.Join(tmp, "outside")

	for _, d := range []string{
		home,
		stateDir,
		filepath.Join(repo, "src", "__tests__"),
		filepath.Join(repo, "src", "__snapshots__"),
		outside,
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// Create an existing test file on disk for Protected == nil tests.
	existingTest := filepath.Join(repo, "src", "__tests__", "existing.test.ts")
	if err := os.WriteFile(existingTest, []byte("test('ok', () => {})\n"), 0o644); err != nil {
		t.Fatalf("write existing test: %v", err)
	}

	polAllowNew := policy.Default()
	polDisallowNew := policy.Default()
	polDisallowNew.Protect.AllowNewTests = false

	protected := map[string]string{
		"src/__tests__/existing.test.ts": "abc123",
		"vitest.config.ts":               "def456",
	}

	baseCtx := Context{
		RepoRoot:  repo,
		Policy:    polAllowNew,
		Protected: protected,
		StateDir:  stateDir,
		Home:      home,
	}

	tests := []struct {
		name       string
		call       ToolCall
		ctx        Context
		wantAllow  bool
		wantRuleID string
		wantReason string
	}{
		{
			name: "rule a: state dir write denied",
			call: ToolCall{
				Tool:  "Write",
				Paths: []string{filepath.Join(stateDir, "sessions", "123", "session.json")},
				Cwd:   repo,
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "H001",
			wantReason: "read-only",
		},
		{
			name: "rule b: home claude settings.json denied",
			call: ToolCall{
				Tool:  "Edit",
				Paths: []string{filepath.Join(home, ".claude", "settings.json")},
				Cwd:   repo,
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "H001",
		},
		{
			name: "rule b: home claude settings.local.json denied",
			call: ToolCall{
				Tool:  "Write",
				Paths: []string{filepath.Join(home, ".claude", "settings.local.json")},
				Cwd:   repo,
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "H001",
		},
		{
			name: "rule b: home claude hooks denied",
			call: ToolCall{
				Tool:  "Write",
				Paths: []string{filepath.Join(home, ".claude", "hooks", "pre.sh")},
				Cwd:   repo,
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "H001",
		},
		{
			name: "rule b: home opencode plugin denied",
			call: ToolCall{
				Tool:  "Write",
				Paths: []string{filepath.Join(home, ".config", "opencode", "plugin", "donegate.ts")},
				Cwd:   repo,
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "H001",
		},
		{
			name: "rule b: home opencode plugins denied",
			call: ToolCall{
				Tool:  "Write",
				Paths: []string{filepath.Join(home, ".config", "opencode", "plugins", "donegate.ts")},
				Cwd:   repo,
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "H001",
		},
		{
			name: "rule b: home pi extensions denied",
			call: ToolCall{
				Tool:  "Write",
				Paths: []string{filepath.Join(home, ".pi", "agent", "extensions", "donegate.ts")},
				Cwd:   repo,
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "H001",
		},
		{
			name: "rule b: repo claude settings.json denied",
			call: ToolCall{
				Tool:  "Edit",
				Paths: []string{filepath.Join(repo, ".claude", "settings.json")},
				Cwd:   repo,
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "H001",
		},
		{
			name: "rule b: repo claude settings.local.json denied",
			call: ToolCall{
				Tool:  "Edit",
				Paths: []string{filepath.Join(repo, ".claude", "settings.local.json")},
				Cwd:   repo,
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "H001",
		},
		{
			name: "rule b: repo claude hooks denied",
			call: ToolCall{
				Tool:  "Write",
				Paths: []string{filepath.Join(repo, ".claude", "hooks", "guard.sh")},
				Cwd:   repo,
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "H001",
		},
		{
			name: "rule c: repo .donegate.yml denied",
			call: ToolCall{
				Tool:  "Edit",
				Paths: []string{filepath.Join(repo, ".donegate.yml")},
				Cwd:   repo,
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "H001",
		},
		{
			name: "rule d: snapshot denied with T004",
			call: ToolCall{
				Tool:  "Write",
				Paths: []string{filepath.Join(repo, "src", "__snapshots__", "App.test.tsx.snap")},
				Cwd:   repo,
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "T004",
			wantReason: "snapshots are read-only (T004)",
		},
		{
			name: "rule e: existing test in baseline denied",
			call: ToolCall{
				Tool:  "Edit",
				Paths: []string{existingTest},
				Cwd:   repo,
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "T001",
			wantReason: "Tests are read-only",
		},
		{
			name: "rule e: new test allowed when AllowNewTests is true",
			call: ToolCall{
				Tool:  "Write",
				Paths: []string{filepath.Join(repo, "src", "__tests__", "brand_new.test.ts")},
				Cwd:   repo,
			},
			ctx:       baseCtx,
			wantAllow: true,
		},
		{
			name: "rule e: new test denied when AllowNewTests is false",
			call: ToolCall{
				Tool:  "Write",
				Paths: []string{filepath.Join(repo, "src", "__tests__", "brand_new.test.ts")},
				Cwd:   repo,
			},
			ctx: Context{
				RepoRoot:  repo,
				Policy:    polDisallowNew,
				Protected: protected,
				StateDir:  stateDir,
				Home:      home,
			},
			wantAllow:  false,
			wantRuleID: "T001",
		},
		{
			name: "rule e: Protected nil fallback denies existing test on disk",
			call: ToolCall{
				Tool:  "Edit",
				Paths: []string{existingTest},
				Cwd:   repo,
			},
			ctx: Context{
				RepoRoot:  repo,
				Policy:    polAllowNew,
				Protected: nil,
				StateDir:  stateDir,
				Home:      home,
			},
			wantAllow:  false,
			wantRuleID: "T001",
		},
		{
			name: "rule e: Protected nil fallback allows new test not on disk",
			call: ToolCall{
				Tool:  "Write",
				Paths: []string{filepath.Join(repo, "src", "__tests__", "missing_new.test.ts")},
				Cwd:   repo,
			},
			ctx: Context{
				RepoRoot:  repo,
				Policy:    polAllowNew,
				Protected: nil,
				StateDir:  stateDir,
				Home:      home,
			},
			wantAllow: true,
		},
		{
			name: "rule f: protected config denied",
			call: ToolCall{
				Tool:  "Edit",
				Paths: []string{filepath.Join(repo, "vitest.config.ts")},
				Cwd:   repo,
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "C001",
			wantReason: "protected test/lint config (C001)",
		},
		{
			name: "relative path resolved against Cwd",
			call: ToolCall{
				Tool:  "Edit",
				Paths: []string{"existing.test.ts"},
				Cwd:   filepath.Join(repo, "src", "__tests__"),
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "T001",
		},
		{
			name: "dot-dot traversal into protected path denied",
			call: ToolCall{
				Tool:  "Edit",
				Paths: []string{"../../vitest.config.ts"},
				Cwd:   filepath.Join(repo, "src", "__tests__"),
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "C001",
		},
		{
			name: "normal source file inside repo allowed",
			call: ToolCall{
				Tool:  "Edit",
				Paths: []string{filepath.Join(repo, "src", "cart.ts")},
				Cwd:   repo,
			},
			ctx:       baseCtx,
			wantAllow: true,
		},
		{
			name: "path outside repo allowed when not state or harness",
			call: ToolCall{
				Tool:  "Write",
				Paths: []string{filepath.Join(outside, "notes.test.ts")},
				Cwd:   repo,
			},
			ctx:       baseCtx,
			wantAllow: true,
		},
		{
			name: "bash sed -i on existing test denied",
			call: ToolCall{
				Tool:    "Bash",
				Command: "sed -i '' 's/900/1000/' src/__tests__/existing.test.ts",
				Cwd:     repo,
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "T001",
		},
		{
			name: "bash redirect to config denied",
			call: ToolCall{
				Tool:    "Bash",
				Command: "echo '{}' > tsconfig.json",
				Cwd:     repo,
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "C001",
		},
		{
			name: "bash vitest -u denied",
			call: ToolCall{
				Tool:    "Bash",
				Command: "npx vitest run -u",
				Cwd:     repo,
			},
			ctx:        baseCtx,
			wantAllow:  false,
			wantRuleID: "T004",
			wantReason: "donegate: snapshot updates are blocked (T004). Fix the code so existing snapshots match.",
		},
		{
			name: "bash harmless go test allowed",
			call: ToolCall{
				Tool:    "Bash",
				Command: "go test ./...",
				Cwd:     repo,
			},
			ctx:       baseCtx,
			wantAllow: true,
		},
		{
			name: "bash harmless npm test allowed",
			call: ToolCall{
				Tool:    "Bash",
				Command: "npm test",
				Cwd:     repo,
			},
			ctx:       baseCtx,
			wantAllow: true,
		},
		{
			name: "bash redirect to /dev/null allowed",
			call: ToolCall{
				Tool:    "Bash",
				Command: "cat src/__tests__/existing.test.ts > /dev/null",
				Cwd:     repo,
			},
			ctx:       baseCtx,
			wantAllow: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(tc.call, tc.ctx)
			if got.Allow != tc.wantAllow {
				t.Fatalf("Allow = %v, want %v (decision=%+v)", got.Allow, tc.wantAllow, got)
			}
			if got.RuleID != tc.wantRuleID {
				t.Errorf("RuleID = %q, want %q", got.RuleID, tc.wantRuleID)
			}
			if tc.wantAllow {
				if got.Path != "" || got.Reason != "" {
					t.Errorf("expected empty Path/Reason on allow, got Path=%q Reason=%q", got.Path, got.Reason)
				}
			} else {
				if got.Reason == "" {
					t.Errorf("expected non-empty Reason on deny")
				}
				if tc.wantReason != "" && !strings.Contains(got.Reason, tc.wantReason) {
					t.Errorf("Reason = %q, want substring %q", got.Reason, tc.wantReason)
				}
			}
		})
	}
}

func TestDecideSymlinkEscape(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	outside := filepath.Join(tmp, "outside")
	if err := os.MkdirAll(filepath.Join(repo, "__tests__"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(repo, "__tests__", "a.test.ts")
	if err := os.WriteFile(target, []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(outside, "link")
	if err := os.Symlink(filepath.Join(repo, "__tests__"), linkDir); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	dec := Decide(ToolCall{
		Tool:  "Edit",
		Paths: []string{filepath.Join(linkDir, "a.test.ts")},
		Cwd:   outside,
	}, Context{
		RepoRoot:  repo,
		Policy:    policy.Default(),
		Protected: map[string]string{"__tests__/a.test.ts": "hash"},
	})
	if dec.Allow || dec.RuleID != "T001" {
		t.Fatalf("expected symlink traversal into test to deny with T001, got %+v", dec)
	}
}

func TestLoadContext(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	stateDir := filepath.Join(tmp, "donegate-state")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("DONEGATE_HOME", stateDir)

	// 1. Outside a git repo: RepoRoot == "" and rules a/b still work.
	nonRepo := filepath.Join(tmp, "plain")
	if err := os.MkdirAll(nonRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, err := LoadContext(nonRepo)
	if err != nil {
		t.Fatalf("LoadContext non-repo: %v", err)
	}
	if ctx.RepoRoot != "" || ctx.Protected != nil || ctx.Policy == nil {
		t.Fatalf("unexpected non-repo context: %+v", ctx)
	}
	if dec := Decide(ToolCall{
		Tool:  "Edit",
		Paths: []string{filepath.Join(home, ".claude", "settings.json")},
		Cwd:   nonRepo,
	}, ctx); dec.Allow || dec.RuleID != "H001" {
		t.Fatalf("expected H001 outside git repo, got %+v", dec)
	}

	// 2. Git repo without session: reads working-tree .donegate.yml, Protected == nil.
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "custom"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.name", "Tester")
	runGit(t, repo, "config", "user.email", "tester@example.com")

	customPolicy := "version: 1\nprotect:\n  tests:\n    - \"custom/**\"\n  allow_new_tests: true\n"
	if err := os.WriteFile(filepath.Join(repo, ".donegate.yml"), []byte(customPolicy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "custom", "check.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "init")

	ctxNoSess, err := LoadContext(repo)
	if err != nil {
		t.Fatalf("LoadContext no session: %v", err)
	}
	if ctxNoSess.Protected != nil {
		t.Fatalf("expected Protected == nil without session, got %v", ctxNoSess.Protected)
	}
	if !ctxNoSess.Policy.IsTest("custom/check.txt") {
		t.Fatalf("expected working-tree .donegate.yml to be loaded")
	}

	// 3. Active session: uses snapshotted policy even if working-tree .donegate.yml changes.
	sess, err := session.Start(repo, "claude")
	if err != nil {
		t.Fatalf("session.Start: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".donegate.yml"), []byte("version: 1\nprotect:\n  tests: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctxSess, err := LoadContext(repo)
	if err != nil {
		t.Fatalf("LoadContext with session: %v", err)
	}
	if ctxSess.RepoRoot != sess.Repo {
		t.Errorf("RepoRoot = %q, want %q", ctxSess.RepoRoot, sess.Repo)
	}
	if ctxSess.Protected == nil {
		t.Fatalf("expected non-nil Protected with active session")
	}
	if !ctxSess.Policy.IsTest("custom/check.txt") {
		t.Fatalf("expected snapshotted session policy to ignore working-tree edit")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func BenchmarkDecide(b *testing.B) {
	tmp := b.TempDir()
	repo := filepath.Join(tmp, "repo")
	home := filepath.Join(tmp, "home")
	stateDir := filepath.Join(tmp, "state")
	ctx := Context{
		RepoRoot: repo,
		Policy:   policy.Default(),
		Protected: map[string]string{
			"src/__tests__/cart.test.ts": "deadbeef",
		},
		StateDir: stateDir,
		Home:     home,
	}
	call := ToolCall{
		Tool:  "Edit",
		Paths: []string{filepath.Join(repo, "src", "cart.ts")},
		Cwd:   repo,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Decide(call, ctx)
	}
}
