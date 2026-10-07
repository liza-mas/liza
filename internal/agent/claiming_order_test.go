package agent

import (
	"reflect"
	"slices"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestOrderDoerCandidates_NumericPriorityOverridesRework(t *testing.T) {
	pr := testPipelineResolver(t)
	owner := "coder-1"
	fresh := schedulingTask("fresh")
	owned := schedulingTask("owned")
	owned.Status, owned.AssignedTo, owned.Priority = models.TaskStatusRejected, &owner, 2
	state := &models.State{Tasks: []models.Task{owned, fresh}}
	got := orderDoerCandidates([]*models.Task{&state.Tasks[0], &state.Tasks[1]}, state, owner, pr)
	if len(got) != 1 || got[0].ID != fresh.ID {
		t.Fatalf("ordered candidates = %v, want only higher-priority fresh task", taskIDsFromCandidates(got))
	}
}

func TestOrderDoerCandidates_ReworkClassesBeforeImpact(t *testing.T) {
	pr := schedulingCustomResolver{PipelineResolver: testPipelineResolver(t)}
	owner := "coder-1"
	state := &models.State{Tasks: []models.Task{
		schedulingTask("fresh"), schedulingTask("other-rejection"),
		schedulingTask("integration-failure"), schedulingTask("owned-rejection"),
		schedulingTask("consumer", "fresh"),
	}}
	state.Tasks[1].Status = models.TaskStatusRejected
	state.Tasks[2].Status = models.TaskStatusIntegrationFailed
	state.Tasks[3].RolePair, state.Tasks[3].Status, state.Tasks[3].AssignedTo = "bespoke", "NEEDS_FIX", &owner
	candidates := []*models.Task{&state.Tasks[0], &state.Tasks[1], &state.Tasks[2], &state.Tasks[3]}
	before := slices.Clone(candidates)
	got := orderDoerCandidates(candidates, state, owner, pr)
	if len(got) != len(candidates) || got[0].ID != "owned-rejection" || got[3].ID != "fresh" {
		t.Fatalf("ordered candidates = %v, want own custom rejection, other rework, fresh", taskIDsFromCandidates(got))
	}
	middle := taskIDsFromCandidates(got[1:3])
	slices.Sort(middle)
	if !slices.Equal(middle, []string{"integration-failure", "other-rejection"}) {
		t.Fatalf("second rework class = %v, want rejection and integration failure", middle)
	}
	if !slices.Equal(candidates, before) {
		t.Fatal("candidate ordering mutated the caller's input slice")
	}
}

func TestOrderDoerCandidates_PreservesEveryExactTie(t *testing.T) {
	pr := testPipelineResolver(t)
	state := &models.State{Tasks: []models.Task{schedulingTask("a"), schedulingTask("b"), schedulingTask("c")}}
	candidates := []*models.Task{&state.Tasks[2], &state.Tasks[0], &state.Tasks[1]}
	before := slices.Clone(candidates)
	got := orderDoerCandidates(candidates, state, "coder-1", pr)
	ids := taskIDsFromCandidates(got)
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"a", "b", "c"}) || !slices.Equal(candidates, before) {
		t.Fatalf("ties = %v, input = %v; want all ties without input mutation", ids, taskIDsFromCandidates(candidates))
	}
	if len(orderDoerCandidates(nil, state, "coder-1", pr)) != 0 {
		t.Fatal("empty candidates produced work")
	}
}

func TestDoerDependencyImpact_TransitiveDiamondCountsUniqueConsumers(t *testing.T) {
	pr := testPipelineResolver(t)
	state := &models.State{Tasks: []models.Task{
		schedulingTask("transitive"), schedulingTask("direct"),
		schedulingTask("left", "transitive"), schedulingTask("right", "transitive"),
		schedulingTask("sink", "left", "right"), schedulingTask("single", "direct"),
	}}
	candidates := []*models.Task{&state.Tasks[1], &state.Tasks[0]}
	impact := doerDependencyImpact(state, pr, candidates)
	if impact["transitive"] != 3 || impact["direct"] != 1 {
		t.Fatalf("impact = %v, want transitive=3 (diamond sink once), direct=1", impact)
	}
	got := orderDoerCandidates(candidates, state, "coder-1", pr)
	if len(got) != 2 || got[0].ID != "transitive" {
		t.Fatalf("ordered candidates = %v, want transitive provider before direct provider", taskIDsFromCandidates(got))
	}
}

func TestDoerDependencyImpact_ExcludesTerminalAndPipelineCleanConsumers(t *testing.T) {
	pr := schedulingCustomResolver{PipelineResolver: testPipelineResolver(t)}
	state := &models.State{Tasks: []models.Task{
		schedulingTask("provider"), schedulingTask("active", "provider"),
		schedulingTask("merged", "provider"), schedulingTask("abandoned", "provider"),
		schedulingTask("superseded", "provider"), schedulingTask("clean", "provider"),
	}}
	state.Tasks[2].Status = models.TaskStatusMerged
	state.Tasks[3].Status = models.TaskStatusAbandoned
	state.Tasks[4].Status = models.TaskStatusSuperseded
	state.Tasks[5].RolePair, state.Tasks[5].Status = "bespoke", "DONE_CUSTOM"
	impact := doerDependencyImpact(state, pr, []*models.Task{&state.Tasks[0]})
	if impact["provider"] != 1 {
		t.Fatalf("impact = %v, want only active consumer counted", impact)
	}
}

func TestDoerDependencyImpact_FollowsNestedSplitReplacements(t *testing.T) {
	pr := testPipelineResolver(t)
	state := &models.State{Tasks: []models.Task{
		schedulingTask("a"), schedulingTask("b"), schedulingTask("done"),
		schedulingTask("old"), schedulingTask("nested"), schedulingTask("consumer", "old"),
	}}
	state.Tasks[2].Status = models.TaskStatusMerged
	state.Tasks[3].Status, state.Tasks[3].SupersededBy = models.TaskStatusSuperseded, []string{"nested", "done"}
	state.Tasks[4].Status, state.Tasks[4].SupersededBy = models.TaskStatusSuperseded, []string{"a", "b"}
	impact := doerDependencyImpact(state, pr, []*models.Task{&state.Tasks[0], &state.Tasks[1], &state.Tasks[2]})
	if impact["a"] != 1 || impact["b"] != 1 || impact["done"] != 0 {
		t.Fatalf("impact = %v, want each unfinished replacement=1, merged replacement=0", impact)
	}
}

func TestDoerDependencyImpact_UnbornSelectedChildrenFavorProducer(t *testing.T) {
	pr := testPipelineResolver(t)
	provider := schedulingTask("provider")
	provider.RolePair, provider.Status = "architecture-pair", "DRAFT_ARCHITECTURE"
	consumer := schedulingTask("consumer")
	consumer.ProviderDependencies = []models.ProviderDependency{{ProviderTask: "provider", Transition: "architecture-to-code-plan", Outputs: []int{0, 2}}}
	state := &models.State{Tasks: []models.Task{provider, schedulingTask("independent"), consumer, schedulingTask("downstream", "consumer")}}
	candidates := []*models.Task{&state.Tasks[1], &state.Tasks[0]}
	impact := doerDependencyImpact(state, pr, candidates)
	if impact["provider"] != 2 || impact["independent"] != 0 {
		t.Fatalf("impact = %v, want two materialized consumers for pending producer", impact)
	}
	got := orderDoerCandidates(candidates, state, "architect-1", pr)
	if got[0].ID != "provider" {
		t.Fatalf("ordered candidates = %v, want pending producer first", taskIDsFromCandidates(got))
	}
}

func TestDoerDependencyImpact_AllSelectedBornChildrenContribute(t *testing.T) {
	pr := testPipelineResolver(t)
	state := schedulingProviderState()
	candidates := []*models.Task{&state.Tasks[1], &state.Tasks[2], &state.Tasks[3]}
	impact := doerDependencyImpact(state, pr, candidates)
	if impact["provider-cp-0"] != 2 || impact["provider-cp-2"] != 2 || impact["provider-cp-1"] != 0 {
		t.Fatalf("impact = %v, want both selected children=2, unselected=0", impact)
	}
	state.Tasks[1].Status = models.TaskStatusMerged
	impact = doerDependencyImpact(state, pr, candidates)
	if impact["provider-cp-0"] != 0 || impact["provider-cp-2"] != 2 {
		t.Fatalf("impact after first child merges = %v, want remaining selected child only", impact)
	}
}

// D-60: a replanned selected child's successor carries the declared demand.
func TestDoerDependencyImpact_ReplannedChildSuccessorContributes(t *testing.T) {
	pr := testPipelineResolver(t)
	state := schedulingProviderState()
	state.Tasks[1].Status = models.TaskStatusMerged
	state.Tasks[1].TransitionsExecuted = map[string]bool{"replanned": true}
	successor := schedulingTask("provider-cp-0-replan-1")
	successor.RolePair, successor.Status, successor.ParentTasks = "code-planning-pair", "DRAFT_CODING_PLAN", []string{"provider"}
	successor.Supersedes = &state.Tasks[1].ID
	state.Tasks = append(state.Tasks, successor)
	impact := doerDependencyImpact(state, pr, []*models.Task{&state.Tasks[len(state.Tasks)-1], &state.Tasks[3]})
	if impact["provider-cp-0-replan-1"] != 2 || impact["provider-cp-2"] != 2 {
		t.Fatalf("impact = %v, want the replan successor to carry both consumers", impact)
	}
}

func TestDoerDependencyImpact_InvalidProviderDeclarationsDoNotBoost(t *testing.T) {
	pr := testPipelineResolver(t)
	for _, tc := range []struct {
		name   string
		mutate func(*models.State)
	}{
		{"retired provider", func(s *models.State) { s.Tasks[0].Status = models.TaskStatusSuperseded }},
		{"retired selected child", func(s *models.State) { s.Tasks[3].Status = models.TaskStatusAbandoned }},
		{"wrong child provenance", func(s *models.State) { s.Tasks[3].ParentTasks = []string{"foreign"} }},
		{"wrong child role", func(s *models.State) { s.Tasks[3].RolePair = "coding-pair" }},
		{"unknown transition", func(s *models.State) { s.Tasks[4].ProviderDependencies[0].Transition = "missing" }},
		{"out of range", func(s *models.State) { s.Tasks[4].ProviderDependencies[0].Outputs = []int{0, 3} }},
		{"unstable Kind", func(s *models.State) { s.Tasks[0].Output[2].Kind = "bootstrap" }},
		{"missing provider", func(s *models.State) { s.Tasks[0].ID = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := schedulingProviderState()
			tc.mutate(state)
			impact := doerDependencyImpact(state, pr, []*models.Task{&state.Tasks[0], &state.Tasks[1], &state.Tasks[3]})
			for _, candidate := range []string{state.Tasks[0].ID, "provider-cp-0", "provider-cp-2"} {
				if impact[candidate] != 0 {
					t.Fatalf("invalid declaration boosted %s: %v", candidate, impact)
				}
			}
		})
	}
}

func TestDoerDependencyImpact_CyclesTerminateAndExcludeSelf(t *testing.T) {
	pr := testPipelineResolver(t)
	state := &models.State{Tasks: []models.Task{
		schedulingTask("a", "b", "a"), schedulingTask("b", "a"), schedulingTask("consumer", "a"),
		schedulingTask("old-a"), schedulingTask("old-b"), schedulingTask("invalid", "old-a"),
	}}
	state.Tasks[3].Status, state.Tasks[3].SupersededBy = models.TaskStatusSuperseded, []string{"old-b"}
	state.Tasks[4].Status, state.Tasks[4].SupersededBy = models.TaskStatusSuperseded, []string{"old-a"}
	impact := doerDependencyImpact(state, pr, []*models.Task{&state.Tasks[0], &state.Tasks[1]})
	if !reflect.DeepEqual(impact, map[string]int{"a": 2, "b": 2}) {
		t.Fatalf("impact = %v, want each cyclic candidate's two other consumers, excluding self", impact)
	}
}

func schedulingTask(id string, dependencies ...string) models.Task {
	return models.Task{ID: id, RolePair: "coding-pair", Status: models.TaskStatusReady, Priority: 1, DependsOn: dependencies}
}

func schedulingProviderState() *models.State {
	provider := schedulingTask("provider")
	provider.RolePair, provider.Status = "architecture-pair", models.TaskStatusMerged
	provider.Output = []models.OutputEntry{{}, {}, {}}
	provider.TransitionsExecuted = map[string]bool{"architecture-to-code-plan": true}
	children := []models.Task{schedulingTask("provider-cp-0"), schedulingTask("provider-cp-1"), schedulingTask("provider-cp-2")}
	for i := range children {
		children[i].RolePair, children[i].Status, children[i].ParentTasks = "code-planning-pair", "DRAFT_CODING_PLAN", []string{"provider"}
	}
	consumer := schedulingTask("consumer")
	consumer.ProviderDependencies = []models.ProviderDependency{{ProviderTask: "provider", Transition: "architecture-to-code-plan", Outputs: []int{0, 2}}}
	return &models.State{Tasks: []models.Task{provider, children[0], children[1], children[2], consumer, schedulingTask("downstream", "consumer")}}
}

type schedulingCustomResolver struct {
	models.PipelineResolver
}

func (r schedulingCustomResolver) RejectedStatus(rolePair string) (models.TaskStatus, error) {
	if rolePair == "bespoke" {
		return "NEEDS_FIX", nil
	}
	return r.PipelineResolver.RejectedStatus(rolePair)
}

func (r schedulingCustomResolver) CleanStatus(rolePair string) (models.TaskStatus, error) {
	if rolePair == "bespoke" {
		return "DONE_CUSTOM", nil
	}
	return r.PipelineResolver.(interface {
		CleanStatus(string) (models.TaskStatus, error)
	}).CleanStatus(rolePair)
}
