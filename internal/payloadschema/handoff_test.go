package payloadschema

import (
	"slices"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

// validHandoffPayload is the minimal accepted canonical object: the optional
// structured fields are absent, as they are for every handoff the CLI sends.
func validHandoffPayload() map[string]any {
	return map[string]any{"summary": "context at 90%", "next_action": "continue from prepareSubmitForReview"}
}

func TestHandoffSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload any
		want    []models.FieldDiagnostic
	}{
		{
			name:    "required fields only",
			payload: validHandoffPayload(),
		},
		{
			name: "every optional field supplied",
			payload: map[string]any{
				"summary": "s", "next_action": "n", "hypothesis": "the lock is held twice",
				"succeeded": []any{"schema"}, "failed": []any{"parity"},
				"key_files": []any{"internal/ops/handoff.go"}, "dead_ends": []any{"retry loop"},
			},
		},
		{
			// A Go caller builds the canonical object with typed slices; the
			// preflight decodes the same object from JSON as []any. Both are
			// the same payload and must reach the same verdict.
			name: "typed string lists validate like decoded lists",
			payload: map[string]any{
				"summary": "s", "next_action": "n",
				"succeeded": []string{"schema"}, "key_files": []string{"internal/ops/handoff.go"},
			},
		},
		{
			name:    "empty optional lists are accepted",
			payload: map[string]any{"summary": "s", "next_action": "n", "succeeded": []any{}, "dead_ends": []string{}},
		},
		{
			name:    "null optional fields are accepted as absent",
			payload: map[string]any{"summary": "s", "next_action": "n", "hypothesis": nil, "failed": nil},
		},
		{
			name:    "empty hypothesis is accepted",
			payload: map[string]any{"summary": "s", "next_action": "n", "hypothesis": ""},
		},
		{
			name:    "missing required fields name both",
			payload: map[string]any{},
			want: []models.FieldDiagnostic{
				correctInput("/summary", "is required", models.FieldValueClassMissing),
				correctInput("/next_action", "is required", models.FieldValueClassMissing),
			},
		},
		{
			name:    "empty summary",
			payload: map[string]any{"summary": "", "next_action": "n"},
			want:    []models.FieldDiagnostic{correctInput("/summary", "must not be empty", models.FieldValueClassMissing)},
		},
		{
			name:    "empty next action",
			payload: map[string]any{"summary": "s", "next_action": ""},
			want:    []models.FieldDiagnostic{correctInput("/next_action", "must not be empty", models.FieldValueClassMissing)},
		},
		{
			name:    "null summary",
			payload: map[string]any{"summary": nil, "next_action": "n"},
			want:    []models.FieldDiagnostic{correctInput("/summary", "must not be null", models.FieldValueClassNull)},
		},
		{
			name:    "non-string next action",
			payload: map[string]any{"summary": "s", "next_action": 7},
			want:    []models.FieldDiagnostic{correctInput("/next_action", "must be a string", models.FieldValueClassWrongType)},
		},
		{
			name:    "non-string hypothesis",
			payload: map[string]any{"summary": "s", "next_action": "n", "hypothesis": []any{"a"}},
			want:    []models.FieldDiagnostic{correctInput("/hypothesis", "must be a string", models.FieldValueClassWrongType)},
		},
		{
			// An empty entry carries no handoff content and would be written
			// into the HandoffEvent as a blank line for the next agent.
			name:    "empty entry in succeeded",
			payload: map[string]any{"summary": "s", "next_action": "n", "succeeded": []any{"schema", ""}},
			want:    []models.FieldDiagnostic{correctInput("/succeeded/1", "must not be empty", models.FieldValueClassMissing)},
		},
		{
			name:    "empty entry in failed",
			payload: map[string]any{"summary": "s", "next_action": "n", "failed": []string{""}},
			want:    []models.FieldDiagnostic{correctInput("/failed/0", "must not be empty", models.FieldValueClassMissing)},
		},
		{
			name:    "empty entry in key_files",
			payload: map[string]any{"summary": "s", "next_action": "n", "key_files": []any{""}},
			want:    []models.FieldDiagnostic{correctInput("/key_files/0", "must not be empty", models.FieldValueClassMissing)},
		},
		{
			name:    "empty entry in dead_ends",
			payload: map[string]any{"summary": "s", "next_action": "n", "dead_ends": []any{"tried X", ""}},
			want:    []models.FieldDiagnostic{correctInput("/dead_ends/1", "must not be empty", models.FieldValueClassMissing)},
		},
		{
			name:    "non-string entry in an optional list",
			payload: map[string]any{"summary": "s", "next_action": "n", "succeeded": []any{3}},
			want:    []models.FieldDiagnostic{correctInput("/succeeded/0", "must be a string", models.FieldValueClassWrongType)},
		},
		{
			name:    "null entry in an optional list",
			payload: map[string]any{"summary": "s", "next_action": "n", "succeeded": []any{nil}},
			want:    []models.FieldDiagnostic{correctInput("/succeeded/0", "must be a string", models.FieldValueClassNull)},
		},
		{
			name:    "optional list is not a list",
			payload: map[string]any{"summary": "s", "next_action": "n", "key_files": "internal/ops/handoff.go"},
			want:    []models.FieldDiagnostic{correctInput("/key_files", "must be a list of non-empty strings", models.FieldValueClassWrongType)},
		},
		{
			name:    "every rejected field is reported once, in payload order",
			payload: map[string]any{"summary": "", "next_action": nil, "succeeded": []any{""}, "dead_ends": []any{""}},
			want: []models.FieldDiagnostic{
				correctInput("/summary", "must not be empty", models.FieldValueClassMissing),
				correctInput("/next_action", "must not be null", models.FieldValueClassNull),
				correctInput("/succeeded/0", "must not be empty", models.FieldValueClassMissing),
				correctInput("/dead_ends/0", "must not be empty", models.FieldValueClassMissing),
			},
		},
		{
			name:    "payload is not an object",
			payload: "context at 90%",
			want:    []models.FieldDiagnostic{correctInput(payloadRootField, "must be a JSON object", models.FieldValueClassWrongType)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			version, diagnostics, err := Validate(handoffOperation, tt.payload)
			if err != nil {
				t.Fatalf("Validate(%q) returned %v, want the registered schema", handoffOperation, err)
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

func TestHandoffSchemaIsDiscoverable(t *testing.T) {
	t.Parallel()

	schema, registered := Lookup(handoffOperation)
	if !registered {
		t.Fatalf("Lookup(%q) found no schema; the operation is not preflightable", handoffOperation)
	}
	if schema.Version != 1 {
		t.Errorf("version = %d, want 1", schema.Version)
	}

	if !slices.Contains(List(), Descriptor{Operation: handoffOperation, Version: 1}) {
		t.Errorf("List() = %+v, want a {%s, 1} row", List(), handoffOperation)
	}
}
