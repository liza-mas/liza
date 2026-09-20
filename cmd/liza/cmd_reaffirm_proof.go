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

var reaffirmProofCmd = &cobra.Command{
	Use:   "reaffirm-proof <task-id> <reference-id>",
	Short: "Record an authorized decision that an approved proof still holds against changed content",
	Long: `Authorize one approved proof against content that changed after its approval.

Requires a registered orchestrator with current ` + brand.EnvName("AGENT_GENERATION") + `
authority and the reaffirm-proof capability. Use inherited agent authority;
do not copy registration credentials into an operator shell.

Acceptance compares every reference an approved proof cites by what it resolves
to, and refuses when the content moved, because it cannot tell a legitimate
extension from a substitution. This records the decision that it was the former.

Both recorded identities are derived from the reviewed carrier and from
integration; --expected-section is a precondition, not an input to the record.
It names the identity you inspected, and the command refuses without recording
if integration has moved since. Run once without it to learn the current
identity, inspect that content, then authorize it.

The record authorizes exactly one transition, so a later change to the same
section refuses again. It grants no status change and no approval of the work.`,
	Args: cobra.ExactArgs(2),
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
		if err := validateAllowedOperation(resolver, authority.ID, "reaffirm-proof"); err != nil {
			return err
		}
		reason, _ := cmd.Flags().GetString("reason")
		expected, _ := cmd.Flags().GetString("expected-section")
		result, err := ops.ReaffirmProof(projectRoot, args[0], args[1], expected, reason, authority)
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		if err != nil {
			return fmt.Errorf("reaffirm proof: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(),
			"Re-affirmed proof %s for %s: %s#%s moved %s -> %s\n",
			result.ReferenceID, result.TaskID, result.CarrierPath, result.Heading,
			shortObjectID(result.ReviewedSection), shortObjectID(result.CurrentSection))
		return nil
	},
}

func shortObjectID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func init() {
	rootCmd.AddCommand(reaffirmProofCmd)
	addAgentIDFlag(reaffirmProofCmd)
	addJSONFlag(reaffirmProofCmd)
	// Validated inside RunE/ops so a missing reason keeps the JSON envelope,
	// matching reconcile-verdict; Cobra's required-flag check runs earlier.
	reaffirmProofCmd.Flags().String("reason", "", "required justification, at most 4096 bytes")
	reaffirmProofCmd.Flags().String("expected-section", "", "object id of the section content you inspected; refuses if integration has moved since")
}
