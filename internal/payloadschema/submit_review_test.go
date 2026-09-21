package payloadschema

import (
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

// requireDiagnostics compares a schema verdict field by field. The exact field
// path, constraint and value class are the contract an agent corrects against,
// so an assertion on the count alone would not prove it.
func requireDiagnostics(t *testing.T, got, want []models.FieldDiagnostic) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("diagnostics = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Field != want[i].Field || got[i].Constraint != want[i].Constraint ||
			got[i].ValueClass != want[i].ValueClass || got[i].SafeAction != want[i].SafeAction {
			t.Errorf("diagnostics[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func correctInput(field, constraint, valueClass string) models.FieldDiagnostic {
	return models.FieldDiagnostic{
		Field:      field,
		Constraint: constraint,
		ValueClass: valueClass,
		SafeAction: models.FieldDiagnosticCorrectInput,
	}
}

func TestSubmitReviewSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload any
		want    []models.FieldDiagnostic
	}{
		{
			name:    "abbreviated ref is valid input",
			payload: map[string]any{"commit_ref": "abc123"},
		},
		{
			name:    "symbolic ref is valid input",
			payload: map[string]any{"commit_ref": "HEAD~1"},
		},
		{
			name:    "40-character hexadecimal object ID is accepted",
			payload: map[string]any{"commit_ref": strings.Repeat("a1b2", 10)},
		},
		{
			name:    "64-character hexadecimal object ID is accepted",
			payload: map[string]any{"commit_ref": strings.Repeat("0f", 32)},
		},
		{
			name:    "uppercase hexadecimal object ID is accepted",
			payload: map[string]any{"commit_ref": strings.Repeat("AB", 20)},
		},
		{
			name:    "unknown keys are ignored",
			payload: map[string]any{"commit_ref": "abc123", "task_id": "task-1"},
		},
		{
			name:    "missing commit ref names the field",
			payload: map[string]any{},
			want:    []models.FieldDiagnostic{correctInput("/commit_ref", "is required", models.FieldValueClassMissing)},
		},
		{
			name:    "null commit ref is not a ref",
			payload: map[string]any{"commit_ref": nil},
			want:    []models.FieldDiagnostic{correctInput("/commit_ref", "must not be null", models.FieldValueClassNull)},
		},
		{
			name:    "non-string commit ref",
			payload: map[string]any{"commit_ref": 40},
			want:    []models.FieldDiagnostic{correctInput("/commit_ref", "must be a string", models.FieldValueClassWrongType)},
		},
		{
			name:    "empty commit ref",
			payload: map[string]any{"commit_ref": ""},
			want:    []models.FieldDiagnostic{correctInput("/commit_ref", "must not be empty", models.FieldValueClassMissing)},
		},
		{
			// A ref of object-ID length that is not hexadecimal cannot be an
			// object ID, so it is rejected here instead of at the Git boundary.
			name:    "40 characters containing a non-hexadecimal character",
			payload: map[string]any{"commit_ref": strings.Repeat("a", 39) + "z"},
			want:    []models.FieldDiagnostic{correctInput("/commit_ref", commitRefHexConstraint, models.FieldValueClassMalformed)},
		},
		{
			name:    "64 characters containing a non-hexadecimal character",
			payload: map[string]any{"commit_ref": strings.Repeat("0", 63) + "g"},
			want:    []models.FieldDiagnostic{correctInput("/commit_ref", commitRefHexConstraint, models.FieldValueClassMalformed)},
		},
		{
			name:    "payload is not an object",
			payload: []any{"abc123"},
			want:    []models.FieldDiagnostic{correctInput(payloadRootField, "must be a JSON object", models.FieldValueClassWrongType)},
		},
		{
			name:    "payload is null",
			payload: nil,
			want:    []models.FieldDiagnostic{correctInput(payloadRootField, "must be a JSON object", models.FieldValueClassNull)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			version, diagnostics, err := Validate(submitForReviewOperation, tt.payload)
			if err != nil {
				t.Fatalf("Validate(%q) returned %v, want the registered schema", submitForReviewOperation, err)
			}
			if version != 1 {
				t.Errorf("schema version = %d, want 1", version)
			}
			requireDiagnostics(t, diagnostics, tt.want)
			for _, diagnostic := range diagnostics {
				if diagnostic.SchemaVersion != version {
					t.Errorf("diagnostic schema_version = %d, want %d", diagnostic.SchemaVersion, version)
				}
			}
		})
	}
}

func TestSubmitReviewSchemaIsDiscoverable(t *testing.T) {
	t.Parallel()

	schema, registered := Lookup(submitForReviewOperation)
	if !registered {
		t.Fatalf("Lookup(%q) found no schema; the operation is not preflightable", submitForReviewOperation)
	}
	if schema.Version != 1 {
		t.Errorf("version = %d, want 1", schema.Version)
	}

	if !slices.Contains(List(), Descriptor{Operation: submitForReviewOperation, Version: 1}) {
		t.Errorf("List() = %+v, want a {%s, 1} row", List(), submitForReviewOperation)
	}
}
