package payloadschema

import (
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// SubmitVerdictOperation names the verdict lifecycle operation on the registry.
const SubmitVerdictOperation = "submit-verdict"

// submitVerdictFields are the keys of the canonical object, mirroring the
// command's flag names. Every value is a string; which ones are required and
// how they constrain each other is the shared validator's rule, not this
// file's.
var submitVerdictFields = []string{"task_id", "verdict", "reason", "agent_id", "impact", "review_commit"}

func init() {
	Register(Schema{Operation: SubmitVerdictOperation, Version: 1, Validate: validateSubmitVerdictPayload})
}

// SubmitVerdictPayload builds the canonical flag-keyed object the mutation
// boundary validates, so the preflight and the command validate the same keys.
func SubmitVerdictPayload(taskID, verdict, reason, agentID, impact, reviewCommit string) map[string]any {
	return map[string]any{
		"task_id":       taskID,
		"verdict":       verdict,
		"reason":        reason,
		"agent_id":      agentID,
		"impact":        impact,
		"review_commit": reviewCommit,
	}
}

// validateSubmitVerdictPayload checks that the canonical object carries
// strings, then delegates every structural rule to
// statevalidate.ValidateVerdictPayloadShape. Authority, the review boundary
// and impact ordering need state and stay at the mutation boundary.
func validateSubmitVerdictPayload(payload any) []models.FieldDiagnostic {
	object, notAnObject := scalarPayloadObject(payload)
	if notAnObject != nil {
		return []models.FieldDiagnostic{*notAnObject}
	}

	var diagnostics []models.FieldDiagnostic
	for _, field := range submitVerdictFields {
		if rejected := scalarPayloadString(object, field, false); rejected != nil {
			diagnostics = append(diagnostics, *rejected)
		}
	}
	if diagnostics != nil {
		return diagnostics
	}

	text := func(field string) string {
		value, _ := object[field].(string)
		return value
	}
	return statevalidate.ValidateVerdictPayloadShape(
		text("task_id"), text("verdict"), text("reason"), text("agent_id"), text("impact"), text("review_commit"))
}
