package ops

import (
	"context"
	stderrors "errors"
	"strings"
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

func TestAwaitVerdict_Approved(t *testing.T) {
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
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	var result *AwaitVerdictResult
	var awaitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, awaitErr = AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 10*time.Second)
	}()

	testhelpers.WaitForAsyncSetup()
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusApproved
		reviewer := "code-reviewer-1"
		tk.ApprovedBy = &reviewer
		tk.History = append(tk.History, models.TaskHistoryEntry{
			Time:  time.Now().UTC(),
			Event: models.TaskEventReviewVerdictApproved,
			Agent: &reviewer,
		})
		return nil
	}); err != nil {
		t.Fatalf("Failed to modify state: %v", err)
	}

	<-done
	if awaitErr != nil {
		t.Fatalf("AwaitVerdict error: %v", awaitErr)
	}
	if result.Verdict != VerdictApproved {
		t.Errorf("Verdict = %q, want APPROVED", result.Verdict)
	}
	if result.ReviewerAgent != "code-reviewer-1" {
		t.Errorf("ReviewerAgent = %q, want code-reviewer-1", result.ReviewerAgent)
	}
	if result.TaskStatus != models.TaskStatusApproved {
		t.Errorf("TaskStatus = %q, want %s", result.TaskStatus, models.TaskStatusApproved)
	}

	// Verify ownership released.
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("Failed to read state: %v", readErr)
	}
	agent := s.Agents["coder-1"]
	if agent.CurrentTask != nil {
		t.Errorf("expected CurrentTask=nil after approval, got %q", *agent.CurrentTask)
	}
}

func TestAwaitVerdict_Rejected_SameAttempt(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.MaxCoderIterations = 10
	state.Config.MaxReviewCycles = 5

	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
	task.Iteration = 1 // well within budget
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
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	var result *AwaitVerdictResult
	var awaitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, awaitErr = AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 10*time.Second)
	}()

	testhelpers.WaitForAsyncSetup()
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusRejected
		reason := "Missing error handling"
		tk.RejectionReason = &reason
		reviewer := "code-reviewer-1"
		tk.History = append(tk.History, models.TaskHistoryEntry{
			Time:  time.Now().UTC(),
			Event: models.TaskEventReviewVerdictRejected,
			Agent: &reviewer,
		})
		return nil
	}); err != nil {
		t.Fatalf("Failed to modify state: %v", err)
	}

	<-done
	if awaitErr != nil {
		t.Fatalf("AwaitVerdict error: %v", awaitErr)
	}
	if result.Verdict != VerdictRejected {
		t.Errorf("Verdict = %q, want REJECTED", result.Verdict)
	}
	if result.Reason == "" {
		t.Error("expected non-empty Reason")
	}
	if result.Guidance == "" {
		t.Error("expected non-empty Guidance")
	}
	if result.ReviewerAgent != "code-reviewer-1" {
		t.Errorf("ReviewerAgent = %q, want code-reviewer-1", result.ReviewerAgent)
	}
	// ClaimTask increments iteration: 1 → 2.
	if result.Iteration != 2 {
		t.Errorf("Iteration = %d, want 2", result.Iteration)
	}

	// Verify task auto-reclaimed (assigned to coder-1, IMPLEMENTING).
	s, readErr := db.For(stateFile).Read()
	if readErr != nil {
		t.Fatalf("Failed to read state: %v", readErr)
	}
	reclaimedTask := s.FindTask("task-1")
	if reclaimedTask == nil {
		t.Fatal("Task not found after reclaim")
	}
	if reclaimedTask.Status != models.TaskStatusImplementing {
		t.Errorf("Task status = %v, want IMPLEMENTING_CODE", reclaimedTask.Status)
	}
	if reclaimedTask.AssignedTo == nil || *reclaimedTask.AssignedTo != "coder-1" {
		t.Error("Task should be assigned to coder-1 after auto-reclaim")
	}
}

func TestAwaitVerdict_Rejected_NewAttempt(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.MaxCoderIterations = 10
	state.Config.MaxReviewCycles = 5

	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
	task.Iteration = 1
	task.Attempt = 1
	task.ReviewCyclesCurrent = 4 // one below limit — budget gate passes
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
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	var result *AwaitVerdictResult
	var awaitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, awaitErr = AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 10*time.Second)
	}()

	testhelpers.WaitForAsyncSetup()
	// Simulate reviewer rejection: increment ReviewCyclesCurrent (as submit_verdict does).
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusRejected
		reason := "Missing tests"
		tk.RejectionReason = &reason
		tk.ReviewCyclesCurrent = 5 // now at limit — ClaimTask triggers new attempt
		reviewer := "code-reviewer-1"
		tk.History = append(tk.History, models.TaskHistoryEntry{
			Time:  time.Now().UTC(),
			Event: models.TaskEventReviewVerdictRejected,
			Agent: &reviewer,
		})
		return nil
	}); err != nil {
		t.Fatalf("Failed to modify state: %v", err)
	}

	<-done
	if awaitErr != nil {
		t.Fatalf("AwaitVerdict error: %v", awaitErr)
	}
	if result.Verdict != VerdictNewAttempt {
		t.Errorf("Verdict = %q, want NEW_ATTEMPT", result.Verdict)
	}

	// Verify ownership released.
	s, readErr := db.For(stateFile).Read()
	if readErr != nil {
		t.Fatalf("Failed to read state: %v", readErr)
	}
	agent := s.Agents["coder-1"]
	if agent.CurrentTask != nil {
		t.Errorf("expected CurrentTask=nil after NEW_ATTEMPT, got %q", *agent.CurrentTask)
	}
}

func TestAwaitVerdict_Terminal(t *testing.T) {
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
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	var result *AwaitVerdictResult
	var awaitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, awaitErr = AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 10*time.Second)
	}()

	testhelpers.WaitForAsyncSetup()
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusBlocked
		reason := "Spec ambiguity"
		tk.BlockedReason = &reason
		return nil
	}); err != nil {
		t.Fatalf("Failed to modify state: %v", err)
	}

	<-done
	if awaitErr != nil {
		t.Fatalf("AwaitVerdict error: %v", awaitErr)
	}
	if result.Verdict != VerdictTerminal {
		t.Errorf("Verdict = %q, want TERMINAL", result.Verdict)
	}
	if !strings.Contains(result.Reason, "terminal status") {
		t.Errorf("Reason = %q, want to contain 'terminal status'", result.Reason)
	}
	if result.TaskStatus != models.TaskStatusBlocked {
		t.Errorf("TaskStatus = %q, want BLOCKED", result.TaskStatus)
	}

	// Verify ownership released.
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("Failed to read state: %v", readErr)
	}
	agent := s.Agents["coder-1"]
	if agent.CurrentTask != nil {
		t.Errorf("expected CurrentTask=nil after terminal, got %q", *agent.CurrentTask)
	}
}

func TestAwaitVerdict_Timeout(t *testing.T) {
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
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	// Very short timeout — task stays submitted, deadline fires.
	result, err := AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("AwaitVerdict error: %v", err)
	}
	if result.Verdict != VerdictTimeout {
		t.Errorf("Verdict = %q, want TIMEOUT", result.Verdict)
	}

	// Verify ownership released.
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("Failed to read state: %v", readErr)
	}
	agent := s.Agents["coder-1"]
	if agent.CurrentTask != nil {
		t.Errorf("expected CurrentTask=nil after timeout, got %q", *agent.CurrentTask)
	}
}

func TestAwaitVerdict_Aborted(t *testing.T) {
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
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	var result *AwaitVerdictResult
	var awaitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, awaitErr = AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 10*time.Second)
	}()

	testhelpers.WaitForAsyncSetup()
	if err := bb.Modify(func(s *models.State) error {
		s.Config.Mode = models.SystemModeStopped
		return nil
	}); err != nil {
		t.Fatalf("Failed to modify state: %v", err)
	}

	<-done
	if awaitErr != nil {
		t.Fatalf("AwaitVerdict error: %v", awaitErr)
	}
	if result.Verdict != VerdictAborted {
		t.Errorf("Verdict = %q, want ABORTED", result.Verdict)
	}

	// Verify ownership released.
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("Failed to read state: %v", readErr)
	}
	agent := s.Agents["coder-1"]
	if agent.CurrentTask != nil {
		t.Errorf("expected CurrentTask=nil after abort, got %q", *agent.CurrentTask)
	}
}

func TestAwaitVerdict_PartiallyApproved(t *testing.T) {
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
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	var result *AwaitVerdictResult
	var awaitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, awaitErr = AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 10*time.Second)
	}()

	// Phase 1: transition to partially approved (quorum not met).
	testhelpers.WaitForAsyncSetup()
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusPartiallyApproved
		return nil
	}); err != nil {
		t.Fatalf("Failed to set partially approved: %v", err)
	}

	// Phase 2: verify AwaitVerdict is still waiting.
	testhelpers.WaitForAsyncSetup()
	select {
	case <-done:
		t.Fatal("AwaitVerdict should not have returned at partially approved")
	default:
		// still waiting — correct
	}

	// Phase 3: transition to approved (quorum met).
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusApproved
		reviewer := "code-reviewer-1"
		tk.ApprovedBy = &reviewer
		tk.History = append(tk.History, models.TaskHistoryEntry{
			Time:  time.Now().UTC(),
			Event: models.TaskEventReviewVerdictApproved,
			Agent: &reviewer,
		})
		return nil
	}); err != nil {
		t.Fatalf("Failed to set approved: %v", err)
	}

	<-done
	if awaitErr != nil {
		t.Fatalf("AwaitVerdict error: %v", awaitErr)
	}
	if result.Verdict != VerdictApproved {
		t.Errorf("Verdict = %q, want APPROVED (after partial)", result.Verdict)
	}
}

func TestAwaitVerdict_RaceGuard(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.MaxCoderIterations = 10
	state.Config.MaxReviewCycles = 5

	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
	task.Iteration = 1
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
	state.Agents["coder-2"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusIdle,
	}
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	var result *AwaitVerdictResult
	var awaitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, awaitErr = AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 10*time.Second)
	}()

	testhelpers.WaitForAsyncSetup()

	// While coder-1 awaits, coder-2 cannot claim the task (still READY_FOR_REVIEW).
	_, claimErr := ClaimTask(tmpDir, "task-1", "coder-2")
	if claimErr == nil {
		t.Fatal("expected ClaimTask by coder-2 to fail while task is READY_FOR_REVIEW")
	}

	// Transition to REJECTED — AwaitVerdict auto-reclaims for coder-1.
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusRejected
		reason := "Needs fixes"
		tk.RejectionReason = &reason
		reviewer := "code-reviewer-1"
		tk.History = append(tk.History, models.TaskHistoryEntry{
			Time:  time.Now().UTC(),
			Event: models.TaskEventReviewVerdictRejected,
			Agent: &reviewer,
		})
		return nil
	}); err != nil {
		t.Fatalf("Failed to modify state: %v", err)
	}

	<-done
	if awaitErr != nil {
		t.Fatalf("AwaitVerdict error: %v", awaitErr)
	}
	if result.Verdict != VerdictRejected {
		t.Errorf("Verdict = %q, want REJECTED", result.Verdict)
	}

	// Verify coder-1 owns the task (auto-reclaimed).
	s, readErr := db.For(stateFile).Read()
	if readErr != nil {
		t.Fatalf("Failed to read state: %v", readErr)
	}
	reclaimedTask := s.FindTask("task-1")
	if reclaimedTask == nil {
		t.Fatal("Task not found")
	}
	if reclaimedTask.AssignedTo == nil || *reclaimedTask.AssignedTo != "coder-1" {
		assigned := "<nil>"
		if reclaimedTask.AssignedTo != nil {
			assigned = *reclaimedTask.AssignedTo
		}
		t.Errorf("Task assigned to %s, want coder-1 (coder-2 should never have acquired)", assigned)
	}

	// Verify coder-2 never acquired the task.
	agent2 := s.Agents["coder-2"]
	if agent2.CurrentTask != nil && *agent2.CurrentTask == "task-1" {
		t.Error("coder-2 should not have acquired the task")
	}
}
