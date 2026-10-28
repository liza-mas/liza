package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Persistence order is not merge order: a merge's integration test runs
// between its ref update and its MERGED write, so an earlier merge can record
// its deferral after a later one. Attribution and due-ness follow ancestry.
func TestDeferredValidationLateDeferralAttributesEarliestMerge(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	check := dvCommand(dvCheckValue, 60)
	culpritBefore := f.merge("task-0", map[string]string{"z.txt": "z\n"})
	culpritCommit := f.merge("task-1", map[string]string{"value.txt": "broken\n"})
	f.merge("task-2", map[string]string{"b.txt": "b\n"}, check)
	// task-1's MERGED write lands after task-2's.
	f.modify(func(state *models.State) {
		ledger := state.Goal.Integration.DeferredValidation
		ledger.Deferrals = append(ledger.Deferrals, models.DeferredValidationDeferral{
			TaskID: "task-1", MergeCommit: culpritCommit, BeforeCommit: culpritBefore, RecordedAt: time.Now().UTC(), Commands: []models.DeferredValidationCommand{check},
		})
	})
	ledger := f.ledger()
	if baseline := deferredValidationBaseline(f.root, ledger, ledger.EffectiveCommands()); baseline != culpritBefore {
		t.Fatalf("periodic baseline = %s, want task-1's start %s so its merge counts", baseline, culpritBefore)
	}

	batch := onlyBatch(t, f.run(false))
	if len(batch.Culprits) != 1 || batch.Culprits[0].TaskID != "task-1" || batch.Culprits[0].MergeCommit != culpritCommit {
		t.Fatalf("culprits = %#v, want task-1", batch.Culprits)
	}
}

// After a batch ends without a verdict at an unchanged HEAD, the trigger still
// starts another while the gate waits, until the per-commit attempt cap.
func TestDeferredValidationTriggerRetriesInfraFailureWithinAttemptCap(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	setup := "exit 3"
	f.modify(func(state *models.State) {
		state.Config.DeferredValidation = &models.DeferredValidationConfig{BatchEveryMerges: 1}
		state.Config.PostWorktreeCmd = &setup
	})
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))
	spawn := func(string) error { return nil }
	for attempt := 1; attempt <= models.DeferredValidationMaxAttemptsPerCommit; attempt++ {
		if started, err := startDeferredValidationIfDueWith(f.root, spawn); err != nil || !started {
			t.Fatalf("attempt %d: started=%v err=%v", attempt, started, err)
		}
		if batch := onlyBatch(t, f.run(false)); batch.Status != models.DeferredValidationBatchInfraFailed {
			t.Fatalf("attempt %d batch = %#v", attempt, batch)
		}
	}
	if started, err := startDeferredValidationIfDueWith(f.root, spawn); err != nil || started {
		t.Fatalf("exhausted attempts must stop retries: started=%v err=%v", started, err)
	}
}

// A halt is expected, not a failure: the trigger stays quiet and starts nothing.
func TestDeferredValidationTriggerIsQuietWhileHalted(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	f.modify(func(state *models.State) {
		state.Config.DeferredValidation = &models.DeferredValidationConfig{BatchEveryMerges: 1}
		state.Config.Mode = models.SystemModeStopped
	})
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))
	spawned := false
	started, err := startDeferredValidationIfDueWith(f.root, func(string) error { spawned = true; return nil })
	if err != nil || started || spawned {
		t.Fatalf("halted trigger: started=%v spawned=%v err=%v", started, spawned, err)
	}
}

// When a step's descendants cannot be shown to have ended, the runner starts
// nothing more, certifies nothing, and leaves an unsettled record that fences
// successors and blocks integration until an operator settles it.
func TestDeferredValidationUnstoppableDescendantsFailClosed(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	trigger := filepath.Join(t.TempDir(), "orphaning-step-ran")
	later := filepath.Join(t.TempDir(), "later-step-ran")
	// The later command's lane queues behind a gate slot the test holds until
	// settlement, and the trigger step runs only once it queued: the orphaned
	// step must end that wait, and no reap but the trigger lane's follows it.
	gate := f.holdGate()
	f.merge("task-1", map[string]string{"a.txt": "a\n"},
		dvCommand(f.dvAwaitGateQueue()+fmt.Sprintf(": > %q", trigger), 60),
		dvCommand(fmt.Sprintf(": > %q # gated", later), 60))
	// Cleanup fails once, for the step that touches the trigger; every later
	// step would clean up normally, so only the runner's own refusal keeps
	// them from starting.
	var failedOnce atomic.Bool
	hooks := deferredValidationHooks{platformSupported: true, reap: func(pid int, wait time.Duration) bool {
		if _, err := os.Stat(trigger); err == nil && failedOnce.CompareAndSwap(false, true) {
			return false
		}
		return reapProcessGroup(pid, wait)
	}}

	result, err := runDeferredValidation(context.Background(), f.root, RunDeferredValidationOptions{}, hooks)
	if err == nil || !strings.Contains(err.Error(), "settle") {
		t.Fatalf("err = %v, want an unsettled refusal naming settle", err)
	}
	if result == nil || len(result.Batches) != 1 {
		t.Fatalf("result = %#v", result)
	}
	batch := result.Batches[0]
	if batch.Status != models.DeferredValidationBatchAborted || batch.Reason != string(deferredValidationReasonUnstoppable) {
		t.Fatalf("batch = %#v, want aborted %s", batch, deferredValidationReasonUnstoppable)
	}
	if _, err := os.Stat(later); !os.IsNotExist(err) {
		t.Fatalf("a later step started after cleanup failed (stat err = %v)", err)
	}
	ledger := f.ledger()
	if ledger.Runner == nil || ledger.Runner.Unsettled != string(deferredValidationReasonUnstoppable) {
		t.Fatalf("runner record = %#v, want it kept unsettled", ledger.Runner)
	}
	state, _ := f.bb.Read()
	head := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
	if _, blocked := deferredValidationGate(state, head); blocked == nil || blocked.Code != string(integrationProgressBlockedDeferredUnsettled) {
		t.Fatalf("gate = %#v, want blocked %s", blocked, integrationProgressBlockedDeferredUnsettled)
	}
	if started, err := startDeferredValidationIfDueWith(f.root, func(string) error { return nil }); err != nil || started {
		t.Fatalf("trigger with an unsettled runner: started=%v err=%v", started, err)
	}
	// Even with the lock free, a successor refuses an unsettled predecessor.
	if _, err := RunDeferredValidation(context.Background(), f.root, RunDeferredValidationOptions{Force: true}); err == nil || !strings.Contains(err.Error(), "settle") {
		t.Fatalf("successor err = %v, want a settle refusal", err)
	}
	if _, err := SettleDeferredValidation(f.root, "operator confirmed no batch process survives"); err != nil {
		t.Fatal(err)
	}
	// The trigger step again waits for the later lane to queue behind the
	// gate; the gate opens once it ran.
	if err := os.Remove(trigger); err != nil {
		t.Fatal(err)
	}
	done := make(chan *RunDeferredValidationResult, 1)
	go func() {
		result, _ := RunDeferredValidation(context.Background(), f.root, RunDeferredValidationOptions{Force: true})
		done <- result
	}()
	waitForFile(t, trigger, time.Minute)
	gate.release()
	if batch := onlyBatch(t, <-done); batch.Status != models.DeferredValidationBatchPassed {
		t.Fatalf("batch after settlement = %#v", batch)
	}
}

// When removing the batch worktree halts, the directory is left alone: a
// descendant may still use it. The directory is not a registered worktree, so
// Git's removal fails and would otherwise reach the filesystem fallback.
func TestDeferredValidationHaltedWorktreeRemovalKeepsDirectory(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))
	worktree := filepath.Join(deferredValidationDir(f.root), "worktree")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(worktree, "descendant-still-uses-this")
	if err := os.WriteFile(marker, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The first step is "worktree prune", the second "worktree remove".
	var steps atomic.Int32
	hooks := deferredValidationHooks{platformSupported: true, reap: func(pid int, wait time.Duration) bool {
		if steps.Add(1) == 2 {
			return false
		}
		return reapProcessGroup(pid, wait)
	}}

	_, err := runDeferredValidation(context.Background(), f.root, RunDeferredValidationOptions{}, hooks)
	if err == nil || !strings.Contains(err.Error(), "settle") {
		t.Fatalf("err = %v, want an unsettled refusal naming settle", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("worktree was deleted after cleanup became unsettled: %v", err)
	}
}

// A platform where the runner cannot establish that descendants ended
// refuses to run rather than certify a batch.
func TestDeferredValidationUnsupportedPlatformRefuses(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))
	_, err := runDeferredValidation(context.Background(), f.root, RunDeferredValidationOptions{}, deferredValidationHooks{reap: func(int, time.Duration) bool { return false }})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("err = %v, want an unsupported-platform refusal", err)
	}
	if ledger := f.ledger(); len(ledger.Batches) != 0 || ledger.Runner != nil {
		t.Fatalf("refused run wrote batches or a runner record: %#v", ledger)
	}
}

// Status presentation masks retirement rationale and batch text in the
// viewer's environment.
func TestDeferredValidationStatusMasksRetirementRuling(t *testing.T) {
	const secret = "synthetic-dv-fixture-value-0123456789"
	t.Setenv("DV_FIXTURE_API_TOKEN", secret)
	f := newDVFixture(t, map[string]string{"old.txt": "x\n"})
	check := dvCommand("test -f old.txt", 60)
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, check)
	if _, err := RetireDeferredValidation(f.root, check.Command, "operator pasted "+secret); err != nil {
		t.Fatal(err)
	}
	status, err := ReadDeferredValidationStatus(f.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Retirements) != 1 || strings.Contains(status.Retirements[0].Ruling, secret) {
		t.Fatalf("retirement ruling not masked: %#v", status.Retirements)
	}
}
