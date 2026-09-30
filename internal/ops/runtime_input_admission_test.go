package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const runtimeInputRegistryPath = "config/runtime-inputs.yaml"

func admissionDeclarations() []models.RuntimeInput {
	return []models.RuntimeInput{{ID: "w03", Commands: creationValidation, Recipe: "project.w03", Consumption: models.RuntimeInputSingleUse, Env: []string{"W03_FIXTURE"}}}
}

// newRuntimeInputAdmissionFixture extends the acceptance creation fixture: the
// allocating output declares a runtime input, the replacement repeats it, and
// registry (when non-empty) is committed to integration and configured.
func newRuntimeInputAdmissionFixture(t *testing.T, registry string) replacementFixture {
	t.Helper()
	f := newAcceptanceCreationFixture(t)
	if registry != "" {
		path := filepath.Join(f.root, runtimeInputRegistryPath)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(registry), 0o644); err != nil {
			t.Fatal(err)
		}
		testhelpers.MustGit(t, f.root, "add", runtimeInputRegistryPath)
		testhelpers.MustGit(t, f.root, "commit", "-m", "test: runtime-input registry")
		testhelpers.MustGit(t, f.root, "branch", "-f", "integration", "HEAD")
	}
	if err := db.For(f.statePath).Modify(func(s *models.State) error {
		if registry != "" {
			s.Config.RuntimeInputRegistry = runtimeInputRegistryPath
		}
		s.FindTask("acceptance-parent").Output[0].RuntimeInputs = admissionDeclarations()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.input.Replacement.RuntimeInputs = admissionDeclarations()
	return f
}

const knownRecipes = "version: 1\nrecipes:\n  project.w03:\n    description: one W03 fixture\n"

func requireRuntimeInputAdmissionRefusal(t *testing.T, f replacementFixture, before []byte, err error, field, class string) {
	t.Helper()
	var lifecycle *LifecycleError
	if !errors.As(err, &lifecycle) || lifecycle.Outcome.Outcome != models.LifecycleInvalidInput || len(lifecycle.Outcome.Diagnostics) == 0 {
		t.Fatalf("err = %v, want a field-attributed INVALID_INPUT refusal", err)
	}
	if d := lifecycle.Outcome.Diagnostics[0]; d.Field != field || d.ValueClass != class {
		t.Fatalf("diagnostic = %+v, want field %q class %q", d, field, class)
	}
	if after := replacementBytes(t, f.statePath); string(before) != string(after) {
		t.Fatal("refused declaration changed state")
	}
}

func TestReplaceTask_AdmitsRuntimeInputsTheAllocationDeclares(t *testing.T) {
	f := newRuntimeInputAdmissionFixture(t, knownRecipes)
	if _, err := f.run(); err != nil {
		t.Fatalf("replacement repeating the allocation's runtime inputs refused: %v", err)
	}
	s := replacementState(t, f)
	replacement := s.FindTask("replacement")
	if !models.RuntimeInputsEqual(replacement.RuntimeInputs, admissionDeclarations()) {
		t.Fatalf("replacement inputs = %+v", replacement.RuntimeInputs)
	}
	integration := testhelpers.MustGit(t, f.root, "rev-parse", "integration")
	if input, err := loadAcceptanceInput(f.root, s, replacement, integration); err != nil || input == nil {
		t.Fatalf("claim predicate refused the admitted replacement: input=%v err=%v", input, err)
	}
}

func TestReplaceTask_RefusesRuntimeInputsWithoutARegisteredRecipe(t *testing.T) {
	for name, tc := range map[string]struct {
		registry, class string
	}{
		"registry not configured": {class: models.FieldValueClassConflict},
		"recipe not registered":   {registry: "version: 1\nrecipes:\n  project.other: {}\n", class: models.FieldValueClassUnknownEnum},
	} {
		t.Run(name, func(t *testing.T) {
			f := newRuntimeInputAdmissionFixture(t, tc.registry)
			before := replacementBytes(t, f.statePath)
			_, err := f.run()
			requireRuntimeInputAdmissionRefusal(t, f, before, err, "/replacement/runtime_inputs/0/recipe", tc.class)
		})
	}
}

// The allocation predicate compares runtime inputs: a child declaring inputs
// its reviewed allocation did not is not allocated, so claim would refuse it.
func TestRuntimeInputsOutsideTheAllocationAreRefused(t *testing.T) {
	f := newRuntimeInputAdmissionFixture(t, knownRecipes)
	f.input.Replacement.RuntimeInputs[0].Consumption = models.RuntimeInputReusable
	before := replacementBytes(t, f.statePath)
	_, err := f.run()
	var lifecycle *LifecycleError
	if !errors.As(err, &lifecycle) || lifecycle.Outcome.Outcome != models.LifecycleInvalidInput {
		t.Fatalf("replacement diverging from its allocation's inputs err = %v, want refused", err)
	}
	if after := replacementBytes(t, f.statePath); string(before) != string(after) {
		t.Fatal("refused replacement changed state")
	}
}

func TestAddTask_RefusesRuntimeInputsOnANonStrictTask(t *testing.T) {
	for name, tc := range map[string]struct {
		planRef, field string
	}{
		"marker-free plan section": {planRef: creationGoalRef + "#Identity", field: "runtime_inputs"},
		"no acceptance carrier":    {field: "/runtime_inputs"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newRuntimeInputAdmissionFixture(t, knownRecipes)
			input := adHocCodingInput(tc.planRef)
			if tc.planRef == "" {
				input.RolePair = "code-planning-pair" // Never judged for acceptance.
			}
			input.RuntimeInputs = admissionDeclarations()
			before := replacementBytes(t, f.statePath)
			_, err := AddTaskWithAuthority(f.statePath, paths.New(f.root).LogPath(), input, f.authority)
			requireRuntimeInputAdmissionRefusal(t, f, before, err, tc.field, models.FieldValueClassConflict)
			if !strings.Contains(err.Error(), "strict acceptance contract") {
				t.Fatalf("refusal %q does not name the strict-contract requirement", err)
			}
		})
	}
}

// Runtime-input names are scrubbed from sessions, so neither class may take a
// name the other holds, whichever is declared first.
func TestRuntimeInputNamesStayDisjointFromSessionPrerequisites(t *testing.T) {
	prerequisite := []models.ValidationPrerequisite{{Command: creationValidation[0], Env: []string{"W03_FIXTURE"}}}

	t.Run("prerequisite then runtime input", func(t *testing.T) {
		// A live task already requires the name as a session prerequisite, so
		// no write may then declare it as a runtime input: the Modify fence
		// refuses the allocation itself, before any replacement.
		f := newAcceptanceCreationFixture(t)
		bb := db.For(f.statePath)
		if err := bb.Modify(func(s *models.State) error {
			consumer := s.FindTask("consumer-a")
			consumer.Validation = creationValidation
			consumer.ValidationPrerequisites = prerequisite
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		err := bb.Modify(func(s *models.State) error {
			s.FindTask("acceptance-parent").Output[0].RuntimeInputs = admissionDeclarations()
			return nil
		})
		if err == nil || !strings.Contains(err.Error(), "W03_FIXTURE would be both a runtime input and a validation prerequisite") {
			t.Fatalf("err = %v, want the collision fenced", err)
		}
	})

	t.Run("runtime input then prerequisite", func(t *testing.T) {
		f := newRuntimeInputAdmissionFixture(t, knownRecipes)
		if _, err := f.run(); err != nil {
			t.Fatal(err)
		}
		other := adHocCodingInput(creationGoalRef + "#Identity")
		other.ValidationPrerequisites = prerequisite
		before := replacementBytes(t, f.statePath)
		_, err := AddTaskWithAuthority(f.statePath, paths.New(f.root).LogPath(), other, f.authority)
		requireRuntimeInputAdmissionRefusal(t, f, before, err, "/validation_prerequisites/0/env/0", models.FieldValueClassConflict)
	})

	t.Run("both on one candidate", func(t *testing.T) {
		f := newRuntimeInputAdmissionFixture(t, knownRecipes)
		f.input.Replacement.ValidationPrerequisites = prerequisite
		before := replacementBytes(t, f.statePath)
		_, err := f.run()
		requireRuntimeInputAdmissionRefusal(t, f, before, err, "/replacement/runtime_inputs/0/env/0", models.FieldValueClassConflict)
	})
}

// A planning output may declare runtime inputs only where its child adopts a
// strict contract, and only for recipes already registered at integration.
func TestSubmitForReview_PlanningOutputRuntimeInputs(t *testing.T) {
	declare := func(t *testing.T, bb *db.Blackboard, taskID string) {
		t.Helper()
		if err := bb.Modify(func(state *models.State) error {
			state.FindTask(taskID).Output[0].RuntimeInputs = []models.RuntimeInput{{ID: "w03", Commands: []string{"node --version"},
				Recipe: "project.w03", Consumption: models.RuntimeInputSingleUse, Env: []string{"W03_FIXTURE"}}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("legacy allocation", func(t *testing.T) {
		root, taskID, commit, agentID, bb := setupPlanningAcceptanceSubmission(t, "", true)
		declare(t, bb, taskID)
		_, err := SubmitForReview(root, taskID, commit, agentID)
		testhelpers.RequireErrorContains(t, err, "strict acceptance contract")
	})

	t.Run("recipe not registered", func(t *testing.T) {
		root, taskID, commit, agentID, bb := setupPlanningAcceptanceSubmission(t, "", false)
		declare(t, bb, taskID)
		_, err := SubmitForReview(root, taskID, commit, agentID)
		var lifecycle *LifecycleError
		if !errors.As(err, &lifecycle) || len(lifecycle.Outcome.Diagnostics) == 0 || lifecycle.Outcome.Diagnostics[0].Field != "/output/0/runtime_inputs/0/recipe" {
			t.Fatalf("err = %v, want the output's recipe refused", err)
		}
		if task := mustReadTask(t, paths.New(root).StatePath(), taskID); task.Status != models.TaskStatusCodePlanning {
			t.Fatalf("refused planning submission moved the task to %s", task.Status)
		}
	})
}

// outputFixture writes a state whose executing producer task "plan" belongs to
// rolePair, assigned to agent, plus extra tasks.
func outputFixture(t *testing.T, rolePair string, status models.TaskStatus, agent string, extra ...models.Task) (string, string) {
	t.Helper()
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	s := testhelpers.CreateValidState()
	plan := testhelpers.BuildTaskByStatus("plan", models.TaskStatusImplementing, time.Now().UTC())
	plan.Status, plan.RolePair, plan.Type, plan.AssignedTo = status, rolePair, models.TaskTypePlanning, &agent
	s.Tasks = append([]models.Task{plan}, extra...)
	testhelpers.WriteInitialState(t, statePath, s)
	return root, statePath
}

func runtimeInputOutput(inputs []models.RuntimeInput, prerequisites []models.ValidationPrerequisite) []models.OutputEntry {
	return []models.OutputEntry{{Desc: "live child", DoneWhen: "live proof passes", Scope: "live", SpecRef: "specs/live.md",
		Validation: creationValidation, RuntimeInputs: inputs, ValidationPrerequisites: prerequisites}}
}

// C3: only output consumed by coding pairs may declare runtime inputs.
func TestSetTaskOutput_RuntimeInputsNeedCodingConsumers(t *testing.T) {
	for name, tc := range map[string]struct {
		rolePair string
		status   models.TaskStatus
		agent    string
		refused  bool
	}{
		"architecture to code planning": {rolePair: "architecture-pair", status: "ARCHITECTING", agent: "architect-1", refused: true},
		"code plan to coding":           {rolePair: "code-planning-pair", status: models.TaskStatusCodePlanning, agent: "code-planner-1"},
		"integration to coding":         {rolePair: "integration-pair", status: "ANALYZING_INTEGRATION", agent: "integration-analyst-1", refused: true},
		"slice integration to coding":   {rolePair: "slice-integration-pair", status: "ANALYZING_SLICE_INTEGRATION", agent: "integration-analyst-1", refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			root, statePath := outputFixture(t, tc.rolePair, tc.status, tc.agent)
			if err := db.For(statePath).Modify(func(s *models.State) error {
				s.FindTask("plan").Type = models.TaskTypeForRole(strings.Split(tc.agent, "-1")[0])
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: "plan", AgentID: tc.agent, Output: runtimeInputOutput(admissionDeclarations(), nil)})
			if !tc.refused {
				if err != nil {
					t.Fatalf("coding allocation refused: %v", err)
				}
				return
			}
			var lifecycle *LifecycleError
			if !errors.As(err, &lifecycle) || len(lifecycle.Outcome.Diagnostics) == 0 || lifecycle.Outcome.Diagnostics[0].Field != "/output/0/runtime_inputs" {
				t.Fatalf("err = %v, want the output's runtime_inputs refused", err)
			}
			if task := mustReadTask(t, statePath, "plan"); len(task.Output) != 0 {
				t.Fatal("refused output was persisted")
			}
		})
	}
}

// Only a code plan both generates coding children and is a planning parent
// that can allocate a strict contract. A main plan's children are plans; an
// integration task generates coding children but is no planning parent.
func TestRuntimeInputConsumerRuleFollowsThePipeline(t *testing.T) {
	root, _ := outputFixture(t, "code-planning-pair", models.TaskStatusCodePlanning, "code-planner-1")
	resolver, _, err := loadResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	output := runtimeInputOutput(admissionDeclarations(), nil)
	for rolePair, refused := range map[string]bool{
		"code-planning-pair": false, "code-planning-main-pair": true, "architecture-pair": true, "coding-pair": true,
		"integration-pair": true, "slice-integration-pair": true,
	} {
		pair, err := resolver.RolePair(rolePair)
		if err != nil {
			t.Fatal(err)
		}
		producer := &models.Task{RolePair: rolePair, Type: models.TaskTypeForRole(pair.Doer)}
		if got := len(runtimeInputConsumerDiagnostics(resolver, producer, output)) > 0; got != refused {
			t.Errorf("%s: refused = %v, want %v", rolePair, got, refused)
		}
	}
	// The integration producers are refused for their type, not their consumers.
	for _, rolePair := range []string{"integration-pair", "slice-integration-pair"} {
		consumers, err := resolver.OutputConsumerRolePairs(rolePair)
		if err != nil || len(consumers) == 0 {
			t.Fatalf("%s consumers = %v, %v", rolePair, consumers, err)
		}
		for _, consumer := range consumers {
			pair, err := resolver.RolePair(consumer)
			if err != nil || models.TaskTypeForRole(pair.Doer) != models.TaskTypeCoding {
				t.Fatalf("%s consumer %s is not a coding pair", rolePair, consumer)
			}
		}
	}
}

// C2: a name enters the deny set when the output is saved, so the collision is
// refused there, against live prerequisites and pending allocations alike.
func TestSetTaskOutput_RuntimeInputNamesStayDisjoint(t *testing.T) {
	prerequisite := []models.ValidationPrerequisite{{Command: creationValidation[0], Env: []string{"W03_FIXTURE"}}}
	live := testhelpers.BuildTaskByStatus("live", models.TaskStatusReady, time.Now().UTC())
	live.Validation, live.ValidationPrerequisites = creationValidation, prerequisite
	pending := testhelpers.BuildTaskByStatus("plan-a", models.TaskStatusMerged, time.Now().UTC())
	pending.RolePair, pending.Type = "code-planning-pair", models.TaskTypePlanning
	pending.Output = runtimeInputOutput(nil, prerequisite)
	consumed := pending
	consumed.Output = runtimeInputOutput(nil, prerequisite)
	consumed.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
	for name, tc := range map[string]struct {
		other   models.Task
		refused bool
	}{
		"live task prerequisite":             {other: live, refused: true},
		"pending plan allocation":            {other: pending, refused: true},
		"consumed plan allocation is closed": {other: consumed},
	} {
		t.Run(name, func(t *testing.T) {
			root, statePath := outputFixture(t, "code-planning-pair", models.TaskStatusCodePlanning, "code-planner-1", tc.other)
			err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: "plan", AgentID: "code-planner-1", Output: runtimeInputOutput(admissionDeclarations(), nil)})
			if !tc.refused {
				if err != nil {
					t.Fatalf("declaration refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "W03_FIXTURE") {
				t.Fatalf("err = %v, want a collision on W03_FIXTURE", err)
			}
			if task := mustReadTask(t, statePath, "plan"); len(task.Output) != 0 {
				t.Fatal("colliding output was persisted, stripping a live prerequisite")
			}
		})
	}

	t.Run("prerequisite output against a declared runtime input", func(t *testing.T) {
		declared := testhelpers.BuildTaskByStatus("declared", models.TaskStatusReady, time.Now().UTC())
		declared.Validation, declared.RuntimeInputs = creationValidation, admissionDeclarations()
		root, statePath := outputFixture(t, "code-planning-pair", models.TaskStatusCodePlanning, "code-planner-1", declared)
		err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: "plan", AgentID: "code-planner-1", Output: runtimeInputOutput(nil, prerequisite)})
		if err == nil || !strings.Contains(err.Error(), "W03_FIXTURE") {
			t.Fatalf("err = %v, want the prerequisite refused", err)
		}
		if task := mustReadTask(t, statePath, "plan"); len(task.Output) != 0 {
			t.Fatal("colliding output was persisted")
		}
	})
}

// C2: two concurrent writers declaring the same name in opposite classes
// cannot both land; the Modify fence decides under the state lock.
func TestRuntimeInputNameCollisionFenceSerializesConcurrentWriters(t *testing.T) {
	second := testhelpers.BuildTaskByStatus("plan-b", models.TaskStatusImplementing, time.Now().UTC())
	planner := "code-planner-2"
	second.Status, second.RolePair, second.Type, second.AssignedTo = models.TaskStatusCodePlanning, "code-planning-pair", models.TaskTypePlanning, &planner
	root, statePath := outputFixture(t, "code-planning-pair", models.TaskStatusCodePlanning, "code-planner-1", second)
	prerequisite := []models.ValidationPrerequisite{{Command: creationValidation[0], Env: []string{"W03_FIXTURE"}}}
	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, input := range []*SetTaskOutputInput{
		{TaskID: "plan", AgentID: "code-planner-1", Output: runtimeInputOutput(admissionDeclarations(), nil)},
		{TaskID: "plan-b", AgentID: planner, Output: runtimeInputOutput(nil, prerequisite)},
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = SetTaskOutput(root, input)
		}()
	}
	close(start)
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("errs = %v, want exactly one writer refused", errs)
	}
	state, err := db.For(statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	if collisions := models.RuntimeInputNameCollisions(state); len(collisions) != 0 {
		t.Fatalf("persisted collisions %v", collisions)
	}
}
