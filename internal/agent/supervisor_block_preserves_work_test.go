package agent

import (
	"context"
	stderrors "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	lizagit "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D-101: a supervisor block must not destroy a doer's work. A session that
// ended on a provider error while a background job kept the CLI alive tripped
// the progress watchdog, whose block deleted the worktree and task branch with
// 75 minutes of uncommitted work in them.

const (
	doerWorkTrackedContent   = "# Test\ndoer edit\n"
	doerWorkStagedName       = "staged.txt"
	doerWorkStagedContent    = "staged work\n"
	doerWorkUntrackedName    = "untracked.txt"
	doerWorkUntrackedContent = "untracked work\n"
)

// leaveDoerWork leaves a tracked edit, a staged new file and an untracked file
// in the worktree, as a doer interrupted mid-task does.
func leaveDoerWork(t *testing.T, worktreeDir string) {
	t.Helper()
	writeTestFile(t, filepath.Join(worktreeDir, "README.md"), doerWorkTrackedContent)
	writeTestFile(t, filepath.Join(worktreeDir, doerWorkStagedName), doerWorkStagedContent)
	testhelpers.MustGit(t, worktreeDir, "add", doerWorkStagedName)
	writeTestFile(t, filepath.Join(worktreeDir, doerWorkUntrackedName), doerWorkUntrackedContent)
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// assertDoerWorkOnDisk checks every piece of the doer's work is still in the
// worktree, committed or not.
func assertDoerWorkOnDisk(t *testing.T, worktreeDir string) {
	t.Helper()
	for name, want := range map[string]string{
		"README.md":           doerWorkTrackedContent,
		doerWorkStagedName:    doerWorkStagedContent,
		doerWorkUntrackedName: doerWorkUntrackedContent,
	} {
		got, err := os.ReadFile(filepath.Join(worktreeDir, name))
		if err != nil {
			t.Fatalf("doer work %s lost: %v", name, err)
		}
		if string(got) != want {
			t.Fatalf("doer work %s = %q, want %q", name, got, want)
		}
	}
}

// assertDoerWorkCommitted checks the work became exactly one commit on top of
// base and the worktree is clean.
func assertDoerWorkCommitted(t *testing.T, worktreeDir, base string) string {
	t.Helper()
	if status := testhelpers.MustGit(t, worktreeDir, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after WIP commit:\n%s", status)
	}
	head := testhelpers.MustGit(t, worktreeDir, "rev-parse", "HEAD")
	if parent := testhelpers.MustGit(t, worktreeDir, "rev-parse", "HEAD^"); parent != base {
		t.Fatalf("WIP commit parent = %s, want base %s", parent, base)
	}
	for name, want := range map[string]string{
		"README.md":           doerWorkTrackedContent,
		doerWorkStagedName:    doerWorkStagedContent,
		doerWorkUntrackedName: doerWorkUntrackedContent,
	} {
		if got := testhelpers.MustGit(t, worktreeDir, "show", "HEAD:"+name); got+"\n" != want {
			t.Fatalf("WIP commit %s = %q, want %q", name, got, want)
		}
	}
	return head
}

// assertWorktreeAndBranchKept checks neither the worktree nor the task branch
// was deleted and the task still records its worktree.
func assertWorktreeAndBranchKept(t *testing.T, projectRoot string, task *models.Task) {
	t.Helper()
	if task.Worktree == nil || *task.Worktree != filepath.ToSlash(filepath.Join(paths.WorktreesDirName, task.ID)) {
		t.Fatalf("task %s worktree = %v, want it kept", task.ID, task.Worktree)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, *task.Worktree)); err != nil {
		t.Fatalf("worktree directory removed: %v", err)
	}
	exists, err := lizagit.New(projectRoot).BranchExists(paths.TaskBranchPrefix + task.ID)
	if err != nil {
		t.Fatalf("BranchExists: %v", err)
	}
	if !exists {
		t.Fatalf("task branch %s deleted", paths.TaskBranchPrefix+task.ID)
	}
}

type watchdogFixture struct {
	projectRoot string
	statePath   string
	bb          *db.Blackboard
	taskID      string
	agentID     string
	baseCommit  string
	worktreeDir string
}

// newWatchdogFixture sets up coder-1 executing task-1 in a real worktree.
func newWatchdogFixture(t *testing.T) *watchdogFixture {
	t.Helper()
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)

	now := time.Now().UTC()
	taskID, agentID := "task-1", "coder-1"
	baseCommit, err := lizagit.New(projectRoot).CreateWorktree(taskID, "main")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now)
	task.AssignedTo = &agentID
	task.BaseCommit = &baseCommit
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	state.Agents = map[string]models.Agent{
		agentID: {Role: models.RoleCoder, Status: models.AgentStatusWorking, CurrentTask: &taskID, Heartbeat: now},
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	return &watchdogFixture{
		projectRoot: projectRoot,
		statePath:   statePath,
		bb:          bb,
		taskID:      taskID,
		agentID:     agentID,
		baseCommit:  baseCommit,
		worktreeDir: filepath.Join(projectRoot, paths.WorktreesDirName, taskID),
	}
}

func (f *watchdogFixture) config(t *testing.T, cliName string, mock *MockLLMAgent) SupervisorConfig {
	t.Helper()
	return SupervisorConfig{
		AgentID:                  f.agentID,
		Authority:                testSupervisorAuthority(t, f.bb, f.agentID),
		Role:                     models.RoleCoder,
		ProjectRoot:              f.projectRoot,
		StatePath:                f.statePath,
		CLIName:                  cliName,
		LLMAgent:                 mock,
		ExecutionTimeout:         5 * time.Second,
		ExecutionProgressTimeout: 150 * time.Millisecond,
	}
}

func (f *watchdogFixture) task(t *testing.T) *models.Task {
	t.Helper()
	state, err := db.For(f.statePath).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	task := state.FindTask(f.taskID)
	if task == nil {
		t.Fatalf("task %s missing", f.taskID)
	}
	return task
}

// stalledSessionWithWork leaves doer work, then goes silent until the
// watchdog cancels it, as a CLI kept alive by a background job does.
func stalledSessionWithWork(t *testing.T, worktreeDir string) func(context.Context, string, string, string, string, []string) error {
	return func(ctx context.Context, _ string, _ string, _ string, _ string, _ []string) error {
		leaveDoerWork(t, worktreeDir)
		<-ctx.Done()
		return ctx.Err()
	}
}

// The incident's terminal provider event, shortened.
const claudeQuotaResultEvent = `{"type":"result","subtype":"success","is_error":true,"terminal_reason":"api_error","api_error_status":429,"result":"You've hit your session limit · resets 1:40am (Europe/Paris)"}`

const claudeUnavailableResultEvent = `{"type":"result","subtype":"success","is_error":true,"result":"Failed to authenticate: OAuth session expired and could not be refreshed"}`

// A watchdog timeout after a provider-ended session is a provider failure,
// not a stalled task: the session must reach the provider-failure exit path
// with the task still owned and its work in place.
func TestExecuteAgentProgressTimeoutAfterProviderEndedSessionIsAProviderFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output string
	}{
		{name: "quota", output: claudeQuotaResultEvent},
		{name: "unavailable", output: claudeUnavailableResultEvent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newWatchdogFixture(t)
			mock := &MockLLMAgent{Output: tc.output + "\n", OnExecute: stalledSessionWithWork(t, f.worktreeDir)}

			exitCode, output, err := executeAgent(context.Background(), f.config(t, "claude", mock), "prompt", nil, f.taskID, models.Config{})
			if err != nil {
				t.Fatalf("executeAgent error: %v", err)
			}
			if exitCode == 0 {
				t.Fatal("exitCode = 0, want a non-zero provider-failure exit")
			}
			if output != tc.output+"\n" {
				t.Fatalf("output = %q, want the provider output passed through", output)
			}
			task := f.task(t)
			if task.Status != models.TaskStatusImplementing || task.AssignedTo == nil || *task.AssignedTo != f.agentID {
				t.Fatalf("task = %s assigned %v, want still IMPLEMENTING for %s", task.Status, task.AssignedTo, f.agentID)
			}
			assertWorktreeAndBranchKept(t, f.projectRoot, task)
			assertDoerWorkOnDisk(t, f.worktreeDir)
		})
	}
}

// After the provider-failure exit, the supervisor's own post-exit steps keep
// the work: a quota ending gets the supervisor WIP commit; an unavailable one
// keeps the dirty worktree, which the next claim adopts.
func TestProviderEndedWatchdogExitKeepsWorkThroughReleaseAndReclaim(t *testing.T) {
	for _, tc := range []struct {
		name          string
		output        string
		supervisorWIP bool
	}{
		{name: "quota", output: claudeQuotaResultEvent, supervisorWIP: true},
		{name: "unavailable", output: claudeUnavailableResultEvent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newWatchdogFixture(t)
			mock := &MockLLMAgent{Output: tc.output + "\n", OnExecute: stalledSessionWithWork(t, f.worktreeDir)}
			config := f.config(t, "claude", mock)

			exitCode, output, err := executeAgent(context.Background(), config, "prompt", nil, f.taskID, models.Config{})
			if err != nil {
				t.Fatalf("executeAgent error: %v", err)
			}
			if exitCode == 0 {
				t.Fatal("exitCode = 0, want a non-zero provider-failure exit")
			}

			// The supervisor's post-exit sequence for that exit.
			commitQuotaTerminationWIP(context.Background(), config, exitCode, f.taskID, output)
			if tc.supervisorWIP {
				assertDoerWorkCommitted(t, f.worktreeDir, f.baseCommit)
			} else if head := testhelpers.MustGit(t, f.worktreeDir, "rev-parse", "HEAD"); head != f.baseCommit {
				t.Fatalf("HEAD = %s, want no supervisor WIP commit for an unavailable-only ending", head)
			}
			if err := resetAgentAfterExit(f.bb, config.Authority, f.projectRoot); err != nil {
				t.Fatalf("resetAgentAfterExit: %v", err)
			}
			released := f.task(t)
			if released.AssignedTo != nil {
				t.Fatalf("claim not released: assigned_to = %v", *released.AssignedTo)
			}
			assertWorktreeAndBranchKept(t, f.projectRoot, released)

			claimAsSecondCoder(t, f)
			assertDoerWorkCommitted(t, f.worktreeDir, f.baseCommit)
		})
	}
}

// claimAsSecondCoder registers coder-2 and claims the task as it.
func claimAsSecondCoder(t *testing.T, f *watchdogFixture) {
	t.Helper()
	if err := f.bb.Modify(func(state *models.State) error {
		state.Agents["coder-2"] = testhelpers.RegisteredTestAgent(models.RoleCoder)
		return nil
	}); err != nil {
		t.Fatalf("register coder-2: %v", err)
	}
	if _, err := ops.ClaimTask(f.projectRoot, f.taskID, "coder-2"); err != nil {
		t.Fatalf("ClaimTask by coder-2: %v", err)
	}
	task := f.task(t)
	if task.AssignedTo == nil || *task.AssignedTo != "coder-2" {
		t.Fatalf("task assigned to %v, want coder-2", task.AssignedTo)
	}
}

// The issue's validation: a watchdog block keeps the work as a WIP commit on
// the kept branch, and unblock plus a fresh claim resumes from it.
func TestWatchdogBlockPreservesWorkForUnblockAndReclaim(t *testing.T) {
	f := newWatchdogFixture(t)
	mock := &MockLLMAgent{OnExecute: stalledSessionWithWork(t, f.worktreeDir)}

	if _, _, err := executeAgent(context.Background(), f.config(t, "codex", mock), "prompt", nil, f.taskID, models.Config{}); err != nil {
		t.Fatalf("executeAgent error: %v", err)
	}
	blocked := f.task(t)
	if blocked.Status != models.TaskStatusBlocked {
		t.Fatalf("task status = %s, want BLOCKED", blocked.Status)
	}
	assertWorktreeAndBranchKept(t, f.projectRoot, blocked)
	wip := assertDoerWorkCommitted(t, f.worktreeDir, f.baseCommit)

	if _, err := ops.UnblockTask(f.projectRoot, f.taskID, "", "environment recovered", "orchestrator-1"); err != nil {
		t.Fatalf("UnblockTask: %v", err)
	}
	claimAsSecondCoder(t, f)
	if head := testhelpers.MustGit(t, f.worktreeDir, "rev-parse", "HEAD"); head != wip {
		t.Fatalf("reclaimed HEAD = %s, want the block's WIP commit %s", head, wip)
	}
	assertDoerWorkOnDisk(t, f.worktreeDir)
}

// A reviewer-path block must not commit on the doer's behalf, and must not
// delete the task's worktree or branch either.
func TestReviewerSupervisorBlockLeavesGitStateUntouched(t *testing.T) {
	f := newWatchdogFixture(t)
	reviewerID := "code-reviewer-1"
	if err := f.bb.Modify(func(state *models.State) error {
		task := state.FindTask(f.taskID)
		task.Status = models.TaskStatusReviewing
		reviewCommit := f.baseCommit
		task.ReviewCommit = &reviewCommit
		task.ReviewingBy = &reviewerID
		task.ReviewLeaseExpires = task.LeaseExpires
		reviewer := testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)
		reviewer.Status = models.AgentStatusReviewing
		reviewer.CurrentTask = &f.taskID
		state.Agents[reviewerID] = reviewer
		return nil
	}); err != nil {
		t.Fatalf("move task to review: %v", err)
	}
	writeTestFile(t, filepath.Join(f.worktreeDir, doerWorkUntrackedName), doerWorkUntrackedContent)

	if err := blockTaskFromSupervisor(f.bb, f.projectRoot, f.taskID, testSupervisorAuthority(t, f.bb, reviewerID), "prompt context build failed"); err != nil {
		t.Fatalf("blockTaskFromSupervisor: %v", err)
	}
	task := f.task(t)
	if task.Status != models.TaskStatusBlocked {
		t.Fatalf("task status = %s, want BLOCKED", task.Status)
	}
	assertWorktreeAndBranchKept(t, f.projectRoot, task)
	if head := testhelpers.MustGit(t, f.worktreeDir, "rev-parse", "HEAD"); head != f.baseCommit {
		t.Fatalf("HEAD = %s, want %s: a reviewer block committed on the doer's behalf", head, f.baseCommit)
	}
	if status := testhelpers.MustGit(t, f.worktreeDir, "status", "--porcelain"); status != "?? "+doerWorkUntrackedName {
		t.Fatalf("worktree status = %q, want only the untracked file", status)
	}
}

// A WIP commit that fails must not stop the block or cost any content.
func TestSupervisorBlockWhoseWIPCommitFailsKeepsContent(t *testing.T) {
	f := newWatchdogFixture(t)
	leaveDoerWork(t, f.worktreeDir)
	gitDir := testhelpers.MustGit(t, f.worktreeDir, "rev-parse", "--absolute-git-dir")
	writeTestFile(t, filepath.Join(gitDir, "index.lock"), "")

	if err := blockTaskFromSupervisor(f.bb, f.projectRoot, f.taskID, testSupervisorAuthority(t, f.bb, f.agentID), "spinning detected"); err != nil {
		t.Fatalf("blockTaskFromSupervisor: %v", err)
	}
	task := f.task(t)
	if task.Status != models.TaskStatusBlocked {
		t.Fatalf("task status = %s, want BLOCKED", task.Status)
	}
	assertWorktreeAndBranchKept(t, f.projectRoot, task)
	if head := testhelpers.MustGit(t, f.worktreeDir, "rev-parse", "HEAD"); head != f.baseCommit {
		t.Fatalf("HEAD = %s, want %s after a failed WIP commit", head, f.baseCommit)
	}
	assertDoerWorkOnDisk(t, f.worktreeDir)
}

// runSupervisorOnClaimedTask runs a supervisor that claims taskID from READY
// and drives the mock until the loop ends.
func runSupervisorOnClaimedTask(t *testing.T, mutate func(*models.Config), mock *MockLLMAgent) (projectRoot, statePath, taskID string) {
	t.Helper()
	projectRoot = t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ = testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)

	taskID = "task-loop"
	state := testhelpers.CreateValidState()
	state.Config.CoderPollInterval = 1
	state.Config.DoerMaxWait = 1
	state.Config.LeaseDuration = 300
	mutate(&state.Config)
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReady, time.Now().UTC())}
	testhelpers.WriteInitialState(t, statePath, state)
	recordSupervisorDelays(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), supervisorRunHangGuard)
	defer cancel()
	if err := RunSupervisor(ctx, SupervisorConfig{
		AgentID:          "coder-1",
		Role:             models.RoleCoder,
		ProjectRoot:      projectRoot,
		StatePath:        statePath,
		LogPath:          filepath.Join(projectRoot, paths.ProjectDirName(), "log.yaml"),
		SpecsDir:         filepath.Join(projectRoot, "specs"),
		CLIName:          "codex",
		LLMAgent:         mock,
		ExecutionTimeout: 10 * time.Second,
	}); err != nil {
		t.Fatalf("RunSupervisor: %v", err)
	}
	return projectRoot, statePath, taskID
}

func readLoopTask(t *testing.T, statePath, taskID string) *models.Task {
	t.Helper()
	state, err := db.For(statePath).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatalf("task %s missing", taskID)
	}
	return task
}

// The crash-loop block runs after the claim was released; it must still block,
// and the dirty work and branch must survive it.
func TestRunSupervisorCrashLoopBlockPreservesDirtyWork(t *testing.T) {
	mock := &MockLLMAgent{ExitCode: 1, ExitError: stderrors.New("provider exit status 1")}
	mock.OnExecute = func(_ context.Context, _ string, _ string, _ string, projectRoot string, _ []string) error {
		leaveDoerWork(t, filepath.Join(projectRoot, paths.WorktreesDirName, "task-loop"))
		return nil
	}
	projectRoot, statePath, taskID := runSupervisorOnClaimedTask(t, func(cfg *models.Config) {
		cfg.CrashRestartThreshold = 1
		cfg.SpinningRestartThreshold = 10
	}, mock)

	task := readLoopTask(t, statePath, taskID)
	if task.Status != models.TaskStatusBlocked || task.BlockedReason == nil || !strings.Contains(*task.BlockedReason, "crash restart loop detected") {
		t.Fatalf("task = %s reason %v, want BLOCKED by the crash loop", task.Status, task.BlockedReason)
	}
	assertWorktreeAndBranchKept(t, projectRoot, task)
	assertDoerWorkOnDisk(t, filepath.Join(projectRoot, paths.WorktreesDirName, taskID))
}

// The provider's report for a background job killed at session end.
const stoppedBackgroundJobEvent = `{"type":"system","subtype":"task_notification","task_id":"bjob1","status":"stopped","summary":"Run long suite"}`

// The abandoned-job loop block also runs after release; same guarantees.
func TestRunSupervisorAbandonedBackgroundLoopBlockPreservesDirtyWork(t *testing.T) {
	mock := &MockLLMAgent{Output: stoppedBackgroundJobEvent + "\n"}
	mock.OnExecute = func(_ context.Context, _ string, _ string, _ string, projectRoot string, _ []string) error {
		leaveDoerWork(t, filepath.Join(projectRoot, paths.WorktreesDirName, "task-loop"))
		return nil
	}
	projectRoot, statePath, taskID := runSupervisorOnClaimedTask(t, func(cfg *models.Config) {
		cfg.SpinningRestartThreshold = 1
	}, mock)

	task := readLoopTask(t, statePath, taskID)
	if task.Status != models.TaskStatusBlocked || task.BlockedReason == nil || !strings.Contains(*task.BlockedReason, "abandoned background job loop") {
		t.Fatalf("task = %s reason %v, want BLOCKED by the abandoned-job loop", task.Status, task.BlockedReason)
	}
	assertWorktreeAndBranchKept(t, projectRoot, task)
	assertDoerWorkOnDisk(t, filepath.Join(projectRoot, paths.WorktreesDirName, taskID))
}

// setBlockInterleaving runs move just before the block's transaction.
func setBlockInterleaving(t *testing.T, move func()) {
	t.Helper()
	supervisorBlockBeforeModifyTestHook = move
	t.Cleanup(func() { supervisorBlockBeforeModifyTestHook = nil })
}

// moveTask applies a concurrent actor's write to the task.
func (f *watchdogFixture) moveTask(t *testing.T, edit func(*models.State, *models.Task)) {
	t.Helper()
	if err := f.bb.Modify(func(state *models.State) error {
		edit(state, state.FindTask(f.taskID))
		return nil
	}); err != nil {
		t.Fatalf("concurrent write: %v", err)
	}
}

func reassignToSecondCoder(state *models.State, task *models.Task) {
	other := "coder-2"
	agent := testhelpers.RegisteredTestAgent(models.RoleCoder)
	agent.Status, agent.CurrentTask = models.AgentStatusWorking, &task.ID
	state.Agents[other] = agent
	state.ReleaseAgent("coder-1")
	task.Status, task.AssignedTo = models.TaskStatusImplementing, &other
	task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventClaimed, Agent: &other})
}

func submitForReview(_ *models.State, task *models.Task) {
	reviewCommit := "review-commit"
	task.Status, task.ReviewCommit = models.TaskStatusReadyForReview, &reviewCommit
	task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventSubmittedForReview, Agent: task.AssignedTo})
}

func replaceRegistration(state *models.State, _ *models.Task) {
	agent := state.Agents["coder-1"]
	agent.Generation = "replacement-generation"
	state.Agents["coder-1"] = agent
}

// An owned block saves work outside the state lock; a task that changed owner
// or lifecycle meanwhile must be left to whoever moved it (R1).
func TestSupervisorBlockLeavesALifecycleMovedDuringTheSave(t *testing.T) {
	for _, tc := range []struct {
		name          string
		move          func(*models.State, *models.Task)
		wantStatus    models.TaskStatus
		wantAssignee  string
		wantAuthError bool
	}{
		{name: "reassigned", move: reassignToSecondCoder, wantStatus: models.TaskStatusImplementing, wantAssignee: "coder-2"},
		{name: "submitted", move: submitForReview, wantStatus: models.TaskStatusReadyForReview, wantAssignee: "coder-1"},
		{name: "registration replaced", move: replaceRegistration, wantStatus: models.TaskStatusImplementing, wantAssignee: "coder-1", wantAuthError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newWatchdogFixture(t)
			leaveDoerWork(t, f.worktreeDir)
			authority := testSupervisorAuthority(t, f.bb, f.agentID)
			setBlockInterleaving(t, func() { f.moveTask(t, tc.move) })

			err := blockTaskFromSupervisor(f.bb, f.projectRoot, f.taskID, authority, "spinning detected")
			if tc.wantAuthError != ops.IsAgentAuthorityError(err) || (!tc.wantAuthError && err != nil) {
				t.Fatalf("blockTaskFromSupervisor error = %v, want authority error %v", err, tc.wantAuthError)
			}
			task := f.task(t)
			if task.Status != tc.wantStatus || task.AssignedTo == nil || *task.AssignedTo != tc.wantAssignee || task.BlockedReason != nil {
				t.Fatalf("task = %s assigned %v blocked %v, want the moved lifecycle %s/%s untouched", task.Status, task.AssignedTo, task.BlockedReason, tc.wantStatus, tc.wantAssignee)
			}
			// The save ran while coder-1 still owned the task.
			assertDoerWorkCommitted(t, f.worktreeDir, f.baseCommit)
			assertWorktreeAndBranchKept(t, f.projectRoot, task)
		})
	}
}

// A released-continuation block acts only on the exact continuation the exit
// released (R4).
func TestReleasedSupervisorBlockActsOnlyOnTheReleasedContinuation(t *testing.T) {
	release := func(t *testing.T, f *watchdogFixture) (models.AgentAuthority, ops.ReleasedTaskClaim) {
		t.Helper()
		authority := testSupervisorAuthority(t, f.bb, f.agentID)
		released, err := releaseAgentAfterExit(f.bb, authority, f.projectRoot)
		if err != nil {
			t.Fatalf("releaseAgentAfterExit: %v", err)
		}
		if released.TaskID != f.taskID || released.TransitionID == "" {
			t.Fatalf("released = %+v, want task-1's released claim", released)
		}
		return authority, released
	}

	t.Run("released continuation is blocked", func(t *testing.T) {
		f := newWatchdogFixture(t)
		leaveDoerWork(t, f.worktreeDir)
		authority, released := release(t, f)
		if err := blockReleasedTaskFromSupervisor(f.bb, f.projectRoot, f.taskID, authority, released, "crash restart loop detected"); err != nil {
			t.Fatalf("blockReleasedTaskFromSupervisor: %v", err)
		}
		task := f.task(t)
		if task.Status != models.TaskStatusBlocked {
			t.Fatalf("task status = %s, want BLOCKED", task.Status)
		}
		assertWorktreeAndBranchKept(t, f.projectRoot, task)
		assertDoerWorkOnDisk(t, f.worktreeDir)
	})

	for _, tc := range []struct {
		name string
		move func(*models.State, *models.Task)
	}{
		{name: "claimed by another coder", move: reassignToSecondCoder},
		{name: "touched by another lifecycle write", move: func(_ *models.State, task *models.Task) {
			task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventClaimReleased})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newWatchdogFixture(t)
			authority, released := release(t, f)
			before := f.task(t)
			setBlockInterleaving(t, func() { f.moveTask(t, tc.move) })
			if err := blockReleasedTaskFromSupervisor(f.bb, f.projectRoot, f.taskID, authority, released, "crash restart loop detected"); err != nil {
				t.Fatalf("blockReleasedTaskFromSupervisor: %v", err)
			}
			if task := f.task(t); task.Status == models.TaskStatusBlocked || task.BlockedReason != nil {
				t.Fatalf("task = %s blocked %v, want the moved lifecycle untouched (was %s)", task.Status, task.BlockedReason, before.Status)
			}
		})
	}

	t.Run("registration replaced", func(t *testing.T) {
		f := newWatchdogFixture(t)
		authority, released := release(t, f)
		setBlockInterleaving(t, func() { f.moveTask(t, replaceRegistration) })
		if err := blockReleasedTaskFromSupervisor(f.bb, f.projectRoot, f.taskID, authority, released, "crash restart loop detected"); !ops.IsAgentAuthorityError(err) {
			t.Fatalf("blockReleasedTaskFromSupervisor error = %v, want an authority refusal", err)
		}
		if task := f.task(t); task.Status == models.TaskStatusBlocked {
			t.Fatal("stale registration blocked the task")
		}
	})

	for _, tc := range []struct {
		name     string
		released ops.ReleasedTaskClaim
	}{
		{name: "nothing released", released: ops.ReleasedTaskClaim{}},
		{name: "another task released", released: ops.ReleasedTaskClaim{TaskID: "task-other", TransitionID: "x"}},
		{name: "token missing", released: ops.ReleasedTaskClaim{TaskID: "task-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newWatchdogFixture(t)
			authority, _ := release(t, f)
			if err := blockReleasedTaskFromSupervisor(f.bb, f.projectRoot, f.taskID, authority, tc.released, "crash restart loop detected"); err != nil {
				t.Fatalf("blockReleasedTaskFromSupervisor: %v", err)
			}
			if task := f.task(t); task.Status == models.TaskStatusBlocked {
				t.Fatal("blocked an unowned task without its released token")
			}
		})
	}
}
