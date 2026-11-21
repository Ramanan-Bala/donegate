package claude

import (
	"strings"
	"testing"
)

func TestParsePreToolUseWithSession(t *testing.T) {
	input := `{"session_id":"claude-sess-9","cwd":"/repo","hook_event_name":"PreToolUse","tool_name":"Edit","tool_input":{"file_path":"src/a.test.ts"}}`
	call, sessID, err := ParsePreToolUseWithSession(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParsePreToolUseWithSession: %v", err)
	}
	if sessID != "claude-sess-9" {
		t.Errorf("session_id = %q, want claude-sess-9", sessID)
	}
	if call.Tool != "Edit" || call.Cwd != "/repo" || len(call.Paths) != 1 || call.Paths[0] != "src/a.test.ts" {
		t.Errorf("unexpected call: %+v", call)
	}
}
