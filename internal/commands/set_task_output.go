package commands

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/payloadschema"
)

// SetTaskOutputPayload returns the canonical object a caller preflights with
// ValidatePayload. It is the same object the mutation boundary validates, so a
// manifest that passes the preflight cannot be rejected there for its shape.
func SetTaskOutputPayload(output []models.OutputEntry) any {
	return payloadschema.SetTaskOutputPayload(output)
}

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
