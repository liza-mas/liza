package commands

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

func TestCancelTaskCommand(t *testing.T) {
	tests := []struct {
		name        string
		taskID      string
		taskStatus  models.TaskStatus
		assignedTo  *string
		reason      string
		wantErr     bool
		errContains string
		wantStatus  models.TaskStatus
	}{
		{
			name:       "cancel from BLOCKED status",
			taskID:     "task-1",
			taskStatus: models.TaskStatusBlocked,
			reason:     "No longer needed",
			wantErr:    false,
			wantStatus: models.TaskStatusAbandoned,
		},
		{
			name:       "cancel from DRAFT_CODE status",
			taskID:     "task-1",
			taskStatus: models.TaskStatus("DRAFT_CODE"),
			reason:     "Requirements changed",
			wantErr:    false,
			wantStatus: models.TaskStatusAbandoned,
		},
		{
			name:       "cancel from REJECTED status",
			taskID:     "task-1",
			taskStatus: models.TaskStatusRejected,
			assignedTo: testhelpers.StringPtr("agent-1"),
			reason:     "Approach abandoned",
			wantErr:    false,
			wantStatus: models.TaskStatusAbandoned,
		},
		{
			name:       "cancel from INTEGRATION_FAILED",
			taskID:     "task-1",
			taskStatus: models.TaskStatusIntegrationFailed,
			reason:     "Giving up",
			wantErr:    false,
			wantStatus: models.TaskStatusAbandoned,
		},
		{
			name:        "error: task not found",
			taskID:      "nonexistent",
			taskStatus:  models.TaskStatusBlocked,
			reason:      "test",
			wantErr:     true,
			errContains: "task not found",
		},
		{
			name:       "cancel from IMPLEMENTING",
			taskID:     "task-1",
			taskStatus: models.TaskStatusImplementing,
			assignedTo: testhelpers.StringPtr("agent-1"),
			reason:     "Mis-framed task",
			wantErr:    false,
			wantStatus: models.TaskStatusAbandoned,
		},
		{
			name:        "error: invalid status APPROVED",
			taskID:      "task-1",
			taskStatus:  models.TaskStatusApproved,
			reason:      "test",
			wantErr:     true,
			errContains: "transition",
		},
		{
			name:        "error: invalid status MERGED",
			taskID:      "task-1",
			taskStatus:  models.TaskStatusMerged,
			reason:      "test",
			wantErr:     true,
			errContains: models.LifecycleAlreadyTransitioned,
		},
		{
			name:        "error: empty reason",
			taskID:      "task-1",
			taskStatus:  models.TaskStatusBlocked,
			reason:      "",
			wantErr:     true,
			errContains: "cancellation reason is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test workspace
			tmpDir := t.TempDir()
			testhelpers.SetupTestGitRepo(t, tmpDir)
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			// Create initial state with test task
			now := time.Now().UTC()
			initialState := &models.State{
				Version: 1,
				Goal: models.Goal{
					ID:               "goal-1",
					Description:      "Test goal",
					SpecRef:          "README.md",
					Created:          now,
					Status:           models.GoalStatusInProgress,
					AlignmentHistory: []models.AlignmentHistory{},
				},
				Tasks:  []models.Task{},
				Agents: make(map[string]models.Agent),
				Sprint: models.Sprint{
					ID:      "sprint-1",
					GoalRef: "goal-1",
					Scope: models.SprintScope{
						Planned: []string{},
						Stretch: []string{},
					},
					Timeline: models.SprintTimeline{
						Started: now,
					},
					Status: models.SprintStatusInProgress,
				},
				CircuitBreaker: models.CircuitBreaker{
					Status:  "OK",
					History: []models.CircuitBreakerHistory{},
				},
				Config: models.Config{
					MaxCoderIterations: 10,
					MaxReviewCycles:    5,
					IntegrationBranch:  "integration",
					LeaseDuration:      1800,
				},
			}

			// Only add task if we expect it to exist
			if tt.taskID != "nonexistent" {
				initialState.Tasks = []models.Task{
					{
						ID:          tt.taskID,
						Status:      tt.taskStatus,
						RolePair:    "coding-pair",
						AssignedTo:  tt.assignedTo,
						Description: "Test task description",
						SpecRef:     "README.md",
						DoneWhen:    "Test completion criteria",
						Scope:       "Test scope",
						History:     []models.TaskHistoryEntry{},
					},
				}
			}

			// Write initial state
			stateYAML, err := yaml.Marshal(initialState)
			if err != nil {
				t.Fatalf("Failed to marshal initial state: %v", err)
			}
			if err := os.WriteFile(statePath, stateYAML, 0644); err != nil {
				t.Fatalf("Failed to write initial state: %v", err)
			}

			// Execute command
			err = CancelTaskCommand(tmpDir, tt.taskID, tt.reason, "test-agent")

			// Check error expectations
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errContains)
				} else if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Expected error containing %q, got %q", tt.errContains, err.Error())
				}
				if tt.taskStatus == models.TaskStatusMerged {
					var lifecycleErr *ops.LifecycleError
					if !errors.As(err, &lifecycleErr) || lifecycleErr.Outcome.Outcome != models.LifecycleAlreadyTransitioned || lifecycleErr.Outcome.SafeAction != "stop" {
						t.Fatalf("terminal cancellation = %v, want ALREADY_TRANSITIONED/stop", err)
					}
					after, readErr := os.ReadFile(statePath)
					if readErr != nil {
						t.Fatal(readErr)
					}
					if !bytes.Equal(stateYAML, after) {
						t.Fatal("terminal cancellation changed state")
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Read and verify final state
			stateData, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatalf("Failed to read final state: %v", err)
			}

			var finalState models.State
			if err := yaml.Unmarshal(stateData, &finalState); err != nil {
				t.Fatalf("Failed to unmarshal final state: %v", err)
			}

			// Find the task
			var task *models.Task
			for i := range finalState.Tasks {
				if finalState.Tasks[i].ID == tt.taskID {
					task = &finalState.Tasks[i]
					break
				}
			}

			if task == nil {
				t.Fatalf("Task %s not found in final state", tt.taskID)
			}

			// Verify status
			if task.Status != tt.wantStatus {
				t.Errorf("Expected status %s, got %s", tt.wantStatus, task.Status)
			}

			// Verify lease fields cleared
			if task.AssignedTo != nil {
				t.Errorf("Expected AssignedTo to be cleared, got %v", *task.AssignedTo)
			}
			if task.LeaseExpires != nil {
				t.Errorf("Expected LeaseExpires to be cleared, got %v", *task.LeaseExpires)
			}
			if task.ReviewingBy != nil {
				t.Errorf("Expected ReviewingBy to be cleared, got %v", *task.ReviewingBy)
			}
			if task.ReviewLeaseExpires != nil {
				t.Errorf("Expected ReviewLeaseExpires to be cleared, got %v", *task.ReviewLeaseExpires)
			}

			// Verify history entry added
			if len(task.History) == 0 {
				t.Errorf("Expected history entry to be added")
			} else {
				lastEntry := task.History[len(task.History)-1]
				if lastEntry.Event != "abandoned" {
					t.Errorf("Expected history event 'abandoned', got %q", lastEntry.Event)
				}
				if lastEntry.Agent == nil || *lastEntry.Agent != "test-agent" {
					t.Errorf("Expected history agent 'test-agent', got %v", lastEntry.Agent)
				}
				if lastEntry.Reason == nil || *lastEntry.Reason != tt.reason {
					t.Errorf("Expected history reason %q, got %v", tt.reason, lastEntry.Reason)
				}
			}
		})
	}
}

func TestCancelTaskCommand_RaceCondition(t *testing.T) {
	// Setup test workspace
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Create initial state with BLOCKED task
	now := time.Now().UTC()
	initialState := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:               "goal-1",
			Description:      "Test goal",
			SpecRef:          "README.md",
			Created:          now,
			Status:           models.GoalStatusInProgress,
			AlignmentHistory: []models.AlignmentHistory{},
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Status:      models.TaskStatusBlocked,
				RolePair:    "coding-pair",
				Description: "Test task description",
				SpecRef:     "README.md",
				DoneWhen:    "Test completion criteria",
				Scope:       "Test scope",
				History:     []models.TaskHistoryEntry{},
			},
		},
		Agents: make(map[string]models.Agent),
		Sprint: models.Sprint{
			ID:      "sprint-1",
			GoalRef: "goal-1",
			Scope: models.SprintScope{
				Planned: []string{},
				Stretch: []string{},
			},
			Timeline: models.SprintTimeline{
				Started: now,
			},
			Status: models.SprintStatusInProgress,
		},
		CircuitBreaker: models.CircuitBreaker{
			Status:  "OK",
			History: []models.CircuitBreakerHistory{},
		},
		Config: models.Config{
			MaxCoderIterations: 10,
			MaxReviewCycles:    5,
			IntegrationBranch:  "integration",
			LeaseDuration:      1800,
		},
	}

	stateYAML, err := yaml.Marshal(initialState)
	if err != nil {
		t.Fatalf("Failed to marshal initial state: %v", err)
	}
	if err := os.WriteFile(statePath, stateYAML, 0644); err != nil {
		t.Fatalf("Failed to write initial state: %v", err)
	}

	// Launch concurrent cancel attempts
	var wg sync.WaitGroup
	results := make(chan error, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := CancelTaskCommand(tmpDir, "task-1", "Test reason", "test-agent")
			results <- err
		}()
	}

	wg.Wait()
	close(results)

	// Check results: exactly one should succeed
	successes := 0
	failures := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}

	if successes != 1 {
		t.Errorf("Expected exactly 1 success, got %d", successes)
	}
	if failures != 2 {
		t.Errorf("Expected exactly 2 failures, got %d", failures)
	}

	// Read and verify final state
	stateData, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("Failed to read final state: %v", err)
	}

	var finalState models.State
	if err := yaml.Unmarshal(stateData, &finalState); err != nil {
		t.Fatalf("Failed to unmarshal final state: %v", err)
	}

	if len(finalState.Tasks) != 1 {
		t.Fatalf("Expected 1 task, got %d", len(finalState.Tasks))
	}

	task := finalState.Tasks[0]
	if task.Status != models.TaskStatusAbandoned {
		t.Errorf("Expected status ABANDONED, got %s", task.Status)
	}
}

func TestCancelTaskCommand_LeaseFieldsCleared(t *testing.T) {
	// Setup test workspace
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Create initial state with BLOCKED task that has stale lease fields
	now := time.Now().UTC()
	staleTime := now.Add(-1 * time.Hour)
	initialState := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:               "goal-1",
			Description:      "Test goal",
			SpecRef:          "README.md",
			Created:          now,
			Status:           models.GoalStatusInProgress,
			AlignmentHistory: []models.AlignmentHistory{},
		},
		Tasks: []models.Task{
			{
				ID:           "task-1",
				Status:       models.TaskStatusBlocked,
				RolePair:     "coding-pair",
				Description:  "Test task description",
				SpecRef:      "README.md",
				DoneWhen:     "Test completion criteria",
				Scope:        "Test scope",
				AssignedTo:   testhelpers.StringPtr("old-agent"),
				LeaseExpires: &staleTime,
				History:      []models.TaskHistoryEntry{},
			},
		},
		Agents: make(map[string]models.Agent),
		Sprint: models.Sprint{
			ID:      "sprint-1",
			GoalRef: "goal-1",
			Scope: models.SprintScope{
				Planned: []string{},
				Stretch: []string{},
			},
			Timeline: models.SprintTimeline{
				Started: now,
			},
			Status: models.SprintStatusInProgress,
		},
		CircuitBreaker: models.CircuitBreaker{
			Status:  "OK",
			History: []models.CircuitBreakerHistory{},
		},
		Config: models.Config{
			MaxCoderIterations: 10,
			MaxReviewCycles:    5,
			IntegrationBranch:  "integration",
			LeaseDuration:      1800,
		},
	}

	stateYAML, err := yaml.Marshal(initialState)
	if err != nil {
		t.Fatalf("Failed to marshal initial state: %v", err)
	}
	if err := os.WriteFile(statePath, stateYAML, 0644); err != nil {
		t.Fatalf("Failed to write initial state: %v", err)
	}

	// Execute cancel command
	err = CancelTaskCommand(tmpDir, "task-1", "Test reason", "test-agent")
	if err != nil {
		t.Fatalf("Failed to cancel task: %v", err)
	}

	// Read and verify lease fields are cleared
	stateData, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("Failed to read final state: %v", err)
	}

	var finalState models.State
	if err := yaml.Unmarshal(stateData, &finalState); err != nil {
		t.Fatalf("Failed to unmarshal final state: %v", err)
	}

	task := finalState.Tasks[0]
	if task.AssignedTo != nil {
		t.Errorf("Expected AssignedTo to be nil, got %v", *task.AssignedTo)
	}
	if task.LeaseExpires != nil {
		t.Errorf("Expected LeaseExpires to be nil, got %v", *task.LeaseExpires)
	}
}

func TestCancelTaskCommand_ValidationIntegration(t *testing.T) {
	// Setup test workspace
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Create initial state with BLOCKED task
	now := time.Now().UTC()
	initialState := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:               "goal-1",
			Description:      "Test goal",
			SpecRef:          "README.md",
			Created:          now,
			Status:           models.GoalStatusInProgress,
			AlignmentHistory: []models.AlignmentHistory{},
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Status:      models.TaskStatusBlocked,
				RolePair:    "coding-pair",
				Description: "Test task description",
				SpecRef:     "README.md",
				DoneWhen:    "Test completion criteria",
				Scope:       "Test scope",
				History:     []models.TaskHistoryEntry{},
			},
		},
		Agents: make(map[string]models.Agent),
		Sprint: models.Sprint{
			ID:      "sprint-1",
			GoalRef: "goal-1",
			Scope: models.SprintScope{
				Planned: []string{},
				Stretch: []string{},
			},
			Timeline: models.SprintTimeline{
				Started: now,
			},
			Status: models.SprintStatusInProgress,
		},
		CircuitBreaker: models.CircuitBreaker{
			Status:  "OK",
			History: []models.CircuitBreakerHistory{},
		},
		Config: models.Config{
			MaxCoderIterations: 10,
			MaxReviewCycles:    5,
			IntegrationBranch:  "integration",
			LeaseDuration:      1800,
		},
	}

	stateYAML, err := yaml.Marshal(initialState)
	if err != nil {
		t.Fatalf("Failed to marshal initial state: %v", err)
	}
	if err := os.WriteFile(statePath, stateYAML, 0644); err != nil {
		t.Fatalf("Failed to write initial state: %v", err)
	}

	// Execute cancel command
	err = CancelTaskCommand(tmpDir, "task-1", "Test reason", "test-agent")
	if err != nil {
		t.Fatalf("Failed to cancel task: %v", err)
	}

	// Run validation command with state path
	err = ValidateCommand(statePath, true) // Skip spec file checks for test
	if err != nil {
		t.Errorf("Validation failed after cancel: %v", err)
	}
}
