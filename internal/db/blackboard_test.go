package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func TestReadContext_CancelsBlockedLockAcquisition(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.yaml")
	if err := os.WriteFile(statePath, []byte("tasks: []\n"), 0644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	holder := New(statePath)
	holderAcquired := make(chan struct{})
	releaseHolder := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- holder.fileLock.WithLockOperation("holder", func() error {
			close(holderAcquired)
			<-releaseHolder
			return nil
		})
	}()
	select {
	case <-holderAcquired:
	case <-time.After(2 * time.Second):
		t.Fatal("holder did not acquire state lock")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(statePath).ReadContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadContext() error = %v, want context.Canceled", err)
	}

	close(releaseHolder)
	if err := <-holderDone; err != nil {
		t.Fatalf("holder error: %v", err)
	}
}

// assertRegularFileMode mirrors testhelpers.AssertRegularFileMode. It is
// duplicated here because internal/testhelpers imports internal/db
// (testhelpers/fixtures.go), and these tests are in package db, so importing
// testhelpers back would create an import cycle.
//
// Windows has no POSIX mode bits: os.Stat reports 0666 for a writable file and
// 0444 for one carrying the read-only attribute, so an exact comparison against
// unixPerm can never hold there; "not read-only" is the observable equivalent.
func assertRegularFileMode(t *testing.T, path string, unixPerm os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("stat %s: %v", path, err)
		return
	}
	if runtime.GOOS == "windows" {
		if info.Mode().Perm()&0o200 == 0 {
			t.Errorf("%s is read-only: mode=%v", path, info.Mode().Perm())
		}
		return
	}
	if info.Mode().Perm() != unixPerm {
		t.Errorf("%s has wrong permissions: got %o, want %o", path, info.Mode().Perm(), unixPerm)
	}
}

// TestFor verifies the process-level singleton behavior of For().
func TestFor(t *testing.T) {
	t.Cleanup(ResetInstances)

	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	// Same path returns same instance
	bb1 := For(statePath)
	bb2 := For(statePath)
	if bb1 != bb2 {
		t.Error("For() with same path returned different instances")
	}

	// Different path returns different instance
	otherPath := filepath.Join(dir, "other.yaml")
	bb3 := For(otherPath)
	if bb1 == bb3 {
		t.Error("For() with different paths returned same instance")
	}

	// Equivalent paths (trailing slash normalization) share instance
	bb4 := For(statePath + "/")
	if bb1 != bb4 {
		t.Error("For() with equivalent path (trailing slash) returned different instance")
	}

	// New() returns independent instance (not the singleton)
	bb5 := New(statePath)
	if bb1 == bb5 {
		t.Error("New() returned the singleton instance")
	}

	// ResetInstances clears the cache
	ResetInstances()
	bb6 := For(statePath)
	if bb1 == bb6 {
		t.Error("For() after ResetInstances returned stale singleton")
	}
}

func TestResetInstance(t *testing.T) {
	t.Cleanup(ResetInstances)

	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")
	otherPath := filepath.Join(dir, "other.yaml")
	stateInstance := For(statePath)
	otherInstance := For(otherPath)

	ResetInstance(statePath + "/")

	if refreshed := For(statePath); refreshed == stateInstance {
		t.Error("For() after ResetInstance returned the stale singleton")
	}
	if preserved := For(otherPath); preserved != otherInstance {
		t.Error("ResetInstance removed an independent path singleton")
	}
}

// TestBlackboardBasicReadWrite tests basic read and write operations
func TestBlackboardBasicReadWrite(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	// Create a test state
	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Created:     now,
			Status:      models.GoalStatusInProgress,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Test task",
				Status:      models.TaskStatusReady,
				Priority:    1,
				SpecRef:     "spec.md",
				DoneWhen:    "Task is done",
				Created:     now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{
			MaxCoderIterations: 5,
			MaxReviewCycles:    3,
			HeartbeatInterval:  30,
			LeaseDuration:      300,
			CoderPollInterval:  10,
			DoerMaxWait:        600,
			IntegrationBranch:  "main",
		},
	}

	// Write the state
	bb := New(statePath)
	if err := bb.Write(state); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Read it back
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	// Verify basic fields
	if readState.Version != state.Version {
		t.Errorf("Version mismatch: got %d, want %d", readState.Version, state.Version)
	}
	if readState.Goal.ID != state.Goal.ID {
		t.Errorf("Goal ID mismatch: got %s, want %s", readState.Goal.ID, state.Goal.ID)
	}
	if len(readState.Tasks) != len(state.Tasks) {
		t.Fatalf("Task count mismatch: got %d, want %d", len(readState.Tasks), len(state.Tasks))
	}
	if readState.Tasks[0].ID != state.Tasks[0].ID {
		t.Errorf("Task ID mismatch: got %s, want %s", readState.Tasks[0].ID, state.Tasks[0].ID)
	}
}

func TestBlackboardWriteRejectsRawProviderTranscriptPayload(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:      "goal-1",
			Status:  models.GoalStatusInProgress,
			Created: time.Now().UTC(),
		},
		Agents: map[string]models.Agent{},
		Config: models.Config{IntegrationBranch: "main"},
		Anomalies: []models.Anomaly{
			{
				Type: "provider_audit_degraded",
				Details: map[string]any{
					"provider": "codex",
					"agent_id": "orchestrator-1",
					"message":  `{"type":"item.completed","item":{"type":"command_execution","aggregated_output":"raw output"}}`,
				},
			},
		},
	}

	err := New(statePath).Write(state)
	if err == nil {
		t.Fatal("Write() error = nil, want state hygiene error")
	}
	if !strings.Contains(err.Error(), "state hygiene validation failed") {
		t.Fatalf("Write() error = %v, want state hygiene error", err)
	}
}

// TestBlackboardReadRaw tests that ReadRaw returns exact file bytes under lock
func TestBlackboardReadRaw(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	// Write known content directly
	content := []byte("version: 1\ngoal:\n  id: goal-1\n")
	if err := os.WriteFile(statePath, content, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	bb := New(statePath)
	data, err := bb.ReadRaw()
	if err != nil {
		t.Fatalf("ReadRaw failed: %v", err)
	}

	if string(data) != string(content) {
		t.Errorf("ReadRaw returned different content:\ngot:  %q\nwant: %q", string(data), string(content))
	}
}

// TestBlackboardReadRawMissingFile tests ReadRaw on non-existent file
func TestBlackboardReadRawMissingFile(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "nonexistent.yaml")

	bb := New(statePath)
	_, err := bb.ReadRaw()
	if err == nil {
		t.Fatal("Expected error for missing file, got nil")
	}
}

// TestBlackboardGetTask tests retrieving a specific task
func TestBlackboardGetTask(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:      "goal-1",
			Status:  models.GoalStatusInProgress,
			Created: now,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "First task",
				Status:      models.TaskStatusReady,
				Priority:    1,
				Created:     now,
			},
			{
				ID:          "task-2",
				Description: "Second task",
				Status:      models.TaskStatusImplementing,
				Priority:    2,
				Created:     now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	bb := New(statePath)
	if err := bb.Write(state); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Get existing task
	task, err := bb.GetTask("task-2")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if task == nil {
		t.Fatal("GetTask returned nil for existing task")
	}
	if task.ID != "task-2" {
		t.Errorf("Task ID mismatch: got %s, want task-2", task.ID)
	}
	if task.Description != "Second task" {
		t.Errorf("Task description mismatch: got %s, want 'Second task'", task.Description)
	}

	// Get non-existent task
	task, err = bb.GetTask("task-999")
	if err != nil {
		t.Fatalf("GetTask failed for non-existent task: %v", err)
	}
	if task != nil {
		t.Error("GetTask should return nil for non-existent task")
	}
}

// TestBlackboardGetAgent tests retrieving a specific agent
func TestBlackboardGetAgent(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Status: models.GoalStatusInProgress, Created: now},
		Tasks:   []models.Task{},
		Agents: map[string]models.Agent{
			"agent-1": {
				Role:      "coder",
				Status:    models.AgentStatusIdle,
				Heartbeat: now,
				Terminal:  "term-1",
			},
			"agent-2": {
				Role:      "code-reviewer",
				Status:    models.AgentStatusWorking,
				Heartbeat: now,
				Terminal:  "term-2",
			},
		},
		Config: models.Config{IntegrationBranch: "main"},
	}

	bb := New(statePath)
	if err := bb.Write(state); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Get existing agent
	agent, err := bb.GetAgent("agent-2")
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	if agent == nil {
		t.Fatal("GetAgent returned nil for existing agent")
	}
	if agent.Role != "code-reviewer" {
		t.Errorf("Agent role mismatch: got %s, want code-reviewer", agent.Role)
	}

	// Get non-existent agent
	agent, err = bb.GetAgent("agent-999")
	if err != nil {
		t.Fatalf("GetAgent failed for non-existent agent: %v", err)
	}
	if agent != nil {
		t.Error("GetAgent should return nil for non-existent agent")
	}
}

// TestBlackboardModify tests atomic read-modify-write operations
func TestBlackboardModify(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Status: models.GoalStatusInProgress, Created: now},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Original description",
				Status:      models.TaskStatusReady,
				Priority:    1,
				Created:     now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	bb := New(statePath)
	if err := bb.Write(state); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Modify the task
	err := bb.Modify(func(s *models.State) error {
		if len(s.Tasks) > 0 {
			s.Tasks[0].Description = "Modified description"
			s.Tasks[0].Status = models.TaskStatusImplementing
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Modify failed: %v", err)
	}

	// Read back and verify
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if len(readState.Tasks) == 0 {
		t.Fatal("No tasks found after modify")
	}
	if readState.Tasks[0].Description != "Modified description" {
		t.Errorf("Description not modified: got %s, want 'Modified description'", readState.Tasks[0].Description)
	}
	if readState.Tasks[0].Status != models.TaskStatusImplementing {
		t.Errorf("Status not modified: got %s, want %s", readState.Tasks[0].Status, models.TaskStatusImplementing)
	}
}

// TestBlackboardUpdateTask tests updating a task
func TestBlackboardUpdateTask(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Status: models.GoalStatusInProgress, Created: now},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Task 1",
				Status:      models.TaskStatusReady,
				Priority:    1,
				Created:     now,
			},
			{
				ID:          "task-2",
				Description: "Task 2",
				Status:      models.TaskStatusReady,
				Priority:    2,
				Created:     now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	bb := New(statePath)
	if err := bb.Write(state); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Update task-2
	assignedTo := "agent-1"
	err := bb.UpdateTask("task-2", func(task *models.Task) error {
		task.Status = models.TaskStatusImplementing
		task.AssignedTo = &assignedTo
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateTask failed: %v", err)
	}

	// Verify the update
	task, err := bb.GetTask("task-2")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if task.Status != models.TaskStatusImplementing {
		t.Errorf("Task status not updated: got %s, want %s", task.Status, models.TaskStatusImplementing)
	}
	if task.AssignedTo == nil || *task.AssignedTo != "agent-1" {
		t.Errorf("Task assignedTo not updated correctly")
	}

	// Verify task-1 was not modified
	task1, err := bb.GetTask("task-1")
	if err != nil {
		t.Fatalf("GetTask task-1 failed: %v", err)
	}
	if task1.Status != models.TaskStatusReady {
		t.Errorf("Task-1 status should be unchanged, got %s", task1.Status)
	}
}

// TestBlackboardLockTimeout tests that lock acquisition times out appropriately
func TestBlackboardLockTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping timeout test in short mode")
	}

	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	// Create initial state
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
		t.Fatalf("Write failed: %v", err)
	}

	// Use short timeout for faster testing
	testTimeout := 1 * time.Second
	bb1 := New(statePath).WithLockTimeout(testTimeout)
	bb2 := New(statePath).WithLockTimeout(testTimeout)

	// Acquire lock with bb1 and hold it longer than timeout
	locked := make(chan struct{})
	done := make(chan struct{})
	releaseLock := make(chan struct{})

	go func() {
		err := bb1.Modify(func(s *models.State) error {
			close(locked)
			<-releaseLock
			return nil
		})
		if err != nil {
			t.Errorf("bb1 Modify failed: %v", err)
		}
		close(done)
	}()

	// Wait for lock to be acquired
	<-locked

	// Try to read with bb2 - should timeout
	start := time.Now()
	_, err := bb2.Read()
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
	if elapsed < 900*time.Millisecond || elapsed > 1200*time.Millisecond {
		t.Errorf("Timeout duration unexpected: %v (expected ~1s)", elapsed)
	}
	close(releaseLock)

	// Wait for first goroutine to finish
	<-done
}

// TestBlackboardFileNotExists tests behavior when state file doesn't exist
func TestBlackboardFileNotExists(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "nonexistent.yaml")

	bb := New(statePath)
	_, err := bb.Read()
	if err == nil {
		t.Error("Expected error reading non-existent file, got nil")
	}
	if !os.IsNotExist(err) {
		t.Errorf("Expected IsNotExist error, got: %v", err)
	}
}

// TestBlackboardUpdateAgent tests updating an agent
func TestBlackboardUpdateAgent(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Status: models.GoalStatusInProgress, Created: now},
		Tasks:   []models.Task{},
		Agents: map[string]models.Agent{
			"agent-1": {
				Role:      "coder",
				Status:    models.AgentStatusIdle,
				Heartbeat: now,
				Terminal:  "term-1",
			},
		},
		Config: models.Config{IntegrationBranch: "main"},
	}

	bb := New(statePath)
	if err := bb.Write(state); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Update agent
	taskID := "task-1"
	err := bb.UpdateAgent("agent-1", func(agent *models.Agent) error {
		agent.Status = models.AgentStatusWorking
		agent.CurrentTask = &taskID
		agent.IterationsTotal = 1
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	// Verify the update
	agent, err := bb.GetAgent("agent-1")
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	if agent.Status != models.AgentStatusWorking {
		t.Errorf("Agent status not updated: got %s, want %s", agent.Status, models.AgentStatusWorking)
	}
	if agent.CurrentTask == nil || *agent.CurrentTask != "task-1" {
		t.Error("Agent CurrentTask not updated correctly")
	}
	if agent.IterationsTotal != 1 {
		t.Errorf("Agent IterationsTotal not updated: got %d, want 1", agent.IterationsTotal)
	}
}

// TestBlackboardUpdateAgentNotFound tests updating a non-existent agent
func TestBlackboardUpdateAgentNotFound(t *testing.T) {
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
		t.Fatalf("Write failed: %v", err)
	}

	// Try to update non-existent agent
	err := bb.UpdateAgent("agent-999", func(agent *models.Agent) error {
		agent.Status = models.AgentStatusWorking
		return nil
	})
	if err == nil {
		t.Error("Expected error updating non-existent agent, got nil")
	}
}

// TestBlackboardUpdateTaskNotFound tests updating a non-existent task
func TestBlackboardUpdateTaskNotFound(t *testing.T) {
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
		t.Fatalf("Write failed: %v", err)
	}

	// Try to update non-existent task
	err := bb.UpdateTask("task-999", func(task *models.Task) error {
		task.Status = models.TaskStatusImplementing
		return nil
	})
	if err == nil {
		t.Error("Expected error updating non-existent task, got nil")
	}
}

// TestBlackboardModifyError tests error handling in Modify
func TestBlackboardModifyError(t *testing.T) {
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
		t.Fatalf("Write failed: %v", err)
	}

	// Test modification function returning error
	testErr := fmt.Errorf("modification failed")
	err := bb.Modify(func(s *models.State) error {
		return testErr
	})
	if err == nil {
		t.Error("Expected error from modification function, got nil")
	}

	// Verify state was not modified
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if readState.Version != 1 {
		t.Error("State should not have been modified")
	}
}

// TestBlackboardModifyFileNotFound tests Modify with non-existent file
func TestBlackboardModifyFileNotFound(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "nonexistent.yaml")

	bb := New(statePath)
	err := bb.Modify(func(s *models.State) error {
		return nil
	})
	if err == nil {
		t.Error("Expected error modifying non-existent file, got nil")
	}
}

// TestBlackboardGetStatePath tests GetStatePath
func TestBlackboardGetStatePath(t *testing.T) {
	statePath := "/path/to/state.yaml"
	bb := New(statePath)

	got := bb.GetStatePath()
	if got != statePath {
		t.Errorf("GetStatePath() = %s, want %s", got, statePath)
	}
}

// TestBlackboardConcurrentReads tests multiple concurrent reads
func TestBlackboardConcurrentReads(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrency test in short mode")
	}

	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Status: models.GoalStatusInProgress, Created: now},
		Tasks: []models.Task{
			{ID: "task-1", Description: "Test", Status: models.TaskStatusReady, Priority: 1, Created: now},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	bb := New(statePath)
	if err := bb.Write(state); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Spawn multiple goroutines to read concurrently
	const numReaders = 10
	done := make(chan bool, numReaders)
	errors := make(chan error, numReaders)

	for range numReaders {
		go func() {
			s, err := bb.Read()
			if err != nil {
				errors <- err
				return
			}
			if s.Version != 1 {
				errors <- fmt.Errorf("unexpected version: %d", s.Version)
				return
			}
			done <- true
		}()
	}

	// Wait for all readers to complete
	for range numReaders {
		select {
		case <-done:
			// Success
		case err := <-errors:
			t.Errorf("Reader error: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for concurrent reads")
		}
	}
}

// TestBlackboardConcurrentModifications tests multiple concurrent modifications
func TestBlackboardConcurrentModifications(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrency test in short mode")
	}

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
		t.Fatalf("Write failed: %v", err)
	}

	// Spawn multiple goroutines to modify concurrently
	const numWriters = 10
	done := make(chan bool, numWriters)
	errors := make(chan error, numWriters)

	for i := range numWriters {
		taskID := fmt.Sprintf("task-%d", i)
		go func(id string) {
			err := bb.Modify(func(s *models.State) error {
				s.Tasks = append(s.Tasks, models.Task{
					ID:          id,
					Description: "Test task",
					Status:      models.TaskStatusReady,
					Priority:    1,
					Created:     now,
				})
				return nil
			})
			if err != nil {
				errors <- err
				return
			}
			done <- true
		}(taskID)
	}

	// Wait for all writers to complete
	for range numWriters {
		select {
		case <-done:
			// Success
		case err := <-errors:
			t.Errorf("Writer error: %v", err)
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for concurrent modifications")
		}
	}

	// Verify all tasks were added
	finalState, err := bb.Read()
	if err != nil {
		t.Fatalf("Final read failed: %v", err)
	}
	if len(finalState.Tasks) != numWriters {
		t.Errorf("Expected %d tasks, got %d", numWriters, len(finalState.Tasks))
	}
}

// TestBlackboardWriteReadOnlyDir tests Write error when directory is read-only
func TestBlackboardWriteReadOnlyDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping test when running as root")
	}

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

	// First write should succeed
	if err := bb.Write(state); err != nil {
		t.Fatalf("Initial write failed: %v", err)
	}

	// Make directory read-only
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatalf("Failed to make directory read-only: %v", err)
	}
	defer os.Chmod(dir, 0755) // Restore permissions for cleanup

	// Use shorter timeout for error case since we expect immediate failure
	bbShortTimeout := bb.WithLockTimeout(500 * time.Millisecond)

	// Try to write again - should fail
	err := bbShortTimeout.Write(state)
	if err == nil {
		t.Error("Expected error writing to read-only directory, got nil")
	}
}

// TestBlackboardOwnerMetadataTracking tests that diagnostic owner metadata is written.
func TestBlackboardOwnerMetadataTracking(t *testing.T) {
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
		t.Fatalf("Write failed: %v", err)
	}

	ownerPath := statePath + ".lock.owner.json"

	// Verify owner metadata exists during lock hold.
	lockHeld := make(chan bool)
	done := make(chan bool)
	finished := make(chan error, 1)

	go func() {
		err := bb.Modify(func(s *models.State) error {
			lockHeld <- true
			// Wait for main goroutine to finish checking
			<-done
			return nil
		})
		finished <- err
	}()

	// Wait for lock to be acquired
	<-lockHeld

	// Owner metadata should exist while lock is held.
	if _, err := os.Stat(ownerPath); err != nil {
		t.Errorf("owner metadata should exist while lock is held, got error: %v", err)
	}

	// Read owner metadata and verify it records the current process.
	metadataData, err := os.ReadFile(ownerPath)
	if err != nil {
		t.Fatalf("Failed to read owner metadata: %v", err)
	}
	var metadata struct {
		PID       int    `json:"pid"`
		Operation string `json:"operation"`
	}
	if err := json.Unmarshal(metadataData, &metadata); err != nil {
		t.Fatalf("Failed to parse owner metadata: %v", err)
	}
	if metadata.PID != os.Getpid() {
		t.Errorf("owner PID mismatch: got %d, want %d", metadata.PID, os.Getpid())
	}
	if metadata.Operation != "modify" {
		t.Errorf("owner operation = %q, want modify", metadata.Operation)
	}

	// Signal goroutine to finish and wait for it
	done <- true
	if goroutineErr := <-finished; goroutineErr != nil {
		t.Errorf("Modify failed: %v", goroutineErr)
	}
}

// TestBlackboardLegacyPIDMetadataDoesNotControlAcquisition verifies leftover
// legacy PID metadata is diagnostic-only when the flock itself is free.
func TestBlackboardLegacyPIDMetadataDoesNotControlAcquisition(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping legacy lock metadata test in short mode")
	}

	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")
	lockPath := statePath + ".lock"
	pidPath := statePath + ".lock.pid"
	ownerPath := statePath + ".lock.owner.json"

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

	if err := os.WriteFile(lockPath, []byte{}, 0644); err != nil {
		t.Fatalf("Failed to create lock file: %v", err)
	}
	if err := os.WriteFile(pidPath, []byte("999999"), 0644); err != nil {
		t.Fatalf("Failed to create PID file: %v", err)
	}

	// Try to acquire with short timeout. Since the flock is free, acquisition
	// succeeds and writes fresh owner metadata without interpreting .lock.pid.
	bb2 := New(statePath).WithLockTimeout(2 * time.Second)
	err := bb2.Modify(func(s *models.State) error {
		s.Version = 2
		return nil
	})

	if err != nil {
		t.Fatalf("Should successfully acquire free lock despite legacy PID metadata, got error: %v", err)
	}

	// Verify state was modified (lock was acquired)
	readState, err := bb2.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if readState.Version != 2 {
		t.Errorf("Version should be 2 after modification, got %d", readState.Version)
	}

	legacyPID, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("legacy PID metadata should remain available for diagnostics: %v", err)
	}
	if string(legacyPID) != "999999" {
		t.Errorf("legacy PID metadata = %q, want unchanged", legacyPID)
	}

	metadataData, err := os.ReadFile(ownerPath)
	if err != nil {
		t.Fatalf("owner metadata was not written: %v", err)
	}
	var metadata struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(metadataData, &metadata); err != nil {
		t.Fatalf("owner metadata is invalid JSON: %v", err)
	}
	if metadata.PID != os.Getpid() {
		t.Errorf("owner metadata PID = %d, want %d", metadata.PID, os.Getpid())
	}
}

// TestBlackboardLiveLockNotCleaned tests that live process locks are NOT cleaned
func TestBlackboardLiveLockNotCleaned(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping live lock test in short mode")
	}

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

	bb1 := New(statePath)
	if err := bb1.Write(state); err != nil {
		t.Fatalf("Initial write failed: %v", err)
	}

	// bb1 holds lock for 2 seconds
	lockHeld := make(chan struct{})
	done := make(chan error)
	releaseLock := make(chan struct{})
	go func() {
		err := bb1.Modify(func(s *models.State) error {
			close(lockHeld)
			<-releaseLock
			return nil
		})
		done <- err
	}()

	<-lockHeld // Wait for lock to be held

	// bb2 tries to acquire with short timeout - should fail because lock is live
	bb2 := New(statePath).WithLockTimeout(1 * time.Second)
	_, err := bb2.Read()

	if err == nil {
		t.Error("Should fail to acquire lock held by live process")
	}

	if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "lock") {
		t.Errorf("Error should mention timeout or lock, got: %v", err)
	}
	close(releaseLock)

	// Wait for goroutine to complete
	if goroutineErr := <-done; goroutineErr != nil {
		t.Errorf("bb1 Modify failed: %v", goroutineErr)
	}
}

// TestBlackboardOwnerMetadataConsistency tests that owner metadata tracks the lock holder.
func TestBlackboardOwnerMetadataConsistency(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")
	ownerPath := statePath + ".lock.owner.json"

	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Status: models.GoalStatusInProgress, Created: now},
		Tasks:   []models.Task{},
		Agents:  make(map[string]models.Agent),
		Config:  models.Config{IntegrationBranch: "main"},
	}

	bb := New(statePath)

	// Multiple operations update owner metadata with the current PID.
	for i := range 3 {
		if err := bb.Write(state); err != nil {
			t.Fatalf("Write %d failed: %v", i, err)
		}

		// Owner metadata should exist with current process PID.
		metadataData, err := os.ReadFile(ownerPath)
		if err != nil {
			t.Fatalf("Iteration %d: owner metadata should exist after operation: %v", i, err)
		}
		var metadata struct {
			PID int `json:"pid"`
		}
		if err := json.Unmarshal(metadataData, &metadata); err != nil {
			t.Fatalf("Iteration %d: invalid owner metadata: %v", i, err)
		}
		if metadata.PID != os.Getpid() {
			t.Errorf("Iteration %d: owner PID mismatch: got %d, want %d", i, metadata.PID, os.Getpid())
		}
	}
}

// TestBlackboardWriteWithFsync tests that Write uses fsync for durability
func TestBlackboardWriteWithFsync(t *testing.T) {
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

	// Write state
	if err := bb.Write(state); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Verify state file exists and has correct permissions
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("State file not found: %v", err)
	}
	assertRegularFileMode(t, statePath, 0644)

	// Verify temp file was cleaned up
	tmpPath := statePath + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("Temporary file should be cleaned up")
	}

	// Verify we can read back the state
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if readState.Version != state.Version {
		t.Errorf("Version mismatch: got %d, want %d", readState.Version, state.Version)
	}
}

// TestBlackboardModifyWithFsync tests that Modify uses fsync for durability
func TestBlackboardModifyWithFsync(t *testing.T) {
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

	// Initial write
	if err := bb.Write(state); err != nil {
		t.Fatalf("Initial write failed: %v", err)
	}

	// Modify state
	if err := bb.Modify(func(s *models.State) error {
		s.Version = 2
		return nil
	}); err != nil {
		t.Fatalf("Modify failed: %v", err)
	}

	// Verify temp file was cleaned up
	tmpPath := statePath + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("Temporary file should be cleaned up after modify")
	}

	// Verify modification persisted
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Read after modify failed: %v", err)
	}
	if readState.Version != 2 {
		t.Errorf("Version should be 2 after modify, got %d", readState.Version)
	}
}

// TestBlackboardAtomicWriteOnError tests cleanup when write fails
func TestBlackboardAtomicWriteOnError(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	now := time.Now().UTC()
	initialState := &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Status: models.GoalStatusInProgress, Created: now},
		Tasks:   []models.Task{},
		Agents:  make(map[string]models.Agent),
		Config:  models.Config{IntegrationBranch: "main"},
	}

	bb := New(statePath)

	// Write initial state
	if err := bb.Write(initialState); err != nil {
		t.Fatalf("Initial write failed: %v", err)
	}

	// Make directory read-only to force write error
	if os.Getuid() != 0 {
		if err := os.Chmod(dir, 0555); err != nil {
			t.Fatalf("Failed to make directory read-only: %v", err)
		}
		defer os.Chmod(dir, 0755)

		// Use shorter timeout for error case since we expect immediate failure
		bbShortTimeout := bb.WithLockTimeout(500 * time.Millisecond)

		// Try to modify - should fail
		err := bbShortTimeout.Modify(func(s *models.State) error {
			s.Version = 2
			return nil
		})
		if err == nil {
			t.Error("Expected error when writing to read-only directory")
		}
	}

	// Restore permissions and verify original state is intact
	os.Chmod(dir, 0755)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Read after failed modify failed: %v", err)
	}
	if readState.Version != 1 {
		t.Errorf("Version should still be 1 after failed modify, got %d", readState.Version)
	}

	// Verify no temp files left behind
	tmpPath := statePath + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("Temporary file should be cleaned up even on error")
	}
}

// BenchmarkBlackboardWrite benchmarks write performance with fsync
func BenchmarkBlackboardWrite(b *testing.B) {
	dir := b.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Status: models.GoalStatusInProgress, Created: now},
		Tasks: []models.Task{
			{ID: "task-1", Status: "pending"},
			{ID: "task-2", Status: "pending"},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	bb := New(statePath)

	b.ResetTimer()
	for i := range b.N {
		state.Version = i + 1
		if err := bb.Write(state); err != nil {
			b.Fatalf("Write failed: %v", err)
		}
	}
}

// BenchmarkBlackboardModify benchmarks modify performance with fsync
func BenchmarkBlackboardModify(b *testing.B) {
	dir := b.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Status: models.GoalStatusInProgress, Created: now},
		Tasks: []models.Task{
			{ID: "task-1", Status: "pending"},
			{ID: "task-2", Status: "pending"},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	bb := New(statePath)

	// Initial write
	if err := bb.Write(state); err != nil {
		b.Fatalf("Initial write failed: %v", err)
	}

	b.ResetTimer()
	for i := range b.N {
		if err := bb.Modify(func(s *models.State) error {
			s.Version = i + 1
			return nil
		}); err != nil {
			b.Fatalf("Modify failed: %v", err)
		}
	}
}

// TestBlackboardReadCachedHit tests that cached reads use cache
func TestBlackboardReadCachedHit(t *testing.T) {
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
		t.Fatalf("Write failed: %v", err)
	}

	// First read - should populate cache
	readState1, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("First ReadCached failed: %v", err)
	}
	if readState1.Version != 1 {
		t.Errorf("First read version: got %d, want 1", readState1.Version)
	}

	// Second read - should use cache (same mtime)
	readState2, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("Second ReadCached failed: %v", err)
	}
	if readState2.Version != 1 {
		t.Errorf("Second read version: got %d, want 1", readState2.Version)
	}

	// Verify both reads return equivalent data
	if readState1.Goal.ID != readState2.Goal.ID {
		t.Error("Cached read returned different data")
	}
}

// TestBlackboardReadCachedInvalidation tests cache invalidation on mtime change
func TestBlackboardReadCachedInvalidation(t *testing.T) {
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
		t.Fatalf("Write failed: %v", err)
	}

	// First read - populates cache
	readState1, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("First ReadCached failed: %v", err)
	}
	if readState1.Version != 1 {
		t.Errorf("First read version: got %d, want 1", readState1.Version)
	}

	// Wait to ensure mtime changes
	time.Sleep(10 * time.Millisecond)

	// Modify state - should invalidate cache
	if err := bb.Modify(func(s *models.State) error {
		s.Version = 2
		return nil
	}); err != nil {
		t.Fatalf("Modify failed: %v", err)
	}

	// Read again - should detect mtime change and reload
	readState2, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("Second ReadCached failed: %v", err)
	}
	if readState2.Version != 2 {
		t.Errorf("After modify version: got %d, want 2", readState2.Version)
	}
}

// TestBlackboardInvalidateCache tests explicit cache invalidation
func TestBlackboardInvalidateCache(t *testing.T) {
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
		t.Fatalf("Write failed: %v", err)
	}

	// Read to populate cache
	_, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("ReadCached failed: %v", err)
	}

	// Explicitly invalidate cache
	bb.InvalidateCache()

	// Next read should reload from disk (we can't directly observe this,
	// but we verify it still works correctly)
	readState, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("ReadCached after invalidation failed: %v", err)
	}
	if readState.Version != 1 {
		t.Errorf("Version after invalidation: got %d, want 1", readState.Version)
	}
}

// TestBlackboardConcurrentCachedReads tests concurrent cached reads
func TestBlackboardConcurrentCachedReads(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Status: models.GoalStatusInProgress, Created: now},
		Tasks:   []models.Task{{ID: "task-1", Status: "pending"}},
		Agents:  make(map[string]models.Agent),
		Config:  models.Config{IntegrationBranch: "main"},
	}

	bb := New(statePath)
	if err := bb.Write(state); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Concurrent reads from multiple goroutines
	const numReaders = 10
	done := make(chan error, numReaders)

	for range numReaders {
		go func() {
			for range 5 {
				s, err := bb.ReadCached()
				if err != nil {
					done <- err
					return
				}
				if s.Version != 1 {
					done <- fmt.Errorf("wrong version: got %d, want 1", s.Version)
					return
				}
			}
			done <- nil
		}()
	}

	// Wait for all readers to complete
	for range numReaders {
		if err := <-done; err != nil {
			t.Errorf("Concurrent read error: %v", err)
		}
	}
}

// TestBlackboardCacheWithExternalModification tests cache detects external changes
func TestBlackboardCacheWithExternalModification(t *testing.T) {
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
		t.Fatalf("Write failed: %v", err)
	}

	// Read to populate cache
	readState1, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("First ReadCached failed: %v", err)
	}
	if readState1.Version != 1 {
		t.Errorf("First read version: got %d, want 1", readState1.Version)
	}

	// Wait to ensure mtime changes
	time.Sleep(10 * time.Millisecond)

	// External modification (simulated by another Blackboard instance)
	bb2 := New(statePath)
	if err := bb2.Modify(func(s *models.State) error {
		s.Version = 3
		return nil
	}); err != nil {
		t.Fatalf("External modify failed: %v", err)
	}

	// Original bb should detect mtime change and reload
	readState2, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("ReadCached after external change failed: %v", err)
	}
	if readState2.Version != 3 {
		t.Errorf("After external modification version: got %d, want 3", readState2.Version)
	}
}

// BenchmarkBlackboardReadCached benchmarks cached read performance
func BenchmarkBlackboardReadCached(b *testing.B) {
	dir := b.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Status: models.GoalStatusInProgress, Created: now},
		Tasks: []models.Task{
			{ID: "task-1", Status: "pending"},
			{ID: "task-2", Status: "pending"},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	bb := New(statePath)
	if err := bb.Write(state); err != nil {
		b.Fatalf("Write failed: %v", err)
	}

	b.ResetTimer()
	for range b.N {
		_, err := bb.ReadCached()
		if err != nil {
			b.Fatalf("ReadCached failed: %v", err)
		}
	}
}

// BenchmarkBlackboardReadUncached benchmarks non-cached read performance
func BenchmarkBlackboardReadUncached(b *testing.B) {
	dir := b.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Status: models.GoalStatusInProgress, Created: now},
		Tasks: []models.Task{
			{ID: "task-1", Status: "pending"},
			{ID: "task-2", Status: "pending"},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	bb := New(statePath)
	if err := bb.Write(state); err != nil {
		b.Fatalf("Write failed: %v", err)
	}

	b.ResetTimer()
	for range b.N {
		_, err := bb.Read()
		if err != nil {
			b.Fatalf("Read failed: %v", err)
		}
	}
}

// TestBlackboardModifyPreservesUnknownFields verifies that YAML fields not present
// in the Go structs survive a Modify round-trip (read → unmarshal → mutate → marshal → write).
func TestBlackboardModifyPreservesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	// Write YAML with unknown fields at root, task, and agent levels,
	// including nested structures (maps, lists) to exercise recursive preservation.
	yamlWithExtras := `version: 1
custom_root_field: preserved
custom_nested:
    key_a: 1
    key_b:
        - alpha
        - beta
goal:
    id: goal-1
    description: Test goal
    spec_ref: spec.md
    created: 2025-01-17T14:00:00Z
    status: IN_PROGRESS
tasks:
    - id: task-1
      description: Test task
      status: READY
      priority: 1
      spec_ref: spec.md
      done_when: Tests pass
      scope: small
      created: 2025-01-17T14:00:00Z
      history: []
      custom_task_field: also preserved
agents:
    agent-1:
        role: coder
        status: IDLE
        heartbeat: 2025-01-17T14:00:00Z
        terminal: /dev/pts/1
        iterations_total: 0
        context_percent: 0
        custom_agent_field: agent extra
config:
    max_coder_iterations: 5
    max_review_cycles: 3
    heartbeat_interval: 30
    lease_duration: 300
    coder_poll_interval: 10
    doer_max_wait: 600
    integration_branch: main
    custom_config_field: config extra
circuit_breaker:
    last_check: 2025-01-17T14:00:00Z
    status: "OK"
sprint:
    id: sprint-1
    goal_ref: goal-1
    scope:
        planned: []
        stretch: []
    timeline:
        started: 2025-01-17T14:00:00Z
        deadline: 2025-01-24T14:00:00Z
    status: IN_PROGRESS
    metrics:
        tasks_done: 0
`
	if err := os.WriteFile(statePath, []byte(yamlWithExtras), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	bb := New(statePath)

	// Modify a known field (task description) — this triggers a full round-trip.
	err := bb.Modify(func(s *models.State) error {
		if len(s.Tasks) == 0 {
			return fmt.Errorf("no tasks found")
		}
		s.Tasks[0].Description = "Modified description"
		return nil
	})
	if err != nil {
		t.Fatalf("Modify failed: %v", err)
	}

	// Read back raw YAML and verify unknown fields survived.
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	content := string(data)

	checks := []struct {
		label string
		field string
	}{
		{"root-level unknown field", "custom_root_field: preserved"},
		{"root-level nested map key", "key_a: 1"},
		{"root-level nested list", "- alpha"},
		{"task-level unknown field", "custom_task_field: also preserved"},
		{"agent-level unknown field", "custom_agent_field: agent extra"},
		{"config-level unknown field", "custom_config_field: config extra"},
	}
	for _, check := range checks {
		if !strings.Contains(content, check.field) {
			t.Errorf("%s lost during round-trip; expected %q in output:\n%s", check.label, check.field, content)
		}
	}

	// Also verify the modification actually applied.
	if !strings.Contains(content, "Modified description") {
		t.Error("Modification not applied: expected 'Modified description' in output")
	}
}

// TestBlackboardWritePreservesUnknownFields verifies that a Read+Write cycle
// preserves unknown fields (as opposed to Modify which does it atomically).
func TestBlackboardWritePreservesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	yamlWithExtras := `version: 1
experimental_section: {key: value}
goal:
    id: goal-1
    description: Test
    spec_ref: spec.md
    created: 2025-01-17T14:00:00Z
    status: IN_PROGRESS
tasks: []
agents: {}
config:
    max_coder_iterations: 5
    max_review_cycles: 3
    heartbeat_interval: 30
    lease_duration: 300
    coder_poll_interval: 10
    doer_max_wait: 600
    integration_branch: main
circuit_breaker:
    last_check: 2025-01-17T14:00:00Z
    status: "OK"
sprint:
    id: sprint-1
    goal_ref: goal-1
    scope:
        planned: []
        stretch: []
    timeline:
        started: 2025-01-17T14:00:00Z
        deadline: 2025-01-24T14:00:00Z
    status: IN_PROGRESS
    metrics:
        tasks_done: 0
`
	if err := os.WriteFile(statePath, []byte(yamlWithExtras), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	bb := New(statePath)

	// Read → mutate → Write (the non-atomic path).
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	state.Version = 2
	if err := bb.Write(state); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Verify unknown field survived.
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !strings.Contains(string(data), "experimental_section") {
		t.Errorf("Unknown field lost during Read+Write round-trip:\n%s", string(data))
	}
}

// TestBlackboardWriteRoundTripsLeadingBlankLineBlockScalars verifies that
// state writes remain parseable when multiline scalar fields begin with a
// leading blank line. This covers rejection_reason and history.reason because
// reviewer-style markdown often starts with a blank line before headings.
func TestBlackboardWriteRoundTripsLeadingBlankLineBlockScalars(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	rejection := "\n---\nBlockers: 2\n- [blocker] specs/plans/EP-001.md:135: PATCH no-op\n- [blocker] docs/usage.md:12: Colon: inside prose"
	historyReason := "\n---\nBlockers: 1\n- [blocker] internal/ops/proceed.go:386: wrong transition"

	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "repro",
			SpecRef:     "specs/goals/repro.md",
			Created:     now,
			Status:      models.GoalStatusInProgress,
		},
		Tasks: []models.Task{{
			ID:              "task-1",
			Description:     "rejected task",
			Status:          models.TaskStatusRejected,
			Priority:        1,
			SpecRef:         "specs/goals/repro.md",
			DoneWhen:        "done",
			Scope:           "small",
			Created:         now,
			RejectionReason: &rejection,
			History: []models.TaskHistoryEntry{{
				Time:   now,
				Event:  models.TaskEventRejected,
				Reason: &historyReason,
			}},
		}},
		Agents: map[string]models.Agent{},
		Config: models.Config{IntegrationBranch: "main"},
	}

	bb := New(statePath)
	if err := bb.Write(state); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	roundTripped, err := bb.Read()
	if err != nil {
		data, readErr := os.ReadFile(statePath)
		if readErr != nil {
			t.Fatalf("Read failed: %v (also failed to inspect state.yaml: %v)", err, readErr)
		}
		t.Fatalf("Read failed after write: %v\nemitted YAML:\n%s", err, string(data))
	}

	if got := roundTripped.Tasks[0].RejectionReason; got == nil || *got != rejection {
		t.Fatalf("rejection_reason round-trip mismatch: got %#v", got)
	}
	if got := roundTripped.Tasks[0].History[0].Reason; got == nil || *got != historyReason {
		t.Fatalf("history.reason round-trip mismatch: got %#v", got)
	}
}

func TestBlackboardWritePreservesValidExplicitIndentSiblingScalars(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	rejection := "\n---\nBlockers: 2\n- [blocker] specs/plans/EP-001.md:135: PATCH no-op"
	validIndented := "  foo\n  bar\n"

	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "repro",
			SpecRef:     "specs/goals/repro.md",
			Created:     now,
			Status:      models.GoalStatusInProgress,
		},
		Tasks: []models.Task{{
			ID:              "task-1",
			Description:     "rejected task",
			Status:          models.TaskStatusRejected,
			Priority:        1,
			SpecRef:         "specs/goals/repro.md",
			DoneWhen:        "done",
			Scope:           "small",
			Created:         now,
			RejectionReason: &rejection,
			History:         []models.TaskHistoryEntry{},
		}},
		Agents: map[string]models.Agent{},
		Config: models.Config{IntegrationBranch: "main"},
		Extra: map[string]any{
			"valid_indented_scalar": validIndented,
		},
	}

	bb := New(statePath)
	if err := bb.Write(state); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	roundTripped, err := bb.Read()
	if err != nil {
		data, readErr := os.ReadFile(statePath)
		if readErr != nil {
			t.Fatalf("Read failed: %v (also failed to inspect state.yaml: %v)", err, readErr)
		}
		t.Fatalf("Read failed after write: %v\nemitted YAML:\n%s", err, string(data))
	}

	got, ok := roundTripped.Extra["valid_indented_scalar"].(string)
	if !ok {
		t.Fatalf("valid_indented_scalar missing or not string: %#v", roundTripped.Extra["valid_indented_scalar"])
	}
	if got != validIndented {
		t.Fatalf("valid_indented_scalar round-trip mismatch:\n got: %q\nwant: %q", got, validIndented)
	}
}

// TestBlackboardReadNormalizesUnderscoreRoles verifies that Read returns
// hyphenated Agent.Role even when state.yaml contains underscore-form roles.
func TestBlackboardReadNormalizesUnderscoreRoles(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	// Write raw YAML with underscore-form role
	rawYAML := `version: 1
goal:
    id: goal-1
    description: Test
    spec_ref: spec.md
    created: 2025-01-17T14:00:00Z
    status: IN_PROGRESS
tasks: []
agents:
    reviewer-1:
        role: code_reviewer
        status: IDLE
        heartbeat: 2025-01-17T14:00:00Z
        iterations_total: 0
        context_percent: 0
    planner-1:
        role: epic_plan_reviewer
        status: IDLE
        heartbeat: 2025-01-17T14:00:00Z
        iterations_total: 0
        context_percent: 0
    coder-1:
        role: coder
        status: IDLE
        heartbeat: 2025-01-17T14:00:00Z
        iterations_total: 0
        context_percent: 0
config:
    max_coder_iterations: 5
    max_review_cycles: 3
    heartbeat_interval: 30
    lease_duration: 300
    coder_poll_interval: 10
    doer_max_wait: 600
    integration_branch: main
`
	if err := os.WriteFile(statePath, []byte(rawYAML), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	bb := New(statePath)

	// Read should normalize underscore roles to hyphenated
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	tests := []struct {
		agentID  string
		wantRole string
	}{
		{"reviewer-1", "code-reviewer"},
		{"planner-1", "epic-plan-reviewer"},
		{"coder-1", "coder"}, // single-word, unchanged
	}

	for _, tt := range tests {
		agent, ok := state.Agents[tt.agentID]
		if !ok {
			t.Fatalf("agent %s not found", tt.agentID)
		}
		if agent.Role != tt.wantRole {
			t.Errorf("agent %s: got role %q, want %q", tt.agentID, agent.Role, tt.wantRole)
		}
	}
}

// TestBlackboardReadCachedNormalizesUnderscoreRoles verifies that ReadCached
// also normalizes underscore-form roles.
func TestBlackboardReadCachedNormalizesUnderscoreRoles(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	rawYAML := `version: 1
goal:
    id: goal-1
    description: Test
    spec_ref: spec.md
    created: 2025-01-17T14:00:00Z
    status: IN_PROGRESS
tasks: []
agents:
    reviewer-1:
        role: us_reviewer
        status: IDLE
        heartbeat: 2025-01-17T14:00:00Z
        iterations_total: 0
        context_percent: 0
config:
    max_coder_iterations: 5
    max_review_cycles: 3
    heartbeat_interval: 30
    lease_duration: 300
    coder_poll_interval: 10
    doer_max_wait: 600
    integration_branch: main
`
	if err := os.WriteFile(statePath, []byte(rawYAML), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	bb := New(statePath)

	state, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("ReadCached failed: %v", err)
	}

	agent, ok := state.Agents["reviewer-1"]
	if !ok {
		t.Fatal("agent reviewer-1 not found")
	}
	if agent.Role != "us-reviewer" {
		t.Errorf("ReadCached: got role %q, want %q", agent.Role, "us-reviewer")
	}
}

// TestBlackboardModifyNormalizesUnderscoreRoles verifies that Modify also
// normalizes underscore-form roles before passing the state to the callback.
func TestBlackboardModifyNormalizesUnderscoreRoles(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	rawYAML := `version: 1
goal:
    id: goal-1
    description: Test
    spec_ref: spec.md
    created: 2025-01-17T14:00:00Z
    status: IN_PROGRESS
tasks: []
agents:
    reviewer-1:
        role: code_plan_reviewer
        status: IDLE
        heartbeat: 2025-01-17T14:00:00Z
        iterations_total: 0
        context_percent: 0
config:
    max_coder_iterations: 5
    max_review_cycles: 3
    heartbeat_interval: 30
    lease_duration: 300
    coder_poll_interval: 10
    doer_max_wait: 600
    integration_branch: main
`
	if err := os.WriteFile(statePath, []byte(rawYAML), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	bb := New(statePath)

	var observedRole string
	err := bb.Modify(func(state *models.State) error {
		agent := state.Agents["reviewer-1"]
		observedRole = agent.Role
		return nil
	})
	if err != nil {
		t.Fatalf("Modify failed: %v", err)
	}

	if observedRole != "code-plan-reviewer" {
		t.Errorf("Modify callback: got role %q, want %q", observedRole, "code-plan-reviewer")
	}
}

// TestRead_NormalizesLegacyAttempted verifies that Read() converts legacy
// attempted: ["agent-1"] into Attempt == 2 and removes the Extra key.
func TestRead_NormalizesLegacyAttempted(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	rawYAML := `version: 1
goal:
    id: goal-1
    description: Test
    spec_ref: spec.md
    created: 2025-01-17T14:00:00Z
    status: IN_PROGRESS
tasks:
    - id: task-1
      description: Test task
      status: IMPLEMENTING_CODE
      priority: 1
      created: 2025-01-17T14:00:00Z
      attempted:
          - agent-1
agents: {}
config:
    max_coder_iterations: 5
    max_review_cycles: 3
    heartbeat_interval: 30
    lease_duration: 300
    coder_poll_interval: 10
    doer_max_wait: 600
    integration_branch: main
`
	if err := os.WriteFile(statePath, []byte(rawYAML), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	bb := New(statePath)

	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if len(state.Tasks) == 0 {
		t.Fatal("expected at least one task")
	}
	task := state.Tasks[0]
	if task.Attempt != 2 {
		t.Errorf("Read: got Attempt %d, want 2", task.Attempt)
	}
	if _, ok := task.Extra["attempted"]; ok {
		t.Error("Read: Extra[\"attempted\"] should have been removed")
	}
}

// TestReadCached_NormalizesLegacyAttempted verifies ReadCached() also normalizes.
func TestReadCached_NormalizesLegacyAttempted(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	rawYAML := `version: 1
goal:
    id: goal-1
    description: Test
    spec_ref: spec.md
    created: 2025-01-17T14:00:00Z
    status: IN_PROGRESS
tasks:
    - id: task-1
      description: Test task
      status: IMPLEMENTING_CODE
      priority: 1
      created: 2025-01-17T14:00:00Z
      attempted:
          - agent-1
agents: {}
config:
    max_coder_iterations: 5
    max_review_cycles: 3
    heartbeat_interval: 30
    lease_duration: 300
    coder_poll_interval: 10
    doer_max_wait: 600
    integration_branch: main
`
	if err := os.WriteFile(statePath, []byte(rawYAML), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	bb := New(statePath)

	state, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("ReadCached failed: %v", err)
	}

	if len(state.Tasks) == 0 {
		t.Fatal("expected at least one task")
	}
	task := state.Tasks[0]
	if task.Attempt != 2 {
		t.Errorf("ReadCached: got Attempt %d, want 2", task.Attempt)
	}
	if _, ok := task.Extra["attempted"]; ok {
		t.Error("ReadCached: Extra[\"attempted\"] should have been removed")
	}
}

// TestModify_NormalizesLegacyAttempted verifies Modify() normalizes legacy
// attempted: before passing state to the callback.
func TestModify_NormalizesLegacyAttempted(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	rawYAML := `version: 1
goal:
    id: goal-1
    description: Test
    spec_ref: spec.md
    created: 2025-01-17T14:00:00Z
    status: IN_PROGRESS
tasks:
    - id: task-1
      description: Test task
      status: IMPLEMENTING_CODE
      priority: 1
      created: 2025-01-17T14:00:00Z
      attempted:
          - agent-1
agents: {}
config:
    max_coder_iterations: 5
    max_review_cycles: 3
    heartbeat_interval: 30
    lease_duration: 300
    coder_poll_interval: 10
    doer_max_wait: 600
    integration_branch: main
`
	if err := os.WriteFile(statePath, []byte(rawYAML), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	bb := New(statePath)

	var observedAttempt int
	var hasExtra bool
	err := bb.Modify(func(state *models.State) error {
		if len(state.Tasks) == 0 {
			return fmt.Errorf("expected at least one task")
		}
		observedAttempt = state.Tasks[0].Attempt
		_, hasExtra = state.Tasks[0].Extra["attempted"]
		return nil
	})
	if err != nil {
		t.Fatalf("Modify failed: %v", err)
	}

	if observedAttempt != 2 {
		t.Errorf("Modify callback: got Attempt %d, want 2", observedAttempt)
	}
	if hasExtra {
		t.Error("Modify callback: Extra[\"attempted\"] should have been removed")
	}
}

// TestRead_NoLegacyField_Unchanged verifies that a task with the new-format
// attempt: field is not modified by normalization.
func TestRead_NoLegacyField_Unchanged(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")

	rawYAML := `version: 1
goal:
    id: goal-1
    description: Test
    spec_ref: spec.md
    created: 2025-01-17T14:00:00Z
    status: IN_PROGRESS
tasks:
    - id: task-1
      description: Test task
      status: IMPLEMENTING_CODE
      priority: 1
      created: 2025-01-17T14:00:00Z
      attempt: 1
agents: {}
config:
    max_coder_iterations: 5
    max_review_cycles: 3
    heartbeat_interval: 30
    lease_duration: 300
    coder_poll_interval: 10
    doer_max_wait: 600
    integration_branch: main
`
	if err := os.WriteFile(statePath, []byte(rawYAML), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	bb := New(statePath)

	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if len(state.Tasks) == 0 {
		t.Fatal("expected at least one task")
	}
	task := state.Tasks[0]
	if task.Attempt != 1 {
		t.Errorf("Read: got Attempt %d, want 1", task.Attempt)
	}
}
