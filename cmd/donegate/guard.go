package main

import (
	"fmt"

	"github.com/Ramanan-Bala/donegate/internal/adapters/claude"
	"github.com/Ramanan-Bala/donegate/internal/guard"
	"github.com/spf13/cobra"
)

func newGuardCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "guard",
		Short: "Evaluate a tool call against the edit guard",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "claude" {
				return fmt.Errorf("unsupported guard format %q (supported: claude)", format)
			}

			call, err := claude.ParsePreToolUse(cmd.InOrStdin())
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "donegate guard: %v\n", err)
				return nil
			}

			ctx, err := guard.LoadContext(call.Cwd)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "donegate guard: %v\n", err)
				return nil
			}

			dec := guard.Decide(call, ctx)
			if err := claude.WritePreToolUse(cmd.OutOrStdout(), dec); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "donegate guard: %v\n", err)
				return nil
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "claude", "Hook JSON format (claude)")
	return cmd
}
