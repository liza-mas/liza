package ops

import (
	"context"
	stderrors "errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
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
	// IMPLEMENTING with no rejection history — reviewer validation rejects first.
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	state.Agents["reviewer-1"] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusIdle}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AwaitResubmission(context.Background(), tmpDir, "task-1", "reviewer-1", 30*time.Second)
	if err == nil {
		t.Fatal("expected error for wrong status")
	}

	var pe *PreconditionError
	if !stderrors.As(err, &pe) {
		t.Fatalf("expected PreconditionError, got %T: %v", err, err)
	}
	testhelpers.RequireErrorContains(t, err, "no rejection history")
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
	state.Agents["reviewer-2"] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusIdle}
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

	wait := startResubmissionWait(t, tmpDir, 10*time.Second)

	waitForReviewOwnership(t, bb, "task-1", "reviewer-1", wait, 10*time.Second)
	// Simulate doer resubmission: transition to CODE_READY_FOR_REVIEW.
	newBase := "newbase123"
	newCommit := "newcommit456"
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusReadyForReview
		tk.BaseCommit = &newBase
		tk.ReviewCommit = &newCommit
		tk.Worktree = nil
		tk.ReviewCyclesCurrent = 2
		return nil
	}); err != nil {
		t.Fatalf("failed to modify state: %v", err)
	}

	result, awaitErr := wait.finish()
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
	if result.BaseCommit != newBase {
		t.Errorf("BaseCommit = %q, want %q", result.BaseCommit, newBase)
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

	wait := startResubmissionWait(t, tmpDir, 10*time.Second)

	waitForReviewOwnership(t, bb, "task-1", "reviewer-1", wait, 10*time.Second)
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusBlocked
		reason := "Spec ambiguity"
		tk.BlockedReason = &reason
		return nil
	}); err != nil {
		t.Fatalf("failed to modify state: %v", err)
	}

	result, awaitErr := wait.finish()
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

	wait := startResubmissionWait(t, tmpDir, 10*time.Second)

	waitForReviewOwnership(t, bb, "task-1", "reviewer-1", wait, 10*time.Second)
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusSuperseded
		return nil
	}); err != nil {
		t.Fatalf("failed to modify state: %v", err)
	}

	result, awaitErr := wait.finish()
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

	wait := startResubmissionWait(t, tmpDir, 10*time.Second)

	waitForReviewOwnership(t, bb, "task-1", "reviewer-1", wait, 10*time.Second)
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusApproved
		approver := "reviewer-2"
		tk.ApprovedBy = &approver
		return nil
	}); err != nil {
		t.Fatalf("failed to modify state: %v", err)
	}

	result, awaitErr := wait.finish()
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

	wait := startResubmissionWait(t, tmpDir, 10*time.Second)

	waitForReviewOwnership(t, bb, "task-1", "reviewer-1", wait, 10*time.Second)
	// Remove the task from state entirely.
	if err := bb.Modify(func(s *models.State) error {
		s.Tasks = []models.Task{}
		return nil
	}); err != nil {
		t.Fatalf("failed to modify state: %v", err)
	}

	result, awaitErr := wait.finish()
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

func TestAwaitResubmission_DelayedWatcherErrorUsesOriginalDeadline(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.SpecRef = state.Goal.SpecRef
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventRejected,
		Agent: strPtr("reviewer-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["reviewer-1"] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusIdle}
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	previousWatcher := newAwaitResubmissionWatcher
	newAwaitResubmissionWatcher = func(*db.Blackboard) (awaitResubmissionWatcher, error) {
		return newDelayedErrorWatcher(300 * time.Millisecond), nil
	}
	t.Cleanup(func() { newAwaitResubmissionWatcher = previousWatcher })

	started := time.Now()
	result, err := AwaitResubmission(context.Background(), tmpDir, "task-1", "reviewer-1", 500*time.Millisecond)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("AwaitResubmission error: %v", err)
	}
	if result.Verdict != ResubmissionTimeout {
		t.Errorf("Verdict = %q, want TIMEOUT", result.Verdict)
	}
	if elapsed < 400*time.Millisecond {
		t.Errorf("returned before original deadline: elapsed = %s", elapsed)
	}
	if elapsed >= 800*time.Millisecond {
		t.Errorf("returned after original deadline tolerance: elapsed = %s, want < 800ms", elapsed)
	}

	readState, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	readTask := readState.FindTask("task-1")
	if readTask == nil {
		t.Fatal("task-1 not found")
	}
	if readTask.ReviewingBy != nil {
		t.Error("review ownership should be released after timeout")
	}
	if readState.Agents["reviewer-1"].CurrentTask != nil {
		t.Error("reviewer ownership should be released after timeout")
	}
	if err := statevalidate.ValidateState(readState, tmpDir, false, io.Discard); err != nil {
		t.Fatalf("state should validate after timeout: %v", err)
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

	wait := startResubmissionWait(t, tmpDir, 10*time.Second)

	waitForReviewOwnership(t, bb, "task-1", "reviewer-1", wait, 10*time.Second)
	if err := bb.Modify(func(s *models.State) error {
		s.Config.Mode = models.SystemModeStopped
		return nil
	}); err != nil {
		t.Fatalf("failed to modify state: %v", err)
	}

	result, awaitErr := wait.finish()
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

// --- Early exit tests (task already BLOCKED/terminal at entry) ---

func TestAwaitResubmission_AlreadyBlocked_RejectingReviewer(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
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

	result, err := AwaitResubmission(context.Background(), tmpDir, "task-1", "reviewer-1", 10*time.Second)
	if err != nil {
		t.Fatalf("AwaitResubmission error: %v", err)
	}
	if result.Verdict != ResubmissionAborted {
		t.Errorf("Verdict = %q, want ABORTED", result.Verdict)
	}
	if result.TaskStatus != models.TaskStatusBlocked {
		t.Errorf("TaskStatus = %q, want BLOCKED", result.TaskStatus)
	}

	// Verify no ownership mutation occurred.
	bb := db.For(stateFile)
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	tk := s.FindTask("task-1")
	if tk.ReviewingBy != nil {
		t.Error("ReviewingBy should remain nil (no ownership acquired)")
	}
	agent := s.Agents["reviewer-1"]
	if agent.CurrentTask != nil {
		t.Error("CurrentTask should remain nil (no ownership acquired)")
	}
}

func TestAwaitResubmission_AlreadyTerminal_RejectingReviewer(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusSuperseded, now)
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

	result, err := AwaitResubmission(context.Background(), tmpDir, "task-1", "reviewer-1", 10*time.Second)
	if err != nil {
		t.Fatalf("AwaitResubmission error: %v", err)
	}
	if result.Verdict != ResubmissionAborted {
		t.Errorf("Verdict = %q, want ABORTED", result.Verdict)
	}
	if result.TaskStatus != models.TaskStatusSuperseded {
		t.Errorf("TaskStatus = %q, want SUPERSEDED", result.TaskStatus)
	}
}

func TestAwaitResubmission_AlreadyBlocked_WrongReviewer(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventRejected,
		Agent: strPtr("reviewer-1"),
	})
	state.Tasks = []models.Task{task}
	state.Agents["reviewer-2"] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusIdle}
	testhelpers.WriteInitialState(t, stateFile, state)

	// reviewer-2 was NOT the rejecting reviewer — should still get precondition error.
	_, err := AwaitResubmission(context.Background(), tmpDir, "task-1", "reviewer-2", 10*time.Second)
	if err == nil {
		t.Fatal("expected error for wrong reviewer on already-blocked task")
	}
	var pe *PreconditionError
	if !stderrors.As(err, &pe) {
		t.Fatalf("expected PreconditionError, got %T: %v", err, err)
	}
	testhelpers.RequireErrorContains(t, err, "not the last rejecting reviewer")
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
	newBase := "earlybase456"
	newCommit := "earlycommit789"
	task.BaseCommit = &newBase
	task.ReviewCommit = &newCommit
	task.Worktree = nil
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
	if result.BaseCommit != newBase {
		t.Errorf("BaseCommit = %q, want %q", result.BaseCommit, newBase)
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

func TestAwaitResubmission_RejectsReviewCommitMismatchOnReclaim(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	testhelpers.MustGit(t, tmpDir, "checkout", "integration")
	g := git.New(tmpDir)
	taskID := "task-resubmit-stale-review"
	baseCommit, err := g.CreateWorktree(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wtPath := g.GetWorktreePath(taskID)
	if err := os.WriteFile(filepath.Join(wtPath, "resubmitted.txt"), []byte("resubmitted work\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "resubmitted.txt")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Resubmit work")
	wtHead := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")
	if baseCommit == wtHead {
		t.Fatal("test setup failed: base commit matches worktree HEAD")
	}

	now := time.Now().UTC()
	worktree := g.GetWorktreeRelPath(taskID)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		{
			ID:                  taskID,
			Status:              models.TaskStatusReadyForReview,
			RolePair:            "coding-pair",
			Worktree:            &worktree,
			ReviewCommit:        &baseCommit,
			ReviewCyclesCurrent: 2,
			History: []models.TaskHistoryEntry{
				{
					Time:  now.Add(-time.Minute),
					Event: models.TaskEventRejected,
					Agent: strPtr("reviewer-1"),
				},
			},
			Created: now,
		},
	}
	state.Agents["reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusIdle,
	}
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	_, err = AwaitResubmission(context.Background(), tmpDir, taskID, "reviewer-1", 10*time.Second)
	if err == nil {
		t.Fatal("Expected review boundary repair error")
	}
	if !strings.Contains(err.Error(), "update-review-commit") {
		t.Errorf("Error = %q, want update-review-commit recovery hint", err.Error())
	}

	readState, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	task := readState.FindTask(taskID)
	if task == nil {
		t.Fatal("task not found")
	}
	if task.Status != models.TaskStatusReadyForReview {
		t.Errorf("task status = %q, want READY_FOR_REVIEW", task.Status)
	}
	if task.ReviewingBy != nil {
		t.Fatal("ReviewingBy should be released after repairable boundary drift")
	}
	if task.IntegrationFailure != nil {
		t.Fatal("IntegrationFailure should not be recorded for repairable boundary drift")
	}
	agent := readState.Agents["reviewer-1"]
	if agent.CurrentTask != nil {
		t.Fatalf("reviewer CurrentTask = %v, want nil", *agent.CurrentTask)
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

	wait := startResubmissionWait(t, tmpDir, 10*time.Second)

	waitForReviewOwnership(t, bb, "task-1", "reviewer-1", wait, 10*time.Second)

	// Another reviewer should NOT be able to claim the task.
	// First transition to SUBMITTED so the task would normally be claimable by a reviewer.
	if err := bb.Modify(func(s *models.State) error {
		tk := s.FindTask("task-1")
		tk.Status = models.TaskStatusReadyForReview
		rc := "newcommit"
		tk.ReviewCommit = &rc
		tk.Worktree = nil
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

	_, awaitErr := wait.finish()
	if awaitErr != nil {
		t.Fatalf("AwaitResubmission error: %v", awaitErr)
	}

	// Verify reviewer-1 owns the task (reclaimed via RESUBMITTED path).
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("failed to read state: %v", readErr)
	}
	tk := s.FindTask("task-1")
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
	wait := startResubmissionWait(t, tmpDir, timeout)

	// Verify initial lease: should be approximately now + timeout + 5min.
	tk := waitForReviewOwnership(t, bb, "task-1", "reviewer-1", wait, 2*time.Second)
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
		tk.Worktree = nil
		return nil
	}); err != nil {
		t.Fatalf("failed to modify state: %v", err)
	}

	result, awaitErr := wait.finish()
	if awaitErr != nil {
		t.Fatalf("AwaitResubmission error: %v", awaitErr)
	}
	if result.Verdict != ResubmissionResubmitted {
		t.Fatalf("Verdict = %q, want RESUBMITTED", result.Verdict)
	}

	// Verify refreshed lease: should be approximately now + 30min.
	s, readErr := bb.Read()
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

type resubmissionWait struct {
	done   chan struct{}
	result *AwaitResubmissionResult
	err    error
}

// All async fixtures in this file use task-1 and reviewer-1. Register cleanup
// before launching so Fatal/FailNow cannot let a worker outlive its fixture.
func startResubmissionWait(t *testing.T, root string, timeout time.Duration) *resubmissionWait {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	wait := &resubmissionWait{done: make(chan struct{})}
	t.Cleanup(func() {
		cancel()
		<-wait.done
	})
	go func() {
		defer close(wait.done)
		wait.result, wait.err = AwaitResubmission(ctx, root, "task-1", "reviewer-1", timeout)
	}()
	return wait
}

func (wait *resubmissionWait) finish() (*AwaitResubmissionResult, error) {
	<-wait.done
	return wait.result, wait.err
}

func waitForReviewOwnership(t *testing.T, bb *db.Blackboard, taskID, reviewerID string, wait *resubmissionWait, timeout time.Duration) *models.Task {
	t.Helper()

	var done <-chan struct{}
	if wait != nil {
		done = wait.done
	}
	deadline := time.Now().Add(timeout)
	pollTicker := time.NewTicker(10 * time.Millisecond)
	defer pollTicker.Stop()
	for time.Now().Before(deadline) {
		// Read takes the worker's exclusive file lock. Frequent readiness reads
		// can obstruct acquisition under load. Atomic state-file replacement
		// lets ReadCached observe committed ownership without taking that lock.
		state, err := bb.ReadCached()
		if err != nil {
			t.Fatalf("failed to read state while waiting for review ownership: %v", err)
		}
		task := state.FindTask(taskID)
		agent := state.Agents[reviewerID]
		if task != nil && task.ReviewingBy != nil && *task.ReviewingBy == reviewerID &&
			task.ReviewLeaseExpires != nil && agent.Status == models.AgentStatusWaiting &&
			agent.CurrentTask != nil && *agent.CurrentTask == taskID {
			return task
		}
		select {
		case <-done:
			t.Fatalf("await worker exited before review ownership: result=%+v error=%v", wait.result, wait.err)
		case <-pollTicker.C:
		}
	}

	t.Fatalf("timed out waiting for %s to acquire review ownership of %s", reviewerID, taskID)
	return nil
}
