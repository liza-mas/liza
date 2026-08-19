package testhelpers

// fixtures.go contains test helpers for creating state and task fixtures.
//
// This file provides factory functions for generating valid State and Task objects
// with appropriate defaults for testing. These fixtures ensure tests have realistic
// data structures without duplicating complex initialization code.
//
// Usage Example:
//
//	func TestStateValidation(t *testing.T) {
//	    state := testhelpers.CreateValidState()
//	    state.Goal.Description = "Custom test goal"
//
//	    tmpDir := t.TempDir()
//	    statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
//	    testhelpers.WriteInitialState(t, statePath, state)
//	    // ... continue with test
//	}
//
//	func TestTaskClaim(t *testing.T) {
//	    now := time.Now().UTC()
//	    task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusPending, now)
//	    // ... continue with test
//	}
//
// The fixtures created by these functions represent valid, production-like state
// that satisfies all validation rules from the original bash implementation.

import (
	"os"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

// CreateValidState creates a complete, valid State object for testing.
// This is the standard state fixture used across multiple test files.
// It includes all required fields with sensible defaults:
//   - Version 1
//   - A test goal in progress status
//   - Empty task list (tests can add tasks as needed)
//   - Empty agent map
//   - A sprint in progress
//   - Circuit breaker in OK status
//   - Standard config with reasonable timeouts
//
// This helper eliminates ~50+ lines of duplicated state initialization code
// that appears 15-20 times across test files. It was originally defined in
// validate_test.go:421 and has been moved here for shared use.
//
// Tests can customize the returned state as needed:
//
//	state := testhelpers.CreateValidState()
//	state.Goal.Description = "Custom goal"
//	state.Tasks = []models.Task{...}
func CreateValidState() *models.State {
	now := time.Now().UTC()
	goalID := "goal-test"

	return &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          goalID,
			Description: "Test goal",
			SpecRef:     "specs/vision.md",
			Created:     now,
			Status:      models.GoalStatusInProgress,
			AlignmentHistory: []models.AlignmentHistory{
				{
					Timestamp: now,
					Event:     "initialization",
					Summary:   "Initial goal",
				},
			},
		},
		Tasks:         []models.Task{},
		Agents:        make(map[string]models.Agent),
		Discovered:    []models.Discovery{},
		HumanNotes:    []models.HumanNote{},
		SpecChanges:   []models.SpecChange{},
		Anomalies:     []models.Anomaly{},
		SprintHistory: []models.SprintSummary{},
		Sprint: models.Sprint{
			ID:      "sprint-1",
			Number:  1,
			GoalRef: goalID,
			Scope: models.SprintScope{
				Planned: []string{},
				Stretch: []string{},
			},
			Timeline: models.SprintTimeline{
				Started: now,
			},
			Status: models.SprintStatusInProgress,
			Metrics: models.SprintMetrics{
				TasksDone:         0,
				TasksInProgress:   0,
				TasksBlocked:      0,
				IterationsTotal:   0,
				ReviewCyclesTotal: 0,
			},
		},
		CircuitBreaker: models.CircuitBreaker{
			Status:  "OK",
			History: []models.CircuitBreakerHistory{},
		},
		Config: models.Config{
			MaxCoderIterations:              10,
			MaxReviewCycles:                 5,
			MaxGlobalIntegrationGenerations: models.NormalizeGlobalIntegrationGenerationLimit(0),
			HeartbeatInterval:               60,
			LeaseDuration:                   1800,
			CoderPollInterval:               30,
			DoerMaxWait:                     1800,
			OrchestratorPollInterval:        60,
			OrchestratorMaxWait:             1800,
			ReviewerPollInterval:            30,
			ReviewerMaxWait:                 1800,
			AgentProgressTimeout:            models.DefaultAgentProgressTimeoutSec,
			IntegrationBranch:               "integration",
			Mode:                            models.SystemModeRunning,
		},
	}
}

// WriteInitialState writes a state to the database and returns a Blackboard instance.
// This is a convenience helper that combines the common pattern of:
//   - Creating a new db.Blackboard
//   - Writing initial state
//   - Handling errors
//
// This helper eliminates ~3-4 lines of duplicated code that appears 15 times
// across test files.
//
// Usage:
//
//	state := testhelpers.CreateValidState()
//	bb := testhelpers.WriteInitialState(t, statePath, state)
//	// ... continue with test using bb
func WriteInitialState(t *testing.T, statePath string, state *models.State) *db.Blackboard {
	t.Helper()

	bb := db.New(statePath)
	if err := bb.Write(state); err != nil {
		t.Fatalf("Failed to write initial state: %v", err)
	}
	return bb
}

// BuildTaskByStatus creates a Task with fields appropriate for the given status.
// This helper understands the state machine requirements and sets required fields
// based on the task status:
//
//   - READY: Basic task ready to be claimed
//   - IMPLEMENTING: Sets AssignedTo, LeaseExpires, BaseCommit, Worktree
//   - READY_FOR_REVIEW: Sets ReviewCommit, all IMPLEMENTING fields
//   - REJECTED: Sets RejectionReason, ReviewCommit, increments Iteration
//   - APPROVED: Sets ApprovedBy, ReviewCommit
//   - MERGED: Sets MergeCommit, ApprovedBy
//   - BLOCKED: Sets BlockedReason, BlockedQuestions
//   - INTEGRATION_FAILED: Sets AssignedTo from previous attempt
//
// This helper eliminates ~20-30 lines of complex conditional logic that appears
// 12-15 times across test files, particularly in claim_task_test.go, wt_delete_test.go,
// submit_review_test.go, and others.
//
// Usage:
//
//	now := time.Now().UTC()
//	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
//	// Task now has all required fields for IMPLEMENTING status
func BuildTaskByStatus(taskID string, status models.TaskStatus, now time.Time) models.Task {
	task := models.Task{
		ID:          taskID,
		Type:        models.TaskTypeCoding,
		Description: "Test task",
		Status:      status,
		Priority:    1,
		Created:     now,
		SpecRef:     "README.md",
		DoneWhen:    "Task is complete",
		Scope:       "Test scope",
		History:     []models.TaskHistoryEntry{},
		RolePair:    inferRolePair(status),
	}

	// Add HandoffEvents for post-submission statuses
	if needsSubmissionEvent(status) {
		task.HandoffEvents = append(task.HandoffEvents, models.HandoffEvent{
			Timestamp: now,
			Agent:     "coder-1",
			Trigger:   models.HandoffTriggerSubmission,
		})
	}
	if status == models.TaskStatusMerged {
		task.HandoffEvents = append(task.HandoffEvents, models.HandoffEvent{
			Timestamp: now,
			Agent:     "code-reviewer-1",
			Trigger:   models.HandoffTriggerCompletion,
		})
	}

	switch status {
	case models.TaskStatusReady:

	case models.TaskStatusImplementing:
		agent := "coder-1"
		task.AssignedTo = &agent
		leaseExpires := now.Add(30 * time.Minute)
		task.LeaseExpires = &leaseExpires
		baseCommit := "abc1234"
		task.BaseCommit = &baseCommit
		worktree := ".worktrees/" + taskID
		task.Worktree = &worktree

	case models.TaskStatusReadyForReview:
		agent := "coder-1"
		task.AssignedTo = &agent
		reviewCommit := "review123"
		task.ReviewCommit = &reviewCommit
		baseCommit := "abc1234"
		task.BaseCommit = &baseCommit
		worktree := ".worktrees/" + taskID
		task.Worktree = &worktree

	case models.TaskStatusReviewing:
		agent := "coder-1"
		task.AssignedTo = &agent
		reviewCommit := "review123"
		task.ReviewCommit = &reviewCommit
		reviewingBy := "code-reviewer-1"
		task.ReviewingBy = &reviewingBy
		reviewLeaseExpires := now.Add(30 * time.Minute)
		task.ReviewLeaseExpires = &reviewLeaseExpires
		baseCommit := "abc1234"
		task.BaseCommit = &baseCommit
		worktree := ".worktrees/" + taskID
		task.Worktree = &worktree

	case models.TaskStatusRejected:
		agent := "coder-1"
		task.AssignedTo = &agent
		leaseExpires := now.Add(30 * time.Minute)
		task.LeaseExpires = &leaseExpires
		reason := "Needs improvement"
		task.RejectionReason = &reason
		reviewCommit := "review123"
		task.ReviewCommit = &reviewCommit
		task.Iteration = 1
		worktree := ".worktrees/" + taskID
		task.Worktree = &worktree

	case models.TaskStatusApproved:
		agent := "code-reviewer-1"
		task.ApprovedBy = &agent
		reviewCommit := "review123"
		task.ReviewCommit = &reviewCommit
		worktree := ".worktrees/" + taskID
		task.Worktree = &worktree

	case models.TaskStatusMerged:
		agent := "code-reviewer-1"
		task.ApprovedBy = &agent
		mergeCommit := "merge456"
		task.MergeCommit = &mergeCommit

	case models.TaskStatusBlocked:
		agent := "coder-1"
		task.AssignedTo = &agent
		reason := "Blocked on clarification"
		task.BlockedReason = &reason
		task.BlockedQuestions = []string{"Need clarification on requirements"}
		worktree := ".worktrees/" + taskID
		task.Worktree = &worktree

	case models.TaskStatusIntegrationFailed:
		agent := "coder-1"
		task.AssignedTo = &agent
		worktree := ".worktrees/" + taskID
		task.Worktree = &worktree

	case models.TaskStatusAbandoned:

	case models.TaskStatusSuperseded:
		supersededBy := []string{"task-new"}
		task.SupersededBy = supersededBy

	case models.TaskStatusDraft:
	}

	return task
}

// StringPtr returns a pointer to the given string.
// This is a convenience helper for creating optional string fields in test data.
//
// This eliminates the duplicate stringPtr helper that appears in both
// claim_task_test.go:654 and models_test.go:481.
//
// Usage:
//
//	task.AssignedTo = testhelpers.StringPtr("agent-1")
func StringPtr(s string) *string {
	return &s
}

// TimePtr returns a pointer to the given time.
// This is a convenience helper for creating optional time fields in test data.
//
// Usage:
//
//	task.LeaseExpires = testhelpers.TimePtr(time.Now().Add(30 * time.Minute))
func TimePtr(t time.Time) *time.Time {
	return &t
}

// TransitionToReviewing transitions a READY_FOR_REVIEW task to REVIEWING state.
// This simulates what the supervisor does when a reviewer claims a task for review.
func TransitionToReviewing(t *testing.T, bb *db.Blackboard, taskID, reviewerID string) {
	t.Helper()

	now := time.Now().UTC()
	leaseExpires := now.Add(30 * time.Minute)

	err := bb.Modify(func(state *models.State) error {
		for i := range state.Tasks {
			if state.Tasks[i].ID == taskID {
				state.Tasks[i].Status = models.TaskStatusReviewing
				state.Tasks[i].ReviewingBy = &reviewerID
				state.Tasks[i].ReviewLeaseExpires = &leaseExpires
				return nil
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to transition task %s to REVIEWING: %v", taskID, err)
	}
}

// RegisterTestAgent registers a test agent with standard defaults in the blackboard.
// This eliminates the repeated 10-line models.Agent struct literal that appears
// 17+ times across integration test files.
//
// Usage:
//
//	testhelpers.RegisterTestAgent(t, bb, "coder-1", "coder")
//	testhelpers.RegisterTestAgent(t, bb, "code-reviewer-1", "code-reviewer")
func RegisterTestAgent(t *testing.T, bb *db.Blackboard, agentID, role string) {
	t.Helper()

	err := bb.Modify(func(state *models.State) error {
		state.Agents[agentID] = RegisteredTestAgent(role)
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to register agent %s: %v", agentID, err)
	}
}

// RegisteredTestAgent returns an agent record equivalent to a live supervisor
// registration for unit tests that construct state before writing it.
func RegisteredTestAgent(role string) models.Agent {
	now := time.Now().UTC()
	leaseExpires := now.Add(30 * time.Minute)
	return models.Agent{
		Role:            role,
		Status:          models.AgentStatusWaiting,
		Heartbeat:       now,
		LeaseExpires:    &leaseExpires,
		CurrentTask:     nil,
		Terminal:        "test",
		Provider:        "test",
		IterationsTotal: 0,
		ContextPercent:  0,
		PID:             os.Getpid(),
	}
}

// needsSubmissionEvent returns true if a task in the given status should have
// at least one HandoffEvent with trigger=submission to pass validation.
func needsSubmissionEvent(status models.TaskStatus) bool {
	switch status {
	case models.TaskStatusReadyForReview, models.TaskStatusReviewing,
		models.TaskStatusRejected, models.TaskStatusApproved,
		models.TaskStatusMerged, models.TaskStatusIntegrationFailed,
		models.TaskStatusPartiallyApproved, models.TaskStatusReviewingCode2,
		models.TaskStatusCodingPlanToReview, models.TaskStatusReviewingCodingPlan,
		models.TaskStatusCodingPlanApproved, models.TaskStatusCodingPlanRejected:
		return true
	}
	return false
}

// inferRolePair returns the role-pair for role-pair-specific statuses.
// Truly role-agnostic statuses (ABANDONED, SUPERSEDED, DRAFT) return empty string —
// tests needing pipeline operations on these must set RolePair explicitly.
func inferRolePair(status models.TaskStatus) string {
	switch status {
	case models.TaskStatusReady, models.TaskStatusImplementing,
		models.TaskStatusReadyForReview, models.TaskStatusReviewing,
		models.TaskStatusRejected, models.TaskStatusApproved,
		models.TaskStatusIntegrationFailed, models.TaskStatusMerged,
		models.TaskStatusBlocked,
		models.TaskStatusPartiallyApproved, models.TaskStatusReviewingCode2:
		return "coding-pair"
	case models.TaskStatusDraftCodingPlan, models.TaskStatusCodePlanning,
		models.TaskStatusCodingPlanToReview, models.TaskStatusReviewingCodingPlan,
		models.TaskStatusCodingPlanApproved, models.TaskStatusCodingPlanRejected:
		return "code-planning-pair"
	default:
		return ""
	}
}
