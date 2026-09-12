package commands

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// UnblockTaskCommand restores a repaired BLOCKED task to its initial or executing status.
func UnblockTaskCommand(projectRoot, taskID, assignTo, reason, agentID string) error {
	return UnblockTaskWithOptionsCommand(projectRoot, taskID, reason, agentID, ops.UnblockTaskOptions{AssignTo: assignTo})
}

// UnblockTaskWithOptionsCommand restores a repaired BLOCKED task.
func UnblockTaskWithOptionsCommand(projectRoot, taskID, reason, agentID string, opts ops.UnblockTaskOptions) error {
	result, err := ops.UnblockTaskWithOptions(projectRoot, taskID, reason, agentID, opts)
	return printUnblockTaskResult(result, err)
}

// UnblockTaskWithAuthorityCommand unblocks a task using generation-fenced authority.
func UnblockTaskWithAuthorityCommand(projectRoot, taskID, reason string, authority models.AgentAuthority, opts ops.UnblockTaskOptions) error {
	result, err := ops.UnblockTaskWithAuthority(projectRoot, taskID, reason, authority, opts)
	return printUnblockTaskResult(result, err)
}

func printUnblockTaskResult(result *ops.UnblockTaskResult, err error) error {
	if err != nil {
		return fmt.Errorf("unblock task: %w", err)
	}

	if printLifecycleResult(result.LifecycleOutcome) {
		return nil
	}

	if result.AssignedTo != "" {
		fmt.Printf("Task %s unblocked: %s -> %s, assigned to %s\n",
			result.TaskID, result.FromStatus, result.ToStatus, result.AssignedTo)
	} else if result.Claimable {
		fmt.Printf("Task %s unblocked: %s -> %s, claimable\n",
			result.TaskID, result.FromStatus, result.ToStatus)
	} else {
		fmt.Printf("Task %s unblocked: %s -> %s, dependency-held\n",
			result.TaskID, result.FromStatus, result.ToStatus)
	}
	if result.Rebase != nil {
		fmt.Printf("Rebased %s: %s -> %s onto %s (%s)\n",
			result.TaskID,
			result.Rebase.OldHead,
			result.Rebase.NewHead,
			result.Rebase.TargetRef,
			result.Rebase.TargetSHA,
		)
	}
	return nil
}
