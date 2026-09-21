package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/payloadschema"
)

// ValidatePayloadOperation names the preflight on the lifecycle operation and
// counter registers. It is deliberately state-free and not RBAC-gated.
const ValidatePayloadOperation = "validate-payload"

// ValidatePayloadResult is one preflight observation: a lifecycle outcome with
// no task boundary, plus the schema version the payload was checked against.
type ValidatePayloadResult struct {
	models.LifecycleOutcome
	SchemaVersion int `json:"schema_version" yaml:"schema_version"`
}

// ValidatePayloadListResult reports which operations can be preflighted, so a
// caller discovers schema versions instead of guessing an operation name.
type ValidatePayloadListResult struct {
	Schemas []payloadschema.Descriptor `json:"schemas" yaml:"schemas"`
}

// Operations whose agent-facing input is one file holding a bare document
// rather than the canonical object, mapped to the canonical key that document
// belongs under. The preflight accepts exactly the file the mutation command
// reads, so no second representation exists for the agent to get wrong.
var singleFilePayloadKeys = map[string]string{
	"set-task-output": "output",
}

// ValidatePayloadSchemas lists every registered schema, sorted by operation.
func ValidatePayloadSchemas() ValidatePayloadListResult {
	return ValidatePayloadListResult{Schemas: payloadschema.List()}
}

// ValidatePayload checks one operation's canonical object against the same
// schema its mutation boundary uses, without reading state, taking a lock or
// touching Git. A structural rejection is an INVALID_INPUT lifecycle error
// whose diagnostics name the offending fields and never their values.
func ValidatePayload(operation string, payload any) (*ValidatePayloadResult, error) {
	version, diagnostics, err := payloadschema.Validate(operation, payload)
	switch {
	case errors.Is(err, payloadschema.ErrUnknownOperation):
		return nil, ops.NewLifecycleInvalidInputError(ValidatePayloadOperation, nil, []models.FieldDiagnostic{{
			Field:      "operation",
			Constraint: "must name a registered payload schema; run --list to see them",
			ValueClass: models.FieldValueClassUnknownEnum,
			SafeAction: models.FieldDiagnosticCorrectInput,
		}}, err)
	case err != nil:
		// The registry classifies nothing else, so this invocation reached no
		// verdict about the payload and must not imply one.
		return nil, ops.WrapLifecycleError(ValidatePayloadOperation, nil, err, models.LifecycleStateChanged, "requery", "none")
	case len(diagnostics) > 0:
		return nil, ops.NewLifecycleInvalidInputError(ValidatePayloadOperation, nil, diagnostics,
			fmt.Errorf("payload rejected by the %s schema, version %d", operation, version))
	}

	outcome := ops.NewLifecycleOutcome(ValidatePayloadOperation, nil, models.LifecycleCompleted, "continue", "none")
	// A preflight commits nothing, so it claims no durable-state change.
	outcome.Changed = nil
	return &ValidatePayloadResult{LifecycleOutcome: outcome, SchemaVersion: version}, nil
}

// ValidatePayloadFile validates the file an agent passes to the operation's own
// command, lifting a bare single-file-flag document to its canonical key first.
func ValidatePayloadFile(operation, path string) (*ValidatePayloadResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, payloadArgumentError("must be a readable file", models.FieldValueClassMissing, err)
	}

	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		// The decoder's message can quote the rejected bytes, so only the
		// offset of the failure crosses into agent-facing output.
		var syntax *json.SyntaxError
		cause := errors.New("payload file is not valid JSON")
		if errors.As(err, &syntax) {
			cause = fmt.Errorf("payload file is not valid JSON at byte offset %d", syntax.Offset)
		}
		return nil, payloadArgumentError("must contain one JSON document", models.FieldValueClassMalformed, cause)
	}
	if key, lifts := singleFilePayloadKeys[operation]; lifts {
		if _, isObject := payload.(map[string]any); isObject {
			return nil, payloadArgumentError(
				fmt.Sprintf("got a JSON object; remove the %q wrapper and pass a bare array", key),
				models.FieldValueClassWrongType, errors.New("payload file must contain a bare document"))
		}
		payload = map[string]any{key: payload}
	}
	return ValidatePayload(operation, payload)
}

// WriteValidatePayloadResult renders an accepted payload's outcome. Diagnostics
// stay JSON-only, as the result contract prescribes for text mode.
func WriteValidatePayloadResult(w io.Writer, result ValidatePayloadResult) {
	WriteLifecycleOutcome(w, result.LifecycleOutcome)
	fmt.Fprintf(w, "schema_version: %d\n", result.SchemaVersion)
}

// WriteValidatePayloadSchemas renders one row per registered schema.
func WriteValidatePayloadSchemas(w io.Writer, listed ValidatePayloadListResult) {
	if len(listed.Schemas) == 0 {
		fmt.Fprintln(w, "No payload schemas are registered.")
		return
	}
	for _, descriptor := range listed.Schemas {
		fmt.Fprintf(w, "%s: version %d\n", descriptor.Operation, descriptor.Version)
	}
}

func payloadArgumentError(constraint, valueClass string, cause error) error {
	return ops.NewLifecycleInvalidInputError(ValidatePayloadOperation, nil, []models.FieldDiagnostic{{
		Field:      "payload",
		Constraint: constraint,
		ValueClass: valueClass,
		SafeAction: models.FieldDiagnosticCorrectInput,
	}}, cause)
}
