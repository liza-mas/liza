package ops

import (
	"reflect"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

func TestIntegrationProgressCoverageRefusals(t *testing.T) {
	t.Parallel()

	available := pipeline.SlicedIntegrationCapability{Available: true}
	for _, tc := range []struct {
		name   string
		mutate func(*models.State)
		want   string
	}{
		{
			name: "two records for one plan",
			mutate: func(s *models.State) {
				record := models.IntegrationCoverageRecord{PlanTaskID: "plan-single", Kind: models.IntegrationCoverageApprovalAttestation}
				s.Goal.Integration.Coverage = []models.IntegrationCoverageRecord{record, record}
			},
			want: `duplicate coverage for plan "plan-single"`,
		},
		{
			name: "record for a plan outside the cohort",
			mutate: func(s *models.State) {
				s.Goal.Integration.Coverage = []models.IntegrationCoverageRecord{{PlanTaskID: "ghost-plan"}}
			},
			want: `coverage references non-contributing plan "ghost-plan"`,
		},
		{
			name: "multi-root plan holding an attestation record",
			mutate: func(s *models.State) {
				s.Goal.Integration.Coverage = []models.IntegrationCoverageRecord{{PlanTaskID: "plan-multi", Kind: models.IntegrationCoverageApprovalAttestation}}
			},
			want: `plan "plan-multi" requires slice coverage`,
		},
		{
			name: "slice report with an unknown verdict",
			mutate: func(s *models.State) {
				addSliceReport(s, "plan-multi", []string{"coding-a", "coding-b"}, models.IntegrationAnalysisVerdict("unclear"), "head-1")
			},
			want: `slice "slice:plan-multi" has invalid verdict "unclear"`,
		},
		{
			name: "slice report whose analysis names another source",
			mutate: func(s *models.State) {
				addSliceReport(s, "plan-multi", []string{"coding-a", "coding-b"}, models.IntegrationAnalysisVerdictClean, "head-1")
				s.Tasks[len(s.Tasks)-1].IntegrationAnalysis.SourceCommit = "head-2"
			},
			want: `slice report "slice:plan-multi" contradicts analysis metadata`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state := multiScopeProgressState()
			tc.mutate(state)
			_, err := EvaluateIntegrationProgress(state, available, "head-1")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("EvaluateIntegrationProgress() error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestIntegrationProgressSliceCoverageWaits(t *testing.T) {
	t.Parallel()

	available := pipeline.SlicedIntegrationCapability{Available: true}
	pendingAnalysis := func(status models.TaskStatus) *models.State {
		state := multiScopeProgressState()
		analysis := progressAnalysis("slice-plan-multi", "slice:plan-multi", models.IntegrationAnalysisPhaseSlice, 0, "head-1")
		analysis.Status = status
		state.Tasks = append(state.Tasks, analysis)
		return state
	}

	t.Run("an analysis in flight is waited on without a new request", func(t *testing.T) {
		t.Parallel()
		decision := evaluateProgress(t, pendingAnalysis(models.TaskStatusCodePlanning), available, "head-1")
		if decision.Waiting == nil || decision.Waiting.Code != string(integrationProgressWaitingSliceCoverage) || len(decision.Waiting.TaskIDs) != 0 {
			t.Fatalf("waiting = %#v, want %q with no task IDs", decision.Waiting, integrationProgressWaitingSliceCoverage)
		}
		if decision.Blocked != nil || len(decision.SliceRequests) != 0 || decision.GlobalReady || decision.GlobalRequest != nil {
			t.Fatalf("decision = %#v, want a bare coverage wait", decision)
		}
		single := progressCoverageByPlan(t, decision.Coverage, "plan-single")
		multi := progressCoverageByPlan(t, decision.Coverage, "plan-multi")
		if !single.Resolved || multi.Resolved || multi.Effective {
			t.Fatalf("coverage = single %#v multi %#v, want only the attested scope resolved", single, multi)
		}
	})

	for _, status := range []models.TaskStatus{models.TaskStatusBlocked, models.TaskStatusAbandoned} {
		t.Run("a "+string(status)+" analysis blocks coverage", func(t *testing.T) {
			t.Parallel()
			decision := evaluateProgress(t, pendingAnalysis(status), available, "head-1")
			if decision.Blocked == nil || decision.Blocked.Code != string(integrationProgressBlockedAnalysis) ||
				!reflect.DeepEqual(decision.Blocked.TaskIDs, []string{"slice-plan-multi"}) || decision.Waiting != nil {
				t.Fatalf("decision = %#v, want %q naming the analysis", decision, integrationProgressBlockedAnalysis)
			}
		})
	}

	t.Run("findings wait on their repairs and resolve when the repairs merge", func(t *testing.T) {
		t.Parallel()
		state := multiScopeProgressState()
		addSliceReport(state, "plan-multi", []string{"coding-a", "coding-b"}, models.IntegrationAnalysisVerdictFindings, "head-1")
		repair := progressChild(progressTask("repair", "coding-pair", models.TaskStatusCodePlanning), "slice-plan-multi")
		state.Tasks = append(state.Tasks, repair)

		decision := evaluateProgress(t, state, available, "head-1")
		if decision.Waiting == nil || decision.Waiting.Code != string(integrationProgressWaitingRepairs) || !reflect.DeepEqual(decision.Waiting.TaskIDs, []string{"repair"}) {
			t.Fatalf("waiting = %#v, want %q naming the repair", decision.Waiting, integrationProgressWaitingRepairs)
		}
		if got := progressCoverageByPlan(t, decision.Coverage, "plan-multi"); !got.Effective || got.Resolved {
			t.Fatalf("coverage = %#v, want effective but unresolved", got)
		}

		state.Tasks[len(state.Tasks)-1] = progressChild(progressCoding("repair", "slice-plan-multi"), "slice-plan-multi")
		decision = evaluateProgress(t, state, available, "head-1")
		if got := progressCoverageByPlan(t, decision.Coverage, "plan-multi"); !got.Resolved || decision.Waiting != nil && decision.Waiting.Code == string(integrationProgressWaitingRepairs) {
			t.Fatalf("coverage = %#v waiting = %#v, want resolved after the repair merged", got, decision.Waiting)
		}
	})
}
