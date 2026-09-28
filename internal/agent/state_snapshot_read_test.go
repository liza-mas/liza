package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D100: supervisor observation, polling, and gate reads use lock-free
// snapshots, so they succeed while writers hold the state lock for longer than
// any lock wait. These tests shorten both lock waits, so they must not run in
// parallel.
const (
	snapshotTestLockTimeout        = 200 * time.Millisecond
	snapshotTestPatientLockTimeout = 300 * time.Millisecond
)

// shortenStateLockWaits makes a read that still takes the state lock fail fast
// instead of waiting out the production timeouts.
func shortenStateLockWaits(t *testing.T) {
	t.Helper()
	t.Cleanup(db.SetDefaultLockTimeoutForTest(snapshotTestLockTimeout))
	t.Cleanup(db.SetPatientReadLockTimeoutForTest(snapshotTestPatientLockTimeout))
}

// publishStateReplacing atomically publishes the state file with old replaced
// by replacement, as a lock-holding writer does, without taking the lock.
func publishStateReplacing(t *testing.T, statePath, old, replacement string) {
	t.Helper()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(old)) {
		t.Fatalf("state file does not contain %q", old)
	}
	tmp := statePath + ".test-publish"
	if err := os.WriteFile(tmp, bytes.Replace(data, []byte(old), []byte(replacement), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, statePath); err != nil {
		t.Fatal(err)
	}
}

func TestReviewerWorktreeSetupReadsStateWhileLockHeld(t *testing.T) {
	shortenStateLockWaits(t)
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// GIVEN a configured setup command and an intact reviewer worktree
	state := testhelpers.CreateValidState()
	postCmd := "echo ran > setup-ran"
	state.Config.PostWorktreeCmd = &postCmd
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, time.Now().UTC())}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	wtPath := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatal(err)
	}

	// WHEN setup runs while a writer holds the state lock
	testhelpers.HoldFileLock(t, statePath)
	err := runReviewerWorktreeSetup(bb, "task-1", wtPath)

	// THEN the configured command ran in the worktree
	if err != nil {
		t.Fatalf("runReviewerWorktreeSetup under a held state lock: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(wtPath, "setup-ran")); statErr != nil {
		t.Fatalf("setup command did not run: %v", statErr)
	}
}

func TestExecutionProgressSnapshotsReadStateWhileLockHeld(t *testing.T) {
	shortenStateLockWaits(t)
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// GIVEN a coder executing its assigned task
	agentID, taskID := "coder-1", "task-1"
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, time.Now().UTC())
	task.AssignedTo = &agentID
	state.Tasks = []models.Task{task}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	pr := testResolver(t)

	// WHEN the watchdogs take progress snapshots while a writer holds the lock
	testhelpers.HoldFileLock(t, statePath)
	execSig, execEligible, execErr := readExecutionProgressSnapshot(context.Background(), tmpDir, bb, taskID, agentID, pr)
	turnSig, turnEligible, turnErr := readSuccessfulTurnProgressSnapshot(tmpDir, bb, taskID, agentID, pr)

	// THEN both observe the executing task
	if execErr != nil || !execEligible || execSig == "" {
		t.Fatalf("execution progress snapshot: sig=%q eligible=%v err=%v", execSig, execEligible, execErr)
	}
	if turnErr != nil || !turnEligible || turnSig == "" {
		t.Fatalf("successful-turn progress snapshot: sig=%q eligible=%v err=%v", turnSig, turnEligible, turnErr)
	}
}

func TestReviewOwnershipLossObservedWhileLockHeld(t *testing.T) {
	shortenStateLockWaits(t)
	config, _, _ := reviewExecutionFixture(t)

	// GIVEN a review ownership watchdog started while a writer holds the lock
	testhelpers.HoldFileLock(t, config.StatePath)
	cancelled := make(chan struct{})
	stop, err := startReviewExecutionWatchdog(context.Background(), config, "review-task", 20*time.Millisecond, func() { close(cancelled) })
	if err != nil {
		t.Fatalf("review watchdog start under a held state lock: %v", err)
	}

	// WHEN that writer publishes a review claim owned by another reviewer
	publishStateReplacing(t, config.StatePath, "reviewing_by: code-reviewer-1", "reviewing_by: other-reviewer")

	// THEN the watchdog observes the loss and cancels the provider turn
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		stop()
		t.Fatal("review ownership loss not observed while the state lock is held")
	}
	if !stop() {
		t.Fatal("watchdog did not report ownership lost")
	}
}

func TestProviderLaunchGateReadsStateWhileLockHeld(t *testing.T) {
	shortenStateLockWaits(t)
	fixture := newProviderGenerationFixture(t)
	testhelpers.HoldFileLock(t, fixture.statePath)

	started := false
	err := newProviderLaunchGate(fixture.config(fixture.authorityA))(context.Background(), func() error {
		started = true
		return nil
	})
	if err != nil || !started {
		t.Fatalf("provider launch under a held state lock: err=%v started=%v", err, started)
	}
}

func TestAutoAssignAgentIDReadsStateWhileLockHeld(t *testing.T) {
	shortenStateLockWaits(t)
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	bb := testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())
	testhelpers.HoldFileLock(t, statePath)

	assignedID, err := AutoAssignAgentID(bb, "coder", 1, func(string) error { return nil })
	if err != nil || assignedID != "coder-1" {
		t.Fatalf("AutoAssignAgentID under a held state lock: id=%q err=%v, want coder-1", assignedID, err)
	}
}

func TestWaitForWorkPollingReadsStateWhileLockHeld(t *testing.T) {
	shortenStateLockWaits(t)
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	bb := testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())
	testhelpers.HoldFileLock(t, statePath)

	hasWork, err := waitForWorkPolling(context.Background(), bb, tmpDir, 20*time.Millisecond, 5*time.Second,
		func(*models.State) (bool, string) { return true, "" })
	if err != nil || !hasWork {
		t.Fatalf("waitForWorkPolling under a held state lock: hasWork=%v err=%v", hasWork, err)
	}
}

func TestCheckAbortObservesStopWhileLockHeld(t *testing.T) {
	shortenStateLockWaits(t)
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeStopped
	testhelpers.WriteInitialState(t, statePath, state)
	testhelpers.HoldFileLock(t, statePath)

	if !checkAbort(tmpDir) {
		t.Fatal("checkAbort under a held state lock did not observe STOPPED")
	}
}

// The clean-task cleanup authority check is the only fence in front of
// DeleteWorktree's effects, so its read keeps the lock: it must observe a
// generation replacement published by the writer it waited for.
func TestCleanTaskCleanupRejectsReplacementPublishedDuringLockWait(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// GIVEN a clean-terminal task that still records a worktree
	taskID := "integration-global-1"
	worktree := filepath.Join(paths.WorktreesDirName, taskID)
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatus("INTEGRATION_ANALYSIS_CLEAN"), time.Now().UTC())
	task.RolePair = "integration-pair"
	task.Worktree = &worktree
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	const orchestratorID = "orchestrator-1"
	state.Agents[orchestratorID] = testhelpers.RegisteredTestAgent("orchestrator")
	bb := testhelpers.WriteInitialState(t, statePath, state)
	authority := testSupervisorAuthority(t, bb, orchestratorID)

	// WHEN cleanup starts while a writer holds the lock, and that writer
	// publishes a replacement generation before releasing it
	release := testhelpers.HoldFileLock(t, statePath)
	done := make(chan error, 1)
	go func() { done <- handleCleanTaskCleanup(tmpDir, authority) }()
	// No hook reports that cleanup reached its read; give it time to do so, and
	// prove it has not finished.
	select {
	case err := <-done:
		t.Fatalf("cleanup returned before the replacement was published: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	publishStateReplacing(t, statePath, "generation: "+authority.Generation, "generation: replacement-generation")
	release()
	err := <-done

	// THEN the stale generation is rejected and the worktree is still recorded
	if !ops.IsAgentAuthorityError(err) {
		t.Fatalf("cleanup with a replaced registration: err=%v, want agent authority error", err)
	}
	after, readErr := bb.ReadSnapshot()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got := after.FindTask(taskID); got == nil || got.Worktree == nil {
		t.Fatal("stale cleanup cleared the task worktree")
	}
}
