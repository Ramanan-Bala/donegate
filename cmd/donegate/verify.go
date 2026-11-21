package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/Ramanan-Bala/donegate/internal/evidence"
	"github.com/Ramanan-Bala/donegate/internal/gitx"
	"github.com/Ramanan-Bala/donegate/internal/rules"
	"github.com/Ramanan-Bala/donegate/internal/verify"
	"github.com/spf13/cobra"
)

var errVerifyFailed = errors.New("verification failed")

func newVerifyCmd() *cobra.Command {
	var base string
	var repo string
	var tier string
	var asJSON bool
	var asMD bool
	var evidenceDir string

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify changes against a base git ref",
		RunE: func(cmd *cobra.Command, args []string) error {
			if asJSON && asMD {
				return fmt.Errorf("cannot combine --json and --md")
			}

			maxLevel, err := tierMaxLevel(tier)
			if err != nil {
				return err
			}

			res, err := verify.Run(verify.Options{
				RepoDir:  repo,
				Base:     base,
				MaxLevel: maxLevel,
			})
			if err != nil {
				return err
			}

			var ev *evidence.Evidence
			if evidenceDir != "" || asMD {
				ev, err = buildVerifyEvidence(repo, tier, res)
				if err != nil {
					return err
				}
				if evidenceDir != "" {
					if _, _, err := evidence.Write(evidenceDir, ev); err != nil {
						return fmt.Errorf("write evidence: %w", err)
					}
				}
			}

			out := cmd.OutOrStdout()
			switch {
			case asJSON:
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(res); err != nil {
					return fmt.Errorf("encode json: %w", err)
				}
			case asMD:
				fmt.Fprint(out, evidence.Markdown(ev))
			default:
				writeTextReport(out, res)
			}

			if res.Verdict == "FAIL" {
				return errVerifyFailed
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&base, "base", "", "Base git ref to diff against (required)")
	cmd.Flags().StringVar(&repo, "repo", ".", "Repository directory")
	cmd.Flags().StringVar(&tier, "tier", "strict", "Verification tier (fast, standard, strict)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print result as JSON")
	cmd.Flags().BoolVar(&asMD, "md", false, "Print markdown evidence report")
	cmd.Flags().StringVar(&evidenceDir, "evidence-dir", "", "Directory to write evidence.json and evidence.md")
	_ = cmd.MarkFlagRequired("base")

	return cmd
}

func buildVerifyEvidence(repo, tier string, res *verify.Result) (*evidence.Evidence, error) {
	repoDir := repo
	if res.Diff != nil && res.Diff.RepoDir != "" {
		repoDir = res.Diff.RepoDir
	}
	if repoDir == "" {
		repoDir = "."
	}

	var diffBytes []byte
	if res.Diff != nil {
		var err error
		diffBytes, err = json.Marshal(res.Diff.Files)
		if err != nil {
			return nil, fmt.Errorf("encode diff files: %w", err)
		}
	}

	return evidence.Build(evidence.Input{
		Session:  "",
		Harness:  "ci",
		Repo:     resolveVerifyRepoIdentifier(repoDir),
		Base:     res.Base,
		Head:     resolveVerifyHead(repoDir),
		Tier:     tier,
		Blocks:   0,
		RuleIDs:  res.RuleIDs,
		Findings: res.Findings,
		Policy:   res.PolicyBytes,
		Diff:     diffBytes,
		Now:      time.Now().UTC(),
	}), nil
}

func resolveVerifyRepoIdentifier(repoDir string) string {
	out, err := gitx.Output(repoDir, nil, "config", "--get", "remote.origin.url")
	if err == nil {
		if u := strings.TrimSpace(string(out)); u != "" {
			return u
		}
	}
	if abs, err := filepath.Abs(repoDir); err == nil {
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			return resolved
		}
		return abs
	}
	return repoDir
}

func resolveVerifyHead(repoDir string) string {
	out, err := gitx.Output(repoDir, nil, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func tierMaxLevel(tier string) (int, error) {
	switch tier {
	case "fast":
		return 1, nil
	case "standard":
		return 2, nil
	case "strict":
		return 0, nil
	default:
		return 0, fmt.Errorf("unknown tier %q (expected fast, standard, or strict)", tier)
	}
}

func writeTextReport(out io.Writer, res *verify.Result) {
	var blocks, warns []rules.Finding
	for _, f := range res.Findings {
		switch f.Severity {
		case rules.Block:
			blocks = append(blocks, f)
		case rules.Warn:
			warns = append(warns, f)
		}
	}

	if res.Verdict == "FAIL" {
		fmt.Fprintf(out, "donegate: not done (%s)\n", countLabel(len(blocks), "failure", "failures"))
		for _, f := range blocks {
			fmt.Fprintln(out, formatFinding("", f))
		}
		for _, f := range warns {
			fmt.Fprintln(out, formatFinding("warn ", f))
		}
		return
	}

	fmt.Fprintf(out, "donegate: PASS (%s)\n", countLabel(len(warns), "warning", "warnings"))
	for _, f := range warns {
		fmt.Fprintln(out, formatFinding("warn ", f))
	}
}

func formatFinding(prefix string, f rules.Finding) string {
	s := fmt.Sprintf("- %s%s %s", prefix, f.RuleID, f.Message)
	if f.Hint != "" {
		s += "; " + f.Hint
	}
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

func countLabel(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}
