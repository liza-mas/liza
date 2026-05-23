package integration

// e2e_workflow_test.go contains end-to-end integration tests for complete task workflows.
//
// These tests verify that entire workflows function correctly when commands are
// used in sequence.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Test setup helper that returns a project directory and cleanup function
func setupTestProject(t *testing.T) (projectDir string, cleanup func()) {
	t.Helper()

	testhelpers.SetupGlobalLiza(t)

	tmpDir := t.TempDir()

	// Initialize git repository
	testhelpers.SetupTestGitRepo(t, tmpDir)

	// Store original directory to restore later
	originalDir, err := os.Getwd()
	testhelpers.AssertNoError(t, err)

	// Change to tmpDir
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	cleanup = func() {
		os.Chdir(originalDir)
	}

	return tmpDir, cleanup
}

// setupIntegrationTest performs the common integration test setup:
// init, create spec, add tasks, and return the blackboard.
func setupIntegrationTest(t *testing.T, projectDir string, taskIDs []string) (*db.Blackboard, string, string) {
	t.Helper()

	testhelpers.CreateCommittedSpecFileOnIntegration(t, projectDir, "feature.md", "# Feature")

	if err := commands.InitCommand("Test goal", "specs/feature.md", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	statePath := filepath.Join(projectDir, ".liza", "state.yaml")
	logPath := filepath.Join(projectDir, ".liza", "log.yaml")
	bb := db.New(statePath)

	for _, taskID := range taskIDs {
		taskInput := &commands.TaskInput{
			ID:          taskID,
			RolePair:    "coding-pair",
			Description: "Test feature " + taskID,
			DoneWhen:    "Done",
			Scope:       "Feature",
			Priority:    1,
			SpecRef:     "specs/feature.md",
			DependsOn:   []string{},
		}
		if err := commands.AddTaskCommand(statePath, logPath, taskInput, "orchestrator-1"); err != nil {
			t.Fatalf("AddTask %s failed: %v", taskID, err)
		}
	}

	return bb, statePath, logPath
}

// TestSimpleWorkflow tests: init -> add-task -> claim -> submit-for-review -> approve -> merge
func TestSimpleWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	projectDir, cleanup := setupTestProject(t)
	defer cleanup()

	// Steps 1-2: Initialize liza and add task
	t.Log("Steps 1-2: Initialize liza and add task")
	taskID := "task-1"
	bb, _, _ := setupIntegrationTest(t, projectDir, []string{taskID})

	// Verify initialization
	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	if state.Goal.Description != "Test goal" {
		t.Errorf("Expected goal description 'Test goal', got %s", state.Goal.Description)
	}

	// Verify task was added
	if len(state.Tasks) != 1 {
		t.Fatalf("Expected 1 task, got %d", len(state.Tasks))
	}
	if state.Tasks[0].Status != models.TaskStatusReady {
		t.Errorf("Expected task status READY, got %s", state.Tasks[0].Status)
	}

	// Step 3: Register an agent
	t.Log("Step 3: Register agent")
	agentID := "coder-1"
	testhelpers.RegisterTestAgent(t, bb, agentID, "coder")

	// Step 4: Claim the task
	t.Log("Step 4: Claim task")
	if err := commands.ClaimTaskCommand(projectDir, taskID, agentID); err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}

	// Verify task was claimed
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task := findTask(state.Tasks, taskID)
	if task == nil {
		t.Fatal("Task not found after claim")
	}
	if task.Status != models.TaskStatusImplementing {
		t.Errorf("Expected task status IMPLEMENTING, got %s", task.Status)
	}
	if task.AssignedTo == nil || *task.AssignedTo != agentID {
		t.Errorf("Expected task assigned to %s", agentID)
	}
	if task.Worktree == nil {
		t.Error("Expected worktree to be set after claim")
	}

	// Step 5: Make a commit and submit for review
	t.Log("Step 5: Make commit and submit for review")
	worktreePath := filepath.Join(projectDir, *task.Worktree)
	testFile := filepath.Join(worktreePath, "feature.go")
	if err := os.WriteFile(testFile, []byte("package main\n\nfunc Feature() {}\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	testTestFile := filepath.Join(worktreePath, "feature_test.go")
	if err := os.WriteFile(testTestFile, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	if err := exec.Command("git", "-C", worktreePath, "add", "feature.go", "feature_test.go").Run(); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}
	if err := exec.Command("git", "-C", worktreePath, "commit", "-m", "Implement feature with tests").Run(); err != nil {
		t.Fatalf("Failed to git commit: %v", err)
	}

	// Write pre-execution checkpoint (required for submission)
	if err := ops.WriteCheckpoint(projectDir, &ops.WriteCheckpointInput{
		TaskID: taskID, AgentID: agentID,
		Intent: "Implement feature", ValidationPlan: "go test ./...",
		FilesToModify: []string{"feature.go"},
	}); err != nil {
		t.Fatalf("WriteCheckpoint failed: %v", err)
	}

	// Get commit hash
	output, err := exec.Command("git", "-C", worktreePath, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("Failed to get commit hash: %v", err)
	}
	reviewCommit := string(output[:40])

	if err := commands.SubmitForReviewCommand(projectDir, taskID, reviewCommit, agentID); err != nil {
		t.Fatalf("SubmitForReview failed: %v", err)
	}

	// Verify task is ready for review
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, taskID)
	if task.Status != models.TaskStatusReadyForReview {
		t.Errorf("Expected task status READY_FOR_REVIEW, got %s", task.Status)
	}

	// Step 6: Register reviewer and approve
	t.Log("Step 6: Register reviewer and approve")
	reviewerID := "code-reviewer-1"
	testhelpers.RegisterTestAgent(t, bb, reviewerID, "code-reviewer")

	// Transition to REVIEWING (simulates supervisor reviewer claim)
	testhelpers.TransitionToReviewing(t, bb, taskID, reviewerID)

	if err := commands.SubmitVerdictCommand(projectDir, taskID, "APPROVED", "", reviewerID, ""); err != nil {
		t.Fatalf("SubmitVerdict failed: %v", err)
	}

	// Verify task is approved
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, taskID)
	if task.Status != models.TaskStatusApproved {
		t.Errorf("Expected task status APPROVED, got %s", task.Status)
	}

	// Step 7: Merge the task
	t.Log("Step 7: Merge task")
	// Set LIZA_AGENT_ID for merge command
	os.Setenv("LIZA_AGENT_ID", reviewerID)
	defer os.Unsetenv("LIZA_AGENT_ID")

	if err := commands.WtMergeCommand(projectDir, taskID, "code-reviewer-1"); err != nil {
		t.Fatalf("WtMerge failed: %v", err)
	}

	// Verify task is merged
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, taskID)
	if task.Status != models.TaskStatusMerged {
		t.Errorf("Expected task status MERGED, got %s", task.Status)
	}
	if task.MergeCommit == nil {
		t.Error("Expected merge commit to be set")
	}

	t.Log("✓ Simple workflow test passed")
}

// TestTaskDependencyWorkflow tests that task dependencies are enforced across the workflow
func TestTaskDependencyWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	projectDir, cleanup := setupTestProject(t)
	defer cleanup()

	// Setup: init + add task-1 (no deps)
	bb, statePath, logPath := setupIntegrationTest(t, projectDir, []string{"task-1"})

	// Add task-2 (depends on task-1) — custom deps, can't use helper
	task2Input := &commands.TaskInput{
		ID:          "task-2",
		RolePair:    "coding-pair",
		Description: "Dependent feature",
		DoneWhen:    "Done",
		Scope:       "Dependent",
		Priority:    1,
		SpecRef:     "specs/feature.md",
		DependsOn:   []string{"task-1"},
	}
	if err := commands.AddTaskCommand(statePath, logPath, task2Input, "orchestrator-1"); err != nil {
		t.Fatalf("AddTask task-2 failed: %v", err)
	}

	// Register agent
	agentID := "coder-1"
	testhelpers.RegisterTestAgent(t, bb, agentID, "coder")

	// Try to claim task-2 (should fail because task-1 not merged)
	t.Log("Attempting to claim task-2 before task-1 is done")
	err := commands.ClaimTaskCommand(projectDir, "task-2", agentID)
	if err == nil {
		t.Fatal("Expected error when claiming task with unmet dependencies")
	}
	testhelpers.AssertErrorContains(t, err, "unmet dependencies")

	// Claim task-1
	t.Log("Claiming task-1")
	if err := commands.ClaimTaskCommand(projectDir, "task-1", agentID); err != nil {
		t.Fatalf("ClaimTask task-1 failed: %v", err)
	}

	// Release agent from task-1 and mark it as merged (simplified for test)
	err = bb.Modify(func(state *models.State) error {
		// Release agent from task
		if agent, ok := state.Agents[agentID]; ok {
			agent.CurrentTask = nil
			agent.Status = models.AgentStatusWaiting
			state.Agents[agentID] = agent
		}
		// Mark task as merged
		for i := range state.Tasks {
			if state.Tasks[i].ID == "task-1" {
				state.Tasks[i].Status = models.TaskStatusMerged
				mergeCommit := "merge123"
				state.Tasks[i].MergeCommit = &mergeCommit
				state.Tasks[i].Worktree = nil
				break
			}
		}
		return nil
	})
	testhelpers.AssertNoError(t, err)

	// Now try to claim task-2 (should succeed)
	t.Log("Claiming task-2 after task-1 is merged")
	if err := commands.ClaimTaskCommand(projectDir, "task-2", agentID); err != nil {
		t.Fatalf("ClaimTask task-2 failed after dependency was met: %v", err)
	}

	// Verify task-2 is claimed
	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	task := findTask(state.Tasks, "task-2")
	if task.Status != models.TaskStatusImplementing {
		t.Errorf("Expected task-2 status IMPLEMENTING, got %s", task.Status)
	}

	t.Log("✓ Task dependency workflow test passed")
}

// TestIntegrationPipelineWithFindings tests the integration sub-pipeline when
// the analyst produces findings: init -> add integration task -> analyst claims ->
// sets output -> submits -> reviewer approves -> auto-transition creates fix tasks.
func TestIntegrationPipelineWithFindings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	projectDir, cleanup := setupTestProject(t)
	defer cleanup()

	// Step 1: Initialize project
	t.Log("Step 1: Initialize project")
	testhelpers.CreateCommittedSpecFileOnIntegration(t, projectDir, "feature.md", "# Feature")

	if err := commands.InitCommand("Integration test goal", "specs/feature.md", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	statePath := filepath.Join(projectDir, ".liza", "state.yaml")
	logPath := filepath.Join(projectDir, ".liza", "log.yaml")
	bb := db.New(statePath)

	// Step 2: Add an integration-pair task
	t.Log("Step 2: Add integration-pair task")
	taskID := "integ-1"
	taskInput := &commands.TaskInput{
		ID:          taskID,
		Type:        "integration",
		RolePair:    "integration-pair",
		Description: "Integration analysis for branch",
		DoneWhen:    "All integration issues identified",
		Scope:       "Full branch diff",
		Priority:    1,
		SpecRef:     "specs/feature.md",
		DependsOn:   []string{},
	}
	if err := commands.AddTaskCommand(statePath, logPath, taskInput, "orchestrator-1"); err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	// Verify task was added with correct type and role-pair
	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	task := findTask(state.Tasks, taskID)
	if task == nil {
		t.Fatal("Integration task not found")
	}
	if task.RolePair != "integration-pair" {
		t.Errorf("Expected role_pair integration-pair, got %s", task.RolePair)
	}
	if task.Type != models.TaskTypeIntegration {
		t.Errorf("Expected type integration, got %s", task.Type)
	}

	// Step 3: Register integration-analyst and claim
	t.Log("Step 3: Register integration-analyst and claim")
	analystID := "integration-analyst-1"
	testhelpers.RegisterTestAgent(t, bb, analystID, "integration-analyst")

	if err := commands.ClaimTaskCommand(projectDir, taskID, analystID); err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}

	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, taskID)
	if task.Status != "ANALYZING_INTEGRATION" {
		t.Errorf("Expected ANALYZING_INTEGRATION, got %s", task.Status)
	}
	if task.Worktree == nil {
		t.Fatal("Expected worktree to be set after claim")
	}

	// Step 4: Set output (findings) and submit for review
	t.Log("Step 4: Set output and submit for review")
	if err := ops.SetTaskOutput(projectDir, &ops.SetTaskOutputInput{
		TaskID:  taskID,
		AgentID: analystID,
		Output: []models.OutputEntry{
			{
				Desc:     "Fix type alignment in handler.go",
				DoneWhen: "Types match across handler and service layer",
				Scope:    "internal/handler.go",
				SpecRef:  "specs/feature.md",
			},
			{
				Desc:     "Add missing serialization tag",
				DoneWhen: "JSON tag present on Response.Status field",
				Scope:    "internal/models/response.go",
				SpecRef:  "specs/feature.md",
			},
		},
	}); err != nil {
		t.Fatalf("SetTaskOutput failed: %v", err)
	}

	// Write checkpoint (required before submission).
	// Integration analyst doesn't modify code files but checkpoint requires at least one entry.
	// Checkpoint doesn't validate file existence — it records intent, not actuality.
	// TODO: WriteCheckpoint's files_to_modify requirement assumes coding tasks;
	// integration/planning tasks that produce findings rather than code changes
	// shouldn't need this field. Consider relaxing for non-coding task types.
	if err := ops.WriteCheckpoint(projectDir, &ops.WriteCheckpointInput{
		TaskID: taskID, AgentID: analystID,
		Intent: "Integration analysis", ValidationPlan: "Review findings",
		FilesToModify: []string{"integration-analysis.md"},
	}); err != nil {
		t.Fatalf("WriteCheckpoint failed: %v", err)
	}

	// Get worktree HEAD for submit (analyst doesn't commit new code)
	worktreePath := filepath.Join(projectDir, *task.Worktree)
	output, err := exec.Command("git", "-C", worktreePath, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("Failed to get commit hash: %v", err)
	}
	reviewCommit := string(output[:len(output)-1]) // trim newline

	if err := commands.SubmitForReviewCommand(projectDir, taskID, reviewCommit, analystID); err != nil {
		t.Fatalf("SubmitForReview failed: %v", err)
	}

	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, taskID)
	if task.Status != "INTEGRATION_ANALYSIS_TO_REVIEW" {
		t.Errorf("Expected INTEGRATION_ANALYSIS_TO_REVIEW, got %s", task.Status)
	}

	// Step 5: Register integration-reviewer and approve
	t.Log("Step 5: Register integration-reviewer and approve")
	reviewerID := "integration-reviewer-1"
	testhelpers.RegisterTestAgent(t, bb, reviewerID, "integration-reviewer")

	// Transition to REVIEWING_INTEGRATION_ANALYSIS
	leaseExpires := state.Agents[reviewerID].LeaseExpires
	err = bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return nil
		}
		task.Status = "REVIEWING_INTEGRATION_ANALYSIS"
		task.ReviewingBy = &reviewerID
		task.ReviewLeaseExpires = leaseExpires

		if agent, ok := state.Agents[reviewerID]; ok {
			agent.Status = models.AgentStatusWorking
			agent.CurrentTask = &taskID
			state.Agents[reviewerID] = agent
		}
		return nil
	})
	testhelpers.AssertNoError(t, err)

	if err := commands.SubmitVerdictCommand(projectDir, taskID, "APPROVED", "", reviewerID, ""); err != nil {
		t.Fatalf("SubmitVerdict failed: %v", err)
	}

	// Verify task is approved (not clean — has output)
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, taskID)
	if task.Status != "INTEGRATION_ANALYSIS_APPROVED" {
		t.Errorf("Expected INTEGRATION_ANALYSIS_APPROVED, got %s", task.Status)
	}

	// Step 5b: Merge the approved integration task (all tasks go through merge now)
	t.Log("Step 5b: Merge approved integration task")
	if _, err := ops.MergeWorktree(projectDir, taskID, reviewerID); err != nil {
		t.Fatalf("MergeWorktree failed: %v", err)
	}
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, taskID)
	if task.Status != models.TaskStatusMerged {
		t.Errorf("Expected MERGED after merge, got %s", task.Status)
	}

	// Step 6: Execute auto-transitions — should create coding-pair fix tasks
	t.Log("Step 6: Execute auto-transitions")
	results, err := ops.ExecuteAvailableTransitions(projectDir, "auto")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 transition result, got %d", len(results))
	}
	result := results[0]
	if result.SourceTaskID != taskID {
		t.Errorf("Expected source task %s, got %s", taskID, result.SourceTaskID)
	}
	if result.TransitionName != "integration-to-fix" {
		t.Errorf("Expected transition integration-to-fix, got %s", result.TransitionName)
	}
	if len(result.ChildTaskIDs) != 2 {
		t.Fatalf("Expected 2 child tasks (one per finding), got %d", len(result.ChildTaskIDs))
	}

	// Verify child tasks are coding-pair tasks in DRAFT_CODE state
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	for _, childID := range result.ChildTaskIDs {
		child := findTask(state.Tasks, childID)
		if child == nil {
			t.Fatalf("Child task %s not found", childID)
		}
		if child.RolePair != "coding-pair" {
			t.Errorf("Child %s: expected role_pair coding-pair, got %s", childID, child.RolePair)
		}
		if child.Status != "DRAFT_CODE" {
			t.Errorf("Child %s: expected DRAFT_CODE, got %s", childID, child.Status)
		}
		if !slices.Contains(child.ParentTasks, taskID) {
			t.Errorf("Child %s: expected parent_tasks to contain %s, got %v", childID, taskID, child.ParentTasks)
		}
	}

	// Verify Goal.BaseCommit was snapshotted when coding-pair children were created.
	// This is the diff base the integration analyst uses (goal.base_commit..HEAD).
	if state.Goal.BaseCommit == nil {
		t.Error("Expected Goal.BaseCommit to be set after coding-pair children created")
	}
}

// TestIntegrationPipelineCleanScan tests the integration sub-pipeline when
// the analyst finds no issues: analyst submits without output -> reviewer
// approves -> routes to INTEGRATION_ANALYSIS_CLEAN terminal state.
func TestIntegrationPipelineCleanScan(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	projectDir, cleanup := setupTestProject(t)
	defer cleanup()

	// Setup: init + spec + integration task
	testhelpers.CreateCommittedSpecFileOnIntegration(t, projectDir, "feature.md", "# Feature")

	if err := commands.InitCommand("Clean scan goal", "specs/feature.md", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	statePath := filepath.Join(projectDir, ".liza", "state.yaml")
	logPath := filepath.Join(projectDir, ".liza", "log.yaml")
	bb := db.New(statePath)

	taskID := "integ-clean-1"
	taskInput := &commands.TaskInput{
		ID:          taskID,
		Type:        "integration",
		RolePair:    "integration-pair",
		Description: "Integration analysis — expecting clean",
		DoneWhen:    "All integration issues identified",
		Scope:       "Full branch diff",
		Priority:    1,
		SpecRef:     "specs/feature.md",
		DependsOn:   []string{},
	}
	if err := commands.AddTaskCommand(statePath, logPath, taskInput, "orchestrator-1"); err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	// Claim with integration-analyst
	analystID := "integration-analyst-1"
	testhelpers.RegisterTestAgent(t, bb, analystID, "integration-analyst")

	if err := commands.ClaimTaskCommand(projectDir, taskID, analystID); err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}

	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	task := findTask(state.Tasks, taskID)
	if task.Worktree == nil {
		t.Fatal("Expected worktree after claim")
	}

	// Submit without setting output (clean scan — no findings).
	// See TODO in TestIntegrationPipelineWithFindings re: files_to_modify for non-coding tasks.
	if err := ops.WriteCheckpoint(projectDir, &ops.WriteCheckpointInput{
		TaskID: taskID, AgentID: analystID,
		Intent: "Integration analysis", ValidationPlan: "Review findings",
		FilesToModify: []string{"integration-analysis.md"},
	}); err != nil {
		t.Fatalf("WriteCheckpoint failed: %v", err)
	}

	worktreePath := filepath.Join(projectDir, *task.Worktree)
	output, err := exec.Command("git", "-C", worktreePath, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("Failed to get commit hash: %v", err)
	}
	reviewCommit := string(output[:len(output)-1])

	if err := commands.SubmitForReviewCommand(projectDir, taskID, reviewCommit, analystID); err != nil {
		t.Fatalf("SubmitForReview failed: %v", err)
	}

	// Reviewer approves — should route to CLEAN (empty output)
	reviewerID := "integration-reviewer-1"
	testhelpers.RegisterTestAgent(t, bb, reviewerID, "integration-reviewer")

	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	leaseExpires := state.Agents[reviewerID].LeaseExpires
	err = bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return nil
		}
		task.Status = "REVIEWING_INTEGRATION_ANALYSIS"
		task.ReviewingBy = &reviewerID
		task.ReviewLeaseExpires = leaseExpires

		if agent, ok := state.Agents[reviewerID]; ok {
			agent.Status = models.AgentStatusWorking
			agent.CurrentTask = &taskID
			state.Agents[reviewerID] = agent
		}
		return nil
	})
	testhelpers.AssertNoError(t, err)

	if err := commands.SubmitVerdictCommand(projectDir, taskID, "APPROVED", "", reviewerID, ""); err != nil {
		t.Fatalf("SubmitVerdict failed: %v", err)
	}

	// Verify task routed to CLEAN terminal state
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, taskID)
	if task.Status != "INTEGRATION_ANALYSIS_CLEAN" {
		t.Errorf("Expected INTEGRATION_ANALYSIS_CLEAN, got %s", task.Status)
	}

	// Auto-transitions should produce nothing (clean is terminal)
	results, err := ops.ExecuteAvailableTransitions(projectDir, "auto")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Expected 0 transition results for clean scan, got %d", len(results))
	}
}

// TestArchitecturePairWorkflow tests the architecture-pair lifecycle end-to-end:
// add task → claim → set output → submit → review → reject → reclaim →
// resubmit → approve → proceed (architecture-to-code-plan transition).
// Verifies the architecture task visits all 6 declared states and that
// the per-subtask transition creates correct code-planning child tasks.
func TestArchitecturePairWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	projectDir, cleanup := setupTestProject(t)
	defer cleanup()

	// Step 1: Initialize project
	t.Log("Step 1: Initialize project")
	testhelpers.CreateCommittedSpecFileOnIntegration(t, projectDir, "arch-feature.md", "# Architecture Feature")

	if err := commands.InitCommand("Architecture test goal", "specs/arch-feature.md", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	statePath := filepath.Join(projectDir, ".liza", "state.yaml")
	logPath := filepath.Join(projectDir, ".liza", "log.yaml")
	bb := db.New(statePath)

	// Step 2: Add architecture task
	t.Log("Step 2: Add architecture task")
	taskID := "arch-1"
	taskInput := &commands.TaskInput{
		ID:          taskID,
		Type:        "architecture",
		RolePair:    "architecture-pair",
		Description: "Architecture for feature",
		DoneWhen:    "Architecture defined",
		Scope:       "Full feature",
		Priority:    1,
		SpecRef:     "specs/arch-feature.md",
		DependsOn:   []string{},
	}
	if err := commands.AddTaskCommand(statePath, logPath, taskInput, "orchestrator-1"); err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	// Verify: state 1 — DRAFT_ARCHITECTURE
	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	task := findTask(state.Tasks, taskID)
	if task == nil {
		t.Fatal("Architecture task not found")
	}
	if task.Status != "DRAFT_ARCHITECTURE" {
		t.Errorf("Expected DRAFT_ARCHITECTURE, got %s", task.Status)
	}
	if task.RolePair != "architecture-pair" {
		t.Errorf("Expected role_pair architecture-pair, got %s", task.RolePair)
	}
	if task.Type != models.TaskTypeArchitecture {
		t.Errorf("Expected type architecture, got %s", task.Type)
	}

	// Step 3: Register architect and claim
	t.Log("Step 3: Register architect and claim")
	architectID := "architect-1"
	testhelpers.RegisterTestAgent(t, bb, architectID, "architect")

	if err := commands.ClaimTaskCommand(projectDir, taskID, architectID); err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}

	// Verify: state 2 — ARCHITECTING
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, taskID)
	if task.Status != "ARCHITECTING" {
		t.Errorf("Expected ARCHITECTING, got %s", task.Status)
	}
	if task.Worktree == nil {
		t.Fatal("Expected worktree to be set after claim")
	}

	// Step 4: Set output with 2 code-planning entries
	t.Log("Step 4: Set output entries")
	if err := ops.SetTaskOutput(projectDir, &ops.SetTaskOutputInput{
		TaskID:  taskID,
		AgentID: architectID,
		Output: []models.OutputEntry{
			{
				Desc:     "Implement auth module",
				DoneWhen: "Auth module complete",
				Scope:    "internal/auth/",
				SpecRef:  "specs/arch-feature.md",
			},
			{
				Desc:     "Implement storage layer",
				DoneWhen: "Storage layer complete",
				Scope:    "internal/storage/",
				SpecRef:  "specs/arch-feature.md",
			},
		},
	}); err != nil {
		t.Fatalf("SetTaskOutput failed: %v", err)
	}

	// Step 5: Write checkpoint, make commit, submit for review
	t.Log("Step 5: Submit for review")
	if err := ops.WriteCheckpoint(projectDir, &ops.WriteCheckpointInput{
		TaskID: taskID, AgentID: architectID,
		Intent: "Architecture definition", ValidationPlan: "Review architecture",
		FilesToModify: []string{"specs/arch-plan/feature.md"},
	}); err != nil {
		t.Fatalf("WriteCheckpoint failed: %v", err)
	}

	worktreePath := filepath.Join(projectDir, *task.Worktree)
	archDoc := filepath.Join(worktreePath, "arch-plan.md")
	if err := os.WriteFile(archDoc, []byte("# Architecture Plan\n"), 0644); err != nil {
		t.Fatalf("Failed to create arch doc: %v", err)
	}
	if err := exec.Command("git", "-C", worktreePath, "add", "arch-plan.md").Run(); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}
	if err := exec.Command("git", "-C", worktreePath, "commit", "-m", "Define architecture").Run(); err != nil {
		t.Fatalf("Failed to git commit: %v", err)
	}

	output, err := exec.Command("git", "-C", worktreePath, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("Failed to get commit hash: %v", err)
	}
	reviewCommit := string(output[:len(output)-1])

	if err := commands.SubmitForReviewCommand(projectDir, taskID, reviewCommit, architectID); err != nil {
		t.Fatalf("SubmitForReview failed: %v", err)
	}

	// Verify: state 3 — ARCHITECTURE_TO_REVIEW
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, taskID)
	if task.Status != "ARCHITECTURE_TO_REVIEW" {
		t.Errorf("Expected ARCHITECTURE_TO_REVIEW, got %s", task.Status)
	}

	// Step 6: Register architecture-reviewer and transition to reviewing
	t.Log("Step 6: Reviewer claims for review")
	reviewerID := "architecture-reviewer-1"
	testhelpers.RegisterTestAgent(t, bb, reviewerID, "architecture-reviewer")

	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	leaseExpires := state.Agents[reviewerID].LeaseExpires
	err = bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return nil
		}
		task.Status = "REVIEWING_ARCHITECTURE"
		task.ReviewingBy = &reviewerID
		task.ReviewLeaseExpires = leaseExpires

		if agent, ok := state.Agents[reviewerID]; ok {
			agent.Status = models.AgentStatusWorking
			agent.CurrentTask = &taskID
			state.Agents[reviewerID] = agent
		}
		return nil
	})
	testhelpers.AssertNoError(t, err)

	// Verify: state 4 — REVIEWING_ARCHITECTURE
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, taskID)
	if task.Status != "REVIEWING_ARCHITECTURE" {
		t.Errorf("Expected REVIEWING_ARCHITECTURE, got %s", task.Status)
	}

	// Step 7: Reject — covers state 5 (ARCHITECTURE_REJECTED)
	t.Log("Step 7: Reject architecture")
	if err := commands.SubmitVerdictCommand(projectDir, taskID, "REJECTED", "Needs more detail on interfaces", reviewerID, ""); err != nil {
		t.Fatalf("SubmitVerdict (REJECTED) failed: %v", err)
	}

	// Verify: state 5 — ARCHITECTURE_REJECTED
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, taskID)
	if task.Status != "ARCHITECTURE_REJECTED" {
		t.Errorf("Expected ARCHITECTURE_REJECTED, got %s", task.Status)
	}

	// Step 8: Re-claim after rejection
	t.Log("Step 8: Re-claim after rejection")
	if err := commands.ClaimTaskCommand(projectDir, taskID, architectID); err != nil {
		t.Fatalf("Re-claim after rejection failed: %v", err)
	}

	// Verify: back to ARCHITECTING
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, taskID)
	if task.Status != "ARCHITECTING" {
		t.Errorf("Expected ARCHITECTING after re-claim, got %s", task.Status)
	}

	// Step 9: Write new checkpoint and re-submit
	t.Log("Step 9: Re-submit for review")
	if err := ops.WriteCheckpoint(projectDir, &ops.WriteCheckpointInput{
		TaskID: taskID, AgentID: architectID,
		Intent: "Revised architecture with interface details", ValidationPlan: "Review architecture",
		FilesToModify: []string{"specs/arch-plan/feature.md"},
	}); err != nil {
		t.Fatalf("WriteCheckpoint (round 2) failed: %v", err)
	}

	// Make another commit in the worktree
	revisedDoc := filepath.Join(worktreePath, "arch-plan.md")
	if err := os.WriteFile(revisedDoc, []byte("# Architecture Plan\n\n## Interfaces\n"), 0644); err != nil {
		t.Fatalf("Failed to update arch doc: %v", err)
	}
	if err := exec.Command("git", "-C", worktreePath, "add", "arch-plan.md").Run(); err != nil {
		t.Fatalf("Failed to git add (round 2): %v", err)
	}
	if err := exec.Command("git", "-C", worktreePath, "commit", "-m", "Add interface details").Run(); err != nil {
		t.Fatalf("Failed to git commit (round 2): %v", err)
	}

	output, err = exec.Command("git", "-C", worktreePath, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("Failed to get commit hash (round 2): %v", err)
	}
	reviewCommit = string(output[:len(output)-1])

	if err := commands.SubmitForReviewCommand(projectDir, taskID, reviewCommit, architectID); err != nil {
		t.Fatalf("SubmitForReview (round 2) failed: %v", err)
	}

	// Verify: ARCHITECTURE_TO_REVIEW again
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, taskID)
	if task.Status != "ARCHITECTURE_TO_REVIEW" {
		t.Errorf("Expected ARCHITECTURE_TO_REVIEW (round 2), got %s", task.Status)
	}

	// Step 10: Reviewer approves this time
	t.Log("Step 10: Approve architecture")
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	leaseExpires = state.Agents[reviewerID].LeaseExpires
	err = bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return nil
		}
		task.Status = "REVIEWING_ARCHITECTURE"
		task.ReviewingBy = &reviewerID
		task.ReviewLeaseExpires = leaseExpires

		if agent, ok := state.Agents[reviewerID]; ok {
			agent.Status = models.AgentStatusWorking
			agent.CurrentTask = &taskID
			state.Agents[reviewerID] = agent
		}
		return nil
	})
	testhelpers.AssertNoError(t, err)

	if err := commands.SubmitVerdictCommand(projectDir, taskID, "APPROVED", "", reviewerID, ""); err != nil {
		t.Fatalf("SubmitVerdict (APPROVED) failed: %v", err)
	}

	// Verify: state 6 — ARCHITECTURE_APPROVED
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, taskID)
	if task.Status != "ARCHITECTURE_APPROVED" {
		t.Errorf("Expected ARCHITECTURE_APPROVED, got %s", task.Status)
	}

	// Step 11: Merge the task so Proceed can execute
	t.Log("Step 11: Merge architecture task")
	os.Setenv("LIZA_AGENT_ID", reviewerID)
	defer os.Unsetenv("LIZA_AGENT_ID")

	if err := commands.WtMergeCommand(projectDir, taskID, reviewerID); err != nil {
		t.Fatalf("WtMerge failed: %v", err)
	}

	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, taskID)
	if task.Status != models.TaskStatusMerged {
		t.Errorf("Expected MERGED after merge, got %s", task.Status)
	}

	// Step 12: Execute architecture-to-code-plan transition via ExecuteAvailableTransitions.
	// architecture-to-code-plan is a manual transition; ExecuteAvailableTransitions handles
	// MERGED tasks by overriding requiredStatus (unlike Proceed which expects the from-phase status).
	t.Log("Step 12: Execute architecture-to-code-plan transition")
	results, err := ops.ExecuteAvailableTransitions(projectDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions failed: %v", err)
	}

	// Step 13: Assert child tasks
	t.Log("Step 13: Verify child tasks")
	if len(results) != 1 {
		t.Fatalf("Expected 1 transition result, got %d", len(results))
	}
	result := results[0]
	if result.SourceTaskID != taskID {
		t.Errorf("Expected source task %s, got %s", taskID, result.SourceTaskID)
	}
	if result.TransitionName != "architecture-to-code-plan" {
		t.Errorf("Expected transition name architecture-to-code-plan, got %s", result.TransitionName)
	}
	if len(result.ChildTaskIDs) != 2 {
		t.Fatalf("Expected 2 child tasks, got %d", len(result.ChildTaskIDs))
	}

	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)

	// Verify transitions_executed on the architecture task
	task = findTask(state.Tasks, taskID)
	if !task.TransitionsExecuted["architecture-to-code-plan"] {
		t.Error("Expected transitions_executed to contain architecture-to-code-plan")
	}

	// Verify each child task
	for _, childID := range result.ChildTaskIDs {
		child := findTask(state.Tasks, childID)
		if child == nil {
			t.Fatalf("Child task %s not found", childID)
		}
		if child.RolePair != "code-planning-pair" {
			t.Errorf("Child %s: expected role_pair code-planning-pair, got %s", childID, child.RolePair)
		}
		if child.Status != "DRAFT_CODING_PLAN" {
			t.Errorf("Child %s: expected DRAFT_CODING_PLAN, got %s", childID, child.Status)
		}
		if !slices.Contains(child.ParentTasks, taskID) {
			t.Errorf("Child %s: expected ParentTasks to contain %s, got %v", childID, taskID, child.ParentTasks)
		}
		if child.SpecRef != "specs/arch-feature.md" {
			t.Errorf("Child %s: expected spec_ref specs/arch-feature.md, got %s", childID, child.SpecRef)
		}
	}

	t.Log("TestArchitecturePairWorkflow passed — all 6 states visited, child tasks verified")
}

// walkTaskToMerged is a helper that walks a task through the full lifecycle:
// claim → checkpoint → commit → submit → reviewer claims → approve → merge.
// The task must already exist in the initial state of its role-pair.
func walkTaskToMerged(t *testing.T, projectDir string, bb *db.Blackboard, taskID, doerID, doerRole, reviewerID, reviewerRole string, reviewingStatus models.TaskStatus) {
	t.Helper()

	// Register agents
	testhelpers.RegisterTestAgent(t, bb, doerID, doerRole)
	testhelpers.RegisterTestAgent(t, bb, reviewerID, reviewerRole)

	// Claim
	if err := commands.ClaimTaskCommand(projectDir, taskID, doerID); err != nil {
		t.Fatalf("ClaimTask %s failed: %v", taskID, err)
	}

	// Write checkpoint
	if err := ops.WriteCheckpoint(projectDir, &ops.WriteCheckpointInput{
		TaskID: taskID, AgentID: doerID,
		Intent: "Implement " + taskID, ValidationPlan: "tests pass",
		FilesToModify: []string{"work.md"},
	}); err != nil {
		t.Fatalf("WriteCheckpoint %s failed: %v", taskID, err)
	}

	// Read worktree path, create a commit
	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	task := findTask(state.Tasks, taskID)
	if task == nil || task.Worktree == nil {
		t.Fatalf("Task %s has no worktree after claim", taskID)
	}
	worktreePath := filepath.Join(projectDir, *task.Worktree)
	workFile := filepath.Join(worktreePath, taskID+".go")
	if err := os.WriteFile(workFile, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("Failed to create work file for %s: %v", taskID, err)
	}
	testFile := filepath.Join(worktreePath, taskID+"_test.go")
	if err := os.WriteFile(testFile, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file for %s: %v", taskID, err)
	}
	if err := exec.Command("git", "-C", worktreePath, "add", taskID+".go", taskID+"_test.go").Run(); err != nil {
		t.Fatalf("git add for %s failed: %v", taskID, err)
	}
	if err := exec.Command("git", "-C", worktreePath, "commit", "-m", "Work on "+taskID).Run(); err != nil {
		t.Fatalf("git commit for %s failed: %v", taskID, err)
	}

	// Submit for review
	output, err := exec.Command("git", "-C", worktreePath, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse for %s failed: %v", taskID, err)
	}
	commitSHA := string(output[:len(output)-1])
	if err := commands.SubmitForReviewCommand(projectDir, taskID, commitSHA, doerID); err != nil {
		t.Fatalf("SubmitForReview %s failed: %v", taskID, err)
	}

	// Reviewer claims (transition to reviewing)
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	leaseExpires := state.Agents[reviewerID].LeaseExpires
	err = bb.Modify(func(s *models.State) error {
		tk := s.FindTask(taskID)
		if tk == nil {
			return nil
		}
		tk.Status = reviewingStatus
		tk.ReviewingBy = &reviewerID
		tk.ReviewLeaseExpires = leaseExpires
		if agent, ok := s.Agents[reviewerID]; ok {
			agent.Status = models.AgentStatusWorking
			agent.CurrentTask = &taskID
			s.Agents[reviewerID] = agent
		}
		return nil
	})
	testhelpers.AssertNoError(t, err)

	// Approve
	if err := commands.SubmitVerdictCommand(projectDir, taskID, "APPROVED", "", reviewerID, ""); err != nil {
		t.Fatalf("SubmitVerdict APPROVED %s failed: %v", taskID, err)
	}

	// Merge
	os.Setenv("LIZA_AGENT_ID", reviewerID)
	if err := commands.WtMergeCommand(projectDir, taskID, reviewerID); err != nil {
		t.Fatalf("WtMerge %s failed: %v", taskID, err)
	}
	os.Unsetenv("LIZA_AGENT_ID")

	// Verify MERGED
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, taskID)
	if task.Status != models.TaskStatusMerged {
		t.Fatalf("Expected %s to be MERGED, got %s", taskID, task.Status)
	}
}

func TestMasterPlanningLifecycleAcceptance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	projectDir, cleanup := setupTestProject(t)
	defer cleanup()

	testhelpers.CreateCommittedSpecFileOnIntegration(t, projectDir, "master.md", "# Master Planning Feature")
	if err := commands.InitCommand("Master planning acceptance", "specs/master.md", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	statePath := filepath.Join(projectDir, ".liza", "state.yaml")
	logPath := filepath.Join(projectDir, ".liza", "log.yaml")
	bb := db.New(statePath)

	masterID := "architecture-master-1"
	if err := commands.AddTaskCommand(statePath, logPath, &commands.TaskInput{
		ID:          masterID,
		Type:        "architecture",
		RolePair:    "architecture-main-pair",
		Description: "Decompose architecture work",
		DoneWhen:    "Specialized architecture tasks are defined",
		Scope:       "Architecture decomposition",
		Priority:    1,
		SpecRef:     "specs/master.md",
	}, "orchestrator-1"); err != nil {
		t.Fatalf("AddTask master failed: %v", err)
	}

	architectID := "architect-1"
	testhelpers.RegisterTestAgent(t, bb, architectID, "architect")
	if err := commands.ClaimTaskCommand(projectDir, masterID, architectID); err != nil {
		t.Fatalf("ClaimTask master failed: %v", err)
	}

	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	masterDoerPrompt := buildIntegrationTaskPrompt(t, projectDir, state, masterID, "architect", architectID)
	assertContainsAll(t, masterDoerPrompt,
		"=== MASTER DECOMPOSITION MANDATE ===",
		"Master Output Contract properties 1-6",
		"1. Non-overlapping scopes.",
		"2. Interface ownership.",
		"3. Shared-file ownership.",
		"4. Dependency ordering.",
		"5. Inherited constraints.",
		"6. Completeness.",
		"Systemic Decomposition Review",
		"systemic-thinking",
		"arch_ref",
	)
	assertNotContains(t, masterDoerPrompt, "=== MASTER DECOMPOSITION REVIEW ===")

	archRef := "specs/arch-plan/master-architecture.md"
	if err := ops.SetTaskOutput(projectDir, &ops.SetTaskOutputInput{
		TaskID:  masterID,
		AgentID: architectID,
		Output:  masterArchitectureOutput(archRef),
	}); err != nil {
		t.Fatalf("SetTaskOutput master failed: %v", err)
	}

	if err := ops.WriteCheckpoint(projectDir, &ops.WriteCheckpointInput{
		TaskID:         masterID,
		AgentID:        architectID,
		Intent:         "Architecture decomposition",
		ValidationPlan: "Review decomposition",
		FilesToModify:  []string{"specs/arch-plan/master-architecture.md"},
	}); err != nil {
		t.Fatalf("WriteCheckpoint master failed: %v", err)
	}

	reviewCommit := commitTaskWorktreeFile(t, projectDir, bb, masterID, filepath.Join("specs", "arch-plan", "master-architecture.md"), "# Master Architecture\n")
	if err := commands.SubmitForReviewCommand(projectDir, masterID, reviewCommit, architectID); err != nil {
		t.Fatalf("SubmitForReview master failed: %v", err)
	}

	firstReviewerID := "architecture-reviewer-1"
	testhelpers.RegisterTestAgent(t, bb, firstReviewerID, "architecture-reviewer")
	claimReviewManually(t, bb, masterID, firstReviewerID, models.TaskStatus("REVIEWING_ARCHITECTURE_MAIN"))

	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	masterReviewerPrompt := buildIntegrationTaskPrompt(t, projectDir, state, masterID, "architecture-reviewer", firstReviewerID)
	assertContainsAll(t, masterReviewerPrompt,
		"=== MASTER DECOMPOSITION REVIEW ===",
		"Invoke `systemic-thinking` before submitting a verdict",
		"missing `arch_ref`",
		"missing typed decomposition metadata",
		"missing systemic-thinking evidence",
		"violates any Master Output Contract property",
		"1. Non-overlapping scopes.",
		"2. Interface ownership.",
		"3. Shared-file ownership.",
		"4. Dependency ordering.",
		"5. Inherited constraints.",
		"6. Completeness.",
	)
	assertNotContains(t, masterReviewerPrompt, "=== MASTER DECOMPOSITION MANDATE ===")

	if err := commands.SubmitVerdictCommand(projectDir, masterID, "APPROVED", "", firstReviewerID, ""); err != nil {
		t.Fatalf("SubmitVerdict first approval failed: %v", err)
	}

	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	masterTask := findTask(state.Tasks, masterID)
	if masterTask.Status != models.TaskStatus("ARCHITECTURE_MAIN_PARTIALLY_APPROVED") {
		t.Fatalf("first approval status = %s, want ARCHITECTURE_MAIN_PARTIALLY_APPROVED", masterTask.Status)
	}

	secondReviewerID := "architecture-reviewer-2"
	testhelpers.RegisterTestAgent(t, bb, secondReviewerID, "architecture-reviewer")
	claimReviewManually(t, bb, masterID, secondReviewerID, models.TaskStatus("REVIEWING_ARCHITECTURE_MAIN_2"))
	if err := commands.SubmitVerdictCommand(projectDir, masterID, "APPROVED", "", secondReviewerID, ""); err != nil {
		t.Fatalf("SubmitVerdict second approval failed: %v", err)
	}

	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	masterTask = findTask(state.Tasks, masterID)
	if masterTask.Status != models.TaskStatus("ARCHITECTURE_MAIN_APPROVED") {
		t.Fatalf("second approval status = %s, want ARCHITECTURE_MAIN_APPROVED", masterTask.Status)
	}
	if len(masterTask.Approvals) != 2 {
		t.Fatalf("approval count = %d, want 2", len(masterTask.Approvals))
	}

	os.Setenv("LIZA_AGENT_ID", secondReviewerID)
	if err := commands.WtMergeCommand(projectDir, masterID, secondReviewerID); err != nil {
		t.Fatalf("WtMerge master failed: %v", err)
	}
	os.Unsetenv("LIZA_AGENT_ID")

	results, err := ops.ExecuteAvailableTransitions(projectDir, "auto")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions auto failed: %v", err)
	}
	var decomposeResult *ops.ProceedResult
	for i := range results {
		if results[i].TransitionName == "arch-decompose" {
			decomposeResult = &results[i]
			break
		}
	}
	if decomposeResult == nil {
		t.Fatalf("arch-decompose transition not executed; results=%v", results)
	}
	if len(decomposeResult.ChildTaskIDs) != 2 {
		t.Fatalf("arch-decompose child count = %d, want 2", len(decomposeResult.ChildTaskIDs))
	}

	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	for i, childID := range decomposeResult.ChildTaskIDs {
		child := findTask(state.Tasks, childID)
		if child == nil {
			t.Fatalf("child %s not found", childID)
		}
		if child.RolePair != "architecture-pair" {
			t.Errorf("child %s role_pair = %q, want architecture-pair", childID, child.RolePair)
		}
		if child.Status != models.TaskStatus("DRAFT_ARCHITECTURE") {
			t.Errorf("child %s status = %s, want DRAFT_ARCHITECTURE", childID, child.Status)
		}
		if child.ArchRef != archRef {
			t.Errorf("child %s arch_ref = %q, want %q", childID, child.ArchRef, archRef)
		}
		if child.Decomposition == nil {
			t.Fatalf("child %s decomposition is nil", childID)
		}
		if child.Decomposition.CoverageNotes != masterArchitectureOutput(archRef)[i].Decomposition.CoverageNotes {
			t.Errorf("child %s coverage notes = %q", childID, child.Decomposition.CoverageNotes)
		}
	}

	specializedDoerPrompt := buildIntegrationTaskPrompt(t, projectDir, state, decomposeResult.ChildTaskIDs[0], "architect", "architect-specialized-1")
	assertNotContains(t, specializedDoerPrompt, "=== MASTER DECOMPOSITION MANDATE ===")
	assertNotContains(t, specializedDoerPrompt, "=== MASTER DECOMPOSITION REVIEW ===")
	assertNotContains(t, specializedDoerPrompt, "Master Output Contract properties 1-6")
	assertNotContains(t, specializedDoerPrompt, "Systemic Decomposition Review")

	specializedReviewerPrompt := buildIntegrationTaskPrompt(t, projectDir, state, decomposeResult.ChildTaskIDs[0], "architecture-reviewer", "architecture-reviewer-specialized-1")
	assertNotContains(t, specializedReviewerPrompt, "=== MASTER DECOMPOSITION REVIEW ===")
	assertNotContains(t, specializedReviewerPrompt, "=== MASTER DECOMPOSITION MANDATE ===")
	assertNotContains(t, specializedReviewerPrompt, "Master Output Contract properties 1-6")
}

func TestManyToOneTransitionLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	projectDir, cleanup := setupTestProject(t)
	defer cleanup()

	// Step 1: Initialize project
	t.Log("Step 1: Initialize project")
	testhelpers.CreateCommittedSpecFileOnIntegration(t, projectDir, "feature.md", "# Feature Spec")

	if err := commands.InitCommand("Many-to-one test goal", "specs/feature.md", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	statePath := filepath.Join(projectDir, ".liza", "state.yaml")
	logPath := filepath.Join(projectDir, ".liza", "log.yaml")
	bb := db.New(statePath)

	// Step 2: Create parent epic-plan task with 3 output entries
	t.Log("Step 2: Create parent epic-plan task")
	parentID := "epic-plan-1"
	taskInput := &commands.TaskInput{
		ID:          parentID,
		RolePair:    "epic-planning-pair",
		Description: "Epic plan for feature",
		DoneWhen:    "Plan approved",
		Scope:       "Full feature",
		Priority:    1,
		SpecRef:     "specs/feature.md",
	}
	if err := commands.AddTaskCommand(statePath, logPath, taskInput, "orchestrator-1"); err != nil {
		t.Fatalf("AddTask epic-plan failed: %v", err)
	}

	// Register epic-planner and claim
	epicPlannerID := "epic-planner-1"
	testhelpers.RegisterTestAgent(t, bb, epicPlannerID, "epic-planner")
	if err := commands.ClaimTaskCommand(projectDir, parentID, epicPlannerID); err != nil {
		t.Fatalf("ClaimTask epic-plan failed: %v", err)
	}

	// Set 3 output entries
	if err := ops.SetTaskOutput(projectDir, &ops.SetTaskOutputInput{
		TaskID:  parentID,
		AgentID: epicPlannerID,
		Output: []models.OutputEntry{
			{Desc: "User story A", DoneWhen: "Story A done", Scope: "module-a/", SpecRef: "specs/feature.md"},
			{Desc: "User story B", DoneWhen: "Story B done", Scope: "module-b/", SpecRef: "specs/feature.md"},
			{Desc: "User story C", DoneWhen: "Story C done", Scope: "module-c/", SpecRef: "specs/feature.md"},
		},
	}); err != nil {
		t.Fatalf("SetTaskOutput failed: %v", err)
	}

	// Write checkpoint, commit, submit, review, approve, merge the epic-plan task
	if err := ops.WriteCheckpoint(projectDir, &ops.WriteCheckpointInput{
		TaskID: parentID, AgentID: epicPlannerID,
		Intent: "Epic plan", ValidationPlan: "review plan",
		FilesToModify: []string{"plan.md"},
	}); err != nil {
		t.Fatalf("WriteCheckpoint epic-plan failed: %v", err)
	}

	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	parentTask := findTask(state.Tasks, parentID)
	worktreePath := filepath.Join(projectDir, *parentTask.Worktree)
	planFile := filepath.Join(worktreePath, "plan.go")
	if err := os.WriteFile(planFile, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("Failed to create plan file: %v", err)
	}
	planTestFile := filepath.Join(worktreePath, "plan_test.go")
	if err := os.WriteFile(planTestFile, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("Failed to create plan test file: %v", err)
	}
	if err := exec.Command("git", "-C", worktreePath, "add", "plan.go", "plan_test.go").Run(); err != nil {
		t.Fatalf("git add plan failed: %v", err)
	}
	if err := exec.Command("git", "-C", worktreePath, "commit", "-m", "Epic plan").Run(); err != nil {
		t.Fatalf("git commit plan failed: %v", err)
	}
	output, err := exec.Command("git", "-C", worktreePath, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse plan failed: %v", err)
	}
	commitSHA := string(output[:len(output)-1])
	if err := commands.SubmitForReviewCommand(projectDir, parentID, commitSHA, epicPlannerID); err != nil {
		t.Fatalf("SubmitForReview epic-plan failed: %v", err)
	}

	// Reviewer approves epic-plan
	epicReviewerID := "epic-plan-reviewer-1"
	testhelpers.RegisterTestAgent(t, bb, epicReviewerID, "epic-plan-reviewer")
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	leaseExpires := state.Agents[epicReviewerID].LeaseExpires
	err = bb.Modify(func(s *models.State) error {
		tk := s.FindTask(parentID)
		if tk == nil {
			return nil
		}
		tk.Status = "REVIEWING_EPIC_PLAN"
		tk.ReviewingBy = &epicReviewerID
		tk.ReviewLeaseExpires = leaseExpires
		if agent, ok := s.Agents[epicReviewerID]; ok {
			agent.Status = models.AgentStatusWorking
			agent.CurrentTask = &parentID
			s.Agents[epicReviewerID] = agent
		}
		return nil
	})
	testhelpers.AssertNoError(t, err)
	if err := commands.SubmitVerdictCommand(projectDir, parentID, "APPROVED", "", epicReviewerID, ""); err != nil {
		t.Fatalf("SubmitVerdict epic-plan failed: %v", err)
	}

	// Merge epic-plan
	os.Setenv("LIZA_AGENT_ID", epicReviewerID)
	if err := commands.WtMergeCommand(projectDir, parentID, epicReviewerID); err != nil {
		t.Fatalf("WtMerge epic-plan failed: %v", err)
	}
	os.Unsetenv("LIZA_AGENT_ID")

	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	parentTask = findTask(state.Tasks, parentID)
	if parentTask.Status != models.TaskStatusMerged {
		t.Fatalf("Expected epic-plan MERGED, got %s", parentTask.Status)
	}

	// Step 3: Execute per-subtask fan-out (epic-to-us) → 3 US children
	t.Log("Step 3: Execute epic-to-us per-subtask transition")
	results, err := ops.ExecuteAvailableTransitions(projectDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions (epic-to-us) failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Expected 1 transition result, got %d", len(results))
	}
	if len(results[0].ChildTaskIDs) != 3 {
		t.Fatalf("Expected 3 children from per-subtask, got %d", len(results[0].ChildTaskIDs))
	}
	childIDs := results[0].ChildTaskIDs
	t.Logf("Created US children: %v", childIDs)

	// Step 4: Walk each US child through lifecycle to MERGED
	t.Log("Step 4: Walk US children to MERGED")
	for i, childID := range childIDs {
		t.Logf("  Walking child %d: %s", i, childID)
		doerID := fmt.Sprintf("us-writer-%d", i)
		reviewerID := fmt.Sprintf("us-reviewer-%d", i)
		walkTaskToMerged(t, projectDir, bb, childID, doerID, "us-writer", reviewerID, "us-reviewer", "REVIEWING_US")
	}

	// Step 5: Verify all 3 children are MERGED
	t.Log("Step 5: Verify all children MERGED")
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	for _, childID := range childIDs {
		child := findTask(state.Tasks, childID)
		if child == nil {
			t.Fatalf("Child %s not found", childID)
		}
		if child.Status != models.TaskStatusMerged {
			t.Fatalf("Child %s expected MERGED, got %s", childID, child.Status)
		}
	}

	// Step 6: Execute many-to-one fan-in (us-to-coding) → 1 grandchild
	t.Log("Step 6: Execute us-to-coding many-to-one transition")
	results, err = ops.ExecuteAvailableTransitions(projectDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions (us-to-coding) failed: %v", err)
	}

	// Filter for us-to-coding results (there should be exactly 1)
	var manyToOneResult *ops.ProceedResult
	for i := range results {
		if results[i].TransitionName == "us-to-coding" {
			manyToOneResult = &results[i]
			break
		}
	}
	if manyToOneResult == nil {
		t.Fatalf("No us-to-coding transition result found in %d results", len(results))
	}

	// Step 7: Assert 1 grandchild created
	t.Log("Step 7: Verify grandchild task")
	if len(manyToOneResult.ChildTaskIDs) != 1 {
		t.Fatalf("Expected 1 grandchild from many-to-one, got %d", len(manyToOneResult.ChildTaskIDs))
	}
	grandchildID := manyToOneResult.ChildTaskIDs[0]

	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	grandchild := findTask(state.Tasks, grandchildID)
	if grandchild == nil {
		t.Fatalf("Grandchild %s not found", grandchildID)
	}

	// Step 8: Assert ParentTasks contains all 3 children IDs
	t.Log("Step 8: Verify ParentTasks")
	if len(grandchild.ParentTasks) != 3 {
		t.Fatalf("Expected 3 ParentTasks, got %d: %v", len(grandchild.ParentTasks), grandchild.ParentTasks)
	}
	for _, childID := range childIDs {
		if !slices.Contains(grandchild.ParentTasks, childID) {
			t.Errorf("Grandchild ParentTasks %v missing child %s", grandchild.ParentTasks, childID)
		}
	}

	// Step 9: Assert correct role_pair, status, spec_ref
	t.Log("Step 9: Verify grandchild properties")
	if grandchild.RolePair != "architecture-main-pair" {
		t.Errorf("Expected role_pair architecture-main-pair, got %s", grandchild.RolePair)
	}
	if grandchild.Status != "DRAFT_ARCHITECTURE_MAIN" {
		t.Errorf("Expected status DRAFT_ARCHITECTURE_MAIN, got %s", grandchild.Status)
	}
	if grandchild.SpecRef != "specs/feature.md" {
		t.Errorf("Expected spec_ref specs/feature.md, got %s", grandchild.SpecRef)
	}
	if grandchild.Type != models.TaskTypeArchitecture {
		t.Errorf("Expected type architecture, got %s", grandchild.Type)
	}

	// Step 10: Assert deterministic child ID = <parent>-<task-slug>
	t.Log("Step 10: Verify deterministic child ID")
	expectedID := parentID + "-architecture"
	if grandchildID != expectedID {
		t.Errorf("Expected grandchild ID %q, got %q", expectedID, grandchildID)
	}

	// Step 11: Assert transitions_executed on all 3 children
	t.Log("Step 11: Verify transitions_executed on children")
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	for _, childID := range childIDs {
		child := findTask(state.Tasks, childID)
		if !child.TransitionsExecuted["us-to-coding"] {
			t.Errorf("Child %s missing transitions_executed[us-to-coding]", childID)
		}
	}

	// Step 12: Re-execute — assert idempotent (no new tasks)
	t.Log("Step 12: Verify idempotency")
	taskCountBefore := len(state.Tasks)
	results, err = ops.ExecuteAvailableTransitions(projectDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions (idempotency) failed: %v", err)
	}
	// us-to-coding should not produce new children
	for _, r := range results {
		if r.TransitionName == "us-to-coding" {
			t.Errorf("us-to-coding fired again — idempotency violated, children: %v", r.ChildTaskIDs)
		}
	}
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	if len(state.Tasks) != taskCountBefore {
		t.Errorf("Idempotency: task count changed from %d to %d", taskCountBefore, len(state.Tasks))
	}

	// Step 13: Crash recovery — remove grandchild, keep transitions_executed, re-execute
	t.Log("Step 13: Verify crash recovery")
	err = bb.Modify(func(s *models.State) error {
		// Remove grandchild while keeping transitions_executed on children
		for i := range s.Tasks {
			if s.Tasks[i].ID == grandchildID {
				s.Tasks = append(s.Tasks[:i], s.Tasks[i+1:]...)
				break
			}
		}
		return nil
	})
	testhelpers.AssertNoError(t, err)

	// Verify grandchild is gone
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	if findTask(state.Tasks, grandchildID) != nil {
		t.Fatal("Grandchild should have been removed for crash recovery test")
	}

	// Re-execute transitions — crash recovery should re-create grandchild
	_, err = ops.ExecuteAvailableTransitions(projectDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions (crash recovery) failed: %v", err)
	}

	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	recoveredGrandchild := findTask(state.Tasks, grandchildID)
	if recoveredGrandchild == nil {
		t.Fatal("Crash recovery failed: grandchild not re-created")
	}
	if len(recoveredGrandchild.ParentTasks) != 3 {
		t.Errorf("Recovered grandchild: expected 3 ParentTasks, got %d", len(recoveredGrandchild.ParentTasks))
	}
	if recoveredGrandchild.RolePair != "architecture-main-pair" {
		t.Errorf("Recovered grandchild: expected architecture-main-pair, got %s", recoveredGrandchild.RolePair)
	}

	t.Log("TestManyToOneTransitionLifecycle passed")
}

// TestArchRefPropagation verifies the full arch_ref data flow:
// 1. Architect sets arch_ref on output[] entries → code-planning children receive it (first hop)
// 2. Code-planning task carries arch_ref but its output[] entries omit it → coding children inherit from parent (second hop)
func TestArchRefPropagation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	projectDir, cleanup := setupTestProject(t)
	defer cleanup()

	// Step 1: Initialize project
	t.Log("Step 1: Initialize project")
	testhelpers.CreateCommittedSpecFileOnIntegration(t, projectDir, "arch-prop.md", "# Arch Ref Propagation Feature")

	if err := commands.InitCommand("Arch ref propagation test", "specs/arch-prop.md", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	statePath := filepath.Join(projectDir, ".liza", "state.yaml")
	logPath := filepath.Join(projectDir, ".liza", "log.yaml")
	bb := db.New(statePath)

	// Step 2: Add architecture task
	t.Log("Step 2: Add architecture task")
	archTaskID := "arch-prop-1"
	taskInput := &commands.TaskInput{
		ID:          archTaskID,
		Type:        "architecture",
		RolePair:    "architecture-pair",
		Description: "Architecture for arch-ref propagation feature",
		DoneWhen:    "Architecture defined",
		Scope:       "Full feature",
		Priority:    1,
		SpecRef:     "specs/arch-prop.md",
		DependsOn:   []string{},
	}
	if err := commands.AddTaskCommand(statePath, logPath, taskInput, "orchestrator-1"); err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	// Step 3: Architect claims and sets output with arch_ref
	t.Log("Step 3: Architect claims and sets output with arch_ref")
	architectID := "architect-10"
	testhelpers.RegisterTestAgent(t, bb, architectID, "architect")

	if err := commands.ClaimTaskCommand(projectDir, archTaskID, architectID); err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}

	// Create the architecture document in the worktree so it can be committed
	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	archTask := findTask(state.Tasks, archTaskID)
	if archTask == nil || archTask.Worktree == nil {
		t.Fatal("Architecture task has no worktree after claim")
	}
	worktreePath := filepath.Join(projectDir, *archTask.Worktree)

	// Create specs/arch-plan directory and document
	archPlanDir := filepath.Join(worktreePath, "specs", "arch-plan")
	if err := os.MkdirAll(archPlanDir, 0755); err != nil {
		t.Fatalf("Failed to create arch-plan dir: %v", err)
	}
	archDocPath := filepath.Join(archPlanDir, "feature.md")
	if err := os.WriteFile(archDocPath, []byte("# Architecture Plan\n\n## Components\n"), 0644); err != nil {
		t.Fatalf("Failed to create arch doc: %v", err)
	}

	// Set output entries with arch_ref pointing to the architecture document
	archRef := "specs/arch-plan/feature.md"
	if err := ops.SetTaskOutput(projectDir, &ops.SetTaskOutputInput{
		TaskID:  archTaskID,
		AgentID: architectID,
		Output: []models.OutputEntry{
			{
				Desc:     "Implement auth module",
				DoneWhen: "Auth module complete",
				Scope:    "internal/auth/",
				SpecRef:  "specs/arch-prop.md",
				ArchRef:  archRef,
			},
			{
				Desc:     "Implement storage layer",
				DoneWhen: "Storage layer complete",
				Scope:    "internal/storage/",
				SpecRef:  "specs/arch-prop.md",
				ArchRef:  archRef,
			},
		},
	}); err != nil {
		t.Fatalf("SetTaskOutput failed: %v", err)
	}

	// Step 4: Walk architecture task through review and merge
	t.Log("Step 4: Submit, review, approve, merge architecture task")
	if err := ops.WriteCheckpoint(projectDir, &ops.WriteCheckpointInput{
		TaskID: archTaskID, AgentID: architectID,
		Intent: "Architecture definition", ValidationPlan: "Review architecture",
		FilesToModify: []string{"specs/arch-plan/feature.md"},
	}); err != nil {
		t.Fatalf("WriteCheckpoint failed: %v", err)
	}

	if err := exec.Command("git", "-C", worktreePath, "add", "specs/arch-plan/feature.md").Run(); err != nil {
		t.Fatalf("git add failed: %v", err)
	}
	if err := exec.Command("git", "-C", worktreePath, "commit", "-m", "Define architecture").Run(); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	output, err := exec.Command("git", "-C", worktreePath, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse failed: %v", err)
	}
	reviewCommit := string(output[:len(output)-1])

	if err := commands.SubmitForReviewCommand(projectDir, archTaskID, reviewCommit, architectID); err != nil {
		t.Fatalf("SubmitForReview failed: %v", err)
	}

	// Reviewer approves
	archReviewerID := "architecture-reviewer-10"
	testhelpers.RegisterTestAgent(t, bb, archReviewerID, "architecture-reviewer")

	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	leaseExpires := state.Agents[archReviewerID].LeaseExpires
	err = bb.Modify(func(s *models.State) error {
		tk := s.FindTask(archTaskID)
		if tk == nil {
			return nil
		}
		tk.Status = "REVIEWING_ARCHITECTURE"
		tk.ReviewingBy = &archReviewerID
		tk.ReviewLeaseExpires = leaseExpires
		if agent, ok := s.Agents[archReviewerID]; ok {
			agent.Status = models.AgentStatusWorking
			agent.CurrentTask = &archTaskID
			s.Agents[archReviewerID] = agent
		}
		return nil
	})
	testhelpers.AssertNoError(t, err)

	if err := commands.SubmitVerdictCommand(projectDir, archTaskID, "APPROVED", "", archReviewerID, ""); err != nil {
		t.Fatalf("SubmitVerdict (APPROVED) failed: %v", err)
	}

	// Merge
	os.Setenv("LIZA_AGENT_ID", archReviewerID)
	if err := commands.WtMergeCommand(projectDir, archTaskID, archReviewerID); err != nil {
		t.Fatalf("WtMerge failed: %v", err)
	}
	os.Unsetenv("LIZA_AGENT_ID")

	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	archTask = findTask(state.Tasks, archTaskID)
	if archTask.Status != models.TaskStatusMerged {
		t.Fatalf("Expected MERGED, got %s", archTask.Status)
	}

	// Step 5: Execute architecture-to-code-plan transition
	t.Log("Step 5: Execute architecture-to-code-plan transition")
	results, err := ops.ExecuteAvailableTransitions(projectDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions failed: %v", err)
	}

	var archToCodePlanResult *ops.ProceedResult
	for i := range results {
		if results[i].TransitionName == "architecture-to-code-plan" {
			archToCodePlanResult = &results[i]
			break
		}
	}
	if archToCodePlanResult == nil {
		t.Fatalf("No architecture-to-code-plan transition result found in %d results", len(results))
	}
	if len(archToCodePlanResult.ChildTaskIDs) != 2 {
		t.Fatalf("Expected 2 code-planning children, got %d", len(archToCodePlanResult.ChildTaskIDs))
	}

	// Step 6: FIRST HOP ASSERTION — code-planning tasks receive arch_ref from output[] entries
	t.Log("Step 6: Assert code-planning children have arch_ref from output[] entries")
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)

	codePlanIDs := archToCodePlanResult.ChildTaskIDs
	for _, cpID := range codePlanIDs {
		child := findTask(state.Tasks, cpID)
		if child == nil {
			t.Fatalf("Code-planning child %s not found", cpID)
		}
		if child.ArchRef != archRef {
			t.Errorf("Code-planning child %s: expected arch_ref %q, got %q", cpID, archRef, child.ArchRef)
		}
		if child.RolePair != "code-planning-pair" {
			t.Errorf("Code-planning child %s: expected role_pair code-planning-pair, got %s", cpID, child.RolePair)
		}
		if child.SpecRef != "specs/arch-prop.md" {
			t.Errorf("Code-planning child %s: expected spec_ref specs/arch-prop.md, got %s", cpID, child.SpecRef)
		}
	}

	// Step 7: Walk first code-planning child through lifecycle with output[] entries that LACK arch_ref
	t.Log("Step 7: Walk code-planning child through lifecycle (output without arch_ref)")
	cpTaskID := codePlanIDs[0]
	cpDoerID := "code-planner-10"
	cpReviewerID := "code-plan-reviewer-10"
	testhelpers.RegisterTestAgent(t, bb, cpDoerID, "code-planner")
	testhelpers.RegisterTestAgent(t, bb, cpReviewerID, "code-plan-reviewer")

	if err := commands.ClaimTaskCommand(projectDir, cpTaskID, cpDoerID); err != nil {
		t.Fatalf("ClaimTask code-planning failed: %v", err)
	}

	// Set output entries WITHOUT arch_ref — testing second-hop inheritance
	if err := ops.SetTaskOutput(projectDir, &ops.SetTaskOutputInput{
		TaskID:  cpTaskID,
		AgentID: cpDoerID,
		Output: []models.OutputEntry{
			{
				Desc:     "Implement auth handler",
				DoneWhen: "Handler tests pass",
				Scope:    "internal/auth/handler.go",
				SpecRef:  "specs/arch-prop.md",
				// No ArchRef — testing parent inheritance
			},
			{
				Desc:     "Implement auth middleware",
				DoneWhen: "Middleware tests pass",
				Scope:    "internal/auth/middleware.go",
				SpecRef:  "specs/arch-prop.md",
				// No ArchRef — testing parent inheritance
			},
		},
	}); err != nil {
		t.Fatalf("SetTaskOutput code-planning failed: %v", err)
	}

	// Checkpoint, commit, submit, review, approve, merge
	if err := ops.WriteCheckpoint(projectDir, &ops.WriteCheckpointInput{
		TaskID: cpTaskID, AgentID: cpDoerID,
		Intent: "Code plan for auth module", ValidationPlan: "tests pass",
		FilesToModify: []string{"plan.md"},
	}); err != nil {
		t.Fatalf("WriteCheckpoint code-planning failed: %v", err)
	}

	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	cpTask := findTask(state.Tasks, cpTaskID)
	if cpTask == nil || cpTask.Worktree == nil {
		t.Fatal("Code-planning task has no worktree after claim")
	}
	cpWorktreePath := filepath.Join(projectDir, *cpTask.Worktree)

	// Create plan file and commit
	planFile := filepath.Join(cpWorktreePath, "plan.md")
	if err := os.WriteFile(planFile, []byte("# Code Plan\n"), 0644); err != nil {
		t.Fatalf("Failed to create plan file: %v", err)
	}
	if err := exec.Command("git", "-C", cpWorktreePath, "add", "plan.md").Run(); err != nil {
		t.Fatalf("git add plan failed: %v", err)
	}
	if err := exec.Command("git", "-C", cpWorktreePath, "commit", "-m", "Code plan").Run(); err != nil {
		t.Fatalf("git commit plan failed: %v", err)
	}

	output, err = exec.Command("git", "-C", cpWorktreePath, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse code-planning failed: %v", err)
	}
	cpCommit := string(output[:len(output)-1])

	if err := commands.SubmitForReviewCommand(projectDir, cpTaskID, cpCommit, cpDoerID); err != nil {
		t.Fatalf("SubmitForReview code-planning failed: %v", err)
	}

	// Reviewer approves
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	leaseExpires = state.Agents[cpReviewerID].LeaseExpires
	err = bb.Modify(func(s *models.State) error {
		tk := s.FindTask(cpTaskID)
		if tk == nil {
			return nil
		}
		tk.Status = "REVIEWING_CODING_PLAN"
		tk.ReviewingBy = &cpReviewerID
		tk.ReviewLeaseExpires = leaseExpires
		if agent, ok := s.Agents[cpReviewerID]; ok {
			agent.Status = models.AgentStatusWorking
			agent.CurrentTask = &cpTaskID
			s.Agents[cpReviewerID] = agent
		}
		return nil
	})
	testhelpers.AssertNoError(t, err)

	if err := commands.SubmitVerdictCommand(projectDir, cpTaskID, "APPROVED", "", cpReviewerID, ""); err != nil {
		t.Fatalf("SubmitVerdict code-planning APPROVED failed: %v", err)
	}

	// Merge code-planning task
	os.Setenv("LIZA_AGENT_ID", cpReviewerID)
	if err := commands.WtMergeCommand(projectDir, cpTaskID, cpReviewerID); err != nil {
		t.Fatalf("WtMerge code-planning failed: %v", err)
	}
	os.Unsetenv("LIZA_AGENT_ID")

	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	cpTask = findTask(state.Tasks, cpTaskID)
	if cpTask.Status != models.TaskStatusMerged {
		t.Fatalf("Expected code-planning MERGED, got %s", cpTask.Status)
	}

	// Verify code-planning task still carries arch_ref
	if cpTask.ArchRef != archRef {
		t.Fatalf("Code-planning task lost arch_ref after merge: expected %q, got %q", archRef, cpTask.ArchRef)
	}

	// Step 8: Execute code-plan-to-coding transition
	t.Log("Step 8: Execute code-plan-to-coding transition")
	results, err = ops.ExecuteAvailableTransitions(projectDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions (code-plan-to-coding) failed: %v", err)
	}

	var codePlanToCodingResult *ops.ProceedResult
	for i := range results {
		if results[i].TransitionName == "code-plan-to-coding" {
			codePlanToCodingResult = &results[i]
			break
		}
	}
	if codePlanToCodingResult == nil {
		t.Fatalf("No code-plan-to-coding transition result found in %d results", len(results))
	}
	if len(codePlanToCodingResult.ChildTaskIDs) != 2 {
		t.Fatalf("Expected 2 coding children, got %d", len(codePlanToCodingResult.ChildTaskIDs))
	}

	// Step 9: SECOND HOP ASSERTION — coding tasks inherit arch_ref from parent code-planning task
	t.Log("Step 9: Assert coding children inherit arch_ref from parent code-planning task")
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)

	for _, codingID := range codePlanToCodingResult.ChildTaskIDs {
		codingTask := findTask(state.Tasks, codingID)
		if codingTask == nil {
			t.Fatalf("Coding child %s not found", codingID)
		}
		if codingTask.ArchRef != archRef {
			t.Errorf("Coding child %s: expected arch_ref %q (inherited from parent), got %q", codingID, archRef, codingTask.ArchRef)
		}
		if codingTask.RolePair != "coding-pair" {
			t.Errorf("Coding child %s: expected role_pair coding-pair, got %s", codingID, codingTask.RolePair)
		}
		if codingTask.SpecRef != "specs/arch-prop.md" {
			t.Errorf("Coding child %s: expected spec_ref specs/arch-prop.md, got %s", codingID, codingTask.SpecRef)
		}
	}

	t.Log("TestArchRefPropagation passed — first hop and second hop arch_ref propagation verified")
}

func masterArchitectureOutput(archRef string) []models.OutputEntry {
	return []models.OutputEntry{
		{
			Desc:     "Design API boundary",
			DoneWhen: "API boundary architecture is approved",
			Scope:    "internal/api",
			SpecRef:  "specs/master.md",
			ArchRef:  archRef,
			Decomposition: &models.DecompositionManifest{
				OwnedFiles:         []string{"internal/api/handler.go"},
				OwnedModules:       []string{"internal/api"},
				InterfacesOwned:    []string{"api.Handler"},
				InterfacesConsumed: []string{"storage.Repository"},
				CoverageNotes:      "Owns API boundary and consumes storage contracts.",
			},
		},
		{
			Desc:      "Design storage boundary",
			DoneWhen:  "Storage boundary architecture is approved",
			Scope:     "internal/storage",
			SpecRef:   "specs/master.md",
			ArchRef:   archRef,
			DependsOn: []string{"0"},
			Decomposition: &models.DecompositionManifest{
				OwnedFiles:        []string{"internal/storage/repository.go"},
				OwnedModules:      []string{"internal/storage"},
				ReadOnlyDependsOn: []int{0},
				InterfacesOwned:   []string{"storage.Repository"},
				CoverageNotes:     "Owns storage contract after reading the API boundary.",
			},
		},
	}
}

func buildIntegrationTaskPrompt(t *testing.T, projectDir string, state *models.State, taskID, role, agentID string) string {
	t.Helper()

	pipeCfg, err := pipeline.LoadFrozen(projectDir)
	if err != nil {
		t.Fatalf("LoadFrozen failed: %v", err)
	}
	resolver := pipeline.NewResolver(pipeCfg)
	strategy, err := agent.NewRoleStrategy(role, resolver)
	if err != nil {
		t.Fatalf("NewRoleStrategy(%s) failed: %v", role, err)
	}

	prompt, err := strategy.BuildPrompt(state, agent.SupervisorConfig{
		AgentID:     agentID,
		Role:        role,
		ProjectRoot: projectDir,
		StatePath:   filepath.Join(projectDir, ".liza", "state.yaml"),
		SpecsDir:    filepath.Join(projectDir, "specs"),
	}, taskID)
	if err != nil {
		t.Fatalf("BuildPrompt(%s, %s) failed: %v", role, taskID, err)
	}
	return prompt
}

func commitTaskWorktreeFile(t *testing.T, projectDir string, bb *db.Blackboard, taskID, relPath, content string) string {
	t.Helper()

	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	task := findTask(state.Tasks, taskID)
	if task == nil || task.Worktree == nil {
		t.Fatalf("task %s has no worktree", taskID)
	}
	worktreePath := filepath.Join(projectDir, *task.Worktree)
	fullPath := filepath.Join(worktreePath, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("mkdir %s failed: %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("write %s failed: %v", fullPath, err)
	}
	if err := exec.Command("git", "-C", worktreePath, "add", filepath.ToSlash(relPath)).Run(); err != nil {
		t.Fatalf("git add %s failed: %v", relPath, err)
	}
	if err := exec.Command("git", "-C", worktreePath, "commit", "-m", "Master planning output").Run(); err != nil {
		t.Fatalf("git commit %s failed: %v", taskID, err)
	}
	output, err := exec.Command("git", "-C", worktreePath, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse %s failed: %v", taskID, err)
	}
	return strings.TrimSpace(string(output))
}

func claimReviewManually(t *testing.T, bb *db.Blackboard, taskID, reviewerID string, reviewingStatus models.TaskStatus) {
	t.Helper()

	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	leaseExpires := state.Agents[reviewerID].LeaseExpires
	err = bb.Modify(func(s *models.State) error {
		task := s.FindTask(taskID)
		if task == nil {
			return fmt.Errorf("task %s not found", taskID)
		}
		task.Status = reviewingStatus
		task.ReviewingBy = &reviewerID
		task.ReviewLeaseExpires = leaseExpires
		if agent, ok := s.Agents[reviewerID]; ok {
			agent.Status = models.AgentStatusWorking
			agent.CurrentTask = &taskID
			s.Agents[reviewerID] = agent
		}
		return nil
	})
	testhelpers.AssertNoError(t, err)
}

func assertContainsAll(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("expected prompt to contain %q", want)
		}
	}
}

func assertNotContains(t *testing.T, got, unwanted string) {
	t.Helper()
	if strings.Contains(got, unwanted) {
		t.Errorf("expected prompt not to contain %q", unwanted)
	}
}

// findTask is a helper function to find a task by ID in a slice of tasks
func findTask(tasks []models.Task, taskID string) *models.Task {
	for i := range tasks {
		if tasks[i].ID == taskID {
			return &tasks[i]
		}
	}
	return nil
}
