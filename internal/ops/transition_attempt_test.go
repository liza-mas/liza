package ops

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// setupTransitionTest creates a test environment with a REJECTED task at attempt 1.
// Returns tmpDir, statePath, and the taskID.
func setupTransitionTest(t *testing.T) (string, string) {
	t.Helper()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.Attempt = 1
	task.ReviewCyclesCurrent = 5
	task.ReviewCyclesTotal = 5
	baseCommit := "abc1234"
	task.BaseCommit = &baseCommit
	approvedBy := "code-reviewer-1"
	mergeCommit := "stale-merge"
	task.ApprovedBy = &approvedBy
	task.Approvals = []models.Approval{{Agent: approvedBy, Provider: "codex", Timestamp: now}}
	task.MergeCommit = &mergeCommit
	task.FailedBy = []string{"coder-1"}
	task.IntegrationFailure = map[string]any{"detail": "stale"}
	task.Output = []models.OutputEntry{{Desc: "stale", DoneWhen: "done", Scope: "scope", SpecRef: "README.md"}}

	state.Tasks = []models.Task{task}

	// Register the assigned agent.
	state.Agents["coder-1"] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: testhelpers.StringPtr("task-1"),
		Heartbeat:   now,
		Terminal:    "test",
	}

	testhelpers.WriteInitialState(t, statePath, state)

	return tmpDir, statePath
}

func TestTransitionToNewAttempt_Success(t *testing.T) {
	tmpDir, statePath := setupTransitionTest(t)

	result, err := TransitionToNewAttempt(tmpDir, "task-1", "review cycle limit reached")
	if err != nil {
		t.Fatalf("TransitionToNewAttempt() error: %v", err)
	}

	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}
	if result.NewAttempt != 2 {
		t.Errorf("NewAttempt = %d, want 2", result.NewAttempt)
	}
	if result.InitialStatus != models.TaskStatusReady {
		t.Errorf("InitialStatus = %q, want %q", result.InitialStatus, models.TaskStatusReady)
	}

	// Verify final state.
	bb := db.New(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}

	task := state.FindTask("task-1")
	if task == nil {
		t.Fatal("task not found")
	}

	if task.Attempt != 2 {
		t.Errorf("Attempt = %d, want 2", task.Attempt)
	}
	if task.Iteration != 0 {
		t.Errorf("Iteration = %d, want 0", task.Iteration)
	}
	if task.ReviewCyclesCurrent != 0 {
		t.Errorf("ReviewCyclesCurrent = %d, want 0", task.ReviewCyclesCurrent)
	}
	if task.AssignedTo != nil {
		t.Errorf("AssignedTo = %v, want nil", *task.AssignedTo)
	}
	if task.Worktree != nil {
		t.Errorf("Worktree = %v, want nil", *task.Worktree)
	}
	if task.BaseCommit != nil {
		t.Errorf("BaseCommit = %v, want nil", *task.BaseCommit)
	}
	if task.RejectionReason != nil {
		t.Errorf("RejectionReason = %v, want nil", *task.RejectionReason)
	}
	if task.ReviewCommit != nil {
		t.Errorf("ReviewCommit = %v, want nil", *task.ReviewCommit)
	}
	if len(task.Output) != 0 {
		t.Errorf("Output = %v, want cleared", task.Output)
	}
	if task.ApprovedBy != nil {
		t.Errorf("ApprovedBy = %v, want nil", *task.ApprovedBy)
	}
	if len(task.Approvals) != 0 {
		t.Errorf("Approvals = %v, want cleared", task.Approvals)
	}
	if task.MergeCommit != nil {
		t.Errorf("MergeCommit = %v, want nil", *task.MergeCommit)
	}
	if task.IntegrationFailure != nil {
		t.Errorf("IntegrationFailure = %v, want nil", task.IntegrationFailure)
	}
	if len(task.FailedBy) != 0 {
		t.Errorf("FailedBy = %v, want cleared", task.FailedBy)
	}
	if task.Status != models.TaskStatusReady {
		t.Errorf("Status = %q, want %q", task.Status, models.TaskStatusReady)
	}

	// Verify history contains new_attempt event with rejection feedback in Note.
	found := false
	for _, h := range task.History {
		if h.Event == models.TaskEventNewAttempt {
			found = true
			if h.Reason == nil || *h.Reason != "review cycle limit reached" {
				t.Errorf("new_attempt reason = %v, want %q", h.Reason, "review cycle limit reached")
			}
			// Note should carry the rejection feedback from prior attempt.
			if h.Note == nil || *h.Note != "Needs improvement" {
				t.Errorf("new_attempt Note = %v, want %q", h.Note, "Needs improvement")
			}
			break
		}
	}
	if !found {
		t.Error("history missing new_attempt event")
	}

	// Verify agent released.
	agent, ok := state.Agents["coder-1"]
	if !ok {
		t.Fatal("agent coder-1 not found")
	}
	if agent.Status != models.AgentStatusIdle {
		t.Errorf("agent status = %q, want IDLE", agent.Status)
	}
	if agent.CurrentTask != nil {
		t.Errorf("agent current_task = %v, want nil", *agent.CurrentTask)
	}
}

func TestTransitionToNewAttempt_Attempt2Rejected(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.Attempt = 2
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, statePath, state)

	_, err := TransitionToNewAttempt(tmpDir, "task-1", "cap reached")
	if err == nil {
		t.Fatal("expected PreconditionError, got nil")
	}
	testhelpers.RequireErrorContains(t, err, "only attempt 1 can transition")
}

func TestTransitionToNewAttempt_Attempt0DefaultsTo1(t *testing.T) {
	tmpDir, statePath := setupTransitionTest(t)

	// Override: set Attempt=0 (legacy/unset).
	bb := db.New(statePath)
	err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("task-1")
		task.Attempt = 0
		return nil
	})
	if err != nil {
		t.Fatalf("failed to set Attempt=0: %v", err)
	}

	result, err := TransitionToNewAttempt(tmpDir, "task-1", "cap reached")
	if err != nil {
		t.Fatalf("TransitionToNewAttempt() error: %v", err)
	}
	if result.NewAttempt != 2 {
		t.Errorf("NewAttempt = %d, want 2", result.NewAttempt)
	}

	state, _ := bb.Read()
	task := state.FindTask("task-1")
	if task.Attempt != 2 {
		t.Errorf("Attempt = %d, want 2", task.Attempt)
	}
}

func TestTransitionToNewAttempt_WorktreeDeletionFailure(t *testing.T) {
	tmpDir, statePath := setupTransitionTest(t)

	// Create a real worktree so Phase 2 has something to delete.
	testhelpers.CreateTestWorktree(t, tmpDir, "task-1")

	// Update state to point to the real worktree.
	bb := db.New(statePath)
	err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("task-1")
		wt := ".worktrees/task-1"
		task.Worktree = &wt
		return nil
	})
	if err != nil {
		t.Fatalf("failed to update worktree: %v", err)
	}

	// Make the worktree non-removable: lock a subdirectory so both
	// "git worktree remove --force" and os.RemoveAll fail. PreventRemoval
	// restores removability on cleanup so t.TempDir() can remove everything.
	worktreeDir := filepath.Join(tmpDir, ".worktrees", "task-1")
	lockedDir := filepath.Join(worktreeDir, "locked")
	if err := os.MkdirAll(lockedDir, 0755); err != nil {
		t.Fatalf("failed to create locked dir: %v", err)
	}
	testhelpers.PreventRemoval(t, lockedDir)

	result, err := TransitionToNewAttempt(tmpDir, "task-1", "review cycle limit reached")
	if err != nil {
		t.Fatalf("TransitionToNewAttempt() error: %v", err)
	}

	// Phase 2 should have failed (worktreeDeleted=false).
	if result.WorktreeDeleted {
		t.Error("WorktreeDeleted = true, want false (deletion should have failed)")
	}

	// Phase 3 should have still completed successfully.
	state, _ := bb.Read()
	task := state.FindTask("task-1")
	if task.Status != models.TaskStatusReady {
		t.Errorf("Status = %q, want %q (Phase 3 should complete despite Phase 2 failure)", task.Status, models.TaskStatusReady)
	}
	if task.AssignedTo != nil {
		t.Errorf("AssignedTo = %v, want nil", *task.AssignedTo)
	}
	if task.RejectionReason != nil {
		t.Errorf("RejectionReason = %v, want nil", *task.RejectionReason)
	}
	if task.Worktree != nil {
		t.Errorf("Worktree = %v, want nil (cleared in Phase 3 even if Phase 2 fails)", *task.Worktree)
	}
	if task.BaseCommit != nil {
		t.Errorf("BaseCommit = %v, want nil", *task.BaseCommit)
	}
}

func TestTransitionToNewAttempt_SentinelReplacedByPhase3(t *testing.T) {
	tmpDir, statePath := setupTransitionTest(t)

	bb := db.New(statePath)

	// Use afterPhase1 hook to simulate concurrent modification.
	testTransitionHooks = &transitionTestHooks{
		afterPhase1: func() {
			err := bb.Modify(func(state *models.State) error {
				task := state.FindTask("task-1")
				intruder := "coder-99"
				task.AssignedTo = &intruder
				return nil
			})
			if err != nil {
				t.Errorf("failed to simulate concurrent modification: %v", err)
			}
		},
	}
	t.Cleanup(func() { testTransitionHooks = nil })

	_, err := TransitionToNewAttempt(tmpDir, "task-1", "cap reached")
	if err == nil {
		t.Fatal("expected error from sentinel check, got nil")
	}
	testhelpers.RequireErrorContains(t, err, "sentinel replaced")
}

func TestTransitionToNewAttempt_ReviewCyclesTotalPreserved(t *testing.T) {
	tmpDir, statePath := setupTransitionTest(t)

	_, err := TransitionToNewAttempt(tmpDir, "task-1", "cap reached")
	if err != nil {
		t.Fatalf("TransitionToNewAttempt() error: %v", err)
	}

	bb := db.New(statePath)
	state, _ := bb.Read()
	task := state.FindTask("task-1")

	// ReviewCyclesTotal from setup is 5 — must be preserved.
	if task.ReviewCyclesTotal != 5 {
		t.Errorf("ReviewCyclesTotal = %d, want 5 (should not be reset)", task.ReviewCyclesTotal)
	}
}

func TestTransitionToNewAttempt_AgentRelease(t *testing.T) {
	tmpDir, statePath := setupTransitionTest(t)

	_, err := TransitionToNewAttempt(tmpDir, "task-1", "cap reached")
	if err != nil {
		t.Fatalf("TransitionToNewAttempt() error: %v", err)
	}

	bb := db.New(statePath)
	state, _ := bb.Read()

	agent, ok := state.Agents["coder-1"]
	if !ok {
		t.Fatal("agent coder-1 not found in state")
	}
	if agent.Status != models.AgentStatusIdle {
		t.Errorf("agent status = %q, want IDLE", agent.Status)
	}
	if agent.CurrentTask != nil {
		t.Errorf("agent current_task = %v, want nil", *agent.CurrentTask)
	}
}

func TestTransitionToNewAttempt_SentinelSetDuringTransition(t *testing.T) {
	tmpDir, statePath := setupTransitionTest(t)

	bb := db.New(statePath)
	var sentinelObserved string

	testTransitionHooks = &transitionTestHooks{
		afterPhase1: func() {
			state, err := bb.Read()
			if err != nil {
				t.Errorf("failed to read state in hook: %v", err)
				return
			}
			task := state.FindTask("task-1")
			if task.AssignedTo != nil {
				sentinelObserved = *task.AssignedTo
			}
		},
	}
	t.Cleanup(func() { testTransitionHooks = nil })

	_, err := TransitionToNewAttempt(tmpDir, "task-1", "cap reached")
	if err != nil {
		t.Fatalf("TransitionToNewAttempt() error: %v", err)
	}

	if sentinelObserved != transitioning {
		t.Errorf("sentinel after Phase 1 = %q, want %q", sentinelObserved, transitioning)
	}
}

func TestTransitionToNewAttempt_Phase1PreservesFieldInvariants(t *testing.T) {
	tmpDir, statePath := setupTransitionTest(t)

	bb := db.New(statePath)

	testTransitionHooks = &transitionTestHooks{
		afterPhase1: func() {
			state, err := bb.Read()
			if err != nil {
				t.Errorf("failed to read state in hook: %v", err)
				return
			}
			task := state.FindTask("task-1")

			// RejectionReason must be preserved (not nil).
			if task.RejectionReason == nil {
				t.Error("Phase 1: RejectionReason is nil, should be preserved")
			}

			// Status must be unchanged (REJECTED).
			if task.Status != models.TaskStatusRejected {
				t.Errorf("Phase 1: Status = %q, want %q (preserved)", task.Status, models.TaskStatusRejected)
			}

			// Worktree must be preserved (not nil).
			if task.Worktree == nil {
				t.Error("Phase 1: Worktree is nil, should be preserved")
			}

			// BaseCommit must be preserved (not nil).
			if task.BaseCommit == nil {
				t.Error("Phase 1: BaseCommit is nil, should be preserved")
			}

			// LeaseExpires must be cleared.
			if task.LeaseExpires != nil {
				t.Error("Phase 1: LeaseExpires should be nil (cleared)")
			}
		},
	}
	t.Cleanup(func() { testTransitionHooks = nil })

	_, err := TransitionToNewAttempt(tmpDir, "task-1", "cap reached")
	if err != nil {
		t.Fatalf("TransitionToNewAttempt() error: %v", err)
	}
}

func TestTransitionToNewAttempt_RejectionReasonOnlyClearedInPhase3(t *testing.T) {
	tmpDir, statePath := setupTransitionTest(t)

	bb := db.New(statePath)
	var rejectionAfterPhase1 *string

	testTransitionHooks = &transitionTestHooks{
		afterPhase1: func() {
			state, err := bb.Read()
			if err != nil {
				t.Errorf("failed to read state in hook: %v", err)
				return
			}
			task := state.FindTask("task-1")
			rejectionAfterPhase1 = task.RejectionReason
		},
	}
	t.Cleanup(func() { testTransitionHooks = nil })

	_, err := TransitionToNewAttempt(tmpDir, "task-1", "cap reached")
	if err != nil {
		t.Fatalf("TransitionToNewAttempt() error: %v", err)
	}

	// RejectionReason should have been set after Phase 1.
	if rejectionAfterPhase1 == nil {
		t.Error("RejectionReason after Phase 1 is nil, should be preserved")
	}

	// RejectionReason should be nil after Phase 3 (final state).
	state, _ := bb.Read()
	task := state.FindTask("task-1")
	if task.RejectionReason != nil {
		t.Errorf("RejectionReason after Phase 3 = %v, want nil", *task.RejectionReason)
	}
}

func TestTransitionToNewAttempt_PreservesRejectionFeedbackInHistoryNote(t *testing.T) {
	tmpDir, statePath := setupTransitionTest(t)

	_, err := TransitionToNewAttempt(tmpDir, "task-1", "review cycle limit reached")
	if err != nil {
		t.Fatalf("TransitionToNewAttempt() error: %v", err)
	}

	bb := db.New(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}

	task := state.FindTask("task-1")
	if task == nil {
		t.Fatal("task not found")
	}

	// Find the new_attempt history entry.
	var entry *models.TaskHistoryEntry
	for i := range task.History {
		if task.History[i].Event == models.TaskEventNewAttempt {
			entry = &task.History[i]
			break
		}
	}
	if entry == nil {
		t.Fatal("history missing new_attempt event")
	}

	// Note must equal the task's RejectionReason prior to transition ("Needs improvement").
	if entry.Note == nil {
		t.Fatal("new_attempt Note is nil, want rejection feedback")
	}
	if *entry.Note != "Needs improvement" {
		t.Errorf("new_attempt Note = %q, want %q", *entry.Note, "Needs improvement")
	}

	// Reason carries the escalation reason, not the rejection feedback.
	if entry.Reason == nil || *entry.Reason != "review cycle limit reached" {
		t.Errorf("new_attempt Reason = %v, want %q", entry.Reason, "review cycle limit reached")
	}
}

func TestTransitionToNewAttempt_ReviewerReleased(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.Attempt = 1
	task.ReviewCyclesCurrent = 5
	task.ReviewCyclesTotal = 5
	reviewCommit := "rev123"
	task.ReviewCommit = &reviewCommit
	reviewerID := "code-reviewer-1"
	task.ReviewingBy = &reviewerID
	leaseExp := now.Add(30 * time.Minute)
	task.ReviewLeaseExpires = &leaseExp

	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: testhelpers.StringPtr("task-1"),
		Heartbeat:   now,
		Terminal:    "test",
	}
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:        "code-reviewer",
		Status:      models.AgentStatusWaiting,
		CurrentTask: testhelpers.StringPtr("task-1"),
		Heartbeat:   now,
		Terminal:    "test",
	}
	testhelpers.WriteInitialState(t, statePath, state)

	_, err := TransitionToNewAttempt(tmpDir, "task-1", "review cycle limit")
	if err != nil {
		t.Fatalf("TransitionToNewAttempt() error: %v", err)
	}

	bb := db.New(statePath)
	s, _ := bb.Read()

	// Task review fields must be cleared.
	tk := s.FindTask("task-1")
	if tk.ReviewCommit != nil {
		t.Errorf("ReviewCommit = %v, want nil", *tk.ReviewCommit)
	}
	if tk.ReviewingBy != nil {
		t.Errorf("ReviewingBy = %v, want nil", *tk.ReviewingBy)
	}
	if tk.ReviewLeaseExpires != nil {
		t.Errorf("ReviewLeaseExpires = %v, want nil", tk.ReviewLeaseExpires)
	}

	// Reviewer agent must be released.
	reviewer := s.Agents["code-reviewer-1"]
	if reviewer.Status != models.AgentStatusIdle {
		t.Errorf("reviewer status = %q, want IDLE", reviewer.Status)
	}
	if reviewer.CurrentTask != nil {
		t.Errorf("reviewer CurrentTask = %v, want nil", *reviewer.CurrentTask)
	}
}

func TestTransitionToNewAttempt_ReviewerNotReleasedOnPhase3Failure(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.Attempt = 1
	task.ReviewCyclesCurrent = 5
	task.ReviewCyclesTotal = 5
	reviewerID := "code-reviewer-1"
	task.ReviewingBy = &reviewerID

	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: testhelpers.StringPtr("task-1"),
		Heartbeat:   now,
		Terminal:    "test",
	}
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:        "code-reviewer",
		Status:      models.AgentStatusWaiting,
		CurrentTask: testhelpers.StringPtr("task-1"),
		Heartbeat:   now,
		Terminal:    "test",
	}
	testhelpers.WriteInitialState(t, statePath, state)

	// Sabotage Phase 3: replace sentinel between phases so Phase 3 fails.
	bb := db.New(statePath)
	testTransitionHooks = &transitionTestHooks{
		afterPhase1: func() {
			_ = bb.Modify(func(s *models.State) error {
				tk := s.FindTask("task-1")
				intruder := "intruder"
				tk.AssignedTo = &intruder
				return nil
			})
		},
	}
	t.Cleanup(func() { testTransitionHooks = nil })

	_, err := TransitionToNewAttempt(tmpDir, "task-1", "review cycle limit")
	if err == nil {
		t.Fatal("expected Phase 3 failure due to sentinel replacement")
	}

	// Reviewer should still be WAITING (not released) since Phase 3 didn't execute.
	s, _ := bb.Read()
	reviewer := s.Agents["code-reviewer-1"]
	if reviewer.Status != models.AgentStatusWaiting {
		t.Errorf("reviewer status = %q, want WAITING (Phase 3 didn't run)", reviewer.Status)
	}
	if reviewer.CurrentTask == nil || *reviewer.CurrentTask != "task-1" {
		t.Error("reviewer CurrentTask should still be task-1 (Phase 3 didn't run)")
	}
}
