package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/spf13/cobra"
)

var recoverIntegrationCmd = &cobra.Command{
	Use:   "recover-integration <task-id>",
	Short: "Retire a premature empty-cohort integration analysis with preserved evidence",
	Long: `Operator recovery for a paused run whose first global analysis was created
before upstream planning finished. Requires an unreviewed submitted report in a
clean worktree, with no contributor, coverage, verdict, closure or descendant.
Preserves the report under a durable Git ref, retires the task, and resets the
empty cohort atomically. Replace old state writers before using this recovery.
Use --dry-run to inspect eligibility without changing Git or state.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		defer func() {
			if isJSON(cmd) && retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
				_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
				retErr = jsonout.ErrAlreadyWritten
			}
		}()
		if brand.LookupEnv(os.Getenv, "AGENT_ID").Value != "" {
			return ops.WrapLifecycleError("recover-integration", nil, fmt.Errorf("recover-integration is an operator-only command; agent sessions cannot retire integration evidence"), models.LifecycleForbidden, "stop", "none")
		}
		root, err := requireProjectRoot()
		if err != nil {
			return err
		}
		reason, _ := cmd.Flags().GetString("reason")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		result, err := ops.RecoverIntegration(root, args[0], reason, dryRun)
		if isJSON(cmd) {
			var warnings []string
			if result != nil {
				warnings = result.Warnings
			}
			return jsonout.WriteResult(os.Stdout, result, warnings, err)
		}
		if err != nil {
			return err
		}
		verb := "Recovered"
		preservation := "preserved at"
		if result.DryRun {
			verb = "Eligible to recover"
			preservation = "would be preserved at"
		} else if result.Replayed {
			verb = "Already recovered"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s; report %s %s %s\n", verb, result.TaskID, result.Recovery.ReportCommit, preservation, result.Recovery.PreservationRef)
		for _, warning := range result.Warnings {
			fmt.Fprintln(cmd.ErrOrStderr(), warning)
		}
		return nil
	},
}

func init() {
	recoverIntegrationCmd.Flags().String("reason", "", "reason for retiring the premature analysis")
	recoverIntegrationCmd.Flags().Bool("dry-run", false, "check eligibility without changing state or Git")
	_ = recoverIntegrationCmd.MarkFlagRequired("reason")
	addJSONFlag(recoverIntegrationCmd)
	rootCmd.AddCommand(recoverIntegrationCmd)
}
