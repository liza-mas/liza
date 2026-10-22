package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/render"
	"github.com/spf13/cobra"
)

var deferredValidationCmd = &cobra.Command{
	Use:   "deferred-validation",
	Short: "Operate the deferred-validation batch and its operator rulings",
	Long: fmt.Sprintf(`Strict admission skips canonical commands an Acceptance Contract defers or an
operator ruling defers. The engine runs them as one batch at integration HEAD
every config.deferred_validation.batch_every_merges merges (default %d) and
before global integration, and bisects a failing command over the merges since
it last passed. A red batch records an immediate discovery naming the culprit
task, which wakes the orchestrator.

rule, unrule, retire, settle and abort are operator-only. run and status are open.`, models.DefaultDeferredValidationBatchEveryMerges),
}

var deferredValidationRuleCmd = &cobra.Command{
	Use:   "rule",
	Short: "Defer an exact canonical command for every strict task listing it",
	Long: `Record an operator ruling that defers an exact canonical command. Strict
admission then skips it and records it in the receipt with operator authority;
the deferred-validation batch satisfies it. A command a runtime input uses still
runs in the gate. --ruling must name the decision that authorizes the deferral.`,
	Args: cobra.NoArgs,
	RunE: deferredValidationOperatorRun(func(cmd *cobra.Command, root string) (any, error) {
		command, _ := cmd.Flags().GetString("command")
		ruling, _ := cmd.Flags().GetString("ruling")
		return ops.RuleDeferredValidation(root, command, ruling)
	}),
}

var deferredValidationUnruleCmd = &cobra.Command{
	Use:   "unrule",
	Short: "Withdraw an operator ruling",
	Long: `Withdraw the ruling for an exact command. Submitted receipts that deferred it
under the ruling no longer satisfy reviewer assignment and need fresh admission;
receipts that executed it are unaffected.`,
	Args: cobra.NoArgs,
	RunE: deferredValidationOperatorRun(func(cmd *cobra.Command, root string) (any, error) {
		command, _ := cmd.Flags().GetString("command")
		return ops.UnruleDeferredValidation(root, command)
	}),
}

var deferredValidationRetireCmd = &cobra.Command{
	Use:   "retire",
	Short: "Remove an obsolete command from the deferred-validation set",
	Long: `Retire an exact command (for example a renamed or removed test) from the
effective batch set for every deferral recorded so far. History is kept, and a
task merged later that defers the same command puts it back.`,
	Args: cobra.NoArgs,
	RunE: deferredValidationOperatorRun(func(cmd *cobra.Command, root string) (any, error) {
		command, _ := cmd.Flags().GetString("command")
		ruling, _ := cmd.Flags().GetString("ruling")
		return ops.RetireDeferredValidation(root, command, ruling)
	}),
}

var deferredValidationSettleCmd = &cobra.Command{
	Use:   "settle",
	Short: "Clear a predecessor runner that did not finish",
	Long: `A runner whose step left processes it could not stop keeps its record
unsettled, which the next run and the integration gate refuse. So does a runner
that died where its lock did not pass to its child processes. Confirm none of its
processes still runs, then settle it. Settling is refused while any process
still holds the runner lock.`,
	Args: cobra.NoArgs,
	RunE: deferredValidationOperatorRun(func(cmd *cobra.Command, root string) (any, error) {
		ruling, _ := cmd.Flags().GetString("ruling")
		return ops.SettleDeferredValidation(root, ruling)
	}),
}

var deferredValidationAbortCmd = &cobra.Command{
	Use:   "abort",
	Short: "End the live deferred-validation batch",
	Long: `Ask the live runner to end its batch. Within seconds it stops starting work,
kills the commands and bisect steps running then, records them not run, and
keeps every result already finished. The batch ends aborted
(aborted_by_operator) and is not retried at the same HEAD without run --force;
the integration gate reports deferred_validation_aborted until a merge or a
forced run. Refused when no runner holds the lock.`,
	Args: cobra.NoArgs,
	RunE: deferredValidationOperatorRun(func(cmd *cobra.Command, root string) (any, error) {
		ruling, _ := cmd.Flags().GetString("ruling")
		return ops.AbortDeferredValidation(root, ruling)
	}),
}

var deferredValidationRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the deferred-validation batch at integration HEAD",
	Long: `Run a batch of the effective deferred commands at integration HEAD unless a
passed batch there already covers them. It is a no-op while another runner holds
the lock. A failed or operator-aborted batch, or exhausted attempts, at an
unchanged HEAD is not retried without --force. Merges and integration
reconciliation start this command automatically.

A command whose newest execution passed at an ancestor of HEAD is carried
forward, not run, when nothing changed since in the paths
config.deferred_validation.scopes declares for it (the whole tree when none)
outside config.deferred_validation.inert_paths. A command whose newest execution
failed, attributed to a culprit whose discovery was converted to a fix task
still in flight, is recorded known_red and neither run nor bisected until that
fix merges or dies. --force carries nothing forward, skips nothing and
re-executes every command, also at a covered HEAD: use it after correcting a
scope or inert path that was too narrow.`,
	Args: cobra.NoArgs,
	RunE: deferredValidationOpenRun(func(cmd *cobra.Command, root string) (any, error) {
		force, _ := cmd.Flags().GetBool("force")
		return ops.RunDeferredValidation(cmd.Context(), root, ops.RunDeferredValidationOptions{Force: force})
	}),
}

var deferredValidationStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show deferred commands, rulings, the runner and recent batches",
	Args:  cobra.NoArgs,
	RunE: deferredValidationOpenRun(func(_ *cobra.Command, root string) (any, error) {
		return ops.ReadDeferredValidationStatus(root)
	}),
}

func deferredValidationOperatorRun(run func(*cobra.Command, string) (any, error)) func(*cobra.Command, []string) error {
	return deferredValidationOpenRun(func(cmd *cobra.Command, root string) (any, error) {
		if brand.LookupEnv(os.Getenv, "AGENT_ID").Value != "" {
			return nil, cliValidationError(fmt.Sprintf("%s is operator-only; agent sessions cannot change deferred-validation authority", cmd.CommandPath()))
		}
		return run(cmd, root)
	})
}

func deferredValidationOpenRun(run func(*cobra.Command, string) (any, error)) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) (retErr error) {
		defer func() {
			if isJSON(cmd) && retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
				_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
				retErr = jsonout.ErrAlreadyWritten
			}
		}()
		root, err := requireProjectRoot()
		if err != nil {
			return err
		}
		result, err := run(cmd, root)
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		if err != nil {
			return err
		}
		text, err := render.FormatYAML(result)
		if err != nil {
			return err
		}
		fmt.Fprint(cmd.OutOrStdout(), text)
		return nil
	}
}

func init() {
	for _, sub := range []*cobra.Command{deferredValidationRuleCmd, deferredValidationUnruleCmd, deferredValidationRetireCmd} {
		sub.Flags().String("command", "", "exact canonical command")
		_ = sub.MarkFlagRequired("command")
	}
	for _, sub := range []*cobra.Command{deferredValidationRuleCmd, deferredValidationRetireCmd, deferredValidationSettleCmd, deferredValidationAbortCmd} {
		sub.Flags().String("ruling", "", "decision that authorizes this change")
		_ = sub.MarkFlagRequired("ruling")
	}
	deferredValidationRunCmd.Flags().Bool("force", false, "re-execute every command, also after a failed or aborted batch, exhausted attempts or a covering batch at the same HEAD")
	for _, sub := range []*cobra.Command{deferredValidationRuleCmd, deferredValidationUnruleCmd, deferredValidationRetireCmd, deferredValidationSettleCmd, deferredValidationAbortCmd, deferredValidationRunCmd, deferredValidationStatusCmd} {
		addJSONFlag(sub)
		deferredValidationCmd.AddCommand(sub)
	}
	rootCmd.AddCommand(deferredValidationCmd)
}
