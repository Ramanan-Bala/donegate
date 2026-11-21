package rules

import (
	"fmt"

	"github.com/Ramanan-Bala/donegate/internal/runner"
)

func init() {
	Register(r002{})
}

type r002 struct{}

func (r002) ID() string                { return "R002" }
func (r002) Level() int                { return 2 }
func (r002) DefaultSeverity() Severity { return Block }

func (r002) Check(c *Context) []Finding {
	if c == nil || len(c.Runs) == 0 {
		return nil
	}

	baselineByProject := make(map[string]runner.Result)
	for _, b := range c.BaselineRuns {
		if b.Kind != runner.Test || b.Format == runner.FormatExitCode {
			continue
		}
		baselineByProject[projectLabel(b, c.Policy)] = b
	}

	var findings []Finding
	for _, run := range c.Runs {
		if run.Kind != runner.Test || run.Format == runner.FormatExitCode {
			continue
		}

		proj := projectLabel(run, c.Policy)
		relDir := projectRelDir(run, c.Policy)

		if run.Total == 0 {
			hint := "check test configuration; tests must run"
			if run.Command != "" {
				hint = fmt.Sprintf("check test configuration and run `%s` in %s to verify tests are discovered", run.Command, relDir)
			}
			findings = append(findings, Finding{
				RuleID:   "R002",
				Severity: Block,
				Path:     proj,
				Message:  fmt.Sprintf("zero tests ran in project %s", proj),
				Hint:     hint,
			})
			continue
		}

		if base, ok := baselineByProject[proj]; ok && run.Total < base.Total {
			findings = append(findings, Finding{
				RuleID:   "R002",
				Severity: Block,
				Path:     proj,
				Message:  fmt.Sprintf("test count dropped from %d to %d in project %s", base.Total, run.Total, proj),
				Hint:     fmt.Sprintf("restore missing tests; baseline ran %d tests in project %s", base.Total, proj),
			})
		}
	}
	return findings
}
