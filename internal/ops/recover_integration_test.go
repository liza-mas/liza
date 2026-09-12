package ops

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func prematureRecoveryFixture(t *testing.T) (*reconcileFixture, string) {
	t.Helper()
	f := newReconcileFixture(t, false)
	id := integrationAnalysisTaskID("global:1")
	g := gitpkg.New(f.projectRoot)
	if _, err := g.CreateWorktree(id, "integration"); err != nil {
		t.Fatal(err)
	}
	wt := g.GetWorktreePath(id)
	writeFixtureFile(t, wt, "premature-report.md", "# Premature report\n")
	testhelpers.MustGit(t, wt, "add", "premature-report.md")
	testhelpers.MustGit(t, wt, "commit", "-m", "test: preserve premature report")
	report, err := g.GetWorktreeHEAD(id)
	if err != nil {
		t.Fatal(err)
	}
	f.mutateState(t, func(s *models.State) {
		s.Config.Mode = models.SystemModePaused
		planner := models.Task{ID: "epic", Type: models.TaskType("epic-planning"), RolePair: "epic-planning-pair", Status: "DRAFT_EPIC_PLAN", Description: "Plan remaining work", Priority: 1, SpecRef: "README.md", DoneWhen: "Plan reviewed", Scope: "planning", Created: time.Now().UTC()}
		analysis := reconcileAnalysisTask(t, f, id, globalIntegrationRolePair, models.IntegrationAnalysisMetadata{Key: "global:1", Phase: models.IntegrationAnalysisPhaseGlobal, Generation: 1, SourceCommit: f.head}, nil)
		resolver, _, err := loadResolver(f.projectRoot)
		if err != nil {
			t.Fatal(err)
		}
		analysis.Status, err = resolver.SubmittedStatus(globalIntegrationRolePair)
		if err != nil {
			t.Fatal(err)
		}
		analysis.BaseCommit = testhelpers.StringPtr(f.head)
		analysis.ReviewCommit = testhelpers.StringPtr(report)
		analysis.Worktree = testhelpers.StringPtr(filepath.Join(paths.WorktreesDirName, id))
		analysis.History = []models.TaskHistoryEntry{{Time: time.Now().UTC(), Event: models.TaskEventSubmittedForReview, Commit: &report}}
		s.Tasks = []models.Task{planner, analysis}
		s.Sprint.Scope.Planned = []string{planner.ID, id}
		s.Goal.Integration = &models.IntegrationLifecycle{ContributingSet: &models.IntegrationContributingSet{Scopes: []models.IntegrationScopeSnapshot{}}}
	})
	return f, id
}

func TestRecoverIntegrationPreservesEvidenceAndReplays(t *testing.T) {
	f, id := prematureRecoveryFixture(t)
	before := f.readState(t)
	old := before.FindTask(id)
	preview, err := RecoverIntegration(f.projectRoot, id, "premature freeze", true)
	if err != nil {
		t.Fatal(err)
	}
	g := gitpkg.New(f.projectRoot)
	if _, err := g.ResolveCommit(preview.Recovery.PreservationRef); err == nil {
		t.Fatal("dry-run created preservation ref")
	}
	if !reflect.DeepEqual(before, f.readState(t)) {
		t.Fatal("dry-run changed state")
	}
	result, err := RecoverIntegration(f.projectRoot, id, "premature freeze", false)
	if err != nil {
		t.Fatal(err)
	}
	after := f.readState(t)
	retired := after.FindTask(id)
	if retired.Status != models.TaskStatusAbandoned || retired.Worktree != nil || retired.ReviewCommit != nil {
		t.Fatalf("task not retired: %#v", retired)
	}
	if !reflect.DeepEqual(old.IntegrationAnalysis, retired.IntegrationAnalysis) || len(retired.History) != len(old.History)+1 {
		t.Fatal("analysis evidence was not retained")
	}
	if after.Goal.Integration.ContributingSet != nil || after.Goal.Integration.FirstGlobalGeneration() != 2 {
		t.Fatal("cohort was not reset with reserved identity")
	}
	if pinned, err := g.ResolveCommit(result.Recovery.PreservationRef); err != nil || pinned != *old.ReviewCommit {
		t.Fatalf("report not pinned: %q %v", pinned, err)
	}
	if _, err := os.Stat(g.GetWorktreePath(id)); !os.IsNotExist(err) {
		t.Fatalf("worktree remains: %v", err)
	}
	if exists, err := g.BranchExists(paths.TaskBranchPrefix + id); err != nil || exists {
		t.Fatalf("task branch remains: %v", err)
	}
	replay, err := RecoverIntegration(f.projectRoot, id, "premature freeze", false)
	if err != nil || !replay.Replayed || !reflect.DeepEqual(after, f.readState(t)) {
		t.Fatalf("replay changed state: %v", err)
	}
	if _, err := RecoverIntegration(f.projectRoot, id, "different reason", false); err == nil {
		t.Fatal("different recovery intent accepted")
	}
	f.mutateState(t, func(s *models.State) {
		s.Tasks[0].Status = models.TaskStatusAbandoned
		s.Config.MaxGlobalIntegrationGenerations = 1
	})
	continued, err := ReconcileIntegrationAnalyses(f.projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(continued.CreatedTaskIDs, []string{"integration-global-2"}) {
		t.Fatalf("wrong next analysis: %#v", continued)
	}
	continuedState := f.readState(t)
	next := continuedState.FindTask("integration-global-2")
	if next == nil || next.IntegrationAnalysis.Generation != 2 || len(next.EffectiveParentTasks()) != 0 || continuedState.FindTask(id).Status != models.TaskStatusAbandoned {
		t.Fatal("recovery identity/provenance not preserved")
	}

	if _, err := Resume(f.projectRoot, "human"); err != nil {
		t.Fatalf("resume recovered run: %v", err)
	}
	const analystID, reviewerID = "integration-analyst-1", "integration-reviewer-1"
	bb := db.For(f.statePath)
	testhelpers.RegisterTestAgent(t, bb, analystID, "integration-analyst")
	testhelpers.RegisterTestAgent(t, bb, reviewerID, "integration-reviewer")
	if _, err := ClaimTask(f.projectRoot, next.ID, analystID); err != nil {
		t.Fatalf("claim recovered analysis: %v", err)
	}
	if err := WriteCheckpoint(f.projectRoot, &WriteCheckpointInput{
		TaskID: next.ID, AgentID: analystID, Intent: "analyze recovered integration source",
		ValidationPlan: "review integration after planning settled", TDDNotRequired: "read-only integration analysis",
	}); err != nil {
		t.Fatalf("checkpoint recovered analysis: %v", err)
	}
	worktree := g.GetWorktreePath(next.ID)
	writeFixtureFile(t, worktree, "recovered-report.md", "# Integration analysis\nNo findings after planning settled.\n")
	testhelpers.MustGit(t, worktree, "add", "recovered-report.md")
	testhelpers.MustGit(t, worktree, "commit", "-m", "test: record recovered integration analysis")
	reportCommit := mustCommit(t, g, paths.TaskBranchPrefix+next.ID)
	if _, err := SubmitForReview(f.projectRoot, next.ID, reportCommit, analystID); err != nil {
		t.Fatalf("submit recovered analysis: %v", err)
	}
	if _, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot: f.projectRoot, AgentID: reviewerID, Role: "integration-reviewer", TaskID: next.ID,
	}); err != nil {
		t.Fatalf("claim recovered analysis review: %v", err)
	}
	awaitingApproval := f.readState(t)
	_, err = StopForGoalCompletion(f.projectRoot, "integration complete")
	requireEffectiveCompletionPrecondition(t, err)
	if !reflect.DeepEqual(awaitingApproval, f.readState(t)) {
		t.Fatal("completion before approval changed state")
	}
	if _, err := SubmitVerdict(f.projectRoot, next.ID, "APPROVED", "", reviewerID, ""); err != nil {
		t.Fatalf("approve recovered analysis: %v", err)
	}
	if _, err := StopForGoalCompletion(f.projectRoot, "integration complete"); err != nil {
		t.Fatalf("complete recovered run: %v", err)
	}
	completed := f.readState(t)
	lifecycle := completed.Goal.Integration
	wantGenerations := []models.IntegrationGlobalGeneration{{
		Generation: 2, AnalysisTaskID: next.ID, AnalysisKey: "global:2",
		Verdict: models.IntegrationAnalysisVerdictClean, SourceCommit: f.head, ReportCommit: reportCommit,
	}}
	if !reflect.DeepEqual(lifecycle.GlobalGenerations, wantGenerations) {
		t.Fatalf("reviewed generations = %#v, want %#v", lifecycle.GlobalGenerations, wantGenerations)
	}
	wantClosure := &models.IntegrationClosure{
		Status: models.IntegrationClosureStatusClean, Generation: 2, AnalysisKey: "global:2", SourceCommit: f.head,
	}
	if !reflect.DeepEqual(lifecycle.Closure, wantClosure) {
		t.Fatalf("closure = %#v, want %#v", lifecycle.Closure, wantClosure)
	}
	if completed.Config.Mode != models.SystemModeStopped || completed.Config.ModeChangedBy == nil {
		t.Fatalf("recovered run did not stop for goal completion: %#v", completed.Config)
	}
	stop, ok := decodeGoalCompleteStopToken(*completed.Config.ModeChangedBy)
	if !ok || stop.Generation != 2 || stop.AnalysisKey != "global:2" || stop.SourceCommit != f.head {
		t.Fatalf("goal completion lost generation-2 provenance: %#v", stop)
	}
	if !reflect.DeepEqual(lifecycle.PrematureRecovery, after.Goal.Integration.PrematureRecovery) ||
		!reflect.DeepEqual(completed.FindTask(id), retired) {
		t.Fatal("completion changed retired analysis or recovery receipt")
	}
	if pinned, err := g.ResolveCommit(result.Recovery.PreservationRef); err != nil || pinned != *old.ReviewCommit {
		t.Fatalf("completion lost preserved report: %q %v", pinned, err)
	}
}

func TestRecoverIntegrationRefusesUnsafeState(t *testing.T) {
	cases := []struct {
		name, want string
		mutate     func(*models.State, string)
	}{
		{"running", "PAUSED", func(s *models.State, _ string) { s.Config.Mode = models.SystemModeRunning }},
		{"nonempty cohort", "empty frozen", func(s *models.State, _ string) {
			s.Goal.Integration.ContributingSet.Scopes = []models.IntegrationScopeSnapshot{{PlanTaskID: "epic", RootTaskIDs: []string{"root"}}}
		}},
		{"verdict", "review verdict", func(s *models.State, id string) {
			s.FindTask(id).History = append(s.FindTask(id).History, models.TaskHistoryEntry{Event: models.TaskEventRejected})
		}},
		{"closure", "closure", func(s *models.State, _ string) {
			s.Goal.Integration.Closure = &models.IntegrationClosure{Status: models.IntegrationClosureStatusBlocked, Reason: "reason"}
		}},
		{"descendant", "descendant", func(s *models.State, id string) { s.Tasks[0].ParentTasks = []string{id} }},
		{"settled", "settled", func(s *models.State, _ string) { s.Tasks[0].Status = models.TaskStatusAbandoned }},
		{"live claim", "registered owner", func(s *models.State, id string) {
			s.FindTask(id).AssignedTo = testhelpers.StringPtr("integration-analyst-1")
			s.Agents["integration-analyst-1"] = models.Agent{PID: os.Getpid()}
		}},
		{"namespace unknown owner", "registered owner", func(s *models.State, id string) {
			s.FindTask(id).AssignedTo = testhelpers.StringPtr("integration-analyst-1")
			s.Agents["integration-analyst-1"] = models.Agent{PID: 99999999, Heartbeat: time.Now().UTC()}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, id := prematureRecoveryFixture(t)
			f.mutateState(t, func(s *models.State) { tc.mutate(s, id) })
			before := f.readState(t)
			_, err := RecoverIntegration(f.projectRoot, id, "premature freeze", false)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
			if !reflect.DeepEqual(before, f.readState(t)) {
				t.Fatal("refused recovery changed state")
			}
			if _, err := gitpkg.New(f.projectRoot).ResolveCommit("refs/integration-recovery/" + id); err == nil {
				t.Fatal("refused recovery created pin")
			}
		})
	}
}

func TestRecoverIntegrationRefusesDirtyReport(t *testing.T) {
	f, id := prematureRecoveryFixture(t)
	writeFixtureFile(t, gitpkg.New(f.projectRoot).GetWorktreePath(id), "uncommitted.md", "keep this work")
	_, err := RecoverIntegration(f.projectRoot, id, "premature freeze", false)
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(gitpkg.New(f.projectRoot).GetWorktreePath(id), "uncommitted.md")); err != nil {
		t.Fatal("dirty work was lost")
	}
}

func TestRecoverIntegrationRechecksStateAfterPin(t *testing.T) {
	f, id := prematureRecoveryFixture(t)
	bb := db.For(f.statePath)
	defer setLifecycleMutationTestHook(bb, func() {
		if err := bb.Modify(func(s *models.State) error { s.Config.Mode = models.SystemModeRunning; return nil }); err != nil {
			t.Fatal(err)
		}
	})()
	_, err := RecoverIntegration(f.projectRoot, id, "premature freeze", false)
	if err == nil || !strings.Contains(err.Error(), "PAUSED") {
		t.Fatalf("mode race accepted: %v", err)
	}
	after := f.readState(t)
	if after.Goal.Integration.PrematureRecovery != nil || after.FindTask(id).Status == models.TaskStatusAbandoned {
		t.Fatal("race partially retired task")
	}
	if _, err := gitpkg.New(f.projectRoot).ResolveCommit("refs/integration-recovery/" + id); err != nil {
		t.Fatal("pinned report lost after refused transaction")
	}
}
