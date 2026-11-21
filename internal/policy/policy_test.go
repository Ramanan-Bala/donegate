package policy

import (
	"reflect"
	"strings"
	"testing"
)

func TestDefault(t *testing.T) {
	p := Default()
	if p.Version != 1 {
		t.Fatalf("Version = %d, want 1", p.Version)
	}
	if !p.Protect.AllowNewTests {
		t.Fatalf("Protect.AllowNewTests = false, want true")
	}
	wantTests := []string{
		"**/*.test.*",
		"**/*.spec.*",
		"**/__tests__/**",
		"**/__snapshots__/**",
		"e2e/**",
	}
	if !reflect.DeepEqual(p.Protect.Tests, wantTests) {
		t.Fatalf("Protect.Tests = %v, want %v", p.Protect.Tests, wantTests)
	}
	wantConfig := []string{
		"**/jest.config.*",
		"**/vitest.config.*",
		"**/tsconfig*.json",
		"**/.eslintrc*",
		"**/eslint.config.*",
		".github/workflows/**",
	}
	if !reflect.DeepEqual(p.Protect.Config, wantConfig) {
		t.Fatalf("Protect.Config = %v, want %v", p.Protect.Config, wantConfig)
	}
	if p.Gate.Tier != "standard" {
		t.Fatalf("Gate.Tier = %q, want standard", p.Gate.Tier)
	}
	if p.Gate.MaxBlocks != 5 {
		t.Fatalf("Gate.MaxBlocks = %d, want 5", p.Gate.MaxBlocks)
	}
	if p.Gate.TimeoutSeconds != 300 {
		t.Fatalf("Gate.TimeoutSeconds = %d, want 300", p.Gate.TimeoutSeconds)
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
		check   func(t *testing.T, p *Policy)
	}{
		{
			name: "partial yaml keeps defaults",
			yaml: `
version: 1
rules:
  T001: warn
gate:
  tier: strict
`,
			check: func(t *testing.T, p *Policy) {
				t.Helper()
				if p.Gate.Tier != "strict" {
					t.Errorf("Gate.Tier = %q, want strict", p.Gate.Tier)
				}
				if p.Gate.MaxBlocks != 5 {
					t.Errorf("Gate.MaxBlocks = %d, want 5", p.Gate.MaxBlocks)
				}
				if p.Gate.TimeoutSeconds != 300 {
					t.Errorf("Gate.TimeoutSeconds = %d, want 300", p.Gate.TimeoutSeconds)
				}
				if !p.Protect.AllowNewTests {
					t.Errorf("Protect.AllowNewTests = false, want true")
				}
				if len(p.Protect.Tests) == 0 || len(p.Protect.Config) == 0 {
					t.Errorf("expected default protect patterns to be preserved")
				}
				if p.Rules["T001"] != "warn" {
					t.Errorf("Rules[T001] = %q, want warn", p.Rules["T001"])
				}
			},
		},
		{
			name: "full yaml overrides defaults",
			yaml: `
version: 1
projects:
  - name: api
    root: backend
    test: npm test
    typecheck: npx tsc --noEmit
    lint: npm run lint
protect:
  allow_new_tests: false
rules:
  S001: block
  R004: off
judge:
  enabled: true
  model: deepseek/deepseek-chat
  base_url: https://openrouter.ai/api/v1
`,
			check: func(t *testing.T, p *Policy) {
				t.Helper()
				if p.Protect.AllowNewTests {
					t.Errorf("Protect.AllowNewTests = true, want false")
				}
				if len(p.Projects) != 1 || p.Projects[0].Name != "api" || p.Projects[0].Root != "backend" {
					t.Errorf("unexpected Projects: %+v", p.Projects)
				}
				if !p.Judge.Enabled || p.Judge.Model != "deepseek/deepseek-chat" || p.Judge.BaseURL != "https://openrouter.ai/api/v1" {
					t.Errorf("unexpected Judge: %+v", p.Judge)
				}
				if p.Rules["S001"] != "block" || p.Rules["R004"] != "off" {
					t.Errorf("unexpected Rules: %+v", p.Rules)
				}
			},
		},
		{
			name:    "bad version",
			yaml:    "version: 2\n",
			wantErr: "unsupported version",
		},
		{
			name:    "zero version",
			yaml:    "version: 0\n",
			wantErr: "unsupported version",
		},
		{
			name: "bad severity",
			yaml: `
version: 1
rules:
  T001: ignore
`,
			wantErr: "invalid severity",
		},
		{
			name:    "invalid yaml syntax",
			yaml:    "version: [unclosed",
			wantErr: "parse policy",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Parse([]byte(tc.yaml))
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, p)
			}
		})
	}
}

func TestPathClassification(t *testing.T) {
	p := Default()

	tests := []struct {
		path       string
		isTest     bool
		isConfig   bool
		isSnapshot bool
	}{
		{path: "src/cart.ts"},
		{path: "src/cart.test.ts", isTest: true},
		{path: "cart.test.ts", isTest: true},
		{path: "src/cart/cart.spec.tsx", isTest: true},
		{path: "src/__tests__/cart.ts", isTest: true},
		{path: "e2e/checkout.ts", isTest: true},
		{path: "src/__snapshots__/cart.test.ts.snap", isTest: true, isSnapshot: true},
		{path: "src/__snapshots__/custom.txt", isTest: true, isSnapshot: true},
		{path: "src/foo.snap", isSnapshot: true},
		{path: "vitest.config.ts", isConfig: true},
		{path: "packages/web/jest.config.js", isConfig: true},
		{path: "tsconfig.json", isConfig: true},
		{path: "apps/api/tsconfig.build.json", isConfig: true},
		{path: ".eslintrc.json", isConfig: true},
		{path: "eslint.config.mjs", isConfig: true},
		{path: ".github/workflows/ci.yml", isConfig: true},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			if got := p.IsTest(tc.path); got != tc.isTest {
				t.Errorf("IsTest(%q) = %v, want %v", tc.path, got, tc.isTest)
			}
			if got := p.IsConfig(tc.path); got != tc.isConfig {
				t.Errorf("IsConfig(%q) = %v, want %v", tc.path, got, tc.isConfig)
			}
			if got := p.IsSnapshot(tc.path); got != tc.isSnapshot {
				t.Errorf("IsSnapshot(%q) = %v, want %v", tc.path, got, tc.isSnapshot)
			}
		})
	}
}
