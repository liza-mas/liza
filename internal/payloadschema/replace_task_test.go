package payloadschema

import (
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func validReplaceTaskPayload() map[string]any {
	return map[string]any{"source_task_id": "source", "reason": "correction", "replacement": validAddTaskPayload(), "consumers": []any{}}
}

func TestReplaceTaskSchema(t *testing.T) {
	for _, field := range []string{"source_task_id", "reason", "replacement"} {
		t.Run("required/"+field, func(t *testing.T) {
			payload := validReplaceTaskPayload()
			delete(payload, field)
			assertTaskSchemaDiagnostic(t, "replace-task", payload, "/"+field, models.FieldValueClassMissing)
		})
	}
	for _, tc := range []struct {
		name, field, class string
		change             func(map[string]any)
	}{
		{"type", "/replacement/desc", models.FieldValueClassWrongType, func(p map[string]any) { p["replacement"].(map[string]any)["desc"] = 5 }},
		{"enum", "/replacement/type", models.FieldValueClassUnknownEnum, func(p map[string]any) { p["replacement"].(map[string]any)["type"] = "PRIVATE_REJECTED_VALUE" }},
		{"consumer type", "/consumers", models.FieldValueClassWrongType, func(p map[string]any) { p["consumers"] = "PRIVATE_REJECTED_VALUE" }},
		{"duplicate consumers", "/consumers/1/task_id", models.FieldValueClassConflict, func(p map[string]any) {
			p["consumers"] = []any{map[string]any{"task_id": "consumer", "expected_depends_on": []string{}, "desired_depends_on": []string{}}, map[string]any{"task_id": "consumer", "expected_depends_on": []string{}, "desired_depends_on": []string{}}}
		}},
		{"explicit lists", "/consumers/0/expected_depends_on", models.FieldValueClassNull, func(p map[string]any) { p["consumers"] = []any{map[string]any{"task_id": "consumer"}} }},
		{"base-only", "/preserved_base/worktree", models.FieldValueClassMissing, func(p map[string]any) { p["preserved_base"] = map[string]any{"base_commit": "abc"} }},
		{"self replacement", "/replacement/id", models.FieldValueClassConflict, func(p map[string]any) { p["replacement"].(map[string]any)["id"] = "source" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := validReplaceTaskPayload()
			tc.change(p)
			assertTaskSchemaDiagnostic(t, "replace-task", p, tc.field, tc.class)
		})
	}
	for _, consumers := range []any{nil, []any{}} {
		payload := validReplaceTaskPayload()
		payload["consumers"] = consumers
		if _, diagnostics, err := Validate("replace-task", payload); err != nil || len(diagnostics) != 0 {
			t.Fatalf("empty consumers rejected: %+v %v", diagnostics, err)
		}
	}
}
