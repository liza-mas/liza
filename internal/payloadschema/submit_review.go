package payloadschema

import (
	"encoding/hex"
	"fmt"

	"github.com/liza-mas/liza/internal/models"
)

const submitForReviewOperation = "submit-for-review"

// payloadRootField names the payload itself, for a document that is not the
// operation's canonical object at all.
const payloadRootField = "payload"

// commitRefHexConstraint explains the one narrowing this schema applies: a ref
// of full object-ID length is a SHA-1 or SHA-256 object ID, so a value of that
// length carrying a non-hexadecimal character cannot resolve to a commit.
const commitRefHexConstraint = "a 40- or 64-character commit ref is a Git object ID and must be hexadecimal"

// Git object-ID lengths, mirroring the submission boundary's own definition of
// a full SHA (ops.fullSubmissionSHA): SHA-1 and SHA-256 hex digests. Shorter
// abbreviated refs and symbolic refs stay valid input.
const (
	sha1HexLength   = 40
	sha256HexLength = 64
)

func init() {
	Register(Schema{Operation: submitForReviewOperation, Version: 1, Validate: validateSubmitForReviewPayload})
}

// validateSubmitForReviewPayload checks the canonical object {"commit_ref"}.
// Resolving the ref against the worktree stays at the mutation boundary: it
// needs Git, which no schema may touch.
func validateSubmitForReviewPayload(payload any) []models.FieldDiagnostic {
	object, notAnObject := scalarPayloadObject(payload)
	if notAnObject != nil {
		return []models.FieldDiagnostic{*notAnObject}
	}
	if rejected := scalarPayloadString(object, "commit_ref", true); rejected != nil {
		return []models.FieldDiagnostic{*rejected}
	}

	commitRef, _ := object["commit_ref"].(string)
	if len(commitRef) != sha1HexLength && len(commitRef) != sha256HexLength {
		return nil
	}
	if _, err := hex.DecodeString(commitRef); err != nil {
		return []models.FieldDiagnostic{scalarPayloadDiagnostic("/commit_ref", commitRefHexConstraint, models.FieldValueClassMalformed)}
	}
	return nil
}

// The helpers below are shared by the scalar-payload schemas in this package's
// sibling files (submit-for-review and handoff). They read the canonical
// object and nothing else: no state, no lock, no Git.

// scalarPayloadObject returns the canonical object, or the one diagnostic that
// says the payload is not an object at all.
func scalarPayloadObject(payload any) (map[string]any, *models.FieldDiagnostic) {
	if object, isObject := payload.(map[string]any); isObject {
		return object, nil
	}
	valueClass := models.FieldValueClassWrongType
	if payload == nil {
		valueClass = models.FieldValueClassNull
	}
	rejected := scalarPayloadDiagnostic(payloadRootField, "must be a JSON object", valueClass)
	return nil, &rejected
}

// scalarPayloadString checks one string field. A required field must be
// present, non-null and non-empty; an optional one may be absent or null but
// must be a string when supplied.
func scalarPayloadString(object map[string]any, key string, required bool) *models.FieldDiagnostic {
	field := "/" + key
	value, present := object[key]
	switch {
	case !present:
		if !required {
			return nil
		}
		rejected := scalarPayloadDiagnostic(field, "is required", models.FieldValueClassMissing)
		return &rejected
	case value == nil:
		if !required {
			return nil
		}
		rejected := scalarPayloadDiagnostic(field, "must not be null", models.FieldValueClassNull)
		return &rejected
	}

	text, isString := value.(string)
	switch {
	case !isString:
		rejected := scalarPayloadDiagnostic(field, "must be a string", models.FieldValueClassWrongType)
		return &rejected
	case required && text == "":
		rejected := scalarPayloadDiagnostic(field, "must not be empty", models.FieldValueClassMissing)
		return &rejected
	}
	return nil
}

// scalarPayloadStringList checks one optional list of non-empty strings,
// reporting every rejected entry by index rather than only the first.
func scalarPayloadStringList(object map[string]any, key string) []models.FieldDiagnostic {
	field := "/" + key
	value, present := object[key]
	if !present || value == nil {
		return nil
	}
	entries, isList := scalarPayloadEntries(value)
	if !isList {
		return []models.FieldDiagnostic{scalarPayloadDiagnostic(field, "must be a list of non-empty strings", models.FieldValueClassWrongType)}
	}

	var diagnostics []models.FieldDiagnostic
	for i, entry := range entries {
		entryField := fmt.Sprintf("%s/%d", field, i)
		text, isString := entry.(string)
		switch {
		case !isString:
			valueClass := models.FieldValueClassWrongType
			if entry == nil {
				valueClass = models.FieldValueClassNull
			}
			diagnostics = append(diagnostics, scalarPayloadDiagnostic(entryField, "must be a string", valueClass))
		case text == "":
			diagnostics = append(diagnostics, scalarPayloadDiagnostic(entryField, "must not be empty", models.FieldValueClassMissing))
		}
	}
	return diagnostics
}

// scalarPayloadEntries accepts both representations of the same list: the
// []any a decoded JSON document carries, and the []string a Go caller builds
// at the mutation boundary. Both must reach the same verdict.
func scalarPayloadEntries(value any) ([]any, bool) {
	switch typed := value.(type) {
	case []any:
		return typed, true
	case []string:
		entries := make([]any, len(typed))
		for i, entry := range typed {
			entries[i] = entry
		}
		return entries, true
	}
	return nil, false
}

// scalarPayloadDiagnostic names a field and its constraint. The rejected value
// never appears: only its class crosses into agent-facing output.
func scalarPayloadDiagnostic(field, constraint, valueClass string) models.FieldDiagnostic {
	return models.FieldDiagnostic{
		Field:      field,
		Constraint: constraint,
		ValueClass: valueClass,
		SafeAction: models.FieldDiagnosticCorrectInput,
	}
}
