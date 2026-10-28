package models

import (
	"fmt"
	"slices"
)

// TaskStatus represents the state of a task
type TaskStatus string

const (
	TaskStatusDraft                TaskStatus = "DRAFT"
	TaskStatusReady                TaskStatus = "DRAFT_CODE"
	TaskStatusImplementing         TaskStatus = "IMPLEMENTING_CODE"
	TaskStatusReadyForReview       TaskStatus = "CODE_TO_REVIEW"
	TaskStatusLegacyReadyForReview TaskStatus = "CODE_READY_FOR_REVIEW"
	TaskStatusReviewing            TaskStatus = "REVIEWING_CODE"
	TaskStatusRejected             TaskStatus = "CODE_REJECTED"
	TaskStatusApproved             TaskStatus = "CODE_APPROVED"
	TaskStatusMerged               TaskStatus = "MERGED"
	TaskStatusBlocked              TaskStatus = "BLOCKED"
	TaskStatusAbandoned            TaskStatus = "ABANDONED"
	TaskStatusSuperseded           TaskStatus = "SUPERSEDED"
	TaskStatusIntegrationFailed    TaskStatus = "INTEGRATION_FAILED"

	// Code-planning pair states
	TaskStatusDraftCodingPlan     TaskStatus = "DRAFT_CODING_PLAN"
	TaskStatusCodePlanning        TaskStatus = "CODE_PLANNING"
	TaskStatusCodingPlanToReview  TaskStatus = "CODING_PLAN_TO_REVIEW"
	TaskStatusReviewingCodingPlan TaskStatus = "REVIEWING_CODING_PLAN"
	TaskStatusCodingPlanApproved  TaskStatus = "CODING_PLAN_APPROVED"
	TaskStatusCodingPlanRejected  TaskStatus = "CODING_PLAN_REJECTED"

	// Quorum review states (coding-pair)
	TaskStatusPartiallyApproved TaskStatus = "CODE_PARTIALLY_APPROVED"
	TaskStatusReviewingCode2    TaskStatus = "REVIEWING_CODE_2"
)

// IsValid checks if the task status is valid
func (ts TaskStatus) IsValid() bool {
	switch ts {
	case TaskStatusDraft, TaskStatusReady, TaskStatusImplementing,
		TaskStatusReadyForReview, TaskStatusLegacyReadyForReview, TaskStatusReviewing, TaskStatusRejected,
		TaskStatusApproved, TaskStatusMerged, TaskStatusBlocked,
		TaskStatusAbandoned, TaskStatusSuperseded, TaskStatusIntegrationFailed,
		TaskStatusDraftCodingPlan, TaskStatusCodePlanning,
		TaskStatusCodingPlanToReview, TaskStatusReviewingCodingPlan,
		TaskStatusCodingPlanApproved, TaskStatusCodingPlanRejected,
		TaskStatusPartiallyApproved, TaskStatusReviewingCode2:
		return true
	}
	return false
}

// IsTerminal checks if the task status is terminal (no further transitions)
func (ts TaskStatus) IsTerminal() bool {
	return ts == TaskStatusMerged || ts == TaskStatusAbandoned || ts == TaskStatusSuperseded
}

type pipelineCleanStatusResolver interface {
	CleanStatus(rolePair string) (TaskStatus, error)
}

// IsOperationallyTerminal reports whether a task has no remaining operational
// work. Pipeline-declared clean states are terminal, while approved states that
// merely feed another pipeline stage remain active.
func IsOperationallyTerminal(task *Task, pr PipelineResolver) bool {
	if task == nil {
		return false
	}
	if task.Status.IsTerminal() {
		return true
	}
	cleanResolver, ok := pr.(pipelineCleanStatusResolver)
	if !ok || task.RolePair == "" {
		return false
	}
	cleanStatus, err := cleanResolver.CleanStatus(task.RolePair)
	return err == nil && task.Status == cleanStatus
}

// IsSprintTerminal checks if the task status is terminal for sprint completion purposes.
// MERGED is the universal sprint-terminal state for all role-pairs.
func (ts TaskStatus) IsSprintTerminal() bool {
	return ts.IsTerminal()
}

// IsReadyForReviewStatus reports whether the status is a submitted-for-review
// coding status, including the legacy value used by older frozen pipelines.
func (ts TaskStatus) IsReadyForReviewStatus() bool {
	return ts == TaskStatusReadyForReview || ts == TaskStatusLegacyReadyForReview
}

// IsPipelineValid checks if the status is valid in a pipeline context.
// A status is valid if it is a hardcoded valid status (legacy) OR appears in
// the provided set of pipeline-declared states OR is a cross-cutting meta-state.
func (ts TaskStatus) IsPipelineValid(declaredStates []TaskStatus) bool {
	if ts.IsValid() {
		return true
	}
	return slices.Contains(declaredStates, ts)
}

// CanPipelineTransition checks if a transition from ts to the given target is
// valid using the provided transition map (built from pipeline config).
func (ts TaskStatus) CanPipelineTransition(to TaskStatus, transitions map[TaskStatus][]TaskStatus) bool {
	return slices.Contains(transitions[ts], to)
}

// IsPipelineSprintTerminal checks if the status is terminal for sprint purposes
// using pipeline-defined terminal states. Universal terminals (MERGED, ABANDONED,
// SUPERSEDED) are always considered sprint-terminal.
func (ts TaskStatus) IsPipelineSprintTerminal(terminalStates []TaskStatus) bool {
	return ts.IsTerminal() || slices.Contains(terminalStates, ts)
}

// TransitionWith validates and applies a status transition using a transition map
// built from pipeline config. The target status must exist as a key in the
// transition map (i.e., be a declared state).
func (t *Task) TransitionWith(to TaskStatus, transitions map[TaskStatus][]TaskStatus) error {
	if !slices.Contains(transitions[t.Status], to) {
		return fmt.Errorf("invalid task transition: %s → %s (task %s)", t.Status, to, t.ID)
	}
	if _, declared := transitions[to]; !declared {
		return fmt.Errorf("target status %s is not a declared state in the transition map (task %s)", to, t.ID)
	}
	t.Status = to
	return nil
}

// IsApprovedForMerge checks if a task is in an approved state eligible for merge.
func IsApprovedForMerge(task *Task, pr PipelineResolver) bool {
	if task.RolePair == "" || pr == nil {
		return false
	}
	approved, err := pr.ApprovedStatus(task.RolePair)
	return err == nil && task.Status == approved
}

// IsSubmittedStatus checks if a task is in a submitted state.
func IsSubmittedStatus(task *Task, pr PipelineResolver) bool {
	if task.RolePair == "" || pr == nil {
		return false
	}
	submitted, err := pr.SubmittedStatus(task.RolePair)
	return err == nil && task.Status == submitted
}

// IsExecutingStatus checks if a task is in an executing state.
func IsExecutingStatus(task *Task, pr PipelineResolver) bool {
	if task.RolePair == "" || pr == nil {
		return false
	}
	executing, err := pr.ExecutingStatus(task.RolePair)
	return err == nil && task.Status == executing
}
