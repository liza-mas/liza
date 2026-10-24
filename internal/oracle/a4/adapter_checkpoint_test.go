package a4

import (
	"errors"
	"fmt"

	"github.com/liza-mas/liza/internal/ops"
)

func init() {
	helperOps["passed-plan-checkpoint"] = func(root string) (string, error) {
		ids, err := ops.CheckpointPassedPlans(root)
		if errors.Is(err, ops.ErrNoPassedPlanCheckpoint) {
			return "no passed plan awaits a checkpoint", nil
		}
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("plans: %v", ids), nil
	}
}
