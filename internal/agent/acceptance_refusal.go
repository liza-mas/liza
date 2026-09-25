package agent

import (
	"errors"
	"sync"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// acceptanceRefusalTracker decides when a doer's refused claim escalates to
// BLOCKED (D63, ADR-0160). A content refusal is deterministic at its origin and
// escalates at once. An allocation refusal escalates on the
// ops.AcceptanceAllocationRefusalThreshold-th consecutive identical
// observation; a different observation, any other claim failure for the task,
// or a successful claim of it restarts the count. Counters are in memory: a
// restarted supervisor re-observes up to the threshold.
type acceptanceRefusalTracker struct {
	mu   sync.Mutex
	seen map[string]acceptanceRefusalCount
}

type acceptanceRefusalCount struct {
	key   string
	count int
}

func newAcceptanceRefusalTracker() *acceptanceRefusalTracker {
	return &acceptanceRefusalTracker{seen: make(map[string]acceptanceRefusalCount)}
}

// observe records a claim-stage refusal and reports whether it escalates now.
func (t *acceptanceRefusalTracker) observe(refusal *ops.AcceptanceEvidenceError) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch refusal.Class {
	case ops.AcceptanceFaultContent:
		delete(t.seen, refusal.TaskID)
		return true
	case ops.AcceptanceFaultAllocation:
		key := refusal.Claim.Digest + "\x00" + refusal.Claim.IntegrationCommit
		entry := t.seen[refusal.TaskID]
		if entry.key != key {
			entry = acceptanceRefusalCount{key: key}
		}
		entry.count++
		t.seen[refusal.TaskID] = entry
		return entry.count >= ops.AcceptanceAllocationRefusalThreshold
	default:
		delete(t.seen, refusal.TaskID)
		return false
	}
}

func (t *acceptanceRefusalTracker) forget(taskID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.seen, taskID)
}

// escalateAcceptanceRefusal blocks the refused candidate when the tracker says
// so. A claim failure of any other kind restarts the candidate's count. Failing
// to block is logged and leaves today's retry in place; only a lost
// registration is returned, since the supervisor must stop on it.
func escalateAcceptanceRefusal(projectRoot string, authority models.AgentAuthority, refusals *acceptanceRefusalTracker, taskID string, claimErr error) error {
	var refusal *ops.AcceptanceEvidenceError
	if !errors.As(claimErr, &refusal) || refusal.Claim == nil {
		refusals.forget(taskID)
		return nil
	}
	if !refusals.observe(refusal) {
		return nil
	}
	blocked, err := ops.BlockAcceptanceRefusedTask(projectRoot, authority, refusal)
	if err != nil {
		if ops.IsAgentAuthorityError(err) {
			return err
		}
		GetLogger().Warn("Failed to escalate refused claim", "task_id", refusal.TaskID, "error", err)
		return nil
	}
	if blocked {
		refusals.forget(refusal.TaskID)
		GetLogger().Error("Refused claim escalated to BLOCKED for the orchestrator",
			"task_id", refusal.TaskID, "fault_class", refusal.Class, "allocation", refusal.Claim.AllocationRef)
	}
	return nil
}
