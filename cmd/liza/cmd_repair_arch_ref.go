package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/spf13/cobra"
)

var repairArchRefCmd = &cobra.Command{
	Use:   "repair-arch-ref <task-id> --arch-ref <path#exact Scope heading> --reason <reason>",
	Short: "Set the empty arch_ref of a task nobody has started",
	Long: `Set the task-level arch_ref of one task whose arch_ref is empty, typically a
replacement created before replacements inherited it. The task must be in its
role pair's initial status and never claimed; a non-empty arch_ref is refused.

--arch-ref must name an artifact on the integration branch and an exact heading
in it (path#Scope heading), checked the way an architecture output's arch_ref
is checked; a working-tree file does not count. The repair is recorded as an
arch_ref_repaired history entry and in the activity log. It changes no status
and wakes no agent: add an operator note to prompt a blocked consumer's
reassessment. Identified agent sessions cannot use this command.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		defer func() {
			if isJSON(cmd) && retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
				_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
				retErr = jsonout.ErrAlreadyWritten
			}
		}()
		if brand.LookupEnv(os.Getenv, "AGENT_ID").Value != "" {
			return cliValidationError("repair-arch-ref is operator-only; agent sessions cannot repair task metadata")
		}
		archRef, _ := cmd.Flags().GetString("arch-ref")
		reason, _ := cmd.Flags().GetString("reason")
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		result, err := ops.RepairArchRef(projectRoot, args[0], archRef, reason)
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
		fmt.Fprintf(cmd.OutOrStdout(), "arch_ref set for %s: %s\n", result.TaskID, result.ArchRef)
		for _, warning := range result.Warnings {
			fmt.Fprintln(cmd.ErrOrStderr(), warning)
		}
		return nil
	},
}

func init() {
	repairArchRefCmd.Flags().String("arch-ref", "", "Integration-branch artifact and exact Scope heading (path#heading)")
	repairArchRefCmd.Flags().String("reason", "", "Why the arch_ref is being restored (recorded in history)")
	_ = repairArchRefCmd.MarkFlagRequired("arch-ref")
	_ = repairArchRefCmd.MarkFlagRequired("reason")
	addJSONFlag(repairArchRefCmd)
	rootCmd.AddCommand(repairArchRefCmd)
}
