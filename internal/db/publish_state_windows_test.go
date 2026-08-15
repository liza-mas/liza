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

// swapRenameState installs a publish primitive for the duration of a test.
func swapRenameState(t *testing.T, fn func(from, to string) error) {
	t.Helper()
	previous := renameState
	renameState = fn
	t.Cleanup(func() { renameState = previous })
}

// TestPublishStateRetriesWhileAReaderHoldsTheTarget drives the real platform
// behaviour: a genuine open handle blocks the first rename, and only the retry
// gets the state published. The reader is released from inside the primitive
// rather than on a timer, so the test proves the retry without waiting on the
// clock.
func TestPublishStateRetriesWhileAReaderHoldsTheTarget(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")
	tmpPath := filepath.Join(dir, "state.yaml.tmp")
	if err := os.WriteFile(statePath, []byte("original"), 0644); err != nil {
		t.Fatalf("seed state file: %v", err)
	}
	if err := os.WriteFile(tmpPath, []byte("published"), 0644); err != nil {
		t.Fatalf("seed temp file: %v", err)
	}

	reader, err := os.Open(statePath)
	if err != nil {
		t.Fatalf("hold the target open: %v", err)
	}
	readerOpen := true
	closeReader := func() {
		if readerOpen {
			reader.Close()
			readerOpen = false
		}
	}
	t.Cleanup(closeReader)

	attempts := 0
	swapRenameState(t, func(from, to string) error {
		attempts++
		err := os.Rename(from, to)
		if attempts == 1 {
			if err == nil {
				t.Error("first rename succeeded while a reader held the target; the platform behaviour this retry exists for no longer reproduces")
			}
			// Release the reader only once it has actually blocked a publish,
			// so a passing test cannot mean the collision never happened.
			closeReader()
		}
		return err
	})

	if err := publishState(tmpPath, statePath); err != nil {
		t.Fatalf("publishState() = %v, want nil", err)
	}
	if attempts != 2 {
		t.Errorf("rename attempts = %d, want 2", attempts)
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read published state: %v", err)
	}
	if string(data) != "published" {
		t.Errorf("published state = %q, want %q", data, "published")
	}
}

// TestPublishStateDoesNotRetryUnrelatedErrors keeps the retry narrow: only the
// code a blocked replace produces is worth another attempt.
func TestPublishStateDoesNotRetryUnrelatedErrors(t *testing.T) {
	attempts := 0
	sentinel := errors.New("disk detached")
	swapRenameState(t, func(from, to string) error {
		attempts++
		return sentinel
	})

	err := publishState("tmp", "state.yaml")
	if !errors.Is(err, sentinel) {
		t.Errorf("publishState() = %v, want %v", err, sentinel)
	}
	if attempts != 1 {
		t.Errorf("rename attempts = %d, want 1", attempts)
	}
}

// TestPublishStateGivesUpAndSurfacesTheOriginalError covers the case the retry
// cannot distinguish: a state file that is genuinely unwritable reports the
// same Windows error as a transient collision, so the budget is the only thing
// that ends it, and the caller must still see the real failure.
func TestPublishStateGivesUpAndSurfacesTheOriginalError(t *testing.T) {
	accessDenied := &os.LinkError{Op: "rename", Err: syscall.Errno(5)}
	attempts := 0
	swapRenameState(t, func(from, to string) error {
		attempts++
		return accessDenied
	})

	start := time.Now()
	err := publishState("tmp", "state.yaml")
	elapsed := time.Since(start)

	if !errors.Is(err, accessDenied.Err) {
		t.Errorf("publishState() = %v, want the original access-denied error", err)
	}
	if elapsed < publishRetryBudget {
		t.Errorf("gave up after %v, want at least the %v budget", elapsed, publishRetryBudget)
	}
	if attempts < 2 {
		t.Errorf("rename attempts = %d, want the publish retried before giving up", attempts)
	}
}
