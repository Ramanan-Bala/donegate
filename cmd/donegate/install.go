package main

import (
	"fmt"
	"path/filepath"

	"github.com/Ramanan-Bala/donegate/internal/install"
	"github.com/spf13/cobra"
)

func newInstallCmd() *cobra.Command {
	var settingsPath string
	var dir string
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
			case "agy", "antigravity":
				resolvedPath, err := resolveAgyHooksPath(dir)
				if err != nil {
					return err
				}

				changed, preview, err := install.InstallAgy(install.AgyOptions{
					HooksPath: resolvedPath,
					Binary:    binaryPath,
					DryRun:    dryRun,
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
					fmt.Fprintf(out, "donegate: Antigravity hooks already installed in %s\n", resolvedPath)
					return nil
				}
				fmt.Fprintf(out, "donegate: installed Antigravity hooks in %s\n", resolvedPath)
				return nil
			case "opencode", "pi":
				opts := install.ShimOptions{
					Dir:    dir,
					Binary: binaryPath,
					DryRun: dryRun,
				}
				var (
					label   string
					path    string
					changed bool
					err     error
				)
				if harness == "opencode" {
					label = "OpenCode"
					path, changed, err = install.InstallOpenCode(opts)
				} else {
					label = "pi"
					path, changed, err = install.InstallPi(opts)
				}
				if err != nil {
					return err
				}

				out := cmd.OutOrStdout()
				if dryRun {
					if !changed {
						fmt.Fprintf(out, "donegate: %s shim already installed at %s (dry-run)\n", label, path)
						return nil
					}
					fmt.Fprintf(out, "donegate: would install %s shim at %s\n", label, path)
					return nil
				}
				if !changed {
					fmt.Fprintf(out, "donegate: %s shim already installed at %s\n", label, path)
					return nil
				}
				fmt.Fprintf(out, "donegate: installed %s shim at %s\n", label, path)
				return nil
			default:
				return fmt.Errorf("harness %q is not supported yet (supported: claude, agy, opencode, pi)", harness)
			}
		},
	}

	cmd.Flags().StringVar(&settingsPath, "settings", "", "Path to Claude Code settings.json (default ~/.claude/settings.json)")
	cmd.Flags().StringVar(&dir, "dir", "", "Target directory for agy hooks.json, opencode plugin, or pi extension")
	cmd.Flags().StringVar(&binaryPath, "binary", "", "Absolute path to donegate binary (default current executable)")
	cmd.Flags().BoolVar(&noSubagent, "no-subagent", false, "Do not install the SubagentStop hook")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print planned changes without writing")

	return cmd
}

func newUninstallCmd() *cobra.Command {
	var settingsPath string
	var dir string

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
			case "agy", "antigravity":
				resolvedPath, err := resolveAgyHooksPath(dir)
				if err != nil {
					return err
				}

				changed, err := install.UninstallAgy(install.AgyOptions{
					HooksPath: resolvedPath,
				})
				if err != nil {
					return err
				}

				out := cmd.OutOrStdout()
				if !changed {
					fmt.Fprintf(out, "donegate: no Antigravity hooks found in %s\n", resolvedPath)
					return nil
				}
				fmt.Fprintf(out, "donegate: uninstalled Antigravity hooks from %s\n", resolvedPath)
				return nil
			case "opencode", "pi":
				opts := install.ShimOptions{Dir: dir}
				var (
					label   string
					path    string
					removed bool
					err     error
				)
				if harness == "opencode" {
					label = "OpenCode"
					path, removed, err = install.UninstallOpenCode(opts)
				} else {
					label = "pi"
					path, removed, err = install.UninstallPi(opts)
				}
				if err != nil {
					return err
				}

				out := cmd.OutOrStdout()
				if !removed {
					fmt.Fprintf(out, "donegate: no %s shim found at %s\n", label, path)
					return nil
				}
				fmt.Fprintf(out, "donegate: uninstalled %s shim from %s\n", label, path)
				return nil
			default:
				return fmt.Errorf("harness %q is not supported yet (supported: claude, agy, opencode, pi)", harness)
			}
		},
	}

	cmd.Flags().StringVar(&settingsPath, "settings", "", "Path to Claude Code settings.json (default ~/.claude/settings.json)")
	cmd.Flags().StringVar(&dir, "dir", "", "Target directory for agy hooks.json, opencode plugin, or pi extension")

	return cmd
}

func resolveAgyHooksPath(dir string) (string, error) {
	if dir != "" {
		return filepath.Join(dir, "hooks.json"), nil
	}
	return install.DefaultAgyHooksPath()
}
