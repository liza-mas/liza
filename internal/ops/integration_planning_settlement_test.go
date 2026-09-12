package ops

import (
	"reflect"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

func TestIntegrationWaitsForUpstreamPlanning(t *testing.T) {
	fixture := newReconcileFixture(t, false)
	for _, tc := range []struct {
		name       string
		rolePair   string
		status     models.TaskStatus
		withOutput bool
	}{
		{"epic master awaiting merge", "epic-planning-main-pair", "EPIC_PLAN_MAIN_APPROVED", true},
		{"epic master unexpanded", "epic-planning-main-pair", models.TaskStatusMerged, true},
		{"epic planning active", "epic-planning-pair", "EPIC_PLANNING", false},
		{"epic output unexpanded", "epic-planning-pair", models.TaskStatusMerged, true},
		{"story fan in unconsumed", "us-writing-pair", models.TaskStatusMerged, false},
		{"architecture master unexpanded", "architecture-main-pair", models.TaskStatusMerged, true},
		{"architecture awaiting merge", "architecture-pair", "ARCHITECTURE_APPROVED", false},
		{"architecture output unexpanded", "architecture-pair", models.TaskStatusMerged, true},
		{"code plan output unexpanded", "code-planning-pair", models.TaskStatusMerged, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := fixture.readState(t)
			task := progressTask("upstream", tc.rolePair, tc.status)
			if tc.withOutput {
				task.Output = []models.OutputEntry{{Desc: "downstream work"}}
			}
			state.Tasks = []models.Task{task}
			for _, frozen := range []bool{false, true} {
				if frozen {
					state.Goal.Integration.ContributingSet = &models.IntegrationContributingSet{Scopes: []models.IntegrationScopeSnapshot{}}
				}
				decision, err := EvaluateLiveIntegrationProgress(state, fixture.projectRoot)
				if err != nil {
					t.Fatal(err)
				}
				if decision.PlanningSettled || decision.FreezeContributingSet || decision.GlobalRequest != nil || decision.IntegrationComplete {
					t.Fatalf("frozen=%v: premature integration decision: %+v", frozen, decision)
				}
				if decision.Waiting == nil || decision.Waiting.Code != integrationProgressWaitingPlanning {
					t.Fatalf("frozen=%v: waiting = %+v, want planning_unsettled", frozen, decision.Waiting)
				}
			}
		})
	}
}

func TestUnsettledPreIntegrationPlanningTasks(t *testing.T) {
	fixture := newReconcileFixture(t, false)
	cfg, err := pipeline.LoadFrozen(fixture.projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := pipeline.NewResolver(cfg).SlicedIntegrationCapability()
	if err != nil {
		t.Fatal(err)
	}

	settledTasks := []models.Task{}
	for rolePair, transitions := range capability.PreIntegrationPlanningTransitions {
		task := progressTask(rolePair, rolePair, models.TaskStatusMerged)
		task.Output = []models.OutputEntry{{Desc: "consumed work"}}
		task.TransitionsExecuted = map[string]bool{}
		for _, transition := range transitions {
			task.TransitionsExecuted[transition.Name] = true
		}
		settledTasks = append(settledTasks, task)
	}
	assertPending := func(t *testing.T, state *models.State, want []string) {
		t.Helper()
		got, err := UnsettledPreIntegrationPlanningTasks(state, capability)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("unsettled tasks = %v, want %v", got, want)
		}
	}

	t.Run("all upstream transitions consumed", func(t *testing.T) {
		state := integrationProgressState(settledTasks...)
		assertPending(t, state, nil)
		decision := evaluateProgress(t, state, capability, "head")
		if !decision.PlanningSettled || decision.GlobalRequest == nil || len(decision.Coverage) != 0 {
			t.Fatalf("settled zero-scope planning did not reach direct global: %+v", decision)
		}
	})
	t.Run("empty per-subtask and abandoned outputs are settled", func(t *testing.T) {
		abandoned := progressTask("abandoned", "epic-planning-main-pair", models.TaskStatusAbandoned)
		abandoned.Output = []models.OutputEntry{{Desc: "retired work"}}
		state := integrationProgressState(abandoned, progressTask("empty", "epic-planning-pair", models.TaskStatusMerged))
		assertPending(t, state, nil)
	})
	t.Run("unrelated transition does not consume a story fan in", func(t *testing.T) {
		story := progressTask("story", "us-writing-pair", models.TaskStatusMerged)
		story.TransitionsExecuted = map[string]bool{"unrelated": true}
		state := integrationProgressState(story, progressTask("epic", "epic-planning-pair", models.TaskStatusBlocked))
		assertPending(t, state, []string{"epic", "story"})
	})
	t.Run("repair planning and replacement ancestry stay outside cohort", func(t *testing.T) {
		analysis := progressAnalysis("analysis", "global:1", models.IntegrationAnalysisPhaseGlobal, 1, "head")
		repair := progressChild(progressTask("repair", "epic-planning-main-pair", models.TaskStatusSuperseded), analysis.ID)
		repair.SupersededBy = []string{"replacement"}
		replacement := progressTask("replacement", "architecture-pair", models.TaskStatusBlocked)
		replacement.Supersedes = progressString(repair.ID)
		assertPending(t, integrationProgressState(analysis, repair, replacement), nil)
	})
	t.Run("replacement planning still blocks original cohort", func(t *testing.T) {
		old := progressTask("old", "epic-planning-pair", models.TaskStatusSuperseded)
		old.SupersededBy = []string{"new"}
		replacement := progressTask("new", "epic-planning-pair", models.TaskStatusMerged)
		replacement.Output = []models.OutputEntry{{Desc: "unexpanded replacement"}}
		state := integrationProgressState(old, replacement)
		assertPending(t, state, []string{"new"})
		replacement.TransitionsExecuted = map[string]bool{"epic-to-us": true}
		state.Tasks[1] = replacement
		assertPending(t, state, nil)
	})
	t.Run("all eligible outgoing transitions must be consumed", func(t *testing.T) {
		custom := pipeline.SlicedIntegrationCapability{PreIntegrationPlanningTransitions: map[string][]pipeline.TransitionDef{
			"custom": {{Name: "first", Cardinality: "one-to-one"}, {Name: "second", Cardinality: "per-subtask"}},
		}}
		task := progressTask("source", "custom", models.TaskStatusMerged)
		task.Output = []models.OutputEntry{{Desc: "work"}}
		task.TransitionsExecuted = map[string]bool{"first": true}
		pending, err := UnsettledPreIntegrationPlanningTasks(integrationProgressState(task), custom)
		if err != nil || !reflect.DeepEqual(pending, []string{"source"}) {
			t.Fatalf("partially consumed source: pending=%v err=%v", pending, err)
		}
	})
	t.Run("runtime no follow up suppresses frozen cross pipeline transitions", func(t *testing.T) {
		story := progressTask("story", "us-writing-pair", models.TaskStatusMerged)
		state := integrationProgressState(story)
		state.Goal.Integration.ContributingSet = &models.IntegrationContributingSet{Scopes: []models.IntegrationScopeSnapshot{}}
		assertPending(t, state, []string{"story"})
		state.Config.NoFollowUp = true
		assertPending(t, state, nil)
		decision := evaluateProgress(t, state, capability, "head")
		if !decision.PlanningSettled || decision.GlobalRequest == nil {
			t.Fatalf("disabled follow-up should not block direct global: %+v", decision)
		}
		// Local fan-out remains enabled under the same runtime policy.
		epic := progressTask("epic", "epic-planning-main-pair", models.TaskStatusMerged)
		epic.Output = []models.OutputEntry{{Desc: "local decomposition"}}
		state.Tasks = append(state.Tasks, epic)
		assertPending(t, state, []string{"epic"})
	})
}

func TestRecoveredIntegrationWaitsBeforeGenerationTwo(t *testing.T) {
	fixture := newReconcileFixture(t, false)
	state := fixture.readState(t)
	planner := progressTask("epic", "epic-planning-pair", models.TaskStatusMerged)
	planner.Output = []models.OutputEntry{{Desc: "future work"}}
	retired := progressAnalysis("integration-global-1", "global:1", models.IntegrationAnalysisPhaseGlobal, 1, "old-head")
	retired.Status = models.TaskStatusAbandoned
	state.Tasks = []models.Task{planner, retired}
	state.Goal.Integration = &models.IntegrationLifecycle{
		PrematureRecovery: &models.IntegrationPrematureRecovery{AnalysisTaskID: retired.ID},
	}
	state.Config.MaxGlobalIntegrationGenerations = 1
	decision, err := EvaluateLiveIntegrationProgress(state, fixture.projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if decision.PlanningSettled || decision.FreezeContributingSet || decision.GlobalRequest != nil {
		t.Fatalf("recovery must still wait for upstream output: %+v", decision)
	}
	state.Tasks[0].TransitionsExecuted = map[string]bool{"epic-to-us": true}
	decision, err = EvaluateLiveIntegrationProgress(state, fixture.projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if decision.GlobalRequest == nil || decision.GlobalRequest.Generation != 2 || decision.GlobalRequest.Key != "global:2" || decision.Exhausted {
		t.Fatalf("first accepted analysis should reserve global:2 without spending the generation budget: %+v", decision)
	}
}
