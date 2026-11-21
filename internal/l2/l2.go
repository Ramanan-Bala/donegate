package l2

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Ramanan-Bala/donegate/internal/gitx"
	"github.com/Ramanan-Bala/donegate/internal/policy"
	"github.com/Ramanan-Bala/donegate/internal/runner"
	"github.com/Ramanan-Bala/donegate/internal/worktree"
)

// Options configures Level 2 clean-worktree command verification.
type Options struct {
	RepoDir  string
	Base     string
	Policy   *policy.Policy
	Restore  []string      // nil -> every base-tree path where IsTest || IsConfig, plus .donegate.yml
	CacheDir string        // where the baseline result is cached, may be ""
	Timeout  time.Duration // per command; default from Policy.Gate.TimeoutSeconds
}

// Run executes each configured project's test, typecheck, and lint commands in a
// clean verification worktree with protected files restored from Base, and returns
// both those runs and baseline test runs at Base.
func Run(ctx context.Context, o Options) (runs, baseline []runner.Result, err error) {
	if o.Policy == nil || len(o.Policy.Projects) == 0 {
		return nil, nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(o.Base) == "" {
		return nil, nil, fmt.Errorf("l2: baseline commit is required")
	}

	repoRoot, err := resolveRepoRoot(o.RepoDir)
	if err != nil {
		return nil, nil, fmt.Errorf("l2: %w", err)
	}

	baseOut, err := gitx.Output(repoRoot, nil, "rev-parse", "--verify", o.Base+"^{commit}")
	if err != nil {
		return nil, nil, fmt.Errorf("l2: resolve baseline commit %s: %w", o.Base, err)
	}
	baseSHA := strings.TrimSpace(string(baseOut))

	timeout := o.Timeout
	if timeout <= 0 && o.Policy.Gate.TimeoutSeconds > 0 {
		timeout = time.Duration(o.Policy.Gate.TimeoutSeconds) * time.Second
	}

	restore := o.Restore
	if restore == nil {
		restore, err = defaultRestorePaths(repoRoot, baseSHA, o.Policy)
		if err != nil {
			return nil, nil, fmt.Errorf("l2: %w", err)
		}
	}

	roots := make([]string, 0, len(o.Policy.Projects))
	for _, p := range o.Policy.Projects {
		roots = append(roots, p.Root)
	}
	linkDirs := worktree.DetectLinkDirs(repoRoot, roots)

	wt, err := worktree.Create(worktree.Options{
		RepoDir:  repoRoot,
		Base:     baseSHA,
		Restore:  restore,
		LinkDirs: linkDirs,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("l2: create verification worktree: %w", err)
	}
	defer func() {
		if closeErr := wt.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("l2: close verification worktree: %w", closeErr)
		}
	}()

	runs, err = runProjectCommands(ctx, wt.Dir, o.Policy.Projects, timeout, false)
	if err != nil {
		return nil, nil, err
	}

	baseline, err = runOrLoadBaseline(ctx, repoRoot, baseSHA, o.Policy.Projects, linkDirs, o.CacheDir, timeout)
	if err != nil {
		return nil, nil, err
	}

	return runs, baseline, nil
}

func runProjectCommands(ctx context.Context, wtDir string, projects []policy.Project, timeout time.Duration, testOnly bool) ([]runner.Result, error) {
	var results []runner.Result
	for _, proj := range projects {
		name := projectName(proj)
		dir := projectDir(wtDir, proj.Root)

		cmds := []struct {
			kind runner.Kind
			cmd  string
		}{
			{kind: runner.Test, cmd: proj.Test},
		}
		if !testOnly {
			cmds = append(cmds,
				struct {
					kind runner.Kind
					cmd  string
				}{kind: runner.Typecheck, cmd: proj.Typecheck},
				struct {
					kind runner.Kind
					cmd  string
				}{kind: runner.Lint, cmd: proj.Lint},
			)
		}

		for _, c := range cmds {
			if strings.TrimSpace(c.cmd) == "" {
				continue
			}
			res, err := runner.Run(ctx, runner.Spec{
				Project: name,
				Kind:    c.kind,
				Command: c.cmd,
				Dir:     dir,
				Timeout: timeout,
			})
			if err != nil {
				return nil, fmt.Errorf("l2: run %s for project %s: %w", c.kind, name, err)
			}
			results = append(results, res)
		}
	}
	return results, nil
}

func runOrLoadBaseline(ctx context.Context, repoRoot, baseSHA string, projects []policy.Project, linkDirs []string, cacheDir string, timeout time.Duration) (baseline []runner.Result, err error) {
	var cachePath string
	if cacheDir != "" {
		cachePath = filepath.Join(cacheDir, fmt.Sprintf("baseline-%s.json", baseSHA))
		if data, readErr := os.ReadFile(cachePath); readErr == nil {
			var cached []runner.Result
			if jsonErr := json.Unmarshal(data, &cached); jsonErr == nil {
				return cached, nil
			}
		}
	}

	hasTest := false
	for _, p := range projects {
		if strings.TrimSpace(p.Test) != "" {
			hasTest = true
			break
		}
	}

	if hasTest {
		wtDir, wtErr := createBaselineWorktree(repoRoot, baseSHA, linkDirs)
		if wtErr != nil {
			return nil, fmt.Errorf("l2: %w", wtErr)
		}
		defer func() {
			if rmErr := removeWorktree(repoRoot, wtDir); rmErr != nil && err == nil {
				err = fmt.Errorf("l2: remove baseline worktree: %w", rmErr)
			}
		}()

		baseline, err = runProjectCommands(ctx, wtDir, projects, timeout, true)
		if err != nil {
			return nil, err
		}
	}

	if cachePath != "" {
		if err := writeBaselineCache(cachePath, baseline); err != nil {
			return nil, fmt.Errorf("l2: %w", err)
		}
	}

	return baseline, nil
}

func writeBaselineCache(cachePath string, baseline []runner.Result) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("create baseline cache dir: %w", err)
	}
	toWrite := baseline
	if toWrite == nil {
		toWrite = []runner.Result{}
	}
	data, err := json.MarshalIndent(toWrite, "", "  ")
	if err != nil {
		return fmt.Errorf("encode baseline cache: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		return fmt.Errorf("write baseline cache: %w", err)
	}
	return nil
}

func createBaselineWorktree(repoRoot, baseSHA string, linkDirs []string) (string, error) {
	tmpRoot := os.TempDir()
	if resolved, err := filepath.EvalSymlinks(tmpRoot); err == nil {
		tmpRoot = resolved
	}
	wtDir, err := os.MkdirTemp(tmpRoot, "donegate-base-wt-*")
	if err != nil {
		return "", fmt.Errorf("create baseline worktree dir: %w", err)
	}
	if _, err := gitx.Output(repoRoot, nil, "worktree", "add", "--detach", wtDir, baseSHA); err != nil {
		_ = os.RemoveAll(wtDir)
		return "", fmt.Errorf("add baseline worktree: %w", err)
	}
	if err := linkDirectories(repoRoot, wtDir, linkDirs); err != nil {
		_ = removeWorktree(repoRoot, wtDir)
		return "", fmt.Errorf("link baseline worktree dirs: %w", err)
	}
	return wtDir, nil
}

func removeWorktree(repoRoot, wtDir string) error {
	_, removeErr := gitx.Output(repoRoot, nil, "worktree", "remove", "--force", wtDir)
	if _, statErr := os.Lstat(wtDir); statErr == nil {
		if err := os.RemoveAll(wtDir); err != nil {
			return fmt.Errorf("remove leftover worktree dir: %w", err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("stat worktree dir: %w", statErr)
	}
	if removeErr != nil {
		if _, pruneErr := gitx.Output(repoRoot, nil, "worktree", "prune"); pruneErr != nil {
			return fmt.Errorf("remove worktree (%v) and prune: %w", removeErr, pruneErr)
		}
	}
	return nil
}

func linkDirectories(repoRoot, wtDir string, linkDirs []string) error {
	for _, raw := range linkDirs {
		rel, ok := cleanRelPath(raw)
		if !ok || rel == "" || rel == "." {
			continue
		}
		target := filepath.Join(repoRoot, filepath.FromSlash(rel))
		info, err := os.Stat(target)
		if err != nil || !info.IsDir() {
			continue
		}
		dst := filepath.Join(wtDir, filepath.FromSlash(rel))
		if _, err := os.Lstat(dst); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat link destination %s: %w", rel, err)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("create parent dir for link %s: %w", rel, err)
		}
		if err := os.Symlink(target, dst); err != nil {
			return fmt.Errorf("symlink %s: %w", rel, err)
		}
	}
	return nil
}

func defaultRestorePaths(repoRoot, baseSHA string, pol *policy.Policy) ([]string, error) {
	out, err := gitx.Output(repoRoot, nil, "-c", "core.quotepath=false", "ls-tree", "-r", "--name-only", "-z", baseSHA)
	if err != nil {
		return nil, fmt.Errorf("list baseline files: %w", err)
	}
	restore := []string{".donegate.yml"}
	for _, tok := range bytes.Split(out, []byte{0}) {
		if len(tok) == 0 {
			continue
		}
		rel := filepath.ToSlash(string(tok))
		if rel == ".donegate.yml" {
			continue
		}
		if pol.IsTest(rel) || pol.IsConfig(rel) {
			restore = append(restore, rel)
		}
	}
	sort.Strings(restore)
	return restore, nil
}

func resolveRepoRoot(repoDir string) (string, error) {
	if repoDir == "" {
		repoDir = "."
	}
	out, err := gitx.Output(repoDir, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("resolve repo root: %w", err)
	}
	abs, err := filepath.Abs(strings.TrimSpace(string(out)))
	if err != nil {
		return "", fmt.Errorf("abs repo root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("eval symlinks repo root: %w", err)
	}
	return resolved, nil
}

func projectName(p policy.Project) string {
	if name := strings.TrimSpace(p.Name); name != "" {
		return name
	}
	if rel, ok := cleanRelPath(p.Root); ok && rel != "" {
		return rel
	}
	return "."
}

func projectDir(wtDir, root string) string {
	rel, ok := cleanRelPath(root)
	if !ok || rel == "" || rel == "." {
		return wtDir
	}
	return filepath.Join(wtDir, filepath.FromSlash(rel))
}

func cleanRelPath(raw string) (string, bool) {
	s := filepath.ToSlash(strings.TrimSpace(raw))
	s = strings.TrimPrefix(s, "/")
	if s == "" {
		return ".", true
	}
	cleaned := path.Clean(s)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}
