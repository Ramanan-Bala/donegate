package diff

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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

func initTestRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "-c", "init.defaultBranch=main", "init")
	writeRepoFile(t, dir, "src/app.ts", []byte("line1\nline2\nline3\n"))
	writeRepoFile(t, dir, "src/old.ts", []byte("old1\nold2\nold3\nold4\nold5\nold6\n"))
	writeRepoFile(t, dir, "src/gone.ts", []byte("gone1\ngone2\n"))
	writeRepoFile(t, dir, "assets/logo.bin", []byte{0x00, 0x01, 0x02, 0x03})
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "base")
	sha := runGit(t, dir, "rev-parse", "HEAD")
	return dir, string(bytes.TrimSpace([]byte(sha)))
}

func TestLoad(t *testing.T) {
	dir, baseSHA := initTestRepo(t)

	// Committed changes: added file, deleted file, renamed+modified file, binary change.
	writeRepoFile(t, dir, "src/added_committed.ts", []byte("a1\na2\n"))
	if err := os.Remove(filepath.Join(dir, "src/gone.ts")); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "mv", "src/old.ts", "src/renamed.ts")
	writeRepoFile(t, dir, "src/renamed.ts", []byte("old1\nold2-edited\nold3\nold4\nold5\nold6\n"))
	writeRepoFile(t, dir, "assets/logo.bin", []byte{0x00, 0x01, 0x02, 0x04, 0x05})
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "changes")

	// Uncommitted working-tree modification and untracked file.
	writeRepoFile(t, dir, "src/app.ts", []byte("line1\nline2-changed\nline3\nline4\n"))
	writeRepoFile(t, dir, "src/untracked.ts", []byte("u1\nu2\n"))

	d, err := Load(dir, "HEAD~1")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if d.Base != baseSHA {
		t.Fatalf("Base = %q, want %q", d.Base, baseSHA)
	}

	wantOrder := []string{
		"assets/logo.bin",
		"src/added_committed.ts",
		"src/app.ts",
		"src/gone.ts",
		"src/renamed.ts",
		"src/untracked.ts",
	}
	var gotOrder []string
	for _, f := range d.Files {
		gotOrder = append(gotOrder, f.Path)
	}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("Files order = %v, want %v", gotOrder, wantOrder)
	}

	tests := []struct {
		name        string
		path        string
		wantStatus  Status
		wantOldPath string
		wantBinary  bool
		wantAdded   []Line
		wantRemoved []Line
	}{
		{
			name:        "uncommitted modification",
			path:        "src/app.ts",
			wantStatus:  Modified,
			wantAdded:   []Line{{No: 2, Text: "line2-changed"}, {No: 4, Text: "line4"}},
			wantRemoved: []Line{{No: 2, Text: "line2"}},
		},
		{
			name:       "committed addition",
			path:       "src/added_committed.ts",
			wantStatus: Added,
			wantAdded:  []Line{{No: 1, Text: "a1"}, {No: 2, Text: "a2"}},
		},
		{
			name:       "untracked addition",
			path:       "src/untracked.ts",
			wantStatus: Added,
			wantAdded:  []Line{{No: 1, Text: "u1"}, {No: 2, Text: "u2"}},
		},
		{
			name:        "committed deletion",
			path:        "src/gone.ts",
			wantStatus:  Deleted,
			wantRemoved: []Line{{No: 1, Text: "gone1"}, {No: 2, Text: "gone2"}},
		},
		{
			name:        "committed rename with edit",
			path:        "src/renamed.ts",
			wantStatus:  Renamed,
			wantOldPath: "src/old.ts",
			wantAdded:   []Line{{No: 2, Text: "old2-edited"}},
			wantRemoved: []Line{{No: 2, Text: "old2"}},
		},
		{
			name:       "binary file",
			path:       "assets/logo.bin",
			wantStatus: Modified,
			wantBinary: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := d.File(tc.path)
			if f == nil {
				t.Fatalf("File(%q) = nil", tc.path)
			}
			if f.Status != tc.wantStatus {
				t.Errorf("Status = %q, want %q", f.Status, tc.wantStatus)
			}
			if f.OldPath != tc.wantOldPath {
				t.Errorf("OldPath = %q, want %q", f.OldPath, tc.wantOldPath)
			}
			if f.Binary != tc.wantBinary {
				t.Errorf("Binary = %v, want %v", f.Binary, tc.wantBinary)
			}
			if !reflect.DeepEqual(f.Added, tc.wantAdded) {
				t.Errorf("Added = %+v, want %+v", f.Added, tc.wantAdded)
			}
			if !reflect.DeepEqual(f.Removed, tc.wantRemoved) {
				t.Errorf("Removed = %+v, want %+v", f.Removed, tc.wantRemoved)
			}
		})
	}

	// Verify ReadBase and ReadHead.
	baseData, err := d.ReadBase("src/app.ts")
	if err != nil {
		t.Fatalf("ReadBase failed: %v", err)
	}
	if string(baseData) != "line1\nline2\nline3\n" {
		t.Errorf("ReadBase = %q, want %q", string(baseData), "line1\nline2\nline3\n")
	}
	headData, err := d.ReadHead("src/app.ts")
	if err != nil {
		t.Fatalf("ReadHead failed: %v", err)
	}
	if string(headData) != "line1\nline2-changed\nline3\nline4\n" {
		t.Errorf("ReadHead = %q", string(headData))
	}
}

func TestLoadBadBaseRef(t *testing.T) {
	dir, _ := initTestRepo(t)
	if _, err := Load(dir, "refs/heads/does-not-exist"); err == nil {
		t.Fatal("expected error for nonexistent base ref, got nil")
	}
}

func runGitEnv(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func commitWithTempIndex(t *testing.T, dir, parent string) string {
	t.Helper()
	realIdx := strings.TrimSpace(runGit(t, dir, "rev-parse", "--git-path", "index"))
	if !filepath.IsAbs(realIdx) {
		realIdx = filepath.Join(dir, realIdx)
	}
	tmpIdx := filepath.Join(t.TempDir(), "index")
	if data, err := os.ReadFile(realIdx); err == nil {
		if err := os.WriteFile(tmpIdx, data, 0o600); err != nil {
			t.Fatalf("write temp index: %v", err)
		}
	}
	idxEnv := []string{"GIT_INDEX_FILE=" + tmpIdx}
	runGitEnv(t, dir, idxEnv, "add", "-A")
	treeSHA := strings.TrimSpace(runGitEnv(t, dir, idxEnv, "write-tree"))
	commitEnv := []string{
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	}
	return strings.TrimSpace(runGitEnv(t, dir, commitEnv, "-c", "commit.gpgsign=false", "commit-tree", treeSHA, "-p", parent, "-m", "temp-index base"))
}

func TestLoadBaseIncludesFileAbsentFromRealIndex(t *testing.T) {
	dir, head := initTestRepo(t)
	writeRepoFile(t, dir, "src/untracked.ts", []byte("u1\nu2\nu3\n"))

	baseSHA := commitWithTempIndex(t, dir, head)

	d, err := Load(dir, baseSHA)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(d.Files) != 0 {
		t.Fatalf("Files = %+v, want no changes", d.Files)
	}

	writeRepoFile(t, dir, "src/untracked.ts", []byte("u1\nu2-edited\nu3\nu4\n"))
	d, err = Load(dir, baseSHA)
	if err != nil {
		t.Fatalf("Load after modify failed: %v", err)
	}
	if len(d.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1 (%+v)", len(d.Files), d.Files)
	}
	f := d.File("src/untracked.ts")
	if f == nil {
		t.Fatalf("File(src/untracked.ts) = nil")
	}
	if f.Status != Modified {
		t.Errorf("Status = %q, want %q", f.Status, Modified)
	}
	wantAdded := []Line{{No: 2, Text: "u2-edited"}, {No: 4, Text: "u4"}}
	wantRemoved := []Line{{No: 2, Text: "u2"}}
	if !reflect.DeepEqual(f.Added, wantAdded) {
		t.Errorf("Added = %+v, want %+v", f.Added, wantAdded)
	}
	if !reflect.DeepEqual(f.Removed, wantRemoved) {
		t.Errorf("Removed = %+v, want %+v", f.Removed, wantRemoved)
	}
}

func TestLoadPreservesRealIndex(t *testing.T) {
	dir, baseSHA := initTestRepo(t)
	writeRepoFile(t, dir, "src/staged.ts", []byte("s1\n"))
	runGit(t, dir, "add", "src/staged.ts")
	writeRepoFile(t, dir, "src/app.ts", []byte("line1\nline2-dirty\nline3\n"))
	writeRepoFile(t, dir, "src/untracked.ts", []byte("u1\n"))

	cachedBefore := runGit(t, dir, "diff", "--cached", "--name-only")
	lsBefore := runGit(t, dir, "ls-files")

	if _, err := Load(dir, baseSHA); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	cachedAfter := runGit(t, dir, "diff", "--cached", "--name-only")
	lsAfter := runGit(t, dir, "ls-files")
	if cachedAfter != cachedBefore {
		t.Errorf("git diff --cached --name-only changed: got %q, want %q", cachedAfter, cachedBefore)
	}
	if lsAfter != lsBefore {
		t.Errorf("git ls-files changed: got %q, want %q", lsAfter, lsBefore)
	}
}

func TestLoadStagedFileDeletedFromDisk(t *testing.T) {
	dir, baseSHA := initTestRepo(t)
	writeRepoFile(t, dir, "src/app.ts", []byte("staged-change\n"))
	runGit(t, dir, "add", "src/app.ts")
	if err := os.Remove(filepath.Join(dir, "src/app.ts")); err != nil {
		t.Fatal(err)
	}

	d, err := Load(dir, baseSHA)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	f := d.File("src/app.ts")
	if f == nil {
		t.Fatalf("File(src/app.ts) = nil")
	}
	if f.Status != Deleted {
		t.Errorf("Status = %q, want %q", f.Status, Deleted)
	}
	wantRemoved := []Line{
		{No: 1, Text: "line1"},
		{No: 2, Text: "line2"},
		{No: 3, Text: "line3"},
	}
	if !reflect.DeepEqual(f.Removed, wantRemoved) {
		t.Errorf("Removed = %+v, want %+v", f.Removed, wantRemoved)
	}
}

func TestLoadSameSizeSameSecondEdit(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "-c", "init.defaultBranch=main", "init")
	// Disable trustctime so ctime changes from os.WriteFile/os.Chtimes across a
	// second boundary cannot mask the mtime-based racy-git condition.
	runGit(t, dir, "config", "core.trustctime", "false")

	ts := time.Now().Add(-5 * time.Second).Truncate(time.Second)
	appPath := filepath.Join(dir, "src", "app.ts")
	writeRepoFile(t, dir, "src/app.ts", []byte("export const x = 1;\n"))
	if err := os.Chtimes(appPath, ts, ts); err != nil {
		t.Fatalf("chtimes before commit: %v", err)
	}

	runGit(t, dir, "add", "-A")
	runGit(t, dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "base")
	baseSHA := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))

	realIdx := strings.TrimSpace(runGit(t, dir, "rev-parse", "--git-path", "index"))
	if !filepath.IsAbs(realIdx) {
		realIdx = filepath.Join(dir, realIdx)
	}

	// Rewrite with different content of identical length and set both the file's
	// mtime and the real index's mtime to the same second stored in the index.
	writeRepoFile(t, dir, "src/app.ts", []byte("export const x = 2;\n"))
	if err := os.Chtimes(appPath, ts, ts); err != nil {
		t.Fatalf("chtimes file: %v", err)
	}
	if err := os.Chtimes(realIdx, ts, ts); err != nil {
		t.Fatalf("chtimes index: %v", err)
	}

	d, err := Load(dir, baseSHA)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	f := d.File("src/app.ts")
	if f == nil {
		t.Fatalf("File(src/app.ts) = nil, want Modified (Files = %+v)", d.Files)
	}
	if f.Status != Modified {
		t.Errorf("Status = %q, want %q", f.Status, Modified)
	}
}
