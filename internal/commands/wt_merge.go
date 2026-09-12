package commands

import (
	"errors"
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// WtMergeCommand merges an approved task into the integration branch and prints status.
// Delegates business logic to ops.MergeWorktree.
// Returns *ops.IntegrationFailedError if merge conflicts or integration tests fail.
func WtMergeCommand(projectRoot, taskID, agentID string) error {
	result, err := ops.MergeWorktree(projectRoot, taskID, agentID)
	if err != nil {
		// Print context for integration failures
		var intErr *ops.IntegrationFailedError
		if errors.As(err, &intErr) {
			printIntegrationFailure(taskID, intErr)
		}
		return err
	}

	printMergeResult(result)
	return nil
}

// WtMergeCommandWithAuthority is the authenticated command adapter.
func WtMergeCommandWithAuthority(projectRoot, taskID string, authority models.AgentAuthority) error {
	return WtMergeCommandWithAuthorityAndOptions(projectRoot, taskID, authority, ops.LifecycleRequestOptions{})
}

func WtMergeCommandWithAuthorityAndOptions(projectRoot, taskID string, authority models.AgentAuthority, request ops.LifecycleRequestOptions) error {
	result, err := ops.MergeWorktreeWithAuthorityAndOptions(projectRoot, taskID, authority, request)
	if err != nil {
		var intErr *ops.IntegrationFailedError
		if errors.As(err, &intErr) {
			printIntegrationFailure(taskID, intErr)
		}
		return err
	}

	printMergeResult(result)
	return nil
}

func printIntegrationFailure(taskID string, intErr *ops.IntegrationFailedError) {
	switch intErr.Reason {
	case ops.IntegrationReasonHEADMismatch:
		fmt.Fprintf(os.Stderr, "⚠️  Worktree HEAD does not match approved commit\n")
		fmt.Fprintf(os.Stderr, "Task %s marked as INTEGRATION_FAILED\n", taskID)
		fmt.Fprintf(os.Stderr, "Worktree preserved for investigation\n")
	case ops.IntegrationReasonMergeConflict:
		fmt.Fprintf(os.Stderr, "⚠️  Merge conflict detected\n")
		fmt.Fprintf(os.Stderr, "Task %s marked as INTEGRATION_FAILED\n", taskID)
		fmt.Fprintf(os.Stderr, "Worktree preserved for conflict resolution\n")
	case ops.IntegrationReasonTestsFailed:
		fmt.Fprintf(os.Stderr, "⚠️  Integration tests failed\n")
		fmt.Fprintf(os.Stderr, "Task %s marked as INTEGRATION_FAILED\n", taskID)
		if intErr.TestOutput != "" {
			fmt.Fprintf(os.Stderr, "Test output:\n%s", intErr.TestOutput)
		}
	case ops.IntegrationReasonStateInvalid:
		fmt.Fprintf(os.Stderr, "⚠️  Post-merge state validation failed\n")
		fmt.Fprintf(os.Stderr, "Task %s marked as INTEGRATION_FAILED\n", taskID)
	default:
		fmt.Fprintf(os.Stderr, "⚠️  Integration failed: %s\n", intErr.Reason)
		fmt.Fprintf(os.Stderr, "Task %s marked as INTEGRATION_FAILED\n", taskID)
	}
	if intErr.RollbackError != nil {
		fmt.Fprintf(os.Stderr, "⚠️  CRITICAL: Rollback also failed: %v\n", intErr.RollbackError)
		fmt.Fprintf(os.Stderr, "Integration branch may contain failing code!\n")
	}
}

func printMergeResult(r *ops.MergeResult) {
	if printLifecycleResult(r.LifecycleOutcome) {
		return
	}
	if r.FastForward {
		fmt.Printf("✓ Fast-forward merge successful\n")
	} else {
		fmt.Printf("✓ Merge commit created\n")
	}

	if r.TestsRan {
		fmt.Println("✓ Integration tests passed")
	}

	shortCommit := r.MergeCommit
	if len(shortCommit) > 7 {
		shortCommit = shortCommit[:7]
	}
	fmt.Printf("✓ Task %s merged successfully\n", r.TaskID)
	fmt.Printf("  Merge commit: %s\n", shortCommit)

	for _, w := range r.Warnings {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
	}
}
