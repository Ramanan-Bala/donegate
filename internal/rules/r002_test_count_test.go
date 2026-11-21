package rules

import (
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/runner"
)

func TestR002Registration(t *testing.T) {
	r, ok := Lookup("R002")
	if !ok {
		t.Fatal("R002 not registered")
	}
	if r.Level() != 2 {
		t.Errorf("Level() = %d, want 2", r.Level())
	}
	if r.DefaultSeverity() != Block {
		t.Errorf("DefaultSeverity() = %q, want %q", r.DefaultSeverity(), Block)
	}
}

func TestR002(t *testing.T) {
	r, ok := Lookup("R002")
	if !ok {
		t.Fatal("R002 not registered")
	}

	tests := []struct {
		name     string
		ctx      *Context
		wantMsgs []string
	}{
		{
			name: "nil context",
			ctx:  nil,
		},
		{
			name: "exit-code format ignored even when total is 0 or dropped",
			ctx: &Context{
				Runs: []runner.Result{
					{Project: "api", Kind: runner.Test, Format: runner.FormatExitCode, Total: 0},
				},
				BaselineRuns: []runner.Result{
					{Project: "api", Kind: runner.Test, Format: runner.FormatVitestJSON, Total: 10},
				},
			},
		},
		{
			name: "same or increased test count passes",
			ctx: &Context{
				Runs: []runner.Result{
					{Project: "api", Kind: runner.Test, Format: runner.FormatVitestJSON, Total: 12},
				},
				BaselineRuns: []runner.Result{
					{Project: "api", Kind: runner.Test, Format: runner.FormatVitestJSON, Total: 10},
				},
			},
		},
		{
			name: "zero tests ran triggers finding",
			ctx: &Context{
				Runs: []runner.Result{
					{Project: "api", Kind: runner.Test, Command: "npm test", Format: runner.FormatVitestJSON, Total: 0},
				},
			},
			wantMsgs: []string{"zero tests ran in project api"},
		},
		{
			name: "test count dropped vs baseline triggers finding",
			ctx: &Context{
				Runs: []runner.Result{
					{Project: "web", Kind: runner.Test, Format: runner.FormatJestJSON, Total: 7},
				},
				BaselineRuns: []runner.Result{
					{Project: "web", Kind: runner.Test, Format: runner.FormatJestJSON, Total: 10},
				},
			},
			wantMsgs: []string{"test count dropped from 10 to 7 in project web"},
		},
		{
			name: "baseline exit-code does not trigger drop check",
			ctx: &Context{
				Runs: []runner.Result{
					{Project: "web", Kind: runner.Test, Format: runner.FormatJUnit, Total: 3},
				},
				BaselineRuns: []runner.Result{
					{Project: "web", Kind: runner.Test, Format: runner.FormatExitCode, Total: 10},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			findings := r.Check(tc.ctx)
			if len(findings) != len(tc.wantMsgs) {
				t.Fatalf("got %d findings (%+v), want %d (%v)", len(findings), findings, len(tc.wantMsgs), tc.wantMsgs)
			}
			for i, want := range tc.wantMsgs {
				f := findings[i]
				if f.RuleID != "R002" {
					t.Errorf("finding[%d].RuleID = %q, want R002", i, f.RuleID)
				}
				if f.Severity != Block {
					t.Errorf("finding[%d].Severity = %q, want block", i, f.Severity)
				}
				if f.Message != want {
					t.Errorf("finding[%d].Message = %q, want %q", i, f.Message, want)
				}
				if f.Hint == "" {
					t.Errorf("finding[%d] missing Hint", i)
				}
			}
		})
	}
}
