package verify

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/Ramanan-Bala/donegate/internal/diff"
	"github.com/Ramanan-Bala/donegate/internal/l2"
	"github.com/Ramanan-Bala/donegate/internal/policy"
	"github.com/Ramanan-Bala/donegate/internal/rules"
	"gopkg.in/yaml.v3"
)

// Options configures a verification run against a base git ref.
type Options struct {
	RepoDir  string
	Base     string
	Policy   *policy.Policy
	Tampered []rules.Tamper
	MaxLevel int
	Restore  []string
	CacheDir string
	Context  context.Context
}

// Result holds the resolved base commit, gate verdict, and sorted findings.
type Result struct {
	Base        string          `json:"base"`
	Verdict     string          `json:"verdict"`
	RuleIDs     []string        `json:"rule_ids"`
	Findings    []rules.Finding `json:"findings"`
	Diff        *diff.Diff      `json:"-"`
	PolicyBytes []byte          `json:"-"`
}

// Run loads the diff against opts.Base, applies the base-commit policy (or opts.Policy when non-nil),
// and evaluates registered rules up to opts.MaxLevel.
func Run(opts Options) (*Result, error) {
	if opts.Base == "" {
		return nil, fmt.Errorf("verify: base ref is required")
	}
	repoDir := opts.RepoDir
	if repoDir == "" {
		repoDir = "."
	}

	d, err := diff.Load(repoDir, opts.Base)
	if err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}

	pol := opts.Policy
	var policyBytes []byte
	if pol == nil {
		pol, policyBytes, err = loadBasePolicy(d)
		if err != nil {
			return nil, fmt.Errorf("verify: %w", err)
		}
	} else {
		policyBytes, err = yaml.Marshal(pol)
		if err != nil {
			return nil, fmt.Errorf("verify: marshal policy: %w", err)
		}
	}

	ctx := &rules.Context{
		Diff:     d,
		Policy:   pol,
		Tampered: opts.Tampered,
	}

	type activeRule struct {
		rule rules.Rule
		sev  rules.Severity
	}
	byLevel := map[int][]activeRule{}
	var levels []int
	for _, r := range rules.All() {
		if opts.MaxLevel > 0 && r.Level() > opts.MaxLevel {
			continue
		}
		sev := r.DefaultSeverity()
		if override, ok := pol.Rules[r.ID()]; ok {
			sev = rules.Severity(override)
		}
		if sev == rules.Off {
			continue
		}
		lvl := r.Level()
		if _, seen := byLevel[lvl]; !seen {
			levels = append(levels, lvl)
		}
		byLevel[lvl] = append(byLevel[lvl], activeRule{rule: r, sev: sev})
	}
	sort.Ints(levels)

	l2Attempted := false
	ruleIDs := []string{}
	findings := []rules.Finding{}
	for _, lvl := range levels {
		for _, ar := range byLevel[lvl] {
			if lvl == 2 && !l2Attempted {
				l2Attempted = true
				if shouldRunL2(opts.MaxLevel, pol, d, opts.Tampered) {
					runCtx := opts.Context
					if runCtx == nil {
						runCtx = context.Background()
					}
					runs, baselineRuns, err := l2.Run(runCtx, l2.Options{
						RepoDir:  d.RepoDir,
						Base:     d.Base,
						Policy:   pol,
						Restore:  opts.Restore,
						CacheDir: opts.CacheDir,
					})
					if err != nil {
						return nil, fmt.Errorf("verify: %w", err)
					}
					ctx.Runs = runs
					ctx.BaselineRuns = baselineRuns
				}
			}
			ruleIDs = append(ruleIDs, ar.rule.ID())
			for _, f := range ar.rule.Check(ctx) {
				if f.RuleID == "" {
					f.RuleID = ar.rule.ID()
				}
				f.Severity = ar.sev
				findings = append(findings, f)
			}
		}
		if hasBlockFinding(findings) {
			break
		}
	}
	sort.Strings(ruleIDs)

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].RuleID != findings[j].RuleID {
			return findings[i].RuleID < findings[j].RuleID
		}
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Line < findings[j].Line
	})

	verdict := "PASS"
	for _, f := range findings {
		if f.Severity == rules.Block {
			verdict = "FAIL"
			break
		}
	}

	return &Result{
		Base:        d.Base,
		Verdict:     verdict,
		RuleIDs:     ruleIDs,
		Findings:    findings,
		Diff:        d,
		PolicyBytes: policyBytes,
	}, nil
}

// loadBasePolicy reads .donegate.yml from the base commit, falling back to policy.Default()
// if the file did not exist there. Working-tree edits to .donegate.yml are ignored.
func loadBasePolicy(d *diff.Diff) (*policy.Policy, []byte, error) {
	exists, err := baseFileExists(d.RepoDir, d.Base, ".donegate.yml")
	if err != nil {
		return nil, nil, err
	}
	if !exists {
		pol := policy.Default()
		defaultBytes, err := yaml.Marshal(pol)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal default policy: %w", err)
		}
		return pol, defaultBytes, nil
	}
	data, err := d.ReadBase(".donegate.yml")
	if err != nil {
		return nil, nil, err
	}
	pol, err := policy.Parse(data)
	if err != nil {
		return nil, nil, err
	}
	return pol, data, nil
}

func baseFileExists(repoDir, baseSHA, path string) (bool, error) {
	cmd := exec.Command("git", "ls-tree", baseSHA, "--", path)
	cmd.Dir = repoDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return false, fmt.Errorf("check %s in base %s: %w: %s", path, baseSHA, err, msg)
		}
		return false, fmt.Errorf("check %s in base %s: %w", path, baseSHA, err)
	}
	return strings.TrimSpace(stdout.String()) != "", nil
}

func hasBlockFinding(findings []rules.Finding) bool {
	for _, f := range findings {
		if f.Severity == rules.Block {
			return true
		}
	}
	return false
}

func shouldRunL2(maxLevel int, pol *policy.Policy, d *diff.Diff, tampered []rules.Tamper) bool {
	if (d == nil || len(d.Files) == 0) && len(tampered) == 0 {
		return false
	}
	if maxLevel > 0 && maxLevel < 2 {
		return false
	}
	for _, r := range rules.All() {
		if r.Level() != 2 {
			continue
		}
		sev := r.DefaultSeverity()
		if pol != nil {
			if override, ok := pol.Rules[r.ID()]; ok {
				sev = rules.Severity(override)
			}
		}
		if sev != rules.Off {
			return true
		}
	}
	return false
}
