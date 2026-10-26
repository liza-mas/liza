package ops

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// planCheckStepFixture is a reviewed pipeline resolver and domain over a state
// holding the given tasks, for the steps RecordPlanCheck is composed of.
func planCheckStepFixture(t *testing.T, tasks ...models.Task) (*models.State, *pipeline.Resolver, PlanHandoffDomain) {
	t.Helper()
	cfg, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		t.Fatal(err)
	}
	resolver := pipeline.NewResolver(cfg)
	state := testhelpers.CreateValidState()
	state.Tasks = tasks
	return state, resolver, NewPlanHandoffDomain(resolver)
}

func TestAdmitPlanCheck_RefusalsKeepTheirOrder(t *testing.T) {
	t.Parallel()
	fenced := handoffPlan("fenced", "code-planning-pair")
	fenced.AmendsPlan = "original"
	retired := handoffPlan("retired", "code-planning-pair")
	retired.PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckRetired, Ruling: "dropped", By: "operator"}
	state, resolver, domain := planCheckStepFixture(t, fenced, retired)

	cases := []struct {
		name    string
		input   PlanCheckInput
		wantErr string
	}{
		{"missing task", PlanCheckInput{TaskID: "absent", Action: PlanCheckActionHold}, `task "absent" not found`},
		{"correction task", PlanCheckInput{TaskID: "fenced", Action: PlanCheckActionHold}, "pending amendment or disposition a correction task"},
		{"notes without per-subtask", PlanCheckInput{TaskID: "retired", Action: PlanCheckActionPass, Notes: []models.PlanValidationNote{{OutputIndex: 7, Message: "x"}}}, "output_index"},
		{"retired cannot be revived", PlanCheckInput{TaskID: "retired", Action: PlanCheckActionHold}, "cannot be revived"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := admitPlanCheck(state, resolver, domain, tc.input)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("admitPlanCheck error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}

	if _, _, _, err := admitPlanCheck(state, resolver, domain, PlanCheckInput{TaskID: "retired", Action: PlanCheckActionRetire, Ruling: "dropped"}); err != nil {
		t.Fatalf("a retiring action revisits a retired hand-off: %v", err)
	}
}

func TestDecidePlanCheck_ReadSideDoesNotPersist(t *testing.T) {
	t.Parallel()
	plan := handoffPlan("p", "code-planning-pair")
	state, resolver, domain := planCheckStepFixture(t, plan)
	task := state.FindTask("p")
	class, blocker := domain.Classify(state, task)

	next, err := decidePlanCheck(state, resolver, domain, task, class, blocker, PlanCheckInput{TaskID: "p", Action: PlanCheckActionPass}, "orchestrator-1")
	if err != nil {
		t.Fatalf("decidePlanCheck: %v", err)
	}
	if !next.changed || next.check == nil || next.check.Verdict != models.PlanCheckPassed || next.note != "passed" {
		t.Fatalf("decision = %+v, want a changed pass", next)
	}
	if task.PlanCheck != nil || len(task.History) != 0 {
		t.Fatalf("deciding persisted: plan_check=%+v history=%d", task.PlanCheck, len(task.History))
	}

	persistPlanCheck(task, next, "orchestrator-1")
	if task.PlanCheck == nil || task.PlanCheck.Verdict != models.PlanCheckPassed || len(task.History) != 1 ||
		task.History[0].Event != models.TaskEventPlanCheck || task.History[0].Note == nil || *task.History[0].Note != "passed" {
		t.Fatalf("persisted task = %+v", task)
	}
}

func TestPersistPlanCheck_ReplayWritesNothing(t *testing.T) {
	t.Parallel()
	plan := withPlanCheck(handoffPlan("p", "code-planning-pair"), models.PlanCheckHeld, "provision")
	before := *plan.PlanCheck
	persistPlanCheck(&plan, planCheckChange{}, "orchestrator-1")
	if plan.PlanCheck.Verdict != before.Verdict || plan.PlanCheck.Ask != before.Ask || len(plan.History) != 0 {
		t.Fatalf("a no-op decision changed the task: %+v", plan)
	}
}

func TestDecidePlanCheck_ClearOfHoldOnExpandedPlanDropsOnlyTheDisposition(t *testing.T) {
	t.Parallel()
	plan := withPlanCheck(handoffPlan("p", "code-planning-pair"), models.PlanCheckHeld, "provision")
	plan.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
	state, resolver, domain := planCheckStepFixture(t, plan)
	task := state.FindTask("p")
	class, blocker := domain.Classify(state, task)
	if class != models.PlanHandoffNotSource {
		t.Fatalf("class = %q, want %q", class, models.PlanHandoffNotSource)
	}

	next, err := decidePlanCheck(state, resolver, domain, task, class, blocker, PlanCheckInput{TaskID: "p", Action: PlanCheckActionClear}, "operator")
	if err != nil {
		t.Fatalf("decidePlanCheck: %v", err)
	}
	if !next.changed || next.check != nil || next.note != "hold cleared (plan no longer awaits hand-off)" {
		t.Fatalf("decision = %+v, want a cleared hold", next)
	}
}

func TestDecidePlanCheck_RestoresDroppedRetirementRuling(t *testing.T) {
	t.Parallel()
	plan := handoffPlan("p", "code-planning-pair")
	plan.PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckRetired, By: "operator-1"}
	state, resolver, domain := planCheckStepFixture(t, plan)
	task := state.FindTask("p")
	class, blocker := domain.Classify(state, task)

	next, err := decidePlanCheck(state, resolver, domain, task, class, blocker, PlanCheckInput{TaskID: "p", Action: PlanCheckActionRetire, Ruling: " Scope dropped "}, "operator-2")
	if err != nil {
		t.Fatalf("decidePlanCheck: %v", err)
	}
	if !next.changed || next.check == nil || next.check.Ruling != "Scope dropped" || next.check.By != "operator-1" ||
		next.note != "retirement ruling restored: Scope dropped" {
		t.Fatalf("decision = %+v, want the ruling restored on the original retirement", next)
	}
	if task.PlanCheck.Ruling != "" {
		t.Fatal("deciding must not write the restored ruling")
	}
}

func TestProjectPlanCheckResult_ReportsDispositionAndClass(t *testing.T) {
	t.Parallel()
	plan := withPlanCheck(handoffPlan("p", "code-planning-pair"), models.PlanCheckPassed, "")
	plan.PlanCheck.ReplacedBy = "c"
	plan.PlanCheck.Ruling = "ruling"
	state, _, domain := planCheckStepFixture(t, plan)
	task := state.FindTask("p")

	result := &PlanCheckResult{TaskID: "p", Action: PlanCheckActionPass}
	projectPlanCheckResult(result, state, domain, task, true)
	wantClass, wantBlocker := domain.Classify(state, task)
	if !result.Changed || result.Verdict != models.PlanCheckPassed || result.ReplacedBy != "c" || result.Ruling != "ruling" ||
		result.Class != wantClass || result.Blocker != wantBlocker {
		t.Fatalf("result = %+v", result)
	}
}

// K7: the A1 vocabulary constants spell the values the pipeline and state carry.
func TestPlanCheckVocabularySpelling(t *testing.T) {
	t.Parallel()
	for constant, want := range map[string]string{
		string(models.CardinalityPerSubtask):   "per-subtask",
		string(models.CardinalityOneToOne):     "one-to-one",
		string(models.CardinalityManyToOne):    "many-to-one",
		string(models.TransitionTriggerManual): "manual",
		models.ReplannedTransition:             "replanned",
	} {
		if constant != want {
			t.Errorf("constant spells %q, want %q", constant, want)
		}
	}
}
