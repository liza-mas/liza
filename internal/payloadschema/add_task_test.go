package payloadschema

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func validAddTaskPayload() map[string]any {
	return map[string]any{"id": "new-task", "role_pair": "custom-pair", "desc": "description", "spec": "specs/task.md", "done": "done", "scope": "scope", "priority": 1}
}

func assertTaskSchemaDiagnostic(t *testing.T, operation string, payload any, field, class string) {
	t.Helper()
	version, diagnostics, err := Validate(operation, payload)
	if err != nil || version != 1 || len(diagnostics) == 0 {
		t.Fatalf("version=%d diagnostics=%+v err=%v", version, diagnostics, err)
	}
	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.SchemaVersion != 1 || !strings.HasPrefix(diagnostic.Field, "/") || diagnostic.Constraint == "" || !models.IsFieldDiagnosticSafeAction(diagnostic.SafeAction) {
			t.Fatalf("invalid diagnostic: %+v", diagnostic)
		}
		if diagnostic.Field == field && diagnostic.ValueClass == class {
			found = true
		}
	}
	encoded, err := json.Marshal(diagnostics)
	if err != nil || strings.Contains(string(encoded), "PRIVATE_REJECTED_VALUE") {
		t.Fatalf("unsafe diagnostics: %s, %v", encoded, err)
	}
	if !found {
		t.Fatalf("missing %s/%s diagnostic: %+v", field, class, diagnostics)
	}
}

func TestAddTaskSchema(t *testing.T) {
	for _, field := range []string{"id", "role_pair", "desc", "spec", "done", "scope", "priority"} {
		t.Run("required/"+field, func(t *testing.T) {
			payload := validAddTaskPayload()
			delete(payload, field)
			assertTaskSchemaDiagnostic(t, "add-task", payload, "/"+field, models.FieldValueClassMissing)
		})
	}
	for _, tc := range []struct {
		field string
		value any
		class string
	}{
		{"desc", 7, models.FieldValueClassWrongType},
		{"priority", "PRIVATE_REJECTED_VALUE", models.FieldValueClassWrongType},
		{"priority", 1.5, models.FieldValueClassWrongType},
		{"priority", 0, models.FieldValueClassOutOfRange},
		{"type", "PRIVATE_REJECTED_VALUE", models.FieldValueClassUnknownEnum},
		{"id", "../PRIVATE_REJECTED_VALUE", models.FieldValueClassMalformed},
		{"spec", "a.md\nb.md", models.FieldValueClassMalformed},
		{"destructive_db", "PRIVATE_REJECTED_VALUE", models.FieldValueClassWrongType},
		{"depends", "PRIVATE_REJECTED_VALUE", models.FieldValueClassWrongType},
		{"validation_prerequisites", []any{map[string]any{"command": "PRIVATE_REJECTED_VALUE"}}, models.FieldValueClassMalformed},
	} {
		t.Run(tc.field+"/"+tc.class, func(t *testing.T) {
			payload := validAddTaskPayload()
			payload[tc.field] = tc.value
			assertTaskSchemaDiagnostic(t, "add-task", payload, "/"+tc.field, tc.class)
		})
	}
	for _, payload := range []any{validAddTaskPayload(), map[string]any{"id": "t", "role_pair": "custom", "desc": "d", "spec": "s", "done": "d", "scope": "s", "priority": 1, "depends": []string{}}} {
		if _, diagnostics, err := Validate("add-task", payload); err != nil || len(diagnostics) != 0 {
			t.Fatalf("valid payload rejected: %+v %v", diagnostics, err)
		}
	}
	t.Run("cardinality/destructive validation", func(t *testing.T) {
		payload := validAddTaskPayload()
		payload["destructive_db"] = true
		payload["validation"] = []string{}
		assertTaskSchemaDiagnostic(t, "add-task", payload, "/validation", models.FieldValueClassMalformed)
	})
	assertTaskSchemaDiagnostic(t, "add-task", nil, "/", models.FieldValueClassNull)
}

func TestPayloadSchemaRegistration(t *testing.T) {
	for _, operation := range []string{"add-task", "replace-task"} {
		found := false
		for _, descriptor := range List() {
			if descriptor.Operation == operation && descriptor.Version == 1 {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing version-1 schema for %s", operation)
		}
	}
}
