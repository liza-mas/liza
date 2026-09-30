package ops

import (
	"fmt"
	"slices"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/runtimeinputs"
)

// RecordRuntimeInputInput is an operator registration of one hand-produced
// materialization (ADR-0169).
type RecordRuntimeInputInput struct {
	Tasks    []string
	InputID  string
	Envelope string
}

// RecordRuntimeInputResult reports the instance a registration resolved to.
// Outcome is "recorded", "bound" (a reusable instance gained tasks) or
// "unchanged" (an existing instance, returned with its state and history).
type RecordRuntimeInputResult struct {
	InstanceID string   `json:"instance_id"`
	InputID    string   `json:"input_id"`
	Recipe     string   `json:"recipe"`
	State      string   `json:"state"`
	Outcome    string   `json:"outcome"`
	Tasks      []string `json:"tasks"`
}

// RecordRuntimeInput registers the envelope's materialization for the given
// tasks' input. The materialization's keyed identity is the instance: an
// identity already in the ledger is returned unchanged, so a consumed or
// invalidated materialization never re-enters as available, however its
// envelope is formatted or wherever its file artifacts moved. The only change
// to an existing instance is binding more tasks to a reusable, available one.
func RecordRuntimeInput(projectRoot string, input RecordRuntimeInputInput) (*RecordRuntimeInputResult, error) {
	tasks := slices.Clone(input.Tasks)
	slices.Sort(tasks)
	tasks = slices.Compact(tasks)
	if len(tasks) == 0 || input.InputID == "" || input.Envelope == "" {
		return nil, &PreconditionError{Reason: "provision --record requires --task, --input and --file"}
	}
	bb := db.For(paths.New(projectRoot).StatePath())
	snapshot, err := bb.Read()
	if err != nil {
		return nil, err
	}
	declaration, err := recordedRuntimeInputDeclaration(snapshot, tasks, input.InputID)
	if err != nil {
		return nil, err
	}
	key, err := loadRuntimeInputKey(snapshot, true)
	if err != nil {
		return nil, err
	}
	if err := runtimeinputs.CheckLocation(input.Envelope, runtimeInputForbiddenRoots(projectRoot)); err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("envelope %s: %v", input.Envelope, err)}
	}
	materialization, err := runtimeinputs.Materialize(key, declaration, input.Envelope, runtimeInputForbiddenRoots(projectRoot))
	if err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("envelope %s: %v", input.Envelope, err)}
	}

	result := &RecordRuntimeInputResult{InstanceID: materialization.Identity, InputID: input.InputID, Recipe: declaration.Recipe}
	err = bb.Modify(func(state *models.State) error {
		current, err := recordedRuntimeInputDeclaration(state, tasks, input.InputID)
		if err != nil {
			return err
		}
		if !models.RuntimeInputEqual(current, declaration) {
			return &PreconditionError{Reason: fmt.Sprintf("input %s changed during registration; retry", input.InputID)}
		}
		if state.RuntimeInputs == nil {
			state.RuntimeInputs = map[string]models.RuntimeInputInstance{}
		}
		instance, exists := state.RuntimeInputs[materialization.Identity]
		if !exists {
			state.RuntimeInputs[materialization.Identity] = models.RuntimeInputInstance{
				Recipe: declaration.Recipe, InputID: declaration.ID, Consumption: declaration.Consumption, Secret: declaration.Secret,
				KeyID: key.ID, Envelope: input.Envelope, Names: materialization.Names,
				Tasks: tasks, State: models.RuntimeInputAvailable, RegisteredAt: time.Now().UTC(),
			}
			result.Outcome, result.State, result.Tasks = "recorded", models.RuntimeInputAvailable, tasks
			return nil
		}
		if instance.Recipe != declaration.Recipe || instance.InputID != declaration.ID || instance.Consumption != declaration.Consumption || instance.Secret != declaration.Secret {
			return &PreconditionError{
				Reason:  fmt.Sprintf("this materialization is already recorded as input %s (recipe %s, %s) and cannot be recorded as a different input", instance.InputID, instance.Recipe, instance.Consumption),
				Details: map[string]any{"conflict": "instance_identity", "instance_id": materialization.Identity},
			}
		}
		result.State, result.Tasks, result.Outcome = instance.State, instance.Tasks, "unchanged"
		if instance.Terminal() {
			return nil // A spent or invalidated materialization stays so.
		}
		if instance.Envelope != input.Envelope {
			return &PreconditionError{
				Reason:  fmt.Sprintf("this materialization is already recorded, available, at %s; move the envelope back or record a different materialization", instance.Envelope),
				Details: map[string]any{"conflict": "instance_locator", "instance_id": materialization.Identity},
			}
		}
		added := slices.DeleteFunc(slices.Clone(tasks), instance.BoundTo)
		if len(added) == 0 {
			return nil
		}
		if instance.Consumption == models.RuntimeInputSingleUse {
			return &PreconditionError{
				Reason:  "a single_use instance serves exactly one task, and this one is already bound",
				Details: map[string]any{"conflict": "single_use_binding", "instance_id": materialization.Identity},
			}
		}
		instance.Tasks = append(slices.Clone(instance.Tasks), added...)
		slices.Sort(instance.Tasks)
		state.RuntimeInputs[materialization.Identity] = instance
		result.Tasks, result.Outcome = instance.Tasks, "bound"
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// recordedRuntimeInputDeclaration returns the one declaration the tasks share
// for inputID. Every task must be live and declare it identically, apart from
// the commands that use it; a single_use input serves exactly one task.
func recordedRuntimeInputDeclaration(state *models.State, tasks []string, inputID string) (models.RuntimeInput, error) {
	var shared *models.RuntimeInput
	for _, taskID := range tasks {
		task := state.FindTask(taskID)
		if task == nil {
			return models.RuntimeInput{}, &PreconditionError{Reason: fmt.Sprintf("task %s does not exist", taskID)}
		}
		if task.Status.IsTerminal() {
			return models.RuntimeInput{}, &PreconditionError{Reason: fmt.Sprintf("task %s is %s; runtime inputs are recorded for live tasks only", taskID, task.Status)}
		}
		declaration := models.FindRuntimeInput(task.RuntimeInputs, inputID)
		if declaration == nil {
			return models.RuntimeInput{}, &PreconditionError{Reason: fmt.Sprintf("task %s declares no runtime input %s", taskID, inputID)}
		}
		comparable := *declaration
		comparable.Commands, comparable.After = nil, nil
		if shared == nil {
			shared = &comparable
			continue
		}
		if !models.RuntimeInputEqual(*shared, comparable) {
			return models.RuntimeInput{}, &PreconditionError{Reason: fmt.Sprintf("tasks declare input %s differently; record one instance per declaration", inputID)}
		}
	}
	if shared.Consumption == models.RuntimeInputSingleUse && len(tasks) != 1 {
		return models.RuntimeInput{}, &PreconditionError{Reason: fmt.Sprintf("input %s is single_use: record it for exactly one task", inputID)}
	}
	return *shared, nil
}
