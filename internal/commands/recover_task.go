package commands

import (
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/ops"
)

// RecoverTaskCommand recovers a task by releasing claims while preserving
// recoverable work by default. With fresh=true it explicitly resets the task
// branch/worktree from integration. Prints results to stdout.
func RecoverTaskCommand(projectRoot, taskID string, force bool, fresh bool, reason string) error {
	return RecoverTaskWithRequestCommand(projectRoot, taskID, force, fresh, reason, ops.LifecycleRequestOptions{})
}

func RecoverTaskWithRequestCommand(projectRoot, taskID string, force bool, fresh bool, reason string, request ops.LifecycleRequestOptions) error {
	result, err := ops.RecoverTaskWithOptions(projectRoot, taskID, reason, ops.RecoverTaskOptions{
		RequestOptions: request,
		Force:          force,
		Fresh:          fresh,
	})
	if err != nil {
		return fmt.Errorf("recover task: %w", err)
	}

	if printLifecycleResult(result.LifecycleOutcome) {
		return nil
	}

	// Print warnings first — they're relevant even when "nothing to recover"
	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "  Warning: %s\n", w)
	}

	if !result.InState && !result.WorktreeRemoved && !result.BranchRemoved {
		fmt.Printf("Task %s: nothing to recover (not in state, no git artifacts)\n", taskID)
		return nil
	}

	fmt.Printf("Recovered task %s\n", result.TaskID)
	if result.InState {
		fmt.Printf("  In state: yes\n")
	} else {
		fmt.Printf("  In state: no (git-only cleanup)\n")
	}
	if result.AgentID != "" {
		fmt.Printf("  Agent: %s (%s)\n", result.AgentID, result.AgentRole)
	}
	if result.ClaimReleased {
		fmt.Printf("  Claim released: yes\n")
	}
	if result.PreservedWorktree {
		fmt.Printf("  Worktree preserved: yes\n")
	}
	if result.WorktreeRecovered {
		fmt.Printf("  Worktree reattached: yes\n")
	}
	if result.FreshReset {
		fmt.Printf("  Fresh reset: yes\n")
	}
	if result.WorktreeCreated {
		fmt.Printf("  Worktree created: yes\n")
	}
	if result.WorktreeRemoved {
		fmt.Printf("  Worktree removed: yes\n")
	}
	if result.BranchRemoved {
		fmt.Printf("  Branch removed: yes\n")
	}
	if result.AgentRecovered {
		fmt.Printf("  Agent recovered: yes\n")
	}

	return nil
}
