package commands

import (
	"fmt"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// SubmitVerdictCommand submits a review verdict and prints the result to stdout.
// Delegates business logic to ops.SubmitVerdict.
func SubmitVerdictCommand(projectRoot, taskID, verdict, reason, agentID, impact string) error {
	result, err := ops.SubmitVerdict(projectRoot, taskID, verdict, reason, agentID, impact)
	if err != nil {
		return fmt.Errorf("submit verdict: %w", err)
	}

	printVerdictResult(result)
	return nil
}

// SubmitVerdictCommandWithAuthority is the authenticated command adapter.
func SubmitVerdictCommandWithAuthority(projectRoot, taskID, verdict, reason string, authority models.AgentAuthority, impact, reviewCommit string) error {
	return SubmitVerdictCommandWithAuthorityAndOptions(projectRoot, taskID, verdict, reason, authority, impact, reviewCommit, ops.LifecycleRequestOptions{})
}

func SubmitVerdictCommandWithAuthorityAndOptions(projectRoot, taskID, verdict, reason string, authority models.AgentAuthority, impact, reviewCommit string, request ops.LifecycleRequestOptions) error {
	result, err := ops.SubmitVerdictWithAuthorityAndOptions(projectRoot, taskID, verdict, reason, authority, impact, reviewCommit, request)
	if err != nil {
		return fmt.Errorf("submit verdict: %w", err)
	}

	printVerdictResult(result)
	return nil
}

func printVerdictResult(r *ops.VerdictResult) {
	if printLifecycleResult(r.LifecycleOutcome) {
		return
	}
	if r.Verdict == "APPROVED" {
		fmt.Printf("APPROVED: %s\n", r.TaskID)
		fmt.Printf("  approved_by: %s\n", r.AgentID)
	} else {
		fmt.Printf("REJECTED: %s\n", r.TaskID)
		fmt.Printf("  rejection_reason: %s\n", r.Reason)
		fmt.Printf("  reviewed_by: %s\n", r.AgentID)
		if r.EscalatedToBlocked {
			fmt.Println("  escalated_to: BLOCKED")
			if r.BlockedReason != "" {
				fmt.Printf("  blocked_reason: %s\n", r.BlockedReason)
			}
			if r.RejectionRCAGated {
				fmt.Println("  rejection_rca_gate: open")
				fmt.Printf("  clear_with: %s\n", brand.Command("record-rejection-rca", r.TaskID, "--rca-file", "<file>"))
				fmt.Printf("  then: %s\n", brand.Command("resume-rejection-rca", r.TaskID, "--disposition-file", "<file>"))
			}
		}
	}
}
