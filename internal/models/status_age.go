package models

import "time"

// statusTransitionEvents are the task-history events written at a site that
// also assigns Task.Status. Time in status is measured from the most recent one.
//
// TaskEventClaimReleased is deliberately absent: it changes status only when the
// releasing agent held the active executing or reviewing status
// (internal/agent/registration.go), and the entry records nothing that
// distinguishes the two cases. See TECH_DEBT.md.
var statusTransitionEvents = map[TaskEventName]struct{}{
	TaskEventClaimed:                  {},
	TaskEventReclaimedAfterRejection:  {},
	TaskEventReassignedAfterRejection: {},
	TaskEventClaimedForIntegrationFix: {},
	TaskEventSubmittedForReview:       {},
	TaskEventApproved:                 {},
	TaskEventRejected:                 {},
	TaskEventBlocked:                  {},
	TaskEventUnblocked:                {},
	TaskEventMerged:                   {},
	TaskEventAbandoned:                {},
	TaskEventSuperseded:               {},
	TaskEventIntegrationFailed:        {},
	TaskEventRecoveredFresh:           {},
	TaskEventRecoveryFreshFailed:      {},
}

// nonStatusTransitionEvents are recorded without assigning Task.Status:
// progress, bookkeeping, claim release, agent-side state, and events recorded
// on a trigger task rather than on the task that moved.
var nonStatusTransitionEvents = map[TaskEventName]struct{}{
	TaskEventCreated:                   {},
	TaskEventInitialization:            {},
	TaskEventPlanning:                  {},
	TaskEventPreExecutionCheckpoint:    {},
	TaskEventOutputSet:                 {},
	TaskEventReviewCommitUpdated:       {},
	TaskEventReviewVerdictApproved:     {},
	TaskEventReviewVerdictRejected:     {},
	TaskEventClaimReleased:             {},
	TaskEventDoerClaimReleased:         {},
	TaskEventReviewClaimReleased:       {},
	TaskEventNewAttempt:                {},
	TaskEventHandoffInitiated:          {},
	TaskEventHandoffResumed:            {},
	TaskEventOwnedTaskResumed:          {},
	TaskEventWorktreeRecovered:         {},
	TaskEventTransitionExecuted:        {},
	TaskEventTransitionCrashRecov:      {},
	TaskEventTransitionCycleBlocked:    {},
	TaskEventOrchestratorAssessment:    {},
	TaskEventReplanned:                 {},
	TaskEventDependenciesRewritten:     {},
	TaskEventDependencyRepairApplied:   {},
	TaskEventRejectionRCARecorded:      {},
	TaskEventRejectionRCAResumed:       {},
	TaskEventReplacementCommitted:      {},
	TaskEventAcceptanceCommitsRemapped: {},
}

// IsStatusTransitionEvent classifies a task-history event name. An event in
// neither list is unclassified: reported as not a transition with
// classified=false, so vocabulary drift surfaces instead of silently moving
// the measured start of a status.
func IsStatusTransitionEvent(name TaskEventName) (transition bool, classified bool) {
	if _, ok := statusTransitionEvents[name]; ok {
		return true, true
	}
	if _, ok := nonStatusTransitionEvents[name]; ok {
		return false, true
	}
	return false, false
}

// TimeInStatus is how long the task has been in its current status, measured
// from the most recent status-transition event in its history, or from Created
// when it has none. Single definition for every surface that reports it.
func TimeInStatus(task *Task, now time.Time) time.Duration {
	if task == nil {
		return 0
	}
	for i := len(task.History) - 1; i >= 0; i-- {
		if transition, _ := IsStatusTransitionEvent(task.History[i].Event); transition {
			return now.Sub(task.History[i].Time)
		}
	}
	return now.Sub(task.Created)
}
