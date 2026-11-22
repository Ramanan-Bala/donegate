package rules

import (
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/diff"
	"github.com/Ramanan-Bala/donegate/internal/policy"
)

func t003LanguagePolicy() *policy.Policy {
	pol := policy.Default()
	pol.Protect.Tests = append(pol.Protect.Tests,
		"**/*_test.go",
		"**/test_*.py",
		"**/*_test.py",
		"**/*Tests.cs",
		"**/*Tests.fs",
		"**/*Tests.vb",
	)
	return pol
}

func TestT003Languages(t *testing.T) {
	r, ok := Lookup("T003")
	if !ok {
		t.Fatal("T003 not registered")
	}

	tests := []struct {
		name      string
		pol       *policy.Policy
		files     []diff.File
		wantLines []int
	}{
		{
			name: "go skip variants and build ignore tags are flagged",
			pol:  t003LanguagePolicy(),
			files: []diff.File{
				{
					Path:   "internal/cart/cart_test.go",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 1, Text: "//go:build ignore"},
						{No: 2, Text: "// +build ignore"},
						{No: 10, Text: `	t.Skip("flaky")`},
						{No: 11, Text: `	t.SkipNow()`},
						{No: 12, Text: `	b.Skipf("slow %d", 1)`},
						{No: 13, Text: `	t.Fatal("real failure")`},
					},
				},
			},
			wantLines: []int{1, 2, 10, 11, 12},
		},
		{
			name: "go negatives and re-indented skip are not flagged",
			pol:  t003LanguagePolicy(),
			files: []diff.File{
				{
					Path:   "internal/cart/cart_test.go",
					Status: diff.Modified,
					Removed: []diff.Line{
						{No: 20, Text: `t.Skip("legacy")`},
					},
					Added: []diff.Line{
						{No: 1, Text: "//go:build !ignore"},
						{No: 2, Text: "//go:build integration"},
						{No: 5, Text: `	skipIfNoDocker(t)`},
						{No: 21, Text: `	t.Skip("legacy")`},
					},
				},
			},
		},
		{
			name: "python pytest and unittest skip decorators are flagged",
			pol:  t003LanguagePolicy(),
			files: []diff.File{
				{
					Path:   "tests/test_cart.py",
					Status: diff.Added,
					Added: []diff.Line{
						{No: 1, Text: "@pytest.mark.skip"},
						{No: 2, Text: `@pytest.mark.skipif(True, reason="broken")`},
						{No: 3, Text: `@pytest.mark.xfail(reason="known")`},
						{No: 4, Text: `    pytest.skip("dynamic")`},
						{No: 5, Text: `@unittest.skip("not ready")`},
						{No: 6, Text: `@unittest.skipIf(True, "win")`},
						{No: 7, Text: `@unittest.skipUnless(False, "req")`},
						{No: 8, Text: "@unittest.expectedFailure"},
						{No: 9, Text: "def test_ok(): assert True"},
					},
				},
			},
			wantLines: []int{1, 2, 3, 4, 5, 6, 7, 8},
		},
		{
			name: "python negatives and re-indented skip are not flagged",
			pol:  t003LanguagePolicy(),
			files: []diff.File{
				{
					Path:   "tests/test_cart.py",
					Status: diff.Modified,
					Removed: []diff.Line{
						{No: 4, Text: "@pytest.mark.skip"},
					},
					Added: []diff.Line{
						{No: 4, Text: "    @pytest.mark.skip"},
						{No: 8, Text: `@pytest.mark.parametrize("x", [1, 2])`},
						{No: 9, Text: "@pytest.mark.skip_custom"},
						{No: 10, Text: "def test_param(x): assert x > 0"},
					},
				},
			},
		},
		{
			name: "dotnet csharp fsharp vb skip and ignore attributes are flagged",
			pol:  t003LanguagePolicy(),
			files: []diff.File{
				{
					Path:   "tests/CartTests.cs",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 3, Text: `    [Ignore]`},
						{No: 4, Text: `    [Ignore("temporarily disabled")]`},
						{No: 5, Text: `    [Test, Ignore("x")]`},
						{No: 6, Text: `    [Fact(Skip = "not implemented")]`},
						{No: 7, Text: `    [Fact(DisplayName = "x", Skip="broken")]`},
						{No: 8, Text: `    [Theory(Skip = "flaky")]`},
						{No: 9, Text: `    [Explicit]`},
						{No: 10, Text: `    [Test, Explicit("manual")]`},
					},
				},
				{
					Path:   "tests/CartTests.fs",
					Status: diff.Added,
					Added: []diff.Line{
						{No: 2, Text: `    [<Ignore("x")>]`},
						{No: 3, Text: `    [<Fact(Skip = "x")>]`},
						{No: 4, Text: `    [<Explicit>]`},
					},
				},
				{
					Path:   "tests/CartTests.vb",
					Status: diff.Added,
					Added: []diff.Line{
						{No: 2, Text: `    [Theory(Skip="x")]`},
					},
				},
			},
			wantLines: []int{3, 4, 5, 6, 7, 8, 9, 10, 2, 3, 4, 2},
		},
		{
			name: "dotnet negatives and re-indented skip are not flagged",
			pol:  t003LanguagePolicy(),
			files: []diff.File{
				{
					Path:   "tests/CartTests.cs",
					Status: diff.Modified,
					Removed: []diff.Line{
						{No: 10, Text: `[Fact(Skip = "legacy")]`},
					},
					Added: []diff.Line{
						{No: 1, Text: `    [Fact]`},
						{No: 2, Text: `    [Theory]`},
						{No: 3, Text: `    [JsonIgnore]`},
						{No: 4, Text: `    [Fact(DisplayName = "Ignore whitespace")]`},
						{No: 5, Text: `    [Fact(DisplayName = "Skip = 1")]`},
						{No: 11, Text: `    [Fact(Skip = "legacy")]`},
					},
				},
			},
		},
		{
			name: "js file containing go t.Skip( text is not flagged by go pattern",
			pol:  t003LanguagePolicy(),
			files: []diff.File{
				{
					Path:   "src/cart.test.js",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 5, Text: `  const s = "t.Skip('not js')";`},
						{No: 6, Text: `  t.Skip('still not js');`},
					},
				},
			},
		},
		{
			name: "default policy without opt-in globs does not flag go python or dotnet tests",
			pol:  policy.Default(),
			files: []diff.File{
				{
					Path:   "internal/cart/cart_test.go",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 5, Text: `t.Skip("x")`}},
				},
				{
					Path:   "tests/test_cart.py",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 5, Text: `@pytest.mark.skip`}},
				},
				{
					Path:   "tests/CartTests.cs",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 5, Text: `[Fact(Skip = "x")]`}},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			findings := r.Check(&Context{
				Diff:   &diff.Diff{Files: tc.files},
				Policy: tc.pol,
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
