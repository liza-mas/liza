package ops

import (
	"reflect"
	"slices"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

func TestEvaluateIntegrationProgress(t *testing.T) {
	available := pipeline.SlicedIntegrationCapability{Available: true}

	t.Run("partial handoff cannot settle coverage and the cohort freezes once", func(t *testing.T) {
		state := integrationProgressState(
			progressPlan("plan-a", models.TaskStatusCodePlanning, false),
			progressCoding("coding-a", "plan-a"),
			progressPlan("plan-b", models.TaskStatusMerged, true),
			progressCoding("coding-b", "plan-b"),
		)

		decision := evaluateProgress(t, state, available, "head-1")
		if decision.PlanningSettled || decision.FreezeContributingSet || decision.ContributingSet != nil {
			t.Fatalf("partial handoff decision = %#v, want unsettled with no cohort", decision)
		}

		state.Tasks[0].Status = models.TaskStatusMerged
		decision = evaluateProgress(t, state, available, "head-1")
		if decision.PlanningSettled {
			t.Fatal("merged plan with unconsumed output settled coverage")
		}

		state.Tasks[0].TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
		decision = evaluateProgress(t, state, available, "head-1")
		wantCohort := &models.IntegrationContributingSet{Scopes: []models.IntegrationScopeSnapshot{
			{PlanTaskID: "plan-a", RootTaskIDs: []string{"coding-a"}},
			{PlanTaskID: "plan-b", RootTaskIDs: []string{"coding-b"}},
		}}
		if !decision.PlanningSettled || !decision.FreezeContributingSet || !reflect.DeepEqual(decision.ContributingSet, wantCohort) {
			t.Fatalf("settled decision cohort = %#v, want %#v", decision, wantCohort)
		}

		state.Goal.Integration = &models.IntegrationLifecycle{ContributingSet: cloneContributingSet(wantCohort)}
		state.Tasks = append(state.Tasks,
			progressAnalysis("global-analysis", "global:1", models.IntegrationAnalysisPhaseGlobal, 1, "head-1"),
			progressChild(progressPlan("escalated-plan", models.TaskStatusMerged, true), "global-analysis"),
			progressCoding("escalated-coding", "escalated-plan"),
		)
		decision = evaluateProgress(t, state, available, "head-1")
		if decision.FreezeContributingSet || !reflect.DeepEqual(decision.ContributingSet, wantCohort) {
			t.Fatalf("persisted cohort changed after escalation: %#v", decision.ContributingSet)
		}
	})

	t.Run("settled zero-scope cohort is persistable and freezes once", func(t *testing.T) {
		state := integrationProgressState()

		decision := evaluateProgress(t, state, available, "head-1")
		if !decision.PlanningSettled || !decision.FreezeContributingSet || decision.ContributingSet == nil {
			t.Fatalf("zero-scope decision = %#v, want settled frozen cohort", decision)
		}
		if decision.ContributingSet.Scopes == nil || len(decision.ContributingSet.Scopes) != 0 {
			t.Fatalf("zero-scope cohort = %#v, want non-nil empty scopes", decision.ContributingSet)
		}
		if len(decision.Coverage) != 0 || len(decision.SliceRequests) != 0 {
			t.Fatalf("zero-scope local work = coverage %#v, slices %#v", decision.Coverage, decision.SliceRequests)
		}

		state.Goal.Integration.ContributingSet = cloneContributingSet(decision.ContributingSet)
		decision = evaluateProgress(t, state, available, "head-1")
		if decision.FreezeContributingSet || decision.ContributingSet == nil || decision.ContributingSet.Scopes == nil {
			t.Fatalf("persisted zero-scope decision = %#v, want reused non-nil empty cohort", decision)
		}
	})

	t.Run("fewer than two contributing scopes create no slices", func(t *testing.T) {
		state := integrationProgressState(
			progressPlan("plan-a", models.TaskStatusMerged, true),
			progressCoding("coding-a", "plan-a"),
		)
		decision := evaluateProgress(t, state, pipeline.SlicedIntegrationCapability{
			Code: "pipeline_upgrade_required", Guidance: "upgrade",
		}, "head-1")
		if len(decision.SliceRequests) != 0 || decision.Blocked != nil {
			t.Fatalf("single-scope decision = %#v, want slice bypass", decision)
		}
	})

	t.Run("one lineage attests and multiple lineages request one deterministic slice", func(t *testing.T) {
		state := integrationProgressState(
			progressPlan("plan-single", models.TaskStatusMerged, true),
			progressCoding("coding-single", "plan-single"),
			progressPlan("plan-multi", models.TaskStatusMerged, true),
			progressCoding("coding-z", "plan-multi"),
			progressCoding("coding-a", "plan-multi"),
		)
		decision := evaluateProgress(t, state, available, "head-1")
		if len(decision.Coverage) != 2 {
			t.Fatalf("coverage count = %d, want 2", len(decision.Coverage))
		}
		if got := decision.Coverage[0]; got.PlanTaskID != "plan-multi" || got.Kind != models.IntegrationCoverageSliceReport || got.AnalysisKey != "slice:plan-multi" || !reflect.DeepEqual(got.RootTaskIDs, []string{"coding-a", "coding-z"}) {
			t.Fatalf("multi-lineage coverage = %#v", got)
		}
		if got := decision.Coverage[1]; got.PlanTaskID != "plan-single" || got.Kind != models.IntegrationCoverageApprovalAttestation || len(got.ApprovalAttestations) != 1 || got.ApprovalAttestations[0].ReviewedTaskID != "coding-single" || !got.Effective || !got.Resolved {
			t.Fatalf("single-lineage coverage = %#v", got)
		}
		if len(decision.SliceRequests) != 1 || decision.SliceRequests[0].Key != "slice:plan-multi" {
			t.Fatalf("slice requests = %#v, want one deterministic request", decision.SliceRequests)
		}

		unavailable := pipeline.SlicedIntegrationCapability{Code: "pipeline_upgrade_required", Guidance: "upgrade pipeline"}
		blocked := evaluateProgress(t, state, unavailable, "head-1")
		if blocked.Blocked == nil || blocked.Blocked.Code != "pipeline_upgrade_required" || blocked.Blocked.Guidance != "upgrade pipeline" {
			t.Fatalf("missing-capability decision = %#v", blocked)
		}
	})

	t.Run("branched replacement leaves remain one lineage with exact deterministic attestations", func(t *testing.T) {
		state := integrationProgressState(
			progressPlan("plan-branched", models.TaskStatusMerged, true),
			progressCoding("coding-root", "plan-branched"),
			progressPlan("plan-other", models.TaskStatusMerged, true),
			progressCoding("coding-other", "plan-other"),
		)
		root := state.FindTask("coding-root")
		root.Status = models.TaskStatusSuperseded
		root.SupersededBy = []string{"coding-leaf-z", "coding-leaf-a"}
		state.Tasks = append(state.Tasks,
			progressCoding("coding-leaf-z", "plan-branched"),
			progressCoding("coding-leaf-a", "plan-branched"),
		)

		decision := evaluateProgress(t, state, available, "head-1")
		wantCohort := &models.IntegrationContributingSet{Scopes: []models.IntegrationScopeSnapshot{
			{PlanTaskID: "plan-branched", RootTaskIDs: []string{"coding-root"}},
			{PlanTaskID: "plan-other", RootTaskIDs: []string{"coding-other"}},
		}}
		if !reflect.DeepEqual(decision.ContributingSet, wantCohort) {
			t.Fatalf("branched cohort = %#v, want %#v", decision.ContributingSet, wantCohort)
		}
		branched := progressCoverageByPlan(t, decision.Coverage, "plan-branched")
		if branched.Kind != models.IntegrationCoverageApprovalAttestation || !branched.Effective || !branched.Resolved {
			t.Fatalf("branched coverage = %#v, want effective resolved approval coverage", branched)
		}
		if got := progressReviewedTaskIDs(branched.ApprovalAttestations); !reflect.DeepEqual(got, []string{"coding-leaf-a", "coding-leaf-z"}) {
			t.Fatalf("branched reviewed tasks = %v, want every sorted merged leaf", got)
		}
		if len(decision.SliceRequests) != 0 {
			t.Fatalf("one-root branched scope requested slices: %#v", decision.SliceRequests)
		}
		missingFacts := cloneProgressState(t, state)
		missingFacts.FindTask("coding-leaf-z").ApprovedBy = nil
		if _, err := EvaluateIntegrationProgress(missingFacts, available, "head-1"); err == nil {
			t.Fatal("branched merged leaf without approval facts did not fail closed")
		}

		state.Goal.Integration.ContributingSet = cloneContributingSet(decision.ContributingSet)
		state.Goal.Integration.Coverage = make([]models.IntegrationCoverageRecord, 0, len(decision.Coverage))
		for _, coverage := range decision.Coverage {
			state.Goal.Integration.Coverage = append(state.Goal.Integration.Coverage, models.IntegrationCoverageRecord{
				PlanTaskID:           coverage.PlanTaskID,
				Kind:                 coverage.Kind,
				ApprovalAttestations: append([]models.IntegrationApprovalAttestation(nil), coverage.ApprovalAttestations...),
			})
		}
		slices.Reverse(state.Goal.Integration.Coverage[0].ApprovalAttestations)
		persisted := evaluateProgress(t, state, available, "head-1")
		if got := progressReviewedTaskIDs(progressCoverageByPlan(t, persisted.Coverage, "plan-branched").ApprovalAttestations); !reflect.DeepEqual(got, []string{"coding-leaf-a", "coding-leaf-z"}) {
			t.Fatalf("persisted branched reviewed tasks = %v", got)
		}

		tests := []struct {
			name   string
			mutate func(*models.State)
		}{
			{
				name: "missing reviewed leaf",
				mutate: func(candidate *models.State) {
					candidate.Goal.Integration.Coverage[0].ApprovalAttestations =
						candidate.Goal.Integration.Coverage[0].ApprovalAttestations[:1]
				},
			},
			{
				name: "extra reviewed task",
				mutate: func(candidate *models.State) {
					candidate.Goal.Integration.Coverage[0].ApprovalAttestations = append(
						candidate.Goal.Integration.Coverage[0].ApprovalAttestations,
						models.IntegrationApprovalAttestation{ReviewedTaskID: "ghost"},
					)
				},
			},
			{
				name: "duplicate reviewed task",
				mutate: func(candidate *models.State) {
					candidate.Goal.Integration.Coverage[0].ApprovalAttestations[1] =
						candidate.Goal.Integration.Coverage[0].ApprovalAttestations[0]
				},
			},
			{
				name: "non-merged reviewed task",
				mutate: func(candidate *models.State) {
					candidate.Goal.Integration.Coverage[0].ApprovalAttestations[1].ReviewedTaskID = "coding-pending"
					candidate.FindTask("coding-root").SupersededBy = append(
						candidate.FindTask("coding-root").SupersededBy,
						"coding-pending",
					)
					candidate.Tasks = append(candidate.Tasks, progressReplacement("coding-pending", "coding-root", models.TaskStatusImplementing))
				},
			},
			{
				name: "out-of-lineage reviewed task",
				mutate: func(candidate *models.State) {
					candidate.Goal.Integration.Coverage[0].ApprovalAttestations[1].ReviewedTaskID = "coding-other"
				},
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				candidate := cloneProgressState(t, state)
				test.mutate(candidate)
				if _, err := EvaluateIntegrationProgress(candidate, available, "head-1"); err == nil {
					t.Fatal("inexact persisted approval membership did not fail closed")
				}
			})
		}
	})

	t.Run("integration escalation remains repair lineage", func(t *testing.T) {
		state := integrationProgressState(
			progressPlan("plan-a", models.TaskStatusMerged, true),
			progressCoding("coding-a", "plan-a"),
			progressPlan("plan-b", models.TaskStatusMerged, true),
			progressCoding("coding-b", "plan-b"),
			progressAnalysis("global-analysis", "global:1", models.IntegrationAnalysisPhaseGlobal, 1, "head-0"),
			progressChild(progressPlan("escalated-plan", models.TaskStatusMerged, true), "global-analysis"),
			progressCoding("escalated-coding", "escalated-plan"),
		)
		decision := evaluateProgress(t, state, available, "head-1")
		if got := scopePlanIDs(decision.ContributingSet); !reflect.DeepEqual(got, []string{"plan-a", "plan-b"}) {
			t.Fatalf("contributing plans = %v, want escalation excluded", got)
		}
		if slices.ContainsFunc(decision.SliceRequests, func(request IntegrationAnalysisRequest) bool {
			return request.OriginatingPlanTaskID == "escalated-plan"
		}) {
			t.Fatalf("escalation created a slice request: %#v", decision.SliceRequests)
		}
	})

	t.Run("replacement lineages resolve recursively and fail closed", func(t *testing.T) {
		base := readyGlobalProgressState("head-1")
		base.Goal.Integration.GlobalGenerations = []models.IntegrationGlobalGeneration{{
			Generation: 1, AnalysisTaskID: "global-1", AnalysisKey: "global:1",
			Verdict: models.IntegrationAnalysisVerdictFindings, SourceCommit: "head-1", ReportCommit: "report-1",
		}}
		base.Tasks = append(base.Tasks,
			progressAnalysis("global-1", "global:1", models.IntegrationAnalysisPhaseGlobal, 1, "head-1"),
			progressChild(progressTask("repair", "coding-pair", models.TaskStatusSuperseded), "global-1"),
			progressReplacement("repair-a", "repair", models.TaskStatusSuperseded),
			progressReplacement("repair-a2", "repair-a", models.TaskStatusMerged),
			progressReplacement("repair-b", "repair", models.TaskStatusMerged),
		)
		base.FindTask("repair").SupersededBy = []string{"repair-a", "repair-b"}
		base.FindTask("repair-a").SupersededBy = []string{"repair-a2"}
		decision := evaluateProgress(t, base, available, "head-2")
		if decision.GlobalRequest == nil || decision.GlobalRequest.Key != "global:2" {
			t.Fatalf("resolved replacement decision = %#v, want global:2", decision)
		}

		pending := cloneProgressState(t, base)
		pending.FindTask("repair-b").Status = models.TaskStatusImplementing
		decision = evaluateProgress(t, pending, available, "head-2")
		if decision.Waiting == nil || decision.Waiting.Code != "integration_repairs_pending" || decision.GlobalRequest != nil {
			t.Fatalf("pending replacement decision = %#v", decision)
		}

		missing := cloneProgressState(t, base)
		missing.FindTask("repair-a").SupersededBy = []string{"missing-replacement"}
		if _, err := EvaluateIntegrationProgress(missing, available, "head-2"); err == nil {
			t.Fatal("missing replacement did not fail closed")
		}

		cyclic := cloneProgressState(t, base)
		cyclic.FindTask("repair-a2").Status = models.TaskStatusSuperseded
		cyclic.FindTask("repair-a2").SupersededBy = []string{"repair-a"}
		if _, err := EvaluateIntegrationProgress(cyclic, available, "head-2"); err == nil {
			t.Fatal("replacement cycle did not fail closed")
		}
	})

	t.Run("blocked or abandoned finding repairs block", func(t *testing.T) {
		for _, status := range []models.TaskStatus{models.TaskStatusBlocked, models.TaskStatusAbandoned} {
			t.Run(string(status), func(t *testing.T) {
				state := readyGlobalProgressState("head-1")
				state.Goal.Integration.GlobalGenerations = []models.IntegrationGlobalGeneration{{
					Generation: 1, AnalysisTaskID: "global-1", AnalysisKey: "global:1",
					Verdict: models.IntegrationAnalysisVerdictFindings, SourceCommit: "head-1", ReportCommit: "report-1",
				}}
				state.Tasks = append(state.Tasks,
					progressAnalysis("global-1", "global:1", models.IntegrationAnalysisPhaseGlobal, 1, "head-1"),
					progressChild(progressTask("repair", "coding-pair", status), "global-1"),
				)
				decision := evaluateProgress(t, state, available, "head-2")
				if decision.Blocked == nil || decision.Blocked.Code != "integration_repair_blocked" || !reflect.DeepEqual(decision.Blocked.TaskIDs, []string{"repair"}) {
					t.Fatalf("%s repair decision = %#v", status, decision)
				}
			})
		}
	})

	t.Run("global readiness waits for every local and repair barrier", func(t *testing.T) {
		state := multiScopeProgressState()
		decision := evaluateProgress(t, state, available, "head-1")
		if decision.GlobalReady || decision.GlobalRequest != nil || decision.Waiting == nil || decision.Waiting.Code != "slice_coverage_pending" {
			t.Fatalf("missing slice decision = %#v", decision)
		}

		addSliceReport(state, "plan-multi", []string{"coding-a", "coding-b"}, models.IntegrationAnalysisVerdictFindings, "head-0")
		state.Tasks = append(state.Tasks, progressChild(progressTask("slice-repair", "coding-pair", models.TaskStatusImplementing), "slice-plan-multi"))
		decision = evaluateProgress(t, state, available, "head-1")
		if decision.GlobalReady || decision.Waiting == nil || decision.Waiting.Code != "integration_repairs_pending" {
			t.Fatalf("active slice repair decision = %#v", decision)
		}

		state.FindTask("slice-repair").Status = models.TaskStatusMerged
		state.Goal.Integration.ContributingSet = &models.IntegrationContributingSet{Scopes: []models.IntegrationScopeSnapshot{
			{PlanTaskID: "plan-multi", RootTaskIDs: []string{"coding-a", "coding-b"}},
			{PlanTaskID: "plan-single", RootTaskIDs: []string{"coding-single"}},
		}}
		state.Tasks = append(state.Tasks, progressChild(progressTask("unrelated-active-coding", "coding-pair", models.TaskStatusImplementing), "plan-single"))
		decision = evaluateProgress(t, state, available, "head-1")
		if decision.GlobalReady || decision.Waiting == nil || decision.Waiting.Code != "coding_work_pending" {
			t.Fatalf("active coding decision = %#v", decision)
		}

		state.FindTask("unrelated-active-coding").Status = models.TaskStatusMerged
		decision = evaluateProgress(t, state, available, "head-1")
		if !decision.GlobalReady || decision.GlobalRequest == nil || decision.GlobalRequest.Key != "global:1" || decision.GlobalRequest.SourceCommit != "head-1" {
			t.Fatalf("ready decision = %#v", decision)
		}
	})

	t.Run("current clean evidence completes and stale evidence requests a new generation", func(t *testing.T) {
		state := readyGlobalProgressState("head-1")
		addGlobalClean(state, 1, "head-1")
		state.Goal.Integration.Closure = &models.IntegrationClosure{
			Status: models.IntegrationClosureStatusClean, Generation: 1, AnalysisKey: "global:1", SourceCommit: "head-1",
		}

		decision := evaluateProgress(t, state, available, "head-1")
		if !decision.IntegrationComplete || decision.GlobalRequest != nil {
			t.Fatalf("current clean decision = %#v", decision)
		}

		decision = evaluateProgress(t, state, available, "head-2")
		if decision.IntegrationComplete || decision.GlobalRequest == nil || decision.GlobalRequest.Key != "global:2" || decision.GlobalRequest.SourceCommit != "head-2" {
			t.Fatalf("stale clean decision = %#v", decision)
		}
	})

	t.Run("generation limits normalize and exhaustion blocks", func(t *testing.T) {
		state := readyGlobalProgressState("head-0")
		state.Config.MaxGlobalIntegrationGenerations = 0
		addGlobalClean(state, 1, "head-1")
		addGlobalClean(state, 2, "head-2")
		decision := evaluateProgress(t, state, available, "head-3")
		if decision.GlobalRequest == nil || decision.GlobalRequest.Generation != 3 {
			t.Fatalf("normalized default decision = %#v, want generation 3", decision)
		}

		state.Config.MaxGlobalIntegrationGenerations = 2
		decision = evaluateProgress(t, state, available, "head-3")
		if decision.Blocked == nil || decision.Blocked.Code != "global_generations_exhausted" || !decision.Exhausted || decision.GlobalRequest != nil {
			t.Fatalf("exhausted decision = %#v", decision)
		}
	})

	t.Run("analysis identities are deterministic and evaluation is pure", func(t *testing.T) {
		state := multiScopeProgressState()
		before := cloneProgressState(t, state)
		first := evaluateProgress(t, state, available, "head-1")
		if !reflect.DeepEqual(state, before) {
			t.Fatal("EvaluateIntegrationProgress mutated its input state")
		}

		permuted := cloneProgressState(t, state)
		slices.Reverse(permuted.Tasks)
		second := evaluateProgress(t, permuted, available, "head-1")
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("task order changed decision:\nfirst:  %#v\nsecond: %#v", first, second)
		}
	})
}

func integrationProgressState(tasks ...models.Task) *models.State {
	return &models.State{
		Goal:   models.Goal{Integration: &models.IntegrationLifecycle{}},
		Tasks:  tasks,
		Config: models.Config{MaxGlobalIntegrationGenerations: 3},
	}
}

func multiScopeProgressState() *models.State {
	return integrationProgressState(
		progressPlan("plan-single", models.TaskStatusMerged, true),
		progressCoding("coding-single", "plan-single"),
		progressPlan("plan-multi", models.TaskStatusMerged, true),
		progressCoding("coding-b", "plan-multi"),
		progressCoding("coding-a", "plan-multi"),
	)
}

func readyGlobalProgressState(head string) *models.State {
	state := multiScopeProgressState()
	addSliceReport(state, "plan-multi", []string{"coding-a", "coding-b"}, models.IntegrationAnalysisVerdictClean, head)
	return state
}

func progressPlan(id string, status models.TaskStatus, consumed bool) models.Task {
	task := progressTask(id, "code-planning-pair", status)
	task.Output = []models.OutputEntry{{Desc: "coding work", DoneWhen: "done", Scope: "internal/", SpecRef: "spec.md"}}
	if consumed {
		task.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
	}
	return task
}

func progressCoding(id, planID string) models.Task {
	task := progressChild(progressTask(id, "coding-pair", models.TaskStatusMerged), planID)
	task.DoneWhen = "acceptance for " + id
	task.Validation = []string{"go test ./..."}
	task.ReviewCommit = progressString("review-" + id)
	task.ApprovedBy = progressString("reviewer-" + id)
	task.MergeCommit = progressString("merge-" + id)
	return task
}

func progressTask(id, rolePair string, status models.TaskStatus) models.Task {
	return models.Task{ID: id, RolePair: rolePair, Status: status}
}

func progressChild(task models.Task, parentID string) models.Task {
	task.ParentTask = progressString(parentID)
	return task
}

func progressReplacement(id, supersedes string, status models.TaskStatus) models.Task {
	task := progressTask(id, "coding-pair", status)
	task.Supersedes = progressString(supersedes)
	if status == models.TaskStatusMerged {
		task.DoneWhen = "acceptance for " + id
		task.Validation = []string{"go test ./..."}
		task.ReviewCommit = progressString("review-" + id)
		task.ApprovedBy = progressString("reviewer-" + id)
		task.MergeCommit = progressString("merge-" + id)
	}
	return task
}

func progressAnalysis(id, key string, phase models.IntegrationAnalysisPhase, generation int, source string) models.Task {
	return models.Task{
		ID: id,
		IntegrationAnalysis: &models.IntegrationAnalysisMetadata{
			Key: key, Phase: phase, Generation: generation, SourceCommit: source,
		},
	}
}

func addSliceReport(state *models.State, planID string, roots []string, verdict models.IntegrationAnalysisVerdict, source string) {
	key := "slice:" + planID
	id := "slice-" + planID
	analysis := progressAnalysis(id, key, models.IntegrationAnalysisPhaseSlice, 0, source)
	analysis.IntegrationAnalysis.OriginatingPlanTaskID = planID
	analysis.IntegrationAnalysis.RootTaskIDs = append([]string(nil), roots...)
	state.Tasks = append(state.Tasks, analysis)
	state.Goal.Integration.Coverage = append(state.Goal.Integration.Coverage, models.IntegrationCoverageRecord{
		PlanTaskID: planID,
		Kind:       models.IntegrationCoverageSliceReport,
		SliceReport: &models.IntegrationSliceReport{
			AnalysisTaskID: id, AnalysisKey: key, Verdict: verdict, SourceCommit: source, ReportCommit: "report-" + id,
		},
	})
}

func addGlobalClean(state *models.State, generation int, source string) {
	key := "global:" + string(rune('0'+generation))
	id := "global-" + string(rune('0'+generation))
	state.Tasks = append(state.Tasks, progressAnalysis(id, key, models.IntegrationAnalysisPhaseGlobal, generation, source))
	state.Goal.Integration.GlobalGenerations = append(state.Goal.Integration.GlobalGenerations, models.IntegrationGlobalGeneration{
		Generation: generation, AnalysisTaskID: id, AnalysisKey: key,
		Verdict: models.IntegrationAnalysisVerdictClean, SourceCommit: source, ReportCommit: "report-" + id,
	})
}

func evaluateProgress(t *testing.T, state *models.State, capability pipeline.SlicedIntegrationCapability, head string) IntegrationProgressDecision {
	t.Helper()
	decision, err := EvaluateIntegrationProgress(state, capability, head)
	if err != nil {
		t.Fatalf("EvaluateIntegrationProgress() error = %v", err)
	}
	return decision
}

func cloneProgressState(t *testing.T, state *models.State) *models.State {
	t.Helper()
	clone := *state
	clone.Tasks = append([]models.Task(nil), state.Tasks...)
	for i := range clone.Tasks {
		clone.Tasks[i].ParentTasks = append([]string(nil), state.Tasks[i].ParentTasks...)
		clone.Tasks[i].SupersededBy = append([]string(nil), state.Tasks[i].SupersededBy...)
		clone.Tasks[i].TransitionsExecuted = cloneProgressBoolMap(state.Tasks[i].TransitionsExecuted)
	}
	if state.Goal.Integration != nil {
		lifecycle := *state.Goal.Integration
		lifecycle.ContributingSet = cloneContributingSet(state.Goal.Integration.ContributingSet)
		if state.Goal.Integration.Coverage != nil {
			lifecycle.Coverage = make([]models.IntegrationCoverageRecord, len(state.Goal.Integration.Coverage))
			for i := range state.Goal.Integration.Coverage {
				lifecycle.Coverage[i] = state.Goal.Integration.Coverage[i]
				lifecycle.Coverage[i].ApprovalAttestations = append(
					[]models.IntegrationApprovalAttestation(nil),
					state.Goal.Integration.Coverage[i].ApprovalAttestations...,
				)
				for j := range lifecycle.Coverage[i].ApprovalAttestations {
					lifecycle.Coverage[i].ApprovalAttestations[j].Validation = append(
						[]string(nil),
						state.Goal.Integration.Coverage[i].ApprovalAttestations[j].Validation...,
					)
				}
			}
		}
		lifecycle.GlobalGenerations = append([]models.IntegrationGlobalGeneration(nil), state.Goal.Integration.GlobalGenerations...)
		if state.Goal.Integration.Closure != nil {
			closure := *state.Goal.Integration.Closure
			lifecycle.Closure = &closure
		}
		clone.Goal.Integration = &lifecycle
	}
	return &clone
}

func cloneContributingSet(set *models.IntegrationContributingSet) *models.IntegrationContributingSet {
	if set == nil {
		return nil
	}
	clone := &models.IntegrationContributingSet{Scopes: append([]models.IntegrationScopeSnapshot(nil), set.Scopes...)}
	for i := range clone.Scopes {
		clone.Scopes[i].RootTaskIDs = append([]string(nil), set.Scopes[i].RootTaskIDs...)
	}
	return clone
}

func cloneProgressBoolMap(values map[string]bool) map[string]bool {
	if values == nil {
		return nil
	}
	clone := make(map[string]bool, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func progressCoverageByPlan(t *testing.T, coverage []IntegrationScopeCoverage, planID string) IntegrationScopeCoverage {
	t.Helper()
	for _, record := range coverage {
		if record.PlanTaskID == planID {
			return record
		}
	}
	t.Fatalf("coverage for plan %q not found in %#v", planID, coverage)
	return IntegrationScopeCoverage{}
}

func progressReviewedTaskIDs(attestations []models.IntegrationApprovalAttestation) []string {
	ids := make([]string, 0, len(attestations))
	for _, attestation := range attestations {
		ids = append(ids, attestation.ReviewedTaskID)
	}
	return ids
}

func scopePlanIDs(set *models.IntegrationContributingSet) []string {
	if set == nil {
		return nil
	}
	ids := make([]string, 0, len(set.Scopes))
	for _, scope := range set.Scopes {
		ids = append(ids, scope.PlanTaskID)
	}
	return ids
}

func progressString(value string) *string {
	return &value
}
