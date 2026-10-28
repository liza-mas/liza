package ops

import (
	"fmt"
	"log"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/secretmask"
)

const AutoResumeConfigKey = "config.auto_resume"

// AutoResumeConfigResult describes the value committed by this operation.
type AutoResumeConfigResult struct {
	Key     string `json:"key"`
	Value   bool   `json:"value"`
	Outcome string `json:"outcome"`
}

// SetAutoResume sets the auto_resume config flag. Idempotent.
func SetAutoResume(projectRoot string, enabled bool) error {
	_, err := SetAutoResumeWithResult(projectRoot, enabled)
	return err
}

// SetAutoResumeWithResult compares and writes the policy under the native lock.
// It changes no execution mode, sprint status or task state.
func SetAutoResumeWithResult(projectRoot string, enabled bool) (*AutoResumeConfigResult, error) {
	bb := db.For(paths.New(projectRoot).StatePath())
	result := &AutoResumeConfigResult{Key: AutoResumeConfigKey, Value: enabled, Outcome: string(configSetUnchanged)}
	previous := false
	err := bb.Modify(func(state *models.State) error {
		previous = state.Config.AutoResume
		if previous != enabled {
			state.Config.AutoResume = enabled
			result.Outcome = string(configSetSet)
		}
		return nil
	})
	if err != nil {
		result.Outcome = string(configSetFailed)
	}
	// Same record shape as the other config_set writers; no reason, since this key takes none.
	log.Printf("config_set key=%s outcome=%s actor=%q project=%q previous=%t value=%t",
		AutoResumeConfigKey, result.Outcome, actorOperator, secretmask.New().MaskText(projectRoot), previous, enabled)
	if err != nil {
		return nil, fmt.Errorf("set %s: %w", AutoResumeConfigKey, err)
	}
	return result, nil
}
