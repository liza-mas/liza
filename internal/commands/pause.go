package commands

import (
	"fmt"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/ops"
)

// PauseCommand pauses the Liza system and prints the result to stdout.
// Delegates business logic to ops.Pause.
func PauseCommand(projectRoot, reason, changedBy string) error {
	result, err := ops.Pause(projectRoot, reason, changedBy)
	if err != nil {
		return fmt.Errorf("pause: %w", err)
	}

	printModeChangeResult("System paused", result,
		"No new claims, provider turns or merges; running turns finish.",
		fmt.Sprintf("Use '%s' to continue.", brand.Command("resume")),
	)
	return nil
}
