package payloadschema

import "github.com/liza-mas/liza/internal/models"

const handoffOperation = "handoff"

// handoffRequiredFields are the two fields the next agent cannot start without.
var handoffRequiredFields = []string{"summary", "next_action"}

// handoffStringListFields are the optional structured fields of a handoff. Each
// is a list whose entries are read by the next agent, so a blank entry is a
// line of nothing rather than an omission.
var handoffStringListFields = []string{"succeeded", "failed", "key_files", "dead_ends"}

func init() {
	Register(Schema{Operation: handoffOperation, Version: 1, Validate: validateHandoffPayload})
}

// validateHandoffPayload checks the canonical object
// {"summary", "next_action", "succeeded", "failed", "hypothesis", "key_files",
// "dead_ends"}. Task ownership and executing status are read from state and
// stay at the mutation boundary.
func validateHandoffPayload(payload any) []models.FieldDiagnostic {
	object, notAnObject := scalarPayloadObject(payload)
	if notAnObject != nil {
		return []models.FieldDiagnostic{*notAnObject}
	}

	var diagnostics []models.FieldDiagnostic
	for _, field := range handoffRequiredFields {
		if rejected := scalarPayloadString(object, field, true); rejected != nil {
			diagnostics = append(diagnostics, *rejected)
		}
	}
	if rejected := scalarPayloadString(object, "hypothesis", false); rejected != nil {
		diagnostics = append(diagnostics, *rejected)
	}
	for _, field := range handoffStringListFields {
		diagnostics = append(diagnostics, scalarPayloadStringList(object, field)...)
	}
	return diagnostics
}
