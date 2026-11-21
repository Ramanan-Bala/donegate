package rules

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/diff"
	"github.com/Ramanan-Bala/donegate/internal/policy"
)

func TestS003Registration(t *testing.T) {
	r, ok := Lookup("S003")
	if !ok {
		t.Fatal("S003 not registered")
	}
	if r.Level() != 1 {
		t.Errorf("Level() = %d, want 1", r.Level())
	}
	if r.DefaultSeverity() != Block {
		t.Errorf("DefaultSeverity() = %q, want %q", r.DefaultSeverity(), Block)
	}
}

func TestS003DirectDiff(t *testing.T) {
	r, ok := Lookup("S003")
	if !ok {
		t.Fatal("S003 not registered")
	}

	tests := []struct {
		name      string
		files     []diff.File
		wantCount int
		wantLine  int
	}{
		{
			name: "process.exit added in source file",
			files: []diff.File{
				{
					Path:   "src/cart.ts",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 7, Text: "  process.exit(0);"}},
				},
			},
			wantCount: 1,
			wantLine:  7,
		},
		{
			name: "static setup paths modified or deleted",
			files: []diff.File{
				{Path: "src/setupTests.ts", Status: diff.Modified},
				{Path: "jest.setup.js", Status: diff.Modified},
				{Path: "vitest.setup.ts", Status: diff.Deleted},
				{Path: "globalSetup.mjs", Status: diff.Modified},
				{Path: "test/setup.ts", Status: diff.Modified},
				{Path: "packages/web/__tests__/setup.cjs", Status: diff.Modified},
			},
			wantCount: 6,
		},
		{
			name: "renamed setup file triggers S003",
			files: []diff.File{
				{Path: "test/renamed.ts", OldPath: "test/setup.ts", Status: diff.Renamed},
			},
			wantCount: 1,
		},
		{
			name: "non-setup source file and added setup file do not trigger",
			files: []diff.File{
				{Path: "src/setup.ts", Status: diff.Modified},
				{Path: "src/cart.ts", Status: diff.Modified, Added: []diff.Line{{No: 2, Text: "export const ok = 1;"}}},
			},
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			findings := r.Check(&Context{
				Diff:   &diff.Diff{Files: tc.files},
				Policy: policy.Default(),
			})
			if len(findings) != tc.wantCount {
				t.Fatalf("got %d findings (%+v), want %d", len(findings), findings, tc.wantCount)
			}
			if tc.wantCount > 0 {
				f := findings[0]
				if f.RuleID != "S003" || f.Message == "" || f.Hint == "" {
					t.Errorf("unexpected finding[0]: %+v", f)
				}
				if tc.wantLine > 0 && f.Line != tc.wantLine {
					t.Errorf("finding[0].Line = %d, want %d", f.Line, tc.wantLine)
				}
			}
		})
	}
}

func TestS003ConfigReferencedSetupFiles(t *testing.T) {
	r, ok := Lookup("S003")
	if !ok {
		t.Fatal("S003 not registered")
	}

	baseFiles := map[string]string{
		"packages/web/vitest.config.ts": `import { defineConfig } from 'vitest/config';
export default defineConfig({
  test: {
    setupFiles: ['./custom-setup.ts'],
    globalSetup: './global-hook.ts',
  },
});
`,
		"jest.config.js": `module.exports = {
  setupFilesAfterEnv: ['<rootDir>/env-after.js'],
};
`,
		"packages/web/custom-setup.ts": "export const x = 1;\n",
		"packages/web/global-hook.ts":  "export default () => {};\n",
		"env-after.js":                 "global.foo = 1;\n",
		"packages/web/normal.ts":       "export const y = 2;\n",
	}

	dir := s003InitRepo(t, baseFiles)
	s003WriteFile(t, dir, "packages/web/custom-setup.ts", "export const x = 2;\n")
	s003WriteFile(t, dir, "env-after.js", "global.foo = 2;\n")
	s003WriteFile(t, dir, "packages/web/normal.ts", "export const y = 3;\n")

	d, err := diff.Load(dir, "HEAD")
	if err != nil {
		t.Fatalf("diff.Load: %v", err)
	}
	findings := r.Check(&Context{Diff: d, Policy: policy.Default()})
	if len(findings) != 2 {
		t.Fatalf("got %d findings (%+v), want 2", len(findings), findings)
	}
	if findings[0].Path != "env-after.js" || findings[1].Path != "packages/web/custom-setup.ts" {
		t.Errorf("unexpected finding paths: %+v", findings)
	}
}

func s003RunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func s003WriteFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", full, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

func s003InitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	s003RunGit(t, dir, "-c", "init.defaultBranch=main", "init")
	for rel, content := range files {
		s003WriteFile(t, dir, rel, content)
	}
	s003RunGit(t, dir, "add", "-A")
	s003RunGit(t, dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "base")
	return dir
}
