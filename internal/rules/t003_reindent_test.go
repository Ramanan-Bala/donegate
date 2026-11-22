package rules

import (
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/diff"
	"github.com/Ramanan-Bala/donegate/internal/policy"
)

func TestT003Reindent(t *testing.T) {
	r, ok := Lookup("T003")
	if !ok {
		t.Fatal("T003 not registered")
	}

	tests := []struct {
		name      string
		file      diff.File
		wantLines []int
	}{
		{
			name: "re-indented existing skip is not flagged",
			file: diff.File{
				Path:    "src/cart.test.ts",
				Status:  diff.Modified,
				Removed: []diff.Line{{No: 7, Text: "it.skip('legacy', () => {});"}},
				Added:   []diff.Line{{No: 8, Text: "    it.skip('legacy', () => {});"}},
			},
		},
		{
			name: "re-indent plus a new skip flags only the new one",
			file: diff.File{
				Path:    "src/cart.test.ts",
				Status:  diff.Modified,
				Removed: []diff.Line{{No: 7, Text: "\tit.skip('legacy', () => {});"}},
				Added: []diff.Line{
					{No: 8, Text: "  it.skip('legacy', () => {});"},
					{No: 12, Text: "  it.skip('applies discount', () => {});"},
				},
			},
			wantLines: []int{12},
		},
		{
			name: "skip whose text changed is still flagged",
			file: diff.File{
				Path:    "src/cart.test.ts",
				Status:  diff.Modified,
				Removed: []diff.Line{{No: 7, Text: "it('applies discount', () => {});"}},
				Added:   []diff.Line{{No: 7, Text: "it.only('applies discount', () => {});"}},
			},
			wantLines: []int{7},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			findings := r.Check(&Context{
				Diff:   &diff.Diff{Files: []diff.File{tc.file}},
				Policy: policy.Default(),
			})
			if len(findings) != len(tc.wantLines) {
				t.Fatalf("got %d findings (%+v), want %d", len(findings), findings, len(tc.wantLines))
			}
			for i, wantLine := range tc.wantLines {
				if f := findings[i]; f.RuleID != "T003" || f.Line != wantLine {
					t.Errorf("unexpected finding[%d]: %+v (want line %d)", i, f, wantLine)
				}
			}
		})
	}
}
