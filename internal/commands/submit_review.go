package commands

import (
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/ops"
)

// SubmitForReviewCommand submits a task for review and prints the result to stdout.
// Delegates business logic to ops.SubmitForReview.
func SubmitForReviewCommand(projectRoot, taskID, commitRef, agentID string) error {
	result, err := ops.SubmitForReview(projectRoot, taskID, commitRef, agentID)
	if err != nil {
		return fmt.Errorf("submit for review: %w", err)
	}

	fmt.Printf("SUBMITTED FOR REVIEW: %s\n", result.TaskID)
	fmt.Printf("  review_commit: %s\n", result.ReviewCommit)
	fmt.Printf("  submitted_by: %s\n", result.AgentID)
	for _, warning := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
	return nil
}
