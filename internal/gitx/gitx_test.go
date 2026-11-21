package gitx

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOutput(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("git", "-c", "init.defaultBranch=main", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}

	t.Run("success", func(t *testing.T) {
		out, err := Output(dir, nil, "rev-parse", "--show-toplevel")
		if err != nil {
			t.Fatalf("Output failed: %v", err)
		}
		if got := strings.TrimSpace(string(out)); got != resolvedDir {
			t.Errorf("got %q, want %q", got, resolvedDir)
		}
	})

	t.Run("extra env", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		tmpIdx := filepath.Join(t.TempDir(), "index")
		if _, err := Output(dir, []string{"GIT_INDEX_FILE=" + tmpIdx}, "add", "a.txt"); err != nil {
			t.Fatalf("Output with env failed: %v", err)
		}
		if _, err := os.Stat(tmpIdx); err != nil {
			t.Errorf("expected temp index at %s: %v", tmpIdx, err)
		}
	})

	t.Run("error wraps subcommand and stderr", func(t *testing.T) {
		_, err := Output(dir, nil, "-c", "core.quotepath=false", "rev-parse", "--verify", "refs/heads/nonexistent")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		msg := err.Error()
		if !strings.Contains(msg, "git rev-parse:") {
			t.Errorf("error %q should contain subcommand prefix", msg)
		}
		if !strings.Contains(msg, "Needed a single revision") {
			t.Errorf("error %q should contain git stderr", msg)
		}
	})
}

func TestTempIndex(t *testing.T) {
	extractIndexPath := func(t *testing.T, env []string) string {
		t.Helper()
		if len(env) != 1 || !strings.HasPrefix(env[0], "GIT_INDEX_FILE=") {
			t.Fatalf("env = %v, want single GIT_INDEX_FILE entry", env)
		}
		tmpIdx := strings.TrimPrefix(env[0], "GIT_INDEX_FILE=")
		rel, err := filepath.Rel(os.TempDir(), tmpIdx)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			t.Fatalf("temp index %q is not inside os.TempDir() %q", tmpIdx, os.TempDir())
		}
		return tmpIdx
	}

	t.Run("preserves mtime and cleans up", func(t *testing.T) {
		dir := t.TempDir()
		cmd := exec.Command("git", "-c", "init.defaultBranch=main", "init")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git init failed: %v\n%s", err, out)
		}
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Output(dir, nil, "add", "a.txt"); err != nil {
			t.Fatalf("git add failed: %v", err)
		}

		realIdx := filepath.Join(dir, ".git", "index")
		past := time.Now().Add(-10 * time.Second).Truncate(time.Second)
		if err := os.Chtimes(realIdx, past, past); err != nil {
			t.Fatalf("chtimes real index: %v", err)
		}
		origInfo, err := os.Stat(realIdx)
		if err != nil {
			t.Fatalf("stat real index: %v", err)
		}

		env, cleanup, err := TempIndex(dir)
		if err != nil {
			t.Fatalf("TempIndex failed: %v", err)
		}
		tmpIdx := extractIndexPath(t, env)
		tmpDir := filepath.Dir(tmpIdx)

		copyInfo, err := os.Stat(tmpIdx)
		if err != nil {
			cleanup()
			t.Fatalf("stat temp index: %v", err)
		}
		if !copyInfo.ModTime().Equal(origInfo.ModTime()) {
			cleanup()
			t.Errorf("temp index mtime = %v, want %v", copyInfo.ModTime(), origInfo.ModTime())
		}

		cleanup()
		if _, err := os.Stat(tmpIdx); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("temp index still exists after cleanup: %v", err)
		}
		if _, err := os.Stat(tmpDir); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("temp dir still exists after cleanup: %v", err)
		}
	})

	t.Run("fresh repo without index", func(t *testing.T) {
		dir := t.TempDir()
		cmd := exec.Command("git", "-c", "init.defaultBranch=main", "init")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git init failed: %v\n%s", err, out)
		}

		realIdx := filepath.Join(dir, ".git", "index")
		if _, err := os.Stat(realIdx); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected no index in fresh repo, got err=%v", err)
		}

		env, cleanup, err := TempIndex(dir)
		if err != nil {
			t.Fatalf("TempIndex on fresh repo failed: %v", err)
		}
		tmpIdx := extractIndexPath(t, env)
		tmpDir := filepath.Dir(tmpIdx)

		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("fresh\n"), 0o644); err != nil {
			cleanup()
			t.Fatal(err)
		}
		if _, err := Output(dir, env, "add", "a.txt"); err != nil {
			cleanup()
			t.Fatalf("git add with temp index failed: %v", err)
		}
		if _, err := os.Stat(realIdx); !errors.Is(err, os.ErrNotExist) {
			cleanup()
			t.Errorf("real index was created unexpectedly: %v", err)
		}

		cleanup()
		if _, err := os.Stat(tmpDir); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("temp dir still exists after cleanup: %v", err)
		}
	})
}
