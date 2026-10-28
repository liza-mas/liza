package a4

import "github.com/liza-mas/liza/internal/ops"

func init() {
	// The refusal is the output: the state golden then shows it wrote nothing.
	helperOps["checkpoint-sprint-complete"] = func(root string) (string, error) {
		if _, err := ops.SprintCheckpoint(root, "SPRINT_COMPLETE"); err != nil {
			return "refused: " + err.Error(), nil
		}
		return "checkpointed", nil
	}
}
