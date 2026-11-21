package rules

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Ramanan-Bala/donegate/internal/policy"
	"github.com/Ramanan-Bala/donegate/internal/runner"
)

func TestR001Registration(t *testing.T) {
	r, ok := Lookup("R001")
	if !ok {
		t.Fatal("R001 not registered")
	}
	if r.Level() != 2 {
		t.Errorf("Level() = %d, want 2", r.Level())
	}
	if r.DefaultSeverity() != Block {
		t.Errorf("DefaultSeverity() = %q, want %q", r.DefaultSeverity(), Block)
	}
}

func TestR001(t *testing.T) {
	r, ok := Lookup("R001")
	if !ok {
		t.Fatal("R001 not registered")
	}

	pol := policy.Default()
	pol.Projects = []policy.Project{
		{Name: "api", Root: "backend", Test: "npm test"},
		{Name: "web", Root: ".", Test: "npm test"},
	}

	makeFailures := func(n int) []runner.TestCase {
		out := make([]runner.TestCase, n)
		for i := range n {
			out[i] = runner.TestCase{
				Name:    fmt.Sprintf("test %d", i+1),
				File:    fmt.Sprintf("src/t%d.test.ts", i+1),
				Status:  "failed",
				Message: fmt.Sprintf("expected %d\n    at stack line", i+1),
			}
		}
		return out
	}

	tests := []struct {
		name     string
		ctx      *Context
		wantLen  int
		checkAll func(t *testing.T, findings []Finding)
	}{
		{
			name:    "nil context returns nil",
			ctx:     nil,
			wantLen: 0,
		},
		{
			name: "passing test and failing typecheck ignored",
			ctx: &Context{
				Policy: pol,
				Runs: []runner.Result{
					{Project: "api", Kind: runner.Test, ExitCode: 0, Failed: 0},
					{Project: "api", Kind: runner.Typecheck, ExitCode: 1, Output: "type error"},
				},
			},
			wantLen: 0,
		},
		{
			name: "parsed failures emit one finding each with first line and project root hint",
			ctx: &Context{
				Policy: pol,
				Runs: []runner.Result{
					{
						Project:  "api",
						Kind:     runner.Test,
						Command:  "npm test",
						Dir:      "/tmp/donegate-wt-999/backend",
						ExitCode: 1,
						Failed:   2,
						Failures: []runner.TestCase{
							{
								Name:    "applies 10% discount",
								File:    "backend/src/cart.test.ts",
								Status:  "failed",
								Message: "expected 900, got 1000\n    at cart.test.ts:10",
							},
							{
								Name:    "empty file falls back to project",
								File:    "",
								Status:  "failed",
								Message: "boom",
							},
						},
					},
				},
			},
			wantLen: 2,
			checkAll: func(t *testing.T, findings []Finding) {
				if findings[0].Path != "backend/src/cart.test.ts" {
					t.Errorf("findings[0].Path = %q, want backend/src/cart.test.ts", findings[0].Path)
				}
				if findings[0].Message != `"applies 10% discount" fails in a clean run: expected 900, got 1000` {
					t.Errorf("findings[0].Message = %q", findings[0].Message)
				}
				if findings[0].Hint != "Run `npm test` in backend to reproduce." {
					t.Errorf("findings[0].Hint = %q", findings[0].Hint)
				}
				if strings.Contains(findings[0].Hint, "donegate-wt") {
					t.Errorf("hint leaked temp dir: %q", findings[0].Hint)
				}
				if findings[1].Path != "api" {
					t.Errorf("findings[1].Path = %q, want api", findings[1].Path)
				}
			},
		},
		{
			name: "more than 5 failures capped at 5 plus summary finding",
			ctx: &Context{
				Policy: pol,
				Runs: []runner.Result{
					{
						Project:  "web",
						Kind:     runner.Test,
						Command:  "npm test",
						Dir:      "/tmp/donegate-wt-123",
						ExitCode: 1,
						Failed:   7,
						Failures: makeFailures(7),
					},
				},
			},
			wantLen: 6,
			checkAll: func(t *testing.T, findings []Finding) {
				last := findings[5]
				if last.Message != "... and 2 more failing tests" {
					t.Errorf("last.Message = %q, want '... and 2 more failing tests'", last.Message)
				}
				if last.Path != "web" {
					t.Errorf("last.Path = %q, want web", last.Path)
				}
				if last.Hint != "Run `npm test` in . to reproduce." {
					t.Errorf("last.Hint = %q", last.Hint)
				}
			},
		},
		{
			name: "timed out test run emits single timeout finding",
			ctx: &Context{
				Policy: pol,
				Runs: []runner.Result{
					{
						Project:  "api",
						Kind:     runner.Test,
						Command:  "npm test",
						Dir:      "/tmp/donegate-wt-123/backend",
						TimedOut: true,
						ExitCode: -1,
						Duration: 5 * time.Second,
					},
				},
			},
			wantLen: 1,
			checkAll: func(t *testing.T, findings []Finding) {
				f := findings[0]
				if f.Path != "api" {
					t.Errorf("Path = %q, want api", f.Path)
				}
				if f.Message != "tests timed out after 5s in project api" {
					t.Errorf("Message = %q", f.Message)
				}
				if f.Hint != "Run `npm test` in backend to reproduce." {
					t.Errorf("Hint = %q", f.Hint)
				}
			},
		},
		{
			name: "non-zero exit with no parsed failures includes last 10 lines",
			ctx: &Context{
				Policy: pol,
				Runs: []runner.Result{
					{
						Project:  "api",
						Kind:     runner.Test,
						Command:  "npm test",
						Dir:      "backend",
						ExitCode: 2,
						Output:   "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nl12\n",
					},
				},
			},
			wantLen: 1,
			checkAll: func(t *testing.T, findings []Finding) {
				f := findings[0]
				if strings.Contains(f.Message, "l1 |") || strings.Contains(f.Message, "l2 |") {
					t.Errorf("expected only last 10 lines, got %q", f.Message)
				}
				if !strings.Contains(f.Message, "l3 | l4 | l5 | l6 | l7 | l8 | l9 | l10 | l11 | l12") {
					t.Errorf("missing last 10 lines in %q", f.Message)
				}
				if f.Hint != "Run `npm test` in backend to reproduce." {
					t.Errorf("Hint = %q", f.Hint)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			findings := r.Check(tc.ctx)
			if len(findings) != tc.wantLen {
				t.Fatalf("got %d findings (%+v), want %d", len(findings), findings, tc.wantLen)
			}
			for i, f := range findings {
				if f.RuleID != "R001" {
					t.Errorf("finding[%d].RuleID = %q, want R001", i, f.RuleID)
				}
				if f.Severity != Block {
					t.Errorf("finding[%d].Severity = %q, want block", i, f.Severity)
				}
			}
			if tc.checkAll != nil {
				tc.checkAll(t, findings)
			}
		})
	}
}
