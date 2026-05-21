package ops

import (
	stderrors "errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func replaceSubmitReviewScipRefreshForTest(refresh func(scipsearch.RefreshOptions) (scipsearch.RefreshResult, error)) func() {
	previous := submitReviewRefreshIndexes
	submitReviewRefreshIndexes = refresh
	return func() {
		submitReviewRefreshIndexes = previous
	}
}

func TestSubmitForReview_Validation(t *testing.T) {
	tests := []struct {
		name        string
		taskID      string
		commitRef   string
		agentID     string
		errContains string
	}{
		{
			name: "empty task ID", commitRef: "abc123", agentID: "coder-1",
			errContains: "task ID is required",
		},
		{
			name: "empty commit ref", taskID: "t1", agentID: "coder-1",
			errContains: "commit ref is required",
		},
		{
			name: "empty agent ID", taskID: "t1", commitRef: "abc123",
			errContains: "LIZA_AGENT_ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SubmitForReview("/nonexistent", tt.taskID, tt.commitRef, tt.agentID)
			testhelpers.RequireErrorContains(t, err, tt.errContains)
		})
	}
}

func TestSubmitForReview_TaskNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SubmitForReview(tmpDir, "nonexistent", "abc123", "coder-1")
	if err == nil {
		t.Fatal("Expected error for nonexistent task")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestSubmitForReview_WrongStatus(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SubmitForReview(tmpDir, "task-1", "abc123", "coder-1")
	testhelpers.RequireErrorContains(t, err, "not IMPLEMENTING")
}

func TestSubmitForReview_WrongAgent(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SubmitForReview(tmpDir, "task-1", "abc123", "coder-2")
	testhelpers.RequireErrorContains(t, err, "not assigned to agent")
}

func TestSubmitForReview_NoWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	task.Worktree = nil // No worktree
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SubmitForReview(tmpDir, "task-1", "abc123", "coder-1")
	testhelpers.RequireErrorContains(t, err, "no worktree")
}

func TestSubmitForReview_TDDWaiverBypassesTestRequirement(t *testing.T) {
	// Unit test: verify that GetTDDWaiver check in SubmitForReview
	// allows submission without test files when waiver is declared.
	// This tests the waiver logic at the data level since the full
	// SubmitForReview path requires a real git worktree.
	agent := "coder-1"
	history := []models.TaskHistoryEntry{
		{
			Event: models.TaskEventPreExecutionCheckpoint,
			Agent: &agent,
			Extra: map[string]any{
				"intent":           "Fix comment typo",
				"tdd_not_required": "cosmetic-only: comment fix, no behavior change",
			},
		},
	}

	// With waiver, GetTDDWaiver should return non-empty
	waiver := GetTDDWaiver(history, "coder-1")
	if waiver == "" {
		t.Fatal("Expected non-empty waiver from checkpoint with tdd_not_required")
	}
	if waiver != "cosmetic-only: comment fix, no behavior change" {
		t.Errorf("Unexpected waiver value: %q", waiver)
	}

	// Without waiver, GetTDDWaiver should return empty
	historyNoWaiver := []models.TaskHistoryEntry{
		{
			Event: models.TaskEventPreExecutionCheckpoint,
			Agent: &agent,
			Extra: map[string]any{
				"intent": "Add feature",
			},
		},
	}
	if GetTDDWaiver(historyNoWaiver, "coder-1") != "" {
		t.Fatal("Expected empty waiver from checkpoint without tdd_not_required")
	}
}

func TestSubmitForReview_NoCheckpoint(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	// Task has worktree but no checkpoint in history
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SubmitForReview(tmpDir, "task-1", "abc123", "coder-1")
	testhelpers.RequireErrorContains(t, err, "pre-execution checkpoint required")
}

// setupRebaseConflictScenario creates a git repo with a worktree whose branch
// conflicts with integration. Returns (tmpDir, taskID, worktreeCommitSHA, agentID, blackboard).
func setupRebaseConflictScenario(t *testing.T) (string, string, string, string, *db.Blackboard) {
	t.Helper()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	testhelpers.MustGit(t, tmpDir, "checkout", "integration")

	g := git.New(tmpDir)
	taskID := "task-rebase-conflict"
	baseCommit, err := g.CreateWorktree(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wtPath := g.GetWorktreePath(taskID)

	// Modify README in worktree (will conflict) and add test file for TDD
	if err := os.WriteFile(filepath.Join(wtPath, "README.md"), []byte("# Task version\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtPath, "task_test.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "README.md", "task_test.go")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Task commit")
	wtCommit := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")

	// Create conflicting change on integration branch
	testhelpers.MustGit(t, tmpDir, "checkout", "integration")
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Integration version\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, tmpDir, "add", "README.md")
	testhelpers.MustGit(t, tmpDir, "commit", "-m", "Integration commit")

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
				Description:  "Task with rebase conflict",
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
							"intent":          "test",
							"validation_plan": "test",
							"files_to_modify": []string{"README.md"},
						},
					},
				},
			},
		},
		Agents: map[string]models.Agent{
			agentID: {Status: models.AgentStatusWorking, CurrentTask: &taskID},
		},
	}

	bb := testhelpers.WriteInitialState(t, statePath, initialState)
	return tmpDir, taskID, wtCommit, agentID, bb
}

func TestSubmitForReview_RebaseConflict_TransitionsToIntegrationFailed(t *testing.T) {
	tmpDir, taskID, wtCommit, agentID, bb := setupRebaseConflictScenario(t)

	_, err := SubmitForReview(tmpDir, taskID, wtCommit, agentID)
	if err == nil {
		t.Fatal("expected error due to rebase conflict, got nil")
	}

	// Should return IntegrationFailedError
	var ifErr *IntegrationFailedError
	if !stderrors.As(err, &ifErr) {
		t.Fatalf("expected *IntegrationFailedError, got %T: %v", err, err)
	}
	if ifErr.Reason != IntegrationReasonMergeConflict {
		t.Errorf("expected reason %q, got %q", IntegrationReasonMergeConflict, ifErr.Reason)
	}

	// Task should be INTEGRATION_FAILED
	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	task := state.FindTask(taskID)
	if task.Status != models.TaskStatusIntegrationFailed {
		t.Errorf("expected INTEGRATION_FAILED, got %s", task.Status)
	}

	// Agent should be released
	if task.AssignedTo != nil {
		t.Errorf("expected agent released (AssignedTo nil), got %v", *task.AssignedTo)
	}
	agent := state.Agents[agentID]
	if agent.Status != models.AgentStatusWaiting {
		t.Errorf("expected agent status WAITING, got %s", agent.Status)
	}
	if agent.CurrentTask != nil {
		t.Errorf("expected agent CurrentTask nil, got %v", *agent.CurrentTask)
	}

	// FailedBy should include the agent
	if len(task.FailedBy) == 0 || task.FailedBy[0] != agentID {
		t.Errorf("expected FailedBy to include %s, got %v", agentID, task.FailedBy)
	}

	// History should have integration_failed entry
	found := false
	for _, h := range task.History {
		if h.Event == models.TaskEventIntegrationFailed {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected integration_failed history entry")
	}

	// Worktree should be clean (rebase aborted) — verify by checking branch
	g := git.New(tmpDir)
	wtPath := g.GetWorktreePath(taskID)
	branch, err := g.GetWorktreeBranch(wtPath)
	if err != nil {
		t.Fatalf("failed to get worktree branch: %v", err)
	}
	if branch == "" {
		t.Error("worktree in detached HEAD state — rebase was not aborted")
	}

	// ReviewCommit should NOT be set
	if task.ReviewCommit != nil {
		t.Errorf("expected ReviewCommit nil, got %v", *task.ReviewCommit)
	}
}

func TestSubmitForReview_RebaseConflict_SecondFailureBlocksTask(t *testing.T) {
	tmpDir, taskID, wtCommit, agentID, bb := setupRebaseConflictScenario(t)

	err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		task.FailedBy = []string{"coder-2"}
		return nil
	})
	testhelpers.AssertNoError(t, err)

	_, err = SubmitForReview(tmpDir, taskID, wtCommit, agentID)
	if err == nil {
		t.Fatal("expected error due to rebase conflict, got nil")
	}

	var ifErr *IntegrationFailedError
	if !stderrors.As(err, &ifErr) {
		t.Fatalf("expected *IntegrationFailedError, got %T: %v", err, err)
	}

	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	task := state.FindTask(taskID)
	if task.Status != models.TaskStatusBlocked {
		t.Fatalf("task status = %s, want %s", task.Status, models.TaskStatusBlocked)
	}
	if task.BlockedReason == nil || !strings.Contains(*task.BlockedReason, "hypothesis_exhaustion") {
		t.Fatalf("BlockedReason = %v, want hypothesis_exhaustion", task.BlockedReason)
	}
	if task.AssignedTo != nil || task.LeaseExpires != nil {
		t.Fatalf("blocked exhausted task should be unassigned with no lease, assigned=%v lease=%v", task.AssignedTo, task.LeaseExpires)
	}
	if len(task.FailedBy) != 2 || task.FailedBy[0] != "coder-2" || task.FailedBy[1] != agentID {
		t.Fatalf("FailedBy = %v, want [coder-2 %s]", task.FailedBy, agentID)
	}
}

func TestSubmitForReview_RebaseConflict_RecordsSubmissionHandoffForIntegrationFailed(t *testing.T) {
	tmpDir, taskID, wtCommit, agentID, bb := setupRebaseConflictScenario(t)

	_, err := SubmitForReview(tmpDir, taskID, wtCommit, agentID)
	if err == nil {
		t.Fatal("expected error due to rebase conflict, got nil")
	}

	var ifErr *IntegrationFailedError
	if !stderrors.As(err, &ifErr) {
		t.Fatalf("expected *IntegrationFailedError, got %T: %v", err, err)
	}

	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatal("task not found")
	}
	if task.Status != models.TaskStatusIntegrationFailed {
		t.Fatalf("task status = %s, want %s", task.Status, models.TaskStatusIntegrationFailed)
	}

	foundSubmission := false
	for _, event := range task.HandoffEvents {
		if event.Trigger == models.HandoffTriggerSubmission {
			foundSubmission = true
			break
		}
	}
	if !foundSubmission {
		t.Fatalf("INTEGRATION_FAILED task missing submission handoff event; handoff_events = %+v", task.HandoffEvents)
	}

	var diagnostic map[string]any
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		if entry.Event != models.TaskEventIntegrationFailed {
			continue
		}
		var ok bool
		diagnostic, ok = entry.Extra["diagnostic"].(map[string]any)
		if !ok {
			t.Fatalf("integration_failed history entry missing diagnostic; Extra = %#v", entry.Extra)
		}
		break
	}
	if diagnostic == nil {
		t.Fatal("missing integration_failed history entry")
	}
	if diagnostic["operation"] != "submit-for-review" {
		t.Errorf("diagnostic operation = %v, want submit-for-review", diagnostic["operation"])
	}
	if diagnostic["reason"] != IntegrationReasonMergeConflict {
		t.Errorf("diagnostic reason = %v, want %q", diagnostic["reason"], IntegrationReasonMergeConflict)
	}
	if diagnostic["recovery_hint"] == "" {
		t.Error("diagnostic recovery_hint is empty")
	}
}

// setupSuccessfulSubmitScenario creates a git repo with a worktree that can be
// cleanly rebased onto integration. Returns (tmpDir, taskID, worktreeCommitSHA, agentID, blackboard).
func setupSuccessfulSubmitScenario(t *testing.T) (string, string, string, string, *db.Blackboard) {
	t.Helper()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	testhelpers.MustGit(t, tmpDir, "checkout", "integration")

	g := git.New(tmpDir)
	taskID := "task-submit-handoff"
	baseCommit, err := g.CreateWorktree(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wtPath := g.GetWorktreePath(taskID)

	// Add a source file and a test file (TDD requirement)
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
	initialState := &models.State{
		Config: models.Config{
			IntegrationBranch: "integration",
			LeaseDuration:     1800,
		},
		Tasks: []models.Task{
			{
				ID:           taskID,
				Description:  "Task for handoff event test",
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
							"intent":          "test handoff event",
							"validation_plan": "verify handoff event appended",
							"files_to_modify": []string{"feature.go"},
						},
					},
				},
			},
		},
		Agents: map[string]models.Agent{
			agentID: {Status: models.AgentStatusWorking, CurrentTask: &taskID},
		},
	}

	bb := testhelpers.WriteInitialState(t, statePath, initialState)
	return tmpDir, taskID, wtCommit, agentID, bb
}

func TestSubmitForReview_WritesHandoffEvent(t *testing.T) {
	tmpDir, taskID, wtCommit, agentID, bb := setupSuccessfulSubmitScenario(t)

	result, err := SubmitForReview(tmpDir, taskID, wtCommit, agentID)
	if err != nil {
		t.Fatalf("SubmitForReview() unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("SubmitForReview() returned nil result")
	}

	// Read state and verify HandoffEvent
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("bb.Read() error: %v", err)
	}
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatal("task not found after submission")
	}

	if len(task.HandoffEvents) != 1 {
		t.Fatalf("expected 1 HandoffEvent, got %d", len(task.HandoffEvents))
	}

	event := task.HandoffEvents[0]
	if event.Trigger != models.HandoffTriggerSubmission {
		t.Errorf("HandoffEvent.Trigger = %q, want %q", event.Trigger, models.HandoffTriggerSubmission)
	}
	if event.Agent != agentID {
		t.Errorf("HandoffEvent.Agent = %q, want %q", event.Agent, agentID)
	}
	if event.Timestamp.IsZero() {
		t.Error("HandoffEvent.Timestamp is zero")
	}
	// Submission is auto-generated: succeeded/failed should be empty
	if len(event.Succeeded) != 0 {
		t.Errorf("HandoffEvent.Succeeded should be empty for submission, got %v", event.Succeeded)
	}
	if len(event.Failed) != 0 {
		t.Errorf("HandoffEvent.Failed should be empty for submission, got %v", event.Failed)
	}

	lastHistory := task.History[len(task.History)-1]
	if lastHistory.Event != models.TaskEventSubmittedForReview {
		t.Fatalf("History event = %q, want %q", lastHistory.Event, models.TaskEventSubmittedForReview)
	}
	if lastHistory.Commit == nil || *lastHistory.Commit != result.ReviewCommit {
		t.Fatalf("History commit = %v, want %s", lastHistory.Commit, result.ReviewCommit)
	}
}

func TestSubmitForReview_RebaseRewriteUsesPostRebaseHead(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	testhelpers.MustGit(t, tmpDir, "checkout", "integration")
	g := git.New(tmpDir)
	taskID := "task-rebase-rewrite"
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
	preRebaseCommit := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")

	testhelpers.MustGit(t, tmpDir, "checkout", "integration")
	if err := os.WriteFile(filepath.Join(tmpDir, "integration.txt"), []byte("integration advanced\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, tmpDir, "add", "integration.txt")
	testhelpers.MustGit(t, tmpDir, "commit", "-m", "Advance integration")

	agentID := "coder-1"
	reviewerID := "code-reviewer-1"
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
				Type:         models.TaskTypeCoding,
				Description:  "Task whose rebase rewrites HEAD",
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
							"intent":          "exercise rewriting submit rebase",
							"validation_plan": "review_commit equals post-rebase HEAD",
							"files_to_modify": []string{"feature.go", "feature_test.go"},
						},
					},
				},
			},
		},
		Agents: map[string]models.Agent{
			agentID:    {Role: "coder", Status: models.AgentStatusWorking, CurrentTask: &taskID},
			reviewerID: testhelpers.RegisteredTestAgent(models.RoleCodeReviewer),
		},
	}
	bb := testhelpers.WriteInitialState(t, statePath, initialState)

	result, err := SubmitForReview(tmpDir, taskID, preRebaseCommit, agentID)
	if err != nil {
		t.Fatalf("SubmitForReview() unexpected error: %v", err)
	}
	postRebaseHead := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")
	if preRebaseCommit == postRebaseHead {
		t.Fatal("test setup failed: rebase did not rewrite the task commit")
	}
	if result.ReviewCommit != postRebaseHead {
		t.Fatalf("result.ReviewCommit = %s, want post-rebase HEAD %s", result.ReviewCommit, postRebaseHead)
	}

	state, err := bb.Read()
	if err != nil {
		t.Fatalf("bb.Read() error: %v", err)
	}
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatal("task not found after submission")
	}
	if task.ReviewCommit == nil || *task.ReviewCommit != postRebaseHead {
		t.Fatalf("task.ReviewCommit = %v, want %s", task.ReviewCommit, postRebaseHead)
	}
	lastHistory := task.History[len(task.History)-1]
	if lastHistory.Event != models.TaskEventSubmittedForReview {
		t.Fatalf("History event = %q, want %q", lastHistory.Event, models.TaskEventSubmittedForReview)
	}
	if lastHistory.Commit == nil || *lastHistory.Commit != postRebaseHead {
		t.Fatalf("History commit = %v, want %s", lastHistory.Commit, postRebaseHead)
	}

	claimResult, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       reviewerID,
		LeaseDuration: 1800,
	})
	if err != nil {
		t.Fatalf("ClaimReviewerTask() error: %v", err)
	}
	if claimResult.ReviewCommit != postRebaseHead {
		t.Fatalf("claim ReviewCommit = %s, want %s", claimResult.ReviewCommit, postRebaseHead)
	}
}

func TestSubmitForReview_ScipRefreshesPostRebaseCandidateBeforeSubmittedTransition(t *testing.T) {
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	testhelpers.MustGit(t, tmpDir, "checkout", "integration")
	g := git.New(tmpDir)
	taskID := "task-submit-scip-refresh"
	baseCommit, err := g.CreateWorktree(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wtPath := g.GetWorktreePath(taskID)

	if err := os.WriteFile(filepath.Join(wtPath, "feature.go"), []byte("package main\n\nfunc Feature() string { return \"post-rebase\" }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtPath, "feature_test.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "feature.go", "feature_test.go")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Add scip-indexed feature")
	preRebaseCommit := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")

	testhelpers.MustGit(t, tmpDir, "checkout", "integration")
	if err := os.WriteFile(filepath.Join(tmpDir, "integration.txt"), []byte("integration advanced\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, tmpDir, "add", "integration.txt")
	testhelpers.MustGit(t, tmpDir, "commit", "-m", "Advance integration")

	agentID := "coder-1"
	leaseExpires := time.Now().UTC().Add(30 * time.Minute)
	worktree := g.GetWorktreeRelPath(taskID)
	initialState := &models.State{
		Config: models.Config{
			IntegrationBranch: "integration",
			LeaseDuration:     1800,
			ScipSearch:        []string{"go"},
		},
		Tasks: []models.Task{
			{
				ID:           taskID,
				Type:         models.TaskTypeCoding,
				Description:  "Task whose submit refreshes scip indexes",
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
							"intent":          "exercise submit scip refresh",
							"validation_plan": "index contains post-rebase candidate before submitted transition",
							"files_to_modify": []string{"feature.go", "feature_test.go"},
						},
					},
				},
			},
		},
		Agents: map[string]models.Agent{
			agentID: {Role: "coder", Status: models.AgentStatusWorking, CurrentTask: &taskID},
		},
	}
	bb := testhelpers.WriteInitialState(t, statePath, initialState)

	refreshCalls := 0
	restore := replaceSubmitReviewScipRefreshForTest(func(opts scipsearch.RefreshOptions) (scipsearch.RefreshResult, error) {
		refreshCalls++
		if opts.TargetRoot != wtPath {
			t.Errorf("TargetRoot = %q, want %q", opts.TargetRoot, wtPath)
		}
		if opts.TargetKind != scipsearch.TargetKindTaskWorktree {
			t.Errorf("TargetKind = %q, want task worktree", opts.TargetKind)
		}
		state, err := bb.Read()
		if err != nil {
			t.Fatalf("bb.Read() during refresh: %v", err)
		}
		task := state.FindTask(taskID)
		if task == nil {
			t.Fatal("task not found during refresh")
		}
		if task.Status != models.TaskStatusImplementing {
			t.Fatalf("refresh ran after submitted transition: status = %s", task.Status)
		}
		opts.Runner = func(plan scipsearch.RuntimeCommandPlan) (string, error) {
			postRebaseHead := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")
			if postRebaseHead == preRebaseCommit {
				t.Fatal("refresh saw pre-rebase HEAD")
			}
			source, err := os.ReadFile(filepath.Join(wtPath, "feature.go"))
			if err != nil {
				return "", err
			}
			content := fmt.Sprintf("%s\n%s", postRebaseHead, source)
			if err := os.WriteFile(plan.OutputPath, []byte(content), 0644); err != nil {
				return "", err
			}
			return "", nil
		}
		return scipsearch.RefreshIndexes(opts)
	})
	defer restore()

	result, err := SubmitForReview(tmpDir, taskID, preRebaseCommit, agentID)
	if err != nil {
		t.Fatalf("SubmitForReview() unexpected error: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none", result.Warnings)
	}
	if refreshCalls != 1 {
		t.Fatalf("refresh calls = %d, want 1", refreshCalls)
	}

	postRebaseHead := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")
	indexPath := filepath.Join(wtPath, ".liza", "scip", "go.scip")
	indexContent, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read regenerated index: %v", err)
	}
	if !strings.Contains(string(indexContent), postRebaseHead) || !strings.Contains(string(indexContent), "post-rebase") {
		t.Fatalf("index content = %q, want post-rebase review candidate", indexContent)
	}
	available, err := scipsearch.AvailableIndexes(scipsearch.RuntimePlanOptions{
		TargetRoot:          wtPath,
		ConfiguredLanguages: []string{"go"},
	})
	if err != nil {
		t.Fatalf("AvailableIndexes() error = %v", err)
	}
	if len(available) != 1 || available[0].Language != "go" || available[0].Path != indexPath {
		t.Fatalf("AvailableIndexes() = %#v, want regenerated go index", available)
	}
	if status := testhelpers.MustGit(t, wtPath, "status", "--porcelain"); status != "" {
		t.Fatalf("git status --porcelain = %q, want clean", status)
	}
}

func TestSubmitForReview_ScipFailureWarnsAndOmitsFailedLanguage(t *testing.T) {
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	tmpDir, taskID, wtCommit, agentID, bb := setupSuccessfulSubmitScenario(t)
	g := git.New(tmpDir)
	wtPath := g.GetWorktreePath(taskID)

	if err := bb.Modify(func(state *models.State) error {
		state.Config.ScipSearch = []string{"go"}
		return nil
	}); err != nil {
		t.Fatalf("set scip config: %v", err)
	}

	restore := replaceSubmitReviewScipRefreshForTest(func(opts scipsearch.RefreshOptions) (scipsearch.RefreshResult, error) {
		opts.Runner = func(scipsearch.RuntimeCommandPlan) (string, error) {
			return "compiler exploded", stderrors.New("scip-go failed")
		}
		return scipsearch.RefreshIndexes(opts)
	})
	defer restore()

	result, err := SubmitForReview(tmpDir, taskID, wtCommit, agentID)
	if err != nil {
		t.Fatalf("SubmitForReview() unexpected error: %v", err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "scip-search go:") || !strings.Contains(result.Warnings[0], "scip-go failed") {
		t.Fatalf("Warnings = %v, want go scip-search failure", result.Warnings)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("bb.Read() error = %v", err)
	}
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatal("task not found after submission")
	}
	if task.Status != models.TaskStatusReadyForReview {
		t.Fatalf("task status = %s, want READY_FOR_REVIEW", task.Status)
	}
	available, err := scipsearch.AvailableIndexes(scipsearch.RuntimePlanOptions{
		TargetRoot:          wtPath,
		ConfiguredLanguages: []string{"go"},
	})
	if err != nil {
		t.Fatalf("AvailableIndexes() error = %v", err)
	}
	if len(available) != 0 {
		t.Fatalf("AvailableIndexes() = %#v, want failed go language omitted", available)
	}
}

func TestSubmitForReview_DoesNotFetchIntegrationBeforeRebase(t *testing.T) {
	tmpDir, taskID, wtCommit, agentID, bb := setupSuccessfulSubmitScenario(t)
	installGitShimFailingFetch(t)

	result, err := SubmitForReview(tmpDir, taskID, wtCommit, agentID)
	if err != nil {
		t.Fatalf("SubmitForReview() unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("SubmitForReview() returned nil result")
	}

	state, err := bb.Read()
	if err != nil {
		t.Fatalf("bb.Read() error: %v", err)
	}
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatal("task not found after submission")
	}
	if task.BaseCommit == nil || *task.BaseCommit == "" {
		t.Fatal("expected task.BaseCommit to be updated to resolved integration commit")
	}
	if *task.BaseCommit != testhelpers.MustGit(t, tmpDir, "rev-parse", "integration") {
		t.Fatalf("task.BaseCommit = %s, want integration HEAD", *task.BaseCommit)
	}
}

func installGitShimFailingFetch(t *testing.T) {
	t.Helper()

	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("find real git: %v", err)
	}

	binDir := t.TempDir()
	shimPath := filepath.Join(binDir, "git")
	escapedRealGit := strings.ReplaceAll(realGit, `"`, `\"`)
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"fetch\" ]; then\n" +
		"  echo \"shim blocked git fetch\" >&2\n" +
		"  exit 42\n" +
		"fi\n" +
		"exec \"" + escapedRealGit + "\" \"$@\"\n"
	if err := os.WriteFile(shimPath, []byte(script), 0755); err != nil {
		t.Fatalf("write git shim: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestSubmitForReview_TDDEnforcement_AcceptsNestedPythonTestFile(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	testhelpers.MustGit(t, tmpDir, "checkout", "integration")

	g := git.New(tmpDir)
	taskID := "task-nested-python-submit"
	baseCommit, err := g.CreateWorktree(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wtPath := g.GetWorktreePath(taskID)

	testDir := filepath.Join(wtPath, "tests", "backend")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtPath, "source_resolution.py"), []byte("def resolve():\n    return True\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(testDir, "test_source_resolution.py"), []byte("def test_resolve():\n    assert True\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "source_resolution.py", "tests/backend/test_source_resolution.py")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Add source resolution with nested Python test")
	wtCommit := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")

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
				Type:         models.TaskTypeCoding,
				Description:  "Task with nested Python test",
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
							"intent":          "test nested Python TDD detection",
							"validation_plan": "submit-for-review accepts nested Python test file",
							"files_to_modify": []string{"source_resolution.py", "tests/backend/test_source_resolution.py"},
						},
					},
				},
			},
		},
		Agents: map[string]models.Agent{
			agentID: {Status: models.AgentStatusWorking, CurrentTask: &taskID},
		},
	}
	testhelpers.WriteInitialState(t, statePath, initialState)

	if _, err := SubmitForReview(tmpDir, taskID, wtCommit, agentID); err != nil {
		t.Fatalf("SubmitForReview() unexpected error: %v", err)
	}
}

func TestSubmitForReview_ResolvesHeadInWorktree(t *testing.T) {
	tmpDir, taskID, _, agentID, bb := setupSuccessfulSubmitScenario(t)

	result, err := SubmitForReview(tmpDir, taskID, "HEAD", agentID)
	if err != nil {
		t.Fatalf("SubmitForReview() unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("SubmitForReview() returned nil result")
	}

	state, err := bb.Read()
	if err != nil {
		t.Fatalf("bb.Read() error: %v", err)
	}
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatal("task not found after submission")
	}
	if task.ReviewCommit == nil {
		t.Fatal("task.ReviewCommit is nil")
	}
	if *task.ReviewCommit != result.ReviewCommit {
		t.Fatalf("task.ReviewCommit = %s, want %s", *task.ReviewCommit, result.ReviewCommit)
	}
}

// TestSubmitForReview_TDDEnforcement_CustomDoerRole verifies that TDD enforcement
// applies to any doer role (not just the literal "coder" role) by using a custom
// pipeline config with a custom doer role name.
func TestSubmitForReview_TDDEnforcement_CustomDoerRole(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	// Overwrite the default pipeline config with one that defines a custom doer role.
	customPipeline := `pipeline:
  roles:
    custom-doer:
      type: doer
      display-name: "Custom Doer"
      allowed-operations:
        - write-checkpoint
        - submit-for-review
        - mark-blocked
        - handoff
    custom-reviewer:
      type: reviewer
      display-name: "Custom Reviewer"
      allowed-operations:
        - submit-verdict
  role-pairs:
    custom-pair:
      doer: custom-doer
      reviewer: custom-reviewer
      states:
        initial: CUSTOM_READY
        executing: CUSTOM_EXECUTING
        submitted: CUSTOM_SUBMITTED
        reviewing: CUSTOM_REVIEWING
        approved: CUSTOM_APPROVED
        rejected: CUSTOM_REJECTED
  sub-pipelines: {}
  pipeline-transitions: []
  entry-points: {}
`
	if err := os.WriteFile(filepath.Join(tmpDir, ".liza", "pipeline.yaml"), []byte(customPipeline), 0644); err != nil {
		t.Fatalf("Failed to write custom pipeline config: %v", err)
	}

	testhelpers.MustGit(t, tmpDir, "checkout", "integration")

	g := git.New(tmpDir)
	taskID := "task-custom-doer-tdd"
	baseCommit, err := g.CreateWorktree(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wtPath := g.GetWorktreePath(taskID)

	// Add a non-test file only (no test files — should trigger TDD enforcement).
	if err := os.WriteFile(filepath.Join(wtPath, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "main.go")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Non-test commit")
	wtCommit := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")

	agentID := "custom-doer-1"
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
				Type:         models.TaskTypeCoding,
				Description:  "Custom doer TDD enforcement test",
				Status:       "CUSTOM_EXECUTING",
				RolePair:     "custom-pair",
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
							"intent":          "test custom doer TDD",
							"validation_plan": "verify TDD enforcement",
							"files_to_modify": []string{"main.go"},
						},
					},
				},
			},
		},
		Agents: map[string]models.Agent{
			agentID: {Role: "custom-doer", Status: models.AgentStatusWorking, CurrentTask: &taskID},
		},
	}

	testhelpers.WriteInitialState(t, statePath, initialState)

	_, err = SubmitForReview(tmpDir, taskID, wtCommit, agentID)
	if err == nil {
		t.Fatal("Expected TDD enforcement error for custom doer role, got nil")
	}
	testhelpers.RequireErrorContains(t, err, "code tasks must include test files")
	testhelpers.RequireErrorContains(t, err, "--tdd-not-required")
	testhelpers.RequireErrorContains(t, err, "documentation/config/spec-only")
}

func TestSubmitForReview_TDDFailure_IncludesDiagnosticDetails(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	testhelpers.MustGit(t, tmpDir, "checkout", "integration")

	g := git.New(tmpDir)
	taskID := "task-tdd-diagnostics"
	baseCommit, err := g.CreateWorktree(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wtPath := g.GetWorktreePath(taskID)

	if err := os.WriteFile(filepath.Join(wtPath, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "main.go")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Non-test commit")
	wtCommit := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")

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
				Type:         models.TaskTypeCoding,
				Description:  "TDD diagnostic details test",
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
							"intent":          "test TDD diagnostics",
							"validation_plan": "verify missing-test error includes inspected files",
							"files_to_modify": []string{"main.go"},
						},
					},
				},
			},
		},
		Agents: map[string]models.Agent{
			agentID: {Status: models.AgentStatusWorking, CurrentTask: &taskID},
		},
	}
	testhelpers.WriteInitialState(t, statePath, initialState)

	_, err = SubmitForReview(tmpDir, taskID, wtCommit, agentID)
	if err == nil {
		t.Fatal("Expected TDD enforcement error, got nil")
	}

	var precondition *PreconditionError
	if !stderrors.As(err, &precondition) {
		t.Fatalf("expected *PreconditionError, got %T: %v", err, err)
	}
	if precondition.Details["base_ref"] != baseCommit {
		t.Errorf("base_ref = %v, want %s", precondition.Details["base_ref"], baseCommit)
	}
	if precondition.Details["head_ref"] != wtCommit {
		t.Errorf("head_ref = %v, want %s", precondition.Details["head_ref"], wtCommit)
	}
	changed, ok := precondition.Details["changed_files_considered"].([]string)
	if !ok {
		t.Fatalf("changed_files_considered has type %T, want []string", precondition.Details["changed_files_considered"])
	}
	if len(changed) != 1 || changed[0] != "main.go" {
		t.Errorf("changed_files_considered = %v, want [main.go]", changed)
	}
	matched, ok := precondition.Details["test_files_matched"].([]string)
	if !ok {
		t.Fatalf("test_files_matched has type %T, want []string", precondition.Details["test_files_matched"])
	}
	if len(matched) != 0 {
		t.Errorf("test_files_matched = %v, want empty", matched)
	}
	patterns, ok := precondition.Details["matcher_patterns"].([]string)
	if !ok {
		t.Fatalf("matcher_patterns has type %T, want []string", precondition.Details["matcher_patterns"])
	}
	if !containsString(patterns, "test_*.py") {
		t.Errorf("matcher_patterns = %v, want to contain test_*.py", patterns)
	}
}

func TestSubmitForReview_TDDEnforcement_UsesRolePairOverStaleCodingType(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	testhelpers.MustGit(t, tmpDir, "checkout", "integration")

	g := git.New(tmpDir)
	taskID := "task-us-writing-stale-coding-type"
	baseCommit, err := g.CreateWorktree(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wtPath := g.GetWorktreePath(taskID)

	storyDir := filepath.Join(wtPath, "specs", "stories")
	if err := os.MkdirAll(storyDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storyDir, "cli-routing.md"), []byte("# CLI routing story\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "specs/stories/cli-routing.md")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Add CLI routing story")
	wtCommit := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")

	agentID := "us-writer-1"
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
				Type:         models.TaskTypeCoding,
				Description:  "US writing task with stale coding type",
				Status:       "WRITING_US",
				RolePair:     "us-writing-pair",
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
							"intent":          "write docs-only user story",
							"validation_plan": "verify story markdown exists and submit for review",
							"files_to_modify": []string{"specs/stories/cli-routing.md"},
						},
					},
				},
			},
		},
		Agents: map[string]models.Agent{
			agentID: {Role: "us-writer", Status: models.AgentStatusWorking, CurrentTask: &taskID},
		},
	}
	bb := testhelpers.WriteInitialState(t, statePath, initialState)
	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver() error = %v", err)
	}
	wantSubmittedStatus, err := resolver.SubmittedStatus("us-writing-pair")
	if err != nil {
		t.Fatalf("SubmittedStatus(us-writing-pair) error = %v", err)
	}

	if _, err := SubmitForReview(tmpDir, taskID, wtCommit, agentID); err != nil {
		t.Fatalf("SubmitForReview() unexpected error: %v", err)
	}

	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatal("task not found after submission")
	}
	if task.Status != wantSubmittedStatus {
		t.Errorf("Status = %v, want %s", task.Status, wantSubmittedStatus)
	}
}

func TestSubmitForReview_NonConflictRebaseDetailsIncludeRecoveryContext(t *testing.T) {
	err := &git.RebaseError{
		Command: []string{"git", "rebase", "abc123"},
		Output:  "fatal: It seems that there is already a rebase-merge directory.",
		Err:     stderrors.New("exit status 128"),
	}

	details := rebaseFailureDetails(err, "integration", "abc123", "def456")
	if details["command"] != "git rebase abc123" {
		t.Errorf("command = %v, want git rebase abc123", details["command"])
	}
	if details["integration_branch"] != "integration" {
		t.Errorf("integration_branch = %v, want integration", details["integration_branch"])
	}
	if details["integration_head"] != "abc123" {
		t.Errorf("integration_head = %v, want abc123", details["integration_head"])
	}
	if details["rebase_base_commit"] != "abc123" {
		t.Errorf("rebase_base_commit = %v, want abc123", details["rebase_base_commit"])
	}
	if details["pre_rebase_head"] != "def456" {
		t.Errorf("pre_rebase_head = %v, want def456", details["pre_rebase_head"])
	}
	if details["rebase_base_ref"] != "integration" {
		t.Errorf("rebase_base_ref = %v, want integration", details["rebase_base_ref"])
	}
	if !strings.Contains(details["stdout_stderr_excerpt"].(string), "rebase-merge") {
		t.Errorf("stdout_stderr_excerpt = %v, want rebase output excerpt", details["stdout_stderr_excerpt"])
	}
	if !strings.Contains(details["recovery_hint"].(string), "retry submit-for-review") {
		t.Errorf("recovery_hint = %v, want retry guidance", details["recovery_hint"])
	}
}

// TestSubmitForReview_ZeroDiffIntegration verifies that SubmitForReview succeeds
// when the integration analyst has not committed any code changes to its worktree.
// The analyst produces findings (not code), so zero-diff submission must work:
// - TDD gate is bypassed because EffectiveType() returns TaskTypeIntegration
// - Rebase is a no-op (no new commits on the task branch)
// - Task transitions to INTEGRATION_ANALYSIS_TO_REVIEW
func TestSubmitForReview_ZeroDiffIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	testhelpers.MustGit(t, tmpDir, "checkout", "integration")

	g := git.New(tmpDir)
	taskID := "task-zero-diff-integration"
	baseCommit, err := g.CreateWorktree(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	// NO commits on worktree branch — zero-diff scenario
	wtCommit := testhelpers.MustGit(t, g.GetWorktreePath(taskID), "rev-parse", "HEAD")

	agentID := "integration-analyst-1"
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
				Type:         models.TaskTypeIntegration,
				Description:  "Integration analysis — zero-diff test",
				Status:       "ANALYZING_INTEGRATION",
				RolePair:     "integration-pair",
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
							"intent":          "scan branch for integration issues",
							"validation_plan": "verify findings against branch diff",
							"files_to_modify": []string{},
						},
					},
				},
			},
		},
		Agents: map[string]models.Agent{
			agentID: {Role: "integration-analyst", Status: models.AgentStatusWorking, CurrentTask: &taskID},
		},
	}

	bb := testhelpers.WriteInitialState(t, statePath, initialState)

	result, err := SubmitForReview(tmpDir, taskID, wtCommit, agentID)
	if err != nil {
		t.Fatalf("SubmitForReview() unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("SubmitForReview() returned nil result")
	}

	// Verify ReviewCommit is non-empty
	if result.ReviewCommit == "" {
		t.Error("expected non-empty ReviewCommit")
	}

	// Read state and verify task transitioned to INTEGRATION_ANALYSIS_TO_REVIEW
	state, err := bb.Read()
	testhelpers.AssertNoError(t, err)
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatal("task not found after submission")
	}
	if task.Status != "INTEGRATION_ANALYSIS_TO_REVIEW" {
		t.Errorf("expected status INTEGRATION_ANALYSIS_TO_REVIEW, got %s", task.Status)
	}

	// Verify ReviewCommit is set on the task
	if task.ReviewCommit == nil || *task.ReviewCommit == "" {
		t.Error("expected task.ReviewCommit to be set")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
