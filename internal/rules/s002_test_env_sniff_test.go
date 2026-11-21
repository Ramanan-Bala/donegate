package rules

import (
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/diff"
	"github.com/Ramanan-Bala/donegate/internal/policy"
)

func TestS002Registration(t *testing.T) {
	r, ok := Lookup("S002")
	if !ok {
		t.Fatal("S002 not registered")
	}
	if r.Level() != 1 {
		t.Errorf("Level() = %d, want 1", r.Level())
	}
	if r.DefaultSeverity() != Block {
		t.Errorf("DefaultSeverity() = %q, want %q", r.DefaultSeverity(), Block)
	}
}

func TestS002(t *testing.T) {
	r, ok := Lookup("S002")
	if !ok {
		t.Fatal("S002 not registered")
	}

	tests := []struct {
		name      string
		files     []diff.File
		wantLines []int
	}{
		{
			name: "all test env sniff patterns in source files",
			files: []diff.File{
				{
					Path:   "src/cart.ts",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 1, Text: `if (process.env.NODE_ENV === 'test') return 1;`},
						{No: 2, Text: `if ('test' == process.env.NODE_ENV) return 2;`},
						{No: 3, Text: `if (process.env.JEST_WORKER_ID) return 3;`},
						{No: 4, Text: `if (process.env.VITEST) return 4;`},
						{No: 5, Text: `if (import.meta.env.VITEST) return 5;`},
						{No: 6, Text: `if (typeof jest !== 'undefined') return 6;`},
					},
				},
			},
			wantLines: []int{1, 2, 3, 4, 5, 6},
		},
		{
			name: "production env check and test files do not trigger",
			files: []diff.File{
				{
					Path:   "src/cart.ts",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 3, Text: `if (process.env.NODE_ENV === 'production') return;`}},
				},
				{
					Path:   "src/cart.test.ts",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 3, Text: `if (process.env.NODE_ENV === 'test') return;`}},
				},
				{
					Path:   "README.md",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 3, Text: `NODE_ENV === 'test'`}},
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
				if f.RuleID != "S002" || f.Line != wantLine || f.Path == "" || f.Message == "" || f.Hint == "" {
					t.Errorf("unexpected finding[%d]: %+v", i, f)
				}
			}
		})
	}
}
