package policy

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

// Policy configures protected paths, gate behaviour, rule severities, and optional L4 judge settings.
type Policy struct {
	Version  int               `yaml:"version"`
	Projects []Project         `yaml:"projects"`
	Protect  Protect           `yaml:"protect"`
	Gate     Gate              `yaml:"gate"`
	Rules    map[string]string `yaml:"rules"`
	Judge    Judge             `yaml:"judge"`
}

// Project describes one package or workspace in a repository.
type Project struct {
	Name      string `yaml:"name"`
	Root      string `yaml:"root"`
	Test      string `yaml:"test"`
	Typecheck string `yaml:"typecheck"`
	Lint      string `yaml:"lint"`
}

// Protect defines read-only test and config globs and whether new test files are allowed.
type Protect struct {
	Tests         []string `yaml:"tests"`
	Config        []string `yaml:"config"`
	AllowNewTests bool     `yaml:"allow_new_tests"`
}

// Gate configures verification tier, retry cap, and execution timeout.
type Gate struct {
	Tier           string `yaml:"tier"`
	MaxBlocks      int    `yaml:"max_blocks"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

// Judge configures the optional L4 model check.
type Judge struct {
	Enabled bool   `yaml:"enabled"`
	Model   string `yaml:"model"`
	BaseURL string `yaml:"base_url"`
}

// Default returns the default donegate policy.
func Default() *Policy {
	return &Policy{
		Version: 1,
		Protect: Protect{
			Tests: []string{
				"**/*.test.*",
				"**/*.spec.*",
				"**/__tests__/**",
				"**/__snapshots__/**",
				"e2e/**",
			},
			Config: []string{
				"**/jest.config.*",
				"**/vitest.config.*",
				"**/tsconfig*.json",
				"**/.eslintrc*",
				"**/eslint.config.*",
				".github/workflows/**",
			},
			AllowNewTests: true,
		},
		Gate: Gate{
			Tier:           "standard",
			MaxBlocks:      5,
			TimeoutSeconds: 300,
		},
		Rules: map[string]string{},
	}
}

// Parse unmarshals YAML policy bytes on top of Default(), preserving defaults for omitted fields.
func Parse(data []byte) (*Policy, error) {
	p := Default()
	if err := yaml.Unmarshal(data, p); err != nil {
		return nil, fmt.Errorf("parse policy: %w", err)
	}
	if p.Version != 1 {
		return nil, fmt.Errorf("parse policy: unsupported version %d (expected 1)", p.Version)
	}
	if p.Rules == nil {
		p.Rules = map[string]string{}
	}
	for id, sev := range p.Rules {
		switch sev {
		case "off", "warn", "block":
		default:
			return nil, fmt.Errorf("parse policy: rule %s has invalid severity %q (expected off, warn, or block)", id, sev)
		}
	}
	return p, nil
}

// IsTest reports whether path matches any pattern in Protect.Tests.
func (p *Policy) IsTest(path string) bool {
	return matchAny(p.Protect.Tests, path)
}

// IsConfig reports whether path matches any pattern in Protect.Config.
func (p *Policy) IsConfig(path string) bool {
	return matchAny(p.Protect.Config, path)
}

// IsSnapshot reports whether path is a snapshot file.
func (p *Policy) IsSnapshot(path string) bool {
	norm := normalizePath(path)
	if norm == "" {
		return false
	}
	if strings.HasSuffix(norm, ".snap") {
		return true
	}
	ok, err := doublestar.Match("**/__snapshots__/**", norm)
	return err == nil && ok
}

func matchAny(patterns []string, path string) bool {
	norm := normalizePath(path)
	if norm == "" {
		return false
	}
	for _, pat := range patterns {
		ok, err := doublestar.Match(pat, norm)
		if err == nil && ok {
			return true
		}
	}
	return false
}

func normalizePath(p string) string {
	s := filepath.ToSlash(p)
	s = strings.TrimPrefix(s, "./")
	s = strings.TrimPrefix(s, "/")
	return s
}
