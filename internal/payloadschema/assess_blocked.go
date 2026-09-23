package payloadschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/models"
)

// assessBlockedTextMaxBytes is the version-1 bound for note, reason and each
// question, matching the 4096-byte state-text bound without importing state IO.
const assessBlockedTextMaxBytes = 4096

func init() {
	Register(Schema{Operation: "assess-blocked", Version: 1, Validate: validateAssessBlocked})
}

// AssessBlockedPayload constructs the flag-keyed canonical JSON object shared
// by preflight and the mutation boundary. Empty reason/questions and a nil
// repair preserve history-only mode; awaited tasks are independent of mode and
// omitted when empty, so payloads without them are unchanged. JSON encoding
// orders map keys stably.
func AssessBlockedPayload(taskID, note, reason string, questions []string, repair *models.RepairRequest, awaited []string) map[string]any {
	payload := map[string]any{
		"task_id":        taskID,
		"note":           note,
		"reason":         reason,
		"questions":      questions,
		"repair_request": repair,
	}
	if len(awaited) > 0 {
		payload["awaited_tasks"] = awaited
	}
	return payload
}

func validateAssessBlocked(payload any) []models.FieldDiagnostic {
	object, rejected := scalarPayloadObject(payload)
	if rejected != nil {
		return []models.FieldDiagnostic{*rejected}
	}
	// Normalize typed slices and RepairRequest pointers to the same JSON types
	// preflight receives. Decoder errors never cross the diagnostic boundary.
	encoded, err := json.Marshal(object)
	if err != nil {
		return []models.FieldDiagnostic{scalarPayloadDiagnostic(payloadRootField, "must be a JSON object", models.FieldValueClassWrongType)}
	}
	object = nil // Decoding must not replace values in the caller's map.
	if err := json.Unmarshal(encoded, &object); err != nil {
		return []models.FieldDiagnostic{scalarPayloadDiagnostic(payloadRootField, "must be a JSON object", models.FieldValueClassWrongType)}
	}

	var diagnostics []models.FieldDiagnostic
	for _, field := range []string{"task_id", "note", "reason"} {
		if rejected := scalarPayloadString(object, field, field == "task_id"); rejected != nil {
			diagnostics = append(diagnostics, *rejected)
		}
	}
	questions, listOK := scalarPayloadEntries(object["questions"])
	if object["questions"] != nil && !listOK {
		diagnostics = append(diagnostics, scalarPayloadDiagnostic("/questions", "must be a list of strings", models.FieldValueClassWrongType))
	}
	awaited, awaitedOK := scalarPayloadEntries(object["awaited_tasks"])
	if object["awaited_tasks"] != nil && !awaitedOK {
		diagnostics = append(diagnostics, scalarPayloadDiagnostic("/awaited_tasks", "must be a list of strings", models.FieldValueClassWrongType))
	}
	if len(diagnostics) > 0 {
		return diagnostics
	}
	for i, entry := range awaited {
		if id, ok := entry.(string); !ok || strings.TrimSpace(id) == "" {
			diagnostics = append(diagnostics, scalarPayloadDiagnostic(fmt.Sprintf("/awaited_tasks/%d", i), "must be a non-blank task ID", models.FieldValueClassMissing))
		}
	}
	for _, field := range []string{"note", "reason"} {
		value, _ := object[field].(string)
		diagnostics = append(diagnostics, assessBlockedText("/"+field, value, false)...)
	}
	reason, _ := object["reason"].(string)
	// Presence of a repair alone must enter reconcile; empty flag defaults do
	// not. This is the same disjunction as the assessment mutation boundary.
	if reason == "" && len(questions) == 0 && object["repair_request"] == nil {
		return diagnostics
	}
	if strings.TrimSpace(reason) == "" {
		diagnostics = append(diagnostics, scalarPayloadDiagnostic("/reason", "reason is required in reconcile mode", models.FieldValueClassMissing))
	}
	switch {
	case len(questions) == 0:
		diagnostics = append(diagnostics, scalarPayloadDiagnostic("/questions", "at least 1 question is required in reconcile mode", models.FieldValueClassMissing))
	case len(questions) > 3:
		diagnostics = append(diagnostics, scalarPayloadDiagnostic("/questions", "must contain at most 3 questions", models.FieldValueClassOutOfRange))
	}
	for i, question := range questions {
		field := fmt.Sprintf("/questions/%d", i)
		text, ok := question.(string)
		if !ok {
			class := models.FieldValueClassWrongType
			if question == nil {
				class = models.FieldValueClassNull
			}
			diagnostics = append(diagnostics, scalarPayloadDiagnostic(field, "must be a string", class))
			continue
		}
		diagnostics = append(diagnostics, assessBlockedText(field, text, true)...)
	}
	taskID, _ := object["task_id"].(string)
	return append(diagnostics, assessBlockedRepair(object["repair_request"], taskID)...)
}

func assessBlockedText(field, text string, required bool) []models.FieldDiagnostic {
	switch {
	case required && strings.TrimSpace(text) == "":
		return []models.FieldDiagnostic{scalarPayloadDiagnostic(field, "must not be blank", models.FieldValueClassMissing)}
	case len(text) > assessBlockedTextMaxBytes:
		return []models.FieldDiagnostic{scalarPayloadDiagnostic(field, "must be at most 4096 bytes", models.FieldValueClassOversized)}
	}
	return nil
}

func assessBlockedRepair(value any, taskID string) []models.FieldDiagnostic {
	if value == nil {
		return nil
	}
	_, ok := value.(map[string]any)
	if !ok {
		return []models.FieldDiagnostic{scalarPayloadDiagnostic("/repair_request", "must be a JSON object", models.FieldValueClassWrongType)}
	}
	// The outer JSON round trip already established that this value can be
	// encoded. Decode the model to reject malformed nested update types too.
	encoded, _ := json.Marshal(value)
	var repair models.RepairRequest
	if err := json.Unmarshal(encoded, &repair); err != nil {
		field := "/repair_request"
		var typeError *json.UnmarshalTypeError
		if errors.As(err, &typeError) && typeError.Field != "" {
			field += "/" + strings.ReplaceAll(typeError.Field, ".", "/")
		}
		return []models.FieldDiagnostic{scalarPayloadDiagnostic(field, "must have the declared repair request field type", models.FieldValueClassWrongType)}
	}
	var diagnostics []models.FieldDiagnostic
	hasCommand := strings.TrimSpace(repair.Command) != ""
	invalidChoice := hasCommand == (len(repair.DependencyUpdates) > 0)
	if invalidChoice {
		class := models.FieldValueClassMissing
		if hasCommand {
			class = models.FieldValueClassConflict
		}
		diagnostics = append(diagnostics, scalarPayloadDiagnostic("/repair_request/command", "exactly one of command or dependency_updates is required", class))
	}
	for _, diagnostic := range ValidateMarkBlockedRepairRequest(&repair, taskID) {
		// F14/F15 define one diagnostic at /command for the exclusive choice.
		if invalidChoice && (diagnostic.Field == "/repair_request/command" || diagnostic.Field == "/repair_request/dependency_updates") {
			continue
		}
		// Missing evidence is already reported; its format is not a second
		// defect until evidence exists (the single diagnostic required by F12).
		if diagnostic.Field == "/repair_request/evidence" && diagnostic.ValueClass == models.FieldValueClassMalformed && len(CompactNonEmpty(repair.Evidence)) == 0 {
			continue
		}
		// Some shared constraints interpolate target/dependency identifiers.
		// Never forward their prose: retain paths/classes with static guidance.
		diagnostic.Constraint = assessBlockedRepairConstraint(diagnostic)
		diagnostics = append(diagnostics, diagnostic)
	}
	return diagnostics
}

func assessBlockedRepairConstraint(diagnostic models.FieldDiagnostic) string {
	switch diagnostic.Field {
	case "/repair_request/operation":
		return "must not be empty"
	case "/repair_request/target":
		if diagnostic.ValueClass == models.FieldValueClassMissing {
			return "must not be empty"
		}
		return "must match the assessed task for dependency repair"
	case "/repair_request/command":
		return "command is required only for command-based repairs"
	case "/repair_request/dependency_updates":
		return "non-empty dependency updates are required only for dependency repairs"
	case "/repair_request/evidence":
		if diagnostic.ValueClass == models.FieldValueClassMalformed {
			return "must contain structured failure evidence"
		}
		return "must contain non-blank strings"
	case "/repair_request/validation":
		return "must contain non-blank strings"
	default:
		return "dependency updates require unique non-blank task IDs and explicit lists of unique non-blank dependency IDs"
	}
}
