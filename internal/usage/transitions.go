// Package usage attributes provider token usage to durable task outcomes.
//
// This file owns the structural "useful state transition" rule and the
// outcome/failure classification over durable task history. Everything here
// is read-only over models: no history writes and no report aggregation.
package usage

import (
	"sort"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// usefulEvents are the task-history events that change task content, the
// ownership boundary, an accepted artifact/commit or the terminal outcome.
var usefulEvents = map[string]struct{}{
	models.TaskEventCreated:                 {},
	models.TaskEventClaimed:                 {},
	models.TaskEventPreExecutionCheckpoint:  {},
	models.TaskEventOutputSet:               {},
	models.TaskEventSubmittedForReview:      {},
	models.TaskEventReviewCommitUpdated:     {},
	models.TaskEventApproved:                {},
	models.TaskEventRejected:                {},
	models.TaskEventReviewVerdictApproved:   {},
	models.TaskEventReviewVerdictRejected:   {},
	models.TaskEventMerged:                  {},
	models.TaskEventSuperseded:              {},
	models.TaskEventAbandoned:               {},
	models.TaskEventBlocked:                 {},
	models.TaskEventUnblocked:               {},
	models.TaskEventIntegrationFailed:       {},
	models.TaskEventTransitionExecuted:      {},
	models.TaskEventDependenciesRewritten:   {},
	models.TaskEventDependencyRepairApplied: {},
	models.TaskEventReplacementCommitted:    {},
	models.TaskEventRejectionRCARecorded:    {},
	models.TaskEventRejectionRCAResumed:     {},
	models.TaskEventHandoffInitiated:        {},
}

// notUsefulEvents are polling, release, re-entry and recovery bookkeeping.
var notUsefulEvents = map[string]struct{}{
	models.TaskEventOrchestratorAssessment:    {},
	models.TaskEventClaimReleased:             {},
	models.TaskEventDoerClaimReleased:         {},
	models.TaskEventReviewClaimReleased:       {},
	models.TaskEventReclaimedAfterRejection:   {},
	models.TaskEventReassignedAfterRejection:  {},
	models.TaskEventNewAttempt:                {},
	models.TaskEventOwnedTaskResumed:          {},
	models.TaskEventHandoffResumed:            {},
	models.TaskEventWorktreeRecovered:         {},
	models.TaskEventClaimedForIntegrationFix:  {},
	models.TaskEventTransitionCycleBlocked:    {},
	models.TaskEventTransitionCrashRecov:      {},
	models.TaskEventPlanning:                  {},
	models.TaskEventInitialization:            {},
	models.TaskEventReplanned:                 {},
	models.TaskEventAcceptanceCommitsRemapped: {},
}

// IsUsefulEvent classifies a task-history event name. An event in neither
// documented list is unclassified: reported as not useful with classified=false
// so vocabulary drift surfaces instead of silently changing the denominator.
func IsUsefulEvent(name string) (useful bool, classified bool) {
	if _, ok := usefulEvents[name]; ok {
		return true, true
	}
	if _, ok := notUsefulEvents[name]; ok {
		return false, true
	}
	return false, false
}

// Evidence names the durable history entry that decided a reading, so the
// reading is reproducible at any as_of. Revision is the task's lifecycle
// revision at the time of the state read (0 when the task has no lifecycle).
type Evidence struct {
	Event    string    `json:"event"`
	Time     time.Time `json:"time"`
	Revision uint64    `json:"revision"`
}

// Transition is a useful state transition of a task. TaskID names the task
// whose history carries the event: the task itself, or a direct dependency
// whose merged event counts for the dependent.
type Transition struct {
	Evidence
	TaskID string `json:"task_id"`
}

// LastUsefulTransition returns the latest useful transition of task at or
// before asOf (a zero asOf means no upper bound): the latest useful event in
// the task's own history, or a direct dependency's merged event when that is
// later. ok is false when no such point exists.
func LastUsefulTransition(task *models.Task, deps []*models.Task, asOf time.Time) (Transition, bool) {
	if task == nil {
		return Transition{}, false
	}
	var best Transition
	found := false
	consider := func(candidate Transition) {
		if !found || candidate.Time.After(best.Time) {
			best, found = candidate, true
		}
	}
	for _, h := range task.History {
		if useful, _ := IsUsefulEvent(h.Event); !useful || !within(h.Time, asOf) {
			continue
		}
		consider(Transition{Evidence: evidenceOf(task, h), TaskID: task.ID})
	}
	for _, dep := range deps {
		if dep == nil {
			continue
		}
		for _, h := range dep.History {
			if h.Event != models.TaskEventMerged || !within(h.Time, asOf) {
				continue
			}
			consider(Transition{Evidence: evidenceOf(dep, h), TaskID: dep.ID})
		}
	}
	return best, found
}

// OutcomeClass is the outcome a task reached, derived from durable history.
type OutcomeClass string

// Outcome classes. blocked is an outcome for attribution although the task
// status is not IsTerminal; unattributed covers records with no task.
const (
	OutcomeMerged       OutcomeClass = "merged"
	OutcomeSuperseded   OutcomeClass = "superseded"
	OutcomeAbandoned    OutcomeClass = "abandoned"
	OutcomeBlocked      OutcomeClass = "blocked"
	OutcomeActive       OutcomeClass = "active"
	OutcomeUnattributed OutcomeClass = "unattributed"
)

// OutcomeClasses lists every outcome class in report order.
var OutcomeClasses = []OutcomeClass{
	OutcomeMerged, OutcomeSuperseded, OutcomeAbandoned,
	OutcomeBlocked, OutcomeActive, OutcomeUnattributed,
}

// decidingEvents map a history event to the outcome it decides. unblocked
// clears blocked back to active.
var decidingEvents = map[string]OutcomeClass{
	models.TaskEventMerged:     OutcomeMerged,
	models.TaskEventSuperseded: OutcomeSuperseded,
	models.TaskEventAbandoned:  OutcomeAbandoned,
	models.TaskEventBlocked:    OutcomeBlocked,
	models.TaskEventUnblocked:  OutcomeActive,
}

// ClassifyOutcome derives the task's outcome at asOf (zero means no upper
// bound) from the latest deciding history event at or before that point. A
// task with no deciding event is active, evidenced by its latest history entry
// at or before asOf. A nil task or empty task id is unattributed.
//
// Task history is append-only, so the last deciding entry in slice order is
// the latest one.
func ClassifyOutcome(task *models.Task, asOf time.Time) (OutcomeClass, Evidence) {
	if task == nil || task.ID == "" {
		return OutcomeUnattributed, Evidence{}
	}
	outcome := OutcomeActive
	var evidence Evidence
	decided := false
	for _, h := range task.History {
		if !within(h.Time, asOf) {
			continue
		}
		if class, ok := decidingEvents[h.Event]; ok {
			outcome, evidence, decided = class, evidenceOf(task, h), true
			continue
		}
		if !decided {
			evidence = evidenceOf(task, h)
		}
	}
	return outcome, evidence
}

// FailureCategory classifies tokens spent after the last useful transition.
type FailureCategory string

// Failure categories, in first-match order.
const (
	FailureRejectionRCA        FailureCategory = "rejection_rca"
	FailureCircuitOpen         FailureCategory = "circuit_open"
	FailureDuplicateAssessment FailureCategory = "duplicate_assessment"
	FailureLifecycleRetry      FailureCategory = "lifecycle_retry"
	FailureRetryTail           FailureCategory = "retry_tail"
	FailureUnknownProvenance   FailureCategory = "unknown_provenance"
)

// FailureCategories lists every failure category in first-match order.
var FailureCategories = []FailureCategory{
	FailureRejectionRCA, FailureCircuitOpen, FailureDuplicateAssessment,
	FailureLifecycleRetry, FailureRetryTail, FailureUnknownProvenance,
}

// lifecycleRetryEvents are the attempt, release and reclaim entries that
// classify a post-transition tail as lifecycle_retry.
var lifecycleRetryEvents = map[string]struct{}{
	models.TaskEventNewAttempt:               {},
	models.TaskEventClaimReleased:            {},
	models.TaskEventDoerClaimReleased:        {},
	models.TaskEventReviewClaimReleased:      {},
	models.TaskEventReclaimedAfterRejection:  {},
	models.TaskEventReassignedAfterRejection: {},
}

// ClassifyFailure decides the failure category of usage spent at or after
// since, first match wins: a rejection_rca_recorded entry, a
// reviewer_claim_circuit_open anomaly naming the task, orchestrator_assessment
// entries, attempt/release/reclaim entries, then retry_tail for authoritative
// records and unknown_provenance otherwise.
func ClassifyFailure(task *models.Task, anomalies []models.Anomaly, since time.Time, authoritative bool) FailureCategory {
	tail := scanFailureTail(task, since)
	switch {
	case tail.rca:
		return FailureRejectionRCA
	// A task-free reading must not match an anomaly whose own task is empty.
	case task != nil && task.ID != "" && hasCircuitOpen(anomalies, task.ID, since):
		return FailureCircuitOpen
	case tail.assessment:
		return FailureDuplicateAssessment
	case tail.retry:
		return FailureLifecycleRetry
	case authoritative:
		return FailureRetryTail
	default:
		return FailureUnknownProvenance
	}
}

// failureTail records which categories the task's own history evidences at or
// after the point. A nil task evidences none.
type failureTail struct {
	rca        bool
	assessment bool
	retry      bool
}

func scanFailureTail(task *models.Task, since time.Time) failureTail {
	var tail failureTail
	if task == nil {
		return tail
	}
	for _, h := range task.History {
		if h.Time.Before(since) {
			continue
		}
		switch h.Event {
		case models.TaskEventRejectionRCARecorded:
			tail.rca = true
		case models.TaskEventOrchestratorAssessment:
			tail.assessment = true
		default:
			if _, ok := lifecycleRetryEvents[h.Event]; ok {
				tail.retry = true
			}
		}
	}
	return tail
}

func hasCircuitOpen(anomalies []models.Anomaly, taskID string, since time.Time) bool {
	for _, a := range anomalies {
		if a.Type == models.AnomalyTypeReviewerClaimCircuitOpen && a.Task == taskID && !a.Timestamp.Before(since) {
			return true
		}
	}
	return false
}

// UnclassifiedEvents returns the sorted, deduplicated event names found in the
// tasks' histories that belong to neither documented list.
func UnclassifiedEvents(tasks []models.Task) []string {
	seen := map[string]struct{}{}
	for i := range tasks {
		for _, h := range tasks[i].History {
			if _, classified := IsUsefulEvent(h.Event); !classified {
				seen[h.Event] = struct{}{}
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// within reports whether at is at or before asOf; a zero asOf is unbounded.
func within(at, asOf time.Time) bool {
	return asOf.IsZero() || !at.After(asOf)
}

func evidenceOf(task *models.Task, h models.TaskHistoryEntry) Evidence {
	e := Evidence{Event: h.Event, Time: h.Time}
	if task.Lifecycle != nil {
		e.Revision = task.Lifecycle.Revision
	}
	return e
}
