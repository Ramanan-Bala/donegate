package evidence

import (
	"fmt"
	"strings"

	"github.com/Ramanan-Bala/donegate/internal/rules"
)

// Markdown renders e as a PR-comment style Markdown report.
func Markdown(e *Evidence) string {
	if e == nil {
		return ""
	}

	var b strings.Builder

	if e.Verdict == "PASS" {
		b.WriteString("## donegate: PASS ✅\n\n")
	} else {
		b.WriteString("## donegate: FAIL ❌\n\n")
	}

	var meta []string
	if e.Tier != "" {
		meta = append(meta, fmt.Sprintf("Tier: `%s`", e.Tier))
	}
	if e.Harness != "" {
		meta = append(meta, fmt.Sprintf("Harness: `%s`", e.Harness))
	}
	if e.Model != "" {
		meta = append(meta, fmt.Sprintf("Model: `%s`", e.Model))
	}
	if e.Base != "" || e.Head != "" {
		meta = append(meta, fmt.Sprintf("`%s..%s`", shortPrefix(e.Base, 7), shortPrefix(e.Head, 7)))
	}
	meta = append(meta, fmt.Sprintf("blocked %d time(s)", e.Blocks))
	b.WriteString(strings.Join(meta, " · "))
	b.WriteString("\n\n")

	b.WriteString("| Check | Status | Detail |\n")
	b.WriteString("| --- | --- | --- |\n")
	for _, c := range e.Checks {
		detail := escapeTableCell(c.Detail)
		if detail == "" {
			fmt.Fprintf(&b, "| %s | %s | |\n", escapeTableCell(c.ID), escapeTableCell(c.Status))
		} else {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", escapeTableCell(c.ID), escapeTableCell(c.Status), detail)
		}
	}

	var warnings, failures []rules.Finding
	for _, c := range e.Checks {
		for _, f := range c.Findings {
			if f.RuleID == "" {
				f.RuleID = c.ID
			}
			switch f.Severity {
			case rules.Warn:
				warnings = append(warnings, f)
			case rules.Block:
				failures = append(failures, f)
			}
		}
	}

	if len(warnings) > 0 {
		b.WriteString("\n### Warnings\n\n")
		for _, f := range warnings {
			writeFindingBullet(&b, f)
		}
	}

	if len(failures) > 0 {
		b.WriteString("\n### Failures\n\n")
		for _, f := range failures {
			writeFindingBullet(&b, f)
		}
	}

	fmt.Fprintf(&b, "\nReport SHA-256: `%s`\n", shortPrefix(e.ReportSHA256, 12))

	return b.String()
}

func writeFindingBullet(b *strings.Builder, f rules.Finding) {
	loc := formatLocation(f.Path, f.Line)
	if loc != "" {
		fmt.Fprintf(b, "- `%s` %s %s\n", f.RuleID, loc, f.Message)
		return
	}
	fmt.Fprintf(b, "- `%s` %s\n", f.RuleID, f.Message)
}

func formatLocation(path string, line int) string {
	if path == "" {
		return ""
	}
	if line > 0 {
		return fmt.Sprintf("%s:%d", path, line)
	}
	return path
}

func escapeTableCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", `\|`)
	return s
}

func shortPrefix(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
