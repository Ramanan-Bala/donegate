package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ramanan-Bala/donegate/internal/allow"
	"github.com/Ramanan-Bala/donegate/internal/session"
	"github.com/spf13/cobra"
)

var isAllowTTY = defaultIsAllowTTY

var errAllowHumanOnly = errors.New("donegate allow must be run by a human in a terminal; agents cannot approve their own exceptions")

func defaultIsAllowTTY() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func newAllowCmd() *cobra.Command {
	var repo string
	var sessionID string
	var list bool
	var revoke bool

	cmd := &cobra.Command{
		Use:   "allow <RULE|all> <path>...",
		Short: "Grant human-approved session exceptions for protected files",
		RunE: func(cmd *cobra.Command, args []string) error {
			if list && revoke {
				return fmt.Errorf("cannot combine --list and --revoke")
			}

			switch {
			case list:
				if len(args) > 0 {
					return fmt.Errorf("--list takes no positional arguments")
				}
				sess, err := resolveAllowSession(repo, sessionID)
				if err != nil {
					return err
				}
				entries, err := allow.List(sess)
				if err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				if len(entries) == 0 {
					fmt.Fprintf(out, "donegate: no allowances in session %s\n", sess.ID)
					return nil
				}
				fmt.Fprintf(out, "Allowances for session %s:\n", sess.ID)
				for _, e := range entries {
					fmt.Fprintf(out, "- %s\t%s\t(by %s at %s)\n", e.Rule, e.Path, e.By, e.At)
				}
				return nil

			case revoke:
				if len(args) == 0 {
					return fmt.Errorf("--revoke requires at least one path")
				}
				sess, err := resolveAllowSession(repo, sessionID)
				if err != nil {
					return err
				}
				resolved := resolveAllowPaths(sess, args)
				normPaths, err := allow.NormalizePaths(sess, resolved)
				if err != nil {
					return err
				}
				if err := allow.Revoke(sess, resolved); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "donegate: revoked allowances for %s in session %s\n", strings.Join(normPaths, ", "), sess.ID)
				return nil

			default:
				if len(args) < 2 {
					return fmt.Errorf("usage: donegate allow <RULE|all> <path>...")
				}
				if !isAllowTTY() {
					return errAllowHumanOnly
				}
				sess, err := resolveAllowSession(repo, sessionID)
				if err != nil {
					return err
				}
				rule := args[0]
				if err := allow.ValidateRule(rule); err != nil {
					return err
				}
				resolved := resolveAllowPaths(sess, args[1:])
				normPaths, err := allow.NormalizePaths(sess, resolved)
				if err != nil {
					return err
				}

				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "Session: %s (%s)\n", sess.ID, sess.Repo)
				fmt.Fprintf(out, "Rule:    %s\n", rule)
				fmt.Fprintln(out, "Paths:")
				for _, p := range normPaths {
					fmt.Fprintf(out, "  - %s\n", p)
				}
				fmt.Fprint(out, "Type 'yes' to approve: ")

				reader := bufio.NewReader(cmd.InOrStdin())
				line, readErr := reader.ReadString('\n')
				if readErr != nil && !errors.Is(readErr, io.EOF) {
					return fmt.Errorf("read confirmation: %w", readErr)
				}
				if strings.TrimSpace(line) != "yes" {
					return fmt.Errorf("aborted: confirmation must be 'yes'")
				}

				if err := allow.Add(sess, rule, resolved, os.Getenv("USER")); err != nil {
					return err
				}
				fmt.Fprintf(out, "donegate: allowed %s for %s in session %s\n", rule, strings.Join(normPaths, ", "), sess.ID)
				return nil
			}
		},
	}

	cmd.Flags().StringVar(&repo, "repo", ".", "Repository directory")
	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID (defaults to active session for --repo)")
	cmd.Flags().BoolVar(&list, "list", false, "List allowances for the session")
	cmd.Flags().BoolVar(&revoke, "revoke", false, "Revoke allowances for the given paths")

	return cmd
}

func resolveAllowSession(repo, sessionID string) (*session.Session, error) {
	if sessionID != "" {
		return session.Load(sessionID)
	}
	return session.Current(repo)
}

func resolveAllowPaths(sess *session.Session, rawPaths []string) []string {
	cwd, err := os.Getwd()
	if err != nil {
		return rawPaths
	}
	resolvedCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		resolvedCwd = filepath.Clean(cwd)
	}
	resolvedRepo, err := filepath.EvalSymlinks(sess.Repo)
	if err != nil {
		resolvedRepo = filepath.Clean(sess.Repo)
	}
	relCwd, relErr := filepath.Rel(resolvedRepo, resolvedCwd)
	cwdInsideSubdir := relErr == nil && relCwd != "." && relCwd != ".." && !strings.HasPrefix(relCwd, ".."+string(filepath.Separator))
	if !cwdInsideSubdir {
		return rawPaths
	}

	out := make([]string, len(rawPaths))
	for i, p := range rawPaths {
		if filepath.IsAbs(p) {
			out[i] = p
			continue
		}
		cwdTarget := filepath.Join(resolvedCwd, p)
		repoTarget := filepath.Join(resolvedRepo, p)
		if _, err := os.Lstat(cwdTarget); err == nil {
			if _, repoErr := os.Lstat(repoTarget); repoErr != nil {
				out[i] = cwdTarget
				continue
			}
		}
		out[i] = p
	}
	return out
}
