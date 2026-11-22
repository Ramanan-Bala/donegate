package rules

import (
	"fmt"
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
		for _, line := range f.Added {
			if removed[strings.TrimSpace(line.Text)] {
				continue
			}
			for _, re := range t003Patterns {
				if match := re.FindString(line.Text); match != "" {
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
