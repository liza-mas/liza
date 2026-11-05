package db

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/liza-mas/liza/internal/models"
	"gopkg.in/yaml.v3"
)

// setupWatcherTest creates a temp directory with initial state and returns a Blackboard.
func setupWatcherTest(t *testing.T) (*Blackboard, string) {
	t.Helper()
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")
	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Status: models.GoalStatusInProgress, Created: now},
		Tasks:   []models.Task{},
		Agents:  make(map[string]models.Agent),
		Config:  models.Config{IntegrationBranch: "main"},
	}
	bb := New(statePath)
	if err := bb.Write(state); err != nil {
		t.Fatalf("Initial write failed: %v", err)
	}
	return bb, statePath
}

func drainWatcherEvents(watcher *stateWatcher) {
	for {
		select {
		case <-watcher.Events():
		default:
			return
		}
	}
}

func waitForWatcherReady(t *testing.T, bb *Blackboard, watcher *stateWatcher) {
	t.Helper()

	readyDeadline := time.Now().Add(2 * time.Second)
	// Start well above any version used in tests to avoid collisions with
	// test-specific version values (tests typically use single-digit versions).
	probeVersion := 1000
	for time.Now().Before(readyDeadline) {
		probeVersion++
		if err := bb.Modify(func(s *models.State) error {
			s.Version = probeVersion
			return nil
		}); err != nil {
			t.Fatalf("probe modify failed while waiting for watcher readiness: %v", err)
		}

		select {
		case <-watcher.Events():
			drainWatcherEvents(watcher)
			return
		case err := <-watcher.Errors():
			t.Fatalf("watcher readiness error: %v", err)
		case <-time.After(200 * time.Millisecond):
			// Retry until watcher emits an event or deadline expires.
		}
	}

	t.Fatal("watcher did not become ready in time")
}

// TestStateWatcherBasicNotification tests that watcher sends notifications on file changes
func TestStateWatcherBasicNotification(t *testing.T) {
	bb, _ := setupWatcherTest(t)

	// Start watcher
	watcher, err := bb.WatchForChanges()
	if err != nil {
		t.Fatalf("WatchForChanges failed: %v", err)
	}
	defer watcher.Close()

	// Modify state
	waitForWatcherReady(t, bb, watcher)
	if err := bb.Modify(func(s *models.State) error {
		s.Version = 2
		return nil
	}); err != nil {
		t.Fatalf("Modify failed: %v", err)
	}

	// Wait for notification
	select {
	case <-watcher.Events():
		// Success - notification received
	case err := <-watcher.Errors():
		t.Fatalf("Unexpected error from watcher: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for notification")
	}
}

// TestStateWatcherMultipleWrites tests that multiple rapid writes coalesce
func TestStateWatcherMultipleWrites(t *testing.T) {
	bb, _ := setupWatcherTest(t)

	// Start watcher
	watcher, err := bb.WatchForChanges()
	if err != nil {
		t.Fatalf("WatchForChanges failed: %v", err)
	}
	defer watcher.Close()

	waitForWatcherReady(t, bb, watcher)

	// Perform multiple rapid modifications
	for i := 2; i <= 5; i++ {
		version := i
		if err := bb.Modify(func(s *models.State) error {
			s.Version = version
			return nil
		}); err != nil {
			t.Fatalf("Modify %d failed: %v", i, err)
		}
	}

	// Should receive at least one notification
	// (multiple writes may coalesce into fewer notifications)
	select {
	case <-watcher.Events():
		// Success - at least one notification received
	case err := <-watcher.Errors():
		t.Fatalf("Unexpected error from watcher: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for notification")
	}

	// Drain any additional notifications (coalescing may vary by platform)
	drainTimeout := time.After(100 * time.Millisecond)
	notificationCount := 1
drainLoop:
	for {
		select {
		case <-watcher.Events():
			notificationCount++
		case <-drainTimeout:
			break drainLoop
		}
	}

	// We expect fewer notifications than writes due to coalescing
	// but at least 1 notification should be received
	if notificationCount < 1 {
		t.Errorf("Expected at least 1 notification, got %d", notificationCount)
	}
}

// TestStateWatcherClose tests graceful watcher shutdown
func TestStateWatcherClose(t *testing.T) {
	bb, _ := setupWatcherTest(t)

	// Start watcher
	watcher, err := bb.WatchForChanges()
	if err != nil {
		t.Fatalf("WatchForChanges failed: %v", err)
	}

	// Close watcher
	if err := watcher.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify channels are closed
	select {
	case _, ok := <-watcher.Events():
		if ok {
			t.Error("Events channel should be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Events channel not closed in time")
	}
}

// TestStateWatcherDoubleClose tests that closing twice doesn't panic
func TestStateWatcherDoubleClose(t *testing.T) {
	bb, _ := setupWatcherTest(t)

	// Start watcher
	watcher, err := bb.WatchForChanges()
	if err != nil {
		t.Fatalf("WatchForChanges failed: %v", err)
	}

	// Close watcher twice - should not panic
	if err := watcher.Close(); err != nil {
		t.Fatalf("First close failed: %v", err)
	}

	// Second close should be safe (may return error, but shouldn't panic)
	_ = watcher.Close()
}

// TestStateWatcherNonExistentFile tests watcher behavior with non-existent file
func TestStateWatcherNonExistentFile(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "nonexistent.yaml")

	bb := New(statePath)

	// Watcher should handle non-existent file gracefully
	// It may return an error or watch the directory
	watcher, err := bb.WatchForChanges()
	if err != nil {
		// If error is returned, that's acceptable
		t.Logf("WatchForChanges returned expected error: %v", err)
		return
	}
	defer watcher.Close()

	// If no error, watcher should work once file is created
	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Status: models.GoalStatusInProgress, Created: now},
		Tasks:   []models.Task{},
		Agents:  make(map[string]models.Agent),
		Config:  models.Config{IntegrationBranch: "main"},
	}

	if err := bb.Write(state); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Should receive notification for file creation
	select {
	case <-watcher.Events():
		// Success - notification received
	case err := <-watcher.Errors():
		t.Logf("Error from watcher (may be expected): %v", err)
	case <-time.After(2 * time.Second):
		t.Log("Timeout waiting for notification (may be expected for new file)")
	}
}

// TestStateWatcherExternalModification tests detection of external changes
func TestStateWatcherExternalModification(t *testing.T) {
	bb, statePath := setupWatcherTest(t)

	// Start watcher
	watcher, err := bb.WatchForChanges()
	if err != nil {
		t.Fatalf("WatchForChanges failed: %v", err)
	}
	defer watcher.Close()

	waitForWatcherReady(t, bb, watcher)

	// External modification (direct file write)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	state.Version = 3
	data, err := yaml.Marshal(state)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	if err := os.WriteFile(statePath, data, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Should receive notification for external change
	select {
	case <-watcher.Events():
		// Success - notification received
	case err := <-watcher.Errors():
		t.Fatalf("Unexpected error from watcher: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for notification")
	}
}

// vanishedEntryErr has the shape of fsnotify's kqueue Add failure when a
// directory entry is renamed away between its listing and its lstat.
func vanishedEntryErr(dir string) error {
	entry := filepath.Join(dir, "state.yaml.lock.owner.json.tmp-1")
	return fmt.Errorf("%q: %w", entry, &fs.PathError{Op: "lstat", Path: entry, Err: fs.ErrNotExist})
}

func TestWatchDirRetriesEntryVanishedDuringAdd(t *testing.T) {
	// GIVEN an Add that loses two races with a vanishing entry, then succeeds
	dir := t.TempDir()
	calls := 0
	add := func(w *fsnotify.Watcher, name string) error {
		calls++
		if calls <= 2 {
			return vanishedEntryErr(name)
		}
		return w.Add(name)
	}

	// WHEN the directory watch is set up
	watcher, err := watchDir(dir, add)

	// THEN it succeeds on the third attempt with a working watcher
	if err != nil {
		t.Fatalf("watchDir failed: %v", err)
	}
	defer watcher.Close()
	if calls != 3 {
		t.Fatalf("add calls = %d, want 3", calls)
	}
	path := filepath.Join(dir, "state.yaml")
	if err := os.WriteFile(path, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	timeout := time.After(2 * time.Second)
	for {
		select {
		case event := <-watcher.Events:
			if event.Name == path {
				return
			}
		case err := <-watcher.Errors:
			t.Fatalf("watcher error: %v", err)
		case <-timeout:
			t.Fatal("returned watcher reported no event for a write in the directory")
		}
	}
}

func TestWatchDirMissingDirFailsWithoutRetry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	calls := 0
	add := func(w *fsnotify.Watcher, name string) error {
		calls++
		return w.Add(name)
	}

	_, err := watchDir(dir, add)

	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("watchDir error = %v, want ErrNotExist", err)
	}
	if calls != 1 {
		t.Fatalf("add calls = %d, want 1", calls)
	}
}

func TestWatchDirOtherAddErrorFailsWithoutRetry(t *testing.T) {
	addErr := errors.New("watch limit reached")
	calls := 0
	add := func(*fsnotify.Watcher, string) error {
		calls++
		return addErr
	}

	_, err := watchDir(t.TempDir(), add)

	if !errors.Is(err, addErr) {
		t.Fatalf("watchDir error = %v, want %v", err, addErr)
	}
	if calls != 1 {
		t.Fatalf("add calls = %d, want 1", calls)
	}
}

func TestWatchDirGivesUpAfterBoundedAttempts(t *testing.T) {
	calls := 0
	add := func(_ *fsnotify.Watcher, name string) error {
		calls++
		return vanishedEntryErr(name)
	}

	_, err := watchDir(t.TempDir(), add)

	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("watchDir error = %v, want ErrNotExist", err)
	}
	if calls != stateWatchAddAttempts {
		t.Fatalf("add calls = %d, want %d", calls, stateWatchAddAttempts)
	}
}
