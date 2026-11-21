package main

import (
	"fmt"

	"github.com/Ramanan-Bala/donegate/internal/adapters/claude"
	"github.com/Ramanan-Bala/donegate/internal/gate"
	"github.com/spf13/cobra"
)

func newGateCmd() *cobra.Command {
	var repo string
	var format string

	cmd := &cobra.Command{
		Use:   "gate",
		Short: "Run the verification done gate for the active session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch format {
			case "text":
				out, err := gate.Run(gate.Options{Cwd: repo})
				if err != nil {
					return err
				}
				if out.Block {
					if out.Message != "" {
						fmt.Fprintln(cmd.OutOrStdout(), out.Message)
					}
					return errVerifyFailed
				}
				if out.UserMessage != "" {
					fmt.Fprintln(cmd.OutOrStdout(), out.UserMessage)
				}
				return nil

			case "claude":
				in, err := claude.ParseStop(cmd.InOrStdin())
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "donegate gate: %v\n", err)
					return nil
				}
				out, err := gate.Run(gate.Options{Cwd: in.Cwd})
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "donegate gate: %v\n", err)
					return nil
				}
				if err := claude.WriteStop(cmd.OutOrStdout(), out); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "donegate gate: %v\n", err)
					return nil
				}
				return nil

			default:
				return fmt.Errorf("unsupported gate format %q (supported: text, claude)", format)
			}
		},
	}

	cmd.Flags().StringVar(&repo, "repo", ".", "Repository directory")
	cmd.Flags().StringVar(&format, "format", "text", "Output format (text, claude)")

	return cmd
}
