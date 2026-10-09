package ops

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestValidationNotesRecoveryPreservesSelectedGuidanceAndContract(t *testing.T) {
	plan := handoffPlan("plan", "code-planning-pair")
	plan.Output = append(plan.Output, models.OutputEntry{Desc: "second", DoneWhen: "pass", Scope: "pkg/b", SpecRef: "README.md", Validation: []string{"check-b"}})
	root, statePath := setupPlanCheckTest(t, plan)
	testhelpers.SetupTestGitRepo(t, root)
	input := PlanCheckInput{TaskID: plan.ID, Action: PlanCheckActionPass, Authority: orchestratorAuthority(), Notes: []models.PlanValidationNote{{OutputIndex: 1, Message: "Confirm intended hooks ran."}}}
	if _, err := RecordPlanCheck(root, input); err != nil {
		t.Fatal(err)
	}
	bb := db.For(statePath)
	head := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	if err := bb.Modify(func(s *models.State) error {
		s.Sprint.Status = models.SprintStatusCompleted
		s.FindTask(plan.ID).BaseCommit, s.FindTask(plan.ID).ReviewCommit, s.FindTask(plan.ID).MergeCommit = &head, &head, &head
		s.FindTask(plan.ID).TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := Proceed(root, plan.ID, "code-plan-to-coding")
	if err != nil || len(result.ChildTaskIDs) != 2 {
		t.Fatalf("recover: %+v %v", result, err)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state.FindTask(plan.ID).Output, plan.Output) {
		t.Fatal("notes changed frozen output")
	}
	for i, id := range result.ChildTaskIDs {
		child := state.FindTask(id)
		if !reflect.DeepEqual(child.Validation, plan.Output[i].Validation) || child.Scope != plan.Output[i].Scope || child.DoneWhen != plan.Output[i].DoneWhen {
			t.Fatal("recovery changed canonical allocation")
		}
		if i == 0 && len(child.ValidationNotes) != 0 {
			t.Fatal("unselected child received guidance")
		}
		if i == 1 && !reflect.DeepEqual(child.ValidationNotes, []models.ValidationNote{{ParentTask: plan.ID, OutputIndex: 1, Message: input.Notes[0].Message}}) {
			t.Fatalf("guidance lost provenance: %+v", child.ValidationNotes)
		}
	}
}

func TestValidationNotesDedupRefusalsAreAtomic(t *testing.T) {
	for _, afterPass := range []bool{false, true} {
		t.Run(map[bool]string{false: "before pass", true: "before generation"}[afterPass], func(t *testing.T) {
			plan := handoffPlan("plan", "code-planning-pair")
			plan.Output[0].Kind = "bootstrap-precommit"
			root, statePath := setupPlanCheckTest(t, plan)
			input := PlanCheckInput{TaskID: plan.ID, Action: PlanCheckActionPass, Authority: orchestratorAuthority(), Notes: []models.PlanValidationNote{{OutputIndex: 0, Message: "Hook evidence."}}}
			if afterPass {
				if _, err := RecordPlanCheck(root, input); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.For(statePath).Modify(func(s *models.State) error {
				incumbent := testhelpers.BuildTaskByStatus("incumbent", models.TaskStatusReady, s.Tasks[0].Created)
				incumbent.Kind = "bootstrap-precommit"
				s.Tasks = append(s.Tasks, incumbent)
				s.Sprint.Scope.Planned = append(s.Sprint.Scope.Planned, incumbent.ID)
				s.Sprint.Status = models.SprintStatusCompleted
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if afterPass {
				_, err = Proceed(root, plan.ID, "code-plan-to-coding")
			} else {
				_, err = RecordPlanCheck(root, input)
			}
			if err == nil || !strings.Contains(err.Error(), "deduplicated") {
				t.Fatalf("dedup: %v", err)
			}
			after, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("refused selection changed state")
			}
		})
	}
}
