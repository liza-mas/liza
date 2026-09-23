package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/pairingindex"
	"github.com/spf13/cobra"
)

// indexRefreshCmd is the coordinator behind the index lifecycle hooks and the
// post-merge trigger. It is hidden: users run the index script directly for a
// manual refresh, and nothing else should call this.
var indexRefreshCmd = &cobra.Command{
	Use:    pairingindex.RefreshCommandName,
	Short:  "Refresh repo-root indexes through the serialized coordinator (index hook backend)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		trigger, err := cmd.Flags().GetString("trigger")
		if err != nil {
			return err
		}
		output, err := gitenv.Output(".", "rev-parse", "--show-toplevel")
		if err != nil {
			return fmt.Errorf("resolve repository root: %w", err)
		}
		return pairingindex.RunRefresh(pairingindex.RefreshOptions{
			RepoRoot: strings.TrimSpace(string(output)),
			Trigger:  trigger,
			Echo:     os.Stderr,
		})
	},
}

func init() {
	indexRefreshCmd.Flags().String("trigger", "manual", "What requested the refresh (a hook name or merge)")
	rootCmd.AddCommand(indexRefreshCmd)
}
