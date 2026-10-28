package statevalidate

import (
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/models"
)

// validateTaskRejectionRCA checks a stored RCA record: its gate-seeded fields,
// its recorded caller fields through the same structural validator both
// request boundaries use, and its disposition. A BLOCKED task whose gate is
// open must additionally carry the typed blocked_reason token, so the reason
// class stays legible to consumers that only read state.
func validateTaskRejectionRCA(v *violations, task *models.Task) {
	validateRejectionRCAGateReason(v, task)
	record := task.RejectionRCA
	if record == nil {
		return
	}
	validateRejectionRCASeededFields(v, task.ID, record)
	// A fingerprint is written only together with the caller fields, so it is
	// the marker that distinguishes a seeded record from a recorded one.
	recorded := record.Fingerprint != ""
	if recorded {
		validateRejectionRCARecordedFields(v, task.ID, record)
	}
	validateRejectionRCADisposition(v, task.ID, record.Disposition, recorded)
}

func validateRejectionRCAGateReason(v *violations, task *models.Task) {
	if task.Status != models.TaskStatusBlocked || !task.RejectionRCAGateOpen() {
		return
	}
	reason := ""
	if task.BlockedReason != nil {
		reason = strings.TrimSpace(*task.BlockedReason)
	}
	if strings.HasPrefix(reason, models.BlockedReasonRejectionRCARequired) {
		return
	}
	v.add(fmt.Errorf("BLOCKED task with an open rejection_rca gate requires a blocked_reason starting with %s: %s",
		models.BlockedReasonRejectionRCARequired, task.ID))
}

func validateRejectionRCASeededFields(v *violations, taskID string, record *models.RejectionRCARecord) {
	if record.SchemaVersion != models.RejectionRCASchemaVersion {
		v.add(fmt.Errorf("task %s rejection_rca schema_version must be %d", taskID, models.RejectionRCASchemaVersion))
	}
	if record.Threshold < 1 {
		v.add(fmt.Errorf("task %s rejection_rca threshold must be at least 1", taskID))
	}
	if record.RejectionCount < record.Threshold {
		v.add(fmt.Errorf("task %s rejection_rca rejection_count must be at least its threshold: %d", taskID, record.Threshold))
	}
	if record.GatedAt.IsZero() {
		v.add(fmt.Errorf("task %s rejection_rca requires gated_at", taskID))
	}
}

func validateRejectionRCARecordedFields(v *violations, taskID string, record *models.RejectionRCARecord) {
	for _, diagnostic := range ValidateRejectionRCARequest(record.Request()) {
		v.add(fmt.Errorf("task %s rejection_rca %s violates: %s", taskID, diagnostic.Field, diagnostic.Constraint))
	}
	if strings.TrimSpace(record.RecordedBy) == "" {
		v.add(fmt.Errorf("task %s rejection_rca requires recorded_by", taskID))
	}
	if record.RecordedAt == nil || record.RecordedAt.IsZero() {
		v.add(fmt.Errorf("task %s rejection_rca requires recorded_at", taskID))
	}
}

func validateRejectionRCADisposition(v *violations, taskID string, disposition *models.RejectionRCADisposition, recorded bool) {
	if disposition == nil {
		return
	}
	if !recorded {
		v.add(fmt.Errorf("task %s rejection_rca disposition requires a recorded RCA", taskID))
	}
	if !models.IsRecoveryPath(disposition.RecoveryPath) {
		v.add(fmt.Errorf("task %s rejection_rca disposition has an unknown recovery_path", taskID))
	} else if disposition.RestoreMode != models.RejectionRCARestoreMode(disposition.RecoveryPath) {
		v.add(fmt.Errorf("task %s rejection_rca disposition restore_mode does not match its recovery_path", taskID))
	}
	if strings.TrimSpace(disposition.Actor) == "" {
		v.add(fmt.Errorf("task %s rejection_rca disposition requires actor", taskID))
	}
	if disposition.DecidedAt.IsZero() {
		v.add(fmt.Errorf("task %s rejection_rca disposition requires decided_at", taskID))
	}
}
