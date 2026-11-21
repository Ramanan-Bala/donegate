package allow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Ramanan-Bala/donegate/internal/session"
)

// Allowance records a human-approved exception for a rule on a repo-relative path.
type Allowance struct {
	Rule string `json:"rule"`
	Path string `json:"path"`
	By   string `json:"by"`
	At   string `json:"at"`
}

var validRules = map[string]bool{
	"T001": true,
	"T002": true,
	"T004": true,
	"C001": true,
	"S003": true,
	"all":  true,
}

// ValidateRule reports whether rule is a supported allowance rule ID (or "all").
func ValidateRule(rule string) error {
	if !validRules[rule] {
		return fmt.Errorf("allow: invalid rule %q (expected T001, T002, T004, C001, S003, or all)", rule)
	}
	return nil
}

// NormalizePaths converts raw paths to repo-relative slash form and rejects paths outside sess.Repo.
func NormalizePaths(sess *session.Session, paths []string) ([]string, error) {
	if sess == nil {
		return nil, fmt.Errorf("allow: nil session")
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("allow: at least one path is required")
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		norm, err := normalizeRepoPath(sess.Repo, p)
		if err != nil {
			return nil, err
		}
		out = append(out, norm)
	}
	return out, nil
}

// Add records human-approved allowances for rule across paths in sess's allow.json.
func Add(sess *session.Session, rule string, paths []string, by string) error {
	if sess == nil {
		return fmt.Errorf("allow: nil session")
	}
	if err := ValidateRule(rule); err != nil {
		return err
	}
	normPaths, err := NormalizePaths(sess, paths)
	if err != nil {
		return err
	}

	if by == "" {
		by = os.Getenv("USER")
	}
	at := time.Now().UTC().Format(time.RFC3339)

	existing, err := List(sess)
	if err != nil {
		return err
	}

	for _, p := range normPaths {
		updated := false
		for i := range existing {
			if existing[i].Rule == rule && normalizeSlash(existing[i].Path) == p {
				existing[i].Path = p
				existing[i].By = by
				existing[i].At = at
				updated = true
				break
			}
		}
		if !updated {
			existing = append(existing, Allowance{
				Rule: rule,
				Path: p,
				By:   by,
				At:   at,
			})
		}
	}

	return writeAllowances(sess, existing)
}

// List reads all allowances recorded for sess.
func List(sess *session.Session) ([]Allowance, error) {
	if sess == nil {
		return nil, fmt.Errorf("allow: nil session")
	}
	dir := sess.Dir()
	if dir == "" {
		return nil, fmt.Errorf("allow: resolve session dir")
	}
	data, err := os.ReadFile(filepath.Join(dir, "allow.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("allow: read allow.json: %w", err)
	}
	var list []Allowance
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("allow: decode allow.json: %w", err)
	}
	return list, nil
}

// Revoke removes all allowances matching any of paths from sess's allow.json.
func Revoke(sess *session.Session, paths []string) error {
	if sess == nil {
		return fmt.Errorf("allow: nil session")
	}
	normPaths, err := NormalizePaths(sess, paths)
	if err != nil {
		return err
	}
	removeSet := make(map[string]bool, len(normPaths))
	for _, p := range normPaths {
		removeSet[p] = true
	}

	existing, err := List(sess)
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return nil
	}

	kept := make([]Allowance, 0, len(existing))
	for _, a := range existing {
		if removeSet[normalizeSlash(a.Path)] {
			continue
		}
		kept = append(kept, a)
	}
	return writeAllowances(sess, kept)
}

// Allows reports whether list permits ruleID on path.
// Any allowance on path also permits H001 on path, and rule "all" permits every path-based rule on path.
func Allows(list []Allowance, ruleID, path string) bool {
	_, ok := Match(list, ruleID, path)
	return ok
}

// Match returns the matching Allowance for (ruleID, path) in list, preferring exact rule matches.
func Match(list []Allowance, ruleID, path string) (Allowance, bool) {
	norm := normalizeSlash(path)
	if norm == "" || norm == "." {
		return Allowance{}, false
	}

	for _, a := range list {
		if normalizeSlash(a.Path) == norm && a.Rule == ruleID {
			return a, true
		}
	}
	for _, a := range list {
		if normalizeSlash(a.Path) != norm {
			continue
		}
		if a.Rule == "all" || ruleID == "H001" {
			return a, true
		}
	}
	return Allowance{}, false
}

func writeAllowances(sess *session.Session, list []Allowance) error {
	dir := sess.Dir()
	if dir == "" {
		return fmt.Errorf("allow: resolve session dir")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("allow: create session dir: %w", err)
	}
	if list == nil {
		list = []Allowance{}
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("allow: encode allow.json: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(dir, "allow.json"), data, 0o600); err != nil {
		return fmt.Errorf("allow: write allow.json: %w", err)
	}
	return nil
}

func normalizeRepoPath(repoRoot, raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("allow: empty path")
	}
	absRepo, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", fmt.Errorf("allow: resolve repo root: %w", err)
	}
	resolvedRepo := resolveSymlinks(filepath.Clean(absRepo))

	var absTarget string
	if filepath.IsAbs(raw) {
		absTarget = resolveSymlinks(filepath.Clean(raw))
	} else {
		absTarget = resolveSymlinks(filepath.Clean(filepath.Join(resolvedRepo, raw)))
	}

	rel, err := filepath.Rel(resolvedRepo, absTarget)
	if err != nil {
		return "", fmt.Errorf("allow: path %q is outside repository %s: %w", raw, repoRoot, err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("allow: path %q is outside repository %s", raw, repoRoot)
	}
	return filepath.ToSlash(rel), nil
}

func normalizeSlash(p string) string {
	s := filepath.ToSlash(strings.TrimSpace(p))
	if s == "" {
		return ""
	}
	s = filepath.Clean(filepath.FromSlash(s))
	s = filepath.ToSlash(s)
	s = strings.TrimPrefix(s, "./")
	if s == "." {
		return ""
	}
	return s
}

func resolveSymlinks(absPath string) string {
	absPath = filepath.Clean(absPath)
	var tail []string
	cur := absPath
	for {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return filepath.Clean(resolved)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return absPath
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}
