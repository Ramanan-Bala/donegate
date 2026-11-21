package rules

import (
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/diff"
	"github.com/Ramanan-Bala/donegate/internal/policy"
)

func TestT001Registration(t *testing.T) {
	r, ok := Lookup("T001")
	if !ok {
		t.Fatal("T001 not registered")
	}
	if r.Level() != 1 {
		t.Errorf("Level() = %d, want 1", r.Level())
	}
	if r.DefaultSeverity() != Block {
		t.Errorf("DefaultSeverity() = %q, want %q", r.DefaultSeverity(), Block)
	}
}

func TestT001(t *testing.T) {
	r, ok := Lookup("T001")
	if !ok {
		t.Fatal("T001 not registered")
	}

	tests := []struct {
		name          string
		allowNewTests bool
		files         []diff.File
		wantPaths     []string
	}{
		{
			name:          "modified source file is allowed",
			allowNewTests: true,
			files: []diff.File{
				{Path: "src/cart.ts", Status: diff.Modified},
			},
		},
		{
			name:          "modified test file triggers T001",
			allowNewTests: true,
			files: []diff.File{
				{Path: "src/cart.test.ts", Status: diff.Modified},
			},
			wantPaths: []string{"src/cart.test.ts"},
		},
		{
			name:          "modified __tests__ file triggers T001",
			allowNewTests: true,
			files: []diff.File{
				{Path: "src/__tests__/cart.ts", Status: diff.Modified},
			},
			wantPaths: []string{"src/__tests__/cart.ts"},
		},
		{
			name:          "modified e2e file triggers T001",
			allowNewTests: true,
			files: []diff.File{
				{Path: "e2e/checkout.ts", Status: diff.Modified},
			},
			wantPaths: []string{"e2e/checkout.ts"},
		},
		{
			name:          "modified snapshot does not trigger T001",
			allowNewTests: true,
			files: []diff.File{
				{Path: "src/__snapshots__/cart.test.ts.snap", Status: diff.Modified},
				{Path: "src/other.snap", Status: diff.Modified},
			},
		},
		{
			name:          "renamed test to non-test triggers T001 with new path",
			allowNewTests: true,
			files: []diff.File{
				{Path: "src/cart.ts", OldPath: "src/cart.test.ts", Status: diff.Renamed},
			},
			wantPaths: []string{"src/cart.ts"},
		},
		{
			name:          "renamed non-test to test triggers T001",
			allowNewTests: true,
			files: []diff.File{
				{Path: "src/cart.spec.ts", OldPath: "src/helper.ts", Status: diff.Renamed},
			},
			wantPaths: []string{"src/cart.spec.ts"},
		},
		{
			name:          "renamed snapshot does not trigger T001",
			allowNewTests: true,
			files: []diff.File{
				{Path: "src/__snapshots__/b.snap", OldPath: "src/__snapshots__/a.snap", Status: diff.Renamed},
			},
		},
		{
			name:          "added test file allowed when allow_new_tests is true",
			allowNewTests: true,
			files: []diff.File{
				{Path: "src/new.test.ts", Status: diff.Added},
			},
		},
		{
			name:          "added test file triggers T001 when allow_new_tests is false",
			allowNewTests: false,
			files: []diff.File{
				{Path: "src/new.test.ts", Status: diff.Added},
			},
			wantPaths: []string{"src/new.test.ts"},
		},
		{
			name:          "added snapshot ignored even when allow_new_tests is false",
			allowNewTests: false,
			files: []diff.File{
				{Path: "src/__snapshots__/new.test.ts.snap", Status: diff.Added},
			},
		},
		{
			name:          "deleted test file does not trigger T001",
			allowNewTests: true,
			files: []diff.File{
				{Path: "src/cart.test.ts", Status: diff.Deleted},
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
			if len(findings) != len(tc.wantPaths) {
				t.Fatalf("got %d findings (%+v), want %d (%v)", len(findings), findings, len(tc.wantPaths), tc.wantPaths)
			}
			for i, wantPath := range tc.wantPaths {
				f := findings[i]
				if f.RuleID != "T001" {
					t.Errorf("finding[%d].RuleID = %q, want T001", i, f.RuleID)
				}
				if f.Path != wantPath {
					t.Errorf("finding[%d].Path = %q, want %q", i, f.Path, wantPath)
				}
				if f.Message == "" || f.Hint == "" {
					t.Errorf("finding[%d] missing Message or Hint: %+v", i, f)
				}
			}
		})
	}
}
