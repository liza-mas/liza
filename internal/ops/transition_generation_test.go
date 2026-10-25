package ops

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

func perSubtaskDef() transitionDef {
	return transitionDef{
		sourceRolePair: "code-planning-pair",
		requiredStatus: models.TaskStatusMerged,
		targetStatus:   models.TaskStatus("DRAFT_CODE"),
		cardinality:    models.CardinalityPerSubtask,
		targetRolePair: "coding-pair",
		taskSlug:       "code",
	}
}

func cohortDef() transitionDef {
	return transitionDef{
		requiredStatus: models.TaskStatusMerged,
		targetStatus:   models.TaskStatus("DRAFT_ARCHITECTURE_MAIN"),
		cardinality:    models.CardinalityManyToOne,
		targetRolePair: "architecture-main-pair",
		taskType:       models.TaskTypeArchitecture,
		taskSlug:       "arm",
	}
}

// Generation and crash recovery build children through the same steps, so a child
// recreated by recovery equals the one the uninterrupted transition produced,
// including a selective inheritance, a sibling edge and a concrete dependency.
func TestRecoveryRecreatesChildIdenticalToFreshGeneration(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	upstream := func(id string) models.Task {
		return models.Task{ID: id, Status: models.TaskStatus("DRAFT_CODE"), RolePair: "coding-pair"}
	}
	state := &models.State{Tasks: []models.Task{
		upstream("up-code-0"), upstream("up-code-1"),
		{
			ID: "plan-1", Status: models.TaskStatusMerged, RolePair: "code-planning-pair", EpicRef: "epic.md", RCARequired: true,
			Output: []models.OutputEntry{
				{Desc: "a", DoneWhen: "a", Scope: "a", SpecRef: "s.md"},
				{Desc: "b", DoneWhen: "b", Scope: "b", SpecRef: "s.md", DependsOn: []string{"0"}, TaskDependsOn: []string{"up-code-0"}},
				{Desc: "c", DoneWhen: "c", Scope: "c", SpecRef: "s.md", InheritInputs: &models.InheritInputs{
					Mode: models.InheritModeSelected, Selections: []models.InputSelection{{UpstreamTask: "up", Outputs: []int{1}}},
				}},
			},
		},
	}}
	inherited := inheritedDepSet{
		all:        []string{"up-code-0", "up-code-1"},
		byUpstream: map[string][]string{"up": {"up-code-0", "up-code-1"}},
		selectable: true,
	}
	tDef := perSubtaskDef()

	fresh := &ProceedResult{}
	if err := proceedInner(state, "plan-1", "t", tDef, inherited, nil, now, fresh); err != nil {
		t.Fatalf("fresh generation: %v", err)
	}
	if want := []string{"plan-1-code-0", "plan-1-code-1", "plan-1-code-2"}; !slices.Equal(fresh.ChildTaskIDs, want) {
		t.Fatalf("fresh children = %v, want %v", fresh.ChildTaskIDs, want)
	}
	want := *state.FindTask("plan-1-code-2")
	if !slices.Equal(want.DependsOn, []string{"up-code-1"}) {
		t.Fatalf("selective child depends_on = %v, want only the selected upstream child", want.DependsOn)
	}

	recoveryState := db.CloneState(state)
	recoveryState.Tasks = slices.DeleteFunc(recoveryState.Tasks, func(task models.Task) bool { return task.ID == "plan-1-code-2" })
	recovered := &ProceedResult{}
	if err := proceedInner(recoveryState, "plan-1", "t", tDef, inherited, nil, now, recovered); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	if !slices.Equal(recovered.ChildTaskIDs, []string{"plan-1-code-2"}) {
		t.Fatalf("recovered children = %v, want only the missing one", recovered.ChildTaskIDs)
	}
	if got := *recoveryState.FindTask("plan-1-code-2"); !reflect.DeepEqual(got, want) {
		t.Fatalf("recreated child differs from the fresh one:\n got %+v\nwant %+v", got, want)
	}
	history := recoveryState.FindTask("plan-1").History
	if last := history[len(history)-1]; last.Event != models.TaskEventTransitionCrashRecov || last.Extra["recovered_children"] != 1 || last.Extra["transition"] != "t" {
		t.Fatalf("recovery history = %+v", last)
	}
}

func cohortState(executed bool) *models.State {
	cohort := makeManyToOneCohort("ep-1", "us-writing-pair", models.TaskStatusMerged, "specs/goal.md", 2)
	if executed {
		for i := range cohort {
			cohort[i].TransitionsExecuted = map[string]bool{"us-to-arch": true}
		}
	}
	// Spare capacity keeps the trigger task pointer valid across the child append.
	tasks := make([]models.Task, 0, len(cohort)+4)
	return &models.State{Tasks: append(tasks, cohort...)}
}

// The many-to-one history carries the cardinality as a plain string, so the
// persisted entry keeps the value it had before the vocabulary was typed.
func TestManyToOneHistoryRecordsCardinalityAsString(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	state := cohortState(false)
	result := &ProceedResult{}
	if err := proceedInner(state, "ep-1-us-0", "us-to-arch", cohortDef(), inheritedDepSet{}, nil, now, result); err != nil {
		t.Fatalf("proceedInner: %v", err)
	}
	if !slices.Equal(result.ChildTaskIDs, []string{"ep-1-arm"}) || !slices.Equal(result.CohortTaskIDs, []string{"ep-1-us-0", "ep-1-us-1"}) {
		t.Fatalf("children %v cohort %v", result.ChildTaskIDs, result.CohortTaskIDs)
	}
	history := state.FindTask("ep-1-us-0").History
	if len(history) != 1 {
		t.Fatalf("trigger history = %+v", history)
	}
	extra := history[0].Extra
	if extra["cardinality"] != "many-to-one" || extra["cohort_size"] != 2 || extra["cohort_parent"] != "ep-1" || extra["children"] != 1 || extra["transition"] != "us-to-arch" {
		t.Fatalf("history extra = %#v", extra)
	}
	if _, ok := extra["cardinality"].(string); !ok {
		t.Fatalf("cardinality is %T, want string", extra["cardinality"])
	}
}

func TestManyToOneRecoveryRecreatesChildAndRecordsCohort(t *testing.T) {
	t.Parallel()
	state := cohortState(true)
	result := &ProceedResult{}
	err := recoverCrashedTransition(state, state.FindTask("ep-1-us-0"), "ep-1-us-0", "us-to-arch", cohortDef(), inheritedDepSet{all: []string{"dep"}}, nil, time.Now().UTC(), result)
	if err != nil {
		t.Fatalf("recoverCrashedTransition: %v", err)
	}
	child := state.FindTask("ep-1-arm")
	if child == nil || !slices.Equal(child.ParentTasks, []string{"ep-1-us-0", "ep-1-us-1"}) || !slices.Equal(child.DependsOn, []string{"dep"}) {
		t.Fatalf("recreated child = %+v", child)
	}
	history := state.FindTask("ep-1-us-0").History
	if len(history) != 1 || history[0].Event != models.TaskEventTransitionCrashRecov {
		t.Fatalf("history = %+v", history)
	}
	extra := history[0].Extra
	if extra["cardinality"] != "many-to-one" || extra["cohort_parent"] != "ep-1" || extra["recovered_children"] != 1 {
		t.Fatalf("history extra = %#v", extra)
	}
}

// A cardinality outside the closed set is refused by generation and is not a
// recoverable transition: recovery reports it as already executed.
func TestUnsupportedCardinalityDispatch(t *testing.T) {
	t.Parallel()
	tDef := perSubtaskDef()
	tDef.cardinality = "fan-out"
	task := models.Task{ID: "plan-1", Status: models.TaskStatusMerged, RolePair: "code-planning-pair", SpecRef: "s.md"}
	state := &models.State{Tasks: []models.Task{task}}

	err := proceedInner(state, "plan-1", "t", tDef, inheritedDepSet{}, nil, time.Now().UTC(), &ProceedResult{})
	if err == nil || !strings.Contains(err.Error(), `unsupported cardinality "fan-out" for transition "t"`) {
		t.Fatalf("generation: %v", err)
	}
	if len(state.Tasks[0].TransitionsExecuted) != 0 || len(state.Tasks) != 1 {
		t.Fatalf("refused generation changed state: %+v", state.Tasks)
	}

	err = recoverCrashedTransition(state, &state.Tasks[0], "plan-1", "t", tDef, inheritedDepSet{}, nil, time.Now().UTC(), &ProceedResult{})
	if err == nil || !strings.Contains(err.Error(), "transition already executed") {
		t.Fatalf("recovery: %v", err)
	}
}
