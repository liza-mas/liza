package ops

import (
	stderrors "errors"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestAwaitVerdict_EmptyTaskID(t *testing.T) {
	_, err := AwaitVerdict("/nonexistent", "", "coder-1", 30*time.Second)
	testhelpers.RequireErrorContains(t, err, "task ID is required")

	var pe *PreconditionError
	if !stderrors.As(err, &pe) {
		t.Fatalf("expected PreconditionError, got %T: %v", err, err)
	}
}

func TestAwaitVerdict_EmptyAgentID(t *testing.T) {
	_, err := AwaitVerdict("/nonexistent", "task-1", "", 30*time.Second)
	testhelpers.RequireErrorContains(t, err, "agent ID is required")

	var pe *PreconditionError
	if !stderrors.As(err, &pe) {
		t.Fatalf("expected PreconditionError, got %T: %v", err, err)
	}
}

func TestAwaitVerdict_TaskNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AwaitVerdict(tmpDir, "nonexistent", "coder-1", 30*time.Second)
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestAwaitVerdict_WrongStatus(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	// IMPLEMENTING is not in the awaitable set (submitted/reviewing/partially-approved)
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AwaitVerdict(tmpDir, "task-1", "coder-1", 30*time.Second)
	if err == nil {
		t.Fatal("expected error for wrong status")
	}

	var pe *PreconditionError
	if !stderrors.As(err, &pe) {
		t.Fatalf("expected PreconditionError, got %T: %v", err, err)
	}
	testhelpers.RequireErrorContains(t, err, "not in an awaitable status")
}

func TestAwaitVerdict_WrongAgent(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
	// Add a submission history entry from coder-1
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventSubmittedForReview,
		Agent: strPtr("coder-1"),
	})
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	// coder-2 was NOT the last submitter
	_, err := AwaitVerdict(tmpDir, "task-1", "coder-2", 30*time.Second)
	if err == nil {
		t.Fatal("expected error for wrong agent")
	}

	var pe *PreconditionError
	if !stderrors.As(err, &pe) {
		t.Fatalf("expected PreconditionError, got %T: %v", err, err)
	}
	testhelpers.RequireErrorContains(t, err, "not the last submitter")
}

func TestAwaitVerdict_OwnershipAcquired(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventSubmittedForReview,
		Agent: strPtr("coder-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusWaiting,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// AwaitVerdict should succeed through preconditions, acquire ownership,
	// then return a placeholder error (event loop not yet implemented).
	result, err := AwaitVerdict(tmpDir, "task-1", "coder-1", 30*time.Second)

	// After ownership acquisition, the function returns a placeholder.
	// We accept either a non-nil result or a non-nil error — the key check
	// is that ownership was acquired before returning.
	_ = result
	_ = err

	// Verify ownership: agent should have CurrentTask set and status WAITING
	bb := db.For(stateFile)
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}

	agent, ok := s.Agents["coder-1"]
	if !ok {
		t.Fatal("agent coder-1 not found in state")
	}
	if agent.Status != models.AgentStatusWaiting {
		t.Errorf("agent status = %q, want %q", agent.Status, models.AgentStatusWaiting)
	}
	if agent.CurrentTask == nil || *agent.CurrentTask != "task-1" {
		ct := "<nil>"
		if agent.CurrentTask != nil {
			ct = *agent.CurrentTask
		}
		t.Errorf("agent CurrentTask = %s, want task-1", ct)
	}
}

func TestAwaitVerdict_ReviewingStatus(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now.Add(-time.Minute),
		Event: models.TaskEventSubmittedForReview,
		Agent: strPtr("coder-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusWaiting,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// REVIEWING is in the awaitable set — should pass preconditions
	result, err := AwaitVerdict(tmpDir, "task-1", "coder-1", 30*time.Second)
	_ = result
	_ = err

	// Verify ownership acquired
	bb := db.For(stateFile)
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	agent := s.Agents["coder-1"]
	if agent.CurrentTask == nil || *agent.CurrentTask != "task-1" {
		t.Error("ownership not acquired for REVIEWING status")
	}
}
