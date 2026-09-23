package filelock

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestTryHoldExcludesOtherHoldersUntilRelease(t *testing.T) {
	fl := New(filepath.Join(t.TempDir(), "index-refresh"))

	held, acquired, err := fl.TryHold("first")
	if err != nil || !acquired {
		t.Fatalf("TryHold() = (%v, %v), want acquired", acquired, err)
	}
	if _, again, err := fl.TryHold("second"); err != nil || again {
		t.Fatalf("second TryHold() = (%v, %v), want busy without error", again, err)
	}
	if err := held.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	next, acquired, err := fl.TryHold("third")
	if err != nil || !acquired {
		t.Fatalf("TryHold() after release = (%v, %v), want acquired", acquired, err)
	}
	if err := next.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
}

// TryHold must share the lock authority of the blocking API (ADR-0061), not
// take a parallel lock that both could hold at once.
func TestTryHoldExcludesBlockingLockers(t *testing.T) {
	fl := New(filepath.Join(t.TempDir(), "index-refresh"))
	held, acquired, err := fl.TryHold("hold")
	if err != nil || !acquired {
		t.Fatalf("TryHold() = (%v, %v), want acquired", acquired, err)
	}
	defer held.Release()

	err = fl.WithTimeout(150*time.Millisecond).WithLockOperation("blocking", func() error {
		t.Fatal("blocking locker ran while TryHold held the lock")
		return nil
	})
	var lockErr *LockError
	if !errors.As(err, &lockErr) || lockErr.Type != LockErrorTimeout {
		t.Fatalf("WithLockOperation() error = %v, want lock timeout", err)
	}
}
