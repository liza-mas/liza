package models_test

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// D-70: a selected-child slot follows a same-pair replace-task successor as it
// follows replan lineage (ADR-0184). Every other supersession stays a
// permanent retirement, so the slot keeps naming the retired child.

func slotChild(id string, status models.TaskStatus, supersededBy ...string) models.Task {
	return models.Task{ID: id, RolePair: "code-planning-pair", Status: status, ParentTasks: []string{"provider"}, SupersededBy: supersededBy}
}

// replacedBy records replace-task evidence on task: the completion entry, or
// (completion false) the supersession entry replace-task writes first.
func replacedBy(task models.Task, successor string, completion bool) models.Task {
	event := models.TaskEventSuperseded
	if completion {
		event = models.TaskEventReplacementCommitted
	}
	task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: event, Extra: map[string]any{"replacement_task_id": successor}})
	return task
}

func replannedChild(id string) models.Task {
	task := slotChild(id, models.TaskStatusMerged)
	task.TransitionsExecuted = map[string]bool{"replanned": true}
	return task
}

func replanOf(task models.Task, original string) models.Task {
	task.Supersedes = &original
	return task
}

func TestEffectiveProviderChildrenFollowsReplaceTaskSuccessor(t *testing.T) {
	t.Parallel()
	cfg, err := pipeline.LoadFromBytes(embedded.PipelineConfigContent())
	if err != nil {
		t.Fatal(err)
	}
	resolver := pipeline.NewResolver(cfg)
	const projected = "provider-cp-0"
	superseded := func(successors ...string) models.Task {
		return slotChild(projected, models.TaskStatusSuperseded, successors...)
	}
	draft := func(id string) models.Task { return slotChild(id, models.TaskStatusDraftCodingPlan) }
	otherParents := draft("r1")
	otherParents.ParentTasks = []string{"other"}
	otherPair := draft("r1")
	otherPair.RolePair = "architecture-pair"
	for name, tc := range map[string]struct {
		tasks []models.Task
		want  string
	}{
		"replace-task completion evidence":   {tasks: []models.Task{replacedBy(superseded("r1"), "r1", true), draft("r1")}, want: "r1"},
		"replace-task supersession evidence": {tasks: []models.Task{replacedBy(superseded("r1"), "r1", false), draft("r1")}, want: "r1"},
		"supersede-task, no evidence":        {tasks: []models.Task{superseded("r1"), draft("r1")}, want: projected},
		"evidence names another task":        {tasks: []models.Task{replacedBy(superseded("r1"), "other", true), draft("r1")}, want: projected},
		"successor with other parents":       {tasks: []models.Task{replacedBy(superseded("r1"), "r1", true), otherParents}, want: projected},
		"successor in another role pair":     {tasks: []models.Task{replacedBy(superseded("r1"), "r1", true), otherPair}, want: projected},
		"two successors":                     {tasks: []models.Task{replacedBy(superseded("r1", "r2"), "r1", true), draft("r1"), draft("r2")}, want: projected},
		"missing successor":                  {tasks: []models.Task{replacedBy(superseded("r1"), "r1", true)}, want: projected},
		"replace then replan": {tasks: []models.Task{
			replacedBy(superseded("r1"), "r1", true), replannedChild("r1"), replanOf(draft("r1-replan-1"), "r1"),
		}, want: "r1-replan-1"},
		"replan then replace": {tasks: []models.Task{
			replannedChild(projected), replacedBy(replanOf(slotChild("replan-1", models.TaskStatusSuperseded, "r1"), projected), "r1", true), draft("r1"),
		}, want: "r1"},
		"cycle": {tasks: []models.Task{
			replacedBy(superseded("r1"), "r1", true), replacedBy(slotChild("r1", models.TaskStatusSuperseded, projected), projected, true),
		}, want: projected},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// GIVEN the provider whose output 0 a consumer selects
			provider := models.Task{ID: "provider", RolePair: "architecture-pair", Status: models.TaskStatusMerged,
				TransitionsExecuted: map[string]bool{"architecture-to-code-plan": true}, Output: []models.OutputEntry{{Desc: "plan"}}}
			state := &models.State{Tasks: append([]models.Task{provider}, tc.tasks...)}
			dep := models.ProviderDependency{ProviderTask: "provider", Transition: "architecture-to-code-plan", Outputs: []int{0}}

			// WHEN the selection is resolved
			_, children, err := models.EffectiveProviderChildren(dep, state, resolver)

			// THEN the slot names the expected task
			if err != nil {
				t.Fatal(err)
			}
			if len(children) != 1 || children[0] != tc.want {
				t.Fatalf("slot resolves to %v, want [%s]", children, tc.want)
			}
		})
	}
}

// Guard: depends_on lineage (ADR-0184 Decisions 1 and 2) stays replan-only.
func TestReplanSuccessorIgnoresReplaceTaskSuccessor(t *testing.T) {
	t.Parallel()
	state := &models.State{Tasks: []models.Task{
		replacedBy(slotChild("provider-cp-0", models.TaskStatusSuperseded, "r1"), "r1", true),
		slotChild("r1", models.TaskStatusDraftCodingPlan),
	}}
	if successor, ok := models.ReplanSuccessor(state, "provider-cp-0"); ok {
		t.Fatalf("ReplanSuccessor followed a replace-task successor to %s", successor.ID)
	}
}
