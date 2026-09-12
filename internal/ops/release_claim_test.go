package ops

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestReleaseClaim_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		taskID      string
		role        string
		errContains string
	}{
		{
			name: "empty task ID", role: "doer",
			errContains: "task ID is required",
		},
		{
			name: "invalid role", taskID: "t1", role: "invalid",
			errContains: "role must be reviewer, doer, or both",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReleaseClaim("/nonexistent", tt.taskID, tt.role, false, "", "human")
			testhelpers.RequireErrorContains(t, err, tt.errContains)
		})
	}
}

func TestReleaseClaim_CoderClaim(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	state.Tasks = []models.Task{task}
	// Register the assigned agent
	state.Agents["coder-1"] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: testhelpers.StringPtr("task-1"),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ReleaseClaim(tmpDir, "task-1", "doer", true, "manual cleanup", "human")
	if err != nil {
		t.Fatalf("ReleaseClaim() error: %v", err)
	}

	if !result.ReleasedDoer {
		t.Error("ReleasedDoer should be true")
	}
	if result.ReleasedReviewer {
		t.Error("ReleasedReviewer should be false")
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	readTask := readState.FindTask("task-1")
	if readTask == nil {
		t.Fatal("Task not found")
	}
	if readTask.Status != models.TaskStatusReady {
		t.Errorf("Status = %v, want READY", readTask.Status)
	}
	if readTask.AssignedTo != nil {
		t.Error("AssignedTo should be nil")
	}
	if readTask.LeaseExpires != nil {
		t.Error("LeaseExpires should be nil")
	}

	// Verify agent released
	agent := readState.Agents["coder-1"]
	if agent.Status != models.AgentStatusIdle {
		t.Errorf("Agent status = %v, want idle", agent.Status)
	}

	// Verify history entry
	lastHistory := readTask.History[len(readTask.History)-1]
	if lastHistory.Event != "doer_claim_released" {
		t.Errorf("History event = %q, want %q", lastHistory.Event, "doer_claim_released")
	}
}

func TestReleaseClaim_CoderClaim_ClearsWorktreeFields(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	// BuildTaskByStatus sets Worktree, BaseCommit, Iteration for IMPLEMENTING
	reviewCommit := "stale-review"
	approvedBy := "code-reviewer-1"
	mergeCommit := "stale-merge"
	task.Output = []models.OutputEntry{{
		Desc:     "stale output",
		DoneWhen: "done",
		Scope:    "scope",
		SpecRef:  "README.md",
	}}
	task.ReviewCommit = &reviewCommit
	task.ApprovedBy = &approvedBy
	task.Approvals = []models.Approval{{
		Agent:     approvedBy,
		Provider:  "codex",
		Timestamp: now,
	}}
	task.MergeCommit = &mergeCommit
	task.IntegrationFailure = map[string]any{"detail": "stale"}
	task.FailedBy = []string{"coder-1"}
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: testhelpers.StringPtr("task-1"),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Create a real worktree so cleanup has something to remove
	gitWrapper := git.New(tmpDir)
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}

	result, err := ReleaseClaim(tmpDir, "task-1", "doer", true, "manual cleanup", "human")
	if err != nil {
		t.Fatalf("ReleaseClaim() error: %v", err)
	}
	if !result.ReleasedDoer {
		t.Fatal("ReleasedDoer should be true")
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	readTask := readState.FindTask("task-1")
	if readTask == nil {
		t.Fatal("Task not found")
	}

	// Verify stale task fields are cleared
	if readTask.Worktree != nil {
		t.Errorf("Worktree should be nil, got %q", *readTask.Worktree)
	}
	if readTask.BaseCommit != nil {
		t.Errorf("BaseCommit should be nil, got %q", *readTask.BaseCommit)
	}
	if readTask.Iteration != 0 {
		t.Errorf("Iteration = %d, want 0", readTask.Iteration)
	}
	if len(readTask.Output) != 0 {
		t.Errorf("Output = %v, want cleared", readTask.Output)
	}
	if readTask.ReviewCommit != nil {
		t.Errorf("ReviewCommit = %v, want nil", *readTask.ReviewCommit)
	}
	if readTask.ApprovedBy != nil {
		t.Errorf("ApprovedBy = %v, want nil", *readTask.ApprovedBy)
	}
	if len(readTask.Approvals) != 0 {
		t.Errorf("Approvals = %v, want cleared", readTask.Approvals)
	}
	if readTask.MergeCommit != nil {
		t.Errorf("MergeCommit = %v, want nil", *readTask.MergeCommit)
	}
	if readTask.IntegrationFailure != nil {
		t.Errorf("IntegrationFailure = %v, want nil", readTask.IntegrationFailure)
	}
	if len(readTask.FailedBy) != 1 || readTask.FailedBy[0] != "coder-1" {
		t.Errorf("FailedBy = %v, want preserved", readTask.FailedBy)
	}

	// Worktree and branch persist after release — cleanup is deferred to
	// the next ClaimTask to avoid a race with concurrent claims.
	// See handleReadyClaimWorktree in claim_task.go.
	branchExists, err := gitWrapper.BranchExists("task/task-1")
	if err != nil {
		t.Fatalf("Failed to check branch existence: %v", err)
	}
	if !branchExists {
		t.Error("Branch task/task-1 should persist after release (deferred cleanup)")
	}

	wtDir := filepath.Join(tmpDir, ".worktrees", "task-1")
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		t.Error("Worktree directory should persist after release (deferred cleanup)")
	}
}

func TestReleaseRejectedClaim(t *testing.T) {
	fixture := newRejectedHandoffFixture(t, true)

	result, err := ReleaseClaim(fixture.projectRoot, fixture.taskID, "doer", true, "handoff", "human")
	if err != nil {
		t.Fatalf("ReleaseClaim() error: %v", err)
	}
	if !result.ReleasedDoer || result.ReleasedReviewer {
		t.Fatalf("ReleaseClaim() result = %+v, want doer-only release", result)
	}

	state := readClaimStateForTest(t, fixture.stateFile)
	task := state.FindTask(fixture.taskID)
	if task == nil {
		t.Fatal("released task not found")
	}
	if task.Status != models.TaskStatusRejected {
		t.Fatalf("Status = %s, want %s", task.Status, models.TaskStatusRejected)
	}
	if task.AssignedTo != nil || task.LeaseExpires != nil {
		t.Fatalf("doer ownership = assigned_to %v lease %v, want released", task.AssignedTo, task.LeaseExpires)
	}
	assertRejectedRecoveryMetadata(t, task, fixture)
	assertRejectedReviewerAffinity(t, state, task, fixture)
	if err := statevalidate.ValidateState(state, fixture.projectRoot, true, io.Discard); err != nil {
		t.Fatalf("released rejected state is invalid: %v", err)
	}
}

func TestRejectedTaskHandoff(t *testing.T) {
	fixture := newRejectedHandoffFixture(t, true)
	bb := db.For(fixture.stateFile)
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(fixture.taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: fixture.taskID}
		}
		doerID := "coder-1"
		task.Status = models.TaskStatusReviewing
		task.AssignedTo = &doerID
		task.RejectionReason = nil
		task.ReviewCommit = &fixture.branchSHA

		doer := state.Agents[doerID]
		doer.Status = models.AgentStatusWorking
		doer.CurrentTask = &fixture.taskID
		state.Agents[doerID] = doer

		reviewer := state.Agents[fixture.reviewerID]
		reviewer.Status = models.AgentStatusWorking
		reviewer.CurrentTask = &fixture.taskID
		state.Agents[fixture.reviewerID] = reviewer
		return nil
	}); err != nil {
		t.Fatalf("prepare reviewing handoff: %v", err)
	}

	reviewerAuthority := models.AgentAuthority{ID: fixture.reviewerID, Generation: testhelpers.TestAgentGeneration}
	if _, err := SubmitVerdictWithAuthority(
		fixture.projectRoot,
		fixture.taskID,
		"REJECTED",
		"preserve the rejected handoff",
		reviewerAuthority,
		"",
		fixture.branchSHA,
	); err != nil {
		t.Fatalf("SubmitVerdictWithAuthority(REJECTED) error: %v", err)
	}
	afterReject := readClaimStateForTest(t, fixture.stateFile)
	rejectedTask := afterReject.FindTask(fixture.taskID)
	if rejectedTask == nil || rejectedTask.Status != models.TaskStatusRejected {
		t.Fatalf("rejected task state = %#v, want CODE_REJECTED", rejectedTask)
	}
	assertRejectedRecoveryMetadata(t, rejectedTask, fixture)

	if err := acquireReviewOwnership(bb, fixture.reviewerID, fixture.taskID, &reviewerAuthority, time.Minute); err != nil {
		t.Fatalf("acquireReviewOwnership() error: %v", err)
	}
	afterReviewerWait := readClaimStateForTest(t, fixture.stateFile)
	rejectedTask = afterReviewerWait.FindTask(fixture.taskID)
	if rejectedTask == nil || rejectedTask.ReviewLeaseExpires == nil {
		t.Fatalf("rejected reviewer ownership missing: %#v", rejectedTask)
	}
	fixture.reviewLease = *rejectedTask.ReviewLeaseExpires
	assertRejectedRecoveryMetadata(t, rejectedTask, fixture)
	assertRejectedReviewerAffinity(t, afterReviewerWait, rejectedTask, fixture)

	if _, err := ReleaseClaimWithAuthority(
		fixture.projectRoot,
		fixture.taskID,
		"doer",
		true,
		"handoff",
		models.AgentAuthority{ID: "coder-1", Generation: testhelpers.TestAgentGeneration},
	); err != nil {
		t.Fatalf("ReleaseClaimWithAuthority() error: %v", err)
	}
	afterRelease := readClaimStateForTest(t, fixture.stateFile)
	releasedTask := afterRelease.FindTask(fixture.taskID)
	assertRejectedRecoveryMetadata(t, releasedTask, fixture)
	assertRejectedReviewerAffinity(t, afterRelease, releasedTask, fixture)
	if err := statevalidate.ValidateState(afterRelease, fixture.projectRoot, true, io.Discard); err != nil {
		t.Fatalf("released rejected state is invalid: %v", err)
	}

	if _, err := ClaimTaskWithAuthority(
		fixture.projectRoot,
		fixture.taskID,
		models.AgentAuthority{ID: "coder-2", Generation: testhelpers.TestAgentGeneration},
	); err != nil {
		t.Fatalf("ClaimTaskWithAuthority() error: %v", err)
	}
	assertRejectedClaimState(t, fixture, "coder-2", fixture.branchSHA)
}

func TestReleaseClaim_ReviewerClaim(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	state.Tasks = []models.Task{task}
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:        "code-reviewer",
		Status:      models.AgentStatusWorking,
		CurrentTask: testhelpers.StringPtr("task-1"),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ReleaseClaim(tmpDir, "task-1", "reviewer", true, "timeout", "human")
	if err != nil {
		t.Fatalf("ReleaseClaim() error: %v", err)
	}

	if !result.ReleasedReviewer {
		t.Error("ReleasedReviewer should be true")
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	readTask := readState.FindTask("task-1")
	if readTask == nil {
		t.Fatal("Task not found")
	}
	if readTask.Status != models.TaskStatusReadyForReview {
		t.Errorf("Status = %v, want %s", readTask.Status, models.TaskStatusReadyForReview)
	}
	if readTask.ReviewingBy != nil {
		t.Error("ReviewingBy should be nil")
	}
	if readTask.ReviewLeaseExpires != nil {
		t.Error("ReviewLeaseExpires should be nil")
	}

	lastHistory := readTask.History[len(readTask.History)-1]
	if lastHistory.Event != "review_claim_released" {
		t.Errorf("History event = %q, want %q", lastHistory.Event, "review_claim_released")
	}
}

func TestReleaseClaim_BothClaims(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: testhelpers.StringPtr("task-1"),
	}
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:        "code-reviewer",
		Status:      models.AgentStatusWorking,
		CurrentTask: testhelpers.StringPtr("task-1"),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ReleaseClaim(tmpDir, "task-1", "both", true, "full reset", "human")
	if err != nil {
		t.Fatalf("ReleaseClaim() error: %v", err)
	}

	if !result.ReleasedDoer {
		t.Error("ReleasedDoer should be true")
	}
	if !result.ReleasedReviewer {
		t.Error("ReleasedReviewer should be true")
	}
}

func TestReleaseClaim_NoClaims(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ReleaseClaim(tmpDir, "task-1", "doer", true, "reason", "human")
	testhelpers.RequireErrorContains(t, err, "no claims to release")
}

func TestReleaseClaim_TaskNotFound(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ReleaseClaim(tmpDir, "nonexistent", "doer", false, "", "human")
	if err == nil {
		t.Fatal("Expected error for nonexistent task")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestReleaseClaim_ActiveLease_NoForce(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	// Ensure lease is in the future
	futureLease := now.Add(30 * time.Minute)
	task.LeaseExpires = &futureLease
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ReleaseClaim(tmpDir, "task-1", "doer", false, "", "human")
	testhelpers.RequireErrorContains(t, err, "lease still valid")
}

func TestReleaseClaim_DefaultAgentAndReason(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: testhelpers.StringPtr("task-1"),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Empty agentID and reason should get defaults
	_, err := ReleaseClaim(tmpDir, "task-1", "doer", true, "", "")
	if err != nil {
		t.Fatalf("ReleaseClaim() error: %v", err)
	}
}

func TestReleaseClaim_PipelineCoderClaim(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupPipelineTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", "IMPLEMENTING_CODE", now)
	task.RolePair = "coding-pair"
	agent := "coder-1"
	task.AssignedTo = &agent
	leaseExpires := now.Add(30 * time.Minute)
	task.LeaseExpires = &leaseExpires
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: testhelpers.StringPtr("task-1"),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ReleaseClaim(tmpDir, "task-1", "doer", true, "pipeline test", "human")
	if err != nil {
		t.Fatalf("ReleaseClaim() error: %v", err)
	}
	if !result.ReleasedDoer {
		t.Error("ReleasedDoer should be true")
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	readTask := readState.FindTask("task-1")
	if readTask == nil {
		t.Fatal("Task not found")
	}
	// Pipeline coder release: IMPLEMENTING_CODE → DRAFT_CODE
	if readTask.Status != "DRAFT_CODE" {
		t.Errorf("Status = %v, want DRAFT_CODE", readTask.Status)
	}
	if readTask.AssignedTo != nil {
		t.Error("AssignedTo should be nil")
	}
}

func TestReleaseClaim_PipelineReviewerClaim(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupPipelineTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", "REVIEWING_CODE", now)
	task.RolePair = "coding-pair"
	reviewer := "code-reviewer-1"
	task.ReviewingBy = &reviewer
	reviewLease := now.Add(30 * time.Minute)
	task.ReviewLeaseExpires = &reviewLease
	state.Tasks = []models.Task{task}
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:        "code-reviewer",
		Status:      models.AgentStatusWorking,
		CurrentTask: testhelpers.StringPtr("task-1"),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ReleaseClaim(tmpDir, "task-1", "reviewer", true, "pipeline test", "human")
	if err != nil {
		t.Fatalf("ReleaseClaim() error: %v", err)
	}
	if !result.ReleasedReviewer {
		t.Error("ReleasedReviewer should be true")
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	readTask := readState.FindTask("task-1")
	if readTask == nil {
		t.Fatal("Task not found")
	}
	// Pipeline reviewer release: REVIEWING_CODE -> CODE_TO_REVIEW
	if readTask.Status != models.TaskStatusReadyForReview {
		t.Errorf("Status = %v, want %s", readTask.Status, models.TaskStatusReadyForReview)
	}
	if readTask.ReviewingBy != nil {
		t.Error("ReviewingBy should be nil")
	}
}

func TestReleaseClaim_PipelineReviewerClaimReviewing2(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupPipelineTest(t)
	writeValidationSpecRefs(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	reviewer := "code-reviewer-2"
	reviewCommit := "review123"
	reviewLease := now.Add(30 * time.Minute)
	agentLease := now.Add(30 * time.Minute)
	worktree := ".worktrees/task-1"
	task := testhelpers.BuildTaskByStatus("task-1", "REVIEWING_CODE_2", now)
	task.RolePair = "coding-pair"
	task.ReviewCommit = &reviewCommit
	task.ReviewingBy = &reviewer
	task.ReviewLeaseExpires = &reviewLease
	task.Worktree = &worktree
	task.HandoffEvents = []models.HandoffEvent{
		{Timestamp: now.Add(-2 * time.Minute), Agent: "coder-1", Trigger: models.HandoffTriggerSubmission},
	}
	task.Approvals = []models.Approval{
		{Agent: "code-reviewer-1", Provider: "codex", Timestamp: now.Add(-time.Minute)},
	}
	state.Tasks = []models.Task{task}
	state.Agents[reviewer] = models.Agent{
		Role:         "code-reviewer",
		Provider:     "codex",
		Status:       models.AgentStatusReviewing,
		CurrentTask:  testhelpers.StringPtr("task-1"),
		LeaseExpires: &agentLease,
		Heartbeat:    now,
		PID:          12345,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ReleaseClaim(tmpDir, "task-1", "reviewer", true, "pipeline test", "human")
	if err != nil {
		t.Fatalf("ReleaseClaim() error: %v", err)
	}
	if !result.ReleasedReviewer {
		t.Error("ReleasedReviewer should be true")
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	readTask := readState.FindTask("task-1")
	if readTask == nil {
		t.Fatal("Task not found")
	}
	if readTask.Status != "CODE_PARTIALLY_APPROVED" {
		t.Errorf("Status = %v, want CODE_PARTIALLY_APPROVED", readTask.Status)
	}
	if readTask.ReviewingBy != nil {
		t.Error("ReviewingBy should be nil")
	}
	if readTask.ReviewLeaseExpires != nil {
		t.Error("ReviewLeaseExpires should be nil")
	}
	if err := statevalidate.ValidateState(readState, tmpDir, false, io.Discard); err != nil {
		t.Fatalf("state should validate after release: %v", err)
	}
}

func TestReleaseClaim_PipelineReviewerClaim_LegacySubmittedStatus(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupPipelineTest(t)
	pipelinePath := filepath.Join(tmpDir, paths.ProjectDirName(), "pipeline.yaml")
	data, err := os.ReadFile(pipelinePath)
	if err != nil {
		t.Fatalf("read pipeline config: %v", err)
	}
	data = []byte(strings.ReplaceAll(string(data), "CODE_TO_REVIEW", "CODE_READY_FOR_REVIEW"))
	if err := os.WriteFile(pipelinePath, data, 0644); err != nil {
		t.Fatalf("write legacy pipeline config: %v", err)
	}

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", "REVIEWING_CODE", now)
	task.RolePair = "coding-pair"
	reviewer := "code-reviewer-1"
	task.ReviewingBy = &reviewer
	reviewLease := now.Add(30 * time.Minute)
	task.ReviewLeaseExpires = &reviewLease
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err = ReleaseClaim(tmpDir, "task-1", "reviewer", true, "pipeline test", "human")
	if err != nil {
		t.Fatalf("ReleaseClaim() error: %v", err)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	readTask := readState.FindTask("task-1")
	if readTask == nil {
		t.Fatal("Task not found")
	}
	if readTask.Status != models.TaskStatusLegacyReadyForReview {
		t.Errorf("Status = %v, want %s", readTask.Status, models.TaskStatusLegacyReadyForReview)
	}
}

func writeValidationSpecRefs(t *testing.T, tmpDir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(tmpDir, "specs"), 0755); err != nil {
		t.Fatalf("create specs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "specs", "vision.md"), []byte("test goal\n"), 0644); err != nil {
		t.Fatalf("write goal spec: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("test task spec\n"), 0644); err != nil {
		t.Fatalf("write task spec: %v", err)
	}
}

type partialResolutionFailureResolver struct{}

func (partialResolutionFailureResolver) RolePairNames() []string { return []string{"coding-pair"} }
func (partialResolutionFailureResolver) RoleType(role string) (string, error) {
	switch role {
	case "coder":
		return "doer", nil
	case "code-reviewer":
		return "reviewer", nil
	default:
		return "", assertAnError{}
	}
}
func (partialResolutionFailureResolver) AllRoleNames() []string { return nil }
func (partialResolutionFailureResolver) DoerRole(string) (string, error) {
	return "coder", nil
}
func (partialResolutionFailureResolver) ReviewerRole(string) (string, error) {
	return "code-reviewer", nil
}
func (partialResolutionFailureResolver) InitialStatus(string) (models.TaskStatus, error) {
	return "DRAFT_CODE", nil
}
func (partialResolutionFailureResolver) RejectedStatus(string) (models.TaskStatus, error) {
	return "CODE_REJECTED", nil
}
func (partialResolutionFailureResolver) SubmittedStatus(string) (models.TaskStatus, error) {
	return "CODE_TO_REVIEW", nil
}
func (partialResolutionFailureResolver) ReviewingStatus(string) (models.TaskStatus, error) {
	return "REVIEWING_CODE", nil
}
func (partialResolutionFailureResolver) ExecutingStatus(string) (models.TaskStatus, error) {
	return "IMPLEMENTING_CODE", nil
}
func (partialResolutionFailureResolver) ApprovedStatus(string) (models.TaskStatus, error) {
	return "CODE_APPROVED", nil
}
func (partialResolutionFailureResolver) PartiallyApprovedStatus(string) (models.TaskStatus, error) {
	return "", assertAnError{}
}
func (partialResolutionFailureResolver) Reviewing2Status(string) (models.TaskStatus, error) {
	return "REVIEWING_CODE_2", nil
}

type assertAnError struct{}

func (assertAnError) Error() string { return "partial status unavailable" }

func TestResolveReviewerClaimReleaseStatus_Reviewing2MissingPartialFailsClosed(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	reviewer := "code-reviewer-2"
	reviewLease := now.Add(30 * time.Minute)
	task := models.Task{
		ID:                 "task-1",
		Status:             "REVIEWING_CODE_2",
		RolePair:           "coding-pair",
		ReviewingBy:        &reviewer,
		ReviewLeaseExpires: &reviewLease,
	}

	_, _, err := ResolveReviewerReleaseStatus(&task, partialResolutionFailureResolver{})
	if err == nil {
		t.Fatal("expected error when reviewing-2 release target cannot be resolved")
	}
	if task.ReviewingBy == nil || *task.ReviewingBy != reviewer {
		t.Fatalf("ReviewingBy = %v, want preserved %s", task.ReviewingBy, reviewer)
	}
	if task.ReviewLeaseExpires == nil || !task.ReviewLeaseExpires.Equal(reviewLease) {
		t.Fatalf("ReviewLeaseExpires = %v, want preserved %v", task.ReviewLeaseExpires, reviewLease)
	}
}
