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
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	// An exclusive handle makes the first real os.ReadFile fail with the
	// sharing violation observed when snapshot reads collide with publication.
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil,
		syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	closeHandle := func() {
		if handle != syscall.InvalidHandle {
			if err := syscall.CloseHandle(handle); err != nil {
				t.Error(err)
			}
			handle = syscall.InvalidHandle
		}
	}
	t.Cleanup(closeHandle)
	attempts := 0
	swapReadStateAttempt(t, func(path string) ([]byte, error) {
		attempts++
		data, err := os.ReadFile(path)
		if attempts == 1 {
			if !errors.Is(err, syscall.Errno(32)) {
				t.Errorf("first read error = %v, want sharing violation", err)
			}
			closeHandle()
		}
		return data, err
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
