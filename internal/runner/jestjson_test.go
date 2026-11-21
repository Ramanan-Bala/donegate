package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseJestJSON(t *testing.T) {
	readFixture := func(t *testing.T, name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatalf("read fixture %s: %v", name, err)
		}
		return data
	}

	longMsg := strings.Repeat("x", 650)
	longMsgReport := []byte(`{
		"numTotalTests": 1,
		"numPassedTests": 0,
		"numFailedTests": 1,
		"testResults": [{
			"name": "src/long.test.ts",
			"status": "failed",
			"assertionResults": [{
				"title": "truncates message",
				"status": "failed",
				"failureMessages": ["\u001b[31m` + longMsg + `\u001b[0m"]
			}]
		}]
	}`)

	tests := []struct {
		name       string
		input      []byte
		wantErr    bool
		wantFormat string
		wantTotal  int
		wantPassed int
		wantFailed int
		wantSkip   int
		checkFail  func(t *testing.T, failures []TestCase)
	}{
		{
			name:       "vitest golden report",
			input:      readFixture(t, "vitest_report.json"),
			wantFormat: FormatVitestJSON,
			wantTotal:  4,
			wantPassed: 2,
			wantFailed: 1,
			wantSkip:   1,
			checkFail: func(t *testing.T, failures []TestCase) {
				t.Helper()
				if len(failures) != 1 {
					t.Fatalf("len(Failures) = %d, want 1", len(failures))
				}
				f := failures[0]
				if f.Name != "cart applies 10% discount over 1000" {
					t.Errorf("Name = %q, want %q", f.Name, "cart applies 10% discount over 1000")
				}
				if f.File != "/repo/frontend/src/cart.test.ts" {
					t.Errorf("File = %q, want %q", f.File, "/repo/frontend/src/cart.test.ts")
				}
				if f.Status != "failed" {
					t.Errorf("Status = %q, want failed", f.Status)
				}
				if strings.Contains(f.Message, "\x1b") {
					t.Errorf("Message still contains ANSI escape: %q", f.Message)
				}
				if !strings.Contains(f.Message, "AssertionError: expected 1000 to be 900") {
					t.Errorf("Message = %q, want AssertionError substring", f.Message)
				}
			},
		},
		{
			name:       "jest golden report",
			input:      readFixture(t, "jest_report.json"),
			wantFormat: FormatJestJSON,
			wantTotal:  6,
			wantPassed: 2,
			wantFailed: 2,
			wantSkip:   2, // 1 pending + 1 todo
			checkFail: func(t *testing.T, failures []TestCase) {
				t.Helper()
				if len(failures) != 2 {
					t.Fatalf("len(Failures) = %d, want 2", len(failures))
				}
				if failures[0].Name != "auth login rejects expired token" {
					t.Errorf("failures[0].Name = %q, want %q", failures[0].Name, "auth login rejects expired token")
				}
				if strings.Contains(failures[0].Message, "\x1b") {
					t.Errorf("failures[0].Message contains ANSI escapes: %q", failures[0].Message)
				}
				if !strings.Contains(failures[0].Message, "Expected: 401") {
					t.Errorf("failures[0].Message = %q, want Expected: 401", failures[0].Message)
				}
				// Second failure has empty fullName -> built from ancestorTitles + title.
				if failures[1].Name != "auth refresh rotates refresh token once" {
					t.Errorf("failures[1].Name = %q, want %q", failures[1].Name, "auth refresh rotates refresh token once")
				}
				if failures[1].File != "/repo/backend/src/auth.test.ts" {
					t.Errorf("failures[1].File = %q, want %q", failures[1].File, "/repo/backend/src/auth.test.ts")
				}
			},
		},
		{
			name:       "strips ANSI and trims failure message to 500 chars",
			input:      longMsgReport,
			wantFormat: FormatJestJSON, // minimal ambiguous JSON defaults to jest-json
			wantTotal:  1,
			wantPassed: 0,
			wantFailed: 1,
			wantSkip:   0,
			checkFail: func(t *testing.T, failures []TestCase) {
				t.Helper()
				if len(failures) != 1 {
					t.Fatalf("len(Failures) = %d, want 1", len(failures))
				}
				msg := failures[0].Message
				if strings.Contains(msg, "\x1b") {
					t.Errorf("Message still contains ANSI escape")
				}
				if got := utf8.RuneCountInString(msg); got != 500 {
					t.Errorf("rune count = %d, want 500", got)
				}
			},
		},
		{
			name:    "malformed json",
			input:   readFixture(t, "malformed.json"),
			wantErr: true,
		},
		{
			name:    "unrelated json object",
			input:   []byte(`{"hello": "world"}`),
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   []byte("   \n"),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseJestJSON(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Format != tc.wantFormat {
				t.Errorf("Format = %q, want %q", got.Format, tc.wantFormat)
			}
			if got.Total != tc.wantTotal || got.Passed != tc.wantPassed || got.Failed != tc.wantFailed || got.Skipped != tc.wantSkip {
				t.Errorf("counts = (total=%d passed=%d failed=%d skipped=%d), want (%d %d %d %d)",
					got.Total, got.Passed, got.Failed, got.Skipped,
					tc.wantTotal, tc.wantPassed, tc.wantFailed, tc.wantSkip)
			}
			if tc.checkFail != nil {
				tc.checkFail(t, got.Failures)
			}
		})
	}
}
