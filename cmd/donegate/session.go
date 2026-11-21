package main

import (
	"encoding/json"
	"fmt"

	"github.com/Ramanan-Bala/donegate/internal/adapters/claude"
	"github.com/Ramanan-Bala/donegate/internal/gitx"
	"github.com/Ramanan-Bala/donegate/internal/session"
	"github.com/spf13/cobra"
)

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage verification sessions",
	}
	cmd.AddCommand(newSessionStartCmd())
	return cmd
}

func newSessionStartCmd() *cobra.Command {
	var repo string
	var harness string
	var asJSON bool
	var format string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Snapshot repo state and policy to start a verification session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch format {
			case "text":
				sess, err := session.Start(repo, harness)
				if err != nil {
					return err
				}

				out := cmd.OutOrStdout()
				if asJSON {
					enc := json.NewEncoder(out)
					enc.SetIndent("", "  ")
					if err := enc.Encode(sess); err != nil {
						return fmt.Errorf("encode json: %w", err)
					}
					return nil
				}

				shortBase := sess.Base
				if len(shortBase) > 7 {
					shortBase = shortBase[:7]
				}
				fmt.Fprintf(out, "donegate: session %s started (base %s, %d protected files)\n", sess.ID, shortBase, len(sess.Protected))
				return nil

			case "claude":
				in, err := claude.ParseSessionStart(cmd.InOrStdin())
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "donegate session start: %v\n", err)
					return nil
				}
				cwd := in.Cwd
				if cwd == "" {
					cwd = "."
				}
				if _, err := gitx.Output(cwd, nil, "rev-parse", "--show-toplevel"); err != nil {
					return nil
				}

				var sess *session.Session
				if (in.Source == "resume" || in.Source == "compact") && in.SessionID != "" {
					if cur, err := session.Current(cwd); err == nil && cur.HarnessSession == in.SessionID {
						sess = cur
					}
				}
				if sess == nil {
					sess, err = session.StartWith(cwd, session.StartOptions{
						Harness:        "claude-code",
						HarnessSession: in.SessionID,
					})
					if err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "donegate session start: %v\n", err)
						return nil
					}
				}

				ctxMsg := fmt.Sprintf("donegate is active for this repo (session %s). Existing tests, snapshots, test/lint config and .donegate.yml are read-only; you may add new test files. When you stop, donegate re-checks your work and will block with specific failures to fix.", sess.ID)
				if err := claude.WriteSessionStart(cmd.OutOrStdout(), ctxMsg); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "donegate session start: %v\n", err)
					return nil
				}
				return nil

			default:
				return fmt.Errorf("unsupported session format %q (supported: text, claude)", format)
			}
		},
	}

	cmd.Flags().StringVar(&repo, "repo", ".", "Repository directory")
	cmd.Flags().StringVar(&harness, "harness", "manual", "Harness name")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print session as JSON")
	cmd.Flags().StringVar(&format, "format", "text", "Output format (text, claude)")

	return cmd
}
