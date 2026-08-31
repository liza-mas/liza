package commands

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestWtDeleteCommand(t *testing.T) {
	tests := []struct {
		name        string
		taskID      string
		taskStatus  models.TaskStatus
		hasWorktree bool
		wantErr     bool
		errContains string
	}{
		{
			name:        "delete worktree for BLOCKED task",
			taskID:      "task-1",
			taskStatus:  models.TaskStatusBlocked,
			hasWorktree: true,
			wantErr:     false,
		},
		{
			name:        "delete worktree for ABANDONED task",
			taskID:      "task-2",
			taskStatus:  models.TaskStatusAbandoned,
			hasWorktree: true,
			wantErr:     false,
		},
		{
			name:        "delete worktree for SUPERSEDED task",
			taskID:      "task-3",
			taskStatus:  models.TaskStatusSuperseded,
			hasWorktree: true,
			wantErr:     false,
		},
		{
			name:        "delete worktree for MERGED task",
			taskID:      "task-4",
			taskStatus:  models.TaskStatusMerged,
			hasWorktree: true,
			wantErr:     false,
		},
		{
			name:        "cannot delete worktree for IMPLEMENTING task",
			taskID:      "task-5",
			taskStatus:  models.TaskStatusImplementing,
			hasWorktree: true,
			wantErr:     true,
			errContains: "cannot delete worktree",
		},
		{
			name:        "cannot delete worktree for READY task",
			taskID:      "task-6",
			taskStatus:  models.TaskStatusReady,
			hasWorktree: true,
			wantErr:     true,
			errContains: "cannot delete worktree",
		},
		{
			name:        "cannot delete worktree for READY_FOR_REVIEW task",
			taskID:      "task-7",
			taskStatus:  models.TaskStatusReadyForReview,
			hasWorktree: true,
			wantErr:     true,
			errContains: "cannot delete worktree",
		},
		{
			name:        "task has no worktree",
			taskID:      "task-8",
			taskStatus:  models.TaskStatusBlocked,
			hasWorktree: false,
			wantErr:     false,
		},
		{
			name:        "empty task ID",
			taskID:      "",
			taskStatus:  models.TaskStatusBlocked,
			hasWorktree: false,
			wantErr:     true,
			errContains: "task ID is required",
		},
		{
			name:        "nonexistent task",
			taskID:      "nonexistent",
			taskStatus:  models.TaskStatusBlocked,
			hasWorktree: false,
			wantErr:     true,
			errContains: "task not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory (project root)
			tmpDir := t.TempDir()

			// Setup git repo and liza directory
			testhelpers.SetupTestGitRepo(t, tmpDir)
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
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
				},
			}

			// Add task if not testing nonexistent task
			if tt.taskID != "nonexistent" && tt.taskID != "" {
				var worktreePath *string
				if tt.hasWorktree {
					wt := path.Join(".worktrees", tt.taskID)
					worktreePath = &wt
				}

				task := models.Task{
					ID:          tt.taskID,
					Description: "Test task",
					Status:      tt.taskStatus,
					Priority:    1,
					Created:     now,
					SpecRef:     "README.md",
					DoneWhen:    "Done",
					Scope:       "Test",
					Worktree:    worktreePath,
					History:     []models.TaskHistoryEntry{},
				}

				// Add required fields for specific statuses
				if tt.taskStatus == models.TaskStatusBlocked {
					reason := "Test block"
					task.BlockedReason = &reason
					task.BlockedQuestions = []string{"Question 1"}
				}
				if tt.taskStatus == models.TaskStatusSuperseded {
					reason := "Test rescope"
					task.RescopeReason = &reason
					task.SupersededBy = []string{"task-other"}
				}
				if tt.taskStatus == models.TaskStatusImplementing {
					agent := "coder-1"
					task.AssignedTo = &agent
					leaseExpires := now.Add(30 * time.Minute)
					task.LeaseExpires = &leaseExpires
					baseCommit := "abc1234"
					task.BaseCommit = &baseCommit
				}
				if tt.taskStatus == models.TaskStatusReadyForReview {
					agent := "coder-1"
					task.AssignedTo = &agent
					reviewCommit := "abc1234"
					task.ReviewCommit = &reviewCommit
				}

				initialState.Tasks = append(initialState.Tasks, task)
			}

			// Write initial state
			bb := testhelpers.WriteInitialState(t, stateFile, initialState)

			// Create worktree if needed
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
			err := WtDeleteCommand(tmpDir, tt.taskID)

			// Check error
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				testhelpers.AssertErrorContains(t, err, tt.errContains)
				return
			}

			testhelpers.AssertNoError(t, err)

			// Verify worktree was deleted
			if tt.hasWorktree {
				wtDir := filepath.Join(tmpDir, ".worktrees", tt.taskID)
				if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
					t.Errorf("Worktree directory still exists: %s", wtDir)
				}

				// Verify branch handling
				branchName := "task/" + tt.taskID
				cmd := exec.Command("git", "-C", tmpDir, "branch", "--list", branchName)
				output, err := cmd.Output()
				if err != nil {
					t.Errorf("Failed to check branch: %v", err)
				}
				branchExists := strings.Contains(string(output), branchName)
				if tt.taskStatus == models.TaskStatusSuperseded {
					// Superseded tasks preserve their branch for successors
					if !branchExists {
						t.Errorf("Branch %s should be preserved for SUPERSEDED task", branchName)
					}
				} else if branchExists {
					t.Errorf("Branch %s still exists", branchName)
				}
			}

			// Verify task.worktree was set to null in state
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

			if task != nil {
				if task.Worktree != nil {
					t.Errorf("task.worktree should be nil, got %v", *task.Worktree)
				}
			}
		})
	}
}

func TestWtDeleteCommandIntegration(t *testing.T) {
	// Create temp directory (project root)
	tmpDir := t.TempDir()

	// Setup git repo and liza directory
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Create initial state with BLOCKED task
	now := time.Now().UTC()
	worktreePath := ".worktrees/task-integration"
	blockedReason := "Integration test block"

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
				ID:               "task-integration",
				Description:      "Integration test task",
				Status:           models.TaskStatusBlocked,
				Priority:         1,
				Created:          now,
				SpecRef:          "README.md",
				DoneWhen:         "Done",
				Scope:            "Test",
				Worktree:         &worktreePath,
				BlockedReason:    &blockedReason,
				BlockedQuestions: []string{"How to proceed?"},
				History:          []models.TaskHistoryEntry{},
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
		},
	}

	bb := testhelpers.WriteInitialState(t, stateFile, initialState)

	// Create worktree using git wrapper
	gitWrapper := git.New(tmpDir)
	_, err := gitWrapper.CreateWorktree("task-integration", "integration")
	if err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}

	// Verify worktree exists before deletion
	wtDir := filepath.Join(tmpDir, ".worktrees", "task-integration")
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		t.Fatal("Worktree should exist before deletion")
	}

	// Run delete command
	if err := WtDeleteCommand(tmpDir, "task-integration"); err != nil {
		t.Fatalf("WtDeleteCommand failed: %v", err)
	}

	// Verify worktree was deleted
	if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
		t.Error("Worktree directory should be deleted")
	}

	// Verify branch was deleted
	exists, err := gitWrapper.BranchExists("task/task-integration")
	if err != nil {
		t.Fatalf("Failed to check branch: %v", err)
	}
	if exists {
		t.Error("Branch should be deleted")
	}

	// Verify task.worktree is null in state
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := &state.Tasks[0]
	if task.Worktree != nil {
		t.Errorf("task.worktree should be nil, got %v", *task.Worktree)
	}
}
