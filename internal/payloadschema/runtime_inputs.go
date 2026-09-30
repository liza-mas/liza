package payloadschema

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/liza-mas/liza/internal/models"
)

const runtimeInputUnknownKeyConstraint = "is not a runtime_inputs field; allowed: id, commands, recipe, consumption, secret, env, files, after"

// runtimeInputPayloadDiagnostics checks one carrier's declarations: every
// element's keys against the closed set, so an unsupported key such as
// "binding" is refused rather than silently dropped by the typed decode, and
// then the structural rules. raw is the undecoded runtime_inputs value, or nil
// when only a typed value is available.
func runtimeInputPayloadDiagnostics(prefix string, raw any, validation []string, inputs []models.RuntimeInput) []models.FieldDiagnostic {
	var diagnostics []models.FieldDiagnostic
	if elements, ok := raw.([]any); ok {
		for i, element := range elements {
			object, isObject := element.(map[string]any)
			if !isObject {
				continue // The typed decode already reported the wrong type.
			}
			for _, key := range slices.Sorted(maps.Keys(object)) {
				if !slices.Contains(models.RuntimeInputKeys, key) {
					diagnostics = append(diagnostics, models.FieldDiagnostic{
						Field:      fmt.Sprintf("%s/runtime_inputs/%d/%s", prefix, i, key),
						Constraint: runtimeInputUnknownKeyConstraint,
						ValueClass: models.FieldValueClassWrongType,
						SafeAction: models.FieldDiagnosticCorrectInput,
					})
				}
			}
		}
	}
	if err := models.ValidateRuntimeInputs(validation, inputs); err != nil {
		diagnostics = append(diagnostics, models.FieldDiagnostic{
			Field:      prefix + "/runtime_inputs",
			Constraint: err.Error(),
			ValueClass: models.FieldValueClassMalformed,
			SafeAction: models.FieldDiagnosticCorrectInput,
		})
	}
	return diagnostics
}

// decodeSetTaskOutputRawEntries returns the manifest's entries as generic
// objects, for checks the typed decode cannot express. Entries that are not
// objects decode as nil; structural errors were already reported.
func decodeSetTaskOutputRawEntries(payload any) []map[string]any {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	var manifest struct {
		Output []json.RawMessage `json:"output"`
	}
	if json.Unmarshal(encoded, &manifest) != nil {
		return nil
	}
	entries := make([]map[string]any, len(manifest.Output))
	for i, raw := range manifest.Output {
		_ = json.Unmarshal(raw, &entries[i])
	}
	return entries
}
