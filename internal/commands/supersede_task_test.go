package commands

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

func TestSupersedeTaskCommand(t *testing.T) {
	tests := []struct {
		name             string
		taskID           string
		taskStatus       models.TaskStatus
		assignedTo       *string
		replacementIDs   []string
		reason           string
		recoverability   string
		wantErr          bool
		errContains      string
		wantStatus       models.TaskStatus
		wantSupersededBy []string
		wantReason       string
	}{
		{
			name:             "basic supersede from BLOCKED status",
			taskID:           "task-1",
			taskStatus:       models.TaskStatusBlocked,
			replacementIDs:   []string{"task-2"},
			reason:           "Split into smaller tasks",
			wantErr:          false,
			wantStatus:       models.TaskStatusSuperseded,
			wantSupersededBy: []string{"task-2"},
			wantReason:       "Split into smaller tasks",
		},
		{
			name:             "supersede with multiple replacements",
			taskID:           "task-1",
			taskStatus:       models.TaskStatusBlocked,
			replacementIDs:   []string{"task-2", "task-3", "task-4"},
			reason:           "Split into three smaller tasks",
			wantErr:          false,
			wantStatus:       models.TaskStatusSuperseded,
			wantSupersededBy: []string{"task-2", "task-3", "task-4"},
			wantReason:       "Split into three smaller tasks",
		},
		{
			name:             "supersede from REJECTED status",
			taskID:           "task-1",
			taskStatus:       models.TaskStatusRejected,
			assignedTo:       testhelpers.StringPtr("agent-1"),
			replacementIDs:   []string{"task-2"},
			reason:           "Revised approach needed",
			wantErr:          false,
			wantStatus:       models.TaskStatusSuperseded,
			wantSupersededBy: []string{"task-2"},
			wantReason:       "Revised approach needed",
		},
		{
			name:             "supersede from READY status",
			taskID:           "task-1",
			taskStatus:       models.TaskStatusReady,
			replacementIDs:   []string{"task-2"},
			reason:           "Requirements changed",
			wantErr:          false,
			wantStatus:       models.TaskStatusSuperseded,
			wantSupersededBy: []string{"task-2"},
			wantReason:       "Requirements changed",
		},
		{
			name:           "error: task not found",
			taskID:         "nonexistent",
			taskStatus:     models.TaskStatusBlocked,
			replacementIDs: []string{"task-2"},
			reason:         "test",
			wantErr:        true,
			errContains:    "task not found",
		},
		{
			name:           "error: invalid status IMPLEMENTING",
			taskID:         "task-1",
			taskStatus:     models.TaskStatusImplementing,
			replacementIDs: []string{"task-2"},
			reason:         "test",
			wantErr:        true,
			errContains:    "cannot supersede task",
		},
		{
			name:           "error: invalid status MERGED",
			taskID:         "task-1",
			taskStatus:     models.TaskStatusMerged,
			replacementIDs: []string{"task-2"},
			reason:         "test",
			wantErr:        true,
			errContains:    "cannot supersede task",
		},
		{
			name:             "supersede without replacements",
			taskID:           "task-1",
			taskStatus:       models.TaskStatusBlocked,
			replacementIDs:   nil,
			reason:           "Work already merged in prior sprint",
			recoverability:   "liza recover-task task-1",
			wantErr:          false,
			wantStatus:       models.TaskStatusSuperseded,
			wantSupersededBy: nil,
			wantReason:       "Work already merged in prior sprint",
		},
		{
			name:           "error: empty rescope reason",
			taskID:         "task-1",
			taskStatus:     models.TaskStatusBlocked,
			replacementIDs: []string{"task-2"},
			reason:         "",
			wantErr:        true,
			errContains:    "rescope reason is required",
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
			err = SupersedeTaskCommand(tmpDir, tt.taskID, tt.replacementIDs, tt.reason, "test-agent", tt.recoverability, "test: changed since block")

			// Check error expectations
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errContains)
				} else if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Expected error containing %q, got %q", tt.errContains, err.Error())
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

			// Verify superseded_by
			if len(task.SupersededBy) != len(tt.wantSupersededBy) {
				t.Errorf("Expected %d replacement IDs, got %d", len(tt.wantSupersededBy), len(task.SupersededBy))
			} else {
				for i, id := range tt.wantSupersededBy {
					if task.SupersededBy[i] != id {
						t.Errorf("Expected replacement ID %s at index %d, got %s", id, i, task.SupersededBy[i])
					}
				}
			}

			// Verify rescope reason
			if task.RescopeReason == nil {
				t.Errorf("Expected rescope reason %q, got nil", tt.wantReason)
			} else if *task.RescopeReason != tt.wantReason {
				t.Errorf("Expected rescope reason %q, got %q", tt.wantReason, *task.RescopeReason)
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
				if lastEntry.Event != "superseded" {
					t.Errorf("Expected history event 'superseded', got %q", lastEntry.Event)
				}
				if lastEntry.Agent == nil || *lastEntry.Agent != "test-agent" {
					t.Errorf("Expected history agent 'test-agent', got %v", lastEntry.Agent)
				}
				if lastEntry.Reason == nil || *lastEntry.Reason != tt.wantReason {
					t.Errorf("Expected history reason %q, got %v", tt.wantReason, lastEntry.Reason)
				}
				// Verify note contains replacement IDs
				if lastEntry.Note == nil {
					t.Errorf("Expected history note to be set")
				} else {
					for _, id := range tt.wantSupersededBy {
						if !strings.Contains(*lastEntry.Note, id) {
							t.Errorf("Expected history note to contain %q, got %q", id, *lastEntry.Note)
						}
					}
				}
			}
		})
	}
}

func TestSupersedeTaskCommand_RaceCondition(t *testing.T) {
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

	// Launch concurrent supersede attempts
	var wg sync.WaitGroup
	results := make(chan error, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			err := SupersedeTaskCommand(
				tmpDir,
				"task-1",
				[]string{"task-2"},
				"Test reason",
				"test-agent",
				// Recoverability command is only valid for no-replacement cleanup.
				"",
				"test: changed since block",
			)
			results <- err
		}(i)
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
			// Verify it's a status-related error (TOCTOU protection)
			if !strings.Contains(err.Error(), "status") && !strings.Contains(err.Error(), "cannot supersede") {
				t.Errorf("Expected status-related error, got: %v", err)
			}
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
	if task.Status != models.TaskStatusSuperseded {
		t.Errorf("Expected status SUPERSEDED, got %s", task.Status)
	}
}

func TestSupersedeTaskCommand_ValidationIntegration(t *testing.T) {
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

	// Execute supersede command
	err = SupersedeTaskCommand(tmpDir, "task-1", []string{"task-2"}, "Test reason", "test-agent", "", "test: changed since block")
	if err != nil {
		t.Fatalf("Failed to supersede task: %v", err)
	}

	// Run validation command with state path
	err = ValidateCommand(statePath, true) // Skip spec file checks for test
	if err != nil {
		t.Errorf("Validation failed after supersede: %v", err)
	}
}

func TestSupersedeTaskCommand_LeaseFieldsCleared(t *testing.T) {
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

	// Execute supersede command
	err = SupersedeTaskCommand(tmpDir, "task-1", []string{"task-2"}, "Test reason", "test-agent", "", "test: changed since block")
	if err != nil {
		t.Fatalf("Failed to supersede task: %v", err)
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
