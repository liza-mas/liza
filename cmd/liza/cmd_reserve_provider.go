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

var reserveProviderCmd = &cobra.Command{
	Use:   "reserve-provider <task-id> <provider-task-id> --transition <name> --reason <reason>",
	Short: "Hold an unstarted writer until every child a provider generates merges",
	Long: fmt.Sprintf(`Place an existing writer after a provider's generated work. The task gains a
provider_reservations entry and stays unclaimable until the provider is
MERGED with --transition executed and every child it generated there has
merged, however many outputs the provider authors. A provider that merges
with no output releases it.

The task must not have started: its role pair's initial or rejected status
or BLOCKED, with no assignee or lease. Replan and same-pair replace-task of
the provider carry the reservation to the successor; cancelling or
superseding the provider is refused while the reservation is unsatisfied.

When other writers already select the provider's outputs by index, the
provider needs max_outputs and those selections must cover every output:
set the cap in the same call with --provider-max-outputs (tighten-only).

--release withdraws the placement; it is a policy decision, never a step of
retiring the provider. A repeated call is a no-op.

Examples:
  %[1]s vb-cp-0 corr --transition architecture-to-code-plan --reason "Correction lands first"
  %[1]s vb-cp-0 corr --transition architecture-to-code-plan --reason "Placement withdrawn" --release`, brand.Command("reserve-provider")),
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		defer func() {
			if isJSON(cmd) && retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
				_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
				retErr = jsonout.ErrAlreadyWritten
			}
		}()
		input := ops.ReserveProviderInput{TaskID: args[0], ProviderTask: args[1]}
		input.Transition, _ = cmd.Flags().GetString("transition")
		input.Reason, _ = cmd.Flags().GetString("reason")
		input.Release, _ = cmd.Flags().GetBool("release")
		input.ProviderMaxOutputs, _ = cmd.Flags().GetInt("provider-max-outputs")

		authority, err := resolveOrchestratorAuthority(cmd)
		if err != nil {
			return err
		}
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		resolver, err := loadResolverForRBAC(projectRoot)
		if err != nil {
			return err
		}
		if err := validateAllowedOperation(resolver, authority.ID, "reserve-provider"); err != nil {
			return err
		}
		result, err := ops.ReserveProviderWithAuthority(projectRoot, input, authority)
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, result, resultWarnings(result), err)
		}
		if err != nil {
			return err
		}
		switch {
		case !result.Changed:
			fmt.Fprintf(cmd.OutOrStdout(), "%s reservation on %s at %s already in place; nothing changed\n", result.TaskID, result.ProviderTask, result.Transition)
		case result.Released:
			fmt.Fprintf(cmd.OutOrStdout(), "%s released its reservation on %s at %s\n", result.TaskID, result.ProviderTask, result.Transition)
		default:
			fmt.Fprintf(cmd.OutOrStdout(), "%s now waits for every child %s generates at %s\n", result.TaskID, result.ProviderTask, result.Transition)
		}
		for _, warning := range result.Warnings {
			fmt.Fprintln(cmd.ErrOrStderr(), warning)
		}
		return nil
	},
}

func init() {
	reserveProviderCmd.Flags().String("transition", "", "Per-subtask transition of the provider whose children the task waits for")
	reserveProviderCmd.Flags().String("reason", "", "Why the writer is placed (recorded in history)")
	reserveProviderCmd.Flags().Bool("release", false, "Withdraw the reservation instead of adding it")
	reserveProviderCmd.Flags().Int("provider-max-outputs", 0, "Set or tighten the provider's max_outputs in the same transaction")
	reserveProviderCmd.ValidArgsFunction = completeTaskIDArgs(2)
	addAgentIDFlag(reserveProviderCmd)
	addJSONFlag(reserveProviderCmd)
	rootCmd.AddCommand(reserveProviderCmd)
}
