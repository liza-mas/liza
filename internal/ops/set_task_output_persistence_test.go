package ops

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestSetTaskOutputPersistence_ConcurrentUnrelatedWriters(t *testing.T) {
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC())}
	testhelpers.WriteInitialState(t, statePath, state)
	output := []models.OutputEntry{{Desc: "Implement storage", DoneWhen: "Storage tests pass", Scope: "storage", SpecRef: "specs/storage.md"}}

	// Hold an unrelated transaction after its read, then start the output writer
	// and independent blackboard writers before allowing that transaction to commit.
	locked := make(chan struct{})
	release := make(chan struct{})
	const writers = 8
	results := make(chan error, writers+2)
	go func() {
		results <- db.New(statePath).Modify(func(current *models.State) error {
			close(locked)
			<-release
			current.Sprint.Metrics.IterationsTotal++
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-results:
		t.Fatalf("initial transaction failed before barrier: %v", err)
	}
	started := make(chan struct{}, writers+1)
	go func() {
		started <- struct{}{}
		results <- SetTaskOutput(projectRoot, &SetTaskOutputInput{TaskID: "task-1", AgentID: "coder-1", Output: slices.Clone(output)})
	}()
	for range writers {
		go func() {
			started <- struct{}{}
			results <- db.New(statePath).Modify(func(current *models.State) error {
				current.Sprint.Metrics.IterationsTotal++
				return nil
			})
		}()
	}
	for range writers + 1 {
		<-started
	}
	close(release)
	for range writers + 2 {
		if err := <-results; err != nil {
			t.Errorf("concurrent transaction failed: %v", err)
		}
	}
	persisted, err := db.New(statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	if got := persisted.FindTask("task-1").Output; !reflect.DeepEqual(got, output) {
		t.Errorf("persisted output = %#v, want %#v", got, output)
	}
	if got := persisted.Sprint.Metrics.IterationsTotal; got != writers+1 {
		t.Errorf("persisted unrelated updates = %d, want %d", got, writers+1)
	}
}

func TestSetTaskOutputPersistence_IntegrationRepairPreservesPlanningOutput(t *testing.T) {
	projectRoot, taskID, reviewCommit, _, bb := setupRebaseConflictScenario(t)
	agentID := "code-planner-1"
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		task.RolePair = "code-planning-pair"
		task.Type = models.TaskTypePlanning
		task.Status = models.TaskStatusCodePlanning
		task.AssignedTo = &agentID
		for i := range task.History {
			task.History[i].Agent = &agentID
		}
		agent := testhelpers.RegisteredTestAgent(models.RoleCodePlanner)
		agent.Status = models.AgentStatusWorking
		agent.CurrentTask = &taskID
		state.Agents = map[string]models.Agent{agentID: agent}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	output := []models.OutputEntry{{Desc: "Implement planned storage", DoneWhen: "Storage tests pass", Scope: "storage", SpecRef: "specs/storage.md"}}
	if err := SetTaskOutput(projectRoot, &SetTaskOutputInput{TaskID: taskID, AgentID: agentID, Output: slices.Clone(output)}); err != nil {
		t.Fatalf("SetTaskOutput: %v", err)
	}
	_, err := SubmitForReview(projectRoot, taskID, reviewCommit, agentID)
	var integrationErr *IntegrationFailedError
	if !errors.As(err, &integrationErr) || integrationErr.Reason != IntegrationReasonMergeConflict {
		t.Fatalf("SubmitForReview error = %v, want integration merge conflict", err)
	}
	persisted, err := db.New(bb.GetStatePath()).Read()
	if err != nil {
		t.Fatal(err)
	}
	if got := persisted.FindTask(taskID).Output; !reflect.DeepEqual(got, output) {
		t.Fatalf("output changed during failed submission: got %#v, want %#v", got, output)
	}
	claim, err := ClaimTask(projectRoot, taskID, agentID)
	if err != nil {
		t.Fatalf("ClaimTask integration repair: %v", err)
	}
	if !claim.IntegrationFix {
		t.Fatal("repair claim did not select integration repair")
	}
	persisted, err = db.New(bb.GetStatePath()).Read()
	if err != nil {
		t.Fatal(err)
	}
	if got := persisted.FindTask(taskID).Output; !reflect.DeepEqual(got, output) {
		t.Errorf("integration repair discarded downstream planning output: got %#v, want %#v", got, output)
	}
}
