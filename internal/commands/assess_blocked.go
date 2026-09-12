package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// AssessBlockedCommand records an orchestrator assessment of a BLOCKED task.
// Delegates business logic to ops.AssessBlocked.
func AssessBlockedCommand(projectRoot, taskID, note, agentID string) error {
	return AssessBlockedWithOptionsCommand(projectRoot, taskID, note, agentID, ops.AssessBlockedOptions{})
}

// AssessBlockedWithOptionsCommand records an orchestrator assessment, optionally
// reconciles canonical blocker metadata, and prints the resulting state.
func AssessBlockedWithOptionsCommand(projectRoot, taskID, note, agentID string, opts ops.AssessBlockedOptions) error {
	result, err := ops.AssessBlockedWithOptions(projectRoot, taskID, note, agentID, opts)
	return printAssessBlockedResult(result, err)
}

// AssessBlockedWithAuthorityCommand records an assessment using generation-fenced authority.
func AssessBlockedWithAuthorityCommand(projectRoot, taskID, note string, authority models.AgentAuthority, opts ops.AssessBlockedOptions) error {
	result, err := ops.AssessBlockedWithAuthority(projectRoot, taskID, note, authority, opts)
	return printAssessBlockedResult(result, err)
}

func printAssessBlockedResult(result *ops.AssessBlockedResult, err error) error {
	if err != nil {
		return fmt.Errorf("assess blocked: %w", err)
	}

	if printLifecycleResult(result.LifecycleOutcome) {
		return nil
	}

	fmt.Printf("Task %s assessed by orchestrator\n", result.TaskID)
	if result.Reason != "" {
		questions, err := json.Marshal(result.Questions)
		if err != nil {
			return fmt.Errorf("encode resulting blocked questions: %w", err)
		}
		fmt.Printf("Reason: %s\nQuestions: %s\n", result.Reason, questions)
		if result.RepairRequest == nil {
			fmt.Println("Repair request: none")
		} else {
			repairRequest, err := json.Marshal(result.RepairRequest)
			if err != nil {
				return fmt.Errorf("encode resulting repair request: %w", err)
			}
			fmt.Printf("Repair request: %s\n", repairRequest)
		}
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
	return nil
}
