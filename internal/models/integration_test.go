package models

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestIntegrationLifecycleYAMLRoundTrip(t *testing.T) {
	state := State{
		Goal: Goal{
			Integration: &IntegrationLifecycle{
				ContributingSet: &IntegrationContributingSet{Scopes: []IntegrationScopeSnapshot{
					{PlanTaskID: "plan-single", RootTaskIDs: []string{"coding-single"}},
					{PlanTaskID: "plan-multi", RootTaskIDs: []string{"coding-a", "coding-b"}},
				}},
				Coverage: []IntegrationCoverageRecord{
					{
						PlanTaskID: "plan-single",
						Kind:       IntegrationCoverageApprovalAttestation,
						ApprovalAttestations: []IntegrationApprovalAttestation{
							{
								ReviewedTaskID:     "coding-single-replacement-a",
								AcceptanceCriteria: "first replacement branch composes",
								ReviewedCommit:     "reviewed-single-a",
								Approver:           "code-reviewer-1",
								Validation:         []string{"go test ./internal/single/a"},
								MergeCommit:        "merged-single-a",
							},
							{
								ReviewedTaskID:     "coding-single-replacement-b",
								AcceptanceCriteria: "second replacement branch composes",
								ReviewedCommit:     "reviewed-single-b",
								Approver:           "code-reviewer-2",
								Validation:         []string{"go test ./internal/single/b"},
								MergeCommit:        "merged-single-b",
							},
						},
					},
					{
						PlanTaskID: "plan-multi",
						Kind:       IntegrationCoverageSliceReport,
						SliceReport: &IntegrationSliceReport{
							AnalysisTaskID: "slice-analysis",
							AnalysisKey:    "slice:plan-multi",
							Verdict:        IntegrationAnalysisVerdictClean,
							SourceCommit:   "slice-source",
							ReportCommit:   "slice-report",
						},
					},
				},
				GlobalGenerations: []IntegrationGlobalGeneration{
					{
						Generation:     1,
						AnalysisTaskID: "global-analysis-1",
						AnalysisKey:    "global:1",
						Verdict:        IntegrationAnalysisVerdictFindings,
						SourceCommit:   "global-source-1",
						ReportCommit:   "global-report-1",
					},
					{
						Generation:     2,
						AnalysisTaskID: "global-analysis-2",
						AnalysisKey:    "global:2",
						Verdict:        IntegrationAnalysisVerdictClean,
						SourceCommit:   "global-source-2",
						ReportCommit:   "global-report-2",
					},
				},
				MutationReceipts: []IntegrationMutationReceipt{
					{TaskID: "fix-1", BeforeCommit: "global-source-1", AfterCommit: "fix-1-commit"},
					{TaskID: "fix-2", BeforeCommit: "fix-1-commit", AfterCommit: "global-source-2"},
				},
				Closure: &IntegrationClosure{
					Status:       IntegrationClosureStatusClean,
					Generation:   2,
					AnalysisKey:  "global:2",
					SourceCommit: "global-source-2",
				},
			},
		},
		Tasks: []Task{
			{
				ID:           "slice-analysis",
				ReviewCommit: stringPointer("slice-report"),
				IntegrationAnalysis: &IntegrationAnalysisMetadata{
					Key:                   "slice:plan-multi",
					Phase:                 IntegrationAnalysisPhaseSlice,
					OriginatingPlanTaskID: "plan-multi",
					RootTaskIDs:           []string{"coding-a", "coding-b"},
					DescendantChanges: []IntegrationDescendantChange{
						{TaskID: "coding-a", Commit: "coding-a-commit"},
						{TaskID: "coding-b", Commit: "coding-b-commit"},
					},
					SourceCommit:        "slice-source",
					AffectedPaths:       []string{"internal/a.go", "internal/b.go"},
					SourceSnapshotPaths: []string{"internal/a.go"},
				},
			},
			{
				ID:           "global-analysis-2",
				ReviewCommit: stringPointer("global-report-2"),
				IntegrationAnalysis: &IntegrationAnalysisMetadata{
					Key:                 "global:2",
					Phase:               IntegrationAnalysisPhaseGlobal,
					Generation:          2,
					SourceCommit:        "global-source-2",
					AffectedPaths:       []string{"internal/a.go", "internal/b.go"},
					SourceSnapshotPaths: []string{"internal/a.go", "internal/b.go"},
				},
			},
		},
	}

	encoded, err := yaml.Marshal(&state)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	var decoded State
	if err := yaml.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	if !reflect.DeepEqual(state.Goal.Integration, decoded.Goal.Integration) {
		t.Fatalf("integration lifecycle round trip mismatch:\nwant: %#v\n got: %#v", state.Goal.Integration, decoded.Goal.Integration)
	}
	if len(decoded.Tasks) != len(state.Tasks) {
		t.Fatalf("decoded task count = %d, want %d", len(decoded.Tasks), len(state.Tasks))
	}
	for i := range state.Tasks {
		if !reflect.DeepEqual(state.Tasks[i].IntegrationAnalysis, decoded.Tasks[i].IntegrationAnalysis) {
			t.Fatalf("task %d integration analysis metadata round trip mismatch:\nwant: %#v\n got: %#v", i, state.Tasks[i].IntegrationAnalysis, decoded.Tasks[i].IntegrationAnalysis)
		}
		if !reflect.DeepEqual(state.Tasks[i].ReviewCommit, decoded.Tasks[i].ReviewCommit) {
			t.Fatalf("task %d review commit round trip mismatch:\nwant: %#v\n got: %#v", i, state.Tasks[i].ReviewCommit, decoded.Tasks[i].ReviewCommit)
		}
	}
	if decoded.Goal.Integration.GlobalGenerations[1].SourceCommit == decoded.Goal.Integration.GlobalGenerations[1].ReportCommit {
		t.Fatal("source commit and analyst report commit were conflated")
	}
	if got := len(decoded.Goal.Integration.Coverage[0].ApprovalAttestations); got != 2 {
		t.Fatalf("decoded approval attestation count = %d, want 2", got)
	}

	zeroScopeState := State{Goal: Goal{Integration: &IntegrationLifecycle{
		ContributingSet: &IntegrationContributingSet{Scopes: []IntegrationScopeSnapshot{}},
	}}}
	zeroScopeEncoded, err := yaml.Marshal(&zeroScopeState)
	if err != nil {
		t.Fatalf("zero-scope yaml.Marshal() error = %v", err)
	}
	var zeroScopeDecoded State
	if err := yaml.Unmarshal(zeroScopeEncoded, &zeroScopeDecoded); err != nil {
		t.Fatalf("zero-scope yaml.Unmarshal() error = %v", err)
	}
	if zeroScopeDecoded.Goal.Integration == nil || zeroScopeDecoded.Goal.Integration.ContributingSet == nil {
		t.Fatal("zero-scope contributing set became nil after round trip")
	}
	if zeroScopeDecoded.Goal.Integration.ContributingSet.Scopes == nil {
		t.Fatal("zero-scope contributing set scopes became nil after round trip")
	}
	if got := len(zeroScopeDecoded.Goal.Integration.ContributingSet.Scopes); got != 0 {
		t.Fatalf("zero-scope contributing set scope count = %d, want 0", got)
	}

	var legacy State
	if err := yaml.Unmarshal([]byte("goal:\n  id: legacy\ntasks:\n  - id: legacy-task\n"), &legacy); err != nil {
		t.Fatalf("legacy yaml.Unmarshal() error = %v", err)
	}
	if legacy.Goal.Integration != nil {
		t.Fatalf("legacy goal integration = %#v, want nil", legacy.Goal.Integration)
	}
	if legacy.Tasks[0].IntegrationAnalysis != nil {
		t.Fatalf("legacy task integration analysis = %#v, want nil", legacy.Tasks[0].IntegrationAnalysis)
	}
}

func stringPointer(value string) *string {
	return &value
}
