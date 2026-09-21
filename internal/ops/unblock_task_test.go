package ops

import (
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestUnblockTask_RestoresExecutingStateAndAssignment(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.LeaseDuration = 1800
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.RolePair = "code-planning-pair"
	task.Worktree = testhelpers.StringPtr(".worktrees/task-1")
	task.RepairRequest = &models.RepairRequest{
		Operation:  "restore_git_write_access",
		Target:     ".git/worktrees/task-1",
		Command:    "git -C .worktrees/task-1 add plan.md",
		Evidence:   []string{"command=git -C .worktrees/task-1 add plan.md exit_code=128 stderr=fatal: Unable to create index.lock: Read-only file system"},
		Validation: []string{"git -C .worktrees/task-1 add plan.md"},
	}
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	bb := db.New(stateFile)
	testhelpers.RegisterTestAgent(t, bb, "code-planner-1", "code-planner")
	setAgentPID(t, bb, "code-planner-1", os.Getpid())

	result, err := UnblockTask(tmpDir, "task-1", "code-planner-1", "git metadata repair verified", "orchestrator-1")
	if err != nil {
		t.Fatalf("UnblockTask() error: %v", err)
	}
	if result.ToStatus != models.TaskStatusCodePlanning {
		t.Fatalf("ToStatus = %s, want %s", result.ToStatus, models.TaskStatusCodePlanning)
	}
	if result.AssignedTo != "code-planner-1" {
		t.Fatalf("AssignedTo = %q, want code-planner-1", result.AssignedTo)
	}

	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Read state: %v", err)
	}
	readTask := readState.FindTask("task-1")
	if readTask == nil {
		t.Fatal("task not found")
	}
	if readTask.Status != models.TaskStatusCodePlanning {
		t.Errorf("Status = %s, want %s", readTask.Status, models.TaskStatusCodePlanning)
	}
	if readTask.AssignedTo == nil || *readTask.AssignedTo != "code-planner-1" {
		t.Fatalf("AssignedTo = %v, want code-planner-1", readTask.AssignedTo)
	}
	if readTask.LeaseExpires == nil {
		t.Fatal("LeaseExpires is nil")
	}
	if readTask.BlockedReason != nil {
		t.Fatal("BlockedReason should be cleared")
	}
	if len(readTask.BlockedQuestions) != 0 {
		t.Fatalf("BlockedQuestions len = %d, want 0", len(readTask.BlockedQuestions))
	}
	if readTask.RepairRequest != nil {
		t.Fatal("RepairRequest should be cleared")
	}
	last := readTask.History[len(readTask.History)-1]
	if last.Event != models.TaskEventUnblocked {
		t.Fatalf("History event = %q, want %q", last.Event, models.TaskEventUnblocked)
	}
	validation, ok := last.Extra["repair_validation"].([]any)
	if !ok {
		t.Fatalf("repair_validation history extra = %T, want []any", last.Extra["repair_validation"])
	}
	if len(validation) != 1 || validation[0] != "git -C .worktrees/task-1 add plan.md" {
		t.Fatalf("repair_validation = %v", validation)
	}

	agent := readState.Agents["code-planner-1"]
	if agent.Status != models.AgentStatusWorking {
		t.Fatalf("Agent status = %s, want %s", agent.Status, models.AgentStatusWorking)
	}
	if agent.CurrentTask == nil || *agent.CurrentTask != "task-1" {
		t.Fatalf("Agent current task = %v, want task-1", agent.CurrentTask)
	}
}

func TestUnblockTask_AllowsAssignToWithoutLiveProcess(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.RolePair = "code-planning-pair"
	task.Worktree = testhelpers.StringPtr(".worktrees/task-1")
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	bb := db.New(stateFile)
	testhelpers.RegisterTestAgent(t, bb, "code-planner-1", "code-planner")
	if err := bb.Modify(func(s *models.State) error {
		agent := s.Agents["code-planner-1"]
		agent.PID = -1
		s.Agents["code-planner-1"] = agent
		return nil
	}); err != nil {
		t.Fatalf("Failed to corrupt agent PID: %v", err)
	}

	result, err := UnblockTask(tmpDir, "task-1", "code-planner-1", "repair verified", "orchestrator-1")
	if err != nil {
		t.Fatalf("UnblockTask() error: %v", err)
	}
	if result.AssignedTo != "code-planner-1" {
		t.Fatalf("AssignedTo = %q, want code-planner-1", result.AssignedTo)
	}
}

func TestUnblockTask_WithoutAssignToMakesTaskClaimable(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.RolePair = "code-planning-pair"
	task.Worktree = nil
	task.BaseCommit = nil
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := UnblockTaskWithOptions(tmpDir, "task-1", "repair verified", "orchestrator-1", UnblockTaskOptions{})
	if err != nil {
		t.Fatalf("UnblockTaskWithOptions() error: %v", err)
	}
	if !result.Claimable {
		t.Fatal("Claimable = false, want true")
	}
	if result.LeaseExpires != nil {
		t.Fatalf("LeaseExpires = %v, want nil", result.LeaseExpires)
	}
	if result.ToStatus != models.TaskStatusDraftCodingPlan {
		t.Fatalf("ToStatus = %s, want %s", result.ToStatus, models.TaskStatusDraftCodingPlan)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Read state: %v", err)
	}
	updated := readState.FindTask("task-1")
	if updated == nil {
		t.Fatal("task not found")
	}
	if updated.Status != models.TaskStatusDraftCodingPlan {
		t.Fatalf("Status = %s, want %s", updated.Status, models.TaskStatusDraftCodingPlan)
	}
	if updated.AssignedTo != nil {
		t.Fatalf("AssignedTo = %v, want nil", *updated.AssignedTo)
	}
	if updated.Worktree != nil {
		t.Fatalf("Worktree = %v, want nil", *updated.Worktree)
	}
	if updated.BlockedReason != nil || len(updated.BlockedQuestions) != 0 || updated.RepairRequest != nil {
		t.Fatalf("blocked metadata not cleared: %+v", updated)
	}
}

func TestClaimTask_PreservesUnblockedWorktree(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateTestWorktree(t, tmpDir, "task-1")

	baseCommit := testhelpers.MustGit(t, tmpDir, "rev-parse", "integration")
	wtDir := filepath.Join(tmpDir, ".worktrees", "task-1")
	planPath := filepath.Join(wtDir, "plan.md")
	if err := os.WriteFile(planPath, []byte("preserved plan\n"), 0644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	testhelpers.MustGit(t, wtDir, "add", "plan.md")
	testhelpers.MustGit(t, wtDir, "commit", "-m", "Preserved plan")
	preservedHead := testhelpers.MustGit(t, wtDir, "rev-parse", "HEAD")

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.RolePair = "code-planning-pair"
	task.BaseCommit = &baseCommit
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	if _, err := UnblockTaskWithOptions(tmpDir, "task-1", "repair verified", "orchestrator-1", UnblockTaskOptions{}); err != nil {
		t.Fatalf("UnblockTaskWithOptions() error: %v", err)
	}

	bb := db.New(stateFile)
	testhelpers.RegisterTestAgent(t, bb, "code-planner-1", "code-planner")
	setAgentPID(t, bb, "code-planner-1", os.Getpid())
	result, err := ClaimTask(tmpDir, "task-1", "code-planner-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	if result.WorktreeRecreated {
		t.Fatal("WorktreeRecreated = true, want false")
	}
	claimedHead := testhelpers.MustGit(t, wtDir, "rev-parse", "HEAD")
	if claimedHead != preservedHead {
		t.Fatalf("HEAD = %s, want preserved %s", claimedHead, preservedHead)
	}
	if _, err := os.Stat(planPath); err != nil {
		t.Fatalf("preserved file missing after claim: %v", err)
	}
}

func TestUnblockTask_RebaseOnMakesClaimableAndUpdatesBaseCommit(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateTestWorktree(t, tmpDir, "task-1")

	baseCommit := testhelpers.MustGit(t, tmpDir, "rev-parse", "integration")
	wtDir := filepath.Join(tmpDir, ".worktrees", "task-1")
	writeAndCommit(t, wtDir, "task.txt", "task work\n", "Task work")
	oldHead := testhelpers.MustGit(t, wtDir, "rev-parse", "HEAD")
	writeAndCommit(t, tmpDir, "integration.txt", "integration move\n", "Move integration")
	targetSHA := testhelpers.MustGit(t, tmpDir, "rev-parse", "HEAD")
	testhelpers.MustGit(t, tmpDir, "branch", "-f", "integration", targetSHA)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.RolePair = "code-planning-pair"
	task.BaseCommit = &baseCommit
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := UnblockTaskWithOptions(tmpDir, "task-1", "repair verified", "orchestrator-1", UnblockTaskOptions{RebaseOn: "integration"})
	if err != nil {
		t.Fatalf("UnblockTaskWithOptions() error: %v", err)
	}
	if result.Rebase == nil {
		t.Fatal("Rebase result is nil")
	}
	if result.Rebase.OldHead != oldHead {
		t.Fatalf("OldHead = %s, want %s", result.Rebase.OldHead, oldHead)
	}
	if result.Rebase.TargetSHA != targetSHA {
		t.Fatalf("TargetSHA = %s, want %s", result.Rebase.TargetSHA, targetSHA)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Read state: %v", err)
	}
	updated := readState.FindTask("task-1")
	if updated == nil {
		t.Fatal("task not found")
	}
	if updated.Status != models.TaskStatusDraftCodingPlan {
		t.Fatalf("Status = %s, want %s", updated.Status, models.TaskStatusDraftCodingPlan)
	}
	if updated.BaseCommit == nil || *updated.BaseCommit != targetSHA {
		t.Fatalf("BaseCommit = %v, want %s", updated.BaseCommit, targetSHA)
	}
	if result.Rebase.NewHead != testhelpers.MustGit(t, wtDir, "rev-parse", "HEAD") {
		t.Fatalf("NewHead does not match worktree HEAD")
	}
}

func TestUnblockTask_RebaseOnAssignToResumesAndUpdatesBaseCommit(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateTestWorktree(t, tmpDir, "task-1")

	baseCommit := testhelpers.MustGit(t, tmpDir, "rev-parse", "integration")
	wtDir := filepath.Join(tmpDir, ".worktrees", "task-1")
	writeAndCommit(t, wtDir, "task.txt", "task work\n", "Task work")
	oldHead := testhelpers.MustGit(t, wtDir, "rev-parse", "HEAD")
	writeAndCommit(t, tmpDir, "integration.txt", "integration move\n", "Move integration")
	targetSHA := testhelpers.MustGit(t, tmpDir, "rev-parse", "HEAD")
	testhelpers.MustGit(t, tmpDir, "branch", "-f", "integration", targetSHA)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.RolePair = "code-planning-pair"
	task.BaseCommit = &baseCommit
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	bb := db.New(stateFile)
	testhelpers.RegisterTestAgent(t, bb, "code-planner-1", "code-planner")
	if err := bb.Modify(func(s *models.State) error {
		agent := s.Agents["code-planner-1"]
		agent.PID = -1
		s.Agents["code-planner-1"] = agent
		return nil
	}); err != nil {
		t.Fatalf("set dead agent PID: %v", err)
	}

	result, err := UnblockTaskWithOptions(tmpDir, "task-1", "repair verified", "orchestrator-1", UnblockTaskOptions{
		AssignTo: "code-planner-1",
		RebaseOn: "integration",
	})
	if err != nil {
		t.Fatalf("UnblockTaskWithOptions() error: %v", err)
	}
	if result.Claimable {
		t.Fatal("Claimable = true, want false")
	}
	if result.ToStatus != models.TaskStatusCodePlanning {
		t.Fatalf("ToStatus = %s, want %s", result.ToStatus, models.TaskStatusCodePlanning)
	}
	if result.AssignedTo != "code-planner-1" {
		t.Fatalf("AssignedTo = %q, want code-planner-1", result.AssignedTo)
	}
	if result.LeaseExpires == nil {
		t.Fatal("LeaseExpires is nil, want direct-resume lease")
	}
	if result.Rebase == nil {
		t.Fatal("Rebase result is nil")
	}
	if result.Rebase.OldHead != oldHead {
		t.Fatalf("OldHead = %s, want %s", result.Rebase.OldHead, oldHead)
	}
	if result.Rebase.TargetSHA != targetSHA {
		t.Fatalf("TargetSHA = %s, want %s", result.Rebase.TargetSHA, targetSHA)
	}

	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Read state: %v", err)
	}
	updated := readState.FindTask("task-1")
	if updated == nil {
		t.Fatal("task not found")
	}
	if updated.Status != models.TaskStatusCodePlanning {
		t.Fatalf("Status = %s, want %s", updated.Status, models.TaskStatusCodePlanning)
	}
	if updated.BaseCommit == nil || *updated.BaseCommit != targetSHA {
		t.Fatalf("BaseCommit = %v, want %s", updated.BaseCommit, targetSHA)
	}
	agent := readState.Agents["code-planner-1"]
	if agent.CurrentTask == nil || *agent.CurrentTask != "task-1" {
		t.Fatalf("Agent current task = %v, want task-1", agent.CurrentTask)
	}
}

func TestUnblockTask_RebaseOnRejectsTrackedDirtyWithoutAllowDirty(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateTestWorktree(t, tmpDir, "task-1")

	baseCommit := testhelpers.MustGit(t, tmpDir, "rev-parse", "integration")
	wtDir := filepath.Join(tmpDir, ".worktrees", "task-1")
	writeAndCommit(t, wtDir, "task.txt", "task work\n", "Task work")
	if err := os.WriteFile(filepath.Join(wtDir, "task.txt"), []byte("dirty task work\n"), 0644); err != nil {
		t.Fatalf("dirty write: %v", err)
	}
	writeAndCommit(t, tmpDir, "integration.txt", "integration move\n", "Move integration")
	targetSHA := testhelpers.MustGit(t, tmpDir, "rev-parse", "HEAD")
	testhelpers.MustGit(t, tmpDir, "branch", "-f", "integration", targetSHA)

	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, time.Now().UTC())
	task.RolePair = "code-planning-pair"
	task.BaseCommit = &baseCommit
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := UnblockTaskWithOptions(tmpDir, "task-1", "repair verified", "orchestrator-1", UnblockTaskOptions{RebaseOn: "integration"})
	if err == nil {
		t.Fatal("expected dirty worktree error, got nil")
	}
	if !strings.Contains(err.Error(), "--allow-dirty") {
		t.Fatalf("error = %q, want --allow-dirty hint", err.Error())
	}
}

func TestUnblockTask_RebaseOnRejectsUntrackedOverwrite(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateTestWorktree(t, tmpDir, "task-1")

	baseCommit := testhelpers.MustGit(t, tmpDir, "rev-parse", "integration")
	wtDir := filepath.Join(tmpDir, ".worktrees", "task-1")
	if err := os.WriteFile(filepath.Join(wtDir, "future.txt"), []byte("local untracked\n"), 0644); err != nil {
		t.Fatalf("write untracked: %v", err)
	}
	writeAndCommit(t, tmpDir, "future.txt", "integration file\n", "Add future file")
	targetSHA := testhelpers.MustGit(t, tmpDir, "rev-parse", "HEAD")
	testhelpers.MustGit(t, tmpDir, "branch", "-f", "integration", targetSHA)

	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, time.Now().UTC())
	task.RolePair = "code-planning-pair"
	task.BaseCommit = &baseCommit
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := UnblockTaskWithOptions(tmpDir, "task-1", "repair verified", "orchestrator-1", UnblockTaskOptions{RebaseOn: "integration"})
	if err == nil {
		t.Fatal("expected untracked overwrite error, got nil")
	}
	if !strings.Contains(err.Error(), "untracked files") || !strings.Contains(err.Error(), "future.txt") {
		t.Fatalf("error = %q, want untracked future.txt", err.Error())
	}
}

func TestUnblockTask_RebaseConflictLeavesBlockedWithRepairRequest(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	writeAndCommit(t, tmpDir, "conflict.txt", "base\n", "Add base conflict file")
	testhelpers.MustGit(t, tmpDir, "branch", "-f", "integration", "HEAD")
	testhelpers.CreateTestWorktree(t, tmpDir, "task-1")

	baseCommit := testhelpers.MustGit(t, tmpDir, "rev-parse", "integration")
	wtDir := filepath.Join(tmpDir, ".worktrees", "task-1")
	writeAndCommit(t, wtDir, "conflict.txt", "task\n", "Task conflict edit")
	writeAndCommit(t, tmpDir, "conflict.txt", "integration\n", "Integration conflict edit")
	targetSHA := testhelpers.MustGit(t, tmpDir, "rev-parse", "HEAD")
	testhelpers.MustGit(t, tmpDir, "branch", "-f", "integration", targetSHA)

	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, time.Now().UTC())
	task.RolePair = "code-planning-pair"
	task.BaseCommit = &baseCommit
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := UnblockTaskWithOptions(tmpDir, "task-1", "repair verified", "orchestrator-1", UnblockTaskOptions{RebaseOn: "integration"})
	if err == nil {
		t.Fatal("expected rebase conflict error, got nil")
	}
	var unblockConflict *UnblockRebaseConflictError
	if !stderrors.As(err, &unblockConflict) {
		t.Fatalf("error = %T %v, want UnblockRebaseConflictError", err, err)
	}

	readState, readErr := db.New(stateFile).Read()
	if readErr != nil {
		t.Fatalf("Read state: %v", readErr)
	}
	updated := readState.FindTask("task-1")
	if updated == nil {
		t.Fatal("task not found")
	}
	if updated.Status != models.TaskStatusBlocked {
		t.Fatalf("Status = %s, want BLOCKED", updated.Status)
	}
	if updated.BlockedReason == nil || !strings.Contains(*updated.BlockedReason, "rebase conflict") {
		t.Fatalf("BlockedReason = %v, want rebase conflict", updated.BlockedReason)
	}
	if updated.RepairRequest == nil {
		t.Fatal("RepairRequest is nil")
	}
	if updated.RepairRequest.Operation != "resolve_unblock_rebase_conflict" {
		t.Fatalf("RepairRequest.Operation = %q", updated.RepairRequest.Operation)
	}
	if updated.IntegrationFailure != nil {
		t.Fatalf("IntegrationFailure = %v, want nil", updated.IntegrationFailure)
	}
	last := updated.History[len(updated.History)-1]
	if last.Event != models.TaskEventBlocked {
		t.Fatalf("last event = %s, want blocked", last.Event)
	}
	if status := testhelpers.MustGit(t, wtDir, "status", "--short"); strings.Contains(status, "UU ") {
		t.Fatalf("rebase was not aborted; status = %q", status)
	}
}

func TestUnblockTask_WithoutAssignToAllowsPendingDependency(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.RolePair = "code-planning-pair"
	task.Worktree = nil
	task.BaseCommit = nil
	task.DependsOn = []string{"dep-1"}
	task.RepairRequest = &models.RepairRequest{
		Operation:  "repair_dependency_graph",
		Target:     "task-1",
		Command:    "repair dependency edge",
		Evidence:   []string{"dependency edge repaired"},
		Validation: []string{"validate dependency graph"},
	}
	dep := testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusImplementing, now)
	dep.RolePair = "code-planning-pair"
	state.Tasks = []models.Task{task, dep}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := UnblockTaskWithOptions(tmpDir, "task-1", "repair verified", "orchestrator-1", UnblockTaskOptions{})
	if err != nil {
		t.Fatalf("UnblockTaskWithOptions() error: %v", err)
	}
	if result.ToStatus != models.TaskStatusDraftCodingPlan {
		t.Fatalf("ToStatus = %s, want %s", result.ToStatus, models.TaskStatusDraftCodingPlan)
	}
	if result.Claimable {
		t.Fatal("Claimable = true, want false while dependency is pending")
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Read state: %v", err)
	}
	updated := readState.FindTask("task-1")
	if updated == nil {
		t.Fatal("task not found")
	}
	if updated.Status != models.TaskStatusDraftCodingPlan {
		t.Fatalf("Status = %s, want %s", updated.Status, models.TaskStatusDraftCodingPlan)
	}
	if updated.AssignedTo != nil || updated.LeaseExpires != nil {
		t.Fatalf("assignment metadata not cleared: assigned_to=%v lease_expires=%v", updated.AssignedTo, updated.LeaseExpires)
	}
	if updated.BlockedReason != nil || len(updated.BlockedQuestions) != 0 || updated.RepairRequest != nil {
		t.Fatalf("blocked metadata not cleared: %+v", updated)
	}
	last := updated.History[len(updated.History)-1]
	if last.Event != models.TaskEventUnblocked {
		t.Fatalf("History event = %q, want %q", last.Event, models.TaskEventUnblocked)
	}
	if last.Extra["repair_operation"] != "repair_dependency_graph" {
		t.Fatalf("repair_operation = %v, want repair_dependency_graph", last.Extra["repair_operation"])
	}
	validation, ok := last.Extra["repair_validation"].([]any)
	if !ok || len(validation) != 1 || validation[0] != "validate dependency graph" {
		t.Fatalf("repair_validation = %#v, want archived validation", last.Extra["repair_validation"])
	}
}

func TestUnblockTask_WithoutAssignToRejectsInvalidOrSupersededDependencies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(task *models.Task, state *models.State, now time.Time)
		wantError string
	}{
		{
			name: "missing dependency",
			configure: func(task *models.Task, _ *models.State, _ time.Time) {
				task.DependsOn = []string{"missing-dep"}
			},
			wantError: "invalid dependency: missing-dep (invalid_missing",
		},
		{
			name: "superseded dependency with pending replacement",
			configure: func(task *models.Task, state *models.State, now time.Time) {
				task.DependsOn = []string{"superseded-dep"}
				superseded := testhelpers.BuildTaskByStatus("superseded-dep", models.TaskStatusSuperseded, now)
				superseded.SupersededBy = []string{"replacement-dep"}
				superseded.RescopeReason = testhelpers.StringPtr("replaced")
				replacement := testhelpers.BuildTaskByStatus("replacement-dep", models.TaskStatusImplementing, now)
				state.Tasks = append(state.Tasks, superseded, replacement)
			},
			wantError: "unsatisfied_superseded",
		},
		{
			name: "self dependency",
			configure: func(task *models.Task, _ *models.State, _ time.Time) {
				task.DependsOn = []string{"task-1"}
			},
			wantError: "cannot depend on itself",
		},
		{
			name: "dependency cycle",
			configure: func(task *models.Task, state *models.State, now time.Time) {
				task.DependsOn = []string{"dep-1"}
				dep := testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusImplementing, now)
				dep.DependsOn = []string{"task-1"}
				state.Tasks = append(state.Tasks, dep)
			},
			wantError: "dependency cycle",
		},
		{
			name: "terminal non-merged dependency",
			configure: func(task *models.Task, state *models.State, now time.Time) {
				task.DependsOn = []string{"dep-1"}
				dep := testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusAbandoned, now)
				state.Tasks = append(state.Tasks, dep)
			},
			wantError: "terminal non-MERGED dependency",
		},
		{
			name: "untrimmed dependency",
			configure: func(task *models.Task, state *models.State, now time.Time) {
				task.DependsOn = []string{" dep-1 "}
				dep := testhelpers.BuildTaskByStatus(" dep-1 ", models.TaskStatusImplementing, now)
				state.Tasks = append(state.Tasks, dep)
			},
			wantError: `invalid depends_on entry " dep-1 "`,
		},
		{
			name: "duplicate dependency",
			configure: func(task *models.Task, state *models.State, now time.Time) {
				task.DependsOn = []string{"dep-1", "dep-1"}
				dep := testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusImplementing, now)
				state.Tasks = append(state.Tasks, dep)
			},
			wantError: `duplicate depends_on entry "dep-1"`,
		},
		{
			name: "downstream dependency",
			configure: func(task *models.Task, state *models.State, now time.Time) {
				task.DependsOn = []string{"dep-1"}
				dep := testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusImplementing, now)
				dep.RolePair = "coding-pair"
				state.Tasks = append(state.Tasks, dep)
			},
			wantError: "downstream dependency",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testhelpers.SetupTestGitRepo(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

			now := time.Now().UTC()
			state := testhelpers.CreateValidState()
			task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
			task.RolePair = "code-planning-pair"
			task.Worktree = nil
			task.BaseCommit = nil
			state.Tasks = []models.Task{task}
			tt.configure(&state.Tasks[0], state, now)
			testhelpers.WriteInitialState(t, stateFile, state)

			_, err := UnblockTaskWithOptions(tmpDir, "task-1", "repair verified", "orchestrator-1", UnblockTaskOptions{})
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Error = %q, want substring %q", err.Error(), tt.wantError)
			}

			readState, readErr := db.New(stateFile).Read()
			if readErr != nil {
				t.Fatalf("Read state: %v", readErr)
			}
			updated := readState.FindTask("task-1")
			if updated == nil || updated.Status != models.TaskStatusBlocked {
				t.Fatalf("task status after rejected unblock = %v, want %s", updated, models.TaskStatusBlocked)
			}
		})
	}
}

func TestUnblockTask_WithoutAssignToRejectsPipelineTerminalDependencies(t *testing.T) {
	t.Parallel()

	pipelineConfig, err := os.ReadFile(filepath.Join(testhelpers.FindRepoRoot(t), "internal", "pipeline", "testdata", "valid-with-clean.yaml"))
	if err != nil {
		t.Fatalf("Read pipeline config: %v", err)
	}

	tests := []struct {
		name   string
		status models.TaskStatus
	}{
		{name: "configured clean", status: "INTEGRATION_ANALYSIS_CLEAN"},
		{name: "transition-source approved", status: "INTEGRATION_ANALYSIS_APPROVED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testhelpers.SetupTestGitRepo(t, tmpDir)
			testhelpers.SetupPipelineConfigBytes(t, tmpDir, pipelineConfig)
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

			now := time.Now().UTC()
			state := testhelpers.CreateValidState()
			task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
			task.RolePair = "coding-pair"
			task.Worktree = nil
			task.BaseCommit = nil
			task.DependsOn = []string{"dep-1"}
			dep := testhelpers.BuildTaskByStatus("dep-1", tt.status, now)
			dep.RolePair = "integration-pair"
			state.Tasks = []models.Task{task, dep}
			testhelpers.WriteInitialState(t, stateFile, state)

			_, err := UnblockTaskWithOptions(tmpDir, "task-1", "repair verified", "orchestrator-1", UnblockTaskOptions{})
			if err == nil {
				t.Fatalf("UnblockTaskWithOptions accepted pipeline-terminal dependency %s", tt.status)
			}
			wantError := fmt.Sprintf("terminal non-MERGED dependency dep-1 (%s)", tt.status)
			if !strings.Contains(err.Error(), wantError) {
				t.Fatalf("Error = %q, want substring %q", err.Error(), wantError)
			}

			readState, readErr := db.New(stateFile).Read()
			if readErr != nil {
				t.Fatalf("Read state: %v", readErr)
			}
			updated := readState.FindTask("task-1")
			if updated == nil || updated.Status != models.TaskStatusBlocked {
				t.Fatalf("task status after rejected unblock = %v, want %s", updated, models.TaskStatusBlocked)
			}
		})
	}
}

func TestUnblockTask_WithAssignToRejectsPendingDependency(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.RolePair = "code-planning-pair"
	task.Worktree = testhelpers.StringPtr(".worktrees/task-1")
	task.DependsOn = []string{"dep-1"}
	dep := testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusImplementing, now)
	dep.RolePair = "code-planning-pair"
	state.Tasks = []models.Task{task, dep}
	testhelpers.WriteInitialState(t, stateFile, state)

	bb := db.New(stateFile)
	testhelpers.RegisterTestAgent(t, bb, "code-planner-1", "code-planner")
	setAgentPID(t, bb, "code-planner-1", os.Getpid())

	_, err := UnblockTask(tmpDir, "task-1", "code-planner-1", "repair verified", "orchestrator-1")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unmet dependencies: dep-1") {
		t.Fatalf("Error = %q, want unmet dependencies", err.Error())
	}
}

func TestUnblockTask_AllowsMergedDependency(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.RolePair = "code-planning-pair"
	task.Worktree = testhelpers.StringPtr(".worktrees/task-1")
	task.DependsOn = []string{"dep-1"}
	dep := testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusMerged, now)
	dep.RolePair = "code-planning-pair"
	state.Tasks = []models.Task{task, dep}
	testhelpers.WriteInitialState(t, stateFile, state)

	bb := db.New(stateFile)
	testhelpers.RegisterTestAgent(t, bb, "code-planner-1", "code-planner")
	setAgentPID(t, bb, "code-planner-1", os.Getpid())

	_, err := UnblockTask(tmpDir, "task-1", "code-planner-1", "repair verified", "orchestrator-1")
	if err != nil {
		t.Fatalf("UnblockTask() error: %v", err)
	}
}

func TestUnblockTask_RejectsUnknownAssignTo(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.RolePair = "code-planning-pair"
	task.Worktree = testhelpers.StringPtr(".worktrees/task-1")
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := UnblockTask(tmpDir, "task-1", "code-planner-99", "repair verified", "orchestrator-1")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "is not registered") {
		t.Fatalf("Error = %q, want not registered", err.Error())
	}
}

func TestUnblockTask_RejectsWrongDoerRole(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.RolePair = "code-planning-pair"
	task.Worktree = testhelpers.StringPtr(".worktrees/task-1")
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	bb := db.New(stateFile)
	testhelpers.RegisterTestAgent(t, bb, "coder-1", "coder")

	_, err := UnblockTask(tmpDir, "task-1", "coder-1", "repair verified", "orchestrator-1")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "does not match task doer role") {
		t.Fatalf("Error = %q, want doer role mismatch", err.Error())
	}
}

func setAgentPID(t *testing.T, bb *db.Blackboard, agentID string, pid int) {
	t.Helper()
	err := bb.Modify(func(state *models.State) error {
		agent := state.Agents[agentID]
		agent.PID = pid
		state.Agents[agentID] = agent
		return nil
	})
	if err != nil {
		t.Fatalf("set agent PID: %v", err)
	}
}

func writeAndCommit(t *testing.T, repoDir, relPath, content, message string) {
	t.Helper()
	absPath := filepath.Join(repoDir, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		t.Fatalf("mkdir for %s: %v", relPath, err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
	testhelpers.MustGit(t, repoDir, "add", relPath)
	testhelpers.MustGit(t, repoDir, "commit", "-m", message)
}

// gatedRejectionRCATask builds a BLOCKED task whose rejection-RCA gate fired at
// threshold 4 on its fourth durable rejection, with nothing recorded yet. The
// typed blocked_reason prefix is what state validation requires of a gate-open
// task, so a fixture without it would never round-trip.
func gatedRejectionRCATask(taskID string, now time.Time) models.Task {
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusBlocked, now)
	task.RolePair = "code-planning-pair"
	task.Iteration = 3
	task.ReviewCyclesCurrent = 4
	task.ReviewCyclesTotal = 4
	reason := models.BlockedReasonRejectionRCARequired + ": 4 durable rejections reached threshold 4"
	task.BlockedReason = &reason
	task.RejectionRCA = &models.RejectionRCARecord{
		SchemaVersion:  models.RejectionRCASchemaVersion,
		Threshold:      4,
		RejectionCount: 4,
		GatedAt:        now.Add(-time.Minute),
		GatingCommit:   "0123456789abcdef0123456789abcdef01234567",
	}
	return task
}

// closeRejectionRCAGate records an RCA and the disposition a recovery path
// authorizes, which is the only state from which unblock-task may restore.
func closeRejectionRCAGate(task *models.Task, recoveryPath string, now time.Time) {
	recordedRejectionRCA(mixedCauseRequest(), "orchestrator-1", now)(task)
	mode := models.RejectionRCARestoreMode(recoveryPath)
	task.RejectionRCA.Disposition = &models.RejectionRCADisposition{
		RecoveryPath:     recoveryPath,
		RestoreMode:      mode,
		Actor:            "orchestrator-1",
		LifecycleVersion: 7,
		DecidedAt:        now,
		Rationale:        "disposition authorized for the unblock path under test",
		IterationExempt:  mode == models.RestoreModeAssign,
	}
}

func TestUnblockTaskRefusesOpenRejectionRCAGate(t *testing.T) {
	t.Parallel()

	// A gated task is restorable only through record-rejection-rca then
	// resume-rejection-rca, so the refusal must name both regardless of which
	// step is still outstanding.
	gateCases := []struct {
		name        string
		recorded    bool
		missingStep string
	}{
		{name: "no RCA recorded", recorded: false, missingStep: "record-rejection-rca"},
		{name: "RCA recorded, no disposition", recorded: true, missingStep: "resume-rejection-rca"},
	}
	for _, testCase := range gateCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			testhelpers.SetupTestGitRepo(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

			now := time.Now().UTC()
			state := testhelpers.CreateValidState()
			task := gatedRejectionRCATask("task-1", now)
			if testCase.recorded {
				recordedRejectionRCA(mixedCauseRequest(), "orchestrator-1", now)(&task)
			}
			state.Tasks = []models.Task{task}
			testhelpers.WriteInitialState(t, stateFile, state)
			before := string(readStateBytes(t, stateFile))

			_, err := UnblockTaskWithOptions(tmpDir, "task-1", "repair verified", "orchestrator-1", UnblockTaskOptions{})
			outcome := requirePrecondition(t, err)
			if outcome.Outcome != models.LifecycleStateChanged || outcome.SafeAction != "requery" {
				t.Errorf("outcome = %s/%s, want STATE_CHANGED/requery — the gate closes through other operations", outcome.Outcome, outcome.SafeAction)
			}
			for _, want := range []string{"record-rejection-rca", "resume-rejection-rca", testCase.missingStep} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal = %q, want it to name %s", err.Error(), want)
				}
			}
			if after := string(readStateBytes(t, stateFile)); after != before {
				t.Error("state and history changed during a refused unblock")
			}
		})
	}

	t.Run("non-gated task still unblocks", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		testhelpers.SetupTestGitRepo(t, tmpDir)
		testhelpers.SetupPipelineConfig(t, tmpDir)
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
		task.RolePair = "code-planning-pair"
		task.Worktree = nil
		task.BaseCommit = nil
		state.Tasks = []models.Task{task}
		testhelpers.WriteInitialState(t, stateFile, state)

		result, err := UnblockTaskWithOptions(tmpDir, "task-1", "repair verified", "orchestrator-1", UnblockTaskOptions{})
		if err != nil {
			t.Fatalf("UnblockTaskWithOptions() error: %v", err)
		}
		if result.ToStatus != models.TaskStatusDraftCodingPlan {
			t.Fatalf("ToStatus = %s, want %s", result.ToStatus, models.TaskStatusDraftCodingPlan)
		}
	})
}

func TestUnblockTaskEnforcesRestoreMode(t *testing.T) {
	t.Parallel()

	t.Run("assign is refused without --assign-to", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		testhelpers.SetupTestGitRepo(t, tmpDir)
		testhelpers.SetupPipelineConfig(t, tmpDir)
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		task := gatedRejectionRCATask("task-1", now)
		closeRejectionRCAGate(&task, models.RecoveryCapabilityReroute, now)
		state.Tasks = []models.Task{task}
		testhelpers.WriteInitialState(t, stateFile, state)
		before := string(readStateBytes(t, stateFile))

		_, err := UnblockTaskWithOptions(tmpDir, "task-1", "capability rerouted", "orchestrator-1", UnblockTaskOptions{})
		outcome := requirePrecondition(t, err)
		if outcome.Outcome != models.LifecycleInvalidInput || outcome.SafeAction != "correct_input" {
			t.Errorf("outcome = %s/%s, want INVALID_INPUT/correct_input — the missing flag is a payload defect", outcome.Outcome, outcome.SafeAction)
		}
		if !strings.Contains(err.Error(), "--assign-to") {
			t.Errorf("refusal = %q, want it to name --assign-to", err.Error())
		}
		if after := string(readStateBytes(t, stateFile)); after != before {
			t.Error("state changed during a refused unblock")
		}
	})

	t.Run("assign with --assign-to resumes without a product iteration", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		testhelpers.SetupTestGitRepo(t, tmpDir)
		testhelpers.SetupPipelineConfig(t, tmpDir)
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		task := gatedRejectionRCATask("task-1", now)
		closeRejectionRCAGate(&task, models.RecoveryLifecycleRepair, now)
		state.Tasks = []models.Task{task}
		testhelpers.WriteInitialState(t, stateFile, state)

		bb := db.New(stateFile)
		testhelpers.RegisterTestAgent(t, bb, "code-planner-1", "code-planner")
		setAgentPID(t, bb, "code-planner-1", os.Getpid())

		result, err := UnblockTask(tmpDir, "task-1", "code-planner-1", "ownership repaired", "orchestrator-1")
		if err != nil {
			t.Fatalf("UnblockTask() error: %v", err)
		}
		if result.ToStatus != models.TaskStatusCodePlanning {
			t.Fatalf("ToStatus = %s, want %s", result.ToStatus, models.TaskStatusCodePlanning)
		}
		updated := mustReadTask(t, stateFile, "task-1")
		if updated.Iteration != 3 {
			t.Fatalf("Iteration = %d, want the pre-gate 3 — an assign restore consumes no product iteration", updated.Iteration)
		}
	})

	t.Run("claimable restores to the initial status and the next claim iterates", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		testhelpers.SetupTestGitRepo(t, tmpDir)
		testhelpers.SetupPipelineConfig(t, tmpDir)
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		task := gatedRejectionRCATask("task-1", now)
		task.Worktree = nil
		task.BaseCommit = nil
		closeRejectionRCAGate(&task, models.RecoveryImplementationCorrection, now)
		state.Tasks = []models.Task{task}
		testhelpers.WriteInitialState(t, stateFile, state)

		result, err := UnblockTaskWithOptions(tmpDir, "task-1", "implementation correction authorized", "orchestrator-1", UnblockTaskOptions{})
		if err != nil {
			t.Fatalf("UnblockTaskWithOptions() error: %v", err)
		}
		if result.ToStatus != models.TaskStatusDraftCodingPlan {
			t.Fatalf("ToStatus = %s, want %s", result.ToStatus, models.TaskStatusDraftCodingPlan)
		}
		if unblocked := mustReadTask(t, stateFile, "task-1"); unblocked.Iteration != 3 {
			t.Fatalf("Iteration after unblock = %d, want the pre-gate 3", unblocked.Iteration)
		}

		bb := db.New(stateFile)
		testhelpers.RegisterTestAgent(t, bb, "code-planner-1", "code-planner")
		setAgentPID(t, bb, "code-planner-1", os.Getpid())
		if _, err := ClaimTask(tmpDir, "task-1", "code-planner-1"); err != nil {
			t.Fatalf("ClaimTask() error: %v", err)
		}
		if claimed := mustReadTask(t, stateFile, "task-1"); claimed.Iteration != 4 {
			t.Fatalf("Iteration after claim = %d, want 4", claimed.Iteration)
		}
	})

	t.Run("none is refused", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		testhelpers.SetupTestGitRepo(t, tmpDir)
		testhelpers.SetupPipelineConfig(t, tmpDir)
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		task := gatedRejectionRCATask("task-1", now)
		closeRejectionRCAGate(&task, models.RecoveryRescope, now)
		state.Tasks = []models.Task{task}
		testhelpers.WriteInitialState(t, stateFile, state)
		before := string(readStateBytes(t, stateFile))

		_, err := UnblockTaskWithOptions(tmpDir, "task-1", "rescoped", "orchestrator-1", UnblockTaskOptions{})
		outcome := requirePrecondition(t, err)
		if outcome.SafeAction != "stop" {
			t.Errorf("safe_action = %s, want stop — no unblock form restores a rescoped task", outcome.SafeAction)
		}
		for _, want := range []string{models.RecoveryRescope, models.RestoreModeNone} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal = %q, want it to name %s", err.Error(), want)
			}
		}
		if after := string(readStateBytes(t, stateFile)); after != before {
			t.Error("state changed during a refused unblock")
		}
	})
}

func TestUnblockTaskRetainsRejectionRCARecord(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := gatedRejectionRCATask("task-1", now)
	task.Worktree = nil
	task.BaseCommit = nil
	closeRejectionRCAGate(&task, models.RecoveryHumanOverride, now)
	fingerprint := task.RejectionRCA.Fingerprint
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	if _, err := UnblockTaskWithOptions(tmpDir, "task-1", "override authorized", "orchestrator-1", UnblockTaskOptions{}); err != nil {
		t.Fatalf("UnblockTaskWithOptions() error: %v", err)
	}

	// The record is audit and telemetry input, so unblock-task keeps it where
	// it clears blocked_reason and repair_request.
	updated := mustReadTask(t, stateFile, "task-1")
	if updated.RejectionRCA == nil {
		t.Fatal("rejection_rca record was dropped by a successful unblock")
	}
	if updated.RejectionRCA.Fingerprint != fingerprint {
		t.Fatalf("fingerprint = %q, want %q", updated.RejectionRCA.Fingerprint, fingerprint)
	}
	if updated.RejectionRCA.Disposition == nil || updated.RejectionRCA.Disposition.RecoveryPath != models.RecoveryHumanOverride {
		t.Fatalf("disposition = %+v, want the recorded human_override", updated.RejectionRCA.Disposition)
	}
	if updated.RejectionRCAGateOpen() {
		t.Error("gate reopened after a successful unblock")
	}
}

func mustReadTask(t *testing.T, stateFile, taskID string) *models.Task {
	t.Helper()
	state, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Read state: %v", err)
	}
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatalf("task %s not found", taskID)
	}
	return task
}
