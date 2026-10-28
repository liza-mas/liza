package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/spf13/cobra"
)

var amendPlanCmd = &cobra.Command{
	Use:   "amend-plan <original-id> (--reason <text> | --apply <correction-id> | --replace-pending <correction-id> --reason <text> | --withdraw-pending <correction-id> --reason <text>)",
	Short: "Review a bounded correction without retiring a merged plan's provider identity",
	Args:  cobra.ExactArgs(1),
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
		root, err := requireProjectRoot()
		if err != nil {
			return err
		}
		reason, _ := cmd.Flags().GetString("reason")
		apply, _ := cmd.Flags().GetString("apply")
		replace, _ := cmd.Flags().GetString("replace-pending")
		withdraw, _ := cmd.Flags().GetString("withdraw-pending")
		actions := 0
		for _, name := range []string{"apply", "replace-pending", "withdraw-pending"} {
			if cmd.Flags().Changed(name) {
				actions++
			}
		}
		if actions > 1 || cmd.Flags().Changed("apply") && (apply == "" || cmd.Flags().Changed("reason")) || cmd.Flags().Changed("replace-pending") && replace == "" || cmd.Flags().Changed("withdraw-pending") && withdraw == "" {
			return cliValidationError("select --reason, --apply <correction>, --replace-pending <correction> --reason, or --withdraw-pending <correction> --reason")
		}
		input := ops.AmendPlanInput{TaskID: args[0], Reason: reason, Apply: apply, ReplacePending: replace, WithdrawPending: withdraw}
		contract, _ := cmd.Flags().GetBool("contract")
		if contract {
			input.Mode = models.PlanAmendmentContract
		}
		input.Trigger, _ = cmd.Flags().GetString("trigger")
		if brand.LookupEnv(os.Getenv, "AGENT_ID").Value != "" || cmd.Flags().Changed("agent-id") {
			authority, err := resolveOrchestratorAuthority(cmd)
			if err != nil {
				return err
			}
			resolver, err := loadResolverForRBAC(root)
			if err != nil {
				return err
			}
			if err := validateRoleType(resolver, authority.ID, roles.RoleTypeOrchestrator); err != nil {
				return err
			}
			input.Authority = &authority
		} else {
			input.ChangedBy = resolveChangedBy(cmd)
		}
		result, err := ops.AmendPlan(root, input)
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		if err != nil {
			return err
		}
		fmt.Printf("Plan %s: correction %s (changed: %t)\n", result.TaskID, result.CorrectionID, result.Changed)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(amendPlanCmd)
	amendPlanCmd.ValidArgsFunction = completeTaskIDArgs(1)
	amendPlanCmd.Flags().String("reason", "", "Reason for the bounded reviewed correction")
	amendPlanCmd.Flags().Bool("contract", false, "Review CONTRACT prose in referenced architecture Scopes with the complete output manifest unchanged, including expanded originals")
	amendPlanCmd.Flags().String("trigger", "", "Explicit correction trigger for daily planning metrics")
	amendPlanCmd.Flags().String("apply", "", "Adopt the exact independently reviewed MERGED pending correction")
	amendPlanCmd.Flags().String("replace-pending", "", "Replace an unapplied MERGED or canceled pending correction")
	amendPlanCmd.Flags().String("withdraw-pending", "", "Operator: release the fence of a canceled pending correction when nothing since the last applied correction reached integration")
	amendPlanCmd.Flags().String("agent-id", "", "Orchestrator identity")
	addChangedByFlag(amendPlanCmd)
	addJSONFlag(amendPlanCmd)
}
