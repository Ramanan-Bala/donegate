package rules

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/Ramanan-Bala/donegate/internal/diff"
)

func init() {
	Register(t003{})
}

var t003Patterns = []*regexp.Regexp{
	regexp.MustCompile(`\b(?:it|test|describe)\.(?:only|skip|todo)\b`),
	regexp.MustCompile(`\btest\.fails\b`),
	regexp.MustCompile(`\bit\.fails\b`),
	regexp.MustCompile(`\b(?:xit|xtest|xdescribe|fit|fdescribe)\s*\(`),
}

var t003GoPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b\w+\.Skip(?:Now|f)?\(`),
	regexp.MustCompile(`^\s*//\s*(?:go:build|\+build)\s+ignore\b`),
}

var t003PyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`@\s*pytest\.mark\.(?:skip|skipif|xfail)\b`),
	regexp.MustCompile(`\bpytest\.skip\s*\(`),
	regexp.MustCompile(`@\s*unittest\.(?:skip|skipIf|skipUnless|expectedFailure)\b`),
}

var t003DotnetPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\[\s*<?\s*(?:[A-Za-z_]\w*\.)*(?:Ignore|Explicit)(?:Attribute)?\b`),
	regexp.MustCompile(`\[(?:[^\]"]|"[^"]*")*[;,]\s*(?:[A-Za-z_]\w*\.)*(?:Ignore|Explicit)(?:Attribute)?\b`),
	regexp.MustCompile(`\[(?:[^\]"]|"[^"]*")*\b(?:Fact|Theory)\s*\((?:[^)"]|"[^"]*")*\bSkip\s*=`),
}

func t003PatternsForPath(p string) []*regexp.Regexp {
	switch strings.ToLower(path.Ext(normalizeRulePath(p))) {
	case ".go":
		return t003GoPatterns
	case ".py":
		return t003PyPatterns
	case ".cs", ".fs", ".vb":
		return t003DotnetPatterns
	default:
		return t003Patterns
	}
}

type t003 struct{}

func (t003) ID() string                { return "T003" }
func (t003) Level() int                { return 1 }
func (t003) DefaultSeverity() Severity { return Block }

func (t003) Check(c *Context) []Finding {
	if c == nil || c.Diff == nil || c.Policy == nil {
		return nil
	}
	var findings []Finding
	for _, f := range c.Diff.Files {
		if f.Status == diff.Deleted || f.Binary {
			continue
		}
		if !isTestNonSnapshot(c.Policy, f.Path) {
			continue
		}
		// A skip that only moved or was re-indented is not new.
		removed := make(map[string]bool, len(f.Removed))
		for _, line := range f.Removed {
			removed[strings.TrimSpace(line.Text)] = true
		}
		patterns := t003PatternsForPath(f.Path)
		for _, line := range f.Added {
			if removed[strings.TrimSpace(line.Text)] {
				continue
			}
			for _, re := range patterns {
				if match := strings.TrimSpace(re.FindString(line.Text)); match != "" {
					findings = append(findings, Finding{
						RuleID:   "T003",
						Severity: Block,
						Path:     f.Path,
						Line:     line.No,
						Message:  fmt.Sprintf("focused or skipped test (%s) added in %s:%d", match, f.Path, line.No),
						Hint:     "remove the focus/skip modifier so all test cases execute",
					})
					break
				}
			}
		}
	}
	return findings
}
