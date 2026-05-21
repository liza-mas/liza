package commands

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestSubmitForReviewCommand(t *testing.T) {
	tests := []struct {
		name          string
		taskID        string
		commitRef     string
		agentID       string
		setupState    func(*models.State)
		wantErr       bool
		wantErrMsg    string
		validateState func(*testing.T, *models.State)
	}{
		// NOTE: The "successful submission from IMPLEMENTING" test is now covered by
		// TestSubmitForReview_RebaseSuccess which properly sets up git repository and worktree
		{
			name:       "missing task ID",
			taskID:     "",
			commitRef:  "abc123",
			agentID:    "coder-1",
			wantErr:    true,
			wantErrMsg: "task ID is required",
		},
		{
			name:       "missing commit ref",
			taskID:     "t1",
			commitRef:  "",
			agentID:    "coder-1",
			wantErr:    true,
			wantErrMsg: "commit ref is required",
		},
		{
			name:       "missing agent ID",
			taskID:     "t1",
			commitRef:  "abc123",
			agentID:    "",
			wantErr:    true,
			wantErrMsg: "LIZA_AGENT_ID is required",
		},
		{
			name:       "task not found",
			taskID:     "nonexistent",
			commitRef:  "abc123",
			agentID:    "coder-1",
			wantErr:    true,
			wantErrMsg: "task not found",
			setupState: func(s *models.State) {
				s.Tasks = []models.Task{}
			},
		},
		{
			name:       "task not in IMPLEMENTING status",
			taskID:     "t1",
			commitRef:  "abc123",
			agentID:    "coder-1",
			wantErr:    true,
			wantErrMsg: "task t1 is not IMPLEMENTING",
			setupState: func(s *models.State) {
				s.Tasks = []models.Task{
					{
						ID:          "t1",
						Description: "Test task",
						Status:      models.TaskStatusReady,
						RolePair:    "coding-pair",
						Created:     time.Now().UTC(),
						History:     []models.TaskHistoryEntry{},
					},
				}
			},
		},
		{
			name:       "task not assigned to agent",
			taskID:     "t1",
			commitRef:  "abc123",
			agentID:    "coder-1",
			wantErr:    true,
			wantErrMsg: "task t1 is not assigned to agent coder-1",
			setupState: func(s *models.State) {
				differentAgent := "coder-2"
				leaseExpires := time.Now().UTC().Add(30 * time.Minute)
				s.Tasks = []models.Task{
					{
						ID:           "t1",
						Description:  "Test task",
						Status:       models.TaskStatusImplementing,
						RolePair:     "coding-pair",
						AssignedTo:   &differentAgent,
						LeaseExpires: &leaseExpires,
						Created:      time.Now().UTC(),
						History:      []models.TaskHistoryEntry{},
					},
				}
			},
		},
		{
			name:       "task assigned to different agent",
			taskID:     "t1",
			commitRef:  "abc123",
			agentID:    "coder-2",
			wantErr:    true,
			wantErrMsg: "task t1 is not assigned to agent coder-2",
			setupState: func(s *models.State) {
				agentID := "coder-1"
				leaseExpires := time.Now().UTC().Add(30 * time.Minute)
				s.Tasks = []models.Task{
					{
						ID:           "t1",
						Description:  "Test task",
						Status:       models.TaskStatusImplementing,
						RolePair:     "coding-pair",
						AssignedTo:   &agentID,
						LeaseExpires: &leaseExpires,
						Created:      time.Now().UTC(),
						History:      []models.TaskHistoryEntry{},
					},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			// Initialize state
			initialState := &models.State{
				Config: models.Config{
					IntegrationBranch: "integration",
					LeaseDuration:     1800,
				},
				Tasks:  []models.Task{},
				Agents: make(map[string]models.Agent),
			}

			// Setup state if provided
			if tt.setupState != nil {
				tt.setupState(initialState)
			}

			// Write initial state
			bb := testhelpers.WriteInitialState(t, statePath, initialState)

			// Execute command
			err := SubmitForReviewCommand(tmpDir, tt.taskID, tt.commitRef, tt.agentID)

			// Check error
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				} else {
					testhelpers.AssertErrorContains(t, err, tt.wantErrMsg)
				}
			} else {
				testhelpers.AssertNoError(t, err)
			}

			// Validate state if no error expected
			if !tt.wantErr && tt.validateState != nil {
				state, err := bb.Read()
				if err != nil {
					t.Fatalf("failed to read state: %v", err)
				}
				tt.validateState(t, state)
			}
		})
	}
}

func TestSubmitForReview_RebaseSuccess(t *testing.T) {
	// Setup test git repository
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Get current branch and ensure we're on integration
	testhelpers.MustGit(t, tmpDir, "checkout", "integration")

	// Create a worktree using git package
	g := git.New(tmpDir)
	taskID := "task-rebase-success"
	baseCommit, err := g.CreateWorktree(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wtPath := g.GetWorktreePath(taskID)

	// Make a commit in the worktree
	wtFile := filepath.Join(wtPath, "task-file.txt")
	if err := os.WriteFile(wtFile, []byte("task work\n"), 0644); err != nil {
		t.Fatal(err)
	}
	wtTestFile := filepath.Join(wtPath, "task_test.go")
	if err := os.WriteFile(wtTestFile, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "task-file.txt", "task_test.go")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Task commit")
	wtCommit := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")

	// Advance integration branch (in project root) - ensure we're on integration first
	testhelpers.MustGit(t, tmpDir, "checkout", "integration")
	integrationFile := filepath.Join(tmpDir, "integration-file.txt")
	if err := os.WriteFile(integrationFile, []byte("integration work\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, tmpDir, "add", "integration-file.txt")
	testhelpers.MustGit(t, tmpDir, "commit", "-m", "Integration commit")

	// Create state with IMPLEMENTING task
	agentID := "coder-1"
	leaseExpires := time.Now().UTC().Add(30 * time.Minute)
	worktree := g.GetWorktreeRelPath(taskID)
	currentTask := taskID
	initialState := &models.State{
		Config: models.Config{
			IntegrationBranch: "integration",
			LeaseDuration:     1800,
		},
		Tasks: []models.Task{
			{
				ID:           taskID,
				Description:  "Test task with rebase",
				Status:       models.TaskStatusImplementing,
				RolePair:     "coding-pair",
				AssignedTo:   &agentID,
				LeaseExpires: &leaseExpires,
				Worktree:     &worktree,
				BaseCommit:   &baseCommit,
				Iteration:    1,
				Created:      time.Now().UTC(),
				History: []models.TaskHistoryEntry{
					{
						Time:  time.Now().UTC(),
						Event: "pre_execution_checkpoint",
						Agent: &agentID,
						Extra: map[string]any{"intent": "test", "validation_plan": "test", "files_to_modify": []string{"task-file.txt"}},
					},
				},
			},
		},
		Agents: map[string]models.Agent{
			agentID: {
				Role:         "coder",
				Status:       models.AgentStatusWorking,
				CurrentTask:  &currentTask,
				LeaseExpires: &leaseExpires,
				Heartbeat:    time.Now().UTC(),
			},
		},
	}

	bb := testhelpers.WriteInitialState(t, statePath, initialState)

	// Execute submit command (should rebase automatically)
	err = SubmitForReviewCommand(tmpDir, taskID, wtCommit, agentID)
	testhelpers.AssertNoError(t, err)

	// Verify state was updated
	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)

	task := &state.Tasks[0]
	if task.Status != models.TaskStatusReadyForReview {
		t.Errorf("expected status READY_FOR_REVIEW, got %s", task.Status)
	}

	agent := state.Agents[agentID]
	if agent.Status != models.AgentStatusWaiting {
		t.Errorf("expected coder status WAITING after submission, got %s", agent.Status)
	}
	if agent.CurrentTask != nil {
		t.Errorf("expected coder current_task nil after submission, got %v", *agent.CurrentTask)
	}

	// Verify review_commit is set (and different from wtCommit due to rebase)
	if task.ReviewCommit == nil {
		t.Fatal("expected review_commit to be set")
	}

	// Verify both files exist in worktree (rebase was successful)
	if _, err := os.Stat(wtFile); os.IsNotExist(err) {
		t.Error("Task file should exist after rebase")
	}
	if _, err := os.Stat(filepath.Join(wtPath, "integration-file.txt")); os.IsNotExist(err) {
		t.Error("Integration file should exist in worktree after rebase")
	}
}

func TestSubmitForReviewCommand_PrintsScipWarnings(t *testing.T) {
	t.Setenv("LIZA_ENABLE_SCIP_SEARCH", "true")

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	installFailingSubmitReviewIndexer(t)

	testhelpers.MustGit(t, tmpDir, "checkout", "integration")
	g := git.New(tmpDir)
	taskID := "task-submit-scip-warning"
	baseCommit, err := g.CreateWorktree(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wtPath := g.GetWorktreePath(taskID)

	if err := os.WriteFile(filepath.Join(wtPath, "feature.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtPath, "feature_test.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "feature.go", "feature_test.go")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Add feature with tests")
	wtCommit := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")

	agentID := "coder-1"
	leaseExpires := time.Now().UTC().Add(30 * time.Minute)
	worktree := g.GetWorktreeRelPath(taskID)
	currentTask := taskID
	initialState := &models.State{
		Config: models.Config{
			IntegrationBranch: "integration",
			LeaseDuration:     1800,
			ScipSearch:        []string{"go"},
		},
		Tasks: []models.Task{
			{
				ID:           taskID,
				Description:  "Test task with scip warning",
				Status:       models.TaskStatusImplementing,
				RolePair:     "coding-pair",
				AssignedTo:   &agentID,
				LeaseExpires: &leaseExpires,
				Worktree:     &worktree,
				BaseCommit:   &baseCommit,
				Iteration:    1,
				Created:      time.Now().UTC(),
				History: []models.TaskHistoryEntry{
					{
						Time:  time.Now().UTC(),
						Event: models.TaskEventPreExecutionCheckpoint,
						Agent: &agentID,
						Extra: map[string]any{
							"intent":          "test scip warning output",
							"validation_plan": "submit succeeds while warning is printed",
							"files_to_modify": []string{"feature.go", "feature_test.go"},
						},
					},
				},
			},
		},
		Agents: map[string]models.Agent{
			agentID: {
				Role:         "coder",
				Status:       models.AgentStatusWorking,
				CurrentTask:  &currentTask,
				LeaseExpires: &leaseExpires,
				Heartbeat:    time.Now().UTC(),
			},
		},
	}
	testhelpers.WriteInitialState(t, statePath, initialState)

	stderr, err := captureSubmitReviewStderr(t, func() error {
		return SubmitForReviewCommand(tmpDir, taskID, wtCommit, agentID)
	})
	testhelpers.AssertNoError(t, err)
	if !strings.Contains(stderr, "warning: scip-search go:") || !strings.Contains(stderr, "fake scip-go failed") {
		t.Fatalf("stderr = %q, want scip-search warning", stderr)
	}
}

func TestSubmitForReview_RebaseConflict(t *testing.T) {
	// Setup test git repository
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Ensure we're on integration branch
	testhelpers.MustGit(t, tmpDir, "checkout", "integration")

	// Create a worktree
	g := git.New(tmpDir)
	taskID := "task-rebase-conflict"
	baseCommit, err := g.CreateWorktree(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wtPath := g.GetWorktreePath(taskID)

	// Modify README in worktree and add test file
	readmeFile := filepath.Join(wtPath, "README.md")
	if err := os.WriteFile(readmeFile, []byte("# Task version\nTask content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	conflictTestFile := filepath.Join(wtPath, "task_test.go")
	if err := os.WriteFile(conflictTestFile, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "README.md", "task_test.go")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Task README")
	wtCommit := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")

	// Modify README differently in integration (conflict) - ensure on integration branch
	testhelpers.MustGit(t, tmpDir, "checkout", "integration")
	readmeRoot := filepath.Join(tmpDir, "README.md")
	if err := os.WriteFile(readmeRoot, []byte("# Integration version\nIntegration content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, tmpDir, "add", "README.md")
	testhelpers.MustGit(t, tmpDir, "commit", "-m", "Integration README")

	// Create state with IMPLEMENTING task
	agentID := "coder-1"
	leaseExpires := time.Now().UTC().Add(30 * time.Minute)
	worktree := g.GetWorktreeRelPath(taskID)
	initialState := &models.State{
		Config: models.Config{
			IntegrationBranch: "integration",
			LeaseDuration:     1800,
		},
		Tasks: []models.Task{
			{
				ID:           taskID,
				Description:  "Test task with rebase conflict",
				Status:       models.TaskStatusImplementing,
				RolePair:     "coding-pair",
				AssignedTo:   &agentID,
				LeaseExpires: &leaseExpires,
				Worktree:     &worktree,
				BaseCommit:   &baseCommit,
				Iteration:    1,
				Created:      time.Now().UTC(),
				History: []models.TaskHistoryEntry{
					{
						Time:  time.Now().UTC(),
						Event: "pre_execution_checkpoint",
						Agent: &agentID,
						Extra: map[string]any{"intent": "test", "validation_plan": "test", "files_to_modify": []string{"task-file.txt"}},
					},
				},
			},
		},
		Agents: make(map[string]models.Agent),
	}

	bb := testhelpers.WriteInitialState(t, statePath, initialState)

	// Execute submit command (should fail due to rebase conflict)
	err = SubmitForReviewCommand(tmpDir, taskID, wtCommit, agentID)
	if err == nil {
		t.Fatal("expected error due to rebase conflict, got nil")
	}

	// Verify error message contains conflict information
	if !strings.Contains(err.Error(), "merge conflict") {
		t.Errorf("expected error to mention merge conflict, got: %v", err)
	}

	// Verify task transitions to INTEGRATION_FAILED (conflict caught early, before review)
	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)

	task := &state.Tasks[0]
	if task.Status != models.TaskStatusIntegrationFailed {
		t.Errorf("expected task to be INTEGRATION_FAILED after rebase conflict, got %s", task.Status)
	}

	// Verify review_commit is NOT set
	if task.ReviewCommit != nil {
		t.Errorf("expected review_commit to be nil after failed submission, got %v", task.ReviewCommit)
	}

	// Verify agent is released (no longer assigned)
	if task.AssignedTo != nil {
		t.Errorf("expected agent to be released after integration failure, got %v", *task.AssignedTo)
	}
}

func installFailingSubmitReviewIndexer(t *testing.T) {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho fake scip-go failed >&2\nexit 3\n"
	if err := os.WriteFile(filepath.Join(binDir, "scip-go"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func captureSubmitReviewStderr(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stderr pipe: %v", err)
	}
	os.Stderr = w
	runErr := fn()
	_ = w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read stderr pipe: %v", err)
	}
	return buf.String(), runErr
}

func TestSubmitForReview_ResolveIntegrationBranchFailure(t *testing.T) {
	// Setup test git repository
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Create a worktree
	g := git.New(tmpDir)
	taskID := "task-fetch-fail"
	baseCommit, err := g.CreateWorktree(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wtPath := g.GetWorktreePath(taskID)

	// Make a commit in worktree
	wtFile := filepath.Join(wtPath, "task-file.txt")
	if err := os.WriteFile(wtFile, []byte("task work\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fetchTestFile := filepath.Join(wtPath, "task_test.go")
	if err := os.WriteFile(fetchTestFile, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "task-file.txt", "task_test.go")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Task commit")
	wtCommit := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")

	// Create state with IMPLEMENTING task but WRONG integration branch name
	agentID := "coder-1"
	leaseExpires := time.Now().UTC().Add(30 * time.Minute)
	worktree := g.GetWorktreeRelPath(taskID)
	initialState := &models.State{
		Config: models.Config{
			IntegrationBranch: "nonexistent-branch", // This will fail integration branch resolution
			LeaseDuration:     1800,
		},
		Tasks: []models.Task{
			{
				ID:           taskID,
				Description:  "Test task with fetch failure",
				Status:       models.TaskStatusImplementing,
				RolePair:     "coding-pair",
				AssignedTo:   &agentID,
				LeaseExpires: &leaseExpires,
				Worktree:     &worktree,
				BaseCommit:   &baseCommit,
				Iteration:    1,
				Created:      time.Now().UTC(),
				History: []models.TaskHistoryEntry{
					{
						Time:  time.Now().UTC(),
						Event: "pre_execution_checkpoint",
						Agent: &agentID,
						Extra: map[string]any{"intent": "test", "validation_plan": "test", "files_to_modify": []string{"task-file.txt"}},
					},
				},
			},
		},
		Agents: make(map[string]models.Agent),
	}

	bb := testhelpers.WriteInitialState(t, statePath, initialState)

	// Execute submit command (should fail before rebase)
	err = SubmitForReviewCommand(tmpDir, taskID, wtCommit, agentID)
	if err == nil {
		t.Fatal("expected error due to missing integration branch, got nil")
	}

	// Verify error message mentions integration branch resolution
	if !strings.Contains(err.Error(), "failed to resolve integration branch HEAD") {
		t.Errorf("expected error to mention integration branch resolution failure, got: %v", err)
	}

	// Verify task remains in IMPLEMENTING status
	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)

	task := &state.Tasks[0]
	if task.Status != models.TaskStatusImplementing {
		t.Errorf("expected task to remain IMPLEMENTING after fetch failure, got %s", task.Status)
	}
}

func TestSubmitForReview_WorktreeGone(t *testing.T) {
	// Setup test git repository
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Create a worktree then delete it
	g := git.New(tmpDir)
	taskID := "task-no-worktree"
	baseCommit, err := g.CreateWorktree(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	// Delete the worktree directory manually
	wtPath := g.GetWorktreePath(taskID)
	if err := os.RemoveAll(wtPath); err != nil {
		t.Fatal(err)
	}

	// Create state with IMPLEMENTING task
	agentID := "coder-1"
	leaseExpires := time.Now().UTC().Add(30 * time.Minute)
	worktree := g.GetWorktreeRelPath(taskID)
	initialState := &models.State{
		Config: models.Config{
			IntegrationBranch: "integration",
			LeaseDuration:     1800,
		},
		Tasks: []models.Task{
			{
				ID:           taskID,
				Description:  "Test task with missing worktree",
				Status:       models.TaskStatusImplementing,
				RolePair:     "coding-pair",
				AssignedTo:   &agentID,
				LeaseExpires: &leaseExpires,
				Worktree:     &worktree,
				BaseCommit:   &baseCommit,
				Iteration:    1,
				Created:      time.Now().UTC(),
				History: []models.TaskHistoryEntry{
					{
						Time:  time.Now().UTC(),
						Event: "pre_execution_checkpoint",
						Agent: &agentID,
						Extra: map[string]any{"intent": "test", "validation_plan": "test", "files_to_modify": []string{"task-file.txt"}},
					},
				},
			},
		},
		Agents: make(map[string]models.Agent),
	}

	bb := testhelpers.WriteInitialState(t, statePath, initialState)

	// Execute submit command (should fail due to missing worktree)
	err = SubmitForReviewCommand(tmpDir, taskID, "abc1234", agentID)
	if err == nil {
		t.Fatal("expected error due to missing worktree, got nil")
	}

	// Verify error message mentions worktree
	if !strings.Contains(err.Error(), "worktree directory does not exist") {
		t.Errorf("expected error to mention missing worktree, got: %v", err)
	}

	// Verify task remains in IMPLEMENTING status
	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)

	task := &state.Tasks[0]
	if task.Status != models.TaskStatusImplementing {
		t.Errorf("expected task to remain IMPLEMENTING, got %s", task.Status)
	}
}

func TestSubmitForReview_DetachedHead(t *testing.T) {
	// Setup test git repository
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Create a worktree
	g := git.New(tmpDir)
	taskID := "task-detached-head"
	baseCommit, err := g.CreateWorktree(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wtPath := g.GetWorktreePath(taskID)

	// Make a commit
	wtFile := filepath.Join(wtPath, "task-file.txt")
	if err := os.WriteFile(wtFile, []byte("task work\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "task-file.txt")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Task commit")
	wtCommit := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")

	// Detach HEAD by checking out the commit directly
	testhelpers.MustGit(t, wtPath, "checkout", wtCommit)

	// Create state with IMPLEMENTING task
	agentID := "coder-1"
	leaseExpires := time.Now().UTC().Add(30 * time.Minute)
	worktree := g.GetWorktreeRelPath(taskID)
	initialState := &models.State{
		Config: models.Config{
			IntegrationBranch: "integration",
			LeaseDuration:     1800,
		},
		Tasks: []models.Task{
			{
				ID:           taskID,
				Description:  "Test task with detached HEAD",
				Status:       models.TaskStatusImplementing,
				RolePair:     "coding-pair",
				AssignedTo:   &agentID,
				LeaseExpires: &leaseExpires,
				Worktree:     &worktree,
				BaseCommit:   &baseCommit,
				Iteration:    1,
				Created:      time.Now().UTC(),
				History: []models.TaskHistoryEntry{
					{
						Time:  time.Now().UTC(),
						Event: "pre_execution_checkpoint",
						Agent: &agentID,
						Extra: map[string]any{"intent": "test", "validation_plan": "test", "files_to_modify": []string{"task-file.txt"}},
					},
				},
			},
		},
		Agents: make(map[string]models.Agent),
	}

	bb := testhelpers.WriteInitialState(t, statePath, initialState)

	// Execute submit command (should fail due to detached HEAD)
	err = SubmitForReviewCommand(tmpDir, taskID, wtCommit, agentID)
	if err == nil {
		t.Fatal("expected error due to detached HEAD, got nil")
	}

	// Verify error message mentions detached HEAD
	if !strings.Contains(err.Error(), "detached HEAD") {
		t.Errorf("expected error to mention detached HEAD, got: %v", err)
	}

	// Verify task remains in IMPLEMENTING status
	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)

	task := &state.Tasks[0]
	if task.Status != models.TaskStatusImplementing {
		t.Errorf("expected task to remain IMPLEMENTING, got %s", task.Status)
	}
}
