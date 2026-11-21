package rules

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Ramanan-Bala/donegate/internal/policy"
	"github.com/Ramanan-Bala/donegate/internal/runner"
)

const maxR001FailuresPerRun = 5

func init() {
	Register(r001{})
}

type r001 struct{}

func (r001) ID() string                { return "R001" }
func (r001) Level() int                { return 2 }
func (r001) DefaultSeverity() Severity { return Block }

func (r001) Check(c *Context) []Finding {
	if c == nil || len(c.Runs) == 0 {
		return nil
	}

	var findings []Finding
	for _, run := range c.Runs {
		if run.Kind != runner.Test || run.OK() {
			continue
		}

		proj := projectLabel(run, c.Policy)
		relDir := projectRelDir(run, c.Policy)
		hint := reproduceHint(run.Command, relDir)

		if run.TimedOut {
			d := run.Duration
			if d >= time.Millisecond {
				d = d.Round(time.Millisecond)
			}
			findings = append(findings, Finding{
				RuleID:   "R001",
				Severity: Block,
				Path:     proj,
				Message:  fmt.Sprintf("tests timed out after %s in project %s", d, proj),
				Hint:     hint,
			})
			continue
		}

		if len(run.Failures) > 0 {
			limit := len(run.Failures)
			overflow := 0
			if limit > maxR001FailuresPerRun {
				overflow = limit - maxR001FailuresPerRun
				limit = maxR001FailuresPerRun
			}
			for _, tc := range run.Failures[:limit] {
				p := tc.File
				if p == "" {
					p = proj
				}
				firstLine := firstNonEmptyLine(tc.Message)
				msg := fmt.Sprintf("%q fails in a clean run", tc.Name)
				if firstLine != "" {
					msg = fmt.Sprintf("%q fails in a clean run: %s", tc.Name, firstLine)
				}
				findings = append(findings, Finding{
					RuleID:   "R001",
					Severity: Block,
					Path:     p,
					Message:  msg,
					Hint:     hint,
				})
			}
			if overflow > 0 {
				findings = append(findings, Finding{
					RuleID:   "R001",
					Severity: Block,
					Path:     proj,
					Message:  fmt.Sprintf("... and %d more failing tests", overflow),
					Hint:     hint,
				})
			}
			continue
		}

		tail := lastOutputLines(run.Output, 10)
		msg := fmt.Sprintf("tests failed (exit %d) in project %s", run.ExitCode, proj)
		if tail != "" {
			msg = fmt.Sprintf("tests failed (exit %d) in project %s: %s", run.ExitCode, proj, tail)
		}
		findings = append(findings, Finding{
			RuleID:   "R001",
			Severity: Block,
			Path:     proj,
			Message:  msg,
			Hint:     hint,
		})
	}
	return findings
}

func projectLabel(run runner.Result, pol *policy.Policy) string {
	if name := strings.TrimSpace(run.Project); name != "" {
		return name
	}
	return projectRelDir(run, pol)
}

func projectRelDir(run runner.Result, pol *policy.Policy) string {
	if pol != nil {
		for _, p := range pol.Projects {
			root := cleanProjectRoot(p.Root)
			if p.Name != "" && p.Name == run.Project {
				return root
			}
			if p.Name == "" && (root == run.Project || (root == "." && run.Project == "")) {
				return root
			}
		}
	}
	if dir := strings.TrimSpace(run.Dir); dir != "" && !filepath.IsAbs(dir) {
		return cleanProjectRoot(dir)
	}
	return "."
}

func cleanProjectRoot(root string) string {
	s := filepath.ToSlash(strings.TrimSpace(root))
	s = strings.TrimPrefix(s, "./")
	s = strings.TrimPrefix(s, "/")
	if s == "" {
		return "."
	}
	cleaned := path.Clean(s)
	if cleaned == "" || cleaned == "." || cleaned == "/" {
		return "."
	}
	return cleaned
}

func reproduceHint(command, relDir string) string {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return ""
	}
	if relDir == "" {
		relDir = "."
	}
	return fmt.Sprintf("Run `%s` in %s to reproduce.", cmd, relDir)
}

func firstNonEmptyLine(s string) string {
	norm := strings.ReplaceAll(s, "\r\n", "\n")
	for _, line := range strings.Split(norm, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func lastOutputLines(s string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	norm := strings.ReplaceAll(s, "\r\n", "\n")
	var lines []string
	for _, line := range strings.Split(norm, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, " | ")
}
