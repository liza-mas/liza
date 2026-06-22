package commands

import (
	"fmt"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/ops"
)

// ProceedCommand executes a manual inter-pair transition on a task.
// Delegates business logic to ops.Proceed.
func ProceedCommand(projectRoot, taskID, transitionName string) error {
	result, err := ops.Proceed(projectRoot, taskID, transitionName)
	if err != nil {
		return err
	}

	printProceedResult(result)
	return nil
}

func printProceedResult(r *ops.ProceedResult) {
	fmt.Printf("Transition %q executed on task %s\n", r.TransitionName, r.SourceTaskID)
	if len(r.ChildTaskIDs) == 0 {
		fmt.Println("  No new child tasks created (crash recovery — all already existed)")
	} else {
		fmt.Printf("  Created %d child task(s):\n", len(r.ChildTaskIDs))
		for _, id := range r.ChildTaskIDs {
			fmt.Printf("    - %s\n", id)
		}
	}
	fmt.Printf("\nRun '%s' to start a new sprint with the child tasks.\n", brand.Command("resume"))
}
