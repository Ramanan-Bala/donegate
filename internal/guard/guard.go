package guard

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Ramanan-Bala/donegate/internal/allow"
	"github.com/Ramanan-Bala/donegate/internal/policy"
)

// ToolCall represents a single agent tool invocation to evaluate against the edit guard.
type ToolCall struct {
	Tool    string
	Paths   []string // file paths the tool writes, as given
	Command string   // shell command, for Bash
	Cwd     string
}

// Context provides the resolved repository, policy, baseline, allowances, and harness paths for Decide.
type Context struct {
	RepoRoot  string
	Policy    *policy.Policy
	Protected map[string]string // session baseline: repo-relative path -> hash; nil if no session
	Allowed   []allow.Allowance // human-approved session allowances
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
		if isDonegateAllowCommand(call.Command) {
			return Decision{
				Allow:  false,
				RuleID: "H001",
				Reason: "donegate: only a human can approve exceptions (donegate allow); ask the user to run it in their terminal.",
			}
		}
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
			if allow.Allows(ctx.Allowed, d.RuleID, d.Path) {
				continue
			}
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

var (
	donegateAllowRawRe = regexp.MustCompile(`(^|[^A-Za-z0-9_])donegate["']?\s+allow\b`)
	cmdDonegateAllowRe = regexp.MustCompile(`cmd/donegate[^\n]*\sallow\b`)

	envValueFlags = map[string]bool{
		"-u": true, "--unset": true,
		"-C": true, "--chdir": true,
		"-P":               true,
		"--default-signal": true, "--ignore-signal": true, "--block-signal": true,
	}
	sudoValueFlags = map[string]bool{
		"-u": true, "--user": true,
		"-g": true, "--group": true,
		"-C": true, "--close-from": true,
		"-D": true, "--chdir": true,
		"-h": true, "--host": true,
		"-p": true, "--prompt": true,
		"-r": true, "--role": true,
		"-t": true, "--type": true,
		"-U": true, "--other-user": true,
		"-T": true, "--command-timeout": true,
	}
	xargsValueFlags = map[string]bool{
		"-I": true, "--replace": true,
		"-L": true, "--max-lines": true,
		"-n": true, "--max-args": true,
		"-P": true, "--max-procs": true,
		"-s": true, "--max-chars": true,
		"-E": true, "--eof": true,
		"-d": true, "--delimiter": true,
		"-a": true, "--arg-file": true,
		"-J": true, "-R": true, "-S": true,
	}
	goRunValueFlags = map[string]bool{
		"-exec": true, "-tags": true, "-ldflags": true, "-gcflags": true,
		"-asmflags": true, "-mod": true, "-modfile": true, "-overlay": true,
		"-p": true, "-pkgdir": true, "-toolexec": true, "-C": true,
	}
)

func isDonegateAllowCommand(command string) bool {
	return isDonegateAllowCommandDepth(command, 0)
}

func isDonegateAllowCommandDepth(command string, depth int) bool {
	if depth > 8 {
		return false
	}
	if cmdDonegateAllowRe.MatchString(command) {
		return true
	}

	cmds := splitCommands(tokenizeShell(command))
	for _, cmdTokens := range cmds {
		words, _ := extractRedirections(cmdTokens)
		words = stripCommandWrappers(words)
		if len(words) == 0 {
			continue
		}

		cmd := words[0]
		base := filepath.Base(cmd)

		if isDonegateCommandWord(cmd) && len(words) >= 2 && words[1] == "allow" {
			return true
		}
		if backtickAllowCommand(words) {
			return true
		}
		if isShellBinary(base) {
			if script, ok := shellDashCScript(words[1:]); ok {
				if isDonegateAllowCommandDepth(script, depth+1) {
					return true
				}
			} else if (len(cmds) > 1 || len(words) == 1 || words[1] == "-s") && donegateAllowRawRe.MatchString(command) {
				return true
			}
			continue
		}
		if base == "eval" && len(words) > 1 {
			if isDonegateAllowCommandDepth(strings.Join(words[1:], " "), depth+1) {
				return true
			}
			continue
		}
		if base == "go" && len(words) >= 4 && words[1] == "run" && isGoRunDonegateAllow(words[2:]) {
			return true
		}
		if isScriptInterpreter(base) && inlineCodeHasDonegateAllow(words[1:]) {
			return true
		}
	}
	return false
}

func stripCommandWrappers(words []string) []string {
	i := 0
	for i < len(words) {
		w := words[i]
		if isEnvAssign(w) {
			i++
			continue
		}
		base := filepath.Base(w)
		switch base {
		case "env":
			i = skipEnvWrapper(words, i+1)
		case "command":
			i = skipSimpleFlags(words, i+1, nil)
		case "exec":
			i = skipSimpleFlags(words, i+1, map[string]bool{"-a": true})
		case "nohup", "time":
			i = skipSimpleFlags(words, i+1, nil)
		case "sudo":
			i = skipSimpleFlags(words, i+1, sudoValueFlags)
		case "xargs":
			i = skipSimpleFlags(words, i+1, xargsValueFlags)
		case "timeout":
			i = skipTimeoutWrapper(words, i+1)
		default:
			return words[i:]
		}
	}
	return words[i:]
}

func skipEnvWrapper(words []string, i int) int {
	for i < len(words) {
		w := words[i]
		if w == "--" {
			return i + 1
		}
		if envValueFlags[w] {
			if i+1 < len(words) {
				i += 2
			} else {
				i++
			}
			continue
		}
		if strings.HasPrefix(w, "-") && w != "-" {
			i++
			continue
		}
		if isEnvAssign(w) {
			i++
			continue
		}
		break
	}
	return i
}

func skipSimpleFlags(words []string, i int, valueFlags map[string]bool) int {
	for i < len(words) {
		w := words[i]
		if w == "--" {
			return i + 1
		}
		if valueFlags != nil && valueFlags[w] {
			if i+1 < len(words) {
				i += 2
			} else {
				i++
			}
			continue
		}
		if strings.HasPrefix(w, "-") && w != "-" {
			i++
			continue
		}
		break
	}
	return i
}

func skipTimeoutWrapper(words []string, i int) int {
	i = skipSimpleFlags(words, i, map[string]bool{"-s": true, "--signal": true, "-k": true, "--kill-after": true})
	if i < len(words) && !strings.HasPrefix(words[i], "-") {
		i++ // duration argument
	}
	return i
}

func isShellBinary(base string) bool {
	switch base {
	case "sh", "bash", "zsh", "dash", "ksh":
		return true
	default:
		return false
	}
}

func shellDashCScript(args []string) (string, bool) {
	hasC := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			if hasC && i+1 < len(args) {
				return args[i+1], true
			}
			return "", false
		}
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && len(a) > 1 {
			if strings.ContainsRune(a[1:], 'c') {
				hasC = true
			}
			continue
		}
		if strings.HasPrefix(a, "--") {
			continue
		}
		if hasC {
			return a, true
		}
		return "", false
	}
	return "", false
}

func isDonegateCommandWord(cmd string) bool {
	if filepath.Base(cmd) == "donegate" {
		return true
	}
	if strings.HasPrefix(cmd, "$(") && strings.Contains(cmd, "donegate") {
		return true
	}
	return false
}

func backtickAllowCommand(words []string) bool {
	if len(words) < 2 || !strings.HasPrefix(words[0], "`") {
		return false
	}
	hasDonegate := false
	for i, w := range words {
		if strings.Contains(w, "donegate") {
			hasDonegate = true
		}
		if strings.HasSuffix(w, "`") {
			return hasDonegate && i+1 < len(words) && words[i+1] == "allow"
		}
	}
	return false
}

func isGoRunDonegateAllow(args []string) bool {
	i := skipSimpleFlags(args, 0, goRunValueFlags)
	if i+1 >= len(args) {
		return false
	}
	target := filepath.ToSlash(args[i])
	return (filepath.Base(target) == "donegate" || strings.Contains(target, "cmd/donegate")) && args[i+1] == "allow"
}

func isScriptInterpreter(base string) bool {
	switch base {
	case "python", "python3", "node", "perl", "ruby":
		return true
	default:
		return false
	}
}

func inlineCodeHasDonegateAllow(args []string) bool {
	for i := 0; i < len(args); i++ {
		if (args[i] == "-c" || args[i] == "-e") && i+1 < len(args) {
			if donegateAllowRawRe.MatchString(args[i+1]) || cmdDonegateAllowRe.MatchString(args[i+1]) {
				return true
			}
		}
	}
	return false
}
