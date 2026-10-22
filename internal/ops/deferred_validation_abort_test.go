package ops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
)

// holdRunnerLock holds the runner lock for the test, as a live runner does.
func holdRunnerLock(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(deferredValidationDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	held, acquired, err := filelock.New(filepath.Join(deferredValidationDir(root), "runner")).TryHold("test-runner")
	if err != nil || !acquired {
		t.Fatalf("hold runner lock: acquired=%v err=%v", acquired, err)
	}
	t.Cleanup(func() { _ = held.Release() })
}

func (f *dvFixture) setRunner(runner *models.DeferredValidationRunner) {
	f.t.Helper()
	f.modify(func(state *models.State) {
		if state.Goal.Integration == nil {
			state.Goal.Integration = &models.IntegrationLifecycle{}
		}
		if state.Goal.Integration.DeferredValidation == nil {
			state.Goal.Integration.DeferredValidation = &models.DeferredValidationLedger{}
		}
		state.Goal.Integration.DeferredValidation.Runner = runner
	})
}

func requireAbortRefused(t *testing.T, err error) {
	t.Helper()
	var precondition *PreconditionError
	if !errors.As(err, &precondition) {
		t.Fatalf("err = %v, want a precondition refusal", err)
	}
}

// An abort is recorded only for a runner that holds the lock: a record a dead
// runner left, an unsettled record or no record is refused without a write.
func TestAbortDeferredValidationRequiresALiveRunner(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	head := f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))
	record := &models.DeferredValidationRunner{Token: "token-1", PID: 1, Commit: head, StartedAt: time.Now().UTC()}

	if _, err := AbortDeferredValidation(f.root, " "); err == nil {
		t.Fatal("an abort without a ruling was accepted")
	}
	f.setRunner(record)
	_, err := AbortDeferredValidation(f.root, "D-181 test")
	requireAbortRefused(t, err)
	if f.ledger().Runner.Abort != nil {
		t.Fatal("an abort was written onto a stale record")
	}

	holdRunnerLock(t, f.root)
	for _, want := range []string{"requested", "unchanged"} {
		result, err := AbortDeferredValidation(f.root, "D-181 test")
		if err != nil || result.Outcome != want {
			t.Fatalf("abort = %#v, %v; want %s", result, err, want)
		}
	}
	if abort := f.ledger().Runner.Abort; abort == nil || abort.Ruling != "D-181 test" {
		t.Fatalf("runner abort = %#v", abort)
	}

	unsettled := *record
	unsettled.Unsettled = deferredValidationReasonUnstoppable
	f.setRunner(&unsettled)
	_, err = AbortDeferredValidation(f.root, "D-181 test")
	requireAbortRefused(t, err)

	f.setRunner(nil)
	_, err = AbortDeferredValidation(f.root, "D-181 test")
	requireAbortRefused(t, err)
}

// An operator abort kills the running step, records it not run, keeps the
// finished result, and suppresses automatic retries at that HEAD (D-181).
func TestDeferredValidationOperatorAbortEndsTheLiveBatch(t *testing.T) {
	shortHaltPoll(t)
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	signal := t.TempDir()
	quickDone, slowStarted, slowArmed := filepath.Join(signal, "quick"), filepath.Join(signal, "slow"), filepath.Join(signal, "armed")
	if err := os.WriteFile(slowArmed, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	quick := dvCommand(fmt.Sprintf("touch %q", quickDone), 60)
	slow := dvCommand(fmt.Sprintf("if [ -f %q ]; then touch %q; sleep 300; fi", slowArmed, slowStarted), 600)
	head := f.merge("task-1", map[string]string{"a.txt": "a\n"}, quick, slow)

	started := time.Now()
	done := f.runInBackground()
	waitForFile(t, quickDone, 30*time.Second)
	waitForFile(t, slowStarted, 30*time.Second)
	if result, err := AbortDeferredValidation(f.root, "D-181 test"); err != nil || result.Outcome != "requested" {
		t.Fatalf("abort = %#v, %v", result, err)
	}

	batch := onlyBatch(t, awaitOutcome(t, done, 60*time.Second, "the aborted step kept running").result)
	if time.Since(started) > 120*time.Second {
		t.Fatalf("the abort did not kill the running step: %s", time.Since(started))
	}
	if batch.Status != models.DeferredValidationBatchAborted || batch.Reason != deferredValidationReasonOperatorAbort {
		t.Fatalf("batch = %#v, want aborted %s", batch, deferredValidationReasonOperatorAbort)
	}
	requireResult(t, batch, 0, models.DeferredValidationResultPassed, "")
	requireResult(t, batch, 1, models.DeferredValidationResultNotRun, "")
	if len(batch.Culprits) != 0 {
		t.Fatalf("an aborted step was bisected: %#v", batch.Culprits)
	}
	if f.ledger().Runner != nil {
		t.Fatal("the aborted batch left its runner record")
	}

	if again := f.run(false); again.Outcome != "skipped" || again.Reason != "aborted_at_head" {
		t.Fatalf("same HEAD after an operator abort: %#v", again)
	}
	state, _ := f.bb.Read()
	if _, blocked := deferredValidationGate(state, head); blocked == nil || blocked.Code != integrationProgressBlockedDeferredAborted {
		t.Fatalf("gate = %#v, want %s", blocked, integrationProgressBlockedDeferredAborted)
	}
	if err := os.Remove(slowArmed); err != nil {
		t.Fatal(err)
	}
	if forced := onlyBatch(t, f.run(true)); forced.Status != models.DeferredValidationBatchPassed {
		t.Fatalf("forced batch = %#v", forced)
	}
}

// An abort observed after the last admitted step, while a finishing read
// runs, still ends the batch as the operator's abort and keeps the result.
func TestDeferredValidationOperatorAbortDuringFinishingRead(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	command := dvCommand("printf 'D181_STEP_FINISHED\\n'", 60)
	head := f.merge("task-1", map[string]string{"a.txt": "a\n"}, command)
	state, err := f.bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	r := &deferredValidationRunner{root: f.root, dir: deferredValidationDir(f.root), bb: f.bb, log: io.Discard}
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sawStep, afterStep, triggered := false, 0, false
	r.mask = func(s string) string {
		if s == "D181_STEP_FINISHED\n" {
			sawStep = true
		}
		return s
	}
	r.reap = func(pid int, wait time.Duration) bool {
		ended := reapProcessGroup(pid, wait)
		if sawStep {
			// The command, then the finishing rev-parse, then the finishing
			// status: abort after the last one ran.
			if afterStep++; afterStep == 3 {
				triggered = true
				r.abortBatch()
			}
		}
		return ended
	}
	batch := r.execute(context.Background(), state, head, "token", false)
	if !triggered {
		t.Fatal("the final finishing read was not reached")
	}
	requireResult(t, batch, 0, models.DeferredValidationResultPassed, "")
	if batch.Status != models.DeferredValidationBatchAborted || batch.Reason != deferredValidationReasonOperatorAbort {
		t.Fatalf("batch = %s/%q, want aborted/%s", batch.Status, batch.Reason, deferredValidationReasonOperatorAbort)
	}
}

// Only an execution the abort interrupted becomes a halt: a nonzero exit
// that completed before the abort keeps its exit code.
func TestDeferredValidationOperatorAbortClassifiesOnlyInterruptedSteps(t *testing.T) {
	t.Parallel()
	newRunner := func(t *testing.T) *deferredValidationRunner {
		f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
		r := &deferredValidationRunner{root: f.root, bb: f.bb, mask: func(s string) string { return s }, log: io.Discard, reap: reapProcessGroup}
		r.beginBatch(context.Background())
		t.Cleanup(func() { r.endWaits(); r.endKill() })
		return r
	}
	shell := func(script string) func(context.Context) (*exec.Cmd, error) {
		return func(ctx context.Context) (*exec.Cmd, error) { return exec.CommandContext(ctx, "sh", "-c", script), nil }
	}

	t.Run("completed failure", func(t *testing.T) {
		t.Parallel()
		r := newRunner(t)
		r.reap = func(pid int, wait time.Duration) bool {
			ended := reapProcessGroup(pid, wait)
			r.abortBatch() // the process exited and its group is empty
			return ended
		}
		result := r.run(context.Background(), time.Minute, true, shell("exit 17"))
		if result.halted || result.exitCode != 17 {
			t.Fatalf("result = halted %v exit %d, want the completed exit 17 kept", result.halted, result.exitCode)
		}
	})
	t.Run("interrupted execution", func(t *testing.T) {
		t.Parallel()
		r := newRunner(t)
		started := filepath.Join(t.TempDir(), "started")
		go func() {
			// No t.Fatal off the test goroutine: abort after the start or
			// the deadline; the assertion below tells them apart.
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); <-ticker.C {
				if _, err := os.Stat(started); err == nil {
					break
				}
			}
			r.abortBatch()
		}()
		begin := time.Now()
		result := r.run(context.Background(), time.Minute, true, shell(fmt.Sprintf("touch %q; sleep 300", started)))
		if !result.halted || time.Since(begin) > 30*time.Second {
			t.Fatalf("result = halted %v after %s, want the running step killed as a halt", result.halted, time.Since(begin))
		}
	})
}

// An abort also ends a lane waiting for its admission slot.
func TestDeferredValidationOperatorAbortEndsASlotWait(t *testing.T) {
	f := newAdmissionDVFixture(t)
	shortHaltPoll(t)
	holdAdmissionSlots(t, f.root, valueAdmissionLimit)

	done := f.runInBackground()
	waitForTickets(t, f.root, valueAdmissionLimit.Class, 1)
	if result, err := AbortDeferredValidation(f.root, "D-181 test"); err != nil || result.Outcome != "requested" {
		t.Fatalf("abort = %#v, %v", result, err)
	}

	batch := onlyBatch(t, awaitOutcome(t, done, 30*time.Second, "the queued probe did not observe the abort").result)
	if batch.Status != models.DeferredValidationBatchAborted || batch.Reason != deferredValidationReasonOperatorAbort ||
		batch.Results[0].Status != models.DeferredValidationResultNotRun {
		t.Fatalf("batch = %#v, want aborted by the operator with the command not run", batch)
	}
}
