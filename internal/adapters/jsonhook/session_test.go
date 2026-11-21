package jsonhook

import (
	"strings"
	"testing"
)

func TestParseGuardWithSession(t *testing.T) {
	t.Run("extracts session when present", func(t *testing.T) {
		input := `{"tool":"edit","paths":["src/a.test.ts"],"cwd":"/repo","session":"oc-42"}`
		call, sess, err := ParseGuardWithSession(strings.NewReader(input))
		if err != nil {
			t.Fatalf("ParseGuardWithSession: %v", err)
		}
		if sess != "oc-42" {
			t.Errorf("session = %q, want oc-42", sess)
		}
		if call.Tool != "Write" || call.Cwd != "/repo" {
			t.Errorf("unexpected call: %+v", call)
		}
	})

	t.Run("missing session yields empty string", func(t *testing.T) {
		input := `{"tool":"edit","paths":["src/a.test.ts"],"cwd":"/repo"}`
		_, sess, err := ParseGuardWithSession(strings.NewReader(input))
		if err != nil {
			t.Fatalf("ParseGuardWithSession: %v", err)
		}
		if sess != "" {
			t.Errorf("session = %q, want empty", sess)
		}
	})
}
