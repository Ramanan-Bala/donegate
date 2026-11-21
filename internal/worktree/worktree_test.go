package worktree

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	fullArgs := append([]string{
		"-c", "user.name=test",
		"-c", "user.email=test@example.com",
		"-c", "commit.gpgsign=false",
		"-c", "init.defaultBranch=main",
	}, args...)
	cmd := exec.Command("git", fullArgs...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", full, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

func readFile(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

func assertNotExists(t *testing.T, dir, rel string) {
	t.Helper()
	_, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(rel)))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected %s to not exist, got err=%v", rel, err)
	}
}

func initTestRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	writeFile(t, dir, ".gitignore", "dist/\nnode_modules/\n")
	writeFile(t, dir, "src/app.ts", "export const x = 1;\n")
	writeFile(t, dir, "src/app.test.ts", "test('x', () => expect(1).toBe(1));\n")
	writeFile(t, dir, "src/keep.test.ts", "test('keep', () => expect(true).toBe(true));\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "initial")
	head := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
	return dir, head
}

// snapshotWorkingTree records all non-.git paths and contents in dir for invariance assertions.
func snapshotWorkingTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return files
}

func TestCreateRestoresProtectedAndPreservesAgentChanges(t *testing.T) {
	dir, base := initTestRepo(t)

	// (a) Source change + test edit by agent; keep.test.ts remains untouched.
	writeFile(t, dir, "src/app.ts", "export const x = 2;\n")
	writeFile(t, dir, "src/app.test.ts", "test('x', () => expect(2).toBe(2));\n")

	// (d) Untracked new source file.
	writeFile(t, dir, "src/helper.ts", "export const helper = true;\n")

	// (e) Ignored files.
	writeFile(t, dir, "dist/bundle.js", "console.log('ignored');\n")

	// (c) File in Restore that did not exist in Base, added by agent.
	writeFile(t, dir, "src/added.test.ts", "test('fake', () => {});\n")

	tree, err := Create(Options{
		RepoDir: dir,
		Base:    base,
		Restore: []string{"src/app.test.ts", "src/keep.test.ts", "src/added.test.ts"},
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer func() {
		if err := tree.Close(); err != nil {
			t.Errorf("Close failed: %v", err)
		}
	}()

	// Source change is present in worktree.
	if got, want := readFile(t, tree.Dir, "src/app.ts"), "export const x = 2;\n"; got != want {
		t.Errorf("src/app.ts = %q, want %q", got, want)
	}
	// Edited protected test is restored to original Base content.
	if got, want := readFile(t, tree.Dir, "src/app.test.ts"), "test('x', () => expect(1).toBe(1));\n"; got != want {
		t.Errorf("src/app.test.ts = %q, want %q", got, want)
	}
	// Untouched protected test stays intact.
	if got, want := readFile(t, tree.Dir, "src/keep.test.ts"), "test('keep', () => expect(true).toBe(true));\n"; got != want {
		t.Errorf("src/keep.test.ts = %q, want %q", got, want)
	}
	// Untracked new source file is present.
	if got, want := readFile(t, tree.Dir, "src/helper.ts"), "export const helper = true;\n"; got != want {
		t.Errorf("src/helper.ts = %q, want %q", got, want)
	}
	// Ignored file is absent.
	assertNotExists(t, tree.Dir, "dist/bundle.js")
	// Protected path absent in Base and added by agent is removed.
	assertNotExists(t, tree.Dir, "src/added.test.ts")

	// Restored lists only paths whose index entry changed (not untouched src/keep.test.ts).
	wantRestored := []string{"src/added.test.ts", "src/app.test.ts"}
	if !reflect.DeepEqual(tree.Restored, wantRestored) {
		t.Errorf("Restored = %v, want %v", tree.Restored, wantRestored)
	}
}

func TestCreateRestoresDeletedProtectedFile(t *testing.T) {
	dir, base := initTestRepo(t)

	// (b) Agent deletes a protected test file.
	if err := os.Remove(filepath.Join(dir, "src", "app.test.ts")); err != nil {
		t.Fatalf("remove test file: %v", err)
	}
	writeFile(t, dir, "src/app.ts", "export const x = 99;\n")

	tree, err := Create(Options{
		RepoDir: dir,
		Base:    base,
		Restore: []string{"src/app.test.ts"},
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer func() {
		if err := tree.Close(); err != nil {
			t.Errorf("Close failed: %v", err)
		}
	}()

	if got, want := readFile(t, tree.Dir, "src/app.test.ts"), "test('x', () => expect(1).toBe(1));\n"; got != want {
		t.Errorf("restored deleted test = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(tree.Restored, []string{"src/app.test.ts"}) {
		t.Errorf("Restored = %v, want [src/app.test.ts]", tree.Restored)
	}
}

func TestCreateSymlinksLinkDirsAndClosePreservesTarget(t *testing.T) {
	dir, base := initTestRepo(t)

	// (f) Create real node_modules and nested frontend/node_modules (ignored by git).
	writeFile(t, dir, "node_modules/pkg/index.js", "module.exports = 42;\n")
	writeFile(t, dir, "frontend/node_modules/ui/index.js", "export const ui = 1;\n")
	writeFile(t, dir, "frontend/.gitignore", "node_modules/\n")

	linkDirs := DetectLinkDirs(dir, []string{"frontend", "backend"})
	if !reflect.DeepEqual(linkDirs, []string{"frontend/node_modules", "node_modules"}) {
		t.Fatalf("DetectLinkDirs = %v, want [frontend/node_modules node_modules]", linkDirs)
	}

	tree, err := Create(Options{
		RepoDir:  dir,
		Base:     base,
		LinkDirs: linkDirs,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	for _, rel := range linkDirs {
		dst := filepath.Join(tree.Dir, filepath.FromSlash(rel))
		info, err := os.Lstat(dst)
		if err != nil {
			t.Fatalf("lstat %s: %v", rel, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("expected %s to be a symlink, mode=%v", rel, info.Mode())
		}
		linkTarget, err := os.Readlink(dst)
		if err != nil {
			t.Fatalf("readlink %s: %v", rel, err)
		}
		resolvedTarget, err := filepath.EvalSymlinks(linkTarget)
		if err != nil {
			t.Fatalf("eval symlink target %s: %v", linkTarget, err)
		}
		wantTarget, err := filepath.EvalSymlinks(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("eval real dir: %v", err)
		}
		if resolvedTarget != wantTarget {
			t.Errorf("symlink %s -> %q, want %q", rel, resolvedTarget, wantTarget)
		}
	}

	// Verify content resolves through the symlink.
	if got, want := readFile(t, tree.Dir, "node_modules/pkg/index.js"), "module.exports = 42;\n"; got != want {
		t.Errorf("node_modules content = %q, want %q", got, want)
	}

	if err := tree.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Ensure Close did not follow symlinks and delete real node_modules contents.
	if got, want := readFile(t, dir, "node_modules/pkg/index.js"), "module.exports = 42;\n"; got != want {
		t.Errorf("real node_modules was modified: %q", got)
	}
	if got, want := readFile(t, dir, "frontend/node_modules/ui/index.js"), "export const ui = 1;\n"; got != want {
		t.Errorf("real frontend/node_modules was modified: %q", got)
	}
}

func TestCreateAndCloseLeaveRealRepoUntouched(t *testing.T) {
	dir, base := initTestRepo(t)

	// Stage one change in the real index and leave another unstaged + untracked.
	writeFile(t, dir, "src/staged.ts", "export const staged = 1;\n")
	runGit(t, dir, "add", "src/staged.ts")
	writeFile(t, dir, "src/app.ts", "export const x = 5;\n")
	writeFile(t, dir, "src/app.test.ts", "test('tampered', () => {});\n")
	writeFile(t, dir, "src/untracked.ts", "export const u = 1;\n")

	// (g) Capture real repo state before Create+Close.
	beforeIndex := runGit(t, dir, "ls-files", "-s")
	beforeHead := runGit(t, dir, "rev-parse", "HEAD")
	beforeBranches := runGit(t, dir, "branch", "--list")
	beforeFiles := snapshotWorkingTree(t, dir)

	tree, err := Create(Options{
		RepoDir: dir,
		Base:    base,
		Restore: []string{"src/app.test.ts"},
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Check state is already untouched while worktree is open.
	if got := runGit(t, dir, "ls-files", "-s"); got != beforeIndex {
		t.Errorf("real index changed during Create:\ngot:\n%s\nwant:\n%s", got, beforeIndex)
	}
	if got := runGit(t, dir, "rev-parse", "HEAD"); got != beforeHead {
		t.Errorf("HEAD changed during Create: got %q, want %q", got, beforeHead)
	}
	if got := runGit(t, dir, "branch", "--list"); got != beforeBranches {
		t.Errorf("branches changed during Create: got %q, want %q", got, beforeBranches)
	}
	if got := snapshotWorkingTree(t, dir); !reflect.DeepEqual(got, beforeFiles) {
		t.Errorf("working tree changed during Create: got %v, want %v", got, beforeFiles)
	}

	// (h) Close removes dir, removes from git worktree list, and is idempotent.
	wtListOpen := runGit(t, dir, "worktree", "list", "--porcelain")
	if !strings.Contains(wtListOpen, tree.Dir) {
		t.Errorf("expected worktree list to contain %q while open, got:\n%s", tree.Dir, wtListOpen)
	}

	if err := tree.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if _, err := os.Lstat(tree.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected worktree dir %s to be removed, got err=%v", tree.Dir, err)
	}
	wtListClosed := runGit(t, dir, "worktree", "list", "--porcelain")
	if strings.Contains(wtListClosed, tree.Dir) {
		t.Errorf("expected worktree list not to contain %q after Close, got:\n%s", tree.Dir, wtListClosed)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}

	// State after Close still identical.
	if got := runGit(t, dir, "ls-files", "-s"); got != beforeIndex {
		t.Errorf("real index changed after Close:\ngot:\n%s\nwant:\n%s", got, beforeIndex)
	}
	if got := runGit(t, dir, "rev-parse", "HEAD"); got != beforeHead {
		t.Errorf("HEAD changed after Close: got %q, want %q", got, beforeHead)
	}
	if got := runGit(t, dir, "branch", "--list"); got != beforeBranches {
		t.Errorf("branches changed after Close: got %q, want %q", got, beforeBranches)
	}
	if got := snapshotWorkingTree(t, dir); !reflect.DeepEqual(got, beforeFiles) {
		t.Errorf("working tree changed after Close: got %v, want %v", got, beforeFiles)
	}
}

func TestCreateWithUnbranchedCommitTreeBase(t *testing.T) {
	dir, head := initTestRepo(t)

	// (i) Create a detached baseline commit using commit-tree (not on any branch),
	// mirroring how session.Start records a working-tree baseline.
	writeFile(t, dir, "src/app.test.ts", "test('baseline', () => expect(10).toBe(10));\n")
	tmpIdx := filepath.Join(t.TempDir(), "idx")
	cmdAdd := exec.Command("git", "add", "-A")
	cmdAdd.Dir = dir
	cmdAdd.Env = append(os.Environ(), "GIT_INDEX_FILE="+tmpIdx)
	if out, err := cmdAdd.CombinedOutput(); err != nil {
		t.Fatalf("temp add: %v\n%s", err, out)
	}
	cmdTree := exec.Command("git", "write-tree")
	cmdTree.Dir = dir
	cmdTree.Env = append(os.Environ(), "GIT_INDEX_FILE="+tmpIdx)
	treeOut, err := cmdTree.CombinedOutput()
	if err != nil {
		t.Fatalf("write-tree: %v\n%s", err, treeOut)
	}
	treeSHA := strings.TrimSpace(string(treeOut))
	baseSHA := strings.TrimSpace(runGit(t, dir, "commit-tree", treeSHA, "-p", head, "-m", "unbranched baseline"))

	// Agent edits both source and test after baseline.
	writeFile(t, dir, "src/app.ts", "export const x = 42;\n")
	writeFile(t, dir, "src/app.test.ts", "test('rigged', () => expect(1).toBe(1));\n")

	customTemp := filepath.Join(t.TempDir(), "custom-wt-root")
	tree, err := Create(Options{
		RepoDir:  dir,
		Base:     baseSHA,
		Restore:  []string{"src/app.test.ts"},
		TempRoot: customTemp,
	})
	if err != nil {
		t.Fatalf("Create with commit-tree base failed: %v", err)
	}
	defer func() {
		if err := tree.Close(); err != nil {
			t.Errorf("Close failed: %v", err)
		}
	}()

	resolvedRoot, err := filepath.EvalSymlinks(customTemp)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(tree.Dir) != resolvedRoot {
		t.Errorf("worktree dir %q not under TempRoot %q", tree.Dir, resolvedRoot)
	}
	if got, want := readFile(t, tree.Dir, "src/app.ts"), "export const x = 42;\n"; got != want {
		t.Errorf("src/app.ts = %q, want %q", got, want)
	}
	if got, want := readFile(t, tree.Dir, "src/app.test.ts"), "test('baseline', () => expect(10).toBe(10));\n"; got != want {
		t.Errorf("src/app.test.ts = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(tree.Restored, []string{"src/app.test.ts"}) {
		t.Errorf("Restored = %v, want [src/app.test.ts]", tree.Restored)
	}
}

func TestCloseWhenDirAlreadyRemoved(t *testing.T) {
	dir, base := initTestRepo(t)

	tree, err := Create(Options{
		RepoDir: dir,
		Base:    base,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := os.RemoveAll(tree.Dir); err != nil {
		t.Fatalf("pre-remove worktree dir: %v", err)
	}

	if err := tree.Close(); err != nil {
		t.Fatalf("Close after manual removal failed: %v", err)
	}
	wtList := runGit(t, dir, "worktree", "list", "--porcelain")
	if strings.Contains(wtList, tree.Dir) {
		t.Errorf("expected pruned worktree list, got:\n%s", wtList)
	}
}

func TestDetectLinkDirs(t *testing.T) {
	cases := []struct {
		name         string
		existingDirs []string
		existingFile string
		projectRoots []string
		want         []string
	}{
		{
			name:         "none exist",
			projectRoots: []string{"frontend", "backend"},
			want:         nil,
		},
		{
			name:         "root only",
			existingDirs: []string{"node_modules"},
			projectRoots: []string{"frontend"},
			want:         []string{"node_modules"},
		},
		{
			name:         "deduped and sorted across root and subprojects",
			existingDirs: []string{"node_modules", "packages/web/node_modules", "api/node_modules"},
			projectRoots: []string{"./packages/web/", "", ".", "api", "packages/web", "missing", "../outside"},
			want:         []string{"api/node_modules", "node_modules", "packages/web/node_modules"},
		},
		{
			name:         "ignores regular file named node_modules",
			existingFile: "node_modules",
			projectRoots: nil,
			want:         nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, rel := range tc.existingDirs {
				if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(rel)), 0o755); err != nil {
					t.Fatalf("mkdir %s: %v", rel, err)
				}
			}
			if tc.existingFile != "" {
				writeFile(t, dir, tc.existingFile, "not a dir")
			}

			got := DetectLinkDirs(dir, tc.projectRoots)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("DetectLinkDirs = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCreateValidationErrors(t *testing.T) {
	dir, _ := initTestRepo(t)

	cases := []struct {
		name string
		opts Options
	}{
		{
			name: "missing base",
			opts: Options{RepoDir: dir},
		},
		{
			name: "invalid base commit",
			opts: Options{RepoDir: dir, Base: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Create(tc.opts); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}
