package analysis

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// Each case sits one anomaly below a pattern's trigger count; the counts are
// literals, independent of the named thresholds.
func TestPatternsStayQuietOneBelowThreshold(t *testing.T) {
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	anomaly := func(typ models.AnomalyType, task string, details map[string]any) models.Anomaly {
		return models.Anomaly{Timestamp: now, Task: task, Reporter: "coder-1", Type: typ, Details: details}
	}
	for _, tc := range []struct {
		name      string
		anomalies []models.Anomaly
	}{
		{"two retry loops sharing a pattern", []models.Anomaly{
			anomaly(models.AnomalyTypeRetryLoop, "task-1", map[string]any{"error_pattern": "same"}),
			anomaly(models.AnomalyTypeRetryLoop, "task-2", map[string]any{"error_pattern": "same"}),
		}},
		{"two debt-creating trade-offs", []models.Anomaly{
			anomaly(models.AnomalyTypeTradeOff, "task-1", map[string]any{"what": "a", "debt_created": true}),
			anomaly(models.AnomalyTypeTradeOff, "task-2", map[string]any{"what": "b", "debt_created": true}),
		}},
		{"one violated assumption", []models.Anomaly{
			anomaly(models.AnomalyTypeAssumptionViolated, "task-1", map[string]any{"assumption": "x"}),
		}},
		{"one spec ambiguity", []models.Anomaly{
			anomaly(models.AnomalyTypeSpecAmbiguity, "task-1", map[string]any{"spec_ref": "specs/a.md"}),
		}},
		{"one workaround", []models.Anomaly{
			anomaly(models.AnomalyTypeWorkaround, "task-1", map[string]any{"root_cause": "x"}),
		}},
		{"one external blocker", []models.Anomaly{
			anomaly(models.AnomalyTypeExternalBlocker, "task-1", map[string]any{"blocker_service": "svc"}),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if result := DetectPatterns(tc.anomalies, nil); result.Triggered {
				t.Fatalf("triggered %s below its threshold", result.Pattern)
			}
		})
	}
}

func TestPlanningReviewChurnNeedsFourRejections(t *testing.T) {
	state := &models.State{Tasks: []models.Task{{
		ID:                "plan-three",
		Type:              models.TaskTypePlanning,
		Status:            models.TaskStatusMerged,
		ReviewCyclesTotal: 3,
	}}}
	if result, _, _ := DetectUnacknowledgedPatterns(state, nil); result.Pattern == "planning_review_churn" {
		t.Fatal("three rejection cycles triggered planning review churn")
	}
}
