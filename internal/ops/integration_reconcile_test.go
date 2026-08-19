package ops

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestReconcileIntegrationAnalyses(t *testing.T) {
	t.Run("settlement atomically freezes cohort attestations tasks and planned membership", func(t *testing.T) {
		fixture := newReconcileFixture(t, false)
		before := fixture.readState(t)

		result, err := ReconcileIntegrationAnalyses(fixture.projectRoot)
		if err != nil {
			t.Fatalf("ReconcileIntegrationAnalyses() error = %v", err)
		}
		if !result.Changed || !reflect.DeepEqual(result.CreatedTaskIDs, []string{"integration-slice-plan-multi"}) || result.Reason != nil {
			t.Fatalf("result = %#v", result)
		}

		after := fixture.readState(t)
		wantCohort := &models.IntegrationContributingSet{Scopes: []models.IntegrationScopeSnapshot{
			{PlanTaskID: "plan-multi", RootTaskIDs: []string{"coding-a", "coding-b"}},
			{PlanTaskID: "plan-single", RootTaskIDs: []string{"coding-single"}},
		}}
		if !reflect.DeepEqual(after.Goal.Integration.ContributingSet, wantCohort) {
			t.Fatalf("cohort = %#v, want %#v", after.Goal.Integration.ContributingSet, wantCohort)
		}
		if len(after.Goal.Integration.Coverage) != 1 {
			t.Fatalf("coverage = %#v, want one attestation record", after.Goal.Integration.Coverage)
		}
		attestations := after.Goal.Integration.Coverage[0].ApprovalAttestations
		if after.Goal.Integration.Coverage[0].PlanTaskID != "plan-single" || len(attestations) != 1 || attestations[0].ReviewedTaskID != "coding-single" {
			t.Fatalf("approval coverage = %#v", after.Goal.Integration.Coverage[0])
		}

		task := after.FindTask("integration-slice-plan-multi")
		if task == nil || task.Type != models.TaskTypeIntegration || task.RolePair != "slice-integration-pair" || task.Priority != 1 {
			t.Fatalf("slice task = %#v", task)
		}
		if !reflect.DeepEqual(task.ParentTasks, []string{"coding-a", "coding-b"}) || len(task.DependsOn) != 0 {
			t.Fatalf("slice provenance = parents %v depends_on %v", task.ParentTasks, task.DependsOn)
		}
		metadata := task.IntegrationAnalysis
		wantChanges := []models.IntegrationDescendantChange{
			{TaskID: "coding-a", Commit: fixture.commits["coding-a"]},
			{TaskID: "coding-b", Commit: fixture.commits["coding-b"]},
		}
		if metadata == nil || metadata.Key != "slice:plan-multi" || metadata.Phase != models.IntegrationAnalysisPhaseSlice || metadata.SourceCommit != fixture.head ||
			!reflect.DeepEqual(metadata.RootTaskIDs, []string{"coding-a", "coding-b"}) ||
			!reflect.DeepEqual(metadata.DescendantChanges, wantChanges) ||
			!reflect.DeepEqual(metadata.AffectedPaths, []string{"a.go", "b.go", "deleted.txt"}) ||
			!reflect.DeepEqual(metadata.SourceSnapshotPaths, []string{"a.go", "b.go"}) {
			t.Fatalf("slice metadata = %#v", metadata)
		}
		if task.SpecRef != "README.md" || task.PlanRef != "README.md" || task.ArchRef != "README.md" {
			t.Fatalf("slice refs = spec %q plan %q arch %q", task.SpecRef, task.PlanRef, task.ArchRef)
		}
		if countStrings(after.Sprint.Scope.Planned, task.ID) != 1 || len(after.Tasks) != len(before.Tasks)+1 {
			t.Fatalf("task/planned registration = tasks %d planned %v", len(after.Tasks), after.Sprint.Scope.Planned)
		}

		second, err := ReconcileIntegrationAnalyses(fixture.projectRoot)
		if err != nil {
			t.Fatalf("repeated ReconcileIntegrationAnalyses() error = %v", err)
		}
		if second.Changed || len(second.CreatedTaskIDs) != 0 {
			t.Fatalf("repeated result = %#v, want no-op", second)
		}
		restarted, err := db.New(fixture.statePath).Read()
		if err != nil {
			t.Fatalf("restart read error = %v", err)
		}
		if countTasks(restarted, task.ID) != 1 || countStrings(restarted.Sprint.Scope.Planned, task.ID) != 1 {
			t.Fatalf("restart state duplicated task/planned membership")
		}
	})

	t.Run("task-order permutation and concurrent callers converge", func(t *testing.T) {
		ordered := newReconcileFixtureAt(t, false, "2001-02-03T04:05:06Z")
		permuted := newReconcileFixtureAt(t, true, "2001-02-03T04:05:07Z")
		orderedTimestamp := testhelpers.MustGit(t, ordered.projectRoot, "show", "-s", "--format=%at", ordered.head)
		permutedTimestamp := testhelpers.MustGit(t, permuted.projectRoot, "show", "-s", "--format=%at", permuted.head)
		if orderedTimestamp == permutedTimestamp {
			t.Fatalf("fixture commit timestamps = %s, want different seconds", orderedTimestamp)
		}
		for _, fixture := range []*reconcileFixture{ordered, permuted} {
			if _, err := ReconcileIntegrationAnalyses(fixture.projectRoot); err != nil {
				t.Fatalf("ReconcileIntegrationAnalyses(%s) error = %v", filepath.Base(fixture.projectRoot), err)
			}
		}
		orderedTask := ordered.readState(t).FindTask("integration-slice-plan-multi")
		permutedTask := permuted.readState(t).FindTask("integration-slice-plan-multi")
		orderedProjection := semanticReconcileProjection(t, ordered, orderedTask)
		permutedProjection := semanticReconcileProjection(t, permuted, permutedTask)
		if !reflect.DeepEqual(orderedProjection, permutedProjection) {
			t.Fatalf("task order changed projection:\nordered: %#v\npermuted: %#v", orderedProjection, permutedProjection)
		}

		concurrent := newReconcileFixture(t, false)
		const callers = 8
		results := make(chan *ReconcileIntegrationAnalysesResult, callers)
		errs := make(chan error, callers)
		var wg sync.WaitGroup
		for range callers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				result, err := ReconcileIntegrationAnalyses(concurrent.projectRoot)
				results <- result
				errs <- err
			}()
		}
		wg.Wait()
		close(results)
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("concurrent ReconcileIntegrationAnalyses() error = %v", err)
			}
		}
		created := 0
		for result := range results {
			if result.Changed {
				created++
			}
		}
		state := concurrent.readState(t)
		if created != 1 || countTasks(state, "integration-slice-plan-multi") != 1 || countStrings(state.Sprint.Scope.Planned, "integration-slice-plan-multi") != 1 {
			t.Fatalf("concurrent projection created=%d tasks=%d planned=%d", created, countTasks(state, "integration-slice-plan-multi"), countStrings(state.Sprint.Scope.Planned, "integration-slice-plan-multi"))
		}
	})

	t.Run("candidate and transition validation roll back the whole transaction", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			prepare   func(*testing.T, *reconcileFixture)
			corrupt   func(*models.State)
			wantError string
		}{
			{
				name: "malformed new analysis metadata",
				corrupt: func(state *models.State) {
					state.FindTask("integration-slice-plan-multi").IntegrationAnalysis.SourceCommit = ""
				},
				wantError: "source commit is empty",
			},
			{
				name: "malformed new task state",
				corrupt: func(state *models.State) {
					state.FindTask("integration-slice-plan-multi").Status = models.TaskStatus("BROKEN")
				},
				wantError: "unknown task status",
			},
			{
				name: "frozen cohort replacement",
				prepare: func(t *testing.T, fixture *reconcileFixture) {
					if _, err := ReconcileIntegrationAnalyses(fixture.projectRoot); err != nil {
						t.Fatalf("prepare reconciliation error = %v", err)
					}
				},
				corrupt: func(state *models.State) {
					state.Goal.Integration.ContributingSet.Scopes[0].RootTaskIDs[0] = "replacement-root"
				},
				wantError: "contributing set cannot change",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				fixture := newReconcileFixture(t, false)
				if tc.prepare != nil {
					tc.prepare(t, fixture)
				}
				before := fixture.readState(t)
				previousHooks := testReconcileIntegrationAnalysesHooks
				testReconcileIntegrationAnalysesHooks = &reconcileIntegrationAnalysesTestHooks{beforeValidation: tc.corrupt}
				t.Cleanup(func() { testReconcileIntegrationAnalysesHooks = previousHooks })

				_, err := ReconcileIntegrationAnalyses(fixture.projectRoot)
				testhelpers.RequireErrorContains(t, err, tc.wantError)
				after := fixture.readState(t)
				if !reflect.DeepEqual(before.Goal.Integration, after.Goal.Integration) || !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Sprint.Scope.Planned, after.Sprint.Scope.Planned) {
					t.Fatalf("failed candidate persisted partial transaction")
				}
			})
		}
	})

	t.Run("fewer than two scopes bypass slices and missing capability blocks required slices", func(t *testing.T) {
		single := newReconcileFixture(t, false)
		single.mutateState(t, func(state *models.State) {
			state.Tasks = slices.DeleteFunc(state.Tasks, func(task models.Task) bool {
				return task.ID == "plan-multi" || task.ID == "coding-a" || task.ID == "coding-b"
			})
			state.Sprint.Scope.Planned = slices.DeleteFunc(state.Sprint.Scope.Planned, func(id string) bool {
				return id == "plan-multi" || id == "coding-a" || id == "coding-b"
			})
		})
		result, err := ReconcileIntegrationAnalyses(single.projectRoot)
		if err != nil {
			t.Fatalf("single-scope reconciliation error = %v", err)
		}
		state := single.readState(t)
		global := state.FindTask("integration-global-1")
		if result.Reason != nil || !reflect.DeepEqual(result.CreatedTaskIDs, []string{"integration-global-1"}) || global == nil {
			t.Fatalf("single-scope result=%#v global=%#v", result, global)
		}
		if !reflect.DeepEqual(global.ParentTasks, []string{"coding-single"}) || global.IntegrationAnalysis.Generation != 1 || global.IntegrationAnalysis.SourceCommit != single.head {
			t.Fatalf("global bypass task = %#v", global)
		}
		if len(state.Goal.Integration.Coverage) != 0 {
			t.Fatalf("single-scope bypass coverage = %#v", state.Goal.Integration.Coverage)
		}

		blocked := newReconcileFixture(t, false)
		previousHooks := testReconcileIntegrationAnalysesHooks
		testReconcileIntegrationAnalysesHooks = &reconcileIntegrationAnalysesTestHooks{capability: &pipeline.SlicedIntegrationCapability{
			Code: pipeline.SlicedIntegrationUpgradeRequired, Guidance: "upgrade frozen pipeline",
		}}
		t.Cleanup(func() { testReconcileIntegrationAnalysesHooks = previousHooks })
		result, err = ReconcileIntegrationAnalyses(blocked.projectRoot)
		if err != nil {
			t.Fatalf("missing-capability reconciliation error = %v", err)
		}
		state = blocked.readState(t)
		closure := state.Goal.Integration.Closure
		if closure == nil || closure.Status != models.IntegrationClosureStatusBlocked || closure.Reason != pipeline.SlicedIntegrationUpgradeRequired || result.Reason == nil || result.Reason.Code != pipeline.SlicedIntegrationUpgradeRequired {
			t.Fatalf("missing-capability closure=%#v result=%#v", closure, result)
		}
		if state.FindTask("integration-slice-plan-multi") != nil {
			t.Fatal("missing capability created a slice task")
		}
	})

	t.Run("global waits for repair replacement lineage then creates bounded next generation", func(t *testing.T) {
		fixture := newReconcileFixture(t, false)
		if _, err := ReconcileIntegrationAnalyses(fixture.projectRoot); err != nil {
			t.Fatalf("slice reconciliation error = %v", err)
		}
		fixture.installCleanSlice(t)
		first, err := ReconcileIntegrationAnalyses(fixture.projectRoot)
		if err != nil {
			t.Fatalf("global generation 1 reconciliation error = %v", err)
		}
		if !reflect.DeepEqual(first.CreatedTaskIDs, []string{"integration-global-1"}) {
			t.Fatalf("generation 1 result = %#v", first)
		}
		state := fixture.readState(t)
		global1 := state.FindTask("integration-global-1")
		if !reflect.DeepEqual(global1.ParentTasks, []string{"coding-single", "integration-slice-plan-multi"}) {
			t.Fatalf("generation 1 parents = %v", global1.ParentTasks)
		}

		fixture.installGlobalFindingsAndPendingReplacement(t)
		waiting, err := ReconcileIntegrationAnalyses(fixture.projectRoot)
		if err != nil {
			t.Fatalf("pending replacement reconciliation error = %v", err)
		}
		if waiting.Changed || waiting.Reason != nil || len(waiting.CreatedTaskIDs) != 0 {
			t.Fatalf("pending replacement result = %#v, want no-op", waiting)
		}
		if fixture.readState(t).FindTask("integration-global-2") != nil {
			t.Fatal("pending replacement created generation 2")
		}

		fixture.resolveReplacementAndAdvanceHead(t)
		second, err := ReconcileIntegrationAnalyses(fixture.projectRoot)
		if err != nil {
			t.Fatalf("generation 2 reconciliation error = %v", err)
		}
		state = fixture.readState(t)
		global2 := state.FindTask("integration-global-2")
		wantParents := []string{"coding-single", "integration-global-1", "integration-slice-plan-multi"}
		if !reflect.DeepEqual(second.CreatedTaskIDs, []string{"integration-global-2"}) || global2 == nil || global2.IntegrationAnalysis.SourceCommit != fixture.head || !reflect.DeepEqual(global2.ParentTasks, wantParents) {
			t.Fatalf("generation 2 result=%#v task=%#v", second, global2)
		}
		repeat, err := ReconcileIntegrationAnalyses(fixture.projectRoot)
		if err != nil || repeat.Changed || countTasks(fixture.readState(t), "integration-global-2") != 1 {
			t.Fatalf("generation 2 repeat result=%#v err=%v", repeat, err)
		}
	})

	t.Run("blocked analysis and generation exhaustion project explicit closure", func(t *testing.T) {
		blocked := newReconcileFixture(t, false)
		if _, err := ReconcileIntegrationAnalyses(blocked.projectRoot); err != nil {
			t.Fatalf("prepare blocked slice error = %v", err)
		}
		blocked.mutateState(t, func(state *models.State) {
			task := state.FindTask("integration-slice-plan-multi")
			reason := "review exhausted"
			task.Status = models.TaskStatusBlocked
			task.BlockedReason = &reason
			task.BlockedQuestions = []string{"How should slice analysis continue?"}
		})
		result, err := ReconcileIntegrationAnalyses(blocked.projectRoot)
		if err != nil {
			t.Fatalf("blocked slice reconciliation error = %v", err)
		}
		closure := blocked.readState(t).Goal.Integration.Closure
		if closure == nil || closure.Status != models.IntegrationClosureStatusBlocked || closure.Reason != integrationProgressBlockedAnalysis || result.Reason == nil || result.Reason.Code != integrationProgressBlockedAnalysis {
			t.Fatalf("blocked slice closure=%#v result=%#v", closure, result)
		}

		exhausted := newReconcileFixture(t, false)
		exhausted.mutateState(t, func(state *models.State) {
			state.Config.MaxGlobalIntegrationGenerations = 1
			state.Goal.Integration.ContributingSet = &models.IntegrationContributingSet{Scopes: []models.IntegrationScopeSnapshot{
				{PlanTaskID: "plan-single", RootTaskIDs: []string{"coding-single"}},
			}}
			analysis := reconcileAnalysisTask(t, exhausted, "integration-global-1", "integration-pair", models.IntegrationAnalysisMetadata{
				Key: "global:1", Phase: models.IntegrationAnalysisPhaseGlobal, Generation: 1, SourceCommit: "stale-head",
			}, []string{"coding-single"})
			analysis.ReviewCommit = testhelpers.StringPtr("report-1")
			state.Tasks = append(state.Tasks, analysis)
			state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, analysis.ID)
			state.Goal.Integration.GlobalGenerations = []models.IntegrationGlobalGeneration{{
				Generation: 1, AnalysisTaskID: analysis.ID, AnalysisKey: "global:1",
				Verdict: models.IntegrationAnalysisVerdictClean, SourceCommit: "stale-head", ReportCommit: "report-1",
			}}
		})
		result, err = ReconcileIntegrationAnalyses(exhausted.projectRoot)
		if err != nil {
			t.Fatalf("exhausted reconciliation error = %v", err)
		}
		state := exhausted.readState(t)
		closure = state.Goal.Integration.Closure
		if closure == nil || closure.Status != models.IntegrationClosureStatusExhausted || closure.Reason != integrationProgressBlockedExhausted || result.Reason == nil || !result.Changed {
			t.Fatalf("exhausted closure=%#v result=%#v", closure, result)
		}
		if state.FindTask("integration-global-2") != nil {
			t.Fatal("exhaustion created an extra generation")
		}
	})

	t.Run("public verdict cannot rewrite projected coverage", func(t *testing.T) {
		fixture := newSubmitVerdictIntegrationFixture(t, models.IntegrationAnalysisPhaseSlice, nil)
		before := fixture.readState(t)
		previousHooks := testSubmitVerdictHooks
		testSubmitVerdictHooks = &submitVerdictTestHooks{beforeValidation: func(state *models.State) {
			state.Goal.Integration.Coverage[0].ApprovalAttestations[0].AcceptanceCriteria = "rewritten"
		}}
		t.Cleanup(func() { testSubmitVerdictHooks = previousHooks })

		_, err := SubmitVerdict(fixture.projectRoot, fixture.taskID, "APPROVED", "", fixture.reviewerID, "")
		testhelpers.RequireErrorContains(t, err, "coverage records are append-only")
		after := fixture.readState(t)
		if !reflect.DeepEqual(before.Goal.Integration, after.Goal.Integration) || !reflect.DeepEqual(before.Tasks, after.Tasks) {
			t.Fatal("rejected public verdict persisted rewritten coverage")
		}
	})
}

type reconcileFixture struct {
	projectRoot string
	statePath   string
	base        string
	head        string
	commits     map[string]string
}

type reconcileTaskProjection struct {
	metadata    models.IntegrationAnalysisMetadata
	parentTasks []string
}

func semanticReconcileProjection(t *testing.T, fixture *reconcileFixture, task *models.Task) reconcileTaskProjection {
	t.Helper()
	if task == nil || task.IntegrationAnalysis == nil {
		t.Fatalf("reconciled task = %#v, want integration analysis metadata", task)
	}
	wantChanges := []models.IntegrationDescendantChange{
		{TaskID: "coding-a", Commit: fixture.commits["coding-a"]},
		{TaskID: "coding-b", Commit: fixture.commits["coding-b"]},
	}
	if task.IntegrationAnalysis.SourceCommit != fixture.head || !reflect.DeepEqual(task.IntegrationAnalysis.DescendantChanges, wantChanges) {
		t.Fatalf("commit attribution = source %q changes %#v, want source %q changes %#v", task.IntegrationAnalysis.SourceCommit, task.IntegrationAnalysis.DescendantChanges, fixture.head, wantChanges)
	}

	metadata := *task.IntegrationAnalysis
	metadata.RootTaskIDs = slices.Clone(metadata.RootTaskIDs)
	metadata.DescendantChanges = slices.Clone(metadata.DescendantChanges)
	metadata.AffectedPaths = slices.Clone(metadata.AffectedPaths)
	metadata.SourceSnapshotPaths = slices.Clone(metadata.SourceSnapshotPaths)
	metadata.SourceCommit = "fixture-head"
	for i := range metadata.DescendantChanges {
		metadata.DescendantChanges[i].Commit = metadata.DescendantChanges[i].TaskID
	}
	return reconcileTaskProjection{metadata: metadata, parentTasks: slices.Clone(task.ParentTasks)}
}

func newReconcileFixture(t *testing.T, reverseTasks bool) *reconcileFixture {
	t.Helper()
	return newReconcileFixtureAt(t, reverseTasks, "")
}

func newReconcileFixtureAt(t *testing.T, reverseTasks bool, commitTimestamp string) *reconcileFixture {
	t.Helper()
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.CreateSpecFile(t, projectRoot, "vision.md", "# Integration test\n")
	gitWrapper := gitpkg.New(projectRoot)
	base, err := gitWrapper.GetCommitSHA("HEAD")
	if err != nil {
		t.Fatalf("read base commit: %v", err)
	}

	writeFixtureFile(t, projectRoot, "a.go", "package fixture\n")
	writeFixtureFile(t, projectRoot, "deleted.txt", "temporary\n")
	testhelpers.MustGit(t, projectRoot, "add", "a.go", "deleted.txt")
	commitFixture(t, projectRoot, "add slice a", commitTimestamp)
	commitA := mustCommit(t, gitWrapper, "HEAD")

	if err := os.Remove(filepath.Join(projectRoot, "deleted.txt")); err != nil {
		t.Fatalf("remove deleted fixture: %v", err)
	}
	writeFixtureFile(t, projectRoot, "b.go", "package fixture\n")
	testhelpers.MustGit(t, projectRoot, "add", "b.go", "deleted.txt")
	commitFixture(t, projectRoot, "add slice b", commitTimestamp)
	commitB := mustCommit(t, gitWrapper, "HEAD")

	writeFixtureFile(t, projectRoot, "single.go", "package fixture\n")
	testhelpers.MustGit(t, projectRoot, "add", "single.go")
	commitFixture(t, projectRoot, "add single scope", commitTimestamp)
	commitSingle := mustCommit(t, gitWrapper, "HEAD")
	testhelpers.MustGit(t, projectRoot, "update-ref", "refs/heads/integration", commitSingle)

	fixture := &reconcileFixture{
		projectRoot: projectRoot,
		statePath:   statePath,
		base:        base,
		head:        commitSingle,
		commits: map[string]string{
			"coding-a":      commitA,
			"coding-b":      commitB,
			"coding-single": commitSingle,
		},
	}
	now := time.Now().UTC()
	planSingle := reconcileMergedPlan(now, "plan-single")
	planMulti := reconcileMergedPlan(now, "plan-multi")
	codingA := reconcileMergedCoding(now, "coding-a", "plan-multi", base, commitA)
	codingB := reconcileMergedCoding(now, "coding-b", "plan-multi", commitA, commitB)
	codingSingle := reconcileMergedCoding(now, "coding-single", "plan-single", commitB, commitSingle)
	tasks := []models.Task{planSingle, codingSingle, planMulti, codingB, codingA}
	if reverseTasks {
		slices.Reverse(tasks)
	}
	state := testhelpers.CreateValidState()
	state.Goal.Integration = &models.IntegrationLifecycle{}
	state.Tasks = tasks
	state.Sprint.Scope.Planned = taskIDs(tasks)
	testhelpers.WriteInitialState(t, statePath, state)
	return fixture
}

func commitFixture(t *testing.T, projectRoot, message, timestamp string) {
	t.Helper()
	args := []string{"commit", "-m", message}
	if timestamp != "" {
		args = append(args, "--date", timestamp)
	}
	testhelpers.MustGit(t, projectRoot, args...)
}

func reconcileMergedPlan(now time.Time, id string) models.Task {
	task := testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, now)
	task.Type = models.TaskTypePlanning
	task.RolePair = "code-planning-pair"
	task.SpecRef = "README.md"
	task.PlanRef = "README.md"
	task.ArchRef = "README.md"
	task.DoneWhen = "plan " + id + " implemented"
	task.Scope = "internal/ops"
	task.Output = []models.OutputEntry{{Desc: "coding", DoneWhen: "coded", Scope: "internal/ops", SpecRef: "README.md"}}
	task.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
	task.ReviewCommit = testhelpers.StringPtr("review-" + id)
	return task
}

func reconcileMergedCoding(now time.Time, id, planID, base, commit string) models.Task {
	task := testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, now)
	task.Type = models.TaskTypeCoding
	task.RolePair = "coding-pair"
	task.ParentTask = testhelpers.StringPtr(planID)
	task.SpecRef = "README.md"
	task.DoneWhen = "acceptance for " + id
	task.Scope = "internal/ops"
	task.Validation = []string{"go test ./internal/ops"}
	task.BaseCommit = testhelpers.StringPtr(base)
	task.ReviewCommit = testhelpers.StringPtr(commit)
	task.MergeCommit = testhelpers.StringPtr(commit)
	return task
}

func (fixture *reconcileFixture) installCleanSlice(t *testing.T) {
	t.Helper()
	fixture.mutateState(t, func(state *models.State) {
		task := state.FindTask("integration-slice-plan-multi")
		task.ReviewCommit = testhelpers.StringPtr("slice-report")
		state.Goal.Integration.Coverage = append(state.Goal.Integration.Coverage, models.IntegrationCoverageRecord{
			PlanTaskID: "plan-multi",
			Kind:       models.IntegrationCoverageSliceReport,
			SliceReport: &models.IntegrationSliceReport{
				AnalysisTaskID: task.ID, AnalysisKey: task.IntegrationAnalysis.Key,
				Verdict: models.IntegrationAnalysisVerdictClean, SourceCommit: task.IntegrationAnalysis.SourceCommit, ReportCommit: "slice-report",
			},
		})
	})
}

func (fixture *reconcileFixture) installGlobalFindingsAndPendingReplacement(t *testing.T) {
	t.Helper()
	fixture.mutateState(t, func(state *models.State) {
		global := state.FindTask("integration-global-1")
		global.ReviewCommit = testhelpers.StringPtr("global-report")
		state.Goal.Integration.GlobalGenerations = []models.IntegrationGlobalGeneration{{
			Generation: 1, AnalysisTaskID: global.ID, AnalysisKey: global.IntegrationAnalysis.Key,
			Verdict: models.IntegrationAnalysisVerdictFindings, SourceCommit: global.IntegrationAnalysis.SourceCommit, ReportCommit: "global-report",
		}}
		now := time.Now().UTC()
		repair := testhelpers.BuildTaskByStatus("global-repair", models.TaskStatusSuperseded, now)
		repair.RolePair = "coding-pair"
		repair.Type = models.TaskTypeCoding
		repair.ParentTask = testhelpers.StringPtr(global.ID)
		repair.SupersededBy = []string{"global-replacement"}
		repair.RescopeReason = testhelpers.StringPtr("replace repair")
		repair.SpecRef = "README.md"
		repair.DoneWhen = "repair global"
		repair.Scope = "internal/ops"
		replacement := testhelpers.BuildTaskByStatus("global-replacement", models.TaskStatusReady, now)
		replacement.RolePair = "coding-pair"
		replacement.Type = models.TaskTypeCoding
		replacement.Supersedes = testhelpers.StringPtr(repair.ID)
		replacement.SpecRef = "README.md"
		replacement.DoneWhen = "replace global repair"
		replacement.Scope = "internal/ops"
		state.Tasks = append(state.Tasks, repair, replacement)
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, repair.ID, replacement.ID)
	})
}

func (fixture *reconcileFixture) resolveReplacementAndAdvanceHead(t *testing.T) {
	t.Helper()
	writeFixtureFile(t, fixture.projectRoot, "repair.go", "package fixture\n")
	testhelpers.MustGit(t, fixture.projectRoot, "add", "repair.go")
	testhelpers.MustGit(t, fixture.projectRoot, "commit", "-m", "resolve global repair")
	fixture.head = mustCommit(t, gitpkg.New(fixture.projectRoot), "HEAD")
	testhelpers.MustGit(t, fixture.projectRoot, "update-ref", "refs/heads/integration", fixture.head)
	fixture.mutateState(t, func(state *models.State) {
		replacement := reconcileMergedCoding(time.Now().UTC(), "global-replacement", "integration-global-1", fixture.commits["coding-single"], fixture.head)
		replacement.Supersedes = testhelpers.StringPtr("global-repair")
		for i := range state.Tasks {
			if state.Tasks[i].ID == replacement.ID {
				state.Tasks[i] = replacement
				break
			}
		}
	})
}

func reconcileAnalysisTask(t *testing.T, fixture *reconcileFixture, id, rolePair string, metadata models.IntegrationAnalysisMetadata, parents []string) models.Task {
	t.Helper()
	resolver, _, err := loadResolver(fixture.projectRoot)
	if err != nil {
		t.Fatalf("load resolver: %v", err)
	}
	status, err := resolver.InitialStatus(rolePair)
	if err != nil {
		t.Fatalf("resolve initial status: %v", err)
	}
	return models.Task{
		ID: id, Type: models.TaskTypeIntegration, RolePair: rolePair,
		Description: "Integration analysis", Status: status, Priority: 1,
		SpecRef: "README.md", DoneWhen: "Integration analysis reviewed", Scope: "integration",
		ParentTasks: slices.Clone(parents), Created: time.Now().UTC(), History: []models.TaskHistoryEntry{},
		IntegrationAnalysis: &metadata,
	}
}

func (fixture *reconcileFixture) readState(t *testing.T) *models.State {
	t.Helper()
	state, err := db.New(fixture.statePath).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	return state
}

func (fixture *reconcileFixture) mutateState(t *testing.T, mutate func(*models.State)) {
	t.Helper()
	state := fixture.readState(t)
	mutate(state)
	testhelpers.WriteInitialState(t, fixture.statePath, state)
}

func writeFixtureFile(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func mustCommit(t *testing.T, gitWrapper *gitpkg.Git, ref string) string {
	t.Helper()
	commit, err := gitWrapper.GetCommitSHA(ref)
	if err != nil {
		t.Fatalf("resolve %s: %v", ref, err)
	}
	return commit
}

func taskIDs(tasks []models.Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

func countTasks(state *models.State, id string) int {
	count := 0
	for _, task := range state.Tasks {
		if task.ID == id {
			count++
		}
	}
	return count
}

func countStrings(values []string, want string) int {
	count := 0
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
}
