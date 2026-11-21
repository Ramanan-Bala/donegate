package rules

import "fmt"

func init() {
	Register(h001{})
}

type h001 struct{}

func (h001) ID() string                { return "H001" }
func (h001) Level() int                { return 1 }
func (h001) DefaultSeverity() Severity { return Block }

func (h001) Check(c *Context) []Finding {
	if c == nil || len(c.Tampered) == 0 {
		return nil
	}
	findings := make([]Finding, 0, len(c.Tampered))
	for _, t := range c.Tampered {
		msg := fmt.Sprintf("%s changed since the session started", t.Path)
		if t.Actual == "" {
			msg = fmt.Sprintf("%s was deleted since the session started", t.Path)
		}
		findings = append(findings, Finding{
			RuleID:   "H001",
			Severity: Block,
			Path:     t.Path,
			Message:  msg,
			Hint:     "restore it; tests, policy and harness config are read-only during a session",
		})
	}
	return findings
}
