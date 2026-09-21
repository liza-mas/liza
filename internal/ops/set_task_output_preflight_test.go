package ops_test

import (
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func preflightEntry() models.OutputEntry {
	return models.OutputEntry{
		Desc:     "Implement storage",
		DoneWhen: "Storage tests pass",
		Scope:    "internal/storage",
		SpecRef:  "specs/storage.md#Storage",
	}
}

func preflightOutput(mutate func(*models.OutputEntry)) []models.OutputEntry {
	entry := preflightEntry()
	if mutate != nil {
		mutate(&entry)
	}
	return []models.OutputEntry{entry}
}

// setupPreflightProject writes a project whose single task is executing and
// assigned, so a rejected manifest is rejected for its shape and nothing else.
func setupPreflightProject(t *testing.T) (projectRoot, statePath string) {
	t.Helper()

	projectRoot = t.TempDir()
	statePath, _ = testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC())}
	testhelpers.WriteInitialState(t, statePath, state)
	return projectRoot, statePath
}

func lifecycleDiagnostics(t *testing.T, err error) models.LifecycleOutcome {
	t.Helper()

	var lifecycleErr *ops.LifecycleError
	if !errors.As(err, &lifecycleErr) {
		t.Fatalf("error carries no lifecycle outcome: %v", err)
	}
	return lifecycleErr.Outcome
}

// TestSetTaskOutputPreflight holds the preflight and the mutation boundary to
// one structural verdict: the same canonical object cannot pass one and fail
// the other, and a rejection reaches no state.
func TestSetTaskOutputPreflight(t *testing.T) {
	t.Parallel()

	rejected := []struct {
		name   string
		output []models.OutputEntry
		field  string
	}{
		{name: "missing desc", output: preflightOutput(func(e *models.OutputEntry) { e.Desc = "" }), field: "/output/0/desc"},
		{name: "missing done_when", output: preflightOutput(func(e *models.OutputEntry) { e.DoneWhen = "" }), field: "/output/0/done_when"},
		{name: "missing scope", output: preflightOutput(func(e *models.OutputEntry) { e.Scope = "" }), field: "/output/0/scope"},
		{name: "missing spec_ref", output: preflightOutput(func(e *models.OutputEntry) { e.SpecRef = "" }), field: "/output/0/spec_ref"},
		{name: "unknown kind", output: preflightOutput(func(e *models.OutputEntry) { e.Kind = "not-a-kind" }), field: "/output/0/kind"},
		{name: "out-of-range depends_on", output: preflightOutput(func(e *models.OutputEntry) { e.DependsOn = []string{"7"} }), field: "/output/0/depends_on"},
		{name: "joined plan_ref", output: preflightOutput(func(e *models.OutputEntry) { e.PlanRef = "specs/a.md;specs/b.md" }), field: "/output/0/plan_ref"},
		{name: "ownerless decomposition", output: preflightOutput(func(e *models.OutputEntry) {
			e.Decomposition = &models.DecompositionManifest{CoverageNotes: "bounded"}
		}), field: "/output/0/decomposition"},
	}

	for _, test := range rejected {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, preflightErr := commands.ValidatePayload(payloadschema.SetTaskOutputOperation, commands.SetTaskOutputPayload(test.output))
			if preflightErr == nil {
				t.Fatal("preflight accepted a structurally invalid manifest")
			}
			preflight := lifecycleDiagnostics(t, preflightErr)

			projectRoot, statePath := setupPreflightProject(t)
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			mutationErr := ops.SetTaskOutput(projectRoot, &ops.SetTaskOutputInput{
				TaskID: "task-1", AgentID: "coder-1", Output: slices.Clone(test.output),
			})
			if mutationErr == nil {
				t.Fatal("mutation boundary accepted a manifest the preflight rejected")
			}
			mutation := lifecycleDiagnostics(t, mutationErr)

			if mutation.Outcome != models.LifecycleInvalidInput || mutation.SafeAction != "correct_input" || mutation.Effects != "none" {
				t.Fatalf("mutation rejection = %s/%s/%s, want INVALID_INPUT/correct_input/none",
					mutation.Outcome, mutation.SafeAction, mutation.Effects)
			}
			if preflight.Outcome != mutation.Outcome {
				t.Fatalf("structural verdicts diverge: preflight %s, mutation %s", preflight.Outcome, mutation.Outcome)
			}
			if !reflect.DeepEqual(preflight.Diagnostics, mutation.Diagnostics) {
				t.Fatalf("diagnostics diverge:\npreflight %#v\nmutation  %#v", preflight.Diagnostics, mutation.Diagnostics)
			}
			if index := slices.IndexFunc(mutation.Diagnostics, func(d models.FieldDiagnostic) bool { return d.Field == test.field }); index < 0 {
				t.Fatalf("no diagnostic on %s: %#v", test.field, mutation.Diagnostics)
			}
			// Text-mode callers see no diagnostics; the error text names the path.
			if !strings.Contains(mutationErr.Error(), test.field) {
				t.Fatalf("error text does not name %s: %v", test.field, mutationErr)
			}

			after, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("a rejected manifest changed state.yaml")
			}
		})
	}

	t.Run("an accepted manifest passes both boundaries", func(t *testing.T) {
		t.Parallel()

		output := preflightOutput(nil)
		result, err := commands.ValidatePayload(payloadschema.SetTaskOutputOperation, commands.SetTaskOutputPayload(output))
		if err != nil {
			t.Fatalf("preflight rejected a valid manifest: %v", err)
		}
		if result.SchemaVersion != 1 || result.Outcome != models.LifecycleCompleted {
			t.Fatalf("preflight result = %#v", result)
		}

		projectRoot, _ := setupPreflightProject(t)
		if err := ops.SetTaskOutput(projectRoot, &ops.SetTaskOutputInput{
			TaskID: "task-1", AgentID: "coder-1", Output: slices.Clone(output),
		}); err != nil {
			t.Fatalf("mutation boundary rejected a manifest the preflight accepted: %v", err)
		}
	})

	// The split the schema declares is observable: a rule that needs the
	// pipeline resolver is not preflightable and still guards the write.
	t.Run("a decomposition-root rule stays at the mutation boundary", func(t *testing.T) {
		t.Parallel()

		rcaNotRequired := false
		output := preflightOutput(func(e *models.OutputEntry) {
			e.PlanRef = "specs/plans/master.md"
			e.RCARequired = &rcaNotRequired
			e.Decomposition = nil
		})
		if _, err := commands.ValidatePayload(payloadschema.SetTaskOutputOperation, commands.SetTaskOutputPayload(output)); err != nil {
			t.Fatalf("preflight claimed a resolver-dependent verdict: %v", err)
		}

		projectRoot := t.TempDir()
		statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
		now := time.Now().UTC()
		root := testhelpers.BuildTaskByStatus("root-task", models.TaskStatus("CODE_PLANNING_MAIN"), now)
		root.RolePair = "code-planning-main-pair"
		root.AssignedTo = testhelpers.StringPtr("master-agent")
		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{root}
		testhelpers.WriteInitialState(t, statePath, state)

		err := ops.SetTaskOutput(projectRoot, &ops.SetTaskOutputInput{
			TaskID: "root-task", AgentID: "master-agent", Output: slices.Clone(output),
		})
		testhelpers.RequireErrorContains(t, err, "output[0].decomposition is required")
	})
}

// TestSetTaskOutputManifestRegression reproduces the manifest repair sequences
// #158 cites: each cost the producing agent a submit/fail/re-read turn that the
// preflight now replaces with a field path.
func TestSetTaskOutputManifestRegression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output []models.OutputEntry
		fields []string
	}{
		{
			// Architect run: the manifest named two architecture refs in one
			// field and left spec_ref to the downstream reader.
			name: "architect manifest",
			output: []models.OutputEntry{{
				Desc:     "Define the storage boundary",
				DoneWhen: "ADR-0142 records the boundary",
				Scope:    "specs/architecture",
				ArchRef:  "specs/architecture/ADR/0142-a.md;specs/architecture/ADR/0143-b.md",
			}},
			fields: []string{"/output/0/spec_ref", "/output/0/arch_ref"},
		},
		{
			// Epic-planner run: the epic ref carried a human annotation and the
			// entry stated no completion criteria.
			name: "epic-planner manifest",
			output: []models.OutputEntry{{
				Desc:    "Plan the ingest epic",
				Scope:   "specs/epics",
				SpecRef: "specs/epics/ingest.md#Scope",
				EpicRef: "specs/epics/ingest.md (see the scope section)",
			}},
			fields: []string{"/output/0/done_when", "/output/0/epic_ref"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := commands.ValidatePayload(payloadschema.SetTaskOutputOperation, commands.SetTaskOutputPayload(test.output))
			if err == nil {
				t.Fatal("preflight accepted a manifest the observed run had to repair")
			}
			outcome := lifecycleDiagnostics(t, err)
			if outcome.Outcome != models.LifecycleInvalidInput || outcome.SafeAction != "correct_input" {
				t.Fatalf("rejection = %s/%s, want INVALID_INPUT/correct_input", outcome.Outcome, outcome.SafeAction)
			}
			for _, field := range test.fields {
				if !slices.ContainsFunc(outcome.Diagnostics, func(d models.FieldDiagnostic) bool { return d.Field == field }) {
					t.Fatalf("no diagnostic on %s: %#v", field, outcome.Diagnostics)
				}
			}
		})
	}
}
