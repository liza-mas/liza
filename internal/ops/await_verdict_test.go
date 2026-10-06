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
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestAwaitVerdict_EmptyTaskID(t *testing.T) {
	t.Parallel()
	_, err := AwaitVerdict(context.Background(), "/nonexistent", "", "coder-1", 30*time.Second)
	testhelpers.RequireErrorContains(t, err, "task ID is required")

	var pe *PreconditionError
	if !stderrors.As(err, &pe) {
		t.Fatalf("expected PreconditionError, got %T: %v", err, err)
	}
}

func TestAwaitVerdict_EmptyAgentID(t *testing.T) {
	t.Parallel()
	_, err := AwaitVerdict(context.Background(), "/nonexistent", "task-1", "", 30*time.Second)
	testhelpers.RequireErrorContains(t, err, "agent ID is required")

	var pe *PreconditionError
	if !stderrors.As(err, &pe) {
		t.Fatalf("expected PreconditionError, got %T: %v", err, err)
	}
}

func TestAwaitVerdict_TaskNotFound(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	// IMPLEMENTING with no submission history — submitter validation rejects first.
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	state.Agents["coder-1"] = models.Agent{Role: "coder", Status: models.AgentStatusIdle}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 30*time.Second)
	if err == nil {
		t.Fatal("expected error for wrong status")
	}

	var pe *PreconditionError
	if !stderrors.As(err, &pe) {
		t.Fatalf("expected PreconditionError, got %T: %v", err, err)
	}
	testhelpers.RequireErrorContains(t, err, "no submission history")
}

func TestAwaitVerdict_WrongAgent(t *testing.T) {
	t.Parallel()
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
	state.Agents["coder-2"] = models.Agent{Role: "coder", Status: models.AgentStatusIdle}
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
	t.Parallel()
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
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
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
	t.Parallel()
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
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
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
	t.Parallel()
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
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
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
	t.Parallel()
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
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
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
	t.Parallel()
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
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
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
	t.Parallel()
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
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
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
			Event: models.TaskEventApproved,
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
	if result.SafeAction != SafeActionStop {
		t.Errorf("SafeAction = %q, want %q", result.SafeAction, SafeActionStop)
	}
	if !strings.Contains(result.Guidance, "Stop this session") {
		t.Errorf("Guidance = %q, want stop-session guidance", result.Guidance)
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
	t.Parallel()
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
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
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
		leaseExpires := time.Now().UTC().Add(30 * time.Minute)
		tk.LeaseExpires = &leaseExpires
		reviewer := "code-reviewer-1"
		tk.History = append(tk.History, models.TaskHistoryEntry{
			Time:  time.Now().UTC(),
			Event: models.TaskEventRejected,
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
		t.Errorf("Verdict = %q, want REJECTED; reason=%q", result.Verdict, result.Reason)
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

func TestAwaitVerdict_Rejected_ObservedByTickWhenWatcherSilent(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	watcherReady := make(chan struct{})
	originalWatcher := newAwaitVerdictWatcher
	newAwaitVerdictWatcher = func(bb *db.Blackboard) (awaitVerdictWatcher, error) {
		close(watcherReady)
		return silentAwaitVerdictWatcher{}, nil
	}
	defer func() { newAwaitVerdictWatcher = originalWatcher }()

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
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	// The budgets below are ceilings, not waits: the test proceeds as soon as
	// each step happens. They were tight enough that the race detector, which
	// slows everything down, pushed the tick past them and failed the test for
	// timing rather than for behaviour. The tick interval above is 10ms, so
	// there is no cost to being generous here.
	var result *AwaitVerdictResult
	var awaitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, awaitErr = AwaitVerdictWithOptions(
			context.Background(), tmpDir, "task-1", "coder-1", 30*time.Second,
			AwaitVerdictOptions{AbortPollInterval: 10 * time.Millisecond},
		)
	}()

	select {
	case <-watcherReady:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for silent watcher setup")
	}

	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusRejected
		reason := "Missing error handling"
		tk.RejectionReason = &reason
		leaseExpires := time.Now().UTC().Add(30 * time.Minute)
		tk.LeaseExpires = &leaseExpires
		reviewer := "code-reviewer-1"
		tk.History = append(tk.History, models.TaskHistoryEntry{
			Time:  time.Now().UTC(),
			Event: models.TaskEventRejected,
			Agent: &reviewer,
		})
		return nil
	}); err != nil {
		t.Fatalf("Failed to modify state: %v", err)
	}

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("AwaitVerdict did not observe rejected verdict through periodic tick")
	}
	if awaitErr != nil {
		t.Fatalf("AwaitVerdict error: %v", awaitErr)
	}
	if result.Verdict != VerdictRejected {
		t.Errorf("Verdict = %q, want REJECTED; reason=%q", result.Verdict, result.Reason)
	}
	if result.ReviewerAgent != "code-reviewer-1" {
		t.Errorf("ReviewerAgent = %q, want code-reviewer-1", result.ReviewerAgent)
	}
	if result.Iteration != 2 {
		t.Errorf("Iteration = %d, want 2", result.Iteration)
	}
}

func TestAwaitVerdict_Rejected_NewAttempt(t *testing.T) {
	t.Parallel()
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
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
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
		leaseExpires := time.Now().UTC().Add(30 * time.Minute)
		tk.LeaseExpires = &leaseExpires
		reviewer := "code-reviewer-1"
		tk.History = append(tk.History, models.TaskHistoryEntry{
			Time:  time.Now().UTC(),
			Event: models.TaskEventRejected,
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
		t.Errorf("Verdict = %q, want NEW_ATTEMPT; reason=%q", result.Verdict, result.Reason)
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
	t.Parallel()
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
	if !strings.Contains(result.Reason, "non-awaitable status") {
		t.Errorf("Reason = %q, want to contain 'non-awaitable status'", result.Reason)
	}
	if result.TaskStatus != models.TaskStatusBlocked {
		t.Errorf("TaskStatus = %q, want BLOCKED", result.TaskStatus)
	}
	if result.SafeAction != SafeActionStop {
		t.Errorf("SafeAction = %q, want %q", result.SafeAction, SafeActionStop)
	}
	if !strings.Contains(result.Guidance, "Stop this session") {
		t.Errorf("Guidance = %q, want stop-session guidance", result.Guidance)
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
	t.Parallel()
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

func TestAwaitVerdict_DelayedWatcherErrorUsesOriginalDeadline(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
	task.SpecRef = state.Goal.SpecRef
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventSubmittedForReview,
		Agent: strPtr("coder-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{Role: "coder", Status: models.AgentStatusWaiting}
	testhelpers.WriteInitialState(t, stateFile, state)

	watcherErr := stderrors.New("delayed watcher failure")
	const simulatedWatcherDelay = 3 * time.Second
	deadlineBase := time.Now()
	currentTime := deadlineBase
	previousNow := awaitVerdictNow
	awaitVerdictNow = func() time.Time { return currentTime }
	t.Cleanup(func() { awaitVerdictNow = previousNow })

	previousWatcher := newAwaitVerdictWatcher
	newAwaitVerdictWatcher = func(*db.Blackboard) (awaitVerdictWatcher, error) {
		currentTime = deadlineBase.Add(simulatedWatcherDelay)
		errors := make(chan error, 1)
		errors <- watcherErr
		return &delayedErrorWatcher{
			events: make(chan struct{}),
			errors: errors,
		}, nil
	}
	t.Cleanup(func() { newAwaitVerdictWatcher = previousWatcher })

	var deadlineObserved time.Time
	var pollIntervalObserved time.Duration
	previousPolling := runAwaitVerdictPolling
	runAwaitVerdictPolling = func(
		_ context.Context,
		_ *db.Blackboard,
		_, _ string,
		_ *models.AgentAuthority,
		deadline time.Time,
		_ models.TaskStatus,
		_ *pipeline.Resolver,
		_, _ string,
		pollInterval time.Duration,
		_ int,
	) (*AwaitVerdictResult, error) {
		deadlineObserved = deadline
		pollIntervalObserved = pollInterval
		return nil, watcherErr
	}
	t.Cleanup(func() { runAwaitVerdictPolling = previousPolling })

	const overallTimeout = 10 * time.Second
	const fallbackPollInterval = 17 * time.Millisecond
	result, err := AwaitVerdictWithOptions(
		context.Background(), tmpDir, "task-1", "coder-1", overallTimeout,
		AwaitVerdictOptions{FallbackPollInterval: fallbackPollInterval},
	)
	if result != nil {
		t.Fatalf("AwaitVerdict result = %#v, want nil", result)
	}
	if !stderrors.Is(err, watcherErr) {
		t.Fatalf("AwaitVerdict error = %v, want %v", err, watcherErr)
	}

	wantDeadline := deadlineBase.Add(overallTimeout)
	if !deadlineObserved.Equal(wantDeadline) {
		t.Fatalf("polling deadline = %s, want original deadline %s", deadlineObserved, wantDeadline)
	}
	if pollIntervalObserved != fallbackPollInterval {
		t.Fatalf("polling interval = %s, want %s", pollIntervalObserved, fallbackPollInterval)
	}
}

func TestAwaitVerdict_Aborted(t *testing.T) {
	t.Parallel()
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

func TestAwaitVerdict_AlreadyBlocked(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventSubmittedForReview,
		Agent: strPtr("coder-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusIdle,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 10*time.Second)
	if err != nil {
		t.Fatalf("AwaitVerdict error: %v", err)
	}
	if result.Verdict != VerdictTerminal {
		t.Errorf("Verdict = %q, want TERMINAL", result.Verdict)
	}
	if result.TaskStatus != models.TaskStatusBlocked {
		t.Errorf("TaskStatus = %q, want BLOCKED", result.TaskStatus)
	}
	if result.SafeAction != SafeActionStop {
		t.Errorf("SafeAction = %q, want %q", result.SafeAction, SafeActionStop)
	}
	if !strings.Contains(result.Guidance, "Stop this session") {
		t.Errorf("Guidance = %q, want stop-session guidance", result.Guidance)
	}

	// Verify no ownership mutation.
	bb := db.For(stateFile)
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	agent := s.Agents["coder-1"]
	if agent.CurrentTask != nil {
		t.Error("CurrentTask should remain nil (no ownership acquired)")
	}
}

func TestAwaitVerdict_AlreadyTerminal(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusSuperseded, now)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventSubmittedForReview,
		Agent: strPtr("coder-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusIdle,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 10*time.Second)
	if err != nil {
		t.Fatalf("AwaitVerdict error: %v", err)
	}
	if result.Verdict != VerdictTerminal {
		t.Errorf("Verdict = %q, want TERMINAL", result.Verdict)
	}
	if result.TaskStatus != models.TaskStatusSuperseded {
		t.Errorf("TaskStatus = %q, want SUPERSEDED", result.TaskStatus)
	}
	if result.SafeAction != SafeActionStop {
		t.Errorf("SafeAction = %q, want %q", result.SafeAction, SafeActionStop)
	}
	if !strings.Contains(result.Guidance, "Stop this session") {
		t.Errorf("Guidance = %q, want stop-session guidance", result.Guidance)
	}
}

func TestAwaitVerdict_AlreadyApproved(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusApproved, now)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventSubmittedForReview,
		Agent: strPtr("coder-1"),
	}, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventApproved,
		Agent: strPtr("code-reviewer-1"),
	})
	reviewer := "code-reviewer-1"
	task.ApprovedBy = &reviewer
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusIdle,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 10*time.Second)
	if err != nil {
		t.Fatalf("AwaitVerdict error: %v", err)
	}
	if result.Verdict != VerdictApproved {
		t.Errorf("Verdict = %q, want APPROVED", result.Verdict)
	}
	if result.ReviewerAgent != "code-reviewer-1" {
		t.Errorf("ReviewerAgent = %q, want code-reviewer-1", result.ReviewerAgent)
	}
	if result.SafeAction != SafeActionStop {
		t.Errorf("SafeAction = %q, want %q", result.SafeAction, SafeActionStop)
	}
	if !strings.Contains(result.Guidance, "Stop this session") {
		t.Errorf("Guidance = %q, want stop-session guidance", result.Guidance)
	}
}

func TestAwaitVerdict_AlreadyMergedWithApprovalHistoryStopsWorktreeCommands(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
	reviewer := "code-reviewer-1"
	reviewCommit := "review-commit"
	task.ReviewCommit = &reviewCommit
	task.History = append(task.History,
		models.TaskHistoryEntry{
			Time:   now.Add(-2 * time.Minute),
			Event:  models.TaskEventSubmittedForReview,
			Agent:  strPtr("coder-1"),
			Commit: &reviewCommit,
		},
		models.TaskHistoryEntry{
			Time:   now.Add(-1 * time.Minute),
			Event:  models.TaskEventApproved,
			Agent:  &reviewer,
			Commit: &reviewCommit,
		},
	)
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusIdle,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 10*time.Second)
	if err != nil {
		t.Fatalf("AwaitVerdict error: %v", err)
	}
	if result.Verdict != VerdictAlreadyTransitioned {
		t.Errorf("Verdict = %q, want %q", result.Verdict, VerdictAlreadyTransitioned)
	}
	if result.TaskStatus != models.TaskStatusMerged {
		t.Errorf("TaskStatus = %q, want MERGED", result.TaskStatus)
	}
	if result.SafeAction != SafeActionStop {
		t.Errorf("SafeAction = %q, want %q", result.SafeAction, SafeActionStop)
	}
	if !strings.Contains(result.Guidance, "do not retry await-verdict or run more worktree commands") {
		t.Errorf("Guidance = %q, want no-worktree-command guidance", result.Guidance)
	}
}

func TestAwaitVerdict_AlreadyBlocked_NonexistentAgent(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventSubmittedForReview,
		Agent: strPtr("coder-1"),
	})
	state.Tasks = []models.Task{task}
	// coder-1 NOT in state.Agents
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 10*time.Second)
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestAwaitVerdict_AlreadyBlocked_WrongSubmitter(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventSubmittedForReview,
		Agent: strPtr("coder-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["coder-2"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusIdle,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-2", 10*time.Second)
	if err == nil {
		t.Fatal("expected error for wrong submitter")
	}
	var pe *PreconditionError
	if !stderrors.As(err, &pe) {
		t.Fatalf("expected PreconditionError, got %T: %v", err, err)
	}
}

func TestAwaitVerdict_PartiallyApproved(t *testing.T) {
	t.Parallel()
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
			Event: models.TaskEventApproved,
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
	t.Parallel()
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
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
	state.Agents["coder-2"] = testhelpers.RegisteredTestAgent("coder")
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
		leaseExpires := time.Now().UTC().Add(30 * time.Minute)
		tk.LeaseExpires = &leaseExpires
		reviewer := "code-reviewer-1"
		tk.History = append(tk.History, models.TaskHistoryEntry{
			Time:  time.Now().UTC(),
			Event: models.TaskEventRejected,
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

func TestAwaitVerdict_RejectedAlreadyReassigned_ReturnsVerdict(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	reviewCommit := "abc123"
	reason := "Missing error handling"
	reviewer := "code-reviewer-1"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	task.AssignedTo = strPtr("coder-2")
	task.ReviewCommit = &reviewCommit
	task.RejectionReason = &reason
	task.History = append(task.History,
		models.TaskHistoryEntry{
			Time:   now.Add(-2 * time.Minute),
			Event:  models.TaskEventSubmittedForReview,
			Agent:  strPtr("coder-1"),
			Commit: &reviewCommit,
		},
		models.TaskHistoryEntry{
			Time:   now.Add(-1 * time.Minute),
			Event:  models.TaskEventRejected,
			Agent:  &reviewer,
			Reason: &reason,
			Commit: &reviewCommit,
		},
		models.TaskHistoryEntry{
			Time:             now,
			Event:            models.TaskEventReassignedAfterRejection,
			Agent:            strPtr("coder-2"),
			PreviousAssignee: strPtr("coder-1"),
		},
	)
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusWaiting,
	}
	state.Agents["coder-2"] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: strPtr("task-1"),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 10*time.Second)
	if err != nil {
		t.Fatalf("AwaitVerdict error: %v", err)
	}
	if result.Verdict != VerdictAlreadyTransitioned {
		t.Errorf("Verdict = %q, want %q", result.Verdict, VerdictAlreadyTransitioned)
	}
	if result.Reason != reason {
		t.Errorf("Reason = %q, want %q", result.Reason, reason)
	}
	if result.ReviewerAgent != reviewer {
		t.Errorf("ReviewerAgent = %q, want %q", result.ReviewerAgent, reviewer)
	}
	if result.TaskStatus != models.TaskStatusImplementing {
		t.Errorf("TaskStatus = %q, want %s", result.TaskStatus, models.TaskStatusImplementing)
	}
	if result.CurrentAssignee != "coder-2" {
		t.Errorf("CurrentAssignee = %q, want coder-2", result.CurrentAssignee)
	}
	if result.ReviewCommit != reviewCommit {
		t.Errorf("ReviewCommit = %q, want %q", result.ReviewCommit, reviewCommit)
	}
	if result.SafeAction != SafeActionStop {
		t.Errorf("SafeAction = %q, want %q", result.SafeAction, SafeActionStop)
	}
}

func TestAwaitVerdict_RejectedBeforeLaterSubmission_ReturnsCallerVerdict(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	firstCommit := "commit-coder-1"
	secondCommit := "commit-coder-2"
	reason := "Missing error handling"
	reviewer := "code-reviewer-1"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
	task.AssignedTo = strPtr("coder-2")
	task.ReviewCommit = &secondCommit
	task.History = append(task.History,
		models.TaskHistoryEntry{
			Time:   now.Add(-4 * time.Minute),
			Event:  models.TaskEventSubmittedForReview,
			Agent:  strPtr("coder-1"),
			Commit: &firstCommit,
		},
		models.TaskHistoryEntry{
			Time:   now.Add(-3 * time.Minute),
			Event:  models.TaskEventRejected,
			Agent:  &reviewer,
			Reason: &reason,
			Commit: &firstCommit,
		},
		models.TaskHistoryEntry{
			Time:             now.Add(-2 * time.Minute),
			Event:            models.TaskEventReassignedAfterRejection,
			Agent:            strPtr("coder-2"),
			PreviousAssignee: strPtr("coder-1"),
		},
		models.TaskHistoryEntry{
			Time:   now.Add(-1 * time.Minute),
			Event:  models.TaskEventSubmittedForReview,
			Agent:  strPtr("coder-2"),
			Commit: &secondCommit,
		},
	)
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusWaiting,
	}
	state.Agents["coder-2"] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: strPtr("task-1"),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 10*time.Second)
	if err != nil {
		t.Fatalf("AwaitVerdict error: %v", err)
	}
	if result.Verdict != VerdictAlreadyTransitioned {
		t.Errorf("Verdict = %q, want %q", result.Verdict, VerdictAlreadyTransitioned)
	}
	if result.Reason != reason {
		t.Errorf("Reason = %q, want %q", result.Reason, reason)
	}
	if result.ReviewCommit != firstCommit {
		t.Errorf("ReviewCommit = %q, want %q", result.ReviewCommit, firstCommit)
	}
	if result.CurrentAssignee != "coder-2" {
		t.Errorf("CurrentAssignee = %q, want coder-2", result.CurrentAssignee)
	}
	if result.SafeAction != SafeActionStop {
		t.Errorf("SafeAction = %q, want %q", result.SafeAction, SafeActionStop)
	}
}

func TestAwaitVerdict_RejectedAfterReviewCommitUpdate_ReturnsUpdatedVerdict(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	oldCommit := "old-review-commit"
	newCommit := "new-review-commit"
	reason := "Tests fail after rebase"
	reviewer := "code-reviewer-1"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	task.AssignedTo = strPtr("coder-2")
	task.ReviewCommit = &newCommit
	task.RejectionReason = &reason
	task.History = append(task.History,
		models.TaskHistoryEntry{
			Time:   now.Add(-4 * time.Minute),
			Event:  models.TaskEventSubmittedForReview,
			Agent:  strPtr("coder-1"),
			Commit: &oldCommit,
		},
		models.TaskHistoryEntry{
			Time:   now.Add(-3 * time.Minute),
			Event:  models.TaskEventReviewCommitUpdated,
			Agent:  strPtr("human"),
			Commit: &newCommit,
			Extra: map[string]any{
				"old_review_commit": oldCommit,
				"new_review_commit": newCommit,
			},
		},
		models.TaskHistoryEntry{
			Time:   now.Add(-2 * time.Minute),
			Event:  models.TaskEventRejected,
			Agent:  &reviewer,
			Reason: &reason,
			Commit: &newCommit,
		},
		models.TaskHistoryEntry{
			Time:             now.Add(-1 * time.Minute),
			Event:            models.TaskEventReassignedAfterRejection,
			Agent:            strPtr("coder-2"),
			PreviousAssignee: strPtr("coder-1"),
		},
	)
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusWaiting,
	}
	state.Agents["coder-2"] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: strPtr("task-1"),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 10*time.Second)
	if err != nil {
		t.Fatalf("AwaitVerdict error: %v", err)
	}
	if result.Verdict != VerdictAlreadyTransitioned {
		t.Errorf("Verdict = %q, want %q", result.Verdict, VerdictAlreadyTransitioned)
	}
	if result.Reason != reason {
		t.Errorf("Reason = %q, want %q", result.Reason, reason)
	}
	if result.ReviewCommit != newCommit {
		t.Errorf("ReviewCommit = %q, want %q", result.ReviewCommit, newCommit)
	}
}

func TestHandleVerdictResult_NonAwaitableStatusRecoversVerdict(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	reviewCommit := "abc123"
	reason := "Missing error handling"
	reviewer := "code-reviewer-1"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	task.AssignedTo = strPtr("coder-2")
	task.ReviewCommit = &reviewCommit
	task.RejectionReason = &reason
	task.History = append(task.History,
		models.TaskHistoryEntry{
			Time:   now.Add(-2 * time.Minute),
			Event:  models.TaskEventSubmittedForReview,
			Agent:  strPtr("coder-1"),
			Commit: &reviewCommit,
		},
		models.TaskHistoryEntry{
			Time:   now.Add(-1 * time.Minute),
			Event:  models.TaskEventRejected,
			Agent:  &reviewer,
			Reason: &reason,
			Commit: &reviewCommit,
		},
	)
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWaiting,
		CurrentTask: strPtr("task-1"),
	}
	state.Agents["coder-2"] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: strPtr("task-1"),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver error: %v", err)
	}
	bb := db.For(stateFile)
	submission, _ := latestSubmissionByAgent(&task, "coder-1")
	result, err := handleVerdictResult(bb, &task, "coder-1", nil, tmpDir, resolver, task.RolePair, submission.index)
	if err != nil {
		t.Fatalf("handleVerdictResult error: %v", err)
	}
	if result.Verdict != VerdictAlreadyTransitioned {
		t.Errorf("Verdict = %q, want %q", result.Verdict, VerdictAlreadyTransitioned)
	}
	if result.Reason != reason {
		t.Errorf("Reason = %q, want %q", result.Reason, reason)
	}
	if result.ReviewCommit != reviewCommit {
		t.Errorf("ReviewCommit = %q, want %q", result.ReviewCommit, reviewCommit)
	}

	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("read state error: %v", err)
	}
	agent := readState.Agents["coder-1"]
	if agent.CurrentTask != nil {
		t.Fatalf("coder-1 CurrentTask = %q, want nil after recovered verdict", *agent.CurrentTask)
	}
}

type silentAwaitVerdictWatcher struct{}

func (silentAwaitVerdictWatcher) Events() <-chan struct{} {
	return make(chan struct{})
}

func (silentAwaitVerdictWatcher) Errors() <-chan error {
	return make(chan error)
}

func (silentAwaitVerdictWatcher) Close() error {
	return nil
}

type delayedErrorWatcher struct {
	events <-chan struct{}
	errors <-chan error
}

func newDelayedErrorWatcher(delay time.Duration) *delayedErrorWatcher {
	events := make(chan struct{})
	errors := make(chan error, 1)
	time.AfterFunc(delay, func() {
		errors <- stderrors.New("delayed watcher failure")
	})
	return &delayedErrorWatcher{events: events, errors: errors}
}

func (w *delayedErrorWatcher) Events() <-chan struct{} { return w.events }

func (w *delayedErrorWatcher) Errors() <-chan error { return w.errors }

func (*delayedErrorWatcher) Close() error { return nil }

// Rejected auto-reclaim goes through ClaimTask, so a failing configured setup
// command must degrade the agent there too — otherwise this path retries the
// failing worktree without a bound (ADR-0117).
func TestAwaitVerdict_RejectedAutoReclaim_PostWorktreeCmdFailureDegradesAgent(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.MaxCoderIterations = 10
	state.Config.MaxReviewCycles = 5
	postCmd := "exit 1"
	state.Config.PostWorktreeCmd = &postCmd

	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
	task.Iteration = 1
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventSubmittedForReview,
		Agent: strPtr("coder-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	var awaitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, awaitErr = AwaitVerdict(context.Background(), tmpDir, "task-1", "coder-1", 10*time.Second)
	}()

	testhelpers.WaitForAsyncSetup()
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusRejected
		reason := "Missing error handling"
		tk.RejectionReason = &reason
		leaseExpires := time.Now().UTC().Add(30 * time.Minute)
		tk.LeaseExpires = &leaseExpires
		reviewer := "code-reviewer-1"
		tk.History = append(tk.History, models.TaskHistoryEntry{
			Time:  time.Now().UTC(),
			Event: models.TaskEventRejected,
			Agent: &reviewer,
		})
		return nil
	}); err != nil {
		t.Fatalf("Failed to modify state: %v", err)
	}

	<-done

	if awaitErr == nil {
		t.Fatal("AwaitVerdict() error = nil, want the setup failure surfaced")
	}
	// Bounded: the caller can distinguish "stop, health recorded" from an
	// ordinary retryable reclaim failure.
	if !stderrors.Is(awaitErr, ErrAgentDegraded) {
		t.Fatalf("AwaitVerdict() error = %v, want ErrAgentDegraded", awaitErr)
	}

	after, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("Read() error = %v", readErr)
	}
	health, ok := after.AgentHealth["coder-1"]
	if !ok {
		t.Fatal("AgentHealth missing entry, want degraded record for the auto-reclaim failure")
	}
	if health.Reason != AgentDegradedWorktreeSetupFailed {
		t.Errorf("health.Reason = %q, want %q", health.Reason, AgentDegradedWorktreeSetupFailed)
	}
	if strings.Contains(health.LastError, "\n") {
		t.Errorf("health.LastError = %q, want a single-line persisted diagnostic", health.LastError)
	}
}
