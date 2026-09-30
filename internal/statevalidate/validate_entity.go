package statevalidate

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
)

// validateDiscovered checks that discovered items have a valid urgency value
// (either "deferred" or "immediate", or empty). Prevents typos and invalid
// urgency levels from entering the backlog where they would be silently ignored
// by the scheduler.
func validateDiscovered(v *violations, state *models.State) {
	for i, disc := range state.Discovered {
		if disc.Urgency != "" && disc.Urgency != "deferred" && disc.Urgency != "immediate" {
			v.add(fmt.Errorf("discovered item %d has invalid urgency '%s' (must be 'deferred' or 'immediate')", i, disc.Urgency))
		}
	}
}

// validateAnomalies checks that each anomaly has a valid type and that
// type-specific required detail fields are present (e.g. retry_loop requires
// count and error_pattern; trade_off requires what, why, debt_created).
// Prevents agents from logging anomalies that cannot be analysed by the
// circuit breaker or human reviewers.
//
// Each missing detail is its own violation, so recording one of several
// missing details is a repair rather than a new violation.
func validateAnomalies(v *violations, state *models.State) {
	for i, anomaly := range state.Anomalies {
		// Check type is valid
		if !anomaly.IsValidType() {
			v.add(fmt.Errorf("unknown anomaly type '%s' at index %d", anomaly.Type, i))
			continue
		}

		// Type-specific detail validation
		switch anomaly.Type {
		case "retry_loop":
			requireAnomalyDetails(v, i, anomaly, "count", "error_pattern")
		case "trade_off":
			requireAnomalyDetails(v, i, anomaly, "what", "why", "debt_created")
		case "external_blocker":
			requireAnomalyDetails(v, i, anomaly, "blocker_service")
		case "assumption_violated":
			requireAnomalyDetails(v, i, anomaly, "assumption", "reality")
		case "system_ambiguity":
			requireAnomalyDetails(v, i, anomaly, "protocol_section", "question")
		case "provider_audit_degraded":
			requireAnomalyDetails(v, i, anomaly, "provider", "agent_id", "message")
		case "agent_degraded":
			requireAnomalyDetails(v, i, anomaly, "agent_id", "role", "reason", "last_error")
		case "stale_verdict":
			requireAnomalyDetails(v, i, anomaly, "attempted_verdict", "current_status")
		case "submit_verdict_failed":
			requireAnomalyDetails(v, i, anomaly, "verdict", "error")
		case models.AnomalyTypeReviewerClaimCircuitOpen:
			// The breaker keys a quarantine on role, failure class and boundary
			// version, and an operator recovers from the counters and the hint;
			// a record missing any of them cannot be acted on.
			requireAnomalyDetails(v, i, anomaly, "role", "failure_class", "attempts", "first_failure", "last_failure", "recovery")
		case models.AnomalyTypeObligationContentDrifted:
			// A reviewer re-reads the section this names, so the record must
			// locate it and say whose obligations rest on it; without the
			// identities there is nothing to compare against the approval.
			// current_section must be present but may be empty: a dropped
			// section has no current content to read, only the reviewed
			// content the obligation no longer rests on.
			requireAnomalyDetails(v, i, anomaly, "path", "heading", "change",
				"reviewed_section", "current_section", "carriers", "obligations")
		case models.AnomalyTypePendingMergeStalled:
			// An operator finds the stuck merge from the reviewer that owns it
			// and judges persistence from the rounds it spent.
			requireAnomalyDetails(v, i, anomaly, "agent_id", "role", "rounds")
		}
	}
}

// requireAnomalyDetails reports each required detail field absent from
// anomaly as its own violation, in the order given, naming what the writer
// failed to record.
func requireAnomalyDetails(v *violations, index int, anomaly models.Anomaly, required ...string) {
	for _, field := range required {
		if anomaly.Details[field] == nil {
			v.add(fmt.Errorf("%s anomaly at index %d missing required details (%s)", anomaly.Type, index, field))
		}
	}
}

// validateHandoffEvents checks that:
// (1) each HandoffEvent has non-zero Timestamp, non-empty Agent, and valid Trigger
// (2) tasks in post-submission states have at least one event with trigger submission
// (3) tasks in MERGED state have at least one event with trigger completion
func validateHandoffEvents(v *violations, state *models.State) {
	for _, task := range state.Tasks {
		for j, event := range task.HandoffEvents {
			if event.Timestamp.IsZero() {
				v.add(fmt.Errorf("task %s: handoff_events[%d] has zero timestamp", task.ID, j))
			}
			if event.Agent == "" {
				v.add(fmt.Errorf("task %s: handoff_events[%d] has empty agent", task.ID, j))
			}
			if !isValidHandoffTrigger(event.Trigger) {
				v.add(fmt.Errorf("task %s: handoff_events[%d] has invalid trigger %q", task.ID, j, event.Trigger))
			}
		}

		if isPostSubmissionStatus(task.Status) {
			requireHandoffTrigger(v, task, models.HandoffTriggerSubmission)
		}
		if task.Status == models.TaskStatusMerged {
			requireHandoffTrigger(v, task, models.HandoffTriggerCompletion)
		}
	}
}

// requireHandoffTrigger reports a task lacking an event with trigger. The
// message names the status for the operator; the identity leaves it out, so a
// task moving between statuses that both require the event keeps one
// violation rather than trading an old one for a new one.
func requireHandoffTrigger(v *violations, task models.Task, trigger models.HandoffTrigger) {
	if hasHandoffTrigger(task.HandoffEvents, trigger) {
		return
	}
	v.addID(fmt.Sprintf("task %s has no handoff event with trigger %q", task.ID, trigger),
		fmt.Errorf("task %s in status %s has no handoff event with trigger %q", task.ID, task.Status, trigger))
}

func isValidHandoffTrigger(trigger models.HandoffTrigger) bool {
	switch trigger {
	case models.HandoffTriggerContextExhaustion,
		models.HandoffTriggerSubmission,
		models.HandoffTriggerCompletion:
		return true
	}
	return false
}

func hasHandoffTrigger(events []models.HandoffEvent, trigger models.HandoffTrigger) bool {
	for _, e := range events {
		if e.Trigger == trigger {
			return true
		}
	}
	return false
}

// isPostSubmissionStatus returns true if the task status implies the task has
// been through the submission flow at least once.
func isPostSubmissionStatus(status models.TaskStatus) bool {
	if status.IsReadyForReviewStatus() {
		return true
	}
	switch status {
	case models.TaskStatusReviewing,
		models.TaskStatusRejected, models.TaskStatusApproved,
		models.TaskStatusMerged, models.TaskStatusIntegrationFailed,
		models.TaskStatusPartiallyApproved, models.TaskStatusReviewingCode2,
		models.TaskStatusCodingPlanToReview, models.TaskStatusReviewingCodingPlan,
		models.TaskStatusCodingPlanApproved, models.TaskStatusCodingPlanRejected:
		return true
	}
	return false
}
