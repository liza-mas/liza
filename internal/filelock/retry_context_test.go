package filelock_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestRetryContextOutlastsAcquisitionTimeout(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.yaml")
	release := testhelpers.HoldFileLock(t, path)
	time.AfterFunc(350*time.Millisecond, release)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// WithTimeout must preserve the retry policy, as Blackboard.Patient does.
	lock := filelock.New(path).WithRetryContext(ctx).WithTimeout(50 * time.Millisecond)
	calls := 0
	if err := lock.WithLock(func() error { calls++; return nil }); err != nil {
		t.Fatalf("transient contention killed operation: %v", err)
	}
	if calls != 1 {
		t.Fatalf("callback ran %d times, want once", calls)
	}
}

func TestRetryContextCancelsWithoutCallback(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.yaml")
	testhelpers.HoldFileLock(t, path)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(250*time.Millisecond, cancel)
	defer cancel()
	called := false
	err := filelock.New(path).WithTimeout(50 * time.Millisecond).WithRetryContext(ctx).WithLock(func() error {
		called = true
		return nil
	})
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("cancellation: err=%v callback=%v", err, called)
	}
}

func TestRetryContextNeverRetriesCallbackError(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	lock := filelock.New(filepath.Join(t.TempDir(), "state.yaml")).WithRetryContext(ctx)
	inner := filelock.NewLockTimeout(errors.New("nested operation timed out after effects"))
	calls := 0
	err := lock.WithLock(func() error { calls++; return inner })
	if err != inner || calls != 1 {
		t.Fatalf("callback error replayed: err=%v calls=%d", err, calls)
	}
}

func TestRetryContextFilesystemErrorReturnsImmediately(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	called := false
	err := filelock.New(filepath.Join(path, "state.yaml")).WithRetryContext(ctx).WithLock(func() error {
		called = true
		return nil
	})
	if !filelock.IsLockErrorType(err, filelock.LockErrorFilesystem) || called {
		t.Fatalf("filesystem error retried or callback ran: err=%v callback=%v", err, called)
	}
}
