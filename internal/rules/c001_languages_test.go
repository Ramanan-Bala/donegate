package rules

import (
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/diff"
	"github.com/Ramanan-Bala/donegate/internal/policy"
)

func c001LanguagePolicy() *policy.Policy {
	pol := policy.Default()
	pol.Protect.Config = append(pol.Protect.Config,
		"**/*.csproj",
		"**/*.fsproj",
		"**/*.vbproj",
		"**/Directory.Build.props",
		"**/pytest.ini",
		"**/setup.cfg",
		"**/tox.ini",
		"**/pyproject.toml",
		"**/Makefile",
		"**/*.mk",
	)
	return pol
}

func TestC001Languages(t *testing.T) {
	r, ok := Lookup("C001")
	if !ok {
		t.Fatal("C001 not registered")
	}

	tests := []struct {
		name       string
		pol        *policy.Policy
		files      []diff.File
		wantLines  []int
		wantReason string
	}{
		{
			name: "dotnet csproj fsproj vbproj and Directory.Build.props Compile Remove flagged",
			pol:  c001LanguagePolicy(),
			files: []diff.File{
				{
					Path:   "src/Cart.Tests/Cart.Tests.csproj",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 12, Text: `    <Compile Remove="FlakyTests.cs" />`}},
				},
				{
					Path:   "src/Cart.Tests/Cart.Tests.fsproj",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 8, Text: `    <Compile Remove="FlakyTests.fs" />`}},
				},
				{
					Path:   "src/Cart.Tests/Cart.Tests.vbproj",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 9, Text: `    <Compile Remove="FlakyTests.vb" />`}},
				},
				{
					Path:   "Directory.Build.props",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 4, Text: `    <Compile Remove="**/*SlowTests.cs" />`}},
				},
			},
			wantLines:  []int{12, 8, 9, 4},
			wantReason: "test source excluded from build",
		},
		{
			name: "dotnet project non-loosening edits are not flagged",
			pol:  c001LanguagePolicy(),
			files: []diff.File{
				{
					Path:   "src/Cart.Tests/Cart.Tests.csproj",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 10, Text: `    <Compile Include="NewTests.cs" />`},
						{No: 11, Text: `    <PackageReference Include="xunit" Version="2.9.0" />`},
					},
				},
			},
		},
		{
			name: "pytest config narrowing flags in ini cfg tox and pyproject",
			pol:  c001LanguagePolicy(),
			files: []diff.File{
				{
					Path:   "pytest.ini",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 3, Text: "addopts = --ignore=tests/slow"}},
				},
				{
					Path:   "setup.cfg",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 14, Text: "addopts = --deselect tests/test_cart.py::test_discount"}},
				},
				{
					Path:   "tox.ini",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 7, Text: `commands = pytest -k "not slow"`}},
				},
				{
					Path:   "pyproject.toml",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 21, Text: `addopts = ["-k=smoke"]`},
						{No: 22, Text: `addopts = ["--continue-on-collection-errors"]`},
					},
				},
			},
			wantLines:  []int{3, 14, 7, 21, 22},
			wantReason: "pytest test selection narrowed",
		},
		{
			name: "pytest config negatives and comments are not flagged",
			pol:  c001LanguagePolicy(),
			files: []diff.File{
				{
					Path:   "pytest.ini",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 2, Text: "# addopts = --ignore=tests/slow"},
						{No: 3, Text: "addopts = -ra -q --strict-markers"},
						{No: 4, Text: "python_files = test_*.py"},
					},
				},
				{
					Path:   "pyproject.toml",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 10, Text: `requires-python = ">=3.11"`},
						{No: 11, Text: `my-key = "value"`},
					},
				},
			},
		},
		{
			name: "go test -run and -skip in Makefile mk and workflows are flagged",
			pol:  c001LanguagePolicy(),
			files: []diff.File{
				{
					Path:   "Makefile",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 15, Text: "\tgo test -run TestFast ./..."},
						{No: 16, Text: "\t@go test ./... -skip=TestSlow"},
					},
				},
				{
					Path:   "mk/test.mk",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 6, Text: "\tgo test -race -run=TestUnit ./..."}},
				},
				{
					Path:   ".github/workflows/ci.yml",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 24, Text: "        run: go test -skip TestIntegration ./..."}},
				},
			},
			wantLines:  []int{15, 16, 6, 24},
			wantReason: "go test filtered with -run/-skip",
		},
		{
			name: "go test runner negatives and comments are not flagged",
			pol:  c001LanguagePolicy(),
			files: []diff.File{
				{
					Path:   "Makefile",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 10, Text: "\t# go test -run TestFast ./..."},
						{No: 11, Text: "\tgo test -race -count=1 ./..."},
						{No: 12, Text: "\tgo test ./internal/dry-run/..."},
					},
				},
				{
					Path:   ".github/workflows/ci.yml",
					Status: diff.Modified,
					Added: []diff.Line{
						{No: 20, Text: "        name: run go test"},
						{No: 21, Text: "        run: go test ./..."},
					},
				},
			},
		},
		{
			name: "unprotected language configs under default policy are not flagged",
			pol:  policy.Default(),
			files: []diff.File{
				{
					Path:   "src/Cart.Tests/Cart.Tests.csproj",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 5, Text: `<Compile Remove="FlakyTests.cs" />`}},
				},
				{
					Path:   "pytest.ini",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 3, Text: "addopts = --ignore=tests/slow"}},
				},
				{
					Path:   "Makefile",
					Status: diff.Modified,
					Added:  []diff.Line{{No: 4, Text: "\tgo test -run TestFast ./..."}},
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
				if f.RuleID != "C001" || f.Line != wantLine || f.Path == "" || f.Hint == "" {
					t.Errorf("unexpected finding[%d]: %+v (want line %d)", i, f, wantLine)
				}
				if tc.wantReason != "" && !strings.Contains(f.Message, tc.wantReason) {
					t.Errorf("finding[%d].Message = %q, want substring %q", i, f.Message, tc.wantReason)
				}
			}
		})
	}
}
