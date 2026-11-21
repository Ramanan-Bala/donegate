package claude

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestParseSessionStart(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    SessionStartInput
		wantErr bool
	}{
		{
			name:  "valid SessionStart payload",
			input: `{"session_id":"s1","cwd":"/repo","source":"startup"}`,
			want:  SessionStartInput{SessionID: "s1", Cwd: "/repo", Source: "startup"},
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
}

func TestWriteSessionStart(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteSessionStart(&buf, "donegate is active"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"donegate is active"}}` + "\n"
	if buf.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
}
