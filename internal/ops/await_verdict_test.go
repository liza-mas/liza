package ops

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestAwaitVerdict_EmptyTaskID(t *testing.T) {
	_, err := AwaitVerdict(context.Background(), "/nonexistent", "", "coder-1", 30*time.Second)
	testhelpers.RequireErrorContains(t, err, "task ID is required")

	var pe *PreconditionError
	if !stderrors.As(err, &pe) {
		t.Fatalf("expected PreconditionError, got %T: %v", err, err)
	}
}

func TestAwaitVerdict_EmptyAgentID(t *testing.T) {
	_, err := AwaitVerdict(context.Background(), "/nonexistent", "task-1", "", 30*time.Second)
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

	_, err := AwaitVerdict(context.Background(), tmpDir, "nonexistent", "coder-1", 30*time.Second)
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

	_, err := AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 30*time.Second)
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
	_, err := AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-2", 30*time.Second)
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

	// Use a pre-cancelled context so the event loop exits immediately
	// after ownership acquisition. This proves preconditions passed and
	// ownership was acquired (context.Canceled != PreconditionError).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := AwaitVerdict(ctx, tmpDir, "task-1", "coder-1", 30*time.Second)
	if !stderrors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled (proving event loop reached), got %v", err)
	}

	// Ownership is released on context cancellation, so CurrentTask is nil.
	// Comprehensive ownership verification tests are in code-planning-3.
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

	// REVIEWING is in the awaitable set — should pass preconditions.
	// Use pre-cancelled context so event loop exits immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := AwaitVerdict(ctx, tmpDir, "task-1", "coder-1", 30*time.Second)
	if !stderrors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled (proving REVIEWING passed preconditions), got %v", err)
	}
}

func TestAwaitVerdict_BudgetExhausted_IterationLimit(t *testing.T) {
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
	// Set iteration at the limit so classifyLimitEscalation returns shouldEscalate=true.
	task.Iteration = 4
	state.Config.MaxCoderIterations = 4
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusWaiting,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 30*time.Second)
	if !stderrors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("expected ErrBudgetExhausted, got %v", err)
	}

	// Verify ownership was released: agent.CurrentTask should be nil.
	bb := db.For(stateFile)
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	agent := s.Agents["coder-1"]
	if agent.CurrentTask != nil {
		t.Errorf("expected CurrentTask=nil after budget exhaustion, got %q", *agent.CurrentTask)
	}
}

func TestAwaitVerdict_BudgetExhausted_ReviewCycleLimit(t *testing.T) {
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
	// Set review cycles at the limit.
	task.ReviewCyclesCurrent = 5
	state.Config.MaxReviewCycles = 5
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusWaiting,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 30*time.Second)
	if !stderrors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("expected ErrBudgetExhausted, got %v", err)
	}

	// Verify ownership was released.
	bb := db.For(stateFile)
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	agent := s.Agents["coder-1"]
	if agent.CurrentTask != nil {
		t.Errorf("expected CurrentTask=nil after budget exhaustion, got %q", *agent.CurrentTask)
	}
}

func TestAwaitVerdict_BudgetWithinLimits(t *testing.T) {
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
	// Well within limits — budget gate should NOT fire.
	task.Iteration = 1
	task.ReviewCyclesCurrent = 0
	state.Config.MaxCoderIterations = 10
	state.Config.MaxReviewCycles = 5
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusWaiting,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Use pre-cancelled context so event loop exits immediately after budget gate passes.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := AwaitVerdict(ctx, tmpDir, "task-1", "coder-1", 30*time.Second)
	// Should NOT be ErrBudgetExhausted — budget gate should pass.
	if stderrors.Is(err, ErrBudgetExhausted) {
		t.Fatal("expected budget gate to pass (within limits), but got ErrBudgetExhausted")
	}
	// With cancelled context, we expect context.Canceled (not a budget error).
	if !stderrors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled after budget gate passed, got %v", err)
	}
}
