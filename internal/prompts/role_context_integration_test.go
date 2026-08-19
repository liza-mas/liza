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
	}

	output := renderIntegrationContextForTest(t, data)
	for _, want := range []string{
		"COVERAGE MAP",
		"plan-single",
		"approval_attestation",
		"plan-sliced",
		"slice_report",
		"navigation evidence, not proof of aggregate correctness",
		"git -C '/tmp/global worktree; echo marker' diff --name-only 'goal-base-222..global-source-456'",
		"git -C '/tmp/global worktree; echo marker' diff 'goal-base-222..global-source-456' -- <path>",
		"independent aggregate review",
		"cross-scope interactions",
		"goal-level merge readiness",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("global context missing %q:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{"..HEAD", "intra-plan composition", "SLICE INTEGRATION CONTEXT"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("global context contains %q:\n%s", unwanted, output)
		}
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
