package prompts

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestSliceIntegrationContext(t *testing.T) {
	data := &RoleContextData{
		Role:                    "integration-reviewer",
		Worktree:                "/tmp/slice worktree; echo marker",
		IntegrationPhase:        models.IntegrationAnalysisPhaseSlice,
		IntegrationSourceCommit: "slice-source-123",
		IntegrationOriginatingPlan: &IntegrationPlanSummary{
			ID:      "plan-alpha",
			PlanRef: "specs/plans/alpha.md",
			ArchRef: "specs/architecture/alpha.md",
		},
		IntegrationDescendants: []IntegrationDescendantSummary{{
			ID:       "coding-a",
			DoneWhen: "Producer and consumer compose",
			Commit:   "slice-commit-aaa",
			DependsOn: []string{
				"shared-contract",
			},
			Decomposition: &models.DecompositionManifest{
				OwnedFiles:            []string{"internal/alpha/a.go"},
				ReadOnlyDependsOn:     []int{2},
				ReadOnlyTaskDependsOn: []string{"shared-read-only"},
				InterfacesOwned:       []string{"alpha.Producer"},
				InterfacesConsumed:    []string{"shared.Contract"},
			},
		}},
		IntegrationAffectedPaths: []string{"internal/alpha/a.go", "internal/alpha/deleted.go"},
		IntegrationSnapshotPaths: []string{"internal/alpha/a file; echo marker.go"},
	}

	output := renderIntegrationContextForTest(t, data)
	for _, want := range []string{
		"ORIGINATING PLAN: plan-alpha",
		"Producer and consumer compose",
		"slice-commit-aaa",
		"alpha.Producer",
		"shared.Contract",
		"shared-read-only",
		"Read-only output dependencies: 2",
		"internal/alpha/deleted.go",
		"git -C '/tmp/slice worktree; echo marker' show 'slice-source-123:internal/alpha/a file; echo marker.go'",
		"intra-plan composition",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("slice context missing %q:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{"..HEAD", "show HEAD:", "goal-level merge readiness", "GLOBAL INTEGRATION CONTEXT"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("slice context contains %q:\n%s", unwanted, output)
		}
	}
}

func TestGlobalIntegrationContext(t *testing.T) {
	data := &RoleContextData{
		Role:                    "integration-reviewer",
		Worktree:                "/tmp/global worktree; echo marker",
		GoalBaseCommit:          "goal-base-222",
		IntegrationPhase:        models.IntegrationAnalysisPhaseGlobal,
		IntegrationGeneration:   3,
		IntegrationSourceCommit: "global-source-456",
		IntegrationCoverage: []IntegrationCoverageSummary{
			{
				PlanTaskID: "plan-single",
				Kind:       string(models.IntegrationCoverageApprovalAttestation),
				ApprovalAttestations: []IntegrationApprovalSummary{{
					ReviewedTaskID: "coding-single",
					ReviewedCommit: "reviewed-single-aaa",
					MergeCommit:    "merged-single-bbb",
				}},
			},
			{
				PlanTaskID: "plan-sliced",
				Kind:       string(models.IntegrationCoverageSliceReport),
				SliceReport: &IntegrationSliceReportSummary{
					AnalysisTaskID: "slice-report-task",
					Verdict:        string(models.IntegrationAnalysisVerdictClean),
					SourceCommit:   "slice-source-789",
					ReportCommit:   "slice-report-commit",
				},
			},
		},
		IntegrationPlanSurfaces: []IntegrationPlanSurfaceSummary{{
			PlanTaskID:      "plan-single",
			Tasks:           []IntegrationTaskChangeSummary{{ID: "coding-single", Description: "Single change", BaseCommit: "base-aaa", ReviewCommit: "review-bbb", PathCount: 2}},
			PathCount:       2,
			InterfacesOwned: []string{"api:v1"},
		}},
		IntegrationPriorRepairs:   []IntegrationTaskChangeSummary{{ID: "fix-global", BaseCommit: "base-ccc", ReviewCommit: "review-ddd", PathCount: 1}},
		IntegrationSeamPaths:      []IntegrationSeamSummary{{Name: "shared.go", PlanTaskIDs: []string{"plan-single", "plan-sliced"}}},
		IntegrationSeamInterfaces: []IntegrationSeamSummary{{Name: "api:v1", PlanTaskIDs: []string{"plan-single", "plan-sliced"}}},
	}

	output := renderIntegrationContextForTest(t, data)
	for _, want := range []string{
		"COVERAGE MAP",
		"plan-single",
		"approval_attestation",
		"plan-sliced",
		"slice_report",
		"navigation evidence, not proof of aggregate correctness",
		"- plan-single (tasks 1, paths 2)\n  - coding-single @ base-aaa..review-bbb (paths 2): Single change\n  Interfaces owned: api:v1",
		"PRIOR GLOBAL REPAIRS (navigation and suite diagnosis; not seams by themselves):\n- fix-global @ base-ccc..review-ddd (paths 1)\n",
		"CROSS-PLAN SEAMS:\n- path shared.go: plan-single, plan-sliced\n- interface api:v1: plan-single, plan-sliced",
		"git -C '/tmp/global worktree; echo marker' show 'global-source-456:<path>'",
		"Plan-internal code is out of scope",
		"Defects confined to code this goal did not change are observations in your report, never fix tasks.",
		"Review only the cross-plan seams listed above",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("global context missing %q:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{
		"..HEAD",
		"intra-plan composition",
		"SLICE INTEGRATION CONTEXT",
		"goal-base-222..global-source-456",
		"independent aggregate review",
		"goal-level merge readiness",
		"no cross-plan seam",
	} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("global context contains %q:\n%s", unwanted, output)
		}
	}

	data.IntegrationSeamPaths = nil
	data.IntegrationSeamInterfaces = nil
	if output := renderIntegrationContextForTest(t, data); !strings.Contains(output, "CROSS-PLAN SEAMS:\n(no cross-plan seam: no path or declared interface is shared by two plans; only suites at HEAD apply)") {
		t.Fatalf("seamless global context missing suites-only notice:\n%s", output)
	}
}

func renderIntegrationContextForTest(t *testing.T, data *RoleContextData) string {
	t.Helper()
	output, err := BuildRoleContext(data.Role, []string{"branch-integration-context", "review-instructions"}, data)
	if err != nil {
		t.Fatalf("BuildRoleContext: %v", err)
	}
	return output
}
