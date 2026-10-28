package ops

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// consumedRepair is a stored apply-dependency-repair request after
// normalisation, bound to the digest of the exact request consumed so every
// audit entry of the batch names it.
type consumedRepair struct {
	request *models.RepairRequest
	digest  string
}

// consumeRepairRequest normalises the source's stored request and refuses one
// that is not an apply-dependency-repair request.
func consumeRepairRequest(source *models.Task) (consumedRepair, error) {
	request, err := normalizeRepairRequest(source.RepairRequest, source.ID)
	if err != nil {
		return consumedRepair{}, err
	}
	if request.Operation != applyDependencyRepairOperation {
		return consumedRepair{}, &PreconditionError{Reason: fmt.Sprintf("blocked task %s repair request operation is %q, want %q", source.ID, request.Operation, applyDependencyRepairOperation)}
	}
	consumed, err := json.Marshal(request)
	if err != nil {
		return consumedRepair{}, err
	}
	return consumedRepair{request: request, digest: lifecycleDigest(consumed)}, nil
}

// auditKeys are the audit keys every history entry of the batch shares.
func (c consumedRepair) auditKeys() map[string]any {
	return map[string]any{
		"manual":                 true,
		"operation":              applyDependencyRepairOperation,
		"repair_evidence":        append([]string{}, c.request.Evidence...),
		"repair_validation":      append([]string{}, c.request.Validation...),
		"repair_request_cleared": true,
		"repair_request_digest":  c.digest,
	}
}

// batchAudit is the common audit extra of each rewritten task's entry.
func (c consumedRepair) batchAudit(sourceTaskID string) map[string]any {
	extra := c.auditKeys()
	extra["repair_source_task"] = sourceTaskID
	return extra
}

// auditSource records the repair on the source when the batch left its own
// dependency list unchanged; a rewritten source already carries the entry.
func (c consumedRepair) auditSource(source *models.Task, updates []AppliedDependencyUpdate, agentID, reason string, now time.Time) {
	if slices.ContainsFunc(updates, func(update AppliedDependencyUpdate) bool { return update.TaskID == source.ID }) {
		return
	}
	affectedTaskIDs := make([]string, len(updates))
	for i, update := range updates {
		affectedTaskIDs[i] = update.TaskID
	}
	extra := c.auditKeys()
	extra["affected_task_ids"] = affectedTaskIDs
	note := "applied dependency repair without changing the source task dependencies"
	source.History = append(source.History, models.TaskHistoryEntry{
		Time:   now,
		Event:  models.TaskEventDependencyRepairApplied,
		Agent:  &agentID,
		Reason: &reason,
		Note:   &note,
		Extra:  extra,
	})
}
