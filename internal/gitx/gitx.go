package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Output executes git in repoDir with optional extra environment variables and returns stdout.
func Output(repoDir string, env []string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		sub := subcommand(args)
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("git %s: %w: %s", sub, err, msg)
		}
		return nil, fmt.Errorf("git %s: %w", sub, err)
	}
	return stdout.Bytes(), nil
}

// TempIndex creates a temporary copy of the repository's git index in a new
// temporary directory, preserving the original index's mtime so git's racy-git
// detection matches the real index. It returns the GIT_INDEX_FILE environment
// entry and a cleanup function that removes the temporary directory.
func TempIndex(repoDir string) (env []string, cleanup func(), err error) {
	idxOut, err := Output(repoDir, nil, "rev-parse", "--git-path", "index")
	if err != nil {
		return nil, nil, fmt.Errorf("locate git index: %w", err)
	}
	realIdx := strings.TrimSpace(string(idxOut))
	if !filepath.IsAbs(realIdx) {
		realIdx = filepath.Join(repoDir, realIdx)
	}

	tmpDir, err := os.MkdirTemp("", "donegate-index-*")
	if err != nil {
		return nil, nil, fmt.Errorf("create temp index dir: %w", err)
	}

	tmpIdx := filepath.Join(tmpDir, "index")
	f, err := os.Open(realIdx)
	if err == nil {
		info, readErr := f.Stat()
		var data []byte
		if readErr == nil {
			data, readErr = io.ReadAll(f)
		}
		f.Close()
		if readErr != nil {
			_ = os.RemoveAll(tmpDir)
			return nil, nil, fmt.Errorf("read git index: %w", readErr)
		}
		if err := os.WriteFile(tmpIdx, data, 0o600); err != nil {
			_ = os.RemoveAll(tmpDir)
			return nil, nil, fmt.Errorf("seed temp index: %w", err)
		}
		mtime := info.ModTime()
		if err := os.Chtimes(tmpIdx, mtime, mtime); err != nil {
			_ = os.RemoveAll(tmpDir)
			return nil, nil, fmt.Errorf("preserve temp index mtime: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = os.RemoveAll(tmpDir)
		return nil, nil, fmt.Errorf("read git index: %w", err)
	}

	cleanup = func() {
		_ = os.RemoveAll(tmpDir)
	}
	return []string{"GIT_INDEX_FILE=" + tmpIdx}, cleanup, nil
}

func subcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-c" || arg == "-C" || arg == "--git-dir" || arg == "--work-tree" {
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return arg
	}
	if len(args) > 0 {
		return args[0]
	}
	return ""
}
