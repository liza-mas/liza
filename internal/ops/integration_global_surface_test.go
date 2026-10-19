package ops

import (
	"reflect"
	"strings"
	"testing"
	"time"

	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// globalSurfaceFixture builds a linear history where every goal task owns one
// reviewed range and a foreign commit lands between them on the same branch.
func globalSurfaceFixture(t *testing.T) (*models.State, string) {
	t.Helper()
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	gitWrapper := gitpkg.New(projectRoot)
	commit := func(message string, files ...string) string {
		t.Helper()
		for _, file := range files {
			writeFixtureFile(t, projectRoot, file, message+"\n")
		}
		testhelpers.MustGit(t, projectRoot, append([]string{"add"}, files...)...)
		commitFixture(t, projectRoot, message, "")
		return mustCommit(t, gitWrapper, "HEAD")
	}

	base := mustCommit(t, gitWrapper, "HEAD")
	single := commit("coding-single", "shared.go", "single.go")
	left := commit("coding-left", "shared.go", "left.go")
	foreign := commit("foreign ticket", "foreign.txt", "single.go")
	right := commit("coding-right-r1", "right.go")
	sliceFix := commit("slice repair", "single.go")
	globalFix := commit("global repair", "left.go")

	now := time.Now().UTC()
	merged := func(id, baseCommit, reviewCommit string, parents ...string) models.Task {
		merge := reviewCommit
		return models.Task{
			ID: id, Description: id + " change", Status: models.TaskStatusMerged, RolePair: "coding-pair",
			ParentTasks: parents, BaseCommit: &baseCommit, ReviewCommit: &reviewCommit, MergeCommit: &merge, Created: now,
		}
	}
	analysis := func(id, key string, phase models.IntegrationAnalysisPhase, generation int) models.Task {
		return models.Task{
			ID: id, Status: models.TaskStatusMerged, Type: models.TaskTypeIntegration, Created: now,
			IntegrationAnalysis: &models.IntegrationAnalysisMetadata{Key: key, Phase: phase, Generation: generation, SourceCommit: globalFix},
		}
	}

	codingSingle := merged("coding-single", base, single, "plan-single")
	codingSingle.Decomposition = &models.DecompositionManifest{InterfacesOwned: []string{"api:v1"}}
	codingLeft := merged("coding-left", single, left, "plan-sliced")
	codingLeft.Decomposition = &models.DecompositionManifest{InterfacesConsumed: []string{"api:v1", "queue:jobs"}}
	superseded := models.Task{ID: "coding-right", Status: models.TaskStatusSuperseded, RolePair: "coding-pair", ParentTasks: []string{"plan-sliced"}, SupersededBy: []string{"coding-right-r1"}, Created: now}
	replacedBy := "coding-right"
	codingRight := merged("coding-right-r1", foreign, right)
	codingRight.Supersedes = &replacedBy
	sliceAnalysis := analysis("slice-analysis", "slice:plan-sliced", models.IntegrationAnalysisPhaseSlice, 0)
	sliceAnalysis.IntegrationAnalysis.OriginatingPlanTaskID = "plan-sliced"
	sliceAnalysis.IntegrationAnalysis.RootTaskIDs = []string{"coding-left", "coding-right"}

	state := &models.State{
		Goal: models.Goal{
			ID: "goal-1", BaseCommit: &base, Status: models.GoalStatusInProgress, Created: now,
			Integration: &models.IntegrationLifecycle{ContributingSet: &models.IntegrationContributingSet{Scopes: []models.IntegrationScopeSnapshot{
				{PlanTaskID: "plan-sliced", RootTaskIDs: []string{"coding-left", "coding-right"}},
				{PlanTaskID: "plan-single", RootTaskIDs: []string{"coding-single"}},
			}}},
		},
		Tasks: []models.Task{
			{ID: "plan-single", Status: models.TaskStatusMerged, RolePair: "code-planning-pair", Created: now},
			{ID: "plan-sliced", Status: models.TaskStatusMerged, RolePair: "code-planning-pair", Created: now},
			codingSingle, codingLeft, superseded, codingRight,
			sliceAnalysis,
			merged("fix-slice", right, sliceFix, "slice-analysis"),
			analysis("global-1", "global:1", models.IntegrationAnalysisPhaseGlobal, 1),
			merged("fix-global", sliceFix, globalFix, "global-1"),
			analysis("global-2", "global:2", models.IntegrationAnalysisPhaseGlobal, 2),
		},
	}
	return state, projectRoot
}

func TestBuildGlobalIntegrationSurface(t *testing.T) {
	state, projectRoot := globalSurfaceFixture(t)

	surface, err := BuildGlobalIntegrationSurface(state, projectRoot, 2)
	if err != nil {
		t.Fatalf("BuildGlobalIntegrationSurface() error = %v", err)
	}

	gotPlans := make(map[string][]string)
	for _, plan := range surface.Plans {
		for _, change := range plan.Tasks {
			gotPlans[plan.PlanTaskID] = append(gotPlans[plan.PlanTaskID], change.TaskID+":"+strings.Join(change.Paths, ","))
		}
	}
	wantPlans := map[string][]string{
		"plan-single": {"coding-single:shared.go,single.go"},
		"plan-sliced": {"coding-left:left.go,shared.go", "coding-right-r1:right.go", "fix-slice:single.go"},
	}
	if !reflect.DeepEqual(gotPlans, wantPlans) {
		t.Fatalf("plan attribution = %v, want %v (foreign.txt and superseded work must be absent)", gotPlans, wantPlans)
	}

	wantSeams := []GlobalIntegrationSeam{
		{Name: "shared.go", PlanTaskIDs: []string{"plan-single", "plan-sliced"}},
		{Name: "single.go", PlanTaskIDs: []string{"plan-single", "plan-sliced"}},
	}
	if !reflect.DeepEqual(surface.SeamPaths, wantSeams) {
		t.Fatalf("seam paths = %#v, want %#v (a prior global repair must not create left.go seam)", surface.SeamPaths, wantSeams)
	}
	wantInterfaces := []GlobalIntegrationSeam{{Name: "api:v1", PlanTaskIDs: []string{"plan-single", "plan-sliced"}}}
	if !reflect.DeepEqual(surface.SeamInterfaces, wantInterfaces) {
		t.Fatalf("seam interfaces = %#v, want %#v", surface.SeamInterfaces, wantInterfaces)
	}
	if len(surface.PriorRepairs) != 1 || surface.PriorRepairs[0].TaskID != "fix-global" || !reflect.DeepEqual(surface.PriorRepairs[0].Paths, []string{"left.go"}) {
		t.Fatalf("prior repairs = %#v, want fix-global touching left.go", surface.PriorRepairs)
	}
	if surface.PreviousGeneration != nil {
		t.Fatalf("previous generation = %#v, want nil without an accepted generation 1", surface.PreviousGeneration)
	}

	t.Run("first generation has no prior repairs", func(t *testing.T) {
		first, err := BuildGlobalIntegrationSurface(state, projectRoot, 1)
		if err != nil {
			t.Fatalf("BuildGlobalIntegrationSurface() error = %v", err)
		}
		if len(first.PriorRepairs) != 0 {
			t.Fatalf("prior repairs = %#v, want none", first.PriorRepairs)
		}
	})

	t.Run("single plan has no cross-plan seam", func(t *testing.T) {
		single := *state
		single.Goal.Integration = &models.IntegrationLifecycle{ContributingSet: &models.IntegrationContributingSet{Scopes: []models.IntegrationScopeSnapshot{
			{PlanTaskID: "plan-sliced", RootTaskIDs: []string{"coding-left", "coding-right"}},
		}}}
		reduced, err := BuildGlobalIntegrationSurface(&single, projectRoot, 2)
		if err != nil {
			t.Fatalf("BuildGlobalIntegrationSurface() error = %v", err)
		}
		if len(reduced.Plans) != 1 || len(reduced.SeamPaths) != 0 || len(reduced.SeamInterfaces) != 0 {
			t.Fatalf("single-plan surface = %#v, want one plan and no seams", reduced)
		}
	})

	t.Run("missing reviewed range fails closed", func(t *testing.T) {
		broken := *state
		broken.Tasks = append([]models.Task(nil), state.Tasks...)
		broken.FindTask("coding-single").ReviewCommit = nil
		_, err := BuildGlobalIntegrationSurface(&broken, projectRoot, 2)
		if err == nil || !strings.Contains(err.Error(), `merged goal task "coding-single" lacks reviewed change attribution`) {
			t.Fatalf("BuildGlobalIntegrationSurface() error = %v, want missing attribution", err)
		}
	})

	// withGeneration1 records generation 1 as accepted with the given verdict
	// and gives its repair approval facts, unless dropRepair removes it.
	withGeneration1 := func(verdict models.IntegrationAnalysisVerdict, dropRepair bool) *models.State {
		fixRound := *state
		integration := *state.Goal.Integration
		integration.GlobalGenerations = []models.IntegrationGlobalGeneration{{
			Generation: 1, AnalysisTaskID: "global-1", AnalysisKey: "global:1",
			Verdict: verdict, SourceCommit: "gen1-source", ReportCommit: "gen1-report",
		}}
		fixRound.Goal.Integration = &integration
		fixRound.Tasks = make([]models.Task, 0, len(state.Tasks))
		for _, task := range state.Tasks {
			if task.ID == "fix-global" {
				if dropRepair {
					continue
				}
				approver := "reviewer-1"
				task.ApprovedBy = &approver
				task.DoneWhen = "left.go stub returns the credential"
				task.Validation = []string{"make test-left"}
			}
			fixRound.Tasks = append(fixRound.Tasks, task)
		}
		return &fixRound
	}

	t.Run("fix round carries the previous generation's repair evidence", func(t *testing.T) {
		fixRound := withGeneration1(models.IntegrationAnalysisVerdictFindings, false)
		got, err := BuildGlobalIntegrationSurface(fixRound, projectRoot, 2)
		if err != nil {
			t.Fatalf("BuildGlobalIntegrationSurface() error = %v", err)
		}
		repair := fixRound.FindTask("fix-global")
		want := &GlobalIntegrationPreviousGeneration{
			Generation: 1, Verdict: models.IntegrationAnalysisVerdictFindings, SourceCommit: "gen1-source",
			Repairs: []models.IntegrationApprovalAttestation{{
				ReviewedTaskID: "fix-global", AcceptanceCriteria: "left.go stub returns the credential",
				ReviewedCommit: *repair.ReviewCommit, Approver: "reviewer-1",
				Validation: []string{"make test-left"}, MergeCommit: *repair.MergeCommit,
			}},
		}
		if !reflect.DeepEqual(got.PreviousGeneration, want) {
			t.Fatalf("previous generation = %#v, want %#v", got.PreviousGeneration, want)
		}
	})

	t.Run("clean predecessor has no repairs", func(t *testing.T) {
		got, err := BuildGlobalIntegrationSurface(withGeneration1(models.IntegrationAnalysisVerdictClean, true), projectRoot, 2)
		if err != nil {
			t.Fatalf("BuildGlobalIntegrationSurface() error = %v", err)
		}
		previous := got.PreviousGeneration
		if previous == nil || previous.Generation != 1 || previous.Verdict != models.IntegrationAnalysisVerdictClean || previous.SourceCommit != "gen1-source" || len(previous.Repairs) != 0 {
			t.Fatalf("previous generation = %#v, want clean generation 1 at gen1-source with no repairs", previous)
		}
	})

	t.Run("first accepted generation after premature recovery has no predecessor", func(t *testing.T) {
		recovered := *state
		integration := *state.Goal.Integration
		integration.PrematureRecovery = &models.IntegrationPrematureRecovery{AnalysisTaskID: "global-1"}
		recovered.Goal.Integration = &integration
		got, err := BuildGlobalIntegrationSurface(&recovered, projectRoot, 2)
		if err != nil {
			t.Fatalf("BuildGlobalIntegrationSurface() error = %v", err)
		}
		if got.PreviousGeneration != nil {
			t.Fatalf("previous generation = %#v, want nil", got.PreviousGeneration)
		}
	})

	t.Run("repair without approval facts fails closed", func(t *testing.T) {
		broken := withGeneration1(models.IntegrationAnalysisVerdictFindings, false)
		broken.FindTask("fix-global").ApprovedBy = nil
		_, err := BuildGlobalIntegrationSurface(broken, projectRoot, 2)
		if err == nil || !strings.Contains(err.Error(), `merged task "fix-global" lacks approval attestation facts`) {
			t.Fatalf("BuildGlobalIntegrationSurface() error = %v, want missing approval facts", err)
		}
	})
}
