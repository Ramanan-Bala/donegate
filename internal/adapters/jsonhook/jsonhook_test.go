package jsonhook

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/gate"
	"github.com/Ramanan-Bala/donegate/internal/guard"
)

func TestParseGuard(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    guard.ToolCall
		wantErr bool
	}{
		{
			name:  "edit with paths maps to Write",
			input: `{"tool":"edit","paths":["src/a.ts"],"command":"","cwd":"/abs/repo"}`,
			want:  guard.ToolCall{Tool: "Write", Paths: []string{"src/a.ts"}, Cwd: "/abs/repo"},
		},
		{
			name:  "custom file tool with multiple paths maps to Write",
			input: `{"tool":"apply_patch","paths":["src/a.ts","src/b.test.ts"],"cwd":"/abs/repo"}`,
			want:  guard.ToolCall{Tool: "Write", Paths: []string{"src/a.ts", "src/b.test.ts"}, Cwd: "/abs/repo"},
		},
		{
			name:  "bash maps to Bash with command",
			input: `{"tool":"bash","paths":[],"command":"vitest -u","cwd":"/abs/repo"}`,
			want:  guard.ToolCall{Tool: "Bash", Command: "vitest -u", Cwd: "/abs/repo"},
		},
		{
			name:  "shell maps to Bash with command",
			input: `{"tool":"shell","command":"sed -i 's/a/b/' src/a.test.ts","cwd":"/abs/repo"}`,
			want:  guard.ToolCall{Tool: "Bash", Command: "sed -i 's/a/b/' src/a.test.ts", Cwd: "/abs/repo"},
		},
		{
			name:  "unknown tool without paths or command has neither",
			input: `{"tool":"grep","paths":[],"command":"","cwd":"/abs/repo"}`,
			want:  guard.ToolCall{Tool: "grep", Cwd: "/abs/repo"},
		},
		{
			name:  "unknown tool ignores command",
			input: `{"tool":"read","command":"rm src/a.test.ts","cwd":"/abs/repo"}`,
			want:  guard.ToolCall{Tool: "read", Cwd: "/abs/repo"},
		},
		{
			name:    "invalid JSON",
			input:   `{bad`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseGuard(strings.NewReader(tc.input))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestWriteGuard(t *testing.T) {
	t.Run("allow", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteGuard(&buf, guard.Decision{Allow: true}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `{"allow":true}` + "\n"
		if buf.String() != want {
			t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
		}
	})

	t.Run("deny", func(t *testing.T) {
		var buf bytes.Buffer
		dec := guard.Decision{
			Allow:  false,
			RuleID: "T001",
			Path:   "src/a.test.ts",
			Reason: "donegate: src/a.test.ts is a protected test file (T001).",
		}
		if err := WriteGuard(&buf, dec); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `{"allow":false,"rule":"T001","path":"src/a.test.ts","reason":"donegate: src/a.test.ts is a protected test file (T001)."}` + "\n"
		if buf.String() != want {
			t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
		}
	})

	t.Run("error", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteGuardError(&buf, errors.New("bad input")); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `{"allow":true,"error":"bad input"}` + "\n"
		if buf.String() != want {
			t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
		}
	})
}

func TestParseAndWriteGate(t *testing.T) {
	t.Run("parse valid", func(t *testing.T) {
		got, err := ParseGate(strings.NewReader(`{"cwd":"/abs/repo","session":"oc-1"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := GateInput{Cwd: "/abs/repo", Session: "oc-1"}
		if got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("parse invalid", func(t *testing.T) {
		if _, err := ParseGate(strings.NewReader(`not json`)); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("write block", func(t *testing.T) {
		var buf bytes.Buffer
		err := WriteGate(&buf, &gate.Outcome{
			Block:        true,
			Message:      "donegate: not done (1 failure, attempt 1/5)",
			Verdict:      "FAIL",
			Attempt:      1,
			MaxBlocks:    5,
			EvidencePath: "/path/evidence.json",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `{"block":true,"message":"donegate: not done (1 failure, attempt 1/5)","verdict":"FAIL","attempt":1,"max_blocks":5,"evidence":"/path/evidence.json"}` + "\n"
		if buf.String() != want {
			t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
		}
	})

	t.Run("write pass", func(t *testing.T) {
		var buf bytes.Buffer
		err := WriteGate(&buf, &gate.Outcome{
			Block:        false,
			UserMessage:  "donegate: verified (0 warnings). Evidence: /path/evidence.json",
			Verdict:      "PASS",
			Attempt:      0,
			MaxBlocks:    5,
			EvidencePath: "/path/evidence.json",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `{"block":false,"user_message":"donegate: verified (0 warnings). Evidence: /path/evidence.json","verdict":"PASS","attempt":0,"max_blocks":5,"evidence":"/path/evidence.json"}` + "\n"
		if buf.String() != want {
			t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
		}
	})

	t.Run("write nothing changed", func(t *testing.T) {
		var buf bytes.Buffer
		err := WriteGate(&buf, &gate.Outcome{
			Block:   false,
			Verdict: "PASS",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `{"block":false}` + "\n"
		if buf.String() != want {
			t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
		}
	})

	t.Run("write no session", func(t *testing.T) {
		var buf bytes.Buffer
		err := WriteGate(&buf, &gate.Outcome{
			Block:       false,
			UserMessage: "donegate: no session for this repo",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `{"block":false}` + "\n"
		if buf.String() != want {
			t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
		}
	})

	t.Run("write error", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteGateError(&buf, errors.New("boom")); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `{"block":false,"error":"boom"}` + "\n"
		if buf.String() != want {
			t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
		}
	})
}

func TestParseAndWriteSessionStart(t *testing.T) {
	t.Run("parse valid", func(t *testing.T) {
		got, err := ParseSessionStart(strings.NewReader(`{"cwd":"/abs/repo","session":"pi-1","source":"resume"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := SessionStartInput{Cwd: "/abs/repo", Session: "pi-1", Source: "resume"}
		if got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("parse invalid", func(t *testing.T) {
		if _, err := ParseSessionStart(strings.NewReader(`{`)); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("write started", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteSessionStart(&buf, "sess-1", false, "donegate is active"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `{"session":"sess-1","reused":false,"context":"donegate is active"}` + "\n"
		if buf.String() != want {
			t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
		}
	})

	t.Run("write reused", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteSessionStart(&buf, "sess-1", true, "donegate is active"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `{"session":"sess-1","reused":true,"context":"donegate is active"}` + "\n"
		if buf.String() != want {
			t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
		}
	})

	t.Run("write skipped", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteSessionStartSkipped(&buf); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `{"skipped":true}` + "\n"
		if buf.String() != want {
			t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
		}
	})

	t.Run("write error", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteSessionStartError(&buf, errors.New("session failed")); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `{"error":"session failed"}` + "\n"
		if buf.String() != want {
			t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
		}
	})
}
