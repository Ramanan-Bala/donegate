package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Kind identifies the verification command category being executed.
type Kind string

const (
	Test      Kind = "test"
	Typecheck Kind = "typecheck"
	Lint      Kind = "lint"
)

const (
	FormatVitestJSON = "vitest-json"
	FormatJestJSON   = "jest-json"
	FormatJUnit      = "junit"
	FormatExitCode   = "exit-code"
)

const maxOutputTailBytes = 4000

// TestCase describes one test case from a structured test report.
type TestCase struct {
	Name    string `json:"name"`
	File    string `json:"file,omitempty"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// Result captures the outcome of running a project's test, typecheck, or lint command.
type Result struct {
	Project  string        `json:"project"`
	Kind     Kind          `json:"kind"`
	Command  string        `json:"command"`
	Dir      string        `json:"dir"`
	ExitCode int           `json:"exit_code"`
	TimedOut bool          `json:"timed_out"`
	Duration time.Duration `json:"duration"`
	Format   string        `json:"format"`
	Total    int           `json:"total"`
	Passed   int           `json:"passed"`
	Failed   int           `json:"failed"`
	Skipped  int           `json:"skipped"`
	Failures []TestCase    `json:"failures,omitempty"`
	Output   string        `json:"output,omitempty"`
}

// OK reports whether the command completed without timing out, exited 0, and had no failed tests.
func (r Result) OK() bool {
	return !r.TimedOut && r.ExitCode == 0 && r.Failed == 0
}

// Spec configures a single runner invocation.
type Spec struct {
	Project string
	Kind    Kind
	Command string
	Dir     string
	Timeout time.Duration
	Env     []string
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]byte, b.buf.Len())
	copy(out, b.buf.Bytes())
	return out
}

// Run executes Spec's command in Spec.Dir with an optional timeout and parses
// structured test output when Spec.Kind is Test. A non-zero exit code is recorded
// on Result and is not returned as a Go error; errors indicate a failure to
// prepare or start the process.
func Run(ctx context.Context, s Spec) (Result, error) {
	commandStr := s.Command
	var jsonPath string
	if strings.Contains(commandStr, "{json}") {
		tmpDir, err := os.MkdirTemp("", "donegate-runner-*")
		if err != nil {
			return Result{}, fmt.Errorf("create temp json report dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)
		jsonPath = filepath.Join(tmpDir, "report.json")
		commandStr = strings.ReplaceAll(commandStr, "{json}", jsonPath)
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if s.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}

	cmd := shellCommand(runCtx, commandStr)
	cmd.Dir = s.Dir

	baseEnv := os.Environ()
	env := make([]string, 0, len(baseEnv)+2+len(s.Env))
	env = append(env, baseEnv...)
	env = append(env, "CI=1", "FORCE_COLOR=0")
	env = append(env, s.Env...)
	cmd.Env = env

	var outBuf lockedBuffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("start command: %w", err)
	}

	waitErr := cmd.Wait()
	duration := time.Since(start)

	res := Result{
		Project:  s.Project,
		Kind:     s.Kind,
		Command:  s.Command,
		Dir:      s.Dir,
		Duration: duration,
	}

	if waitErr != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			res.TimedOut = true
			res.ExitCode = -1
		} else {
			var exitErr *exec.ExitError
			switch {
			case errors.As(waitErr, &exitErr):
				res.ExitCode = exitErr.ExitCode()
			case errors.Is(waitErr, exec.ErrWaitDelay) && cmd.ProcessState != nil:
				res.ExitCode = cmd.ProcessState.ExitCode()
			default:
				res.ExitCode = -1
			}
		}
	}

	fullOutput := outBuf.Bytes()
	res.Output = tailOutput(fullOutput, maxOutputTailBytes)

	if s.Kind != Test {
		res.Format = FormatExitCode
		return res, nil
	}

	if res.TimedOut {
		res.Format = FormatExitCode
		res.Failed = 1
		return res, nil
	}

	if parsed, ok := parseTestOutput(s, jsonPath, fullOutput); ok {
		res.Format = parsed.Format
		res.Total = parsed.Total
		res.Passed = parsed.Passed
		res.Failed = parsed.Failed
		res.Skipped = parsed.Skipped
		res.Failures = parsed.Failures
		return res, nil
	}

	res.Format = FormatExitCode
	if res.ExitCode != 0 {
		res.Failed = 1
	}
	return res, nil
}

func parseTestOutput(s Spec, jsonPath string, fullOutput []byte) (Result, bool) {
	// 1. Parse the {json} report file if the placeholder was used.
	if jsonPath != "" {
		if data, err := os.ReadFile(jsonPath); err == nil && len(bytes.TrimSpace(data)) > 0 {
			if parsed, err := ParseJestJSON(data); err == nil {
				return parsed, true
			}
		}
	}

	// 2. Scan combined output for a Jest/Vitest JSON object.
	if parsed, ok := extractJestJSONFromOutput(fullOutput); ok {
		return parsed, true
	}

	// 3. Look for a JUnit XML file referenced by the command or s.Env.
	for _, p := range junitCandidatePaths(s.Command, s.Env, s.Dir) {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if parsed, err := ParseJUnit(data); err == nil {
			return parsed, true
		}
	}

	// 4. If --reporter=junit was requested without writing a file, try JUnit on stdout.
	if mentionsJUnitReporter(s.Command) {
		if parsed, ok := extractJUnitFromOutput(fullOutput); ok {
			return parsed, true
		}
	}

	return Result{}, false
}

func junitCandidatePaths(command string, env []string, dir string) []string {
	var raw []string

	// Command flags: --outputFile=<x>.xml, --outputFile <x>.xml, --junitxml=<x>.xml, etc.
	tokens := splitShellTokens(command)
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		lower := strings.ToLower(tok)
		if eqIdx := strings.IndexByte(tok, '='); eqIdx >= 0 {
			flag := strings.ToLower(tok[:eqIdx])
			val := trimQuotes(tok[eqIdx+1:])
			if (flag == "--outputfile" || flag == "--output-file" || flag == "--junitxml" || flag == "--junit-xml") && isXMLPath(val) {
				raw = append(raw, val)
			}
			continue
		}
		if (lower == "--outputfile" || lower == "--output-file" || lower == "--junitxml" || lower == "--junit-xml") && i+1 < len(tokens) {
			val := trimQuotes(tokens[i+1])
			if isXMLPath(val) {
				raw = append(raw, val)
			}
			i++
			continue
		}
	}

	// Environment variables referencing JUnit XML paths.
	var junitDir, junitName string
	for _, entry := range env {
		eqIdx := strings.IndexByte(entry, '=')
		if eqIdx <= 0 {
			continue
		}
		k := strings.TrimSpace(entry[:eqIdx])
		v := trimQuotes(strings.TrimSpace(entry[eqIdx+1:]))
		switch strings.ToUpper(k) {
		case "JEST_JUNIT_OUTPUT_FILE":
			if v != "" {
				raw = append(raw, v)
			}
		case "JEST_JUNIT_OUTPUT_DIR":
			junitDir = v
		case "JEST_JUNIT_OUTPUT_NAME":
			junitName = v
		default:
			if isXMLPath(v) {
				raw = append(raw, v)
			}
		}
	}
	if junitDir != "" {
		name := junitName
		if name == "" {
			name = "junit.xml"
		}
		raw = append(raw, filepath.Join(junitDir, name))
	} else if junitName != "" && isXMLPath(junitName) {
		raw = append(raw, junitName)
	}

	// Default JUnit filename if --reporter=junit was passed without an explicit file.
	if mentionsJUnitReporter(command) {
		raw = append(raw, "junit.xml")
	}

	seen := make(map[string]struct{}, len(raw))
	paths := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) && dir != "" {
			p = filepath.Join(dir, p)
		}
		p = filepath.Clean(p)
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}
	return paths
}

func mentionsJUnitReporter(command string) bool {
	lower := strings.ToLower(command)
	return strings.Contains(lower, "--reporter=junit") ||
		strings.Contains(lower, "--reporter junit") ||
		strings.Contains(lower, "--reporters=junit") ||
		strings.Contains(lower, "--reporters=jest-junit") ||
		strings.Contains(lower, "jest-junit")
}

func extractJUnitFromOutput(output []byte) (Result, bool) {
	if res, err := ParseJUnit(output); err == nil {
		return res, true
	}
	for _, marker := range [][]byte{[]byte("<?xml"), []byte("<testsuites"), []byte("<testsuite")} {
		idx := bytes.Index(output, marker)
		if idx < 0 {
			continue
		}
		if res, err := ParseJUnit(output[idx:]); err == nil {
			return res, true
		}
	}
	return Result{}, false
}

func splitShellTokens(s string) []string {
	var tokens []string
	var cur strings.Builder
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case ' ', '\t', '\n', '\r', ';', '&', '|':
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

func trimQuotes(s string) string {
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}

func isXMLPath(s string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(s)), ".xml")
}

func tailOutput(b []byte, limit int) string {
	if len(b) <= limit {
		return string(b)
	}
	tail := b[len(b)-limit:]
	// Avoid starting in the middle of a multi-byte UTF-8 rune.
	for len(tail) > 0 && (tail[0]&0xc0) == 0x80 {
		tail = tail[1:]
	}
	return string(tail)
}
