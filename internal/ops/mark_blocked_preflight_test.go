package ops_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// markBlockedFixture is one blocking call expressed once and submitted to both
// boundaries: the state-free preflight and the mutation path.
type markBlockedFixture struct {
	name      string
	taskID    string
	reason    string
	questions []string
	opts      ops.MarkBlockedOptions
}

func markBlockedCommandRepair() *models.RepairRequest {
	return &models.RepairRequest{
		Operation:  "add-task",
		Target:     "architecture-2",
		Command:    "add-task --id architecture-2 --json",
		Evidence:   []string{"command=add-task --id architecture-2 --json exit_code=1 stderr=command requires role type [orchestrator]"},
		Validation: []string{"validate --json"},
	}
}

func markBlockedDependencyRepair() *models.RepairRequest {
	return &models.RepairRequest{
		Operation: models.RepairOperationApplyDependencyRepair,
		Target:    "task-1",
		DependencyUpdates: []models.DependencyUpdate{{
			TaskID:            "consumer-1",
			ExpectedDependsOn: []string{},
			DesiredDependsOn:  []string{"producer-1"},
		}},
		Evidence:   []string{"error=dependency repair requires orchestrator authority"},
		Validation: []string{"validate --json"},
	}
}

func TestMarkBlockedPreflight(t *testing.T) {
	t.Parallel()

	fixtures := []markBlockedFixture{
		{name: "accepted without a repair request", taskID: "task-1", reason: "blocked", questions: []string{"q1"}},
		{
			name: "accepted with a command-based repair request", taskID: "task-1", reason: "blocked", questions: []string{"q1"},
			opts: ops.MarkBlockedOptions{RepairRequest: markBlockedCommandRepair()},
		},
		{
			name: "accepted with a declarative dependency repair", taskID: "task-1", reason: "blocked", questions: []string{"q1"},
			opts: ops.MarkBlockedOptions{RepairRequest: markBlockedDependencyRepair()},
		},
		{name: "rejected without a reason", taskID: "task-1", questions: []string{"q1"}},
		{name: "rejected without questions", taskID: "task-1", reason: "blocked"},
		{name: "rejected with four questions", taskID: "task-1", reason: "blocked", questions: []string{"q1", "q2", "q3", "q4"}},
		{
			name: "rejected with a duplicate depends-on entry", taskID: "task-1", reason: "blocked", questions: []string{"q1"},
			opts: ops.MarkBlockedOptions{DependsOn: []string{"dep-1", "dep-1"}},
		},
		{
			name: "rejected with unstructured repair evidence", taskID: "task-1", reason: "blocked", questions: []string{"q1"},
			opts: func() ops.MarkBlockedOptions {
				request := markBlockedCommandRepair()
				request.Evidence = []string{"the add-task call failed"}
				return ops.MarkBlockedOptions{RepairRequest: request}
			}(),
		},
		{
			name: "rejected with a declarative repair targeting another task", taskID: "task-1", reason: "blocked", questions: []string{"q1"},
			opts: func() ops.MarkBlockedOptions {
				request := markBlockedDependencyRepair()
				request.Target = "task-2"
				return ops.MarkBlockedOptions{RepairRequest: request}
			}(),
		},
		{
			name: "rejected with an implicit dependency list", taskID: "task-1", reason: "blocked", questions: []string{"q1"},
			opts: func() ops.MarkBlockedOptions {
				request := markBlockedDependencyRepair()
				request.DependencyUpdates[0].ExpectedDependsOn = nil
				return ops.MarkBlockedOptions{RepairRequest: request}
			}(),
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()

			preflight := structuralDiagnostics(t, preflightMarkBlocked(t, fixture))
			projectRoot, statePath := markBlockedProject(t)
			before := readFileBytes(t, statePath)

			_, err := ops.MarkBlockedWithOptions(projectRoot, fixture.taskID, fixture.reason, fixture.questions, "coder-1", fixture.opts)
			mutation := structuralDiagnostics(t, err)

			if !reflect.DeepEqual(mutation, preflight) {
				t.Fatalf("mutation diagnostics = %#v, preflight diagnostics = %#v", mutation, preflight)
			}
			if mutation == nil {
				if err != nil {
					t.Fatalf("structurally valid payload failed at the mutation boundary: %v", err)
				}
				return
			}
			// A structural rejection never reaches the state lock.
			if after := readFileBytes(t, statePath); !reflect.DeepEqual(after, before) {
				t.Fatal("state file changed while rejecting a structurally invalid payload")
			}
		})
	}
}

func TestMarkBlockedPreflightRejectedValues(t *testing.T) {
	t.Parallel()

	const marker = "SYNTHETIC_REJECTED_VALUE_158"
	for _, tc := range []struct {
		name  string
		field string
		alter func(*markBlockedFixture)
	}{
		{
			name: "duplicate depends-on entry", field: "/depends_on/1",
			alter: func(f *markBlockedFixture) { f.opts.DependsOn = []string{marker, marker} },
		},
		{
			name: "mismatched repair target", field: "/repair_request/target",
			// The old constraint echoed the blocked task ID, not the target.
			alter: func(f *markBlockedFixture) { f.taskID = marker },
		},
		{
			name: "duplicate dependency update task", field: "/repair_request/dependency_updates/1/task_id",
			alter: func(f *markBlockedFixture) {
				update := f.opts.RepairRequest.DependencyUpdates[0]
				update.TaskID = marker
				f.opts.RepairRequest.DependencyUpdates = []models.DependencyUpdate{update, update}
			},
		},
		{
			name: "duplicate expected dependency", field: "/repair_request/dependency_updates/0/expected_depends_on/1",
			alter: func(f *markBlockedFixture) {
				f.opts.RepairRequest.DependencyUpdates[0].ExpectedDependsOn = []string{marker, marker}
			},
		},
		{
			name: "duplicate desired dependency", field: "/repair_request/dependency_updates/0/desired_depends_on/1",
			alter: func(f *markBlockedFixture) {
				f.opts.RepairRequest.DependencyUpdates[0].DesiredDependsOn = []string{marker, marker}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture := markBlockedFixture{
				taskID: "task-1", reason: "blocked", questions: []string{"q1"},
				opts: ops.MarkBlockedOptions{RepairRequest: markBlockedDependencyRepair()},
			}
			tc.alter(&fixture)
			preflightErr := preflightMarkBlocked(t, fixture)
			projectRoot, statePath := markBlockedProject(t)
			before := readFileBytes(t, statePath)
			_, mutationErr := ops.MarkBlockedWithOptions(projectRoot, fixture.taskID, fixture.reason, fixture.questions, "coder-1", fixture.opts)
			if !reflect.DeepEqual(structuralDiagnostics(t, preflightErr), structuralDiagnostics(t, mutationErr)) {
				t.Fatal("preflight and mutation diagnostics differ")
			}
			if !bytes.Equal(readFileBytes(t, statePath), before) {
				t.Fatal("state file changed while rejecting a structurally invalid payload")
			}
			for name, err := range map[string]error{"preflight": preflightErr, "mutation": mutationErr} {
				t.Run(name, func(t *testing.T) {
					diagnostics := structuralDiagnostics(t, err)
					if len(diagnostics) != 1 || diagnostics[0].Field != tc.field ||
						diagnostics[0].ValueClass != models.FieldValueClassConflict {
						t.Fatalf("diagnostics = %+v, want one conflict on %s", diagnostics, tc.field)
					}
					var envelope bytes.Buffer
					if writeErr := jsonout.WriteResult(&envelope, nil, nil, err); !errors.Is(writeErr, jsonout.ErrAlreadyWritten) {
						t.Fatalf("envelope write = %v, want ErrAlreadyWritten", writeErr)
					}
					if strings.Contains(envelope.String(), marker) {
						t.Errorf("rejected value echoed in envelope: %s", envelope.String())
					}
					var rendered struct {
						Result models.LifecycleOutcome `json:"result"`
						Error  struct {
							Details struct {
								Diagnostics []models.FieldDiagnostic `json:"diagnostics"`
							} `json:"details"`
						} `json:"error"`
					}
					if decodeErr := json.Unmarshal(envelope.Bytes(), &rendered); decodeErr != nil {
						t.Fatal(decodeErr)
					}
					if !reflect.DeepEqual(rendered.Result.Diagnostics, diagnostics) ||
						!reflect.DeepEqual(rendered.Error.Details.Diagnostics, diagnostics) {
						t.Fatal("rendered result.diagnostics or error.details lost diagnostics")
					}
				})
			}
		})
	}
}

func TestMarkBlockedPreflightLeavesStateRulesAtTheMutationBoundary(t *testing.T) {
	t.Parallel()

	// Whether a dependency exists is a state question, so the schema accepts
	// this payload and only the mutation boundary can reject it.
	fixture := markBlockedFixture{
		taskID: "task-1", reason: "blocked", questions: []string{"q1"},
		opts: ops.MarkBlockedOptions{DependsOn: []string{"absent-task"}},
	}
	if diagnostics := structuralDiagnostics(t, preflightMarkBlocked(t, fixture)); diagnostics != nil {
		t.Fatalf("preflight diagnostics = %#v, want none", diagnostics)
	}

	projectRoot, _ := markBlockedProject(t)
	_, err := ops.MarkBlockedWithOptions(projectRoot, fixture.taskID, fixture.reason, fixture.questions, "coder-1", fixture.opts)
	if err == nil {
		t.Fatal("MarkBlockedWithOptions() accepted a dependency on an absent task")
	}
	if diagnostics := structuralDiagnostics(t, err); diagnostics != nil {
		t.Fatalf("mutation diagnostics = %#v, want a state rejection carrying none", diagnostics)
	}
}

// preflightMarkBlocked runs the canonical object through the same file-shaped
// input an agent passes to the preflight command, and returns its error for
// diagnostic parity and full-envelope assertions.
func preflightMarkBlocked(t *testing.T, fixture markBlockedFixture) error {
	t.Helper()

	payload := commands.MarkBlockedPayload(fixture.taskID, fixture.reason, fixture.questions, fixture.opts)
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal canonical object: %v", err)
	}
	path := filepath.Join(t.TempDir(), "mark-blocked.json")
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatalf("write payload file: %v", err)
	}

	_, err = commands.ValidatePayloadFile("mark-blocked", path)
	return err
}

// structuralDiagnostics reports the field diagnostics of a structural
// rejection, and nil for any other outcome — including a state rejection,
// which carries no diagnostics because no field was malformed.
func structuralDiagnostics(t *testing.T, err error) []models.FieldDiagnostic {
	t.Helper()

	if err == nil {
		return nil
	}
	var lifecycle *ops.LifecycleError
	if !errors.As(err, &lifecycle) {
		t.Fatalf("error %v is not a lifecycle error", err)
	}
	if lifecycle.Outcome.Outcome != models.LifecycleInvalidInput {
		return nil
	}
	return lifecycle.Outcome.Diagnostics
}

func markBlockedProject(t *testing.T) (projectRoot, statePath string) {
	t.Helper()

	projectRoot = t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ = testhelpers.SetupLizaDir(t, projectRoot)

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC())}
	testhelpers.WriteInitialState(t, statePath, state)
	return projectRoot, statePath
}

func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
