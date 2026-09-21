//go:build windows

package db

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func swapReadStateAttempt(t *testing.T, fn func(string) ([]byte, error)) {
	t.Helper()
	previous := readStateAttempt
	readStateAttempt = fn
	t.Cleanup(func() { readStateAttempt = previous })
}

func TestReadStateFileRetriesSharingCollision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.yaml")
	if err := os.WriteFile(path, []byte("published"), 0644); err != nil {
		t.Fatal(err)
	}
	// Inject the error at the I/O boundary: exclusive-handle sharing semantics
	// do not reliably make os.ReadFile fail on every Windows runner. Concurrent
	// publication is exercised separately by TestReadSnapshotConcurrentPublication.
	attempts := 0
	swapReadStateAttempt(t, func(path string) ([]byte, error) {
		attempts++
		if attempts == 1 {
			return nil, &os.PathError{Op: "open", Path: path, Err: syscall.Errno(32)}
		}
		return os.ReadFile(path)
	})
	data, err := readStateFile(path)
	if err != nil || string(data) != "published" {
		t.Fatalf("readStateFile() = %q, %v; want published, nil", data, err)
	}
	if attempts != 2 {
		t.Errorf("read attempts = %d, want 2", attempts)
	}
}

func TestReadStateFileDoesNotRetryOtherErrors(t *testing.T) {
	for _, cause := range []error{os.ErrNotExist, syscall.Errno(5), errors.New("disk detached")} {
		t.Run(cause.Error(), func(t *testing.T) {
			original := &os.PathError{Op: "open", Path: "state.yaml", Err: cause}
			attempts := 0
			swapReadStateAttempt(t, func(string) ([]byte, error) {
				attempts++
				return nil, original
			})
			if _, err := readStateFile("state.yaml"); err != original {
				t.Errorf("readStateFile() error = %v, want original %v", err, original)
			}
			if attempts != 1 {
				t.Errorf("read attempts = %d, want 1", attempts)
			}
		})
	}
}

func TestReadStateFileStopsAfterSharingRetryBudget(t *testing.T) {
	original := &os.PathError{Op: "open", Path: "state.yaml", Err: syscall.Errno(32)}
	attempts := 0
	swapReadStateAttempt(t, func(string) ([]byte, error) {
		attempts++
		return nil, original
	})
	start := time.Now()
	_, err := readStateFile("state.yaml")
	elapsed := time.Since(start)
	if err != original {
		t.Errorf("readStateFile() error = %v, want original %v", err, original)
	}
	if elapsed < publishRetryBudget {
		t.Errorf("gave up after %v, want at least %v", elapsed, publishRetryBudget)
	}
	if attempts < 2 {
		t.Errorf("read attempts = %d, want a retry before giving up", attempts)
	}
}
