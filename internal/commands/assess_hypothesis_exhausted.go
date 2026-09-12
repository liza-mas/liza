package commands

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// AssessHypothesisExhaustedCommand records an orchestrator assessment of a hypothesis-exhausted task.
// Delegates business logic to ops.AssessHypothesisExhausted.
func AssessHypothesisExhaustedCommand(projectRoot, taskID, note, agentID string) error {
	result, err := ops.AssessHypothesisExhausted(projectRoot, taskID, note, agentID)
	return printAssessHypothesisExhaustedResult(result, err)
}

// AssessHypothesisExhaustedWithAuthorityCommand records an assessment using generation-fenced authority.
func AssessHypothesisExhaustedWithAuthorityCommand(projectRoot, taskID, note string, authority models.AgentAuthority) error {
	return AssessHypothesisExhaustedWithAuthorityAndOptionsCommand(projectRoot, taskID, note, authority, ops.LifecycleRequestOptions{})
}

func AssessHypothesisExhaustedWithAuthorityAndOptionsCommand(projectRoot, taskID, note string, authority models.AgentAuthority, request ops.LifecycleRequestOptions) error {
	result, err := ops.AssessHypothesisExhaustedWithAuthorityAndOptions(projectRoot, taskID, note, authority, request)
	return printAssessHypothesisExhaustedResult(result, err)
}

func printAssessHypothesisExhaustedResult(result *ops.AssessHypothesisExhaustedResult, err error) error {
	if err != nil {
		return fmt.Errorf("assess hypothesis-exhausted: %w", err)
	}

	if printLifecycleResult(result.LifecycleOutcome) {
		return nil
	}

	fmt.Printf("Task %s assessed by orchestrator (hypothesis-exhausted)\n", result.TaskID)
	return nil
}
