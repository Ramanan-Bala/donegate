package guard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ramanan-Bala/donegate/internal/policy"
)

// ToolCall represents a single agent tool invocation to evaluate against the edit guard.
type ToolCall struct {
	Tool    string
	Paths   []string // file paths the tool writes, as given
	Command string   // shell command, for Bash
	Cwd     string
}

// Context provides the resolved repository, policy, baseline, and harness paths for Decide.
type Context struct {
	RepoRoot  string
	Policy    *policy.Policy
	Protected map[string]string // session baseline: repo-relative path -> hash; nil if no session
	StateDir  string
	Home      string
}

// Decision is the result of evaluating a ToolCall.
type Decision struct {
	Allow  bool
	RuleID string
	Path   string
	Reason string
}

// Decide evaluates whether a tool call should be allowed or denied.
// First deny wins; when allowed, Allow is true and all other fields are empty.
func Decide(call ToolCall, ctx Context) Decision {
	pol := ctx.Policy
	if pol == nil {
		pol = policy.Default()
	}

	stateDir := resolveRoot(ctx.StateDir, call.Cwd)
	home := resolveRoot(ctx.Home, call.Cwd)
	repoRoot := resolveRoot(ctx.RepoRoot, call.Cwd)

	paths := make([]string, 0, len(call.Paths))
	paths = append(paths, call.Paths...)

	isBash := call.Tool == "Bash"
	var snapUpdate bool
	if isBash {
		var bashPaths []string
		bashPaths, snapUpdate = ShellWrites(call.Command)
		paths = append(paths, bashPaths...)
	}

	for _, raw := range paths {
		if raw == "" {
			continue
		}
		if isBash && home != "" {
			if raw == "~" {
				raw = home
			} else if strings.HasPrefix(raw, "~/") {
				raw = filepath.Join(home, raw[2:])
			}
		}

		absPath := resolveAbs(raw, call.Cwd)
		if d := checkPath(absPath, repoRoot, stateDir, home, pol, ctx.Protected); !d.Allow {
			return d
		}
	}

	if snapUpdate {
		return Decision{
			Allow:  false,
			RuleID: "T004",
			Reason: "donegate: snapshot updates are blocked (T004). Fix the code so existing snapshots match.",
		}
	}

	return Decision{Allow: true}
}

func checkPath(absPath, repoRoot, stateDir, home string, pol *policy.Policy, protected map[string]string) Decision {
	// a. Inside StateDir
	if _, ok := relWithin(stateDir, absPath); ok {
		return Decision{
			Allow:  false,
			RuleID: "H001",
			Path:   absPath,
			Reason: fmt.Sprintf("donegate: %s is protected (H001); donegate state is read-only.", absPath),
		}
	}

	// b. Harness hook/config files under Home
	if homeRel, ok := relWithin(home, absPath); ok && isHomeHarnessFile(homeRel) {
		return Decision{
			Allow:  false,
			RuleID: "H001",
			Path:   absPath,
			Reason: fmt.Sprintf("donegate: %s is a protected harness config/hook file (H001). Harness configuration is read-only.", absPath),
		}
	}

	rel, inRepo := relWithin(repoRoot, absPath)
	if !inRepo {
		return Decision{Allow: true}
	}

	// b (repo). Harness hook/config files inside RepoRoot
	if isRepoHarnessFile(rel) {
		return Decision{
			Allow:  false,
			RuleID: "H001",
			Path:   rel,
			Reason: fmt.Sprintf("donegate: %s is a protected harness config/hook file (H001). Harness configuration is read-only.", rel),
		}
	}

	// c. Repo .donegate.yml
	if rel == ".donegate.yml" {
		return Decision{
			Allow:  false,
			RuleID: "H001",
			Path:   rel,
			Reason: "donegate: .donegate.yml is protected policy config (H001). Policy is read-only during a session.",
		}
	}

	// d. Snapshots (checked before IsTest since default test globs include __snapshots__/**)
	if pol.IsSnapshot(rel) {
		return Decision{
			Allow:  false,
			RuleID: "T004",
			Path:   rel,
			Reason: fmt.Sprintf("donegate: %s snapshots are read-only (T004); fix the output instead of updating snapshots.", rel),
		}
	}

	// e. Tests
	if pol.IsTest(rel) {
		exists := false
		if protected != nil {
			_, exists = protected[rel]
		} else {
			_, err := os.Lstat(absPath)
			exists = err == nil
		}
		if exists || !pol.Protect.AllowNewTests {
			return Decision{
				Allow:  false,
				RuleID: "T001",
				Path:   rel,
				Reason: fmt.Sprintf("donegate: %s is a protected test file (T001). Tests are read-only; fix the source under test instead.", rel),
			}
		}
	}

	// f. Test/lint/CI config
	if pol.IsConfig(rel) {
		return Decision{
			Allow:  false,
			RuleID: "C001",
			Path:   rel,
			Reason: fmt.Sprintf("donegate: %s is protected test/lint config (C001). Fix the code, not the toolchain.", rel),
		}
	}

	return Decision{Allow: true}
}

func isHomeHarnessFile(rel string) bool {
	return rel == ".claude/settings.json" ||
		rel == ".claude/settings.local.json" ||
		rel == ".claude/hooks" || strings.HasPrefix(rel, ".claude/hooks/") ||
		rel == ".config/opencode/plugin" || strings.HasPrefix(rel, ".config/opencode/plugin/") ||
		rel == ".config/opencode/plugins" || strings.HasPrefix(rel, ".config/opencode/plugins/") ||
		rel == ".pi/agent/extensions" || strings.HasPrefix(rel, ".pi/agent/extensions/")
}

func isRepoHarnessFile(rel string) bool {
	return rel == ".claude/settings.json" ||
		rel == ".claude/settings.local.json" ||
		rel == ".claude/hooks" || strings.HasPrefix(rel, ".claude/hooks/")
}

func relWithin(base, target string) (string, bool) {
	if base == "" {
		return "", false
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func resolveRoot(dir, cwd string) string {
	if dir == "" {
		return ""
	}
	return resolveAbs(dir, cwd)
}

func resolveAbs(p, cwd string) string {
	if !filepath.IsAbs(p) {
		if cwd != "" {
			p = filepath.Join(cwd, p)
		}
		if !filepath.IsAbs(p) {
			if abs, err := filepath.Abs(p); err == nil {
				p = abs
			}
		}
	}
	return resolveSymlinks(filepath.Clean(p))
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
