package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// setupImplementingTask creates a state with coder-1 implementing task-1.
// The agent PID defaults to 999999 (dead). Returns (tmpDir, stateFile).
func setupImplementingTask(t *testing.T, agentPID int) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	taskRef := "task-1"
	now := time.Now().UTC()
	leaseExpires := now.Add(-10 * time.Minute) // expired
	worktreeRef := ".worktrees/task-1"
	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusWorking,
		CurrentTask:  &taskRef,
		LeaseExpires: &leaseExpires,
		PID:          agentPID,
	}
	state.Tasks = append(state.Tasks, models.Task{
		ID:           "task-1",
		Description:  "test task",
		Status:       models.TaskStatusImplementing,
		RolePair:     "coding-pair",
		Priority:     1,
		AssignedTo:   strPtr("coder-1"),
		Worktree:     &worktreeRef,
		LeaseExpires: &leaseExpires,
		SpecRef:      "spec.md",
		DoneWhen:     "tests pass",
		Scope:        "small",
	})
	testhelpers.WriteInitialState(t, stateFile, state)
	return tmpDir, stateFile
}

// setupEmptyState creates a valid state with no tasks or agents. Returns (tmpDir, stateFile).
func setupEmptyState(t *testing.T) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.WriteInitialState(t, stateFile, testhelpers.CreateValidState())
	return tmpDir, stateFile
}

func TestRecoverTask_Validation(t *testing.T) {
	_, err := RecoverTask("/nonexistent", "", false, "reason")
	if err == nil {
		t.Fatal("Expected error for empty task ID")
	}
	if !strings.Contains(err.Error(), "task ID required") {
		t.Errorf("Error = %q, want to contain 'task ID required'", err.Error())
	}
}

func TestRecoverTask_InvalidTaskID(t *testing.T) {
	_, err := RecoverTask("/nonexistent", "../escape", false, "reason")
	if err == nil {
		t.Fatal("Expected error for invalid task ID")
	}
	if !strings.Contains(err.Error(), "invalid task ID") {
		t.Errorf("Error = %q, want to contain 'invalid task ID'", err.Error())
	}
}

func TestRecoverTask_NotInState_NoForce(t *testing.T) {
	tmpDir, _ := setupEmptyState(t)

	_, err := RecoverTask(tmpDir, "task-1", false, "reason")
	if err == nil {
		t.Fatal("Expected error for task not in state without --force")
	}
	if !strings.Contains(err.Error(), "not found in state") {
		t.Errorf("Error = %q, want to contain 'not found in state'", err.Error())
	}
}

func TestRecoverTask_NotInState_Force_CleansGitArtifacts(t *testing.T) {
	tmpDir, _ := setupEmptyState(t)
	testhelpers.SetupTestGitRepo(t, tmpDir)

	// Create a worktree directory (simulating orphaned artifact)
	wtDir := filepath.Join(tmpDir, ".worktrees", "task-1")
	if err := os.MkdirAll(wtDir, 0755); err != nil {
		t.Fatalf("Failed to create worktree dir: %v", err)
	}

	result, err := RecoverTask(tmpDir, "task-1", true, "orphan cleanup")
	if err != nil {
		t.Fatalf("RecoverTask() error: %v", err)
	}

	if result.InState {
		t.Error("Expected InState=false")
	}
	if result.AgentRecovered {
		t.Error("Expected AgentRecovered=false")
	}
	if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
		t.Error("Expected worktree directory to be removed")
	}
}

func TestRecoverTask_NotInState_Force_NothingToClean(t *testing.T) {
	tmpDir, _ := setupEmptyState(t)
	testhelpers.SetupTestGitRepo(t, tmpDir)

	result, err := RecoverTask(tmpDir, "task-1", true, "cleanup")
	if err != nil {
		t.Fatalf("RecoverTask() error: %v", err)
	}

	if result.InState {
		t.Error("Expected InState=false")
	}
}

func TestRecoverTask_ImplementingTask_WithAgent(t *testing.T) {
	tmpDir, stateFile := setupImplementingTask(t, 999999)

	result, err := RecoverTask(tmpDir, "task-1", false, "crashed")
	if err != nil {
		t.Fatalf("RecoverTask() error: %v", err)
	}

	if !result.InState {
		t.Error("Expected InState=true")
	}
	if result.AgentID != "coder-1" {
		t.Errorf("AgentID = %q, want %q", result.AgentID, "coder-1")
	}
	if !result.ClaimReleased {
		t.Error("Expected ClaimReleased=true")
	}
	if !result.AgentRecovered {
		t.Error("Expected AgentRecovered=true")
	}

	// Verify state
	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	if _, exists := readState.Agents["coder-1"]; exists {
		t.Error("Agent should be removed from state")
	}

	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("Task should still exist")
	}
	if task.Status != models.TaskStatusReady {
		t.Errorf("Task status = %s, want %s", task.Status, models.TaskStatusReady)
	}
	if task.AssignedTo != nil {
		t.Error("Task AssignedTo should be nil")
	}
	if task.LeaseExpires != nil {
		t.Error("Task LeaseExpires should be nil")
	}
	if task.Worktree != nil {
		t.Error("Task Worktree should be nil")
	}

	lastNote := readState.HumanNotes[len(readState.HumanNotes)-1]
	if !strings.Contains(lastNote.Message, "task-1") || !strings.Contains(lastNote.Message, "coder-1") {
		t.Errorf("Note message = %q, want to contain task and agent IDs", lastNote.Message)
	}
	if !lastNote.SeenByOrchestrator() {
		t.Error("audit note must be stamped seen so it does not wake the orchestrator")
	}
}

func TestRecoverTask_ReviewingTask(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	gitWrapper := git.New(tmpDir)
	baseCommit, err := gitWrapper.CreateWorktree("task-2", "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	reviewCommit, err := gitWrapper.GetWorktreeHEAD("task-2")
	if err != nil {
		t.Fatalf("GetWorktreeHEAD() error: %v", err)
	}

	taskRef := "task-2"
	now := time.Now().UTC()
	leaseExpires := now.Add(-10 * time.Minute)
	worktreeRef := ".worktrees/task-2"
	state := testhelpers.CreateValidState()
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:         "code-reviewer",
		Status:       models.AgentStatusReviewing,
		CurrentTask:  &taskRef,
		LeaseExpires: &leaseExpires,
	}
	state.Tasks = append(state.Tasks, models.Task{
		ID:                 "task-2",
		Description:        "test task",
		Status:             models.TaskStatusReviewing,
		RolePair:           "coding-pair",
		Priority:           1,
		ReviewingBy:        strPtr("code-reviewer-1"),
		ReviewLeaseExpires: &leaseExpires,
		ReviewCommit:       &reviewCommit,
		BaseCommit:         &baseCommit,
		Worktree:           &worktreeRef,
		SpecRef:            "spec.md",
		DoneWhen:           "tests pass",
		Scope:              "small",
	})
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RecoverTask(tmpDir, "task-2", false, "crashed")
	if err != nil {
		t.Fatalf("RecoverTask() error: %v", err)
	}

	if result.AgentID != "code-reviewer-1" {
		t.Errorf("AgentID = %q, want %q", result.AgentID, "code-reviewer-1")
	}
	if !result.ClaimReleased {
		t.Error("Expected ClaimReleased=true")
	}
	if !result.AgentRecovered {
		t.Error("Expected AgentRecovered=true")
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if _, exists := readState.Agents["code-reviewer-1"]; exists {
		t.Error("Agent should be removed from state")
	}
	task := readState.FindTask("task-2")
	if task == nil {
		t.Fatal("Task should still exist")
	}
	if task.Status != models.TaskStatusReadyForReview {
		t.Errorf("Task status = %s, want %s", task.Status, models.TaskStatusReadyForReview)
	}
	if task.ReviewingBy != nil {
		t.Error("Task ReviewingBy should be nil")
	}
	if task.Worktree == nil || *task.Worktree != worktreeRef {
		t.Fatalf("Task Worktree = %v, want %s", task.Worktree, worktreeRef)
	}
	if task.ReviewCommit == nil || *task.ReviewCommit != reviewCommit {
		t.Fatalf("Task ReviewCommit = %v, want %s", task.ReviewCommit, reviewCommit)
	}
}

func TestRecoverTask_MissingPipelineConfigPreservesReviewerAgent(t *testing.T) {
	tmpDir := t.TempDir()

	// Manually create the project runtime directory WITHOUT pipeline config so resolver loading fails.
	lizaDir := filepath.Join(tmpDir, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatalf("failed to create project runtime directory: %v", err)
	}
	lockPath := filepath.Join(lizaDir, "state.yaml.lock")
	if err := os.WriteFile(lockPath, []byte{}, 0644); err != nil {
		t.Fatalf("Failed to create lock file: %v", err)
	}

	stateFile := filepath.Join(lizaDir, "state.yaml")
	taskID := "task-nil-reviewer"
	reviewerID := "code-reviewer-1"
	now := time.Now().UTC()
	leaseExpires := now.Add(-10 * time.Minute)
	state := testhelpers.CreateValidState()
	state.Agents[reviewerID] = models.Agent{
		Role:         "code-reviewer",
		Status:       models.AgentStatusReviewing,
		CurrentTask:  &taskID,
		LeaseExpires: &leaseExpires,
		PID:          999999,
	}
	state.Tasks = append(state.Tasks, models.Task{
		ID:                 taskID,
		Description:        "test task",
		Status:             models.TaskStatusReviewing,
		RolePair:           "coding-pair",
		Priority:           1,
		ReviewingBy:        &reviewerID,
		ReviewLeaseExpires: &leaseExpires,
		SpecRef:            "spec.md",
		DoneWhen:           "tests pass",
		Scope:              "small",
	})
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := RecoverTask(tmpDir, taskID, false, "crashed")
	if err == nil {
		t.Fatal("RecoverTask() error = nil, want missing pipeline config error")
	}
	if !strings.Contains(err.Error(), "pipeline config") {
		t.Fatalf("RecoverTask() error = %q, want pipeline config error", err.Error())
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if _, exists := readState.Agents[reviewerID]; !exists {
		t.Fatal("reviewer agent should be preserved when claim release is skipped")
	}
	task := readState.FindTask(taskID)
	if task == nil {
		t.Fatal("task should still exist")
	}
	if task.ReviewingBy == nil || *task.ReviewingBy != reviewerID {
		t.Fatalf("task ReviewingBy = %v, want %s", task.ReviewingBy, reviewerID)
	}
}

func TestRecoverTask_DualClaim_ReviewerPIDAlive_NoForce(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	coderTask := "task-1"
	reviewerTask := "task-1"
	now := time.Now().UTC()
	expiredLease := now.Add(-10 * time.Minute)
	activeLease := now.Add(10 * time.Minute)
	worktreeRef := ".worktrees/task-1"
	reviewCommit := "abc123"
	baseCommit := "def456"
	state := testhelpers.CreateValidState()
	// Coder: dead PID, expired lease
	state.Agents["coder-1"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusWaiting,
		CurrentTask:  &coderTask,
		LeaseExpires: &expiredLease,
		PID:          999999,
	}
	// Reviewer: alive PID, active lease
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:         "code-reviewer",
		Status:       models.AgentStatusReviewing,
		CurrentTask:  &reviewerTask,
		LeaseExpires: &activeLease,
		PID:          os.Getpid(),
	}
	state.Tasks = append(state.Tasks, models.Task{
		ID:                 "task-1",
		Description:        "test task",
		Status:             models.TaskStatusReviewing,
		RolePair:           "coding-pair",
		Priority:           1,
		AssignedTo:         strPtr("coder-1"),
		ReviewingBy:        strPtr("code-reviewer-1"),
		ReviewLeaseExpires: &activeLease,
		LeaseExpires:       &expiredLease,
		Worktree:           &worktreeRef,
		ReviewCommit:       &reviewCommit,
		BaseCommit:         &baseCommit,
		SpecRef:            "spec.md",
		DoneWhen:           "tests pass",
		Scope:              "small",
	})
	testhelpers.WriteInitialState(t, stateFile, state)

	// Should refuse — reviewer PID is alive
	_, err := RecoverTask(tmpDir, "task-1", false, "reason")
	if err == nil {
		t.Fatal("Expected error for alive reviewer PID without force")
	}
	if !strings.Contains(err.Error(), "reviewer") {
		t.Errorf("Error = %q, want to mention 'reviewer'", err.Error())
	}
	if !strings.Contains(err.Error(), "still running") {
		t.Errorf("Error = %q, want to contain 'still running'", err.Error())
	}

	// Verify nothing was modified
	readState, _ := db.New(stateFile).Read()
	if _, exists := readState.Agents["code-reviewer-1"]; !exists {
		t.Error("Reviewer agent should NOT have been deleted")
	}
	if _, exists := readState.Agents["coder-1"]; !exists {
		t.Error("Coder agent should NOT have been deleted")
	}
}

func TestRecoverTask_PIDAlive_NoForce(t *testing.T) {
	tmpDir, _ := setupImplementingTask(t, os.Getpid())

	_, err := RecoverTask(tmpDir, "task-1", false, "reason")
	if err == nil {
		t.Fatal("Expected error for alive PID without force")
	}
	if !strings.Contains(err.Error(), "still running") {
		t.Errorf("Error = %q, want to contain 'still running'", err.Error())
	}
}

func TestRecoverTask_PIDAlive_WithForce(t *testing.T) {
	tmpDir, _ := setupImplementingTask(t, os.Getpid())

	result, err := RecoverTask(tmpDir, "task-1", true, "forced recovery")
	if err != nil {
		t.Fatalf("RecoverTask() with force error: %v", err)
	}
	if !result.AgentRecovered {
		t.Error("Expected AgentRecovered=true with force")
	}
}

func TestRecoverTask_NoAgent_TaskOnly(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	worktreeRef := ".worktrees/task-1"
	state := testhelpers.CreateValidState()
	state.Tasks = append(state.Tasks, models.Task{
		ID:          "task-1",
		Description: "test task",
		Status:      models.TaskStatusReady,
		RolePair:    "coding-pair",
		Priority:    1,
		Worktree:    &worktreeRef,
		SpecRef:     "spec.md",
		DoneWhen:    "tests pass",
		Scope:       "small",
	})
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RecoverTask(tmpDir, "task-1", false, "cleanup")
	if err != nil {
		t.Fatalf("RecoverTask() error: %v", err)
	}

	if !result.InState {
		t.Error("Expected InState=true")
	}
	if result.AgentID != "" {
		t.Errorf("AgentID = %q, want empty", result.AgentID)
	}
	if result.AgentRecovered {
		t.Error("Expected AgentRecovered=false")
	}

	task, _ := db.New(stateFile).Read()
	if task.FindTask("task-1").Worktree != nil {
		t.Error("Task Worktree should be nil")
	}
}

func TestRecoverTask_Idempotent(t *testing.T) {
	tmpDir, _ := setupImplementingTask(t, 999999)

	result1, err := RecoverTask(tmpDir, "task-1", false, "first")
	if err != nil {
		t.Fatalf("First RecoverTask() error: %v", err)
	}
	if !result1.AgentRecovered {
		t.Error("First recovery should recover agent")
	}

	// Second recovery — task still in state but already clean
	result2, err := RecoverTask(tmpDir, "task-1", false, "second")
	if err != nil {
		t.Fatalf("Second RecoverTask() error: %v", err)
	}
	if result2.AgentRecovered {
		t.Error("Second recovery should not recover agent (already gone)")
	}
	if result2.ClaimReleased {
		t.Error("Second recovery should not release claim (already released)")
	}
}

func TestRecoverTask_DefaultReason(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Tasks = append(state.Tasks, models.Task{
		ID:          "task-1",
		Description: "test task",
		Status:      models.TaskStatusReady,
		RolePair:    "coding-pair",
		Priority:    1,
		SpecRef:     "spec.md",
		DoneWhen:    "tests pass",
		Scope:       "small",
	})
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RecoverTask(tmpDir, "task-1", false, "")
	if err != nil {
		t.Fatalf("RecoverTask() error: %v", err)
	}
	if !result.InState {
		t.Error("Expected InState=true")
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	lastNote := readState.HumanNotes[len(readState.HumanNotes)-1]
	if !strings.Contains(lastNote.Message, "task recovery") {
		t.Errorf("Note message = %q, want to contain default reason 'task recovery'", lastNote.Message)
	}
}

func TestRecoverTask_MissingReviewCommitCorruption(t *testing.T) {
	// Task is CODE_READY_FOR_REVIEW but ReviewCommit is nil (corrupted state).
	// recover-task should detect this and reset to initial status.
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	approvedBy := "code-reviewer-1"
	mergeCommit := "stale-merge"
	state.Tasks = []models.Task{
		{
			ID:           "task-corrupted",
			Status:       models.TaskStatusReadyForReview,
			RolePair:     "coding-pair",
			Priority:     1,
			Created:      now,
			ReviewCommit: nil, // corrupted: missing review_commit
			ApprovedBy:   &approvedBy,
			Approvals:    []models.Approval{{Agent: approvedBy, Provider: "codex", Timestamp: now}},
			MergeCommit:  &mergeCommit,
			FailedBy:     []string{"coder-1"},
			Output:       []models.OutputEntry{{Desc: "stale", DoneWhen: "done", Scope: "scope", SpecRef: "README.md"}},
			History:      []models.TaskHistoryEntry{},
			IntegrationFailure: map[string]any{
				"detail": "stale",
			},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RecoverTaskWithOptions(tmpDir, "task-corrupted", "fix corruption", RecoverTaskOptions{Force: true, Fresh: true})
	if err != nil {
		t.Fatalf("RecoverTask() error: %v", err)
	}

	if !result.FreshReset {
		t.Error("FreshReset = false, want true")
	}

	// Verify task was reset to initial status
	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	task := readState.FindTask("task-corrupted")
	if task == nil {
		t.Fatal("task not found in state")
	}
	if task.Status != models.TaskStatusReady {
		t.Errorf("Status = %q, want %q (reset to initial)", task.Status, models.TaskStatusReady)
	}
	if task.ReviewingBy != nil {
		t.Error("ReviewingBy should be nil after reset")
	}
	if task.ReviewLeaseExpires != nil {
		t.Error("ReviewLeaseExpires should be nil after reset")
	}
	if task.Approvals != nil {
		t.Error("Approvals should be nil after reset")
	}
	if task.ApprovedBy != nil {
		t.Error("ApprovedBy should be nil after reset")
	}
	if task.MergeCommit != nil {
		t.Error("MergeCommit should be nil after reset")
	}
	if task.IntegrationFailure != nil {
		t.Error("IntegrationFailure should be nil after reset")
	}
	if len(task.Output) != 0 {
		t.Errorf("Output = %v, want cleared after reset", task.Output)
	}
	if len(task.FailedBy) != 0 {
		t.Errorf("FailedBy = %v, want cleared after reset", task.FailedBy)
	}
}

func TestRecoverTask_ReviewingTask_ReattachesMissingWorktreeFromBranch(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	gitWrapper := git.New(tmpDir)
	baseCommit, err := gitWrapper.CreateWorktree("task-review", "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	reviewCommit, err := gitWrapper.GetWorktreeHEAD("task-review")
	if err != nil {
		t.Fatalf("GetWorktreeHEAD() error: %v", err)
	}
	if err := gitWrapper.RemoveWorktreeDir("task-review"); err != nil {
		t.Fatalf("RemoveWorktreeDir() error: %v", err)
	}

	taskID := "task-review"
	reviewerID := "code-reviewer-1"
	now := time.Now().UTC()
	leaseExpires := now.Add(-10 * time.Minute)
	worktreeRef := ".worktrees/task-review"
	state := testhelpers.CreateValidState()
	state.Agents[reviewerID] = models.Agent{
		Role:         "code-reviewer",
		Status:       models.AgentStatusReviewing,
		CurrentTask:  &taskID,
		LeaseExpires: &leaseExpires,
	}
	state.Tasks = []models.Task{{
		ID:                 taskID,
		Description:        "review candidate",
		Status:             models.TaskStatusReviewing,
		RolePair:           "coding-pair",
		Priority:           1,
		ReviewingBy:        &reviewerID,
		ReviewLeaseExpires: &leaseExpires,
		ReviewCommit:       &reviewCommit,
		BaseCommit:         &baseCommit,
		Worktree:           &worktreeRef,
		SpecRef:            "spec.md",
		DoneWhen:           "tests pass",
		Scope:              "small",
	}}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RecoverTask(tmpDir, taskID, false, "reattach reviewer worktree")
	if err != nil {
		t.Fatalf("RecoverTask() error: %v", err)
	}
	if !result.WorktreeRecovered {
		t.Error("WorktreeRecovered = false, want true")
	}
	if !result.PreservedWorktree {
		t.Error("PreservedWorktree = false, want true")
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	task := readState.FindTask(taskID)
	if task.Status != models.TaskStatusReadyForReview {
		t.Fatalf("Status = %s, want %s", task.Status, models.TaskStatusReadyForReview)
	}
	if task.ReviewCommit == nil || *task.ReviewCommit != reviewCommit {
		t.Fatalf("ReviewCommit = %v, want %s", task.ReviewCommit, reviewCommit)
	}
	if task.Worktree == nil || *task.Worktree != worktreeRef {
		t.Fatalf("Worktree = %v, want %s", task.Worktree, worktreeRef)
	}
}

func TestRecoverTask_ReviewingTask_BothWorktreeAndBranchMissingFailsClosed(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	reviewCommit := testhelpers.MustGit(t, tmpDir, "rev-parse", "integration")
	baseCommit := reviewCommit
	taskID := "task-review"
	reviewerID := "code-reviewer-1"
	now := time.Now().UTC()
	leaseExpires := now.Add(-10 * time.Minute)
	state := testhelpers.CreateValidState()
	state.Agents[reviewerID] = models.Agent{
		Role:         "code-reviewer",
		Status:       models.AgentStatusReviewing,
		CurrentTask:  &taskID,
		LeaseExpires: &leaseExpires,
	}
	state.Tasks = []models.Task{{
		ID:                 taskID,
		Description:        "review candidate",
		Status:             models.TaskStatusReviewing,
		RolePair:           "coding-pair",
		Priority:           1,
		ReviewingBy:        &reviewerID,
		ReviewLeaseExpires: &leaseExpires,
		ReviewCommit:       &reviewCommit,
		BaseCommit:         &baseCommit,
		SpecRef:            "spec.md",
		DoneWhen:           "tests pass",
		Scope:              "small",
	}}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := RecoverTask(tmpDir, taskID, false, "missing candidate")
	if err == nil {
		t.Fatal("RecoverTask() error = nil, want fail-closed missing candidate error")
	}
	if !strings.Contains(err.Error(), "submitted candidate is unrecoverable") {
		t.Fatalf("RecoverTask() error = %q, want submitted candidate unrecoverable", err.Error())
	}
}

func TestRecoverTask_ImplementingTask_BothWorktreeAndBranchMissingReleasesToInitial(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	taskID := "task-impl"
	doerID := "coder-1"
	now := time.Now().UTC()
	leaseExpires := now.Add(-10 * time.Minute)
	state := testhelpers.CreateValidState()
	state.Agents[doerID] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusWorking,
		CurrentTask:  &taskID,
		LeaseExpires: &leaseExpires,
		PID:          999999,
	}
	state.Tasks = []models.Task{{
		ID:           taskID,
		Description:  "implementation",
		Status:       models.TaskStatusImplementing,
		RolePair:     "coding-pair",
		Priority:     1,
		AssignedTo:   &doerID,
		LeaseExpires: &leaseExpires,
		SpecRef:      "spec.md",
		DoneWhen:     "tests pass",
		Scope:        "small",
	}}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RecoverTask(tmpDir, taskID, false, "missing implementation worktree")
	if err != nil {
		t.Fatalf("RecoverTask() error: %v", err)
	}
	if result.PreservedWorktree {
		t.Error("PreservedWorktree = true, want false")
	}
	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	task := readState.FindTask(taskID)
	if task.Status != models.TaskStatusReady {
		t.Fatalf("Status = %s, want %s", task.Status, models.TaskStatusReady)
	}
	if task.Worktree != nil {
		t.Fatalf("Worktree = %v, want nil for next fresh claim", task.Worktree)
	}
}

func TestRecoverTaskFresh_ReviewingTaskResetsInitialAndClearsReviewMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	gitWrapper := git.New(tmpDir)
	baseCommit, err := gitWrapper.CreateWorktree("task-review", "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	reviewCommit, err := gitWrapper.GetWorktreeHEAD("task-review")
	if err != nil {
		t.Fatalf("GetWorktreeHEAD() error: %v", err)
	}

	taskID := "task-review"
	reviewerID := "code-reviewer-1"
	now := time.Now().UTC()
	leaseExpires := now.Add(-10 * time.Minute)
	worktreeRef := ".worktrees/task-review"
	state := testhelpers.CreateValidState()
	state.Agents[reviewerID] = models.Agent{
		Role:         "code-reviewer",
		Status:       models.AgentStatusReviewing,
		CurrentTask:  &taskID,
		LeaseExpires: &leaseExpires,
	}
	state.Tasks = []models.Task{{
		ID:                 taskID,
		Description:        "review candidate",
		Status:             models.TaskStatusReviewing,
		RolePair:           "coding-pair",
		Priority:           1,
		ReviewingBy:        &reviewerID,
		ReviewLeaseExpires: &leaseExpires,
		ReviewCommit:       &reviewCommit,
		BaseCommit:         &baseCommit,
		Worktree:           &worktreeRef,
		SpecRef:            "spec.md",
		DoneWhen:           "tests pass",
		Scope:              "small",
		Output:             []models.OutputEntry{{Desc: "submitted", DoneWhen: "done", Scope: "scope", SpecRef: "README.md"}},
	}}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RecoverTaskWithOptions(tmpDir, taskID, "discard stale candidate", RecoverTaskOptions{Fresh: true})
	if err != nil {
		t.Fatalf("RecoverTaskWithOptions() error: %v", err)
	}
	if !result.FreshReset {
		t.Error("FreshReset = false, want true")
	}
	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	task := readState.FindTask(taskID)
	if task.Status != models.TaskStatusReady {
		t.Fatalf("Status = %s, want %s", task.Status, models.TaskStatusReady)
	}
	if task.ReviewCommit != nil {
		t.Fatalf("ReviewCommit = %v, want nil", *task.ReviewCommit)
	}
	if len(task.Output) != 0 {
		t.Fatalf("Output = %v, want cleared", task.Output)
	}
	if task.Worktree == nil || *task.Worktree != worktreeRef {
		t.Fatalf("Worktree = %v, want %s", task.Worktree, worktreeRef)
	}
}

func TestRecoverTaskFresh_LiveClaimRequiresForce(t *testing.T) {
	tmpDir, _ := setupImplementingTask(t, os.Getpid())

	_, err := RecoverTaskWithOptions(tmpDir, "task-1", "discard active work", RecoverTaskOptions{Fresh: true})
	if err == nil {
		t.Fatal("RecoverTaskWithOptions() error = nil, want live PID refusal")
	}
	if !strings.Contains(err.Error(), "--fresh --force") {
		t.Fatalf("RecoverTaskWithOptions() error = %q, want --fresh --force hint", err.Error())
	}
}

func TestRecoverTask_PreserveRejectsClaimDriftBeforeRelease(t *testing.T) {
	tmpDir, stateFile := setupImplementingTask(t, 999999)

	taskID := "task-1"
	nextDoerID := "coder-2"
	testRecoverTaskHooks = &recoverTaskTestHooks{
		beforePreserveModify: func() {
			err := db.New(stateFile).Modify(func(state *models.State) error {
				leaseExpires := time.Now().UTC().Add(30 * time.Minute)
				state.Agents[nextDoerID] = models.Agent{
					Role:         "coder",
					Status:       models.AgentStatusWorking,
					CurrentTask:  &taskID,
					LeaseExpires: &leaseExpires,
					PID:          0,
				}
				task := state.FindTask(taskID)
				task.AssignedTo = &nextDoerID
				task.LeaseExpires = &leaseExpires
				return nil
			})
			if err != nil {
				panic(err)
			}
		},
	}
	defer func() { testRecoverTaskHooks = nil }()

	_, err := RecoverTask(tmpDir, taskID, false, "stale recovery snapshot")
	if err == nil {
		t.Fatal("RecoverTask() error = nil, want race condition")
	}
	if !strings.Contains(err.Error(), "assigned_to changed during recover-task") {
		t.Fatalf("RecoverTask() error = %q, want assigned_to drift", err.Error())
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	task := readState.FindTask(taskID)
	if task.AssignedTo == nil || *task.AssignedTo != nextDoerID {
		t.Fatalf("AssignedTo = %v, want %s", task.AssignedTo, nextDoerID)
	}
	if _, exists := readState.Agents[nextDoerID]; !exists {
		t.Fatalf("agent %s should remain registered", nextDoerID)
	}
}

func TestRecoverTaskFresh_RejectsStateDriftBeforeGitCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	gitWrapper := git.New(tmpDir)
	baseCommit, err := gitWrapper.CreateWorktree("task-review", "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	reviewCommit, err := gitWrapper.GetWorktreeHEAD("task-review")
	if err != nil {
		t.Fatalf("GetWorktreeHEAD() error: %v", err)
	}

	taskID := "task-review"
	worktreeRef := ".worktrees/task-review"
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{{
		ID:           taskID,
		Description:  "review candidate",
		Status:       models.TaskStatusReadyForReview,
		RolePair:     "coding-pair",
		Priority:     1,
		ReviewCommit: &reviewCommit,
		BaseCommit:   &baseCommit,
		Worktree:     &worktreeRef,
		SpecRef:      "spec.md",
		DoneWhen:     "tests pass",
		Scope:        "small",
	}}
	testhelpers.WriteInitialState(t, stateFile, state)

	testRecoverTaskHooks = &recoverTaskTestHooks{
		beforeFreshModify: func() {
			err := db.New(stateFile).Modify(func(state *models.State) error {
				task := state.FindTask(taskID)
				task.Status = models.TaskStatusMerged
				return nil
			})
			if err != nil {
				panic(err)
			}
		},
	}
	defer func() { testRecoverTaskHooks = nil }()

	_, err = RecoverTaskWithOptions(tmpDir, taskID, "stale fresh reset", RecoverTaskOptions{Fresh: true})
	if err == nil {
		t.Fatal("RecoverTaskWithOptions() error = nil, want race condition")
	}
	if !strings.Contains(err.Error(), "status changed") {
		t.Fatalf("RecoverTaskWithOptions() error = %q, want status drift", err.Error())
	}
	if _, err := os.Stat(filepath.Join(tmpDir, ".worktrees", taskID)); err != nil {
		t.Fatalf("worktree should remain after rejected fresh reset: %v", err)
	}
	branchExists, err := gitWrapper.BranchExists("task/" + taskID)
	if err != nil {
		t.Fatalf("BranchExists() error: %v", err)
	}
	if !branchExists {
		t.Fatal("task branch should remain after rejected fresh reset")
	}
}

func TestRecoverTaskFresh_CreateFailureAfterCleanupBlocksWithTruthfulState(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	gitWrapper := git.New(tmpDir)
	baseCommit, err := gitWrapper.CreateWorktree("task-review", "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	reviewCommit, err := gitWrapper.GetWorktreeHEAD("task-review")
	if err != nil {
		t.Fatalf("GetWorktreeHEAD() error: %v", err)
	}

	taskID := "task-review"
	reviewerID := "code-reviewer-1"
	worktreeRef := ".worktrees/task-review"
	state := testhelpers.CreateValidState()
	state.Agents[reviewerID] = models.Agent{
		Role:        "code-reviewer",
		Status:      models.AgentStatusReviewing,
		CurrentTask: &taskID,
	}
	state.Tasks = []models.Task{{
		ID:           taskID,
		Description:  "review candidate",
		Status:       models.TaskStatusReviewing,
		RolePair:     "coding-pair",
		Priority:     1,
		ReviewingBy:  &reviewerID,
		ReviewCommit: &reviewCommit,
		BaseCommit:   &baseCommit,
		Worktree:     &worktreeRef,
		SpecRef:      "spec.md",
		DoneWhen:     "tests pass",
		Scope:        "small",
	}}
	testhelpers.WriteInitialState(t, stateFile, state)

	testRecoverTaskHooks = &recoverTaskTestHooks{
		beforeFreshCreate: func() {
			err := os.MkdirAll(filepath.Join(tmpDir, ".worktrees", taskID), 0755)
			if err != nil {
				panic(err)
			}
		},
	}
	defer func() { testRecoverTaskHooks = nil }()

	_, err = RecoverTaskWithOptions(tmpDir, taskID, "fresh creation race", RecoverTaskOptions{Fresh: true})
	if err == nil {
		t.Fatal("RecoverTaskWithOptions() error = nil, want create failure")
	}
	if !strings.Contains(err.Error(), "task marked BLOCKED") {
		t.Fatalf("RecoverTaskWithOptions() error = %q, want blocked repair state note", err.Error())
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Read state: %v", err)
	}
	task := readState.FindTask(taskID)
	if task.Status != models.TaskStatusBlocked {
		t.Fatalf("Status = %s, want BLOCKED", task.Status)
	}
	if task.Worktree != nil {
		t.Fatalf("Worktree = %v, want nil after failed fresh creation", *task.Worktree)
	}
	if task.BaseCommit != nil {
		t.Fatalf("BaseCommit = %v, want nil after failed fresh creation", *task.BaseCommit)
	}
	if task.ReviewCommit != nil {
		t.Fatalf("ReviewCommit = %v, want nil after failed fresh creation", *task.ReviewCommit)
	}
	if task.ReviewingBy != nil {
		t.Fatalf("ReviewingBy = %v, want nil after failed fresh creation", *task.ReviewingBy)
	}
	if task.BlockedReason == nil || !strings.Contains(*task.BlockedReason, "recover-task --fresh failed after deleting git artifacts") {
		t.Fatalf("BlockedReason = %v, want fresh failure reason", task.BlockedReason)
	}
	if _, exists := readState.Agents[reviewerID]; exists {
		t.Fatalf("agent %s should be removed after destructive fresh cleanup", reviewerID)
	}
	branchExists, err := gitWrapper.BranchExists("task/" + taskID)
	if err != nil {
		t.Fatalf("BranchExists() error: %v", err)
	}
	if branchExists {
		t.Fatal("task branch should be absent after failed fresh cleanup")
	}
}

func TestRecoverTaskFresh_CleanupFailureLeavesStateAndArtifactsUnchanged(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	gitWrapper := git.New(tmpDir)
	baseCommit, err := gitWrapper.CreateWorktree("task-review", "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	reviewCommit, err := gitWrapper.GetWorktreeHEAD("task-review")
	if err != nil {
		t.Fatalf("GetWorktreeHEAD() error: %v", err)
	}

	taskID := "task-review"
	reviewerID := "code-reviewer-1"
	worktreeRef := ".worktrees/task-review"
	state := testhelpers.CreateValidState()
	state.Agents[reviewerID] = models.Agent{
		Role:        "code-reviewer",
		Status:      models.AgentStatusReviewing,
		CurrentTask: &taskID,
	}
	state.Tasks = []models.Task{{
		ID:           taskID,
		Description:  "review candidate",
		Status:       models.TaskStatusReviewing,
		RolePair:     "coding-pair",
		Priority:     1,
		ReviewingBy:  &reviewerID,
		ReviewCommit: &reviewCommit,
		BaseCommit:   &baseCommit,
		Worktree:     &worktreeRef,
		SpecRef:      "spec.md",
		DoneWhen:     "tests pass",
		Scope:        "small",
	}}
	testhelpers.WriteInitialState(t, stateFile, state)

	testRecoverTaskHooks = &recoverTaskTestHooks{
		cleanupGitArtifacts: func() error {
			return errors.New("simulated cleanup failure")
		},
	}
	defer func() { testRecoverTaskHooks = nil }()

	_, err = RecoverTaskWithOptions(tmpDir, taskID, "fresh cleanup failure", RecoverTaskOptions{Fresh: true})
	if err == nil {
		t.Fatal("RecoverTaskWithOptions() error = nil, want cleanup failure")
	}
	if !strings.Contains(err.Error(), "fresh reset cleanup failed") {
		t.Fatalf("RecoverTaskWithOptions() error = %q, want cleanup failure", err.Error())
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Read state: %v", err)
	}
	task := readState.FindTask(taskID)
	if task.Status != models.TaskStatusReviewing {
		t.Fatalf("Status = %s, want REVIEWING_CODE", task.Status)
	}
	if task.Worktree == nil || *task.Worktree != worktreeRef {
		t.Fatalf("Worktree = %v, want %s", task.Worktree, worktreeRef)
	}
	if task.BaseCommit == nil || *task.BaseCommit != baseCommit {
		t.Fatalf("BaseCommit = %v, want %s", task.BaseCommit, baseCommit)
	}
	if task.ReviewCommit == nil || *task.ReviewCommit != reviewCommit {
		t.Fatalf("ReviewCommit = %v, want %s", task.ReviewCommit, reviewCommit)
	}
	if task.ReviewingBy == nil || *task.ReviewingBy != reviewerID {
		t.Fatalf("ReviewingBy = %v, want %s", task.ReviewingBy, reviewerID)
	}
	if _, exists := readState.Agents[reviewerID]; !exists {
		t.Fatalf("agent %s should remain after failed cleanup", reviewerID)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, ".worktrees", taskID)); err != nil {
		t.Fatalf("worktree should remain after failed cleanup: %v", err)
	}
	branchExists, err := gitWrapper.BranchExists("task/" + taskID)
	if err != nil {
		t.Fatalf("BranchExists() error: %v", err)
	}
	if !branchExists {
		t.Fatal("task branch should remain after failed cleanup")
	}
}

func TestRecoverTask_ReviewingTask_DirtyWorktreeFailsClosed(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	gitWrapper := git.New(tmpDir)
	baseCommit, err := gitWrapper.CreateWorktree("task-review", "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	reviewCommit, err := gitWrapper.GetWorktreeHEAD("task-review")
	if err != nil {
		t.Fatalf("GetWorktreeHEAD() error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".worktrees", "task-review", "unsubmitted.txt"), []byte("local edit\n"), 0644); err != nil {
		t.Fatalf("Write dirty file: %v", err)
	}

	taskID := "task-review"
	reviewerID := "code-reviewer-1"
	now := time.Now().UTC()
	leaseExpires := now.Add(-10 * time.Minute)
	worktreeRef := ".worktrees/task-review"
	state := testhelpers.CreateValidState()
	state.Agents[reviewerID] = models.Agent{
		Role:         "code-reviewer",
		Status:       models.AgentStatusReviewing,
		CurrentTask:  &taskID,
		LeaseExpires: &leaseExpires,
	}
	state.Tasks = []models.Task{{
		ID:                 taskID,
		Description:        "review candidate",
		Status:             models.TaskStatusReviewing,
		RolePair:           "coding-pair",
		Priority:           1,
		ReviewingBy:        &reviewerID,
		ReviewLeaseExpires: &leaseExpires,
		ReviewCommit:       &reviewCommit,
		BaseCommit:         &baseCommit,
		Worktree:           &worktreeRef,
		SpecRef:            "spec.md",
		DoneWhen:           "tests pass",
		Scope:              "small",
	}}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err = RecoverTask(tmpDir, taskID, false, "dirty reviewer worktree")
	if err == nil {
		t.Fatal("RecoverTask() error = nil, want dirty worktree refusal")
	}
	if !strings.Contains(err.Error(), "preserved worktree is dirty") {
		t.Fatalf("RecoverTask() error = %q, want dirty worktree refusal", err.Error())
	}
}

func TestRecoverTask_BlockedTaskPreservesBlockedStateAndWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	gitWrapper := git.New(tmpDir)
	baseCommit, err := gitWrapper.CreateWorktree("task-blocked", "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}

	taskID := "task-blocked"
	doerID := "coder-1"
	now := time.Now().UTC()
	leaseExpires := now.Add(-10 * time.Minute)
	worktreeRef := ".worktrees/task-blocked"
	blockedReason := "needs operator decision"
	blockedQuestions := []string{"choose direction"}
	state := testhelpers.CreateValidState()
	state.Agents[doerID] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusWorking,
		CurrentTask:  &taskID,
		LeaseExpires: &leaseExpires,
		PID:          999999,
	}
	state.Tasks = []models.Task{{
		ID:               taskID,
		Description:      "blocked work",
		Status:           models.TaskStatusBlocked,
		RolePair:         "coding-pair",
		Priority:         1,
		AssignedTo:       &doerID,
		LeaseExpires:     &leaseExpires,
		Worktree:         &worktreeRef,
		BaseCommit:       &baseCommit,
		BlockedReason:    &blockedReason,
		BlockedQuestions: blockedQuestions,
		SpecRef:          "spec.md",
		DoneWhen:         "tests pass",
		Scope:            "small",
	}}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RecoverTask(tmpDir, taskID, false, "recover blocked substrate")
	if err != nil {
		t.Fatalf("RecoverTask() error: %v", err)
	}
	if !result.PreservedWorktree {
		t.Error("PreservedWorktree = false, want true")
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	task := readState.FindTask(taskID)
	if task.Status != models.TaskStatusBlocked {
		t.Fatalf("Status = %s, want %s", task.Status, models.TaskStatusBlocked)
	}
	if task.AssignedTo != nil {
		t.Fatalf("AssignedTo = %v, want nil", task.AssignedTo)
	}
	if task.BlockedReason == nil || *task.BlockedReason != blockedReason {
		t.Fatalf("BlockedReason = %v, want %q", task.BlockedReason, blockedReason)
	}
	if len(task.BlockedQuestions) != 1 || task.BlockedQuestions[0] != blockedQuestions[0] {
		t.Fatalf("BlockedQuestions = %v, want %v", task.BlockedQuestions, blockedQuestions)
	}
	if task.Worktree == nil || *task.Worktree != worktreeRef {
		t.Fatalf("Worktree = %v, want %s", task.Worktree, worktreeRef)
	}
}

func TestRecoverTaskFresh_BlockedTaskPreservesBlockedStateAndReason(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	gitWrapper := git.New(tmpDir)
	baseCommit, err := gitWrapper.CreateWorktree("task-blocked", "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	reviewCommit, err := gitWrapper.GetWorktreeHEAD("task-blocked")
	if err != nil {
		t.Fatalf("GetWorktreeHEAD() error: %v", err)
	}

	taskID := "task-blocked"
	blockedReason := "hypothesis exhausted"
	blockedQuestions := []string{"rescope?"}
	worktreeRef := ".worktrees/task-blocked"
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{{
		ID:               taskID,
		Description:      "blocked work",
		Status:           models.TaskStatusBlocked,
		RolePair:         "coding-pair",
		Priority:         1,
		Worktree:         &worktreeRef,
		BaseCommit:       &baseCommit,
		ReviewCommit:     &reviewCommit,
		Approvals:        []models.Approval{{Agent: "code-reviewer-1", Provider: "codex", Timestamp: time.Now().UTC()}},
		Output:           []models.OutputEntry{{Desc: "context", DoneWhen: "done", Scope: "scope", SpecRef: "README.md"}},
		FailedBy:         []string{"coder-1", "coder-2"},
		BlockedReason:    &blockedReason,
		BlockedQuestions: blockedQuestions,
		SpecRef:          "spec.md",
		DoneWhen:         "tests pass",
		Scope:            "small",
	}}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RecoverTaskWithOptions(tmpDir, taskID, "discard substrate but stay blocked", RecoverTaskOptions{Fresh: true})
	if err != nil {
		t.Fatalf("RecoverTaskWithOptions() error: %v", err)
	}
	if !result.FreshReset {
		t.Error("FreshReset = false, want true")
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	task := readState.FindTask(taskID)
	if task.Status != models.TaskStatusBlocked {
		t.Fatalf("Status = %s, want %s", task.Status, models.TaskStatusBlocked)
	}
	if task.BlockedReason == nil || *task.BlockedReason != blockedReason {
		t.Fatalf("BlockedReason = %v, want %q", task.BlockedReason, blockedReason)
	}
	if len(task.BlockedQuestions) != 1 || task.BlockedQuestions[0] != blockedQuestions[0] {
		t.Fatalf("BlockedQuestions = %v, want %v", task.BlockedQuestions, blockedQuestions)
	}
	if task.ReviewCommit != nil {
		t.Fatalf("ReviewCommit = %v, want nil", *task.ReviewCommit)
	}
	if len(task.Approvals) != 0 {
		t.Fatalf("Approvals = %v, want cleared", task.Approvals)
	}
	if len(task.Output) != 1 {
		t.Fatalf("Output = %v, want preserved blocked context", task.Output)
	}
	if len(task.FailedBy) != 2 {
		t.Fatalf("FailedBy = %v, want preserved blocked diagnostics", task.FailedBy)
	}
	if task.Worktree == nil || *task.Worktree != worktreeRef {
		t.Fatalf("Worktree = %v, want %s", task.Worktree, worktreeRef)
	}
}

func TestRecoverTask_IntegrationFailed_BothWorktreeAndBranchMissingFailsClosed(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	taskID := "task-integration-failed"
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{{
		ID:          taskID,
		Description: "integration failed",
		Status:      models.TaskStatusIntegrationFailed,
		RolePair:    "coding-pair",
		Priority:    1,
		SpecRef:     "spec.md",
		DoneWhen:    "tests pass",
		Scope:       "small",
	}}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := RecoverTask(tmpDir, taskID, false, "missing integration repair substrate")
	if err == nil {
		t.Fatal("RecoverTask() error = nil, want fail-closed integration repair error")
	}
	if !strings.Contains(err.Error(), "integration-failed repair substrate is unrecoverable") {
		t.Fatalf("RecoverTask() error = %q, want integration repair substrate error", err.Error())
	}
}
