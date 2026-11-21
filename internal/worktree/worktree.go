package worktree

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Ramanan-Bala/donegate/internal/gitx"
)

// Options configures creating a clean verification worktree.
type Options struct {
	RepoDir  string
	Base     string   // baseline commit sha
	Restore  []string // repo-relative paths to restore from Base
	LinkDirs []string // repo-relative directories to symlink from RepoDir into the worktree
	TempRoot string   // parent directory for the worktree; defaults to os.TempDir()
}

// Tree represents a temporary detached git worktree prepared for verification.
type Tree struct {
	Dir      string
	Commit   string
	Restored []string // paths whose content was changed back

	repoDir string
	closed  bool
}

type treeEntry struct {
	mode    string
	objType string
	sha     string
}

type indexEntry struct {
	mode  string
	sha   string
	stage string
}

// Create builds a detached throwaway git worktree containing the current working-tree
// changes from RepoDir, with every path in Restore reset to its state in Base.
func Create(opts Options) (*Tree, error) {
	if strings.TrimSpace(opts.Base) == "" {
		return nil, fmt.Errorf("create worktree: baseline commit is required")
	}

	repoDir, err := resolveRepoRoot(opts.RepoDir)
	if err != nil {
		return nil, err
	}

	baseOut, err := gitx.Output(repoDir, nil, "rev-parse", "--verify", opts.Base+"^{commit}")
	if err != nil {
		return nil, fmt.Errorf("resolve baseline commit %s: %w", opts.Base, err)
	}
	baseSHA := strings.TrimSpace(string(baseOut))

	idxEnv, cleanupIdx, err := gitx.TempIndex(repoDir)
	if err != nil {
		return nil, err
	}
	defer cleanupIdx()

	if _, err := gitx.Output(repoDir, idxEnv, "add", "-A"); err != nil {
		return nil, fmt.Errorf("stage working tree: %w", err)
	}

	restored, err := restorePaths(repoDir, baseSHA, idxEnv, opts.Restore)
	if err != nil {
		return nil, err
	}

	commitSHA, err := commitTempIndex(repoDir, baseSHA, idxEnv)
	if err != nil {
		return nil, err
	}

	wtDir, err := prepareWorktreeDir(opts.TempRoot)
	if err != nil {
		return nil, err
	}

	if _, err := gitx.Output(repoDir, nil, "worktree", "add", "--detach", wtDir, commitSHA); err != nil {
		_ = os.RemoveAll(wtDir)
		return nil, fmt.Errorf("add worktree: %w", err)
	}

	tree := &Tree{
		Dir:      wtDir,
		Commit:   commitSHA,
		Restored: restored,
		repoDir:  repoDir,
	}

	if err := linkDirectories(repoDir, wtDir, opts.LinkDirs); err != nil {
		_ = tree.Close()
		return nil, err
	}

	return tree, nil
}

// Close removes the temporary worktree registration and directory. Close is idempotent.
func (t *Tree) Close() error {
	if t == nil || t.closed || t.Dir == "" {
		return nil
	}
	t.closed = true

	_, removeErr := gitx.Output(t.repoDir, nil, "worktree", "remove", "--force", t.Dir)

	if _, statErr := os.Lstat(t.Dir); statErr == nil {
		if err := os.RemoveAll(t.Dir); err != nil {
			return fmt.Errorf("remove leftover worktree dir: %w", err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("stat worktree dir: %w", statErr)
	}

	if removeErr != nil {
		if _, pruneErr := gitx.Output(t.repoDir, nil, "worktree", "prune"); pruneErr != nil {
			return fmt.Errorf("remove worktree (%v) and prune: %w", removeErr, pruneErr)
		}
	}

	return nil
}

// DetectLinkDirs returns sorted, de-duplicated "<root>/node_modules" (or "node_modules" for root "")
// paths that exist as directories in repoDir.
func DetectLinkDirs(repoDir string, projectRoots []string) []string {
	seen := make(map[string]struct{})
	var out []string

	roots := make([]string, 0, 1+len(projectRoots))
	roots = append(roots, "")
	roots = append(roots, projectRoots...)

	for _, root := range roots {
		rel, ok := cleanRelPath(root)
		if !ok {
			continue
		}
		candidate := "node_modules"
		if rel != "" {
			candidate = path.Join(rel, "node_modules")
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		info, err := os.Stat(filepath.Join(repoDir, filepath.FromSlash(candidate)))
		if err != nil || !info.IsDir() {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}

	sort.Strings(out)
	return out
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

func restorePaths(repoDir, baseSHA string, idxEnv []string, paths []string) ([]string, error) {
	targets := normalizeRestorePaths(paths)
	if len(targets) == 0 {
		return nil, nil
	}

	baseEntries, err := listTreeEntries(repoDir, baseSHA)
	if err != nil {
		return nil, err
	}

	idxEntries, err := listIndexEntries(repoDir, idxEnv)
	if err != nil {
		return nil, err
	}

	var restored []string
	for _, rel := range targets {
		baseEntry, inBase := baseEntries[rel]
		curEntries := idxEntries[rel]

		if inBase {
			if baseEntry.objType != "blob" {
				return nil, fmt.Errorf("restore %s: expected blob in baseline, got %s", rel, baseEntry.objType)
			}
			if indexMatchesBlob(curEntries, baseEntry) {
				continue
			}
			cacheInfo := baseEntry.mode + "," + baseEntry.sha + "," + rel
			if _, err := gitx.Output(repoDir, idxEnv, "update-index", "--add", "--cacheinfo", cacheInfo); err != nil {
				return nil, fmt.Errorf("restore %s from baseline: %w", rel, err)
			}
			restored = append(restored, rel)
			continue
		}

		if len(curEntries) == 0 {
			continue
		}
		if _, err := gitx.Output(repoDir, idxEnv, "update-index", "--force-remove", "--", rel); err != nil {
			return nil, fmt.Errorf("remove %s from worktree index: %w", rel, err)
		}
		restored = append(restored, rel)
	}

	return restored, nil
}

func listTreeEntries(repoDir, commitSHA string) (map[string]treeEntry, error) {
	out, err := gitx.Output(repoDir, nil, "-c", "core.quotepath=false", "ls-tree", "-r", "-z", commitSHA)
	if err != nil {
		return nil, fmt.Errorf("list baseline tree %s: %w", commitSHA, err)
	}
	entries := make(map[string]treeEntry)
	for _, rec := range bytes.Split(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		meta, relPath, ok := strings.Cut(string(rec), "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 {
			return nil, fmt.Errorf("parse ls-tree entry %q", meta)
		}
		entries[filepath.ToSlash(relPath)] = treeEntry{
			mode:    fields[0],
			objType: fields[1],
			sha:     fields[2],
		}
	}
	return entries, nil
}

func listIndexEntries(repoDir string, idxEnv []string) (map[string][]indexEntry, error) {
	out, err := gitx.Output(repoDir, idxEnv, "-c", "core.quotepath=false", "ls-files", "-s", "-z")
	if err != nil {
		return nil, fmt.Errorf("list temp index: %w", err)
	}
	entries := make(map[string][]indexEntry)
	for _, rec := range bytes.Split(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		meta, relPath, ok := strings.Cut(string(rec), "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 {
			return nil, fmt.Errorf("parse ls-files entry %q", meta)
		}
		norm := filepath.ToSlash(relPath)
		entries[norm] = append(entries[norm], indexEntry{
			mode:  fields[0],
			sha:   fields[1],
			stage: fields[2],
		})
	}
	return entries, nil
}

func indexMatchesBlob(entries []indexEntry, target treeEntry) bool {
	if len(entries) != 1 {
		return false
	}
	e := entries[0]
	return e.stage == "0" && e.mode == target.mode && e.sha == target.sha
}

func commitTempIndex(repoDir, baseSHA string, idxEnv []string) (string, error) {
	treeOut, err := gitx.Output(repoDir, idxEnv, "write-tree")
	if err != nil {
		return "", fmt.Errorf("write worktree tree: %w", err)
	}
	treeSHA := strings.TrimSpace(string(treeOut))

	commitEnv := []string{
		"GIT_AUTHOR_NAME=donegate",
		"GIT_AUTHOR_EMAIL=donegate@localhost",
		"GIT_COMMITTER_NAME=donegate",
		"GIT_COMMITTER_EMAIL=donegate@localhost",
	}
	commitOut, err := gitx.Output(
		repoDir,
		commitEnv,
		"-c", "commit.gpgsign=false",
		"commit-tree", treeSHA,
		"-p", baseSHA,
		"-m", "donegate verification tree",
	)
	if err != nil {
		return "", fmt.Errorf("commit worktree tree: %w", err)
	}
	return strings.TrimSpace(string(commitOut)), nil
}

func prepareWorktreeDir(tempRoot string) (string, error) {
	if tempRoot == "" {
		tempRoot = os.TempDir()
	}
	if err := os.MkdirAll(tempRoot, 0o755); err != nil {
		return "", fmt.Errorf("create temp root: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(tempRoot); err == nil {
		tempRoot = resolved
	}
	wtDir, err := os.MkdirTemp(tempRoot, "donegate-wt-*")
	if err != nil {
		return "", fmt.Errorf("create worktree dir: %w", err)
	}
	return wtDir, nil
}

func linkDirectories(repoDir, wtDir string, linkDirs []string) error {
	for _, raw := range linkDirs {
		rel, ok := cleanRelPath(raw)
		if !ok || rel == "" {
			continue
		}
		target := filepath.Join(repoDir, filepath.FromSlash(rel))
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

func normalizeRestorePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		rel, ok := cleanRelPath(raw)
		if !ok || rel == "" {
			continue
		}
		if _, exists := seen[rel]; exists {
			continue
		}
		seen[rel] = struct{}{}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

func cleanRelPath(raw string) (string, bool) {
	s := filepath.ToSlash(strings.TrimSpace(raw))
	s = strings.TrimPrefix(s, "/")
	if s == "" {
		return "", true
	}
	cleaned := path.Clean(s)
	if cleaned == "." {
		return "", true
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}
