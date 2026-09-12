package commands

import (
	"fmt"

	"github.com/liza-mas/liza/internal/ops"
)

// ReleaseClaimCommand releases claims on a task and prints the result to stdout.
// Delegates business logic to ops.ReleaseClaim.
func ReleaseClaimCommand(projectRoot, taskID, role string, force bool, reason, agentID string) error {
	return ReleaseClaimWithOptionsCommand(projectRoot, taskID, role, force, reason, agentID, ops.LifecycleRequestOptions{})
}

func ReleaseClaimWithOptionsCommand(projectRoot, taskID, role string, force bool, reason, agentID string, request ops.LifecycleRequestOptions) error {
	result, err := ops.ReleaseClaimWithRequest(projectRoot, taskID, role, force, reason, agentID, nil, request)
	if err != nil {
		return fmt.Errorf("release claim: %w", err)
	}

	if printLifecycleResult(result.LifecycleOutcome) {
		return nil
	}

	if result.ReleasedReviewer {
		fmt.Printf("Released review claim for %s\n", result.TaskID)
	}
	if result.ReleasedDoer {
		fmt.Printf("Released doer claim for %s\n", result.TaskID)
	}
	return nil
}
