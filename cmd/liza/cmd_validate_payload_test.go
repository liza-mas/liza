package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// The four lifecycle schemas are registered by sibling tasks, so the CLI test
// registers its own fixture. Its name cannot collide with a real operation.
const (
	cliPreflightOperation = "cli-preflight-fixture"
	cliPreflightVersion   = 2
)

func init() {
	payloadschema.Register(payloadschema.Schema{
		Operation: cliPreflightOperation,
		Version:   cliPreflightVersion,
		Validate: func(payload any) []models.FieldDiagnostic {
			object, isObject := payload.(map[string]any)
			if isObject && object["reason"] != nil {
				return nil
			}
			return []models.FieldDiagnostic{{
				Field: "/reason", Constraint: "is required",
				ValueClass: models.FieldValueClassMissing, SafeAction: models.FieldDiagnosticCorrectInput,
			}}
		},
	})
}

// setupValidatePayloadCLI returns a directory that is not a project: the
// preflight must work there, which is also how "no state.yaml is opened or
// created" is observed.
func setupValidatePayloadCLI(t *testing.T) string {
	t.Helper()
	resetValidatePayloadCLI(t)
	return t.TempDir()
}

func resetValidatePayloadCLI(t *testing.T) {
	t.Helper()
	resetRootCmdForTest(t)
	// No agent identity: the preflight is not RBAC-gated and must not resolve one.
	t.Setenv(brand.EnvName("AGENT_ID"), "")
	t.Setenv(brand.LegacyEnvName("AGENT_ID"), "")
	// The shared reset helper does not know this command's flags.
	t.Cleanup(func() {
		resetFlagIfPresent(validatePayloadCmd, "payload")
		resetFlagIfPresent(validatePayloadCmd, "list")
	})
}

func writeCLIPayload(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "payload.json")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write payload file: %v", err)
	}
	return path
}

func assertNoStateFile(t *testing.T, dir string) {
	t.Helper()
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Name() == "state.yaml" || info.Name() == paths.ProjectDirName() {
			t.Fatalf("preflight created project state at %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
}

func TestValidatePayloadCommand(t *testing.T) {
	t.Run("valid payload exits zero with ok:true", func(t *testing.T) {
		dir := setupValidatePayloadCLI(t)
		payload := writeCLIPayload(t, dir, `{"reason":"structural"}`)

		stdout, err := executeRootCommandCapture(t, dir, "validate-payload", cliPreflightOperation, "--payload", payload, "--json")
		if err != nil {
			t.Fatalf("valid payload failed: %v\n%s", err, stdout)
		}
		envelope := parseEnvelope(t, stdout)
		if envelope["ok"] != true {
			t.Fatalf("envelope not ok: %s", stdout)
		}
		result, isObject := envelope["result"].(map[string]any)
		if !isObject {
			t.Fatalf("missing result object: %s", stdout)
		}
		if result["outcome"] != models.LifecycleCompleted || result["safe_action"] != "continue" || result["effects"] != "none" {
			t.Fatalf("result = %v, want COMPLETED/continue/none", result)
		}
		if result["schema_version"] != float64(cliPreflightVersion) {
			t.Fatalf("schema_version = %v, want %d", result["schema_version"], cliPreflightVersion)
		}
		if _, present := result["diagnostics"]; present {
			t.Fatalf("valid payload carries diagnostics: %s", stdout)
		}
		assertNoStateFile(t, dir)
	})

	t.Run("invalid payload exits nonzero with result diagnostics", func(t *testing.T) {
		dir := setupValidatePayloadCLI(t)
		payload := writeCLIPayload(t, dir, `{"other":"value"}`)

		stdout, err := executeRootCommandCapture(t, dir, "validate-payload", cliPreflightOperation, "--payload", payload, "--json")
		if err == nil {
			t.Fatalf("invalid payload accepted: %s", stdout)
		}
		envelope := parseEnvelope(t, stdout)
		if envelope["ok"] != false {
			t.Fatalf("envelope not a failure: %s", stdout)
		}
		result, isObject := envelope["result"].(map[string]any)
		if !isObject {
			t.Fatalf("missing result object: %s", stdout)
		}
		if result["outcome"] != models.LifecycleInvalidInput || result["safe_action"] != models.FieldDiagnosticCorrectInput {
			t.Fatalf("result = %v, want INVALID_INPUT/correct_input", result)
		}
		diagnostics, isList := result["diagnostics"].([]any)
		if !isList || len(diagnostics) == 0 {
			t.Fatalf("missing result.diagnostics: %s", stdout)
		}
		diagnostic, isObject := diagnostics[0].(map[string]any)
		if !isObject || diagnostic["field"] != "/reason" || diagnostic["schema_version"] != float64(cliPreflightVersion) ||
			diagnostic["constraint"] == "" || diagnostic["value_class"] != models.FieldValueClassMissing {
			t.Fatalf("diagnostic = %v, want the schema version, field, constraint and value class", diagnostics[0])
		}
		assertNoStateFile(t, dir)
	})

	t.Run("unregistered operation names the operation argument", func(t *testing.T) {
		dir := setupValidatePayloadCLI(t)
		payload := writeCLIPayload(t, dir, `{"reason":"structural"}`)

		stdout, err := executeRootCommandCapture(t, dir, "validate-payload", "no-such-operation", "--payload", payload, "--json")
		if err == nil {
			t.Fatalf("unregistered operation accepted: %s", stdout)
		}
		result := parseEnvelope(t, stdout)["result"].(map[string]any)
		diagnostics := result["diagnostics"].([]any)
		if diagnostics[0].(map[string]any)["field"] != "operation" {
			t.Fatalf("diagnostic = %v, want the operation argument", diagnostics[0])
		}
	})

	t.Run("list reports the registered schemas", func(t *testing.T) {
		dir := setupValidatePayloadCLI(t)

		stdout, err := executeRootCommandCapture(t, dir, "validate-payload", "--list", "--json")
		if err != nil {
			t.Fatalf("--list failed: %v\n%s", err, stdout)
		}
		var envelope struct {
			OK     bool `json:"ok"`
			Result struct {
				Schemas []payloadschema.Descriptor `json:"schemas"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
			t.Fatalf("parse --list envelope: %v\n%s", err, stdout)
		}
		if !envelope.OK || !reflect.DeepEqual(envelope.Result.Schemas, payloadschema.List()) {
			t.Fatalf("--list rows %+v differ from the registry %+v", envelope.Result.Schemas, payloadschema.List())
		}
		assertNoStateFile(t, dir)
	})

	t.Run("text mode reports the outcome and schema version", func(t *testing.T) {
		dir := setupValidatePayloadCLI(t)
		payload := writeCLIPayload(t, dir, `{"reason":"structural"}`)

		stdout, err := executeRootCommandCapture(t, dir, "validate-payload", cliPreflightOperation, "--payload", payload)
		if err != nil {
			t.Fatalf("text mode failed: %v\n%s", err, stdout)
		}
		if !strings.Contains(stdout, "outcome: "+models.LifecycleCompleted) ||
			!strings.Contains(stdout, "schema_version: 2") {
			t.Fatalf("text output = %q, want the outcome and schema version", stdout)
		}
	})

	t.Run("counts both verdicts inside a project without touching state", func(t *testing.T) {
		root, statePath := setupMutationTestProject(t, nil)
		resetValidatePayloadCLI(t)
		payloads := t.TempDir()
		before, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := executeRootCommandCapture(t, root, "validate-payload", cliPreflightOperation,
			"--payload", writeCLIPayload(t, payloads, `{"reason":"structural"}`), "--json"); err != nil {
			t.Fatalf("valid payload failed inside a project: %v", err)
		}
		if _, err := executeRootCommandCapture(t, root, "validate-payload", cliPreflightOperation,
			"--payload", writeCLIPayload(t, payloads, `{"other":"value"}`), "--json"); err == nil {
			t.Fatal("invalid payload accepted inside a project")
		}

		after, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatal("preflight changed state")
		}
		// Preflight rejections stay separable from state conflicts.
		metrics := ops.ReadLifecycleOutcomes(root, ops.CaptureLifecycleSprint(readState(t, statePath).Sprint))
		counts := metrics.Counts[commands.ValidatePayloadOperation]
		if !metrics.Available || counts[models.LifecycleCompleted] != 1 || counts[models.LifecycleInvalidInput] != 1 {
			t.Fatalf("preflight outcomes not counted once each: %+v", metrics)
		}
	})

	t.Run("missing arguments are rejected before any file access", func(t *testing.T) {
		for name, args := range map[string][]string{
			"no operation":           {"validate-payload", "--json"},
			"no payload":             {"validate-payload", cliPreflightOperation, "--json"},
			"list with an operation": {"validate-payload", cliPreflightOperation, "--list", "--json"},
		} {
			t.Run(name, func(t *testing.T) {
				dir := setupValidatePayloadCLI(t)
				stdout, err := executeRootCommandCapture(t, dir, args...)
				if err == nil {
					t.Fatalf("accepted %v: %s", args, stdout)
				}
				if parseEnvelope(t, stdout)["ok"] != false {
					t.Fatalf("missing failure envelope: %s", stdout)
				}
			})
		}
	})
}

// The rejected and the accepted manifest differ in exactly one field, so
// correcting the diagnostic the preflight names is the only edit between the
// failing and the passing run.
const (
	e2eManifestMissingSpecRef = `[{"desc":"Implement the preflight","done_when":"the preflight test passes","scope":"cmd/liza","plan_ref":"specs/plan.md"}]`
	e2eManifestCorrected      = `[{"desc":"Implement the preflight","done_when":"the preflight test passes","scope":"cmd/liza","plan_ref":"specs/plan.md","spec_ref":"specs/vision.md"}]`
	e2eMissingSpecRefField    = "/output/0/spec_ref"
)

// Promoted from the generation-3 integration report's opt-in reproduction.
func TestIntegrationGlobal3NullManifestParity(t *testing.T) {
	for _, content := range []string{"null", "[]"} {
		t.Run(content, func(t *testing.T) {
			root, statePath := setupMutationTestProject(t, func(state *models.State) {
				state.Agents["code-planner-1"] = mutationTestAgent("code-planner")
				task := testhelpers.BuildTaskByStatus("output-plan", models.TaskStatusCodePlanning, time.Now().UTC())
				task.RolePair = "code-planning-pair"
				task.AssignedTo = testhelpers.StringPtr("code-planner-1")
				task.Output = []models.OutputEntry{{Desc: "Existing work", DoneWhen: "Implemented", Scope: "Example", SpecRef: "specs/vision.md"}}
				state.Tasks = []models.Task{task}
			})
			resetValidatePayloadCLI(t)
			manifest := writeCLIPayload(t, t.TempDir(), content)
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			var preflightDiagnostics any
			for _, args := range [][]string{
				{"validate-payload", "set-task-output", "--payload", manifest, "--json"},
				{"set-task-output", "output-plan", "--agent-id", "code-planner-1", "--output", manifest, "--json"},
			} {
				stdout, callErr := executeRootCommandCapture(t, root, args...)
				envelope := parseEnvelope(t, stdout)
				result, ok := envelope["result"].(map[string]any)
				if !ok {
					t.Fatalf("%s returned no lifecycle result: %s", args[0], stdout)
				}
				if content == "null" {
					if callErr == nil || envelope["ok"] != false || result["operation"] != args[0] ||
						result["outcome"] != models.LifecycleInvalidInput || result["safe_action"] != "correct_input" || result["effects"] != "none" {
						t.Fatalf("%s must reject null with INVALID_INPUT/correct_input and no effects: %s", args[0], stdout)
					}
					diagnostics, ok := result["diagnostics"].([]any)
					if !ok || len(diagnostics) != 1 {
						t.Fatalf("%s must report one missing output diagnostic: %s", args[0], stdout)
					}
					diagnostic, ok := diagnostics[0].(map[string]any)
					if !ok || diagnostic["field"] != "/output" || diagnostic["value_class"] != models.FieldValueClassMissing ||
						diagnostic["schema_version"] != float64(1) || diagnostic["safe_action"] != "correct_input" || diagnostic["constraint"] == "" {
						t.Fatalf("%s returned an incomplete null diagnostic: %s", args[0], stdout)
					}
					if args[0] == "validate-payload" {
						preflightDiagnostics = diagnostics
					} else if !reflect.DeepEqual(preflightDiagnostics, diagnostics) {
						t.Fatalf("null diagnostics disagree: preflight %v, mutation %v", preflightDiagnostics, diagnostics)
					}
				} else if callErr != nil || envelope["ok"] != true || result["outcome"] != models.LifecycleCompleted {
					t.Fatalf("%s must accept explicit []: error=%v response=%s", args[0], callErr, stdout)
				}
				if content == "null" || args[0] == "validate-payload" {
					after, err := os.ReadFile(statePath)
					if err != nil {
						t.Fatal(err)
					}
					// Includes existing output, task history and lifecycle boundary.
					if !bytes.Equal(before, after) {
						t.Fatalf("%s changed state for %s", args[0], content)
					}
				} else {
					if result["effects"] != "committed" || result["output_count"] != float64(0) {
						t.Fatalf("missing clearing receipt: %s", stdout)
					}
					task := mustFindTask(t, readState(t, statePath), "output-plan")
					if len(task.Output) != 0 {
						t.Fatalf("explicit [] did not clear existing output: %v", task.Output)
					}
					event := task.History[len(task.History)-1]
					if event.Event != "task_output_set" || event.Extra["previous_output_count"] != 1 || event.Extra["output_count"] != 0 {
						t.Fatalf("missing clearing history: %#v", event)
					}
				}
			}
		})
	}
}

// TestValidatePayloadPreflightE2E observes the preflight and the mutation from
// outside both: one file, two commands, and the state file read from disk
// around each call. A rejection that left no trace is only provable here,
// where the failed invocation is not the process doing the checking.
func TestValidatePayloadPreflightE2E(t *testing.T) {
	root, statePath := setupMutationTestProject(t, func(state *models.State) {
		state.Agents["code-planner-1"] = mutationTestAgent("code-planner")
		task := testhelpers.BuildTaskByStatus("output-plan", models.TaskStatusCodePlanning, time.Now().UTC())
		task.RolePair = "code-planning-pair"
		task.AssignedTo = testhelpers.StringPtr("code-planner-1")
		state.Tasks = []models.Task{task}
	})
	resetValidatePayloadCLI(t)

	schema, registered := payloadschema.Lookup(payloadschema.SetTaskOutputOperation)
	if !registered {
		t.Fatalf("%s has no registered schema", payloadschema.SetTaskOutputOperation)
	}
	// The manifest lives outside the project so writing it cannot be mistaken
	// for the state change under observation.
	manifest := filepath.Join(t.TempDir(), "outputs.json")
	writeManifest := func(content string) {
		t.Helper()
		if err := os.WriteFile(manifest, []byte(content), 0600); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
	}
	stateBytes := func() []byte {
		t.Helper()
		data, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatalf("read state: %v", err)
		}
		return data
	}
	preflightArgs := []string{"validate-payload", payloadschema.SetTaskOutputOperation, "--payload", manifest, "--json"}
	mutationArgs := []string{"set-task-output", "output-plan", "--agent-id", "code-planner-1", "--output", manifest, "--json"}

	// rejectManifest asserts one command's refusal and returns its diagnostics,
	// so the two boundaries can be compared as the caller sees them.
	rejectManifest := func(name string, args []string) []any {
		t.Helper()
		before := stateBytes()
		stdout, err := executeRootCommandCapture(t, root, args...)
		if err == nil {
			t.Fatalf("%s accepted the manifest: %s", name, stdout)
		}
		envelope := parseEnvelope(t, stdout)
		result, isObject := envelope["result"].(map[string]any)
		if envelope["ok"] != false || !isObject {
			t.Fatalf("%s envelope = %s, want a failure carrying result", name, stdout)
		}
		if result["outcome"] != models.LifecycleInvalidInput ||
			result["safe_action"] != models.FieldDiagnosticCorrectInput || result["effects"] != "none" {
			t.Fatalf("%s result = %v, want INVALID_INPUT/correct_input with no effects", name, result)
		}
		diagnostics, isList := result["diagnostics"].([]any)
		if !isList || len(diagnostics) != 1 {
			t.Fatalf("%s result.diagnostics = %v, want the one offending field", name, result["diagnostics"])
		}
		diagnostic, isObject := diagnostics[0].(map[string]any)
		if !isObject || diagnostic["field"] != e2eMissingSpecRefField ||
			diagnostic["value_class"] != models.FieldValueClassMissing ||
			diagnostic["schema_version"] != float64(schema.Version) {
			t.Fatalf("%s diagnostic = %v, want %s missing at schema version %d",
				name, diagnostics[0], e2eMissingSpecRefField, schema.Version)
		}
		if after := stateBytes(); !bytes.Equal(before, after) {
			t.Fatalf("%s changed state.yaml", name)
		}
		return diagnostics
	}

	writeManifest(e2eManifestMissingSpecRef)
	preflightDiagnostics := rejectManifest("validate-payload", preflightArgs)
	mutationDiagnostics := rejectManifest("set-task-output", mutationArgs)
	// One validator, so the two boundaries report the same defect identically.
	if !reflect.DeepEqual(preflightDiagnostics, mutationDiagnostics) {
		t.Fatalf("boundaries disagree: preflight %v, mutation %v", preflightDiagnostics, mutationDiagnostics)
	}

	writeManifest(e2eManifestCorrected)
	stdout, err := executeRootCommandCapture(t, root, preflightArgs...)
	if err != nil {
		t.Fatalf("corrected manifest rejected by the preflight: %v\n%s", err, stdout)
	}
	result, isObject := parseEnvelope(t, stdout)["result"].(map[string]any)
	if !isObject || result["outcome"] != models.LifecycleCompleted ||
		result["schema_version"] != float64(schema.Version) {
		t.Fatalf("preflight result = %s, want COMPLETED at schema version %d", stdout, schema.Version)
	}

	if stdout, err := executeRootCommandCapture(t, root, mutationArgs...); err != nil {
		t.Fatalf("corrected manifest rejected by set-task-output: %v\n%s", err, stdout)
	}

	// An independent invocation reads the entries back: the accepted manifest
	// is durable, not just reported as written.
	stdout, err = executeRootCommandCapture(t, root, "get", "output-plan", "--json")
	if err != nil {
		t.Fatalf("read the task back: %v\n%s", err, stdout)
	}
	task, isObject := parseEnvelope(t, stdout)["result"].(map[string]any)
	if !isObject {
		t.Fatalf("get returned no task: %s", stdout)
	}
	output, isList := task["output"].([]any)
	if !isList || len(output) != 1 {
		t.Fatalf("task output = %v, want the one accepted entry", task["output"])
	}
	entry, isObject := output[0].(map[string]any)
	if !isObject || entry["desc"] != "Implement the preflight" || entry["spec_ref"] != "specs/vision.md" {
		t.Fatalf("persisted entry = %v, want the corrected manifest", output[0])
	}
}
