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
		Long:          "weCodex bridges WeChat to a local Codex runtime and lets you manage Codex CLI threads with /new, /list, and /use N.",
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
