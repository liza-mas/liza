package main

import (
	"fmt"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/spf13/cobra"
)

var (
	contractActiveProvider    string
	contractActiveProjectRoot string
)

// contractActiveCmd tells the session hooks whether the running CLI loads
// CORE.md through its contract link, so they ask for the read only when it
// does not (DEV-954). It prints "active <path>" or "inactive" and exits 0;
// only a run that cannot check exits non-zero, which the hooks treat as
// inactive. It never writes or fetches.
var contractActiveCmd = &cobra.Command{
	Use:    "contract-active",
	Short:  "Report whether a provider loads the contract (session hook backend)",
	Hidden: true,
	Args:   cobra.NoArgs,
	// A command's own hook replaces the root's, which can write; this one must not.
	PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
	RunE: func(cmd *cobra.Command, _ []string) error {
		homeDir, err := paths.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		active, err := commands.LoadedContract(homeDir, contractActiveProjectRoot, contractActiveProvider)
		if err != nil {
			return err
		}
		if active == "" {
			fmt.Fprintln(cmd.OutOrStdout(), "inactive")
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), "active "+active)
		return nil
	},
}

func init() {
	contractActiveCmd.Flags().StringVar(&contractActiveProvider, "provider", "", "provider id whose contract link to check")
	contractActiveCmd.Flags().StringVar(&contractActiveProjectRoot, "project-root", "", "repository whose repo or local contract link also counts")
	_ = contractActiveCmd.MarkFlagRequired("provider")
	rootCmd.AddCommand(contractActiveCmd)
}
