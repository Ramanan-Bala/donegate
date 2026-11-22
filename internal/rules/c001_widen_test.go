package rules

import (
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/diff"
	"github.com/Ramanan-Bala/donegate/internal/policy"
)

func TestC001FilterWidening(t *testing.T) {
	r, ok := Lookup("C001")
	if !ok {
		t.Fatal("C001 not registered")
	}

	cfg := func(removed, added string) []diff.File {
		return []diff.File{{
			Path:    "vitest.config.ts",
			Status:  diff.Modified,
			Removed: []diff.Line{{No: 4, Text: removed}},
			Added:   []diff.Line{{No: 4, Text: added}},
		}}
	}

	tests := []struct {
		name      string
		files     []diff.File
		wantLines []int
	}{
		{
			name:  "include entry added widens coverage",
			files: cfg(`    include: ["src/**/*.test.ts"],`, `    include: ["src/**/*.test.ts", "lib/**/*.test.ts"],`),
		},
		{
			name:  "exclude entry removed widens coverage",
			files: cfg(`    exclude: ['node_modules', 'e2e/**'],`, `    exclude: ['node_modules'],`),
		},
		{
			name:  "testPathIgnorePatterns emptied widens coverage",
			files: cfg(`  testPathIgnorePatterns: ["/legacy/"],`, `  testPathIgnorePatterns: [],`),
		},
		{
			name:      "include entry removed narrows coverage",
			files:     cfg(`    include: ["src/**/*.test.ts", "lib/**/*.test.ts"],`, `    include: ["src/**/*.test.ts"],`),
			wantLines: []int{4},
		},
		{
			name:      "exclude entry added narrows coverage",
			files:     cfg(`    exclude: ['node_modules'],`, `    exclude: ['node_modules', 'src/cart/**'],`),
			wantLines: []int{4},
		},
		{
			name:      "include entry swapped is mixed and flagged",
			files:     cfg(`    include: ["src/**/*.test.ts"],`, `    include: ["lib/**/*.test.ts"],`),
			wantLines: []int{4},
		},
		{
			name:      "multi-line array opener cannot be classified",
			files:     cfg(`    exclude: ['node_modules'],`, `    exclude: [`),
			wantLines: []int{4},
		},
		{
			name: "new filter key with no removed counterpart is flagged",
			files: []diff.File{{
				Path:   "vitest.config.ts",
				Status: diff.Modified,
				Added:  []diff.Line{{No: 5, Text: `    exclude: ["src/**"],`}},
			}},
			wantLines: []int{5},
		},
		{
			name:      "different key on removed line is not a counterpart",
			files:     cfg(`    include: ["src/**"],`, `    exclude: [],`),
			wantLines: []int{4},
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
				if f := findings[i]; f.RuleID != "C001" || f.Line != wantLine {
					t.Errorf("unexpected finding[%d]: %+v (want line %d)", i, f, wantLine)
				}
			}
		})
	}
}
