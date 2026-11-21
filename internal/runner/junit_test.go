package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseJUnit(t *testing.T) {
	readFixture := func(t *testing.T, name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatalf("read fixture %s: %v", name, err)
		}
		return data
	}

	singleSuite := []byte(`<?xml version="1.0" encoding="utf-8"?>
<testsuite name="single" tests="100" failures="50">
  <testcase name="passes" classname="pkg" file="pkg_test.go" />
  <testcase name="fails" classname="pkg" file="pkg_test.go">
    <failure message="boom" />
  </testcase>
</testsuite>`)

	tests := []struct {
		name       string
		input      []byte
		wantErr    bool
		wantTotal  int
		wantPassed int
		wantFailed int
		wantSkip   int
		checkFail  func(t *testing.T, failures []TestCase)
	}{
		{
			name:       "pytest junit golden",
			input:      readFixture(t, "pytest_junit.xml"),
			wantTotal:  4,
			wantPassed: 1,
			wantFailed: 2, // 1 <failure> + 1 <error>
			wantSkip:   1,
			checkFail: func(t *testing.T, failures []TestCase) {
				t.Helper()
				if len(failures) != 2 {
					t.Fatalf("len(Failures) = %d, want 2", len(failures))
				}
				if failures[0].Name != "tests.test_cart.test_discount" {
					t.Errorf("failures[0].Name = %q, want %q", failures[0].Name, "tests.test_cart.test_discount")
				}
				if failures[0].File != "tests/test_cart.py" {
					t.Errorf("failures[0].File = %q, want %q", failures[0].File, "tests/test_cart.py")
				}
				if failures[0].Status != "failed" {
					t.Errorf("failures[0].Status = %q, want failed", failures[0].Status)
				}
				if failures[0].Message != "AssertionError: assert 1000 == 900" {
					t.Errorf("failures[0].Message = %q, want %q", failures[0].Message, "AssertionError: assert 1000 == 900")
				}
				if failures[1].Name != "tests.test_cart.test_db_fixture" {
					t.Errorf("failures[1].Name = %q, want %q", failures[1].Name, "tests.test_cart.test_db_fixture")
				}
				if failures[1].Message != "RuntimeError: connection refused" {
					t.Errorf("failures[1].Message = %q, want %q", failures[1].Message, "RuntimeError: connection refused")
				}
			},
		},
		{
			name:       "go-junit-report golden ignores wrong suite attributes",
			input:      readFixture(t, "go_junit_report.xml"),
			wantTotal:  3,
			wantPassed: 1,
			wantFailed: 1,
			wantSkip:   1,
			checkFail: func(t *testing.T, failures []TestCase) {
				t.Helper()
				if len(failures) != 1 {
					t.Fatalf("len(Failures) = %d, want 1", len(failures))
				}
				f := failures[0]
				if f.Name != "github.com/example/cart.TestDiscount" {
					t.Errorf("Name = %q, want %q", f.Name, "github.com/example/cart.TestDiscount")
				}
				if !strings.Contains(f.Message, "cart_test.go:24: got 1000, want 900") {
					t.Errorf("Message = %q, want failure body", f.Message)
				}
			},
		},
		{
			name:       "single testsuite root ignores suite attributes",
			input:      singleSuite,
			wantTotal:  2,
			wantPassed: 1,
			wantFailed: 1,
			wantSkip:   0,
			checkFail: func(t *testing.T, failures []TestCase) {
				t.Helper()
				if len(failures) != 1 {
					t.Fatalf("len(Failures) = %d, want 1", len(failures))
				}
				if failures[0].File != "pkg_test.go" || failures[0].Message != "boom" {
					t.Errorf("unexpected failure: %+v", failures[0])
				}
			},
		},
		{
			name:    "malformed xml",
			input:   readFixture(t, "malformed.xml"),
			wantErr: true,
		},
		{
			name:    "non-junit xml root",
			input:   []byte(`<html><body>not junit</body></html>`),
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   []byte(""),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseJUnit(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Format != FormatJUnit {
				t.Errorf("Format = %q, want %q", got.Format, FormatJUnit)
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
