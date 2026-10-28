package ops

import (
	"errors"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func setTaskOutputTestEntries() []models.OutputEntry {
	return []models.OutputEntry{{Desc: "d", DoneWhen: "dw", Scope: "s", SpecRef: "specs/feature.md"}}
}

// requireSetTaskOutputRefusal asserts the lifecycle result carried by a
// refused set-task-output call, with literal expectations independent of the
// typed constants the production code uses.
func requireSetTaskOutputRefusal(t *testing.T, err error, outcome, safeAction, effects string) {
	t.Helper()
	var lifecycleErr *LifecycleError
	if !errors.As(err, &lifecycleErr) {
		t.Fatalf("refusal carries no lifecycle result: %v", err)
	}
	got := lifecycleErr.Outcome
	if got.Operation != "set-task-output" || got.Outcome != outcome || string(got.SafeAction) != safeAction || string(got.Effects) != effects {
		t.Fatalf("lifecycle result = %s/%s/%s/%s, want set-task-output/%s/%s/%s",
			got.Operation, got.Outcome, got.SafeAction, got.Effects, outcome, safeAction, effects)
	}
}

func requireSetTaskOutputPhase(t *testing.T, err error, phase string) {
	t.Helper()
	var operational *OperationalError
	if !errors.As(err, &operational) {
		t.Fatalf("refusal carries no operational error: %v", err)
	}
	if operational.Phase != phase {
		t.Fatalf("operational phase = %q, want %q", operational.Phase, phase)
	}
}

func TestSetTaskOutputLifecycleResults(t *testing.T) {
	t.Parallel()

	t.Run("missing input requests corrected input", func(t *testing.T) {
		t.Parallel()
		_, err := SetTaskOutputWithOptions(t.TempDir(), nil)
		requireSetTaskOutputRefusal(t, err, models.LifecycleInvalidInput, "correct_input", "none")
	})

	t.Run("task outside an executing state is already transitioned", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC())}
		testhelpers.WriteInitialState(t, stateFile, state)

		_, err := SetTaskOutputWithOptions(tmpDir, &SetTaskOutputInput{TaskID: "task-1", AgentID: "coder-1", Output: setTaskOutputTestEntries()})
		testhelpers.RequireErrorContains(t, err, "task task-1 is not in an executing state (current status: "+string(state.Tasks[0].Status)+")")
		requireSetTaskOutputRefusal(t, err, models.LifecycleAlreadyTransitioned, "stop", "none")
		requireSetTaskOutputPhase(t, err, "persist-output")
	})

	t.Run("task assigned to another agent is a stale caller", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC())}
		testhelpers.WriteInitialState(t, stateFile, state)

		_, err := SetTaskOutputWithOptions(tmpDir, &SetTaskOutputInput{TaskID: "task-1", AgentID: "coder-99", Output: setTaskOutputTestEntries()})
		testhelpers.RequireErrorContains(t, err, "task task-1 is not assigned to agent coder-99 (currently assigned to: coder-1)")
		requireSetTaskOutputRefusal(t, err, models.LifecycleStaleCaller, "stop", "none")
		requireSetTaskOutputPhase(t, err, "persist-output")
	})
}

func TestSetTaskOutputPhaseLabels(t *testing.T) {
	t.Parallel()

	t.Run("stateless input refusal is validate-output", func(t *testing.T) {
		t.Parallel()
		_, err := SetTaskOutputWithOptions(t.TempDir(), &SetTaskOutputInput{AgentID: "coder-1", Output: setTaskOutputTestEntries()})
		testhelpers.RequireErrorContains(t, err, "task_id is required")
		requireSetTaskOutputPhase(t, err, "validate-output")
	})

	t.Run("unloadable pipeline is load-pipeline", func(t *testing.T) {
		t.Parallel()
		_, err := SetTaskOutputWithOptions(t.TempDir(), &SetTaskOutputInput{TaskID: "task-1", AgentID: "coder-1", Output: setTaskOutputTestEntries()})
		requireSetTaskOutputPhase(t, err, "load-pipeline")
	})
}
