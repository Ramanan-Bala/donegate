package main

import (
	"fmt"

	"github.com/Ramanan-Bala/donegate/internal/install"
	"github.com/spf13/cobra"
)

func newInstallCmd() *cobra.Command {
	var settingsPath string
	var binaryPath string
	var noSubagent bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "install <harness>",
		Short: "Install donegate hooks for an AI coding harness",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			harness := args[0]
			switch harness {
			case "claude":
				resolvedPath := settingsPath
				if resolvedPath == "" {
					var err error
					resolvedPath, err = install.DefaultClaudeSettingsPath()
					if err != nil {
						return err
					}
				}

				changed, preview, err := install.InstallClaude(install.ClaudeOptions{
					SettingsPath: resolvedPath,
					Binary:       binaryPath,
					SubagentStop: !noSubagent,
					DryRun:       dryRun,
				})
				if err != nil {
					return err
				}

				out := cmd.OutOrStdout()
				if dryRun {
					if _, err := out.Write(preview); err != nil {
						return fmt.Errorf("write preview: %w", err)
					}
					return nil
				}
				if !changed {
					fmt.Fprintf(out, "donegate: Claude Code hooks already installed in %s\n", resolvedPath)
					return nil
				}
				fmt.Fprintf(out, "donegate: installed Claude Code hooks in %s\n", resolvedPath)
				return nil
			default:
				return fmt.Errorf("harness %q is not supported yet (supported: claude)", harness)
			}
		},
	}

	cmd.Flags().StringVar(&settingsPath, "settings", "", "Path to Claude Code settings.json (default ~/.claude/settings.json)")
	cmd.Flags().StringVar(&binaryPath, "binary", "", "Absolute path to donegate binary (default current executable)")
	cmd.Flags().BoolVar(&noSubagent, "no-subagent", false, "Do not install the SubagentStop hook")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print resulting settings JSON without writing")

	return cmd
}

func newUninstallCmd() *cobra.Command {
	var settingsPath string

	cmd := &cobra.Command{
		Use:   "uninstall <harness>",
		Short: "Uninstall donegate hooks for an AI coding harness",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			harness := args[0]
			switch harness {
			case "claude":
				resolvedPath := settingsPath
				if resolvedPath == "" {
					var err error
					resolvedPath, err = install.DefaultClaudeSettingsPath()
					if err != nil {
						return err
					}
				}

				changed, err := install.UninstallClaude(install.ClaudeOptions{
					SettingsPath: resolvedPath,
				})
				if err != nil {
					return err
				}

				out := cmd.OutOrStdout()
				if !changed {
					fmt.Fprintf(out, "donegate: no Claude Code hooks found in %s\n", resolvedPath)
					return nil
				}
				fmt.Fprintf(out, "donegate: uninstalled Claude Code hooks from %s\n", resolvedPath)
				return nil
			default:
				return fmt.Errorf("harness %q is not supported yet (supported: claude)", harness)
			}
		},
	}

	cmd.Flags().StringVar(&settingsPath, "settings", "", "Path to Claude Code settings.json (default ~/.claude/settings.json)")

	return cmd
}
