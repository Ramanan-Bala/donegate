package session

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/diff"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeRepoFile(t *testing.T, dir, rel string, content []byte) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", full, err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

func initCommitRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "-c", "init.defaultBranch=main", "init")
	writeRepoFile(t, dir, "src/app.ts", []byte("export const x = 1;\n"))
	writeRepoFile(t, dir, "src/app.test.ts", []byte("test('x', () => expect(1).toBe(1));\n"))
	writeRepoFile(t, dir, "vitest.config.ts", []byte("export default {};\n"))
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "initial")
	head := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
	return dir, head
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestStateDir(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom-home")
	t.Setenv("DONEGATE_HOME", custom)

	got, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir failed: %v", err)
	}
	if got != custom {
		t.Fatalf("StateDir = %q, want %q", got, custom)
	}

	t.Setenv("DONEGATE_HOME", "")
	gotDefault, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir default failed: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	wantDefault := filepath.Join(home, ".donegate")
	if gotDefault != wantDefault {
		t.Fatalf("StateDir default = %q, want %q", gotDefault, wantDefault)
	}
}

func TestStartCleanRepo(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("DONEGATE_HOME", stateDir)

	dir, head := initCommitRepo(t)
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	sess, err := Start(dir, "claude-code")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if sess.Repo != resolvedDir {
		t.Errorf("Repo = %q, want %q", sess.Repo, resolvedDir)
	}
	if sess.Head != head {
		t.Errorf("Head = %q, want %q", sess.Head, head)
	}
	if sess.Base == "" {
		t.Fatal("Base is empty")
	}
	if sess.Harness != "claude-code" {
		t.Errorf("Harness = %q, want claude-code", sess.Harness)
	}

	refSHA := strings.TrimSpace(runGit(t, dir, "rev-parse", "--verify", "refs/donegate/sessions/"+sess.ID))
	if refSHA != sess.Base {
		t.Errorf("pinned ref = %q, want %q", refSHA, sess.Base)
	}

	wantProtected := map[string]string{
		"src/app.test.ts":  sha256Hex([]byte("test('x', () => expect(1).toBe(1));\n")),
		"vitest.config.ts": sha256Hex([]byte("export default {};\n")),
	}
	if !reflect.DeepEqual(sess.Protected, wantProtected) {
		t.Errorf("Protected = %v, want %v", sess.Protected, wantProtected)
	}

	info, err := os.Stat(sess.Dir())
	if err != nil {
		t.Fatalf("stat session dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("session dir perm = %o, want 700", perm)
	}
	fileInfo, err := os.Stat(filepath.Join(sess.Dir(), "session.json"))
	if err != nil {
		t.Fatalf("stat session.json: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("session.json perm = %o, want 600", perm)
	}
}

func TestStartDirtyRepoPreservesIndexHeadAndBranches(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("DONEGATE_HOME", stateDir)

	dir, head := initCommitRepo(t)
	writeRepoFile(t, dir, "src/staged.ts", []byte("export const staged = 1;\n"))
	runGit(t, dir, "add", "src/staged.ts")

	// Uncommitted modification + untracked file on top of a staged change.
	writeRepoFile(t, dir, "src/app.ts", []byte("export const x = 2;\n"))
	writeRepoFile(t, dir, "src/untracked.ts", []byte("export const u = 3;\n"))

	cachedBefore := runGit(t, dir, "diff", "--cached", "--name-only")
	branchesBefore := runGit(t, dir, "branch", "--list")

	sess, err := Start(dir, "manual")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if got := runGit(t, dir, "diff", "--cached", "--name-only"); got != cachedBefore {
		t.Errorf("real index changed: got %q, want %q", got, cachedBefore)
	}
	if got := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD")); got != head {
		t.Errorf("HEAD changed: got %q, want %q", got, head)
	}
	if got := runGit(t, dir, "branch", "--list"); got != branchesBefore {
		t.Errorf("branches changed: got %q, want %q", got, branchesBefore)
	}

	if got := runGit(t, dir, "show", sess.Base+":src/app.ts"); got != "export const x = 2;\n" {
		t.Errorf("Base src/app.ts = %q", got)
	}
	if got := runGit(t, dir, "show", sess.Base+":src/untracked.ts"); got != "export const u = 3;\n" {
		t.Errorf("Base src/untracked.ts = %q", got)
	}
	if got := runGit(t, dir, "show", sess.Base+":src/staged.ts"); got != "export const staged = 1;\n" {
		t.Errorf("Base src/staged.ts = %q", got)
	}
}

func TestDiffLoadAgainstSessionBase(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("DONEGATE_HOME", stateDir)

	dir, _ := initCommitRepo(t)
	// Pre-existing dirty modification to a tracked file before session start.
	writeRepoFile(t, dir, "src/app.ts", []byte("export const x = 2;\n"))

	sess, err := Start(dir, "manual")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Make a further edit after session start; diff against Base must report only that edit.
	writeRepoFile(t, dir, "src/after.ts", []byte("export const after = true;\n"))
	d, err := diff.Load(dir, sess.Base)
	if err != nil {
		t.Fatalf("diff.Load failed: %v", err)
	}
	if len(d.Files) != 1 || d.Files[0].Path != "src/after.ts" {
		t.Fatalf("diff files = %+v, want only src/after.ts", d.Files)
	}
}

func TestStartNoCommitsRepo(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("DONEGATE_HOME", stateDir)

	dir := t.TempDir()
	runGit(t, dir, "-c", "init.defaultBranch=main", "init")
	writeRepoFile(t, dir, "src/init.ts", []byte("init\n"))
	writeRepoFile(t, dir, "src/init.test.ts", []byte("test\n"))

	sess, err := Start(dir, "manual")
	if err != nil {
		t.Fatalf("Start on unborn repo failed: %v", err)
	}
	if sess.Head != "" {
		t.Errorf("Head = %q, want empty", sess.Head)
	}
	if sess.Base == "" {
		t.Fatal("Base is empty")
	}

	parents := strings.Fields(runGit(t, dir, "rev-list", "--parents", "-n", "1", sess.Base))
	if len(parents) != 1 || parents[0] != sess.Base {
		t.Errorf("expected root commit with no parents, got %v", parents)
	}
	if got := runGit(t, dir, "show", sess.Base+":src/init.ts"); got != "init\n" {
		t.Errorf("Base src/init.ts = %q", got)
	}
}

func TestPolicySnapshotIsolation(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("DONEGATE_HOME", stateDir)

	dir, _ := initCommitRepo(t)
	origPolicy := "version: 1\ngate:\n  tier: strict\nrules:\n  T001: warn\n"
	writeRepoFile(t, dir, ".donegate.yml", []byte(origPolicy))

	sess, err := Start(dir, "manual")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if sess.PolicySHA256 != sha256Hex([]byte(origPolicy)) {
		t.Errorf("PolicySHA256 = %q, want %q", sess.PolicySHA256, sha256Hex([]byte(origPolicy)))
	}
	if _, ok := sess.Protected[".donegate.yml"]; !ok {
		t.Errorf("expected .donegate.yml in Protected")
	}

	// Edit working-tree .donegate.yml; Policy() must still read the snapshot.
	writeRepoFile(t, dir, ".donegate.yml", []byte("version: 1\ngate:\n  tier: fast\nrules:\n  T001: off\n"))

	pol, err := sess.Policy()
	if err != nil {
		t.Fatalf("Policy failed: %v", err)
	}
	if pol.Gate.Tier != "strict" {
		t.Errorf("Gate.Tier = %q, want strict", pol.Gate.Tier)
	}
	if pol.Rules["T001"] != "warn" {
		t.Errorf("Rules[T001] = %q, want warn", pol.Rules["T001"])
	}

	t.Run("invalid policy fails Start", func(t *testing.T) {
		badDir, _ := initCommitRepo(t)
		writeRepoFile(t, badDir, ".donegate.yml", []byte("version: 99\n"))
		if _, err := Start(badDir, "manual"); err == nil {
			t.Fatal("expected error for invalid .donegate.yml, got nil")
		}
	})
}

func TestCheckProtected(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("DONEGATE_HOME", stateDir)

	dir, _ := initCommitRepo(t)
	sess, err := Start(dir, "manual")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	mismatches, err := sess.CheckProtected()
	if err != nil {
		t.Fatalf("CheckProtected clean failed: %v", err)
	}
	if len(mismatches) != 0 {
		t.Fatalf("expected no mismatches, got %+v", mismatches)
	}

	modifiedContent := []byte("test('x', () => expect(1).toBe(2));\n")
	writeRepoFile(t, dir, "src/app.test.ts", modifiedContent)
	if err := os.Remove(filepath.Join(dir, "vitest.config.ts")); err != nil {
		t.Fatal(err)
	}

	mismatches, err = sess.CheckProtected()
	if err != nil {
		t.Fatalf("CheckProtected dirty failed: %v", err)
	}

	want := []HashMismatch{
		{
			Path:     "src/app.test.ts",
			Expected: sess.Protected["src/app.test.ts"],
			Actual:   sha256Hex(modifiedContent),
		},
		{
			Path:     "vitest.config.ts",
			Expected: sess.Protected["vitest.config.ts"],
			Actual:   "",
		},
	}
	if !reflect.DeepEqual(mismatches, want) {
		t.Errorf("mismatches = %+v, want %+v", mismatches, want)
	}
}

func TestCurrentAndLoad(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("DONEGATE_HOME", stateDir)

	unknownRepo, _ := initCommitRepo(t)
	if _, err := Current(unknownRepo); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Current on unknown repo err = %v, want ErrNoSession", err)
	}

	dir, _ := initCommitRepo(t)
	tests := []struct {
		name    string
		harness string
	}{
		{name: "first", harness: "claude-code"},
		{name: "second", harness: "opencode"},
	}

	var sessions []*Session
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Start(dir, tc.harness)
			if err != nil {
				t.Fatalf("Start failed: %v", err)
			}
			sessions = append(sessions, s)
		})
	}

	latest := sessions[len(sessions)-1]
	cur, err := Current(filepath.Join(dir, "src"))
	if err != nil {
		t.Fatalf("Current failed: %v", err)
	}
	if cur.ID != latest.ID {
		t.Errorf("Current ID = %q, want %q", cur.ID, latest.ID)
	}

	firstLoaded, err := Load(sessions[0].ID)
	if err != nil {
		t.Fatalf("Load first session failed: %v", err)
	}
	if firstLoaded.Harness != "claude-code" {
		t.Errorf("first session Harness = %q, want claude-code", firstLoaded.Harness)
	}
}

func TestStartWithAndPolicyBytes(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("DONEGATE_HOME", stateDir)

	dir, _ := initCommitRepo(t)
	origPolicy := "version: 1\ngate:\n  tier: fast\n"
	writeRepoFile(t, dir, ".donegate.yml", []byte(origPolicy))

	sess, err := StartWith(dir, StartOptions{
		Harness:        "claude-code",
		HarnessSession: "claude-sess-42",
	})
	if err != nil {
		t.Fatalf("StartWith failed: %v", err)
	}
	if sess.Harness != "claude-code" {
		t.Errorf("Harness = %q, want claude-code", sess.Harness)
	}
	if sess.HarnessSession != "claude-sess-42" {
		t.Errorf("HarnessSession = %q, want claude-sess-42", sess.HarnessSession)
	}

	cur, err := Current(dir)
	if err != nil {
		t.Fatalf("Current failed: %v", err)
	}
	if cur.HarnessSession != "claude-sess-42" {
		t.Errorf("loaded HarnessSession = %q, want claude-sess-42", cur.HarnessSession)
	}

	raw, err := cur.PolicyBytes()
	if err != nil {
		t.Fatalf("PolicyBytes failed: %v", err)
	}
	if string(raw) != origPolicy {
		t.Errorf("PolicyBytes = %q, want %q", string(raw), origPolicy)
	}
}
