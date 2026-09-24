package commands

import (
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/ops"
)

// ReplanCommand re-invokes a planner after the human amends a plan file.
// Delegates business logic to ops.Replan.
func ReplanCommand(projectRoot, taskID, changedBy, reason string) error {
	result, err := ops.Replan(projectRoot, &ops.ReplanInput{
		TaskID:    taskID,
		ChangedBy: changedBy,
		Reason:    reason,
	})
	if err != nil {
		return err
	}

	fmt.Printf("Replanned: %s → %s\n", result.OriginalTaskID, result.NewTaskID)
	fmt.Printf("  Role pair: %s\n", result.RolePair)
	fmt.Printf("  Spec ref:  %s\n", result.SpecRef)
	fmt.Println()
	if result.Resumed {
		fmt.Println("Sprint resumed. Planner agents will pick up the new task.")
	} else {
		fmt.Println("Planner agents will pick up the new task.")
	}

	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}

	return nil
}
