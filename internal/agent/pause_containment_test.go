package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	lizagit "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D86: pause must contain work. The supervisor gate must not open on an
// unreadable state, provider starts must be ordered against pause, and the
// reviewer merge loop must not merge while paused.

func TestWaitWhilePaused_UnreadableStateKeepsWaiting(t *testing.T) {
	// GIVEN a PAUSED system whose state file then becomes unreadable
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModePaused
	testhelpers.WriteInitialState(t, statePath, state)
	if err := os.WriteFile(statePath, []byte("config: [unterminated\n"), 0o644); err != nil {
		t.Fatalf("corrupt state: %v", err)
	}

	// WHEN a supervisor waits at the pause gate
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := waitWhilePaused(ctx, projectRoot, "doer")

	// THEN the gate stays closed until the context ends
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waitWhilePaused() = %v, want context deadline: an unreadable state must not open the gate", err)
	}
}

func TestProviderLaunchGate_RefusesStartWhilePaused(t *testing.T) {
	// GIVEN a registered agent and a PAUSED system
	fixture := newProviderGenerationFixture(t)
	if _, err := ops.Pause(fixture.projectRoot, "contain", "human"); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	// WHEN the supervisor launches a provider
	started := false
	err := newProviderLaunchGate(fixture.config(fixture.authorityA))(context.Background(), func() error {
		started = true
		return nil
	})

	// THEN no provider starts
	if started {
		t.Fatal("provider started while PAUSED")
	}
	if err == nil {
		t.Fatal("launch gate returned nil while PAUSED, want a refusal")
	}
}

func TestProviderLaunchGate_PauseWaitsForAdmittedStart(t *testing.T) {
	// GIVEN a provider start admitted while RUNNING and still in progress
	fixture := newProviderGenerationFixture(t)
	gate := newProviderLaunchGate(fixture.config(fixture.authorityA))
	var seq atomic.Int64
	var startReturned, pauseReturned int64
	entered, release := make(chan struct{}), make(chan struct{})
	launchDone := make(chan error, 1)
	go func() {
		launchDone <- gate(context.Background(), func() error {
			close(entered)
			<-release
			startReturned = seq.Add(1)
			return nil
		})
	}()
	waitForTestSignal(t, entered, "provider start entered")

	// WHEN the operator pauses during that start
	pauseDone := make(chan error, 1)
	go func() {
		_, err := ops.Pause(fixture.projectRoot, "contain", "human")
		pauseReturned = seq.Add(1)
		pauseDone <- err
	}()
	waitForPausedCommit(t, fixture.statePath)

	// THEN pause does not return while the admitted start is still running
	select {
	case err := <-pauseDone:
		close(release)
		t.Fatalf("Pause returned (err=%v) while an admitted provider start was still in progress", err)
	case <-time.After(500 * time.Millisecond):
	}
	close(release)
	if err := waitForTestResult(t, launchDone, "launch"); err != nil {
		t.Fatalf("admitted launch failed: %v", err)
	}
	if err := waitForTestResult(t, pauseDone, "pause"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if startReturned == 0 || pauseReturned < startReturned {
		t.Fatalf("pause returned at %d before admitted start returned at %d", pauseReturned, startReturned)
	}

	// AND a launch after pause returned does not start a provider
	started := false
	_ = gate(context.Background(), func() error { started = true; return nil })
	if started {
		t.Fatal("provider started after Pause returned")
	}
}

func TestHandleApprovedMerges_DoesNotMergeWhilePaused(t *testing.T) {
	// GIVEN an approved task and a PAUSED system
	projectRoot, stateFile, taskID := setupAgentMergeRepo(t)
	if _, err := ops.Pause(projectRoot, "contain", "human"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	bb := db.New(stateFile)
	pr, err := ops.LoadResolverForModels(projectRoot)
	if err != nil {
		t.Fatalf("LoadResolverForModels: %v", err)
	}

	// WHEN the reviewer merge loop runs
	_ = handleApprovedMerges(projectRoot, "code-reviewer-2", bb, pr)

	// THEN nothing is merged
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if task := state.FindTask(taskID); task == nil || task.Status != models.TaskStatusApproved {
		t.Fatalf("task after paused merge loop = %+v, want still APPROVED", task)
	}
}

func waitForPausedCommit(t *testing.T, statePath string) {
	t.Helper()
	waitForState(t, db.For(statePath), "PAUSED commit", func(state *models.State) bool {
		return state.Config.Mode == models.SystemModePaused
	})
}

// waitForState returns the first state snapshot satisfying ready, re-reading on
// each state-file change. The watch starts before the first read, so no write
// between them is missed.
func waitForState(t *testing.T, bb *db.Blackboard, what string, ready func(*models.State) bool) *models.State {
	t.Helper()
	watcher, err := bb.WatchForChanges()
	if err != nil {
		t.Fatalf("watch state: %v", err)
	}
	defer func() { _ = watcher.Close() }()
	deadline := time.After(15 * time.Second)
	for {
		if state, err := bb.ReadSnapshot(); err == nil && ready(state) {
			return state
		}
		select {
		case <-watcher.Events():
		case err := <-watcher.Errors():
			t.Fatalf("%s: watch state: %v", what, err)
		case <-deadline:
			t.Fatalf("%s: not observed within 15s", what)
		}
	}
}

func waitForTestSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: not observed within 5s", what)
	}
}

func waitForTestResult(t *testing.T, ch <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: did not finish within 5s", what)
		return nil
	}
}

func TestProviderLaunchGate_FailedStartReleasesAdmission(t *testing.T) {
	// GIVEN a provider start admitted while RUNNING that fails to spawn
	fixture := newProviderGenerationFixture(t)
	spawnErr := errors.New("spawn failed")

	// WHEN the launch gate runs it
	err := newProviderLaunchGate(fixture.config(fixture.authorityA))(context.Background(), func() error { return spawnErr })

	// THEN the failure surfaces and the admission lock is released, so a pause
	// completes its barrier well within a short timeout
	if !errors.Is(err, spawnErr) {
		t.Fatalf("launch gate = %v, want the spawn failure", err)
	}
	t.Cleanup(ops.SetWorkAdmissionBarrierTimeoutForTest(500 * time.Millisecond))
	if _, err := ops.Pause(fixture.projectRoot, "contain", "human"); err != nil {
		t.Fatalf("Pause after a failed start: %v; the admission lock was not released", err)
	}
}

func TestHandleApprovedMerges_PauseMidBatchStopsLaterMerges(t *testing.T) {
	// GIVEN two approved tasks, and a pause committed once the first merge is prepared
	projectRoot, stateFile, firstID := setupAgentMergeRepo(t)
	const secondID = "merge-second"
	second := approvedAgentMergeTask(t, projectRoot, secondID)
	bb := db.New(stateFile)
	if err := bb.Modify(func(state *models.State) error {
		state.Tasks = append(state.Tasks, second)
		return nil
	}); err != nil {
		t.Fatalf("add second task: %v", err)
	}
	var prepared []string
	t.Cleanup(ops.SetMergePreparedTestHookForTest(func(taskID string) {
		prepared = append(prepared, taskID)
		if len(prepared) == 1 {
			if _, err := ops.Pause(projectRoot, "contain", "human"); err != nil {
				t.Errorf("Pause: %v", err)
			}
		}
	}))
	pr, err := ops.LoadResolverForModels(projectRoot)
	if err != nil {
		t.Fatalf("LoadResolverForModels: %v", err)
	}

	// WHEN the reviewer merge loop runs
	err = handleApprovedMerges(projectRoot, "code-reviewer-2", bb, pr)

	// THEN the prepared merge completes, the batch stops, and the later task stays APPROVED
	if !errors.Is(err, ops.ErrSystemHalted) {
		t.Fatalf("handleApprovedMerges() = %v, want a halt refusal stopping the batch", err)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if task := state.FindTask(firstID); task == nil || task.Status != models.TaskStatusMerged {
		t.Fatalf("first task = %+v, want MERGED: its preparation committed before the pause", task)
	}
	if task := state.FindTask(secondID); task == nil || task.Status != models.TaskStatusApproved {
		t.Fatalf("second task = %+v, want still APPROVED", task)
	}
	if len(prepared) != 1 {
		t.Fatalf("prepared merges = %v, want only the first", prepared)
	}
}

func TestStrategyClaimTask_HaltIsNotAClaimFailure(t *testing.T) {
	t.Run("doer", func(t *testing.T) {
		// GIVEN a claimable coding task and a PAUSED system
		project := setupAcceptanceRefusalProject(t, func(task *models.Task) {
			task.PlanRef = ""
			task.SpecRef = "specs/acceptance-goal.md#identity"
		})
		if _, err := ops.Pause(project.root, "contain", "human"); err != nil {
			t.Fatalf("Pause: %v", err)
		}

		// WHEN the doer strategy claims
		_, _, err := project.strategy.ClaimTask(project.config, project.bb)

		// THEN the halt is reported as such and nothing is marked failed
		if !errors.Is(err, ops.ErrSystemHalted) {
			t.Fatalf("ClaimTask() = %v, want ErrSystemHalted", err)
		}
		state, task := project.task(t)
		if task.AssignedTo != nil || task.Status == models.TaskStatusBlocked {
			t.Fatalf("task status=%s assigned=%v, want unclaimed and not escalated", task.Status, task.AssignedTo)
		}
		assertNotDegraded(t, state, project.config.AgentID)
	})
	t.Run("reviewer", func(t *testing.T) {
		// GIVEN a reviewable task and a PAUSED system
		project := setupReviewClaimProject(t)
		project.addReviewableTask(t, "task-review", false)
		config := project.supervisorConfig(t)
		if _, err := ops.Pause(project.root, "contain", "human"); err != nil {
			t.Fatalf("Pause: %v", err)
		}

		// WHEN the reviewer strategy claims
		_, _, err := newReviewerStrategyForTest(t).ClaimTask(config, project.bb)

		// THEN the halt is reported as such and nothing is marked failed
		if !errors.Is(err, ops.ErrSystemHalted) {
			t.Fatalf("ClaimTask() = %v, want ErrSystemHalted", err)
		}
		state, err := project.bb.Read()
		if err != nil {
			t.Fatal(err)
		}
		if task := state.FindTask("task-review"); task.ReviewingBy != nil || task.Status != models.TaskStatusReadyForReview {
			t.Fatalf("task status=%s reviewing=%v, want unclaimed", task.Status, task.ReviewingBy)
		}
		assertNotDegraded(t, state, config.AgentID)
	})
}

// pauseGatedProvider pauses the system just before each of its first launches,
// then reports the gate refusal as ACPX does: a masked exit 1. Unpaused, it
// starts and runs until cancelled.
type pauseGatedProvider struct {
	projectRoot string
	pauses      atomic.Int32
	starts      atomic.Int32
	refused     chan struct{}
	started     chan struct{}
}

func newPauseGatedProvider(projectRoot string, pauses int32) *pauseGatedProvider {
	p := &pauseGatedProvider{projectRoot: projectRoot, refused: make(chan struct{}, 4), started: make(chan struct{}, 4)}
	p.pauses.Store(pauses)
	return p
}

func (p *pauseGatedProvider) Run(ctx context.Context, req LLMAgentRunRequest) (LLMAgentRunResult, error) {
	if p.pauses.Add(-1) >= 0 {
		if _, err := ops.Pause(p.projectRoot, "contain", "human"); err != nil {
			return LLMAgentRunResult{}, err
		}
	}
	if err := req.LaunchGate.launch(ctx, func() error { p.starts.Add(1); return nil }); err != nil {
		p.refused <- struct{}{}
		return LLMAgentRunResult{ExitCode: 1}, errors.New("provider exited with status 1")
	}
	p.started <- struct{}{}
	<-ctx.Done()
	return LLMAgentRunResult{}, ctx.Err()
}

func (p *pauseGatedProvider) RunInteractive(context.Context, LLMAgentInteractiveRequest) (int, error) {
	return 0, errors.New("interactive mode is not used")
}

func TestRunSupervisor_HaltedDoerLaunchResumesSameIteration(t *testing.T) {
	// GIVEN a doer supervisor whose first two launches are refused by a pause
	root, bb := setupPauseSupervisorProject(t, testhelpers.BuildTaskByStatus("pause-task", models.TaskStatusReady, time.Now().UTC()))
	provider := newPauseGatedProvider(root, 2)
	stop := startPauseSupervisor(t, root, "coder-1", "coder", provider)
	var claimedAttempt int

	for cycle := 1; cycle <= 2; cycle++ {
		// WHEN the launch is refused
		waitForTestSignal(t, provider.refused, "halted launch")

		// THEN the claim is released as a continuation of iteration 1, its worktree kept
		task := waitForPauseTask(t, bb, "pause-task", func(task *models.Task) bool { return task.AssignedTo == nil })
		if !task.Continuation || task.Iteration != 1 {
			t.Fatalf("cycle %d: continuation=%v iteration=%d, want a continuation of iteration 1", cycle, task.Continuation, task.Iteration)
		}
		if cycle == 1 {
			claimedAttempt = task.EffectiveAttempt()
		} else if got := task.EffectiveAttempt(); got != claimedAttempt {
			t.Fatalf("cycle %d: attempt %d, want %d", cycle, got, claimedAttempt)
		}
		if task.Worktree == nil {
			t.Fatalf("cycle %d: worktree dropped on halt release", cycle)
		}
		if _, err := os.Stat(filepath.Join(root, *task.Worktree)); err != nil {
			t.Fatalf("cycle %d: worktree missing: %v", cycle, err)
		}
		testhelpers.MustGit(t, root, "rev-parse", "--verify", "refs/heads/task/pause-task")
		if n := provider.starts.Load(); n != 0 {
			t.Fatalf("cycle %d: %d provider starts while PAUSED", cycle, n)
		}
		if _, err := ops.Resume(root, "human"); err != nil {
			t.Fatalf("Resume: %v", err)
		}
	}

	// AND after resume the same iteration starts once
	waitForTestSignal(t, provider.started, "resumed provider start")
	task := waitForPauseTask(t, bb, "pause-task", func(task *models.Task) bool { return task.AssignedTo != nil })
	if task.Iteration != 1 || task.Continuation {
		t.Fatalf("resumed claim iteration=%d continuation=%v, want iteration 1 with the marker consumed", task.Iteration, task.Continuation)
	}
	last := task.History[len(task.History)-1]
	if last.Event != models.TaskEventClaimed || last.Extra["continuation"] != true {
		t.Fatalf("last history = %+v, want a continuation claim", last)
	}
	if n := provider.starts.Load(); n != 1 {
		t.Fatalf("provider starts = %d, want 1", n)
	}

	// AND once that turn ends, an ordinary claim starts the next iteration
	stop()
	if task := readPauseTask(t, bb, "pause-task"); task.AssignedTo != nil {
		if _, err := ops.ReleaseClaim(root, task.ID, "doer", true, "turn ended", "human"); err != nil {
			t.Fatalf("ReleaseClaim: %v", err)
		}
	}
	if err := bb.Modify(func(state *models.State) error {
		state.Agents["coder-2"] = testhelpers.RegisteredTestAgent(models.RoleCoder)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.ClaimTask(root, "pause-task", "coder-2"); err != nil {
		t.Fatalf("ordinary reclaim: %v", err)
	}
	if task := readPauseTask(t, bb, "pause-task"); task.Iteration != 2 {
		t.Fatalf("ordinary reclaim iteration = %d, want 2", task.Iteration)
	}
}

func TestRunSupervisor_HaltedReviewerLaunchReleasesReview(t *testing.T) {
	// GIVEN a reviewable task and a reviewer supervisor whose launch is refused by a pause
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	task := testhelpers.BuildTaskByStatus("pause-review", models.TaskStatusReadyForReview, time.Now().UTC())
	git := lizagit.New(root)
	if _, err := git.CreateWorktree(task.ID, "integration"); err != nil {
		t.Fatal(err)
	}
	sha, err := git.GetCommitSHA("integration")
	if err != nil {
		t.Fatal(err)
	}
	task.BaseCommit, task.ReviewCommit, task.AssignedTo = &sha, &sha, nil
	root, bb := setupPauseSupervisorProjectAt(t, root, task)
	before := readPauseTask(t, bb, task.ID)
	provider := newPauseGatedProvider(root, 1)
	stop := startPauseSupervisor(t, root, "code-reviewer-1", "code-reviewer", provider)

	// WHEN the launch is refused
	waitForTestSignal(t, provider.refused, "halted review launch")

	// THEN the review claim is released with no review counted
	released := waitForPauseTask(t, bb, task.ID, func(task *models.Task) bool { return task.ReviewingBy == nil })
	if released.Status != models.TaskStatusReadyForReview {
		t.Fatalf("status = %s, want READY_FOR_REVIEW", released.Status)
	}
	if released.Iteration != before.Iteration || released.ReviewCyclesCurrent != before.ReviewCyclesCurrent || released.ReviewCyclesTotal != before.ReviewCyclesTotal {
		t.Fatalf("counters changed: iteration %d->%d, review cycles %d/%d->%d/%d", before.Iteration, released.Iteration,
			before.ReviewCyclesCurrent, before.ReviewCyclesTotal, released.ReviewCyclesCurrent, released.ReviewCyclesTotal)
	}
	if n := provider.starts.Load(); n != 0 {
		t.Fatalf("%d provider starts while PAUSED", n)
	}

	// AND after resume another reviewer claims it and starts its turn
	stop()
	if _, err := ops.Resume(root, "human"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	next := newPauseGatedProvider(root, 0)
	startPauseSupervisor(t, root, "code-reviewer-2", "code-reviewer", next)
	waitForTestSignal(t, next.started, "resumed review start")
	if claimed := readPauseTask(t, bb, task.ID); claimed.ReviewingBy == nil || *claimed.ReviewingBy != "code-reviewer-2" {
		t.Fatalf("reviewing_by = %v, want code-reviewer-2", claimed.ReviewingBy)
	}
}

func setupPauseSupervisorProject(t *testing.T, task models.Task) (string, *db.Blackboard) {
	t.Helper()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	return setupPauseSupervisorProjectAt(t, root, task)
}

func setupPauseSupervisorProjectAt(t *testing.T, root string, task models.Task) (string, *db.Blackboard) {
	t.Helper()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.SetupPipelineConfig(t, root)
	state := testhelpers.CreateValidState()
	state.Config.CoderPollInterval = 1
	state.Config.ReviewerPollInterval = 1
	state.Tasks = []models.Task{task}
	return root, testhelpers.WriteInitialState(t, statePath, state)
}

// startPauseSupervisor runs a supervisor and returns an idempotent stop that
// cancels it and waits for it to exit.
func startPauseSupervisor(t *testing.T, root, agentID, role string, provider LLMAgent) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunSupervisor(ctx, SupervisorConfig{
			AgentID: agentID, Role: role,
			ProjectRoot: root, StatePath: paths.New(root).StatePath(),
			LogPath: paths.New(root).LogPath(), SpecsDir: filepath.Join(root, "specs"),
			CLIName: "codex", LLMAgent: provider, ExecutionTimeout: 30 * time.Second,
		})
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Error("supervisor did not exit after cancellation")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func readPauseTask(t *testing.T, bb *db.Blackboard, taskID string) models.Task {
	t.Helper()
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatalf("task %s missing", taskID)
	}
	return *task
}

func waitForPauseTask(t *testing.T, bb *db.Blackboard, taskID string, ready func(*models.Task) bool) models.Task {
	t.Helper()
	state := waitForState(t, bb, "task "+taskID, func(state *models.State) bool {
		task := state.FindTask(taskID)
		return task != nil && ready(task)
	})
	return *state.FindTask(taskID)
}

func assertNotDegraded(t *testing.T, state *models.State, agentID string) {
	t.Helper()
	if health, ok := state.AgentHealth[agentID]; ok && health.State == models.AgentHealthDegraded {
		t.Fatalf("agent %s marked degraded by a halt refusal: %+v", agentID, health)
	}
}
