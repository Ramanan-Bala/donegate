package rules

import (
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/diff"
	"github.com/Ramanan-Bala/donegate/internal/policy"
)

func TestT004Registration(t *testing.T) {
	r, ok := Lookup("T004")
	if !ok {
		t.Fatal("T004 not registered")
	}
	if r.Level() != 1 {
		t.Errorf("Level() = %d, want 1", r.Level())
	}
	if r.DefaultSeverity() != Block {
		t.Errorf("DefaultSeverity() = %q, want %q", r.DefaultSeverity(), Block)
	}
}

func TestT004(t *testing.T) {
	r, ok := Lookup("T004")
	if !ok {
		t.Fatal("T004 not registered")
	}

	type wantFinding struct {
		path string
		line int
	}

	tests := []struct {
		name          string
		allowNewTests bool
		files         []diff.File
		want          []wantFinding
	}{
		{
			name:          "modified snapshot triggers T004",
			allowNewTests: true,
			files: []diff.File{
				{Path: "src/__snapshots__/cart.test.ts.snap", Status: diff.Modified},
			},
			want: []wantFinding{{path: "src/__snapshots__/cart.test.ts.snap"}},
		},
		{
			name:          "deleted snapshot triggers T004",
			allowNewTests: true,
			files: []diff.File{
				{Path: "src/other.snap", Status: diff.Deleted},
			},
			want: []wantFinding{{path: "src/other.snap"}},
		},
		{
			name:          "renamed snapshot triggers T004",
			allowNewTests: true,
			files: []diff.File{
				{Path: "src/__snapshots__/new.snap", OldPath: "src/__snapshots__/old.snap", Status: diff.Renamed},
			},
			want: []wantFinding{{path: "src/__snapshots__/new.snap"}},
		},
		{
			name:          "added snapshot allowed when allow_new_tests is true",
			allowNewTests: true,
			files: []diff.File{
				{Path: "src/__snapshots__/new.test.ts.snap", Status: diff.Added},
			},
		},
		{
			name:          "added snapshot blocked when allow_new_tests is false",
			allowNewTests: false,
			files: []diff.File{
				{Path: "src/__snapshots__/new.test.ts.snap", Status: diff.Added},
			},
			want: []wantFinding{{path: "src/__snapshots__/new.test.ts.snap"}},
		},
		{
			name:          "inline snapshot added in modified test triggers T004 with line",
			allowNewTests: true,
			files: []diff.File{
				{
					Path:   "src/cart.test.ts",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 14, Text: "  expect(formatTotal(900)).toMatchInlineSnapshot(`\"$1000.00\"`);"},
					},
				},
			},
			want: []wantFinding{{path: "src/cart.test.ts", line: 14}},
		},
		{
			name:          "inline snapshot in newly added test is allowed",
			allowNewTests: true,
			files: []diff.File{
				{
					Path:   "src/new.test.ts",
					Status: diff.Added,
					Added: []diff.Line{
						{No: 5, Text: "  expect(x).toMatchInlineSnapshot(`\"ok\"`);"},
					},
				},
			},
		},
		{
			name:          "honest source edit has no findings",
			allowNewTests: true,
			files: []diff.File{
				{
					Path:   "src/cart.ts",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 10, Text: "return roundPrice(total);"}},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pol := policy.Default()
			pol.Protect.AllowNewTests = tc.allowNewTests
			findings := r.Check(&Context{
				Diff:   &diff.Diff{Files: tc.files},
				Policy: pol,
			})
			if len(findings) != len(tc.want) {
				t.Fatalf("got %d findings (%+v), want %d", len(findings), findings, len(tc.want))
			}
			for i, w := range tc.want {
				f := findings[i]
				if f.RuleID != "T004" || f.Path != w.path || f.Line != w.line || f.Message == "" || f.Hint == "" {
					t.Errorf("finding[%d] = %+v, want path=%q line=%d", i, f, w.path, w.line)
				}
			}
		})
	}
}
