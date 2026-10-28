package ops

import (
	"reflect"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func c6OrchestratorFixture(t *testing.T) *c6Fixture {
	t.Helper()
	now := time.Now().UTC()
	return newC6Fixture(t, func(s *models.State) {
		s.Agents["code-reviewer-1"] = c6LiveAgent("code-reviewer", now)
	})
}

func TestC6RecordProviderAuditDegradedAppendsTheAnomalyFields(t *testing.T) {
	f := c6OrchestratorFixture(t)
	started := time.Now().UTC().Add(-time.Second)

	err := RecordProviderAuditDegraded(f.bb, c6Authority("code-reviewer-1"), "task-1", "code-reviewer-1", "codex", "failed to record rollout items: thread not found")
	if err != nil {
		t.Fatalf("RecordProviderAuditDegraded: %v", err)
	}

	anomalies := f.read(t).Anomalies
	if len(anomalies) != 1 {
		t.Fatalf("anomalies = %+v, want one", anomalies)
	}
	got := anomalies[0]
	want := map[string]any{
		"provider": "codex",
		"agent_id": "code-reviewer-1",
		"message":  "failed to record rollout items: thread not found",
		"impact":   "provider transcript or rollout persistence may be incomplete",
	}
	if got.Type != models.AnomalyTypeProviderAuditDegraded || got.Task != "task-1" || got.Reporter != "code-reviewer-1" ||
		got.Timestamp.Before(started) || !reflect.DeepEqual(got.Details, want) {
		t.Fatalf("anomaly = %+v, want type provider_audit_degraded with details %v", got, want)
	}
}

func TestC6RecordPendingMergeStallAppendsTheAnomalyFields(t *testing.T) {
	f := c6OrchestratorFixture(t)

	if err := RecordPendingMergeStall(f.bb, c6Authority("code-reviewer-1"), "code-reviewer-1", "code-reviewer", 4); err != nil {
		t.Fatalf("RecordPendingMergeStall: %v", err)
	}

	anomalies := f.read(t).Anomalies
	if len(anomalies) != 1 {
		t.Fatalf("anomalies = %+v, want one", anomalies)
	}
	got := anomalies[0]
	want := map[string]any{
		"agent_id": "code-reviewer-1",
		"role":     "code-reviewer",
		"rounds":   4,
		"impact":   models.PendingMergeStallImpact,
	}
	if got.Type != models.AnomalyTypePendingMergeStalled || got.Task != "" || got.Reporter != "code-reviewer-1" || !reflect.DeepEqual(got.Details, want) {
		t.Fatalf("anomaly = %+v, want type pending_merge_stalled with details %v", got, want)
	}
}

func TestC6SupervisorAnomaliesRefuseAStaleGeneration(t *testing.T) {
	f := c6OrchestratorFixture(t)
	before := f.bytes(t)
	stale := models.AgentAuthority{ID: "code-reviewer-1", Generation: "replaced"}

	requireAuthorityError(t, RecordProviderAuditDegraded(f.bb, stale, "task-1", "code-reviewer-1", "codex", "m"), "code-reviewer-1")
	requireAuthorityError(t, RecordPendingMergeStall(f.bb, stale, "code-reviewer-1", "code-reviewer", 1), "code-reviewer-1")
	f.requireUnchanged(t, before, "anomaly records by a replaced generation")
}
