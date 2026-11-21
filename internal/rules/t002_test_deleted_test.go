package rules

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/diff"
	"github.com/Ramanan-Bala/donegate/internal/policy"
)

func TestT002Registration(t *testing.T) {
	r, ok := Lookup("T002")
	if !ok {
		t.Fatal("T002 not registered")
	}
	if r.Level() != 1 {
		t.Errorf("Level() = %d, want 1", r.Level())
	}
	if r.DefaultSeverity() != Block {
		t.Errorf("DefaultSeverity() = %q, want %q", r.DefaultSeverity(), Block)
	}
}

func TestT002DirectDiff(t *testing.T) {
	r, ok := Lookup("T002")
	if !ok {
		t.Fatal("T002 not registered")
	}

	tests := []struct {
		name      string
		files     []diff.File
		wantPaths []string
	}{
		{
			name: "deleted test file triggers T002",
			files: []diff.File{
				{Path: "src/price.test.ts", Status: diff.Deleted},
			},
			wantPaths: []string{"src/price.test.ts"},
		},
		{
			name: "deleted __tests__ file triggers T002",
			files: []diff.File{
				{Path: "src/__tests__/cart.ts", Status: diff.Deleted},
			},
			wantPaths: []string{"src/__tests__/cart.ts"},
		},
		{
			name: "deleted snapshot does not trigger T002",
			files: []diff.File{
				{Path: "src/__snapshots__/cart.test.ts.snap", Status: diff.Deleted},
			},
		},
		{
			name: "deleted source file does not trigger T002",
			files: []diff.File{
				{Path: "src/price.ts", Status: diff.Deleted},
			},
		},
		{
			name: "modified test without RepoDir swallows read error",
			files: []diff.File{
				{Path: "src/cart.test.ts", Status: diff.Modified},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			findings := r.Check(&Context{
				Diff:   &diff.Diff{Files: tc.files},
				Policy: policy.Default(),
			})
			if len(findings) != len(tc.wantPaths) {
				t.Fatalf("got %d findings (%+v), want %d", len(findings), findings, len(tc.wantPaths))
			}
			for i, wantPath := range tc.wantPaths {
				f := findings[i]
				if f.RuleID != "T002" || f.Path != wantPath || f.Message == "" || f.Hint == "" {
					t.Errorf("unexpected finding[%d]: %+v", i, f)
				}
			}
		})
	}

	if got := r.Check(nil); len(got) != 0 {
		t.Errorf("Check(nil) = %+v, want empty", got)
	}
}

func TestT002TestCaseCountDrop(t *testing.T) {
	r, ok := Lookup("T002")
	if !ok {
		t.Fatal("T002 not registered")
	}

	tests := []struct {
		name        string
		baseFiles   map[string]string
		headFiles   map[string]string
		renameFrom  string
		renameTo    string
		wantMessage string
	}{
		{
			name: "removing one it() triggers T002",
			baseFiles: map[string]string{
				"src/a.test.ts": "it('one', () => {});\nit('two', () => {});\n",
			},
			headFiles: map[string]string{
				"src/a.test.ts": "it('one', () => {});\n",
			},
			wantMessage: "1 test case(s) removed from src/a.test.ts",
		},
		{
			name: "removing test.concurrent and tagged template triggers T002",
			baseFiles: map[string]string{
				"src/a.test.ts": "test.concurrent('one', () => {});\nit`two`(() => {});\ntest.each([1])('three', () => {});\n",
			},
			headFiles: map[string]string{
				"src/a.test.ts": "test.concurrent('one', () => {});\n",
			},
			wantMessage: "2 test case(s) removed from src/a.test.ts",
		},
		{
			name: "renamed test with fewer cases triggers T002",
			baseFiles: map[string]string{
				"src/old.test.ts": "it('one', () => {});\nit('two', () => {});\n",
			},
			renameFrom: "src/old.test.ts",
			renameTo:   "src/new.test.ts",
			headFiles: map[string]string{
				"src/new.test.ts": "it('one', () => {});\n",
			},
			wantMessage: "1 test case(s) removed from src/new.test.ts",
		},
		{
			name: "equal test count does not trigger T002",
			baseFiles: map[string]string{
				"src/a.test.ts": "it('one', () => {});\nit('two', () => {});\n",
			},
			headFiles: map[string]string{
				"src/a.test.ts": "it('one', () => {});\nit('two renamed', () => {});\n",
			},
		},
		{
			name: "adding a test case does not trigger T002",
			baseFiles: map[string]string{
				"src/a.test.ts": "it('one', () => {});\n",
			},
			headFiles: map[string]string{
				"src/a.test.ts": "it('one', () => {});\ntest('two', () => {});\n",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t002InitRepo(t, tc.baseFiles)
			if tc.renameFrom != "" {
				t002RunGit(t, dir, "mv", tc.renameFrom, tc.renameTo)
			}
			for rel, content := range tc.headFiles {
				t002WriteFile(t, dir, rel, content)
			}
			d, err := diff.Load(dir, "HEAD")
			if err != nil {
				t.Fatalf("diff.Load: %v", err)
			}
			findings := r.Check(&Context{Diff: d, Policy: policy.Default()})
			if tc.wantMessage == "" {
				if len(findings) != 0 {
					t.Fatalf("got %+v, want 0 findings", findings)
				}
				return
			}
			if len(findings) != 1 {
				t.Fatalf("got %d findings (%+v), want 1", len(findings), findings)
			}
			if findings[0].Message != tc.wantMessage {
				t.Errorf("Message = %q, want %q", findings[0].Message, tc.wantMessage)
			}
			if findings[0].Hint == "" {
				t.Errorf("missing Hint: %+v", findings[0])
			}
		})
	}
}

func t002RunGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(bytes.TrimSpace(out))
}

func t002WriteFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", full, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

func t002InitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	t002RunGit(t, dir, "-c", "init.defaultBranch=main", "init")
	for rel, content := range files {
		t002WriteFile(t, dir, rel, content)
	}
	t002RunGit(t, dir, "add", "-A")
	t002RunGit(t, dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "base")
	if strings.TrimSpace(t002RunGit(t, dir, "rev-parse", "HEAD")) == "" {
		t.Fatal("empty base SHA")
	}
	return dir
}
