package integration

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

const slicedIntegrationTimeout = 10 * time.Second

func TestSlicedIntegrationLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	t.Run("settled boundary and zero-slice bypass", func(t *testing.T) {
		barriers := []struct {
			name     string
			unsettle func(*models.State)
		}{
			{name: "planning source", unsettle: func(state *models.State) {
				plan := mergedPlanTask(time.Now().UTC(), "plan-a")
				plan.Status = models.TaskStatusReady
				plan.ReviewCommit, plan.MergeCommit, plan.ApprovedBy = nil, nil, nil
				*state.FindTask("plan-a") = plan
			}},
			{name: "coding-producing transition", unsettle: func(state *models.State) {
				state.FindTask("plan-a").TransitionsExecuted = nil
			}},
			{name: "resulting coding work", unsettle: func(state *models.State) {
				pending := testhelpers.BuildTaskByStatus("coding-a-1", models.TaskStatusReady, time.Now().UTC())
				pending.ParentTask = testhelpers.StringPtr("plan-a")
				pending.RolePair = "coding-pair"
				*state.FindTask("coding-a-1") = pending
			}},
		}
		for _, barrier := range barriers {
			t.Run(barrier.name, func(t *testing.T) {
				fixture := newSlicedLifecycleFixture(t, true)
				fixture.modify(t, barrier.unsettle)
				result := reconcileSlicedLifecycle(t, fixture.root)
				if result.Changed || len(result.CreatedTaskIDs) != 0 || fixture.read(t).Goal.Integration != nil {
					t.Fatalf("unsettled %s opened integration: result=%#v lifecycle=%#v", barrier.name, result, fixture.read(t).Goal.Integration)
				}
			})
		}

		for _, scopes := range []int{0, 1} {
			t.Run(string(rune('0'+scopes))+" contributing scopes", func(t *testing.T) {
				fixture := newSlicedLifecycleFixture(t, false)
				if scopes == 0 {
					fixture.modify(t, func(state *models.State) {
						state.Tasks = nil
						state.Sprint.Scope.Planned = nil
					})
				}
				result := reconcileSlicedLifecycle(t, fixture.root)
				assertTaskIDs(t, result.CreatedTaskIDs, "integration-global-1")
				state := fixture.read(t)
				if state.Goal.Integration == nil || state.Goal.Integration.ContributingSet == nil || len(state.Goal.Integration.ContributingSet.Scopes) != scopes {
					t.Fatalf("%d-scope contributing set = %#v", scopes, state.Goal.Integration)
				}
				if len(state.Goal.Integration.Coverage) != 0 || countSlicedTasks(state, models.IntegrationAnalysisPhaseSlice) != 0 {
					t.Fatalf("%d-scope bypass created local coverage", scopes)
				}
				global := state.FindTask("integration-global-1")
				if global == nil || global.IntegrationAnalysis == nil || global.IntegrationAnalysis.SourceCommit != fixture.head {
					t.Fatalf("%d-scope global analysis = %#v", scopes, global)
				}
			})
		}
	})

	t.Run("mixed coverage concurrent creation and restart recovery", func(t *testing.T) {
		fixture := newSlicedLifecycleFixture(t, true)
		results := reconcileConcurrently(t, fixture.root, 8)
		changed := 0
		for _, result := range results {
			if result.Changed {
				changed++
			}
		}
		if changed != 1 {
			t.Fatalf("concurrent changed results = %d, want 1", changed)
		}

		beforeRestart := fixture.read(t)
		assertMixedCoverage(t, beforeRestart, fixture.head)
		for _, id := range []string{"integration-slice-plan-a", "integration-slice-plan-b"} {
			if countTaskID(beforeRestart, id) != 1 || countString(beforeRestart.Sprint.Scope.Planned, id) != 1 {
				t.Fatalf("%s duplicated: tasks=%d planned=%d", id, countTaskID(beforeRestart, id), countString(beforeRestart.Sprint.Scope.Planned, id))
			}
		}
		frozen := cloneLifecycleProjection(beforeRestart)

		db.ResetInstances()
		restarted, err := db.New(fixture.statePath).Read()
		if err != nil {
			t.Fatalf("fresh blackboard read: %v", err)
		}
		if !reflect.DeepEqual(cloneLifecycleProjection(restarted), frozen) {
			t.Fatal("restart changed the frozen lifecycle projection")
		}
		repeat := reconcileSlicedLifecycle(t, fixture.root)
		if repeat.Changed || len(repeat.CreatedTaskIDs) != 0 {
			t.Fatalf("restart reconciliation = %#v, want idempotent no-op", repeat)
		}
		assertBoundedSlicePrompts(t, fixture)
	})

	t.Run("blocked slice fan-in and replacement resolution", func(t *testing.T) {
		fixture := newSlicedLifecycleFixture(t, true)
		seedRealMutationReceipt(t, fixture)
		reconcileSlicedLifecycle(t, fixture.root)
		completeIntegrationAnalysis(t, fixture, "integration-slice-plan-a", nil)
		if result := reconcileSlicedLifecycle(t, fixture.root); len(result.CreatedTaskIDs) != 0 {
			t.Fatalf("one clean slice opened global analysis: %#v", result)
		}

		completeIntegrationAnalysis(t, fixture, "integration-slice-plan-b", []models.OutputEntry{{
			Desc: "repair slice composition", DoneWhen: "slice composes", Scope: "slice-fix.txt", SpecRef: "README.md",
		}})
		markPlanningTransitionsConsumed(t, fixture)
		transitionResults, err := ops.ExecuteAvailableTransitions(fixture.root, "auto")
		if err != nil || len(transitionResults) != 1 || len(transitionResults[0].ChildTaskIDs) != 1 {
			t.Fatalf("create slice fix: results=%#v err=%v", transitionResults, err)
		}
		fixID := transitionResults[0].ChildTaskIDs[0]
		coderID := ensureTestAgent(t, fixture, "coder-1", "coder")
		if _, err := ops.ClaimTask(fixture.root, fixID, coderID); err != nil {
			t.Fatalf("claim slice fix: %v", err)
		}
		if _, err := ops.MarkBlocked(fixture.root, fixID, "slice repair cannot complete", []string{"Create replacement?"}, coderID); err != nil {
			t.Fatalf("MarkBlocked(slice fix): %v", err)
		}
		result := reconcileSlicedLifecycle(t, fixture.root)
		state := fixture.read(t)
		if result.Reason == nil || state.Goal.Integration.Closure == nil || state.Goal.Integration.Closure.Status != models.IntegrationClosureStatusBlocked {
			t.Fatalf("blocked slice fan-in result=%#v closure=%#v", result, state.Goal.Integration.Closure)
		}
		if state.FindTask("integration-global-1") != nil {
			t.Fatal("blocked slice fan-in created global analysis")
		}

		addReplacementTask(t, fixture, "slice-fix-replacement")
		if _, err := ops.SupersedeTask(fixture.root, fixID, []string{"slice-fix-replacement"}, "replace blocked slice repair", "orchestrator-1"); err != nil {
			t.Fatalf("SupersedeTask(slice fix): %v", err)
		}
		mergeCodingTask(t, fixture, "slice-fix-replacement", "slice-fix-replacement.txt")
		results := reconcileConcurrently(t, fixture.root, 6)
		assertExactlyOneCreatedTask(t, results, "integration-global-1")
		state = fixture.read(t)
		if countTaskID(state, "integration-global-1") != 1 || countString(state.Sprint.Scope.Planned, "integration-global-1") != 1 {
			t.Fatalf("resolved fan-in duplicated global task")
		}
	})

	t.Run("promoted repair and sibling mutation preserve frozen slices", func(t *testing.T) {
		fixture := newSlicedLifecycleFixture(t, true)
		seedRealMutationReceipt(t, fixture)
		reconcileSlicedLifecycle(t, fixture.root)
		completeIntegrationAnalysis(t, fixture, "integration-slice-plan-a", nil)
		completeIntegrationAnalysis(t, fixture, "integration-slice-plan-b", []models.OutputEntry{{
			Desc: "repair composition through planning", DoneWhen: "planned repair composes", Scope: "promoted-repair.txt", SpecRef: "README.md",
		}})
		markPlanningTransitionsConsumed(t, fixture)
		transitionResults, err := ops.ExecuteAvailableTransitions(fixture.root, "auto")
		if err != nil || len(transitionResults) != 1 || len(transitionResults[0].ChildTaskIDs) != 1 {
			t.Fatalf("create promoted integration fix: results=%#v err=%v", transitionResults, err)
		}
		fixID := transitionResults[0].ChildTaskIDs[0]
		beforeEscalation := cloneSliceEvidence(fixture.read(t))
		frozenCohort := *fixture.read(t).Goal.Integration.ContributingSet
		const escalatedPlanID = "integration-repair-code-plan"
		addIntegrationRepairPlan(t, fixture, escalatedPlanID, fixID)
		mergeCodingTask(t, fixture, fixID, "promoted-repair.txt")
		codingChildren := completeIntegrationRepairPlan(t, fixture, escalatedPlanID)
		state := fixture.read(t)
		if !reflect.DeepEqual(*state.Goal.Integration.ContributingSet, frozenCohort) {
			t.Fatalf("promoted repair changed frozen cohort: got=%#v want=%#v", state.Goal.Integration.ContributingSet, frozenCohort)
		}
		if !reflect.DeepEqual(cloneSliceEvidence(state), beforeEscalation) {
			t.Fatal("promoted repair changed coverage, slice keys, evidence, or slice planned memberships")
		}
		if state.FindTask("integration-slice-"+escalatedPlanID) != nil || countSlicedTasks(state, models.IntegrationAnalysisPhaseSlice) != 2 {
			t.Fatal("promoted integration repair created a new slice")
		}
		plan := state.FindTask(escalatedPlanID)
		if plan == nil || plan.ParentTask == nil || *plan.ParentTask != fixID || plan.Status != models.TaskStatusMerged || countString(state.Sprint.Scope.Planned, escalatedPlanID) != 1 {
			t.Fatalf("promoted repair plan lineage = %#v", plan)
		}
		for _, childID := range codingChildren {
			child := state.FindTask(childID)
			if child == nil || !slices.Contains(child.EffectiveParentTasks(), escalatedPlanID) || child.Status != models.TaskStatusMerged || child.MergeCommit == nil || countString(state.Sprint.Scope.Planned, childID) != 1 {
				t.Fatalf("promoted coding descendant %s = %#v", childID, child)
			}
		}

		before := cloneSliceEvidence(state)
		taskID, reviewerID := prepareApprovedMutation(t, fixture, "later-sibling-mutation")
		beforeHead := fixture.integrationHead(t)
		if _, err := ops.MergeWorktree(fixture.root, taskID, reviewerID); err != nil {
			t.Fatalf("MergeWorktree(sibling): %v", err)
		}
		afterHead := fixture.integrationHead(t)
		assertRealMutationReceipt(t, fixture.read(t), taskID, beforeHead, afterHead)
		if !reflect.DeepEqual(cloneSliceEvidence(fixture.read(t)), before) {
			t.Fatal("later sibling mutation rewrote completed slice evidence")
		}
		reconcileSlicedLifecycle(t, fixture.root)
		state = fixture.read(t)
		globalOne := state.FindTask("integration-global-1")
		if globalOne == nil || globalOne.IntegrationAnalysis.SourceCommit != afterHead {
			t.Fatalf("global analysis source after sibling mutation = %#v, want %s", globalOne, afterHead)
		}
	})

	t.Run("slice repair review exhaustion blocks global fan-in", func(t *testing.T) {
		fixture := newSlicedLifecycleFixture(t, true)
		fixture.modify(t, func(state *models.State) { state.Config.MaxReviewCycles = 1 })
		reconcileSlicedLifecycle(t, fixture.root)
		completeIntegrationAnalysis(t, fixture, "integration-slice-plan-a", nil)
		completeIntegrationAnalysis(t, fixture, "integration-slice-plan-b", []models.OutputEntry{{
			Desc: "repair exhausted slice", DoneWhen: "slice repaired", Scope: "slice-exhausted.txt", SpecRef: "README.md",
		}})
		markPlanningTransitionsConsumed(t, fixture)
		transitionResults, err := ops.ExecuteAvailableTransitions(fixture.root, "auto")
		if err != nil || len(transitionResults) != 1 || len(transitionResults[0].ChildTaskIDs) != 1 {
			t.Fatalf("create exhausted slice fix: results=%#v err=%v", transitionResults, err)
		}
		fixID := transitionResults[0].ChildTaskIDs[0]
		prepareCodingTaskReview(t, fixture, fixID, "slice-exhausted.txt")
		firstRejection, err := ops.SubmitVerdict(fixture.root, fixID, "REJECTED", "still violates slice composition", "code-reviewer-1", "")
		if err != nil || !firstRejection.NewAttemptTriggered {
			t.Fatalf("SubmitVerdict(first exhaustion cycle): result=%#v err=%v", firstRejection, err)
		}
		prepareCodingTaskReview(t, fixture, fixID, "slice-exhausted.txt")
		secondRejection, err := ops.SubmitVerdict(fixture.root, fixID, "REJECTED", "still violates slice composition", "code-reviewer-1", "")
		if err != nil || !secondRejection.EscalatedToBlocked {
			t.Fatalf("SubmitVerdict(second exhaustion cycle): result=%#v err=%v", secondRejection, err)
		}
		state := fixture.read(t)
		if state.FindTask(fixID).Status != models.TaskStatusBlocked {
			t.Fatalf("exhausted slice fix status = %s", state.FindTask(fixID).Status)
		}
		result := reconcileSlicedLifecycle(t, fixture.root)
		state = fixture.read(t)
		if result.Reason == nil || state.Goal.Integration.Closure == nil || state.Goal.Integration.Closure.Status != models.IntegrationClosureStatusBlocked || state.FindTask("integration-global-1") != nil {
			t.Fatalf("slice exhaustion result=%#v closure=%#v", result, state.Goal.Integration.Closure)
		}
	})

	t.Run("global fix rescan restart and generation exhaustion", func(t *testing.T) {
		fixture := newSlicedLifecycleFixture(t, true)
		seedRealMutationReceipt(t, fixture)
		fixture.modify(t, func(state *models.State) { state.Config.MaxGlobalIntegrationGenerations = 2 })
		reconcileSlicedLifecycle(t, fixture.root)
		completeIntegrationAnalysis(t, fixture, "integration-slice-plan-a", nil)
		completeIntegrationAnalysis(t, fixture, "integration-slice-plan-b", nil)
		result := reconcileSlicedLifecycle(t, fixture.root)
		assertTaskIDs(t, result.CreatedTaskIDs, "integration-global-1")
		globalOne := fixture.read(t).FindTask("integration-global-1")
		if globalOne == nil || globalOne.IntegrationAnalysis.SourceCommit != fixture.integrationHead(t) {
			t.Fatalf("generation one = %#v", globalOne)
		}
		assertGlobalIntegrationPrompt(t, fixture, "integration-global-1")

		completeIntegrationAnalysis(t, fixture, "integration-global-1", []models.OutputEntry{{
			Desc: "repair aggregate", DoneWhen: "aggregate repaired", Scope: "global-fix.txt", SpecRef: "README.md",
		}})
		markPlanningTransitionsConsumed(t, fixture)
		transitionResults, err := ops.ExecuteAvailableTransitions(fixture.root, "auto")
		if err != nil {
			t.Fatalf("execute global repair transition: %v", err)
		}
		if len(transitionResults) != 1 || len(transitionResults[0].ChildTaskIDs) != 1 {
			t.Fatalf("global repair transition results = %#v", transitionResults)
		}
		fixID := transitionResults[0].ChildTaskIDs[0]
		mergeCodingTask(t, fixture, fixID, "global-fix.txt")
		reconcileSlicedLifecycle(t, fixture.root)
		state := fixture.read(t)
		globalTwo := state.FindTask("integration-global-2")
		if globalTwo == nil || globalTwo.IntegrationAnalysis == nil || globalTwo.IntegrationAnalysis.Generation != 2 || globalTwo.IntegrationAnalysis.SourceCommit != fixture.integrationHead(t) {
			t.Fatalf("generation two after fix = %#v", globalTwo)
		}
		if len(state.Goal.Integration.Coverage) != 3 {
			t.Fatalf("global repair changed frozen local coverage: %#v", state.Goal.Integration.Coverage)
		}

		beforeRestart := cloneLifecycleProjection(state)
		db.ResetInstances()
		repeat := reconcileSlicedLifecycle(t, fixture.root)
		if repeat.Changed || !reflect.DeepEqual(cloneLifecycleProjection(fixture.read(t)), beforeRestart) {
			t.Fatalf("generation-two restart was not idempotent: %#v", repeat)
		}

		completeIntegrationAnalysis(t, fixture, "integration-global-2", []models.OutputEntry{{
			Desc: "repair aggregate again", DoneWhen: "second aggregate repair merged", Scope: "global-fix-2.txt", SpecRef: "README.md",
		}})
		markPlanningTransitionsConsumed(t, fixture)
		transitionResults, err = ops.ExecuteAvailableTransitions(fixture.root, "auto")
		if err != nil {
			t.Fatalf("execute exhausting repair transition: %v", err)
		}
		if len(transitionResults) != 1 || len(transitionResults[0].ChildTaskIDs) != 1 {
			t.Fatalf("exhausting repair transition results = %#v", transitionResults)
		}
		mergeCodingTask(t, fixture, transitionResults[0].ChildTaskIDs[0], "global-fix-2.txt")
		reconcileSlicedLifecycle(t, fixture.root)
		state = fixture.read(t)
		if state.Goal.Integration.Closure == nil || state.Goal.Integration.Closure.Status != models.IntegrationClosureStatusExhausted {
			t.Fatalf("generation exhaustion closure = %#v", state.Goal.Integration.Closure)
		}
		if state.FindTask("integration-global-3") != nil {
			t.Fatal("generation exhaustion created generation three")
		}
	})

	t.Run("frozen pipeline fails closed", func(t *testing.T) {
		fixture := newSlicedLifecycleFixture(t, true)
		freezeLegacyIntegrationPipeline(t, fixture.root)
		result := reconcileSlicedLifecycle(t, fixture.root)
		state := fixture.read(t)
		if result.Reason == nil || result.Reason.Code != pipeline.SlicedIntegrationUpgradeRequired {
			t.Fatalf("legacy frozen reconciliation reason = %#v", result.Reason)
		}
		if state.Goal.Integration == nil || state.Goal.Integration.Closure == nil || state.Goal.Integration.Closure.Status != models.IntegrationClosureStatusBlocked || state.Goal.Integration.Closure.Reason != pipeline.SlicedIntegrationUpgradeRequired {
			t.Fatalf("legacy frozen closure = %#v", state.Goal.Integration)
		}
		if countSlicedTasks(state, models.IntegrationAnalysisPhaseSlice)+countSlicedTasks(state, models.IntegrationAnalysisPhaseGlobal) != 0 {
			t.Fatal("legacy frozen pipeline created integration analysis work")
		}
	})

	t.Run("immediate invalidation blocks resume and advance", func(t *testing.T) {
		for _, operation := range []struct {
			name   string
			invoke func(string) error
		}{
			{name: "resume", invoke: func(root string) error { _, err := ops.Resume(root, "tester"); return err }},
			{name: "advance", invoke: func(root string) error { _, err := ops.AdvanceSprint(root); return err }},
		} {
			t.Run(operation.name, func(t *testing.T) {
				fixture := newCleanCompletionFixture(t)
				fixture.modify(t, func(state *models.State) {
					state.Sprint.Status = models.SprintStatusCheckpoint
					state.Sprint.Scope.Planned = []string{"integration-global-1"}
				})
				source := fixture.integrationHead(t)
				taskID, reviewerID := prepareApprovedMutation(t, fixture, "post-clean-mutation")
				before := fixture.integrationHead(t)
				if _, err := ops.MergeWorktree(fixture.root, taskID, reviewerID); err != nil {
					t.Fatalf("MergeWorktree(): %v", err)
				}
				live := fixture.integrationHead(t)
				if live == source {
					t.Fatal("public mutation did not advance integration HEAD")
				}
				assertRealMutationReceipt(t, fixture.read(t), taskID, before, live)
				err := operation.invoke(fixture.root)
				var precondition *ops.PreconditionError
				if !errors.As(err, &precondition) || !strings.Contains(precondition.Reason, "integration") {
					t.Fatalf("%s error = %v, want integration precondition", operation.name, err)
				}
				state := fixture.read(t)
				if state.Sprint.Status != models.SprintStatusCheckpoint || len(state.SprintHistory) != 0 {
					t.Fatalf("%s advanced stale sprint: status=%s history=%v", operation.name, state.Sprint.Status, state.SprintHistory)
				}
				next := state.FindTask("integration-global-2")
				if next == nil || next.IntegrationAnalysis == nil || next.IntegrationAnalysis.SourceCommit != live {
					t.Fatalf("%s next generation = %#v, want source %s", operation.name, next, live)
				}
			})
		}
	})
}

func TestSlicedIntegrationFinalizationRace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	t.Run("mutation before finalization", func(t *testing.T) {
		fixture := newPendingGlobalFinalizationFixture(t)
		source := fixture.integrationHead(t)
		taskID, reviewerID := prepareApprovedMutation(t, fixture, "mutation-before-finalization")
		barrier := installMergeBarrier(t, fixture.root)
		mergeDone := make(chan error, 1)
		verdictDone := make(chan error, 1)
		go func() {
			_, err := ops.MergeWorktree(fixture.root, taskID, reviewerID)
			mergeDone <- err
		}()
		barrier.wait(t)
		live := fixture.integrationHead(t)
		if live == source {
			t.Fatal("mutation did not linearize before finalization")
		}
		go func() {
			_, err := ops.SubmitVerdict(fixture.root, "integration-global-1", "APPROVED", "", "integration-reviewer-1", "")
			verdictDone <- err
		}()
		receiveError(t, "finalization", verdictDone)
		barrier.release(t)
		receiveError(t, "mutation", mergeDone)

		state := fixture.read(t)
		if len(state.Goal.Integration.GlobalGenerations) != 1 || state.Goal.Integration.GlobalGenerations[0].SourceCommit != source {
			t.Fatalf("finalized evidence = %#v, want immutable source %s", state.Goal.Integration.GlobalGenerations, source)
		}
		if state.Goal.Integration.Closure != nil && state.Goal.Integration.Closure.Status == models.IntegrationClosureStatusClean {
			t.Fatalf("stale finalization produced clean closure: %#v", state.Goal.Integration.Closure)
		}
		if _, err := ops.StopForGoalCompletion(fixture.root, "goal complete"); err == nil {
			t.Fatal("stale finalization allowed automatic goal-complete stop")
		}
		if state = fixture.read(t); state.Config.Mode != models.SystemModeRunning {
			t.Fatalf("stale finalization left mode %s", state.Config.Mode)
		}
		assertRealMutationReceipt(t, state, taskID, source, live)
		assertIntegrationIncomplete(t, fixture, live)
	})

	t.Run("mutation after finalization", func(t *testing.T) {
		fixture := newPendingGlobalFinalizationFixture(t)
		source := fixture.integrationHead(t)
		taskID, reviewerID := prepareApprovedMutation(t, fixture, "mutation-after-finalization")
		if _, err := ops.SubmitVerdict(fixture.root, "integration-global-1", "APPROVED", "", "integration-reviewer-1", ""); err != nil {
			t.Fatalf("SubmitVerdict(clean): %v", err)
		}
		releaseCompletion := holdProjectLock(t, fixture.root, "integration-completion")
		integrationWatcher, integrationOwnerPath := watchProjectLockOwner(t, fixture.root, "integration-mutation")
		completionWatcher, completionOwnerPath := watchProjectLockOwner(t, fixture.root, "integration-completion")
		stopDone := make(chan error, 1)
		mergeDone := make(chan error, 1)
		go func() {
			_, err := ops.StopForGoalCompletion(fixture.root, "goal complete")
			stopDone <- err
		}()
		waitForLockOperation(t, integrationWatcher, integrationOwnerPath, "verify effective integration completion")
		releaseState := holdBlackboardWriteLock(t, fixture)
		releaseCompletion()
		waitForLockOperationOrResult(t, completionWatcher, completionOwnerPath, "goal-complete stop", stopDone)
		barrier := installMergeBarrier(t, fixture.root)
		go func() {
			_, err := ops.MergeWorktree(fixture.root, taskID, reviewerID)
			mergeDone <- err
		}()
		releaseState()
		barrier.wait(t)
		barrier.release(t)
		receiveError(t, "mutation", mergeDone)
		stopErr := receiveOperationResult(t, "goal-complete stop", stopDone)
		if stopErr != nil {
			var precondition *ops.PreconditionError
			if !errors.As(stopErr, &precondition) || !strings.Contains(precondition.Reason, "integration_state_changed") {
				t.Fatalf("goal-complete stop error = %v, want success or integration_state_changed precondition", stopErr)
			}
		}

		live := fixture.integrationHead(t)
		if live == source {
			t.Fatal("mutation-after-finalization did not advance integration HEAD")
		}
		state := fixture.read(t)
		if len(state.Goal.Integration.GlobalGenerations) != 1 || state.Goal.Integration.GlobalGenerations[0].SourceCommit != source {
			t.Fatalf("prior clean evidence was rewritten: %#v", state.Goal.Integration.GlobalGenerations)
		}
		if state.Config.Mode != models.SystemModeRunning {
			t.Fatalf("mutation-side invalidation left mode %s", state.Config.Mode)
		}
		if _, err := ops.StopForGoalCompletion(fixture.root, "stale goal complete"); err == nil {
			t.Fatal("post-finalization mutation retained effective stale success")
		}
		assertRealMutationReceipt(t, state, taskID, source, live)
		assertIntegrationIncomplete(t, fixture, live)
		reconcileSlicedLifecycle(t, fixture.root)
		next := fixture.read(t).FindTask("integration-global-2")
		if next == nil || next.IntegrationAnalysis == nil || next.IntegrationAnalysis.SourceCommit != live {
			t.Fatalf("post-finalization mutation next generation = %#v, want source %s", next, live)
		}
	})
}

func TestSlicedIntegrationBarrierHelper(t *testing.T) {
	address := os.Getenv("LIZA_TEST_SLICED_BARRIER_ADDR")
	if address == "" {
		return
	}
	if lockPath := os.Getenv("LIZA_TEST_SLICED_LOCK_PATH"); lockPath != "" {
		lock := filelock.New(lockPath).WithTimeout(slicedIntegrationTimeout)
		if err := lock.WithLockOperation("external test hold", func() error {
			connection, err := net.DialTimeout("tcp", address, slicedIntegrationTimeout)
			if err != nil {
				return err
			}
			defer connection.Close()
			if _, err := connection.Write([]byte{1}); err != nil {
				return err
			}
			buffer := []byte{0}
			_, err = connection.Read(buffer)
			return err
		}); err != nil {
			t.Fatalf("hold external sliced integration lock: %v", err)
		}
		return
	}
	connection, err := net.DialTimeout("tcp", address, slicedIntegrationTimeout)
	if err != nil {
		t.Fatalf("connect to sliced integration barrier: %v", err)
	}
	defer connection.Close()
	if _, err := connection.Write([]byte{1}); err != nil {
		t.Fatalf("signal sliced integration barrier: %v", err)
	}
	buffer := []byte{0}
	if _, err := connection.Read(buffer); err != nil {
		t.Fatalf("await sliced integration barrier release: %v", err)
	}
}

type slicedLifecycleFixture struct {
	root                  string
	statePath             string
	goalBase              string
	head                  string
	commits               map[string]string
	sourceSentinels       map[string]string
	aggregateSentinelPath string
	aggregateSentinel     string
}

type lifecycleProjection struct {
	Lifecycle *models.IntegrationLifecycle
	Tasks     map[string]models.Task
	Planned   []string
}

type sliceEvidenceProjection struct {
	Coverage []models.IntegrationCoverageRecord
	Tasks    map[string]models.Task
	Planned  []string
}

type mergeBarrier struct {
	listener net.Listener
	conn     net.Conn
}

func newSlicedLifecycleFixture(t *testing.T, multipleScopes bool) *slicedLifecycleFixture {
	t.Helper()
	db.ResetInstances()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	base := testhelpers.MustGit(t, root, "rev-parse", "HEAD")

	ids := []string{"coding-single-leaf-a", "coding-single-leaf-z"}
	if multipleScopes {
		ids = []string{"coding-a-1", "coding-a-2", "coding-b-1", "coding-b-2", "coding-single-leaf-a", "coding-single-leaf-z"}
	}
	commits := make(map[string]string, len(ids))
	bases := make(map[string]string, len(ids))
	sourceSentinels := map[string]string{
		"coding-a-1":           "ALPHA-LEFT-IMMUTABLE-SOURCE-731",
		"coding-a-2":           "ALPHA-RIGHT-IMMUTABLE-SOURCE-947",
		"coding-b-1":           "BETA-LEFT-IMMUTABLE-SOURCE-263",
		"coding-b-2":           "BETA-RIGHT-IMMUTABLE-SOURCE-589",
		"coding-single-leaf-a": "ATTESTATION-ONLY-SOURCE-181",
		"coding-single-leaf-z": "ATTESTATION-ONLY-SOURCE-827",
	}
	previous := base
	for _, id := range ids {
		bases[id] = previous
		path := id + ".txt"
		if err := os.WriteFile(filepath.Join(root, path), []byte(sourceSentinels[id]+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		testhelpers.MustGit(t, root, "add", path)
		testhelpers.MustGit(t, root, "commit", "-m", "add "+id)
		previous = testhelpers.MustGit(t, root, "rev-parse", "HEAD")
		commits[id] = previous
	}
	const aggregateSentinelPath = "aggregate-independent-source.txt"
	const aggregateSentinel = "GOAL-WIDE-AGGREGATE-SOURCE-ONLY-419"
	if err := os.WriteFile(filepath.Join(root, aggregateSentinelPath), []byte(aggregateSentinel+"\n"), 0o644); err != nil {
		t.Fatalf("write aggregate source sentinel: %v", err)
	}
	testhelpers.MustGit(t, root, "add", aggregateSentinelPath)
	testhelpers.MustGit(t, root, "commit", "-m", "add independent aggregate source")
	previous = testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	testhelpers.MustGit(t, root, "update-ref", "refs/heads/integration", previous)

	now := time.Now().UTC()
	tasks := make([]models.Task, 0, len(ids)+3)
	if multipleScopes {
		tasks = append(tasks, mergedPlanTask(now, "plan-a"), mergedPlanTask(now, "plan-b"))
		for _, id := range []string{"coding-a-1", "coding-a-2"} {
			tasks = append(tasks, mergedCodingTask(now, id, "plan-a", bases[id], commits[id]))
		}
		for _, id := range []string{"coding-b-1", "coding-b-2"} {
			tasks = append(tasks, mergedCodingTask(now, id, "plan-b", bases[id], commits[id]))
		}
	}
	rootTask := testhelpers.BuildTaskByStatus("coding-single", models.TaskStatusSuperseded, now)
	rootTask.Type = models.TaskTypeCoding
	rootTask.RolePair = "coding-pair"
	rootTask.ParentTask = testhelpers.StringPtr("plan-single")
	rootTask.SupersededBy = []string{"coding-single-leaf-z", "coding-single-leaf-a"}
	rootTask.RescopeReason = testhelpers.StringPtr("split one lineage into replacement leaves")
	rootTask.SpecRef = "README.md"
	rootTask.DoneWhen = "single lineage root replaced"
	rootTask.Scope = "coding-single.txt"
	leafA := mergedCodingTask(now, "coding-single-leaf-a", "plan-single", bases["coding-single-leaf-a"], commits["coding-single-leaf-a"])
	leafA.Supersedes = testhelpers.StringPtr(rootTask.ID)
	leafZ := mergedCodingTask(now, "coding-single-leaf-z", "plan-single", bases["coding-single-leaf-z"], commits["coding-single-leaf-z"])
	leafZ.Supersedes = testhelpers.StringPtr(rootTask.ID)
	tasks = append(tasks, mergedPlanTask(now, "plan-single"), rootTask, leafA, leafZ)

	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	state.Goal.BaseCommit = testhelpers.StringPtr(base)
	state.Goal.Integration = nil
	state.Tasks = tasks
	state.Sprint.Scope.Planned = taskIDsForSlicedFixture(tasks)
	testhelpers.WriteInitialState(t, statePath, state)
	db.ResetInstances()
	return &slicedLifecycleFixture{
		root: root, statePath: statePath, goalBase: base, head: previous, commits: commits,
		sourceSentinels: sourceSentinels, aggregateSentinelPath: aggregateSentinelPath, aggregateSentinel: aggregateSentinel,
	}
}

func mergedPlanTask(now time.Time, id string) models.Task {
	task := testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, now)
	task.Type = models.TaskTypePlanning
	task.RolePair = "code-planning-pair"
	task.SpecRef = "README.md"
	task.PlanRef = "README.md"
	task.ArchRef = "README.md"
	task.DoneWhen = "plan " + id + " implemented"
	task.Scope = id
	task.Output = []models.OutputEntry{{Desc: "coding from " + id, DoneWhen: "coding merged", Scope: id, SpecRef: "README.md"}}
	task.TransitionsExecuted = map[string]bool{
		"code-plan-decompose": true,
		"code-plan-to-coding": true,
	}
	task.ReviewCommit = testhelpers.StringPtr("review-" + id)
	return task
}

func mergedCodingTask(now time.Time, id, planID, base, commit string) models.Task {
	task := testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, now)
	task.Type = models.TaskTypeCoding
	task.RolePair = "coding-pair"
	task.ParentTask = testhelpers.StringPtr(planID)
	task.SpecRef = "README.md"
	task.DoneWhen = "acceptance for " + id
	task.Scope = id + ".txt"
	task.Validation = []string{"project test"}
	task.BaseCommit = testhelpers.StringPtr(base)
	task.ReviewCommit = testhelpers.StringPtr(commit)
	task.MergeCommit = testhelpers.StringPtr(commit)
	task.Decomposition = &models.DecompositionManifest{
		OwnedFiles:      []string{id + ".txt"},
		OwnedModules:    []string{planID},
		InterfacesOwned: []string{id + ".Boundary"},
		CoverageNotes:   "bounded coverage for " + planID,
	}
	return task
}

func (fixture *slicedLifecycleFixture) read(t *testing.T) *models.State {
	t.Helper()
	state, err := db.For(fixture.statePath).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	return state
}

func (fixture *slicedLifecycleFixture) modify(t *testing.T, mutate func(*models.State)) {
	t.Helper()
	if err := db.For(fixture.statePath).Modify(func(state *models.State) error {
		mutate(state)
		return nil
	}); err != nil {
		t.Fatalf("modify state prerequisite: %v", err)
	}
}

func (fixture *slicedLifecycleFixture) integrationHead(t *testing.T) string {
	t.Helper()
	head, err := gitpkg.New(fixture.root).GetCommitSHA("refs/heads/integration")
	if err != nil {
		t.Fatalf("read integration HEAD: %v", err)
	}
	return head
}

func reconcileSlicedLifecycle(t *testing.T, root string) *ops.ReconcileIntegrationAnalysesResult {
	t.Helper()
	result, err := ops.ReconcileIntegrationAnalyses(root)
	if err != nil {
		t.Fatalf("ReconcileIntegrationAnalyses(): %v", err)
	}
	return result
}

func reconcileConcurrently(t *testing.T, root string, callers int) []*ops.ReconcileIntegrationAnalysesResult {
	t.Helper()
	start := make(chan struct{})
	type call struct {
		result *ops.ReconcileIntegrationAnalysesResult
		err    error
	}
	calls := make(chan call, callers)
	var ready sync.WaitGroup
	ready.Add(callers)
	for range callers {
		go func() {
			ready.Done()
			<-start
			result, err := ops.ReconcileIntegrationAnalyses(root)
			calls <- call{result: result, err: err}
		}()
	}
	ready.Wait()
	close(start)
	results := make([]*ops.ReconcileIntegrationAnalysesResult, 0, callers)
	for range callers {
		select {
		case call := <-calls:
			if call.err != nil {
				t.Fatalf("concurrent reconciliation: %v", call.err)
			}
			results = append(results, call.result)
		case <-time.After(slicedIntegrationTimeout):
			t.Fatal("timed out waiting for concurrent reconciliation")
		}
	}
	return results
}

func assertMixedCoverage(t *testing.T, state *models.State, sourceCommit string) {
	t.Helper()
	if state.Goal.Integration == nil || state.Goal.Integration.ContributingSet == nil {
		t.Fatalf("integration lifecycle not frozen: %#v", state.Goal.Integration)
	}
	wantScopes := []models.IntegrationScopeSnapshot{
		{PlanTaskID: "plan-a", RootTaskIDs: []string{"coding-a-1", "coding-a-2"}},
		{PlanTaskID: "plan-b", RootTaskIDs: []string{"coding-b-1", "coding-b-2"}},
		{PlanTaskID: "plan-single", RootTaskIDs: []string{"coding-single"}},
	}
	if !reflect.DeepEqual(state.Goal.Integration.ContributingSet.Scopes, wantScopes) {
		t.Fatalf("contributing scopes = %#v, want %#v", state.Goal.Integration.ContributingSet.Scopes, wantScopes)
	}
	if len(state.Goal.Integration.Coverage) != 1 {
		t.Fatalf("initial mixed coverage = %#v, want one attestation", state.Goal.Integration.Coverage)
	}
	attestation := state.Goal.Integration.Coverage[0]
	if attestation.PlanTaskID != "plan-single" || attestation.Kind != models.IntegrationCoverageApprovalAttestation || len(attestation.ApprovalAttestations) != 2 {
		t.Fatalf("approval attestation = %#v", attestation)
	}
	for i, id := range []string{"coding-single-leaf-a", "coding-single-leaf-z"} {
		leaf := state.FindTask(id)
		got := attestation.ApprovalAttestations[i]
		want := models.IntegrationApprovalAttestation{
			ReviewedTaskID: id, AcceptanceCriteria: leaf.DoneWhen, ReviewedCommit: *leaf.ReviewCommit,
			Approver: *leaf.ApprovedBy, Validation: []string{"project test"}, MergeCommit: *leaf.MergeCommit,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("approval attestation %s = %#v, want %#v", id, got, want)
		}
	}
	for _, planID := range []string{"plan-a", "plan-b"} {
		task := state.FindTask("integration-slice-" + planID)
		if task == nil || task.IntegrationAnalysis == nil {
			t.Fatalf("slice %s missing", planID)
		}
		metadata := task.IntegrationAnalysis
		rootIDs := []string{"coding-" + strings.TrimPrefix(planID, "plan-") + "-1", "coding-" + strings.TrimPrefix(planID, "plan-") + "-2"}
		changes := make([]models.IntegrationDescendantChange, 0, len(rootIDs))
		paths := make([]string, 0, len(rootIDs))
		for _, id := range rootIDs {
			changes = append(changes, models.IntegrationDescendantChange{TaskID: id, Commit: *state.FindTask(id).MergeCommit})
			paths = append(paths, id+".txt")
		}
		if metadata.Key != "slice:"+planID || metadata.Phase != models.IntegrationAnalysisPhaseSlice || metadata.OriginatingPlanTaskID != planID || metadata.SourceCommit != sourceCommit ||
			!reflect.DeepEqual(metadata.RootTaskIDs, rootIDs) || !reflect.DeepEqual(metadata.DescendantChanges, changes) ||
			!reflect.DeepEqual(metadata.AffectedPaths, paths) || !reflect.DeepEqual(metadata.SourceSnapshotPaths, paths) {
			t.Fatalf("slice %s metadata = %#v", planID, metadata)
		}
	}
	if state.FindTask("integration-slice-plan-single") != nil {
		t.Fatal("attestation-only scope received a slice")
	}
}

func assertBoundedSlicePrompts(t *testing.T, fixture *slicedLifecycleFixture) {
	t.Helper()
	for _, tc := range []struct {
		planID      string
		rootIDs     []string
		otherPlanID string
		otherIDs    []string
		agentID     string
	}{
		{planID: "plan-a", rootIDs: []string{"coding-a-1", "coding-a-2"}, otherPlanID: "plan-b", otherIDs: []string{"coding-b-1", "coding-b-2"}, agentID: "integration-analyst-11"},
		{planID: "plan-b", rootIDs: []string{"coding-b-1", "coding-b-2"}, otherPlanID: "plan-a", otherIDs: []string{"coding-a-1", "coding-a-2"}, agentID: "integration-analyst-12"},
	} {
		taskID := "integration-slice-" + tc.planID
		ensureTestAgent(t, fixture, tc.agentID, roles.IntegrationAnalyst)
		if _, err := ops.ClaimTask(fixture.root, taskID, tc.agentID); err != nil {
			t.Fatalf("ClaimTask(%s): %v", taskID, err)
		}
		prompt := buildIntegrationPrompt(t, fixture, taskID, tc.agentID)
		for _, want := range append([]string{
			"SLICE INTEGRATION CONTEXT", "SOURCE COMMIT: " + fixture.head, "ORIGINATING PLAN: " + tc.planID,
			"README.md", tc.planID,
		}, tc.rootIDs...) {
			if !strings.Contains(prompt, want) {
				t.Fatalf("slice %s prompt missing %q", tc.planID, want)
			}
		}
		for _, rootID := range tc.rootIDs {
			for _, want := range []string{fixture.commits[rootID], rootID + ".txt", rootID + ".Boundary", "acceptance for " + rootID} {
				if !strings.Contains(prompt, want) {
					t.Fatalf("slice %s prompt missing bounded evidence %q", tc.planID, want)
				}
			}
			assertImmutableSnapshotRead(t, fixture, prompt, taskID, rootID+".txt", fixture.sourceSentinels[rootID])
		}
		unwanted := append([]string{}, tc.otherIDs...)
		unwanted = append(unwanted,
			tc.otherPlanID,
			"plan-single", "coding-single", "coding-single-leaf-a", "coding-single-leaf-z",
			fixture.sourceSentinels["coding-single-leaf-a"], fixture.sourceSentinels["coding-single-leaf-z"],
			fixture.aggregateSentinelPath, fixture.aggregateSentinel, "..HEAD", "show HEAD:",
		)
		for _, unwanted := range unwanted {
			if strings.Contains(prompt, unwanted) {
				t.Fatalf("slice %s prompt leaked %q", tc.planID, unwanted)
			}
		}
	}
}

func buildIntegrationPrompt(t *testing.T, fixture *slicedLifecycleFixture, taskID, agentID string) string {
	t.Helper()
	frozen, err := pipeline.LoadFrozen(fixture.root)
	if err != nil {
		t.Fatalf("LoadFrozen(): %v", err)
	}
	strategy, err := agent.NewRoleStrategy(roles.IntegrationAnalyst, pipeline.NewResolver(frozen))
	if err != nil {
		t.Fatalf("NewRoleStrategy(): %v", err)
	}
	prompt, err := strategy.BuildPrompt(fixture.read(t), agent.SupervisorConfig{
		Role: roles.IntegrationAnalyst, AgentID: agentID, ProjectRoot: fixture.root,
		SpecsDir: filepath.Join(fixture.root, "specs"), StatePath: fixture.statePath,
	}, taskID)
	if err != nil {
		t.Fatalf("BuildPrompt(%s): %v", taskID, err)
	}
	return prompt
}

func assertImmutableSnapshotRead(t *testing.T, fixture *slicedLifecycleFixture, prompt, taskID, path, sentinel string) {
	t.Helper()
	task := fixture.read(t).FindTask(taskID)
	if task == nil || task.Worktree == nil {
		t.Fatalf("snapshot task %s has no worktree", taskID)
	}
	worktree := filepath.Join(fixture.root, *task.Worktree)
	wantRead := "git -C '" + worktree + "' show '" + fixture.head + ":" + path + "'"
	if !strings.Contains(prompt, wantRead) {
		t.Fatalf("snapshot prompt missing immutable read %q", wantRead)
	}
	contents := testhelpers.MustGit(t, fixture.root, "show", fixture.head+":"+path)
	if contents != sentinel {
		t.Fatalf("immutable read %s:%s = %q, want source sentinel %q", fixture.head, path, contents, sentinel)
	}
}

func assertGlobalIntegrationPrompt(t *testing.T, fixture *slicedLifecycleFixture, taskID string) {
	t.Helper()
	const analystID = "integration-analyst-1"
	ensureTestAgent(t, fixture, analystID, roles.IntegrationAnalyst)
	if _, err := ops.ClaimTask(fixture.root, taskID, analystID); err != nil {
		t.Fatalf("ClaimTask(%s): %v", taskID, err)
	}
	prompt := buildIntegrationPrompt(t, fixture, taskID, analystID)
	state := fixture.read(t)
	task := state.FindTask(taskID)
	if task == nil || task.Worktree == nil || task.IntegrationAnalysis == nil {
		t.Fatalf("global prompt task = %#v", task)
	}
	source := task.IntegrationAnalysis.SourceCommit
	if source != fixture.integrationHead(t) {
		t.Fatalf("global prompt source = %s, want live integration HEAD", source)
	}
	for _, want := range []string{
		"GLOBAL INTEGRATION CONTEXT", "GENERATION: 1", "SOURCE COMMIT: " + source,
		"COVERAGE MAP", "navigation evidence, not proof of aggregate correctness", "independent aggregate review",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("global prompt missing %q", want)
		}
	}
	for _, record := range state.Goal.Integration.Coverage {
		for _, want := range []string{record.PlanTaskID, string(record.Kind)} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("global prompt missing coverage witness %q", want)
			}
		}
		for _, attestation := range record.ApprovalAttestations {
			for _, want := range []string{
				attestation.ReviewedTaskID, attestation.AcceptanceCriteria, attestation.ReviewedCommit,
				attestation.Approver, attestation.MergeCommit,
			} {
				if !strings.Contains(prompt, want) {
					t.Fatalf("global prompt missing approval witness %q", want)
				}
			}
			for _, validation := range attestation.Validation {
				if !strings.Contains(prompt, validation) {
					t.Fatalf("global prompt missing approval validation %q", validation)
				}
			}
		}
		if report := record.SliceReport; report != nil {
			for _, want := range []string{report.AnalysisTaskID, report.AnalysisKey, string(report.Verdict), report.SourceCommit, report.ReportCommit} {
				if !strings.Contains(prompt, want) {
					t.Fatalf("global prompt missing slice witness %q", want)
				}
			}
		}
	}
	worktree := filepath.Join(fixture.root, *task.Worktree)
	diffRange := fixture.goalBase + ".." + source
	for _, want := range []string{
		"git -C '" + worktree + "' diff --name-only '" + diffRange + "'",
		"git -C '" + worktree + "' diff --stat '" + diffRange + "'",
		"git -C '" + worktree + "' diff '" + diffRange + "' -- <path>",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("global prompt missing aggregate boundary %q", want)
		}
	}
	changedPaths := strings.Fields(testhelpers.MustGit(t, fixture.root, "diff", "--name-only", diffRange))
	if !slices.Contains(changedPaths, fixture.aggregateSentinelPath) {
		t.Fatalf("aggregate boundary paths = %v, missing %s", changedPaths, fixture.aggregateSentinelPath)
	}
	contents := testhelpers.MustGit(t, fixture.root, "show", source+":"+fixture.aggregateSentinelPath)
	if contents != fixture.aggregateSentinel {
		t.Fatalf("aggregate source sentinel = %q, want %q", contents, fixture.aggregateSentinel)
	}
	for _, localSentinel := range fixture.sourceSentinels {
		if strings.Contains(prompt, localSentinel) {
			t.Fatalf("global coverage map treated local source body %q as aggregate proof", localSentinel)
		}
	}
}

func assertExactlyOneCreatedTask(t *testing.T, results []*ops.ReconcileIntegrationAnalysesResult, taskID string) {
	t.Helper()
	created := 0
	for _, result := range results {
		for _, id := range result.CreatedTaskIDs {
			if id == taskID {
				created++
			}
		}
	}
	if created != 1 {
		t.Fatalf("concurrent reconciliation created %s %d times, want 1", taskID, created)
	}
}

func cloneSliceEvidence(state *models.State) sliceEvidenceProjection {
	projection := sliceEvidenceProjection{
		Coverage: slices.Clone(state.Goal.Integration.Coverage),
		Tasks:    make(map[string]models.Task),
	}
	for _, task := range state.Tasks {
		if task.IntegrationAnalysis != nil && task.IntegrationAnalysis.Phase == models.IntegrationAnalysisPhaseSlice {
			projection.Tasks[task.ID] = task
			if slices.Contains(state.Sprint.Scope.Planned, task.ID) {
				projection.Planned = append(projection.Planned, task.ID)
			}
		}
	}
	return projection
}

func addReplacementTask(t *testing.T, fixture *slicedLifecycleFixture, taskID string) {
	t.Helper()
	lp := paths.New(fixture.root)
	_, err := ops.AddTask(lp.StatePath(), lp.LogPath(), &ops.AddTaskInput{
		ID: taskID, Type: string(models.TaskTypeCoding), RolePair: "coding-pair",
		Description: "replace blocked slice repair", SpecRef: "README.md", DoneWhen: "replacement repair merged",
		Validation: []string{"project test"}, Scope: taskID + ".txt", Priority: 1,
	}, "orchestrator-1")
	if err != nil {
		t.Fatalf("AddTask(%s): %v", taskID, err)
	}
}

func addIntegrationRepairPlan(t *testing.T, fixture *slicedLifecycleFixture, taskID, parentFixID string) {
	t.Helper()
	lp := paths.New(fixture.root)
	if _, err := ops.AddTask(lp.StatePath(), lp.LogPath(), &ops.AddTaskInput{
		ID: taskID, Type: string(models.TaskTypePlanning), RolePair: "code-planning-pair",
		Description: "plan a non-trivial integration repair", SpecRef: "README.md", PlanRef: "README.md",
		DoneWhen: "repair plan produces reviewed coding descendants", Scope: taskID + ".md", Priority: 1,
	}, "orchestrator-1"); err != nil {
		t.Fatalf("AddTask(%s): %v", taskID, err)
	}
	fixture.modify(t, func(state *models.State) {
		plan := state.FindTask(taskID)
		plan.ParentTask = testhelpers.StringPtr(parentFixID)
	})
}

func completeIntegrationRepairPlan(t *testing.T, fixture *slicedLifecycleFixture, taskID string) []string {
	t.Helper()
	plannerID := ensureTestAgent(t, fixture, "code-planner-1", "code-planner")
	reviewerID := ensureTestAgent(t, fixture, "code-plan-reviewer-1", "code-plan-reviewer")
	if _, err := ops.ClaimTask(fixture.root, taskID, plannerID); err != nil {
		t.Fatalf("ClaimTask(%s): %v", taskID, err)
	}
	task := fixture.read(t).FindTask(taskID)
	if task == nil || task.Worktree == nil {
		t.Fatalf("claimed repair plan %s has no worktree", taskID)
	}
	worktree := filepath.Join(fixture.root, *task.Worktree)
	planFile := taskID + ".md"
	if err := os.WriteFile(filepath.Join(worktree, planFile), []byte("reviewed integration repair plan\n"), 0o644); err != nil {
		t.Fatalf("write repair plan: %v", err)
	}
	testhelpers.MustGit(t, worktree, "add", planFile)
	testhelpers.MustGit(t, worktree, "commit", "-m", "plan integration repair")
	output := []models.OutputEntry{
		{Desc: "implement first planned repair", DoneWhen: "first repair merged", Scope: "planned-repair-a.txt", SpecRef: "README.md", PlanRef: "README.md"},
		{Desc: "implement second planned repair", DoneWhen: "second repair merged", Scope: "planned-repair-b.txt", SpecRef: "README.md", PlanRef: "README.md"},
	}
	if err := ops.SetTaskOutput(fixture.root, &ops.SetTaskOutputInput{TaskID: taskID, AgentID: plannerID, Output: output}); err != nil {
		t.Fatalf("SetTaskOutput(%s): %v", taskID, err)
	}
	if err := ops.WriteCheckpoint(fixture.root, &ops.WriteCheckpointInput{
		TaskID: taskID, AgentID: plannerID, Intent: "plan non-trivial integration repair",
		ValidationPlan: "review plan and merge every coding descendant", FilesToModify: []string{planFile}, TDDNotRequired: "planning artifact only",
	}); err != nil {
		t.Fatalf("WriteCheckpoint(%s): %v", taskID, err)
	}
	reviewCommit := testhelpers.MustGit(t, worktree, "rev-parse", "HEAD")
	if _, err := ops.SubmitForReview(fixture.root, taskID, reviewCommit, plannerID); err != nil {
		t.Fatalf("SubmitForReview(%s): %v", taskID, err)
	}
	if _, err := ops.ClaimReviewerTask(ops.ClaimReviewerTaskInput{
		ProjectRoot: fixture.root, AgentID: reviewerID, Role: "code-plan-reviewer", TaskID: taskID,
	}); err != nil {
		t.Fatalf("ClaimReviewerTask(%s): %v", taskID, err)
	}
	if _, err := ops.SubmitVerdict(fixture.root, taskID, "APPROVED", "", reviewerID, ""); err != nil {
		t.Fatalf("SubmitVerdict(%s): %v", taskID, err)
	}
	if _, err := ops.MergeWorktree(fixture.root, taskID, reviewerID); err != nil {
		t.Fatalf("MergeWorktree(%s): %v", taskID, err)
	}
	transitions, err := ops.ExecuteAvailableTransitions(fixture.root, "manual")
	if err != nil || len(transitions) != 1 || transitions[0].TransitionName != "code-plan-to-coding" || len(transitions[0].ChildTaskIDs) != len(output) {
		t.Fatalf("code-planning escalation transition: results=%#v err=%v", transitions, err)
	}
	for i, childID := range transitions[0].ChildTaskIDs {
		mergeCodingTask(t, fixture, childID, output[i].Scope)
	}
	return transitions[0].ChildTaskIDs
}

func prepareCodingTaskReview(t *testing.T, fixture *slicedLifecycleFixture, taskID, fileName string) {
	t.Helper()
	coderID := ensureTestAgent(t, fixture, "coder-1", "coder")
	ensureTestAgent(t, fixture, "code-reviewer-1", "code-reviewer")
	if _, err := ops.ClaimTask(fixture.root, taskID, coderID); err != nil {
		t.Fatalf("ClaimTask(%s): %v", taskID, err)
	}
	state := fixture.read(t)
	task := state.FindTask(taskID)
	if task == nil || task.Worktree == nil {
		t.Fatalf("claimed coding task %s has no worktree", taskID)
	}
	worktree := filepath.Join(fixture.root, *task.Worktree)
	if err := os.WriteFile(filepath.Join(worktree, fileName), []byte(taskID+"\n"), 0o644); err != nil {
		t.Fatalf("write coding fixture: %v", err)
	}
	testFile := strings.TrimSuffix(fileName, filepath.Ext(fileName)) + "_test.go"
	if err := os.WriteFile(filepath.Join(worktree, testFile), []byte("package fixture\n"), 0o644); err != nil {
		t.Fatalf("write coding test fixture: %v", err)
	}
	testhelpers.MustGit(t, worktree, "add", fileName, testFile)
	testhelpers.MustGit(t, worktree, "commit", "-m", "repair integration")
	if err := ops.WriteCheckpoint(fixture.root, &ops.WriteCheckpointInput{
		TaskID: taskID, AgentID: coderID, Intent: "repair integration finding", ValidationPlan: "project validation", FilesToModify: []string{fileName, testFile},
	}); err != nil {
		t.Fatalf("WriteCheckpoint(%s): %v", taskID, err)
	}
	reviewCommit := testhelpers.MustGit(t, worktree, "rev-parse", "HEAD")
	if _, err := ops.SubmitForReview(fixture.root, taskID, reviewCommit, coderID); err != nil {
		t.Fatalf("SubmitForReview(%s): %v", taskID, err)
	}
	if _, err := ops.ClaimReviewerTask(ops.ClaimReviewerTaskInput{
		ProjectRoot: fixture.root, AgentID: "code-reviewer-1", Role: "code-reviewer", TaskID: taskID,
	}); err != nil {
		t.Fatalf("ClaimReviewerTask(%s): %v", taskID, err)
	}
}

func prepareApprovedMutation(t *testing.T, fixture *slicedLifecycleFixture, taskID string) (string, string) {
	t.Helper()
	lp := paths.New(fixture.root)
	fileName := taskID + ".md"
	if _, err := ops.AddTask(lp.StatePath(), lp.LogPath(), &ops.AddTaskInput{
		ID: taskID, Type: string(models.TaskTypeArchitecture), RolePair: "architecture-pair",
		Description: "approved integration mutation", SpecRef: "README.md", DoneWhen: "mutation merged",
		Scope: fileName, Priority: 1,
	}, "orchestrator-1"); err != nil {
		t.Fatalf("AddTask(%s): %v", taskID, err)
	}
	architectID := ensureTestAgent(t, fixture, "architect-1", "architect")
	reviewerID := ensureTestAgent(t, fixture, "architecture-reviewer-1", "architecture-reviewer")
	if _, err := ops.ClaimTask(fixture.root, taskID, architectID); err != nil {
		t.Fatalf("ClaimTask(%s): %v", taskID, err)
	}
	task := fixture.read(t).FindTask(taskID)
	if task == nil || task.Worktree == nil {
		t.Fatalf("claimed architecture task %s has no worktree", taskID)
	}
	worktree := filepath.Join(fixture.root, *task.Worktree)
	if err := os.WriteFile(filepath.Join(worktree, fileName), []byte(taskID+"\n"), 0o644); err != nil {
		t.Fatalf("write architecture fixture: %v", err)
	}
	testhelpers.MustGit(t, worktree, "add", fileName)
	testhelpers.MustGit(t, worktree, "commit", "-m", "record architecture mutation")
	if err := ops.WriteCheckpoint(fixture.root, &ops.WriteCheckpointInput{
		TaskID: taskID, AgentID: architectID, Intent: "record architecture mutation",
		ValidationPlan: "project validation", FilesToModify: []string{fileName},
	}); err != nil {
		t.Fatalf("WriteCheckpoint(%s): %v", taskID, err)
	}
	reviewCommit := testhelpers.MustGit(t, worktree, "rev-parse", "HEAD")
	if _, err := ops.SubmitForReview(fixture.root, taskID, reviewCommit, architectID); err != nil {
		t.Fatalf("SubmitForReview(%s): %v", taskID, err)
	}
	if _, err := ops.ClaimReviewerTask(ops.ClaimReviewerTaskInput{
		ProjectRoot: fixture.root, AgentID: reviewerID, Role: "architecture-reviewer", TaskID: taskID,
	}); err != nil {
		t.Fatalf("ClaimReviewerTask(%s): %v", taskID, err)
	}
	if _, err := ops.SubmitVerdict(fixture.root, taskID, "APPROVED", "", reviewerID, ""); err != nil {
		t.Fatalf("SubmitVerdict(%s): %v", taskID, err)
	}
	return taskID, reviewerID
}

func seedRealMutationReceipt(t *testing.T, fixture *slicedLifecycleFixture) {
	t.Helper()
	const taskID = "fixture-prior-mutation"
	taskIDOut, reviewerID := prepareApprovedMutation(t, fixture, taskID)
	before := fixture.integrationHead(t)
	if _, err := ops.MergeWorktree(fixture.root, taskIDOut, reviewerID); err != nil {
		t.Fatalf("MergeWorktree(real receipt prefix): %v", err)
	}
	after := fixture.integrationHead(t)
	assertRealMutationReceipt(t, fixture.read(t), taskID, before, after)
	fixture.head = after
}

func assertRealMutationReceipt(t *testing.T, state *models.State, taskID, before, after string) {
	t.Helper()
	if before == after {
		t.Fatalf("mutation %s did not advance integration HEAD", taskID)
	}
	count := 0
	for _, receipt := range state.Goal.Integration.MutationReceipts {
		if receipt.TaskID != taskID {
			continue
		}
		count++
		if receipt.BeforeCommit != before || receipt.AfterCommit != after {
			t.Fatalf("mutation receipt %s = %#v, want %s -> %s", taskID, receipt, before, after)
		}
	}
	if count != 1 {
		t.Fatalf("mutation receipt %s count = %d, want 1", taskID, count)
	}
}

func assertIntegrationIncomplete(t *testing.T, fixture *slicedLifecycleFixture, head string) {
	t.Helper()
	frozen, err := pipeline.LoadFrozen(fixture.root)
	if err != nil {
		t.Fatalf("LoadFrozen(): %v", err)
	}
	decision, err := ops.EvaluateIntegrationProgress(fixture.read(t), pipeline.NewResolver(frozen).SlicedIntegrationCapability(), head)
	if err != nil {
		t.Fatalf("EvaluateIntegrationProgress(): %v", err)
	}
	if decision.IntegrationComplete {
		t.Fatalf("fresh progress reports stale integration complete at %s", head)
	}
}

func installMergeBarrier(t *testing.T, root string) *mergeBarrier {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for merge barrier: %v", err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	t.Setenv("LIZA_TEST_SLICED_BARRIER_ADDR", listener.Addr().String())
	t.Setenv("LIZA_TEST_SLICED_BARRIER_HELPER", executable)
	scriptsDir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("create integration scripts dir: %v", err)
	}
	script := "#!/bin/sh\nexec \"$LIZA_TEST_SLICED_BARRIER_HELPER\" -test.run '^TestSlicedIntegrationBarrierHelper$'\n"
	if err := os.WriteFile(filepath.Join(scriptsDir, "integration-test.sh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write integration merge barrier: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return &mergeBarrier{listener: listener}
}

func (barrier *mergeBarrier) wait(t *testing.T) {
	t.Helper()
	if tcp, ok := barrier.listener.(*net.TCPListener); ok {
		if err := tcp.SetDeadline(time.Now().Add(slicedIntegrationTimeout)); err != nil {
			t.Fatalf("set merge barrier deadline: %v", err)
		}
	}
	connection, err := barrier.listener.Accept()
	if err != nil {
		t.Fatalf("await merge barrier: %v", err)
	}
	buffer := []byte{0}
	if _, err := connection.Read(buffer); err != nil {
		t.Fatalf("read merge barrier signal: %v", err)
	}
	barrier.conn = connection
}

func (barrier *mergeBarrier) release(t *testing.T) {
	t.Helper()
	if barrier.conn == nil {
		t.Fatal("merge barrier released before it was reached")
	}
	if _, err := barrier.conn.Write([]byte{1}); err != nil {
		t.Fatalf("release merge barrier: %v", err)
	}
	if err := barrier.conn.Close(); err != nil {
		t.Fatalf("close merge barrier: %v", err)
	}
}

func holdProjectLock(t *testing.T, root, purpose string) func() {
	t.Helper()
	return holdExternalLock(t, projectLockProtectedPath(root, purpose))
}

func holdBlackboardWriteLock(t *testing.T, fixture *slicedLifecycleFixture) func() {
	t.Helper()
	return holdExternalLock(t, fixture.statePath)
}

func holdExternalLock(t *testing.T, protectedPath string) func() {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for external lock barrier: %v", err)
	}
	if tcp, ok := listener.(*net.TCPListener); ok {
		if err := tcp.SetDeadline(time.Now().Add(slicedIntegrationTimeout)); err != nil {
			t.Fatalf("set external lock barrier deadline: %v", err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	command := exec.Command(executable, "-test.run", "^TestSlicedIntegrationBarrierHelper$")
	command.Env = append(os.Environ(),
		"LIZA_TEST_SLICED_BARRIER_ADDR="+listener.Addr().String(),
		"LIZA_TEST_SLICED_LOCK_PATH="+protectedPath,
	)
	if err := command.Start(); err != nil {
		t.Fatalf("start external lock helper: %v", err)
	}
	connection, err := listener.Accept()
	if err != nil {
		t.Fatalf("await external lock helper: %v", err)
	}
	buffer := []byte{0}
	if _, err := connection.Read(buffer); err != nil {
		t.Fatalf("read external lock signal: %v", err)
	}
	return func() {
		if _, err := connection.Write([]byte{1}); err != nil {
			t.Fatalf("release external lock helper: %v", err)
		}
		if err := connection.Close(); err != nil {
			t.Fatalf("close external lock connection: %v", err)
		}
		if err := command.Wait(); err != nil {
			t.Fatalf("external lock helper: %v", err)
		}
		_ = listener.Close()
	}
}

func watchProjectLockOwner(t *testing.T, root, purpose string) (*fsnotify.Watcher, string) {
	t.Helper()
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("create lock owner watcher: %v", err)
	}
	gitDir := filepath.Join(root, ".git")
	if err := watcher.Add(gitDir); err != nil {
		t.Fatalf("watch Git lock directory: %v", err)
	}
	t.Cleanup(func() { _ = watcher.Close() })
	return watcher, projectLockProtectedPath(root, purpose) + ".lock.owner.json"
}

func projectLockProtectedPath(root, purpose string) string {
	lockName := strings.TrimPrefix(paths.ProjectDirName(), ".") + "-" + purpose
	return filepath.Join(root, ".git", lockName)
}

func waitForLockOperation(t *testing.T, watcher *fsnotify.Watcher, ownerPath, operation string) {
	t.Helper()
	type ownerMetadata struct {
		Operation string `json:"operation"`
	}
	timer := time.NewTimer(slicedIntegrationTimeout)
	defer timer.Stop()
	for {
		select {
		case event := <-watcher.Events:
			if event.Name != ownerPath {
				continue
			}
			data, err := os.ReadFile(ownerPath)
			if err != nil {
				continue
			}
			var owner ownerMetadata
			if json.Unmarshal(data, &owner) == nil && owner.Operation == operation {
				return
			}
		case err := <-watcher.Errors:
			t.Fatalf("watch lock owner: %v", err)
		case <-timer.C:
			t.Fatalf("timed out waiting for %q lock owner", operation)
		}
	}
}

func waitForLockOperationOrResult(t *testing.T, watcher *fsnotify.Watcher, ownerPath, operation string, result <-chan error) {
	t.Helper()
	type ownerMetadata struct {
		Operation string `json:"operation"`
	}
	timer := time.NewTimer(slicedIntegrationTimeout)
	defer timer.Stop()
	for {
		select {
		case event := <-watcher.Events:
			if event.Name != ownerPath {
				continue
			}
			data, err := os.ReadFile(ownerPath)
			if err != nil {
				continue
			}
			var owner ownerMetadata
			if json.Unmarshal(data, &owner) == nil && owner.Operation == operation {
				return
			}
		case err := <-result:
			t.Fatalf("%s completed before overlap barrier: %v", operation, err)
		case err := <-watcher.Errors:
			t.Fatalf("watch lock owner: %v", err)
		case <-timer.C:
			t.Fatalf("timed out waiting for %q lock owner", operation)
		}
	}
}

func completeIntegrationAnalysis(t *testing.T, fixture *slicedLifecycleFixture, taskID string, output []models.OutputEntry) {
	t.Helper()
	prepareIntegrationAnalysisReview(t, fixture, taskID, output)
	if _, err := ops.SubmitVerdict(fixture.root, taskID, "APPROVED", "", "integration-reviewer-1", ""); err != nil {
		t.Fatalf("SubmitVerdict(%s): %v", taskID, err)
	}
	state := fixture.read(t)
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatalf("analysis %s missing after verdict", taskID)
	}
	if len(output) > 0 {
		if _, err := ops.MergeWorktree(fixture.root, taskID, "integration-reviewer-1"); err != nil {
			t.Fatalf("MergeWorktree(%s): %v", taskID, err)
		}
	}
}

func prepareIntegrationAnalysisReview(t *testing.T, fixture *slicedLifecycleFixture, taskID string, output []models.OutputEntry) {
	t.Helper()
	analystID := ensureTestAgent(t, fixture, "integration-analyst-1", "integration-analyst")
	ensureTestAgent(t, fixture, "integration-reviewer-1", "integration-reviewer")
	if task := fixture.read(t).FindTask(taskID); task != nil && task.AssignedTo == nil {
		if _, err := ops.ClaimTask(fixture.root, taskID, analystID); err != nil {
			t.Fatalf("ClaimTask(%s): %v", taskID, err)
		}
	}
	if output != nil {
		if err := ops.SetTaskOutput(fixture.root, &ops.SetTaskOutputInput{TaskID: taskID, AgentID: analystID, Output: output}); err != nil {
			t.Fatalf("SetTaskOutput(%s): %v", taskID, err)
		}
	}
	if err := ops.WriteCheckpoint(fixture.root, &ops.WriteCheckpointInput{
		TaskID: taskID, AgentID: analystID, Intent: "analyze immutable integration source", ValidationPlan: "review persisted lifecycle evidence", TDDNotRequired: "read-only integration analysis",
	}); err != nil {
		t.Fatalf("WriteCheckpoint(%s): %v", taskID, err)
	}
	state := fixture.read(t)
	task := state.FindTask(taskID)
	if task == nil || task.Worktree == nil {
		t.Fatalf("claimed analysis %s has no worktree", taskID)
	}
	reviewCommit := testhelpers.MustGit(t, filepath.Join(fixture.root, *task.Worktree), "rev-parse", "HEAD")
	if _, err := ops.SubmitForReview(fixture.root, taskID, reviewCommit, analystID); err != nil {
		t.Fatalf("SubmitForReview(%s): %v", taskID, err)
	}
	if _, err := ops.ClaimReviewerTask(ops.ClaimReviewerTaskInput{
		ProjectRoot: fixture.root, AgentID: "integration-reviewer-1", Role: "integration-reviewer", TaskID: taskID,
	}); err != nil {
		t.Fatalf("ClaimReviewerTask(%s): %v", taskID, err)
	}
}

func ensureTestAgent(t *testing.T, fixture *slicedLifecycleFixture, id, role string) string {
	t.Helper()
	if _, ok := fixture.read(t).Agents[id]; !ok {
		testhelpers.RegisterTestAgent(t, db.For(fixture.statePath), id, role)
	}
	return id
}

func mergeCodingTask(t *testing.T, fixture *slicedLifecycleFixture, taskID, fileName string) {
	t.Helper()
	prepareCodingTaskReview(t, fixture, taskID, fileName)
	if _, err := ops.SubmitVerdict(fixture.root, taskID, "APPROVED", "", "code-reviewer-1", ""); err != nil {
		t.Fatalf("SubmitVerdict(%s): %v", taskID, err)
	}
	if _, err := ops.MergeWorktree(fixture.root, taskID, "code-reviewer-1"); err != nil {
		t.Fatalf("MergeWorktree(%s): %v", taskID, err)
	}
}

func newCleanCompletionFixture(t *testing.T) *slicedLifecycleFixture {
	t.Helper()
	fixture := newSlicedLifecycleFixture(t, false)
	seedRealMutationReceipt(t, fixture)
	reconcileSlicedLifecycle(t, fixture.root)
	prepareIntegrationAnalysisReview(t, fixture, "integration-global-1", nil)
	if _, err := ops.SubmitVerdict(fixture.root, "integration-global-1", "APPROVED", "", "integration-reviewer-1", ""); err != nil {
		t.Fatalf("clean global verdict: %v", err)
	}
	return fixture
}

func newPendingGlobalFinalizationFixture(t *testing.T) *slicedLifecycleFixture {
	t.Helper()
	fixture := newSlicedLifecycleFixture(t, false)
	seedRealMutationReceipt(t, fixture)
	reconcileSlicedLifecycle(t, fixture.root)
	prepareIntegrationAnalysisReview(t, fixture, "integration-global-1", nil)
	return fixture
}

func markPlanningTransitionsConsumed(t *testing.T, fixture *slicedLifecycleFixture) {
	t.Helper()
	fixture.modify(t, func(state *models.State) {
		for i := range state.Tasks {
			if state.Tasks[i].RolePair == "code-planning-pair" {
				state.Tasks[i].Output = nil
				state.Tasks[i].TransitionsExecuted = map[string]bool{
					"code-plan-decompose": true,
					"code-plan-to-coding": true,
				}
			}
		}
	})
	for _, id := range []string{"plan-a", "plan-b", "plan-single"} {
		if task := fixture.read(t).FindTask(id); task != nil && !task.TransitionsExecuted["code-plan-to-coding"] {
			t.Fatalf("planning transition for %s was not persisted: %#v", id, task.TransitionsExecuted)
		}
	}
}

func freezeLegacyIntegrationPipeline(t *testing.T, root string) {
	t.Helper()
	config, err := pipeline.LoadFrozen(root)
	if err != nil {
		t.Fatalf("load frozen pipeline: %v", err)
	}
	delete(config.Pipeline.RolePairs, "slice-integration-pair")
	integration := config.Pipeline.SubPipelines["integration-subpipeline"]
	integration.Steps = slices.DeleteFunc(integration.Steps, func(step string) bool { return step == "slice-integration-pair" })
	integration.Transitions = slices.DeleteFunc(integration.Transitions, func(transition pipeline.TransitionDef) bool {
		return transition.Name == "slice-integration-to-fix"
	})
	config.Pipeline.SubPipelines["integration-subpipeline"] = integration
	data, err := yaml.Marshal(config)
	if err != nil {
		t.Fatalf("marshal legacy pipeline: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".liza", "pipeline.yaml"), data, 0o644); err != nil {
		t.Fatalf("write legacy pipeline: %v", err)
	}
}

func receiveError(t *testing.T, operation string, result <-chan error) {
	t.Helper()
	if err := receiveOperationResult(t, operation, result); err != nil {
		t.Fatalf("%s error: %v", operation, err)
	}
}

func receiveOperationResult(t *testing.T, operation string, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(slicedIntegrationTimeout):
		t.Fatalf("timed out waiting for %s", operation)
		return nil
	}
}

func assertTaskIDs(t *testing.T, got []string, want ...string) {
	t.Helper()
	got = slices.Clone(got)
	want = slices.Clone(want)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("created task IDs = %v, want %v", got, want)
	}
}

func cloneLifecycleProjection(state *models.State) lifecycleProjection {
	tasks := make(map[string]models.Task)
	for _, task := range state.Tasks {
		if task.IntegrationAnalysis != nil {
			tasks[task.ID] = task
		}
	}
	return lifecycleProjection{Lifecycle: state.Goal.Integration, Tasks: tasks, Planned: slices.Clone(state.Sprint.Scope.Planned)}
}

func countSlicedTasks(state *models.State, phase models.IntegrationAnalysisPhase) int {
	count := 0
	for i := range state.Tasks {
		if state.Tasks[i].IntegrationAnalysis != nil && state.Tasks[i].IntegrationAnalysis.Phase == phase {
			count++
		}
	}
	return count
}

func countTaskID(state *models.State, id string) int {
	count := 0
	for i := range state.Tasks {
		if state.Tasks[i].ID == id {
			count++
		}
	}
	return count
}

func countString(values []string, want string) int {
	count := 0
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
}

func taskIDsForSlicedFixture(tasks []models.Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}
