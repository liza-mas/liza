package commands

import (
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/payloadschema"
)

// MarkBlockedPayload builds the canonical object a caller preflights, from the
// same command arguments and options the blocking command receives. It
// delegates to the mutation boundary's own builder so the preflight and the
// mutation cannot describe the call differently.
func MarkBlockedPayload(taskID, reason string, questions []string, opts ops.MarkBlockedOptions) payloadschema.MarkBlockedPayload {
	return ops.MarkBlockedPayload(taskID, reason, questions, opts)
}

// MarkBlockedCommand marks a task as BLOCKED and prints the result to stdout.
// Delegates business logic to ops.MarkBlocked.
func MarkBlockedCommand(projectRoot, taskID, reason string, questions []string, agentID string) error {
	return MarkBlockedWithOptionsCommand(projectRoot, taskID, reason, questions, agentID, ops.MarkBlockedOptions{})
}

// MarkBlockedWithOptionsCommand marks a task as BLOCKED with optional structured
// metadata and prints the result to stdout.
func MarkBlockedWithOptionsCommand(projectRoot, taskID, reason string, questions []string, agentID string, opts ops.MarkBlockedOptions) error {
	result, err := ops.MarkBlockedWithOptions(projectRoot, taskID, reason, questions, agentID, opts)
	return printMarkBlockedResult(result, err)
}

// MarkBlockedWithAuthorityCommand marks a task BLOCKED using generation-fenced authority.
func MarkBlockedWithAuthorityCommand(projectRoot, taskID, reason string, questions []string, authority models.AgentAuthority, opts ops.MarkBlockedOptions) error {
	result, err := ops.MarkBlockedWithAuthority(projectRoot, taskID, reason, questions, authority, opts)
	return printMarkBlockedResult(result, err)
}

func printMarkBlockedResult(result *ops.MarkBlockedResult, err error) error {
	if err != nil {
		return fmt.Errorf("mark blocked: %w", err)
	}

	if printLifecycleResult(result.LifecycleOutcome) {
		return nil
	}

	fmt.Printf("Task %s marked as BLOCKED\nReason: %s\n", result.TaskID, result.Reason)
	if len(result.DependsOn) > 0 {
		fmt.Printf("Depends on: %v\n", result.DependsOn)
	}
	if result.RepairRequest != nil {
		fmt.Printf("Repair request: %s\n", result.RepairRequest.Operation)
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
	return nil
}
