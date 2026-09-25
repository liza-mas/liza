package main

import (
	"os"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/spf13/cobra"
)

// hookStopGuardCmd is the backend of the Claude Code Stop hook installed by
// init. It is hidden: only the hook should call it.
var hookStopGuardCmd = &cobra.Command{
	Use:    "hook-stop-guard",
	Short:  "Keep an agent turn open while its background jobs run (Claude Code Stop hook backend)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return commands.StopGuardCommand(os.Stdin, os.Stdout, os.Getenv)
	},
}

func init() {
	rootCmd.AddCommand(hookStopGuardCmd)
}
