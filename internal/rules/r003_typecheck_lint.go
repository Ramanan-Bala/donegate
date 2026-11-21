package rules

import (
	"fmt"

	"github.com/Ramanan-Bala/donegate/internal/runner"
)

func init() {
	Register(r003{})
}

type r003 struct{}

func (r003) ID() string                { return "R003" }
func (r003) Level() int                { return 2 }
func (r003) DefaultSeverity() Severity { return Block }

func (r003) Check(c *Context) []Finding {
	if c == nil || len(c.Runs) == 0 {
		return nil
	}

	var findings []Finding
	for _, run := range c.Runs {
		if (run.Kind != runner.Typecheck && run.Kind != runner.Lint) || run.OK() {
			continue
		}

		proj := projectLabel(run, c.Policy)
		relDir := projectRelDir(run, c.Policy)
		tail := lastOutputLines(run.Output, 10)
		msg := fmt.Sprintf("%s failed in project %s", run.Kind, proj)
		if tail != "" {
			msg = fmt.Sprintf("%s failed in project %s: %s", run.Kind, proj, tail)
		}
		findings = append(findings, Finding{
			RuleID:   "R003",
			Severity: Block,
			Path:     proj,
			Message:  msg,
			Hint:     reproduceHint(run.Command, relDir),
		})
	}
	return findings
}
