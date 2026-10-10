package payloadschema

import (
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestCorrectivePlanningMetadataPayload(t *testing.T) {
	for _, tc := range []struct {
		change any
		field  string
		class  string
	}{
		{map[string]any{"kind": "other", "trigger": "gap", "original_task_id": "original"}, "kind", models.FieldValueClassUnknownEnum},
		{map[string]any{"kind": "correction", "original_task_id": "original"}, "trigger", models.FieldValueClassMissing},
		{map[string]any{"kind": "replan", "trigger": "gap"}, "original_task_id", models.FieldValueClassMissing},
		{map[string]any{"kind": "correction", "trigger": "gap", "original_task_id": "new-task"}, "original_task_id", models.FieldValueClassMalformed},
	} {
		payload := validAddTaskPayload()
		payload["planning_change"] = tc.change
		assertTaskSchemaDiagnostic(t, "add-task", payload, "/planning_change/"+tc.field, tc.class)
	}
	payload := validAddTaskPayload()
	payload["planning_change"] = models.NewPlanningChange(models.PlanningChangeCorrection, "consumer-gap", "original")
	if _, diagnostics, err := Validate("add-task", payload); err != nil || len(diagnostics) != 0 {
		t.Fatalf("valid corrective task refused: %v %v", diagnostics, err)
	}
}
