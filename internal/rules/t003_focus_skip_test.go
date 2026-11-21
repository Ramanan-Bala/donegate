package rules

import (
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/diff"
	"github.com/Ramanan-Bala/donegate/internal/policy"
)

func TestT003Registration(t *testing.T) {
	r, ok := Lookup("T003")
	if !ok {
		t.Fatal("T003 not registered")
	}
	if r.Level() != 1 {
		t.Errorf("Level() = %d, want 1", r.Level())
	}
	if r.DefaultSeverity() != Block {
		t.Errorf("DefaultSeverity() = %q, want %q", r.DefaultSeverity(), Block)
	}
}

func TestT003(t *testing.T) {
	r, ok := Lookup("T003")
	if !ok {
		t.Fatal("T003 not registered")
	}

	tests := []struct {
		name      string
		files     []diff.File
		wantLines []int
	}{
		{
			name: "it.only in modified test",
			files: []diff.File{
				{
					Path:   "src/cart.test.ts",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 5, Text: "  it.only('works', () => {"}},
				},
			},
			wantLines: []int{5},
		},
		{
			name: "various focus/skip/todo/fails patterns in added test",
			files: []diff.File{
				{
					Path:   "src/__tests__/new.ts",
					Status: diff.Added,
					Added: []diff.Line{
						{No: 1, Text: "describe.only('suite', () => {"},
						{No: 2, Text: "  test.skip('a', () => {});"},
						{No: 3, Text: "  it.todo('b');"},
						{No: 4, Text: "  test.fails('c', () => {});"},
						{No: 5, Text: "  it.fails('d', () => {});"},
						{No: 6, Text: "  xit('e', () => {});"},
						{No: 7, Text: "  xtest('f', () => {});"},
						{No: 8, Text: "  xdescribe('g', () => {});"},
						{No: 9, Text: "  fit('h', () => {});"},
						{No: 10, Text: "  fdescribe('i', () => {});"},
						{No: 11, Text: "  describe.skip('j', () => {});"},
						{No: 12, Text: "  it('normal', () => {});"},
					},
				},
			},
			wantLines: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
		},
		{
			name: "removed lines and deleted files are ignored",
			files: []diff.File{
				{
					Path:    "src/cart.test.ts",
					Status:  diff.Modified,
					Removed: []diff.Line{{No: 3, Text: "  it.only('old', () => {});"}},
					Added:   []diff.Line{{No: 3, Text: "  it('old', () => {});"}},
				},
				{
					Path:   "src/other.test.ts",
					Status: diff.Deleted,
					Added:  []diff.Line{{No: 1, Text: "it.skip('x', () => {});"}},
				},
			},
		},
		{
			name: "source file and snapshot are ignored",
			files: []diff.File{
				{
					Path:   "src/cart.ts",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 4, Text: "const fit = (x: number) => x; fit(1);"}},
				},
				{
					Path:   "src/__snapshots__/cart.test.ts.snap",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 1, Text: "it.skip('snap');"}},
				},
			},
		},
		{
			name: "honest new test file produces no findings",
			files: []diff.File{
				{
					Path:   "src/discount.spec.ts",
					Status: diff.Added,
					Added: []diff.Line{
						{No: 1, Text: "describe('discount', () => {"},
						{No: 2, Text: "  it('applies 10%', () => expect(true).toBe(true));"},
						{No: 3, Text: "});"},
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
				if f.RuleID != "T003" || f.Line != wantLine || f.Path == "" || f.Message == "" || f.Hint == "" {
					t.Errorf("unexpected finding[%d]: %+v (want line %d)", i, f, wantLine)
				}
			}
		})
	}
}
