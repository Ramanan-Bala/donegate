package guard

import (
	"path/filepath"
	"strings"
)

type shellToken struct {
	val  string
	isOp bool
}

// ShellWrites performs best-effort extraction of file paths written or deleted by a shell
// command and reports whether the command invokes a jest/vitest snapshot update.
func ShellWrites(command string) (paths []string, snapshotUpdate bool) {
	tokens := tokenizeShell(command)
	for _, cmdTokens := range splitCommands(tokens) {
		words, redirPaths := extractRedirections(cmdTokens)
		paths = append(paths, redirPaths...)

		cmdPaths, cmdSnap := analyzeSimpleCommand(words)
		paths = append(paths, cmdPaths...)
		if cmdSnap {
			snapshotUpdate = true
		}
	}
	return paths, snapshotUpdate
}

func tokenizeShell(s string) []shellToken {
	var tokens []shellToken
	i := 0
	n := len(s)

	for i < n {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\r' {
			i++
			continue
		}
		if c == '\n' {
			tokens = append(tokens, shellToken{val: "\n", isOp: true})
			i++
			continue
		}
		if c == '#' {
			for i < n && s[i] != '\n' {
				i++
			}
			continue
		}
		if c == ';' {
			tokens = append(tokens, shellToken{val: ";", isOp: true})
			i++
			continue
		}
		if c == '|' {
			if i+1 < n && s[i+1] == '|' {
				tokens = append(tokens, shellToken{val: "||", isOp: true})
				i += 2
			} else {
				tokens = append(tokens, shellToken{val: "|", isOp: true})
				i++
			}
			continue
		}
		if c == '&' {
			if i+1 < n && s[i+1] == '&' {
				tokens = append(tokens, shellToken{val: "&&", isOp: true})
				i += 2
				continue
			}
			if i+1 < n && s[i+1] == '>' {
				if i+2 < n && s[i+2] == '>' {
					tokens = append(tokens, shellToken{val: "&>>", isOp: true})
					i += 3
				} else {
					tokens = append(tokens, shellToken{val: "&>", isOp: true})
					i += 2
				}
				continue
			}
			tokens = append(tokens, shellToken{val: "&", isOp: true})
			i++
			continue
		}
		if c == '>' {
			if i+1 < n && s[i+1] == '>' {
				tokens = append(tokens, shellToken{val: ">>", isOp: true})
				i += 2
			} else {
				tokens = append(tokens, shellToken{val: ">", isOp: true})
				i++
			}
			continue
		}
		if c == '<' {
			if i+2 < n && s[i+1] == '<' && s[i+2] == '<' {
				tokens = append(tokens, shellToken{val: "<<<", isOp: true})
				i += 3
			} else if i+1 < n && s[i+1] == '<' {
				tokens = append(tokens, shellToken{val: "<<", isOp: true})
				i += 2
			} else {
				tokens = append(tokens, shellToken{val: "<", isOp: true})
				i++
			}
			continue
		}
		if (c == '1' || c == '2') && i+1 < n && s[i+1] == '>' {
			if i+2 < n && s[i+2] == '>' {
				tokens = append(tokens, shellToken{val: s[i : i+3], isOp: true})
				i += 3
			} else {
				tokens = append(tokens, shellToken{val: s[i : i+2], isOp: true})
				i += 2
			}
			continue
		}

		var buf strings.Builder
		hasWord := false
		for i < n {
			ch := s[i]
			if ch == '\'' {
				hasWord = true
				i++
				for i < n && s[i] != '\'' {
					buf.WriteByte(s[i])
					i++
				}
				if i < n {
					i++
				}
				continue
			}
			if ch == '"' {
				hasWord = true
				i++
				for i < n && s[i] != '"' {
					if s[i] == '\\' && i+1 < n {
						next := s[i+1]
						if next == '"' || next == '\\' || next == '$' || next == '`' {
							buf.WriteByte(next)
							i += 2
							continue
						}
						if next == '\n' {
							i += 2
							continue
						}
					}
					buf.WriteByte(s[i])
					i++
				}
				if i < n {
					i++
				}
				continue
			}
			if ch == '\\' && i+1 < n {
				hasWord = true
				if s[i+1] == '\n' {
					i += 2
					continue
				}
				buf.WriteByte(s[i+1])
				i += 2
				continue
			}
			if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' ||
				ch == ';' || ch == '|' || ch == '>' || ch == '<' {
				break
			}
			if ch == '&' && i+1 < n && (s[i+1] == '&' || s[i+1] == '>') {
				break
			}
			hasWord = true
			buf.WriteByte(ch)
			i++
		}
		if hasWord {
			tokens = append(tokens, shellToken{val: buf.String()})
		}
	}
	return tokens
}

func splitCommands(tokens []shellToken) [][]shellToken {
	var cmds [][]shellToken
	var cur []shellToken
	for _, tok := range tokens {
		if tok.isOp && isSeparator(tok.val) {
			if len(cur) > 0 {
				cmds = append(cmds, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, tok)
	}
	if len(cur) > 0 {
		cmds = append(cmds, cur)
	}
	return cmds
}

func isSeparator(op string) bool {
	switch op {
	case "&&", "||", ";", "|", "\n", "&":
		return true
	default:
		return false
	}
}

func isOutputRedir(op string) bool {
	switch op {
	case ">", ">>", "1>", "1>>", "2>", "2>>", "&>", "&>>":
		return true
	default:
		return false
	}
}

func extractRedirections(tokens []shellToken) (words []string, paths []string) {
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok.isOp {
			if isOutputRedir(tok.val) && i+1 < len(tokens) && !tokens[i+1].isOp {
				target := tokens[i+1].val
				if target != "" && target != "/dev/null" && !strings.HasPrefix(target, "&") {
					paths = append(paths, target)
				}
				i++
				continue
			}
			if (tok.val == "<" || tok.val == "<<" || tok.val == "<<<") && i+1 < len(tokens) && !tokens[i+1].isOp {
				i++
			}
			continue
		}
		words = append(words, tok.val)
	}
	return words, paths
}

func analyzeSimpleCommand(words []string) (paths []string, snapshotUpdate bool) {
	words = stripEnvPrefix(words)
	if len(words) == 0 {
		return nil, false
	}

	cmd := filepath.Base(words[0])
	args := words[1:]

	switch cmd {
	case "tee":
		paths = collectNonOptionArgs(args)
	case "sed":
		paths = sedWriteArgs(args)
	case "perl":
		paths = perlWriteArgs(args)
	case "mv", "rm":
		paths = collectNonOptionArgs(args)
	case "cp":
		nonOpts := collectNonOptionArgs(args)
		if len(nonOpts) >= 2 {
			paths = []string{nonOpts[len(nonOpts)-1]}
		}
	case "truncate":
		paths = truncateWriteArgs(args)
	case "dd":
		for _, a := range args {
			if strings.HasPrefix(a, "of=") {
				target := strings.TrimPrefix(a, "of=")
				if target != "" && target != "/dev/null" {
					paths = append(paths, target)
				}
			}
		}
	case "git":
		paths = gitWriteArgs(args)
	case "jest", "vitest":
		snapshotUpdate = hasSnapshotFlag(args)
	case "npx", "bunx":
		snapshotUpdate = runnerSnapshotUpdate(args, false)
	case "pnpm", "yarn":
		snapshotUpdate = pkgManagerSnapshotUpdate(cmd, args)
	case "npm":
		snapshotUpdate = npmSnapshotUpdate(args)
	}
	return paths, snapshotUpdate
}

func stripEnvPrefix(words []string) []string {
	i := 0
	for i < len(words) {
		w := words[i]
		if w == "env" || w == "command" {
			i++
			continue
		}
		if isEnvAssign(w) {
			i++
			continue
		}
		break
	}
	return words[i:]
}

func isEnvAssign(w string) bool {
	eq := strings.IndexByte(w, '=')
	if eq <= 0 {
		return false
	}
	name := w[:eq]
	for j := 0; j < len(name); j++ {
		c := name[j]
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
		isDigit := c >= '0' && c <= '9'
		if j == 0 && !isAlpha {
			return false
		}
		if !isAlpha && !isDigit {
			return false
		}
	}
	return true
}

func collectNonOptionArgs(args []string) []string {
	var out []string
	afterDashDash := false
	for _, a := range args {
		if !afterDashDash {
			if a == "--" {
				afterDashDash = true
				continue
			}
			if strings.HasPrefix(a, "-") && a != "-" {
				continue
			}
		}
		if a != "" {
			out = append(out, a)
		}
	}
	return out
}

func sedWriteArgs(args []string) []string {
	inPlace := false
	hasScriptFlag := false
	var positional []string
	afterDashDash := false

	for i := 0; i < len(args); i++ {
		a := args[i]
		if !afterDashDash {
			if a == "--" {
				afterDashDash = true
				continue
			}
			if a == "-i" {
				inPlace = true
				// macOS BSD sed uses `-i ''` for zero-length backup suffix
				if i+1 < len(args) && args[i+1] == "" {
					i++
				}
				continue
			}
			if strings.HasPrefix(a, "-i") || a == "--in-place" || strings.HasPrefix(a, "--in-place=") {
				inPlace = true
				continue
			}
			if a == "-e" || a == "--expression" || a == "-f" || a == "--file" {
				hasScriptFlag = true
				if i+1 < len(args) {
					i++
				}
				continue
			}
			if strings.HasPrefix(a, "--expression=") || strings.HasPrefix(a, "--file=") ||
				strings.HasPrefix(a, "-e") || strings.HasPrefix(a, "-f") {
				hasScriptFlag = true
				continue
			}
			if strings.HasPrefix(a, "-") && a != "-" {
				continue
			}
		}
		if a != "" {
			positional = append(positional, a)
		}
	}

	if !inPlace {
		return nil
	}
	if !hasScriptFlag {
		if len(positional) <= 1 {
			return nil
		}
		return positional[1:]
	}
	return positional
}

func perlWriteArgs(args []string) []string {
	inPlace := false
	var files []string
	afterDashDash := false

	for i := 0; i < len(args); i++ {
		a := args[i]
		if !afterDashDash {
			if a == "--" {
				afterDashDash = true
				continue
			}
			if a == "-e" || a == "-E" {
				if i+1 < len(args) {
					i++
				}
				continue
			}
			if strings.HasPrefix(a, "-e") || strings.HasPrefix(a, "-E") {
				continue
			}
			if strings.HasPrefix(a, "-i") {
				inPlace = true
				continue
			}
			if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && len(a) > 1 {
				flags := a[1:]
				if strings.ContainsRune(flags, 'i') {
					inPlace = true
				}
				if strings.HasSuffix(flags, "e") || strings.HasSuffix(flags, "E") {
					if i+1 < len(args) {
						i++
					}
				}
				continue
			}
			if strings.HasPrefix(a, "--") {
				continue
			}
		}
		if a != "" {
			files = append(files, a)
		}
	}

	if !inPlace {
		return nil
	}
	return files
}

func truncateWriteArgs(args []string) []string {
	var out []string
	afterDashDash := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !afterDashDash {
			if a == "--" {
				afterDashDash = true
				continue
			}
			if a == "-s" || a == "--size" || a == "-r" || a == "--reference" {
				if i+1 < len(args) {
					i++
				}
				continue
			}
			if strings.HasPrefix(a, "-") && a != "-" {
				continue
			}
		}
		if a != "" {
			out = append(out, a)
		}
	}
	return out
}

func gitWriteArgs(args []string) []string {
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "-C" || a == "-c" || a == "--git-dir" || a == "--work-tree" || a == "--namespace" {
			i += 2
			continue
		}
		if strings.HasPrefix(a, "-") {
			i++
			continue
		}
		break
	}
	if i >= len(args) {
		return nil
	}
	sub := args[i]
	rest := args[i+1:]

	switch sub {
	case "checkout":
		for j, a := range rest {
			if a == "--" {
				var out []string
				for _, p := range rest[j+1:] {
					if p != "" {
						out = append(out, p)
					}
				}
				return out
			}
		}
	case "restore":
		var out []string
		afterDashDash := false
		for j := 0; j < len(rest); j++ {
			a := rest[j]
			if !afterDashDash {
				if a == "--" {
					afterDashDash = true
					continue
				}
				if a == "-s" || a == "--source" || a == "--conflict" {
					j++
					continue
				}
				if strings.HasPrefix(a, "-") {
					continue
				}
			}
			if a != "" {
				out = append(out, a)
			}
		}
		return out
	}
	return nil
}

func isJestOrVitest(tok string) bool {
	base := filepath.Base(tok)
	return base == "jest" || base == "vitest" ||
		strings.HasPrefix(base, "jest@") || strings.HasPrefix(base, "vitest@")
}

func hasSnapshotFlag(args []string) bool {
	for _, a := range args {
		if a == "-u" || a == "--update" || a == "--updateSnapshot" {
			return true
		}
	}
	return false
}

func runnerSnapshotUpdate(args []string, allowExecSubcmd bool) bool {
	i := 0
	for i < len(args) {
		a := args[i]
		if allowExecSubcmd && (a == "exec" || a == "dlx") {
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			i++
			continue
		}
		break
	}
	if i >= len(args) {
		return false
	}
	if isJestOrVitest(args[i]) {
		return hasSnapshotFlag(args[i+1:])
	}
	return false
}

func pkgManagerSnapshotUpdate(cmd string, args []string) bool {
	if len(args) > 0 && args[0] == "test" {
		return hasSnapshotFlagAfterDashDash(args[1:])
	}
	if len(args) > 1 && args[0] == "run" && args[1] == "test" {
		return hasSnapshotFlagAfterDashDash(args[2:])
	}
	return runnerSnapshotUpdate(args, cmd == "pnpm")
}

func npmSnapshotUpdate(args []string) bool {
	if len(args) > 0 && args[0] == "test" {
		return hasSnapshotFlagAfterDashDash(args[1:])
	}
	if len(args) > 1 && args[0] == "run" && args[1] == "test" {
		return hasSnapshotFlagAfterDashDash(args[2:])
	}
	return false
}

func hasSnapshotFlagAfterDashDash(args []string) bool {
	for i, a := range args {
		if a == "--" {
			return hasSnapshotFlag(args[i+1:])
		}
	}
	return false
}
