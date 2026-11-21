package agy

import (
	"strings"
	"testing"
)

func TestParsePreToolUseWithSession(t *testing.T) {
	input := `{"conversationId":"conv-77","cwd":"/repo","hookEventName":"PreToolUse","toolCall":{"id":"t1","name":"edit_file","args":{"Path":"src/a.test.ts"}}}`
	call, convID, err := ParsePreToolUseWithSession(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParsePreToolUseWithSession: %v", err)
	}
	if convID != "conv-77" {
		t.Errorf("conversationId = %q, want conv-77", convID)
	}
	if call.Tool != "Write" || call.Cwd != "/repo" || len(call.Paths) != 1 || call.Paths[0] != "src/a.test.ts" {
		t.Errorf("unexpected call: %+v", call)
	}
}
