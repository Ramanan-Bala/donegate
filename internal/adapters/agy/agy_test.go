package agy

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/gate"
	"github.com/Ramanan-Bala/donegate/internal/guard"
)

func TestParseAndWriteSessionStart(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    SessionStartInput
		wantErr bool
	}{
		{
			name: "payload from hooks.md",
			input: `{
				"conversationId": "ec33ebf9-0cba-4100-8142-c61503f6c587",
				"workspacePaths": ["/path/to/workspace"],
				"transcriptPath": "/path/to/workspace/.gemini/antigravity/transcript.jsonl",
				"artifactDirectoryPath": "/path/to/workspace/.gemini/antigravity/artifacts",
				"modelName": "auto",
				"hookEventName": "SessionStart",
				"cwd": "/path/to/workspace"
			}`,
			want: SessionStartInput{
				ConversationID: "ec33ebf9-0cba-4100-8142-c61503f6c587",
				Cwd:            "/path/to/workspace",
				WorkspacePaths: []string{"/path/to/workspace"},
				HookEventName:  "SessionStart",
			},
		},
		{
			name:  "falls back to workspacePaths[0] when cwd omitted",
			input: `{"conversationId":"conv-2","workspacePaths":["/ws/one","/ws/two"],"hookEventName":"SessionStart"}`,
			want: SessionStartInput{
				ConversationID: "conv-2",
				Cwd:            "/ws/one",
				WorkspacePaths: []string{"/ws/one", "/ws/two"},
				HookEventName:  "SessionStart",
			},
		},
		{
			name:    "invalid JSON",
			input:   `{bad`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseSessionStart(strings.NewReader(tc.input))
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

	t.Run("write SessionStart injectSteps", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteSessionStart(&buf, "donegate is active"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `{"injectSteps":[{"systemMessage":{"systemMessage":"donegate is active"}}]}` + "\n"
		if buf.String() != want {
			t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
		}
	})

	t.Run("write empty on skip or error", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteEmpty(&buf); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if buf.String() != "{}\n" {
			t.Errorf("got %q, want %q", buf.String(), "{}\n")
		}
	})
}

func TestParseAndWritePreToolUse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    guard.ToolCall
		wantErr bool
	}{
		{
			name: "real-looking payload from hooks.md",
			input: `{
				"toolCall": {
					"id": "toolu_01",
					"name": "run_command",
					"args": {
						"CommandLine": "npm test"
					}
				},
				"stepIdx": 19,
				"conversationId": "ec33ebf9-0cba-4100-8142-c61503f6c587",
				"workspacePaths": ["/path/to/workspace"],
				"transcriptPath": "/path/to/workspace/.gemini/antigravity/transcript.jsonl",
				"artifactDirectoryPath": "/path/to/workspace/.gemini/antigravity/artifacts",
				"modelName": "auto",
				"lastUserInput": "Fix the failing unit test",
				"hookEventName": "PreToolUse",
				"cwd": "/path/to/workspace"
			}`,
			want: guard.ToolCall{Tool: "Bash", Command: "npm test", Cwd: "/path/to/workspace"},
		},
		{
			name:  "write_file{Path}",
			input: `{"cwd":"/repo","hookEventName":"PreToolUse","toolCall":{"id":"t1","name":"write_file","args":{"Path":"src/a.test.ts","Content":"x","Overwrite":true}}}`,
			want:  guard.ToolCall{Tool: "Write", Paths: []string{"src/a.test.ts"}, Cwd: "/repo"},
		},
		{
			name:  "edit_file{Path}",
			input: `{"cwd":"/repo","hookEventName":"PreToolUse","toolCall":{"id":"t2","name":"edit_file","args":{"Path":"src/b.test.ts","Target":"a","Replacement":"b"}}}`,
			want:  guard.ToolCall{Tool: "Write", Paths: []string{"src/b.test.ts"}, Cwd: "/repo"},
		},
		{
			name:  "run_command{CommandLine}",
			input: `{"cwd":"/repo","hookEventName":"PreToolUse","toolCall":{"id":"t3","name":"run_command","args":{"CommandLine":"vitest -u","WaitMsBeforeAsync":5000}}}`,
			want:  guard.ToolCall{Tool: "Bash", Command: "vitest -u", Cwd: "/repo"},
		},
		{
			name:  "shell tool fallback to Command",
			input: `{"workspacePaths":["/ws"],"hookEventName":"PreToolUse","toolCall":{"id":"t4","name":"execute_shell","args":{"Command":"sed -i s/a/b/ foo.test.ts"}}}`,
			want:  guard.ToolCall{Tool: "Bash", Command: "sed -i s/a/b/ foo.test.ts", Cwd: "/ws"},
		},
		{
			name:  "terminal tool",
			input: `{"cwd":"/repo","toolCall":{"id":"t5","name":"run_terminal_cmd","args":{"CommandLine":"rm a.test.ts"}}}`,
			want:  guard.ToolCall{Tool: "Bash", Command: "rm a.test.ts", Cwd: "/repo"},
		},
		{
			name:  "replace/delete/move/create with multiple path keys",
			input: `{"cwd":"/repo","toolCall":{"id":"t6","name":"move_file","args":{"Source":"a.test.ts","Destination":"b.ts","TargetFile":"c.ts","AbsolutePath":"/repo/d.ts","FilePath":"e.ts","File":"f.ts"}}}`,
			want:  guard.ToolCall{Tool: "Write", Paths: []string{"c.ts", "/repo/d.ts", "e.ts", "f.ts", "b.ts", "a.test.ts"}, Cwd: "/repo"},
		},
		{
			name:  "replace_file_content{TargetFile}",
			input: `{"cwd":"/repo","toolCall":{"id":"t7","name":"replace_file_content","args":{"TargetFile":"src/c.test.ts"}}}`,
			want:  guard.ToolCall{Tool: "Write", Paths: []string{"src/c.test.ts"}, Cwd: "/repo"},
		},
		{
			name:  "delete_file{AbsolutePath}",
			input: `{"cwd":"/repo","toolCall":{"id":"t8","name":"delete_file","args":{"AbsolutePath":"/repo/src/d.test.ts"}}}`,
			want:  guard.ToolCall{Tool: "Write", Paths: []string{"/repo/src/d.test.ts"}, Cwd: "/repo"},
		},
		{
			name:  "create_file{FilePath}",
			input: `{"cwd":"/repo","toolCall":{"id":"t9","name":"create_file","args":{"FilePath":"src/new.ts"}}}`,
			want:  guard.ToolCall{Tool: "Write", Paths: []string{"src/new.ts"}, Cwd: "/repo"},
		},
		{
			name:  "read_file{Path} has no objection",
			input: `{"cwd":"/repo","hookEventName":"PreToolUse","toolCall":{"id":"t10","name":"read_file","args":{"Path":"src/a.test.ts"}}}`,
			want:  guard.ToolCall{Tool: "read_file", Cwd: "/repo"},
		},
		{
			name:  "unknown tool has no objection",
			input: `{"cwd":"/repo","hookEventName":"PreToolUse","toolCall":{"id":"t11","name":"grep_search","args":{"Path":"src/a.test.ts","Query":"test"}}}`,
			want:  guard.ToolCall{Tool: "grep_search", Cwd: "/repo"},
		},
		{
			name:    "invalid JSON",
			input:   `{bad`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePreToolUse(strings.NewReader(tc.input))
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

	t.Run("allow writes decision ask and never allow", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WritePreToolUse(&buf, guard.Decision{Allow: true}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `{"decision":"ask"}` + "\n"
		if buf.String() != want {
			t.Errorf("got %q, want %q", buf.String(), want)
		}
		if strings.Contains(buf.String(), "allow") {
			t.Errorf("output must never contain 'allow': %q", buf.String())
		}
	})

	t.Run("deny writes decision deny with reason", func(t *testing.T) {
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
		want := `{"decision":"deny","reason":"donegate: src/cart.test.ts is a protected test file (T001)."}` + "\n"
		if buf.String() != want {
			t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
		}
	})
}

func TestParseAndWriteStop(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    StopInput
		wantErr bool
	}{
		{
			name: "payload from hooks.md",
			input: `{
				"executionNum": 1,
				"terminationReason": "NO_TOOL_CALL",
				"error": "",
				"fullyIdle": true,
				"continuationActive": true,
				"conversationId": "ec33ebf9-0cba-4100-8142-c61503f6c587",
				"workspacePaths": ["/path/to/workspace"],
				"hookEventName": "Stop",
				"cwd": "/path/to/workspace"
			}`,
			want: StopInput{
				ConversationID:     "ec33ebf9-0cba-4100-8142-c61503f6c587",
				Cwd:                "/path/to/workspace",
				WorkspacePaths:     []string{"/path/to/workspace"},
				HookEventName:      "Stop",
				TerminationReason:  "NO_TOOL_CALL",
				ContinuationActive: true,
				ExecutionNum:       1,
			},
		},
		{
			name:  "falls back to workspacePaths[0]",
			input: `{"conversationId":"c1","workspacePaths":["/ws"],"terminationReason":"USER_CANCELED"}`,
			want: StopInput{
				ConversationID:    "c1",
				Cwd:               "/ws",
				WorkspacePaths:    []string{"/ws"},
				TerminationReason: "USER_CANCELED",
			},
		},
		{
			name:    "invalid JSON",
			input:   `{bad`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseStop(strings.NewReader(tc.input))
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

	t.Run("block writes decision continue JSON", func(t *testing.T) {
		var outBuf, errBuf bytes.Buffer
		err := WriteStop(&outBuf, &errBuf, &gate.Outcome{
			Block:   true,
			Message: "donegate: not done (1 failure, attempt 1/5)",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `{"decision":"continue","reason":"donegate: not done (1 failure, attempt 1/5)"}` + "\n"
		if outBuf.String() != want {
			t.Errorf("got:\n%s\nwant:\n%s", outBuf.String(), want)
		}
		if errBuf.Len() != 0 {
			t.Errorf("expected empty stderr, got %q", errBuf.String())
		}
	})

	t.Run("allow with UserMessage writes {} to stdout and UserMessage to stderr", func(t *testing.T) {
		var outBuf, errBuf bytes.Buffer
		err := WriteStop(&outBuf, &errBuf, &gate.Outcome{
			Block:       false,
			UserMessage: "donegate: verified (0 warnings). Evidence: /tmp/evidence.json",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outBuf.String() != "{}\n" {
			t.Errorf("stdout = %q, want %q", outBuf.String(), "{}\n")
		}
		wantErr := "donegate: verified (0 warnings). Evidence: /tmp/evidence.json\n"
		if errBuf.String() != wantErr {
			t.Errorf("stderr = %q, want %q", errBuf.String(), wantErr)
		}
	})

	t.Run("allow without UserMessage writes {} and empty stderr", func(t *testing.T) {
		var outBuf, errBuf bytes.Buffer
		if err := WriteStop(&outBuf, &errBuf, &gate.Outcome{Block: false}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outBuf.String() != "{}\n" {
			t.Errorf("stdout = %q, want %q", outBuf.String(), "{}\n")
		}
		if errBuf.Len() != 0 {
			t.Errorf("expected empty stderr, got %q", errBuf.String())
		}
	})

	t.Run("nil outcome writes {}", func(t *testing.T) {
		var outBuf, errBuf bytes.Buffer
		if err := WriteStop(&outBuf, &errBuf, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outBuf.String() != "{}\n" {
			t.Errorf("stdout = %q, want %q", outBuf.String(), "{}\n")
		}
	})
}
