package commands

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// SprintCheckpointCommand creates a sprint checkpoint and prints the result to stdout.
// Delegates business logic to ops.SprintCheckpoint.
// CLI checkpoints pass an empty trigger; ops may auto-detect transition checkpoints.
func SprintCheckpointCommand(projectRoot string) error {
	result, err := ops.SprintCheckpoint(projectRoot, "")
	if err != nil {
		return fmt.Errorf("checkpoint: %w", err)
	}

	fmt.Println("Sprint checkpoint created")
	fmt.Printf("  Status: IN_PROGRESS → CHECKPOINT\n")
	fmt.Printf("  Checkpoint at: %s\n", result.CheckpointAt.Format(time.RFC3339))
	fmt.Println()
	fmt.Printf("Sprint summary written to: %s\n", result.ReportPath)
	fmt.Println()
	if models.IsTransitionCheckpointTrigger(result.Trigger) {
		fmt.Println("Transition gate is pending; doer/reviewer agents may continue existing work.")
		fmt.Printf("Orchestrator transition execution waits for '%s'.\n", brand.Command("resume"))
	} else {
		fmt.Println("Agents will pause at their next check.")
	}
	fmt.Printf("Review the sprint summary, then use '%s' to continue.\n", brand.Command("resume"))
	return nil
}
