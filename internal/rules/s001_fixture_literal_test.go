package rules

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/diff"
	"github.com/Ramanan-Bala/donegate/internal/policy"
)

func TestS001Registration(t *testing.T) {
	r, ok := Lookup("S001")
	if !ok {
		t.Fatal("S001 not registered")
	}
	if r.Level() != 1 {
		t.Errorf("Level() = %d, want 1", r.Level())
	}
	if r.DefaultSeverity() != Warn {
		t.Errorf("DefaultSeverity() = %q, want %q", r.DefaultSeverity(), Warn)
	}
}

func TestS001(t *testing.T) {
	r, ok := Lookup("S001")
	if !ok {
		t.Fatal("S001 not registered")
	}

	baseFiles := map[string]string{
		"src/__tests__/fixtures/orders.json": `[
  { "id": "ORD-7781", "subtotal": 1234.56, "magic": 42000, "neg": -15000 }
]
`,
		"src/cart.test.ts": `import { expect, it } from 'vitest';
it('checks values', () => {
  expect(1111.1).toBe(1111.1);
  expect(0.9).toBe(0.9);
  expect(0.95).toBe(0.95);
  expect(1000).toBe(1000);
  expect("abcd").toBe("abcd");
  expect("short1").not.toBe("x");
});
`,
		"src/cart.ts": `export const EXISTING_ID = "ORD-7781";
export function calc(n: number): number {
  if (n === 42000) return 1;
  return n;
}
`,
	}

	tests := []struct {
		name        string
		headFiles   map[string]string
		wantCount   int
		wantLine    int
		wantSubstrs []string
	}{
		{
			name: "fractional literal 1111.10 matches 1111.1 from test",
			headFiles: map[string]string{
				"src/cart.ts": baseFiles["src/cart.ts"] + "export const f = () => 1111.10;\n",
			},
			wantCount:   1,
			wantLine:    6,
			wantSubstrs: []string{"1111.10", "src/cart.test.ts"},
		},
		{
			name: "fractional literal 1234.56 from json fixture",
			headFiles: map[string]string{
				"src/cart.ts": baseFiles["src/cart.ts"] + "export const g = (x: number) => x === 1234.56;\n",
			},
			wantCount:   1,
			wantLine:    6,
			wantSubstrs: []string{"1234.56", "src/__tests__/fixtures/orders.json"},
		},
		{
			name: "negative integer >= 10000 abs value matches",
			headFiles: map[string]string{
				"src/cart.ts": baseFiles["src/cart.ts"] + "export const neg = -15000;\n",
			},
			wantCount:   1,
			wantLine:    6,
			wantSubstrs: []string{"-15000", "orders.json"},
		},
		{
			name: "quoted string literal >= 5 chars with digit in new source file",
			headFiles: map[string]string{
				"src/newmod.ts": "export const hit = 'short1';\n",
			},
			wantCount:   1,
			wantLine:    1,
			wantSubstrs: []string{"short1", "src/cart.test.ts"},
		},
		{
			name: "non-distinctive literals 0.9, 0.95, 1000 do not trigger",
			headFiles: map[string]string{
				"src/cart.ts": baseFiles["src/cart.ts"] + "export const ok = (x: number) => (x >= 1000 ? x * 0.9 : x * 0.95);\n",
			},
			wantCount: 0,
		},
		{
			name: "literals already in base version of same file do not trigger",
			headFiles: map[string]string{
				"src/cart.ts": `export const EXISTING_ID = "ORD-7781";
export function calc(n: number): number {
  if (n === 42000) return 2;
  return n;
}
`,
			},
			wantCount: 0,
		},
		{
			name: "non-source files like README.md do not trigger",
			headFiles: map[string]string{
				"README.md": "Example order ORD-7781 total 1234.56\n",
			},
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := s001InitRepo(t, baseFiles)
			for rel, content := range tc.headFiles {
				s001WriteFile(t, dir, rel, content)
			}
			d, err := diff.Load(dir, "HEAD")
			if err != nil {
				t.Fatalf("diff.Load: %v", err)
			}
			findings := r.Check(&Context{Diff: d, Policy: policy.Default()})
			if len(findings) != tc.wantCount {
				t.Fatalf("got %d findings (%+v), want %d", len(findings), findings, tc.wantCount)
			}
			if tc.wantCount > 0 {
				f := findings[0]
				if f.RuleID != "S001" || f.Severity != Warn || f.Line != tc.wantLine || f.Hint == "" {
					t.Errorf("unexpected finding: %+v", f)
				}
				for _, sub := range tc.wantSubstrs {
					if !strings.Contains(f.Message, sub) {
						t.Errorf("Message %q missing %q", f.Message, sub)
					}
				}
			}
		})
	}

	// Missing RepoDir/Base returns nil cleanly.
	if got := r.Check(&Context{Diff: &diff.Diff{}, Policy: policy.Default()}); len(got) != 0 {
		t.Errorf("expected no findings without RepoDir/Base, got %+v", got)
	}
}

func s001RunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func s001WriteFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", full, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

func s001InitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	s001RunGit(t, dir, "-c", "init.defaultBranch=main", "init")
	for rel, content := range files {
		s001WriteFile(t, dir, rel, content)
	}
	s001RunGit(t, dir, "add", "-A")
	s001RunGit(t, dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "base")
	return dir
}
