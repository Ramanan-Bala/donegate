package runner

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestResultOK(t *testing.T) {
	tests := []struct {
		name string
		res  Result
		want bool
	}{
		{
			name: "passing",
			res:  Result{ExitCode: 0, TimedOut: false, Failed: 0},
			want: true,
		},
		{
			name: "non-zero exit code",
			res:  Result{ExitCode: 1, TimedOut: false, Failed: 0},
			want: false,
		},
		{
			name: "timed out",
			res:  Result{ExitCode: 0, TimedOut: true, Failed: 0},
			want: false,
		},
		{
			name: "failed tests with zero exit",
			res:  Result{ExitCode: 0, TimedOut: false, Failed: 1},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.res.OK(); got != tc.want {
				t.Errorf("OK() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command tests use POSIX /bin/sh")
	}

	absTestdata, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatalf("abs testdata: %v", err)
	}
	vitestFixture := filepath.Join(absTestdata, "vitest_report.json")
	jestFixture := filepath.Join(absTestdata, "jest_report.json")
	pytestFixture := filepath.Join(absTestdata, "pytest_junit.xml")
	malformedJSON := filepath.Join(absTestdata, "malformed.json")
	malformedXML := filepath.Join(absTestdata, "malformed.xml")

	tests := []struct {
		name  string
		spec  func(dir string) Spec
		check func(t *testing.T, res Result)
	}{
		{
			name: "exit 0",
			spec: func(dir string) Spec {
				return Spec{
					Project: "web",
					Kind:    Test,
					Command: "printf 'all green\\n'",
					Dir:     dir,
				}
			},
			check: func(t *testing.T, res Result) {
				t.Helper()
				if !res.OK() {
					t.Errorf("OK() = false, want true (res=%+v)", res)
				}
				if res.ExitCode != 0 || res.TimedOut {
					t.Errorf("ExitCode=%d TimedOut=%v, want 0 false", res.ExitCode, res.TimedOut)
				}
				if res.Format != FormatExitCode {
					t.Errorf("Format = %q, want %q", res.Format, FormatExitCode)
				}
				if res.Failed != 0 {
					t.Errorf("Failed = %d, want 0", res.Failed)
				}
				if strings.TrimSpace(res.Output) != "all green" {
					t.Errorf("Output = %q, want %q", res.Output, "all green")
				}
			},
		},
		{
			name: "exit 1 fallback",
			spec: func(dir string) Spec {
				return Spec{
					Project: "web",
					Kind:    Test,
					Command: "echo 'boom' >&2; exit 1",
					Dir:     dir,
				}
			},
			check: func(t *testing.T, res Result) {
				t.Helper()
				if res.OK() {
					t.Errorf("OK() = true, want false")
				}
				if res.ExitCode != 1 {
					t.Errorf("ExitCode = %d, want 1", res.ExitCode)
				}
				if res.Format != FormatExitCode {
					t.Errorf("Format = %q, want %q", res.Format, FormatExitCode)
				}
				if res.Failed != 1 {
					t.Errorf("Failed = %d, want 1", res.Failed)
				}
				if !strings.Contains(res.Output, "boom") {
					t.Errorf("Output = %q, want substring boom", res.Output)
				}
			},
		},
		{
			name: "{json} placeholder substitution with vitest fixture",
			spec: func(dir string) Spec {
				return Spec{
					Project: "web",
					Kind:    Test,
					Command: "cp '" + vitestFixture + "' {json} && exit 1",
					Dir:     dir,
				}
			},
			check: func(t *testing.T, res Result) {
				t.Helper()
				if res.OK() {
					t.Errorf("OK() = true, want false")
				}
				if res.ExitCode != 1 {
					t.Errorf("ExitCode = %d, want 1", res.ExitCode)
				}
				if res.Format != FormatVitestJSON {
					t.Errorf("Format = %q, want %q", res.Format, FormatVitestJSON)
				}
				if res.Total != 4 || res.Passed != 2 || res.Failed != 1 || res.Skipped != 1 {
					t.Errorf("counts = (%d %d %d %d), want (4 2 1 1)", res.Total, res.Passed, res.Failed, res.Skipped)
				}
				if len(res.Failures) != 1 || res.Failures[0].Name != "cart applies 10% discount over 1000" {
					t.Errorf("unexpected Failures: %+v", res.Failures)
				}
				if !strings.Contains(res.Command, "{json}") {
					t.Errorf("Result.Command = %q, want original command with {json}", res.Command)
				}
			},
		},
		{
			name: "json printed on stdout among noise",
			spec: func(dir string) Spec {
				return Spec{
					Project: "api",
					Kind:    Test,
					Command: "echo '> api@1.0.0 test'; echo 'noise { not json }'; cat '" + jestFixture + "'; echo 'trailing log line'; exit 1",
					Dir:     dir,
				}
			},
			check: func(t *testing.T, res Result) {
				t.Helper()
				if res.Format != FormatJestJSON {
					t.Errorf("Format = %q, want %q", res.Format, FormatJestJSON)
				}
				if res.Total != 6 || res.Passed != 2 || res.Failed != 2 || res.Skipped != 2 {
					t.Errorf("counts = (%d %d %d %d), want (6 2 2 2)", res.Total, res.Passed, res.Failed, res.Skipped)
				}
				if len(res.Failures) != 2 {
					t.Errorf("len(Failures) = %d, want 2", len(res.Failures))
				}
			},
		},
		{
			name: "junit xml via --outputFile flag",
			spec: func(dir string) Spec {
				return Spec{
					Project: "py",
					Kind:    Test,
					Command: "cp '" + pytestFixture + "' report.xml && true --outputFile=report.xml; exit 1",
					Dir:     dir,
				}
			},
			check: func(t *testing.T, res Result) {
				t.Helper()
				if res.Format != FormatJUnit {
					t.Errorf("Format = %q, want %q", res.Format, FormatJUnit)
				}
				if res.Total != 4 || res.Passed != 1 || res.Failed != 2 || res.Skipped != 1 {
					t.Errorf("counts = (%d %d %d %d), want (4 1 2 1)", res.Total, res.Passed, res.Failed, res.Skipped)
				}
				if len(res.Failures) != 2 {
					t.Errorf("len(Failures) = %d, want 2", len(res.Failures))
				}
			},
		},
		{
			name: "junit xml via env var",
			spec: func(dir string) Spec {
				return Spec{
					Project: "api",
					Kind:    Test,
					Command: "cp '" + pytestFixture + "' out.xml; exit 1",
					Dir:     dir,
					Env:     []string{"JEST_JUNIT_OUTPUT_FILE=out.xml"},
				}
			},
			check: func(t *testing.T, res Result) {
				t.Helper()
				if res.Format != FormatJUnit {
					t.Errorf("Format = %q, want %q", res.Format, FormatJUnit)
				}
				if res.Total != 4 || res.Failed != 2 {
					t.Errorf("Total=%d Failed=%d, want 4 2", res.Total, res.Failed)
				}
			},
		},
		{
			name: "malformed json falls back to exit code without panic",
			spec: func(dir string) Spec {
				return Spec{
					Project: "web",
					Kind:    Test,
					Command: "cp '" + malformedJSON + "' {json}; exit 1",
					Dir:     dir,
				}
			},
			check: func(t *testing.T, res Result) {
				t.Helper()
				if res.Format != FormatExitCode {
					t.Errorf("Format = %q, want %q", res.Format, FormatExitCode)
				}
				if res.ExitCode != 1 || res.Failed != 1 {
					t.Errorf("ExitCode=%d Failed=%d, want 1 1", res.ExitCode, res.Failed)
				}
			},
		},
		{
			name: "malformed xml falls back to exit code without panic",
			spec: func(dir string) Spec {
				return Spec{
					Project: "py",
					Kind:    Test,
					Command: "cp '" + malformedXML + "' bad.xml && true --outputFile=bad.xml; exit 1",
					Dir:     dir,
				}
			},
			check: func(t *testing.T, res Result) {
				t.Helper()
				if res.Format != FormatExitCode {
					t.Errorf("Format = %q, want %q", res.Format, FormatExitCode)
				}
				if res.ExitCode != 1 || res.Failed != 1 {
					t.Errorf("ExitCode=%d Failed=%d, want 1 1", res.ExitCode, res.Failed)
				}
			},
		},
		{
			name: "typecheck ignores json output and uses exit code only",
			spec: func(dir string) Spec {
				return Spec{
					Project: "web",
					Kind:    Typecheck,
					Command: "cat '" + vitestFixture + "'; exit 0",
					Dir:     dir,
				}
			},
			check: func(t *testing.T, res Result) {
				t.Helper()
				if !res.OK() {
					t.Errorf("OK() = false, want true")
				}
				if res.Format != FormatExitCode {
					t.Errorf("Format = %q, want %q", res.Format, FormatExitCode)
				}
				if res.Total != 0 || res.Failed != 0 || len(res.Failures) != 0 {
					t.Errorf("expected zero parsed counts for Typecheck, got %+v", res)
				}
			},
		},
		{
			name: "lint exit code only",
			spec: func(dir string) Spec {
				return Spec{
					Project: "web",
					Kind:    Lint,
					Command: "echo 'lint error' >&2; exit 2",
					Dir:     dir,
				}
			},
			check: func(t *testing.T, res Result) {
				t.Helper()
				if res.OK() {
					t.Errorf("OK() = true, want false")
				}
				if res.ExitCode != 2 || res.Format != FormatExitCode {
					t.Errorf("ExitCode=%d Format=%q, want 2 %q", res.ExitCode, res.Format, FormatExitCode)
				}
			},
		},
		{
			name: "sets CI=1 FORCE_COLOR=0 and custom Env",
			spec: func(dir string) Spec {
				return Spec{
					Project: "web",
					Kind:    Test,
					Command: `printf "CI=%s FORCE_COLOR=%s CUSTOM=%s" "$CI" "$FORCE_COLOR" "$CUSTOM_VAR"`,
					Dir:     dir,
					Env:     []string{"CUSTOM_VAR=hello"},
				}
			},
			check: func(t *testing.T, res Result) {
				t.Helper()
				if res.Output != "CI=1 FORCE_COLOR=0 CUSTOM=hello" {
					t.Errorf("Output = %q, want %q", res.Output, "CI=1 FORCE_COLOR=0 CUSTOM=hello")
				}
			},
		},
		{
			name: "output tail truncation keeps last 4000 bytes",
			spec: func(dir string) Spec {
				return Spec{
					Project: "web",
					Kind:    Test,
					Command: `awk 'BEGIN { for (i = 0; i < 6000; i++) printf "A"; printf "TAIL_MARKER" }'`,
					Dir:     dir,
				}
			},
			check: func(t *testing.T, res Result) {
				t.Helper()
				if len(res.Output) != maxOutputTailBytes {
					t.Errorf("len(Output) = %d, want %d", len(res.Output), maxOutputTailBytes)
				}
				if !strings.HasSuffix(res.Output, "TAIL_MARKER") {
					t.Errorf("Output does not end with TAIL_MARKER")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			res, err := Run(context.Background(), tc.spec(dir))
			if err != nil {
				t.Fatalf("Run returned unexpected error: %v", err)
			}
			tc.check(t, res)
		})
	}
}

func TestRunTimeoutKillsProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group timeout test is unix-only")
	}

	start := time.Now()
	res, err := Run(context.Background(), Spec{
		Project: "slow",
		Kind:    Test,
		Command: "sleep 5",
		Dir:     t.TempDir(),
		Timeout: 200 * time.Millisecond,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if !res.TimedOut {
		t.Errorf("TimedOut = false, want true")
	}
	if res.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1", res.ExitCode)
	}
	if res.OK() {
		t.Errorf("OK() = true, want false")
	}
	if elapsed >= 2*time.Second {
		t.Errorf("Run took %v, expected < 2s (child process likely not killed)", elapsed)
	}
}

func TestRunStartError(t *testing.T) {
	_, err := Run(context.Background(), Spec{
		Project: "missing-dir",
		Kind:    Test,
		Command: "echo hi",
		Dir:     filepath.Join(t.TempDir(), "does-not-exist"),
	})
	if err == nil {
		t.Fatal("expected error when starting command in nonexistent Dir, got nil")
	}
}

func TestResultJSONTags(t *testing.T) {
	res := Result{
		Project:  "web",
		Kind:     Test,
		Command:  "npm test",
		Dir:      "/repo",
		ExitCode: 1,
		TimedOut: false,
		Duration: 250 * time.Millisecond,
		Format:   FormatVitestJSON,
		Total:    2,
		Passed:   1,
		Failed:   1,
		Skipped:  0,
		Failures: []TestCase{{Name: "test a", File: "a.test.ts", Status: "failed", Message: "oops"}},
		Output:   "out",
	}

	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal Result: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal Result: %v", err)
	}
	for _, key := range []string{
		"project", "kind", "command", "dir", "exit_code", "timed_out",
		"duration", "format", "total", "passed", "failed", "skipped",
		"failures", "output",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q in %s", key, string(raw))
		}
	}
}
