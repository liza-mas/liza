package statevalidate

import (
	"fmt"
	"io"
	"slices"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// ValidateAddedTask validates the task added by a repair-oriented task creation
// operation without requiring unrelated pre-existing records to be globally valid.
func ValidateAddedTask(state *models.State, projectRoot, taskID string, skipSpecFileCheck bool, warnWriter io.Writer) error {
	if warnWriter == nil {
		warnWriter = io.Discard
	}
	if state == nil {
		return fmt.Errorf("state is nil")
	}

	cfg, cfgErr := pipeline.LoadFrozen(projectRoot)
	if cfgErr != nil {
		return cfgErr
	}
	var resolver *pipeline.Resolver
	if cfg != nil {
		resolver = pipeline.NewResolver(cfg)
	}

	taskIndex := -1
	for i := range state.Tasks {
		if state.Tasks[i].ID == taskID {
			if taskIndex >= 0 {
				return fmt.Errorf("duplicate task ID %q at tasks[%d] and tasks[%d]", taskID, taskIndex, i)
			}
			taskIndex = i
		}
	}
	if taskIndex < 0 {
		return fmt.Errorf("task %q not found", taskID)
	}
	if !slices.Contains(state.Sprint.Scope.Planned, taskID) {
		return fmt.Errorf("sprint.scope.planned missing added task %q", taskID)
	}

	task := state.Tasks[taskIndex]
	// The scoped copy omits every other task, so parent existence is checked
	// against the full state instead of requiring the parents to be in scope.
	for _, parentID := range task.EffectiveParentTasks() {
		if state.FindTask(parentID) == nil {
			return fmt.Errorf("task %s has parent_task referencing non-existent task '%s'", task.ID, parentID)
		}
	}
	scoped := task
	scoped.ParentTask, scoped.ParentTasks = nil, nil
	taskState := scopedTaskState(state, scoped)
	if err := validateTaskStates(taskState, projectRoot, skipSpecFileCheck, resolver); err != nil {
		return err
	}
	if err := validateTaskInvariants(taskState, projectRoot, skipSpecFileCheck, resolver, cfg); err != nil {
		return err
	}
	if err := validateDependenciesForTask(state, projectRoot, skipSpecFileCheck, resolver, cfg, warnWriter, &task); err != nil {
		return err
	}
	if err := checkCircular(task.ID, task.ID, map[string]bool{}, state); err != nil {
		return err
	}
	return nil
}

func scopedTaskState(state *models.State, task models.Task) *models.State {
	return &models.State{
		Config: state.Config,
		Tasks:  []models.Task{task},
	}
}
