package payloadschema

import (
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/models"
)

func init() {
	Register(Schema{Operation: "replace-task", Version: 1, Validate: validateReplaceTaskPayload})
}

func validateReplaceTaskPayload(payload any) []models.FieldDiagnostic {
	var input struct {
		SourceTaskID  string                    `json:"source_task_id"`
		Reason        string                    `json:"reason"`
		Consumers     []models.DependencyUpdate `json:"consumers"`
		PreservedBase *struct {
			BaseCommit string `json:"base_commit"`
			Worktree   string `json:"worktree"`
		} `json:"preserved_base"`
	}
	object, diagnostics := decodeTaskPayload(payload, &input)
	if len(diagnostics) > 0 {
		return diagnostics
	}
	diagnostics = append(diagnostics, taskIDDiagnostics("/source_task_id", input.SourceTaskID)...)
	if strings.TrimSpace(input.Reason) == "" {
		diagnostics = append(diagnostics, scalarPayloadDiagnostic("/reason", "replacement reason is required", models.FieldValueClassMissing))
	}
	replacement, present := object["replacement"]
	if !present {
		diagnostics = append(diagnostics, scalarPayloadDiagnostic("/replacement", "replacement is required", models.FieldValueClassMissing))
	} else {
		for _, diagnostic := range validateAddTaskPayload(replacement) {
			diagnostic.Field = "/replacement" + strings.TrimSuffix(diagnostic.Field, "/")
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	replacementObject, _ := replacement.(map[string]any)
	replacementID, _ := replacementObject["id"].(string)
	if replacementID != "" && replacementID == input.SourceTaskID {
		diagnostics = append(diagnostics, scalarPayloadDiagnostic("/replacement/id", "replacement must differ from source", models.FieldValueClassConflict))
	}
	if len(input.Consumers) > 0 {
		// Reuse declarative dependency-list shape validation, but never expose
		// its value-bearing prose. Empty consumer batches are valid here.
		for _, diagnostic := range ValidateMarkBlockedDependencyUpdates(input.Consumers) {
			diagnostic.Field = strings.Replace(diagnostic.Field, "/repair_request/dependency_updates", "/consumers", 1)
			diagnostic.Constraint = "consumers must be unique tasks with explicit lists of nonempty, unique dependency IDs"
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	for i, consumer := range input.Consumers {
		field := fmt.Sprintf("/consumers/%d/task_id", i)
		diagnostics = append(diagnostics, taskIDDiagnostics(field, consumer.TaskID)...)
		if consumer.TaskID != "" && (consumer.TaskID == input.SourceTaskID || consumer.TaskID == replacementID) {
			diagnostics = append(diagnostics, scalarPayloadDiagnostic(field, "consumers must differ from source and replacement", models.FieldValueClassConflict))
		}
	}
	if base := input.PreservedBase; base != nil {
		for _, field := range []struct{ key, value string }{{"base_commit", base.BaseCommit}, {"worktree", base.Worktree}} {
			if field.value == "" {
				diagnostics = append(diagnostics, scalarPayloadDiagnostic("/preserved_base/"+field.key, "preserved base requires both base_commit and worktree", models.FieldValueClassMissing))
			}
		}
	}
	return diagnostics
}
