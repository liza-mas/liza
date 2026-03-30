package mcp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleAwaitResubmissionMissingTaskID(t *testing.T) {
	projectRoot, cleanup := setupTestWorkspaceWithGit(t)
	defer cleanup()

	server := NewServer(projectRoot, filepath.Join(projectRoot, ".liza", "log.yaml"))

	_, err := server.handleAwaitResubmission(map[string]any{
		"agent_id": "code-reviewer-1",
	})
	if err == nil {
		t.Fatal("Expected error when task_id is missing")
	}
	if !strings.Contains(err.Error(), "task_id") {
		t.Errorf("Expected error about task_id, got: %v", err)
	}
}

func TestHandleAwaitResubmissionMissingAgentID(t *testing.T) {
	projectRoot, cleanup := setupTestWorkspaceWithGit(t)
	defer cleanup()

	server := NewServer(projectRoot, filepath.Join(projectRoot, ".liza", "log.yaml"))

	_, err := server.handleAwaitResubmission(map[string]any{
		"task_id": "task-1",
	})
	if err == nil {
		t.Fatal("Expected error when agent_id is missing")
	}
	if !strings.Contains(err.Error(), "agent_id") {
		t.Errorf("Expected error about agent_id, got: %v", err)
	}
}

func TestHandleAwaitResubmissionTaskNotInAwaitableStatus(t *testing.T) {
	projectRoot, cleanup := setupTestWorkspaceWithGit(t)
	defer cleanup()

	server := NewServer(projectRoot, filepath.Join(projectRoot, ".liza", "log.yaml"))

	// task-1 is in READY status (not rejected/submitted), so AwaitResubmission
	// should fail precondition check immediately without blocking.
	_, err := server.handleAwaitResubmission(map[string]any{
		"task_id":         "task-1",
		"agent_id":        "code-reviewer-1",
		"timeout_seconds": float64(5),
	})
	if err == nil {
		t.Fatal("Expected error for task not in awaitable status")
	}
	if !strings.Contains(err.Error(), "await resubmission failed") {
		t.Errorf("Expected 'await resubmission failed' error, got: %v", err)
	}
}
