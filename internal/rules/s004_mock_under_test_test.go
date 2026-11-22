package rules

import (
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/diff"
	"github.com/Ramanan-Bala/donegate/internal/policy"
)

func TestS004Registration(t *testing.T) {
	r, ok := Lookup("S004")
	if !ok {
		t.Fatal("S004 not registered")
	}
	if r.Level() != 1 {
		t.Errorf("Level() = %d, want 1", r.Level())
	}
	if r.DefaultSeverity() != Warn {
		t.Errorf("DefaultSeverity() = %q, want %q", r.DefaultSeverity(), Warn)
	}
}

func TestS004(t *testing.T) {
	r, ok := Lookup("S004")
	if !ok {
		t.Fatal("S004 not registered")
	}

	tests := []struct {
		name      string
		files     []diff.File
		wantLines []int
	}{
		{
			name: "jest and vi mock/doMock of modified or added source files",
			files: []diff.File{
				{Path: "src/cart.ts", Status: diff.Modified},
				{Path: "src/pricing/index.tsx", Status: diff.Added},
				{Path: "src/tax.service.ts", Status: diff.Modified},
				{
					Path:   "src/__tests__/cart.test.ts",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 3, Text: `jest.mock('../cart');`},
						{No: 4, Text: `vi.mock('../pricing', () => ({ total: () => 0 }));`},
						{No: 5, Text: `vi.doMock('../pricing/index.tsx');`},
						{No: 6, Text: `jest.doMock('../tax.service');`},
						{No: 7, Text: `vi.mock('/src/cart.js');`},
					},
				},
			},
			wantLines: []int{3, 4, 5, 6, 7},
		},
		{
			name: "new test file at root mocking root source file",
			files: []diff.File{
				{Path: "discount.ts", Status: diff.Modified},
				{
					Path:   "discount.test.ts",
					Status: diff.Added,
					Added: []diff.Line{
						{No: 2, Text: "vi.mock('./discount');"},
					},
				},
			},
			wantLines: []int{2},
		},
		{
			name: "bare package specifiers are ignored",
			files: []diff.File{
				{Path: "src/cart.ts", Status: diff.Modified},
				{
					Path:   "src/__tests__/cart.test.ts",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 2, Text: `jest.mock('axios');`},
						{No: 3, Text: `vi.mock('@acme/cart');`},
						{No: 4, Text: `vi.doMock('lodash/debounce');`},
						{No: 5, Text: `jest.doMock('cart');`},
					},
				},
			},
		},
		{
			name: "mocking unchanged source or deleted source is ignored",
			files: []diff.File{
				{Path: "src/cart.ts", Status: diff.Modified},
				{Path: "src/deleted.ts", Status: diff.Deleted},
				{
					Path:   "src/__tests__/cart.test.ts",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 2, Text: `jest.mock('../logger');`},
						{No: 3, Text: `vi.mock('../deleted');`},
					},
				},
			},
		},
		{
			name: "removed lines, snapshots, and source files calling mock are ignored",
			files: []diff.File{
				{
					Path:   "src/cart.ts",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 1, Text: `vi.mock('./cart');`}},
				},
				{
					Path:   "src/__snapshots__/cart.test.ts.snap",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 1, Text: `vi.mock('../cart');`}},
				},
				{
					Path:    "src/__tests__/cart.test.ts",
					Status:  diff.Modified,
					Removed: []diff.Line{{No: 2, Text: `jest.mock('../cart');`}},
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
				if f.RuleID != "S004" || f.Severity != Warn || f.Line != wantLine || f.Path == "" || f.Message == "" || f.Hint == "" {
					t.Errorf("unexpected finding[%d]: %+v (want line %d)", i, f, wantLine)
				}
			}
		})
	}
}
