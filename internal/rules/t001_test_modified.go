package rules

import (
	"fmt"

	"github.com/Ramanan-Bala/donegate/internal/diff"
)

func init() {
	Register(t001{})
}

type t001 struct{}

func (t001) ID() string                { return "T001" }
func (t001) Level() int                { return 1 }
func (t001) DefaultSeverity() Severity { return Block }

func (t001) Check(c *Context) []Finding {
	if c == nil || c.Diff == nil || c.Policy == nil {
		return nil
	}
	var findings []Finding
	for _, f := range c.Diff.Files {
		switch f.Status {
		case diff.Modified, diff.Renamed:
			isTestNew := c.Policy.IsTest(f.Path) && !c.Policy.IsSnapshot(f.Path)
			isTestOld := f.Status == diff.Renamed && f.OldPath != "" && c.Policy.IsTest(f.OldPath) && !c.Policy.IsSnapshot(f.OldPath)
			if isTestNew || isTestOld {
				findings = append(findings, Finding{
					RuleID:   "T001",
					Severity: Block,
					Path:     f.Path,
					Message:  fmt.Sprintf("%s was modified. Tests are read-only", f.Path),
					Hint:     "revert it and fix the source under test instead",
				})
			}
		case diff.Added:
			if !c.Policy.Protect.AllowNewTests && c.Policy.IsTest(f.Path) && !c.Policy.IsSnapshot(f.Path) {
				findings = append(findings, Finding{
					RuleID:   "T001",
					Severity: Block,
					Path:     f.Path,
					Message:  fmt.Sprintf("%s was added. Tests are read-only", f.Path),
					Hint:     "remove it and fix the source under test instead",
				})
			}
		}
	}
	return findings
}
