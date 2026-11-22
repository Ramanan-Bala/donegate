package rules

import (
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/diff"
	"github.com/Ramanan-Bala/donegate/internal/policy"
)

func TestT005Registration(t *testing.T) {
	r, ok := Lookup("T005")
	if !ok {
		t.Fatal("T005 not registered")
	}
	if r.Level() != 1 {
		t.Errorf("Level() = %d, want 1", r.Level())
	}
	if r.DefaultSeverity() != Block {
		t.Errorf("DefaultSeverity() = %q, want %q", r.DefaultSeverity(), Block)
	}
}

func TestT005(t *testing.T) {
	r, ok := Lookup("T005")
	if !ok {
		t.Fatal("T005 not registered")
	}

	tests := []struct {
		name         string
		files        []diff.File
		wantLines    []int
		wantMsgSub   string
		wantFindings int
	}{
		{
			name: "all weak matchers flagged when strict matcher removed",
			files: []diff.File{
				{
					Path:   "src/cart.test.ts",
					Status: diff.Modified,
					Removed: []diff.Line{
						{No: 10, Text: "expect(a).toBe(1);"},
						{No: 11, Text: "expect(b).toEqual({ x: 1 });"},
						{No: 12, Text: "expect(c).toStrictEqual({ y: 2 });"},
						{No: 13, Text: "expect(d).toMatchObject({ z: 3 });"},
						{No: 14, Text: "expect(fn).toHaveBeenCalledWith('arg');"},
						{No: 15, Text: "expect(run).toThrow('boom');"},
					},
					Added: []diff.Line{
						{No: 10, Text: "expect(a).toBeDefined();"},
						{No: 11, Text: "expect(b).toBeTruthy();"},
						{No: 12, Text: "expect(c).toBeFalsy();"},
						{No: 13, Text: "expect(d).not.toBeNull();"},
						{No: 14, Text: "expect(fn).not.toBeUndefined();"},
						{No: 15, Text: "expect(run).toEqual(expect.anything());"},
					},
				},
			},
			wantLines:    []int{10, 11, 12, 13, 14, 15},
			wantFindings: 6,
		},
		{
			name: "toThrow with arg triggers weak replacement",
			files: []diff.File{
				{
					Path:    "src/err.spec.ts",
					Status:  diff.Modified,
					Removed: []diff.Line{{No: 4, Text: "expect(fn).toThrow(TypeError);"}},
					Added:   []diff.Line{{No: 4, Text: "expect(fn).toBeDefined();"}},
				},
			},
			wantLines:    []int{4},
			wantFindings: 1,
		},
		{
			name: "bare toThrow() without arg does not count as strict matcher",
			files: []diff.File{
				{
					Path:    "src/err.spec.ts",
					Status:  diff.Modified,
					Removed: []diff.Line{{No: 4, Text: "expect(fn).toThrow();"}},
					Added:   []diff.Line{{No: 4, Text: "expect(fn).toBeDefined();"}},
				},
			},
			wantFindings: 0,
		},
		{
			name: "weak matcher without removed strict matcher is ignored",
			files: []diff.File{
				{
					Path:    "src/cart.test.ts",
					Status:  diff.Modified,
					Removed: []diff.Line{{No: 5, Text: "const res = compute();"}},
					Added: []diff.Line{
						{No: 5, Text: "const res = compute();"},
						{No: 6, Text: "expect(res).toBeDefined();"},
					},
				},
			},
			wantFindings: 0,
		},
		{
			name: "fewer expect calls triggers single file finding",
			files: []diff.File{
				{
					Path:   "src/cart.test.ts",
					Status: diff.Modified,
					Removed: []diff.Line{
						{No: 8, Text: "expect(a).toBe(1);"},
						{No: 9, Text: "expect(b).toBe(2);"},
						{No: 10, Text: "expect(c).toBe(3);"},
					},
					Added: []diff.Line{
						{No: 8, Text: "expect(a).toBe(1);"},
					},
				},
			},
			wantLines:    []int{0},
			wantMsgSub:   "2 fewer expect() calls",
			wantFindings: 1,
		},
		{
			name: "deleted whole test case is left to T002",
			files: []diff.File{
				{
					Path:   "src/cart.test.ts",
					Status: diff.Modified,
					Removed: []diff.Line{
						{No: 8, Text: "  it('applies discount', () => {"},
						{No: 9, Text: "    expect(total(1000)).toBe(900);"},
						{No: 10, Text: "  });"},
					},
				},
			},
			wantFindings: 0,
		},
		{
			name: "re-indented test case still counts missing expects",
			files: []diff.File{
				{
					Path:   "src/cart.test.ts",
					Status: diff.Modified,
					Removed: []diff.Line{
						{No: 8, Text: "it('applies discount', () => {"},
						{No: 9, Text: "  expect(total(1000)).toBe(900);"},
					},
					Added: []diff.Line{
						{No: 8, Text: "  it('applies discount', () => {"},
					},
				},
			},
			wantLines:    []int{0},
			wantMsgSub:   "1 fewer expect() calls",
			wantFindings: 1,
		},
		{
			name: "both weak matcher and fewer expect calls flagged",
			files: []diff.File{
				{
					Path:   "src/cart.test.ts",
					Status: diff.Modified,
					Removed: []diff.Line{
						{No: 1, Text: "expect(a).toEqual({ ok: true });"},
						{No: 2, Text: "expect(b).toBe(2);"},
					},
					Added: []diff.Line{
						{No: 1, Text: "expect(a).toBeTruthy();"},
					},
				},
			},
			wantLines:    []int{1, 0},
			wantMsgSub:   "1 fewer expect() calls",
			wantFindings: 2,
		},
		{
			name: "added test file, deleted test file, snapshot, and source file are ignored",
			files: []diff.File{
				{
					Path:    "src/new.test.ts",
					Status:  diff.Added,
					Removed: []diff.Line{{No: 1, Text: "expect(a).toBe(1);"}},
					Added:   []diff.Line{{No: 1, Text: "expect(a).toBeDefined();"}},
				},
				{
					Path:    "src/old.test.ts",
					Status:  diff.Deleted,
					Removed: []diff.Line{{No: 1, Text: "expect(a).toBe(1);"}},
				},
				{
					Path:    "src/__snapshots__/cart.test.ts.snap",
					Status:  diff.Modified,
					Removed: []diff.Line{{No: 1, Text: "expect(a).toBe(1);"}},
					Added:   []diff.Line{{No: 1, Text: "expect(a).toBeDefined();"}},
				},
				{
					Path:    "src/cart.ts",
					Status:  diff.Modified,
					Removed: []diff.Line{{No: 1, Text: "expect(a).toBe(1);"}},
					Added:   []diff.Line{{No: 1, Text: "expect(a).toBeDefined();"}},
				},
			},
			wantFindings: 0,
		},
		{
			name: "equal expect count and strict replacement produces no findings",
			files: []diff.File{
				{
					Path:    "src/cart.test.ts",
					Status:  diff.Modified,
					Removed: []diff.Line{{No: 5, Text: "expect(cartTotal([])).toBe(0);"}},
					Added:   []diff.Line{{No: 5, Text: "expect(cartTotal([])).toStrictEqual(0);"}},
				},
			},
			wantFindings: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			findings := r.Check(&Context{
				Diff:   &diff.Diff{Files: tc.files},
				Policy: policy.Default(),
			})
			if len(findings) != tc.wantFindings {
				t.Fatalf("got %d findings (%+v), want %d", len(findings), findings, tc.wantFindings)
			}
			for i, wantLine := range tc.wantLines {
				f := findings[i]
				if f.RuleID != "T005" || f.Severity != Block || f.Line != wantLine || f.Path == "" || f.Message == "" || f.Hint == "" {
					t.Errorf("unexpected finding[%d]: %+v (want line %d)", i, f, wantLine)
				}
			}
			if tc.wantMsgSub != "" {
				found := false
				for _, f := range findings {
					if strings.Contains(f.Message, tc.wantMsgSub) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected a finding message containing %q, got %+v", tc.wantMsgSub, findings)
				}
			}
		})
	}
}
