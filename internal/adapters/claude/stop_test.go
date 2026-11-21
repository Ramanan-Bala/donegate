package claude

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/gate"
)

func TestParseStop(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    StopInput
		wantErr bool
	}{
		{
			name:  "valid Stop payload",
			input: `{"session_id":"s1","cwd":"/repo","hook_event_name":"Stop","stop_hook_active":true}`,
			want:  StopInput{SessionID: "s1", Cwd: "/repo", HookEventName: "Stop", StopHookActive: true},
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
}

func TestWriteStop(t *testing.T) {
	t.Run("block writes decision block JSON", func(t *testing.T) {
		var buf bytes.Buffer
		err := WriteStop(&buf, &gate.Outcome{
			Block:   true,
			Message: "donegate: not done (1 failure, attempt 1/5)",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `{"decision":"block","reason":"donegate: not done (1 failure, attempt 1/5)"}` + "\n"
		if buf.String() != want {
			t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
		}
	})

	t.Run("allow with UserMessage writes systemMessage JSON", func(t *testing.T) {
		var buf bytes.Buffer
		err := WriteStop(&buf, &gate.Outcome{
			Block:       false,
			UserMessage: "donegate: verified (0 warnings). Evidence: /tmp/evidence.json",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `{"systemMessage":"donegate: verified (0 warnings). Evidence: /tmp/evidence.json"}` + "\n"
		if buf.String() != want {
			t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
		}
	})

	t.Run("allow without UserMessage writes nothing", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteStop(&buf, &gate.Outcome{Block: false}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if buf.Len() != 0 {
			t.Errorf("expected empty output, got %q", buf.String())
		}
	})
}
