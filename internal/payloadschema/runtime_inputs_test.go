package payloadschema

import (
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
)

func runtimeInputPayload(extra map[string]any) []any {
	input := map[string]any{"id": "w03", "commands": []any{"sh boundary_test.sh"}, "recipe": "project.w03", "consumption": "single_use", "env": []any{"W03_FIXTURE"}}
	for key, value := range extra {
		input[key] = value
	}
	return []any{input}
}

// A key outside the closed set, such as the deferred "binding", is refused on
// both carriers instead of being dropped by the typed decode.
func TestRuntimeInputPayloadRefusesUnknownKeys(t *testing.T) {
	task := validAddTaskPayload()
	task["validation"] = []any{"sh boundary_test.sh"}
	task["runtime_inputs"] = runtimeInputPayload(map[string]any{"binding": "PRIVATE_REJECTED_VALUE"})
	assertTaskSchemaDiagnostic(t, "add-task", task, "/runtime_inputs/0/binding", models.FieldValueClassWrongType)

	output := map[string]any{"output": []any{map[string]any{
		"desc": "d", "done_when": "done", "scope": "s", "spec_ref": "specs/a.md#A",
		"validation":     []any{"sh boundary_test.sh"},
		"runtime_inputs": runtimeInputPayload(map[string]any{"binding": "PRIVATE_REJECTED_VALUE"}),
	}}}
	assertTaskSchemaDiagnostic(t, "set-task-output", output, "/output/0/runtime_inputs/0/binding", models.FieldValueClassWrongType)
}

func TestRuntimeInputPayloadStructuralRules(t *testing.T) {
	task := validAddTaskPayload()
	task["validation"] = []any{"sh other.sh"}
	task["runtime_inputs"] = runtimeInputPayload(nil)
	assertTaskSchemaDiagnostic(t, "add-task", task, "/runtime_inputs", models.FieldValueClassMalformed)

	task["validation"] = []any{"sh boundary_test.sh"}
	if _, diagnostics, err := Validate("add-task", task); err != nil || len(diagnostics) != 0 {
		t.Fatalf("valid declaration refused: %+v %v", diagnostics, err)
	}
}

// A reserved name is refused at every carrier before any state write.
func TestRuntimeInputPayloadRefusesReservedNames(t *testing.T) {
	for _, name := range []string{"PATH", "LD_PRELOAD", brand.EnvName("AGENT_ID")} {
		task := validAddTaskPayload()
		task["validation"] = []any{"sh boundary_test.sh"}
		task["runtime_inputs"] = runtimeInputPayload(map[string]any{"env": []any{name}})
		assertTaskSchemaDiagnostic(t, "add-task", task, "/runtime_inputs", models.FieldValueClassMalformed)

		output := map[string]any{"output": []any{map[string]any{
			"desc": "d", "done_when": "done", "scope": "s", "spec_ref": "specs/a.md#A",
			"validation":     []any{"sh boundary_test.sh"},
			"runtime_inputs": runtimeInputPayload(map[string]any{"env": []any{name}}),
		}}}
		assertTaskSchemaDiagnostic(t, "set-task-output", output, "/output/0/runtime_inputs", models.FieldValueClassMalformed)
	}
}
