package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "donegate",
		Short:         "Cross-harness verification gate for AI coding agents",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.AddCommand(newGateCmd())
	rootCmd.AddCommand(newGuardCmd())
	rootCmd.AddCommand(newSessionCmd())
	rootCmd.AddCommand(newInstallCmd())
	rootCmd.AddCommand(newUninstallCmd())
	rootCmd.AddCommand(newVerifyCmd())
	rootCmd.AddCommand(newVersionCmd())
	return rootCmd
}

func run(args []string) int {
	rootCmd := newRootCmd()
	rootCmd.SetArgs(args)
	if err := rootCmd.Execute(); err != nil {
		if errors.Is(err, errVerifyFailed) {
			return 1
		}
		fmt.Fprintf(os.Stderr, "donegate: %v\n", err)
		return 2
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:]))
}
