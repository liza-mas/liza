package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/perm"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// setupStateWithDeps creates state containing a target task and a dependent task.
func setupStateWithDeps(t *testing.T, tmpDir string) string {
	t.Helper()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC()))
	dep := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, time.Now().UTC())
	dep.DependsOn = []string{"task-1"}
	state.Tasks = append(state.Tasks, dep)
	testhelpers.WriteInitialState(t, stateFile, state)
	return stateFile
}

// --- CheckDeleteTask tests ---

func TestCheckDeleteTask_Validation(t *testing.T) {
	t.Parallel()

	_, err := CheckDeleteTask("/nonexistent", "", false)
	if err == nil {
		t.Fatal("Expected error for empty task ID")
	}
	if !strings.Contains(err.Error(), "task ID required") {
		t.Errorf("Error = %q, want to contain 'task ID required'", err.Error())
	}
}

func TestCheckDeleteTask_NotFound(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.WriteInitialState(t, stateFile, testhelpers.CreateValidState())

	_, err := CheckDeleteTask(tmpDir, "nonexistent", false)
	if err == nil {
		t.Fatal("Expected error for nonexistent task")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestCheckDeleteTask_StatusBlocked(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	validLease := now.Add(1 * time.Hour)

	tests := []struct {
		name       string
		status     models.TaskStatus
		lease      *time.Time // non-nil sets LeaseExpires
		wantErrMsg string
	}{
		{"MERGED", models.TaskStatusMerged, nil, "cannot delete MERGED task"},
		{"IMPLEMENTING_CODE with active lease", models.TaskStatusImplementing, &validLease, "actively being worked on"},
		{"CODE_TO_REVIEW", models.TaskStatusReadyForReview, nil, "under review"},
		{"legacy CODE_READY_FOR_REVIEW", models.TaskStatusLegacyReadyForReview, nil, "under review"},
		{"REVIEWING_CODE", models.TaskStatusReviewing, nil, "under review"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			state := testhelpers.CreateValidState()
			task := testhelpers.BuildTaskByStatus("task-1", tt.status, now)
			task.LeaseExpires = tt.lease
			state.Tasks = append(state.Tasks, task)
			testhelpers.WriteInitialState(t, stateFile, state)

			_, err := CheckDeleteTask(tmpDir, "task-1", false)
			if err == nil {
				t.Fatalf("Expected error for %s task", tt.status)
			}
			if !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Errorf("Error = %q, want to contain %q", err.Error(), tt.wantErrMsg)
			}
		})
	}
}

func TestCheckDeleteTask_MergedForce(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, time.Now().UTC()))
	testhelpers.WriteInitialState(t, stateFile, state)

	info, err := CheckDeleteTask(tmpDir, "task-1", true)
	if err != nil {
		t.Fatalf("CheckDeleteTask() error: %v", err)
	}
	if info.Status != models.TaskStatusMerged {
		t.Errorf("Status = %q, want MERGED", info.Status)
	}
}

func TestCheckDeleteTask_DependentsBlocked(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	setupStateWithDeps(t, tmpDir)

	_, err := CheckDeleteTask(tmpDir, "task-1", false)
	if err == nil {
		t.Fatal("Expected error for task with dependents")
	}
	if !strings.Contains(err.Error(), "depend on it") {
		t.Errorf("Error = %q, want to contain 'depend on it'", err.Error())
	}
}

func TestCheckDeleteTask_DependentsForce(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	setupStateWithDeps(t, tmpDir)

	info, err := CheckDeleteTask(tmpDir, "task-1", true)
	if err != nil {
		t.Fatalf("CheckDeleteTask() error: %v", err)
	}
	if len(info.DependentTasks) != 1 || info.DependentTasks[0] != "task-2" {
		t.Errorf("DependentTasks = %v, want [task-2]", info.DependentTasks)
	}
}

func TestCheckDeleteTask_ReturnsInfo(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusApproved, time.Now().UTC())
	wt := "worktrees/task-1"
	task.Worktree = &wt
	state.Tasks = append(state.Tasks, task)
	testhelpers.WriteInitialState(t, stateFile, state)

	info, err := CheckDeleteTask(tmpDir, "task-1", false)
	if err != nil {
		t.Fatalf("CheckDeleteTask() error: %v", err)
	}
	if info.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", info.TaskID, "task-1")
	}
	if info.Status != models.TaskStatusApproved {
		t.Errorf("Status = %q, want APPROVED", info.Status)
	}
	if !info.HasWorktree {
		t.Error("HasWorktree should be true")
	}
}

// --- DeleteTask tests ---

func TestDeleteTask_Validation(t *testing.T) {
	t.Parallel()

	_, err := DeleteTask("/nonexistent", "", false, false, "reason")
	if err == nil {
		t.Fatal("Expected error for empty task ID")
	}
	if !strings.Contains(err.Error(), "task ID required") {
		t.Errorf("Error = %q, want to contain 'task ID required'", err.Error())
	}
}

func TestDeleteTask_SuccessfulDeletion(t *testing.T) {
	t.Parallel()

	for _, status := range []models.TaskStatus{models.TaskStatusDraft, models.TaskStatusReady} {
		t.Run(string(status), func(t *testing.T) {
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			state := testhelpers.CreateValidState()
			state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus("task-1", status, time.Now().UTC()))
			testhelpers.WriteInitialState(t, stateFile, state)

			result, err := DeleteTask(tmpDir, "task-1", false, false, "test deletion")
			if err != nil {
				t.Fatalf("DeleteTask() error: %v", err)
			}
			if result.PreviousStatus != status {
				t.Errorf("PreviousStatus = %q, want %q", result.PreviousStatus, status)
			}

			bb := db.New(stateFile)
			readState, err := bb.Read()
			if err != nil {
				t.Fatalf("Failed to read state: %v", err)
			}
			if readState.FindTask("task-1") != nil {
				t.Error("Task should be removed from state")
			}
		})
	}
}

func TestDeleteTask_ForceMerged(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, time.Now().UTC()))
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := DeleteTask(tmpDir, "task-1", true, false, "forced deletion")
	if err != nil {
		t.Fatalf("DeleteTask() error: %v", err)
	}
	if result.PreviousStatus != models.TaskStatusMerged {
		t.Errorf("PreviousStatus = %q, want MERGED", result.PreviousStatus)
	}
}

func TestDeleteTask_ClearsAgentState(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusDraft, time.Now().UTC())
	agentID := "coder-1"
	task.AssignedTo = &agentID
	state.Tasks = append(state.Tasks, task)
	taskRef := "task-1"
	state.Agents["coder-1"] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: &taskRef,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := DeleteTask(tmpDir, "task-1", false, false, "cleanup")
	if err != nil {
		t.Fatalf("DeleteTask() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	agent, exists := readState.Agents["coder-1"]
	if !exists {
		t.Fatal("Agent should still exist")
	}
	if agent.CurrentTask != nil {
		t.Errorf("Agent CurrentTask should be nil, got %v", agent.CurrentTask)
	}
	if agent.Status != models.AgentStatusIdle {
		t.Errorf("Agent Status = %q, want idle", agent.Status)
	}
}

func TestDeleteTask_RemovesFromSprintScope(t *testing.T) {
	t.Parallel()

	for _, scope := range []string{"planned", "stretch"} {
		t.Run(scope, func(t *testing.T) {
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			state := testhelpers.CreateValidState()
			state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC()))
			switch scope {
			case "planned":
				state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, "task-1")
			case "stretch":
				state.Sprint.Scope.Stretch = append(state.Sprint.Scope.Stretch, "task-1")
			}
			testhelpers.WriteInitialState(t, stateFile, state)

			_, err := DeleteTask(tmpDir, "task-1", false, false, "test")
			if err != nil {
				t.Fatalf("DeleteTask() error: %v", err)
			}

			bb := db.New(stateFile)
			readState, err := bb.Read()
			if err != nil {
				t.Fatalf("Failed to read state: %v", err)
			}
			var ids []string
			switch scope {
			case "planned":
				ids = readState.Sprint.Scope.Planned
			case "stretch":
				ids = readState.Sprint.Scope.Stretch
			}
			for _, id := range ids {
				if id == "task-1" {
					t.Errorf("Task should have been removed from sprint.scope.%s", scope)
				}
			}
		})
	}
}

func TestDeleteTask_AddsAuditTrail(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus("task-1", models.TaskStatusDraft, time.Now().UTC()))
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := DeleteTask(tmpDir, "task-1", false, false, "no longer needed")
	if err != nil {
		t.Fatalf("DeleteTask() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if len(readState.HumanNotes) == 0 {
		t.Fatal("Expected human note to be added")
	}
	lastNote := readState.HumanNotes[len(readState.HumanNotes)-1]
	if !strings.Contains(lastNote.Message, "task-1") {
		t.Errorf("Note message = %q, want to contain task ID", lastNote.Message)
	}
	if !strings.Contains(lastNote.Message, "no longer needed") {
		t.Errorf("Note message = %q, want to contain reason", lastNote.Message)
	}
	if lastNote.For != "all" {
		t.Errorf("Note For = %q, want 'all'", lastNote.For)
	}
}

// --- DeleteTask precondition enforcement (direct calls bypassing CheckDeleteTask) ---

func TestDeleteTask_StatusBlocked(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	validLease := now.Add(1 * time.Hour)

	tests := []struct {
		name       string
		status     models.TaskStatus
		lease      *time.Time
		wantErrMsg string
	}{
		{"MERGED", models.TaskStatusMerged, nil, "cannot delete MERGED task"},
		{"IMPLEMENTING_CODE with active lease", models.TaskStatusImplementing, &validLease, "actively being worked on"},
		{"CODE_TO_REVIEW", models.TaskStatusReadyForReview, nil, "under review"},
		{"legacy CODE_READY_FOR_REVIEW", models.TaskStatusLegacyReadyForReview, nil, "under review"},
		{"REVIEWING_CODE", models.TaskStatusReviewing, nil, "under review"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			state := testhelpers.CreateValidState()
			task := testhelpers.BuildTaskByStatus("task-1", tt.status, now)
			task.LeaseExpires = tt.lease
			state.Tasks = append(state.Tasks, task)
			testhelpers.WriteInitialState(t, stateFile, state)

			_, err := DeleteTask(tmpDir, "task-1", false, false, "test")
			if err == nil {
				t.Fatalf("Expected error for %s task", tt.status)
			}
			if !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Errorf("Error = %q, want to contain %q", err.Error(), tt.wantErrMsg)
			}
		})
	}
}

func TestDeleteTask_DependentsBlocked(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile := setupStateWithDeps(t, tmpDir)

	_, err := DeleteTask(tmpDir, "task-1", false, false, "test")
	if err == nil {
		t.Fatal("Expected error for task with dependents")
	}
	if !strings.Contains(err.Error(), "depends on it") {
		t.Errorf("Error = %q, want to contain 'depends on it'", err.Error())
	}

	// Verify task was NOT deleted
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.FindTask("task-1") == nil {
		t.Error("Task should still exist after blocked deletion")
	}
}

func TestDeleteTask_DependentsForce(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	setupStateWithDeps(t, tmpDir)

	result, err := DeleteTask(tmpDir, "task-1", true, false, "forced")
	if err != nil {
		t.Fatalf("DeleteTask() error: %v", err)
	}
	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}
}

func TestDeleteTask_BlocksDeletionWhenChildParentTaskReferencesTarget(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	parent := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
	child := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, now)
	child.ParentTask = strPtr("task-1")
	state.Tasks = append(state.Tasks, parent, child)
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := DeleteTask(tmpDir, "task-1", false, false, "cleanup")
	if err == nil {
		t.Fatal("DeleteTask() should reject deleting a parent task that is still referenced by child.parent_task")
	}
	if !strings.Contains(err.Error(), "parent_task") && !strings.Contains(err.Error(), "depends on it") {
		t.Fatalf("DeleteTask() error = %q, want to mention parent/child referential integrity", err.Error())
	}

	bb := db.New(stateFile)
	readState, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("Failed to read state: %v", readErr)
	}
	if readState.FindTask("task-1") == nil {
		t.Fatal("Parent task should still exist after blocked deletion")
	}
}

func TestDeleteTask_ParentReferenceForceBypassesBlock(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	parent := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
	child := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, now)
	child.ParentTask = strPtr("task-1")
	state.Tasks = append(state.Tasks, parent, child)
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := DeleteTask(tmpDir, "task-1", true, false, "forced cleanup")
	if err != nil {
		t.Fatalf("DeleteTask() with force should bypass parent_task block: %v", err)
	}
	if result.TaskID != "task-1" {
		t.Fatalf("TaskID = %q, want %q", result.TaskID, "task-1")
	}

	bb := db.New(stateFile)
	readState, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("Failed to read state: %v", readErr)
	}
	if readState.FindTask("task-1") != nil {
		t.Fatal("Parent task should be deleted when force=true")
	}
	if readState.FindTask("task-2") == nil {
		t.Fatal("Child task should remain after forced parent deletion")
	}
}

// --- Worktree handling ---

func TestDeleteTask_NoWorktree(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusDraft, time.Now().UTC())
	task.Worktree = nil
	state.Tasks = append(state.Tasks, task)
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := DeleteTask(tmpDir, "task-1", false, false, "test")
	if err != nil {
		t.Fatalf("DeleteTask() error: %v", err)
	}
	if result.WorktreeDeleted {
		t.Error("WorktreeDeleted should be false when no worktree")
	}
	if result.WorktreePreserved {
		t.Error("WorktreePreserved should be false when no worktree")
	}
}

func TestDeleteTask_WorktreePreserved(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusDraft, time.Now().UTC())
	wt := "worktrees/task-1"
	task.Worktree = &wt
	state.Tasks = append(state.Tasks, task)
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := DeleteTask(tmpDir, "task-1", false, false, "test")
	if err != nil {
		t.Fatalf("DeleteTask() error: %v", err)
	}
	if result.WorktreeDeleted {
		t.Error("WorktreeDeleted should be false when deleteWorktree=false")
	}
	if !result.WorktreePreserved {
		t.Error("WorktreePreserved should be true when worktree exists but deleteWorktree=false")
	}
}

func TestDeleteTask_CommitFailureDoesNotDeleteWorktreeOrBranch(t *testing.T) {
	t.Parallel()

	// This test forces a commit failure by making the state directory
	// non-writable, which perm.DenyWrites does with a chmod on POSIX and an
	// explicit deny entry in the DACL on Windows.
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusDraft, time.Now().UTC())
	wt := ".worktrees/task-1"
	task.Worktree = &wt
	state.Tasks = append(state.Tasks, task)
	testhelpers.WriteInitialState(t, stateFile, state)

	gitWrapper := git.New(tmpDir)
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("Failed to create task worktree: %v", err)
	}

	// Force state commit failure while keeping lock acquisition possible.
	// filelock writes state.yaml.lock.pid on each lock, so pre-create it
	// before making .liza non-writable.
	lizaDir := filepath.Join(tmpDir, ".liza")
	pidPath := filepath.Join(lizaDir, "state.yaml.lock.pid")
	if err := os.WriteFile(pidPath, []byte("0"), 0644); err != nil {
		t.Fatalf("Failed to pre-create pid file: %v", err)
	}
	restore, err := perm.DenyWrites(lizaDir)
	if err != nil {
		t.Fatalf("Failed to make .liza read-only: %v", err)
	}
	t.Cleanup(func() {
		if err := restore(); err != nil {
			t.Errorf("restore write access: %v", err)
		}
	})

	_, err = DeleteTask(tmpDir, "task-1", false, true, "test")
	if err == nil {
		t.Fatal("Expected DeleteTask to fail when state commit cannot write")
	}
	if !strings.Contains(err.Error(), "failed to delete task") {
		t.Errorf("Error = %q, want to contain 'failed to delete task'", err.Error())
	}

	// Task should still exist because state commit failed.
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.FindTask("task-1") == nil {
		t.Fatal("Task should still exist after failed delete")
	}

	// Side effects must not run before successful commit.
	worktreePath := filepath.Join(tmpDir, ".worktrees", "task-1")
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("Worktree should still exist after failed delete, stat error: %v", err)
	}
	branches := testhelpers.MustGit(t, tmpDir, "branch", "--list", "task/task-1")
	if !strings.Contains(branches, "task/task-1") {
		t.Fatalf("Task branch should still exist after failed delete, branches output: %q", branches)
	}
}
