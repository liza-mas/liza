package a4

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
	"gopkg.in/yaml.v3"
)

func transitionID(data []byte, id string) (string, error) {
	var state models.State
	if err := yaml.Unmarshal(data, &state); err != nil {
		return "", err
	}
	task := state.FindTask(id)
	if task == nil {
		return "", fmt.Errorf("missing fixture task %s", id)
	}
	return models.TaskTransitionID(task), nil
}
