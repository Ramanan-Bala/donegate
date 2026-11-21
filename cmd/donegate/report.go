package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Ramanan-Bala/donegate/internal/evidence"
	"github.com/Ramanan-Bala/donegate/internal/session"
	"github.com/spf13/cobra"
)

var errNoEvidence = errors.New("no evidence")

func newReportCmd() *cobra.Command {
	var sessionID string
	var repo string
	var asMD bool
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Print the evidence report for a verification session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if asMD && asJSON {
				return fmt.Errorf("cannot combine --md and --json")
			}

			var (
				sess *session.Session
				err  error
			)
			if sessionID != "" {
				sess, err = session.Load(sessionID)
				if err != nil {
					return err
				}
			} else {
				sess, err = session.Current(repo)
				if err != nil {
					if errors.Is(err, session.ErrNoSession) {
						fmt.Fprintln(cmd.ErrOrStderr(), "donegate report: no active session (no evidence yet); run `donegate session start` and `donegate gate` first")
						return errNoEvidence
					}
					return err
				}
			}

			evPath := filepath.Join(sess.Dir(), "evidence", "evidence.json")
			ev, err := evidence.Read(evPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					fmt.Fprintf(cmd.ErrOrStderr(), "donegate report: no evidence yet for session %s; run `donegate gate` first\n", sess.ID)
					return errNoEvidence
				}
				return err
			}

			if err := ev.Verify(); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "donegate report: warning: %v\n", err)
			}

			out := cmd.OutOrStdout()
			switch {
			case asMD:
				fmt.Fprint(out, evidence.Markdown(ev))
			case asJSON:
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(ev); err != nil {
					return fmt.Errorf("encode json: %w", err)
				}
			default:
				writeEvidenceText(out, ev)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID (defaults to active session for --repo)")
	cmd.Flags().StringVar(&repo, "repo", ".", "Repository directory")
	cmd.Flags().BoolVar(&asMD, "md", false, "Print report as Markdown")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print evidence as JSON")

	return cmd
}

func writeEvidenceText(out io.Writer, ev *evidence.Evidence) {
	var failed, warned []evidence.Check
	for _, c := range ev.Checks {
		switch c.Status {
		case "fail":
			failed = append(failed, c)
		case "warn":
			warned = append(warned, c)
		}
	}

	fmt.Fprintf(out, "donegate: %s (session %s, tier %s, %s, %s, %s)\n",
		ev.Verdict,
		ev.Session,
		ev.Tier,
		countLabel(ev.Blocks, "block", "blocks"),
		countLabel(len(failed), "failure", "failures"),
		countLabel(len(warned), "warning", "warnings"),
	)
	for _, c := range failed {
		if c.Detail != "" {
			fmt.Fprintf(out, "- fail %s: %s\n", c.ID, c.Detail)
		} else {
			fmt.Fprintf(out, "- fail %s\n", c.ID)
		}
	}
	for _, c := range warned {
		if c.Detail != "" {
			fmt.Fprintf(out, "- warn %s: %s\n", c.ID, c.Detail)
		} else {
			fmt.Fprintf(out, "- warn %s\n", c.ID)
		}
	}
}
