package ops

import (
	"bytes"
	"context"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

var valueAdmissionLimit = models.AdmissionLimit{Class: "suite", MaxConcurrent: 1, Match: []string{"value.txt"}}

func newAdmissionDVFixture(t *testing.T) *dvFixture {
	t.Helper()
	shortAdmissionSlotPoll(t)
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))
	f.modify(func(state *models.State) { state.Config.AdmissionLimits = []models.AdmissionLimit{valueAdmissionLimit} })
	return f
}

func TestDeferredValidationProbeWaitsInTheSharedAdmissionPool(t *testing.T) {
	f := newAdmissionDVFixture(t)
	held := holdAdmissionSlots(t, f.root, valueAdmissionLimit)

	type outcome struct {
		result *RunDeferredValidationResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := RunDeferredValidation(context.Background(), f.root, RunDeferredValidationOptions{})
		done <- outcome{result, err}
	}()
	waitForTickets(t, f.root, valueAdmissionLimit.Class, 1)
	select {
	case got := <-done:
		t.Fatalf("runner finished while its probe's class was full: %+v, %v", got.result, got.err)
	default:
	}
	held.release()
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	if batch := onlyBatch(t, got.result); batch.Status != models.DeferredValidationBatchPassed {
		t.Fatalf("batch after the slot freed = %+v", batch)
	}
}

// dvOutcome is one runner invocation's return.
type dvOutcome struct {
	result *RunDeferredValidationResult
	err    error
}

func (f *dvFixture) runInBackground() <-chan dvOutcome {
	done := make(chan dvOutcome, 1)
	go func() {
		result, err := RunDeferredValidation(context.Background(), f.root, RunDeferredValidationOptions{})
		done <- dvOutcome{result, err}
	}()
	return done
}

func awaitOutcome(t *testing.T, done <-chan dvOutcome, limit time.Duration, why string) dvOutcome {
	t.Helper()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		return got
	case <-time.After(limit):
		t.Fatalf("runner still running after %s: %s", limit, why)
	}
	return dvOutcome{}
}

func shortHaltPoll(t *testing.T) {
	t.Helper()
	previous := deferredValidationHaltPollInterval
	deferredValidationHaltPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { deferredValidationHaltPollInterval = previous })
}

func (f *dvFixture) setMode(mode models.SystemMode) error {
	return f.bb.Modify(func(state *models.State) error { state.Config.Mode = mode; return nil })
}

// lockedLog is a standard-logger sink the test reads while the runner writes.
type lockedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedLog) contains(text string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Contains(l.buf.String(), text)
}

// A probe budgeted at the whole acceptance cap still queues for its slot:
// the detached runner has no submit grace to protect, so a full class makes
// it wait instead of aborting the batch (D-223).
func TestDeferredValidationCapBudgetProbeQueuesForItsSlot(t *testing.T) {
	f := newAdmissionDVFixture(t)
	shortAdmissionSlotCap(t, 60*time.Second) // the fixture command's budget
	held := holdAdmissionSlots(t, f.root, valueAdmissionLimit)
	// The waiting line is logged only after a slot attempt failed with time
	// left to wait, so the slot is released only once the probe was blocked.
	waits := &lockedLog{}
	previous := log.Writer()
	log.SetOutput(waits)
	t.Cleanup(func() { log.SetOutput(previous) })

	done := f.runInBackground()
	for !waits.contains("admission slot of class " + valueAdmissionLimit.Class) {
		select {
		case got := <-done:
			t.Fatalf("runner finished while its probe's class was full: %+v, %v", got.result, got.err)
		case <-time.After(5 * time.Millisecond):
		}
	}
	held.release()

	got := awaitOutcome(t, done, 30*time.Second, "the freed slot was not taken")
	if batch := onlyBatch(t, got.result); batch.Status != models.DeferredValidationBatchPassed {
		t.Fatalf("batch after the slot freed = %+v", batch)
	}
}

// A halt ends a slot wait that no release would end: the batch aborts as
// halted with the command not run.
func TestDeferredValidationHaltEndsASlotWait(t *testing.T) {
	f := newAdmissionDVFixture(t)
	shortHaltPoll(t)
	holdAdmissionSlots(t, f.root, valueAdmissionLimit)

	done := f.runInBackground()
	waitForTickets(t, f.root, valueAdmissionLimit.Class, 1)
	if err := f.setMode(models.SystemModePaused); err != nil {
		t.Fatal(err)
	}

	got := awaitOutcome(t, done, 30*time.Second, "the queued probe did not observe the halt")
	batch := onlyBatch(t, got.result)
	if batch.Status != models.DeferredValidationBatchAborted || batch.Reason != "halted" ||
		len(batch.Results) != 1 || batch.Results[0].Status != models.DeferredValidationResultNotRun {
		t.Fatalf("batch = %+v, want aborted halted with the command not run", batch)
	}
	if f.ledger().Runner != nil {
		t.Fatal("aborted batch left its runner record")
	}
}
