package commands

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"gopkg.in/yaml.v3"
)

func TestInspectPlanningChangesUsesCurrentLogicalCreationRecords(t *testing.T) {
	state := &models.State{Tasks: []models.Task{
		{ID: "architecture", Type: models.TaskTypeArchitecture},
		{ID: "change", Created: time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC),
			PlanningChange: models.NewPlanningChange(models.PlanningChangeCorrection, "consumer-gap", "architecture")},
	}}
	// Persisted sprint counters may predate commissioning. Inspection must
	// still show the new work without requiring a metrics-update mutation.
	state.Sprint.Metrics.PlanningChanges = models.PlanningChangeMetrics{UnknownAttribution: 99}
	for _, format := range []string{"json", "yaml", "value"} {
		output, err := inspectMetrics(state, inspectMetricsOptions{Format: format, ProjectRoot: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		text := output.(string)
		if format == "value" {
			if !strings.Contains(text, "2026-10-10 correction trigger=consumer-gap architecture=true: 1") || !strings.Contains(text, "Unknown attribution: 0") {
				t.Fatalf("missing creation-day metrics: %s", text)
			}
			continue
		}
		var decoded metricsInfo
		if format == "json" {
			err = json.Unmarshal([]byte(text), &decoded)
		} else {
			err = yaml.Unmarshal([]byte(text), &decoded)
		}
		if err != nil || len(decoded.PlanningChanges.Daily) != 1 || decoded.PlanningChanges.Daily[0].Count != 1 || decoded.PlanningChanges.UnknownAttribution != 0 {
			t.Fatalf("logical creation counts missing: %#v, %v", decoded.PlanningChanges, err)
		}
	}
}
