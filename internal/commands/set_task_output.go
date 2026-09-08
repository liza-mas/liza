package commands

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// SetTaskOutputCommand sets output entries on a task.
// Delegates business logic to ops.SetTaskOutput.
func SetTaskOutputCommand(projectRoot string, input *ops.SetTaskOutputInput) error {
	if err := ops.SetTaskOutput(projectRoot, input); err != nil {
		return err
	}

	fmt.Printf("Output set on task %s (%d entries)\n", input.TaskID, len(input.Output))
	return nil
}

// SetTaskOutputWithAuthorityCommand sets output using generation-fenced authority.
func SetTaskOutputWithAuthorityCommand(projectRoot string, input *ops.SetTaskOutputInput, authority models.AgentAuthority) error {
	if err := ops.SetTaskOutputWithAuthority(projectRoot, input, authority); err != nil {
		return err
	}

	fmt.Printf("Output set on task %s (%d entries)\n", input.TaskID, len(input.Output))
	return nil
}
