package rules

import (
	"fmt"
	"regexp"

	"github.com/Ramanan-Bala/donegate/internal/diff"
)

func init() {
	Register(t002{})
}

var t002TestCaseRe = regexp.MustCompile(`\b(?:it|test)(?:\.[A-Za-z]+)*\s*[(` + "`" + `]`)

type t002 struct{}

func (t002) ID() string                { return "T002" }
func (t002) Level() int                { return 1 }
func (t002) DefaultSeverity() Severity { return Block }

func (t002) Check(c *Context) []Finding {
	if c == nil || c.Diff == nil || c.Policy == nil {
		return nil
	}
	var findings []Finding
	for _, f := range c.Diff.Files {
		switch f.Status {
		case diff.Deleted:
			if isTestNonSnapshot(c.Policy, f.Path) {
				findings = append(findings, Finding{
					RuleID:   "T002",
					Severity: Block,
					Path:     f.Path,
					Message:  fmt.Sprintf("%s was deleted. Tests are read-only", f.Path),
					Hint:     "restore the test file; fix the code instead",
				})
			}
		case diff.Modified, diff.Renamed:
			if f.Binary {
				continue
			}
			isTestNew := isTestNonSnapshot(c.Policy, f.Path)
			isTestOld := f.Status == diff.Renamed && f.OldPath != "" && isTestNonSnapshot(c.Policy, f.OldPath)
			if !isTestNew && !isTestOld {
				continue
			}
			basePath := f.Path
			if f.Status == diff.Renamed && f.OldPath != "" {
				basePath = f.OldPath
			}
			if c.Diff.Base == "" {
				continue
			}
			baseData, err := c.Diff.ReadBase(basePath)
			if err != nil {
				continue
			}
			headData, err := c.Diff.ReadHead(f.Path)
			if err != nil {
				continue
			}
			baseCount := len(t002TestCaseRe.FindAll(baseData, -1))
			headCount := len(t002TestCaseRe.FindAll(headData, -1))
			if headCount < baseCount {
				removed := baseCount - headCount
				findings = append(findings, Finding{
					RuleID:   "T002",
					Severity: Block,
					Path:     f.Path,
					Message:  fmt.Sprintf("%d test case(s) removed from %s", removed, f.Path),
					Hint:     "restore the removed test case(s); fix the source under test instead",
				})
			}
		}
	}
	return findings
}
