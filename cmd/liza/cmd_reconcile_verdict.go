package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/spf13/cobra"
)

var reconcileVerdictCmd = &cobra.Command{
	Use:   "reconcile-verdict <task-id> <finding-id> <accepted|refuted|superseded|escalated>",
	Short: "Record an authorized orchestrator's decision on quarantined verdict evidence",
	Long: `Append an audited reconciliation decision without changing task status or review quorum.

Requires a registered orchestrator with current ` + brand.EnvName("AGENT_GENERATION") + `
authority and the reconcile-verdict capability. Use inherited agent authority;
do not copy registration credentials into an operator shell.

Refuted and superseded clear a conflicting hold with a reason. Accepted rejection
keeps the unchanged review boundary blocked. Escalated keeps the hold pending.
Unmatched evidence remains non-gating regardless of disposition.`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		if isJSON(cmd) {
			log.SetOutput(io.Discard)
			defer log.SetOutput(os.Stderr)
			defer func() {
				if retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
					_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
					retErr = jsonout.ErrAlreadyWritten
				}
			}()
		}
		authority, err := requireAgentAuthority(cmd)
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
		if err := validateRoleType(resolver, authority.ID, "orchestrator"); err != nil {
			return err
		}
		if err := validateAllowedOperation(resolver, authority.ID, "reconcile-verdict"); err != nil {
			return err
		}
		reason, _ := cmd.Flags().GetString("reason")
		err = ops.ReconcileVerdict(projectRoot, args[0], args[1], args[2], reason, authority)
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, map[string]string{
				"task_id": args[0], "finding_id": args[1], "disposition": args[2],
			}, nil, err)
		}
		if err != nil {
			return fmt.Errorf("reconcile verdict: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Reconciled finding %s on %s: %s\n", args[1], args[0], args[2])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(reconcileVerdictCmd)
	addAgentIDFlag(reconcileVerdictCmd)
	addJSONFlag(reconcileVerdictCmd)
	// Validate the required reason inside RunE/ops so missing input retains
	// the JSON error envelope. Cobra's required-flag check runs before RunE.
	reconcileVerdictCmd.Flags().String("reason", "", "required justification, at most 4096 bytes")
	reconcileVerdictCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return completeTaskIDs(cmd, args, toComplete)
		}
		if len(args) == 2 {
			return completeValues("accepted", "refuted", "superseded", "escalated")(cmd, args, toComplete)
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}
