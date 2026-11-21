package rules

import (
	"fmt"
	"regexp"

	"github.com/Ramanan-Bala/donegate/internal/diff"
)

func init() {
	Register(s002{})
}

var s002Patterns = []*regexp.Regexp{
	regexp.MustCompile(`NODE_ENV\s*[!=]==?\s*['"]test['"]`),
	regexp.MustCompile(`['"]test['"]\s*[!=]==?\s*process\.env\.NODE_ENV`),
	regexp.MustCompile(`\bJEST_WORKER_ID\b`),
	regexp.MustCompile(`process\.env\.VITEST\b`),
	regexp.MustCompile(`import\.meta\.env\.VITEST\b`),
	regexp.MustCompile(`typeof\s+jest\s*!==?\s*['"]undefined['"]`),
}

type s002 struct{}

func (s002) ID() string                { return "S002" }
func (s002) Level() int                { return 1 }
func (s002) DefaultSeverity() Severity { return Block }

func (s002) Check(c *Context) []Finding {
	if c == nil || c.Diff == nil || c.Policy == nil {
		return nil
	}
	var findings []Finding
	for _, f := range c.Diff.Files {
		if f.Status == diff.Deleted || f.Binary {
			continue
		}
		if !isSourceFile(c.Policy, f.Path) {
			continue
		}
		for _, line := range f.Added {
			for _, re := range s002Patterns {
				if re.MatchString(line.Text) {
					findings = append(findings, Finding{
						RuleID:   "S002",
						Severity: Block,
						Path:     f.Path,
						Line:     line.No,
						Message:  fmt.Sprintf("test environment check detected in %s:%d", f.Path, line.No),
						Hint:     "remove test-environment branching so production and test code share the same behaviour",
					})
					break
				}
			}
		}
	}
	return findings
}
