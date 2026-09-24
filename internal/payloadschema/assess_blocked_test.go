package payloadschema

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestAssessBlockedSchema(t *testing.T) {
	t.Parallel()
	completeRepair := func() map[string]any {
		return map[string]any{
			"operation": "recover-task", "target": "task-1", "command": "recover-task task-1",
			"evidence": []string{"error=provider unavailable"}, "validation": []string{"validate"},
		}
	}
	repairWith := func(field string, value any) map[string]any {
		r := completeRepair()
		r[field] = value
		if value == nil {
			delete(r, field)
		}
		return r
	}
	reconcile := func(repair any) map[string]any {
		return map[string]any{"task_id": "task-1", "reason": "provider unavailable", "questions": []string{"Retry?"}, "repair_request": repair}
	}
	missing := models.FieldValueClassMissing
	// F1-F15 are the shared fixtures in the reviewed output-2 plan's
	// Payload schema and parity section; the ops parity tests use the same IDs.
	tests := []struct {
		name    string
		payload map[string]any
		want    []models.FieldDiagnostic
	}{
		{"F1", map[string]any{"task_id": "task-1", "note": "still blocked"}, nil},
		{"F2", map[string]any{"task_id": "task-1"}, nil},
		{"F3", reconcile(nil), nil},
		{"F4", map[string]any{"task_id": "task-1", "reason": "blocked", "questions": []string{"q1", "q2", "q3"}, "repair_request": completeRepair()}, nil},
		{"F5", map[string]any{"task_id": "", "note": "blocked"}, []models.FieldDiagnostic{correctInput("/task_id", "must not be empty", missing)}},
		{"F6", map[string]any{"task_id": "task-1", "repair_request": completeRepair()}, []models.FieldDiagnostic{
			correctInput("/reason", "reason is required in reconcile mode", missing),
			correctInput("/questions", "at least 1 question is required in reconcile mode", missing),
		}},
		{"F7", map[string]any{"task_id": "task-1", "reason": "blocked"}, []models.FieldDiagnostic{correctInput("/questions", "at least 1 question is required in reconcile mode", missing)}},
		{"F8", map[string]any{"task_id": "task-1", "questions": []string{"q"}}, []models.FieldDiagnostic{correctInput("/reason", "reason is required in reconcile mode", missing)}},
		{"F9", map[string]any{"task_id": "task-1", "reason": "blocked", "questions": []string{"q1", "q2", "q3", "q4"}}, []models.FieldDiagnostic{correctInput("/questions", "must contain at most 3 questions", models.FieldValueClassOutOfRange)}},
		{"F10", map[string]any{"task_id": "task-1", "reason": "blocked", "questions": []string{"  "}}, []models.FieldDiagnostic{correctInput("/questions/0", "must not be blank", missing)}},
		{"F11", reconcile(repairWith("operation", nil)), []models.FieldDiagnostic{correctInput("/repair_request/operation", "must not be empty", missing)}},
		{"F12", reconcile(repairWith("evidence", []string{})), []models.FieldDiagnostic{correctInput("/repair_request/evidence", "must contain non-blank strings", missing)}},
		{"F13", reconcile(repairWith("validation", []string{})), []models.FieldDiagnostic{correctInput("/repair_request/validation", "must contain non-blank strings", missing)}},
		{"F14", reconcile(repairWith("dependency_updates", []any{map[string]any{"task_id": "task-1", "expected_depends_on": []string{}, "desired_depends_on": []string{}}})), []models.FieldDiagnostic{correctInput("/repair_request/command", "exactly one of command or dependency_updates is required", models.FieldValueClassConflict)}},
		{"F15", reconcile(repairWith("command", "")), []models.FieldDiagnostic{correctInput("/repair_request/command", "exactly one of command or dependency_updates is required", missing)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertAssessBlockedDiagnostics(t, tt.payload, tt.want)
			encoded, err := json.Marshal(tt.payload)
			if err != nil {
				t.Fatal(err)
			}
			var decoded any
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			assertAssessBlockedDiagnostics(t, decoded, tt.want)
		})
	}
}

func assertAssessBlockedDiagnostics(t *testing.T, payload any, want []models.FieldDiagnostic) {
	t.Helper()
	version, diagnostics, err := Validate("assess-blocked", payload)
	if err != nil || version != 1 {
		t.Fatalf("version=%d, error=%v", version, err)
	}
	requireDiagnostics(t, diagnostics, want)
	for _, diagnostic := range diagnostics {
		if diagnostic.SchemaVersion != 1 {
			t.Errorf("schema_version=%d", diagnostic.SchemaVersion)
		}
	}
}

func TestAssessBlockedSchemaConstructor(t *testing.T) {
	t.Parallel()
	schema, ok := Lookup("assess-blocked")
	if !ok || schema.Version != 1 {
		t.Fatalf("schema=%+v, found=%v", schema, ok)
	}
	if !slices.Contains(List(), Descriptor{Operation: "assess-blocked", Version: 1}) {
		t.Fatal("schema not discoverable")
	}
	repair := &models.RepairRequest{Operation: "recover-task", Target: "task-1", Command: "recover-task task-1", Evidence: []string{"error=unavailable"}, Validation: []string{"validate"}}
	payload := AssessBlockedPayload("task-1", "note", "reason", []string{"q1", "q2"}, repair, nil, false)
	want := map[string]any{"task_id": "task-1", "note": "note", "reason": "reason", "questions": []string{"q1", "q2"}, "repair_request": repair}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("payload=%+v, want %+v", payload, want)
	}
	first, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(AssessBlockedPayload("task-1", "note", "reason", []string{"q1", "q2"}, repair, nil, false))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("equal inputs have unequal encodings")
	}
	assertAssessBlockedDiagnostics(t, payload, nil)
	if !reflect.DeepEqual(payload, want) {
		t.Fatal("validation changed the caller's payload")
	}
	assertAssessBlockedDiagnostics(t, AssessBlockedPayload("task-1", "note", "", nil, nil, nil, false), nil)
	assertAssessBlockedDiagnostics(t, AssessBlockedPayload("task-1", "", "", []string{}, nil, nil, false), nil)
}

func TestAssessBlockedSchemaAwaitedTasks(t *testing.T) {
	t.Parallel()
	assertAssessBlockedDiagnostics(t, AssessBlockedPayload("task-1", "note", "", nil, nil, []string{"gen-1", "gen-2"}, false), nil)
	for _, tt := range []struct {
		name    string
		awaited any
		field   string
		class   string
	}{
		{"not a list", "gen-1", "/awaited_tasks", models.FieldValueClassWrongType},
		{"blank entry", []any{"gen-1", " "}, "/awaited_tasks/1", models.FieldValueClassMissing},
		{"non-string entry", []any{42}, "/awaited_tasks/0", models.FieldValueClassMissing},
	} {
		t.Run(tt.name, func(t *testing.T) {
			payload := AssessBlockedPayload("task-1", "note", "", nil, nil, nil, false)
			payload["awaited_tasks"] = tt.awaited
			_, diagnostics, err := Validate("assess-blocked", payload)
			if err != nil || len(diagnostics) != 1 || diagnostics[0].Field != tt.field || diagnostics[0].ValueClass != tt.class {
				t.Fatalf("diagnostics = %+v, %v; want one %s at %s", diagnostics, err, tt.class, tt.field)
			}
		})
	}
}

func TestAssessBlockedSchemaClearAwaits(t *testing.T) {
	t.Parallel()
	assertAssessBlockedDiagnostics(t, AssessBlockedPayload("task-1", "note", "", nil, nil, nil, true), nil)
	if _, present := AssessBlockedPayload("task-1", "note", "", nil, nil, nil, false)["clear_awaits"]; present {
		t.Fatal("a false clear flag changed the payload shape")
	}
	for _, tt := range []struct {
		name    string
		payload map[string]any
		class   string
	}{
		{"not a boolean", func() map[string]any {
			payload := AssessBlockedPayload("task-1", "note", "", nil, nil, nil, false)
			payload["clear_awaits"] = "yes"
			return payload
		}(), models.FieldValueClassWrongType},
		{"with awaited tasks", AssessBlockedPayload("task-1", "note", "", nil, nil, []string{"gen-1"}, true), models.FieldValueClassConflict},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, diagnostics, err := Validate("assess-blocked", tt.payload)
			if err != nil || len(diagnostics) != 1 || diagnostics[0].Field != "/clear_awaits" || diagnostics[0].ValueClass != tt.class {
				t.Fatalf("diagnostics = %+v, %v; want one %s at /clear_awaits", diagnostics, err, tt.class)
			}
		})
	}
}

func TestAssessBlockedSchemaDependencyRepair(t *testing.T) {
	t.Parallel()
	const privateID = "private-dependency-marker"
	repair := &models.RepairRequest{
		Operation: models.RepairOperationApplyDependencyRepair, Target: "task-1",
		Evidence: []string{"error=dependency unavailable"}, Validation: []string{"validate"},
		DependencyUpdates: []models.DependencyUpdate{{TaskID: "task-1", ExpectedDependsOn: []string{}, DesiredDependsOn: []string{privateID}}},
	}
	assertAssessBlockedDiagnostics(t, AssessBlockedPayload("task-1", "", "blocked", []string{"Repair?"}, repair, nil, false), nil)
	repair.DependencyUpdates[0].DesiredDependsOn = []string{privateID, privateID}
	payload := AssessBlockedPayload("task-1", "", "blocked", []string{"Repair?"}, repair, nil, false)
	assertAssessBlockedDiagnostics(t, payload, []models.FieldDiagnostic{correctInput(
		"/repair_request/dependency_updates/0/desired_depends_on/1",
		"dependency updates require unique non-blank task IDs and explicit lists of unique non-blank dependency IDs",
		models.FieldValueClassConflict)})
	_, diagnostics, err := Validate("assess-blocked", payload)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), privateID) {
		t.Fatal("shared validator disclosed a rejected dependency")
	}
}

func TestAssessBlockedSchemaTextBounds(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"note", "reason", "question"} {
		t.Run(field, func(t *testing.T) {
			for _, size := range []int{4096, 4097} {
				payload := AssessBlockedPayload("task-1", "", "reason", []string{"question"}, nil, nil, false)
				path := "/" + field
				text := strings.Repeat("x", size)
				if field == "question" {
					payload["questions"] = []string{text}
					path = "/questions/0"
				} else {
					payload[field] = text
				}
				var want []models.FieldDiagnostic
				if size > 4096 {
					want = []models.FieldDiagnostic{correctInput(path, "must be at most 4096 bytes", models.FieldValueClassOversized)}
				}
				assertAssessBlockedDiagnostics(t, payload, want)
			}
		})
	}
}

func TestAssessBlockedSchemaTypesAndPrivacy(t *testing.T) {
	t.Parallel()
	const rejected = "private-payload-marker"
	tests := []struct {
		name                     string
		payload                  any
		field, constraint, class string
	}{
		{"root", rejected, "payload", "must be a JSON object", models.FieldValueClassWrongType},
		{"null", nil, "payload", "must be a JSON object", models.FieldValueClassNull},
		{"task type", map[string]any{"task_id": []string{rejected}}, "/task_id", "must be a string", models.FieldValueClassWrongType},
		{"note type", map[string]any{"task_id": "task-1", "note": []string{rejected}}, "/note", "must be a string", models.FieldValueClassWrongType},
		{"question type", map[string]any{"task_id": "task-1", "reason": "reason", "questions": []any{map[string]any{rejected: true}}}, "/questions/0", "must be a string", models.FieldValueClassWrongType},
		{"questions type", map[string]any{"task_id": "task-1", "reason": "reason", "questions": rejected}, "/questions", "must be a list of strings", models.FieldValueClassWrongType},
		{"repair type", map[string]any{"task_id": "task-1", "reason": "reason", "questions": []string{"q"}, "repair_request": rejected}, "/repair_request", "must be a JSON object", models.FieldValueClassWrongType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertAssessBlockedDiagnostics(t, tt.payload, []models.FieldDiagnostic{correctInput(tt.field, tt.constraint, tt.class)})
			_, diagnostics, _ := Validate("assess-blocked", tt.payload)
			encoded, err := json.Marshal(diagnostics)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), rejected) {
				t.Fatal("diagnostics disclose rejected value")
			}
		})
	}
}
