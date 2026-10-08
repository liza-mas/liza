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

var deferProviderDependencyCmd = &cobra.Command{
	Use:   "defer-provider-dependency <task-id> --provider-task <id> --transition <name> --reason <reason>",
	Short: "Move an unstarted plan's writer-order wait to the outputs it generates",
	Long: `Move one task-level provider_dependencies declaration of a task nobody has
started into its descendant_dependencies, applied at the task's per-subtask
transition. Use it when a wait that orders writers (for example coding after
another plan's coding output) was placed on their planner, which then cannot
start until those writers merge. Once deferred, the planner is claimable and
every output it writes carries the wait.

The task must be in its role pair's initial status or BLOCKED, never claimed.
--provider-task and --transition must name one of its declarations, and the
provider children that declaration selects must have the role pair of the
tasks it will hold; a planning-level prerequisite cannot be deferred. The move
is in place: decomposition, arch_ref and the reviewed parent output are kept,
and the move is recorded as provider_dependency_deferred history and in the
activity log. It changes no status and wakes no agent.
Identified agent sessions cannot use this command.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		defer func() {
			if isJSON(cmd) && retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
				_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
				retErr = jsonout.ErrAlreadyWritten
			}
		}()
		if brand.LookupEnv(os.Getenv, "AGENT_ID").Value != "" {
			return cliValidationError("defer-provider-dependency is operator-only; agent sessions cannot move task declarations")
		}
		providerTask, _ := cmd.Flags().GetString("provider-task")
		transition, _ := cmd.Flags().GetString("transition")
		reason, _ := cmd.Flags().GetString("reason")
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		result, err := ops.DeferProviderDependency(projectRoot, args[0], providerTask, transition, reason)
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
		fmt.Fprintf(cmd.OutOrStdout(), "provider wait %s %s deferred for %s to its outputs at %s\n", result.ProviderTask, result.Transition, result.TaskID, result.AtTransition)
		for _, warning := range result.Warnings {
			fmt.Fprintln(cmd.ErrOrStderr(), warning)
		}
		return nil
	},
}

func init() {
	deferProviderDependencyCmd.Flags().String("provider-task", "", "Provider task the declaration names")
	deferProviderDependencyCmd.Flags().String("transition", "", "Transition the declaration names")
	deferProviderDependencyCmd.Flags().String("reason", "", "Why the wait moves to the generated outputs (recorded in history)")
	_ = deferProviderDependencyCmd.MarkFlagRequired("provider-task")
	_ = deferProviderDependencyCmd.MarkFlagRequired("transition")
	_ = deferProviderDependencyCmd.MarkFlagRequired("reason")
	addJSONFlag(deferProviderDependencyCmd)
	rootCmd.AddCommand(deferProviderDependencyCmd)
}
