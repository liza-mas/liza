package statevalidate

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

func TestIntegrationLifecycleValidation(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupPipelineConfig(t, projectRoot)

	t.Run("valid lifecycle composes through ValidateState", func(t *testing.T) {
		state := validIntegrationState(t)
		if err := ValidateState(state, projectRoot, true, nil); err != nil {
			t.Fatalf("ValidateState() error = %v", err)
		}
	})

	structuralCases := []struct {
		name    string
		mutate  func(*models.State)
		wantErr string
	}{
		{
			name: "duplicate analysis key",
			mutate: func(state *models.State) {
				state.Tasks[2].IntegrationAnalysis.Key = state.Tasks[0].IntegrationAnalysis.Key
			},
			wantErr: "duplicate integration analysis key",
		},
		{
			name: "unknown analysis phase",
			mutate: func(state *models.State) {
				state.Tasks[0].IntegrationAnalysis.Phase = models.IntegrationAnalysisPhase("other")
			},
			wantErr: "invalid integration analysis phase",
		},
		{
			name: "slice metadata has global generation",
			mutate: func(state *models.State) {
				state.Tasks[0].IntegrationAnalysis.Generation = 1
			},
			wantErr: "slice analysis generation",
		},
		{
			name: "global metadata has slice fields",
			mutate: func(state *models.State) {
				state.Tasks[1].IntegrationAnalysis.OriginatingPlanTaskID = "plan-multi"
			},
			wantErr: "global analysis slice fields",
		},
		{
			name: "duplicate descendant task",
			mutate: func(state *models.State) {
				state.Tasks[0].IntegrationAnalysis.DescendantChanges[1].TaskID = "coding-a"
			},
			wantErr: "duplicate descendant task",
		},
		{
			name: "duplicate descendant commit",
			mutate: func(state *models.State) {
				state.Tasks[0].IntegrationAnalysis.DescendantChanges[1].Commit = "coding-a-commit"
			},
			wantErr: "duplicate descendant commit",
		},
		{
			name: "empty descendant commit",
			mutate: func(state *models.State) {
				state.Tasks[0].IntegrationAnalysis.DescendantChanges[0].Commit = ""
			},
			wantErr: "descendant commit is empty",
		},
		{
			name: "duplicate affected path",
			mutate: func(state *models.State) {
				state.Tasks[0].IntegrationAnalysis.AffectedPaths = []string{"a.go", "a.go"}
			},
			wantErr: "duplicate affected path",
		},
		{
			name: "duplicate snapshot path",
			mutate: func(state *models.State) {
				state.Tasks[0].IntegrationAnalysis.SourceSnapshotPaths = []string{"a.go", "a.go"}
			},
			wantErr: "duplicate source snapshot path",
		},
		{
			name: "duplicate contributing plan",
			mutate: func(state *models.State) {
				state.Goal.Integration.ContributingSet.Scopes = append(
					state.Goal.Integration.ContributingSet.Scopes,
					models.IntegrationScopeSnapshot{PlanTaskID: "plan-multi", RootTaskIDs: []string{"coding-c"}},
				)
			},
			wantErr: "duplicate contributing plan",
		},
		{
			name: "duplicate root in scope",
			mutate: func(state *models.State) {
				state.Goal.Integration.ContributingSet.Scopes[1].RootTaskIDs = []string{"coding-a", "coding-a"}
			},
			wantErr: "duplicate root task",
		},
		{
			name: "root shared across scopes",
			mutate: func(state *models.State) {
				state.Goal.Integration.ContributingSet.Scopes[0].RootTaskIDs = []string{"coding-a"}
			},
			wantErr: "belongs to multiple contributing plans",
		},
		{
			name: "coverage references unknown plan",
			mutate: func(state *models.State) {
				state.Goal.Integration.Coverage[0].PlanTaskID = "plan-unknown"
			},
			wantErr: "coverage references unknown contributing plan",
		},
		{
			name: "unknown coverage kind",
			mutate: func(state *models.State) {
				state.Goal.Integration.Coverage[0].Kind = models.IntegrationCoverageKind("other")
			},
			wantErr: "invalid integration coverage kind",
		},
		{
			name: "coverage has both payloads",
			mutate: func(state *models.State) {
				state.Goal.Integration.Coverage[0].SliceReport = state.Goal.Integration.Coverage[1].SliceReport
			},
			wantErr: "exactly one payload",
		},
		{
			name: "coverage has neither payload",
			mutate: func(state *models.State) {
				state.Goal.Integration.Coverage[0].ApprovalAttestation = nil
			},
			wantErr: "exactly one payload",
		},
		{
			name: "approval attestation missing fact",
			mutate: func(state *models.State) {
				state.Goal.Integration.Coverage[0].ApprovalAttestation.Approver = ""
			},
			wantErr: "approval attestation approver is empty",
		},
		{
			name: "slice references missing task",
			mutate: func(state *models.State) {
				state.Goal.Integration.Coverage[1].SliceReport.AnalysisTaskID = "missing"
			},
			wantErr: "slice report references missing analysis task",
		},
		{
			name: "slice plan differs from metadata plan",
			mutate: func(state *models.State) {
				state.Tasks[0].IntegrationAnalysis.OriginatingPlanTaskID = "plan-single"
			},
			wantErr: "slice coverage plan does not match analysis metadata plan",
		},
		{
			name: "slice roots missing frozen root",
			mutate: func(state *models.State) {
				state.Tasks[0].IntegrationAnalysis.RootTaskIDs = []string{"coding-a"}
			},
			wantErr: "slice analysis roots do not match frozen roots",
		},
		{
			name: "slice roots add frozen root",
			mutate: func(state *models.State) {
				state.Tasks[0].IntegrationAnalysis.RootTaskIDs = []string{"coding-a", "coding-b", "coding-c"}
			},
			wantErr: "slice analysis roots do not match frozen roots",
		},
		{
			name: "slice roots cross plan boundary",
			mutate: func(state *models.State) {
				state.Tasks[0].IntegrationAnalysis.RootTaskIDs = []string{"coding-a", "coding-single"}
			},
			wantErr: "slice analysis roots do not match frozen roots",
		},
		{
			name: "slice analysis reused by coverage",
			mutate: func(state *models.State) {
				state.Goal.Integration.ContributingSet.Scopes = append(
					state.Goal.Integration.ContributingSet.Scopes,
					models.IntegrationScopeSnapshot{PlanTaskID: "plan-other", RootTaskIDs: []string{"coding-c"}},
				)
				state.Goal.Integration.Coverage = append(state.Goal.Integration.Coverage, models.IntegrationCoverageRecord{
					PlanTaskID: "plan-other",
					Kind:       models.IntegrationCoverageSliceReport,
					SliceReport: &models.IntegrationSliceReport{
						AnalysisTaskID: "slice-analysis",
						AnalysisKey:    "slice:plan-multi",
						Verdict:        models.IntegrationAnalysisVerdictClean,
						SourceCommit:   "slice-source",
						ReportCommit:   "slice-report",
					},
				})
			},
			wantErr: "slice analysis is reused",
		},
		{
			name: "zero generation",
			mutate: func(state *models.State) {
				state.Goal.Integration.GlobalGenerations[0].Generation = 0
			},
			wantErr: "global generation 0, want 1",
		},
		{
			name: "gapped generation",
			mutate: func(state *models.State) {
				state.Goal.Integration.GlobalGenerations[1].Generation = 3
			},
			wantErr: "global generation 3, want 2",
		},
		{
			name: "duplicated generation",
			mutate: func(state *models.State) {
				state.Goal.Integration.GlobalGenerations[1].Generation = 1
			},
			wantErr: "global generation 1, want 2",
		},
		{
			name: "descending generation",
			mutate: func(state *models.State) {
				state.Goal.Integration.GlobalGenerations[0], state.Goal.Integration.GlobalGenerations[1] =
					state.Goal.Integration.GlobalGenerations[1], state.Goal.Integration.GlobalGenerations[0]
			},
			wantErr: "global generation 2, want 1",
		},
		{
			name: "mutation receipt missing commit",
			mutate: func(state *models.State) {
				state.Goal.Integration.MutationReceipts[0].BeforeCommit = ""
			},
			wantErr: "mutation receipt before commit is empty",
		},
		{
			name: "mutation receipt commits equal",
			mutate: func(state *models.State) {
				state.Goal.Integration.MutationReceipts[0].AfterCommit = state.Goal.Integration.MutationReceipts[0].BeforeCommit
			},
			wantErr: "mutation receipt commits must differ",
		},
		{
			name: "clean slice missing source commit",
			mutate: func(state *models.State) {
				state.Goal.Integration.Coverage[1].SliceReport.SourceCommit = ""
			},
			wantErr: "clean slice report source commit is empty",
		},
		{
			name: "clean global missing source commit",
			mutate: func(state *models.State) {
				state.Goal.Integration.GlobalGenerations[1].SourceCommit = ""
			},
			wantErr: "clean global generation source commit is empty",
		},
		{
			name: "clean closure missing source commit",
			mutate: func(state *models.State) {
				state.Goal.Integration.Closure.SourceCommit = ""
			},
			wantErr: "clean integration closure source commit is empty",
		},
		{
			name: "clean closure points to findings",
			mutate: func(state *models.State) {
				state.Goal.Integration.Closure.Generation = 1
				state.Goal.Integration.Closure.AnalysisKey = "global:1"
				state.Goal.Integration.Closure.SourceCommit = "global-source-1"
			},
			wantErr: "clean integration closure references non-clean generation",
		},
		{
			name: "clean closure key mismatch",
			mutate: func(state *models.State) {
				state.Goal.Integration.Closure.AnalysisKey = "global:other"
			},
			wantErr: "clean integration closure does not match generation",
		},
		{
			name: "blocked closure missing reason",
			mutate: func(state *models.State) {
				state.Goal.Integration.Closure = &models.IntegrationClosure{Status: models.IntegrationClosureStatusBlocked}
			},
			wantErr: "blocked integration closure reason is empty",
		},
		{
			name: "exhausted closure missing reason",
			mutate: func(state *models.State) {
				state.Goal.Integration.Closure = &models.IntegrationClosure{Status: models.IntegrationClosureStatusExhausted}
			},
			wantErr: "exhausted integration closure reason is empty",
		},
	}

	for _, tc := range structuralCases {
		t.Run(tc.name, func(t *testing.T) {
			state := cloneIntegrationState(t, validIntegrationState(t))
			tc.mutate(state)
			err := ValidateState(state, projectRoot, true, nil)
			assertIntegrationErrorContains(t, err, tc.wantErr)
		})
	}

	transitionCases := []struct {
		name    string
		mutate  func(*models.State)
		wantErr string
	}{
		{
			name: "contributing set cleared",
			mutate: func(state *models.State) {
				state.Goal.Integration.ContributingSet = nil
			},
			wantErr: "contributing set cannot be cleared",
		},
		{
			name: "contributing set replaced",
			mutate: func(state *models.State) {
				state.Goal.Integration.ContributingSet.Scopes[0].PlanTaskID = "other-plan"
			},
			wantErr: "contributing set cannot change",
		},
		{
			name: "coverage removed",
			mutate: func(state *models.State) {
				state.Goal.Integration.Coverage = state.Goal.Integration.Coverage[:1]
			},
			wantErr: "coverage records are append-only",
		},
		{
			name: "coverage reordered",
			mutate: func(state *models.State) {
				state.Goal.Integration.Coverage[0], state.Goal.Integration.Coverage[1] = state.Goal.Integration.Coverage[1], state.Goal.Integration.Coverage[0]
			},
			wantErr: "coverage records are append-only",
		},
		{
			name: "generation rewritten",
			mutate: func(state *models.State) {
				state.Goal.Integration.GlobalGenerations[0].ReportCommit = "rewritten"
			},
			wantErr: "global generations are append-only",
		},
		{
			name: "receipt removed",
			mutate: func(state *models.State) {
				state.Goal.Integration.MutationReceipts = state.Goal.Integration.MutationReceipts[:1]
			},
			wantErr: "mutation receipts are append-only",
		},
		{
			name: "analysis metadata cleared",
			mutate: func(state *models.State) {
				state.Tasks[0].IntegrationAnalysis = nil
			},
			wantErr: "integration analysis metadata cannot be cleared",
		},
		{
			name: "analysis metadata changed",
			mutate: func(state *models.State) {
				state.Tasks[0].IntegrationAnalysis.SourceCommit = "rewritten"
			},
			wantErr: "integration analysis metadata cannot change",
		},
	}

	for _, tc := range transitionCases {
		t.Run(tc.name, func(t *testing.T) {
			previous := validIntegrationState(t)
			candidate := cloneIntegrationState(t, previous)
			tc.mutate(candidate)
			err := ValidateIntegrationLifecycleTransition(previous, candidate)
			assertIntegrationErrorContains(t, err, tc.wantErr)
		})
	}

	t.Run("append-only transition and root set reordering are valid", func(t *testing.T) {
		previous := validIntegrationState(t)
		candidate := cloneIntegrationState(t, previous)
		candidate.Goal.Integration.ContributingSet.Scopes[1].RootTaskIDs = []string{"coding-b", "coding-a"}
		candidate.Tasks[0].IntegrationAnalysis.RootTaskIDs = []string{"coding-b", "coding-a"}
		candidate.Goal.Integration.MutationReceipts = append(candidate.Goal.Integration.MutationReceipts,
			models.IntegrationMutationReceipt{TaskID: "fix-3", BeforeCommit: "global-source-2", AfterCommit: "global-source-3"})
		candidate.Goal.Integration.Closure = &models.IntegrationClosure{
			Status: models.IntegrationClosureStatusBlocked,
			Reason: "new mutation requires another generation",
		}

		if err := ValidateIntegrationLifecycleTransition(previous, candidate); err != nil {
			t.Fatalf("ValidateIntegrationLifecycleTransition() error = %v", err)
		}
	})
}

func validIntegrationState(t *testing.T) *models.State {
	t.Helper()
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	sliceTask := testhelpers.BuildTaskByStatus("slice-analysis", models.TaskStatusMerged, now)
	sliceTask.ReviewCommit = testhelpers.StringPtr("slice-report")
	sliceTask.IntegrationAnalysis = &models.IntegrationAnalysisMetadata{
		Key:                   "slice:plan-multi",
		Phase:                 models.IntegrationAnalysisPhaseSlice,
		OriginatingPlanTaskID: "plan-multi",
		RootTaskIDs:           []string{"coding-a", "coding-b"},
		DescendantChanges: []models.IntegrationDescendantChange{
			{TaskID: "coding-a", Commit: "coding-a-commit"},
			{TaskID: "coding-b", Commit: "coding-b-commit"},
		},
		SourceCommit:        "slice-source",
		AffectedPaths:       []string{"a.go", "b.go"},
		SourceSnapshotPaths: []string{"a.go", "b.go"},
	}

	globalTask1 := testhelpers.BuildTaskByStatus("global-analysis-1", models.TaskStatusMerged, now)
	globalTask1.ReviewCommit = testhelpers.StringPtr("global-report-1")
	globalTask1.IntegrationAnalysis = &models.IntegrationAnalysisMetadata{
		Key:                 "global:1",
		Phase:               models.IntegrationAnalysisPhaseGlobal,
		Generation:          1,
		DescendantChanges:   []models.IntegrationDescendantChange{{TaskID: "coding-single", Commit: "merged-single"}},
		SourceCommit:        "global-source-1",
		AffectedPaths:       []string{"a.go"},
		SourceSnapshotPaths: []string{"a.go"},
	}

	globalTask2 := testhelpers.BuildTaskByStatus("global-analysis-2", models.TaskStatusMerged, now)
	globalTask2.ReviewCommit = testhelpers.StringPtr("global-report-2")
	globalTask2.IntegrationAnalysis = &models.IntegrationAnalysisMetadata{
		Key:                 "global:2",
		Phase:               models.IntegrationAnalysisPhaseGlobal,
		Generation:          2,
		DescendantChanges:   []models.IntegrationDescendantChange{{TaskID: "fix-2", Commit: "global-source-2"}},
		SourceCommit:        "global-source-2",
		AffectedPaths:       []string{"a.go", "b.go"},
		SourceSnapshotPaths: []string{"a.go", "b.go"},
	}
	state.Tasks = []models.Task{sliceTask, globalTask1, globalTask2}

	state.Goal.Integration = &models.IntegrationLifecycle{
		ContributingSet: &models.IntegrationContributingSet{Scopes: []models.IntegrationScopeSnapshot{
			{PlanTaskID: "plan-single", RootTaskIDs: []string{"coding-single"}},
			{PlanTaskID: "plan-multi", RootTaskIDs: []string{"coding-a", "coding-b"}},
		}},
		Coverage: []models.IntegrationCoverageRecord{
			{
				PlanTaskID: "plan-single",
				Kind:       models.IntegrationCoverageApprovalAttestation,
				ApprovalAttestation: &models.IntegrationApprovalAttestation{
					ReviewedTaskID:     "coding-single",
					AcceptanceCriteria: "single lineage accepted",
					ReviewedCommit:     "reviewed-single",
					Approver:           "code-reviewer-1",
					Validation:         []string{"go test ./internal/single"},
					MergeCommit:        "merged-single",
				},
			},
			{
				PlanTaskID: "plan-multi",
				Kind:       models.IntegrationCoverageSliceReport,
				SliceReport: &models.IntegrationSliceReport{
					AnalysisTaskID: "slice-analysis",
					AnalysisKey:    "slice:plan-multi",
					Verdict:        models.IntegrationAnalysisVerdictClean,
					SourceCommit:   "slice-source",
					ReportCommit:   "slice-report",
				},
			},
		},
		GlobalGenerations: []models.IntegrationGlobalGeneration{
			{Generation: 1, AnalysisTaskID: "global-analysis-1", AnalysisKey: "global:1", Verdict: models.IntegrationAnalysisVerdictFindings, SourceCommit: "global-source-1", ReportCommit: "global-report-1"},
			{Generation: 2, AnalysisTaskID: "global-analysis-2", AnalysisKey: "global:2", Verdict: models.IntegrationAnalysisVerdictClean, SourceCommit: "global-source-2", ReportCommit: "global-report-2"},
		},
		MutationReceipts: []models.IntegrationMutationReceipt{
			{TaskID: "fix-1", BeforeCommit: "global-source-1", AfterCommit: "fix-1-commit"},
			{TaskID: "fix-2", BeforeCommit: "fix-1-commit", AfterCommit: "global-source-2"},
		},
		Closure: &models.IntegrationClosure{
			Status:       models.IntegrationClosureStatusClean,
			Generation:   2,
			AnalysisKey:  "global:2",
			SourceCommit: "global-source-2",
		},
	}
	return state
}

func cloneIntegrationState(t *testing.T, state *models.State) *models.State {
	t.Helper()
	encoded, err := yaml.Marshal(state)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}
	var cloned models.State
	if err := yaml.Unmarshal(encoded, &cloned); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	return &cloned
}

func assertIntegrationErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want substring %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want substring %q", err, want)
	}
}
