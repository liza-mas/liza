package statevalidate

import (
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/models"
)

// validateDiscovered checks that discovered items have a valid urgency value
// (either "deferred" or "immediate", or empty). Prevents typos and invalid
// urgency levels from entering the backlog where they would be silently ignored
// by the scheduler.
func validateDiscovered(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
	for i, disc := range state.Discovered {
		if disc.Urgency != "" && disc.Urgency != "deferred" && disc.Urgency != "immediate" {
			return fmt.Errorf("discovered item %d has invalid urgency '%s' (must be 'deferred' or 'immediate')", i, disc.Urgency)
		}
	}
	return nil
}

// validateAnomalies checks that each anomaly has a valid type and that
// type-specific required detail fields are present (e.g. retry_loop requires
// count and error_pattern; trade_off requires what, why, debt_created).
// Prevents agents from logging anomalies that cannot be analysed by the
// circuit breaker or human reviewers.
func validateAnomalies(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
	for i, anomaly := range state.Anomalies {
		// Check type is valid
		if !anomaly.IsValidType() {
			return fmt.Errorf("unknown anomaly type '%s' at index %d", anomaly.Type, i)
		}

		// Type-specific detail validation
		switch anomaly.Type {
		case "retry_loop":
			if anomaly.Details["count"] == nil || anomaly.Details["error_pattern"] == nil {
				return fmt.Errorf("retry_loop anomaly at index %d missing required details (count, error_pattern)", i)
			}
		case "trade_off":
			if anomaly.Details["what"] == nil || anomaly.Details["why"] == nil || anomaly.Details["debt_created"] == nil {
				return fmt.Errorf("trade_off anomaly at index %d missing required details (what, why, debt_created)", i)
			}
		case "external_blocker":
			if anomaly.Details["blocker_service"] == nil {
				return fmt.Errorf("external_blocker anomaly at index %d missing required details (blocker_service)", i)
			}
		case "assumption_violated":
			if anomaly.Details["assumption"] == nil || anomaly.Details["reality"] == nil {
				return fmt.Errorf("assumption_violated anomaly at index %d missing required details (assumption, reality)", i)
			}
		case "system_ambiguity":
			if anomaly.Details["protocol_section"] == nil || anomaly.Details["question"] == nil {
				return fmt.Errorf("system_ambiguity anomaly at index %d missing required details (protocol_section, question)", i)
			}
		case "provider_audit_degraded":
			if anomaly.Details["provider"] == nil || anomaly.Details["agent_id"] == nil || anomaly.Details["message"] == nil {
				return fmt.Errorf("provider_audit_degraded anomaly at index %d missing required details (provider, agent_id, message)", i)
			}
		case "agent_degraded":
			if anomaly.Details["agent_id"] == nil || anomaly.Details["role"] == nil || anomaly.Details["reason"] == nil || anomaly.Details["last_error"] == nil {
				return fmt.Errorf("agent_degraded anomaly at index %d missing required details (agent_id, role, reason, last_error)", i)
			}
		case "stale_verdict":
			if anomaly.Details["attempted_verdict"] == nil || anomaly.Details["current_status"] == nil {
				return fmt.Errorf("stale_verdict anomaly at index %d missing required details (attempted_verdict, current_status)", i)
			}
		case "submit_verdict_failed":
			if anomaly.Details["verdict"] == nil || anomaly.Details["error"] == nil {
				return fmt.Errorf("submit_verdict_failed anomaly at index %d missing required details (verdict, error)", i)
			}
		case models.AnomalyTypeObligationContentDrifted:
			// A reviewer re-reads the section this names, so the record must
			// locate it and say whose obligations rest on it; without the
			// identities there is nothing to compare against the approval.
			// current_section must be present but may be empty: a dropped
			// section has no current content to read, only the reviewed
			// content the obligation no longer rests on.
			missing := missingAnomalyDetails(anomaly, "path", "heading", "change",
				"reviewed_section", "current_section", "carriers", "obligations")
			if len(missing) > 0 {
				return fmt.Errorf("%s anomaly at index %d missing required details (%s)",
					models.AnomalyTypeObligationContentDrifted, i, strings.Join(missing, ", "))
			}
		}
	}
	return nil
}

// validateHandoffEvents checks that:
// (1) each HandoffEvent has non-zero Timestamp, non-empty Agent, and valid Trigger
// (2) tasks in post-submission states have at least one event with trigger submission
// (3) tasks in MERGED state have at least one event with trigger completion
func validateHandoffEvents(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
	for _, task := range state.Tasks {
		for j, event := range task.HandoffEvents {
			if event.Timestamp.IsZero() {
				return fmt.Errorf("task %s: handoff_events[%d] has zero timestamp", task.ID, j)
			}
			if event.Agent == "" {
				return fmt.Errorf("task %s: handoff_events[%d] has empty agent", task.ID, j)
			}
			if !isValidHandoffTrigger(event.Trigger) {
				return fmt.Errorf("task %s: handoff_events[%d] has invalid trigger %q", task.ID, j, event.Trigger)
			}
		}

		if isPostSubmissionStatus(task.Status) {
			if !hasHandoffTrigger(task.HandoffEvents, models.HandoffTriggerSubmission) {
				return fmt.Errorf("task %s in status %s has no handoff event with trigger %q",
					task.ID, task.Status, models.HandoffTriggerSubmission)
			}
		}

		if task.Status == models.TaskStatusMerged {
			if !hasHandoffTrigger(task.HandoffEvents, models.HandoffTriggerCompletion) {
				return fmt.Errorf("task %s in status %s has no handoff event with trigger %q",
					task.ID, task.Status, models.HandoffTriggerCompletion)
			}
		}
	}
	return nil
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

// missingAnomalyDetails lists the required detail fields absent from anomaly, in
// the order given, so the error names what the writer failed to record.
func missingAnomalyDetails(anomaly models.Anomaly, required ...string) []string {
	var missing []string
	for _, field := range required {
		if anomaly.Details[field] == nil {
			missing = append(missing, field)
		}
	}
	return missing
}
