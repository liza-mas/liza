package integration

import (
	"io"
	"reflect"
	"testing"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// Regression for issue #151: completed integration evidence must survive
// ownership cleanup so that unrelated rejected work remains reclaimable.
func TestGlobalIntegrationCommitPreservation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	for _, releaseDoer := range []bool{false, true} {
		name := "ordinary_merge"
		if releaseDoer {
			name = "forced_doer_release_after_merge"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newSlicedLifecycleFixture(t, false)
			seedRealMutationReceipt(t, fixture)
			reconcileSlicedLifecycle(t, fixture.root)
			const analysisID = "integration-global-1"
			completeIntegrationAnalysis(t, fixture, analysisID, []models.OutputEntry{{
				Desc: "repair aggregate", DoneWhen: "aggregate repaired", Scope: "global-fix.txt", SpecRef: "README.md",
			}})
			markPlanningTransitionsConsumed(t, fixture)
			transitions, err := ops.ExecuteAvailableTransitions(fixture.root, "auto")
			if err != nil || len(transitions) != 1 || len(transitions[0].ChildTaskIDs) != 1 {
				t.Fatalf("create global fix: results=%#v err=%v", transitions, err)
			}
			mergeCodingTask(t, fixture, transitions[0].ChildTaskIDs[0], "global-fix.txt")

			// Exercise the same unrelated rejected-task reclaim that failed in #151.
			const unrelatedID = "unrelated-rejected-task"
			addReplacementTask(t, fixture, unrelatedID)
			prepareCodingTaskReview(t, fixture, unrelatedID, unrelatedID+".txt")
			if _, err := ops.SubmitVerdict(fixture.root, unrelatedID, "REJECTED", "needs another revision", "code-reviewer-1", ""); err != nil {
				t.Fatalf("reject unrelated task: %v", err)
			}
			validateOptions := commands.ValidateOptions{SkipSpecFileCheck: true, SkipProcessChecks: true, WarnWriter: io.Discard}
			if err := commands.ValidateCommandWithOptions(fixture.statePath, validateOptions); err != nil {
				t.Fatalf("state invalid before cleanup: %v", err)
			}
			before := fixture.read(t)
			analysis := before.FindTask(analysisID)
			if analysis.Status != models.TaskStatusMerged || analysis.ReviewCommit == nil || analysis.MergeCommit == nil || analysis.AssignedTo == nil {
				t.Fatalf("merged analysis lacks commit evidence or retained doer assignment: %#v", analysis)
			}
			reviewCommit, mergeCommit := *analysis.ReviewCommit, *analysis.MergeCommit
			if len(before.Goal.Integration.GlobalGenerations) != 1 || before.Goal.Integration.GlobalGenerations[0].ReportCommit != reviewCommit {
				t.Fatalf("generation does not match reviewed analysis: %#v", before.Goal.Integration.GlobalGenerations)
			}

			if releaseDoer {
				result, err := ops.ReleaseClaim(fixture.root, analysisID, "doer", true, "clean up completed analyst ownership", "human")
				if err != nil {
					t.Fatalf("release merged doer claim: %v", err)
				}
				if result == nil || !result.ReleasedDoer {
					t.Fatalf("release reported no doer claim released: %+v", result)
				}
			}

			// Reopen persisted state; no direct fixture mutation removes evidence.
			db.ResetInstance(fixture.statePath)
			after := fixture.read(t)
			analysis = after.FindTask(analysisID)
			if releaseDoer && (analysis.AssignedTo != nil || analysis.LeaseExpires != nil) {
				t.Fatal("successful release retained merged task doer ownership or lease")
			}
			if analysis.Status != models.TaskStatusMerged {
				t.Errorf("analysis status = %s, want MERGED", analysis.Status)
			}
			if analysis.ReviewCommit == nil || *analysis.ReviewCommit != reviewCommit {
				t.Errorf("merged analysis lost review_commit; want %s", reviewCommit)
			}
			if analysis.MergeCommit == nil || *analysis.MergeCommit != mergeCommit {
				t.Errorf("merged analysis lost merge_commit; want %s", mergeCommit)
			}
			if !reflect.DeepEqual(analysis.Output, before.FindTask(analysisID).Output) ||
				!reflect.DeepEqual(analysis.Approvals, before.FindTask(analysisID).Approvals) ||
				!reflect.DeepEqual(analysis.ApprovedBy, before.FindTask(analysisID).ApprovedBy) ||
				!reflect.DeepEqual(analysis.BaseCommit, before.FindTask(analysisID).BaseCommit) {
				t.Error("merged analysis lost output, approval evidence, or base commit")
			}
			if err := commands.ValidateCommandWithOptions(fixture.statePath, validateOptions); err != nil {
				t.Errorf("persisted state no longer validates: %v", err)
			}
			if _, err := ops.ClaimTask(fixture.root, unrelatedID, "coder-1"); err != nil {
				t.Errorf("unrelated rejected task cannot be reclaimed: %v", err)
			}
		})
	}
}
