package integration

// await_verdict_test.go contains an end-to-end integration test for the
// submit → await → rejected → fix → resubmit → approved → merge flow.

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/paths"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const (
	awaitTaskID     = "task-1"
	awaitCoderID    = "coder-1"
	awaitReviewerID = "code-reviewer-1"
)

type awaitIntegrationFixture struct {
	projectRoot string
	bb          *db.Blackboard
}

type awaitVerdictCall struct {
	result *commands.AwaitVerdictResult
	err    error
}

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

	// --- Phase 2: Coder calls the public command adapter ---
	awaitCall := startAwaitVerdict(projectDir, "task-1", coderID, 30*time.Second)

	// Let AwaitVerdict start watching before reviewer acts
	testhelpers.WaitForAsyncSetup()

	// --- Phase 3: Reviewer rejects ---
	testhelpers.TransitionToReviewing(t, bb, "task-1", reviewerID)
	if err := commands.SubmitVerdictCommand(projectDir, "task-1", "REJECTED", "Missing error handling", reviewerID, ""); err != nil {
		t.Fatalf("SubmitVerdict (reject) failed: %v", err)
	}

	// --- Phase 4: Verify rejection result ---
	awaitResult := receiveAwaitVerdict(t, awaitCall, 10*time.Second)
	if awaitResult.Verdict != string(ops.VerdictRejected) {
		t.Fatalf("Verdict = %q, want %q", awaitResult.Verdict, string(ops.VerdictRejected))
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

	// --- Phase 6: Coder awaits and reviewer approves ---
	awaitCall = startAwaitVerdict(projectDir, "task-1", coderID, 30*time.Second)
	testhelpers.WaitForAsyncSetup()
	testhelpers.TransitionToReviewing(t, bb, "task-1", reviewerID)
	if err := commands.SubmitVerdictCommand(projectDir, "task-1", "APPROVED", "", reviewerID, ""); err != nil {
		t.Fatalf("SubmitVerdict (approve) failed: %v", err)
	}
	awaitResult = receiveAwaitVerdict(t, awaitCall, 10*time.Second)
	if awaitResult.Verdict != string(ops.VerdictApproved) {
		t.Fatalf("Verdict = %q, want %q", awaitResult.Verdict, string(ops.VerdictApproved))
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

	// A post-transition caller must stop instead of re-entering the lifecycle.
	awaitCall = startAwaitVerdict(projectDir, "task-1", coderID, 30*time.Second)
	awaitResult = receiveAwaitVerdict(t, awaitCall, 10*time.Second)
	if awaitResult.Verdict != string(ops.VerdictAlreadyTransitioned) {
		t.Fatalf("Post-merge verdict = %q, want %q", awaitResult.Verdict, string(ops.VerdictAlreadyTransitioned))
	}
	if awaitResult.SafeAction != string(ops.SafeActionStop) {
		t.Fatalf("Post-merge safe action = %q, want %q", awaitResult.SafeAction, string(ops.SafeActionStop))
	}
}

func setupIsolatedAwaitProject(t *testing.T) awaitIntegrationFixture {
	t.Helper()

	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	testhelpers.CreateCommittedSpecFileOnIntegration(t, projectRoot, "feature.md", "# Feature")
	if err := ops.InitProject(projectRoot, ops.InitProjectParams{
		Description: "Test goal",
		SpecRef:     "specs/feature.md",
	}); err != nil {
		t.Fatalf("InitProject failed: %v", err)
	}

	statePath := filepath.Join(projectRoot, paths.ProjectDirName(), "state.yaml")
	logPath := filepath.Join(projectRoot, paths.ProjectDirName(), "log.yaml")
	bb := db.New(statePath)
	if err := commands.AddTaskCommand(statePath, logPath, &commands.TaskInput{
		ID:          awaitTaskID,
		RolePair:    "coding-pair",
		Description: "Test bounded await lifecycle",
		DoneWhen:    "Done",
		Scope:       "Feature",
		Priority:    1,
		SpecRef:     "specs/feature.md",
		DependsOn:   []string{},
	}, "orchestrator-1"); err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	testhelpers.RegisterTestAgent(t, bb, awaitCoderID, "coder")
	testhelpers.RegisterTestAgent(t, bb, awaitReviewerID, "code-reviewer")
	if err := commands.ClaimTaskCommand(projectRoot, awaitTaskID, awaitCoderID); err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}

	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	task := state.FindTask(awaitTaskID)
	if task == nil || task.Worktree == nil {
		t.Fatal("claimed task has no worktree")
	}
	worktreePath := filepath.Join(projectRoot, *task.Worktree)
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
	if err := ops.WriteCheckpoint(projectRoot, &ops.WriteCheckpointInput{
		TaskID: awaitTaskID, AgentID: awaitCoderID,
		Intent: "Implement feature", ValidationPlan: "go test ./...",
		FilesToModify: []string{"feature.go"},
	}); err != nil {
		t.Fatalf("WriteCheckpoint failed: %v", err)
	}
	if err := commands.SubmitForReviewCommand(
		projectRoot, awaitTaskID, getHeadSHA(t, worktreePath), awaitCoderID,
	); err != nil {
		t.Fatalf("SubmitForReview failed: %v", err)
	}

	return awaitIntegrationFixture{projectRoot: projectRoot, bb: bb}
}

func rejectAwaitFixture(t *testing.T, fixture awaitIntegrationFixture) {
	t.Helper()
	testhelpers.TransitionToReviewing(t, fixture.bb, awaitTaskID, awaitReviewerID)
	if err := commands.SubmitVerdictCommand(
		fixture.projectRoot,
		awaitTaskID,
		string(ops.VerdictRejected),
		"Missing error handling",
		awaitReviewerID,
		"",
	); err != nil {
		t.Fatalf("SubmitVerdict (reject) failed: %v", err)
	}
}

func startAwaitVerdict(projectRoot, taskID, agentID string, remaining time.Duration) <-chan awaitVerdictCall {
	result := make(chan awaitVerdictCall, 1)
	go func() {
		awaitResult, err := commands.AwaitVerdictWithOptions(
			projectRoot, taskID, agentID, remaining,
			commands.AwaitVerdictOptions{
				FallbackPollInterval: 10 * time.Millisecond,
			},
		)
		result <- awaitVerdictCall{result: awaitResult, err: err}
	}()
	return result
}

func receiveAwaitVerdict(t *testing.T, call <-chan awaitVerdictCall, guard time.Duration) *commands.AwaitVerdictResult {
	t.Helper()
	select {
	case outcome := <-call:
		if outcome.err != nil {
			t.Fatalf("AwaitVerdict returned error: %v", outcome.err)
		}
		if outcome.result == nil {
			t.Fatal("AwaitVerdict returned a nil result")
		}
		return outcome.result
	case <-time.After(guard):
		t.Fatalf("AwaitVerdict caller did not return within %s", guard)
		return nil
	}
}

func assertBoundedAwaitState(t *testing.T, fixture awaitIntegrationFixture, reviewer bool) {
	t.Helper()
	state, err := fixture.bb.Read()
	testhelpers.AssertNoError(t, err)
	task := state.FindTask(awaitTaskID)
	if task == nil {
		t.Fatal("task not found after bounded await return")
	}

	if reviewer {
		if state.Agents[awaitReviewerID].CurrentTask != nil {
			t.Error("reviewer current_task should be nil after bounded await return")
		}
		if task.ReviewingBy != nil {
			t.Error("task reviewing_by should be nil after bounded await return")
		}
		if task.ReviewLeaseExpires != nil {
			t.Error("task review_lease_expires should be nil after bounded await return")
		}
	} else if state.Agents[awaitCoderID].CurrentTask != nil {
		t.Error("doer current_task should be nil after bounded await return")
	}

	if err := statevalidate.ValidateState(state, fixture.projectRoot, false, io.Discard); err != nil {
		t.Fatalf("state should validate after bounded await return: %v", err)
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
