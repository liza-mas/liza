package agent

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/precommit"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const (
	a8StageTaskID   = "cp-stage-1"
	a8StageProvider = "provider-1"
	a8MissingParent = "parent-absent"
)

// a8StageFixture is a code-planner doer on iteration 2 whose state carries a
// merged provider, a plan sibling and a prior rejection citing a Markdown line
// at a commit. Each stage-order case varies one input of it.
func a8StageFixture() (*models.State, SupervisorConfig) {
	rejected := "specs/missing.md:3 needs a section"
	commit := strings.Repeat("e", 40)
	reason := "REJECTED: " + rejected
	task := models.Task{
		ID:              a8StageTaskID,
		Description:     "Plan the assembly",
		Status:          models.TaskStatusCodePlanning,
		RolePair:        "code-planning-pair",
		Priority:        1,
		Iteration:       2,
		Attempt:         1,
		DoneWhen:        "Plan merged",
		PlanRef:         "specs/plan.md#Task 1",
		DependsOn:       []string{a8StageProvider},
		RejectionReason: &reason,
		History:         []models.TaskHistoryEntry{{Event: models.TaskEventRejected, Commit: &commit, Reason: &rejected}},
	}
	provider := models.Task{ID: a8StageProvider, Status: models.TaskStatusMerged, Type: models.TaskTypeArchitecture}
	sibling := models.Task{ID: "cp-stage-0", Status: models.TaskStatusMerged}
	state := &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Description: "Stage order", SpecRef: "specs/goal.md", Status: models.GoalStatusInProgress},
		Tasks:   []models.Task{task, provider, sibling},
		Config:  models.Config{IntegrationBranch: "main"},
	}
	state.Sprint.Scope.Planned = []string{sibling.ID, task.ID}
	return state, SupervisorConfig{Role: models.RoleCodePlanner, AgentID: "code-planner-1"}
}

// TestA8TaskContextStageOrder pins the stage order of buildTaskRoleContextData
// observed from its result alone: an error returns no partial record, and the
// planner navigation stage runs and returns before the reference stage.
func TestA8TaskContextStageOrder(t *testing.T) {
	resolver := embeddedPipelineResolver(t)

	t.Run("planner stage fails before the reference stage", func(t *testing.T) {
		state, config := a8StageFixture()
		// A missing retained commit is disclosed as advisory absence; only an
		// unavailable repository fails the planner stage (see
		// TestPlannerReworkDisclosesMissingCommitFileAndAmbiguousOrOutOfRangeSections).
		config.ProjectRoot = filepath.Join(t.TempDir(), "absent")
		state.Tasks[0].ParentTasks = []string{a8MissingParent}

		data, err := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
		if data != nil || err == nil {
			t.Fatalf("data = %v, err = %v; want nil data and an error", data, err)
		}
		if !strings.HasPrefix(err.Error(), "planner navigation context: resolve planner rejection citations:") {
			t.Fatalf("error = %q, want the planner navigation stage", err)
		}
		if strings.Contains(err.Error(), "reference context for task") || errors.Is(err, precommit.ErrContextBuild) {
			t.Fatalf("reference stage ran before the planner stage returned: %v", err)
		}
	})

	t.Run("reference stage fails after the planner stage", func(t *testing.T) {
		state, config := a8StageFixture()
		config.ProjectRoot = t.TempDir()
		testhelpers.SetupTestGitRepo(t, config.ProjectRoot)
		state.Tasks[0].RejectionReason = nil
		state.Tasks[0].ParentTasks = []string{a8MissingParent}

		data, err := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
		if data != nil || err == nil {
			t.Fatalf("data = %v, err = %v; want nil data and an error", data, err)
		}
		if !strings.HasPrefix(err.Error(), `reference context for task "`+a8StageTaskID+`":`) ||
			!errors.Is(err, precommit.ErrContextBuild) ||
			!strings.Contains(err.Error(), `direct parent "`+a8MissingParent+`" not found`) {
			t.Fatalf("error = %q, want the reference stage naming the missing parent", err)
		}
	})

	t.Run("success populates identity, plan position, providers and references", func(t *testing.T) {
		state, config := a8StageFixture()
		config.ProjectRoot = t.TempDir()
		testhelpers.SetupTestGitRepo(t, config.ProjectRoot)
		state.Tasks[0].RejectionReason = nil

		data, err := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
		if err != nil {
			t.Fatalf("buildTaskRoleContextData: %v", err)
		}
		if data.Role != models.RoleCodePlanner || data.AgentID != "code-planner-1" || data.RoleType != "doer" ||
			data.TaskID != a8StageTaskID || data.DoneWhen != "Plan merged" || data.Description != "Plan the assembly" ||
			data.PlanRef != "specs/plan.md" || data.PlanSection != "Task 1" || data.IterationNum != 2 || data.AttemptNum != 1 {
			t.Fatalf("identity/task fields = %+v", data)
		}
		if data.TotalPlanTasks != 2 || data.TaskOrdinal != 2 {
			t.Fatalf("plan position = %d/%d, want 2/2", data.TaskOrdinal, data.TotalPlanTasks)
		}
		if len(data.PlannerProviders) != 1 || data.PlannerProviders[0].ID != a8StageProvider || data.PlannerProviders[0].Status != string(models.TaskStatusMerged) {
			t.Fatalf("planner provider rows = %+v", data.PlannerProviders)
		}
		wantLegacy := []prompts.LegacyArtifactReference{{Field: "plan_ref", Ref: "specs/plan.md#Task 1", File: "specs/plan.md"}}
		if data.ResolvedReferenceContext != "" || !reflect.DeepEqual(data.LegacyArtifactReferences, wantLegacy) {
			t.Fatalf("reference outputs = %q, %+v; want legacy %+v", data.ResolvedReferenceContext, data.LegacyArtifactReferences, wantLegacy)
		}
	})
}

// a8ReviewRepository answers the changed-file listing of the two reviewed
// ranges so the counting repository can serve real parent and review carriers.
type a8ReviewRepository struct {
	*countingReferenceRepository
	changed map[string][]string
}

func (r a8ReviewRepository) DiffFiles(_, base, review string) ([]string, error) {
	return r.changed[base+".."+review], nil
}

// TestA8ReferenceHeadCapturedOnce pins that a build walking a strict scalar
// carrier, a merged parent's reviewed range and the current review range
// resolves the configured integration branch exactly once. Pinned-reference
// resolutions name other refs and are not counted.
func TestA8ReferenceHeadCapturedOnce(t *testing.T) {
	head := strings.Repeat("a", 40)
	source := strings.Repeat("b", 40)
	parentBase, parentReview := strings.Repeat("c", 40), strings.Repeat("d", 40)
	currentBase, currentReview := strings.Repeat("e", 40), strings.Repeat("f", 40)
	carrier := func(local string) string {
		return strictCarrier(source, "OBL", "specs/source.md", "Contract", local)
	}
	counting := &countingReferenceRepository{
		resolved: map[string]string{"main": head, source: source, parentBase: parentBase, parentReview: parentReview, currentBase: currentBase, currentReview: currentReview},
		blobs: map[string]string{
			head + ":specs/carrier.md":          carrier("# Carrier\n\nscalar\n"),
			parentReview + ":specs/parent.md":   carrier("# Parent\n\nparent range\n"),
			head + ":specs/parent.md":           carrier("# Parent\n\nparent range\n"),
			currentReview + ":specs/current.md": carrier("# Current\n\ncurrent range\n"),
			source + ":specs/source.md":         "# Source\n\n## Contract\ninherited\n",
			head + ":specs/source.md":           "# Source\n\n## Contract\ninherited\n",
		},
		oids: map[string]string{
			head + ":specs/carrier.md":          "carrier-oid",
			parentReview + ":specs/parent.md":   "parent-oid",
			head + ":specs/parent.md":           "parent-oid",
			currentReview + ":specs/current.md": "current-oid",
			source + ":specs/source.md":         "source-oid",
			head + ":specs/source.md":           "source-oid",
		},
	}
	repo := a8ReviewRepository{counting, map[string][]string{
		parentBase + ".." + parentReview:   {"specs/parent.md"},
		currentBase + ".." + currentReview: {"specs/current.md"},
	}}
	parentID := "parent"
	parent := models.Task{ID: parentID, Status: models.TaskStatusMerged, BaseCommit: &parentBase, ReviewCommit: &parentReview, MergeCommit: &parentReview}
	task := models.Task{ID: "review", SpecRef: "specs/carrier.md", ParentTask: &parentID, BaseCommit: &currentBase, ReviewCommit: &currentReview}
	state := referenceTestState(parent, task)

	context, legacy, err := buildReferenceContextWithRepository(repo, &state.Tasks[1], state, SupervisorConfig{ProjectRoot: "/repo"}, "reviewer", false)
	if err != nil {
		t.Fatalf("buildReferenceContextWithRepository: %v", err)
	}
	for _, want := range []string{"scalar", "parent range", "current range", "inherited"} {
		if !strings.Contains(context, want) {
			t.Fatalf("context lost %q:\n%s", want, context)
		}
	}
	if len(legacy) != 0 {
		t.Fatalf("legacy references = %+v, want none for strict carriers", legacy)
	}
	if got := counting.resolveCalls["main"]; got != 1 {
		t.Fatalf("integration HEAD resolved %d times across scalar, parent and review stages, want once", got)
	}
}
