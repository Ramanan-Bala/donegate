package rules

import (
	"fmt"
	"regexp"

	"github.com/Ramanan-Bala/donegate/internal/diff"
)

func init() {
	Register(t005{})
}

var (
	t005WeakMatcherPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\btoBeDefined\s*\(\s*\)`),
		regexp.MustCompile(`\btoBeTruthy\s*\(\s*\)`),
		regexp.MustCompile(`\btoBeFalsy\s*\(\s*\)`),
		regexp.MustCompile(`\bnot\.toBeNull\s*\(\s*\)`),
		regexp.MustCompile(`\bnot\.toBeUndefined\s*\(\s*\)`),
		regexp.MustCompile(`\bexpect\.anything\s*\(\s*\)`),
	}
	t005StrictMatcherPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\btoBe\s*\(`),
		regexp.MustCompile(`\btoEqual\s*\(`),
		regexp.MustCompile(`\btoStrictEqual\s*\(`),
		regexp.MustCompile(`\btoMatchObject\s*\(`),
		regexp.MustCompile(`\btoHaveBeenCalledWith\s*\(`),
		regexp.MustCompile(`\btoThrow\s*\(\s*[^)\s]`),
	}
	t005ExpectRe = regexp.MustCompile(`\bexpect\s*\(`)
)

const t005Hint = "restore the original assertion and fix the source under test instead"

type t005 struct{}

func (t005) ID() string                { return "T005" }
func (t005) Level() int                { return 1 }
func (t005) DefaultSeverity() Severity { return Block }

func (t005) Check(c *Context) []Finding {
	if c == nil || c.Diff == nil || c.Policy == nil {
		return nil
	}
	var findings []Finding
	for _, f := range c.Diff.Files {
		if f.Status != diff.Modified || f.Binary {
			continue
		}
		if !isTestNonSnapshot(c.Policy, f.Path) {
			continue
		}

		hasStrictRemoved := false
		removedExpects := 0
		removedCases, addedCases := 0, 0
		for _, line := range f.Removed {
			removedCases += len(t002TestCaseRe.FindAllString(line.Text, -1))
			if !hasStrictRemoved && matchesAnyPattern(t005StrictMatcherPatterns, line.Text) {
				hasStrictRemoved = true
			}
			removedExpects += len(t005ExpectRe.FindAllString(line.Text, -1))
		}

		addedExpects := 0
		for _, line := range f.Added {
			addedCases += len(t002TestCaseRe.FindAllString(line.Text, -1))
			addedExpects += len(t005ExpectRe.FindAllString(line.Text, -1))
			if !hasStrictRemoved {
				continue
			}
			for _, re := range t005WeakMatcherPatterns {
				if match := re.FindString(line.Text); match != "" {
					findings = append(findings, Finding{
						RuleID:   "T005",
						Severity: Block,
						Path:     f.Path,
						Line:     line.No,
						Message:  fmt.Sprintf("weakened assertion (%s) in %s:%d", match, f.Path, line.No),
						Hint:     t005Hint,
					})
					break
				}
			}
		}

		// A deleted test case takes its expects with it; T002 reports that, so
		// only count missing expects when every test case survived.
		if removedExpects > addedExpects && removedCases <= addedCases {
			fewer := removedExpects - addedExpects
			findings = append(findings, Finding{
				RuleID:   "T005",
				Severity: Block,
				Path:     f.Path,
				Message:  fmt.Sprintf("%s has %d fewer expect() calls", f.Path, fewer),
				Hint:     t005Hint,
			})
		}
	}
	return findings
}

func matchesAnyPattern(patterns []*regexp.Regexp, text string) bool {
	for _, re := range patterns {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}
