package evidence

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAllowedInEvidenceAndMarkdown(t *testing.T) {
	in := Input{
		Session: "s1",
		Harness: "claude-code",
		Repo:    "/tmp/repo",
		Base:    "1111111",
		Head:    "2222222",
		Tier:    "standard",
		Blocks:  0,
		RuleIDs: []string{"H001", "T001"},
		Allowed: []AllowedFinding{
			{
				Rule: "T001",
				Path: "src/cart.test.ts",
				By:   "alice",
				At:   "2026-11-21T14:00:00Z",
			},
		},
		Now: fixedTime(),
	}

	ev := Build(in)
	if ev.Schema != Schema {
		t.Errorf("Schema = %q, want %q", ev.Schema, Schema)
	}
	if ev.Verdict != "PASS" {
		t.Errorf("Verdict = %q, want PASS", ev.Verdict)
	}
	if err := ev.Verify(); err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"allowed":[`) {
		t.Errorf("expected allowed field in JSON: %s", raw)
	}

	// Tampering with Allowed must fail Verify.
	ev.Allowed[0].By = "mallory"
	if err := ev.Verify(); err == nil {
		t.Fatal("Verify succeeded after tampering with Allowed")
	}

	md := Markdown(Build(in))
	if !strings.Contains(md, "### Approved exceptions") {
		t.Errorf("Markdown missing Approved exceptions section:\n%s", md)
	}
	if !strings.Contains(md, "- `T001` src/cart.test.ts (approved by alice at 2026-11-21T14:00:00Z)") {
		t.Errorf("Markdown missing formatted allowed bullet:\n%s", md)
	}
}
