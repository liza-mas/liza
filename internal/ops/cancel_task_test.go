package ops

import (
	"bytes"
	stderrors "errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestCancelTask_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		taskID  string
		reason  string
		agentID string
		wantErr string
	}{
		{
			name: "empty task ID", reason: "r", agentID: "orch-1",
			wantErr: "task ID is required",
		},
		{
			name: "empty reason", taskID: "t1", agentID: "orch-1",
			wantErr: "cancellation reason is required",
		},
		{
			name: "empty agent ID", taskID: "t1", reason: "r",
			wantErr: "orchestrator agent ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CancelTask("/nonexistent", tt.taskID, tt.reason, tt.agentID)
			testhelpers.RequireErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestCancelTask_FromBlocked(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	assignee := "coder-1"
	reviewCommit := "stale-review"
	approvedBy := "code-reviewer-1"
	mergeCommit := "stale-merge"
	task.AssignedTo = &assignee
	task.ReviewCommit = &reviewCommit
	task.ApprovedBy = &approvedBy
	task.Approvals = []models.Approval{{Agent: approvedBy, Provider: "codex", Timestamp: now}}
	task.MergeCommit = &mergeCommit
	task.IntegrationFailure = map[string]any{"detail": "stale"}
	task.FailedBy = []string{"coder-1"}
	task.Output = []models.OutputEntry{{Desc: "retired output", DoneWhen: "done", Scope: "scope", SpecRef: "README.md"}}
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := CancelTask(tmpDir, "task-1", "No longer needed", "orchestrator-1")
	if err != nil {
		t.Fatalf("CancelTask() error: %v", err)
	}

	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}
	if result.OriginalStatus != models.TaskStatusBlocked {
		t.Errorf("OriginalStatus = %v, want BLOCKED", result.OriginalStatus)
	}

	// Verify state
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	updatedTask := readState.FindTask("task-1")
	if updatedTask == nil {
		t.Fatal("Task not found")
	}
	if updatedTask.Status != models.TaskStatusAbandoned {
		t.Errorf("Status = %v, want ABANDONED", updatedTask.Status)
	}

	// Verify fields cleared
	if updatedTask.AssignedTo != nil {
		t.Error("AssignedTo should be nil after cancel")
	}
	if updatedTask.LeaseExpires != nil {
		t.Error("LeaseExpires should be nil after cancel")
	}
	if updatedTask.ReviewingBy != nil {
		t.Error("ReviewingBy should be nil after cancel")
	}
	if updatedTask.ReviewLeaseExpires != nil {
		t.Error("ReviewLeaseExpires should be nil after cancel")
	}
	if updatedTask.Worktree != nil {
		t.Error("Worktree should be nil after cancel")
	}
	if updatedTask.ReviewCommit != nil {
		t.Errorf("ReviewCommit = %v, want nil after cancel", *updatedTask.ReviewCommit)
	}
	if updatedTask.ApprovedBy != nil {
		t.Errorf("ApprovedBy = %v, want nil after cancel", *updatedTask.ApprovedBy)
	}
	if len(updatedTask.Approvals) != 0 {
		t.Errorf("Approvals = %v, want cleared after cancel", updatedTask.Approvals)
	}
	if updatedTask.MergeCommit != nil {
		t.Errorf("MergeCommit = %v, want nil after cancel", *updatedTask.MergeCommit)
	}
	if updatedTask.IntegrationFailure != nil {
		t.Errorf("IntegrationFailure = %v, want nil after cancel", updatedTask.IntegrationFailure)
	}
	if len(updatedTask.FailedBy) != 1 || updatedTask.FailedBy[0] != "coder-1" {
		t.Errorf("FailedBy = %v, want preserved", updatedTask.FailedBy)
	}
	if len(updatedTask.Output) != 1 || updatedTask.Output[0].Desc != "retired output" {
		t.Errorf("Output = %v, want preserved as terminal audit context", updatedTask.Output)
	}

	// Verify history entry
	lastHistory := updatedTask.History[len(updatedTask.History)-1]
	if lastHistory.Event != models.TaskEventAbandoned {
		t.Errorf("History event = %q, want %q", lastHistory.Event, models.TaskEventAbandoned)
	}
	if lastHistory.Agent == nil || *lastHistory.Agent != "orchestrator-1" {
		t.Errorf("History agent = %v, want orchestrator-1", lastHistory.Agent)
	}
	if lastHistory.Reason == nil || *lastHistory.Reason != "No longer needed" {
		t.Errorf("History reason = %v, want 'No longer needed'", lastHistory.Reason)
	}
}

func TestCancelTask_RemovesActiveDependentDependencies(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	active := testhelpers.BuildTaskByStatus("active-dependent", models.TaskStatusReady, now)
	active.DependsOn = []string{"prep", "task-1"}
	terminal := testhelpers.BuildTaskByStatus("terminal-dependent", models.TaskStatusMerged, now)
	terminal.DependsOn = []string{"task-1"}
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
		testhelpers.BuildTaskByStatus("prep", models.TaskStatusMerged, now),
		active,
		terminal,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := CancelTask(tmpDir, "task-1", "No longer needed", "orchestrator-1")
	if err != nil {
		t.Fatalf("CancelTask() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	active = *readState.FindTask("active-dependent")
	if !slices.Equal(active.DependsOn, []string{"prep"}) {
		t.Fatalf("active depends_on = %v, want [prep]", active.DependsOn)
	}
	lastHistory := active.History[len(active.History)-1]
	if lastHistory.Event != models.TaskEventDependenciesRewritten {
		t.Fatalf("active last history event = %q, want %q", lastHistory.Event, models.TaskEventDependenciesRewritten)
	}

	terminal = *readState.FindTask("terminal-dependent")
	if !slices.Equal(terminal.DependsOn, []string{"task-1"}) {
		t.Fatalf("terminal depends_on = %v, want historical dependency preserved", terminal.DependsOn)
	}
}

func TestCancelTask_RemovesOperationalOutputTaskDependsOn(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	parent := testhelpers.BuildTaskByStatus("parent-plan", models.TaskStatusMerged, now)
	parent.RolePair = "architecture-pair"
	parent.Output = []models.OutputEntry{{
		Desc:          "Plan child",
		DoneWhen:      "child planned",
		Scope:         "internal/ops",
		SpecRef:       "specs/plan.md",
		TaskDependsOn: []string{"task-1", "keep-dep"},
	}}
	target := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	target.RolePair = "code-planning-pair"
	keep := testhelpers.BuildTaskByStatus("keep-dep", models.TaskStatusMerged, now)
	keep.RolePair = "code-planning-pair"
	state.Tasks = []models.Task{parent, target, keep}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := CancelTask(tmpDir, "task-1", "No longer needed", "orchestrator-1")
	if err != nil {
		t.Fatalf("CancelTask() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	parent = *readState.FindTask("parent-plan")
	if !slices.Equal(parent.Output[0].TaskDependsOn, []string{"keep-dep"}) {
		t.Fatalf("output task_depends_on = %v, want [keep-dep]", parent.Output[0].TaskDependsOn)
	}
}

func TestCancelTask_FromInitialCodingPair(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatus("DRAFT_CODE"), now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := CancelTask(tmpDir, "task-1", "Requirements changed", "orchestrator-1")
	if err != nil {
		t.Fatalf("CancelTask() error: %v", err)
	}
	if result.OriginalStatus != models.TaskStatus("DRAFT_CODE") {
		t.Errorf("OriginalStatus = %v, want DRAFT_CODE", result.OriginalStatus)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.FindTask("task-1").Status != models.TaskStatusAbandoned {
		t.Errorf("Status = %v, want ABANDONED", readState.FindTask("task-1").Status)
	}
}

func TestCancelTask_FromInitialEpicPlanningPair(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatus("DRAFT_EPIC_PLAN"), now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := CancelTask(tmpDir, "task-1", "No longer needed", "orchestrator-1")
	if err != nil {
		t.Fatalf("CancelTask() error: %v", err)
	}
}

func TestCancelTask_FromRejected(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := CancelTask(tmpDir, "task-1", "Approach abandoned", "orchestrator-1")
	if err != nil {
		t.Fatalf("CancelTask() error: %v", err)
	}
	if result.OriginalStatus != models.TaskStatusRejected {
		t.Errorf("OriginalStatus = %v, want REJECTED", result.OriginalStatus)
	}
}

func TestCancelTask_FromIntegrationFailed(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusIntegrationFailed, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := CancelTask(tmpDir, "task-1", "Giving up on integration", "orchestrator-1")
	if err != nil {
		t.Fatalf("CancelTask() error: %v", err)
	}
	if result.OriginalStatus != models.TaskStatusIntegrationFailed {
		t.Errorf("OriginalStatus = %v, want INTEGRATION_FAILED", result.OriginalStatus)
	}
}

func TestCancelTask_FromActiveStates(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name       string
		status     models.TaskStatus
		reviewerID string
	}{
		{name: "executing", status: models.TaskStatusImplementing},
		{name: "submitted", status: models.TaskStatusReadyForReview},
		{name: "reviewing", status: models.TaskStatusReviewing, reviewerID: "code-reviewer-1"},
		{name: "reviewing-2", status: models.TaskStatusReviewingCode2, reviewerID: "code-reviewer-2"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

			now := time.Now().UTC()
			state := testhelpers.CreateValidState()
			task := testhelpers.BuildTaskByStatus("task-1", tt.status, now)
			coderID := "coder-1"
			worktree := ".worktrees/task-1"
			reviewCommit := "stale-review"
			task.AssignedTo = &coderID
			task.Worktree = &worktree
			task.ReviewCommit = &reviewCommit
			task.LeaseExpires = testhelpers.TimePtr(now.Add(30 * time.Minute))
			state.Agents = map[string]models.Agent{
				coderID: {
					Role:         "coder",
					Status:       models.AgentStatusWorking,
					CurrentTask:  &task.ID,
					LeaseExpires: task.LeaseExpires,
					Heartbeat:    now,
				},
			}
			if tt.reviewerID != "" {
				task.ReviewingBy = &tt.reviewerID
				task.ReviewLeaseExpires = testhelpers.TimePtr(now.Add(30 * time.Minute))
				state.Agents[tt.reviewerID] = models.Agent{
					Role:        "code-reviewer",
					Status:      models.AgentStatusReviewing,
					CurrentTask: &task.ID,
					Heartbeat:   now,
				}
			}
			state.Tasks = []models.Task{task}
			testhelpers.WriteInitialState(t, stateFile, state)

			result, err := CancelTask(tmpDir, "task-1", "Mis-framed task", "orchestrator-1")
			if err != nil {
				t.Fatalf("CancelTask() error: %v", err)
			}
			if result.OriginalStatus != tt.status {
				t.Fatalf("OriginalStatus = %v, want %v", result.OriginalStatus, tt.status)
			}

			bb := db.New(stateFile)
			readState, err := bb.Read()
			if err != nil {
				t.Fatalf("Failed to read state: %v", err)
			}
			updatedTask := readState.FindTask("task-1")
			if updatedTask.Status != models.TaskStatusAbandoned {
				t.Fatalf("Status = %v, want ABANDONED", updatedTask.Status)
			}
			if updatedTask.AssignedTo != nil || updatedTask.LeaseExpires != nil {
				t.Fatalf("doer claim not cleared: assigned_to=%v lease=%v", updatedTask.AssignedTo, updatedTask.LeaseExpires)
			}
			if updatedTask.ReviewingBy != nil || updatedTask.ReviewLeaseExpires != nil {
				t.Fatalf("review claim not cleared: reviewing_by=%v lease=%v", updatedTask.ReviewingBy, updatedTask.ReviewLeaseExpires)
			}
			if updatedTask.Worktree != nil {
				t.Fatalf("Worktree = %v, want nil", *updatedTask.Worktree)
			}
			if updatedTask.ReviewCommit != nil {
				t.Fatalf("ReviewCommit = %v, want nil", *updatedTask.ReviewCommit)
			}
			if agent := readState.Agents[coderID]; agent.CurrentTask != nil || agent.Status != models.AgentStatusIdle {
				t.Fatalf("coder agent not released: status=%s current_task=%v", agent.Status, agent.CurrentTask)
			}
			if tt.reviewerID != "" {
				if agent := readState.Agents[tt.reviewerID]; agent.CurrentTask != nil || agent.Status != models.AgentStatusIdle {
					t.Fatalf("reviewer agent not released: status=%s current_task=%v", agent.Status, agent.CurrentTask)
				}
			}
		})
	}
}

func TestCancelTask_StaleOperationsFailAfterActiveCancel(t *testing.T) {
	t.Parallel()

	t.Run("doer submit-for-review", func(t *testing.T) {
		tmpDir := t.TempDir()
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		coderID := "coder-1"
		task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
		task.AssignedTo = &coderID
		task.LeaseExpires = testhelpers.TimePtr(now.Add(30 * time.Minute))
		state.Tasks = []models.Task{task}
		state.Agents = map[string]models.Agent{
			coderID: {
				Role:        "coder",
				Status:      models.AgentStatusWorking,
				CurrentTask: &task.ID,
				Heartbeat:   now,
			},
		}
		testhelpers.WriteInitialState(t, stateFile, state)

		if _, err := CancelTask(tmpDir, "task-1", "Mis-framed task", "orchestrator-1"); err != nil {
			t.Fatalf("CancelTask() error: %v", err)
		}

		_, err := SubmitForReview(tmpDir, "task-1", "HEAD", coderID)
		testhelpers.RequireErrorContains(t, err, "not IMPLEMENTING_CODE")
	})

	t.Run("reviewer submit-verdict", func(t *testing.T) {
		tmpDir := t.TempDir()
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		reviewerID := "code-reviewer-1"
		reviewCommit := "stale-review"
		task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
		task.ReviewingBy = &reviewerID
		task.ReviewLeaseExpires = testhelpers.TimePtr(now.Add(30 * time.Minute))
		task.ReviewCommit = &reviewCommit
		state.Tasks = []models.Task{task}
		state.Agents = map[string]models.Agent{
			reviewerID: {
				Role:        "code-reviewer",
				Status:      models.AgentStatusReviewing,
				CurrentTask: &task.ID,
				Heartbeat:   now,
			},
		}
		testhelpers.WriteInitialState(t, stateFile, state)

		if _, err := CancelTask(tmpDir, "task-1", "Mis-framed task", "orchestrator-1"); err != nil {
			t.Fatalf("CancelTask() error: %v", err)
		}

		_, err := SubmitVerdict(tmpDir, "task-1", "APPROVED", "", reviewerID, "")
		testhelpers.RequireErrorContains(t, err, "not in a reviewing state")
	})
}

func TestCancelTask_RejectFromApproved(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusApproved, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := CancelTask(tmpDir, "task-1", "reason", "orchestrator-1")
	if err == nil {
		t.Fatal("Expected error for APPROVED task")
	}
	testhelpers.AssertErrorContains(t, err, "transition")
}

func TestCancelTask_RejectFromMerged(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)
	before, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatal(err)
	}

	_, err = CancelTask(tmpDir, "task-1", "reason", "orchestrator-1")
	if err == nil {
		t.Fatal("Expected error for MERGED task")
	}
	var lifecycleErr *LifecycleError
	if !stderrors.As(err, &lifecycleErr) || lifecycleErr.Outcome.Outcome != models.LifecycleAlreadyTransitioned || lifecycleErr.Outcome.SafeAction != "stop" || lifecycleErr.Outcome.TaskStatus != models.TaskStatusMerged {
		t.Fatalf("terminal cancellation = %v, want ALREADY_TRANSITIONED/stop/MERGED", err)
	}
	after, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("terminal cancellation changed state")
	}
}

func TestCancelTask_TaskNotFound(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := CancelTask(tmpDir, "nonexistent", "reason", "orchestrator-1")
	if err == nil {
		t.Fatal("Expected error for nonexistent task")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestCancelTask_CleansUpWorktree(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	// Create a real git worktree
	gw := git.New(tmpDir)
	_, err := gw.CreateWorktree("task-1", "main")
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}

	// Verify worktree directory exists
	wtPath := filepath.Join(tmpDir, ".worktrees", "task-1")
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree directory should exist: %v", err)
	}

	// Set up state with BLOCKED task that has a worktree
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	worktree := ".worktrees/task-1"
	task.Worktree = &worktree
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := CancelTask(tmpDir, "task-1", "No longer needed", "orchestrator-1")
	if err != nil {
		t.Fatalf("CancelTask() error: %v", err)
	}

	// No warnings expected — cleanup should succeed with real git repo
	if len(result.Warnings) > 0 {
		t.Errorf("unexpected warnings: %v", result.Warnings)
	}

	// Verify worktree directory removed
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree directory should be removed after cancel")
	}

	// Verify state: Worktree field cleared
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	updatedTask := readState.FindTask("task-1")
	if updatedTask.Worktree != nil {
		t.Error("Worktree should be nil in state after cancel")
	}

	// Verify own branch deleted
	exists, brErr := gw.BranchExists("task/task-1")
	if brErr != nil {
		t.Fatalf("BranchExists error: %v", brErr)
	}
	if exists {
		t.Error("task branch should be deleted after cancel")
	}
}

func TestCancelTask_DeletesBranchEvenWithoutWorktree(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	// Create a branch but no worktree (simulating recovery/manual cleanup)
	testhelpers.MustGit(t, tmpDir, "branch", "task/task-1")

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	// Worktree is nil — already cleaned up
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := CancelTask(tmpDir, "task-1", "No longer needed", "orchestrator-1")
	if err != nil {
		t.Fatalf("CancelTask() error: %v", err)
	}
	if len(result.Warnings) > 0 {
		t.Errorf("unexpected warnings: %v", result.Warnings)
	}

	// Branch should still be deleted even though Worktree was nil
	gw := git.New(tmpDir)
	exists, brErr := gw.BranchExists("task/task-1")
	if brErr != nil {
		t.Fatalf("BranchExists error: %v", brErr)
	}
	if exists {
		t.Error("task branch should be deleted after cancel even when Worktree was nil")
	}
}
