package ops

import (
	"context"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
)

// readTaskState reads state from the blackboard and finds the specified task.
// Returns an error if state cannot be read or the task doesn't exist.
func readTaskState(bb *db.Blackboard, taskID string) (*models.State, *models.Task, error) {
	state, err := bb.Read()
	return findReadTask(state, err, taskID)
}

// readTaskStatePatient is readTaskState through Blackboard.ReadContextPatient.
func readTaskStatePatient(ctx context.Context, bb *db.Blackboard, taskID string) (*models.State, *models.Task, error) {
	state, err := bb.ReadContextPatient(ctx)
	return findReadTask(state, err, taskID)
}

func findReadTask(state *models.State, readErr error, taskID string) (*models.State, *models.Task, error) {
	if readErr != nil {
		return nil, nil, readErr
	}
	task := state.FindTask(taskID)
	if task == nil {
		return nil, nil, &errors.NotFoundError{Entity: "task", ID: taskID}
	}
	return state, task, nil
}
