package main

import (
	"fmt"

	"github.com/liza-mas/liza/internal/embedded"
	"github.com/spf13/cobra"
)

var systemPromptStaleProjectRoot string

// systemPromptStaleCmd tells session-context.sh which pairing.md sources
// changed or were removed since the file was built, so an injected pairing
// session reads the changed ones instead of trusting the stale copy and
// disregards the removed ones (DEV-951). It prints one "changed <label>" or
// "removed <label>" line per source, nothing when the file is current or the
// project has no marker, and exits 0; only a run that cannot check exits
// non-zero, which the hook treats as an unconfirmed bundle. It never writes.
var systemPromptStaleCmd = &cobra.Command{
	Use:    "system-prompt-stale",
	Short:  "List the pairing.md sources changed or removed since it was built (session hook backend)",
	Hidden: true,
	Args:   cobra.NoArgs,
	// A command's own hook replaces the root's, which can write; this one must not.
	PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
	RunE: func(cmd *cobra.Command, _ []string) error {
		changed, removed, err := embedded.StalePairingSources(systemPromptStaleProjectRoot)
		if err != nil {
			return err
		}
		for _, label := range changed {
			fmt.Fprintln(cmd.OutOrStdout(), "changed "+label)
		}
		for _, label := range removed {
			fmt.Fprintln(cmd.OutOrStdout(), "removed "+label)
		}
		return nil
	},
}

func init() {
	systemPromptStaleCmd.Flags().StringVar(&systemPromptStaleProjectRoot, "project-root", "", "repository whose pairing.md to check")
	_ = systemPromptStaleCmd.MarkFlagRequired("project-root")
	rootCmd.AddCommand(systemPromptStaleCmd)
}
