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

// --- Precondition tests ---

func TestAwaitResubmission_EmptyTaskID(t *testing.T) {
	_, err := AwaitResubmission(context.Background(), "/nonexistent", "", "reviewer-1", 30*time.Second)
	testhelpers.RequireErrorContains(t, err, "task ID is required")

	var pe *PreconditionError
	if !stderrors.As(err, &pe) {
		t.Fatalf("expected PreconditionError, got %T: %v", err, err)
	}
}

func TestAwaitResubmission_EmptyAgentID(t *testing.T) {
	_, err := AwaitResubmission(context.Background(), "/nonexistent", "task-1", "", 30*time.Second)
	testhelpers.RequireErrorContains(t, err, "agent ID is required")

	var pe *PreconditionError
	if !stderrors.As(err, &pe) {
		t.Fatalf("expected PreconditionError, got %T: %v", err, err)
	}
}

func TestAwaitResubmission_TaskNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AwaitResubmission(context.Background(), tmpDir, "nonexistent", "reviewer-1", 30*time.Second)
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestAwaitResubmission_WrongStatus(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	// IMPLEMENTING is not rejected or submitted — should fail precondition.
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AwaitResubmission(context.Background(), tmpDir, "task-1", "reviewer-1", 30*time.Second)
	if err == nil {
		t.Fatal("expected error for wrong status")
	}

	var pe *PreconditionError
	if !stderrors.As(err, &pe) {
		t.Fatalf("expected PreconditionError, got %T: %v", err, err)
	}
	testhelpers.RequireErrorContains(t, err, "not in a rejected or submitted status")
}

func TestAwaitResubmission_WrongAgent(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	// Add rejection history from reviewer-1.
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventRejected,
		Agent: strPtr("reviewer-1"),
	})
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	// reviewer-2 was NOT the last rejecting reviewer.
	_, err := AwaitResubmission(context.Background(), tmpDir, "task-1", "reviewer-2", 30*time.Second)
	if err == nil {
		t.Fatal("expected error for wrong agent")
	}

	var pe *PreconditionError
	if !stderrors.As(err, &pe) {
		t.Fatalf("expected PreconditionError, got %T: %v", err, err)
	}
	testhelpers.RequireErrorContains(t, err, "not the last rejecting reviewer")
}

// --- Ownership test ---

func TestAwaitResubmission_OwnershipAcquired(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventRejected,
		Agent: strPtr("reviewer-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusIdle,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Use a pre-cancelled context so the event loop exits immediately
	// after ownership acquisition. context.Canceled proves preconditions
	// passed and ownership was acquired.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := AwaitResubmission(ctx, tmpDir, "task-1", "reviewer-1", 30*time.Second)
	if !stderrors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled (proving event loop reached), got %v", err)
	}

	// Verify ownership was set before context cancellation released it.
	// After cancellation, releaseReviewOwnership clears CurrentTask,
	// but we can verify the agent exists and was processed.
	bb := db.For(stateFile)
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	// ReviewingBy is cleared on context cancel (releaseReviewOwnership).
	tk := s.FindTask("task-1")
	if tk == nil {
		t.Fatal("task not found")
	}
	if tk.ReviewingBy != nil {
		t.Error("expected ReviewingBy=nil after context cancel (ownership released)")
	}
}

// --- Verdict path tests ---

func TestAwaitResubmission_Resubmitted(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventRejected,
		Agent: strPtr("reviewer-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusIdle,
	}
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	var result *AwaitResubmissionResult
	var awaitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, awaitErr = AwaitResubmission(context.Background(), tmpDir, "task-1", "reviewer-1", 10*time.Second)
	}()

	testhelpers.WaitForAsyncSetup()
	// Simulate doer resubmission: transition to CODE_READY_FOR_REVIEW.
	newCommit := "newcommit456"
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusReadyForReview
		tk.ReviewCommit = &newCommit
		tk.ReviewCyclesCurrent = 2
		return nil
	}); err != nil {
		t.Fatalf("failed to modify state: %v", err)
	}

	<-done
	if awaitErr != nil {
		t.Fatalf("AwaitResubmission error: %v", awaitErr)
	}
	if result.Verdict != ResubmissionResubmitted {
		t.Errorf("Verdict = %q, want RESUBMITTED", result.Verdict)
	}
	if result.TaskStatus != models.TaskStatusReviewing {
		t.Errorf("TaskStatus = %q, want REVIEWING_CODE", result.TaskStatus)
	}
	if result.ReviewCommit != newCommit {
		t.Errorf("ReviewCommit = %q, want %q", result.ReviewCommit, newCommit)
	}
	if result.ReviewCycle != 2 {
		t.Errorf("ReviewCycle = %d, want 2", result.ReviewCycle)
	}

	// Verify ownership state after reclaim.
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	tk := s.FindTask("task-1")
	if tk == nil {
		t.Fatal("task not found after reclaim")
	}
	if tk.ReviewingBy == nil || *tk.ReviewingBy != "reviewer-1" {
		t.Errorf("ReviewingBy should be reviewer-1 after reclaim")
	}
	if tk.ReviewLeaseExpires == nil {
		t.Error("ReviewLeaseExpires should be set after reclaim")
	}
	agent := s.Agents["reviewer-1"]
	if agent.Status != models.AgentStatusReviewing {
		t.Errorf("agent status = %q, want REVIEWING", agent.Status)
	}
	if agent.CurrentTask == nil || *agent.CurrentTask != "task-1" {
		t.Error("agent CurrentTask should be task-1 after reclaim")
	}
}

func TestAwaitResubmission_Terminal_Blocked(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventRejected,
		Agent: strPtr("reviewer-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusIdle,
	}
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	var result *AwaitResubmissionResult
	var awaitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, awaitErr = AwaitResubmission(context.Background(), tmpDir, "task-1", "reviewer-1", 10*time.Second)
	}()

	testhelpers.WaitForAsyncSetup()
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusBlocked
		reason := "Spec ambiguity"
		tk.BlockedReason = &reason
		return nil
	}); err != nil {
		t.Fatalf("failed to modify state: %v", err)
	}

	<-done
	if awaitErr != nil {
		t.Fatalf("AwaitResubmission error: %v", awaitErr)
	}
	if result.Verdict != ResubmissionTerminal {
		t.Errorf("Verdict = %q, want TERMINAL", result.Verdict)
	}
	if result.TaskStatus != models.TaskStatusBlocked {
		t.Errorf("TaskStatus = %q, want BLOCKED", result.TaskStatus)
	}

	// Verify ownership released.
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	tk := s.FindTask("task-1")
	if tk.ReviewingBy != nil {
		t.Error("ReviewingBy should be nil after terminal")
	}
	agent := s.Agents["reviewer-1"]
	if agent.CurrentTask != nil {
		t.Errorf("expected CurrentTask=nil after terminal, got %q", *agent.CurrentTask)
	}
}

func TestAwaitResubmission_Terminal_Superseded(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventRejected,
		Agent: strPtr("reviewer-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusIdle,
	}
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	var result *AwaitResubmissionResult
	var awaitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, awaitErr = AwaitResubmission(context.Background(), tmpDir, "task-1", "reviewer-1", 10*time.Second)
	}()

	testhelpers.WaitForAsyncSetup()
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusSuperseded
		return nil
	}); err != nil {
		t.Fatalf("failed to modify state: %v", err)
	}

	<-done
	if awaitErr != nil {
		t.Fatalf("AwaitResubmission error: %v", awaitErr)
	}
	if result.Verdict != ResubmissionTerminal {
		t.Errorf("Verdict = %q, want TERMINAL", result.Verdict)
	}
	if result.TaskStatus != models.TaskStatusSuperseded {
		t.Errorf("TaskStatus = %q, want SUPERSEDED", result.TaskStatus)
	}

	// Verify ownership released.
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	agent := s.Agents["reviewer-1"]
	if agent.CurrentTask != nil {
		t.Errorf("expected CurrentTask=nil after terminal, got %q", *agent.CurrentTask)
	}
}

func TestAwaitResubmission_Terminal_Approved(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventRejected,
		Agent: strPtr("reviewer-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusIdle,
	}
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	var result *AwaitResubmissionResult
	var awaitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, awaitErr = AwaitResubmission(context.Background(), tmpDir, "task-1", "reviewer-1", 10*time.Second)
	}()

	testhelpers.WaitForAsyncSetup()
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusApproved
		approver := "reviewer-2"
		tk.ApprovedBy = &approver
		return nil
	}); err != nil {
		t.Fatalf("failed to modify state: %v", err)
	}

	<-done
	if awaitErr != nil {
		t.Fatalf("AwaitResubmission error: %v", awaitErr)
	}
	if result.Verdict != ResubmissionTerminal {
		t.Errorf("Verdict = %q, want TERMINAL", result.Verdict)
	}
	if result.TaskStatus != models.TaskStatusApproved {
		t.Errorf("TaskStatus = %q, want CODE_APPROVED", result.TaskStatus)
	}

	// Verify ownership released.
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	agent := s.Agents["reviewer-1"]
	if agent.CurrentTask != nil {
		t.Errorf("expected CurrentTask=nil after terminal, got %q", *agent.CurrentTask)
	}
}

func TestAwaitResubmission_TaskDisappears(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventRejected,
		Agent: strPtr("reviewer-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusIdle,
	}
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	var result *AwaitResubmissionResult
	var awaitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, awaitErr = AwaitResubmission(context.Background(), tmpDir, "task-1", "reviewer-1", 10*time.Second)
	}()

	testhelpers.WaitForAsyncSetup()
	// Remove the task from state entirely.
	if err := bb.Modify(func(s *models.State) error {
		s.Tasks = []models.Task{}
		return nil
	}); err != nil {
		t.Fatalf("failed to modify state: %v", err)
	}

	<-done
	if awaitErr != nil {
		t.Fatalf("AwaitResubmission error: %v", awaitErr)
	}
	if result.Verdict != ResubmissionTerminal {
		t.Errorf("Verdict = %q, want TERMINAL", result.Verdict)
	}
	if result.Reason == "" {
		t.Error("expected non-empty Reason for task disappearance")
	}

	// Verify ownership released.
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	agent := s.Agents["reviewer-1"]
	if agent.CurrentTask != nil {
		t.Errorf("expected CurrentTask=nil after disappearance, got %q", *agent.CurrentTask)
	}
}

func TestAwaitResubmission_Timeout(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventRejected,
		Agent: strPtr("reviewer-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusIdle,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Very short timeout — task stays REJECTED, deadline fires.
	result, err := AwaitResubmission(context.Background(), tmpDir, "task-1", "reviewer-1", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("AwaitResubmission error: %v", err)
	}
	if result.Verdict != ResubmissionTimeout {
		t.Errorf("Verdict = %q, want TIMEOUT", result.Verdict)
	}

	// Verify ownership released.
	bb := db.For(stateFile)
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	tk := s.FindTask("task-1")
	if tk.ReviewingBy != nil {
		t.Error("ReviewingBy should be nil after timeout")
	}
	agent := s.Agents["reviewer-1"]
	if agent.CurrentTask != nil {
		t.Errorf("expected CurrentTask=nil after timeout, got %q", *agent.CurrentTask)
	}
}

func TestAwaitResubmission_Aborted(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventRejected,
		Agent: strPtr("reviewer-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusIdle,
	}
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	var result *AwaitResubmissionResult
	var awaitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, awaitErr = AwaitResubmission(context.Background(), tmpDir, "task-1", "reviewer-1", 10*time.Second)
	}()

	testhelpers.WaitForAsyncSetup()
	if err := bb.Modify(func(s *models.State) error {
		s.Config.Mode = models.SystemModeStopped
		return nil
	}); err != nil {
		t.Fatalf("failed to modify state: %v", err)
	}

	<-done
	if awaitErr != nil {
		t.Fatalf("AwaitResubmission error: %v", awaitErr)
	}
	if result.Verdict != ResubmissionAborted {
		t.Errorf("Verdict = %q, want ABORTED", result.Verdict)
	}

	// Verify ownership released.
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	tk := s.FindTask("task-1")
	if tk.ReviewingBy != nil {
		t.Error("ReviewingBy should be nil after abort")
	}
	agent := s.Agents["reviewer-1"]
	if agent.CurrentTask != nil {
		t.Errorf("expected CurrentTask=nil after abort, got %q", *agent.CurrentTask)
	}
}

// --- Edge case tests ---

func TestAwaitResubmission_EarlyResubmission(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	// Task is already SUBMITTED (CODE_READY_FOR_REVIEW) at entry — fast-doer edge case.
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
	// Need a rejection history entry from this agent to pass precondition.
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now.Add(-time.Minute),
		Event: models.TaskEventRejected,
		Agent: strPtr("reviewer-1"),
	})
	newCommit := "earlycommit789"
	task.ReviewCommit = &newCommit
	task.ReviewCyclesCurrent = 3
	state.Tasks = []models.Task{task}
	state.Agents["reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusIdle,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Should return immediately without entering the wait loop.
	result, err := AwaitResubmission(context.Background(), tmpDir, "task-1", "reviewer-1", 10*time.Second)
	if err != nil {
		t.Fatalf("AwaitResubmission error: %v", err)
	}
	if result.Verdict != ResubmissionResubmitted {
		t.Errorf("Verdict = %q, want RESUBMITTED", result.Verdict)
	}
	if result.TaskStatus != models.TaskStatusReviewing {
		t.Errorf("TaskStatus = %q, want REVIEWING_CODE", result.TaskStatus)
	}
	if result.ReviewCommit != newCommit {
		t.Errorf("ReviewCommit = %q, want %q", result.ReviewCommit, newCommit)
	}
	if result.ReviewCycle != 3 {
		t.Errorf("ReviewCycle = %d, want 3", result.ReviewCycle)
	}

	// Verify task reclaimed to REVIEWING with ownership.
	bb := db.For(stateFile)
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	tk := s.FindTask("task-1")
	if tk.Status != models.TaskStatusReviewing {
		t.Errorf("task status = %q, want REVIEWING_CODE", tk.Status)
	}
	if tk.ReviewingBy == nil || *tk.ReviewingBy != "reviewer-1" {
		t.Error("ReviewingBy should be reviewer-1 after early reclaim")
	}
}

func TestAwaitResubmission_RaceGuard(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	// Task is REJECTED with ReviewCommit set (needed for reviewer claimability).
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventRejected,
		Agent: strPtr("reviewer-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusIdle,
	}
	state.Agents["reviewer-2"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusIdle,
	}
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	var awaitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, awaitErr = AwaitResubmission(context.Background(), tmpDir, "task-1", "reviewer-1", 10*time.Second)
	}()

	testhelpers.WaitForAsyncSetup()

	// Verify ReviewingBy is now set (reviewer-1 acquired ownership).
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	tk := s.FindTask("task-1")
	if tk.ReviewingBy == nil || *tk.ReviewingBy != "reviewer-1" {
		t.Fatal("ReviewingBy should be reviewer-1 after ownership acquisition")
	}

	// Another reviewer should NOT be able to claim the task.
	// First transition to SUBMITTED so the task would normally be claimable by a reviewer.
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusReadyForReview
		rc := "newcommit"
		tk.ReviewCommit = &rc
		// ReviewingBy stays set from acquireReviewOwnership.
		return nil
	}); err != nil {
		t.Fatalf("failed to modify state: %v", err)
	}

	// reviewer-2 attempts to claim — should fail because ReviewingBy is set.
	_, claimErr := ClaimTask(tmpDir, "task-1", "reviewer-2")
	if claimErr == nil {
		t.Fatal("expected ClaimTask by reviewer-2 to fail while ReviewingBy is set")
	}

	<-done
	if awaitErr != nil {
		t.Fatalf("AwaitResubmission error: %v", awaitErr)
	}

	// Verify reviewer-1 owns the task (reclaimed via RESUBMITTED path).
	s, readErr = bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	tk = s.FindTask("task-1")
	if tk.ReviewingBy == nil || *tk.ReviewingBy != "reviewer-1" {
		t.Error("ReviewingBy should be reviewer-1 after reclaim")
	}
	agent2 := s.Agents["reviewer-2"]
	if agent2.CurrentTask != nil && *agent2.CurrentTask == "task-1" {
		t.Error("reviewer-2 should not have acquired the task")
	}
}

func TestAwaitResubmission_ReviewLeaseExpires(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventRejected,
		Agent: strPtr("reviewer-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusIdle,
	}
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	timeout := 10 * time.Second
	var result *AwaitResubmissionResult
	var awaitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, awaitErr = AwaitResubmission(context.Background(), tmpDir, "task-1", "reviewer-1", timeout)
	}()

	testhelpers.WaitForAsyncSetup()

	// Verify initial lease: should be approximately now + timeout + 5min.
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	tk := s.FindTask("task-1")
	if tk.ReviewLeaseExpires == nil {
		t.Fatal("ReviewLeaseExpires should be set on entry")
	}
	expectedEntryLease := now.Add(timeout + 5*time.Minute)
	entryLeaseDiff := tk.ReviewLeaseExpires.Sub(expectedEntryLease)
	if entryLeaseDiff < -5*time.Second || entryLeaseDiff > 5*time.Second {
		t.Errorf("entry ReviewLeaseExpires = %v, want ~%v (diff=%v)", tk.ReviewLeaseExpires, expectedEntryLease, entryLeaseDiff)
	}

	// Trigger resubmission to verify refreshed lease.
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusReadyForReview
		rc := "refreshcommit"
		tk.ReviewCommit = &rc
		return nil
	}); err != nil {
		t.Fatalf("failed to modify state: %v", err)
	}

	<-done
	if awaitErr != nil {
		t.Fatalf("AwaitResubmission error: %v", awaitErr)
	}
	if result.Verdict != ResubmissionResubmitted {
		t.Fatalf("Verdict = %q, want RESUBMITTED", result.Verdict)
	}

	// Verify refreshed lease: should be approximately now + 30min.
	s, readErr = bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	tk = s.FindTask("task-1")
	if tk.ReviewLeaseExpires == nil {
		t.Fatal("ReviewLeaseExpires should be set after reclaim")
	}
	expectedReclaimLease := time.Now().Add(30 * time.Minute)
	reclaimLeaseDiff := tk.ReviewLeaseExpires.Sub(expectedReclaimLease)
	if reclaimLeaseDiff < -5*time.Second || reclaimLeaseDiff > 5*time.Second {
		t.Errorf("reclaim ReviewLeaseExpires = %v, want ~%v (diff=%v)", tk.ReviewLeaseExpires, expectedReclaimLease, reclaimLeaseDiff)
	}
}
