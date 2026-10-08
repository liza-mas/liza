package models

import (
	"fmt"
	"reflect"
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
	TaskEventOutputSet              TaskEventName = "task_output_set"
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
	TaskEventTransitionFailed       TaskEventName = "transition_failed"
	TaskEventTransitionCrashRecov   TaskEventName = "transition_crash_recovery"
	TaskEventReviewVerdictApproved  TaskEventName = "review_verdict_approved"
	TaskEventReviewVerdictRejected  TaskEventName = "review_verdict_rejected"

	TaskEventInitialization            TaskEventName = "initialization"
	TaskEventCreated                   TaskEventName = "created"
	TaskEventClaimed                   TaskEventName = "claimed"
	TaskEventAbandoned                 TaskEventName = "abandoned"
	TaskEventClaimedForIntegrationFix  TaskEventName = "claimed_for_integration_fix"
	TaskEventClaimReleased             TaskEventName = "claim_released"
	TaskEventReclaimedAfterRejection   TaskEventName = "reclaimed_after_rejection"
	TaskEventReassignedAfterRejection  TaskEventName = "reassigned_after_rejection"
	TaskEventWorktreeRecovered         TaskEventName = "worktree_recovered"
	TaskEventDoerClaimReleased         TaskEventName = "doer_claim_released"
	TaskEventReviewClaimReleased       TaskEventName = "review_claim_released"
	TaskEventOrchestratorAssessment    TaskEventName = "orchestrator_assessment"
	TaskEventReplanned                 TaskEventName = "replanned" // rippletide-override: user approved
	TaskEventTransitionCycleBlocked    TaskEventName = "transition_cycle_blocked"
	TaskEventNewAttempt                TaskEventName = "new_attempt"
	TaskEventReviewCommitUpdated       TaskEventName = "review_commit_updated"
	TaskEventDependenciesRewritten     TaskEventName = "dependencies_rewritten"
	TaskEventDependencyRepairApplied   TaskEventName = "dependency_repair_applied"
	TaskEventRejectionRCARecorded      TaskEventName = "rejection_rca_recorded"
	TaskEventRejectionRCAResumed       TaskEventName = "rejection_rca_resumed"
	TaskEventReplacementCommitted      TaskEventName = "replacement_committed"
	TaskEventAcceptanceCommitsRemapped TaskEventName = "acceptance_commits_remapped"
	TaskEventRecoveredFresh            TaskEventName = "task_recovered_fresh"
	TaskEventRecoveryFreshFailed       TaskEventName = "task_recovery_fresh_failed"
	TaskEventPlanCheck                 TaskEventName = "plan_check"
	// TaskEventArchRefRepaired records an operator setting the empty arch_ref of
	// an unstarted task (repair-arch-ref, D-76).
	TaskEventArchRefRepaired TaskEventName = "arch_ref_repaired"
	// TaskEventProviderDeclarationStale records a BLOCKED task's draft output
	// declaration left stale by a provider retirement (ADR-0188).
	TaskEventProviderDeclarationStale TaskEventName = "provider_declaration_stale"
)

// TaskHistoryEntry represents a single event in a task's history
type TaskHistoryEntry struct {
	Time                  time.Time      `yaml:"time"`
	Event                 string         `yaml:"event"`
	Agent                 *string        `yaml:"agent,omitempty"`
	PreviousAssignee      *string        `yaml:"previous_assignee,omitempty"`
	Reason                *string        `yaml:"reason,omitempty"`
	Commit                *string        `yaml:"commit,omitempty"`
	Note                  *string        `yaml:"note,omitempty"`
	SubmissionInputCommit string         `yaml:"submission_input_commit,omitempty" json:"submission_input_commit,omitempty"`
	SubmissionAttempt     int            `yaml:"submission_attempt,omitempty" json:"submission_attempt,omitempty"`
	Extra                 map[string]any `yaml:",inline"`
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

// HumanNoteOrchestratorSeenKey marks a note no orchestrator turn owes: either
// a completed HUMAN_NOTE turn rendered it, or it was recorded as an audit
// entry (delete/recover) rather than a request. Unmarked notes wake an idle
// orchestrator (HUMAN_NOTE).
const HumanNoteOrchestratorSeenKey = "orchestrator_seen_at"

// SeenByOrchestrator reports whether no orchestrator turn owes this note any
// more (see HumanNoteOrchestratorSeenKey).
func (n *HumanNote) SeenByOrchestrator() bool {
	if n == nil || n.Extra == nil {
		return false
	}
	_, seen := n.Extra[HumanNoteOrchestratorSeenKey]
	return seen
}

// MarkSeenByOrchestrator records that no orchestrator turn owes this note:
// a turn rendered or consumed it, or it is an audit entry.
func (n *HumanNote) MarkSeenByOrchestrator(at time.Time) {
	if n.Extra == nil {
		n.Extra = map[string]any{}
	}
	n.Extra[HumanNoteOrchestratorSeenKey] = at.UTC().Format(time.RFC3339)
}

// SpecChange tracks modifications to specification documents
type SpecChange struct {
	Timestamp   time.Time      `yaml:"timestamp"`
	Spec        string         `yaml:"spec"`
	Change      string         `yaml:"change"`
	TriggeredBy string         `yaml:"triggered_by"`
	Extra       map[string]any `yaml:",inline"`
}

// AnomalyTypeReviewerClaimCircuitOpen records a reviewer-claim circuit breaker
// opening after repeated identical pre-claim failures. Its required details
// are listed in anomalyRequiredDetails.
const AnomalyTypeReviewerClaimCircuitOpen = "reviewer_claim_circuit_open"

// AnomalyTypeObligationContentDrifted records that the content an approved
// plan's obligation rests on is no longer what the reviewer saw. It is a
// reviewable event, never a block: refusing here would strand every child of a
// merged plan whose review boundary no command can move (ADR-0133 clause 4).
// Its required details are listed in anomalyRequiredDetails.
//
// reviewed_section and current_section mean different things per change:
//   - repinned, retargeted: the section at the authorizing review commit
//     (what the reviewer saw), and the section at the merge commit.
//   - dropped: the reviewed section, and "" — nothing rests on it now.
//   - stale: the section at the carrier's current pin, which a post-review
//     re-pin may have made unreviewed, and the section at the merge commit,
//     or "" when it no longer resolves. It does not assert a reviewer saw
//     reviewed_section.
const AnomalyTypeObligationContentDrifted = "obligation_content_drifted"

// AnomalyTypePendingMergeStalled records that a reviewer's bounded wake gave up
// retrying a merge it owns. It is not a retry_loop: that type describes a retry
// cluster inside a task's execution, and retry-cluster detection answers it
// with HALT, which repeated stalls of one supervisor loop do not warrant.
// Its required details are listed in anomalyRequiredDetails.
const AnomalyTypePendingMergeStalled = "pending_merge_stalled"

// PendingMergeStallImpact is the impact detail every pending-merge stall
// record carries. Legacy stall records are recognized by it, so it must not
// change while such records may remain unmigrated.
const PendingMergeStallImpact = "an approved task this reviewer owns has not merged; downstream work stays gated until it does"

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
		AnomalyTypeReviewerClaimCircuitOpen, AnomalyTypeObligationContentDrifted,
		AnomalyTypePendingMergeStalled, AnomalyTypeRuntimeInputUnavailable,
	}
	return slices.Contains(validTypes, a.Type)
}

// AnomalyViolation is one anomaly defect: an unknown type or one missing
// required detail. ID is its identity when a state is compared with its
// pre-image (ADR-0165); Err is the operator-facing message.
type AnomalyViolation struct {
	ID  string
	Err error
}

// anomalyRequiredDetails lists, per anomaly type, the details a record cannot
// be acted on without. Types absent here require none.
var anomalyRequiredDetails = map[string][]string{
	"retry_loop":              {"count", "error_pattern"},
	"trade_off":               {"what", "why", "debt_created"},
	"external_blocker":        {"blocker_service"},
	"assumption_violated":     {"assumption", "reality"},
	"system_ambiguity":        {"protocol_section", "question"},
	"provider_audit_degraded": {"provider", "agent_id", "message"},
	"agent_degraded":          {"agent_id", "role", "reason", "last_error"},
	"stale_verdict":           {"attempted_verdict", "current_status"},
	"submit_verdict_failed":   {"verdict", "error"},
	// The breaker keys a quarantine on role, failure class and boundary
	// version, and an operator recovers from the counters and the hint; a
	// record missing any of them cannot be acted on.
	AnomalyTypeReviewerClaimCircuitOpen: {"role", "failure_class", "attempts", "first_failure", "last_failure", "recovery"},
	// A reviewer re-reads the section this names, so the record must locate
	// it and say whose obligations rest on it; without the identities there
	// is nothing to compare against the approval. current_section must be
	// present but may be empty: a dropped section has no current content to
	// read, only the reviewed content the obligation no longer rests on.
	AnomalyTypeObligationContentDrifted: {"path", "heading", "change", "reviewed_section", "current_section", "carriers", "obligations"},
	// An operator finds the stuck merge from the reviewer that owns it and
	// judges persistence from the rounds it spent.
	AnomalyTypePendingMergeStalled: {"agent_id", "role", "rounds"},
	// An operator provisions the named input for the named task; the bound
	// instance re-arms deduplication once a new instance is recorded.
	AnomalyTypeRuntimeInputUnavailable: {"task_id", "input_id", "code", "operation", "bound_instance"},
}

// AnomalyViolations returns every defect of anomalies, in order: one per
// unknown type, and one per missing required detail. Each identity names the
// record by index, which is stable because anomalies are only appended, so
// recording one of several missing details repairs one violation and adds
// none (ADR-0165). statevalidate reports these; Blackboard.Modify refuses the
// ones a transaction adds (ADR-0166).
func AnomalyViolations(anomalies []Anomaly) []AnomalyViolation {
	var violations []AnomalyViolation
	add := func(err error) {
		violations = append(violations, AnomalyViolation{ID: err.Error(), Err: err})
	}
	for i := range anomalies {
		anomaly := &anomalies[i]
		if !anomaly.IsValidType() {
			add(fmt.Errorf("unknown anomaly type '%s' at index %d", anomaly.Type, i))
			continue
		}
		for _, field := range anomalyRequiredDetails[anomaly.Type] {
			if anomalyDetailMissing(anomaly.Details[field]) {
				add(fmt.Errorf("%s anomaly at index %d missing required details (%s)", anomaly.Type, i, field))
			}
		}
	}
	return violations
}

// anomalyDetailMissing reports whether a detail is absent once persisted: nil,
// or a nil pointer, which YAML writes as null. Nil or empty slices and maps are
// written as [] and {} and stay present.
func anomalyDetailMissing(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// MigrateLegacyPendingMergeStall retypes a pending-merge stall record written
// before AnomalyTypePendingMergeStalled existed, when the writer filed it as a
// retry_loop without the count and error_pattern that type requires. Only the
// writer's full signature qualifies; a partial match is someone else's
// malformed record and stays for inspection. Returns true if the record changed.
func (a *Anomaly) MigrateLegacyPendingMergeStall() bool {
	if a.Type != "retry_loop" || a.Details["count"] != nil || a.Details["error_pattern"] != nil {
		return false
	}
	if a.Details["agent_id"] == nil || a.Details["role"] == nil || a.Details["rounds"] == nil {
		return false
	}
	if impact, _ := a.Details["impact"].(string); impact != PendingMergeStallImpact {
		return false
	}
	a.Type = AnomalyTypePendingMergeStalled
	return true
}

// CircuitBreakerResponseType identifies the proportional action selected for a
// circuit-breaker pattern.
type CircuitBreakerResponseType string

const (
	CircuitBreakerResponseWarning    CircuitBreakerResponseType = "WARNING"
	CircuitBreakerResponseCheckpoint CircuitBreakerResponseType = "CHECKPOINT"
	CircuitBreakerResponseHalt       CircuitBreakerResponseType = "HALT"
)

// CircuitBreakerEvidenceClass identifies the lifecycle position of qualifying
// provider-audit evidence relative to the latest resolved response boundary.
type CircuitBreakerEvidenceClass string

const (
	CircuitBreakerEvidenceAcknowledgedHistorical CircuitBreakerEvidenceClass = "ACKNOWLEDGED_HISTORICAL"
	CircuitBreakerEvidenceNew                    CircuitBreakerEvidenceClass = "NEW"
	CircuitBreakerEvidenceContinuing             CircuitBreakerEvidenceClass = "CONTINUING"
)

// CircuitBreakerResponse represents an active proportional response. HALT
// responses also have a CurrentTrigger for backward-compatible hard-trigger
// state; CHECKPOINT responses do not.
type CircuitBreakerResponse struct {
	Timestamp      time.Time                   `yaml:"timestamp"`
	Pattern        string                      `yaml:"pattern"`
	Severity       string                      `yaml:"severity"`
	Response       CircuitBreakerResponseType  `yaml:"response"`
	Classification CircuitBreakerEvidenceClass `yaml:"classification"`
	Explanation    string                      `yaml:"explanation"`
	ReportFile     string                      `yaml:"report_file"`
	Subject        *CircuitBreakerSubject      `yaml:"subject,omitempty"`
	Extra          map[string]any              `yaml:",inline"`
}

// CircuitBreakerSubject binds a response to the one task and blocked episode it
// reported, so resolving it releases only that episode (see
// BlockedRecoveryReleased). Patterns that are not task-scoped leave it nil.
type CircuitBreakerSubject struct {
	TaskID    string    `yaml:"task_id"`
	BlockedAt time.Time `yaml:"blocked_at"`
}

// CircuitBreaker tracks circuit breaker status and history
type CircuitBreaker struct {
	LastCheck       time.Time               `yaml:"last_check"`
	Status          string                  `yaml:"status"` // "OK" or "TRIGGERED"
	CurrentTrigger  *CircuitBreakerTrigger  `yaml:"current_trigger,omitempty"`
	CurrentResponse *CircuitBreakerResponse `yaml:"current_response,omitempty"`
	History         []CircuitBreakerHistory `yaml:"history"`
	Extra           map[string]any          `yaml:",inline"`
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
	Timestamp      time.Time                   `yaml:"timestamp"`
	Pattern        *string                     `yaml:"pattern,omitempty"`
	Severity       *string                     `yaml:"severity,omitempty"`
	Result         string                      `yaml:"result"` // "OK" or "TRIGGERED"
	Response       CircuitBreakerResponseType  `yaml:"response,omitempty"`
	Classification CircuitBreakerEvidenceClass `yaml:"classification,omitempty"`
	Explanation    string                      `yaml:"explanation,omitempty"`
	// SupersededByResponse records an Analyze-authored replacement without
	// acknowledging the superseded boundary. It is currently HALT-only.
	SupersededByResponse CircuitBreakerResponseType `yaml:"superseded_by_response,omitempty"`
	Resolution           *string                    `yaml:"resolution,omitempty"`
	ResolvedAt           *time.Time                 `yaml:"resolved_at,omitempty"`
	Subject              *CircuitBreakerSubject     `yaml:"subject,omitempty"`
	Extra                map[string]any             `yaml:",inline"`
}
