package rules

import (
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/policy"
	"github.com/Ramanan-Bala/donegate/internal/runner"
)

func TestR003Registration(t *testing.T) {
	r, ok := Lookup("R003")
	if !ok {
		t.Fatal("R003 not registered")
	}
	if r.Level() != 2 {
		t.Errorf("Level() = %d, want 2", r.Level())
	}
	if r.DefaultSeverity() != Block {
		t.Errorf("DefaultSeverity() = %q, want %q", r.DefaultSeverity(), Block)
	}
}

func TestR003(t *testing.T) {
	r, ok := Lookup("R003")
	if !ok {
		t.Fatal("R003 not registered")
	}

	pol := policy.Default()
	pol.Projects = []policy.Project{
		{Name: "api", Root: "backend", Typecheck: "npx tsc --noEmit", Lint: "npm run lint"},
	}

	tests := []struct {
		name     string
		ctx      *Context
		wantLen  int
		checkAll func(t *testing.T, findings []Finding)
	}{
		{
			name:    "nil context",
			ctx:     nil,
			wantLen: 0,
		},
		{
			name: "passing typecheck/lint and failing test ignored",
			ctx: &Context{
				Policy: pol,
				Runs: []runner.Result{
					{Project: "api", Kind: runner.Typecheck, ExitCode: 0},
					{Project: "api", Kind: runner.Lint, ExitCode: 0},
					{Project: "api", Kind: runner.Test, ExitCode: 1},
				},
			},
			wantLen: 0,
		},
		{
			name: "failing typecheck and lint emit findings with trimmed last 10 lines",
			ctx: &Context{
				Policy: pol,
				Runs: []runner.Result{
					{
						Project:  "api",
						Kind:     runner.Typecheck,
						Command:  "npx tsc --noEmit",
						Dir:      "/tmp/donegate-wt-1/backend",
						ExitCode: 2,
						Output:   "  src/a.ts(1,1): error TS2322  \n\n  src/b.ts(2,3): error TS2304 \n",
					},
					{
						Project:  "api",
						Kind:     runner.Lint,
						Command:  "npm run lint",
						Dir:      "/tmp/donegate-wt-1/backend",
						ExitCode: 1,
						Output:   "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n",
					},
				},
			},
			wantLen: 2,
			checkAll: func(t *testing.T, findings []Finding) {
				if findings[0].Message != "typecheck failed in project api: src/a.ts(1,1): error TS2322 | src/b.ts(2,3): error TS2304" {
					t.Errorf("findings[0].Message = %q", findings[0].Message)
				}
				if findings[0].Hint != "Run `npx tsc --noEmit` in backend to reproduce." {
					t.Errorf("findings[0].Hint = %q", findings[0].Hint)
				}
				if findings[1].Message != "lint failed in project api: 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10 | 11 | 12" {
					t.Errorf("findings[1].Message = %q", findings[1].Message)
				}
				if findings[1].Hint != "Run `npm run lint` in backend to reproduce." {
					t.Errorf("findings[1].Hint = %q", findings[1].Hint)
				}
				if strings.Contains(findings[0].Hint, "donegate-wt") {
					t.Errorf("hint leaked temp dir: %q", findings[0].Hint)
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
				if f.RuleID != "R003" {
					t.Errorf("finding[%d].RuleID = %q, want R003", i, f.RuleID)
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
