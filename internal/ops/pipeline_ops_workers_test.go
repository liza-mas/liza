package ops

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// effectiveCompletionWorkers owns the goroutines and blocking gates of a
// completion scenario. Register hook restoration before creating this owner.
type effectiveCompletionWorkers struct {
	wg sync.WaitGroup
}

func newEffectiveCompletionWorkers(t *testing.T, release ...func()) *effectiveCompletionWorkers {
	t.Helper()
	workers := &effectiveCompletionWorkers{}
	t.Cleanup(func() {
		for _, unblock := range release {
			unblock()
		}
		workers.wg.Wait()
	})
	return workers
}

func (workers *effectiveCompletionWorkers) start(invoke func() error) <-chan error {
	done := make(chan error, 1)
	workers.wg.Add(1)
	go func() {
		defer workers.wg.Done()
		done <- invoke()
	}()
	return done
}

func TestEffectiveIntegrationCompletionGateWorkerLifetime(t *testing.T) {
	progressionGate := make(chan struct{})
	receiptGate := make(chan struct{})
	progressionReleased := make(chan struct{})
	receiptReleased := make(chan struct{})
	allowWorkerReturn := make(chan struct{})
	hooksRestored := make(chan struct{})
	ownerReturned := make(chan struct{})
	unblockProgression := sync.OnceFunc(func() { close(progressionGate) })
	unblockReceipt := sync.OnceFunc(func() { close(receiptGate) })
	unblockReturn := sync.OnceFunc(func() { close(allowWorkerReturn) })
	var progressionResult, receiptResult <-chan error
	workerErr := errors.New("receipt worker failed")

	// The owner exits before its workers reach their hooks, as on an
	// authorization timeout. Keep both workers alive after gate release so
	// hook restoration cannot race past an unfinished worker unnoticed.
	go func() {
		defer close(ownerReturned)
		t.Run("owner", func(t *testing.T) {
			t.Cleanup(func() { close(hooksRestored) })
			workers := newEffectiveCompletionWorkers(t, unblockProgression, unblockReceipt)
			progressionResult = workers.start(func() error {
				<-progressionGate
				close(progressionReleased)
				<-allowWorkerReturn
				return nil
			})
			receiptResult = workers.start(func() error {
				<-receiptGate
				close(receiptReleased)
				<-allowWorkerReturn
				return workerErr
			})
		})
	}()
	// Also drain the deliberately broken harness when this regression fails.
	defer func() {
		unblockProgression()
		unblockReceipt()
		unblockReturn()
		<-ownerReturned
		if err := <-progressionResult; err != nil {
			t.Errorf("progression result = %v", err)
		}
		if err := <-receiptResult; !errors.Is(err, workerErr) {
			t.Errorf("receipt result = %v, want %v", err, workerErr)
		}
	}()
	for _, released := range []<-chan struct{}{progressionReleased, receiptReleased} {
		select {
		case <-released:
		case <-time.After(5 * time.Second):
			t.Fatal("cleanup did not release every worker gate")
		}
	}
	select {
	case <-ownerReturned:
		t.Error("subtest returned while its workers were still running")
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case <-hooksRestored:
		t.Error("shared hooks restored before worker exit")
	default:
	}
}
