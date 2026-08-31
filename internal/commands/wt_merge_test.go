package commands

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestWtMergeCommand(t *testing.T) {
	tests := []struct {
		name                string
		taskID              string
		taskStatus          models.TaskStatus
		reviewCommit        *string
		agentID             string
		setupWorktree       bool
		worktreeHeadMatch   bool
		createConflict      bool
		integrationTestFile string
		wantErr             bool
		errContains         string
		wantStatus          models.TaskStatus
	}{
		{
			name:              "successful merge - fast-forward",
			taskID:            "task-1",
			taskStatus:        models.TaskStatusApproved,
			reviewCommit:      testhelpers.StringPtr("abc1234"),
			agentID:           "coder-1",
			setupWorktree:     true,
			worktreeHeadMatch: true,
			wantErr:           false,
			wantStatus:        models.TaskStatusMerged,
		},
		{
			name:              "successful merge - merge commit",
			taskID:            "task-2",
			taskStatus:        models.TaskStatusApproved,
			reviewCommit:      testhelpers.StringPtr("def5678"),
			agentID:           "coder-2",
			setupWorktree:     true,
			worktreeHeadMatch: true,
			wantErr:           false,
			wantStatus:        models.TaskStatusMerged,
		},
		{
			name:              "merge conflict - marks INTEGRATION_FAILED",
			taskID:            "task-3",
			taskStatus:        models.TaskStatusApproved,
			reviewCommit:      testhelpers.StringPtr("ghi9012"),
			agentID:           "coder-3",
			setupWorktree:     true,
			worktreeHeadMatch: true,
			createConflict:    true,
			wantErr:           true,
			errContains:       "integration failed",
			wantStatus:        models.TaskStatusIntegrationFailed,
		},
		{
			name:          "task not APPROVED",
			taskID:        "task-4",
			taskStatus:    models.TaskStatusReadyForReview,
			reviewCommit:  testhelpers.StringPtr("jkl3456"),
			agentID:       "coder-4",
			setupWorktree: true,
			wantErr:       true,
			errContains:   "task must be in an approved state",
			wantStatus:    models.TaskStatusReadyForReview,
		},
		{
			name:          "missing agent ID",
			taskID:        "task-5",
			taskStatus:    models.TaskStatusApproved,
			reviewCommit:  testhelpers.StringPtr("mno7890"),
			agentID:       "",
			setupWorktree: true,
			wantErr:       true,
			errContains:   "agent ID is required",
			wantStatus:    models.TaskStatusApproved,
		},
		{
			name:          "task has no review_commit",
			taskID:        "task-6",
			taskStatus:    models.TaskStatusApproved,
			reviewCommit:  nil,
			agentID:       "coder-6",
			setupWorktree: false,
			wantErr:       true,
			errContains:   "task has no review_commit",
			wantStatus:    models.TaskStatusApproved,
		},
		{
			name:              "worktree HEAD does not match review_commit",
			taskID:            "task-7",
			taskStatus:        models.TaskStatusApproved,
			reviewCommit:      testhelpers.StringPtr("stu5678"),
			agentID:           "coder-7",
			setupWorktree:     true,
			worktreeHeadMatch: false,
			wantErr:           true,
			errContains:       "integration failed",
			wantStatus:        models.TaskStatusIntegrationFailed,
		},
		{
			name:          "task not found",
			taskID:        "nonexistent",
			agentID:       "coder-8",
			setupWorktree: false,
			wantErr:       true,
			errContains:   "task not found",
		},
		{
			name:          "empty task ID",
			taskID:        "",
			agentID:       "coder-9",
			setupWorktree: false,
			wantErr:       true,
			errContains:   "task ID is required",
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

			// Ensure we're on a branch (SetupTestGitRepo may create detached HEAD)
			cmd := exec.Command("git", "-C", tmpDir, "checkout", "-b", "integration")
			output, err := cmd.CombinedOutput()
			if err != nil && !strings.Contains(string(output), "already exists") {
				t.Fatalf("Failed to create integration branch: %v\nOutput: %s", err, output)
			}

			// If branch already exists, just checkout
			if strings.Contains(string(output), "already exists") {
				cmd2 := exec.Command("git", "-C", tmpDir, "checkout", "integration")
				if err := cmd2.Run(); err != nil {
					t.Fatalf("Failed to checkout integration branch: %v", err)
				}
			}

			// Create initial state
			now := time.Now().UTC()
			initialState := testhelpers.CreateValidState()
			initialState.Config.IntegrationBranch = "integration"
			initialState.Goal.SpecRef = "README.md"

			// Add task if not testing nonexistent task
			if tt.taskID != "nonexistent" && tt.taskID != "" {
				var worktreePath *string
				if tt.setupWorktree {
					wt := path.Join(".worktrees", tt.taskID)
					worktreePath = &wt
				}

				agent := "coder-1"
				baseCommit := "base123"
				task := models.Task{
					ID:           tt.taskID,
					Description:  "Test task",
					Status:       tt.taskStatus,
					RolePair:     "coding-pair",
					Priority:     1,
					Created:      now,
					SpecRef:      "README.md",
					DoneWhen:     "Done",
					Scope:        "Test",
					Worktree:     worktreePath,
					AssignedTo:   &agent,
					BaseCommit:   &baseCommit,
					ReviewCommit: tt.reviewCommit,
					History:      []models.TaskHistoryEntry{},
				}

				if tt.taskStatus == models.TaskStatusApproved {
					approvedBy := "code-reviewer-1"
					task.ApprovedBy = &approvedBy
				}

				initialState.Tasks = append(initialState.Tasks, task)
			}

			// Write initial state
			bb := testhelpers.WriteInitialState(t, stateFile, initialState)

			// Create worktree if needed
			var wtCommit string
			if tt.setupWorktree && tt.taskID != "" && tt.taskID != "nonexistent" {
				wtDir := filepath.Join(tmpDir, ".worktrees", tt.taskID)
				if err := os.MkdirAll(filepath.Dir(wtDir), 0755); err != nil {
					t.Fatalf("Failed to create worktrees directory: %v", err)
				}

				// Create worktree
				cmd := exec.Command("git", "-C", tmpDir, "worktree", "add", wtDir, "integration", "-b", "task/"+tt.taskID)
				if err := cmd.Run(); err != nil {
					t.Fatalf("Failed to create worktree: %v", err)
				}

				// Make a commit in the worktree
				testFile := filepath.Join(wtDir, "test-"+tt.taskID+".txt")
				if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
					t.Fatalf("Failed to write test file: %v", err)
				}

				cmd = exec.Command("git", "-C", wtDir, "add", ".")
				if err := cmd.Run(); err != nil {
					t.Fatalf("Failed to git add: %v", err)
				}

				cmd = exec.Command("git", "-C", wtDir, "commit", "-m", "Test commit for "+tt.taskID)
				if err := cmd.Run(); err != nil {
					t.Fatalf("Failed to git commit: %v", err)
				}

				// Get commit SHA
				cmd = exec.Command("git", "-C", wtDir, "rev-parse", "HEAD")
				output, err := cmd.Output()
				if err != nil {
					t.Fatalf("Failed to get commit SHA: %v", err)
				}
				wtCommit = strings.TrimSpace(string(output))

				// Normalize review_commit test fixture to a valid SHA.
				// - matching case: review_commit == worktree HEAD
				// - mismatch case: review_commit == integration HEAD (valid, but different)
				if tt.reviewCommit != nil {
					targetReviewCommit := wtCommit
					if !tt.worktreeHeadMatch {
						cmd = exec.Command("git", "-C", tmpDir, "rev-parse", "integration")
						integrationOut, err := cmd.Output()
						if err != nil {
							t.Fatalf("Failed to get integration HEAD: %v", err)
						}
						targetReviewCommit = strings.TrimSpace(string(integrationOut))
					}
					err := bb.Modify(func(s *models.State) error {
						for i := range s.Tasks {
							if s.Tasks[i].ID == tt.taskID {
								s.Tasks[i].ReviewCommit = &targetReviewCommit
								return nil
							}
						}
						return nil
					})
					if err != nil {
						t.Fatalf("Failed to update review_commit: %v", err)
					}
				}

				// Create conflict scenario if needed
				if tt.createConflict {
					// Checkout integration branch and make conflicting change
					cmd = exec.Command("git", "-C", tmpDir, "checkout", "integration")
					if err := cmd.Run(); err != nil {
						t.Fatalf("Failed to checkout integration: %v", err)
					}

					conflictFile := filepath.Join(tmpDir, "test-"+tt.taskID+".txt")
					if err := os.WriteFile(conflictFile, []byte("conflicting content"), 0644); err != nil {
						t.Fatalf("Failed to write conflict file: %v", err)
					}

					cmd = exec.Command("git", "-C", tmpDir, "add", ".")
					if err := cmd.Run(); err != nil {
						t.Fatalf("Failed to git add conflict: %v", err)
					}

					cmd = exec.Command("git", "-C", tmpDir, "commit", "-m", "Conflicting commit")
					if err := cmd.Run(); err != nil {
						t.Fatalf("Failed to commit conflict: %v", err)
					}
				}
			}

			// Run command (agent ID now passed as parameter)
			err = WtMergeCommand(tmpDir, tt.taskID, tt.agentID)

			// Check error
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				testhelpers.AssertErrorContains(t, err, tt.errContains)

				// Verify task status didn't change
				state, readErr := bb.Read()
				if readErr != nil {
					t.Fatalf("Failed to read state: %v", readErr)
				}

				var task *models.Task
				for i := range state.Tasks {
					if state.Tasks[i].ID == tt.taskID {
						task = &state.Tasks[i]
						break
					}
				}

				if task != nil && task.Status != tt.wantStatus {
					t.Errorf("Task status = %v, want %v", task.Status, tt.wantStatus)
				}
				return
			}

			testhelpers.AssertNoError(t, err)

			// Verify task status
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
				t.Fatal("Task not found in state")
			}

			if task.Status != tt.wantStatus {
				t.Errorf("Task status = %v, want %v", task.Status, tt.wantStatus)
			}

			// For successful merges
			if tt.wantStatus == models.TaskStatusMerged {
				// Verify worktree was deleted
				wtDir := filepath.Join(tmpDir, ".worktrees", tt.taskID)
				if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
					t.Errorf("Worktree directory should be deleted: %s", wtDir)
				}

				// Verify task.worktree is nil
				if task.Worktree != nil {
					t.Errorf("task.worktree should be nil, got %v", *task.Worktree)
				}

				// Verify merge_commit is set
				if task.MergeCommit == nil {
					t.Error("merge_commit should be set")
				}

				// Verify history entry exists
				foundMergeEvent := false
				for _, entry := range task.History {
					if entry.Event == "merged" {
						foundMergeEvent = true
						break
					}
				}
				if !foundMergeEvent {
					t.Error("History should contain 'merged' event")
				}

				// Verify commit is in integration branch
				cmd := exec.Command("git", "-C", tmpDir, "checkout", "integration")
				if err := cmd.Run(); err != nil {
					t.Fatalf("Failed to checkout integration: %v", err)
				}

				cmd = exec.Command("git", "-C", tmpDir, "log", "--oneline")
				output, err := cmd.Output()
				if err != nil {
					t.Fatalf("Failed to get git log: %v", err)
				}

				if !strings.Contains(string(output), "Test commit for "+tt.taskID) {
					t.Errorf("Integration branch should contain the merged commit")
				}
			}

			// For INTEGRATION_FAILED status
			if tt.wantStatus == models.TaskStatusIntegrationFailed {
				// Verify worktree still exists (for conflict resolution)
				wtDir := filepath.Join(tmpDir, ".worktrees", tt.taskID)
				if _, err := os.Stat(wtDir); os.IsNotExist(err) {
					t.Error("Worktree should still exist for conflict resolution")
				}

				// Verify task.worktree is still set
				if task.Worktree == nil {
					t.Error("task.worktree should still be set for INTEGRATION_FAILED")
				}

				// Verify failed_by includes the agent
				if len(task.FailedBy) == 0 {
					t.Error("failed_by should include the agent")
				}
			}
		})
	}
}

func TestWtMergeCommand_PreventsDuplicateFailedBy(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Setup git repo and liza directory
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Create integration branch
	cmd := exec.Command("git", "-C", tmpDir, "checkout", "-b", "integration")
	output, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(output), "already exists") {
		t.Fatalf("Failed to create integration branch: %v\nOutput: %s", err, output)
	}

	// If branch already exists, just checkout
	if strings.Contains(string(output), "already exists") {
		cmd2 := exec.Command("git", "-C", tmpDir, "checkout", "integration")
		if err := cmd2.Run(); err != nil {
			t.Fatalf("Failed to checkout integration branch: %v", err)
		}
	}

	// Create initial state
	now := time.Now().UTC()
	initialState := testhelpers.CreateValidState()
	initialState.Config.IntegrationBranch = "integration"
	initialState.Goal.SpecRef = "README.md"

	taskID := "duplicate-test"
	agentID := "agent-1"
	worktreePath := path.Join(".worktrees", taskID)
	baseCommit := "base123"
	reviewCommit := "review456"

	task := models.Task{
		ID:           taskID,
		Description:  "Test duplicate prevention",
		Status:       models.TaskStatusApproved,
		RolePair:     "coding-pair",
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Done",
		Scope:        "Test",
		Worktree:     &worktreePath,
		AssignedTo:   &agentID,
		BaseCommit:   &baseCommit,
		ReviewCommit: &reviewCommit,
		ApprovedBy:   testhelpers.StringPtr("code-reviewer-1"),
		FailedBy:     []string{"agent-1"}, // Already has agent-1
		History:      []models.TaskHistoryEntry{},
	}

	initialState.Tasks = append(initialState.Tasks, task)
	bb := testhelpers.WriteInitialState(t, stateFile, initialState)

	// Create worktree
	wtDir := filepath.Join(tmpDir, ".worktrees", taskID)
	if err := os.MkdirAll(filepath.Dir(wtDir), 0755); err != nil {
		t.Fatalf("Failed to create worktrees directory: %v", err)
	}

	cmd = exec.Command("git", "-C", tmpDir, "worktree", "add", wtDir, "integration", "-b", "task/"+taskID)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}

	// Make a commit in the worktree
	testFile := filepath.Join(wtDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cmd = exec.Command("git", "-C", wtDir, "add", ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}

	cmd = exec.Command("git", "-C", wtDir, "commit", "-m", "Test commit")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git commit: %v", err)
	}

	// Get commit SHA and update review_commit
	cmd = exec.Command("git", "-C", wtDir, "rev-parse", "HEAD")
	shaOutput, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to get commit SHA: %v", err)
	}
	wtCommit := strings.TrimSpace(string(shaOutput))

	err = bb.Modify(func(s *models.State) error {
		for i := range s.Tasks {
			if s.Tasks[i].ID == taskID {
				s.Tasks[i].ReviewCommit = &wtCommit
				return nil
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to update review_commit: %v", err)
	}

	// Create a merge conflict
	cmd = exec.Command("git", "-C", tmpDir, "checkout", "integration")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to checkout integration: %v", err)
	}

	conflictFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(conflictFile, []byte("conflicting content"), 0644); err != nil {
		t.Fatalf("Failed to write conflict file: %v", err)
	}

	cmd = exec.Command("git", "-C", tmpDir, "add", ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git add conflict: %v", err)
	}

	cmd = exec.Command("git", "-C", tmpDir, "commit", "-m", "Conflicting commit")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to commit conflict: %v", err)
	}

	// Run merge command (should fail due to conflict)
	err = WtMergeCommand(tmpDir, taskID, agentID)
	if err == nil {
		t.Fatal("Expected merge to fail due to conflict")
	}

	// Verify failed_by still contains only one agent-1
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	var updatedTask *models.Task
	for i := range state.Tasks {
		if state.Tasks[i].ID == taskID {
			updatedTask = &state.Tasks[i]
			break
		}
	}

	if updatedTask == nil {
		t.Fatal("Task not found in state")
	}

	// Check failed_by array
	if len(updatedTask.FailedBy) != 1 {
		t.Errorf("failed_by length = %d, want 1 (should not have duplicate)", len(updatedTask.FailedBy))
	}

	if len(updatedTask.FailedBy) > 0 && updatedTask.FailedBy[0] != agentID {
		t.Errorf("failed_by[0] = %v, want %v", updatedTask.FailedBy[0], agentID)
	}

	// Verify status is INTEGRATION_FAILED
	if updatedTask.Status != models.TaskStatusIntegrationFailed {
		t.Errorf("status = %v, want %v", updatedTask.Status, models.TaskStatusIntegrationFailed)
	}
}
