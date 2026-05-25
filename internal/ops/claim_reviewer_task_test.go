package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestClaimReviewerTask_Validation(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupLizaDir(t, tmpDir)

	tests := []struct {
		name        string
		input       ClaimReviewerTaskInput
		errContains string
	}{
		{
			name:        "empty agent ID",
			input:       ClaimReviewerTaskInput{ProjectRoot: tmpDir, LeaseDuration: 1800},
			errContains: "agent ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ClaimReviewerTask(tt.input)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("Error = %q, want to contain %q", err.Error(), tt.errContains)
			}
		})
	}
}

func TestClaimReviewerTask_DefaultLeaseDuration(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimReviewerTaskTestAgents(state)
	reviewCommit := "abc123"
	state.Tasks = []models.Task{
		{
			ID:           "task-1",
			Status:       models.TaskStatusReadyForReview,
			RolePair:     "coding-pair",
			ReviewCommit: &reviewCommit,
			Created:      now,
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// When LeaseDuration is 0, should use default (1800 seconds)
	start := time.Now()
	result, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-1",
		LeaseDuration: 0, // Should use default
	})
	if err != nil {
		t.Fatalf("ClaimReviewerTask() error: %v", err)
	}

	// Verify lease was set using default duration (1800s = 30m)
	expectedLeaseMin := start.Add(1700 * time.Second) // Allow some tolerance
	expectedLeaseMax := start.Add(1900 * time.Second)
	if result.LeaseExpires.Before(expectedLeaseMin) || result.LeaseExpires.After(expectedLeaseMax) {
		t.Errorf("LeaseExpires = %v, expected between %v and %v", result.LeaseExpires, expectedLeaseMin, expectedLeaseMax)
	}
}

func TestClaimReviewerTask_MissingRegisteredAgentDoesNotCreateGhost(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	reviewCommit := "abc123"
	state.Tasks = []models.Task{
		{
			ID:           "task-1",
			Status:       models.TaskStatusReadyForReview,
			RolePair:     "coding-pair",
			Priority:     1,
			ReviewCommit: &reviewCommit,
			History:      []models.TaskHistoryEntry{},
			Created:      now,
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-1",
		LeaseDuration: 1800,
	})
	if err == nil {
		t.Fatal("Expected error for missing registered reviewer")
	}
	if !strings.Contains(err.Error(), "agent code-reviewer-1 is not registered") {
		t.Errorf("Error = %q, want missing registered agent", err.Error())
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if _, exists := readState.Agents["code-reviewer-1"]; exists {
		t.Fatal("ClaimReviewerTask created a ghost agent row")
	}
	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.ReviewingBy != nil {
		t.Fatal("Task should not be review-assigned after rejected claim")
	}
}

func TestClaimReviewerTask_CorruptRegisteredAgentRejected(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*models.Agent)
		errContains string
	}{
		{
			name:        "empty role",
			mutate:      func(agent *models.Agent) { agent.Role = "" },
			errContains: "has no registered role",
		},
		{
			name:        "empty provider",
			mutate:      func(agent *models.Agent) { agent.Provider = "" },
			errContains: "has no registered provider",
		},
		{
			name:        "zero pid",
			mutate:      func(agent *models.Agent) { agent.PID = 0 },
			errContains: "has no registered process PID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testhelpers.SetupTestGitRepo(t, tmpDir)
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

			now := time.Now().UTC()
			state := testhelpers.CreateValidState()
			agent := testhelpers.RegisteredTestAgent("code-reviewer")
			tt.mutate(&agent)
			state.Agents["code-reviewer-1"] = agent
			reviewCommit := "abc123"
			state.Tasks = []models.Task{
				{
					ID:           "task-1",
					Status:       models.TaskStatusReadyForReview,
					RolePair:     "coding-pair",
					Priority:     1,
					ReviewCommit: &reviewCommit,
					History:      []models.TaskHistoryEntry{},
					Created:      now,
				},
			}
			testhelpers.WriteInitialState(t, stateFile, state)

			_, err := ClaimReviewerTask(ClaimReviewerTaskInput{
				ProjectRoot:   tmpDir,
				AgentID:       "code-reviewer-1",
				LeaseDuration: 1800,
			})
			if err == nil {
				t.Fatal("Expected error for corrupt registered reviewer")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("Error = %q, want to contain %q", err.Error(), tt.errContains)
			}

			bb := db.New(stateFile)
			readState, err := bb.Read()
			if err != nil {
				t.Fatalf("Failed to read state: %v", err)
			}
			task := readState.FindTask("task-1")
			if task == nil {
				t.Fatal("Task not found")
			}
			if task.ReviewingBy != nil {
				t.Fatal("Task should not be review-assigned after rejected claim")
			}
		})
	}
}

func TestClaimReviewerTask_NoReviewableTasks(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimReviewerTaskTestAgents(state)
	// Only create tasks in non-reviewable states
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-ready", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("task-implementing", models.TaskStatusImplementing, now),
		testhelpers.BuildTaskByStatus("task-merged", models.TaskStatusMerged, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-1",
		LeaseDuration: 1800,
	})
	if err == nil {
		t.Fatal("Expected error when no reviewable tasks, got nil")
	}
	if !strings.Contains(err.Error(), "no reviewable tasks found") {
		t.Errorf("Error = %q, want to contain 'no reviewable tasks found'", err.Error())
	}
}

func TestClaimReviewerTask_Success(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimReviewerTaskTestAgents(state)
	worktree, reviewCommit := createClaimReviewWorktree(t, tmpDir, "task-1")
	state.Tasks = []models.Task{
		{
			ID:           "task-1",
			Status:       models.TaskStatusReadyForReview,
			RolePair:     "coding-pair",
			Priority:     1,
			Worktree:     &worktree,
			ReviewCommit: &reviewCommit,
			History:      []models.TaskHistoryEntry{},
			Created:      now,
		},
	}
	state.Agents["code-reviewer-1"] = testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-1",
		LeaseDuration: 1800,
	})
	if err != nil {
		t.Fatalf("ClaimReviewerTask() error: %v", err)
	}

	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}
	if result.Worktree != worktree {
		t.Errorf("Worktree = %q, want %q", result.Worktree, worktree)
	}
	if result.ReviewCommit != reviewCommit {
		t.Errorf("ReviewCommit = %q, want %q", result.ReviewCommit, reviewCommit)
	}
	if result.LeaseExpires.IsZero() {
		t.Error("LeaseExpires should not be zero")
	}

	// Verify state was updated
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusReviewing {
		t.Errorf("Task status = %v, want REVIEWING_CODE", task.Status)
	}
	if task.ReviewingBy == nil || *task.ReviewingBy != "code-reviewer-1" {
		t.Error("Task ReviewingBy should be code-reviewer-1")
	}
	if task.ReviewLeaseExpires == nil {
		t.Error("Task ReviewLeaseExpires should be set")
	}

	agent, exists := readState.Agents["code-reviewer-1"]
	if !exists {
		t.Fatal("Agent not found")
	}
	if agent.Status != models.AgentStatusReviewing {
		t.Errorf("Agent status = %v, want REVIEWING_CODE", agent.Status)
	}
	if agent.CurrentTask == nil || *agent.CurrentTask != "task-1" {
		t.Error("Agent CurrentTask should be task-1")
	}
}

func TestClaimReviewerTask_RejectsReviewCommitMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	worktree, headCommit := createClaimReviewWorktree(t, tmpDir, "task-1")
	staleCommit := testhelpers.MustGit(t, tmpDir, "rev-parse", "integration")
	if staleCommit == headCommit {
		t.Fatal("test setup failed: stale commit matches worktree HEAD")
	}

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimReviewerTaskTestAgents(state)
	coderID := "coder-1"
	otherTaskID := "other-task"
	state.Agents[coderID] = models.Agent{
		Role:        models.RoleCoder,
		Status:      models.AgentStatusWorking,
		CurrentTask: &otherTaskID,
	}
	state.Tasks = []models.Task{
		{
			ID:           "task-1",
			Status:       models.TaskStatusReadyForReview,
			RolePair:     "coding-pair",
			Priority:     1,
			AssignedTo:   &coderID,
			Worktree:     &worktree,
			ReviewCommit: &staleCommit,
			Created:      now,
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-1",
		LeaseDuration: 1800,
	})
	if err == nil {
		t.Fatal("Expected error for review_commit/worktree HEAD mismatch")
	}
	if !strings.Contains(err.Error(), IntegrationReasonReviewBoundaryMismatch) {
		t.Errorf("Error = %q, want review boundary mismatch", err.Error())
	}

	readState, readErr := db.New(stateFile).Read()
	if readErr != nil {
		t.Fatalf("Failed to read state: %v", readErr)
	}
	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusIntegrationFailed {
		t.Errorf("Task status = %v, want INTEGRATION_FAILED", task.Status)
	}
	if task.ReviewingBy != nil {
		t.Fatal("Task should not be review-assigned after boundary mismatch")
	}
	if task.AssignedTo != nil {
		t.Fatal("Task AssignedTo should be cleared after boundary mismatch")
	}
	if task.IntegrationFailure == nil {
		t.Fatal("IntegrationFailure should be recorded after boundary mismatch")
	}
	if task.IntegrationFailure["reason"] != IntegrationReasonReviewBoundaryMismatch {
		t.Errorf("IntegrationFailure reason = %v, want %q", task.IntegrationFailure["reason"], IntegrationReasonReviewBoundaryMismatch)
	}
	if task.IntegrationFailure["operation"] != reviewBoundaryOperationAssignment {
		t.Errorf("IntegrationFailure operation = %v, want %q", task.IntegrationFailure["operation"], reviewBoundaryOperationAssignment)
	}
	coder := readState.Agents[coderID]
	if coder.Status != models.AgentStatusWorking {
		t.Errorf("coder status = %q, want WORKING", coder.Status)
	}
	if coder.CurrentTask == nil || *coder.CurrentTask != otherTaskID {
		t.Fatalf("coder CurrentTask = %v, want %s", coder.CurrentTask, otherTaskID)
	}
}

func TestClaimReviewerTask_ReleasesSameTaskAssignedAgentOnBoundaryMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	worktree, headCommit := createClaimReviewWorktree(t, tmpDir, "task-1")
	staleCommit := testhelpers.MustGit(t, tmpDir, "rev-parse", "integration")
	if staleCommit == headCommit {
		t.Fatal("test setup failed: stale commit matches worktree HEAD")
	}

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimReviewerTaskTestAgents(state)
	coderID := "coder-1"
	taskID := "task-1"
	state.Agents[coderID] = models.Agent{
		Role:        models.RoleCoder,
		Status:      models.AgentStatusWorking,
		CurrentTask: &taskID,
	}
	state.Tasks = []models.Task{
		{
			ID:           taskID,
			Status:       models.TaskStatusReadyForReview,
			RolePair:     "coding-pair",
			Priority:     1,
			AssignedTo:   &coderID,
			Worktree:     &worktree,
			ReviewCommit: &staleCommit,
			Created:      now,
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-1",
		LeaseDuration: 1800,
	})
	if err == nil {
		t.Fatal("Expected error for review_commit/worktree HEAD mismatch")
	}

	readState, readErr := db.New(stateFile).Read()
	if readErr != nil {
		t.Fatalf("Failed to read state: %v", readErr)
	}
	task := readState.FindTask(taskID)
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusIntegrationFailed {
		t.Errorf("Task status = %v, want INTEGRATION_FAILED", task.Status)
	}
	if task.AssignedTo != nil {
		t.Fatal("Task AssignedTo should be cleared after boundary mismatch")
	}
	coder := readState.Agents[coderID]
	if coder.Status != models.AgentStatusIdle {
		t.Errorf("coder status = %q, want IDLE", coder.Status)
	}
	if coder.CurrentTask != nil {
		t.Fatalf("coder CurrentTask = %v, want nil", *coder.CurrentTask)
	}
}

func TestClaimReviewerTask_SkipsStaleReviewBoundaryCandidate(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	staleWorktree, staleHead := createClaimReviewWorktree(t, tmpDir, "task-stale")
	validWorktree, validHead := createClaimReviewWorktree(t, tmpDir, "task-valid")
	staleCommit := testhelpers.MustGit(t, tmpDir, "rev-parse", "integration")
	if staleCommit == staleHead {
		t.Fatal("test setup failed: stale commit matches stale worktree HEAD")
	}

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimReviewerTaskTestAgents(state)
	state.Tasks = []models.Task{
		{
			ID:           "task-stale",
			Status:       models.TaskStatusReadyForReview,
			RolePair:     "coding-pair",
			Priority:     1,
			Worktree:     &staleWorktree,
			ReviewCommit: &staleCommit,
			Created:      now.Add(-time.Minute),
		},
		{
			ID:           "task-valid",
			Status:       models.TaskStatusReadyForReview,
			RolePair:     "coding-pair",
			Priority:     2,
			Worktree:     &validWorktree,
			ReviewCommit: &validHead,
			Created:      now,
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-1",
		LeaseDuration: 1800,
	})
	if err != nil {
		t.Fatalf("ClaimReviewerTask() error = %v", err)
	}
	if result.TaskID != "task-valid" {
		t.Fatalf("claimed task = %q, want task-valid", result.TaskID)
	}

	readState, readErr := db.New(stateFile).Read()
	if readErr != nil {
		t.Fatalf("Failed to read state: %v", readErr)
	}
	staleTask := readState.FindTask("task-stale")
	if staleTask == nil {
		t.Fatal("stale task not found")
	}
	if staleTask.Status != models.TaskStatusIntegrationFailed {
		t.Errorf("stale task status = %v, want INTEGRATION_FAILED", staleTask.Status)
	}
	validTask := readState.FindTask("task-valid")
	if validTask == nil {
		t.Fatal("valid task not found")
	}
	if validTask.Status != models.TaskStatusReviewing {
		t.Errorf("valid task status = %v, want REVIEWING_CODE", validTask.Status)
	}
	if validTask.ReviewingBy == nil || *validTask.ReviewingBy != "code-reviewer-1" {
		t.Fatalf("valid task ReviewingBy = %v, want code-reviewer-1", validTask.ReviewingBy)
	}
}

func TestClaimReviewerTask_PrioritySelection(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimReviewerTaskTestAgents(state)
	reviewCommit1 := "abc123"
	reviewCommit2 := "def456"
	state.Tasks = []models.Task{
		{
			ID:           "task-low",
			Status:       models.TaskStatusReadyForReview,
			RolePair:     "coding-pair",
			Priority:     3,
			ReviewCommit: &reviewCommit1,
			Created:      now.Add(-1 * time.Minute),
		},
		{
			ID:           "task-high",
			Status:       models.TaskStatusReadyForReview,
			RolePair:     "coding-pair",
			Priority:     1,
			ReviewCommit: &reviewCommit2,
			Created:      now,
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-1",
		LeaseDuration: 1800,
	})
	if err != nil {
		t.Fatalf("ClaimReviewerTask() error: %v", err)
	}

	// Should claim the high-priority task (lower number = higher priority)
	if result.TaskID != "task-high" {
		t.Errorf("TaskID = %q, want %q (higher priority)", result.TaskID, "task-high")
	}
}

func TestClaimReviewerTask_TieBreaking(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimReviewerTaskTestAgents(state)
	reviewCommit1 := "abc123"
	reviewCommit2 := "def456"
	state.Tasks = []models.Task{
		{
			ID:           "task-new",
			Status:       models.TaskStatusReadyForReview,
			RolePair:     "coding-pair",
			Priority:     2,
			ReviewCommit: &reviewCommit2,
			Created:      now,
		},
		{
			ID:           "task-old",
			Status:       models.TaskStatusReadyForReview,
			RolePair:     "coding-pair",
			Priority:     2,
			ReviewCommit: &reviewCommit1,
			Created:      now.Add(-1 * time.Minute),
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-1",
		LeaseDuration: 1800,
	})
	if err != nil {
		t.Fatalf("ClaimReviewerTask() error: %v", err)
	}

	// With randomized selection, either task is valid (same priority tier)
	validIDs := map[string]bool{"task-old": true, "task-new": true}
	if !validIDs[result.TaskID] {
		t.Errorf("TaskID = %q, want one of %v", result.TaskID, validIDs)
	}
}

func TestClaimReviewerTask_MissingReviewCommit(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimReviewerTaskTestAgents(state)
	// Task is READY_FOR_REVIEW but missing ReviewCommit (corrupted state)
	state.Tasks = []models.Task{
		{
			ID:       "task-1",
			Status:   models.TaskStatusReadyForReview,
			RolePair: "coding-pair",
			Priority: 1,
			// ReviewCommit intentionally nil
			History: []models.TaskHistoryEntry{},
			Created: now,
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-1",
		LeaseDuration: 1800,
	})
	if err == nil {
		t.Fatal("Expected error for missing review_commit, got nil")
	}
	// Task is filtered out by IsClaimable (review_commit nil → not claimable),
	// so the error is "no reviewable tasks found" rather than "no review_commit".
	if !strings.Contains(err.Error(), "no reviewable tasks found") {
		t.Errorf("Error = %q, want to contain 'no reviewable tasks found'", err.Error())
	}
}

func TestClaimReviewerTask_CodePlanReviewerExplicitRole(t *testing.T) {
	// Verifies that a code-plan-reviewer agent with an explicit Role field
	// correctly claims code-planning-pair tasks.
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimReviewerTaskTestAgents(state)
	reviewCommit := "abc123"
	state.Tasks = []models.Task{
		{
			ID:           "plan-task-1",
			Status:       models.TaskStatusCodingPlanToReview,
			RolePair:     "code-planning-pair",
			Priority:     1,
			ReviewCommit: &reviewCommit,
			History:      []models.TaskHistoryEntry{},
			Created:      now,
		},
	}
	state.Agents["code-plan-reviewer-1"] = testhelpers.RegisteredTestAgent(models.RoleCodePlanReviewer)
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-plan-reviewer-1",
		Role:          models.RoleCodePlanReviewer,
		LeaseDuration: 1800,
	})
	if err != nil {
		t.Fatalf("ClaimReviewerTask() error: %v", err)
	}

	if result.TaskID != "plan-task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "plan-task-1")
	}

	// Verify the task transitioned to reviewing state
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	task := readState.FindTask("plan-task-1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusReviewingCodingPlan {
		t.Errorf("Task status = %v, want REVIEWING_CODING_PLAN", task.Status)
	}
	if task.ReviewingBy == nil || *task.ReviewingBy != "code-plan-reviewer-1" {
		t.Error("Task ReviewingBy should be code-plan-reviewer-1")
	}
}

func TestClaimReviewerTask_SkipsAlreadyReviewing(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimReviewerTaskTestAgents(state)
	reviewer := "code-reviewer-99"
	leaseExpires := now.Add(1 * time.Hour)
	reviewCommit1 := "abc123"
	reviewCommit2 := "def456"
	state.Tasks = []models.Task{
		{
			ID:                 "task-reviewing",
			Status:             models.TaskStatusReviewing,
			RolePair:           "coding-pair",
			Priority:           1, // High priority but already claimed
			ReviewCommit:       &reviewCommit1,
			ReviewingBy:        &reviewer,
			ReviewLeaseExpires: &leaseExpires,
			Created:            now,
		},
		{
			ID:           "task-available",
			Status:       models.TaskStatusReadyForReview,
			RolePair:     "coding-pair",
			Priority:     3, // Lower priority but available
			ReviewCommit: &reviewCommit2,
			Created:      now,
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-1",
		LeaseDuration: 1800,
	})
	if err != nil {
		t.Fatalf("ClaimReviewerTask() error: %v", err)
	}

	// Should skip the REVIEWING task and claim the available one
	if result.TaskID != "task-available" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-available")
	}
}

func TestClaimReviewerTask_PartiallyApproved(t *testing.T) {
	// Verifies that a partially_approved task can be claimed and transitions to reviewing_2.
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimReviewerTaskTestAgents(state)
	reviewCommit := "abc123"
	state.Tasks = []models.Task{
		{
			ID:           "task-pa",
			Status:       models.TaskStatusPartiallyApproved,
			RolePair:     "coding-pair",
			Priority:     1,
			ReviewCommit: &reviewCommit,
			History:      []models.TaskHistoryEntry{},
			Created:      now,
			Approvals: []models.Approval{
				{Agent: "code-reviewer-2", Provider: "anthropic", Timestamp: now},
			},
		},
	}
	state.Agents["code-reviewer-1"] = testhelpers.RegisteredTestAgent("code-reviewer")
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-1",
		LeaseDuration: 1800,
	})
	if err != nil {
		t.Fatalf("ClaimReviewerTask() error: %v", err)
	}

	if result.TaskID != "task-pa" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-pa")
	}

	// Verify state was updated to REVIEWING_CODE_2
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-pa")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusReviewingCode2 {
		t.Errorf("Task status = %v, want REVIEWING_CODE_2", task.Status)
	}
	if task.ReviewingBy == nil || *task.ReviewingBy != "code-reviewer-1" {
		t.Error("Task ReviewingBy should be code-reviewer-1")
	}
}

func TestClaimReviewerTask_SkipsTaskAlreadyApprovedByClaimer(t *testing.T) {
	// Verifies the independent-review filter: a reviewer that already approved
	// a task in round 1 must not be allowed to claim the same partially_approved
	// task for round 2. Without this filter, the agent loop polling for
	// partially_approved tasks would happily self-rubber-stamp the quorum.
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimReviewerTaskTestAgents(state)
	reviewCommit := "abc123"
	state.Tasks = []models.Task{
		{
			ID:           "task-pa",
			Status:       models.TaskStatusPartiallyApproved,
			RolePair:     "coding-pair",
			Priority:     1,
			ReviewCommit: &reviewCommit,
			History:      []models.TaskHistoryEntry{},
			Created:      now,
			// Reviewer-1 already approved — must NOT be eligible for round 2.
			Approvals: []models.Approval{
				{Agent: "code-reviewer-1", Provider: "anthropic", Timestamp: now},
			},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-1",
		LeaseDuration: 1800,
	})
	if err == nil {
		t.Fatal("expected error when prior approver tries to re-claim, got nil")
	}
	if !strings.Contains(err.Error(), "already approved by claimer") {
		t.Errorf("error = %q, want substring %q", err.Error(), "already approved by claimer")
	}

	// Verify task state is unchanged — still partially_approved, no new
	// ReviewingBy assignment.
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	task := readState.FindTask("task-pa")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusPartiallyApproved {
		t.Errorf("status = %v, want unchanged PARTIALLY_APPROVED", task.Status)
	}
	if task.ReviewingBy != nil {
		t.Errorf("ReviewingBy = %v, want nil (no claim should have occurred)", *task.ReviewingBy)
	}
}

func TestClaimReviewerTask_Round2GoesToDifferentReviewer(t *testing.T) {
	// End-to-end claim-filter check: when reviewer-1 has approved and
	// reviewer-2 polls, reviewer-2 picks up the partially_approved task
	// for round 2 even though reviewer-1's loop would have rejected it.
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimReviewerTaskTestAgents(state)
	reviewCommit := "abc123"
	state.Tasks = []models.Task{
		{
			ID:           "task-pa",
			Status:       models.TaskStatusPartiallyApproved,
			RolePair:     "coding-pair",
			Priority:     1,
			ReviewCommit: &reviewCommit,
			History:      []models.TaskHistoryEntry{},
			Created:      now,
			Approvals: []models.Approval{
				{Agent: "code-reviewer-1", Provider: "anthropic", Timestamp: now},
			},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// reviewer-2 (a different reviewer) must succeed in claiming.
	result, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-2",
		LeaseDuration: 1800,
	})
	if err != nil {
		t.Fatalf("ClaimReviewerTask() error: %v", err)
	}
	if result.TaskID != "task-pa" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-pa")
	}

	// Status should now be REVIEWING_CODE_2 with reviewer-2 holding the lease.
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	task := readState.FindTask("task-pa")
	if task.Status != models.TaskStatusReviewingCode2 {
		t.Errorf("status = %v, want REVIEWING_CODE_2", task.Status)
	}
	if task.ReviewingBy == nil || *task.ReviewingBy != "code-reviewer-2" {
		t.Errorf("ReviewingBy = %v, want code-reviewer-2", task.ReviewingBy)
	}
}

func TestClaimReviewerTask_ClaimPriority_PartiallyApprovedOverSubmitted(t *testing.T) {
	// Verifies that partially_approved tasks are selected before submitted tasks
	// at the same priority level.
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimReviewerTaskTestAgents(state)
	rc1 := "abc123"
	rc2 := "def456"
	state.Tasks = []models.Task{
		{
			ID:           "task-submitted",
			Status:       models.TaskStatusReadyForReview,
			RolePair:     "coding-pair",
			Priority:     1, // Same priority
			ReviewCommit: &rc1,
			History:      []models.TaskHistoryEntry{},
			Created:      now.Add(-1 * time.Minute),
		},
		{
			ID:           "task-pa",
			Status:       models.TaskStatusPartiallyApproved,
			RolePair:     "coding-pair",
			Priority:     1, // Same priority
			ReviewCommit: &rc2,
			History:      []models.TaskHistoryEntry{},
			Created:      now,
			Approvals: []models.Approval{
				{Agent: "code-reviewer-2", Provider: "anthropic", Timestamp: now},
			},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-1",
		LeaseDuration: 1800,
	})
	if err != nil {
		t.Fatalf("ClaimReviewerTask() error: %v", err)
	}

	// Partially_approved should be claimed first
	if result.TaskID != "task-pa" {
		t.Errorf("TaskID = %q, want %q (partially_approved preferred)", result.TaskID, "task-pa")
	}
}

func TestClaimReviewerTask_DiversityWithApprovals(t *testing.T) {
	// Verifies that for partially_approved tasks, the one whose existing
	// approval provider differs from the claimer's provider is preferred.
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimReviewerTaskTestAgents(state)
	rc1 := "abc123"
	rc2 := "def456"
	state.Tasks = []models.Task{
		{
			ID:           "task-same",
			Status:       models.TaskStatusPartiallyApproved,
			RolePair:     "coding-pair",
			Priority:     1,
			ReviewCommit: &rc1,
			History:      []models.TaskHistoryEntry{},
			Created:      now,
			Approvals: []models.Approval{
				{Agent: "code-reviewer-3", Provider: "google", Timestamp: now},
			},
		},
		{
			ID:           "task-diverse",
			Status:       models.TaskStatusPartiallyApproved,
			RolePair:     "coding-pair",
			Priority:     1,
			ReviewCommit: &rc2,
			History:      []models.TaskHistoryEntry{},
			Created:      now,
			Approvals: []models.Approval{
				{Agent: "code-reviewer-2", Provider: "anthropic", Timestamp: now},
			},
		},
	}
	// Claimer is google provider — should prefer task-diverse (approved by anthropic)
	googleReviewer := testhelpers.RegisteredTestAgent("code-reviewer")
	googleReviewer.Provider = "google"
	state.Agents["code-reviewer-1"] = googleReviewer
	testhelpers.WriteInitialState(t, stateFile, state)

	// Run multiple times to verify diversity preference is deterministic
	for i := 0; i < 5; i++ {
		result, err := ClaimReviewerTask(ClaimReviewerTaskInput{
			ProjectRoot:   tmpDir,
			AgentID:       "code-reviewer-1",
			LeaseDuration: 1800,
		})
		if err != nil {
			t.Fatalf("ClaimReviewerTask() iteration %d error: %v", i, err)
		}
		if result.TaskID != "task-diverse" {
			t.Errorf("iteration %d: TaskID = %q, want %q (diverse provider preferred)", i, result.TaskID, "task-diverse")
		}

		// Reset state for next iteration
		state.Tasks[0].Status = models.TaskStatusPartiallyApproved
		state.Tasks[0].ReviewingBy = nil
		state.Tasks[0].ReviewLeaseExpires = nil
		state.Tasks[1].Status = models.TaskStatusPartiallyApproved
		state.Tasks[1].ReviewingBy = nil
		state.Tasks[1].ReviewLeaseExpires = nil
		googleReviewer = testhelpers.RegisteredTestAgent("code-reviewer")
		googleReviewer.Provider = "google"
		state.Agents["code-reviewer-1"] = googleReviewer
		testhelpers.WriteInitialState(t, stateFile, state)
	}
}

func TestClaimReviewerTask_DiversityFreshSubmissions(t *testing.T) {
	// Tests fresh-submission diversity preference through selectBestCandidate.
	//
	// Architecture note: in production, all candidates for a single claiming agent
	// share one role-pair (and thus one reviewer pool), so isDiversitySatisfiable
	// returns the same value for all candidates. To test the preference *logic*,
	// we call selectBestCandidate directly with a mock resolver that maps different
	// role-pairs to different reviewer roles, creating tasks with distinct
	// diversity-satisfiability.
	//
	// Subcases from done_when:
	// (a) single alternate reviewer with different provider — preferred
	// (b) single alternate reviewer with same provider — no preference
	// (c) multiple alternate reviewers all sharing one different provider — preferred
	// (d) multiple alternate reviewers with mixed providers — preferred

	t.Run("diversity-satisfiable preferred over non-satisfiable", func(t *testing.T) {
		// Two equal-priority submitted tasks. One in role-pair "pair-diverse"
		// (has alternate reviewer with different provider), one in "pair-uniform"
		// (all reviewers share the claimer's provider). Verifies diverse task is chosen.
		pr := &diversityTestResolver{
			pairs: map[string]diversityPairDef{
				"pair-diverse": {reviewer: "rv-diverse", submitted: "SUBMITTED_D"},
				"pair-uniform": {reviewer: "rv-uniform", submitted: "SUBMITTED_U"},
			},
		}
		state := &models.State{
			Agents: map[string]models.Agent{
				"rv-diverse-2": reviewerCapacityTestAgent("rv-diverse", "anthropic"), // different from claimer
				"rv-uniform-2": reviewerCapacityTestAgent("rv-uniform", "google"),    // same as claimer
			},
		}
		taskDiverse := &models.Task{
			ID: "task-diverse", RolePair: "pair-diverse", Priority: 1,
			Status: "SUBMITTED_D",
		}
		taskUniform := &models.Task{
			ID: "task-uniform", RolePair: "pair-uniform", Priority: 1,
			Status: "SUBMITTED_U",
		}

		// Run multiple times to confirm deterministic preference, not random luck.
		for i := 0; i < 10; i++ {
			result := selectBestCandidate(
				[]*models.Task{taskDiverse, taskUniform},
				pr, "google", "claimer-1", state,
			)
			if result == nil {
				t.Fatal("selectBestCandidate returned nil")
			}
			if result.ID != "task-diverse" {
				t.Errorf("iteration %d: got %q, want %q (diversity-satisfiable preferred)",
					i, result.ID, "task-diverse")
			}
		}
	})

	t.Run("a: single alternate reviewer different provider", func(t *testing.T) {
		pr := &diversityTestResolver{
			pairs: map[string]diversityPairDef{
				"pair-a": {reviewer: "rv-a", submitted: "SUBMITTED_A"},
				"pair-b": {reviewer: "rv-b", submitted: "SUBMITTED_B"},
			},
		}
		state := &models.State{
			Agents: map[string]models.Agent{
				"rv-a-other": reviewerCapacityTestAgent("rv-a", "anthropic"), // diverse
				// No rv-b agent → not satisfiable
			},
		}
		taskA := &models.Task{ID: "task-a", RolePair: "pair-a", Priority: 1, Status: "SUBMITTED_A"}
		taskB := &models.Task{ID: "task-b", RolePair: "pair-b", Priority: 1, Status: "SUBMITTED_B"}

		for i := 0; i < 10; i++ {
			result := selectBestCandidate(
				[]*models.Task{taskA, taskB}, pr, "google", "claimer-1", state,
			)
			if result == nil || result.ID != "task-a" {
				t.Errorf("iteration %d: got %v, want task-a (single diverse reviewer preferred)", i, result)
			}
		}
	})

	t.Run("invalid alternate reviewer does not satisfy diversity", func(t *testing.T) {
		pr := &diversityTestResolver{
			pairs: map[string]diversityPairDef{
				"pair-a": {reviewer: "rv-a", submitted: "SUBMITTED_A"},
			},
		}
		state := &models.State{
			Agents: map[string]models.Agent{
				"rv-a-ghost": {Role: "rv-a", Provider: "anthropic", PID: 0}, // missing PID and lease
			},
		}
		task := &models.Task{ID: "task-a", RolePair: "pair-a", Priority: 1, Status: "SUBMITTED_A"}

		if isDiversitySatisfiable(task, "google", "claimer-1", pr, state) {
			t.Fatal("invalid alternate reviewer should not satisfy diversity")
		}
	})

	t.Run("b: single alternate reviewer same provider - no preference", func(t *testing.T) {
		pr := &diversityTestResolver{
			pairs: map[string]diversityPairDef{
				"pair-a": {reviewer: "rv-a", submitted: "SUBMITTED_A"},
				"pair-b": {reviewer: "rv-b", submitted: "SUBMITTED_B"},
			},
		}
		state := &models.State{
			Agents: map[string]models.Agent{
				"rv-a-other": reviewerCapacityTestAgent("rv-a", "google"), // same provider
				"rv-b-other": reviewerCapacityTestAgent("rv-b", "google"), // same provider
			},
		}
		taskA := &models.Task{ID: "task-a", RolePair: "pair-a", Priority: 1, Status: "SUBMITTED_A"}
		taskB := &models.Task{ID: "task-b", RolePair: "pair-b", Priority: 1, Status: "SUBMITTED_B"}

		// Neither is diversity-satisfiable → both go to "rest" → random selection.
		// Just verify it returns one of them without panic.
		result := selectBestCandidate(
			[]*models.Task{taskA, taskB}, pr, "google", "claimer-1", state,
		)
		if result == nil {
			t.Fatal("selectBestCandidate returned nil")
		}
		valid := result.ID == "task-a" || result.ID == "task-b"
		if !valid {
			t.Errorf("got %q, want task-a or task-b", result.ID)
		}
	})

	t.Run("c: multiple alternate reviewers all one different provider", func(t *testing.T) {
		pr := &diversityTestResolver{
			pairs: map[string]diversityPairDef{
				"pair-a": {reviewer: "rv-a", submitted: "SUBMITTED_A"},
				"pair-b": {reviewer: "rv-b", submitted: "SUBMITTED_B"},
			},
		}
		state := &models.State{
			Agents: map[string]models.Agent{
				"rv-a-2": reviewerCapacityTestAgent("rv-a", "google"),    // same
				"rv-a-3": reviewerCapacityTestAgent("rv-a", "anthropic"), // different → pair-a diverse
				"rv-b-2": reviewerCapacityTestAgent("rv-b", "google"),    // same only
			},
		}
		taskA := &models.Task{ID: "task-a", RolePair: "pair-a", Priority: 1, Status: "SUBMITTED_A"}
		taskB := &models.Task{ID: "task-b", RolePair: "pair-b", Priority: 1, Status: "SUBMITTED_B"}

		for i := 0; i < 10; i++ {
			result := selectBestCandidate(
				[]*models.Task{taskA, taskB}, pr, "google", "claimer-1", state,
			)
			if result == nil || result.ID != "task-a" {
				t.Errorf("iteration %d: got %v, want task-a (diversity satisfiable via mixed pool)", i, result)
			}
		}
	})

	t.Run("d: multiple alternate reviewers mixed providers", func(t *testing.T) {
		pr := &diversityTestResolver{
			pairs: map[string]diversityPairDef{
				"pair-a": {reviewer: "rv-a", submitted: "SUBMITTED_A"},
				"pair-b": {reviewer: "rv-b", submitted: "SUBMITTED_B"},
			},
		}
		state := &models.State{
			Agents: map[string]models.Agent{
				"rv-a-2": reviewerCapacityTestAgent("rv-a", "anthropic"), // different → diverse
				"rv-a-3": reviewerCapacityTestAgent("rv-a", "openai"),    // different → diverse
				"rv-b-2": reviewerCapacityTestAgent("rv-b", "google"),    // same only
			},
		}
		taskA := &models.Task{ID: "task-a", RolePair: "pair-a", Priority: 1, Status: "SUBMITTED_A"}
		taskB := &models.Task{ID: "task-b", RolePair: "pair-b", Priority: 1, Status: "SUBMITTED_B"}

		for i := 0; i < 10; i++ {
			result := selectBestCandidate(
				[]*models.Task{taskA, taskB}, pr, "google", "claimer-1", state,
			)
			if result == nil || result.ID != "task-a" {
				t.Errorf("iteration %d: got %v, want task-a (diversity always satisfiable)", i, result)
			}
		}
	})
}

func registerClaimReviewerTaskTestAgents(state *models.State) {
	state.Agents["code-reviewer-1"] = testhelpers.RegisteredTestAgent("code-reviewer")
	state.Agents["code-reviewer-2"] = testhelpers.RegisteredTestAgent("code-reviewer")
	state.Agents["code-plan-reviewer-1"] = testhelpers.RegisteredTestAgent("code-plan-reviewer")
}

func createClaimReviewWorktree(t *testing.T, projectRoot, taskID string) (string, string) {
	t.Helper()

	testhelpers.MustGit(t, projectRoot, "checkout", "integration")
	g := git.New(projectRoot)
	if _, err := g.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wtPath := g.GetWorktreePath(taskID)
	if err := os.WriteFile(filepath.Join(wtPath, "review.txt"), []byte("reviewable work\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "review.txt")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Add reviewable work")
	return g.GetWorktreeRelPath(taskID), testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")
}

func reviewerCapacityTestAgent(role, provider string) models.Agent {
	agent := testhelpers.RegisteredTestAgent(role)
	agent.Provider = provider
	return agent
}

func TestReviewerCapacityLeaseGrace(t *testing.T) {
	now := time.Now().UTC()
	agent := reviewerCapacityTestAgent("code-reviewer", "anthropic")

	withinGrace := now.Add(-(models.LeaseExpiryGracePeriod - time.Second))
	agent.LeaseExpires = &withinGrace
	if !hasReviewerCapacity(agent, "code-reviewer", now) {
		t.Fatal("lease expired within grace should still count as reviewer capacity")
	}

	beyondGrace := now.Add(-(models.LeaseExpiryGracePeriod + time.Second))
	agent.LeaseExpires = &beyondGrace
	if hasReviewerCapacity(agent, "code-reviewer", now) {
		t.Fatal("lease expired beyond grace should not count as reviewer capacity")
	}
}

// diversityPairDef defines a minimal role-pair for diversity testing.
type diversityPairDef struct {
	reviewer  string
	submitted models.TaskStatus
}

// diversityTestResolver is a minimal PipelineResolver mock for testing
// fresh-submission diversity preference. It maps role-pairs to distinct
// reviewer roles, enabling isDiversitySatisfiable to differentiate tasks.
type diversityTestResolver struct {
	pairs map[string]diversityPairDef
}

func (r *diversityTestResolver) ReviewerRole(rp string) (string, error) {
	p, ok := r.pairs[rp]
	if !ok {
		return "", fmt.Errorf("unknown role-pair %q", rp)
	}
	return p.reviewer, nil
}

func (r *diversityTestResolver) SubmittedStatus(rp string) (models.TaskStatus, error) {
	p, ok := r.pairs[rp]
	if !ok {
		return "", fmt.Errorf("unknown role-pair %q", rp)
	}
	return p.submitted, nil
}

func (r *diversityTestResolver) PartiallyApprovedStatus(string) (models.TaskStatus, error) {
	return "", fmt.Errorf("not configured")
}

// Unused interface methods — return errors.
func (r *diversityTestResolver) DoerRole(string) (string, error) { return "", fmt.Errorf("unused") }
func (r *diversityTestResolver) RoleType(string) (string, error) { return "", fmt.Errorf("unused") }
func (r *diversityTestResolver) AllRoleNames() []string          { return nil }
func (r *diversityTestResolver) InitialStatus(string) (models.TaskStatus, error) {
	return "", fmt.Errorf("unused")
}
func (r *diversityTestResolver) RejectedStatus(string) (models.TaskStatus, error) {
	return "", fmt.Errorf("unused")
}
func (r *diversityTestResolver) ReviewingStatus(string) (models.TaskStatus, error) {
	return "", fmt.Errorf("unused")
}
func (r *diversityTestResolver) ExecutingStatus(string) (models.TaskStatus, error) {
	return "", fmt.Errorf("unused")
}
func (r *diversityTestResolver) ApprovedStatus(string) (models.TaskStatus, error) {
	return "", fmt.Errorf("unused")
}
func (r *diversityTestResolver) Reviewing2Status(string) (models.TaskStatus, error) {
	return "", fmt.Errorf("unused")
}

func TestClaimReviewerTask_ReviewClaimCooldown(t *testing.T) {
	t.Run("recent review_claim_released from same agent filters candidate", func(t *testing.T) {
		tmpDir := t.TempDir()
		testhelpers.SetupTestGitRepo(t, tmpDir)
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		registerClaimReviewerTaskTestAgents(state)
		reviewCommit := "abc123"
		state.Tasks = []models.Task{
			{
				ID:           "task-1",
				Status:       models.TaskStatusReadyForReview,
				RolePair:     "coding-pair",
				Priority:     1,
				ReviewCommit: &reviewCommit,
				Created:      now,
				History: []models.TaskHistoryEntry{
					{
						Time:  now.Add(-10 * time.Second),
						Event: models.TaskEventReviewClaimReleased,
						Agent: testhelpers.StringPtr("code-reviewer-1"),
					},
				},
			},
		}
		state.Agents["code-reviewer-1"] = testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)
		testhelpers.WriteInitialState(t, stateFile, state)

		_, err := ClaimReviewerTask(ClaimReviewerTaskInput{
			ProjectRoot:   tmpDir,
			AgentID:       "code-reviewer-1",
			LeaseDuration: 1800,
		})
		if err == nil {
			t.Fatal("Expected PreconditionError due to cooldown, got nil")
		}
		if !strings.Contains(err.Error(), "claim cooldown") {
			t.Errorf("Error = %q, want to contain 'claim cooldown'", err.Error())
		}
	})

	t.Run("recent claim_released from same agent filters candidate", func(t *testing.T) {
		tmpDir := t.TempDir()
		testhelpers.SetupTestGitRepo(t, tmpDir)
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		registerClaimReviewerTaskTestAgents(state)
		reviewCommit := "abc123"
		state.Tasks = []models.Task{
			{
				ID:           "task-1",
				Status:       models.TaskStatusReadyForReview,
				RolePair:     "coding-pair",
				Priority:     1,
				ReviewCommit: &reviewCommit,
				Created:      now,
				History: []models.TaskHistoryEntry{
					{
						Time:  now.Add(-30 * time.Second),
						Event: models.TaskEventClaimReleased,
						Agent: testhelpers.StringPtr("code-reviewer-1"),
					},
				},
			},
		}
		state.Agents["code-reviewer-1"] = testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)
		testhelpers.WriteInitialState(t, stateFile, state)

		_, err := ClaimReviewerTask(ClaimReviewerTaskInput{
			ProjectRoot:   tmpDir,
			AgentID:       "code-reviewer-1",
			LeaseDuration: 1800,
		})
		if err == nil {
			t.Fatal("Expected PreconditionError due to cooldown, got nil")
		}
		if !strings.Contains(err.Error(), "claim cooldown") {
			t.Errorf("Error = %q, want to contain 'claim cooldown'", err.Error())
		}
	})

	t.Run("recent review_claim_released from different agent does not filter", func(t *testing.T) {
		tmpDir := t.TempDir()
		testhelpers.SetupTestGitRepo(t, tmpDir)
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		registerClaimReviewerTaskTestAgents(state)
		reviewCommit := "abc123"
		state.Tasks = []models.Task{
			{
				ID:           "task-1",
				Status:       models.TaskStatusReadyForReview,
				RolePair:     "coding-pair",
				Priority:     1,
				ReviewCommit: &reviewCommit,
				Created:      now,
				History: []models.TaskHistoryEntry{
					{
						Time:  now.Add(-10 * time.Second),
						Event: models.TaskEventReviewClaimReleased,
						Agent: testhelpers.StringPtr("code-reviewer-OTHER"),
					},
				},
			},
		}
		state.Agents["code-reviewer-1"] = testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)
		testhelpers.WriteInitialState(t, stateFile, state)

		result, err := ClaimReviewerTask(ClaimReviewerTaskInput{
			ProjectRoot:   tmpDir,
			AgentID:       "code-reviewer-1",
			LeaseDuration: 1800,
		})
		if err != nil {
			t.Fatalf("ClaimReviewerTask() error: %v", err)
		}
		if result.TaskID != "task-1" {
			t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
		}
	})

	t.Run("old review_claim_released beyond cooldown does not filter", func(t *testing.T) {
		tmpDir := t.TempDir()
		testhelpers.SetupTestGitRepo(t, tmpDir)
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		registerClaimReviewerTaskTestAgents(state)
		reviewCommit := "abc123"
		state.Tasks = []models.Task{
			{
				ID:           "task-1",
				Status:       models.TaskStatusReadyForReview,
				RolePair:     "coding-pair",
				Priority:     1,
				ReviewCommit: &reviewCommit,
				Created:      now,
				History: []models.TaskHistoryEntry{
					{
						Time:  now.Add(-120 * time.Second),
						Event: models.TaskEventReviewClaimReleased,
						Agent: testhelpers.StringPtr("code-reviewer-1"),
					},
				},
			},
		}
		state.Agents["code-reviewer-1"] = testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)
		testhelpers.WriteInitialState(t, stateFile, state)

		result, err := ClaimReviewerTask(ClaimReviewerTaskInput{
			ProjectRoot:   tmpDir,
			AgentID:       "code-reviewer-1",
			LeaseDuration: 1800,
		})
		if err != nil {
			t.Fatalf("ClaimReviewerTask() error: %v", err)
		}
		if result.TaskID != "task-1" {
			t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
		}
	})

	t.Run("all candidates in cooldown returns PreconditionError", func(t *testing.T) {
		tmpDir := t.TempDir()
		testhelpers.SetupTestGitRepo(t, tmpDir)
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		registerClaimReviewerTaskTestAgents(state)
		rc1 := "abc123"
		rc2 := "def456"
		state.Tasks = []models.Task{
			{
				ID:           "task-1",
				Status:       models.TaskStatusReadyForReview,
				RolePair:     "coding-pair",
				Priority:     1,
				ReviewCommit: &rc1,
				Created:      now,
				History: []models.TaskHistoryEntry{
					{
						Time:  now.Add(-5 * time.Second),
						Event: models.TaskEventReviewClaimReleased,
						Agent: testhelpers.StringPtr("code-reviewer-1"),
					},
				},
			},
			{
				ID:           "task-2",
				Status:       models.TaskStatusReadyForReview,
				RolePair:     "coding-pair",
				Priority:     1,
				ReviewCommit: &rc2,
				Created:      now,
				History: []models.TaskHistoryEntry{
					{
						Time:  now.Add(-15 * time.Second),
						Event: models.TaskEventClaimReleased,
						Agent: testhelpers.StringPtr("code-reviewer-1"),
					},
				},
			},
		}
		state.Agents["code-reviewer-1"] = testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)
		testhelpers.WriteInitialState(t, stateFile, state)

		_, err := ClaimReviewerTask(ClaimReviewerTaskInput{
			ProjectRoot:   tmpDir,
			AgentID:       "code-reviewer-1",
			LeaseDuration: 1800,
		})
		if err == nil {
			t.Fatal("Expected PreconditionError when all candidates in cooldown, got nil")
		}
		if !strings.Contains(err.Error(), "claim cooldown") {
			t.Errorf("Error = %q, want to contain 'claim cooldown'", err.Error())
		}
	})

	t.Run("mixed cooldown selects non-cooldown candidate", func(t *testing.T) {
		tmpDir := t.TempDir()
		testhelpers.SetupTestGitRepo(t, tmpDir)
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		registerClaimReviewerTaskTestAgents(state)
		rc1 := "abc123"
		rc2 := "def456"
		state.Tasks = []models.Task{
			{
				ID:           "task-cooldown",
				Status:       models.TaskStatusReadyForReview,
				RolePair:     "coding-pair",
				Priority:     1,
				ReviewCommit: &rc1,
				Created:      now,
				History: []models.TaskHistoryEntry{
					{
						Time:  now.Add(-10 * time.Second),
						Event: models.TaskEventReviewClaimReleased,
						Agent: testhelpers.StringPtr("code-reviewer-1"),
					},
				},
			},
			{
				ID:           "task-available",
				Status:       models.TaskStatusReadyForReview,
				RolePair:     "coding-pair",
				Priority:     1,
				ReviewCommit: &rc2,
				Created:      now,
				History:      []models.TaskHistoryEntry{},
			},
		}
		state.Agents["code-reviewer-1"] = testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)
		testhelpers.WriteInitialState(t, stateFile, state)

		result, err := ClaimReviewerTask(ClaimReviewerTaskInput{
			ProjectRoot:   tmpDir,
			AgentID:       "code-reviewer-1",
			LeaseDuration: 1800,
		})
		if err != nil {
			t.Fatalf("ClaimReviewerTask() error: %v", err)
		}
		if result.TaskID != "task-available" {
			t.Errorf("TaskID = %q, want %q (non-cooldown candidate)", result.TaskID, "task-available")
		}
	})
}

// doerDiversityResolver is a mock for testing doer-provider diversity filtering.
// It implements the interface required by filterDoerProviderDiversity.
type doerDiversityResolver struct {
	diversity      string            // default value returned by ProviderDiversity
	impactOverride map[string]string // impact → diversity override (optional)
	reviewerRole   string
}

func (r *doerDiversityResolver) ProviderDiversity(_ string, impact string) (string, error) {
	if r.impactOverride != nil {
		if v, ok := r.impactOverride[impact]; ok {
			return v, nil
		}
	}
	return r.diversity, nil
}

func (r *doerDiversityResolver) ReviewerRole(string) (string, error) {
	return r.reviewerRole, nil
}

func TestIsBlockedByDoerDiversity(t *testing.T) {
	doerID := "coder-1"

	t.Run("blocked when claimer shares doer provider and diverse reviewer exists", func(t *testing.T) {
		task := &models.Task{ID: "task-1", RolePair: "coding-pair", AssignedTo: &doerID}
		state := &models.State{
			Agents: map[string]models.Agent{
				"coder-1":         {Role: "coder", Provider: "anthropic"},
				"code-reviewer-1": {Role: "code-reviewer", Provider: "anthropic"},       // claimer
				"code-reviewer-2": reviewerCapacityTestAgent("code-reviewer", "google"), // diverse
			},
		}
		resolver := &doerDiversityResolver{diversity: "preferred", reviewerRole: "code-reviewer"}

		blocked := isBlockedByDoerDiversity(task, "anthropic", "code-reviewer-1", state, resolver)
		if !blocked {
			t.Error("expected blocked: claimer shares doer provider and diverse reviewer exists")
		}
	})

	t.Run("not blocked when claimer has different provider than doer", func(t *testing.T) {
		task := &models.Task{ID: "task-1", RolePair: "coding-pair", AssignedTo: &doerID}
		state := &models.State{
			Agents: map[string]models.Agent{
				"coder-1":         {Role: "coder", Provider: "anthropic"},
				"code-reviewer-1": {Role: "code-reviewer", Provider: "google"}, // different from doer
			},
		}
		resolver := &doerDiversityResolver{diversity: "preferred", reviewerRole: "code-reviewer"}

		blocked := isBlockedByDoerDiversity(task, "google", "code-reviewer-1", state, resolver)
		if blocked {
			t.Error("should not block: claimer has different provider than doer")
		}
	})

	t.Run("not blocked when no diverse reviewer registered", func(t *testing.T) {
		task := &models.Task{ID: "task-1", RolePair: "coding-pair", AssignedTo: &doerID}
		state := &models.State{
			Agents: map[string]models.Agent{
				"coder-1":         {Role: "coder", Provider: "anthropic"},
				"code-reviewer-1": {Role: "code-reviewer", Provider: "anthropic"}, // same as doer
			},
		}
		resolver := &doerDiversityResolver{diversity: "preferred", reviewerRole: "code-reviewer"}

		blocked := isBlockedByDoerDiversity(task, "anthropic", "code-reviewer-1", state, resolver)
		if blocked {
			t.Error("should not block: no diverse reviewer registered (fallback)")
		}
	})

	t.Run("not blocked when only diverse reviewer is invalid", func(t *testing.T) {
		task := &models.Task{ID: "task-1", RolePair: "coding-pair", AssignedTo: &doerID}
		state := &models.State{
			Agents: map[string]models.Agent{
				"coder-1":         {Role: "coder", Provider: "anthropic"},
				"code-reviewer-1": {Role: "code-reviewer", Provider: "anthropic"},
				"code-reviewer-2": {Role: "code-reviewer", Provider: "google", PID: 0}, // missing PID and lease
			},
		}
		resolver := &doerDiversityResolver{diversity: "preferred", reviewerRole: "code-reviewer"}

		blocked := isBlockedByDoerDiversity(task, "anthropic", "code-reviewer-1", state, resolver)
		if blocked {
			t.Error("should not block: invalid diverse reviewer does not count as capacity")
		}
	})

	t.Run("not blocked when diversity not configured", func(t *testing.T) {
		task := &models.Task{ID: "task-1", RolePair: "coding-pair", AssignedTo: &doerID}
		state := &models.State{
			Agents: map[string]models.Agent{
				"coder-1":         {Role: "coder", Provider: "anthropic"},
				"code-reviewer-1": {Role: "code-reviewer", Provider: "anthropic"},
				"code-reviewer-2": reviewerCapacityTestAgent("code-reviewer", "google"),
			},
		}
		resolver := &doerDiversityResolver{diversity: "", reviewerRole: "code-reviewer"}

		blocked := isBlockedByDoerDiversity(task, "anthropic", "code-reviewer-1", state, resolver)
		if blocked {
			t.Error("should not block: provider-diversity not configured")
		}
	})

	t.Run("not blocked when doer agent not in state", func(t *testing.T) {
		missingDoer := "coder-gone"
		task := &models.Task{ID: "task-1", RolePair: "coding-pair", AssignedTo: &missingDoer}
		state := &models.State{
			Agents: map[string]models.Agent{
				"code-reviewer-1": reviewerCapacityTestAgent("code-reviewer", "anthropic"),
				"code-reviewer-2": reviewerCapacityTestAgent("code-reviewer", "google"),
			},
		}
		resolver := &doerDiversityResolver{diversity: "preferred", reviewerRole: "code-reviewer"}

		blocked := isBlockedByDoerDiversity(task, "anthropic", "code-reviewer-1", state, resolver)
		if blocked {
			t.Error("should not block: doer agent not in state (skip filter)")
		}
	})

	t.Run("not blocked when task has no AssignedTo", func(t *testing.T) {
		task := &models.Task{ID: "task-1", RolePair: "coding-pair", AssignedTo: nil}
		state := &models.State{
			Agents: map[string]models.Agent{
				"code-reviewer-1": reviewerCapacityTestAgent("code-reviewer", "anthropic"),
				"code-reviewer-2": reviewerCapacityTestAgent("code-reviewer", "google"),
			},
		}
		resolver := &doerDiversityResolver{diversity: "preferred", reviewerRole: "code-reviewer"}

		blocked := isBlockedByDoerDiversity(task, "anthropic", "code-reviewer-1", state, resolver)
		if blocked {
			t.Error("should not block: task has no AssignedTo")
		}
	})

	t.Run("blocked even when diverse reviewer is busy", func(t *testing.T) {
		task := &models.Task{ID: "task-1", RolePair: "coding-pair", AssignedTo: &doerID}
		busyTask := "other-task"
		state := &models.State{
			Agents: map[string]models.Agent{
				"coder-1":         {Role: "coder", Provider: "anthropic"},
				"code-reviewer-1": {Role: "code-reviewer", Provider: "anthropic"},
				"code-reviewer-2": func() models.Agent {
					agent := reviewerCapacityTestAgent("code-reviewer", "google")
					agent.Status = models.AgentStatusReviewing
					agent.CurrentTask = &busyTask
					return agent
				}(),
			},
		}
		resolver := &doerDiversityResolver{diversity: "preferred", reviewerRole: "code-reviewer"}

		blocked := isBlockedByDoerDiversity(task, "anthropic", "code-reviewer-1", state, resolver)
		if !blocked {
			t.Error("expected blocked: diverse reviewer is registered (even if busy)")
		}
	})
}

func TestFilterDoerProviderDiversity(t *testing.T) {
	doerID := "coder-1"

	t.Run("filters all candidates when all share doer provider", func(t *testing.T) {
		tasks := []*models.Task{
			{ID: "task-1", RolePair: "coding-pair", AssignedTo: &doerID},
			{ID: "task-2", RolePair: "coding-pair", AssignedTo: &doerID},
		}
		state := &models.State{
			Agents: map[string]models.Agent{
				"coder-1":         {Role: "coder", Provider: "anthropic"},
				"code-reviewer-1": {Role: "code-reviewer", Provider: "anthropic"},
				"code-reviewer-2": reviewerCapacityTestAgent("code-reviewer", "google"),
			},
		}
		resolver := &doerDiversityResolver{diversity: "preferred", reviewerRole: "code-reviewer"}

		filtered := filterDoerProviderDiversity(tasks, "anthropic", "code-reviewer-1", state, resolver)
		if len(filtered) != 0 {
			t.Errorf("expected 0 candidates (all blocked), got %d", len(filtered))
		}
	})

	t.Run("keeps candidates with different doer provider", func(t *testing.T) {
		doer2 := "coder-2"
		tasks := []*models.Task{
			{ID: "task-blocked", RolePair: "coding-pair", AssignedTo: &doerID}, // doer is anthropic
			{ID: "task-ok", RolePair: "coding-pair", AssignedTo: &doer2},       // doer is google
		}
		state := &models.State{
			Agents: map[string]models.Agent{
				"coder-1":         {Role: "coder", Provider: "anthropic"},
				"coder-2":         {Role: "coder", Provider: "google"},
				"code-reviewer-1": {Role: "code-reviewer", Provider: "anthropic"},
				"code-reviewer-2": reviewerCapacityTestAgent("code-reviewer", "google"),
			},
		}
		resolver := &doerDiversityResolver{diversity: "preferred", reviewerRole: "code-reviewer"}

		filtered := filterDoerProviderDiversity(tasks, "anthropic", "code-reviewer-1", state, resolver)
		if len(filtered) != 1 {
			t.Fatalf("expected 1 candidate, got %d", len(filtered))
		}
		if filtered[0].ID != "task-ok" {
			t.Errorf("expected task-ok to survive, got %s", filtered[0].ID)
		}
	})

	t.Run("skips filter when claimer has no provider", func(t *testing.T) {
		tasks := []*models.Task{
			{ID: "task-1", RolePair: "coding-pair", AssignedTo: &doerID},
		}
		state := &models.State{
			Agents: map[string]models.Agent{
				"coder-1":         {Role: "coder", Provider: "anthropic"},
				"code-reviewer-2": reviewerCapacityTestAgent("code-reviewer", "google"),
			},
		}
		resolver := &doerDiversityResolver{diversity: "preferred", reviewerRole: "code-reviewer"}

		filtered := filterDoerProviderDiversity(tasks, "", "code-reviewer-1", state, resolver)
		if len(filtered) != 1 {
			t.Errorf("expected all candidates kept (no claimer provider), got %d", len(filtered))
		}
	})

	t.Run("uses effective impact from task history", func(t *testing.T) {
		// Diversity is configured only for "significant" impact (not at base level).
		// Task has a checkpoint history entry declaring "significant" impact.
		// The filter should resolve effective impact and block accordingly.
		doer := "coder-1"
		task := &models.Task{
			ID:         "task-sig",
			RolePair:   "coding-pair",
			AssignedTo: &doer,
			Status:     models.TaskStatusPartiallyApproved,
			Approvals: []models.Approval{
				{Agent: "code-reviewer-3", Provider: "openai"},
			},
			History: []models.TaskHistoryEntry{
				{
					Event: models.TaskEventPreExecutionCheckpoint,
					Extra: map[string]any{"impact": "significant"},
				},
			},
		}
		state := &models.State{
			Agents: map[string]models.Agent{
				"coder-1":         {Role: "coder", Provider: "anthropic"},
				"code-reviewer-1": {Role: "code-reviewer", Provider: "anthropic"},
				"code-reviewer-2": reviewerCapacityTestAgent("code-reviewer", "google"),
			},
		}
		// No base-level diversity; only "significant" has it.
		resolver := &doerDiversityResolver{
			diversity:      "",
			impactOverride: map[string]string{"significant": "preferred"},
			reviewerRole:   "code-reviewer",
		}

		filtered := filterDoerProviderDiversity(
			[]*models.Task{task}, "anthropic", "code-reviewer-1", state, resolver,
		)
		if len(filtered) != 0 {
			t.Error("expected blocked: significant impact activates diversity from override")
		}
	})

	t.Run("standard impact not blocked when diversity only on override", func(t *testing.T) {
		// Diversity is configured only for "significant" impact.
		// Task has no impact history (standard). Should NOT be blocked.
		doer := "coder-1"
		task := &models.Task{
			ID:         "task-std",
			RolePair:   "coding-pair",
			AssignedTo: &doer,
			History:    []models.TaskHistoryEntry{},
		}
		state := &models.State{
			Agents: map[string]models.Agent{
				"coder-1":         {Role: "coder", Provider: "anthropic"},
				"code-reviewer-1": {Role: "code-reviewer", Provider: "anthropic"},
				"code-reviewer-2": reviewerCapacityTestAgent("code-reviewer", "google"),
			},
		}
		resolver := &doerDiversityResolver{
			diversity:      "",
			impactOverride: map[string]string{"significant": "preferred"},
			reviewerRole:   "code-reviewer",
		}

		filtered := filterDoerProviderDiversity(
			[]*models.Task{task}, "anthropic", "code-reviewer-1", state, resolver,
		)
		if len(filtered) != 1 {
			t.Error("should not block: standard impact has no diversity configured")
		}
	})
}
