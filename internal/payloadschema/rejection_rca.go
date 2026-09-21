package payloadschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// Operation names of the two rejection-RCA lifecycle operations on the
// registry. Their canonical objects are the request types a caller may send —
// never the stored record, whose remaining fields the gate seeds or the
// operation derives.
const (
	RecordRejectionRCAOperation = "record-rejection-rca"
	ResumeRejectionRCAOperation = "resume-rejection-rca"
)

const (
	rejectionRCAWrongTypeConstraint  = "must have the type the request declares"
	rejectionRCAUnknownKeyConstraint = "is not a request field; gate-seeded and derived fields cannot be supplied"
)

// The keys each canonical object may carry, read from the request types'
// own JSON tags so a field added to models is accepted here without a second
// list to keep in step.
var (
	rejectionRCARequestKeys      = jsonKeys(reflect.TypeFor[models.RejectionRCARequest]())
	rejectionRCAContributionKeys = jsonKeys(reflect.TypeFor[models.RejectionRCAContribution]())
	rejectionRCADispositionKeys  = jsonKeys(reflect.TypeFor[models.RejectionRCADispositionRequest]())
)

func init() {
	Register(Schema{Operation: RecordRejectionRCAOperation, Version: models.RejectionRCASchemaVersion, Validate: validateRecordRejectionRCA})
	Register(Schema{Operation: ResumeRejectionRCAOperation, Version: models.RejectionRCASchemaVersion, Validate: validateResumeRejectionRCA})
}

// validateRecordRejectionRCA decodes the canonical object into
// models.RejectionRCARequest and delegates every structural rule to
// statevalidate.ValidateRejectionRCARequest. The rejection_index bound that
// needs the task's durable rejection count is a precondition of the
// operation, not a diagnostic.
func validateRecordRejectionRCA(payload any) []models.FieldDiagnostic {
	request, isTyped := payload.(models.RejectionRCARequest)
	if !isTyped {
		object, diagnostics := rejectionRCAObject(payload)
		if diagnostics != nil {
			return diagnostics
		}
		request, diagnostics = decodeRejectionRCARequest(object)
		if diagnostics != nil {
			return diagnostics
		}
	}
	return statevalidate.ValidateRejectionRCARequest(request)
}

// validateResumeRejectionRCA decodes the canonical object into
// models.RejectionRCADispositionRequest and delegates to
// statevalidate.ValidateRejectionRCADispositionRequest.
func validateResumeRejectionRCA(payload any) []models.FieldDiagnostic {
	request, isTyped := payload.(models.RejectionRCADispositionRequest)
	if !isTyped {
		object, diagnostics := rejectionRCAObject(payload)
		if diagnostics != nil {
			return diagnostics
		}
		diagnostics = rejectionRCAUnknownKeys("", object, rejectionRCADispositionKeys)
		if rejected := decodeRejectionRCAValue("", object, &request); rejected != nil {
			diagnostics = append(diagnostics, *rejected)
		}
		if diagnostics != nil {
			return diagnostics
		}
	}
	return statevalidate.ValidateRejectionRCADispositionRequest(request)
}

// decodeRejectionRCARequest decodes the request in two stages, because the
// JSON decoder names a mistyped member of a list element without its index:
// the scalar members first, then each contribution on its own so the
// diagnostic path keeps the element index.
func decodeRejectionRCARequest(object map[string]any) (models.RejectionRCARequest, []models.FieldDiagnostic) {
	diagnostics := rejectionRCAUnknownKeys("", object, rejectionRCARequestKeys)
	if entries, isList := object["contributions"].([]any); isList {
		for i, entry := range entries {
			if member, isObject := entry.(map[string]any); isObject {
				diagnostics = append(diagnostics, rejectionRCAUnknownKeys(fmt.Sprintf("/contributions/%d", i), member, rejectionRCAContributionKeys)...)
			}
		}
	}

	var envelope struct {
		SchemaVersion int               `json:"schema_version"`
		Summary       string            `json:"summary"`
		Contributions []json.RawMessage `json:"contributions"`
	}
	if rejected := decodeRejectionRCAValue("", object, &envelope); rejected != nil {
		return models.RejectionRCARequest{}, append(diagnostics, *rejected)
	}

	request := models.RejectionRCARequest{SchemaVersion: envelope.SchemaVersion, Summary: envelope.Summary}
	for i, raw := range envelope.Contributions {
		var contribution models.RejectionRCAContribution
		if rejected := decodeRejectionRCAValue(fmt.Sprintf("/contributions/%d", i), raw, &contribution); rejected != nil {
			diagnostics = append(diagnostics, *rejected)
			continue
		}
		request.Contributions = append(request.Contributions, contribution)
	}
	if diagnostics != nil {
		return models.RejectionRCARequest{}, diagnostics
	}
	return request, nil
}

// rejectionRCAObject returns the canonical object, or the one diagnostic that
// says the payload is not an object at all.
func rejectionRCAObject(payload any) (map[string]any, []models.FieldDiagnostic) {
	if object, isObject := payload.(map[string]any); isObject {
		return object, nil
	}
	valueClass := models.FieldValueClassWrongType
	if payload == nil {
		valueClass = models.FieldValueClassNull
	}
	return nil, []models.FieldDiagnostic{rejectionRCADiagnostic("/", rejectionRCAWrongTypeConstraint, valueClass)}
}

// rejectionRCAUnknownKeys reports every key of object that the request type
// does not define, in key order so the verdict is deterministic.
func rejectionRCAUnknownKeys(prefix string, object map[string]any, known []string) []models.FieldDiagnostic {
	var diagnostics []models.FieldDiagnostic
	for _, key := range slices.Sorted(maps.Keys(object)) {
		if !slices.Contains(known, key) {
			diagnostics = append(diagnostics, rejectionRCADiagnostic(prefix+"/"+key, rejectionRCAUnknownKeyConstraint, models.FieldValueClassWrongType))
		}
	}
	return diagnostics
}

// decodeRejectionRCAValue round-trips one JSON value into its request type and
// maps the decoder's first type error onto the field path under prefix. The
// decoder's message can quote the rejected value, so only the field it names
// crosses into a diagnostic.
func decodeRejectionRCAValue(prefix string, value, target any) *models.FieldDiagnostic {
	field := prefix
	if field == "" {
		field = "/"
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		rejected := rejectionRCADiagnostic(field, rejectionRCAWrongTypeConstraint, models.FieldValueClassWrongType)
		return &rejected
	}
	err = json.Unmarshal(encoded, target)
	if err == nil {
		return nil
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		field = prefix + "/" + strings.ReplaceAll(typeErr.Field, ".", "/")
	}
	rejected := rejectionRCADiagnostic(field, rejectionRCAWrongTypeConstraint, models.FieldValueClassWrongType)
	return &rejected
}

func rejectionRCADiagnostic(field, constraint, valueClass string) models.FieldDiagnostic {
	return models.FieldDiagnostic{
		Field:      field,
		Constraint: constraint,
		ValueClass: valueClass,
		SafeAction: models.FieldDiagnosticCorrectInput,
	}
}

// jsonKeys lists the JSON member names a struct type declares.
func jsonKeys(structType reflect.Type) []string {
	keys := make([]string, 0, structType.NumField())
	for i := range structType.NumField() {
		name, _, _ := strings.Cut(structType.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			keys = append(keys, name)
		}
	}
	return keys
}
