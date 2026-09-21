package payloadschema

import (
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statehygiene"
)

// validVerdictPayload is a REJECTED verdict every field of which the shared
// validator accepts, so each case below changes exactly one field.
func validVerdictPayload() map[string]any {
	return SubmitVerdictPayload("task-1", "REJECTED", "the fix misses the boundary case", "reviewer-1", "standard", strings.Repeat("ab", 20))
}

func TestSubmitVerdictSchemaDiagnostics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(payload map[string]any)
		payload any
		want    []models.FieldDiagnostic
	}{
		{
			name:   "valid payload yields nil",
			mutate: func(map[string]any) {},
		},
		{
			name:   "approved verdict without a reason is valid",
			mutate: func(p map[string]any) { p["verdict"] = "APPROVED"; delete(p, "reason") },
		},
		{
			name:   "absent impact and review commit are valid",
			mutate: func(p map[string]any) { delete(p, "impact"); delete(p, "review_commit") },
		},
		{
			name:   "missing task_id",
			mutate: func(p map[string]any) { delete(p, "task_id") },
			want:   []models.FieldDiagnostic{correctInput("/task_id", "a non-empty value is required", models.FieldValueClassMissing)},
		},
		{
			name:   "lowercase verdict",
			mutate: func(p map[string]any) { p["verdict"] = "rejected" },
			want:   []models.FieldDiagnostic{correctInput("/verdict", "must be APPROVED or REJECTED", models.FieldValueClassUnknownEnum)},
		},
		{
			name:   "REJECTED with a blank reason",
			mutate: func(p map[string]any) { p["reason"] = "   " },
			want:   []models.FieldDiagnostic{correctInput("/reason", "a non-empty value is required", models.FieldValueClassMissing)},
		},
		{
			name:   "reason one byte over the state-text limit",
			mutate: func(p map[string]any) { p["reason"] = strings.Repeat("r", statehygiene.MaxStateTextBytes+1) },
			want:   []models.FieldDiagnostic{correctInput("/reason", "must be at most 4096 bytes", models.FieldValueClassOversized)},
		},
		{
			name:   "unknown impact",
			mutate: func(p map[string]any) { p["impact"] = "cosmetic" },
			want:   []models.FieldDiagnostic{correctInput("/impact", "must be standard, significant or architecture", models.FieldValueClassUnknownEnum)},
		},
		{
			name:   "non-hex review_commit",
			mutate: func(p map[string]any) { p["review_commit"] = strings.Repeat("a", 39) + "z" },
			want:   []models.FieldDiagnostic{correctInput("/review_commit", "must be the full immutable commit SHA reviewed", models.FieldValueClassMalformed)},
		},
		{
			name:   "non-string member is reported before delegation",
			mutate: func(p map[string]any) { p["reason"] = 7 },
			want:   []models.FieldDiagnostic{correctInput("/reason", "must be a string", models.FieldValueClassWrongType)},
		},
		{
			name:    "payload is not an object",
			payload: []any{"task-1"},
			want:    []models.FieldDiagnostic{correctInput(payloadRootField, "must be a JSON object", models.FieldValueClassWrongType)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			payload := tt.payload
			if tt.mutate != nil {
				object := validVerdictPayload()
				tt.mutate(object)
				payload = object
			}

			version, diagnostics, err := Validate(SubmitVerdictOperation, payload)
			if err != nil {
				t.Fatalf("Validate(%q) returned %v, want the registered schema", SubmitVerdictOperation, err)
			}
			if version != 1 {
				t.Errorf("schema version = %d, want 1", version)
			}
			requireDiagnostics(t, diagnostics, tt.want)
			for _, diagnostic := range diagnostics {
				if diagnostic.SchemaVersion != 1 {
					t.Errorf("diagnostic schema_version = %d, want 1", diagnostic.SchemaVersion)
				}
			}
		})
	}
}

func TestSubmitVerdictSchemaRegistered(t *testing.T) {
	t.Parallel()

	schema, registered := Lookup(SubmitVerdictOperation)
	if !registered {
		t.Fatalf("Lookup(%q) found no schema; the operation is not preflightable", SubmitVerdictOperation)
	}
	if schema.Version != 1 {
		t.Errorf("version = %d, want 1", schema.Version)
	}
	if !slices.Contains(List(), Descriptor{Operation: SubmitVerdictOperation, Version: 1}) {
		t.Errorf("List() = %+v, want a {%s, 1} row", List(), SubmitVerdictOperation)
	}
}
