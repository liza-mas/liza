package commands

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// ReplaceTaskWithAuthorityAndOptionsCommand renders a generation-fenced replacement.
func ReplaceTaskWithAuthorityAndOptionsCommand(projectRoot string, input ops.ReplaceTaskInput, authority models.AgentAuthority, request ops.LifecycleRequestOptions) error {
	result, err := ops.ReplaceTaskWithAuthorityAndOptions(projectRoot, input, authority, request)
	return printReplaceTaskResult(result, err)
}

func printReplaceTaskResult(result *ops.ReplaceTaskResult, err error) error {
	if err != nil {
		return fmt.Errorf("replace task: %w", err)
	}
	if printLifecycleResult(result.LifecycleOutcome) {
		return nil
	}
	fmt.Printf("Replaced task %s with %s (was %s)\n", result.SourceTaskID, result.ReplacementTaskID, result.SourceOriginalStatus)
	for _, warning := range result.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
	return nil
}
