package testhelpers

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
)

func TestHoldFileLockHoldsUntilRelease(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.yaml")
	release := HoldFileLock(t, path)
	contender := filelock.New(path).WithTimeout(50 * time.Millisecond)
	if err := contender.WithLock(func() error { return nil }); !filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		t.Fatalf("contender while held: err=%v, want lock timeout", err)
	}
	release()
	if err := filelock.New(path).WithTimeout(5 * time.Second).WithLock(func() error { return nil }); err != nil {
		t.Fatalf("contender after release: %v", err)
	}
}
