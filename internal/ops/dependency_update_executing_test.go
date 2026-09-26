package ops

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D13: a dependency update that would give an executing consumer an unmet
// dependency is refused before any mutation with a typed prerequisite, not
// discovered by full-state validation and reported as a retryable change.

func requireExecutingConsumerRefusal(t *testing.T, err error, consumerID string) {
	t.Helper()
	var lifecycle *LifecycleError
	if !errors.As(err, &lifecycle) {
		t.Fatalf("error = %v, want a lifecycle error", err)
	}
	if lifecycle.Outcome.Outcome != models.LifecycleAlreadyTransitioned || lifecycle.Outcome.SafeAction != "stop" {
		t.Fatalf("outcome = %s/%s, want %s/stop: %v", lifecycle.Outcome.Outcome, lifecycle.Outcome.SafeAction, models.LifecycleAlreadyTransitioned, err)
	}
	var precondition *PreconditionError
	if !errors.As(err, &precondition) {
		t.Fatalf("error = %v, want a precondition error", err)
	}
	if precondition.Details["prerequisite"] != "consumer_not_executing" || precondition.Details["task_id"] != consumerID {
		t.Fatalf("details = %v, want prerequisite consumer_not_executing for %s", precondition.Details, consumerID)
	}
}

func executingConsumerState(t *testing.T) (string, string, *models.State) {
	t.Helper()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	state.Agents["coder-1"] = workingCoder("consumer")
	consumer := testhelpers.BuildTaskByStatus("consumer", models.TaskStatusImplementing, now)
	consumer.DependsOn = []string{"done"}
	makeTaskWorktreeDir(t, root, "consumer")
	state.Tasks = []models.Task{
		consumer,
		testhelpers.BuildTaskByStatus("done", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("also-done", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("pending", models.TaskStatusReady, now),
	}
	return root, stateFile, state
}

// workingCoder is the registered coder an executing fixture task requires.
func workingCoder(taskID string) models.Agent {
	agent := testhelpers.RegisteredTestAgent(models.RoleCoder)
	agent.Status = models.AgentStatusWorking
	agent.CurrentTask = &taskID
	return agent
}

// makeTaskWorktreeDir satisfies state validation for an executing fixture task.
func makeTaskWorktreeDir(t *testing.T, root, taskID string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".worktrees", taskID), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readDependencyUpdateState(t *testing.T, stateFile string) *models.State {
	t.Helper()
	state, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func requireTasksUnchanged(t *testing.T, stateFile string, before []models.Task) {
	t.Helper()
	after, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after.Tasks) {
		t.Fatalf("refused update changed tasks\nbefore: %#v\nafter:  %#v", before, after.Tasks)
	}
}

func TestRetargetDependency_RefusesUnmetDependencyOnExecutingConsumer(t *testing.T) {
	t.Parallel()
	root, stateFile, state := executingConsumerState(t)
	testhelpers.WriteInitialState(t, stateFile, state)
	before := readDependencyUpdateState(t, stateFile).Tasks

	_, err := RetargetDependency(root, "consumer", "done", []string{"pending"}, "insert corrective predecessor", "orchestrator-1")
	requireExecutingConsumerRefusal(t, err, "consumer")
	requireTasksUnchanged(t, stateFile, before)
}

// The satisfied-to-satisfied counterpart stays allowed on an executing task.
func TestRetargetDependency_AllowsSatisfiedEdgeOnExecutingConsumer(t *testing.T) {
	t.Parallel()
	root, stateFile, state := executingConsumerState(t)
	testhelpers.WriteInitialState(t, stateFile, state)

	if _, err := RetargetDependency(root, "consumer", "done", []string{"also-done"}, "equivalent provider", "orchestrator-1"); err != nil {
		t.Fatalf("satisfied retarget refused: %v", err)
	}
	if got := readDependencyUpdateState(t, stateFile).FindTask("consumer").DependsOn; !slices.Equal(got, []string{"also-done"}) {
		t.Fatalf("depends_on = %v, want [also-done]", got)
	}
}

func TestApplyDependencyRepair_RefusesUnmetDependencyOnExecutingConsumer(t *testing.T) {
	t.Parallel()
	root, stateFile, state := executingConsumerState(t)
	source := testhelpers.BuildTaskByStatus("repair-source", models.TaskStatusBlocked, time.Now().UTC())
	source.RepairRequest = dependencyRepairRequest([]models.DependencyUpdate{
		{TaskID: "consumer", ExpectedDependsOn: []string{"done"}, DesiredDependsOn: []string{"pending"}},
	})
	state.Tasks = append(state.Tasks, source)
	testhelpers.WriteInitialState(t, stateFile, state)
	before := readDependencyUpdateState(t, stateFile).Tasks

	_, err := ApplyDependencyRepair(root, "repair-source", "Apply stored graph repair", "orchestrator-1")
	requireExecutingConsumerRefusal(t, err, "consumer")
	requireTasksUnchanged(t, stateFile, before)
}

func TestReplaceTask_RefusesUnmetDependencyOnExecutingConsumer(t *testing.T) {
	t.Parallel()
	f := newReplacementFixture(t)
	makeTaskWorktreeDir(t, f.root, "consumer-a")
	if err := db.For(f.statePath).Modify(func(s *models.State) error {
		s.Agents["coder-1"] = workingCoder("consumer-a")
		now := time.Now().UTC()
		consumer := testhelpers.BuildTaskByStatus("consumer-a", models.TaskStatusImplementing, now)
		consumer.DependsOn = []string{"done"}
		*s.FindTask("consumer-a") = consumer
		s.Tasks = append(s.Tasks, testhelpers.BuildTaskByStatus("done", models.TaskStatusMerged, now))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.input.Consumers[0] = models.DependencyUpdate{TaskID: "consumer-a", ExpectedDependsOn: []string{"done"}, DesiredDependsOn: []string{"replacement"}}
	before := replacementState(t, f).Tasks

	_, err := f.run()
	requireExecutingConsumerRefusal(t, err, "consumer-a")
	requireTasksUnchanged(t, f.statePath, before)
}
