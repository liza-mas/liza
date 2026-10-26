package agent

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Characterization of the claim, restart and wait concerns (A9 PB-3 to PB-6).
// Written green on the unchanged tree before claimDoerTaskWithOptionalAuthority,
// handleApprovedMergesWithOptionalAuthority, exit42RestartTracker.Handle and
// waitForWorkEventDriven were reduced: they pin the paths the existing tests do
// not reach, so each reduction keeps its value, order and control effect.

// PB-3: a candidate whose approvals are below the effective quorum is skipped
// with the task left APPROVED, and the batch goes on to the next candidate.
func TestA9ClaimPB3MergeBatchSkipsBelowQuorumAndContinues(t *testing.T) {
	projectRoot, statePath, mergeID := setupAgentMergeRepo(t)
	const lowQuorumID = "merge-below-quorum"
	low := approvedAgentMergeTask(t, projectRoot, lowQuorumID)
	low.Approvals = low.Approvals[:1]

	bb := db.New(statePath)
	off := false
	if err := bb.Modify(func(s *models.State) error {
		s.Config.AutoCheckpointSummary = &off
		s.Tasks = append([]models.Task{low}, s.Tasks...)
		return nil
	}); err != nil {
		t.Fatalf("prepare tasks: %v", err)
	}
	pr, err := ops.LoadResolverForModels(projectRoot)
	if err != nil {
		t.Fatalf("load resolver: %v", err)
	}

	if err := handleApprovedMerges(projectRoot, "code-reviewer-2", bb, pr); err != nil {
		t.Fatalf("handleApprovedMerges: %v", err)
	}

	after, err := bb.Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if got := after.FindTask(lowQuorumID); got == nil || got.Status != models.TaskStatusApproved {
		t.Fatalf("below-quorum task = %+v, want left APPROVED", got)
	}
	if got := after.FindTask(mergeID); got == nil || got.Status != models.TaskStatusMerged {
		t.Fatalf("later candidate = %+v, want MERGED after the skipped one", got)
	}
}

// PB-3: a reviewer that does not own the pending merge merges nothing.
func TestA9ClaimPB3MergeBatchIgnoresMergesOwnedByAnotherReviewer(t *testing.T) {
	projectRoot, statePath, taskID := setupAgentMergeRepo(t)
	bb := db.New(statePath)
	pr, err := ops.LoadResolverForModels(projectRoot)
	if err != nil {
		t.Fatalf("load resolver: %v", err)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	owner := models.ApprovedMergeOwner(state, state.FindTask(taskID), pr, time.Now().UTC())
	other := "code-reviewer-1"
	if owner == other {
		other = "code-reviewer-2"
	}

	if err := handleApprovedMerges(projectRoot, other, bb, pr); err != nil {
		t.Fatalf("handleApprovedMerges: %v", err)
	}

	after, err := bb.Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if got := after.FindTask(taskID); got == nil || got.Status != models.TaskStatusApproved {
		t.Fatalf("task = %+v, want left APPROVED for its owner %q", got, owner)
	}
}

func a9ExitRestartFixture(t *testing.T, mutate func(*models.State)) (*db.Blackboard, string, models.Task) {
	t.Helper()
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	agentID := "coder-1"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC())
	task.AssignedTo = &agentID
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = models.Agent{Role: "coder", Status: models.AgentStatusWorking}
	if mutate != nil {
		mutate(state)
	}
	return testhelpers.WriteInitialState(t, statePath, state), tmpDir, task
}

// PB-4: progress on the task resets the in-memory count and the backoff, the
// count is persisted on the task, and a task-less restart is tracked per agent.
func TestA9RestartPB4ProgressResetsCountAndTaskLessKeyIsPerAgent(t *testing.T) {
	bb, tmpDir, task := a9ExitRestartFixture(t, func(s *models.State) {
		s.Config.Exit42RestartThreshold = 99
		s.Config.Exit42MaxBackoffSeconds = 64
	})
	authority := testSupervisorAuthority(t, bb, "coder-1")
	tracker := newExit42RestartTracker()

	handle := func(taskID string) exit42RestartOutcome {
		t.Helper()
		outcome, err := tracker.Handle(bb, tmpDir, "coder", taskID, authority)
		if err != nil {
			t.Fatalf("Handle(%q): %v", taskID, err)
		}
		return outcome
	}

	if got := handle(task.ID); got.RestartCount != 1 || got.Delay != 2*time.Second {
		t.Fatalf("first restart = %+v, want count 1 delay 2s", got)
	}
	if got := handle(task.ID); got.RestartCount != 2 || got.Delay != 4*time.Second {
		t.Fatalf("second restart = %+v, want count 2 delay 4s", got)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if got := state.FindTask(task.ID).Exit42RestartCount; got != 2 {
		t.Fatalf("persisted restart count = %d, want 2", got)
	}

	if err := bb.Modify(func(s *models.State) error {
		s.FindTask(task.ID).Description = "progressed between restarts"
		return nil
	}); err != nil {
		t.Fatalf("record progress: %v", err)
	}
	if got := handle(task.ID); got.RestartCount != 1 || got.Delay != 2*time.Second {
		t.Fatalf("restart after progress = %+v, want the count and backoff reset", got)
	}

	if got := handle(""); got.RestartCount != 1 {
		t.Fatalf("task-less restart = %+v, want its own per-agent count 1", got)
	}
	if got := handle(""); got.RestartCount != 2 {
		t.Fatalf("second task-less restart = %+v, want count 2", got)
	}
	if got := handle(task.ID); got.RestartCount != 2 {
		t.Fatalf("task restart after task-less ones = %+v, want the task's own count 2", got)
	}
}

// PB-5: over the threshold an owned active task is blocked with the restart
// loop reason and the questions, its claim cleared and one blocked history
// entry written, and the tracker forgets the key; a task the agent does not own
// is counted but never blocked.
func TestA9RestartPB5BlockRecordsReasonQuestionsAndHistory(t *testing.T) {
	bb, tmpDir, task := a9ExitRestartFixture(t, func(s *models.State) { s.Config.Exit42RestartThreshold = 1 })
	authority := testSupervisorAuthority(t, bb, "coder-1")
	tracker := newExit42RestartTracker()

	if outcome, err := tracker.Handle(bb, tmpDir, "coder", task.ID, authority); err != nil || outcome.BlockedTask {
		t.Fatalf("restart at threshold = %+v, %v; want counted without block", outcome, err)
	}
	outcome, err := tracker.Handle(bb, tmpDir, "coder", task.ID, authority)
	if err != nil {
		t.Fatalf("Handle over threshold: %v", err)
	}
	if !outcome.BlockedTask || outcome.Delay != 0 || outcome.RestartCount != 2 {
		t.Fatalf("outcome = %+v, want blocked with zero delay and count 2", outcome)
	}

	state, err := bb.Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	blocked := state.FindTask(task.ID)
	wantReason := "exit code 42 restart loop detected: 2 consecutive restarts without progress (threshold=1)"
	if blocked.Status != models.TaskStatusBlocked || blocked.BlockedReason == nil || *blocked.BlockedReason != wantReason {
		t.Fatalf("blocked task = status %q reason %v, want BLOCKED %q", blocked.Status, blocked.BlockedReason, wantReason)
	}
	if len(blocked.BlockedQuestions) != 2 || blocked.AssignedTo != nil || blocked.LeaseExpires != nil {
		t.Fatalf("blocked questions %v assigned %v lease %v, want 2 questions and the claim cleared",
			blocked.BlockedQuestions, blocked.AssignedTo, blocked.LeaseExpires)
	}
	entries := 0
	for _, h := range blocked.History {
		if h.Event == models.TaskEventBlocked && h.Reason != nil && *h.Reason == wantReason && h.Agent != nil && *h.Agent == "coder-1" {
			entries++
		}
	}
	if entries != 1 {
		t.Fatalf("blocked history entries = %d, want 1", entries)
	}
	if _, kept := tracker.byKey[exit42TrackerKey(task.ID, "coder", "coder-1")]; kept {
		t.Fatal("tracker kept its key after blocking the task")
	}
}

func TestA9RestartPB5UnownedTaskIsCountedButNeverBlocked(t *testing.T) {
	bb, tmpDir, task := a9ExitRestartFixture(t, func(s *models.State) {
		s.Config.Exit42RestartThreshold = 1
		other := "coder-2"
		s.Tasks[0].AssignedTo = &other
		s.Agents[other] = models.Agent{Role: "coder", Status: models.AgentStatusWorking}
	})
	authority := testSupervisorAuthority(t, bb, "coder-1")
	tracker := newExit42RestartTracker()

	for i := 1; i <= 3; i++ {
		outcome, err := tracker.Handle(bb, tmpDir, "coder", task.ID, authority)
		if err != nil || outcome.BlockedTask || outcome.RestartCount != i {
			t.Fatalf("restart %d = %+v, %v; want counted %d and not blocked", i, outcome, err, i)
		}
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if got := state.FindTask(task.ID); got.Status != models.TaskStatusImplementing || got.Exit42RestartCount != 3 {
		t.Fatalf("task = status %q count %d, want IMPLEMENTING count 3", got.Status, got.Exit42RestartCount)
	}
}

func a9WaitFixture(t *testing.T) (*db.Blackboard, string) {
	t.Helper()
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{}
	testhelpers.WriteInitialState(t, statePath, state)
	return db.New(statePath), tmpDir
}

func a9SetStateWatcher(t *testing.T, factory func(*db.Blackboard) (stateWatcher, error)) {
	t.Helper()
	previous := newStateWatcher
	newStateWatcher = factory
	t.Cleanup(func() { newStateWatcher = previous })
}

// workAfter reports no work for the first calls and work afterwards, counting
// every evaluation; the first call is the initial check.
func workAfter(calls *atomic.Int32, noWorkCalls int32) workCheckFunc {
	return func(*models.State) (bool, string) {
		return calls.Add(1) > noWorkCalls, ""
	}
}

// PB-6: a watcher that cannot be created falls back to polling.
func TestA9WaitPB6WatcherSetupFailureFallsBackToPolling(t *testing.T) {
	a9SetStateWatcher(t, func(*db.Blackboard) (stateWatcher, error) { return nil, errors.New("no watcher") })
	bb, root := a9WaitFixture(t)
	var calls atomic.Int32

	hasWork, err := waitForWorkEventDriven(context.Background(), bb, root, 10*time.Millisecond, 5*time.Second, workAfter(&calls, 1), nil)
	if err != nil || !hasWork {
		t.Fatalf("wait = %v, %v; want work found by the polling fallback", hasWork, err)
	}
	if calls.Load() < 2 {
		t.Fatalf("work evaluations = %d, want the initial check and a polling check", calls.Load())
	}
}

type errorWatcher struct{ errs chan error }

func (w *errorWatcher) Events() <-chan struct{} { return make(chan struct{}) }
func (w *errorWatcher) Errors() <-chan error    { return w.errs }
func (w *errorWatcher) Close() error            { return nil }

// PB-6: a watcher error mid-wait falls back to polling for the rest of it.
func TestA9WaitPB6WatcherErrorFallsBackToPolling(t *testing.T) {
	watcher := &errorWatcher{errs: make(chan error, 1)}
	watcher.errs <- errors.New("watcher broke")
	a9SetStateWatcher(t, func(*db.Blackboard) (stateWatcher, error) { return watcher, nil })
	bb, root := a9WaitFixture(t)
	var calls atomic.Int32

	hasWork, err := waitForWorkEventDriven(context.Background(), bb, root, 10*time.Millisecond, 5*time.Second, workAfter(&calls, 1), nil)
	if err != nil || !hasWork {
		t.Fatalf("wait = %v, %v; want work found after the watcher error", hasWork, err)
	}
}

// PB-6: a surplus agent is reported before work is evaluated, on the initial
// check and in the polling loop alike.
func TestA9WaitPB6SurplusPrecedesWork(t *testing.T) {
	bb, root := a9WaitFixture(t)
	surplus := func(*models.State) bool { return true }
	for name, wait := range map[string]func(*atomic.Int32) (bool, error){
		"event-driven": func(calls *atomic.Int32) (bool, error) {
			return waitForWorkEventDriven(context.Background(), bb, root, 10*time.Millisecond, time.Second, workAfter(calls, 0), surplus)
		},
		"polling": func(calls *atomic.Int32) (bool, error) {
			return waitForWorkPolling(context.Background(), bb, root, 10*time.Millisecond, time.Second, workAfter(calls, 0), surplus)
		},
	} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			hasWork, err := wait(&calls)
			if hasWork || !errors.Is(err, errPoolSurplus) {
				t.Fatalf("wait = %v, %v; want errPoolSurplus without work", hasWork, err)
			}
			if calls.Load() != 0 {
				t.Fatalf("work was evaluated %d times before the surplus result", calls.Load())
			}
		})
	}
}

// PB-6: an already cancelled context returns its error before any evaluation,
// and a silent watcher ends at max wait with no work and no error.
func TestA9WaitPB6CancellationBeforeWorkAndMaxWaitWithSilentWatcher(t *testing.T) {
	bb, root := a9WaitFixture(t)
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if hasWork, err := waitForWorkEventDriven(ctx, bb, root, 10*time.Millisecond, time.Second, workAfter(&calls, 0), nil); hasWork || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait = %v, %v; want context.Canceled", hasWork, err)
	}
	if calls.Load() != 0 {
		t.Fatalf("cancelled wait evaluated work %d times", calls.Load())
	}

	a9SetStateWatcher(t, func(*db.Blackboard) (stateWatcher, error) { return newSilentWatcher(), nil })
	start := time.Now()
	hasWork, err := waitForWorkEventDriven(context.Background(), bb, root, 10*time.Millisecond, 150*time.Millisecond,
		func(*models.State) (bool, string) { return false, "" }, nil)
	if err != nil || hasWork {
		t.Fatalf("max-wait result = %v, %v; want no work and no error", hasWork, err)
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("max wait ended after %v, want about 150ms", elapsed)
	}
}

// PB-1/PB-2: with no handoff and no owned task, a nil-authority claim falls to a
// fresh candidate and reports exhausted candidates as the combined error.
func TestA9ClaimPB1NilAuthorityFreshClaimNamesNoClaimableTasks(t *testing.T) {
	bb, root := a9WaitFixture(t)
	_, _, err := claimDoerTask(root, "coder-1", models.RoleCoder, bb)
	if err == nil || !strings.Contains(err.Error(), "no claimable tasks found") {
		t.Fatalf("claimDoerTask on empty state = %v, want no claimable tasks found", err)
	}
}
