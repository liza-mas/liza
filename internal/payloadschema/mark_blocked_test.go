package payloadschema

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

// diag builds the diagnostic the mark-blocked schema is expected to produce.
// Every rejection of this schema is correctable by the caller, and the registry
// stamps version 1 on each entry.
func diag(field, constraint, valueClass string) models.FieldDiagnostic {
	return models.FieldDiagnostic{
		SchemaVersion: 1,
		Field:         field,
		Constraint:    constraint,
		ValueClass:    valueClass,
		SafeAction:    models.FieldDiagnosticCorrectInput,
	}
}

func validMarkBlockedPayload() MarkBlockedPayload {
	return MarkBlockedPayload{
		TaskID:    "task-1",
		Reason:    "blocked on an orchestrator-only repair",
		Questions: []string{"Can the orchestrator apply the repair?"},
	}
}

func commandRepairRequest() *models.RepairRequest {
	return &models.RepairRequest{
		Operation:  "add-task",
		Target:     "architecture-2",
		Command:    "add-task --id architecture-2 --json",
		Evidence:   []string{"command=add-task --id architecture-2 --json exit_code=1 stderr=command requires role type [orchestrator]"},
		Validation: []string{"validate --json"},
	}
}

func dependencyRepairRequest() *models.RepairRequest {
	return &models.RepairRequest{
		Operation: models.RepairOperationApplyDependencyRepair,
		Target:    "task-1",
		DependencyUpdates: []models.DependencyUpdate{{
			TaskID:            "consumer-1",
			ExpectedDependsOn: []string{},
			DesiredDependsOn:  []string{"producer-1"},
		}},
		Evidence:   []string{"error=dependency repair requires orchestrator authority"},
		Validation: []string{"validate --json"},
	}
}

func withRepair(request *models.RepairRequest) MarkBlockedPayload {
	payload := validMarkBlockedPayload()
	payload.RepairRequest = request
	return payload
}

func TestMarkBlockedSchema(t *testing.T) {
	t.Parallel()

	const structuredEvidence = `repair requests require structured failure evidence; valid examples: "command=add-task --json exit_code=1 stderr=command requires role type [orchestrator]", "command=provider-call exit_code=1 error=unavailable", or "error=provider session thread not found"`

	tests := []struct {
		name    string
		payload func() MarkBlockedPayload
		want    []models.FieldDiagnostic
	}{
		{
			name:    "accepts a payload with no repair request",
			payload: validMarkBlockedPayload,
		},
		{
			name: "rejects zero questions",
			payload: func() MarkBlockedPayload {
				payload := validMarkBlockedPayload()
				payload.Questions = nil
				return payload
			},
			want: []models.FieldDiagnostic{diag("/questions", "at least 1 question is required", models.FieldValueClassMissing)},
		},
		{
			name: "accepts three questions",
			payload: func() MarkBlockedPayload {
				payload := validMarkBlockedPayload()
				payload.Questions = []string{"q1", "q2", "q3"}
				return payload
			},
		},
		{
			name: "rejects four questions",
			payload: func() MarkBlockedPayload {
				payload := validMarkBlockedPayload()
				payload.Questions = []string{"q1", "q2", "q3", "q4"}
				return payload
			},
			want: []models.FieldDiagnostic{diag("/questions", "maximum 3 questions allowed per blocking protocol", models.FieldValueClassOutOfRange)},
		},
		{
			name: "rejects a missing task id",
			payload: func() MarkBlockedPayload {
				payload := validMarkBlockedPayload()
				payload.TaskID = " "
				return payload
			},
			want: []models.FieldDiagnostic{diag("/task_id", "task ID is required", models.FieldValueClassMissing)},
		},
		{
			name: "rejects a missing reason",
			payload: func() MarkBlockedPayload {
				payload := validMarkBlockedPayload()
				payload.Reason = ""
				return payload
			},
			want: []models.FieldDiagnostic{diag("/reason", "reason is required", models.FieldValueClassMissing)},
		},
		{
			name: "accepts comma-separated depends_on entries",
			payload: func() MarkBlockedPayload {
				payload := validMarkBlockedPayload()
				payload.DependsOn = []string{"dep-1, dep-2", "dep-3"}
				return payload
			},
		},
		{
			name: "rejects an empty depends_on entry",
			payload: func() MarkBlockedPayload {
				payload := validMarkBlockedPayload()
				payload.DependsOn = []string{"dep-1", " "}
				return payload
			},
			want: []models.FieldDiagnostic{diag("/depends_on/1", "depends-on entries cannot be empty", models.FieldValueClassMissing)},
		},
		{
			name: "rejects a duplicate depends_on entry across comma splitting",
			payload: func() MarkBlockedPayload {
				payload := validMarkBlockedPayload()
				payload.DependsOn = []string{"dep-1,dep-1"}
				return payload
			},
			want: []models.FieldDiagnostic{diag("/depends_on/0", "depends-on entries must be unique", models.FieldValueClassConflict)},
		},
		{
			name:    "accepts a command-based repair request",
			payload: func() MarkBlockedPayload { return withRepair(commandRepairRequest()) },
		},
		{
			name: "accepts standalone error evidence",
			payload: func() MarkBlockedPayload {
				request := commandRepairRequest()
				request.Evidence = []string{"error=provider session thread not found"}
				return withRepair(request)
			},
		},
		{
			name: "rejects a missing repair operation",
			payload: func() MarkBlockedPayload {
				request := commandRepairRequest()
				request.Operation = "  "
				return withRepair(request)
			},
			want: []models.FieldDiagnostic{diag("/repair_request/operation", "repair request operation is required", models.FieldValueClassMissing)},
		},
		{
			name: "rejects a missing repair target",
			payload: func() MarkBlockedPayload {
				request := commandRepairRequest()
				request.Target = ""
				return withRepair(request)
			},
			want: []models.FieldDiagnostic{diag("/repair_request/target", "repair request target is required", models.FieldValueClassMissing)},
		},
		{
			name: "rejects a command-based request without a command",
			payload: func() MarkBlockedPayload {
				request := commandRepairRequest()
				request.Command = ""
				return withRepair(request)
			},
			want: []models.FieldDiagnostic{diag("/repair_request/command", "repair request command is required", models.FieldValueClassMissing)},
		},
		{
			name: "rejects dependency updates on a command-based request",
			payload: func() MarkBlockedPayload {
				request := commandRepairRequest()
				request.DependencyUpdates = dependencyRepairRequest().DependencyUpdates
				return withRepair(request)
			},
			want: []models.FieldDiagnostic{diag("/repair_request/dependency_updates", "command-based repair requests must not include dependency_updates", models.FieldValueClassConflict)},
		},
		{
			name: "rejects missing repair evidence",
			payload: func() MarkBlockedPayload {
				request := commandRepairRequest()
				request.Evidence = nil
				return withRepair(request)
			},
			want: []models.FieldDiagnostic{
				diag("/repair_request/evidence", "repair request evidence is required", models.FieldValueClassMissing),
				diag("/repair_request/evidence", structuredEvidence, models.FieldValueClassMalformed),
			},
		},
		{
			name: "rejects missing repair validation",
			payload: func() MarkBlockedPayload {
				request := commandRepairRequest()
				request.Validation = []string{" "}
				return withRepair(request)
			},
			want: []models.FieldDiagnostic{diag("/repair_request/validation", "repair request validation is required", models.FieldValueClassMissing)},
		},
		{
			name: "rejects unstructured failure evidence",
			payload: func() MarkBlockedPayload {
				request := commandRepairRequest()
				request.Evidence = []string{"git add failed with a read-only filesystem"}
				return withRepair(request)
			},
			want: []models.FieldDiagnostic{diag("/repair_request/evidence", structuredEvidence, models.FieldValueClassMalformed)},
		},
		{
			name:    "accepts a declarative dependency repair",
			payload: func() MarkBlockedPayload { return withRepair(dependencyRepairRequest()) },
		},
		{
			name: "accepts a whitespace-padded declarative dependency repair",
			payload: func() MarkBlockedPayload {
				request := dependencyRepairRequest()
				request.Operation = " apply-dependency-repair "
				request.Target = " task-1 "
				request.DependencyUpdates[0].TaskID = " consumer-1 "
				request.DependencyUpdates[0].DesiredDependsOn = []string{" producer-1 "}
				return withRepair(request)
			},
		},
		{
			name: "rejects a declarative repair targeting another task",
			payload: func() MarkBlockedPayload {
				request := dependencyRepairRequest()
				request.Target = "task-2"
				return withRepair(request)
			},
			want: []models.FieldDiagnostic{diag("/repair_request/target", "declarative dependency repair target must match blocked task", models.FieldValueClassConflict)},
		},
		{
			name: "rejects a declarative repair carrying a command",
			payload: func() MarkBlockedPayload {
				request := dependencyRepairRequest()
				request.Command = "apply-dependency-repair --json"
				return withRepair(request)
			},
			want: []models.FieldDiagnostic{diag("/repair_request/command", "declarative dependency repair must not include a command", models.FieldValueClassConflict)},
		},
		{
			name: "rejects a declarative repair without dependency updates",
			payload: func() MarkBlockedPayload {
				request := dependencyRepairRequest()
				request.DependencyUpdates = nil
				return withRepair(request)
			},
			want: []models.FieldDiagnostic{diag("/repair_request/dependency_updates", "declarative dependency repair dependency_updates is required", models.FieldValueClassMissing)},
		},
		{
			name: "rejects a dependency update without a task id",
			payload: func() MarkBlockedPayload {
				request := dependencyRepairRequest()
				request.DependencyUpdates[0].TaskID = " "
				return withRepair(request)
			},
			want: []models.FieldDiagnostic{diag("/repair_request/dependency_updates/0/task_id", "dependency_updates[0].task_id is required", models.FieldValueClassMissing)},
		},
		{
			name: "rejects duplicate dependency update task ids",
			payload: func() MarkBlockedPayload {
				request := dependencyRepairRequest()
				request.DependencyUpdates = append(request.DependencyUpdates, models.DependencyUpdate{
					TaskID:            " consumer-1 ",
					ExpectedDependsOn: []string{},
					DesiredDependsOn:  []string{},
				})
				return withRepair(request)
			},
			want: []models.FieldDiagnostic{diag("/repair_request/dependency_updates/1/task_id", "dependency update task_id values must be unique", models.FieldValueClassConflict)},
		},
		{
			name: "rejects an implicit expected dependency list",
			payload: func() MarkBlockedPayload {
				request := dependencyRepairRequest()
				request.DependencyUpdates[0].ExpectedDependsOn = nil
				return withRepair(request)
			},
			want: []models.FieldDiagnostic{diag("/repair_request/dependency_updates/0/expected_depends_on", "dependency_updates[0].expected_depends_on must be an explicit list", models.FieldValueClassNull)},
		},
		{
			name: "rejects an implicit desired dependency list",
			payload: func() MarkBlockedPayload {
				request := dependencyRepairRequest()
				request.DependencyUpdates[0].DesiredDependsOn = nil
				return withRepair(request)
			},
			want: []models.FieldDiagnostic{diag("/repair_request/dependency_updates/0/desired_depends_on", "dependency_updates[0].desired_depends_on must be an explicit list", models.FieldValueClassNull)},
		},
		{
			name: "rejects an empty dependency list entry",
			payload: func() MarkBlockedPayload {
				request := dependencyRepairRequest()
				request.DependencyUpdates[0].DesiredDependsOn = []string{"producer-1", " "}
				return withRepair(request)
			},
			want: []models.FieldDiagnostic{diag("/repair_request/dependency_updates/0/desired_depends_on/1", "dependency_updates[0].desired_depends_on entries cannot be empty", models.FieldValueClassMissing)},
		},
		{
			name: "rejects a duplicate dependency list entry",
			payload: func() MarkBlockedPayload {
				request := dependencyRepairRequest()
				request.DependencyUpdates[0].ExpectedDependsOn = []string{"producer-1", " producer-1 "}
				return withRepair(request)
			},
			want: []models.FieldDiagnostic{diag("/repair_request/dependency_updates/0/expected_depends_on/1", "dependency_updates[0].expected_depends_on entries must be unique", models.FieldValueClassConflict)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			payload := tt.payload()
			version, got, err := Validate(MarkBlockedOperation, payload)
			if err != nil {
				t.Fatalf("Validate() error: %v", err)
			}
			if version != 1 {
				t.Errorf("version = %d, want 1", version)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("diagnostics = %#v, want %#v", got, tt.want)
			}

			// The preflight decodes an agent's JSON file into map[string]any,
			// so the same payload must reach the same verdict in that shape.
			_, fromJSON, err := Validate(MarkBlockedOperation, jsonRoundTrip(t, payload))
			if err != nil {
				t.Fatalf("Validate(JSON) error: %v", err)
			}
			if !reflect.DeepEqual(fromJSON, got) {
				t.Errorf("JSON diagnostics = %#v, want %#v", fromJSON, got)
			}
		})
	}
}

func TestMarkBlockedSchemaRegistration(t *testing.T) {
	t.Parallel()

	if !slices.Contains(List(), Descriptor{Operation: MarkBlockedOperation, Version: 1}) {
		t.Errorf("List() = %#v, want a {%s, 1} descriptor", List(), MarkBlockedOperation)
	}
}

func TestMarkBlockedSchemaDecodeFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload any
		want    models.FieldDiagnostic
	}{
		{
			name:    "a bare array is not the canonical object",
			payload: []any{"task-1"},
			want:    diag("/", "must be the mark-blocked canonical JSON object", models.FieldValueClassWrongType),
		},
		{
			name:    "a scalar field of the wrong type names its own path",
			payload: map[string]any{"task_id": "task-1", "reason": "blocked", "questions": "not-a-list"},
			want:    diag("/questions", "must have the type the mark-blocked canonical object declares", models.FieldValueClassWrongType),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, got, err := Validate(MarkBlockedOperation, tt.payload)
			if err != nil {
				t.Fatalf("Validate() error: %v", err)
			}
			if !reflect.DeepEqual(got, []models.FieldDiagnostic{tt.want}) {
				t.Fatalf("diagnostics = %#v, want %#v", got, []models.FieldDiagnostic{tt.want})
			}
		})
	}
}

func jsonRoundTrip(t *testing.T, payload MarkBlockedPayload) any {
	t.Helper()

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return decoded
}
