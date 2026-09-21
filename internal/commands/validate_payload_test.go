package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/payloadschema"
)

// The schemas of the four lifecycle commands are registered by sibling tasks,
// so these fixtures give the preflight something to validate against without
// coupling this file to any one command's rules. Their names cannot collide
// with a real operation.
const (
	preflightFixtureOperation     = "commands-preflight-fixture"
	preflightFixtureVersion       = 3
	preflightLiftFixtureOperation = "commands-preflight-lift-fixture"
	preflightLiftFixtureKey       = "entries"
	// A rejected value the schema sees but must never place in a diagnostic.
	preflightFixtureSecret = "s3cr3t-token-value"
)

var preflightFixtureCalls atomic.Int64

func init() {
	payloadschema.Register(payloadschema.Schema{
		Operation: preflightFixtureOperation,
		Version:   preflightFixtureVersion,
		Validate: func(payload any) []models.FieldDiagnostic {
			preflightFixtureCalls.Add(1)
			object, isObject := payload.(map[string]any)
			if !isObject {
				return []models.FieldDiagnostic{{
					Field: "", Constraint: "must be a JSON object",
					ValueClass: models.FieldValueClassWrongType, SafeAction: models.FieldDiagnosticCorrectInput,
				}}
			}
			if _, present := object["token"]; !present {
				return nil
			}
			return []models.FieldDiagnostic{{
				Field: "/token", Constraint: "must be absent",
				ValueClass: models.FieldValueClassMalformed, SafeAction: models.FieldDiagnosticCorrectInput,
			}}
		},
	})
	payloadschema.Register(payloadschema.Schema{
		Operation: preflightLiftFixtureOperation,
		Version:   1,
		Validate: func(payload any) []models.FieldDiagnostic {
			object, isObject := payload.(map[string]any)
			if !isObject || object[preflightLiftFixtureKey] == nil {
				return []models.FieldDiagnostic{{
					Field: "/" + preflightLiftFixtureKey, Constraint: "is required",
					ValueClass: models.FieldValueClassMissing, SafeAction: models.FieldDiagnosticCorrectInput,
				}}
			}
			return nil
		},
	})
}

// liftFixturePayloadKey makes the lift fixture look like a single-file-flag
// command for the duration of one test, without inventing a schema for a real
// operation this task does not own.
func liftFixturePayloadKey(t *testing.T) {
	t.Helper()
	singleFilePayloadKeys[preflightLiftFixtureOperation] = preflightLiftFixtureKey
	t.Cleanup(func() { delete(singleFilePayloadKeys, preflightLiftFixtureOperation) })
}

func writePayloadFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write payload file: %v", err)
	}
	return path
}

// lifecycleOutcomeOf asserts an error carries a lifecycle outcome and returns it.
func lifecycleOutcomeOf(t *testing.T, err error) models.LifecycleOutcome {
	t.Helper()
	if err == nil {
		t.Fatal("expected a lifecycle error, got nil")
	}
	var lifecycle *ops.LifecycleError
	if !errors.As(err, &lifecycle) {
		t.Fatalf("error is not a lifecycle error: %v", err)
	}
	return lifecycle.Outcome
}

func assertRejected(t *testing.T, err error) []models.FieldDiagnostic {
	t.Helper()
	outcome := lifecycleOutcomeOf(t, err)
	if outcome.Operation != ValidatePayloadOperation {
		t.Fatalf("operation = %q, want %q", outcome.Operation, ValidatePayloadOperation)
	}
	if outcome.Outcome != models.LifecycleInvalidInput || outcome.SafeAction != models.FieldDiagnosticCorrectInput || outcome.Effects != "none" {
		t.Fatalf("outcome = %s/%s/%s, want INVALID_INPUT/correct_input/none", outcome.Outcome, outcome.SafeAction, outcome.Effects)
	}
	if len(outcome.Diagnostics) == 0 {
		t.Fatal("rejection carries no diagnostics")
	}
	return outcome.Diagnostics
}

func TestValidatePayloadOutcome(t *testing.T) {
	t.Run("valid payload completes with its schema version", func(t *testing.T) {
		before := preflightFixtureCalls.Load()

		result, err := ValidatePayload(preflightFixtureOperation, map[string]any{"field": "value"})
		if err != nil {
			t.Fatalf("valid payload rejected: %v", err)
		}
		if result.Outcome != models.LifecycleCompleted || result.SafeAction != "continue" || result.Effects != "none" {
			t.Fatalf("outcome = %s/%s/%s, want COMPLETED/continue/none", result.Outcome, result.SafeAction, result.Effects)
		}
		if result.SchemaVersion != preflightFixtureVersion {
			t.Fatalf("schema_version = %d, want %d", result.SchemaVersion, preflightFixtureVersion)
		}
		if len(result.Diagnostics) != 0 {
			t.Fatalf("valid payload carries diagnostics: %v", result.Diagnostics)
		}
		// The preflight commits nothing, so it asserts no durable-state change.
		if result.Changed != nil {
			t.Fatalf("changed = %v, want absent", *result.Changed)
		}
		if result.TaskID != "" {
			t.Fatalf("task_id = %q, want empty: the preflight has no task boundary", result.TaskID)
		}
		if calls := preflightFixtureCalls.Load() - before; calls != 1 {
			t.Fatalf("schema validated %d times, want exactly 1", calls)
		}
	})

	t.Run("invalid payload names the field without its value", func(t *testing.T) {
		before := preflightFixtureCalls.Load()

		_, err := ValidatePayload(preflightFixtureOperation, map[string]any{"token": preflightFixtureSecret})
		diagnostics := assertRejected(t, err)

		if len(diagnostics) != 1 {
			t.Fatalf("diagnostics = %d, want 1", len(diagnostics))
		}
		diagnostic := diagnostics[0]
		if diagnostic.SchemaVersion != preflightFixtureVersion {
			t.Fatalf("schema_version = %d, want %d", diagnostic.SchemaVersion, preflightFixtureVersion)
		}
		if diagnostic.Field != "/token" || diagnostic.Constraint == "" ||
			diagnostic.ValueClass != models.FieldValueClassMalformed || diagnostic.SafeAction != models.FieldDiagnosticCorrectInput {
			t.Fatalf("diagnostic = %+v, want the field path, constraint, value class and safe action", diagnostic)
		}
		if calls := preflightFixtureCalls.Load() - before; calls != 1 {
			t.Fatalf("schema validated %d times, want exactly 1", calls)
		}

		// The rejected value must not survive anywhere in the rendered envelope.
		var envelope bytes.Buffer
		if writeErr := jsonout.WriteResult(&envelope, nil, nil, err); !errors.Is(writeErr, jsonout.ErrAlreadyWritten) {
			t.Fatalf("envelope write = %v, want ErrAlreadyWritten", writeErr)
		}
		if strings.Contains(envelope.String(), preflightFixtureSecret) {
			t.Fatalf("rejected value echoed in the envelope: %s", envelope.String())
		}
		if !strings.Contains(envelope.String(), `"diagnostics"`) {
			t.Fatalf("envelope carries no diagnostics: %s", envelope.String())
		}
	})

	t.Run("unregistered operation names the operation argument", func(t *testing.T) {
		_, err := ValidatePayload("no-such-operation", map[string]any{})
		diagnostics := assertRejected(t, err)

		if len(diagnostics) != 1 || diagnostics[0].Field != "operation" {
			t.Fatalf("diagnostics = %+v, want one entry on the operation argument", diagnostics)
		}
		if !strings.Contains(diagnostics[0].Constraint, "--list") {
			t.Fatalf("constraint = %q, want it to point at --list", diagnostics[0].Constraint)
		}
		if diagnostics[0].ValueClass != models.FieldValueClassUnknownEnum {
			t.Fatalf("value_class = %q, want %q", diagnostics[0].ValueClass, models.FieldValueClassUnknownEnum)
		}
		if !errors.Is(err, payloadschema.ErrUnknownOperation) {
			t.Fatalf("cause = %v, want ErrUnknownOperation", err)
		}
	})

	t.Run("bare document validates as its lifted canonical object", func(t *testing.T) {
		liftFixturePayloadKey(t)

		fromFile, err := ValidatePayloadFile(preflightLiftFixtureOperation, writePayloadFile(t, `[{"desc":"first"}]`))
		if err != nil {
			t.Fatalf("bare document rejected: %v", err)
		}
		fromCanonical, err := ValidatePayload(preflightLiftFixtureOperation,
			map[string]any{preflightLiftFixtureKey: []any{map[string]any{"desc": "first"}}})
		if err != nil {
			t.Fatalf("canonical object rejected: %v", err)
		}
		if !reflect.DeepEqual(fromFile, fromCanonical) {
			t.Fatalf("bare document result %+v differs from canonical object result %+v", fromFile, fromCanonical)
		}

		_, err = ValidatePayloadFile(preflightLiftFixtureOperation, writePayloadFile(t, `{"other":1}`))
		diagnostics := assertRejected(t, err)
		if !strings.Contains(diagnostics[0].Constraint, preflightLiftFixtureKey) ||
			!strings.Contains(diagnostics[0].Constraint, "bare array") {
			t.Fatalf("diagnostics = %+v, want the wrapper name and bare array correction", diagnostics)
		}
	})

	t.Run("single file rejects wrapped manifest and accepts bare array", func(t *testing.T) {
		const bare = `[{"desc":"first","done_when":"verified","scope":"one file","spec_ref":"specs/task.md"}]`
		const wrapped = `{"output":` + bare + `}`

		_, err := ValidatePayloadFile("set-task-output", writePayloadFile(t, wrapped))
		diagnostics := assertRejected(t, err)
		if len(diagnostics) != 1 || diagnostics[0].Field != "payload" ||
			diagnostics[0].ValueClass != models.FieldValueClassWrongType ||
			diagnostics[0].SafeAction != models.FieldDiagnosticCorrectInput {
			t.Fatalf("diagnostics = %+v, want one wrong-type payload diagnostic with correct_input", diagnostics)
		}
		if !strings.Contains(diagnostics[0].Constraint, `"output" wrapper`) ||
			!strings.Contains(diagnostics[0].Constraint, "pass a bare array") {
			t.Fatalf("constraint = %q, want the wrapper name and bare array correction", diagnostics[0].Constraint)
		}

		fromFile, err := ValidatePayloadFile("set-task-output", writePayloadFile(t, bare))
		if err != nil {
			t.Fatalf("bare manifest rejected: %v", err)
		}
		if fromFile.Outcome != models.LifecycleCompleted || fromFile.SafeAction != "continue" ||
			fromFile.Effects != "none" || fromFile.SchemaVersion != 1 {
			t.Fatalf("bare manifest result = %+v, want COMPLETED/continue/none with schema version 1", fromFile)
		}

		var canonical map[string]any
		if err := json.Unmarshal([]byte(wrapped), &canonical); err != nil {
			t.Fatal(err)
		}
		fromCanonical, err := ValidatePayload("set-task-output", canonical)
		if err != nil {
			t.Fatalf("canonical API payload rejected: %v", err)
		}
		if !reflect.DeepEqual(fromFile, fromCanonical) {
			t.Fatalf("bare result %+v differs from canonical API result %+v", fromFile, fromCanonical)
		}
	})

	t.Run("unreadable and malformed payload files name the payload argument", func(t *testing.T) {
		for name, path := range map[string]string{
			"missing":   filepath.Join(t.TempDir(), "absent.json"),
			"malformed": writePayloadFile(t, "{not json"),
		} {
			t.Run(name, func(t *testing.T) {
				_, err := ValidatePayloadFile(preflightFixtureOperation, path)
				diagnostics := assertRejected(t, err)
				if len(diagnostics) != 1 || diagnostics[0].Field != "payload" {
					t.Fatalf("diagnostics = %+v, want one entry on the payload argument", diagnostics)
				}
			})
		}
	})
}

func TestValidatePayloadList(t *testing.T) {
	listed := ValidatePayloadSchemas()

	// Asserted against the registry itself: schemas registered by sibling
	// tasks must extend these rows, not break them.
	if !reflect.DeepEqual(listed.Schemas, payloadschema.List()) {
		t.Fatalf("listed schemas %+v differ from the registry %+v", listed.Schemas, payloadschema.List())
	}
	registered := map[string]int{}
	for _, descriptor := range listed.Schemas {
		registered[descriptor.Operation] = descriptor.Version
	}
	if registered[preflightFixtureOperation] != preflightFixtureVersion {
		t.Fatalf("fixture schema missing from %+v", listed.Schemas)
	}

	rendered, err := json.Marshal(listed)
	if err != nil {
		t.Fatalf("marshal list result: %v", err)
	}
	if !strings.Contains(string(rendered), `"operation"`) || !strings.Contains(string(rendered), `"version"`) {
		t.Fatalf("list rows lack operation/version keys: %s", rendered)
	}

	var text bytes.Buffer
	WriteValidatePayloadSchemas(&text, listed)
	for _, descriptor := range listed.Schemas {
		if !strings.Contains(text.String(), descriptor.Operation) {
			t.Fatalf("text listing omits %q: %s", descriptor.Operation, text.String())
		}
	}
}
