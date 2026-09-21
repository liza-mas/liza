package payloadschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/models"
)

// MarkBlockedOperation names the blocking lifecycle operation on the registry.
const MarkBlockedOperation = "mark-blocked"

// markBlockedStructuredEvidence states what a repair request's evidence must
// record. The command example names no distribution binary (this package
// cannot reach the brand presentation) and the whole text fits the
// per-diagnostic byte bound so no example is clipped.
const markBlockedStructuredEvidence = `repair requests require structured failure evidence; valid examples: ` +
	`"command=add-task --json exit_code=1 stderr=command requires role type [orchestrator]", ` +
	`"command=provider-call exit_code=1 error=unavailable", or "error=provider session thread not found"`

// MarkBlockedPayload is the canonical object of the mark-blocked operation.
// The blocked task's ID is payload rather than caller identity because a
// declarative dependency repair must target the task it blocks; the agent ID
// authorizing the call is not read by any structural rule and stays at the
// mutation boundary.
type MarkBlockedPayload struct {
	TaskID        string                `json:"task_id"`
	Reason        string                `json:"reason"`
	Questions     []string              `json:"questions,omitempty"`
	DependsOn     []string              `json:"depends_on,omitempty"`
	RepairRequest *models.RepairRequest `json:"repair_request,omitempty"`
}

func init() {
	Register(Schema{Operation: MarkBlockedOperation, Version: 1, Validate: validateMarkBlocked})
}

func validateMarkBlocked(payload any) []models.FieldDiagnostic {
	decoded, decodeDiagnostic := decodeMarkBlocked(payload)
	if decodeDiagnostic != nil {
		return []models.FieldDiagnostic{*decodeDiagnostic}
	}

	taskID := strings.TrimSpace(decoded.TaskID)

	var diagnostics []models.FieldDiagnostic
	if taskID == "" {
		diagnostics = append(diagnostics, markBlockedDiagnostic("/task_id", "task ID is required", models.FieldValueClassMissing))
	}
	if strings.TrimSpace(decoded.Reason) == "" {
		diagnostics = append(diagnostics, markBlockedDiagnostic("/reason", "reason is required", models.FieldValueClassMissing))
	}
	switch {
	case len(decoded.Questions) == 0:
		diagnostics = append(diagnostics, markBlockedDiagnostic("/questions", "at least 1 question is required", models.FieldValueClassMissing))
	case len(decoded.Questions) > 3:
		diagnostics = append(diagnostics, markBlockedDiagnostic("/questions", "maximum 3 questions allowed per blocking protocol", models.FieldValueClassOutOfRange))
	}
	diagnostics = append(diagnostics, ValidateMarkBlockedDependsOn(decoded.DependsOn)...)
	return append(diagnostics, ValidateMarkBlockedRepairRequest(decoded.RepairRequest, taskID)...)
}

// ValidateMarkBlockedDependsOn checks the dependency IDs a blocking call adds.
// Entries are comma-separable, so each split part carries the constraint and
// the diagnostic path names the entry the caller wrote.
func ValidateMarkBlockedDependsOn(values []string) []models.FieldDiagnostic {
	var diagnostics []models.FieldDiagnostic
	seen := make(map[string]bool)
	for i, value := range values {
		field := fmt.Sprintf("/depends_on/%d", i)
		for _, part := range strings.Split(value, ",") {
			dependencyID := strings.TrimSpace(part)
			switch {
			case dependencyID == "":
				diagnostics = append(diagnostics, markBlockedDiagnostic(field, "depends-on entries cannot be empty", models.FieldValueClassMissing))
			case seen[dependencyID]:
				diagnostics = append(diagnostics, markBlockedDiagnostic(field, "depends-on entries must be unique", models.FieldValueClassConflict))
			default:
				seen[dependencyID] = true
			}
		}
	}
	return diagnostics
}

// ValidateMarkBlockedRepairRequest checks a blocked doer's repair request
// against the task it blocks. A nil request is valid: a blocker needs no
// repair. Whether the named target exists, and whether the graph it describes
// is acyclic, are state questions and stay at the mutation boundary.
func ValidateMarkBlockedRepairRequest(request *models.RepairRequest, blockedTaskID string) []models.FieldDiagnostic {
	if request == nil {
		return nil
	}

	operation := strings.TrimSpace(request.Operation)
	target := strings.TrimSpace(request.Target)
	command := strings.TrimSpace(request.Command)
	evidence := CompactNonEmpty(request.Evidence)
	validation := CompactNonEmpty(request.Validation)

	var diagnostics []models.FieldDiagnostic
	if operation == "" {
		diagnostics = append(diagnostics, markBlockedDiagnostic("/repair_request/operation", "repair request operation is required", models.FieldValueClassMissing))
	}
	if target == "" {
		diagnostics = append(diagnostics, markBlockedDiagnostic("/repair_request/target", "repair request target is required", models.FieldValueClassMissing))
	}

	if operation == models.RepairOperationApplyDependencyRepair {
		if target != "" && target != blockedTaskID {
			diagnostics = append(diagnostics, markBlockedDiagnostic("/repair_request/target",
				"declarative dependency repair target must match blocked task", models.FieldValueClassConflict))
		}
		if command != "" {
			diagnostics = append(diagnostics, markBlockedDiagnostic("/repair_request/command", "declarative dependency repair must not include a command", models.FieldValueClassConflict))
		}
		diagnostics = append(diagnostics, ValidateMarkBlockedDependencyUpdates(request.DependencyUpdates)...)
	} else {
		if command == "" {
			diagnostics = append(diagnostics, markBlockedDiagnostic("/repair_request/command", "repair request command is required", models.FieldValueClassMissing))
		}
		if request.DependencyUpdates != nil {
			diagnostics = append(diagnostics, markBlockedDiagnostic("/repair_request/dependency_updates", "command-based repair requests must not include dependency_updates", models.FieldValueClassConflict))
		}
	}

	if len(evidence) == 0 {
		diagnostics = append(diagnostics, markBlockedDiagnostic("/repair_request/evidence", "repair request evidence is required", models.FieldValueClassMissing))
	}
	if len(validation) == 0 {
		diagnostics = append(diagnostics, markBlockedDiagnostic("/repair_request/validation", "repair request validation is required", models.FieldValueClassMissing))
	}
	if compacted := (models.RepairRequest{Evidence: evidence}); !compacted.HasStructuredFailureEvidence() {
		diagnostics = append(diagnostics, markBlockedDiagnostic("/repair_request/evidence", markBlockedStructuredEvidence, models.FieldValueClassMalformed))
	}
	return diagnostics
}

// ValidateMarkBlockedDependencyUpdates checks the complete per-task dependency
// declarations of a declarative repair. The batch is required, each task
// appears once, and both lists are explicit so an omitted list can never read
// as "leave this one alone".
func ValidateMarkBlockedDependencyUpdates(updates []models.DependencyUpdate) []models.FieldDiagnostic {
	if len(updates) == 0 {
		return []models.FieldDiagnostic{markBlockedDiagnostic("/repair_request/dependency_updates",
			"declarative dependency repair dependency_updates is required", models.FieldValueClassMissing)}
	}

	var diagnostics []models.FieldDiagnostic
	seenTasks := make(map[string]bool, len(updates))
	for i, update := range updates {
		taskID := strings.TrimSpace(update.TaskID)
		field := fmt.Sprintf("/repair_request/dependency_updates/%d/task_id", i)
		switch {
		case taskID == "":
			diagnostics = append(diagnostics, markBlockedDiagnostic(field, fmt.Sprintf("dependency_updates[%d].task_id is required", i), models.FieldValueClassMissing))
		case seenTasks[taskID]:
			diagnostics = append(diagnostics, markBlockedDiagnostic(field, "dependency update task_id values must be unique", models.FieldValueClassConflict))
		default:
			seenTasks[taskID] = true
		}
		diagnostics = append(diagnostics, ValidateMarkBlockedDependencyList(update.ExpectedDependsOn, "expected_depends_on", i)...)
		diagnostics = append(diagnostics, ValidateMarkBlockedDependencyList(update.DesiredDependsOn, "desired_depends_on", i)...)
	}
	return diagnostics
}

// ValidateMarkBlockedDependencyList checks one explicit dependency list of a
// declarative repair. A nil list is a distinct defect from an empty one: an
// empty list declares "no dependencies", a missing one declares nothing.
func ValidateMarkBlockedDependencyList(values []string, field string, updateIndex int) []models.FieldDiagnostic {
	path := fmt.Sprintf("/repair_request/dependency_updates/%d/%s", updateIndex, field)
	if values == nil {
		return []models.FieldDiagnostic{markBlockedDiagnostic(path,
			fmt.Sprintf("dependency_updates[%d].%s must be an explicit list", updateIndex, field), models.FieldValueClassNull)}
	}

	var diagnostics []models.FieldDiagnostic
	seen := make(map[string]bool, len(values))
	for i, value := range values {
		dependencyID := strings.TrimSpace(value)
		entryPath := fmt.Sprintf("%s/%d", path, i)
		switch {
		case dependencyID == "":
			diagnostics = append(diagnostics, markBlockedDiagnostic(entryPath,
				fmt.Sprintf("dependency_updates[%d].%s entries cannot be empty", updateIndex, field), models.FieldValueClassMissing))
		case seen[dependencyID]:
			diagnostics = append(diagnostics, markBlockedDiagnostic(entryPath,
				fmt.Sprintf("dependency_updates[%d].%s entries must be unique", updateIndex, field), models.FieldValueClassConflict))
		default:
			seen[dependencyID] = true
		}
	}
	return diagnostics
}

// decodeMarkBlocked accepts either the decoded JSON document a preflight reads
// from a file or the canonical struct a mutation boundary builds, so both
// boundaries reach the same validator over the same fields.
func decodeMarkBlocked(payload any) (MarkBlockedPayload, *models.FieldDiagnostic) {
	if typed, ok := payload.(MarkBlockedPayload); ok {
		return typed, nil
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return MarkBlockedPayload{}, markBlockedDecodeDiagnostic("/", "must be the mark-blocked canonical JSON object")
	}
	var decoded MarkBlockedPayload
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		// The decoder's message can quote the rejected value, so only the field
		// it names crosses into a diagnostic.
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) && typeErr.Field != "" {
			return MarkBlockedPayload{}, markBlockedDecodeDiagnostic("/"+strings.ReplaceAll(typeErr.Field, ".", "/"),
				"must have the type the mark-blocked canonical object declares")
		}
		return MarkBlockedPayload{}, markBlockedDecodeDiagnostic("/", "must be the mark-blocked canonical JSON object")
	}
	return decoded, nil
}

func markBlockedDecodeDiagnostic(field, constraint string) *models.FieldDiagnostic {
	diagnostic := markBlockedDiagnostic(field, constraint, models.FieldValueClassWrongType)
	return &diagnostic
}

func markBlockedDiagnostic(field, constraint, valueClass string) models.FieldDiagnostic {
	return models.FieldDiagnostic{
		Field:      field,
		Constraint: constraint,
		ValueClass: valueClass,
		SafeAction: models.FieldDiagnosticCorrectInput,
	}
}

// CompactNonEmpty returns the trimmed, non-blank entries of a string list. It
// is the normalization every schema rule that asks whether a list is empty
// assumes, so a mutation boundary that persists such a list uses it too rather
// than reimplementing the rule the verdict was reached with.
func CompactNonEmpty(values []string) []string {
	var compacted []string
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			compacted = append(compacted, trimmed)
		}
	}
	return compacted
}
