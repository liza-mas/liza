package commands

import (
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// SupersedeTaskCommand marks a task as SUPERSEDED and prints the result to stdout.
// Delegates business logic to ops.SupersedeTask.
func SupersedeTaskCommand(projectRoot, taskID string, replacementIDs []string, reason, agentID, recoverabilityCommand string) error {
	result, err := ops.SupersedeTaskWithOptions(projectRoot, taskID, replacementIDs, reason, agentID, ops.SupersedeTaskOptions{
		RecoverabilityCommand: recoverabilityCommand,
	})
	return printSupersedeTaskResult(result, err)
}

// SupersedeTaskWithAuthorityCommand supersedes a task using generation-fenced authority.
func SupersedeTaskWithAuthorityCommand(projectRoot, taskID string, replacementIDs []string, reason, recoverabilityCommand string, authority models.AgentAuthority) error {
	return SupersedeTaskWithAuthorityAndOptionsCommand(projectRoot, taskID, replacementIDs, reason, recoverabilityCommand, authority, ops.LifecycleRequestOptions{})
}

func SupersedeTaskWithAuthorityAndOptionsCommand(projectRoot, taskID string, replacementIDs []string, reason, recoverabilityCommand string, authority models.AgentAuthority, request ops.LifecycleRequestOptions) error {
	result, err := ops.SupersedeTaskWithAuthority(projectRoot, taskID, replacementIDs, reason, authority, ops.SupersedeTaskOptions{
		Request:               request,
		RecoverabilityCommand: recoverabilityCommand,
	})
	return printSupersedeTaskResult(result, err)
}

func printSupersedeTaskResult(result *ops.SupersedeResult, err error) error {
	if err != nil {
		return fmt.Errorf("supersede task: %w", err)
	}

	if printLifecycleResult(result.LifecycleOutcome) {
		return nil
	}

	if len(result.ReplacementIDs) > 0 {
		fmt.Printf("Superseded task %s (was %s) with replacements: %s\n",
			result.TaskID, result.OriginalStatus, strings.Join(result.ReplacementIDs, ", "))
	} else {
		fmt.Printf("Superseded task %s (was %s) without replacements\n",
			result.TaskID, result.OriginalStatus)
	}
	return nil
}
