package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "wecodex",
		Short:         "WeChat bridge for Codex runtime",
		SilenceErrors: true,
	}
	root.AddCommand(statusCmd)
	root.AddCommand(loginCmd)
	root.AddCommand(startCmd)
	return root
}

var rootCmd = newRootCmd()

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
