package integration

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

const slicedIntegrationTimeout = 10 * time.Second

func TestSlicedIntegrationLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	t.Run("settled boundary and zero-slice bypass", func(t *testing.T) {
		fixture := newSlicedLifecycleFixture(t, true)
		fixture.modify(t, func(state *models.State) {
			state.FindTask("plan-a").TransitionsExecuted = nil
		})

		result := reconcileSlicedLifecycle(t, fixture.root)
		if result.Changed || len(result.CreatedTaskIDs) != 0 {
			t.Fatalf("unsettled reconciliation = %#v, want no-op", result)
		}
		state := fixture.read(t)
		if state.Goal.Integration != nil {
			t.Fatalf("unsettled planning opened integration lifecycle: %#v", state.Goal.Integration)
		}

		fixture.modify(t, func(state *models.State) {
			state.FindTask("plan-a").TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
		})
		result = reconcileSlicedLifecycle(t, fixture.root)
		assertTaskIDs(t, result.CreatedTaskIDs, "integration-slice-plan-a", "integration-slice-plan-b")
		assertMixedCoverage(t, fixture.read(t), fixture.head)

		single := newSlicedLifecycleFixture(t, false)
		result = reconcileSlicedLifecycle(t, single.root)
		assertTaskIDs(t, result.CreatedTaskIDs, "integration-global-1")
		state = single.read(t)
		if state.Goal.Integration == nil || state.Goal.Integration.ContributingSet == nil || len(state.Goal.Integration.ContributingSet.Scopes) != 1 {
			t.Fatalf("single-scope contributing set = %#v", state.Goal.Integration)
		}
		if len(state.Goal.Integration.Coverage) != 0 || countSlicedTasks(state, models.IntegrationAnalysisPhaseSlice) != 0 {
			t.Fatalf("single-scope bypass created local coverage: lifecycle=%#v tasks=%#v", state.Goal.Integration, state.Tasks)
		}
		global := state.FindTask("integration-global-1")
		if global == nil || global.IntegrationAnalysis == nil || global.IntegrationAnalysis.SourceCommit != single.head {
			t.Fatalf("zero-slice global analysis = %#v, want source %s", global, single.head)
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
	})

	t.Run("blocked slice fan-in", func(t *testing.T) {
		fixture := newSlicedLifecycleFixture(t, true)
		reconcileSlicedLifecycle(t, fixture.root)
		completeIntegrationAnalysis(t, fixture, "integration-slice-plan-a", nil)
		if result := reconcileSlicedLifecycle(t, fixture.root); len(result.CreatedTaskIDs) != 0 {
			t.Fatalf("one clean slice opened global analysis: %#v", result)
		}

		analystID := ensureTestAgent(t, fixture, "integration-analyst-1", "integration-analyst")
		if _, err := ops.ClaimTask(fixture.root, "integration-slice-plan-b", analystID); err != nil {
			t.Fatalf("claim blocked slice: %v", err)
		}
		if _, err := ops.MarkBlocked(fixture.root, "integration-slice-plan-b", "slice repair lineage exhausted", []string{"How should the slice be repaired?"}, analystID); err != nil {
			t.Fatalf("MarkBlocked(slice): %v", err)
		}
		result := reconcileSlicedLifecycle(t, fixture.root)
		state := fixture.read(t)
		if result.Reason == nil || state.Goal.Integration.Closure == nil || state.Goal.Integration.Closure.Status != models.IntegrationClosureStatusBlocked {
			t.Fatalf("blocked slice fan-in result=%#v closure=%#v", result, state.Goal.Integration.Closure)
		}
		if state.FindTask("integration-global-1") != nil {
			t.Fatal("blocked slice fan-in created global analysis")
		}
	})

	t.Run("global fix rescan restart and generation exhaustion", func(t *testing.T) {
		fixture := newSlicedLifecycleFixture(t, true)
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
				taskID, reviewerID := installApprovedMutation(t, fixture, "post-clean-mutation")
				if _, err := ops.MergeWorktree(fixture.root, taskID, reviewerID); err != nil {
					t.Fatalf("MergeWorktree(): %v", err)
				}
				live := fixture.integrationHead(t)
				if live == source {
					t.Fatal("public mutation did not advance integration HEAD")
				}
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
		taskID, reviewerID := installApprovedMutation(t, fixture, "mutation-before-finalization")

		mergeStart := make(chan struct{})
		mergeDone := make(chan error, 1)
		verdictStart := make(chan struct{})
		verdictDone := make(chan error, 1)
		go func() {
			<-mergeStart
			_, err := ops.MergeWorktree(fixture.root, taskID, reviewerID)
			mergeDone <- err
		}()
		go func() {
			<-verdictStart
			_, err := ops.SubmitVerdict(fixture.root, "integration-global-1", "APPROVED", "", "integration-reviewer-1", "")
			verdictDone <- err
		}()

		close(mergeStart)
		receiveError(t, "mutation", mergeDone)
		live := fixture.integrationHead(t)
		if live == source {
			t.Fatal("mutation-before-finalization did not advance integration HEAD")
		}
		close(verdictStart)
		receiveError(t, "finalization", verdictDone)

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
	})

	t.Run("mutation after finalization", func(t *testing.T) {
		fixture := newPendingGlobalFinalizationFixture(t)
		source := fixture.integrationHead(t)
		verdictStart := make(chan struct{})
		verdictDone := make(chan error, 1)
		go func() {
			<-verdictStart
			_, err := ops.SubmitVerdict(fixture.root, "integration-global-1", "APPROVED", "", "integration-reviewer-1", "")
			verdictDone <- err
		}()
		close(verdictStart)
		receiveError(t, "finalization", verdictDone)
		if _, err := ops.StopForGoalCompletion(fixture.root, "goal complete"); err != nil {
			t.Fatalf("StopForGoalCompletion(): %v", err)
		}

		taskID, reviewerID := installApprovedMutation(t, fixture, "mutation-after-finalization")
		mergeStart := make(chan struct{})
		mergeDone := make(chan error, 1)
		go func() {
			<-mergeStart
			_, err := ops.MergeWorktree(fixture.root, taskID, reviewerID)
			mergeDone <- err
		}()
		close(mergeStart)
		receiveError(t, "mutation", mergeDone)

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
		reconcileSlicedLifecycle(t, fixture.root)
		next := fixture.read(t).FindTask("integration-global-2")
		if next == nil || next.IntegrationAnalysis == nil || next.IntegrationAnalysis.SourceCommit != live {
			t.Fatalf("post-finalization mutation next generation = %#v, want source %s", next, live)
		}
	})
}

type slicedLifecycleFixture struct {
	root      string
	statePath string
	head      string
	commits   map[string]string
}

type lifecycleProjection struct {
	Lifecycle *models.IntegrationLifecycle
	Tasks     map[string]models.Task
	Planned   []string
}

func newSlicedLifecycleFixture(t *testing.T, multipleScopes bool) *slicedLifecycleFixture {
	t.Helper()
	db.ResetInstances()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	base := testhelpers.MustGit(t, root, "rev-parse", "HEAD")

	ids := []string{"coding-single"}
	if multipleScopes {
		ids = []string{"coding-a-1", "coding-a-2", "coding-b-1", "coding-b-2", "coding-single"}
	}
	commits := make(map[string]string, len(ids))
	bases := make(map[string]string, len(ids))
	previous := base
	for _, id := range ids {
		bases[id] = previous
		path := id + ".txt"
		if err := os.WriteFile(filepath.Join(root, path), []byte(id+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		testhelpers.MustGit(t, root, "add", path)
		testhelpers.MustGit(t, root, "commit", "-m", "add "+id)
		previous = testhelpers.MustGit(t, root, "rev-parse", "HEAD")
		commits[id] = previous
	}
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
	tasks = append(tasks, mergedPlanTask(now, "plan-single"), mergedCodingTask(now, "coding-single", "plan-single", bases["coding-single"], commits["coding-single"]))

	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	state.Goal.Integration = nil
	state.Tasks = tasks
	state.Sprint.Scope.Planned = taskIDsForSlicedFixture(tasks)
	testhelpers.WriteInitialState(t, statePath, state)
	db.ResetInstances()
	return &slicedLifecycleFixture{root: root, statePath: statePath, head: previous, commits: commits}
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
	if attestation.PlanTaskID != "plan-single" || attestation.Kind != models.IntegrationCoverageApprovalAttestation || len(attestation.ApprovalAttestations) != 1 || attestation.ApprovalAttestations[0].ReviewedTaskID != "coding-single" {
		t.Fatalf("approval attestation = %#v", attestation)
	}
	for _, planID := range []string{"plan-a", "plan-b"} {
		task := state.FindTask("integration-slice-" + planID)
		if task == nil || task.IntegrationAnalysis == nil {
			t.Fatalf("slice %s missing", planID)
		}
		metadata := task.IntegrationAnalysis
		if metadata.Key != "slice:"+planID || metadata.Phase != models.IntegrationAnalysisPhaseSlice || metadata.OriginatingPlanTaskID != planID || metadata.SourceCommit != sourceCommit || len(metadata.RootTaskIDs) != 2 || len(metadata.DescendantChanges) != 2 || len(metadata.AffectedPaths) != 2 || len(metadata.SourceSnapshotPaths) != 2 {
			t.Fatalf("slice %s metadata = %#v", planID, metadata)
		}
	}
	if state.FindTask("integration-slice-plan-single") != nil {
		t.Fatal("attestation-only scope received a slice")
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
	if _, err := ops.ClaimTask(fixture.root, taskID, analystID); err != nil {
		t.Fatalf("ClaimTask(%s): %v", taskID, err)
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
	if _, err := ops.SubmitVerdict(fixture.root, taskID, "APPROVED", "", "code-reviewer-1", ""); err != nil {
		t.Fatalf("SubmitVerdict(%s): %v", taskID, err)
	}
	ensureMutationReceiptPrefix(t, fixture)
	if _, err := ops.MergeWorktree(fixture.root, taskID, "code-reviewer-1"); err != nil {
		t.Fatalf("MergeWorktree(%s): %v", taskID, err)
	}
}

func newCleanCompletionFixture(t *testing.T) *slicedLifecycleFixture {
	t.Helper()
	fixture := newSlicedLifecycleFixture(t, false)
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
	reconcileSlicedLifecycle(t, fixture.root)
	prepareIntegrationAnalysisReview(t, fixture, "integration-global-1", nil)
	return fixture
}

func installApprovedMutation(t *testing.T, fixture *slicedLifecycleFixture, taskID string) (string, string) {
	t.Helper()
	const reviewerID = "code-reviewer-1"
	worktree := filepath.Join(fixture.root, ".worktrees", taskID)
	if err := os.MkdirAll(filepath.Dir(worktree), 0o755); err != nil {
		t.Fatalf("create worktree parent: %v", err)
	}
	testhelpers.MustGit(t, fixture.root, "worktree", "add", "-b", "task/"+taskID, worktree, "integration")
	fileName := taskID + ".txt"
	if err := os.WriteFile(filepath.Join(worktree, fileName), []byte(taskID+"\n"), 0o644); err != nil {
		t.Fatalf("write mutation: %v", err)
	}
	testhelpers.MustGit(t, worktree, "add", fileName)
	testhelpers.MustGit(t, worktree, "commit", "-m", "mutate integration after clean analysis")
	base := fixture.integrationHead(t)
	review := testhelpers.MustGit(t, worktree, "rev-parse", "HEAD")
	relative := filepath.Join(".worktrees", taskID)
	fixture.modify(t, func(state *models.State) {
		now := time.Now().UTC()
		if state.Goal.Integration != nil && len(state.Goal.Integration.MutationReceipts) == 0 {
			state.Goal.Integration.MutationReceipts = []models.IntegrationMutationReceipt{{
				TaskID: "fixture-prior-mutation", BeforeCommit: "fixture-before", AfterCommit: "fixture-after",
			}}
		}
		state.Tasks = append(state.Tasks, models.Task{
			ID: taskID, Type: models.TaskTypeCoding, RolePair: "coding-pair", Description: "approved integration mutation",
			Status: models.TaskStatusApproved, Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "mutation merged", Scope: fileName,
			ParentTask: testhelpers.StringPtr("integration-global-1"), Worktree: &relative, AssignedTo: testhelpers.StringPtr("coder-1"),
			BaseCommit: &base, ReviewCommit: &review, ApprovedBy: testhelpers.StringPtr(reviewerID), History: []models.TaskHistoryEntry{},
			HandoffEvents: []models.HandoffEvent{{Timestamp: now, Agent: "coder-1", Trigger: models.HandoffTriggerSubmission}},
		})
	})
	return taskID, reviewerID
}

func ensureMutationReceiptPrefix(t *testing.T, fixture *slicedLifecycleFixture) {
	t.Helper()
	fixture.modify(t, func(state *models.State) {
		if state.Goal.Integration != nil && len(state.Goal.Integration.MutationReceipts) == 0 {
			state.Goal.Integration.MutationReceipts = []models.IntegrationMutationReceipt{{
				TaskID: "fixture-prior-mutation", BeforeCommit: "fixture-before", AfterCommit: "fixture-after",
			}}
		}
	})
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
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("%s error: %v", operation, err)
		}
	case <-time.After(slicedIntegrationTimeout):
		t.Fatalf("timed out waiting for %s", operation)
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
