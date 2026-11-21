package main

import (
	"encoding/json"
	"fmt"

	"github.com/Ramanan-Bala/donegate/internal/adapters/agy"
	"github.com/Ramanan-Bala/donegate/internal/adapters/claude"
	"github.com/Ramanan-Bala/donegate/internal/adapters/jsonhook"
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

				sess, _, skipped, err := startOrReuseSession(in.Cwd, "claude-code", in.SessionID, in.Source)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "donegate session start: %v\n", err)
					return nil
				}
				if skipped {
					return nil
				}

				if err := claude.WriteSessionStart(cmd.OutOrStdout(), sessionContextMessage(sess.ID)); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "donegate session start: %v\n", err)
					return nil
				}
				return nil

			case "json":
				in, err := jsonhook.ParseSessionStart(cmd.InOrStdin())
				if err != nil {
					if werr := jsonhook.WriteSessionStartError(cmd.OutOrStdout(), err); werr != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "donegate session start: %v\n", werr)
					}
					return nil
				}

				sess, reused, skipped, err := startOrReuseSession(in.Cwd, harness, in.Session, in.Source)
				if err != nil {
					if werr := jsonhook.WriteSessionStartError(cmd.OutOrStdout(), err); werr != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "donegate session start: %v\n", werr)
					}
					return nil
				}
				if skipped {
					if werr := jsonhook.WriteSessionStartSkipped(cmd.OutOrStdout()); werr != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "donegate session start: %v\n", werr)
					}
					return nil
				}

				if err := jsonhook.WriteSessionStart(cmd.OutOrStdout(), sess.ID, reused, sessionContextMessage(sess.ID)); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "donegate session start: %v\n", err)
					return nil
				}
				return nil

			case "agy":
				in, err := agy.ParseSessionStart(cmd.InOrStdin())
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "donegate session start: %v\n", err)
					_ = agy.WriteEmpty(cmd.OutOrStdout())
					return nil
				}

				sess, _, skipped, err := startOrReuseSession(in.Cwd, "antigravity", in.ConversationID, "resume")
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "donegate session start: %v\n", err)
					_ = agy.WriteEmpty(cmd.OutOrStdout())
					return nil
				}
				if skipped {
					if werr := agy.WriteEmpty(cmd.OutOrStdout()); werr != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "donegate session start: %v\n", werr)
					}
					return nil
				}

				if err := agy.WriteSessionStart(cmd.OutOrStdout(), sessionContextMessage(sess.ID)); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "donegate session start: %v\n", err)
					_ = agy.WriteEmpty(cmd.OutOrStdout())
					return nil
				}
				return nil

			default:
				return fmt.Errorf("unsupported session format %q (supported: text, claude, json, agy)", format)
			}
		},
	}

	cmd.Flags().StringVar(&repo, "repo", ".", "Repository directory")
	cmd.Flags().StringVar(&harness, "harness", "manual", "Harness name")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print session as JSON")
	cmd.Flags().StringVar(&format, "format", "text", "Output format (text, claude, json, agy)")

	return cmd
}

func startOrReuseSession(cwd, harness, harnessSession, source string) (sess *session.Session, reused bool, skipped bool, err error) {
	if cwd == "" {
		cwd = "."
	}
	if _, err := gitx.Output(cwd, nil, "rev-parse", "--show-toplevel"); err != nil {
		return nil, false, true, nil
	}

	if (source == "resume" || source == "compact") && harnessSession != "" {
		if cur, err := session.Current(cwd); err == nil && cur.HarnessSession == harnessSession {
			return cur, true, false, nil
		}
	}

	sess, err = session.StartWith(cwd, session.StartOptions{
		Harness:        harness,
		HarnessSession: harnessSession,
	})
	if err != nil {
		return nil, false, false, err
	}
	return sess, false, false, nil
}

func sessionContextMessage(sessionID string) string {
	return fmt.Sprintf("donegate is active for this repo (session %s). Existing tests, snapshots, test/lint config and .donegate.yml are read-only; you may add new test files. When you stop, donegate re-checks your work and will block with specific failures to fix.", sessionID)
}
