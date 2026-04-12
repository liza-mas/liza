package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestWtCreateCommand(t *testing.T) {
	tests := []struct {
		name        string
		taskID      string
		taskStatus  models.TaskStatus
		fresh       bool
		existingWT  bool
		wantErr     bool
		errContains string
	}{
		{
			name:       "create worktree for IMPLEMENTING task",
			taskID:     "task-1",
			taskStatus: models.TaskStatusImplementing,
			fresh:      false,
			existingWT: false,
			wantErr:    false,
		},
		{
			name:       "create worktree for CODE_PLANNING task",
			taskID:     "task-cp",
			taskStatus: models.TaskStatusCodePlanning,
			fresh:      false,
			existingWT: false,
			wantErr:    false,
		},
		{
			name:        "task not in executing state",
			taskID:      "task-2",
			taskStatus:  models.TaskStatusReady,
			fresh:       false,
			existingWT:  false,
			wantErr:     true,
			errContains: "not in an executing state",
		},
		{
			name:       "worktree already exists without fresh",
			taskID:     "task-3",
			taskStatus: models.TaskStatusImplementing,
			fresh:      false,
			existingWT: true,
			wantErr:    false, // Should succeed without error
		},
		{
			name:       "worktree already exists with fresh",
			taskID:     "task-4",
			taskStatus: models.TaskStatusImplementing,
			fresh:      true,
			existingWT: true,
			wantErr:    false,
		},
		{
			name:        "empty task ID",
			taskID:      "",
			taskStatus:  models.TaskStatusImplementing,
			fresh:       false,
			existingWT:  false,
			wantErr:     true,
			errContains: "task ID is required",
		},
		{
			name:        "nonexistent task",
			taskID:      "nonexistent",
			taskStatus:  models.TaskStatusImplementing,
			fresh:       false,
			existingWT:  false,
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
			agent := "coder-1"
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
				worktreePath := filepath.Join(".worktrees", tt.taskID)
				rolePair := "coding-pair"
				if tt.taskStatus == models.TaskStatusCodePlanning {
					rolePair = "code-planning-pair"
				}
				task := models.Task{
					ID:          tt.taskID,
					Description: "Test task",
					Status:      tt.taskStatus,
					RolePair:    rolePair,
					Priority:    1,
					Created:     now,
					SpecRef:     "README.md",
					DoneWhen:    "Done",
					Scope:       "Test",
					History:     []models.TaskHistoryEntry{},
				}

				if tt.taskStatus == models.TaskStatusImplementing {
					task.AssignedTo = &agent
					task.Worktree = &worktreePath
					leaseExpires := now.Add(30 * time.Minute)
					task.LeaseExpires = &leaseExpires
				}

				initialState.Tasks = append(initialState.Tasks, task)
			}

			// Write initial state
			bb := testhelpers.WriteInitialState(t, stateFile, initialState)

			// Create existing worktree if needed
			if tt.existingWT && tt.taskID != "" && tt.taskID != "nonexistent" {
				wtDir := filepath.Join(tmpDir, ".worktrees", tt.taskID)
				if err := os.MkdirAll(wtDir, 0755); err != nil {
					t.Fatalf("Failed to create existing worktree: %v", err)
				}
				// Create a minimal git worktree
				if err := exec.Command("git", "-C", tmpDir, "worktree", "add", wtDir, "integration", "-b", "task/"+tt.taskID).Run(); err != nil {
					t.Fatalf("Failed to create existing worktree: %v", err)
				}

				// Set base_commit in state since worktree already exists
				gitWrapper := git.New(tmpDir)
				baseCommit, err := gitWrapper.GetCommitSHA("integration")
				if err != nil {
					t.Fatalf("Failed to get base commit: %v", err)
				}

				err = bb.Modify(func(state *models.State) error {
					for i := range state.Tasks {
						if state.Tasks[i].ID == tt.taskID {
							state.Tasks[i].BaseCommit = &baseCommit
							return nil
						}
					}
					return fmt.Errorf("task not found")
				})
				if err != nil {
					t.Fatalf("Failed to set base_commit: %v", err)
				}
			}

			// Run command
			err := WtCreateCommand(tmpDir, tt.taskID, tt.fresh)

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

			// Verify worktree was created
			wtDir := filepath.Join(tmpDir, ".worktrees", tt.taskID)
			if _, err := os.Stat(wtDir); os.IsNotExist(err) {
				t.Errorf("Worktree directory not created: %s", wtDir)
			}

			// Verify branch was created
			branchName := "task/" + tt.taskID
			cmd := exec.Command("git", "-C", tmpDir, "branch", "--list", branchName)
			output, err := cmd.Output()
			if err != nil {
				t.Errorf("Failed to check branch: %v", err)
			}
			if !strings.Contains(string(output), branchName) {
				t.Errorf("Branch %s not created", branchName)
			}

			// Verify base_commit was updated in state
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

			if task.BaseCommit == nil {
				t.Errorf("base_commit not set in state")
			} else if *task.BaseCommit == "" {
				t.Errorf("base_commit is empty")
			}
		})
	}
}

func TestWtCreateCommand_PostWorktreeCmd(t *testing.T) {
	tmpDir := t.TempDir()

	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	agent := "coder-1"
	worktreePath := filepath.Join(".worktrees", "task-postcmd")
	leaseExpires := now.Add(30 * time.Minute)
	postCmd := "touch .post-worktree-ran"

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
				ID:           "task-postcmd",
				Description:  "Test task",
				Status:       models.TaskStatusImplementing,
				RolePair:     "coding-pair",
				Priority:     1,
				Created:      now,
				SpecRef:      "README.md",
				DoneWhen:     "Done",
				Scope:        "Test",
				AssignedTo:   &agent,
				Worktree:     &worktreePath,
				LeaseExpires: &leaseExpires,
				History:      []models.TaskHistoryEntry{},
			},
		},
		Agents: make(map[string]models.Agent),
		Sprint: models.Sprint{
			ID:      "sprint-1",
			GoalRef: "goal-1",
			Scope: models.SprintScope{
				Planned: []string{"task-postcmd"},
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
			PostWorktreeCmd:    &postCmd,
		},
	}

	testhelpers.WriteInitialState(t, stateFile, initialState)

	if err := WtCreateCommand(tmpDir, "task-postcmd", false); err != nil {
		t.Fatalf("WtCreateCommand failed: %v", err)
	}

	// Verify post-worktree command ran: the sentinel file should exist in the worktree
	wtDir := filepath.Join(tmpDir, ".worktrees", "task-postcmd")
	sentinelPath := filepath.Join(wtDir, ".post-worktree-ran")
	if _, err := os.Stat(sentinelPath); os.IsNotExist(err) {
		t.Error("post_worktree_cmd did not run: sentinel file not found in worktree")
	}
}

func TestWtCreateCommand_NoPostWorktreeCmd(t *testing.T) {
	// When PostWorktreeCmd is nil, no command should run and no warnings should appear.
	tmpDir := t.TempDir()

	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	agent := "coder-1"
	worktreePath := filepath.Join(".worktrees", "task-nocmd")
	leaseExpires := now.Add(30 * time.Minute)

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
				ID:           "task-nocmd",
				Description:  "Test task",
				Status:       models.TaskStatusImplementing,
				RolePair:     "coding-pair",
				Priority:     1,
				Created:      now,
				SpecRef:      "README.md",
				DoneWhen:     "Done",
				Scope:        "Test",
				AssignedTo:   &agent,
				Worktree:     &worktreePath,
				LeaseExpires: &leaseExpires,
				History:      []models.TaskHistoryEntry{},
			},
		},
		Agents: make(map[string]models.Agent),
		Sprint: models.Sprint{
			ID:      "sprint-1",
			GoalRef: "goal-1",
			Scope: models.SprintScope{
				Planned: []string{"task-nocmd"},
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
			// PostWorktreeCmd is nil — no command should run
		},
	}

	testhelpers.WriteInitialState(t, stateFile, initialState)

	err := WtCreateCommand(tmpDir, "task-nocmd", false)
	if err != nil {
		t.Fatalf("WtCreateCommand failed: %v", err)
	}

	// No sentinel file should exist
	wtDir := filepath.Join(tmpDir, ".worktrees", "task-nocmd")
	sentinelPath := filepath.Join(wtDir, ".post-worktree-ran")
	if _, err := os.Stat(sentinelPath); err == nil {
		t.Error("sentinel file should not exist when PostWorktreeCmd is nil")
	}
}

func TestWtCreateCommand_ProvisionClaudeConfig(t *testing.T) {
	tmpDir := t.TempDir()

	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Create Claude Code config files in the project root.
	settingsContent := []byte(`{"permissions":{"defaultMode":"acceptEdits"}}`)
	localContent := []byte(`{"alwaysThinkingEnabled":true}`)
	hookContent := []byte("#!/bin/sh\necho ok\n")

	claudeDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(filepath.Join(claudeDir, "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), settingsContent, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), localContent, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "hooks", "enforce-init.sh"), hookContent, 0755); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	agent := "coder-1"
	worktreePath := filepath.Join(".worktrees", "task-config")
	leaseExpires := now.Add(30 * time.Minute)

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
				ID:           "task-config",
				Description:  "Test task",
				Status:       models.TaskStatusImplementing,
				RolePair:     "coding-pair",
				Priority:     1,
				Created:      now,
				SpecRef:      "README.md",
				DoneWhen:     "Done",
				Scope:        "Test",
				AssignedTo:   &agent,
				Worktree:     &worktreePath,
				LeaseExpires: &leaseExpires,
				History:      []models.TaskHistoryEntry{},
			},
		},
		Agents: make(map[string]models.Agent),
		Sprint: models.Sprint{
			ID:      "sprint-1",
			GoalRef: "goal-1",
			Scope: models.SprintScope{
				Planned: []string{"task-config"},
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

	testhelpers.WriteInitialState(t, stateFile, initialState)

	if err := WtCreateCommand(tmpDir, "task-config", false); err != nil {
		t.Fatalf("WtCreateCommand failed: %v", err)
	}

	wtDir := filepath.Join(tmpDir, ".worktrees", "task-config")

	// Verify all config files were copied with correct content.
	for _, tc := range []struct {
		rel     string
		content []byte
		mode    os.FileMode
	}{
		{filepath.Join(".claude", "settings.json"), settingsContent, 0644},
		{filepath.Join(".claude", "settings.local.json"), localContent, 0644},
		{filepath.Join(".claude", "hooks", "enforce-init.sh"), hookContent, 0755},
	} {
		path := filepath.Join(wtDir, tc.rel)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: not copied: %v", tc.rel, err)
			continue
		}
		if string(got) != string(tc.content) {
			t.Errorf("%s: content mismatch: got %q, want %q", tc.rel, got, tc.content)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("%s: stat failed: %v", tc.rel, err)
			continue
		}
		if info.Mode().Perm() != tc.mode {
			t.Errorf("%s: mode %v, want %v", tc.rel, info.Mode().Perm(), tc.mode)
		}
	}
}

func TestWtCreateCommand_ProvisionClaudeConfig_NoFiles(t *testing.T) {
	// When no Claude Code config files exist, worktree creation succeeds
	// without errors or warnings from provisioning.
	tmpDir := t.TempDir()

	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	agent := "coder-1"
	worktreePath := filepath.Join(".worktrees", "task-noconfig")
	leaseExpires := now.Add(30 * time.Minute)

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
				ID:           "task-noconfig",
				Description:  "Test task",
				Status:       models.TaskStatusImplementing,
				RolePair:     "coding-pair",
				Priority:     1,
				Created:      now,
				SpecRef:      "README.md",
				DoneWhen:     "Done",
				Scope:        "Test",
				AssignedTo:   &agent,
				Worktree:     &worktreePath,
				LeaseExpires: &leaseExpires,
				History:      []models.TaskHistoryEntry{},
			},
		},
		Agents: make(map[string]models.Agent),
		Sprint: models.Sprint{
			ID:      "sprint-1",
			GoalRef: "goal-1",
			Scope: models.SprintScope{
				Planned: []string{"task-noconfig"},
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

	testhelpers.WriteInitialState(t, stateFile, initialState)

	err := WtCreateCommand(tmpDir, "task-noconfig", false)
	if err != nil {
		t.Fatalf("WtCreateCommand failed: %v", err)
	}

	// Verify no .claude/ leaked into the worktree.
	wtDir := filepath.Join(tmpDir, ".worktrees", "task-noconfig")
	for _, rel := range []string{".claude"} {
		if _, err := os.Stat(filepath.Join(wtDir, rel)); err == nil {
			t.Errorf("%s should not exist in worktree when absent from project root", rel)
		}
	}
}

func TestWtCreateCommandIntegration(t *testing.T) {
	// Create temp directory (project root)
	tmpDir := t.TempDir()

	// Setup git repo and liza directory
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Create initial state with IMPLEMENTING task
	now := time.Now().UTC()
	agent := "coder-1"
	worktreePath := ".worktrees/task-integration"
	leaseExpires := now.Add(30 * time.Minute)

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
				ID:           "task-integration",
				Description:  "Integration test task",
				Status:       models.TaskStatusImplementing,
				RolePair:     "coding-pair",
				Priority:     1,
				Created:      now,
				SpecRef:      "README.md",
				DoneWhen:     "Done",
				Scope:        "Test",
				AssignedTo:   &agent,
				Worktree:     &worktreePath,
				LeaseExpires: &leaseExpires,
				History:      []models.TaskHistoryEntry{},
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

	// Run command
	if err := WtCreateCommand(tmpDir, "task-integration", false); err != nil {
		t.Fatalf("WtCreateCommand failed: %v", err)
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

	// Verify base_commit is set correctly
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := &state.Tasks[0]
	if task.BaseCommit == nil {
		t.Fatal("base_commit not set")
	}

	// Verify base_commit is a valid git SHA
	gitWrapper := git.New(tmpDir)
	integrationSHA, err := gitWrapper.GetCommitSHA("integration")
	if err != nil {
		t.Fatalf("Failed to get integration SHA: %v", err)
	}

	if integrationSHA != *task.BaseCommit {
		t.Errorf("base_commit %s doesn't match integration branch %s", *task.BaseCommit, integrationSHA)
	}
}
