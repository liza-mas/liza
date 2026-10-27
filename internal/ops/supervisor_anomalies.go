package ops

import (
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

// providerAuditDegradedImpact is the impact detail of a provider audit
// degradation record.
const providerAuditDegradedImpact = "provider transcript or rollout persistence may be incomplete"

// RecordProviderAuditDegraded appends a provider_audit_degraded anomaly in the
// generation-fenced transaction. reporter is the supervisor's agent ID and is
// recorded both as the reporter and as the agent_id detail. Detection, the
// alert and the warning policy stay with the caller.
func RecordProviderAuditDegraded(bb *db.Blackboard, authority models.AgentAuthority, taskID, reporter, provider, message string) error {
	return ModifyWithAgentAuthority(bb, authority, func(state *models.State) error {
		state.Anomalies = append(state.Anomalies, models.Anomaly{
			Timestamp: time.Now().UTC(),
			Task:      taskID,
			Reporter:  reporter,
			Type:      models.AnomalyTypeProviderAuditDegraded,
			Details: map[string]any{
				"provider": provider,
				"agent_id": reporter,
				"message":  message,
				"impact":   providerAuditDegradedImpact,
			},
		})
		return nil
	})
}

// RecordPendingMergeStall appends the durable evidence that a reviewer's
// automatic merge convergence gave up, so the stall is visible without reading
// supervisor logs. reporter is the supervisor's agent ID, recorded as reporter
// and as the agent_id detail.
func RecordPendingMergeStall(bb *db.Blackboard, authority models.AgentAuthority, reporter, role string, rounds int) error {
	return ModifyWithAgentAuthority(bb, authority, func(state *models.State) error {
		state.Anomalies = append(state.Anomalies, models.Anomaly{
			Timestamp: time.Now().UTC(),
			Reporter:  reporter,
			Type:      models.AnomalyTypePendingMergeStalled,
			Details: map[string]any{
				"agent_id": reporter,
				"role":     role,
				"rounds":   rounds,
				"impact":   models.PendingMergeStallImpact,
			},
		})
		return nil
	})
}
