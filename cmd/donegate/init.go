package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ramanan-Bala/donegate/internal/gitx"
	"github.com/Ramanan-Bala/donegate/internal/initcfg"
	"github.com/Ramanan-Bala/donegate/internal/policy"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var force bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Detect the project stack and write a starter .donegate.yml",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, err := resolveInitRepoRoot(".")
			if err != nil {
				return err
			}

			outPath := filepath.Join(repoRoot, ".donegate.yml")
			if !dryRun {
				_, statErr := os.Stat(outPath)
				if statErr == nil && !force {
					return fmt.Errorf("%s already exists (use --force to overwrite)", outPath)
				}
				if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
					return fmt.Errorf("check %s: %w", outPath, statErr)
				}
			}

			res, err := initcfg.Detect(repoRoot)
			if err != nil {
				return err
			}

			rendered, err := initcfg.Render(res)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if dryRun {
				if _, err := out.Write(rendered); err != nil {
					return fmt.Errorf("write output: %w", err)
				}
				return nil
			}

			if err := os.WriteFile(outPath, rendered, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", outPath, err)
			}

			fmt.Fprintf(out, "donegate: wrote %s\n", outPath)
			for _, p := range res.Projects {
				fmt.Fprintln(out, formatInitProjectSummary(p))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing .donegate.yml")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print generated .donegate.yml to stdout without writing")

	return cmd
}

func resolveInitRepoRoot(dir string) (string, error) {
	out, err := gitx.Output(dir, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("resolve repo root: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func formatInitProjectSummary(p policy.Project) string {
	root := p.Root
	if root == "" {
		root = "."
	}
	var cmds []string
	if p.Test != "" {
		cmds = append(cmds, "test="+p.Test)
	}
	if p.Typecheck != "" {
		cmds = append(cmds, "typecheck="+p.Typecheck)
	}
	if p.Lint != "" {
		cmds = append(cmds, "lint="+p.Lint)
	}
	if len(cmds) == 0 {
		return fmt.Sprintf("- %s (%s): no commands detected", p.Name, root)
	}
	return fmt.Sprintf("- %s (%s): %s", p.Name, root, strings.Join(cmds, ", "))
}
