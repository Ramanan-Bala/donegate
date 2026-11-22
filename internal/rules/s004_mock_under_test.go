package rules

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/Ramanan-Bala/donegate/internal/diff"
)

func init() {
	Register(s004{})
}

var s004MockRe = regexp.MustCompile(`\b(?:jest|vi)\.(?:mock|doMock)\s*\(\s*(?:'([^']+)'|"([^"]+)"|` + "`([^`]+)`" + `)`)

type s004 struct{}

func (s004) ID() string                { return "S004" }
func (s004) Level() int                { return 1 }
func (s004) DefaultSeverity() Severity { return Warn }

func (s004) Check(c *Context) []Finding {
	if c == nil || c.Diff == nil || c.Policy == nil {
		return nil
	}

	changedSources := map[string]string{}
	for _, f := range c.Diff.Files {
		if f.Binary {
			continue
		}
		if f.Status != diff.Modified && f.Status != diff.Added {
			continue
		}
		if !isSourceFile(c.Policy, f.Path) {
			continue
		}
		key := s004StripModulePath(f.Path)
		if key != "" {
			changedSources[key] = normalizeRulePath(f.Path)
		}
	}
	if len(changedSources) == 0 {
		return nil
	}

	var findings []Finding
	for _, f := range c.Diff.Files {
		if f.Status == diff.Deleted || f.Binary || len(f.Added) == 0 {
			continue
		}
		if !isTestNonSnapshot(c.Policy, f.Path) {
			continue
		}
		testDir := path.Dir(normalizeRulePath(f.Path))
		for _, line := range f.Added {
			for _, m := range s004MockRe.FindAllStringSubmatch(line.Text, -1) {
				spec := m[1] + m[2] + m[3]
				resolved, ok := s004ResolveModule(testDir, spec)
				if !ok {
					continue
				}
				if srcPath, hit := changedSources[resolved]; hit {
					findings = append(findings, Finding{
						RuleID:   "S004",
						Severity: Warn,
						Path:     f.Path,
						Line:     line.No,
						Message:  fmt.Sprintf("test mocks changed source module %q (%s) in %s:%d", spec, srcPath, f.Path, line.No),
						Hint:     "remove the mock of the module under test so tests exercise the real implementation",
					})
					break
				}
			}
		}
	}
	return findings
}

func s004ResolveModule(testDir, spec string) (string, bool) {
	if !strings.HasPrefix(spec, ".") && !strings.HasPrefix(spec, "/") {
		return "", false
	}
	var joined string
	if strings.HasPrefix(spec, "/") {
		joined = spec
	} else {
		joined = path.Clean(path.Join(testDir, spec))
		if joined == ".." || strings.HasPrefix(joined, "../") {
			return "", false
		}
	}
	stripped := s004StripModulePath(joined)
	if stripped == "" {
		return "", false
	}
	return stripped, true
}

func s004StripModulePath(p string) string {
	s := normalizeRulePath(p)
	if s == "" {
		return ""
	}
	s = path.Clean(s)
	ext := strings.ToLower(path.Ext(s))
	if sourceFileExts[ext] {
		s = s[:len(s)-len(ext)]
	}
	s = strings.TrimSuffix(s, "/index")
	return s
}
