package integration

// await_verdict_test.go contains an end-to-end integration test for the
// submit → await → rejected → fix → resubmit → approved → merge flow.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// TestAwaitVerdict_RejectionFlow exercises the full submit → await → rejected
// → fix → resubmit → approved → merge lifecycle using a real AwaitVerdict
// blocking call.
func TestAwaitVerdict_RejectionFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// --- Setup: project, task, agents ---
	projectDir, cleanup := setupTestProject(t)
	defer cleanup()

	bb, _, _ := setupIntegrationTest(t, projectDir, []string{"task-1"})

	coderID := "coder-1"
	reviewerID := "code-reviewer-1"
	testhelpers.RegisterTestAgent(t, bb, coderID, "coder")
	testhelpers.RegisterTestAgent(t, bb, reviewerID, "code-reviewer")

	// --- Phase 1: Coder claims, implements, submits ---
	if err := commands.ClaimTaskCommand(projectDir, "task-1", coderID); err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}

	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	task := findTask(state.Tasks, "task-1")
	if task == nil {
		t.Fatal("Task not found after claim")
	}
	worktreePath := filepath.Join(projectDir, *task.Worktree)

	// Create code + test file in worktree
	if err := os.WriteFile(filepath.Join(worktreePath, "feature.go"),
		[]byte("package main\n\nfunc Feature() {}\n"), 0644); err != nil {
		t.Fatalf("Failed to create feature.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreePath, "feature_test.go"),
		[]byte("package main\n"), 0644); err != nil {
		t.Fatalf("Failed to create feature_test.go: %v", err)
	}

	if err := exec.Command("git", "-C", worktreePath, "add", "feature.go", "feature_test.go").Run(); err != nil {
		t.Fatalf("git add failed: %v", err)
	}
	if err := exec.Command("git", "-C", worktreePath, "commit", "-m", "feat: initial implementation").Run(); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	// Write checkpoint (required for submission)
	if err := ops.WriteCheckpoint(projectDir, &ops.WriteCheckpointInput{
		TaskID: "task-1", AgentID: coderID,
		Intent: "Implement feature", ValidationPlan: "go test ./...",
		FilesToModify: []string{"feature.go"},
	}); err != nil {
		t.Fatalf("WriteCheckpoint failed: %v", err)
	}

	commitSHA := getHeadSHA(t, worktreePath)
	if err := commands.SubmitForReviewCommand(projectDir, "task-1", commitSHA, coderID); err != nil {
		t.Fatalf("SubmitForReview failed: %v", err)
	}

	// Verify task is ready for review
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, "task-1")
	if task.Status != models.TaskStatusReadyForReview {
		t.Fatalf("Expected READY_FOR_REVIEW, got %s", task.Status)
	}

	// --- Phase 2: Coder calls AwaitVerdict (blocks in goroutine) ---
	var awaitResult *ops.AwaitVerdictResult
	var awaitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		awaitResult, awaitErr = ops.AwaitVerdict(
			context.Background(), projectDir, "task-1", coderID, 30*time.Second)
	}()

	// Let AwaitVerdict start watching before reviewer acts
	testhelpers.WaitForAsyncSetup()

	// --- Phase 3: Reviewer rejects ---
	testhelpers.TransitionToReviewing(t, bb, "task-1", reviewerID)
	if err := commands.SubmitVerdictCommand(projectDir, "task-1", "REJECTED", "Missing error handling", reviewerID, ""); err != nil {
		t.Fatalf("SubmitVerdict (reject) failed: %v", err)
	}

	// --- Phase 4: Verify rejection result ---
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("AwaitVerdict timed out waiting for rejection verdict")
	}

	if awaitErr != nil {
		t.Fatalf("AwaitVerdict returned error: %v", awaitErr)
	}
	if awaitResult.Verdict != ops.VerdictRejected {
		t.Fatalf("Verdict = %q, want %q", awaitResult.Verdict, ops.VerdictRejected)
	}
	if awaitResult.Reason == "" {
		t.Error("Expected non-empty rejection reason")
	}

	// Verify auto-reclaim: task back to IMPLEMENTING_CODE, iteration incremented
	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, "task-1")
	if task.Status != models.TaskStatusImplementing {
		t.Errorf("After rejection: status = %s, want %s", task.Status, models.TaskStatusImplementing)
	}
	if task.Iteration != 2 {
		t.Errorf("After rejection: iteration = %d, want 2", task.Iteration)
	}

	// --- Phase 5: Coder fixes and resubmits ---
	if err := os.WriteFile(filepath.Join(worktreePath, "feature.go"),
		[]byte("package main\n\nimport \"errors\"\n\nvar ErrInvalid = errors.New(\"invalid\")\n\nfunc Feature() error { return nil }\n"), 0644); err != nil {
		t.Fatalf("Failed to write fix: %v", err)
	}
	if err := exec.Command("git", "-C", worktreePath, "add", "feature.go").Run(); err != nil {
		t.Fatalf("git add (fix) failed: %v", err)
	}
	if err := exec.Command("git", "-C", worktreePath, "commit", "-m", "fix: add error handling").Run(); err != nil {
		t.Fatalf("git commit (fix) failed: %v", err)
	}

	newSHA := getHeadSHA(t, worktreePath)
	if err := commands.SubmitForReviewCommand(projectDir, "task-1", newSHA, coderID); err != nil {
		t.Fatalf("SubmitForReview (resubmit) failed: %v", err)
	}

	// --- Phase 6: Reviewer approves ---
	testhelpers.TransitionToReviewing(t, bb, "task-1", reviewerID)
	if err := commands.SubmitVerdictCommand(projectDir, "task-1", "APPROVED", "", reviewerID, ""); err != nil {
		t.Fatalf("SubmitVerdict (approve) failed: %v", err)
	}

	// --- Phase 7: Merge ---
	if err := commands.WtMergeCommand(projectDir, "task-1", reviewerID); err != nil {
		t.Fatalf("WtMerge failed: %v", err)
	}

	state, err = bb.Read()
	testhelpers.AssertNoError(t, err)
	task = findTask(state.Tasks, "task-1")
	if task.Status != models.TaskStatusMerged {
		t.Errorf("After merge: status = %s, want %s", task.Status, models.TaskStatusMerged)
	}
	if task.MergeCommit == nil {
		t.Error("Expected merge commit to be set")
	}
}

// getHeadSHA returns the HEAD commit SHA for the given repo path.
func getHeadSHA(t *testing.T, repoPath string) string {
	t.Helper()
	output, err := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("Failed to get HEAD SHA in %s: %v", repoPath, err)
	}
	return strings.TrimSpace(string(output))
}
