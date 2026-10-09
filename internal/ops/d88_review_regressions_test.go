package ops

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestD88ReviewAmendmentUndecidedOutsideManualDomain(t *testing.T) {
	for _, shape := range []string{"auto-only", "many-to-one-only"} {
		t.Run(shape, func(t *testing.T) {
			config, err := pipeline.LoadEmbeddedReference()
			if err != nil {
				t.Fatal(err)
			}
			for name, sub := range config.Pipeline.SubPipelines {
				for i := range sub.Transitions {
					if strings.HasPrefix(sub.Transitions[i].From, "code-planning-pair.") {
						if shape == "auto-only" {
							sub.Transitions[i].Trigger = "auto"
							sub.Transitions[i].Cardinality = "one-to-one"
						} else {
							sub.Transitions[i].Cardinality = "many-to-one"
						}
					}
				}
				config.Pipeline.SubPipelines[name] = sub
			}
			domain := NewPlanHandoffDomain(pipeline.NewResolver(config))
			original := handoffPlan("original", "code-planning-pair")
			original.PlanAmendment = &models.PlanAmendment{Pending: "correction", Corrections: []string{"correction"}, OriginalOutput: original.Output}
			correction := models.Task{ID: "correction", RolePair: original.RolePair, AmendsPlan: original.ID, Status: models.TaskStatusMerged, Output: original.Output}
			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{original, correction}
			state.Sprint.Scope.Planned = []string{original.ID, correction.ID}
			parent := state.FindTask(original.ID)
			if domain.InDomain(parent) {
				t.Fatal("fixture still has a manual reviewed handoff")
			}
			if !domain.PlanningCompleteEligible(state, parent) {
				t.Fatal("terminal review did not wake")
			}
			if domain.RecordUndecided(state, state, parent.ID, time.Now().UTC()) == nil {
				t.Fatal("undisposed terminal review was not recorded")
			}
			if domain.PlanningCompleteEligible(state, parent) || domain.RecordUndecided(state, state, parent.ID, time.Now().UTC()) != nil {
				t.Fatal("unchanged amendment wake/observation repeated")
			}
			state.FindTask(correction.ID).Description = "fresh review evidence"
			if !domain.PlanningCompleteEligible(state, parent) {
				t.Fatal("changed amendment input did not re-enable wake")
			}
		})
	}
}

func TestD88ReviewCorrectionsNeverJoinManyToOneCohort(t *testing.T) {
	for _, predecessor := range []models.TaskStatus{"", models.TaskStatusMerged, models.TaskStatusAbandoned} {
		t.Run("predecessor="+string(predecessor), func(t *testing.T) {
			root, _ := setupPhase2PipelineProceedTest(t)
			resolver, _, err := loadResolver(root)
			if err != nil {
				t.Fatal(err)
			}
			definition, err := buildTransitionDefFromPipeline(resolver, "us-to-coding")
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			first := models.Task{ID: "first", RolePair: definition.sourceRolePair, Status: models.TaskStatusMerged, ParentTasks: []string{"cohort"}, Created: now, SpecRef: "README.md"}
			second := first
			second.ID = "second"
			applied := first
			applied.ID = "applied"
			applied.AmendsPlan = first.ID
			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{first, second, applied}
			state.Sprint.Scope.Planned = []string{first.ID, second.ID, applied.ID}
			if predecessor != "" {
				old := applied
				old.ID = "quarantined"
				old.Status = predecessor
				state.Tasks = append(state.Tasks, old)
				state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, old.ID)
			}
			infos := []ManyToOneTransitionInfo{{Name: "us-to-coding", SourceRolePair: definition.sourceRolePair}}
			state.FindTask(first.ID).PlanAmendment = &models.PlanAmendment{Pending: applied.ID}
			if CountReadyManyToOneCohorts(state, infos) != 0 {
				t.Fatal("pending original admitted cohort")
			}
			state.FindTask(first.ID).PlanAmendment = &models.PlanAmendment{Applied: []string{applied.ID}}
			if CountReadyManyToOneCohorts(state, infos) != 1 || IsManyToOneReady(state.FindTask(applied.ID), state, infos) {
				t.Fatal("correction polluted automatic cohort readiness")
			}
			result := &ProceedResult{}
			if err := proceedInner(state, second.ID, "us-to-coding", definition, inheritedDepSet{}, resolver, now, result); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(result.CohortTaskIDs, []string{first.ID, second.ID}) || len(result.ChildTaskIDs) != 1 {
				t.Fatalf("cohort includes review records: %+v", result)
			}
			child := state.FindTask(result.ChildTaskIDs[0])
			if !slices.Equal(child.EffectiveParentTasks(), []string{first.ID, second.ID}) {
				t.Fatal(child.EffectiveParentTasks())
			}
			state.Tasks = slices.DeleteFunc(state.Tasks, func(task models.Task) bool { return task.ID == child.ID })
			recovered := &ProceedResult{}
			if err := proceedInner(state, second.ID, "us-to-coding", definition, inheritedDepSet{}, resolver, now, recovered); err != nil || len(recovered.ChildTaskIDs) != 1 {
				t.Fatalf("cohort recovery: %+v %v", recovered, err)
			}
			if len(state.FindTask(applied.ID).TransitionsExecuted) != 0 {
				t.Fatal("correction transition was marked")
			}
		})
	}
}

func TestD88ReviewNotesDoNotBlockUnrelatedAutomaticTransition(t *testing.T) {
	plan := handoffPlan("plan", "code-planning-pair")
	root, statePath := setupMixedSourceTest(t, plan)
	if _, err := RecordPlanCheck(root, PlanCheckInput{TaskID: plan.ID, Action: PlanCheckActionPass, Authority: orchestratorAuthority(), Notes: []models.PlanValidationNote{{OutputIndex: 0, Message: "Check hook coverage."}}}); err != nil {
		t.Fatal(err)
	}
	result, err := ExecuteAvailableTransitions(root, "auto")
	if err != nil || len(result) != 1 || len(result[0].ChildTaskIDs) != 1 {
		t.Fatalf("unrelated auto path: %+v %v", result, err)
	}
	auditID := result[0].ChildTaskIDs[0]
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.FindTask(auditID).ValidationNotes) != 0 {
		t.Fatal("audit received selected coding advice")
	}
	if err := bb.Modify(func(s *models.State) error {
		s.Tasks = slices.DeleteFunc(s.Tasks, func(task models.Task) bool { return task.ID == auditID })
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	recovery, err := ExecuteAvailableTransitions(root, "auto")
	if err != nil || len(recovery) != 1 || len(recovery[0].ChildTaskIDs) != 1 {
		t.Fatalf("unrelated auto recovery: %+v %v", recovery, err)
	}
	manual, err := ExecuteAvailableTransitions(root, "manual")
	if err != nil || len(manual) != 1 || len(manual[0].ChildTaskIDs) != 1 {
		t.Fatalf("selected manual handoff: %+v %v", manual, err)
	}
	state, err = bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.FindTask(auditID).ValidationNotes) != 0 || !reflect.DeepEqual(state.FindTask(manual[0].ChildTaskIDs[0]).ValidationNotes, []models.ValidationNote{{ParentTask: plan.ID, OutputIndex: 0, Message: "Check hook coverage."}}) {
		t.Fatal("advice did not remain confined to selected handoff")
	}
}
