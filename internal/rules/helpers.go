package rules

import (
	"bytes"
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/Ramanan-Bala/donegate/internal/policy"
)

var sourceFileExts = map[string]bool{
	".js":  true,
	".jsx": true,
	".ts":  true,
	".tsx": true,
	".mjs": true,
	".cjs": true,
	".mts": true,
	".cts": true,
}

func isSourceFile(pol *policy.Policy, p string) bool {
	if pol == nil {
		return false
	}
	ext := strings.ToLower(path.Ext(normalizeRulePath(p)))
	if !sourceFileExts[ext] {
		return false
	}
	return !pol.IsTest(p) && !pol.IsConfig(p)
}

func isTestNonSnapshot(pol *policy.Policy, p string) bool {
	if pol == nil {
		return false
	}
	return pol.IsTest(p) && !pol.IsSnapshot(p)
}

func normalizeRulePath(p string) string {
	s := filepath.ToSlash(p)
	s = strings.TrimPrefix(s, "./")
	s = strings.TrimPrefix(s, "/")
	return s
}

func listBaseTree(repoDir, base string) ([]string, error) {
	if repoDir == "" || base == "" {
		return nil, fmt.Errorf("missing repo dir or base commit")
	}
	cmd := exec.Command("git", "-c", "core.quotepath=false", "ls-tree", "-r", "--name-only", "-z", base)
	cmd.Dir = repoDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git ls-tree: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var paths []string
	for _, tok := range bytes.Split(stdout.Bytes(), []byte{0}) {
		if len(tok) == 0 {
			continue
		}
		paths = append(paths, filepath.ToSlash(string(tok)))
	}
	return paths, nil
}
