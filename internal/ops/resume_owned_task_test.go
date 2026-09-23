package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestResumeOwnedTask_Validation(t *testing.T) {
	t.Parallel()
	_, err := ResumeOwnedTask(ResumeOwnedTaskInput{
		ProjectRoot: "/tmp",
		AgentID:     "",
	})
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "agent ID is required") {
		t.Errorf("Error = %q, want to contain 'agent ID is required'", err.Error())
	}
}

func TestResumeOwnedTask_SuccessRepairsRestartedAgentCurrentTask(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateTestWorktree(t, tmpDir, "task-1")

	now := time.Now().UTC()
	agentID := "coder-1"
	state := testhelpers.CreateValidState()
	state.Config.LeaseDuration = 120
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	task.AssignedTo = &agentID
	task.HandoffPending = false
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = resumableOwnedAgent(models.RoleCoder, models.AgentStatusIdle, nil, now)
	testhelpers.WriteInitialState(t, stateFile, state)

	callStart := time.Now().UTC()
	result, err := ResumeOwnedTask(ResumeOwnedTaskInput{
		ProjectRoot: tmpDir,
		AgentID:     agentID,
	})
	if err != nil {
		t.Fatalf("ResumeOwnedTask() error: %v", err)
	}

	if !result.Found {
		t.Fatal("Found = false, want true")
	}
	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want task-1", result.TaskID)
	}
	if result.Worktree != ".worktrees/task-1" {
		t.Errorf("Worktree = %q, want .worktrees/task-1", result.Worktree)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	readTask := readState.FindTask("task-1")
	if readTask == nil {
		t.Fatal("task-1 not found")
	}
	if readTask.LeaseExpires == nil || !readTask.LeaseExpires.After(callStart) {
		t.Errorf("LeaseExpires = %v, want renewed after %v", readTask.LeaseExpires, callStart)
	}
	if len(readTask.History) == 0 || readTask.History[len(readTask.History)-1].Event != models.TaskEventOwnedTaskResumed {
		t.Errorf("last history event = %v, want %s", readTask.History, models.TaskEventOwnedTaskResumed)
	}

	agent := readState.Agents[agentID]
	if agent.Status != models.AgentStatusWorking {
		t.Errorf("agent status = %s, want %s", agent.Status, models.AgentStatusWorking)
	}
	if agent.CurrentTask == nil || *agent.CurrentTask != "task-1" {
		t.Errorf("agent current_task = %v, want task-1", agent.CurrentTask)
	}
}

func TestResumeOwnedTask_CodePlannerWithoutCurrentTask(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateTestWorktree(t, tmpDir, "plan-1")

	now := time.Now().UTC()
	agentID := "code-planner-1"
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("plan-1", models.TaskStatusCodePlanning, now)
	task.AssignedTo = &agentID
	leaseExpires := now.Add(30 * time.Minute)
	task.LeaseExpires = &leaseExpires
	worktree := ".worktrees/plan-1"
	task.Worktree = &worktree
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = resumableOwnedAgent(models.RoleCodePlanner, models.AgentStatusIdle, nil, now)
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ResumeOwnedTask(ResumeOwnedTaskInput{
		ProjectRoot: tmpDir,
		AgentID:     agentID,
	})
	if err != nil {
		t.Fatalf("ResumeOwnedTask() error: %v", err)
	}
	if !result.Found {
		t.Fatal("Found = false, want true")
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	agent := readState.Agents[agentID]
	if agent.CurrentTask == nil || *agent.CurrentTask != "plan-1" {
		t.Errorf("agent current_task = %v, want plan-1", agent.CurrentTask)
	}
}

func TestResumeOwnedTask_BlocksMissingWorktreeMetadata(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	agentID := "coder-1"
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	task.AssignedTo = &agentID
	task.Worktree = nil
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = resumableOwnedAgent(models.RoleCoder, models.AgentStatusWorking, &task.ID, now)
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ResumeOwnedTask(ResumeOwnedTaskInput{
		ProjectRoot: tmpDir,
		AgentID:     agentID,
	})
	if err != nil {
		t.Fatalf("ResumeOwnedTask() error: %v", err)
	}
	if !result.Blocked {
		t.Fatal("Blocked = false, want true")
	}

	assertTaskBlockedForOwnedResume(t, stateFile, "task-1", "no worktree in state")
}

func TestResumeOwnedTask_BlocksMissingWorktreeOnDisk(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	agentID := "coder-1"
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	task.AssignedTo = &agentID
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = resumableOwnedAgent(models.RoleCoder, models.AgentStatusWorking, &task.ID, now)
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ResumeOwnedTask(ResumeOwnedTaskInput{
		ProjectRoot: tmpDir,
		AgentID:     agentID,
	})
	if err != nil {
		t.Fatalf("ResumeOwnedTask() error: %v", err)
	}
	if !result.Blocked {
		t.Fatal("Blocked = false, want true")
	}

	assertTaskBlockedForOwnedResume(t, stateFile, "task-1", "worktree not healthy")
}

func TestResumeOwnedTask_BlocksOrphanedWorktreeWithoutGitLink(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	agentID := "coder-1"
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	task.AssignedTo = &agentID
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = resumableOwnedAgent(models.RoleCoder, models.AgentStatusWorking, &task.ID, now)
	testhelpers.WriteInitialState(t, stateFile, state)

	if err := os.MkdirAll(filepath.Join(tmpDir, ".worktrees", "task-1"), 0755); err != nil {
		t.Fatalf("create orphaned worktree dir: %v", err)
	}

	result, err := ResumeOwnedTask(ResumeOwnedTaskInput{
		ProjectRoot: tmpDir,
		AgentID:     agentID,
	})
	if err != nil {
		t.Fatalf("ResumeOwnedTask() error: %v", err)
	}
	if !result.Blocked {
		t.Fatal("Blocked = false, want true")
	}

	assertTaskBlockedForOwnedResume(t, stateFile, "task-1", "worktree not healthy")
}

func TestResumeOwnedTask_DoesNotBlockOrResumeIfOwnershipChangesDuringValidation(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	agentID := "coder-1"
	otherAgentID := "coder-2"
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	task.AssignedTo = &agentID
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = resumableOwnedAgent(models.RoleCoder, models.AgentStatusWorking, &task.ID, now)
	state.Agents[otherAgentID] = resumableOwnedAgent(models.RoleCoder, models.AgentStatusIdle, nil, now)
	testhelpers.WriteInitialState(t, stateFile, state)

	oldValidate := validateOwnedResumeWorktreeHealth
	validateOwnedResumeWorktreeHealth = func(_ *git.Git, taskID string) error {
		err := db.New(stateFile).Modify(func(s *models.State) error {
			tk := s.FindTask(taskID)
			if tk != nil {
				tk.AssignedTo = &otherAgentID
			}
			s.ReleaseAgent(agentID)
			return nil
		})
		if err != nil {
			return err
		}
		return fmt.Errorf("simulated unhealthy worktree")
	}
	defer func() { validateOwnedResumeWorktreeHealth = oldValidate }()

	result, err := ResumeOwnedTask(ResumeOwnedTaskInput{
		ProjectRoot: tmpDir,
		AgentID:     agentID,
	})
	if err != nil {
		t.Fatalf("ResumeOwnedTask() error: %v", err)
	}
	if result.Found || result.Blocked {
		t.Fatalf("result = %+v, want no resume and no block", result)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	readTask := readState.FindTask("task-1")
	if readTask == nil {
		t.Fatal("task-1 not found")
	}
	if readTask.Status != models.TaskStatusImplementing {
		t.Errorf("status = %s, want %s", readTask.Status, models.TaskStatusImplementing)
	}
	if readTask.AssignedTo == nil || *readTask.AssignedTo != otherAgentID {
		t.Errorf("assigned_to = %v, want %s", readTask.AssignedTo, otherAgentID)
	}
}

func TestResumeOwnedTask_PredicateGuardsRoleAndCurrentTask(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	testhelpers.SetupPipelineConfig(t, tmpDir)
	pr, err := LoadResolverForModels(tmpDir)
	if err != nil {
		t.Fatalf("LoadResolverForModels() error: %v", err)
	}

	now := time.Now().UTC()
	agentID := "coder-1"
	otherTask := "task-2"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	task.AssignedTo = &agentID

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = resumableOwnedAgent(models.RoleCodePlanner, models.AgentStatusIdle, nil, now)
	if models.IsResumableOwnedTask(state, &state.Tasks[0], agentID, pr) {
		t.Fatal("wrong role should not be resumable")
	}

	state.Agents[agentID] = resumableOwnedAgent(models.RoleCoder, models.AgentStatusIdle, &otherTask, now)
	if models.IsResumableOwnedTask(state, &state.Tasks[0], agentID, pr) {
		t.Fatal("agent already working another task should not be resumable")
	}

	state.Agents[agentID] = resumableOwnedAgent(models.RoleCoder, models.AgentStatusIdle, nil, now)
	if !models.IsResumableOwnedTask(state, &state.Tasks[0], agentID, pr) {
		t.Fatal("owned executing task with compatible agent should be resumable")
	}
}

func resumableOwnedAgent(role string, status models.AgentStatus, currentTask *string, now time.Time) models.Agent {
	leaseExpires := now.Add(30 * time.Minute)
	return models.Agent{
		Role:         role,
		Status:       status,
		CurrentTask:  currentTask,
		LeaseExpires: &leaseExpires,
		Heartbeat:    now,
		Terminal:     "test",
		Provider:     "test",
		PID:          os.Getpid(),
	}
}

func assertTaskBlockedForOwnedResume(t *testing.T, stateFile, taskID, reasonContains string) {
	t.Helper()

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	task := readState.FindTask(taskID)
	if task == nil {
		t.Fatalf("%s not found", taskID)
	}
	if task.Status != models.TaskStatusBlocked {
		t.Fatalf("status = %s, want %s", task.Status, models.TaskStatusBlocked)
	}
	if task.AssignedTo != nil {
		t.Errorf("assigned_to = %v, want nil", *task.AssignedTo)
	}
	if task.LeaseExpires != nil {
		t.Errorf("lease_expires = %v, want nil", task.LeaseExpires)
	}
	if task.BlockedReason == nil || !strings.Contains(*task.BlockedReason, reasonContains) {
		t.Errorf("blocked_reason = %v, want to contain %q", task.BlockedReason, reasonContains)
	}
	if len(task.BlockedQuestions) == 0 {
		t.Fatal("blocked_questions empty, want repair question")
	}
	if len(task.History) == 0 || task.History[len(task.History)-1].Event != models.TaskEventBlocked {
		t.Errorf("last history = %v, want blocked event", task.History)
	}
}
