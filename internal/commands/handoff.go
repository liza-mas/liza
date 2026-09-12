package commands

import (
	"fmt"

	"github.com/liza-mas/liza/internal/ops"
)

// HandoffCommand initiates a context-exhaustion handoff and prints the result to stdout.
// Delegates business logic to ops.Handoff.
func HandoffCommand(projectRoot string, input *ops.HandoffInput) error {
	input.ProjectRoot = projectRoot
	result, err := ops.Handoff(input)
	if err != nil {
		return fmt.Errorf("handoff: %w", err)
	}

	if printLifecycleResult(result.LifecycleOutcome) {
		return nil
	}

	fmt.Printf("HANDOFF: %s\n", result.TaskID)
	fmt.Printf("  by: %s\n", result.AgentID)
	return nil
}
