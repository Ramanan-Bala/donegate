package rules

import (
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/diff"
	"github.com/Ramanan-Bala/donegate/internal/policy"
)

func TestC002Registration(t *testing.T) {
	r, ok := Lookup("C002")
	if !ok {
		t.Fatal("C002 not registered")
	}
	if r.Level() != 1 {
		t.Errorf("Level() = %d, want 1", r.Level())
	}
	if r.DefaultSeverity() != Warn {
		t.Errorf("DefaultSeverity() = %q, want %q", r.DefaultSeverity(), Warn)
	}
}

func TestC002(t *testing.T) {
	r, ok := Lookup("C002")
	if !ok {
		t.Fatal("C002 not registered")
	}

	tests := []struct {
		name      string
		files     []diff.File
		wantLines []int
	}{
		{
			name: "all suppression forms in source and test files",
			files: []diff.File{
				{
					Path:   "src/cart.ts",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 1, Text: "// @ts-ignore"},
						{No: 2, Text: "// @ts-expect-error legacy call"},
						{No: 3, Text: "// @ts-nocheck"},
						{No: 4, Text: "/* eslint-disable */"},
						{No: 5, Text: "const x = 1; // eslint-disable-line no-unused-vars"},
						{No: 6, Text: "// eslint-disable-next-line @typescript-eslint/no-explicit-any"},
						{No: 7, Text: "return (value as any).total;"},
					},
				},
				{
					Path:   "src/cart.test.tsx",
					Status: diff.Added,
					Added: []diff.Line{
						{No: 10, Text: "const el = mockData as any;"},
					},
				},
			},
			wantLines: []int{1, 2, 3, 4, 5, 6, 7, 10},
		},
		{
			name: "all supported TS/JS extensions are checked",
			files: []diff.File{
				{Path: "src/a.js", Status: diff.Added, Added: []diff.Line{{No: 1, Text: "// eslint-disable-next-line"}}},
				{Path: "src/b.jsx", Status: diff.Added, Added: []diff.Line{{No: 2, Text: "// eslint-disable-next-line"}}},
				{Path: "src/c.mjs", Status: diff.Added, Added: []diff.Line{{No: 3, Text: "// eslint-disable-next-line"}}},
				{Path: "src/d.cjs", Status: diff.Added, Added: []diff.Line{{No: 4, Text: "// eslint-disable-next-line"}}},
				{Path: "src/e.mts", Status: diff.Added, Added: []diff.Line{{No: 5, Text: "// @ts-ignore"}}},
				{Path: "src/f.cts", Status: diff.Added, Added: []diff.Line{{No: 6, Text: "// @ts-expect-error"}}},
			},
			wantLines: []int{1, 2, 3, 4, 5, 6},
		},
		{
			name: "one finding per added line even with multiple suppressions",
			files: []diff.File{
				{
					Path:   "src/cart.ts",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 12, Text: "const x = (a as any) + (b as any); // eslint-disable-line"},
					},
				},
			},
			wantLines: []int{12},
		},
		{
			name: "moved or re-indented suppressions are not flagged",
			files: []diff.File{
				{
					Path:   "src/cart.ts",
					Status: diff.Modified,
					Removed: []diff.Line{
						{No: 5, Text: "// @ts-ignore"},
						{No: 6, Text: "// @ts-expect-error"},
						{No: 7, Text: "// @ts-nocheck"},
						{No: 8, Text: "// eslint-disable-next-line"},
						{No: 9, Text: "const a = x as any;"},
					},
					Added: []diff.Line{
						{No: 15, Text: "  // @ts-ignore"},
						{No: 16, Text: "  // @ts-expect-error"},
						{No: 17, Text: "  // @ts-nocheck"},
						{No: 18, Text: "  // eslint-disable-next-line"},
						{No: 19, Text: "  const a = x as any;"},
					},
				},
			},
		},
		{
			name: "non-TS/JS, config, snapshot, and deleted files are ignored",
			files: []diff.File{
				{
					Path:   "README.md",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 1, Text: "// @ts-ignore"}},
				},
				{
					Path:   "vitest.config.ts",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 1, Text: "// @ts-ignore"}},
				},
				{
					Path:   "src/__snapshots__/cart.test.ts.snap",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 1, Text: "// @ts-ignore"}},
				},
				{
					Path:   "src/deleted.ts",
					Status: diff.Deleted,
					Added:  []diff.Line{{No: 1, Text: "// @ts-ignore"}},
				},
			},
		},
		{
			name: "normal code without suppressions produces no findings",
			files: []diff.File{
				{
					Path:   "src/cart.ts",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 1, Text: "const hasAny = true;"},
						{No: 2, Text: "export function sum(a: number, b: number): number { return a + b; }"},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			findings := r.Check(&Context{
				Diff:   &diff.Diff{Files: tc.files},
				Policy: policy.Default(),
			})
			if len(findings) != len(tc.wantLines) {
				t.Fatalf("got %d findings (%+v), want %d", len(findings), findings, len(tc.wantLines))
			}
			for i, wantLine := range tc.wantLines {
				f := findings[i]
				if f.RuleID != "C002" || f.Severity != Warn || f.Line != wantLine || f.Path == "" || f.Message == "" || f.Hint == "" {
					t.Errorf("unexpected finding[%d]: %+v (want line %d)", i, f, wantLine)
				}
			}
		})
	}
}
