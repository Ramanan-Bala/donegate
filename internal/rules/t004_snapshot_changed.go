package rules

import (
	"fmt"
	"regexp"

	"github.com/Ramanan-Bala/donegate/internal/diff"
)

func init() {
	Register(t004{})
}

var t004InlineSnapRe = regexp.MustCompile(`\btoMatchInlineSnapshot\s*\(`)

type t004 struct{}

func (t004) ID() string                { return "T004" }
func (t004) Level() int                { return 1 }
func (t004) DefaultSeverity() Severity { return Block }

func (t004) Check(c *Context) []Finding {
	if c == nil || c.Diff == nil || c.Policy == nil {
		return nil
	}
	var findings []Finding
	for _, f := range c.Diff.Files {
		switch f.Status {
		case diff.Modified, diff.Renamed, diff.Deleted:
			isSnap := c.Policy.IsSnapshot(f.Path) || (f.Status == diff.Renamed && f.OldPath != "" && c.Policy.IsSnapshot(f.OldPath))
			if isSnap {
				findings = append(findings, Finding{
					RuleID:   "T004",
					Severity: Block,
					Path:     f.Path,
					Message:  fmt.Sprintf("snapshot %s was %s", f.Path, f.Status),
					Hint:     "snapshots are read-only; fix the output instead of running -u",
				})
				continue
			}
			if (f.Status == diff.Modified || f.Status == diff.Renamed) && !f.Binary && isTestNonSnapshot(c.Policy, f.Path) {
				for _, line := range f.Added {
					if t004InlineSnapRe.MatchString(line.Text) {
						findings = append(findings, Finding{
							RuleID:   "T004",
							Severity: Block,
							Path:     f.Path,
							Line:     line.No,
							Message:  fmt.Sprintf("inline snapshot modified in %s:%d", f.Path, line.No),
							Hint:     "snapshots are read-only; fix the output instead of running -u",
						})
					}
				}
			}
		case diff.Added:
			if c.Policy.IsSnapshot(f.Path) && !c.Policy.Protect.AllowNewTests {
				findings = append(findings, Finding{
					RuleID:   "T004",
					Severity: Block,
					Path:     f.Path,
					Message:  fmt.Sprintf("snapshot %s was added while allow_new_tests is false", f.Path),
					Hint:     "snapshots are read-only; fix the output instead of running -u",
				})
			}
		}
	}
	return findings
}
