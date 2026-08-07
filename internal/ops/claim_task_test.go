package ops

import (
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/functionalclusters"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/stacklit"
	"github.com/liza-mas/liza/internal/statevalidate"
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

func TestClaimGenerationFence(t *testing.T) {
	const (
		taskID  = "task-claim-generation-fence"
		agentID = "coder-1"
	)

	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)

	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	agent := state.Agents[agentID]
	agent.Generation = lifecycleGenerationA
	state.Agents[agentID] = agent
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReady, time.Now().UTC()),
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	previousHooks := testClaimTaskHooks
	testClaimTaskHooks = &claimTaskTestHooks{beforePhase3Modify: func() {
		setLifecycleAgentGeneration(t, bb, agentID, lifecycleGenerationB)
	}}
	t.Cleanup(func() { testClaimTaskHooks = previousHooks })

	_, err := ClaimTaskWithAuthority(projectRoot, taskID, models.AgentAuthority{
		ID: agentID, Generation: lifecycleGenerationA,
	})
	assertLifecycleAuthorityError(t, err, agentID)

	after := readClaimStateForTest(t, statePath)
	task := after.FindTask(taskID)
	if task == nil {
		t.Fatal("task missing after stale claim")
	}
	if task.Status != models.TaskStatusReady || task.AssignedTo != nil || task.LeaseExpires != nil ||
		task.Worktree != nil || task.BaseCommit != nil {
		t.Fatalf("stale claim published task state: %#v", task)
	}
	if got := after.Agents[agentID].Generation; got != lifecycleGenerationB {
		t.Fatalf("agent generation = %q, want %q", got, lifecycleGenerationB)
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
// task's ownership lease has expired and both worktree dir and branch are
// present, reassignment preserves the worktree.
func TestClaimTask_RejectedWorktreePresent_Preserved(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	expiredLease := now.Add(-time.Minute)
	task.LeaseExpires = &expiredLease
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

	// After ownership expiry, a different coder may claim and preserve worktree state.
	result, err := ClaimTask(tmpDir, "task-1", "coder-2")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}

	if result.PreviousAssignee != "coder-1" {
		t.Errorf("PreviousAssignee = %q, want %q", result.PreviousAssignee, "coder-1")
	}
	if result.WorktreeRecreated {
		t.Error("WorktreeRecreated should be false — post-expiry reassignment preserves worktree")
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

func TestClaimRejectedTask(t *testing.T) {
	t.Run("reattaches valid branch when directory is absent", func(t *testing.T) {
		fixture := newRejectedHandoffFixture(t, true)
		gitWrapper := git.New(fixture.projectRoot)
		if err := gitWrapper.RemoveWorktreeDir(fixture.taskID); err != nil {
			t.Fatalf("RemoveWorktreeDir() error: %v", err)
		}

		if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, "coder-2"); err != nil {
			t.Fatalf("ClaimTask() error: %v", err)
		}
		assertRejectedClaimState(t, fixture, "coder-2", fixture.branchSHA)
	})

	t.Run("creates from integration only when no reusable artifact exists", func(t *testing.T) {
		fixture := newRejectedHandoffFixture(t, false)
		integrationSHA := strings.TrimSpace(testhelpers.MustGit(t, fixture.projectRoot, "rev-parse", "integration"))

		if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, "coder-2"); err != nil {
			t.Fatalf("ClaimTask() error: %v", err)
		}
		fixture.baseCommit = integrationSHA
		fixture.branchSHA = integrationSHA
		assertRejectedClaimState(t, fixture, "coder-2", integrationSHA)
	})

	t.Run("fails closed without deleting unclassifiable corruption", func(t *testing.T) {
		fixture := newRejectedHandoffFixture(t, false)
		worktreeDir := filepath.Join(fixture.projectRoot, fixture.worktreeRel)
		if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
			t.Fatalf("MkdirAll() error: %v", err)
		}
		sentinel := filepath.Join(worktreeDir, "unclassifiable.txt")
		if err := os.WriteFile(sentinel, []byte("preserve me\n"), 0o644); err != nil {
			t.Fatalf("WriteFile() error: %v", err)
		}

		if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, "coder-2"); err == nil {
			t.Fatal("ClaimTask() error = nil, want fail-closed corruption diagnostic")
		}
		if _, err := os.Stat(sentinel); err != nil {
			t.Fatalf("corrupt artifact was deleted: %v", err)
		}
	})

	t.Run("fails closed on noncanonical preserved worktree metadata", func(t *testing.T) {
		fixture := newRejectedHandoffFixture(t, true)
		noncanonicalRel := filepath.Join(paths.WorktreesDirName, "recovery-task-1")
		noncanonicalDir := filepath.Join(fixture.projectRoot, noncanonicalRel)
		if err := os.MkdirAll(noncanonicalDir, 0o755); err != nil {
			t.Fatalf("MkdirAll() error: %v", err)
		}
		sentinel := filepath.Join(noncanonicalDir, "preserve-me.txt")
		if err := os.WriteFile(sentinel, []byte("preserve me\n"), 0o644); err != nil {
			t.Fatalf("WriteFile() error: %v", err)
		}

		bb := db.For(fixture.stateFile)
		if err := bb.Modify(func(state *models.State) error {
			task := state.FindTask(fixture.taskID)
			if task == nil {
				return fmt.Errorf("task not found")
			}
			task.Worktree = &noncanonicalRel
			return nil
		}); err != nil {
			t.Fatalf("set noncanonical worktree metadata: %v", err)
		}

		if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, "coder-2"); err == nil ||
			!strings.Contains(err.Error(), "want \".worktrees/task-1\"") {
			t.Fatalf("ClaimTask() error = %v, want canonical worktree diagnostic", err)
		}
		if _, err := os.Stat(sentinel); err != nil {
			t.Fatalf("noncanonical recovery artifact was deleted: %v", err)
		}
		if got := strings.TrimSpace(testhelpers.MustGit(t, fixture.projectRoot, "rev-parse", fixture.branchName)); got != fixture.branchSHA {
			t.Fatalf("task branch moved from %s to %s", fixture.branchSHA, got)
		}
		after := readClaimStateForTest(t, fixture.stateFile)
		task := after.FindTask(fixture.taskID)
		if task == nil || task.Worktree == nil || *task.Worktree != noncanonicalRel || task.Status != models.TaskStatusRejected {
			t.Fatalf("rejected state changed after noncanonical metadata failure: %#v", task)
		}
	})

	t.Run("concurrent retries publish one current doer generation", func(t *testing.T) {
		fixture := newRejectedHandoffFixture(t, true)
		if _, err := ReleaseClaim(fixture.projectRoot, fixture.taskID, "doer", true, "handoff", "human"); err != nil {
			t.Fatalf("ReleaseClaim() error: %v", err)
		}

		bb := db.For(fixture.stateFile)
		if err := bb.Modify(func(state *models.State) error {
			agent := state.Agents["coder-2"]
			agent.Generation = lifecycleGenerationA
			state.Agents["coder-2"] = agent
			return nil
		}); err != nil {
			t.Fatalf("set stale coder generation: %v", err)
		}

		staleAtPhase3 := make(chan error, 1)
		firstPhase3 := make(chan struct{}, 1)
		firstPhase3 <- struct{}{}
		previousHooks := testClaimTaskHooks
		testClaimTaskHooks = &claimTaskTestHooks{beforePhase3Modify: func() {
			select {
			case <-firstPhase3:
				staleAtPhase3 <- bb.Modify(func(state *models.State) error {
					agent := state.Agents["coder-2"]
					agent.Generation = lifecycleGenerationB
					state.Agents["coder-2"] = agent
					return nil
				})
			default:
			}
		}}
		t.Cleanup(func() { testClaimTaskHooks = previousHooks })

		staleResult := make(chan error, 1)
		go func() {
			_, err := ClaimTaskWithAuthority(fixture.projectRoot, fixture.taskID, models.AgentAuthority{
				ID: "coder-2", Generation: lifecycleGenerationA,
			})
			staleResult <- err
		}()
		if err := <-staleAtPhase3; err != nil {
			t.Fatalf("rotate coder generation at stale phase 3: %v", err)
		}

		_, currentErr := ClaimTaskWithAuthority(fixture.projectRoot, fixture.taskID, models.AgentAuthority{
			ID: "coder-2", Generation: lifecycleGenerationB,
		})
		if currentErr != nil {
			t.Fatalf("current ClaimTaskWithAuthority() error: %v", currentErr)
		}
		assertLifecycleAuthorityError(t, <-staleResult, "coder-2")
		assertRejectedClaimState(t, fixture, "coder-2", fixture.branchSHA)

		state := readClaimStateForTest(t, fixture.stateFile)
		winnerAgent := state.Agents["coder-2"]
		if winnerAgent.Generation != lifecycleGenerationB {
			t.Fatalf("winner generation = %q, want current %q", winnerAgent.Generation, lifecycleGenerationB)
		}
	})
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

// TestClaimTask_RejectedWorktreeDirExistsBranchMissing_FailsClosed verifies that
// an unclassifiable rejected-task directory is preserved for manual recovery.
func TestClaimTask_RejectedWorktreeDirExistsBranchMissing_FailsClosed(t *testing.T) {
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

	sentinelPath := filepath.Join(wtDir, "preserve-me")
	if err := os.WriteFile(sentinelPath, []byte("corrupt artifact"), 0644); err != nil {
		t.Fatalf("Failed to write corruption sentinel: %v", err)
	}

	if _, err := ClaimTask(tmpDir, "task-1", "coder-1"); err == nil {
		t.Fatal("ClaimTask() error = nil, want unclassifiable artifact failure")
	}

	if contents, err := os.ReadFile(sentinelPath); err != nil || string(contents) != "corrupt artifact" {
		t.Fatalf("Unclassifiable artifact was not preserved: contents=%q err=%v", contents, err)
	}
}

// TestClaimTask_RejectedMutateTask_NoCounterReset verifies that ReviewCyclesCurrent
// is NOT reset when a different coder claims after ownership expiry within the
// same attempt. The attempt, not the agent, is the resource boundary.
func TestClaimTask_RejectedMutateTask_NoCounterReset(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.ReviewCyclesCurrent = 3 // Non-zero — should be preserved on different-coder claim.
	expiredLease := now.Add(-time.Minute)
	task.LeaseExpires = &expiredLease
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)
	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver() error: %v", err)
	}
	if got := models.GetTaskReadiness(state, resolver).Claimable; got != 1 {
		t.Fatalf("claimable readiness = %d, want 1 after rejected ownership lease expiry", got)
	}

	// Create worktree (required for claim to succeed).
	gitWrapper := git.New(tmpDir)
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}

	// After ownership expiry, a different coder can claim without resetting counters.
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
		t.Errorf("ReviewCyclesCurrent = %d, want 3 (should not reset on post-expiry different-coder claim within same attempt)", claimedTask.ReviewCyclesCurrent)
	}
}

func TestClaimTask_RejectedActiveLeaseBlocksDifferentCoder(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	futureLease := now.Add(30 * time.Minute)
	task.LeaseExpires = &futureLease
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)
	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver() error: %v", err)
	}
	if got := models.GetTaskReadiness(state, resolver).Claimable; got != 0 {
		t.Fatalf("claimable readiness = %d, want 0 while rejected work is reserved by another agent", got)
	}

	_, err = ClaimTask(tmpDir, "task-1", "coder-2")
	if err == nil {
		t.Fatal("Expected active rejected ownership lease to block different coder")
	}
	var precondErr *PreconditionError
	if !stderrors.As(err, &precondErr) {
		t.Fatalf("Expected PreconditionError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "cannot claim rejected work before the lease expires") {
		t.Errorf("Error = %q, want active ownership lease message", err.Error())
	}

	readState := readClaimStateForTest(t, stateFile)
	blockedTask := readState.FindTask("task-1")
	if blockedTask == nil {
		t.Fatal("Task not found in state")
	}
	if blockedTask.Status != models.TaskStatusRejected {
		t.Errorf("Status = %v, want REJECTED", blockedTask.Status)
	}
	if blockedTask.AssignedTo == nil || *blockedTask.AssignedTo != "coder-1" {
		t.Errorf("AssignedTo = %v, want coder-1", blockedTask.AssignedTo)
	}
}

func TestClaimTask_RejectedActiveLeaseAllowsSameCoder(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	futureLease := now.Add(30 * time.Minute)
	task.LeaseExpires = &futureLease
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	gitWrapper := git.New(tmpDir)
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("Failed to create initial rejected worktree: %v", err)
	}

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	if result.PreviousAssignee != "coder-1" {
		t.Errorf("PreviousAssignee = %q, want coder-1", result.PreviousAssignee)
	}
	if result.SourceStatus != models.TaskStatusRejected {
		t.Errorf("SourceStatus = %v, want REJECTED", result.SourceStatus)
	}
}

func TestClaimTask_RejectedAssignedWithoutLeaseFailsClosed(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.LeaseExpires = nil
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	for _, agentID := range []string{"coder-1", "coder-2"} {
		_, err := ClaimTask(tmpDir, "task-1", agentID)
		if err == nil {
			t.Fatalf("Expected malformed rejected ownership to block %s", agentID)
		}
		var precondErr *PreconditionError
		if !stderrors.As(err, &precondErr) {
			t.Fatalf("Expected PreconditionError for %s, got %T: %v", agentID, err, err)
		}
		if !strings.Contains(err.Error(), "without lease_expires") {
			t.Errorf("Error for %s = %q, want malformed ownership message", agentID, err.Error())
		}
	}
}

func TestClaimTask_RejectedOwnershipRaceRecheckedInPhase3(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.AssignedTo = nil
	task.LeaseExpires = nil
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	testClaimTaskHooks = &claimTaskTestHooks{
		beforePhase3Modify: func() {
			bb := db.For(stateFile)
			if err := bb.Modify(func(state *models.State) error {
				task := state.FindTask("task-1")
				if task == nil {
					return fmt.Errorf("task not found")
				}
				assignedTo := "coder-2"
				futureLease := time.Now().UTC().Add(30 * time.Minute)
				task.AssignedTo = &assignedTo
				task.LeaseExpires = &futureLease
				return nil
			}); err != nil {
				t.Fatalf("failed to inject ownership race: %v", err)
			}
		},
	}
	t.Cleanup(func() {
		testClaimTaskHooks = nil
	})

	_, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err == nil {
		t.Fatal("Expected Phase 3 rejected ownership race guard to block claim")
	}
	var precondErr *PreconditionError
	if !stderrors.As(err, &precondErr) {
		t.Fatalf("Expected PreconditionError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "cannot claim rejected work before the lease expires") {
		t.Errorf("Error = %q, want active ownership lease message", err.Error())
	}

	readState := readClaimStateForTest(t, stateFile)
	racedTask := readState.FindTask("task-1")
	if racedTask == nil {
		t.Fatal("Task not found in state")
	}
	if racedTask.Status != models.TaskStatusRejected {
		t.Errorf("Status = %v, want REJECTED", racedTask.Status)
	}
	if racedTask.AssignedTo == nil || *racedTask.AssignedTo != "coder-2" {
		t.Errorf("AssignedTo = %v, want coder-2", racedTask.AssignedTo)
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
	if task.Worktree == nil || *task.Worktree != path.Join(paths.WorktreesDirName, "task-1") {
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

	worktreeRel := path.Join(paths.WorktreesDirName, "task-1")
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

	worktreeRel := path.Join(paths.WorktreesDirName, "task-1")
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
	requirePosixShell(t)
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(stacklit.EnvEnableStacklit, "false")

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

func TestClaimTask_CopyWorktreeEnvFilesOnFreshClaim(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	commitEnvIgnoreForWorktreeTest(t, tmpDir)
	writeRootFileForWorktreeTest(t, tmpDir, ".env", "ROOT_ENV=1\n")
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(stacklit.EnvEnableStacklit, "false")

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Config.CopyWorktreeEnvFiles = true
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("ClaimTask() warnings = %v, want none", result.Warnings)
	}

	worktreeDir := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")
	assertWorktreeFileExists(t, worktreeDir, ".env")
	assertWorktreePathIgnored(t, worktreeDir, ".env")
	assertGitStatusClean(t, worktreeDir)
}

// A failed setup command means the worktree is not build-ready. The claim must
// fail closed rather than hand it to a coder session (ADR-0031 amendment
// 2026-08-23; superseded the earlier warn-and-continue contract).
func TestClaimTask_PostWorktreeCmdFailureFailsClaimClosed(t *testing.T) {
	requirePosixShell(t)
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)

	// Configure a post-worktree command that will fail.
	postCmd := "echo boom >&2; exit 1"
	state.Config.PostWorktreeCmd = &postCmd
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err == nil {
		t.Fatalf("ClaimTask() error = nil, want post-worktree setup failure; result = %+v", result)
	}
	var setupErr *PostWorktreeSetupError
	if !stderrors.As(err, &setupErr) {
		t.Fatalf("ClaimTask() error = %v, want *PostWorktreeSetupError", err)
	}
	if setupErr.Cmd != postCmd {
		t.Errorf("setupErr.Cmd = %q, want %q", setupErr.Cmd, postCmd)
	}

	// No partial claim: the task keeps its pre-claim status and stays unassigned.
	after, readErr := db.For(stateFile).Read()
	if readErr != nil {
		t.Fatalf("Read() error = %v", readErr)
	}
	task := after.FindTask("task-1")
	if task == nil {
		t.Fatal("FindTask(task-1) = nil")
	}
	if task.Status != models.TaskStatusReady {
		t.Errorf("task.Status = %s, want %s", task.Status, models.TaskStatusReady)
	}
	if task.AssignedTo != nil {
		t.Errorf("task.AssignedTo = %q, want nil", *task.AssignedTo)
	}
	if agent := after.Agents["coder-1"]; agent.CurrentTask != nil {
		t.Errorf("agent.CurrentTask = %q, want nil", *agent.CurrentTask)
	}
}

// The failing worktree is preserved so the setup command can be reproduced
// against it; a later fresh claim treats it as a stale resource.
func TestClaimTask_PostWorktreeCmdFailurePreservesWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)

	postCmd := "exit 1"
	state.Config.PostWorktreeCmd = &postCmd
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	if _, err := ClaimTask(tmpDir, "task-1", "coder-1"); err == nil {
		t.Fatal("ClaimTask() error = nil, want post-worktree setup failure")
	}

	worktreeDir := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")
	if _, statErr := os.Stat(worktreeDir); statErr != nil {
		t.Errorf("os.Stat(%q) error = %v, want worktree preserved for inspection", worktreeDir, statErr)
	}
}

func TestClaimTask_PostWorktreeCmdRunsOnSameCoderReclaim(t *testing.T) {
	requirePosixShell(t)
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(stacklit.EnvEnableStacklit, "false")

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

// Reclaim paths reuse an existing worktree, so a setup failure there means the
// worktree missed bootstrap or regressed — the reclaim must not proceed.
func TestClaimTask_PostWorktreeCmdFailureFailsReclaimClosed(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      models.TaskStatus
		preCreateWt bool
	}{
		// Both rejected branches run the hook (rejectedClaimStrategy always
		// returns true), so both must fail closed.
		{name: "rejected reclaim preserved worktree", status: models.TaskStatusRejected, preCreateWt: true},
		{name: "rejected reclaim recreated worktree", status: models.TaskStatusRejected, preCreateWt: false},
		{name: "integration fix", status: models.TaskStatusIntegrationFailed, preCreateWt: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testhelpers.SetupTestGitRepo(t, tmpDir)
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			t.Setenv(stacklit.EnvEnableStacklit, "false")

			now := time.Now().UTC()
			state := testhelpers.CreateValidState()
			registerClaimTaskTestAgents(state)

			postCmd := "exit 1"
			state.Config.PostWorktreeCmd = &postCmd
			state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", tc.status, now)}
			testhelpers.WriteInitialState(t, stateFile, state)

			if tc.preCreateWt {
				if _, err := git.New(tmpDir).CreateWorktree("task-1", "integration"); err != nil {
					t.Fatalf("Failed to create initial worktree: %v", err)
				}
			}

			_, err := ClaimTask(tmpDir, "task-1", "coder-1")
			if err == nil {
				t.Fatal("ClaimTask() error = nil, want post-worktree setup failure")
			}
			var setupErr *PostWorktreeSetupError
			if !stderrors.As(err, &setupErr) {
				t.Fatalf("ClaimTask() error = %v, want *PostWorktreeSetupError", err)
			}

			after, readErr := db.For(stateFile).Read()
			if readErr != nil {
				t.Fatalf("Read() error = %v", readErr)
			}
			task := after.FindTask("task-1")
			if task == nil {
				t.Fatal("FindTask(task-1) = nil")
			}
			if task.Status != tc.status {
				t.Errorf("task.Status = %s, want unchanged %s", task.Status, tc.status)
			}
		})
	}
}

// preservedInitialClaimStrategy.shouldRunPostWorktreeCmd returns true, so the
// preserved-branch continuation path must fail closed like every other claim.
func TestClaimTask_PostWorktreeCmdFailureFailsPreservedInitialClaimClosed(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)

	bb := db.For(fixture.stateFile)
	if err := bb.Modify(func(state *models.State) error {
		postCmd := "exit 1"
		state.Config.PostWorktreeCmd = &postCmd
		return nil
	}); err != nil {
		t.Fatalf("Modify() error = %v", err)
	}

	statusBefore := mustFindTaskStatus(t, bb, fixture.taskID)

	_, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID)
	if err == nil {
		t.Fatal("ClaimTask() error = nil, want post-worktree setup failure")
	}
	var setupErr *PostWorktreeSetupError
	if !stderrors.As(err, &setupErr) {
		t.Fatalf("ClaimTask() error = %v, want *PostWorktreeSetupError", err)
	}
	if got := mustFindTaskStatus(t, bb, fixture.taskID); got != statusBefore {
		t.Errorf("task.Status = %s, want unchanged %s", got, statusBefore)
	}
}

// Fixing the command must make the same claim succeed — fail-closed is a gate,
// not a permanent rejection of the task.
func TestClaimTask_SucceedsAfterPostWorktreeCmdFixed(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(stacklit.EnvEnableStacklit, "false")

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	broken := "exit 1"
	state.Config.PostWorktreeCmd = &broken
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	if _, err := ClaimTask(tmpDir, "task-1", "coder-1"); err == nil {
		t.Fatal("ClaimTask() error = nil, want initial setup failure")
	}

	bb := db.For(stateFile)
	if err := bb.Modify(func(state *models.State) error {
		fixed := "touch .post-worktree-ran"
		state.Config.PostWorktreeCmd = &fixed
		return nil
	}); err != nil {
		t.Fatalf("Modify() error = %v", err)
	}

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() after fix error = %v, want success", err)
	}
	markerPath := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1", ".post-worktree-ran")
	if _, statErr := os.Stat(markerPath); statErr != nil {
		t.Errorf("os.Stat(marker) error = %v, want the fixed command to have run", statErr)
	}
	if result.TaskID != "task-1" {
		t.Errorf("result.TaskID = %q, want task-1", result.TaskID)
	}
}

func mustFindTaskStatus(t *testing.T, bb *db.Blackboard, taskID string) models.TaskStatus {
	t.Helper()
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatalf("FindTask(%q) = nil", taskID)
	}
	return task.Status
}

func TestClaimTaskPreparesSembleIgnoreForFreshClaim(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(stacklit.EnvEnableStacklit, "false")

	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC()),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}

	worktreeDir := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")
	if result.WorktreeRel != path.Join(paths.WorktreesDirName, "task-1") {
		t.Fatalf("WorktreeRel = %q, want task worktree", result.WorktreeRel)
	}
	assertPrepareSembleIgnorePayload(t, worktreeDir)
	assertPrepareSemblePrivateExcludeCount(t, worktreeDir, ".sembleignore", 1)
	assertGitStatusClean(t, worktreeDir)
}

func TestClaimTaskPreparesSembleIgnoreForRejectedReclaim(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(stacklit.EnvEnableStacklit, "false")

	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, time.Now().UTC()),
	}
	testhelpers.WriteInitialState(t, stateFile, state)
	gitWrapper := git.New(tmpDir)
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("CreateWorktree() setup error: %v", err)
	}

	_, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}

	worktreeDir := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")
	assertPrepareSembleIgnorePayload(t, worktreeDir)
	assertPrepareSemblePrivateExcludeCount(t, worktreeDir, ".sembleignore", 1)
	assertGitStatusClean(t, worktreeDir)
}

func TestClaimTask_ScipIndexesEnabledWorktreeAfterPostWorktreeCmd(t *testing.T) {
	requirePosixShell(t)
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	addTrackedGoSourceForClaimScipTest(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	t.Setenv(stacklit.EnvEnableStacklit, "false")

	markerPath := filepath.Join(tmpDir, "post-worktree-ran")
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Config.ScipSearch = []string{"go"}
	// The command runs in a POSIX shell, which reads a native Windows path as a
	// string of escapes: "touch C:\dir\marker" creates a file named "Cdirmarker".
	postCmd := fmt.Sprintf("touch %q", filepath.ToSlash(markerPath))
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
	if len(calls) != 2 || calls[0].Language != "go" || calls[1].Name != "scip-search" {
		t.Fatalf("indexer calls = %#v, want go indexer and aggregate calls", calls)
	}

	worktreeDir := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")
	wantIndexPath := filepath.Join(worktreeDir, paths.ProjectDirName(), "scip", "go.scip")
	indexes := availableClaimScipIndexes(t, worktreeDir, []string{"go"})
	if len(indexes) != 1 || indexes[0].Language != "go" || indexes[0].Path != wantIndexPath {
		t.Fatalf("AvailableIndexes() = %#v, want go index at %s", indexes, wantIndexPath)
	}
	if !filepath.IsAbs(indexes[0].Path) {
		t.Fatalf("index path %q is not absolute", indexes[0].Path)
	}
	assertGitStatusClean(t, worktreeDir)
}

func TestClaimTaskSembleIgnorePreparationRunsAfterPostWorktreeBeforeIndexRefresh(t *testing.T) {
	requirePosixShell(t)
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	addTrackedGoSourceForClaimScipTest(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	t.Setenv(stacklit.EnvEnableStacklit, "false")

	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Config.ScipSearch = []string{"go"}
	postCmd := "printf '" + paths.ProjectDirName() + "/\\n' > .sembleignore"
	state.Config.PostWorktreeCmd = &postCmd
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC()),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	withClaimTaskScipRuntimeRunner(t, func(plan scipsearch.RuntimeCommandPlan) (string, error) {
		got := readPrepareSembleIgnoreFile(t, plan.Dir)
		if got != semble.GeneratedWorktreeIgnorePayload() {
			return "", fmt.Errorf(".sembleignore before index refresh = %q, want generated payload", got)
		}
		return writeClaimScipIndex(plan, []byte(plan.Dir))
	})

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("ClaimTask() warnings = %v, want none", result.Warnings)
	}
	worktreeDir := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")
	assertPrepareSembleIgnorePayload(t, worktreeDir)
	assertPrepareSemblePrivateExcludeCount(t, worktreeDir, paths.ProjectDirName()+"/scip/", 1)
	assertPrepareSemblePrivateExcludeCount(t, worktreeDir, ".sembleignore", 1)
	assertGitStatusClean(t, worktreeDir)
}

func TestClaimTask_ScipDisabledActivationNoop(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	addTrackedGoSourceForClaimScipTest(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(scipsearch.EnvEnableScipSearch, "false")
	t.Setenv(stacklit.EnvEnableStacklit, "false")

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
	if _, err := os.Stat(filepath.Join(worktreeDir, paths.ProjectDirName(), "scip")); !os.IsNotExist(err) {
		t.Fatalf("%s/scip stat error = %v, want not exist", paths.ProjectDirName(), err)
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
	t.Setenv(stacklit.EnvEnableStacklit, "false")

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

func TestClaimTask_FunctionalClustersFailedBuildWarningReturned(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	addTrackedGoSourceForClaimScipTest(t, tmpDir)
	if err := os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte("stacklit.json\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.gitignore) error: %v", err)
	}
	testhelpers.MustGit(t, tmpDir, "add", ".gitignore")
	testhelpers.MustGit(t, tmpDir, "commit", "-m", "Ignore Stacklit index")
	testhelpers.MustGit(t, tmpDir, "branch", "-f", "integration", "HEAD")
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	t.Setenv(stacklit.EnvEnableStacklit, "true")
	t.Setenv(functionalclusters.EnvEnableFunctionalClusters, "true")

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Config.ScipSearch = []string{"go"}
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	withClaimTaskScipRuntimeRunner(t, func(plan scipsearch.RuntimeCommandPlan) (string, error) {
		return writeClaimScipIndex(plan, []byte(plan.Dir))
	})
	withClaimTaskStacklitRuntimeRunner(t, func(plan stacklit.RuntimeCommandPlan) (string, error) {
		if err := os.WriteFile(plan.OutputPath, []byte("stacklit index\n"), 0o644); err != nil {
			return "", err
		}
		return "", nil
	})
	withClaimTaskFunctionalClustersRuntimeRunner(t, func(functionalclusters.RuntimeCommandPlan) (string, error) {
		return "functional-clusters stderr", stderrors.New("functional clusters boom")
	})

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() should succeed on Functional Clusters failure, got: %v", err)
	}
	if len(result.Warnings) != 1 ||
		!strings.Contains(result.Warnings[0], "functional-clusters:") ||
		!strings.Contains(result.Warnings[0], "functional clusters boom") {
		t.Fatalf("ClaimTask() warnings = %v, want Functional Clusters warning with diagnostic", result.Warnings)
	}

	worktreeDir := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")
	assertGitStatusClean(t, worktreeDir)
}

func TestClaimTaskSembleIgnorePreparationWarningsAreBounded(t *testing.T) {
	requirePosixShell(t)
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	if err := os.WriteFile(filepath.Join(tmpDir, ".sembleignore"), []byte("operator-owned marker\n"+paths.ProjectDirName()+"/\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.sembleignore) error: %v", err)
	}
	testhelpers.MustGit(t, tmpDir, "add", ".sembleignore")
	testhelpers.MustGit(t, tmpDir, "commit", "-m", "track incomplete semble ignore")
	testhelpers.MustGit(t, tmpDir, "branch", "-f", "integration", "HEAD")
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(stacklit.EnvEnableStacklit, "false")

	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC()),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("ClaimTask() warnings = %#v, want one Semble warning", result.Warnings)
	}
	warning := result.Warnings[0]
	for _, want := range []string{"tracked .sembleignore", "missing required patterns"} {
		if !strings.Contains(warning, want) {
			t.Fatalf("warning = %q, want to contain %q", warning, want)
		}
	}
	if strings.Contains(warning, "operator-owned marker") {
		t.Fatalf("warning includes tracked file contents: %q", warning)
	}
	if len(warning) > 512 {
		t.Fatalf("warning length = %d, want bounded <= 512", len(warning))
	}

	worktreeDir := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")
	if got := readPrepareSembleIgnoreFile(t, worktreeDir); got != "operator-owned marker\n"+paths.ProjectDirName()+"/\n" {
		t.Fatalf("tracked .sembleignore mutated: got %q", got)
	}
	assertPrepareSemblePrivateExcludeCount(t, worktreeDir, ".sembleignore", 0)
	assertGitStatusClean(t, worktreeDir)
}

func TestClaimTask_ScipConcurrentClaimsUseIsolatedIndexes(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	addTrackedGoSourceForClaimScipTest(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	t.Setenv(stacklit.EnvEnableStacklit, "false")

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
		if plan.Name == "scip-search" {
			finalPath := filepath.Join(plan.Dir, paths.ProjectDirName(), "scip", plan.Language+".scip")
			mu.Lock()
			outputs[finalPath] = plan.Dir
			mu.Unlock()
		}
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

	task1Index := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1", paths.ProjectDirName(), "scip", "go.scip")
	task2Index := filepath.Join(tmpDir, paths.WorktreesDirName, "task-2", paths.ProjectDirName(), "scip", "go.scip")
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

func TestClaimTaskSembleIgnorePreparationConcurrentCallsCleanStatus(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	t.Setenv(stacklit.EnvEnableStacklit, "false")

	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	now := time.Now().UTC()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

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
		claim := claim
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

	for _, taskID := range []string{"task-1", "task-2"} {
		worktreeDir := filepath.Join(tmpDir, paths.WorktreesDirName, taskID)
		assertPrepareSembleIgnorePayload(t, worktreeDir)
		assertPrepareSemblePrivateExcludeCount(t, worktreeDir, ".sembleignore", 1)
		assertGitStatusClean(t, worktreeDir)
	}
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
	expiredLease := now.Add(-time.Minute)
	task.LeaseExpires = &expiredLease
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

func TestClaimTask_PreservedInitialRebasesOntoCapturedIntegrationCommit(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	targetSHA := advancePreservedClaimIntegration(t, fixture.projectRoot, "dependency.txt", "merged dependency\n", "Merge dependency")

	result, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID)
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	if result.BaseCommit != targetSHA {
		t.Fatalf("BaseCommit = %s, want captured integration SHA %s", result.BaseCommit, targetSHA)
	}

	claimedHead := testhelpers.MustGit(t, fixture.worktreeDir, "rev-parse", "HEAD")
	if claimedHead == fixture.preservedHead {
		t.Fatal("preserved commit SHA was not rewritten by rebase")
	}
	if got := testhelpers.MustGit(t, fixture.worktreeDir, "log", "-1", "--format=%s"); got != fixture.commitMessage {
		t.Fatalf("commit message = %q, want %q", got, fixture.commitMessage)
	}
	if got, err := os.ReadFile(filepath.Join(fixture.worktreeDir, "task.txt")); err != nil || string(got) != fixture.taskContent {
		t.Fatalf("task content = %q, %v; want %q", got, err, fixture.taskContent)
	}
	if got, err := os.ReadFile(filepath.Join(fixture.worktreeDir, "dependency.txt")); err != nil || string(got) != "merged dependency\n" {
		t.Fatalf("dependency content = %q, %v", got, err)
	}
	ancestor, err := git.New(fixture.projectRoot).IsAncestor(targetSHA, claimedHead)
	if err != nil {
		t.Fatalf("IsAncestor() error: %v", err)
	}
	if !ancestor {
		t.Fatalf("rebased HEAD %s does not descend from %s", claimedHead, targetSHA)
	}

	state := readClaimStateForTest(t, fixture.stateFile)
	task := state.FindTask(fixture.taskID)
	if task == nil {
		t.Fatal("task not found")
	}
	if task.BaseCommit == nil || *task.BaseCommit != targetSHA {
		t.Fatalf("task base_commit = %v, want %s", task.BaseCommit, targetSHA)
	}
	if task.AssignedTo == nil || *task.AssignedTo != fixture.agentID {
		t.Fatalf("task assigned_to = %v, want %s", task.AssignedTo, fixture.agentID)
	}
}

func TestClaimTask_PreservedInitialDependencyGatePrecedesFilesystemWork(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	state := readClaimStateForTest(t, fixture.stateFile)
	pending := testhelpers.BuildTaskByStatus("dependency-1", models.TaskStatusReady, time.Now().UTC())
	task := state.FindTask(fixture.taskID)
	task.DependsOn = []string{pending.ID}
	state.Tasks = append(state.Tasks, pending)
	testhelpers.WriteInitialState(t, fixture.stateFile, state)
	advancePreservedClaimIntegration(t, fixture.projectRoot, "dependency.txt", "not yet available\n", "Prepare dependency")

	_, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID)
	if err == nil || !strings.Contains(err.Error(), "unmet dependencies") {
		t.Fatalf("ClaimTask() error = %v, want unmet dependencies", err)
	}
	if head := testhelpers.MustGit(t, fixture.worktreeDir, "rev-parse", "HEAD"); head != fixture.preservedHead {
		t.Fatalf("worktree HEAD moved before dependency gate: got %s, want %s", head, fixture.preservedHead)
	}
}

func TestClaimTask_PreservedInitialIntegrationMoveBeforeCriticalSectionFailsClosed(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	targetSHA := advancePreservedClaimIntegration(t, fixture.projectRoot, "dependency.txt", "dependency v1\n", "Merge dependency")
	laterSHA := advancePreservedClaimIntegration(t, fixture.projectRoot, "later.txt", "later integration\n", "Advance integration later")
	testhelpers.MustGit(t, fixture.projectRoot, "branch", "-f", "integration", targetSHA)

	testClaimTaskHooks = &claimTaskTestHooks{
		beforePhase3Modify: func() {
			if err := git.New(fixture.projectRoot).UpdateRef("refs/heads/integration", laterSHA, targetSHA); err != nil {
				t.Fatalf("advance integration before critical section: %v", err)
			}
		},
	}
	t.Cleanup(func() { testClaimTaskHooks = nil })

	_, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID)
	if err == nil || !strings.Contains(err.Error(), "integration ref changed") {
		t.Fatalf("ClaimTask() error = %v, want integration ref changed", err)
	}

	state := readClaimStateForTest(t, fixture.stateFile)
	task := state.FindTask(fixture.taskID)
	if task.AssignedTo != nil {
		t.Fatalf("assigned_to = %v, want nil", task.AssignedTo)
	}
	if task.BaseCommit == nil || *task.BaseCommit != fixture.originalBase {
		t.Fatalf("base_commit = %v, want unchanged %s", task.BaseCommit, fixture.originalBase)
	}
	if got, readErr := os.ReadFile(filepath.Join(fixture.worktreeDir, "task.txt")); readErr != nil || string(got) != fixture.taskContent {
		t.Fatalf("preserved patch content = %q, %v", got, readErr)
	}
}

func TestClaimTask_PreservedInitialIntegrationMoveAfterEqualityWaitsForAssignment(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	targetSHA := advancePreservedClaimIntegration(t, fixture.projectRoot, "dependency.txt", "dependency v1\n", "Merge dependency")
	laterSHA := advancePreservedClaimIntegration(t, fixture.projectRoot, "later.txt", "later integration\n", "Advance integration later")
	testhelpers.MustGit(t, fixture.projectRoot, "branch", "-f", "integration", targetSHA)

	const moverOperation = "test preserved claim cooperating integration move"
	moverReachedLock := make(chan struct{})
	moverDone := make(chan error, 1)
	beforeEffectiveIntegrationCompletionLinearizationTestHook = func(operation string) {
		if operation == moverOperation {
			close(moverReachedLock)
		}
	}
	t.Cleanup(func() {
		beforeEffectiveIntegrationCompletionLinearizationTestHook = nil
		testClaimTaskHooks = nil
	})
	testClaimTaskHooks = &claimTaskTestHooks{
		afterPreservedIntegrationEqualityCheck: func() {
			go func() {
				moverDone <- withEffectiveIntegrationCompletionLinearization(fixture.projectRoot, moverOperation, func() error {
					state, readErr := db.New(fixture.stateFile).Read()
					if readErr != nil {
						return fmt.Errorf("read state after assignment: %w", readErr)
					}
					task := state.FindTask(fixture.taskID)
					if task == nil || task.AssignedTo == nil || *task.AssignedTo != fixture.agentID {
						return fmt.Errorf("integration mover ran before assignment committed: %+v", task)
					}
					return withIntegrationMutationLock(fixture.projectRoot, "test preserved claim ref move", func() error {
						return git.New(fixture.projectRoot).UpdateRef("refs/heads/integration", laterSHA, targetSHA)
					})
				})
			}()
			<-moverReachedLock
			if got := testhelpers.MustGit(t, fixture.projectRoot, "rev-parse", "integration"); got != targetSHA {
				t.Fatalf("integration moved inside equality/assignment boundary: got %s, want %s", got, targetSHA)
			}
			select {
			case moveErr := <-moverDone:
				t.Fatalf("cooperating integration move completed before assignment: %v", moveErr)
			default:
			}
		},
	}

	result, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID)
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	if result.BaseCommit != targetSHA {
		t.Fatalf("BaseCommit = %s, want assignment-linearized %s", result.BaseCommit, targetSHA)
	}
	if moveErr := <-moverDone; moveErr != nil {
		t.Fatalf("cooperating integration move: %v", moveErr)
	}
	if got := testhelpers.MustGit(t, fixture.projectRoot, "rev-parse", "integration"); got != laterSHA {
		t.Fatalf("integration = %s, want post-assignment move %s", got, laterSHA)
	}
}

func TestClaimTask_PreservedInitialDirtyWorktreeBecomesRecoveryState(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	advancePreservedClaimIntegration(t, fixture.projectRoot, "dependency.txt", "merged dependency\n", "Merge dependency")
	if err := os.WriteFile(filepath.Join(fixture.worktreeDir, "task.txt"), []byte("dirty task work\n"), 0o644); err != nil {
		t.Fatalf("dirty task worktree: %v", err)
	}

	_, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID)
	if err == nil || !strings.Contains(err.Error(), "preserved worktree is dirty") {
		t.Fatalf("ClaimTask() error = %v, want dirty recovery error", err)
	}
	assertPreservedInitialRecoveryState(t, fixture, "dirty")
}

func TestClaimTask_PreservedInitialNonConflictRebaseFailureBecomesRecoveryState(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	advancePreservedClaimIntegration(t, fixture.projectRoot, "dependency.txt", "merged dependency\n", "Merge dependency")
	hookPath := filepath.Join(fixture.projectRoot, ".git", "hooks", "pre-rebase")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write pre-rebase hook: %v", err)
	}

	_, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID)
	if err == nil || !strings.Contains(err.Error(), "preserved worktree rebase failed") {
		t.Fatalf("ClaimTask() error = %v, want non-conflict rebase recovery error", err)
	}
	assertPreservedInitialRecoveryState(t, fixture, "rebase failed")
	assertGitStatusClean(t, fixture.worktreeDir)
}

func TestClaimTask_PreservedInitialGenericRebaseFailureWithInProgressStateAborts(t *testing.T) {
	fixture := newPreservedInitialClaimFixtureWithBaseFile(t, "conflict.txt", "base\n")
	writeAndCommit(t, fixture.worktreeDir, "conflict.txt", "task\n", fixture.commitMessage)
	fixture.preservedHead = testhelpers.MustGit(t, fixture.worktreeDir, "rev-parse", "HEAD")
	writeAndCommit(t, fixture.projectRoot, "conflict.txt", "integration\n", "Merge conflicting dependency")
	targetSHA := testhelpers.MustGit(t, fixture.projectRoot, "rev-parse", "HEAD")
	testhelpers.MustGit(t, fixture.projectRoot, "branch", "-f", "integration", targetSHA)

	hookPath := filepath.Join(fixture.projectRoot, ".git", "hooks", "pre-rebase")
	hook := "#!/bin/sh\ngit rebase --no-verify \"$1\" >/dev/null 2>&1\nexit 1\n"
	if err := os.WriteFile(hookPath, []byte(hook), 0o755); err != nil {
		t.Fatalf("write pre-rebase hook: %v", err)
	}

	_, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID)
	if err == nil || !strings.Contains(err.Error(), "preserved worktree rebase failed") {
		t.Fatalf("ClaimTask() error = %v, want generic rebase recovery error", err)
	}
	if strings.Contains(err.Error(), "preserved worktree rebase conflict") {
		t.Fatalf("ClaimTask() error = %v, want generic failure classification", err)
	}
	assertPreservedInitialRecoveryState(t, fixture, "rebase failed")
	assertGitStatusClean(t, fixture.worktreeDir)
	if head := testhelpers.MustGit(t, fixture.worktreeDir, "rev-parse", "HEAD"); head != fixture.preservedHead {
		t.Fatalf("HEAD after aborted generic rebase failure = %s, want %s", head, fixture.preservedHead)
	}
}

func TestClaimTask_PreservedInitialRebaseConflictBecomesRecoveryState(t *testing.T) {
	fixture := newPreservedInitialClaimFixtureWithBaseFile(t, "conflict.txt", "base\n")
	writeAndCommit(t, fixture.worktreeDir, "conflict.txt", "task\n", fixture.commitMessage)
	fixture.preservedHead = testhelpers.MustGit(t, fixture.worktreeDir, "rev-parse", "HEAD")
	writeAndCommit(t, fixture.projectRoot, "conflict.txt", "integration\n", "Merge conflicting dependency")
	targetSHA := testhelpers.MustGit(t, fixture.projectRoot, "rev-parse", "HEAD")
	testhelpers.MustGit(t, fixture.projectRoot, "branch", "-f", "integration", targetSHA)

	_, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID)
	if err == nil || !strings.Contains(err.Error(), "preserved worktree rebase conflict") {
		t.Fatalf("ClaimTask() error = %v, want rebase conflict recovery error", err)
	}
	assertPreservedInitialRecoveryState(t, fixture, "rebase conflict")
	assertGitStatusClean(t, fixture.worktreeDir)
	if head := testhelpers.MustGit(t, fixture.worktreeDir, "rev-parse", "HEAD"); head != fixture.preservedHead {
		t.Fatalf("HEAD after aborted rebase = %s, want %s", head, fixture.preservedHead)
	}
}

type preservedInitialClaimFixture struct {
	projectRoot   string
	stateFile     string
	worktreeDir   string
	taskID        string
	agentID       string
	originalBase  string
	preservedHead string
	commitMessage string
	taskContent   string
}

func newPreservedInitialClaimFixture(t *testing.T) *preservedInitialClaimFixture {
	t.Helper()
	return newPreservedInitialClaimFixtureWithBaseFile(t, "", "")
}

func newPreservedInitialClaimFixtureWithBaseFile(t *testing.T, baseFile, baseContent string) *preservedInitialClaimFixture {
	t.Helper()
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	if baseFile != "" {
		writeAndCommit(t, projectRoot, baseFile, baseContent, "Add preserved claim base file")
		testhelpers.MustGit(t, projectRoot, "branch", "-f", "integration", "HEAD")
	}
	stateFile, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.CreateTestWorktree(t, projectRoot, "task-1")

	worktreeDir := filepath.Join(projectRoot, paths.WorktreesDirName, "task-1")
	originalBase := testhelpers.MustGit(t, projectRoot, "rev-parse", "integration")
	commitMessage := "Preserve task change"
	taskContent := "preserved task content\n"
	writeAndCommit(t, worktreeDir, "task.txt", taskContent, commitMessage)
	preservedHead := testhelpers.MustGit(t, worktreeDir, "rev-parse", "HEAD")

	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC())
	task.Worktree = testhelpers.StringPtr(filepath.Join(paths.WorktreesDirName, "task-1"))
	task.BaseCommit = &originalBase
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	return &preservedInitialClaimFixture{
		projectRoot: projectRoot, stateFile: stateFile, worktreeDir: worktreeDir,
		taskID: "task-1", agentID: "coder-1", originalBase: originalBase,
		preservedHead: preservedHead, commitMessage: commitMessage, taskContent: taskContent,
	}
}

func advancePreservedClaimIntegration(t *testing.T, projectRoot, name, content, message string) string {
	t.Helper()
	writeAndCommit(t, projectRoot, name, content, message)
	sha := testhelpers.MustGit(t, projectRoot, "rev-parse", "HEAD")
	testhelpers.MustGit(t, projectRoot, "branch", "-f", "integration", sha)
	return sha
}

func assertPreservedInitialRecoveryState(t *testing.T, fixture *preservedInitialClaimFixture, reasonContains string) {
	t.Helper()
	state := readClaimStateForTest(t, fixture.stateFile)
	task := state.FindTask(fixture.taskID)
	if task == nil {
		t.Fatal("task not found")
	}
	if task.Status != models.TaskStatusBlocked {
		t.Fatalf("status = %s, want BLOCKED", task.Status)
	}
	if task.AssignedTo != nil || task.LeaseExpires != nil {
		t.Fatalf("recovery task ownership = assigned_to %v lease %v, want unassigned", task.AssignedTo, task.LeaseExpires)
	}
	if task.BaseCommit == nil || *task.BaseCommit != fixture.originalBase {
		t.Fatalf("base_commit = %v, want unchanged %s", task.BaseCommit, fixture.originalBase)
	}
	if task.BlockedReason == nil || !strings.Contains(*task.BlockedReason, reasonContains) {
		t.Fatalf("blocked_reason = %v, want %q", task.BlockedReason, reasonContains)
	}
	if len(task.BlockedQuestions) == 0 || task.RepairRequest == nil || len(task.RepairRequest.Evidence) == 0 || len(task.RepairRequest.Validation) == 0 {
		t.Fatalf("recovery metadata is not actionable: %+v", task)
	}
	agent := state.Agents[fixture.agentID]
	if agent.CurrentTask != nil || agent.Status == models.AgentStatusWorking {
		t.Fatalf("agent = %+v, want unassigned", agent)
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

func withClaimTaskStacklitRuntimeRunner(t *testing.T, runner stacklit.RuntimeRunner) {
	t.Helper()
	stacklitRuntimeRunnerMu.Lock()
	previous := stacklitRuntimeRunner
	stacklitRuntimeRunner = runner
	stacklitRuntimeRunnerMu.Unlock()

	t.Cleanup(func() {
		stacklitRuntimeRunnerMu.Lock()
		stacklitRuntimeRunner = previous
		stacklitRuntimeRunnerMu.Unlock()
	})
}

func withClaimTaskFunctionalClustersRuntimeRunner(t *testing.T, runner functionalclusters.RuntimeRunner) {
	t.Helper()
	functionalClustersRuntimeRunnerMu.Lock()
	previous := functionalClustersRuntimeRunner
	functionalClustersRuntimeRunner = runner
	functionalClustersRuntimeRunnerMu.Unlock()

	t.Cleanup(func() {
		functionalClustersRuntimeRunnerMu.Lock()
		functionalClustersRuntimeRunner = previous
		functionalClustersRuntimeRunnerMu.Unlock()
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

type rejectedHandoffFixture struct {
	projectRoot string
	stateFile   string
	taskID      string
	worktreeRel string
	branchName  string
	baseCommit  string
	branchSHA   string
	reviewerID  string
	reviewLease time.Time
}

func newRejectedHandoffFixture(t *testing.T, createArtifact bool) *rejectedHandoffFixture {
	t.Helper()

	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	stateFile, _ := testhelpers.SetupLizaDir(t, projectRoot)
	now := time.Now().UTC()
	fixture := &rejectedHandoffFixture{
		projectRoot: projectRoot,
		stateFile:   stateFile,
		taskID:      "task-1",
		worktreeRel: filepath.Join(paths.WorktreesDirName, "task-1"),
		branchName:  paths.TaskBranchPrefix + "task-1",
		reviewerID:  "code-reviewer-1",
		reviewLease: now.Add(30 * time.Minute),
	}

	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	reviewer := testhelpers.RegisteredTestAgent("code-reviewer")
	reviewer.CurrentTask = &fixture.taskID
	reviewer.LeaseExpires = &fixture.reviewLease
	state.Agents[fixture.reviewerID] = reviewer

	task := testhelpers.BuildTaskByStatus(fixture.taskID, models.TaskStatusRejected, now)
	expiredLease := now.Add(-time.Minute)
	task.LeaseExpires = &expiredLease
	task.ReviewingBy = &fixture.reviewerID
	task.ReviewLeaseExpires = &fixture.reviewLease

	if createArtifact {
		gitWrapper := git.New(projectRoot)
		baseCommit, err := gitWrapper.CreateWorktree(fixture.taskID, "integration")
		if err != nil {
			t.Fatalf("CreateWorktree() error: %v", err)
		}
		fixture.baseCommit = baseCommit
		task.Worktree = &fixture.worktreeRel
		task.BaseCommit = &fixture.baseCommit

		worktreeDir := filepath.Join(projectRoot, fixture.worktreeRel)
		marker := filepath.Join(worktreeDir, "rejected-change.txt")
		if err := os.WriteFile(marker, []byte("preserved rejected work\n"), 0o644); err != nil {
			t.Fatalf("WriteFile() error: %v", err)
		}
		testhelpers.MustGit(t, worktreeDir, "add", "rejected-change.txt")
		testhelpers.MustGit(t, worktreeDir, "commit", "-m", "Preserve rejected work")
		fixture.branchSHA = strings.TrimSpace(testhelpers.MustGit(t, projectRoot, "rev-parse", fixture.branchName))
	} else {
		task.Worktree = nil
		task.BaseCommit = nil
	}

	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)
	return fixture
}

func assertRejectedClaimState(t *testing.T, fixture *rejectedHandoffFixture, agentID, wantBranchSHA string) {
	t.Helper()

	gitWrapper := git.New(fixture.projectRoot)
	branchSHA, err := gitWrapper.GetCommitSHA(fixture.branchName)
	if err != nil {
		t.Fatalf("GetCommitSHA(%s) error: %v", fixture.branchName, err)
	}
	if branchSHA != wantBranchSHA {
		t.Fatalf("branch SHA = %s, want %s", branchSHA, wantBranchSHA)
	}

	state := readClaimStateForTest(t, fixture.stateFile)
	task := state.FindTask(fixture.taskID)
	if task == nil {
		t.Fatal("claimed task not found")
	}
	if task.Status != models.TaskStatusImplementing {
		t.Fatalf("Status = %s, want %s", task.Status, models.TaskStatusImplementing)
	}
	if task.AssignedTo == nil || *task.AssignedTo != agentID || task.LeaseExpires == nil {
		t.Fatalf("claim ownership = assigned_to %v lease %v, want %s with lease", task.AssignedTo, task.LeaseExpires, agentID)
	}
	assertRejectedRecoveryMetadata(t, task, fixture)
	assertRejectedReviewerAffinity(t, state, task, fixture)
	if err := statevalidate.ValidateState(state, fixture.projectRoot, true, io.Discard); err != nil {
		t.Fatalf("claimed rejected state is invalid: %v", err)
	}
}

func assertRejectedRecoveryMetadata(t *testing.T, task *models.Task, fixture *rejectedHandoffFixture) {
	t.Helper()
	if task.Worktree == nil || *task.Worktree != fixture.worktreeRel ||
		task.BaseCommit == nil || *task.BaseCommit != fixture.baseCommit {
		t.Fatalf("recovery tuple = worktree %v base %v, want %q at %q", task.Worktree, task.BaseCommit, fixture.baseCommit, fixture.worktreeRel)
	}
}

func assertRejectedReviewerAffinity(t *testing.T, _ *models.State, task *models.Task, fixture *rejectedHandoffFixture) {
	t.Helper()
	if task.ReviewingBy == nil || *task.ReviewingBy != fixture.reviewerID ||
		task.ReviewLeaseExpires == nil || !task.ReviewLeaseExpires.Equal(fixture.reviewLease) {
		t.Fatalf("reviewer affinity = reviewer %v lease %v, want %q through %s", task.ReviewingBy, task.ReviewLeaseExpires, fixture.reviewerID, fixture.reviewLease)
	}
}
