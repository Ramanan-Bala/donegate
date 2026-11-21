package claude

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/guard"
)

func TestParsePreToolUse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    guard.ToolCall
		wantErr bool
	}{
		{
			name:  "Write",
			input: `{"session_id":"s1","cwd":"/repo","hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":"/repo/a.ts","content":"x"}}`,
			want:  guard.ToolCall{Tool: "Write", Paths: []string{"/repo/a.ts"}, Cwd: "/repo"},
		},
		{
			name:  "Edit",
			input: `{"session_id":"s1","cwd":"/repo","hook_event_name":"PreToolUse","tool_name":"Edit","tool_input":{"file_path":"src/a.test.ts","old_string":"a","new_string":"b"}}`,
			want:  guard.ToolCall{Tool: "Edit", Paths: []string{"src/a.test.ts"}, Cwd: "/repo"},
		},
		{
			name:  "MultiEdit",
			input: `{"session_id":"s1","cwd":"/repo","hook_event_name":"PreToolUse","tool_name":"MultiEdit","tool_input":{"file_path":"src/b.test.ts","edits":[]}}`,
			want:  guard.ToolCall{Tool: "MultiEdit", Paths: []string{"src/b.test.ts"}, Cwd: "/repo"},
		},
		{
			name:  "NotebookEdit",
			input: `{"session_id":"s1","cwd":"/repo","hook_event_name":"PreToolUse","tool_name":"NotebookEdit","tool_input":{"notebook_path":"nb.ipynb"}}`,
			want:  guard.ToolCall{Tool: "NotebookEdit", Paths: []string{"nb.ipynb"}, Cwd: "/repo"},
		},
		{
			name:  "Bash",
			input: `{"session_id":"s1","cwd":"/repo","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"vitest -u"}}`,
			want:  guard.ToolCall{Tool: "Bash", Command: "vitest -u", Cwd: "/repo"},
		},
		{
			name:  "Other tool Read",
			input: `{"session_id":"s1","cwd":"/repo","hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/repo/a.test.ts"}}`,
			want:  guard.ToolCall{Tool: "Read", Cwd: "/repo"},
		},
		{
			name:    "Invalid JSON",
			input:   `{bad`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePreToolUse(strings.NewReader(tc.input))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
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

func TestWritePreToolUse(t *testing.T) {
	t.Run("allow writes nothing", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WritePreToolUse(&buf, guard.Decision{Allow: true}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if buf.Len() != 0 {
			t.Fatalf("expected empty output, got %q", buf.String())
		}
	})

	t.Run("deny writes exact Claude hook JSON shape", func(t *testing.T) {
		var buf bytes.Buffer
		dec := guard.Decision{
			Allow:  false,
			RuleID: "T001",
			Path:   "src/cart.test.ts",
			Reason: "donegate: src/cart.test.ts is a protected test file (T001).",
		}
		if err := WritePreToolUse(&buf, dec); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"donegate: src/cart.test.ts is a protected test file (T001)."}}` + "\n"
		if buf.String() != want {
			t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
		}
	})
}
