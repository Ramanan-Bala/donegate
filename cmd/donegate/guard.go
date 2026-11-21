package main

import (
	"fmt"

	"github.com/Ramanan-Bala/donegate/internal/adapters/agy"
	"github.com/Ramanan-Bala/donegate/internal/adapters/claude"
	"github.com/Ramanan-Bala/donegate/internal/adapters/jsonhook"
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
			switch format {
			case "claude":
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

			case "json":
				call, err := jsonhook.ParseGuard(cmd.InOrStdin())
				if err != nil {
					if werr := jsonhook.WriteGuardError(cmd.OutOrStdout(), err); werr != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "donegate guard: %v\n", werr)
					}
					return nil
				}

				ctx, err := guard.LoadContext(call.Cwd)
				if err != nil {
					if werr := jsonhook.WriteGuardError(cmd.OutOrStdout(), err); werr != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "donegate guard: %v\n", werr)
					}
					return nil
				}

				dec := guard.Decide(call, ctx)
				if err := jsonhook.WriteGuard(cmd.OutOrStdout(), dec); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "donegate guard: %v\n", err)
					return nil
				}
				return nil

			case "agy":
				call, err := agy.ParsePreToolUse(cmd.InOrStdin())
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "donegate guard: %v\n", err)
					_ = agy.WritePreToolUseAsk(cmd.OutOrStdout())
					return nil
				}

				ctx, err := guard.LoadContext(call.Cwd)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "donegate guard: %v\n", err)
					_ = agy.WritePreToolUseAsk(cmd.OutOrStdout())
					return nil
				}

				dec := guard.Decide(call, ctx)
				if err := agy.WritePreToolUse(cmd.OutOrStdout(), dec); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "donegate guard: %v\n", err)
					_ = agy.WritePreToolUseAsk(cmd.OutOrStdout())
					return nil
				}
				return nil

			default:
				return fmt.Errorf("unsupported guard format %q (supported: claude, json, agy)", format)
			}
		},
	}

	cmd.Flags().StringVar(&format, "format", "claude", "Hook JSON format (claude, json, agy)")
	return cmd
}
