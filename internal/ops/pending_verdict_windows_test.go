//go:build windows

package ops

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
)

// Errors are injected at the I/O boundary: concurrent rename/remove collisions
// do not fail opens deterministically on every Windows runner. Concurrent
// envelope publication is exercised by TestLifecycleVerdictConcurrentReplay.
func TestRetrySharingViolationRetriesCollision(t *testing.T) {
	attempts := 0
	err := retrySharingViolation(func() error {
		attempts++
		if attempts == 1 {
			return &os.PathError{Op: "open", Path: "envelope.json", Err: syscall.Errno(32)}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retrySharingViolation() = %v, want nil", err)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
}

func TestRetrySharingViolationDoesNotRetryOtherErrors(t *testing.T) {
	for _, cause := range []error{os.ErrNotExist, syscall.Errno(5), errors.New("disk detached")} {
		t.Run(cause.Error(), func(t *testing.T) {
			original := &os.PathError{Op: "open", Path: "envelope.json", Err: cause}
			attempts := 0
			err := retrySharingViolation(func() error {
				attempts++
				return original
			})
			if err != original {
				t.Errorf("retrySharingViolation() = %v, want original %v", err, original)
			}
			if attempts != 1 {
				t.Errorf("attempts = %d, want 1", attempts)
			}
		})
	}
}

func TestRetrySharingViolationStopsAfterBudget(t *testing.T) {
	original := &os.PathError{Op: "open", Path: "envelope.json", Err: syscall.Errno(32)}
	attempts := 0
	start := time.Now()
	err := retrySharingViolation(func() error {
		attempts++
		return original
	})
	if elapsed := time.Since(start); elapsed < pendingVerdictRetryBudget {
		t.Errorf("gave up after %v, want at least %v", elapsed, pendingVerdictRetryBudget)
	}
	if err != original {
		t.Errorf("retrySharingViolation() = %v, want original %v", err, original)
	}
	if attempts < 2 {
		t.Errorf("attempts = %d, want a retry before giving up", attempts)
	}
}
