package rules

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Ramanan-Bala/donegate/internal/diff"
)

func init() {
	Register(c001{})
}

var (
	c001PassWithNoTestsRe     = regexp.MustCompile(`\bpassWithNoTests\W*\s*:?\s*true\b`)
	c001StrictFalseRe         = regexp.MustCompile(`(?:^|[\s,{])['"]?strict['"]?\s*:\s*false\b`)
	c001NoImplicitAnyRe       = regexp.MustCompile(`(?:^|[\s,{])['"]?noImplicitAny['"]?\s*:\s*false\b`)
	c001FilterKeyRe           = regexp.MustCompile(`(?:^|[\s,{])['"]?(include|exclude|testMatch|testPathIgnorePatterns|coveragePathIgnorePatterns|ignorePatterns|ignores)['"]?\s*:`)
	c001EslintOffRe           = regexp.MustCompile(`:\s*(?:\[\s*)?['"]?(?:off|0)['"]?\s*[,\s}\]]*$`)
	c001WorkflowLoosenRe      = regexp.MustCompile(`continue-on-error\s*:\s*true\b|\|\|\s*true\b`)
	c001CoverageRe            = regexp.MustCompile(`['"]?(lines|branches|functions|statements)['"]?\s*:\s*(\d+)`)
	c001PkgLoosenRe           = regexp.MustCompile(`\|\|\s*true\b|\|\|\s*exit\s+0\b|;\s*exit\s+0\b|--passWithNoTests\b`)
	c001DotnetCompileRemoveRe = regexp.MustCompile(`<\s*Compile\s+Remove\s*=`)
	c001PytestNarrowRe        = regexp.MustCompile(`--ignore\b|--deselect\b|--continue-on-collection-errors\b|(?:^|[\s"'])-k(?:\s|=|["'])`)
	c001GoTestFilterRe        = regexp.MustCompile(`\bgo\s+test\b.*\s--?(?:run|skip)(?:\s|=|$)`)
)

type c001 struct{}

func (c001) ID() string                { return "C001" }
func (c001) Level() int                { return 1 }
func (c001) DefaultSeverity() Severity { return Block }

func (c001) Check(c *Context) []Finding {
	if c == nil || c.Diff == nil || c.Policy == nil {
		return nil
	}
	var findings []Finding
	for _, f := range c.Diff.Files {
		normPath := normalizeRulePath(f.Path)

		isCfg := c.Policy.IsConfig(f.Path) || (f.Status == diff.Renamed && f.OldPath != "" && c.Policy.IsConfig(f.OldPath))
		if isCfg {
			switch f.Status {
			case diff.Deleted:
				findings = append(findings, Finding{
					RuleID:   "C001",
					Severity: Block,
					Path:     f.Path,
					Message:  fmt.Sprintf("config file %s was deleted", f.Path),
					Hint:     "restore the configuration file instead of deleting checks",
				})
			case diff.Modified, diff.Renamed:
				if !f.Binary {
					findings = append(findings, c001CheckConfigFile(f, normPath)...)
				}
			}
		}

		if f.Status == diff.Modified && (normPath == "package.json" || strings.HasSuffix(normPath, "/package.json")) {
			findings = append(findings, c001CheckPackageJSON(c.Diff, f)...)
		}

		if (f.Status == diff.Modified || f.Status == diff.Renamed) && !f.Binary && path.Base(normPath) == ".eslintignore" {
			for _, line := range f.Added {
				trimmed := strings.TrimSpace(line.Text)
				if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
					findings = append(findings, Finding{
						RuleID:   "C001",
						Severity: Block,
						Path:     f.Path,
						Line:     line.No,
						Message:  fmt.Sprintf("added ignore entry %q in %s:%d", trimmed, f.Path, line.No),
						Hint:     "remove the new ignore entry and fix the lint issues in source instead",
					})
				}
			}
		}
	}
	return findings
}

func c001CheckConfigFile(f diff.File, normPath string) []Finding {
	removedCov := map[string]int{}
	for _, line := range f.Removed {
		for _, m := range c001CoverageRe.FindAllStringSubmatch(line.Text, -1) {
			val, err := strconv.Atoi(m[2])
			if err != nil {
				continue
			}
			if prev, ok := removedCov[m[1]]; !ok || val > prev {
				removedCov[m[1]] = val
			}
		}
	}

	isEslint := c001IsEslintConfig(normPath)
	isWorkflow := strings.HasPrefix(normPath, ".github/workflows/")
	isDotnetProj := c001IsDotnetProject(normPath)
	isPytestCfg := c001IsPytestConfig(normPath)
	isGoTestRunner := isWorkflow || c001IsMakefile(normPath)

	var findings []Finding
	for _, line := range f.Added {
		trimmed := strings.TrimSpace(line.Text)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		var reason string
		switch {
		case c001PassWithNoTestsRe.MatchString(line.Text):
			reason = "passWithNoTests enabled"
		case c001StrictFalseRe.MatchString(line.Text):
			reason = "strict set to false"
		case c001NoImplicitAnyRe.MatchString(line.Text):
			reason = "noImplicitAny set to false"
		case c001FilterKeyRe.MatchString(line.Text) && !c001FilterWidened(line.Text, f.Removed):
			reason = "test/lint include or ignore pattern modified"
		case isEslint && c001EslintOffRe.MatchString(line.Text):
			reason = "ESLint rule turned off"
		case isWorkflow && c001WorkflowLoosenRe.MatchString(line.Text):
			reason = "CI workflow failure suppressed"
		case isDotnetProj && c001DotnetCompileRemoveRe.MatchString(line.Text):
			reason = "test source excluded from build"
		case isPytestCfg && c001PytestNarrowRe.MatchString(line.Text):
			reason = "pytest test selection narrowed"
		case isGoTestRunner && c001GoTestFilterRe.MatchString(line.Text):
			reason = "go test filtered with -run/-skip"
		default:
			for _, m := range c001CoverageRe.FindAllStringSubmatch(line.Text, -1) {
				val, err := strconv.Atoi(m[2])
				if err != nil {
					continue
				}
				if oldVal, ok := removedCov[m[1]]; ok && val < oldVal {
					reason = fmt.Sprintf("coverage threshold %s lowered from %d to %d", m[1], oldVal, val)
					break
				}
			}
		}
		if reason != "" {
			findings = append(findings, Finding{
				RuleID:   "C001",
				Severity: Block,
				Path:     f.Path,
				Line:     line.No,
				Message:  fmt.Sprintf("config loosened (%s) in %s:%d", reason, f.Path, line.No),
				Hint:     "revert the config relaxation and fix the underlying test, type, or lint failures",
			})
		}
	}
	return findings
}

// c001InclusionKeys widen coverage when entries are added; every other filter
// key (exclude, ignore patterns) widens coverage when entries are removed.
var c001InclusionKeys = map[string]bool{"include": true, "testMatch": true}

// c001FilterWidened reports whether an added filter-key line only widens test
// coverage compared with a removed line for the same key. Both lines must hold
// a complete inline array; anything else is treated as loosening.
func c001FilterWidened(added string, removed []diff.Line) bool {
	m := c001FilterKeyRe.FindStringSubmatch(added)
	if m == nil {
		return false
	}
	key := m[1]
	newEntries, ok := c001InlineEntries(added)
	if !ok {
		return false
	}
	for _, r := range removed {
		rm := c001FilterKeyRe.FindStringSubmatch(r.Text)
		if rm == nil || rm[1] != key {
			continue
		}
		oldEntries, ok := c001InlineEntries(r.Text)
		if !ok {
			return false
		}
		if c001InclusionKeys[key] {
			return c001Subset(oldEntries, newEntries)
		}
		return c001Subset(newEntries, oldEntries)
	}
	return false
}

// c001InlineEntries parses the entries of a single-line array such as
// `include: ["a", 'b']`. It returns false when the line has no complete array.
func c001InlineEntries(line string) (map[string]bool, bool) {
	start := strings.Index(line, "[")
	end := strings.LastIndex(line, "]")
	if start < 0 || end < start {
		return nil, false
	}
	entries := map[string]bool{}
	for _, part := range strings.Split(line[start+1:end], ",") {
		e := strings.Trim(strings.TrimSpace(part), `"'`+"`")
		if e != "" {
			entries[e] = true
		}
	}
	return entries, true
}

func c001Subset(a, b map[string]bool) bool {
	for e := range a {
		if !b[e] {
			return false
		}
	}
	return true
}

func c001IsEslintConfig(normPath string) bool {
	base := strings.ToLower(path.Base(normPath))
	return strings.HasPrefix(base, ".eslintrc") || strings.HasPrefix(base, "eslint.config.")
}

func c001IsDotnetProject(normPath string) bool {
	base := strings.ToLower(path.Base(normPath))
	ext := path.Ext(base)
	return ext == ".csproj" || ext == ".fsproj" || ext == ".vbproj" || base == "directory.build.props"
}

func c001IsPytestConfig(normPath string) bool {
	switch strings.ToLower(path.Base(normPath)) {
	case "pytest.ini", "setup.cfg", "tox.ini", "pyproject.toml":
		return true
	}
	return false
}

func c001IsMakefile(normPath string) bool {
	base := strings.ToLower(path.Base(normPath))
	return base == "makefile" || strings.HasSuffix(base, ".mk")
}

type c001PackageJSON struct {
	Scripts map[string]string `json:"scripts"`
}

func c001CheckPackageJSON(d *diff.Diff, f diff.File) []Finding {
	if d.Base == "" {
		return nil
	}
	baseData, err := d.ReadBase(f.Path)
	if err != nil {
		return nil
	}
	headData, err := d.ReadHead(f.Path)
	if err != nil {
		return nil
	}
	var basePkg, headPkg c001PackageJSON
	if err := json.Unmarshal(baseData, &basePkg); err != nil {
		return nil
	}
	if err := json.Unmarshal(headData, &headPkg); err != nil {
		return nil
	}

	namesSet := map[string]struct{}{}
	for name := range basePkg.Scripts {
		if c001IsWatchedScript(name) {
			namesSet[name] = struct{}{}
		}
	}
	for name := range headPkg.Scripts {
		if c001IsWatchedScript(name) {
			namesSet[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(namesSet))
	for name := range namesSet {
		names = append(names, name)
	}
	sort.Strings(names)

	var findings []Finding
	for _, name := range names {
		baseCmd, inBase := basePkg.Scripts[name]
		headCmd, inHead := headPkg.Scripts[name]
		if inBase && !inHead {
			findings = append(findings, Finding{
				RuleID:   "C001",
				Severity: Block,
				Path:     f.Path,
				Message:  fmt.Sprintf("script %q was removed from %s", name, f.Path),
				Hint:     fmt.Sprintf("restore script %q in %s instead of removing verification", name, f.Path),
			})
			continue
		}
		if inHead {
			if match := c001PkgLoosenRe.FindString(headCmd); match != "" && !c001PkgLoosenRe.MatchString(baseCmd) {
				findings = append(findings, Finding{
					RuleID:   "C001",
					Severity: Block,
					Path:     f.Path,
					Message:  fmt.Sprintf("script %q in %s added %q", name, f.Path, match),
					Hint:     fmt.Sprintf("remove %q from script %q so failures are not masked", match, name),
				})
			}
		}
	}
	return findings
}

func c001IsWatchedScript(name string) bool {
	switch name {
	case "test", "lint", "typecheck", "check", "ci":
		return true
	}
	return strings.HasPrefix(name, "test:") || strings.HasPrefix(name, "lint:")
}
