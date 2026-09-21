package payloadschema

import (
	"slices"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// validRejectionRCAObject is the decoded JSON form of a request the shared
// validator accepts, so each case below changes exactly one thing.
func validRejectionRCAObject() map[string]any {
	return map[string]any{
		"schema_version": float64(models.RejectionRCASchemaVersion),
		"summary":        "two product defects and one reviewer capability failure",
		"contributions": []any{
			map[string]any{
				"rejection_index": float64(1),
				"categories":      []any{models.RejectionCauseProductDefect},
				"evidence":        []any{"review verdict 1: identity scalar mismatch"},
			},
			map[string]any{
				"rejection_index": float64(2),
				"categories":      []any{models.RejectionCauseCapabilityFailure, models.RejectionCauseProductDefect},
			},
		},
	}
}

func validRejectionRCARequest() models.RejectionRCARequest {
	return models.RejectionRCARequest{
		SchemaVersion: models.RejectionRCASchemaVersion,
		Summary:       "two product defects and one reviewer capability failure",
		Contributions: []models.RejectionRCAContribution{
			{RejectionIndex: 1, Categories: []string{models.RejectionCauseProductDefect}, Evidence: []string{"review verdict 1: identity scalar mismatch"}},
			{RejectionIndex: 2, Categories: []string{models.RejectionCauseCapabilityFailure, models.RejectionCauseProductDefect}},
		},
	}
}

// requireSchemaVersion proves the registry stamped every diagnostic.
func requireSchemaVersion(t *testing.T, diagnostics []models.FieldDiagnostic, version int) {
	t.Helper()
	if version != 1 {
		t.Errorf("schema version = %d, want 1", version)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.SchemaVersion != 1 {
			t.Errorf("diagnostic %s schema_version = %d, want 1", diagnostic.Field, diagnostic.SchemaVersion)
		}
	}
}

// withoutSchemaVersion strips the registry's stamp so a schema verdict can be
// compared with the shared validator's own output.
func withoutSchemaVersion(diagnostics []models.FieldDiagnostic) []models.FieldDiagnostic {
	stripped := slices.Clone(diagnostics)
	for i := range stripped {
		stripped[i].SchemaVersion = 0
	}
	return stripped
}

// wrongType and notRequestKey are the two decode verdicts the RCA schemas
// report before delegating.
func wrongType(field string) models.FieldDiagnostic {
	return correctInput(field, rejectionRCAWrongTypeConstraint, models.FieldValueClassWrongType)
}

func notRequestKey(field string) models.FieldDiagnostic {
	return correctInput(field, rejectionRCAUnknownKeyConstraint, models.FieldValueClassWrongType)
}

func TestRejectionRCASchemaDiagnostics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(object map[string]any)
		payload any
		want    []models.FieldDiagnostic
	}{
		{
			name:   "valid object yields nil",
			mutate: func(map[string]any) {},
		},
		{
			name:    "typed request passes through",
			payload: validRejectionRCARequest(),
		},
		{
			name:    "payload is not an object",
			payload: "summary only",
			want:    []models.FieldDiagnostic{wrongType("/")},
		},
		{
			name:    "payload is a list",
			payload: []any{validRejectionRCAObject()},
			want:    []models.FieldDiagnostic{wrongType("/")},
		},
		{
			name:    "payload is null",
			payload: nil,
			want:    []models.FieldDiagnostic{correctInput("/", rejectionRCAWrongTypeConstraint, models.FieldValueClassNull)},
		},
		{
			name:   "schema_version is a string",
			mutate: func(o map[string]any) { o["schema_version"] = "1" },
			want:   []models.FieldDiagnostic{wrongType("/schema_version")},
		},
		{
			name:   "summary is a number",
			mutate: func(o map[string]any) { o["summary"] = 5 },
			want:   []models.FieldDiagnostic{wrongType("/summary")},
		},
		{
			name:   "contributions is a string",
			mutate: func(o map[string]any) { o["contributions"] = "product_defect" },
			want:   []models.FieldDiagnostic{wrongType("/contributions")},
		},
		{
			name: "contribution is not an object",
			mutate: func(o map[string]any) {
				o["contributions"] = []any{map[string]any{"rejection_index": 1, "categories": []any{"unknown"}}, 5}
			},
			want: []models.FieldDiagnostic{wrongType("/contributions/1")},
		},
		{
			name: "rejection_index is a string on the second contribution",
			mutate: func(o map[string]any) {
				o["contributions"].([]any)[1].(map[string]any)["rejection_index"] = "2"
			},
			want: []models.FieldDiagnostic{wrongType("/contributions/1/rejection_index")},
		},
		{
			name: "categories holds a number",
			mutate: func(o map[string]any) {
				o["contributions"].([]any)[0].(map[string]any)["categories"] = []any{1}
			},
			want: []models.FieldDiagnostic{wrongType("/contributions/0/categories")},
		},
		{
			name:   "seeded threshold is not a request field",
			mutate: func(o map[string]any) { o["threshold"] = 4 },
			want:   []models.FieldDiagnostic{notRequestKey("/threshold")},
		},
		{
			name:   "seeded gated_at is not a request field",
			mutate: func(o map[string]any) { o["gated_at"] = "2026-09-19T00:00:00Z" },
			want:   []models.FieldDiagnostic{notRequestKey("/gated_at")},
		},
		{
			name: "several seeded keys are reported in key order",
			mutate: func(o map[string]any) {
				o["rejection_count"] = 4
				o["fingerprint"] = "deadbeef"
				o["disposition"] = map[string]any{"recovery_path": "rescope"}
			},
			want: []models.FieldDiagnostic{notRequestKey("/disposition"), notRequestKey("/fingerprint"), notRequestKey("/rejection_count")},
		},
		{
			name: "unknown key inside a contribution",
			mutate: func(o map[string]any) {
				o["contributions"].([]any)[1].(map[string]any)["recorded_by"] = "orchestrator-1"
			},
			want: []models.FieldDiagnostic{notRequestKey("/contributions/1/recorded_by")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			payload := tt.payload
			if tt.mutate != nil {
				object := validRejectionRCAObject()
				tt.mutate(object)
				payload = object
			}

			version, diagnostics, err := Validate(RecordRejectionRCAOperation, payload)
			if err != nil {
				t.Fatalf("Validate(%q) returned %v, want the registered schema", RecordRejectionRCAOperation, err)
			}
			requireSchemaVersion(t, diagnostics, version)
			requireDiagnostics(t, diagnostics, tt.want)
		})
	}
}

// TestRejectionRCASchemaDelegates proves the structural verdict is the shared
// validator's: the schema's diagnostics for a decoded object equal
// statevalidate's for the same typed request, and the typed request reaches
// the same verdict through the registry as its JSON form does.
func TestRejectionRCASchemaDelegates(t *testing.T) {
	t.Parallel()

	object := validRejectionRCAObject()
	object["contributions"].([]any)[0].(map[string]any)["rejection_index"] = float64(0)
	object["summary"] = ""
	request := validRejectionRCARequest()
	request.Contributions[0].RejectionIndex = 0
	request.Summary = ""

	want := statevalidate.ValidateRejectionRCARequest(request)
	if len(want) != 2 {
		t.Fatalf("shared validator diagnostics = %+v, want the summary and rejection_index defects", want)
	}

	_, fromObject, err := Validate(RecordRejectionRCAOperation, object)
	if err != nil {
		t.Fatalf("Validate(object) returned %v", err)
	}
	requireDiagnostics(t, withoutSchemaVersion(fromObject), want)

	_, fromRequest, err := Validate(RecordRejectionRCAOperation, request)
	if err != nil {
		t.Fatalf("Validate(request) returned %v", err)
	}
	requireDiagnostics(t, withoutSchemaVersion(fromRequest), want)
}

func TestResumeRejectionRCASchemaDiagnostics(t *testing.T) {
	t.Parallel()

	validObject := func() map[string]any {
		return map[string]any{
			"schema_version": float64(models.RejectionRCASchemaVersion),
			"recovery_path":  models.RecoveryCapabilityReroute,
			"rationale":      "the reviewer session could not run the Postgres suite",
		}
	}

	tests := []struct {
		name    string
		mutate  func(object map[string]any)
		payload any
		want    []models.FieldDiagnostic
	}{
		{
			name:   "valid object yields nil",
			mutate: func(map[string]any) {},
		},
		{
			name:   "rationale is optional",
			mutate: func(o map[string]any) { delete(o, "rationale") },
		},
		{
			name:    "typed request passes through",
			payload: models.RejectionRCADispositionRequest{SchemaVersion: models.RejectionRCASchemaVersion, RecoveryPath: models.RecoveryRescope},
		},
		{
			name:    "payload is not an object",
			payload: []any{"capability_reroute"},
			want:    []models.FieldDiagnostic{wrongType("/")},
		},
		{
			name:   "recovery_path is a number",
			mutate: func(o map[string]any) { o["recovery_path"] = 2 },
			want:   []models.FieldDiagnostic{wrongType("/recovery_path")},
		},
		{
			name:   "rationale is a list",
			mutate: func(o map[string]any) { o["rationale"] = []any{"one", "two"} },
			want:   []models.FieldDiagnostic{wrongType("/rationale")},
		},
		{
			name:   "schema_version is a boolean",
			mutate: func(o map[string]any) { o["schema_version"] = true },
			want:   []models.FieldDiagnostic{wrongType("/schema_version")},
		},
		{
			name:   "derived restore_mode is not a request field",
			mutate: func(o map[string]any) { o["restore_mode"] = models.RestoreModeAssign },
			want:   []models.FieldDiagnostic{notRequestKey("/restore_mode")},
		},
		{
			name:   "derived actor and decided_at are not request fields",
			mutate: func(o map[string]any) { o["actor"] = "orchestrator-1"; o["decided_at"] = "2026-09-19T00:00:00Z" },
			want:   []models.FieldDiagnostic{notRequestKey("/actor"), notRequestKey("/decided_at")},
		},
		{
			name:   "unknown recovery path is the shared validator's verdict",
			mutate: func(o map[string]any) { o["recovery_path"] = "retry_harder" },
			want:   withoutSchemaVersion(statevalidate.ValidateRejectionRCADispositionRequest(models.RejectionRCADispositionRequest{SchemaVersion: 1, RecoveryPath: "retry_harder"})),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			payload := tt.payload
			if tt.mutate != nil {
				object := validObject()
				tt.mutate(object)
				payload = object
			}

			version, diagnostics, err := Validate(ResumeRejectionRCAOperation, payload)
			if err != nil {
				t.Fatalf("Validate(%q) returned %v, want the registered schema", ResumeRejectionRCAOperation, err)
			}
			requireSchemaVersion(t, diagnostics, version)
			requireDiagnostics(t, withoutSchemaVersion(diagnostics), tt.want)
		})
	}

	// The delegated case must be a real defect, or the comparison above proves
	// nothing.
	if statevalidate.ValidateRejectionRCADispositionRequest(models.RejectionRCADispositionRequest{SchemaVersion: 1, RecoveryPath: "retry_harder"}) == nil {
		t.Errorf("shared validator accepted an unknown recovery path; the delegation case is vacuous")
	}
}

func TestResumeRejectionRCARationale(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		models.RecoveryImplementationCorrection, models.RecoveryCapabilityReroute,
		models.RecoveryLifecycleRepair, models.RecoveryRescope, models.RecoveryHumanOverride,
	} {
		for _, rationale := range []struct {
			name    string
			value   string
			present bool
		}{
			{name: "missing"},
			{name: "empty", present: true},
			{name: "whitespace", value: " \t\n\u2003", present: true},
			{name: "nonblank", value: "operator accepts the residual risk", present: true},
		} {
			t.Run(path+"/"+rationale.name, func(t *testing.T) {
				t.Parallel()
				payload := map[string]any{"schema_version": float64(1), "recovery_path": path}
				if rationale.present {
					payload["rationale"] = rationale.value
				}
				version, diagnostics, err := Validate(ResumeRejectionRCAOperation, payload)
				if err != nil {
					t.Fatal(err)
				}
				requireSchemaVersion(t, diagnostics, version)
				var want []models.FieldDiagnostic
				if path == models.RecoveryHumanOverride && rationale.name != "nonblank" {
					want = []models.FieldDiagnostic{correctInput("/rationale", "a non-empty value is required", models.FieldValueClassMissing)}
				}
				requireDiagnostics(t, withoutSchemaVersion(diagnostics), want)
			})
		}
	}
}

func TestRejectionRCASchemasRegistered(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{RecordRejectionRCAOperation, ResumeRejectionRCAOperation} {
		schema, registered := Lookup(operation)
		if !registered {
			t.Fatalf("Lookup(%q) found no schema; the operation is not preflightable", operation)
		}
		if schema.Version != 1 {
			t.Errorf("%s version = %d, want 1", operation, schema.Version)
		}
		if !slices.Contains(List(), Descriptor{Operation: operation, Version: 1}) {
			t.Errorf("List() = %+v, want a {%s, 1} row", List(), operation)
		}
	}
}
