package ops

import (
	"context"
	stderrors "errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D86: in a halt mode (PAUSED, CIRCUIT_BREAKER_TRIPPED) no agent may claim new
// work or start a new merge. These tests pin the refusal at the operation
// boundary, independently of the supervisor's pause gate.

var haltModes = []models.SystemMode{models.SystemModePaused, models.SystemModeCircuitBreakerTripped}

func assertHaltRefusal(t *testing.T, err error, mode models.SystemMode) {
	t.Helper()
	if err == nil {
		t.Fatalf("operation succeeded in %s mode, want a halt refusal", mode)
	}
	var pe *PreconditionError
	if !stderrors.As(err, &pe) {
		t.Fatalf("error = %T (%v), want a *PreconditionError refusal", err, err)
	}
	if !strings.Contains(err.Error(), string(mode)) {
		t.Fatalf("error = %q, want it to name mode %s", err.Error(), mode)
	}
}

func readTaskForHaltTest(t *testing.T, statePath, taskID string) models.Task {
	t.Helper()
	state, err := db.For(statePath).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatalf("task %s missing", taskID)
	}
	return *task
}

func TestClaimTask_RefusedInHaltModes(t *testing.T) {
	for _, mode := range haltModes {
		t.Run(string(mode), func(t *testing.T) {
			// GIVEN a claimable doer task while the system is halted
			projectRoot := t.TempDir()
			testhelpers.SetupTestGitRepo(t, projectRoot)
			statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
			state := testhelpers.CreateValidState()
			registerClaimTaskTestAgents(state)
			state.Config.Mode = mode
			state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC())}
			testhelpers.WriteInitialState(t, statePath, state)
			before := readTaskForHaltTest(t, statePath, "task-1")

			// WHEN a doer claims it
			_, err := ClaimTask(projectRoot, "task-1", "coder-1")

			// THEN the claim is refused and nothing changed
			assertHaltRefusal(t, err, mode)
			if after := readTaskForHaltTest(t, statePath, "task-1"); !reflect.DeepEqual(before, after) {
				t.Fatalf("refused claim changed the task:\nbefore=%#v\nafter=%#v", before, after)
			}
			if _, statErr := os.Stat(filepath.Join(projectRoot, ".worktrees", "task-1")); !os.IsNotExist(statErr) {
				t.Fatalf("refused claim created a worktree (stat err=%v)", statErr)
			}
		})
	}
}

func TestClaimTask_SucceedsAfterResume(t *testing.T) {
	// GIVEN a claim refused while PAUSED
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Config.Mode = models.SystemModePaused
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC())}
	testhelpers.WriteInitialState(t, statePath, state)
	_, err := ClaimTask(projectRoot, "task-1", "coder-1")
	assertHaltRefusal(t, err, models.SystemModePaused)

	// WHEN the operator resumes and the doer claims again
	if _, err := Resume(projectRoot, "human"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if _, err := ClaimTask(projectRoot, "task-1", "coder-1"); err != nil {
		t.Fatalf("claim after resume: %v", err)
	}

	// THEN the task is claimed normally
	task := readTaskForHaltTest(t, statePath, "task-1")
	if task.Status != models.TaskStatusImplementing || task.AssignedTo == nil || *task.AssignedTo != "coder-1" {
		t.Fatalf("task after resume claim = status %s assignee %v, want IMPLEMENTING by coder-1", task.Status, task.AssignedTo)
	}
}

func TestClaimReviewerTask_RefusedInHaltModes(t *testing.T) {
	for _, mode := range haltModes {
		t.Run(string(mode), func(t *testing.T) {
			// GIVEN a task awaiting review while the system is halted
			projectRoot := t.TempDir()
			testhelpers.SetupTestGitRepo(t, projectRoot)
			statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
			state := testhelpers.CreateValidState()
			registerClaimReviewerTaskTestAgents(state)
			state.Config.Mode = mode
			worktree, reviewCommit := createClaimReviewWorktree(t, projectRoot, "task-1")
			baseCommit := testhelpers.MustGit(t, projectRoot, "merge-base", reviewCommit, "integration")
			state.Tasks = []models.Task{{
				ID: "task-1", Status: models.TaskStatusReadyForReview, RolePair: "coding-pair", Priority: 1,
				Worktree: &worktree, BaseCommit: &baseCommit, ReviewCommit: &reviewCommit,
				History: []models.TaskHistoryEntry{}, Created: time.Now().UTC(),
			}}
			state.Agents["code-reviewer-1"] = testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)
			testhelpers.WriteInitialState(t, statePath, state)
			before := readTaskForHaltTest(t, statePath, "task-1")

			// WHEN a reviewer claims review work
			_, err := ClaimReviewerTask(ClaimReviewerTaskInput{ProjectRoot: projectRoot, AgentID: "code-reviewer-1", LeaseDuration: 1800})

			// THEN the claim is refused and nothing changed
			assertHaltRefusal(t, err, mode)
			if after := readTaskForHaltTest(t, statePath, "task-1"); !reflect.DeepEqual(before, after) {
				t.Fatalf("refused review claim changed the task:\nbefore=%#v\nafter=%#v", before, after)
			}
		})
	}
}

func TestMergeWorktree_RefusedWhilePausedThenMergesAfterResume(t *testing.T) {
	// GIVEN an approved task and a PAUSED system
	projectRoot, statePath := setupMergeTestRepo(t, "merge-paused", "coder-1")
	if _, err := Pause(projectRoot, "contain", "human"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	refBefore := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration")
	before := readTaskForHaltTest(t, statePath, "merge-paused")

	// WHEN a merge is attempted
	_, err := MergeWorktree(projectRoot, "merge-paused", "coder-1")

	// THEN it is refused with no ref change and no recorded preparation
	assertHaltRefusal(t, err, models.SystemModePaused)
	if refAfter := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration"); refAfter != refBefore {
		t.Fatalf("integration ref moved while PAUSED: %s -> %s", refBefore, refAfter)
	}
	after := readTaskForHaltTest(t, statePath, "merge-paused")
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("refused merge changed the task:\nbefore=%#v\nafter=%#v", before, after)
	}
	if after.Lifecycle != nil && after.Lifecycle.Preparation != nil {
		t.Fatalf("refused merge recorded a preparation: %#v", after.Lifecycle.Preparation)
	}

	// WHEN the operator resumes
	if _, err := Resume(projectRoot, "human"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if _, err := MergeWorktree(projectRoot, "merge-paused", "coder-1"); err != nil {
		t.Fatalf("merge after resume: %v", err)
	}

	// THEN the merge completes
	if task := readTaskForHaltTest(t, statePath, "merge-paused"); task.Status != models.TaskStatusMerged {
		t.Fatalf("status after resume merge = %s, want MERGED", task.Status)
	}
}

func TestAwaitVerdictAutoReclaim_ReclaimsWhileRunning(t *testing.T) {
	// Control for the PAUSED case below: the same fixture reclaims normally.
	projectRoot, statePath, bb, before := setupAwaitVerdictRejectedForHaltTest(t, models.SystemModeRunning)
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		t.Fatalf("loadResolver: %v", err)
	}

	result, err := handleVerdictResult(bb, &before, "coder-1", nil, projectRoot, resolver, before.RolePair)

	if err != nil {
		t.Fatalf("handleVerdictResult: %v", err)
	}
	after := readTaskForHaltTest(t, statePath, "task-1")
	if result.Verdict != VerdictRejected || after.Status != models.TaskStatusImplementing || after.Iteration != before.Iteration+1 {
		t.Fatalf("control reclaim: verdict %q reason %q, task %s iteration %d", result.Verdict, result.Reason, after.Status, after.Iteration)
	}
}

func setupAwaitVerdictRejectedForHaltTest(t *testing.T, mode models.SystemMode) (string, string, *db.Blackboard, models.Task) {
	t.Helper()
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.MaxCoderIterations = 10
	state.Config.MaxReviewCycles = 5
	state.Config.Mode = mode
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
	task.Iteration = 1
	task.Status = models.TaskStatusRejected
	reason := "Missing error handling"
	task.RejectionReason = &reason
	leaseExpires := now.Add(30 * time.Minute)
	task.LeaseExpires = &leaseExpires
	reviewer := "code-reviewer-1"
	task.History = append(task.History,
		models.TaskHistoryEntry{Time: now, Event: models.TaskEventSubmittedForReview, Agent: strPtr("coder-1")},
		models.TaskHistoryEntry{Time: now, Event: models.TaskEventRejected, Agent: &reviewer},
	)
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
	bb := testhelpers.WriteInitialState(t, statePath, state)
	return projectRoot, statePath, bb, readTaskForHaltTest(t, statePath, "task-1")
}

func TestAwaitVerdictAutoReclaim_RefusedWhilePaused(t *testing.T) {
	// GIVEN a rejected task whose doer awaits its verdict, and a PAUSED system
	projectRoot, statePath, bb, before := setupAwaitVerdictRejectedForHaltTest(t, models.SystemModePaused)
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		t.Fatalf("loadResolver: %v", err)
	}

	// WHEN await-verdict handles the rejection
	result, err := handleVerdictResult(bb, &before, "coder-1", nil, projectRoot, resolver, before.RolePair)

	// THEN it does not reclaim, and tells the doer to stop because of the pause
	if err != nil {
		t.Fatalf("handleVerdictResult: %v", err)
	}
	after := readTaskForHaltTest(t, statePath, "task-1")
	if after.Status != models.TaskStatusRejected || after.Iteration != before.Iteration {
		t.Fatalf("auto-reclaim ran while PAUSED: status %s iteration %d (was %s/%d)", after.Status, after.Iteration, before.Status, before.Iteration)
	}
	if result.SafeAction != SafeActionStop || !strings.Contains(result.Reason, string(models.SystemModePaused)) {
		t.Fatalf("result = verdict %q safe_action %q reason %q, want stop naming PAUSED", result.Verdict, result.SafeAction, result.Reason)
	}
}

func assertSystemHalted(t *testing.T, err error) {
	t.Helper()
	if !stderrors.Is(err, ErrSystemHalted) {
		t.Fatalf("error = %v, want ErrSystemHalted", err)
	}
}

func pauseInClaimHook(t *testing.T, projectRoot string) {
	t.Helper()
	previous := testClaimTaskHooks
	testClaimTaskHooks = &claimTaskTestHooks{afterInitialAdmission: func() {
		if _, err := Pause(projectRoot, "contain", "human"); err != nil {
			t.Errorf("Pause in claim hook: %v", err)
		}
	}}
	t.Cleanup(func() { testClaimTaskHooks = previous })
}

// A pause landing after the claim's first read must stop every write the claim
// would have triggered: stranded takeover, attempt rollover, blocked escalation.
func TestClaimTask_PauseAfterFirstReadRefusesClaimTriggeredWrites(t *testing.T) {
	t.Run("stranded takeover", func(t *testing.T) {
		fixture := newPreservedInitialClaimFixture(t)
		strandPreservedTask(t, fixture, time.Now().UTC().Add(-time.Minute))
		before := snapshotClaimOwnership(t, fixture.stateFile, fixture.taskID)
		pauseInClaimHook(t, fixture.projectRoot)

		_, err := ClaimTask(fixture.projectRoot, fixture.taskID, "coder-2")

		assertSystemHalted(t, err)
		assertClaimOwnershipUnchanged(t, fixture, before)
		testClaimTaskHooks = nil
		if _, err := Resume(fixture.projectRoot, "human"); err != nil {
			t.Fatalf("Resume: %v", err)
		}
		if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, "coder-2"); err != nil {
			t.Fatalf("claim after resume: %v", err)
		}
		assertStrandedTakeover(t, fixture, "coder-2")
	})

	limitFixture := func(t *testing.T, attempt int, gitRepo bool) (string, string) {
		t.Helper()
		projectRoot := t.TempDir()
		if gitRepo {
			testhelpers.SetupTestGitRepo(t, projectRoot)
		}
		statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		registerClaimTaskTestAgents(state)
		state.Config.MaxCoderIterations = 3
		task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
		task.Iteration = 3
		task.Attempt = attempt
		state.Tasks = []models.Task{task}
		taskRef := "task-1"
		agent := testhelpers.RegisteredTestAgent("coder")
		agent.Status = models.AgentStatusWorking
		agent.CurrentTask = &taskRef
		agent.Heartbeat = now
		state.Agents["coder-1"] = agent
		testhelpers.WriteInitialState(t, statePath, state)
		return projectRoot, statePath
	}
	for _, tc := range []struct {
		name       string
		attempt    int
		gitRepo    bool
		afterClaim string
	}{
		{"attempt rollover", 1, true, "transitioned to attempt"},
		{"blocked escalation", 2, false, "transitioned to BLOCKED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectRoot, statePath := limitFixture(t, tc.attempt, tc.gitRepo)
			before := readTaskForHaltTest(t, statePath, "task-1")
			pauseInClaimHook(t, projectRoot)

			_, err := ClaimTask(projectRoot, "task-1", "coder-1")

			assertSystemHalted(t, err)
			if after := readTaskForHaltTest(t, statePath, "task-1"); !reflect.DeepEqual(before, after) {
				t.Fatalf("halted claim changed the task:\nbefore=%#v\nafter=%#v", before, after)
			}
			testClaimTaskHooks = nil
			if _, err := Resume(projectRoot, "human"); err != nil {
				t.Fatalf("Resume: %v", err)
			}
			if _, err := ClaimTask(projectRoot, "task-1", "coder-1"); err == nil || !strings.Contains(err.Error(), tc.afterClaim) {
				t.Fatalf("claim after resume = %v, want the deferred escalation %q", err, tc.afterClaim)
			}
		})
	}
}

func holdWorkAdmissionShared(t *testing.T, projectRoot string) (release func()) {
	t.Helper()
	held, done := make(chan struct{}), make(chan struct{})
	releaseCh := make(chan struct{})
	go func() {
		defer close(done)
		if err := WithWorkAdmissionSharedLock(context.Background(), projectRoot, "test-start", func() error {
			close(held)
			<-releaseCh
			return nil
		}); err != nil {
			t.Errorf("hold work admission: %v", err)
		}
	}()
	<-held
	return func() { close(releaseCh); <-done }
}

func TestPause_BarrierTimeoutKeepsPausedAndRetrySucceeds(t *testing.T) {
	// GIVEN a provider start holding admission past the barrier timeout
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())
	t.Cleanup(SetWorkAdmissionBarrierTimeoutForTest(100 * time.Millisecond))
	release := holdWorkAdmissionShared(t, projectRoot)

	// WHEN the operator pauses
	_, err := Pause(projectRoot, "contain", "human")

	// THEN pause reports an incomplete barrier and the system stays PAUSED
	var barrierErr *WorkAdmissionBarrierError
	if !stderrors.As(err, &barrierErr) {
		release()
		t.Fatalf("Pause() = %v, want *WorkAdmissionBarrierError", err)
	}
	paused, readErr := db.For(statePath).Read()
	if readErr != nil || paused.Config.Mode != models.SystemModePaused || paused.Config.ModeChangedAt == nil {
		release()
		t.Fatalf("mode after barrier timeout = %+v (err %v), want PAUSED committed", paused.Config, readErr)
	}
	changedAt := *paused.Config.ModeChangedAt

	// WHEN the start finishes and the operator retries
	release()
	result, err := Pause(projectRoot, "retry", "human")

	// THEN the retry succeeds without rewriting the pause
	if err != nil || result.Previous != models.SystemModePaused {
		t.Fatalf("retry Pause() = %+v, %v; want success from PAUSED", result, err)
	}
	after, _ := db.For(statePath).Read()
	if !after.Config.ModeChangedAt.Equal(changedAt) {
		t.Fatalf("retry rewrote mode_changed_at: %v -> %v", changedAt, after.Config.ModeChangedAt)
	}
}

func TestAnalyze_TripWaitsForAdmissionBarrier(t *testing.T) {
	// GIVEN anomalies that trip the breaker and a provider start in progress
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	for range 3 {
		state.Anomalies = append(state.Anomalies, models.Anomaly{Type: "retry_loop", Timestamp: now, Task: "task-1", Reporter: "coder-1", Details: map[string]any{"error_pattern": "connection refused"}})
	}
	testhelpers.WriteInitialState(t, statePath, state)
	t.Cleanup(SetWorkAdmissionBarrierTimeoutForTest(100 * time.Millisecond))
	release := holdWorkAdmissionShared(t, projectRoot)

	// WHEN the breaker trips
	_, err := Analyze(projectRoot)

	// THEN the trip is committed but reported incomplete until the start finishes
	var barrierErr *WorkAdmissionBarrierError
	if !stderrors.As(err, &barrierErr) {
		release()
		t.Fatalf("Analyze() = %v, want *WorkAdmissionBarrierError", err)
	}
	if tripped, _ := db.For(statePath).Read(); tripped.Config.Mode != models.SystemModeCircuitBreakerTripped {
		release()
		t.Fatalf("mode = %s, want CIRCUIT_BREAKER_TRIPPED", tripped.Config.Mode)
	}
	release()
	if _, err := Analyze(projectRoot); err != nil {
		t.Fatalf("rerun Analyze() = %v, want the barrier to pass", err)
	}
}

func TestMergeWorktree_PreparedBeforePauseCompletes(t *testing.T) {
	// GIVEN a merge whose preparation committed before the pause
	projectRoot, statePath := setupMergeTestRepo(t, "merge-prepared", "coder-1")
	refBefore := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration")
	t.Cleanup(SetMergePreparedTestHookForTest(func(string) {
		if _, err := Pause(projectRoot, "contain", "human"); err != nil {
			t.Errorf("Pause: %v", err)
		}
	}))

	// WHEN the merge continues
	_, err := MergeWorktree(projectRoot, "merge-prepared", "coder-1")

	// THEN the in-flight merge completes and retires its preparation
	if err != nil {
		t.Fatalf("prepared merge after pause: %v", err)
	}
	task := readTaskForHaltTest(t, statePath, "merge-prepared")
	if task.Status != models.TaskStatusMerged || (task.Lifecycle != nil && task.Lifecycle.Preparation != nil) {
		t.Fatalf("task = %s, preparation %+v; want MERGED with no preparation", task.Status, task.Lifecycle)
	}
	if ref := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration"); ref == refBefore {
		t.Fatal("integration ref did not advance for the prepared merge")
	}
}

func TestMergeWorktree_InterruptedPreparationUntouchedWhilePaused(t *testing.T) {
	// GIVEN a merge interrupted right after its preparation committed
	projectRoot, statePath := setupMergeTestRepo(t, "merge-interrupted", "coder-1")
	restore := SetMergePreparedTestHookForTest(func(string) { runtime.Goexit() })
	interrupted := make(chan struct{})
	go func() {
		defer close(interrupted)
		_, _ = MergeWorktree(projectRoot, "merge-interrupted", "coder-1")
	}()
	<-interrupted
	restore()
	before := readTaskForHaltTest(t, statePath, "merge-interrupted")
	if before.Lifecycle == nil || before.Lifecycle.Preparation == nil {
		t.Fatalf("fixture: no interrupted preparation recorded: %+v", before.Lifecycle)
	}
	if _, err := Pause(projectRoot, "contain", "human"); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	// WHEN a merge is retried while PAUSED
	_, err := MergeWorktree(projectRoot, "merge-interrupted", "coder-1")

	// THEN it is refused and the interrupted preparation is left for resume
	assertSystemHalted(t, err)
	if after := readTaskForHaltTest(t, statePath, "merge-interrupted"); !reflect.DeepEqual(before, after) {
		t.Fatalf("refused merge touched the interrupted preparation:\nbefore=%#v\nafter=%#v", before, after)
	}
	if _, err := Resume(projectRoot, "human"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if _, err := MergeWorktree(projectRoot, "merge-interrupted", "coder-1"); err != nil {
		t.Fatalf("merge after resume: %v", err)
	}
	if task := readTaskForHaltTest(t, statePath, "merge-interrupted"); task.Status != models.TaskStatusMerged {
		t.Fatalf("status after resume = %s, want MERGED", task.Status)
	}
}

func TestResumeInterruptedMerge_PauseAfterInitialReadLeavesPreparation(t *testing.T) {
	// GIVEN an interrupted merge preparation, and a retry that read RUNNING
	projectRoot, statePath := setupMergeTestRepo(t, "merge-race", "coder-1")
	restore := SetMergePreparedTestHookForTest(func(string) { runtime.Goexit() })
	interrupted := make(chan struct{})
	go func() {
		defer close(interrupted)
		_, _ = MergeWorktree(projectRoot, "merge-race", "coder-1")
	}()
	<-interrupted
	restore()
	bb := db.For(statePath)
	state, task, err := readTaskState(bb, "merge-race")
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewLifecycleRequest(integrationOperationWTMerge, task, "coder-1", nil, LifecycleRequestOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CheckLifecycleRequest(task, request, state.Agents); err == nil {
		t.Fatal("fixture: the retry must be refused into interrupted recovery")
	}
	before := readTaskForHaltTest(t, statePath, "merge-race")

	// WHEN pause commits before the retry's recovery retires the preparation
	if _, err := Pause(projectRoot, "contain", "human"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	resumed, err := resumeInterruptedMerge(bb, projectRoot, state, task, request, nil)

	// THEN recovery is refused and the preparation is left for resume
	assertSystemHalted(t, err)
	if resumed {
		t.Fatal("recovery reported resumed while PAUSED")
	}
	if after := readTaskForHaltTest(t, statePath, "merge-race"); !reflect.DeepEqual(before, after) {
		t.Fatalf("halted recovery changed the task:\nbefore=%#v\nafter=%#v", before, after)
	}
	if _, err := Resume(projectRoot, "human"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if _, err := MergeWorktree(projectRoot, "merge-race", "coder-1"); err != nil {
		t.Fatalf("merge after resume: %v", err)
	}
	if task := readTaskForHaltTest(t, statePath, "merge-race"); task.Status != models.TaskStatusMerged {
		t.Fatalf("status after resume = %s, want MERGED", task.Status)
	}
}
