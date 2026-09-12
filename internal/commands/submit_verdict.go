package commands

import (
	"fmt"

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
	result, err := ops.SubmitVerdictWithAuthority(projectRoot, taskID, verdict, reason, authority, impact, reviewCommit)
	if err != nil {
		return fmt.Errorf("submit verdict: %w", err)
	}

	printVerdictResult(result)
	return nil
}

func printVerdictResult(r *ops.VerdictResult) {
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
		}
	}
}
