package rules

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/Ramanan-Bala/donegate/internal/diff"
)

func init() {
	Register(c002{})
}

type c002Pattern struct {
	name string
	re   *regexp.Regexp
}

var c002Patterns = []c002Pattern{
	{name: "@ts-ignore", re: regexp.MustCompile(`@ts-ignore\b`)},
	{name: "@ts-expect-error", re: regexp.MustCompile(`@ts-expect-error\b`)},
	{name: "@ts-nocheck", re: regexp.MustCompile(`@ts-nocheck\b`)},
	{name: "eslint-disable", re: regexp.MustCompile(`\beslint-disable(?:-next-line|-line)?\b`)},
	{name: "as any", re: regexp.MustCompile(`\bas\s+any\b`)},
}

type c002 struct{}

func (c002) ID() string                { return "C002" }
func (c002) Level() int                { return 1 }
func (c002) DefaultSeverity() Severity { return Warn }

func (c002) Check(c *Context) []Finding {
	if c == nil || c.Diff == nil || c.Policy == nil {
		return nil
	}
	var findings []Finding
	for _, f := range c.Diff.Files {
		if f.Status == diff.Deleted || f.Binary || len(f.Added) == 0 {
			continue
		}
		ext := strings.ToLower(path.Ext(normalizeRulePath(f.Path)))
		if !sourceFileExts[ext] {
			continue
		}
		if !isSourceFile(c.Policy, f.Path) && !isTestNonSnapshot(c.Policy, f.Path) {
			continue
		}

		removedCounts := make([]int, len(c002Patterns))
		for _, line := range f.Removed {
			for i, pat := range c002Patterns {
				removedCounts[i] += len(pat.re.FindAllString(line.Text, -1))
			}
		}

		addedCounts := make([]int, len(c002Patterns))
		for _, line := range f.Added {
			for i, pat := range c002Patterns {
				addedCounts[i] += len(pat.re.FindAllString(line.Text, -1))
			}
		}

		isNew := make([]bool, len(c002Patterns))
		anyNew := false
		for i := range c002Patterns {
			if addedCounts[i] > removedCounts[i] {
				isNew[i] = true
				anyNew = true
			}
		}
		if !anyNew {
			continue
		}

		for _, line := range f.Added {
			for i, pat := range c002Patterns {
				if isNew[i] && pat.re.MatchString(line.Text) {
					findings = append(findings, Finding{
						RuleID:   "C002",
						Severity: Warn,
						Path:     f.Path,
						Line:     line.No,
						Message:  fmt.Sprintf("new %s suppression in %s:%d", pat.name, f.Path, line.No),
						Hint:     "remove the suppression and fix the underlying type or lint error instead",
					})
					break
				}
			}
		}
	}
	return findings
}
