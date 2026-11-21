package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxMessageRunes = 500

var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)

type jestReport struct {
	NumTotalTestSuites        *int              `json:"numTotalTestSuites"`
	NumPassedTestSuites       *int              `json:"numPassedTestSuites"`
	NumFailedTestSuites       *int              `json:"numFailedTestSuites"`
	NumPendingTestSuites      *int              `json:"numPendingTestSuites"`
	NumRuntimeErrorTestSuites *int              `json:"numRuntimeErrorTestSuites"`
	NumTotalTests             *int              `json:"numTotalTests"`
	NumPassedTests            *int              `json:"numPassedTests"`
	NumFailedTests            *int              `json:"numFailedTests"`
	NumPendingTests           *int              `json:"numPendingTests"`
	NumTodoTests              *int              `json:"numTodoTests"`
	OpenHandles               json.RawMessage   `json:"openHandles"`
	WasInterrupted            *bool             `json:"wasInterrupted"`
	Snapshot                  map[string]any    `json:"snapshot"`
	TestResults               *[]jestTestResult `json:"testResults"`
}

type jestTestResult struct {
	Name             string                `json:"name"`
	Status           string                `json:"status"`
	Message          string                `json:"message"`
	AssertionResults []jestAssertionResult `json:"assertionResults"`
}

type jestAssertionResult struct {
	AncestorTitles  []string        `json:"ancestorTitles"`
	FullName        string          `json:"fullName"`
	Title           string          `json:"title"`
	Status          string          `json:"status"`
	FailureMessages []string        `json:"failureMessages"`
	Meta            json.RawMessage `json:"meta"`
}

// ParseJestJSON parses Jest `--json` or Vitest `--reporter=json` output.
func ParseJestJSON(data []byte) (Result, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return Result{}, fmt.Errorf("parse jest json: input is not a JSON object")
	}

	var report jestReport
	if err := json.Unmarshal(trimmed, &report); err != nil {
		return Result{}, fmt.Errorf("parse jest json: %w", err)
	}
	if !looksLikeJestReport(&report) {
		return Result{}, fmt.Errorf("parse jest json: missing expected Jest/Vitest report fields")
	}

	return buildJestResult(&report), nil
}

// extractJestJSONFromOutput scans combined command output for an embedded
// Jest/Vitest JSON report (which may be surrounded by non-JSON log lines).
func extractJestJSONFromOutput(output []byte) (Result, bool) {
	if res, err := ParseJestJSON(output); err == nil {
		return res, true
	}

	if !bytes.Contains(output, []byte(`"testResults"`)) && !bytes.Contains(output, []byte(`"numTotalTests"`)) {
		return Result{}, false
	}

	// First try `{` characters at line starts, then fall back to any `{`.
	if res, ok := scanJestJSON(output, true); ok {
		return res, true
	}
	return scanJestJSON(output, false)
}

func scanJestJSON(output []byte, lineStartOnly bool) (Result, bool) {
	const maxAttempts = 2000
	attempts := 0

	for i := 0; i < len(output) && attempts < maxAttempts; i++ {
		if output[i] != '{' {
			continue
		}
		if lineStartOnly && i > 0 && output[i-1] != '\n' && output[i-1] != '\r' {
			continue
		}
		if !lineStartOnly && (i == 0 || output[i-1] == '\n' || output[i-1] == '\r') {
			continue
		}
		attempts++

		dec := json.NewDecoder(bytes.NewReader(output[i:]))
		var report jestReport
		if err := dec.Decode(&report); err != nil {
			continue
		}
		if looksLikeJestReport(&report) {
			return buildJestResult(&report), true
		}
		if off := int(dec.InputOffset()); off > 1 {
			i += off - 1
		}
	}
	return Result{}, false
}

func looksLikeJestReport(r *jestReport) bool {
	if r.TestResults != nil {
		return true
	}
	if r.NumTotalTests != nil && (r.NumPassedTests != nil || r.NumFailedTests != nil) {
		return true
	}
	return false
}

func buildJestResult(report *jestReport) Result {
	res := Result{
		Format: detectJestOrVitestFormat(report),
	}

	var countTotal, countPassed, countFailed, countSkipped int
	if report.TestResults != nil {
		for _, tr := range *report.TestResults {
			suiteFailedAssertions := 0
			for _, ar := range tr.AssertionResults {
				countTotal++
				status := normalizeJestStatus(ar.Status)
				switch status {
				case "passed":
					countPassed++
				case "failed":
					countFailed++
					suiteFailedAssertions++
					res.Failures = append(res.Failures, TestCase{
						Name:    assertionName(ar),
						File:    tr.Name,
						Status:  "failed",
						Message: firstFailureMessage(ar.FailureMessages),
					})
				case "skipped", "todo":
					countSkipped++
				}
			}

			// Handle suite-level failures (e.g. syntax/import errors before tests run).
			if strings.EqualFold(tr.Status, "failed") && suiteFailedAssertions == 0 {
				name := filepath.Base(tr.Name)
				if name == "" || name == "." {
					name = "test suite"
				}
				res.Failures = append(res.Failures, TestCase{
					Name:    name,
					File:    tr.Name,
					Status:  "failed",
					Message: cleanMessage(tr.Message),
				})
			}
		}
	}

	if report.NumTotalTests != nil {
		res.Total = *report.NumTotalTests
		if report.NumPassedTests != nil {
			res.Passed = *report.NumPassedTests
		} else {
			res.Passed = countPassed
		}
		if report.NumFailedTests != nil {
			res.Failed = *report.NumFailedTests
		} else {
			res.Failed = countFailed
		}
		if report.NumPendingTests != nil || report.NumTodoTests != nil {
			if report.NumPendingTests != nil {
				res.Skipped += *report.NumPendingTests
			}
			if report.NumTodoTests != nil {
				res.Skipped += *report.NumTodoTests
			}
		} else {
			res.Skipped = countSkipped
		}
	} else {
		res.Total = countTotal
		res.Passed = countPassed
		res.Failed = countFailed
		res.Skipped = countSkipped
	}

	if res.Failed < len(res.Failures) {
		res.Failed = len(res.Failures)
	}

	return res
}

func detectJestOrVitestFormat(r *jestReport) string {
	hasJestTopLevel := r.WasInterrupted != nil ||
		len(r.OpenHandles) > 0 ||
		r.NumRuntimeErrorTestSuites != nil

	hasVitestMeta := false
	if r.TestResults != nil {
		for _, tr := range *r.TestResults {
			for _, ar := range tr.AssertionResults {
				if len(ar.Meta) > 0 {
					hasVitestMeta = true
					break
				}
			}
			if hasVitestMeta {
				break
			}
		}
	}

	if hasVitestMeta && !hasJestTopLevel {
		return FormatVitestJSON
	}
	if r.Snapshot != nil {
		if _, hasFilesRemoved := r.Snapshot["filesRemoved"]; !hasFilesRemoved {
			return FormatVitestJSON
		}
		return FormatJestJSON
	}
	// No snapshot object: Vitest reports commonly omit snapshot or Jest-only top-level keys,
	// whereas real Jest reports include wasInterrupted/openHandles/snapshot.
	if !hasJestTopLevel && r.NumTotalTestSuites != nil {
		return FormatVitestJSON
	}
	return FormatJestJSON
}

func normalizeJestStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "passed":
		return "passed"
	case "failed":
		return "failed"
	case "pending", "skipped", "disabled":
		return "skipped"
	case "todo":
		return "todo"
	default:
		return strings.ToLower(strings.TrimSpace(s))
	}
}

func assertionName(ar jestAssertionResult) string {
	if full := strings.TrimSpace(ar.FullName); full != "" {
		return full
	}
	parts := make([]string, 0, len(ar.AncestorTitles)+1)
	for _, a := range ar.AncestorTitles {
		if t := strings.TrimSpace(a); t != "" {
			parts = append(parts, t)
		}
	}
	if title := strings.TrimSpace(ar.Title); title != "" {
		parts = append(parts, title)
	}
	return strings.Join(parts, " ")
}

func firstFailureMessage(msgs []string) string {
	for _, m := range msgs {
		if cleaned := cleanMessage(m); cleaned != "" {
			return cleaned
		}
	}
	return ""
}

func cleanMessage(s string) string {
	s = ansiEscapeRe.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= maxMessageRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxMessageRunes])
}
