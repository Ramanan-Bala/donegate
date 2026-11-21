package rules

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/diff"
	"github.com/Ramanan-Bala/donegate/internal/policy"
)

func TestC001Registration(t *testing.T) {
	r, ok := Lookup("C001")
	if !ok {
		t.Fatal("C001 not registered")
	}
	if r.Level() != 1 {
		t.Errorf("Level() = %d, want 1", r.Level())
	}
	if r.DefaultSeverity() != Block {
		t.Errorf("DefaultSeverity() = %q, want %q", r.DefaultSeverity(), Block)
	}
}

func TestC001DirectDiff(t *testing.T) {
	r, ok := Lookup("C001")
	if !ok {
		t.Fatal("C001 not registered")
	}

	tests := []struct {
		name      string
		files     []diff.File
		wantCount int
		wantLine  int
	}{
		{
			name: "deleted config file triggers C001",
			files: []diff.File{
				{Path: "vitest.config.ts", Status: diff.Deleted},
			},
			wantCount: 1,
		},
		{
			name: "passWithNoTests true triggers C001",
			files: []diff.File{
				{
					Path:   "jest.config.js",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 4, Text: "  passWithNoTests: true,"}},
				},
			},
			wantCount: 1,
			wantLine:  4,
		},
		{
			name: "tsconfig strict false and noImplicitAny false trigger C001",
			files: []diff.File{
				{
					Path:   "tsconfig.json",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 5, Text: `    "strict": false,`},
						{No: 6, Text: `    "noImplicitAny": false,`},
					},
				},
			},
			wantCount: 2,
			wantLine:  5,
		},
		{
			name: "filter keys trigger C001",
			files: []diff.File{
				{
					Path:   "jest.config.ts",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 2, Text: "  testMatch: ['**/foo.test.ts'],"},
						{No: 3, Text: "  testPathIgnorePatterns: ['/node_modules/', '/src/'],"},
						{No: 4, Text: "  coveragePathIgnorePatterns: ['/src/'],"},
					},
				},
				{
					Path:   "eslint.config.js",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 7, Text: "  ignores: ['src/**'],"}},
				},
				{
					Path:   ".eslintrc.json",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 3, Text: `  "ignorePatterns": ["dist", "src"]`}},
				},
			},
			wantCount: 5,
			wantLine:  2,
		},
		{
			name: "eslint rule set to off triggers C001",
			files: []diff.File{
				{
					Path:   ".eslintrc.json",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 8, Text: `    "@typescript-eslint/no-explicit-any": "off",`},
						{No: 9, Text: `    "no-console": 0`},
						{No: 10, Text: `    "semi": "error"`},
					},
				},
				{
					Path:   "eslint.config.mjs",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 12, Text: `    'no-unused-vars': ['off'],`}},
				},
			},
			wantCount: 3,
			wantLine:  8,
		},
		{
			name: "workflow continue-on-error and || true trigger C001",
			files: []diff.File{
				{
					Path:   ".github/workflows/ci.yml",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 14, Text: "        continue-on-error: true"},
						{No: 18, Text: "        run: npm test || true"},
					},
				},
			},
			wantCount: 2,
			wantLine:  14,
		},
		{
			name: "coverage threshold lowered triggers C001",
			files: []diff.File{
				{
					Path:    "vitest.config.ts",
					Status:  diff.Modified,
					Removed: []diff.Line{{No: 10, Text: "      lines: 80,"}},
					Added:   []diff.Line{{No: 10, Text: "      lines: 50,"}},
				},
			},
			wantCount: 1,
			wantLine:  10,
		},
		{
			name: "coverage threshold raised does not trigger C001",
			files: []diff.File{
				{
					Path:    "vitest.config.ts",
					Status:  diff.Modified,
					Removed: []diff.Line{{No: 10, Text: "      lines: 80,"}},
					Added:   []diff.Line{{No: 10, Text: "      lines: 90,"}},
				},
			},
			wantCount: 0,
		},
		{
			name: "modified eslintignore with new entry triggers C001",
			files: []diff.File{
				{
					Path:   "web/.eslintignore",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 1, Text: ""},
						{No: 2, Text: "# comment only"},
						{No: 3, Text: "src/legacy.ts"},
					},
				},
			},
			wantCount: 1,
			wantLine:  3,
		},
		{
			name: "honest source and config comment edits do not trigger C001",
			files: []diff.File{
				{
					Path:   "src/cart.ts",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 2, Text: "const strict = false;"}},
				},
				{
					Path:   "vitest.config.ts",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 2, Text: "// include: ['all']"}},
				},
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
				if findings[0].RuleID != "C001" || findings[0].Message == "" || findings[0].Hint == "" {
					t.Errorf("unexpected finding[0]: %+v", findings[0])
				}
				if tc.wantLine > 0 && findings[0].Line != tc.wantLine {
					t.Errorf("finding[0].Line = %d, want %d", findings[0].Line, tc.wantLine)
				}
			}
		})
	}
}

func TestC001PackageJSON(t *testing.T) {
	r, ok := Lookup("C001")
	if !ok {
		t.Fatal("C001 not registered")
	}

	tests := []struct {
		name      string
		relPath   string
		baseJSON  string
		headJSON  string
		wantCount int
	}{
		{
			name:      "|| true added to test script",
			relPath:   "package.json",
			baseJSON:  `{"scripts":{"test":"vitest run","build":"vite build"}}`,
			headJSON:  `{"scripts":{"test":"vitest run || true","build":"vite build"}}`,
			wantCount: 1,
		},
		{
			name:      "|| exit 0 added to lint:ci in nested package.json",
			relPath:   "packages/web/package.json",
			baseJSON:  `{"scripts":{"lint:ci":"eslint ."}}`,
			headJSON:  `{"scripts":{"lint:ci":"eslint . || exit 0"}}`,
			wantCount: 1,
		},
		{
			name:      "; exit 0 and --passWithNoTests added",
			relPath:   "package.json",
			baseJSON:  `{"scripts":{"ci":"npm test","test:unit":"jest"}}`,
			headJSON:  `{"scripts":{"ci":"npm test; exit 0","test:unit":"jest --passWithNoTests"}}`,
			wantCount: 2,
		},
		{
			name:      "typecheck script removed",
			relPath:   "package.json",
			baseJSON:  `{"scripts":{"test":"vitest","typecheck":"tsc --noEmit"}}`,
			headJSON:  `{"scripts":{"test":"vitest"}}`,
			wantCount: 1,
		},
		{
			name:      "unwatched script or pre-existing || true does not trigger",
			relPath:   "package.json",
			baseJSON:  `{"scripts":{"test":"vitest || true","dev":"vite"}}`,
			headJSON:  `{"scripts":{"test":"vitest run || true","dev":"vite || true"}}`,
			wantCount: 0,
		},
		{
			name:      "unparseable JSON returns no findings without crashing",
			relPath:   "package.json",
			baseJSON:  `{"scripts":{"test":"vitest"}}`,
			headJSON:  `{not valid json`,
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := c001InitRepo(t, map[string]string{tc.relPath: tc.baseJSON})
			c001WriteFile(t, dir, tc.relPath, tc.headJSON)
			d, err := diff.Load(dir, "HEAD")
			if err != nil {
				t.Fatalf("diff.Load: %v", err)
			}
			findings := r.Check(&Context{Diff: d, Policy: policy.Default()})
			if len(findings) != tc.wantCount {
				t.Fatalf("got %d findings (%+v), want %d", len(findings), findings, tc.wantCount)
			}
		})
	}
}

func c001RunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func c001WriteFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", full, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

func c001InitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	c001RunGit(t, dir, "-c", "init.defaultBranch=main", "init")
	for rel, content := range files {
		c001WriteFile(t, dir, rel, content)
	}
	c001RunGit(t, dir, "add", "-A")
	c001RunGit(t, dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "base")
	return dir
}
