package allow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ramanan-Bala/donegate/internal/session"
)

func initTestSession(t *testing.T) (*session.Session, string) {
	t.Helper()
	t.Setenv("DONEGATE_HOME", t.TempDir())
	repo := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("-c", "init.defaultBranch=main", "init")
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "a.test.ts"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "init")

	sess, err := session.Start(repo, "manual")
	if err != nil {
		t.Fatalf("session.Start: %v", err)
	}
	return sess, repo
}

func TestAddListRevokeAndValidation(t *testing.T) {
	sess, repo := initTestSession(t)

	// Empty initially.
	list, err := List(sess)
	if err != nil {
		t.Fatalf("List empty: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %+v", list)
	}

	// Invalid rule rejected.
	for _, badRule := range []string{"", "H001", "R001", "T003", "T005", "bogus"} {
		if err := Add(sess, badRule, []string{"src/a.test.ts"}, "alice"); err == nil {
			t.Errorf("Add with invalid rule %q succeeded, want error", badRule)
		}
	}

	// Paths outside repo rejected.
	outside := filepath.Join(t.TempDir(), "out.test.ts")
	for _, badPath := range []string{"", ".", "../outside.test.ts", outside} {
		if err := Add(sess, "T001", []string{badPath}, "alice"); err == nil {
			t.Errorf("Add with outside path %q succeeded, want error", badPath)
		}
	}

	// Symlink escape rejected.
	outsideDir := t.TempDir()
	linkPath := filepath.Join(repo, "escape")
	if err := os.Symlink(outsideDir, linkPath); err == nil {
		if err := Add(sess, "T001", []string{"escape/evil.test.ts"}, "alice"); err == nil {
			t.Errorf("Add through symlink escape succeeded, want error")
		}
	}

	// Valid rules + path normalization (relative with ./, .., and absolute inside repo).
	absInside := filepath.Join(repo, "src", "b.test.ts")
	if err := Add(sess, "T001", []string{"./src/../src/a.test.ts", absInside}, "alice"); err != nil {
		t.Fatalf("Add T001: %v", err)
	}
	if err := Add(sess, "all", []string{"vitest.config.ts"}, "bob"); err != nil {
		t.Fatalf("Add all: %v", err)
	}

	// Verify file permissions and JSON schema.
	allowPath := filepath.Join(sess.Dir(), "allow.json")
	info, err := os.Stat(allowPath)
	if err != nil {
		t.Fatalf("stat allow.json: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("allow.json perm = %o, want 600", perm)
	}
	raw, err := os.ReadFile(allowPath)
	if err != nil {
		t.Fatalf("read allow.json: %v", err)
	}
	var decoded []Allowance
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal allow.json: %v", err)
	}
	if len(decoded) != 3 {
		t.Fatalf("decoded len = %d, want 3 (%+v)", len(decoded), decoded)
	}
	for _, entry := range decoded {
		if _, err := time.Parse(time.RFC3339, entry.At); err != nil {
			t.Errorf("At %q is not RFC3339: %v", entry.At, err)
		}
		if strings.HasPrefix(entry.Path, "/") || strings.HasPrefix(entry.Path, "./") {
			t.Errorf("Path %q not normalized to repo-relative slash form", entry.Path)
		}
	}

	// Re-adding same (rule, path) deduplicates.
	if err := Add(sess, "T001", []string{"src/a.test.ts"}, "carol"); err != nil {
		t.Fatalf("re-Add T001: %v", err)
	}
	list, err = List(sess)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len after deduplicate = %d, want 3", len(list))
	}
	if list[0].By != "carol" {
		t.Errorf("re-added entry By = %q, want carol", list[0].By)
	}

	// Revoke using absolute or ./ path removes matching entry.
	if err := Revoke(sess, []string{"./src/b.test.ts"}); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	list, err = List(sess)
	if err != nil {
		t.Fatalf("List after revoke: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len after revoke = %d, want 2 (%+v)", len(list), list)
	}
	for _, e := range list {
		if e.Path == "src/b.test.ts" {
			t.Errorf("src/b.test.ts still present after revoke")
		}
	}
}

func TestAllows(t *testing.T) {
	list := []Allowance{
		{Rule: "T001", Path: "src/a.test.ts", By: "alice", At: "2026-11-21T00:00:00Z"},
		{Rule: "all", Path: "src/any.test.ts", By: "bob", At: "2026-11-21T00:00:00Z"},
	}

	tests := []struct {
		name   string
		ruleID string
		path   string
		want   bool
	}{
		{name: "exact rule match", ruleID: "T001", path: "src/a.test.ts", want: true},
		{name: "normalized ./ path match", ruleID: "T001", path: "./src/a.test.ts", want: true},
		{name: "allowance for R also allows H001", ruleID: "H001", path: "src/a.test.ts", want: true},
		{name: "different rule not allowed", ruleID: "T002", path: "src/a.test.ts", want: false},
		{name: "different path not allowed", ruleID: "T001", path: "src/other.test.ts", want: false},
		{name: "all allows T001", ruleID: "T001", path: "src/any.test.ts", want: true},
		{name: "all allows T002", ruleID: "T002", path: "src/any.test.ts", want: true},
		{name: "all allows T003", ruleID: "T003", path: "src/any.test.ts", want: true},
		{name: "all allows C001", ruleID: "C001", path: "src/any.test.ts", want: true},
		{name: "all allows H001", ruleID: "H001", path: "src/any.test.ts", want: true},
		{name: "empty path never allowed even with all", ruleID: "T004", path: "", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Allows(list, tc.ruleID, tc.path); got != tc.want {
				t.Errorf("Allows(%q, %q) = %v, want %v", tc.ruleID, tc.path, got, tc.want)
			}
		})
	}
}
