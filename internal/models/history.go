package models

import (
	"slices"
	"time"
)

// GoalStatus represents the state of a goal
type GoalStatus string

const (
	GoalStatusInProgress GoalStatus = "IN_PROGRESS"
	GoalStatusCompleted  GoalStatus = "COMPLETED"
	GoalStatusAborted    GoalStatus = "ABORTED"
)

// IsValid checks if the goal status is valid
func (gs GoalStatus) IsValid() bool {
	return gs == GoalStatusInProgress || gs == GoalStatusCompleted || gs == GoalStatusAborted
}

// Goal represents the high-level goal spanning one or more sprints
type Goal struct {
	ID          string  `yaml:"id"`
	Description string  `yaml:"description"`
	SpecRef     string  `yaml:"spec_ref"`
	EntryPoint  string  `yaml:"entry_point,omitempty"`
	BaseCommit  *string `yaml:"base_commit,omitempty"`

	Integration *IntegrationLifecycle `yaml:"integration,omitempty"`

	Created          time.Time          `yaml:"created"`
	Status           GoalStatus         `yaml:"status"`
	AlignmentHistory []AlignmentHistory `yaml:"alignment_history"`
	Extra            map[string]any     `yaml:",inline"`
}

// AlignmentHistory tracks goal alignment events
type AlignmentHistory struct {
	Timestamp time.Time      `yaml:"timestamp"`
	Event     string         `yaml:"event"`
	Summary   string         `yaml:"summary"`
	Extra     map[string]any `yaml:",inline"`
}

// TaskEventName identifies a task-history event.
type TaskEventName = string

// Task-event constants used in history entries across ops, commands, and agent packages.
const (
	TaskEventPlanning               TaskEventName = "planning"
	TaskEventPreExecutionCheckpoint TaskEventName = "pre_execution_checkpoint"
	TaskEventSubmittedForReview     TaskEventName = "submitted_for_review"
	TaskEventApproved               TaskEventName = "approved"
	TaskEventRejected               TaskEventName = "rejected"
	TaskEventBlocked                TaskEventName = "blocked"
	TaskEventUnblocked              TaskEventName = "unblocked"
	TaskEventMerged                 TaskEventName = "merged"
	TaskEventSuperseded             TaskEventName = "superseded"
	TaskEventIntegrationFailed      TaskEventName = "integration_failed"
	TaskEventHandoffInitiated       TaskEventName = "handoff_initiated"
	TaskEventHandoffResumed         TaskEventName = "handoff_resumed"
	TaskEventOwnedTaskResumed       TaskEventName = "owned_task_resumed"
	TaskEventTransitionExecuted     TaskEventName = "transition_executed"
	TaskEventTransitionCrashRecov   TaskEventName = "transition_crash_recovery"
	TaskEventReviewVerdictApproved  TaskEventName = "review_verdict_approved"
	TaskEventReviewVerdictRejected  TaskEventName = "review_verdict_rejected"

	TaskEventInitialization           TaskEventName = "initialization"
	TaskEventCreated                  TaskEventName = "created"
	TaskEventClaimed                  TaskEventName = "claimed"
	TaskEventAbandoned                TaskEventName = "abandoned"
	TaskEventClaimedForIntegrationFix TaskEventName = "claimed_for_integration_fix"
	TaskEventClaimReleased            TaskEventName = "claim_released"
	TaskEventReclaimedAfterRejection  TaskEventName = "reclaimed_after_rejection"
	TaskEventReassignedAfterRejection TaskEventName = "reassigned_after_rejection"
	TaskEventWorktreeRecovered        TaskEventName = "worktree_recovered"
	TaskEventDoerClaimReleased        TaskEventName = "doer_claim_released"
	TaskEventReviewClaimReleased      TaskEventName = "review_claim_released"
	TaskEventOrchestratorAssessment   TaskEventName = "orchestrator_assessment"
	TaskEventReplanned                TaskEventName = "replanned" // rippletide-override: user approved
	TaskEventTransitionCycleBlocked   TaskEventName = "transition_cycle_blocked"
	TaskEventNewAttempt               TaskEventName = "new_attempt"
	TaskEventReviewCommitUpdated      TaskEventName = "review_commit_updated"
	TaskEventDependenciesRewritten    TaskEventName = "dependencies_rewritten"
)

// TaskHistoryEntry represents a single event in a task's history
type TaskHistoryEntry struct {
	Time             time.Time      `yaml:"time"`
	Event            string         `yaml:"event"`
	Agent            *string        `yaml:"agent,omitempty"`
	PreviousAssignee *string        `yaml:"previous_assignee,omitempty"`
	Reason           *string        `yaml:"reason,omitempty"`
	Commit           *string        `yaml:"commit,omitempty"`
	Note             *string        `yaml:"note,omitempty"`
	Extra            map[string]any `yaml:",inline"`
}

// Discovery represents a finding by an agent during work
type Discovery struct {
	ID              string         `yaml:"id"`
	By              string         `yaml:"by"`
	During          string         `yaml:"during"`
	Description     string         `yaml:"description"`
	Severity        string         `yaml:"severity"`
	Urgency         string         `yaml:"urgency"`
	Recommendation  string         `yaml:"recommendation"`
	Created         time.Time      `yaml:"created"`
	ConvertedToTask *string        `yaml:"converted_to_task,omitempty"`
	Extra           map[string]any `yaml:",inline"`
}

// IsValidSeverity checks if the discovery severity is valid
func (d *Discovery) IsValidSeverity() bool {
	return d.Severity == "critical" || d.Severity == "high" ||
		d.Severity == "medium" || d.Severity == "low"
}

// IsValidUrgency checks if the discovery urgency is valid
func (d *Discovery) IsValidUrgency() bool {
	return d.Urgency == "immediate" || d.Urgency == "deferred"
}

// HandoffTrigger identifies what caused a handoff event.
type HandoffTrigger string

const (
	HandoffTriggerContextExhaustion HandoffTrigger = "context_exhaustion"
	HandoffTriggerSubmission        HandoffTrigger = "submission"
	HandoffTriggerCompletion        HandoffTrigger = "completion"
)

// HandoffEvent captures structured context at task handoff points.
// Events are append-only and form an ordered audit trail on each task.
type HandoffEvent struct {
	Timestamp  time.Time      `yaml:"timestamp"`
	Agent      string         `yaml:"agent"`
	Trigger    HandoffTrigger `yaml:"trigger"`
	Succeeded  []string       `yaml:"succeeded,omitempty"`
	Failed     []string       `yaml:"failed,omitempty"`
	Hypothesis string         `yaml:"hypothesis,omitempty"`
	NextStep   string         `yaml:"next_step,omitempty"`
	KeyFiles   []string       `yaml:"key_files,omitempty"`
	DeadEnds   []string       `yaml:"dead_ends,omitempty"`
}

// HumanNote represents a note from a human to agents
type HumanNote struct {
	Timestamp time.Time      `yaml:"timestamp"`
	Message   string         `yaml:"message"`
	For       string         `yaml:"for"`
	Extra     map[string]any `yaml:",inline"`
}

// SpecChange tracks modifications to specification documents
type SpecChange struct {
	Timestamp   time.Time      `yaml:"timestamp"`
	Spec        string         `yaml:"spec"`
	Change      string         `yaml:"change"`
	TriggeredBy string         `yaml:"triggered_by"`
	Extra       map[string]any `yaml:",inline"`
}

// Anomaly represents an execution anomaly that may trigger circuit breaker
type Anomaly struct {
	Timestamp time.Time      `yaml:"timestamp"`
	Task      string         `yaml:"task"`
	Reporter  string         `yaml:"reporter"`
	Type      string         `yaml:"type"`
	Details   map[string]any `yaml:"details"`
	Extra     map[string]any `yaml:",inline"`
}

// IsValidType checks if the anomaly type is valid
func (a *Anomaly) IsValidType() bool {
	validTypes := []string{
		"retry_loop", "trade_off", "spec_ambiguity", "external_blocker",
		"assumption_violated", "scope_deviation", "workaround", "debt_created",
		"spec_changed", "hypothesis_exhaustion", "spec_gap", "review_budget_exhausted",
		"review_exhaustion", "reviewer_loop", "stale_verdict", "system_ambiguity",
		"provider_audit_degraded", "agent_degraded", "submit_verdict_failed",
	}
	return slices.Contains(validTypes, a.Type)
}

// CircuitBreaker tracks circuit breaker status and history
type CircuitBreaker struct {
	LastCheck      time.Time               `yaml:"last_check"`
	Status         string                  `yaml:"status"` // "OK" or "TRIGGERED"
	CurrentTrigger *CircuitBreakerTrigger  `yaml:"current_trigger,omitempty"`
	History        []CircuitBreakerHistory `yaml:"history"`
	Extra          map[string]any          `yaml:",inline"`
}

// IsValidStatus checks if the circuit breaker status is valid
func (cb *CircuitBreaker) IsValidStatus() bool {
	return cb.Status == "OK" || cb.Status == "TRIGGERED"
}

// CircuitBreakerTrigger represents an active circuit breaker trigger
type CircuitBreakerTrigger struct {
	Timestamp  time.Time      `yaml:"timestamp"`
	Pattern    string         `yaml:"pattern"`
	Severity   string         `yaml:"severity"`
	ReportFile string         `yaml:"report_file"`
	Extra      map[string]any `yaml:",inline"`
}

// CircuitBreakerHistory tracks historical circuit breaker checks
type CircuitBreakerHistory struct {
	Timestamp  time.Time      `yaml:"timestamp"`
	Pattern    *string        `yaml:"pattern,omitempty"`
	Severity   *string        `yaml:"severity,omitempty"`
	Result     string         `yaml:"result"` // "OK" or "TRIGGERED"
	Resolution *string        `yaml:"resolution,omitempty"`
	ResolvedAt *time.Time     `yaml:"resolved_at,omitempty"`
	Extra      map[string]any `yaml:",inline"`
}
