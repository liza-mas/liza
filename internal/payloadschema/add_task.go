package payloadschema

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// This private decoding shape mirrors AddTaskInput without importing ops, which
// consumes this package. Both typed callers and decoded preflight JSON cross
// the same JSON boundary; pipeline-dependent role/type checks stay in ops.
type addTaskPayload struct {
	ID                      string                          `json:"id"`
	Type                    string                          `json:"type"`
	RolePair                string                          `json:"role_pair"`
	Description             string                          `json:"desc"`
	SpecRef                 string                          `json:"spec"`
	PlanRef                 string                          `json:"plan_ref"`
	DoneWhen                string                          `json:"done"`
	Validation              []string                        `json:"validation"`
	ValidationPrerequisites []models.ValidationPrerequisite `json:"validation_prerequisites"`
	DestructiveDB           bool                            `json:"destructive_db"`
	Scope                   string                          `json:"scope"`
	Priority                int                             `json:"priority"`
	RCARequired             bool                            `json:"rca_required"`
	DependsOn               []string                        `json:"depends"`
}

func init() {
	Register(Schema{Operation: "add-task", Version: 1, Validate: validateAddTaskPayload})
}

func validateAddTaskPayload(payload any) []models.FieldDiagnostic {
	var input addTaskPayload
	object, diagnostics := decodeTaskPayload(payload, &input)
	if len(diagnostics) > 0 {
		return diagnostics
	}
	diagnostics = append(diagnostics, taskIDDiagnostics("/id", input.ID)...)
	for _, field := range []struct{ key, value, label string }{
		{"desc", input.Description, "description"}, {"spec", input.SpecRef, "spec_ref"},
		{"done", input.DoneWhen, "done_when"}, {"scope", input.Scope, "scope"},
	} {
		if field.value == "" {
			diagnostics = append(diagnostics, scalarPayloadDiagnostic("/"+field.key, field.label+" is required", models.FieldValueClassMissing))
		}
	}
	for _, ref := range []struct{ key, value string }{{"spec", input.SpecRef}, {"plan_ref", input.PlanRef}} {
		if err := statevalidate.ValidateArtifactRefScalar(ref.key, ref.value, ""); err != nil || strings.ContainsAny(ref.value, "\r\n") {
			diagnostics = append(diagnostics, scalarPayloadDiagnostic("/"+ref.key, "multiple refs or invalid path syntax: use one clean repository-relative artifact reference", models.FieldValueClassMalformed))
		}
	}
	if input.Priority < 1 {
		class := models.FieldValueClassOutOfRange
		if _, present := object["priority"]; !present {
			class = models.FieldValueClassMissing
		}
		diagnostics = append(diagnostics, scalarPayloadDiagnostic("/priority", "priority must be positive", class))
	}
	if input.Type != "" && !models.TaskType(input.Type).IsValid() {
		diagnostics = append(diagnostics, scalarPayloadDiagnostic("/type", "unknown task type; must be empty or a registered task type", models.FieldValueClassUnknownEnum))
	}
	// These model validators deliberately return field/index context only.
	if err := models.ValidateValidationSafety("validation", input.Validation, input.DestructiveDB); err != nil {
		diagnostics = append(diagnostics, scalarPayloadDiagnostic("/validation", err.Error(), models.FieldValueClassMalformed))
	}
	if err := models.ValidateValidationPrerequisites(input.Validation, input.ValidationPrerequisites); err != nil {
		diagnostics = append(diagnostics, scalarPayloadDiagnostic("/validation_prerequisites", err.Error(), models.FieldValueClassMalformed))
	}
	if input.RolePair == "" {
		diagnostics = append(diagnostics, scalarPayloadDiagnostic("/role_pair", "role_pair is required", models.FieldValueClassMissing))
	}
	return diagnostics
}

// decodeTaskPayload is shared by creation and replacement, keeping typed ops
// inputs and decoded JSON on one structural path. Decoder errors can quote
// payload values, so only the declared field name crosses into diagnostics.
func decodeTaskPayload(payload, target any) (map[string]any, []models.FieldDiagnostic) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, []models.FieldDiagnostic{scalarPayloadDiagnostic("/", "must be a JSON object", models.FieldValueClassWrongType)}
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		return nil, []models.FieldDiagnostic{scalarPayloadDiagnostic("/", "must be a JSON object", models.FieldValueClassWrongType)}
	}
	if object == nil {
		return nil, []models.FieldDiagnostic{scalarPayloadDiagnostic("/", "must not be null", models.FieldValueClassNull)}
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		field := "/"
		var typeError *json.UnmarshalTypeError
		if errors.As(err, &typeError) && typeError.Field != "" {
			field += strings.ReplaceAll(typeError.Field, ".", "/")
		}
		return nil, []models.FieldDiagnostic{scalarPayloadDiagnostic(field, "must have the declared JSON field type", models.FieldValueClassWrongType)}
	}
	return object, nil
}

// Match the task-ID path boundary without importing paths: schema packages may
// depend only on models and pure statevalidate rules, never runtime packages.
var taskPayloadIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

func taskIDDiagnostics(field, value string) []models.FieldDiagnostic {
	if value == "" {
		return []models.FieldDiagnostic{scalarPayloadDiagnostic(field, "invalid task ID: task ID cannot be empty", models.FieldValueClassMissing)}
	}
	if !taskPayloadIDPattern.MatchString(value) || strings.Contains(value, "..") {
		return []models.FieldDiagnostic{scalarPayloadDiagnostic(field, "invalid task ID: use alphanumeric initial character followed by alphanumeric, dot, underscore or hyphen; no double dots", models.FieldValueClassMalformed)}
	}
	return nil
}
