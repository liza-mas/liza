package main

import (
	"errors"

	"github.com/liza-mas/liza/internal/semble"
	"github.com/spf13/cobra"
)

// The SessionStart backend uses the same bounded, coordinated corpus probe as
// task prompts. It emits no corpus content or executable diagnostics.
var sembleReadyCmd = &cobra.Command{
	Use:    "semble-ready <project-root>",
	Short:  "Check offline Semble corpus readiness (session hook backend)",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !semble.CheckRepositoryReadiness(semble.ValidationOptions{TargetRoot: args[0]}).Ready {
			return errors.New("semble repository unavailable")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(sembleReadyCmd)
}
