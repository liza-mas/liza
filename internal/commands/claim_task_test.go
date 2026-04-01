package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestClaimTaskCommand(t *testing.T) {
	tests := []struct {
		name           string
		taskID         string
		agentID        string
		taskStatus     models.TaskStatus
		hasWorktree    bool
		hasDependency  bool
		depStatus      models.TaskStatus
		agentBusy      bool
		wantErr        bool
		errContains    string
		wantWorktree   bool
		checkIteration bool
		previousAgent  *string
	}{
		{
			name:           "claim READY task",
			taskID:         "task-1",
			agentID:        "coder-1",
			taskStatus:     models.TaskStatusReady,
			hasWorktree:    false,
			wantErr:        false,
			wantWorktree:   true,
			checkIteration: true,
		},
		{
			name:          "claim READY task with unmet dependencies",
			taskID:        "task-2",
			agentID:       "coder-1",
			taskStatus:    models.TaskStatusReady,
			hasWorktree:   false,
			hasDependency: true,
			depStatus:     models.TaskStatusReady,
			wantErr:       true,
			errContains:   "unmet dependencies",
		},
		{
			name:          "claim READY task with satisfied dependencies",
			taskID:        "task-3",
			agentID:       "coder-1",
			taskStatus:    models.TaskStatusReady,
			hasWorktree:   false,
			hasDependency: true,
			depStatus:     models.TaskStatusMerged,
			wantErr:       false,
			wantWorktree:  true,
		},
		{
			name:          "claim READY task with superseded dependency",
			taskID:        "task-3",
			agentID:       "coder-1",
			taskStatus:    models.TaskStatusReady,
			hasWorktree:   false,
			hasDependency: true,
			depStatus:     models.TaskStatusSuperseded,
			wantErr:       false,
			wantWorktree:  true,
		},
		{
			name:          "claim REJECTED task by same coder",
			taskID:        "task-4",
			agentID:       "coder-1",
			taskStatus:    models.TaskStatusRejected,
			hasWorktree:   true,
			wantErr:       false,
			wantWorktree:  true,
			previousAgent: testhelpers.StringPtr("coder-1"),
		},
		{
			name:          "claim REJECTED task by different coder",
			taskID:        "task-5",
			agentID:       "coder-2",
			taskStatus:    models.TaskStatusRejected,
			hasWorktree:   true,
			wantErr:       false,
			wantWorktree:  true,
			previousAgent: testhelpers.StringPtr("coder-1"),
		},
		{
			name:         "claim INTEGRATION_FAILED task",
			taskID:       "task-6",
			agentID:      "coder-1",
			taskStatus:   models.TaskStatusIntegrationFailed,
			hasWorktree:  true,
			wantErr:      false,
			wantWorktree: true,
		},
		{
			name:        "cannot claim IMPLEMENTING task",
			taskID:      "task-7",
			agentID:     "coder-1",
			taskStatus:  models.TaskStatusImplementing,
			hasWorktree: true,
			wantErr:     true,
			errContains: "not claimable by",
		},
		{
			name:        "cannot claim BLOCKED task",
			taskID:      "task-8",
			agentID:     "coder-1",
			taskStatus:  models.TaskStatusBlocked,
			hasWorktree: false,
			wantErr:     true,
			errContains: "not claimable by",
		},
		{
			name:        "agent already busy",
			taskID:      "task-9",
			agentID:     "coder-1",
			taskStatus:  models.TaskStatusReady,
			hasWorktree: false,
			agentBusy:   true,
			wantErr:     true,
			errContains: "already working",
		},
		{
			name:        "empty task ID",
			taskID:      "",
			agentID:     "coder-1",
			taskStatus:  models.TaskStatusReady,
			hasWorktree: false,
			wantErr:     true,
			errContains: "task ID is required",
		},
		{
			name:        "empty agent ID",
			taskID:      "task-10",
			agentID:     "",
			taskStatus:  models.TaskStatusReady,
			hasWorktree: false,
			wantErr:     true,
			errContains: "agent ID is required",
		},
		{
			name:        "nonexistent task",
			taskID:      "nonexistent",
			agentID:     "coder-1",
			taskStatus:  models.TaskStatusReady,
			hasWorktree: false,
			wantErr:     true,
			errContains: "task not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory (project root)
			tmpDir := t.TempDir()

			// Initialize git repo
			testhelpers.SetupTestGitRepo(t, tmpDir)

			// Create .liza directory
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			// Create initial state
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

			// Add dependency task if needed
			if tt.hasDependency {
				depTask := models.Task{
					ID:          "task-0",
					Description: "Dependency task",
					Status:      tt.depStatus,
					RolePair:    "coding-pair",
					Priority:    1,
					Created:     now,
					SpecRef:     "README.md",
					DoneWhen:    "Done",
					Scope:       "Test",
					History:     []models.TaskHistoryEntry{},
				}
				initialState.Tasks = append(initialState.Tasks, depTask)
			}

			// Add task if not testing nonexistent task
			if tt.taskID != "nonexistent" && tt.taskID != "" {
				var worktreePath *string
				var baseCommit *string
				var assignedTo *string
				var leaseExpires *time.Time
				var reviewCommit *string
				var rejectionReason *string
				var iteration int

				if tt.hasWorktree {
					wt := filepath.Join(".worktrees", tt.taskID)
					worktreePath = &wt
					bc := "abc1234"
					baseCommit = &bc
				}

				if tt.taskStatus == models.TaskStatusImplementing {
					agent := "coder-other"
					assignedTo = &agent
					exp := now.Add(30 * time.Minute)
					leaseExpires = &exp
				}

				if tt.taskStatus == models.TaskStatusRejected {
					if tt.previousAgent != nil {
						assignedTo = tt.previousAgent
					} else {
						agent := "coder-other"
						assignedTo = &agent
					}
					reason := "Test rejection"
					rejectionReason = &reason
					rc := "def5678"
					reviewCommit = &rc
					iteration = 1
				}

				if tt.taskStatus == models.TaskStatusIntegrationFailed {
					agent := "coder-other"
					assignedTo = &agent
				}

				if tt.taskStatus == models.TaskStatusBlocked {
					reason := "Test block"
					task := models.Task{
						ID:               tt.taskID,
						Description:      "Test task",
						Status:           tt.taskStatus,
						RolePair:         "coding-pair",
						Priority:         1,
						Created:          now,
						SpecRef:          "README.md",
						DoneWhen:         "Done",
						Scope:            "Test",
						Worktree:         worktreePath,
						BaseCommit:       baseCommit,
						AssignedTo:       assignedTo,
						LeaseExpires:     leaseExpires,
						ReviewCommit:     reviewCommit,
						RejectionReason:  rejectionReason,
						Iteration:        iteration,
						BlockedReason:    &reason,
						BlockedQuestions: []string{"Question 1"},
						History:          []models.TaskHistoryEntry{},
					}
					if tt.hasDependency {
						task.DependsOn = []string{"task-0"}
					}
					initialState.Tasks = append(initialState.Tasks, task)
				} else {
					task := models.Task{
						ID:              tt.taskID,
						Description:     "Test task",
						Status:          tt.taskStatus,
						RolePair:        "coding-pair",
						Priority:        1,
						Created:         now,
						SpecRef:         "README.md",
						DoneWhen:        "Done",
						Scope:           "Test",
						Worktree:        worktreePath,
						BaseCommit:      baseCommit,
						AssignedTo:      assignedTo,
						LeaseExpires:    leaseExpires,
						ReviewCommit:    reviewCommit,
						RejectionReason: rejectionReason,
						Iteration:       iteration,
						History:         []models.TaskHistoryEntry{},
					}
					if tt.hasDependency {
						task.DependsOn = []string{"task-0"}
					}
					initialState.Tasks = append(initialState.Tasks, task)
				}
			}

			// Add agent if busy
			if tt.agentBusy {
				initialState.Agents[tt.agentID] = models.Agent{
					Status:      models.AgentStatusWorking,
					CurrentTask: testhelpers.StringPtr("task-other"),
				}
			}

			// Write initial state
			bb := testhelpers.WriteInitialState(t, statePath, initialState)

			// Create existing worktree if needed
			if tt.hasWorktree && tt.taskID != "" && tt.taskID != "nonexistent" {
				wtDir := filepath.Join(tmpDir, ".worktrees", tt.taskID)
				if err := os.MkdirAll(wtDir, 0755); err != nil {
					t.Fatalf("Failed to create worktree directory: %v", err)
				}
				// Create a minimal git worktree
				if err := exec.Command("git", "-C", tmpDir, "worktree", "add", wtDir, "integration", "-b", "task/"+tt.taskID).Run(); err != nil {
					t.Fatalf("Failed to create worktree: %v", err)
				}
			}

			// Run command
			err := ClaimTaskCommand(tmpDir, tt.taskID, tt.agentID)

			// Check error
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				testhelpers.AssertErrorContains(t, err, tt.errContains)
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Verify task was claimed
			state, err := bb.Read()
			if err != nil {
				t.Fatalf("Failed to read state: %v", err)
			}

			var task *models.Task
			for i := range state.Tasks {
				if state.Tasks[i].ID == tt.taskID {
					task = &state.Tasks[i]
					break
				}
			}

			if task == nil {
				t.Fatalf("Task %s not found in state", tt.taskID)
			}

			// Check task status
			if task.Status != models.TaskStatusImplementing {
				t.Errorf("Expected task status IMPLEMENTING, got %s", task.Status)
			}

			// Check assigned_to
			if task.AssignedTo == nil || *task.AssignedTo != tt.agentID {
				t.Errorf("Expected assigned_to %s, got %v", tt.agentID, task.AssignedTo)
			}

			// Check worktree
			if tt.wantWorktree {
				if task.Worktree == nil {
					t.Errorf("Expected worktree to be set")
				} else {
					wtDir := filepath.Join(tmpDir, *task.Worktree)
					if _, err := os.Stat(wtDir); os.IsNotExist(err) {
						t.Errorf("Worktree directory not created: %s", wtDir)
					}
				}
			}

			// Check base_commit
			if task.BaseCommit == nil {
				t.Errorf("Expected base_commit to be set")
			}

			// Check lease_expires
			if task.LeaseExpires == nil {
				t.Errorf("Expected lease_expires to be set")
			} else if task.LeaseExpires.Before(now) {
				t.Errorf("Expected lease_expires to be in the future")
			}

			// Check iteration
			if tt.checkIteration {
				if task.Iteration == 0 {
					t.Errorf("Expected iteration to be set")
				} else if task.Iteration != 1 {
					t.Errorf("Expected iteration 1, got %d", task.Iteration)
				}
			}

			// Check agent status
			agent, exists := state.Agents[tt.agentID]
			if !exists {
				t.Errorf("Agent %s not found in state", tt.agentID)
			} else {
				if agent.Status != models.AgentStatusWorking {
					t.Errorf("Expected agent status WORKING, got %s", agent.Status)
				}
				if agent.CurrentTask == nil || *agent.CurrentTask != tt.taskID {
					t.Errorf("Expected agent current_task %s, got %v", tt.taskID, agent.CurrentTask)
				}
				if agent.LeaseExpires == nil {
					t.Errorf("Expected agent lease_expires to be set")
				}
				if agent.Heartbeat.IsZero() {
					t.Errorf("Expected agent heartbeat to be set")
				}
			}

			// Check history
			if len(task.History) == 0 {
				t.Errorf("Expected history entry")
			} else {
				lastEvent := task.History[len(task.History)-1]
				if lastEvent.Agent == nil || *lastEvent.Agent != tt.agentID {
					t.Errorf("Expected history agent %s, got %v", tt.agentID, lastEvent.Agent)
				}
				if lastEvent.Event == "" {
					t.Errorf("Expected history event to be set")
				}
			}
		})
	}
}

func TestClaimTaskCommandIntegration(t *testing.T) {
	// Create temp directory (project root)
	tmpDir := t.TempDir()

	// Initialize git repo
	testhelpers.SetupTestGitRepo(t, tmpDir)

	// Create .liza directory
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Create initial state with READY task
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
				ID:          "task-integration",
				Description: "Integration test task",
				Status:      models.TaskStatusReady,
				RolePair:    "coding-pair",
				Priority:    1,
				Created:     now,
				SpecRef:     "README.md",
				DoneWhen:    "Done",
				Scope:       "Test",
				History:     []models.TaskHistoryEntry{},
			},
		},
		Agents: make(map[string]models.Agent),
		Sprint: models.Sprint{
			ID:      "sprint-1",
			GoalRef: "goal-1",
			Scope: models.SprintScope{
				Planned: []string{"task-integration"},
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

	bb := testhelpers.WriteInitialState(t, statePath, initialState)

	// Run claim command
	if err := ClaimTaskCommand(tmpDir, "task-integration", "coder-1"); err != nil {
		t.Fatalf("ClaimTaskCommand failed: %v", err)
	}

	// Verify worktree was created and is functional
	wtDir := filepath.Join(tmpDir, ".worktrees", "task-integration")

	// Check that worktree directory exists
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		t.Fatalf("Worktree directory not created: %s", wtDir)
	}

	// Verify we can perform git operations in the worktree
	testFile := filepath.Join(wtDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file in worktree: %v", err)
	}

	// Verify git status works in worktree
	cmd := exec.Command("git", "-C", wtDir, "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to run git status in worktree: %v", err)
	}

	if !strings.Contains(string(output), "test.txt") {
		t.Errorf("Git status should show test.txt as untracked")
	}

	// Verify state was updated correctly
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := &state.Tasks[0]
	if task.Status != models.TaskStatusImplementing {
		t.Errorf("Expected task status IMPLEMENTING, got %s", task.Status)
	}
	if task.AssignedTo == nil || *task.AssignedTo != "coder-1" {
		t.Errorf("Expected assigned_to coder-1, got %v", task.AssignedTo)
	}
	if task.Worktree == nil {
		t.Error("Expected worktree to be set")
	}
	if task.BaseCommit == nil {
		t.Error("Expected base_commit to be set")
	}
	if task.LeaseExpires == nil {
		t.Error("Expected lease_expires to be set")
	}

	agent, exists := state.Agents["coder-1"]
	if !exists {
		t.Fatal("Agent coder-1 not found in state")
	}
	if agent.Status != models.AgentStatusWorking {
		t.Errorf("Expected agent status WORKING, got %s", agent.Status)
	}
	if agent.CurrentTask == nil || *agent.CurrentTask != "task-integration" {
		t.Errorf("Expected agent current_task task-integration, got %v", agent.CurrentTask)
	}
}
