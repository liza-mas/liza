package integration

// concurrent_operations_test.go contains integration tests for concurrent operations.
//
// These tests verify that the blackboard locking mechanism correctly handles
// race conditions when multiple agents operate simultaneously.

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// TestConcurrentClaimAttempts tests that only one agent can claim a task
// when multiple agents try to claim simultaneously.
func TestConcurrentClaimAttempts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	projectDir, cleanup := setupTestProject(t)
	defer cleanup()

	taskID := "task-1"
	bb, _, _ := setupIntegrationTest(t, projectDir, []string{taskID})

	// Register multiple agents
	numAgents := 5
	agentIDs := make([]string, numAgents)
	for i := 0; i < numAgents; i++ {
		agentID := "coder-" + string(rune('1'+i))
		agentIDs[i] = agentID
		testhelpers.RegisterTestAgent(t, bb, agentID, "coder")
	}

	// Have all agents try to claim the same task concurrently
	var wg sync.WaitGroup
	results := make([]error, numAgents)

	t.Log("Starting concurrent claim attempts")
	for i := 0; i < numAgents; i++ {
		wg.Add(1)
		go func(agentIndex int) {
			defer wg.Done()
			agentID := agentIDs[agentIndex]
			results[agentIndex] = commands.ClaimTaskCommand(projectDir, taskID, agentID)
		}(i)
	}

	wg.Wait()

	// Verify that exactly one claim succeeded
	successCount := 0
	var successfulAgent string
	for i, err := range results {
		if err == nil {
			successCount++
			successfulAgent = agentIDs[i]
			t.Logf("Agent %s successfully claimed task", agentIDs[i])
		} else {
			t.Logf("Agent %s failed to claim: %v", agentIDs[i], err)
		}
	}

	if successCount != 1 {
		t.Errorf("Expected exactly 1 successful claim, got %d", successCount)
	}

	// Verify final state
	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)

	task := findTask(state.Tasks, taskID)
	if task == nil {
		t.Fatal("Task not found")
	}

	if task.Status != models.TaskStatusImplementing {
		t.Errorf("Expected task status IMPLEMENTING, got %s", task.Status)
	}

	if task.AssignedTo == nil {
		t.Error("Expected task to be assigned")
	} else if *task.AssignedTo != successfulAgent {
		t.Errorf("Expected task assigned to %s, got %s", successfulAgent, *task.AssignedTo)
	}

	t.Log("✓ Concurrent claim test passed")
}

// TestConcurrentClaimsOfDifferentTasks tests that multiple agents can
// claim different tasks simultaneously without interference.
func TestConcurrentClaimsOfDifferentTasks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	projectDir, cleanup := setupTestProject(t)
	defer cleanup()

	numTasks := 5
	taskIDs := make([]string, numTasks)
	for i := 0; i < numTasks; i++ {
		taskIDs[i] = "task-" + string(rune('a'+i))
	}
	bb, _, _ := setupIntegrationTest(t, projectDir, taskIDs)

	// Register multiple agents (one per task)
	agentIDs := make([]string, numTasks)
	for i := 0; i < numTasks; i++ {
		agentID := "coder-" + string(rune('1'+i))
		agentIDs[i] = agentID
		testhelpers.RegisterTestAgent(t, bb, agentID, "coder")
	}

	// Have each agent claim a different task concurrently
	var wg sync.WaitGroup
	results := make([]error, numTasks)

	t.Log("Starting concurrent claims of different tasks")
	for i := 0; i < numTasks; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index] = commands.ClaimTaskCommand(projectDir, taskIDs[index], agentIDs[index])
		}(i)
	}

	wg.Wait()

	// Verify that all claims succeeded
	for i, err := range results {
		if err != nil {
			t.Errorf("Agent %s failed to claim task %s: %v", agentIDs[i], taskIDs[i], err)
		}
	}

	// Verify final state - all tasks should be claimed by their respective agents
	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)

	for i := 0; i < numTasks; i++ {
		task := findTask(state.Tasks, taskIDs[i])
		if task == nil {
			t.Errorf("Task %s not found", taskIDs[i])
			continue
		}

		if task.Status != models.TaskStatusImplementing {
			t.Errorf("Expected task %s status IMPLEMENTING, got %s", taskIDs[i], task.Status)
		}

		if task.AssignedTo == nil {
			t.Errorf("Expected task %s to be assigned", taskIDs[i])
		} else if *task.AssignedTo != agentIDs[i] {
			t.Errorf("Expected task %s assigned to %s, got %s", taskIDs[i], agentIDs[i], *task.AssignedTo)
		}
	}

	t.Log("✓ Concurrent claims of different tasks test passed")
}

// TestConcurrentStateModifications tests that concurrent modifications to state
// are properly serialized and no updates are lost.
func TestConcurrentStateModifications(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	tmpDir := t.TempDir()

	// Setup minimal state
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	bb := testhelpers.WriteInitialState(t, statePath, state)

	// Perform concurrent modifications that increment a counter
	numGoroutines := 10
	modificationsPerGoroutine := 5

	var wg sync.WaitGroup
	errors := make([]error, numGoroutines*modificationsPerGoroutine)
	errorIndex := 0
	var errorMutex sync.Mutex

	t.Log("Starting concurrent state modifications")
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < modificationsPerGoroutine; j++ {
				err := bb.Modify(func(state *models.State) error {
					// Increment a counter in config (using HeartbeatInterval as test counter)
					state.Config.HeartbeatInterval++
					return nil
				})

				errorMutex.Lock()
				errors[errorIndex] = err
				errorIndex++
				errorMutex.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// Verify no errors occurred
	for i, err := range errors {
		if err != nil {
			t.Errorf("Modification %d failed: %v", i, err)
		}
	}

	// Verify final counter value
	finalState, err := bb.Read()
	testhelpers.AssertNoError(t, err)

	expectedCount := 60 + (numGoroutines * modificationsPerGoroutine) // 60 is initial value from CreateValidState
	if finalState.Config.HeartbeatInterval != expectedCount {
		t.Errorf("Expected counter to be %d, got %d (some updates were lost)", expectedCount, finalState.Config.HeartbeatInterval)
	}

	t.Log("✓ Concurrent state modifications test passed")
}

// TestConcurrentReadsDuringWrite tests that reads are blocked during writes
// (verifying lock behavior).
func TestConcurrentReadsDuringWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	tmpDir := t.TempDir()

	// Setup
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	bb := testhelpers.WriteInitialState(t, statePath, state)

	// Start a long-running write
	var wg sync.WaitGroup
	writeStarted := make(chan struct{})
	writeCompleted := make(chan struct{})
	allowWriteFinish := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := bb.Modify(func(state *models.State) error {
			close(writeStarted)
			<-allowWriteFinish
			state.Config.HeartbeatInterval = 999
			return nil
		})
		if err != nil {
			t.Errorf("Write failed: %v", err)
		}
		close(writeCompleted)
	}()

	// Wait for write to start
	<-writeStarted

	// Try to read during the write - it should stay blocked while the writer holds the lock.
	readDone := make(chan struct{})
	var readErr error
	readStart := time.Now()
	go func() {
		_, readErr = bb.Read()
		close(readDone)
	}()

	select {
	case <-readDone:
		t.Fatal("Read completed too quickly, may not have been properly blocked by write lock")
	case <-time.After(50 * time.Millisecond):
		// Expected: still blocked while write lock is held.
	}
	close(allowWriteFinish)

	<-readDone
	readDuration := time.Since(readStart)
	testhelpers.AssertNoError(t, readErr)
	if readDuration < 50*time.Millisecond {
		t.Errorf("Read completed too quickly, expected it to remain blocked for at least 50ms (got %v)", readDuration)
	}

	// Wait for write to complete
	<-writeCompleted
	wg.Wait()

	// Verify the write succeeded
	finalState, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	if finalState.Config.HeartbeatInterval != 999 {
		t.Errorf("Expected HeartbeatInterval to be 999, got %d", finalState.Config.HeartbeatInterval)
	}

	t.Log("✓ Concurrent reads during write test passed")
}

// TestConcurrentClaimWithWorktreeConflict tests the scenario where multiple
// coders try to claim the same task and create worktrees simultaneously,
// ensuring only one succeeds and invalid state is never created.
func TestConcurrentClaimWithWorktreeConflict(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	projectDir, cleanup := setupTestProject(t)
	defer cleanup()

	taskID := "test-task"
	bb, _, _ := setupIntegrationTest(t, projectDir, []string{taskID})

	// Register multiple agents
	numCoders := 5
	agentIDs := make([]string, numCoders)
	for i := 0; i < numCoders; i++ {
		agentID := "coder-" + string(rune('1'+i))
		agentIDs[i] = agentID
		testhelpers.RegisterTestAgent(t, bb, agentID, "coder")
	}

	// Launch concurrent claim attempts
	var wg sync.WaitGroup
	results := make([]error, numCoders)

	t.Log("Starting concurrent claim attempts with worktree creation")
	for i := 0; i < numCoders; i++ {
		wg.Add(1)
		go func(coderNum int) {
			defer wg.Done()
			agentID := agentIDs[coderNum]
			// All coders try to claim the same task
			results[coderNum] = commands.ClaimTaskCommand(projectDir, taskID, agentID)
		}(i)
	}

	wg.Wait()

	// Verify results
	successCount := 0
	failureCount := 0
	var successfulAgent string

	for i, err := range results {
		if err == nil {
			successCount++
			successfulAgent = agentIDs[i]
			t.Logf("Agent %s successfully claimed task", agentIDs[i])
		} else {
			failureCount++
			t.Logf("Agent %s failed to claim: %v", agentIDs[i], err)
			// Should fail with one of the expected race condition errors
			errMsg := err.Error()
			if !strings.Contains(errMsg, "race condition") &&
				!strings.Contains(errMsg, "worktree") &&
				!strings.Contains(errMsg, "branch") &&
				!strings.Contains(errMsg, "is IMPLEMENTING") {
				t.Errorf("Agent %s: unexpected error type: %v", agentIDs[i], err)
			}
		}
	}

	// Exactly one should succeed
	if successCount != 1 {
		t.Errorf("Expected exactly 1 success, got %d", successCount)
	}
	if failureCount != numCoders-1 {
		t.Errorf("Expected %d failures, got %d", numCoders-1, failureCount)
	}

	// CRITICAL: Verify state is valid
	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)

	task := findTask(state.Tasks, taskID)
	if task == nil {
		t.Fatal("Task disappeared from state")
	}

	// Task should be CLAIMED
	if task.Status != models.TaskStatusImplementing {
		t.Errorf("Task status should be IMPLEMENTING, got %s", task.Status)
	}

	// Should be assigned to the successful agent
	if task.AssignedTo == nil {
		t.Fatal("Task marked IMPLEMENTING but AssignedTo is nil")
	}
	if *task.AssignedTo != successfulAgent {
		t.Errorf("Task assigned to %s, expected %s", *task.AssignedTo, successfulAgent)
	}

	// Worktree must be set
	if task.Worktree == nil {
		t.Fatal("Task marked IMPLEMENTING but worktree is nil")
	}

	// CRITICAL CHECK: Worktree directory must exist on disk
	worktreePath := filepath.Join(projectDir, *task.Worktree)
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		t.Fatalf("INVALID STATE: Task marked IMPLEMENTING with worktree=%s but directory does not exist",
			*task.Worktree)
	}

	t.Logf("✓ Worktree exists at %s", worktreePath)
	t.Log("✓ Concurrent claim with worktree conflict test passed")
}

// TestConcurrentClaimIntegrationFailedTask tests that when multiple agents
// attempt to claim the same INTEGRATION_FAILED task concurrently, only one
// succeeds. This validates the fix for the task coordination race condition
// where ReadCached() was allowing multiple agents to select the same task.
func TestConcurrentClaimIntegrationFailedTask(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	projectDir, cleanup := setupTestProject(t)
	defer cleanup()

	taskID := "task-failed"
	bb, _, _ := setupIntegrationTest(t, projectDir, []string{taskID})

	// Create a real git worktree (Phase 3 validates .git link file)
	testhelpers.CreateTestWorktree(t, projectDir, taskID)

	// Set task to INTEGRATION_FAILED with a worktree (simulating failed merge)
	worktreeName := ".worktrees/task-failed"
	integrationFix := true
	err := bb.Modify(func(state *models.State) error {
		for i := range state.Tasks {
			if state.Tasks[i].ID == taskID {
				state.Tasks[i].Status = models.TaskStatusIntegrationFailed
				state.Tasks[i].IntegrationFix = integrationFix
				state.Tasks[i].Worktree = &worktreeName
				break
			}
		}
		return nil
	})
	testhelpers.AssertNoError(t, err)

	// Register multiple coder agents
	numAgents := 3
	agentIDs := make([]string, numAgents)
	for i := 0; i < numAgents; i++ {
		agentID := "coder-" + string(rune('1'+i))
		agentIDs[i] = agentID
		testhelpers.RegisterTestAgent(t, bb, agentID, "coder")
	}

	// Have all agents try to claim the same INTEGRATION_FAILED task concurrently
	var wg sync.WaitGroup
	results := make([]error, numAgents)

	t.Log("Starting concurrent claim attempts for INTEGRATION_FAILED task")
	for i := 0; i < numAgents; i++ {
		wg.Add(1)
		go func(agentIndex int) {
			defer wg.Done()
			agentID := agentIDs[agentIndex]
			results[agentIndex] = commands.ClaimTaskCommand(projectDir, taskID, agentID)
		}(i)
	}

	wg.Wait()

	// Verify that exactly one claim succeeded
	successCount := 0
	var successfulAgent string
	for i, err := range results {
		if err == nil {
			successCount++
			successfulAgent = agentIDs[i]
			t.Logf("Agent %s successfully claimed INTEGRATION_FAILED task", agentIDs[i])
		} else {
			// Expected errors: either "race condition", "not claimable", or "is IMPLEMENTING"
			if !strings.Contains(err.Error(), "race condition") &&
				!strings.Contains(err.Error(), "not claimable") &&
				!strings.Contains(err.Error(), "no claimable tasks") &&
				!strings.Contains(err.Error(), "is IMPLEMENTING") {
				t.Logf("Agent %s failed with unexpected error: %v", agentIDs[i], err)
			} else {
				t.Logf("Agent %s failed as expected: %v", agentIDs[i], err)
			}
		}
	}

	if successCount != 1 {
		t.Errorf("Expected exactly 1 successful claim, got %d", successCount)
	}

	// Verify final state
	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)

	task := findTask(state.Tasks, taskID)
	if task == nil {
		t.Fatal("Task not found")
	}

	// Task should be CLAIMED
	if task.Status != models.TaskStatusImplementing {
		t.Errorf("Expected task status IMPLEMENTING, got %s", task.Status)
	}

	// Should be assigned to exactly one agent
	if task.AssignedTo == nil {
		t.Error("Expected task to be assigned")
	} else if *task.AssignedTo != successfulAgent {
		t.Errorf("Expected task assigned to %s, got %s", successfulAgent, *task.AssignedTo)
	}

	// IntegrationFix flag should still be set
	if !task.IntegrationFix {
		t.Error("Expected IntegrationFix=true to be preserved")
	}

	// Worktree should be preserved (not recreated)
	if task.Worktree == nil {
		t.Error("Expected worktree to be preserved")
	} else if *task.Worktree != worktreeName {
		t.Errorf("Expected worktree=%s, got %s", worktreeName, *task.Worktree)
	}

	// Verify worktree still exists
	worktreePath := filepath.Join(projectDir, worktreeName)
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		t.Errorf("Worktree directory should still exist at %s", worktreePath)
	}

	t.Log("✓ Concurrent INTEGRATION_FAILED claim test passed")
}

// TestConcurrentMerges tests that multiple approved tasks can be merged
// concurrently without race conditions or corruption.
// This verifies the fix for the MergeWorktree race condition where
// concurrent reviewers would corrupt the integration branch.
func TestConcurrentMerges(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	projectDir, cleanup := setupTestProject(t)
	defer cleanup()

	numTasks := 3
	taskIDs := make([]string, numTasks)
	agentIDs := make([]string, numTasks)
	reviewerIDs := make([]string, numTasks)

	bb, _, _ := setupIntegrationTest(t, projectDir, []string{})

	// Register multiple coders and reviewers
	for i := 0; i < numTasks; i++ {
		taskIDs[i] = "task-merge-" + string(rune('a'+i))
		agentIDs[i] = "coder-" + string(rune('1'+i))
		reviewerIDs[i] = "code-reviewer-" + string(rune('1'+i))
		testhelpers.RegisterTestAgent(t, bb, agentIDs[i], "coder")
		testhelpers.RegisterTestAgent(t, bb, reviewerIDs[i], "code-reviewer")
	}

	// Create approved tasks with worktrees
	for i, taskID := range taskIDs {
		err := bb.Modify(func(state *models.State) error {
			// Create worktree directory and branch
			wtDir := filepath.Join(projectDir, ".worktrees", taskID)
			if err := os.MkdirAll(filepath.Dir(wtDir), 0755); err != nil {
				return err
			}

			// Create worktree from integration
			cmd := exec.Command("git", "-C", projectDir, "worktree", "add", wtDir, "integration", "-b", "task/"+taskID)
			if err := cmd.Run(); err != nil {
				t.Fatalf("Failed to create worktree for %s: %v", taskID, err)
			}

			// Create unique file in worktree and commit
			testFile := filepath.Join(wtDir, "feature-"+string(rune('a'+i))+".txt")
			if err := os.WriteFile(testFile, []byte("content from "+taskID), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			cmd = exec.Command("git", "-C", wtDir, "add", ".")
			if err := cmd.Run(); err != nil {
				t.Fatalf("Failed to git add: %v", err)
			}

			cmd = exec.Command("git", "-C", wtDir, "commit", "-m", "Commit for "+taskID)
			if err := cmd.Run(); err != nil {
				t.Fatalf("Failed to git commit: %v", err)
			}

			// Get commit SHA
			cmd = exec.Command("git", "-C", wtDir, "rev-parse", "HEAD")
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("Failed to get commit SHA: %v", err)
			}
			reviewCommit := strings.TrimSpace(string(output))

			wtRel := path.Join(".worktrees", taskID)
			now := time.Now().UTC()
			task := models.Task{
				ID:           taskID,
				Description:  "Test merge task " + taskID,
				Status:       models.TaskStatusApproved,
				RolePair:     "coding-pair",
				Priority:     i + 1,
				Created:      now,
				SpecRef:      "README.md",
				DoneWhen:     "Done",
				Scope:        "Test",
				Worktree:     &wtRel,
				AssignedTo:   &agentIDs[i],
				ReviewCommit: &reviewCommit,
				ApprovedBy:   &reviewerIDs[i],
				History:      []models.TaskHistoryEntry{},
			}
			state.Tasks = append(state.Tasks, task)
			return nil
		})
		testhelpers.AssertNoError(t, err)
	}

	t.Log("Starting concurrent merges")

	// Concurrently merge all approved tasks
	// Use a sync barrier so all goroutines reach the merge call before any proceeds,
	// forcing contention at the stale-head window.
	var wg sync.WaitGroup
	results := make([]error, numTasks)
	ready := make(chan struct{})
	var startWg sync.WaitGroup
	startWg.Add(numTasks)

	for i := 0; i < numTasks; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			startWg.Done() // signal ready
			<-ready        // wait for all goroutines to be ready
			// Each reviewer merges their approved task
			results[index] = commands.WtMergeCommand(projectDir, taskIDs[index], reviewerIDs[index])
		}(i)
	}

	startWg.Wait() // all goroutines spawned and ready
	close(ready)   // release all at once — maximum contention

	wg.Wait()

	// Verify all merges succeeded
	successCount := 0
	for i, err := range results {
		if err != nil {
			t.Logf("Merge %s failed: %v", taskIDs[i], err)
		} else {
			successCount++
			t.Logf("Merge %s succeeded", taskIDs[i])
		}
	}

	if successCount != numTasks {
		t.Errorf("Expected %d successful merges, got %d", numTasks, successCount)
	}

	// Verify final state
	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)

	for _, taskID := range taskIDs {
		task := findTask(state.Tasks, taskID)
		if task == nil {
			t.Errorf("Task %s not found", taskID)
			continue
		}

		if task.Status != models.TaskStatusMerged {
			t.Errorf("Task %s should be MERGED, got %s", taskID, task.Status)
		}

		if task.MergeCommit == nil {
			t.Errorf("Task %s should have merge_commit set", taskID)
		}
	}

	// Verify integration branch contains all changes
	cmd := exec.Command("git", "-C", projectDir, "log", "--oneline", "integration")
	output, err := cmd.Output()
	testhelpers.AssertNoError(t, err)
	logOutput := string(output)

	for _, taskID := range taskIDs {
		if !strings.Contains(logOutput, "Commit for "+taskID) {
			t.Errorf("Integration branch log should contain commit for %s", taskID)
		}
	}

	// The integration ref must contain every merged file while the checked-out
	// main working tree is restored after all concurrent merges complete.
	treeCmd := exec.Command("git", "-C", projectDir, "ls-tree", "-r", "--name-only", "integration")
	treeOutput, err := treeCmd.Output()
	testhelpers.AssertNoError(t, err)
	treeFiles := "\n" + string(treeOutput)
	for _, taskID := range taskIDs {
		featureFile := "feature-" + strings.TrimPrefix(taskID, "task-merge-") + ".txt"
		if !strings.Contains(treeFiles, "\n"+featureFile+"\n") {
			t.Errorf("Integration branch tree should contain %s", featureFile)
		}
		if _, statErr := os.Stat(filepath.Join(projectDir, featureFile)); !os.IsNotExist(statErr) {
			t.Errorf("Checked-out main working tree should not contain %s after restore; stat error = %v", featureFile, statErr)
		}
	}

	t.Log("✓ Concurrent merges test passed")
}
