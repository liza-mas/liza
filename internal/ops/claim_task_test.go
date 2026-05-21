package ops

import (
	stderrors "errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestClaimTask_Validation(t *testing.T) {
	tests := []struct {
		name        string
		taskID      string
		agentID     string
		errContains string
	}{
		{
			name:        "empty task ID",
			taskID:      "",
			agentID:     "coder-1",
			errContains: "task ID is required",
		},
		{
			name:        "empty agent ID",
			taskID:      "task-1",
			agentID:     "",
			errContains: "agent ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ClaimTask("/nonexistent", tt.taskID, tt.agentID)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("Error = %q, want to contain %q", err.Error(), tt.errContains)
			}
		})
	}
}

func TestClaimTask_ReadyTask(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}

	// Verify result fields
	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}
	if result.AgentID != "coder-1" {
		t.Errorf("AgentID = %q, want %q", result.AgentID, "coder-1")
	}
	if result.SourceStatus != models.TaskStatusReady {
		t.Errorf("SourceStatus = %v, want READY", result.SourceStatus)
	}
	if result.BaseCommit == "" {
		t.Error("BaseCommit should not be empty")
	}
	if result.IntegrationFix {
		t.Error("IntegrationFix should be false for READY task")
	}

	// Verify state updated
	readState := readClaimStateForTest(t, stateFile)
	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("Task not found in state")
	}
	if task.Status != models.TaskStatusImplementing {
		t.Errorf("Task status = %v, want IMPLEMENTING", task.Status)
	}
	if task.AssignedTo == nil || *task.AssignedTo != "coder-1" {
		t.Error("AssignedTo should be coder-1")
	}
	if task.Iteration != 1 {
		t.Errorf("Iteration = %d, want 1", task.Iteration)
	}
	if task.Worktree == nil {
		t.Error("Worktree should be set")
	}

	// Verify worktree was created on disk
	wtDir := filepath.Join(tmpDir, ".worktrees", "task-1")
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		t.Errorf("Worktree directory should exist at %s", wtDir)
	}

	// Verify agent registered
	agent, exists := readState.Agents["coder-1"]
	if !exists {
		t.Fatal("Agent not found in state")
	}
	if agent.CurrentTask == nil || *agent.CurrentTask != "task-1" {
		t.Error("Agent CurrentTask should be task-1")
	}
	if agent.Status != models.AgentStatusWorking {
		t.Errorf("Agent Status = %v, want working", agent.Status)
	}
}

func TestClaimTask_MissingRegisteredAgentDoesNotCreateGhost(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err == nil {
		t.Fatal("Expected error for missing registered agent")
	}
	if !strings.Contains(err.Error(), "agent coder-1 is not registered") {
		t.Errorf("Error = %q, want missing registered agent", err.Error())
	}

	readState := readClaimStateForTest(t, stateFile)
	if _, exists := readState.Agents["coder-1"]; exists {
		t.Fatal("ClaimTask created a ghost agent row")
	}
	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("Task not found in state")
	}
	if task.AssignedTo != nil {
		t.Fatal("Task should not be assigned after rejected claim")
	}
}

func TestClaimTask_CorruptRegisteredAgentRejected(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*models.Agent)
		errContains string
	}{
		{
			name:        "empty role",
			mutate:      func(agent *models.Agent) { agent.Role = "" },
			errContains: "has no registered role",
		},
		{
			name:        "empty provider",
			mutate:      func(agent *models.Agent) { agent.Provider = "" },
			errContains: "has no registered provider",
		},
		{
			name:        "zero pid",
			mutate:      func(agent *models.Agent) { agent.PID = 0 },
			errContains: "has no registered process PID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testhelpers.SetupTestGitRepo(t, tmpDir)
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

			now := time.Now().UTC()
			state := testhelpers.CreateValidState()
			agent := testhelpers.RegisteredTestAgent("coder")
			tt.mutate(&agent)
			state.Agents["coder-1"] = agent
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
			}
			testhelpers.WriteInitialState(t, stateFile, state)

			_, err := ClaimTask(tmpDir, "task-1", "coder-1")
			if err == nil {
				t.Fatal("Expected error for corrupt registered agent")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("Error = %q, want to contain %q", err.Error(), tt.errContains)
			}

			readState := readClaimStateForTest(t, stateFile)
			task := readState.FindTask("task-1")
			if task == nil {
				t.Fatal("Task not found in state")
			}
			if task.AssignedTo != nil {
				t.Fatal("Task should not be assigned after rejected claim")
			}
		})
	}
}

func TestClaimTask_TaskNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimTask(tmpDir, "nonexistent", "coder-1")
	if err == nil {
		t.Fatal("Expected error for nonexistent task")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestClaimTask_WrongStatus(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimTask(tmpDir, "task-1", "coder-2")
	if err == nil {
		t.Fatal("Expected error for IMPLEMENTING task")
	}
	if !strings.Contains(err.Error(), "not claimable by") {
		t.Errorf("Error = %q, want to contain 'not claimable by'", err.Error())
	}
}

func TestClaimTask_AgentBusy(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	// Agent is busy with another task
	otherTask := "task-other"
	busyAgent := testhelpers.RegisteredTestAgent("coder")
	busyAgent.Status = models.AgentStatusWorking
	busyAgent.CurrentTask = &otherTask
	state.Agents["coder-1"] = busyAgent
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err == nil {
		t.Fatal("Expected error for busy agent")
	}
	if !strings.Contains(err.Error(), "already working") {
		t.Errorf("Error = %q, want to contain 'already working'", err.Error())
	}
}

func TestClaimTask_UnmetDependencies(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	depTask := testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusReady, now)
	mainTask := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
	mainTask.DependsOn = []string{"dep-1"}
	state.Tasks = []models.Task{depTask, mainTask}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err == nil {
		t.Fatal("Expected error for unmet dependencies")
	}
	if !strings.Contains(err.Error(), "unmet dependencies") {
		t.Errorf("Error = %q, want to contain 'unmet dependencies'", err.Error())
	}
}

func TestClaimTask_RejectsStaleSupersededDependencyEvenWithMergedReplacement(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	depTask := testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusSuperseded, now)
	depTask.SupersededBy = []string{"dep-2"}
	replacement := testhelpers.BuildTaskByStatus("dep-2", models.TaskStatusMerged, now)
	mainTask := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
	mainTask.DependsOn = []string{"dep-1"}
	state.Tasks = []models.Task{depTask, replacement, mainTask}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err == nil {
		t.Fatal("Expected error for stale superseded dependency")
	}
	if !strings.Contains(err.Error(), "unsatisfied_superseded") {
		t.Errorf("Error = %q, want unsatisfied_superseded", err.Error())
	}
}

func TestClaimTask_MetDependencies(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	depTask := testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusMerged, now)
	mainTask := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
	mainTask.DependsOn = []string{"dep-1"}
	state.Tasks = []models.Task{depTask, mainTask}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}
}

func TestUnmetDependencies(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name      string
		dependsOn []string
		tasks     []models.Task
		want      []string
	}{
		{
			name:      "no dependencies",
			dependsOn: nil,
			tasks:     nil,
			want:      nil,
		},
		{
			name:      "all dependencies merged",
			dependsOn: []string{"dep-1", "dep-2"},
			tasks: []models.Task{
				testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusMerged, now),
				testhelpers.BuildTaskByStatus("dep-2", models.TaskStatusMerged, now),
			},
			want: nil,
		},
		{
			name:      "includes missing and non-merged dependencies",
			dependsOn: []string{"dep-1", "dep-missing", "dep-2"},
			tasks: []models.Task{
				testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusMerged, now),
				testhelpers.BuildTaskByStatus("dep-2", models.TaskStatusReady, now),
			},
			want: []string{
				"dep-missing (invalid_missing via dep-missing); blocking: dep-missing",
				"dep-2 (unsatisfied_pending via dep-2); blocking: dep-2",
			},
		},
		{
			name:      "superseded dependency with merged replacement remains unmet until edge is rewritten",
			dependsOn: []string{"dep-1"},
			tasks: []models.Task{
				func() models.Task {
					task := testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusSuperseded, now)
					task.SupersededBy = []string{"dep-2"}
					return task
				}(),
				testhelpers.BuildTaskByStatus("dep-2", models.TaskStatusMerged, now),
			},
			want: []string{
				"dep-1 (unsatisfied_superseded via dep-1); blocking: dep-1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			registerClaimTaskTestAgents(state)
			state.Tasks = append([]models.Task(nil), tt.tasks...)

			task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
			task.DependsOn = tt.dependsOn

			got := unmetDependencies(&task, state)
			gotSummaries := make([]string, 0, len(got))
			for _, dep := range got {
				gotSummaries = append(gotSummaries, dep.Summary())
			}
			if !slices.Equal(gotSummaries, tt.want) {
				t.Errorf("unmetDependencies() = %v, want %v", gotSummaries, tt.want)
			}
		})
	}
}

func TestClaimTask_IntegrationFailed(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusIntegrationFailed, now)
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Create a real git worktree (Phase 3 validates .git link file)
	testhelpers.CreateTestWorktree(t, tmpDir, "task-1")

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}

	if !result.IntegrationFix {
		t.Error("IntegrationFix should be true for INTEGRATION_FAILED task")
	}
	if result.SourceStatus != models.TaskStatusIntegrationFailed {
		t.Errorf("SourceStatus = %v, want INTEGRATION_FAILED", result.SourceStatus)
	}

	// Verify task state
	readState := readClaimStateForTest(t, stateFile)
	claimedTask := readState.FindTask("task-1")
	if claimedTask == nil {
		t.Fatal("Task not found")
	}
	if !claimedTask.IntegrationFix {
		t.Error("IntegrationFix flag should be set in state")
	}
}

// TestClaimTask_RejectedWorktreePresent_Preserved verifies that when a REJECTED
// task has both worktree dir and branch present, claiming preserves the worktree
// regardless of coder identity (different coder does NOT trigger teardown+recreate).
func TestClaimTask_RejectedWorktreePresent_Preserved(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	// task.AssignedTo is "coder-1" from BuildTaskByStatus
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Create worktree with diverged content (simulating prior rejected work).
	gitWrapper := git.New(tmpDir)
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("Failed to create initial rejected worktree: %v", err)
	}
	wtDir := filepath.Join(tmpDir, ".worktrees", "task-1")
	changesFile := filepath.Join(wtDir, "rejected-change.txt")
	if err := os.WriteFile(changesFile, []byte("rejected change\n"), 0644); err != nil {
		t.Fatalf("Failed to write rejected-change.txt: %v", err)
	}
	if err := exec.Command("git", "-C", wtDir, "add", "rejected-change.txt").Run(); err != nil {
		t.Fatalf("Failed to add rejected worktree file: %v", err)
	}
	if err := exec.Command("git", "-C", wtDir, "commit", "-m", "Rejected work").Run(); err != nil {
		t.Fatalf("Failed to commit rejected worktree file: %v", err)
	}
	oldBranchSHA, err := gitWrapper.GetCommitSHA("task/task-1")
	if err != nil {
		t.Fatalf("Failed to read old task branch SHA: %v", err)
	}

	// Different coder claims — worktree should be preserved (identity-free).
	result, err := ClaimTask(tmpDir, "task-1", "coder-2")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}

	if result.PreviousAssignee != "coder-1" {
		t.Errorf("PreviousAssignee = %q, want %q", result.PreviousAssignee, "coder-1")
	}
	if result.WorktreeRecreated {
		t.Error("WorktreeRecreated should be false — worktree preserved regardless of coder identity")
	}

	// Verify the worktree retains the prior rejected work (branch SHA unchanged).
	currentBranchSHA, err := gitWrapper.GetCommitSHA("task/task-1")
	if err != nil {
		t.Fatalf("Failed to read task branch SHA after claim: %v", err)
	}
	if currentBranchSHA != oldBranchSHA {
		t.Errorf("Task branch SHA changed from %s to %s — worktree should be preserved", oldBranchSHA, currentBranchSHA)
	}

	readState := readClaimStateForTest(t, stateFile)
	claimedTask := readState.FindTask("task-1")
	if claimedTask == nil {
		t.Fatal("Task not found in state")
	}
	if claimedTask.Status != models.TaskStatusImplementing {
		t.Errorf("Task status = %v, want IMPLEMENTING", claimedTask.Status)
	}
	if claimedTask.AssignedTo == nil || *claimedTask.AssignedTo != "coder-2" {
		t.Errorf("AssignedTo = %v, want coder-2", claimedTask.AssignedTo)
	}
}

// TestClaimTask_RejectedWorktreeMissing_Recreated verifies that when a REJECTED
// task's worktree directory is absent, ClaimTask recreates it from integration.
func TestClaimTask_RejectedWorktreeMissing_Recreated(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	// No worktree created on disk — simulates worktree lost after cleanup or crash.
	integrationSHA, err := git.New(tmpDir).GetCommitSHA("integration")
	if err != nil {
		t.Fatalf("Failed to read integration SHA: %v", err)
	}

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}

	// Worktree should be freshly created from integration.
	wtDir := filepath.Join(tmpDir, ".worktrees", "task-1")
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		t.Fatal("Worktree should exist after claim")
	}

	gitWrapper := git.New(tmpDir)
	worktreeHead, err := gitWrapper.GetWorktreeHEAD("task-1")
	if err != nil {
		t.Fatalf("Expected valid worktree HEAD, got error: %v", err)
	}
	if worktreeHead != integrationSHA {
		t.Errorf("Worktree HEAD = %s, want integration SHA %s", worktreeHead, integrationSHA)
	}

	if result.BaseCommit != integrationSHA {
		t.Errorf("BaseCommit = %s, want integration SHA %s", result.BaseCommit, integrationSHA)
	}
}

// TestClaimTask_RejectedWorktreeDirExistsBranchMissing_Recreated verifies that
// when the worktree directory exists but the task branch is missing, the orphaned
// directory is removed and the worktree is recreated from integration.
func TestClaimTask_RejectedWorktreeDirExistsBranchMissing_Recreated(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Create worktree normally, then forcibly delete the branch to simulate orphan.
	gitWrapper := git.New(tmpDir)
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("Failed to create initial worktree: %v", err)
	}

	// Remove the worktree tracking so we can delete the branch.
	wtDir := filepath.Join(tmpDir, ".worktrees", "task-1")
	if err := gitWrapper.RemoveWorktreeDir("task-1"); err != nil {
		t.Fatalf("Failed to remove worktree dir: %v", err)
	}
	// Recreate the directory as a plain dir (orphaned — no .git link, no branch).
	if err := os.MkdirAll(wtDir, 0755); err != nil {
		t.Fatalf("Failed to create orphaned worktree dir: %v", err)
	}
	// Delete the branch.
	if err := gitWrapper.DeleteBranch("task/task-1"); err != nil {
		t.Fatalf("Failed to delete task branch: %v", err)
	}

	// Verify precondition: dir exists, branch does not.
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		t.Fatal("Worktree dir should exist before claim")
	}
	branchExists, err := gitWrapper.BranchExists("task/task-1")
	if err != nil {
		t.Fatalf("Failed to check branch: %v", err)
	}
	if branchExists {
		t.Fatal("Branch should NOT exist before claim (orphaned dir scenario)")
	}

	integrationSHA, err := gitWrapper.GetCommitSHA("integration")
	if err != nil {
		t.Fatalf("Failed to read integration SHA: %v", err)
	}

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}

	// Worktree should be recreated from integration.
	worktreeHead, err := gitWrapper.GetWorktreeHEAD("task-1")
	if err != nil {
		t.Fatalf("Expected valid worktree HEAD after orphan recovery, got error: %v", err)
	}
	if worktreeHead != integrationSHA {
		t.Errorf("Worktree HEAD = %s, want integration SHA %s", worktreeHead, integrationSHA)
	}

	if result.BaseCommit != integrationSHA {
		t.Errorf("BaseCommit = %s, want integration SHA %s", result.BaseCommit, integrationSHA)
	}
}

// TestClaimTask_RejectedMutateTask_NoCounterReset verifies that ReviewCyclesCurrent
// is NOT reset when a different coder claims within the same attempt. The attempt —
// not the agent — is the resource boundary.
func TestClaimTask_RejectedMutateTask_NoCounterReset(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.ReviewCyclesCurrent = 3 // Non-zero — should be preserved on different-coder claim.
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Create worktree (required for claim to succeed).
	gitWrapper := git.New(tmpDir)
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}

	// Different coder claims — ReviewCyclesCurrent must NOT be reset.
	result, err := ClaimTask(tmpDir, "task-1", "coder-2")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	if result.PreviousAssignee != "coder-1" {
		t.Errorf("PreviousAssignee = %q, want %q", result.PreviousAssignee, "coder-1")
	}

	readState := readClaimStateForTest(t, stateFile)
	claimedTask := readState.FindTask("task-1")
	if claimedTask == nil {
		t.Fatal("Task not found in state")
	}
	if claimedTask.ReviewCyclesCurrent != 3 {
		t.Errorf("ReviewCyclesCurrent = %d, want 3 (should not reset on different-coder claim within same attempt)", claimedTask.ReviewCyclesCurrent)
	}
}

func TestClaimTask_RejectedAtIterationLimitTransitionsToBlocked(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Config.MaxCoderIterations = 3

	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.Iteration = 3
	task.Attempt = 2 // Attempt 2: iteration cap → BLOCKED (not new attempt)
	state.Tasks = []models.Task{task}

	taskRef := "task-1"
	agent := testhelpers.RegisteredTestAgent("coder")
	agent.CurrentTask = &taskRef
	state.Agents["coder-1"] = agent

	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err == nil {
		t.Fatal("Expected iteration-limit error")
	}
	if !strings.Contains(err.Error(), "transitioned to BLOCKED") {
		t.Errorf("Error = %q, want to contain 'transitioned to BLOCKED'", err.Error())
	}

	readState := readClaimStateForTest(t, stateFile)
	blockedTask := readState.FindTask("task-1")
	if blockedTask == nil {
		t.Fatal("Task not found in state")
	}
	if blockedTask.Status != models.TaskStatusBlocked {
		t.Errorf("Task status = %v, want BLOCKED", blockedTask.Status)
	}
	if blockedTask.AssignedTo != nil {
		t.Error("AssignedTo should be cleared when task is blocked")
	}
	if blockedTask.BlockedReason == nil || !strings.Contains(*blockedTask.BlockedReason, "max iterations") {
		t.Errorf("BlockedReason = %v, want max-iterations reason", blockedTask.BlockedReason)
	}
	if len(blockedTask.BlockedQuestions) == 0 {
		t.Error("BlockedQuestions should be populated")
	}

	agent = readState.Agents["coder-1"]
	if agent.Status != models.AgentStatusIdle {
		t.Errorf("Agent status = %v, want IDLE", agent.Status)
	}
	if agent.CurrentTask != nil {
		t.Error("Agent CurrentTask should be cleared after limit-based block")
	}
}

func TestClaimTask_ReadyWithStaleBranchAndWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Create a stale worktree and branch (simulating orphaned resources from
	// a previous claim that was released without cleanup).
	gitWrapper := git.New(tmpDir)
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("Failed to create stale worktree: %v", err)
	}

	// Verify stale resources exist before claim
	wtDir := filepath.Join(tmpDir, ".worktrees", "task-1")
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		t.Fatal("Stale worktree should exist before claim")
	}
	branchExists, err := gitWrapper.BranchExists("task/task-1")
	if err != nil {
		t.Fatalf("Failed to check branch: %v", err)
	}
	if !branchExists {
		t.Fatal("Stale branch should exist before claim")
	}

	// Claim should succeed despite stale resources
	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}

	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}
	if result.SourceStatus != models.TaskStatusReady {
		t.Errorf("SourceStatus = %v, want READY", result.SourceStatus)
	}

	// Verify worktree exists (freshly created)
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		t.Error("Worktree should exist after successful claim")
	}

	// Verify state is correct
	readState := readClaimStateForTest(t, stateFile)
	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusImplementing {
		t.Errorf("Status = %v, want IMPLEMENTING", task.Status)
	}
	if task.Worktree == nil {
		t.Error("Worktree should be set")
	}
}

func TestClaimTask_ReadyWithPrunableWorktreeRegistration_RecreatesCleanWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	gitWrapper := git.New(tmpDir)
	integrationSHA, err := gitWrapper.GetCommitSHA("integration")
	if err != nil {
		t.Fatalf("failed to read integration SHA: %v", err)
	}
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("failed to create stale worktree: %v", err)
	}

	// Simulate a bootstrap/cleanup failure that deleted the directory without
	// unregistering Git's worktree metadata. Git still considers the task branch
	// checked out until Liza cleans up the registration.
	wtDir := filepath.Join(tmpDir, ".worktrees", "task-1")
	if err := os.RemoveAll(wtDir); err != nil {
		t.Fatalf("failed to remove worktree directory: %v", err)
	}

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() should recover stale prunable worktree registration, got: %v", err)
	}

	worktreeHead, err := gitWrapper.GetWorktreeHEAD("task-1")
	if err != nil {
		t.Fatalf("expected valid recreated worktree HEAD, got: %v", err)
	}
	if worktreeHead != integrationSHA {
		t.Errorf("Worktree HEAD = %s, want integration SHA %s", worktreeHead, integrationSHA)
	}
	if result.BaseCommit != integrationSHA {
		t.Errorf("BaseCommit = %s, want integration SHA %s", result.BaseCommit, integrationSHA)
	}

	readState := readClaimStateForTest(t, stateFile)
	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("task not found")
	}
	if task.Status != models.TaskStatusImplementing {
		t.Errorf("Status = %v, want IMPLEMENTING", task.Status)
	}
	if task.Worktree == nil || *task.Worktree != filepath.Join(paths.WorktreesDirName, "task-1") {
		t.Errorf("Worktree = %v, want .worktrees/task-1", task.Worktree)
	}
}

func TestHandleReadyClaimWorktree_ConcurrentWinnerDoesNotDeleteWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	testhelpers.WriteInitialState(t, stateFile, state)
	bb := db.New(stateFile)

	gitWrapper := git.New(tmpDir)
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("Failed to create winning worktree: %v", err)
	}

	worktreeRel := filepath.Join(paths.WorktreesDirName, "task-1")
	worktreeDir := filepath.Join(tmpDir, worktreeRel)

	err := handleReadyClaimWorktree(
		bb,
		gitWrapper,
		"task-1",
		models.TaskStatusReady,
		"integration",
		worktreeDir,
		worktreeRel,
		false,
	)
	if err == nil {
		t.Fatal("Expected race-condition error")
	}
	if !strings.Contains(err.Error(), "race condition") {
		t.Fatalf("Error = %q, want race condition", err.Error())
	}

	if _, err := os.Stat(worktreeDir); os.IsNotExist(err) {
		t.Fatal("Winner worktree should remain on disk")
	}
	branchExists, err := gitWrapper.BranchExists("task/task-1")
	if err != nil {
		t.Fatalf("Failed to check branch after race: %v", err)
	}
	if !branchExists {
		t.Fatal("Winner branch should remain after race")
	}
}

func TestHandleReadyClaimWorktree_CleanupAbortedWhenTaskClaimedConcurrently(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Write state with task already in IMPLEMENTING (simulates concurrent winner).
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)
	bb := db.New(stateFile)

	gitWrapper := git.New(tmpDir)
	// Create a worktree to simulate the concurrent winner's worktree on disk.
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}

	worktreeRel := filepath.Join(paths.WorktreesDirName, "task-1")
	worktreeDir := filepath.Join(tmpDir, worktreeRel)

	// cleanupAllowed=true but task is IMPLEMENTING → guard must abort cleanup.
	err := handleReadyClaimWorktree(
		bb,
		gitWrapper,
		"task-1",
		models.TaskStatusReady, // initial status we expected
		"integration",
		worktreeDir,
		worktreeRel,
		true, // cleanup would be allowed for stale resources
	)
	if err == nil {
		t.Fatal("Expected race-condition error, got nil")
	}
	if !strings.Contains(err.Error(), "claimed concurrently") {
		t.Fatalf("Error = %q, want to contain 'claimed concurrently'", err.Error())
	}

	// Worktree must NOT have been deleted.
	if _, statErr := os.Stat(worktreeDir); os.IsNotExist(statErr) {
		t.Fatal("Worktree should remain on disk — guard must prevent deletion")
	}
}

func TestClaimTask_PostWorktreeCmdRunsOnFreshClaim(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)

	// Configure a post-worktree command that creates a marker file.
	postCmd := "touch .post-worktree-ran"
	state.Config.PostWorktreeCmd = &postCmd
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}

	// Verify the post-worktree command ran in the worktree directory.
	markerPath := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1", ".post-worktree-ran")
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Error("Post-worktree command did not run: marker file missing")
	}

	// No warnings expected on success.
	if len(result.Warnings) != 0 {
		t.Errorf("Expected no warnings, got %v", result.Warnings)
	}
}

func TestClaimTask_PostWorktreeCmdFailureProducesWarning(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)

	// Configure a post-worktree command that will fail.
	postCmd := "exit 1"
	state.Config.PostWorktreeCmd = &postCmd
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() should succeed even when post-worktree-cmd fails, got: %v", err)
	}

	// Warning should be surfaced in result.
	if len(result.Warnings) == 0 {
		t.Error("Expected warning from failed post-worktree-cmd")
	} else if !strings.Contains(result.Warnings[0], "post-worktree-cmd") {
		t.Errorf("Warning = %q, want to contain 'post-worktree-cmd'", result.Warnings[0])
	}
}

func TestClaimTask_PostWorktreeCmdRunsOnSameCoderReclaim(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)

	// Configure a post-worktree command that creates a marker file.
	postCmd := "touch .post-worktree-ran"
	state.Config.PostWorktreeCmd = &postCmd
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Create the real git worktree that same-coder reclaim expects.
	gitWrapper := git.New(tmpDir)
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("Failed to create initial rejected worktree: %v", err)
	}

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}

	// Post-worktree command MUST run on same-coder reclaim for consistency
	// with wt_create (which runs it on existing worktrees too). This catches
	// worktrees that missed bootstrap and ensures build-readiness on reclaim.
	markerPath := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1", ".post-worktree-ran")
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Error("Post-worktree command should run on same-coder reclaim")
	}
	if len(result.Warnings) != 0 {
		t.Errorf("Expected no warnings, got %v", result.Warnings)
	}
}

func TestClaimTask_ScipIndexesEnabledWorktreeAfterPostWorktreeCmd(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	addTrackedGoSourceForClaimScipTest(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")

	markerPath := filepath.Join(tmpDir, "post-worktree-ran")
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Config.ScipSearch = []string{"go"}
	postCmd := fmt.Sprintf("touch %s", markerPath)
	state.Config.PostWorktreeCmd = &postCmd
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	var calls []scipsearch.RuntimeCommandPlan
	withClaimTaskScipRuntimeRunner(t, func(plan scipsearch.RuntimeCommandPlan) (string, error) {
		if _, err := os.Stat(markerPath); err != nil {
			return "", fmt.Errorf("post-worktree marker not present before indexing: %w", err)
		}
		calls = append(calls, plan)
		return writeClaimScipIndex(plan, []byte(plan.Dir))
	})

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("ClaimTask() warnings = %v, want none", result.Warnings)
	}
	if len(calls) != 1 || calls[0].Language != "go" {
		t.Fatalf("indexer calls = %#v, want one go call", calls)
	}

	worktreeDir := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")
	wantIndexPath := filepath.Join(worktreeDir, ".liza", "scip", "go.scip")
	indexes := availableClaimScipIndexes(t, worktreeDir, []string{"go"})
	if len(indexes) != 1 || indexes[0].Language != "go" || indexes[0].Path != wantIndexPath {
		t.Fatalf("AvailableIndexes() = %#v, want go index at %s", indexes, wantIndexPath)
	}
	if !filepath.IsAbs(indexes[0].Path) {
		t.Fatalf("index path %q is not absolute", indexes[0].Path)
	}
	assertGitStatusClean(t, worktreeDir)
}

func TestClaimTask_ScipDisabledActivationNoop(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	addTrackedGoSourceForClaimScipTest(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(scipsearch.EnvEnableScipSearch, "false")

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Config.ScipSearch = []string{"go"}
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)
	withClaimTaskScipRuntimeRunner(t, func(plan scipsearch.RuntimeCommandPlan) (string, error) {
		t.Fatalf("unexpected indexer call when scip-search activation is disabled: %#v", plan)
		return "", nil
	})

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("ClaimTask() warnings = %v, want none", result.Warnings)
	}

	worktreeDir := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")
	if _, err := os.Stat(filepath.Join(worktreeDir, ".liza", "scip")); !os.IsNotExist(err) {
		t.Fatalf(".liza/scip stat error = %v, want not exist", err)
	}
	if indexes := availableClaimScipIndexes(t, worktreeDir, []string{"go"}); len(indexes) != 0 {
		t.Fatalf("AvailableIndexes() = %#v, want none", indexes)
	}
}

func TestClaimTask_ScipFailedIndexerWarningOnly(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	addTrackedGoSourceForClaimScipTest(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Config.ScipSearch = []string{"go"}
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)
	withClaimTaskScipRuntimeRunner(t, func(plan scipsearch.RuntimeCommandPlan) (string, error) {
		return "indexer stderr", stderrors.New("boom")
	})

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() should succeed on indexer failure, got: %v", err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "scip-search go:") || !strings.Contains(result.Warnings[0], "boom") {
		t.Fatalf("ClaimTask() warnings = %v, want scip-search go warning with diagnostic", result.Warnings)
	}

	readState := readClaimStateForTest(t, stateFile)
	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("task not found after claim")
	}
	if task.Status != models.TaskStatusImplementing {
		t.Fatalf("task status = %s, want IMPLEMENTING", task.Status)
	}

	worktreeDir := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")
	if indexes := availableClaimScipIndexes(t, worktreeDir, []string{"go"}); len(indexes) != 0 {
		t.Fatalf("AvailableIndexes() = %#v, want none after failed indexer", indexes)
	}
	assertGitStatusClean(t, worktreeDir)
}

func TestClaimTask_ScipConcurrentClaimsUseIsolatedIndexes(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	addTrackedGoSourceForClaimScipTest(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Config.ScipSearch = []string{"go"}
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	var mu sync.Mutex
	outputs := map[string]string{}
	withClaimTaskScipRuntimeRunner(t, func(plan scipsearch.RuntimeCommandPlan) (string, error) {
		mu.Lock()
		outputs[plan.OutputPath] = plan.Dir
		mu.Unlock()
		return writeClaimScipIndex(plan, []byte(plan.Dir))
	})

	type claimOutcome struct {
		result *ClaimResult
		err    error
	}
	results := make(chan claimOutcome, 2)
	for _, claim := range []struct {
		taskID  string
		agentID string
	}{
		{taskID: "task-1", agentID: "coder-1"},
		{taskID: "task-2", agentID: "coder-2"},
	} {
		go func() {
			result, err := ClaimTask(tmpDir, claim.taskID, claim.agentID)
			results <- claimOutcome{result: result, err: err}
		}()
	}

	for range 2 {
		outcome := <-results
		if outcome.err != nil {
			t.Fatalf("ClaimTask() concurrent claim error: %v", outcome.err)
		}
		if len(outcome.result.Warnings) != 0 {
			t.Fatalf("ClaimTask() warnings = %v, want none", outcome.result.Warnings)
		}
	}

	task1Index := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1", ".liza", "scip", "go.scip")
	task2Index := filepath.Join(tmpDir, paths.WorktreesDirName, "task-2", ".liza", "scip", "go.scip")
	if task1Index == task2Index {
		t.Fatal("concurrent claims produced identical index paths")
	}
	for _, indexPath := range []string{task1Index, task2Index} {
		if !filepath.IsAbs(indexPath) {
			t.Fatalf("index path %q is not absolute", indexPath)
		}
		content, err := os.ReadFile(indexPath)
		if err != nil {
			t.Fatalf("ReadFile(%s) error: %v", indexPath, err)
		}
		mu.Lock()
		want := outputs[indexPath]
		mu.Unlock()
		if string(content) != want {
			t.Fatalf("index %s content = %q, want its own worktree dir %q", indexPath, content, want)
		}
	}
	if task1Content, err := os.ReadFile(task1Index); err != nil {
		t.Fatalf("ReadFile(%s) error: %v", task1Index, err)
	} else if task2Content, err := os.ReadFile(task2Index); err != nil {
		t.Fatalf("ReadFile(%s) error: %v", task2Index, err)
	} else if string(task1Content) == string(task2Content) {
		t.Fatalf("concurrent claims shared output content: %q", task1Content)
	}
	assertGitStatusClean(t, filepath.Join(tmpDir, paths.WorktreesDirName, "task-1"))
	assertGitStatusClean(t, filepath.Join(tmpDir, paths.WorktreesDirName, "task-2"))
}

// TestClaimTask_IterationLimitDoesNotReleaseCoder_WhenCoderMovedOn verifies
// that when a REJECTED task hits iteration limit and transitions to BLOCKED,
// it does NOT reset a coder who has already claimed a different task.
func TestClaimTask_IterationLimitDoesNotReleaseCoder_WhenCoderMovedOn(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Config.MaxCoderIterations = 3

	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.Iteration = 3
	task.Attempt = 2 // Attempt 2: iteration cap → BLOCKED (not new attempt)
	// task.AssignedTo is "coder-1" (set by BuildTaskByStatus for REJECTED)
	state.Tasks = []models.Task{task}

	// Coder has moved on: CurrentTask = "task-2", status = WORKING.
	task2ID := "task-2"
	movedAgent := testhelpers.RegisteredTestAgent("coder")
	movedAgent.Status = models.AgentStatusWorking
	movedAgent.CurrentTask = &task2ID
	state.Agents["coder-1"] = movedAgent

	testhelpers.WriteInitialState(t, stateFile, state)

	// A different coder tries to claim task-1, triggering iteration limit.
	_, err := ClaimTask(tmpDir, "task-1", "coder-2")
	if err == nil {
		t.Fatal("Expected iteration-limit error")
	}
	if !strings.Contains(err.Error(), "transitioned to BLOCKED") {
		t.Errorf("Error = %q, want to contain 'transitioned to BLOCKED'", err.Error())
	}

	readState := readClaimStateForTest(t, stateFile)
	blockedTask := readState.FindTask("task-1")
	if blockedTask == nil {
		t.Fatal("Task not found in state")
	}
	if blockedTask.Status != models.TaskStatusBlocked {
		t.Errorf("Task status = %v, want BLOCKED", blockedTask.Status)
	}
	if blockedTask.AssignedTo != nil {
		t.Error("AssignedTo should be cleared when task is blocked")
	}

	// The key assertion: coder-1 is still WORKING on task-2.
	agent := readState.Agents["coder-1"]
	if agent.Status != models.AgentStatusWorking {
		t.Errorf("Agent status = %v, want WORKING (coder moved to task-2, should not be released)", agent.Status)
	}
	if agent.CurrentTask == nil || *agent.CurrentTask != task2ID {
		t.Errorf("Agent CurrentTask = %v, want %q", agent.CurrentTask, task2ID)
	}
}

func TestClaimTask_IterationCapAttempt1_TriggersNewAttempt(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Config.MaxCoderIterations = 3

	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.Iteration = 3 // at limit
	task.Attempt = 1
	state.Tasks = []models.Task{task}

	taskRef := "task-1"
	agent := testhelpers.RegisteredTestAgent("coder")
	agent.Status = models.AgentStatusWorking
	agent.CurrentTask = &taskRef
	agent.Heartbeat = now
	state.Agents["coder-1"] = agent

	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err == nil {
		t.Fatal("Expected PreconditionError for attempt transition")
	}

	var precondErr *PreconditionError
	if !stderrors.As(err, &precondErr) {
		t.Fatalf("Expected PreconditionError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "transitioned to attempt") {
		t.Errorf("Error = %q, want to contain 'transitioned to attempt'", err.Error())
	}

	readState := readClaimStateForTest(t, stateFile)
	transitioned := readState.FindTask("task-1")
	if transitioned == nil {
		t.Fatal("Task not found in state")
	}
	if transitioned.Attempt != 2 {
		t.Errorf("Attempt = %d, want 2", transitioned.Attempt)
	}
	if transitioned.Status != models.TaskStatusReady {
		t.Errorf("Status = %v, want READY (initial pipeline status)", transitioned.Status)
	}
	if transitioned.Iteration != 0 {
		t.Errorf("Iteration = %d, want 0 (reset)", transitioned.Iteration)
	}
	if transitioned.ReviewCyclesCurrent != 0 {
		t.Errorf("ReviewCyclesCurrent = %d, want 0 (reset)", transitioned.ReviewCyclesCurrent)
	}
}

func TestClaimTask_IterationCapAttempt2_TriggersBlocked(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Config.MaxCoderIterations = 3

	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.Iteration = 3 // at limit
	task.Attempt = 2
	state.Tasks = []models.Task{task}

	taskRef := "task-1"
	agent := testhelpers.RegisteredTestAgent("coder")
	agent.CurrentTask = &taskRef
	state.Agents["coder-1"] = agent

	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err == nil {
		t.Fatal("Expected PreconditionError for BLOCKED transition")
	}

	var precondErr *PreconditionError
	if !stderrors.As(err, &precondErr) {
		t.Fatalf("Expected PreconditionError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "transitioned to BLOCKED") {
		t.Errorf("Error = %q, want to contain 'transitioned to BLOCKED'", err.Error())
	}

	readState := readClaimStateForTest(t, stateFile)
	blockedTask := readState.FindTask("task-1")
	if blockedTask == nil {
		t.Fatal("Task not found in state")
	}
	if blockedTask.Status != models.TaskStatusBlocked {
		t.Errorf("Task status = %v, want BLOCKED", blockedTask.Status)
	}
}

func TestClaimTask_ReviewCapAttempt2_TriggersBlocked(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Config.MaxCoderIterations = 10
	state.Config.MaxReviewCycles = 3

	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.Iteration = 2           // below iteration limit
	task.ReviewCyclesCurrent = 3 // at review limit
	task.Attempt = 2             // attempt 2 → BLOCKED (not new attempt)
	state.Tasks = []models.Task{task}

	taskRef := "task-1"
	agent := testhelpers.RegisteredTestAgent("coder")
	agent.CurrentTask = &taskRef
	state.Agents["coder-1"] = agent

	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err == nil {
		t.Fatal("Expected PreconditionError for BLOCKED transition")
	}

	var precondErr *PreconditionError
	if !stderrors.As(err, &precondErr) {
		t.Fatalf("Expected PreconditionError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "transitioned to BLOCKED") {
		t.Errorf("Error = %q, want to contain 'transitioned to BLOCKED'", err.Error())
	}

	readState := readClaimStateForTest(t, stateFile)
	blockedTask := readState.FindTask("task-1")
	if blockedTask == nil {
		t.Fatal("Task not found in state")
	}
	if blockedTask.Status != models.TaskStatusBlocked {
		t.Errorf("Task status = %v, want BLOCKED", blockedTask.Status)
	}
	if blockedTask.AssignedTo != nil {
		t.Error("AssignedTo should be cleared when task is blocked")
	}
	if blockedTask.BlockedReason == nil || !strings.Contains(*blockedTask.BlockedReason, "review budget exhausted") {
		t.Errorf("BlockedReason = %v, want to contain 'review budget exhausted'", blockedTask.BlockedReason)
	}
	if len(blockedTask.BlockedQuestions) == 0 {
		t.Error("BlockedQuestions should be populated")
	}

	agent = readState.Agents["coder-1"]
	if agent.Status != models.AgentStatusIdle {
		t.Errorf("Agent status = %v, want IDLE", agent.Status)
	}
	if agent.CurrentTask != nil {
		t.Error("Agent CurrentTask should be cleared after limit-based block")
	}
}

func TestClaimTask_SentinelAssignedTo_Rejected(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	sentinel := "$transitioning"
	task.AssignedTo = &sentinel
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err == nil {
		t.Fatal("Expected PreconditionError for sentinel AssignedTo, got nil")
	}
	var precondErr *PreconditionError
	if !stderrors.As(err, &precondErr) {
		t.Fatalf("Expected PreconditionError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "is in transition") {
		t.Errorf("Error = %q, want to contain 'is in transition'", err.Error())
	}
}

// readClaimStateForTest reads state for claim test verification.
func readClaimStateForTest(t *testing.T, stateFile string) *models.State {
	t.Helper()
	bb := db.New(stateFile)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	return state
}

func withClaimTaskScipRuntimeRunner(t *testing.T, runner scipsearch.RuntimeRunner) {
	t.Helper()
	scipRuntimeRunnerMu.Lock()
	previous := scipRuntimeRunner
	scipRuntimeRunner = runner
	scipRuntimeRunnerMu.Unlock()

	t.Cleanup(func() {
		scipRuntimeRunnerMu.Lock()
		scipRuntimeRunner = previous
		scipRuntimeRunnerMu.Unlock()
	})
}

func addTrackedGoSourceForClaimScipTest(t *testing.T, projectRoot string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module example.com/scipclaim\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(go.mod) error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(main.go) error: %v", err)
	}
	testhelpers.MustGit(t, projectRoot, "add", "go.mod", "main.go")
	testhelpers.MustGit(t, projectRoot, "commit", "-m", "Add Go source")
	testhelpers.MustGit(t, projectRoot, "branch", "-f", "integration", "HEAD")
}

func writeClaimScipIndex(plan scipsearch.RuntimeCommandPlan, content []byte) (string, error) {
	if err := os.MkdirAll(filepath.Dir(plan.OutputPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(plan.OutputPath, content, 0o644); err != nil {
		return "", err
	}
	return "", nil
}

func availableClaimScipIndexes(t *testing.T, worktreeDir string, languages []string) []scipsearch.IndexRef {
	t.Helper()
	indexes, err := scipsearch.AvailableIndexes(scipsearch.RuntimePlanOptions{
		TargetRoot:          worktreeDir,
		ConfiguredLanguages: languages,
	})
	if err != nil {
		t.Fatalf("AvailableIndexes() error: %v", err)
	}
	return indexes
}

func assertGitStatusClean(t *testing.T, worktreeDir string) {
	t.Helper()
	output, err := exec.Command("git", "-C", worktreeDir, "status", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("git status --porcelain error: %v: %s", err, output)
	}
	if strings.TrimSpace(string(output)) != "" {
		t.Fatalf("git status --porcelain = %q, want clean", output)
	}
}

func registerClaimTaskTestAgents(state *models.State) {
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
	state.Agents["coder-2"] = testhelpers.RegisteredTestAgent("coder")
	state.Agents["code-planner-1"] = testhelpers.RegisteredTestAgent("code-planner")
}
