package commands

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// SetTaskOutputCommand sets output entries on a task.
// Delegates business logic to ops.SetTaskOutput.
func SetTaskOutputCommand(projectRoot string, input *ops.SetTaskOutputInput) error {
	result, err := ops.SetTaskOutputWithOptions(projectRoot, input)
	if err != nil {
		return err
	}

	if printLifecycleResult(result.LifecycleOutcome) {
		return nil
	}

	fmt.Printf("Output set on task %s (%d entries)\n", input.TaskID, len(input.Output))
	return nil
}

// SetTaskOutputWithAuthorityCommand sets output using generation-fenced authority.
func SetTaskOutputWithAuthorityCommand(projectRoot string, input *ops.SetTaskOutputInput, authority models.AgentAuthority) error {
	result, err := ops.SetTaskOutputWithAuthorityAndOptions(projectRoot, input, authority)
	if err != nil {
		return err
	}

	if printLifecycleResult(result.LifecycleOutcome) {
		return nil
	}

	fmt.Printf("Output set on task %s (%d entries)\n", input.TaskID, len(input.Output))
	return nil
}
