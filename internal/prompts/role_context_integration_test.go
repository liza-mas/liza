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

	// Without an accepted predecessor the generation keeps the full review.
	for _, want := range []string{
		"\nSEAM REVIEW:\n",
		"\nSUITES AT HEAD:\n",
		"1. Re-read the cross-plan seams above at global-source-456 with the seam-review commands",
		"5. Enrich only within the seams and observed suite failures: could fixing A break the fix for B, and did the analyst miss a seam disagreement or a goal-owned suite failure?",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("full global context missing %q:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{"FIX-ROUND SCOPE", "escalation only", "Fix round:", "outside the delta"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("full global context contains fix-round text %q:\n%s", unwanted, output)
		}
	}

	data.IntegrationSeamPaths = nil
	data.IntegrationSeamInterfaces = nil
	if output := renderIntegrationContextForTest(t, data); !strings.Contains(output, "CROSS-PLAN SEAMS:\n(no cross-plan seam: no path or declared interface is shared by two plans; only suites at HEAD apply)") {
		t.Fatalf("seamless global context missing suites-only notice:\n%s", output)
	}
}

func TestGlobalIntegrationFixRoundContext(t *testing.T) {
	data := &RoleContextData{
		Role:                            "integration-reviewer",
		Worktree:                        "/tmp/global worktree; echo marker",
		IntegrationPhase:                models.IntegrationAnalysisPhaseGlobal,
		IntegrationGeneration:           2,
		IntegrationSourceCommit:         "gen2-source",
		IntegrationPreviousGeneration:   1,
		IntegrationPreviousVerdict:      string(models.IntegrationAnalysisVerdictFindings),
		IntegrationPreviousSourceCommit: "gen1-source",
		IntegrationFixRepairs: []IntegrationApprovalSummary{
			{ReviewedTaskID: "fix-stub", AcceptanceCriteria: "The harness stub returns the credential", ReviewedCommit: "rev-1", Approver: "reviewer-1", Validation: []string{"make test-a", "make test-b"}, MergeCommit: "merge-1"},
			{ReviewedTaskID: "fix-imports", AcceptanceCriteria: "conftest imports resolve", ReviewedCommit: "rev-2", Approver: "reviewer-2", MergeCommit: "merge-2"},
		},
		IntegrationSeamPaths: []IntegrationSeamSummary{{Name: "shared.go", PlanTaskIDs: []string{"plan-a", "plan-b"}}},
	}

	output := renderIntegrationContextForTest(t, data)
	for _, want := range []string{
		"FIX-ROUND SCOPE (generation 1 was findings at gen1-source):",
		"git -C '/tmp/global worktree; echo marker' diff --name-only gen1-source..gen2-source",
		"git -C '/tmp/global worktree; echo marker' diff --stat gen1-source..gen2-source",
		"REPAIRS OF GENERATION 1 (approval evidence: the reviewer accepted each acceptance criterion at the reviewed commit; do not re-prove it):",
		"- fix-stub @ rev-1; merge merge-1; approver reviewer-1\n  Acceptance criteria: The harness stub returns the credential\n  Validation: make test-a; make test-b\n",
		"- fix-imports @ rev-2; merge merge-2; approver reviewer-2\n  Acceptance criteria: conftest imports resolve\n  Validation: (none declared)\n",
		"a repair without declared validation, or delta behavior they do not name, needs your own smallest covering command at the source commit.",
		"Record a clean verdict without broad suites only when the delta touches only tests, test support, fixtures or docs, every repair resolves its finding, approval evidence or your delta commands cover each changed behavior, and no concrete gap remains.",
		"Escalate to the full review below only when the delta touches production code or contracts (interfaces, schemas, migrations, configuration), or you name a concrete gap.",
		"\nSEAM REVIEW (escalation only):\n",
		"\nSUITES AT HEAD (escalation only):\n",
		"Fix round: review the delta since generation 1 and the analyst's escalation decision.",
		"1. Re-read only the fix-round delta above at gen2-source; re-read the cross-plan seams with the seam-review commands only if the analyst escalated or you name a concrete gap.",
		"Check the analyst's delta classification and escalation decision, that each repair resolves its finding",
		"A fix-task outside the delta also needs a recorded escalation.",
		"Re-run a check the repairs' approval evidence names only to reproduce a reported failure at HEAD, after an escalation, or for a concrete gap you name.",
		"5. Enrich only within the delta: could one repair break another or a seam it touches, and did the analyst miss a delta-caused seam disagreement or goal-owned failure? Seams and suites outside the delta stay settled unless the analyst escalated or you name a concrete gap.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("fix-round context missing %q:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{
		"(none: the delta is work merged by others since that source)",
		"Do not re-run checks",
		"1. Re-read the cross-plan seams above",
		"5. Enrich only within the seams and observed suite failures",
	} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("fix-round context contains %q:\n%s", unwanted, output)
		}
	}

	data.IntegrationPreviousVerdict = string(models.IntegrationAnalysisVerdictClean)
	data.IntegrationFixRepairs = nil
	data.IntegrationSeamPaths = nil
	output = renderIntegrationContextForTest(t, data)
	for _, want := range []string{
		"FIX-ROUND SCOPE (generation 1 was clean at gen1-source):",
		"(none: the delta is work merged by others since that source)",
		"CROSS-PLAN SEAMS:\n(no cross-plan seam: no path or declared interface is shared by two plans; on escalation only suites at HEAD apply)",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("clean-predecessor fix-round context missing %q:\n%s", want, output)
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
