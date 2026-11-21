package rules

import "testing"

func TestH001Registration(t *testing.T) {
	r, ok := Lookup("H001")
	if !ok {
		t.Fatal("H001 not registered")
	}
	if r.Level() != 1 {
		t.Errorf("Level() = %d, want 1", r.Level())
	}
	if r.DefaultSeverity() != Block {
		t.Errorf("DefaultSeverity() = %q, want %q", r.DefaultSeverity(), Block)
	}
}

func TestH001(t *testing.T) {
	r, ok := Lookup("H001")
	if !ok {
		t.Fatal("H001 not registered")
	}

	tests := []struct {
		name     string
		ctx      *Context
		wantMsgs []string
	}{
		{
			name: "nil context reports nothing",
			ctx:  nil,
		},
		{
			name: "no tampered entries reports nothing",
			ctx:  &Context{},
		},
		{
			name: "changed and deleted entries produce findings",
			ctx: &Context{
				Tampered: []Tamper{
					{Path: "vitest.config.ts", Expected: "aaa", Actual: "bbb"},
					{Path: ".donegate.yml", Expected: "ccc", Actual: ""},
				},
			},
			wantMsgs: []string{
				"vitest.config.ts changed since the session started",
				".donegate.yml was deleted since the session started",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			findings := r.Check(tc.ctx)
			if len(findings) != len(tc.wantMsgs) {
				t.Fatalf("got %d findings (%+v), want %d", len(findings), findings, len(tc.wantMsgs))
			}
			const wantHint = "restore it; tests, policy and harness config are read-only during a session"
			for i, wantMsg := range tc.wantMsgs {
				f := findings[i]
				if f.RuleID != "H001" {
					t.Errorf("finding[%d].RuleID = %q, want H001", i, f.RuleID)
				}
				if f.Severity != Block {
					t.Errorf("finding[%d].Severity = %q, want %q", i, f.Severity, Block)
				}
				if f.Path != tc.ctx.Tampered[i].Path {
					t.Errorf("finding[%d].Path = %q, want %q", i, f.Path, tc.ctx.Tampered[i].Path)
				}
				if f.Message != wantMsg {
					t.Errorf("finding[%d].Message = %q, want %q", i, f.Message, wantMsg)
				}
				if f.Hint != wantHint {
					t.Errorf("finding[%d].Hint = %q, want %q", i, f.Hint, wantHint)
				}
			}
		})
	}
}
