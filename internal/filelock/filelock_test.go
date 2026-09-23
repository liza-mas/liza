package filelock

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestFileLockBasic(t *testing.T) {
	dir := t.TempDir()
	protectedPath := filepath.Join(dir, "data.yaml")

	fl := New(protectedPath)

	executed := false
	err := fl.WithLock(func() error {
		executed = true
		return nil
	})

	if err != nil {
		t.Fatalf("WithLock() error = %v", err)
	}
	if !executed {
		t.Error("function was not executed")
	}
}

func TestFileLockOperation(t *testing.T) {
	dir := t.TempDir()
	protectedPath := filepath.Join(dir, "data.yaml")

	fl := New(protectedPath)
	fl.EnableMetrics()

	err := fl.WithLockOperation("test-op", func() error {
		return nil
	})

	if err != nil {
		t.Fatalf("WithLockOperation() error = %v", err)
	}

	recorder := fl.GetMetricsRecorder()
	if recorder == nil {
		t.Fatal("metrics recorder is nil")
	}

	metrics := recorder.GetMetrics()
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	if metrics[0].Operation != "test-op" {
		t.Errorf("operation = %q, want %q", metrics[0].Operation, "test-op")
	}
}

func TestFileLockWithTimeout(t *testing.T) {
	dir := t.TempDir()
	protectedPath := filepath.Join(dir, "data.yaml")

	fl := New(protectedPath)
	customFL := fl.WithTimeout(5 * time.Second)

	if customFL.lockTimeout != 5*time.Second {
		t.Errorf("lockTimeout = %v, want %v", customFL.lockTimeout, 5*time.Second)
	}

	// Verify paths are preserved
	if customFL.lockPath != fl.lockPath {
		t.Errorf("lockPath = %q, want %q", customFL.lockPath, fl.lockPath)
	}
}

func TestFileLockConcurrent(t *testing.T) {
	dir := t.TempDir()
	protectedPath := filepath.Join(dir, "data.yaml")

	// This test proves that every contender is admitted, not that ten of them
	// queue within the production DefaultLockTimeout. Each hold writes owner
	// metadata through four file operations, and on the loaded Windows CI
	// runner seven of ten contenders exhausted 10s that way (DEV-783). The
	// generous timeout is a hang guard: WithLock returns once it is admitted.
	fl := New(protectedPath).WithTimeout(2 * time.Minute)

	// Write initial data
	if err := os.WriteFile(protectedPath, []byte("0"), 0644); err != nil {
		t.Fatal(err)
	}

	const numGoroutines = 10
	var counter atomic.Int64
	done := make(chan error, numGoroutines)

	for range numGoroutines {
		go func() {
			err := fl.WithLock(func() error {
				counter.Add(1)
				return nil
			})
			done <- err
		}()
	}

	for range numGoroutines {
		if err := <-done; err != nil {
			t.Errorf("WithLock() error = %v", err)
		}
	}

	if counter.Load() != numGoroutines {
		t.Errorf("counter = %d, want %d", counter.Load(), numGoroutines)
	}
}

func TestWithLockOperationContext_CancelsBlockedAcquisition(t *testing.T) {
	protectedPath := filepath.Join(t.TempDir(), "data.yaml")
	holderAcquired := make(chan struct{})
	releaseHolder := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- New(protectedPath).WithLockOperation("holder", func() error {
			close(holderAcquired)
			<-releaseHolder
			return nil
		})
	}()
	select {
	case <-holderAcquired:
	case <-time.After(2 * time.Second):
		t.Fatal("holder did not acquire lock")
	}

	ctx, cancel := context.WithCancel(context.Background())
	callbackRan := false
	waiterDone := make(chan error, 1)
	go func() {
		waiterDone <- New(protectedPath).WithLockOperationContext(ctx, "waiter", func() error {
			callbackRan = true
			return nil
		})
	}()
	cancel()

	select {
	case err := <-waiterDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("WithLockOperationContext() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("context cancellation did not interrupt lock acquisition")
	}
	if callbackRan {
		t.Fatal("lock callback ran after context cancellation")
	}

	close(releaseHolder)
	if err := <-holderDone; err != nil {
		t.Fatalf("holder error: %v", err)
	}
}

func TestSharedLocksRunConcurrentlyAndBlockExclusiveLock(t *testing.T) {
	protectedPath := filepath.Join(t.TempDir(), "data.yaml")
	sharedRelease := make(chan struct{})
	sharedAcquired := make(chan struct{}, 2)
	sharedDone := make(chan error, 2)

	for range 2 {
		go func() {
			sharedDone <- New(protectedPath).WithSharedLockOperation("shared", func() error {
				sharedAcquired <- struct{}{}
				<-sharedRelease
				return nil
			})
		}()
	}
	for range 2 {
		select {
		case <-sharedAcquired:
		case <-time.After(2 * time.Second):
			t.Fatal("shared lock was not acquired concurrently")
		}
	}

	exclusiveStarted := make(chan struct{})
	exclusiveAcquired := make(chan struct{})
	exclusiveDone := make(chan error, 1)
	go func() {
		close(exclusiveStarted)
		exclusiveDone <- New(protectedPath).WithLockOperation("exclusive", func() error {
			close(exclusiveAcquired)
			return nil
		})
	}()
	<-exclusiveStarted
	select {
	case <-exclusiveAcquired:
		t.Fatal("exclusive lock acquired while shared locks were held")
	case <-time.After(200 * time.Millisecond):
	}

	close(sharedRelease)
	for range 2 {
		if err := <-sharedDone; err != nil {
			t.Fatalf("WithSharedLockOperation() error = %v", err)
		}
	}
	select {
	case err := <-exclusiveDone:
		if err != nil {
			t.Fatalf("WithLockOperation() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("exclusive lock did not acquire after shared locks released")
	}
}

func TestFileLockCreatesLockFile(t *testing.T) {
	dir := t.TempDir()
	protectedPath := filepath.Join(dir, "data.yaml")

	fl := New(protectedPath)

	err := fl.WithLock(func() error {
		// Lock file should exist during operation
		lockPath := protectedPath + ".lock"
		if _, err := os.Stat(lockPath); os.IsNotExist(err) {
			t.Error("lock file does not exist during operation")
		}

		// Owner metadata is diagnostic and should be written best-effort.
		ownerPath := protectedPath + ".lock.owner.json"
		if _, err := os.Stat(ownerPath); os.IsNotExist(err) {
			t.Error("owner metadata file does not exist during operation")
		}

		return nil
	})

	if err != nil {
		t.Fatalf("WithLock() error = %v", err)
	}
}

func TestFileLockMetricsEnableDisable(t *testing.T) {
	dir := t.TempDir()
	protectedPath := filepath.Join(dir, "data.yaml")

	fl := New(protectedPath)

	// Initially no recorder
	if fl.GetMetricsRecorder() != nil {
		t.Error("expected nil recorder before enabling")
	}

	// Enable
	fl.EnableMetrics()
	if fl.GetMetricsRecorder() == nil {
		t.Error("expected non-nil recorder after enabling")
	}

	// Perform operation
	fl.WithLock(func() error { return nil })

	recorder := fl.GetMetricsRecorder()
	if len(recorder.GetMetrics()) != 1 {
		t.Errorf("expected 1 metric, got %d", len(recorder.GetMetrics()))
	}

	// Disable and perform operation
	fl.DisableMetrics()
	fl.WithLock(func() error { return nil })

	// Should still have only 1 metric
	if len(recorder.GetMetrics()) != 1 {
		t.Errorf("expected 1 metric after disable, got %d", len(recorder.GetMetrics()))
	}

	// Re-enable and perform operation
	fl.EnableMetrics()
	fl.WithLock(func() error { return nil })

	// Should now have 2 metrics
	if len(recorder.GetMetrics()) != 2 {
		t.Errorf("expected 2 metrics after re-enable, got %d", len(recorder.GetMetrics()))
	}
}

func TestFileLockPaths(t *testing.T) {
	fl := New("/tmp/test/state.yaml")

	if fl.lockPath != "/tmp/test/state.yaml.lock" {
		t.Errorf("lockPath = %q, want %q", fl.lockPath, "/tmp/test/state.yaml.lock")
	}
	if fl.ownerPath != "/tmp/test/state.yaml.lock.owner.json" {
		t.Errorf("ownerPath = %q, want %q", fl.ownerPath, "/tmp/test/state.yaml.lock.owner.json")
	}
}

func TestFileLockDefaults(t *testing.T) {
	fl := New("/tmp/test")

	if fl.lockTimeout != DefaultLockTimeout {
		t.Errorf("lockTimeout = %v, want %v", fl.lockTimeout, DefaultLockTimeout)
	}
}

func TestWithLockIgnoresLegacyPIDMetadataWhenLockFree(t *testing.T) {
	dir := t.TempDir()
	protectedPath := filepath.Join(dir, "data.yaml")

	fl := New(protectedPath)

	legacyPIDPath := protectedPath + ".lock.pid"
	if err := os.WriteFile(legacyPIDPath, []byte("99999999"), 0644); err != nil {
		t.Fatal(err)
	}

	executed := false
	err := fl.WithLockOperation("legacy-metadata", func() error {
		executed = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithLockOperation() error = %v", err)
	}
	if !executed {
		t.Fatal("function was not executed")
	}

	data, err := os.ReadFile(fl.ownerPath)
	if err != nil {
		t.Fatalf("owner metadata was not written: %v", err)
	}
	var metadata ownerMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("owner metadata is not JSON: %v", err)
	}
	if metadata.PID != os.Getpid() {
		t.Errorf("owner metadata PID = %d, want %d", metadata.PID, os.Getpid())
	}
	if metadata.Operation != "legacy-metadata" {
		t.Errorf("owner metadata operation = %q, want legacy-metadata", metadata.Operation)
	}
}

func TestWithLockTimesOutWhenHeldEvenWithDeadLegacyPIDMetadata(t *testing.T) {
	dir := t.TempDir()
	protectedPath := filepath.Join(dir, "data.yaml")

	// Create initial protected file
	if err := os.WriteFile(protectedPath, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create protected file: %v", err)
	}

	lock1 := New(protectedPath)
	lock1 = lock1.WithTimeout(5 * time.Second)

	// Acquire lock1
	lock1Acquired := make(chan struct{})
	lock1Done := make(chan struct{})
	releaseLock1 := make(chan struct{})
	go func() {
		_ = lock1.WithLock(func() error {
			close(lock1Acquired)
			<-releaseLock1
			return nil
		})
		close(lock1Done)
	}()

	// Wait for lock1 to be acquired
	select {
	case <-lock1Acquired:
		// Good, lock1 is held
	case <-time.After(2 * time.Second):
		t.Fatal("lock1 was not acquired")
	}

	legacyPIDPath := protectedPath + ".lock.pid"
	if err := os.WriteFile(legacyPIDPath, []byte("99999999"), 0644); err != nil {
		t.Fatal(err)
	}

	lock2 := New(protectedPath)
	lock2 = lock2.WithTimeout(100 * time.Millisecond)

	errChan := make(chan error, 1)
	go func() {
		err := lock2.WithLock(func() error {
			return nil
		})
		errChan <- err
	}()

	// Wait for lock2 to timeout
	select {
	case err := <-errChan:
		if err == nil {
			t.Fatal("lock2 should have timed out")
		}
		var lockErr *LockError
		if !errors.As(err, &lockErr) {
			t.Fatalf("error = %T %v, want *LockError", err, err)
		}
		if lockErr.Type != LockErrorTimeout {
			t.Fatalf("lock error type = %v, want %v", lockErr.Type, LockErrorTimeout)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("lock2 should have timed out quickly")
	}
	close(releaseLock1)

	// Wait for lock1 to release
	select {
	case <-lock1Done:
		// Good
	case <-time.After(3 * time.Second):
		t.Fatal("lock1 was not released")
	}

	legacyPID, err := os.ReadFile(legacyPIDPath)
	if err != nil {
		t.Fatalf("legacy PID metadata should remain diagnostic-only: %v", err)
	}
	if string(legacyPID) != "99999999" {
		t.Errorf("legacy PID metadata = %q, want unchanged", legacyPID)
	}
}

func TestOwnerMetadataWriteFailureDoesNotBlockLock(t *testing.T) {
	dir := t.TempDir()
	protectedPath := filepath.Join(dir, "data.yaml")

	fl := New(protectedPath)
	if err := os.Mkdir(fl.ownerPath, 0755); err != nil {
		t.Fatal(err)
	}

	executed := false
	err := fl.WithLock(func() error {
		executed = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithLock() error = %v", err)
	}
	if !executed {
		t.Error("function should run even when diagnostic metadata cannot be written")
	}
}
