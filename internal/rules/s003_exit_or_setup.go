package rules

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/Ramanan-Bala/donegate/internal/diff"
)

func init() {
	Register(s003{})
}

var (
	s003ProcessExitRe = regexp.MustCompile(`\bprocess\.exit\s*\(`)
	s003SetupKeyRe    = regexp.MustCompile(`\b(?:setupFiles|setupFilesAfterEach|setupFilesAfterEnv|globalSetup)\b`)
	s003QuotedStrRe   = regexp.MustCompile(`['"]([^'"]+)['"]`)
)

type s003 struct{}

func (s003) ID() string                { return "S003" }
func (s003) Level() int                { return 1 }
func (s003) DefaultSeverity() Severity { return Block }

func (s003) Check(c *Context) []Finding {
	if c == nil || c.Diff == nil || c.Policy == nil {
		return nil
	}

	var configSetupPaths map[string]struct{}
	configLoaded := false

	var findings []Finding
	for _, f := range c.Diff.Files {
		if f.Status != diff.Deleted && !f.Binary && isSourceFile(c.Policy, f.Path) {
			for _, line := range f.Added {
				if s003ProcessExitRe.MatchString(line.Text) {
					findings = append(findings, Finding{
						RuleID:   "S003",
						Severity: Block,
						Path:     f.Path,
						Line:     line.No,
						Message:  fmt.Sprintf("process.exit() added in %s:%d", f.Path, line.No),
						Hint:     "remove process.exit() from source code; handle errors instead of terminating the test runner",
					})
				}
			}
		}

		if f.Status == diff.Modified || f.Status == diff.Renamed || f.Status == diff.Deleted {
			newNorm := normalizeRulePath(f.Path)
			oldNorm := ""
			if f.Status == diff.Renamed && f.OldPath != "" {
				oldNorm = normalizeRulePath(f.OldPath)
			}

			isSetup := s003IsStaticSetupPath(newNorm) || (oldNorm != "" && s003IsStaticSetupPath(oldNorm))
			if !isSetup && c.Diff.RepoDir != "" && c.Diff.Base != "" {
				if !configLoaded {
					configSetupPaths = s003LoadConfigSetupPaths(c.Diff)
					configLoaded = true
				}
				_, isSetupNew := configSetupPaths[newNorm]
				_, isSetupOld := configSetupPaths[oldNorm]
				isSetup = isSetupNew || (oldNorm != "" && isSetupOld)
			}
			if isSetup {
				findings = append(findings, Finding{
					RuleID:   "S003",
					Severity: Block,
					Path:     f.Path,
					Message:  fmt.Sprintf("test setup file %s was %s", f.Path, f.Status),
					Hint:     "revert changes to test setup files instead of altering the global test environment",
				})
			}
		}
	}
	return findings
}

func s003IsStaticSetupPath(normPath string) bool {
	if normPath == "" {
		return false
	}
	base := path.Base(normPath)
	if strings.HasPrefix(base, "setupTests.") ||
		strings.HasPrefix(base, "jest.setup.") ||
		strings.HasPrefix(base, "vitest.setup.") ||
		strings.HasPrefix(base, "globalSetup.") {
		return true
	}
	switch base {
	case "setup.js", "setup.ts", "setup.mjs", "setup.cjs":
		dir := path.Dir(normPath)
		for _, seg := range strings.Split(dir, "/") {
			if seg == "test" || seg == "tests" || seg == "__tests__" {
				return true
			}
		}
	}
	return false
}

func s003LoadConfigSetupPaths(d *diff.Diff) map[string]struct{} {
	out := map[string]struct{}{}
	paths, err := listBaseTree(d.RepoDir, d.Base)
	if err != nil {
		return out
	}
	for _, cfgPath := range paths {
		base := path.Base(cfgPath)
		if !strings.HasPrefix(base, "jest.config.") && !strings.HasPrefix(base, "vitest.config.") {
			continue
		}
		data, err := d.ReadBase(cfgPath)
		if err != nil {
			continue
		}
		cfgDir := path.Dir(normalizeRulePath(cfgPath))
		inArray := false
		for _, line := range strings.Split(string(data), "\n") {
			matchedKey := s003SetupKeyRe.MatchString(line)
			if !matchedKey && !inArray {
				continue
			}
			searchArea := line
			if matchedKey {
				loc := s003SetupKeyRe.FindStringIndex(line)
				searchArea = line[loc[1]:]
			}
			for _, m := range s003QuotedStrRe.FindAllStringSubmatch(searchArea, -1) {
				ref := strings.TrimPrefix(m[1], "<rootDir>/")
				if ref == "" {
					continue
				}
				resolved := normalizeRulePath(path.Clean(path.Join(cfgDir, ref)))
				if resolved != "" && resolved != "." {
					out[resolved] = struct{}{}
				}
			}
			if matchedKey && strings.Contains(searchArea, "[") && !strings.Contains(searchArea, "]") {
				inArray = true
			} else if inArray && strings.Contains(line, "]") {
				inArray = false
			}
		}
	}
	return out
}
