package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/precommit"
	"github.com/liza-mas/liza/internal/referencecontract"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestBuildPromptReferenceFirstPlanningPilot(t *testing.T) {
	repo := t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)

	writeReferenceFixture(t, repo, "specs/override.md", "# Decisions\n\n## Human Decision\nDEC-1: explicit later authority.\n")
	overrideRevision := commitReferenceFixture(t, repo, "test: add explicit decision override")
	writeReferenceFixture(t, repo, "specs/requirements.md", "# Requirements\n\n## Règle `X` — débit\nREQ-1: preserve bounded behavior.\n\n## Sibling\nSIBLING-MUST-NOT-LOAD\n")
	sourceRevision := commitReferenceFixture(t, repo, "test: add inherited requirements")
	carrier := strictCarrierWithOverride(sourceRevision, overrideRevision)
	writeReferenceFixture(t, repo, "specs/goal.md", carrier)
	commitReferenceFixture(t, repo, "test: add strict goal carrier")

	state := referenceTestState(models.Task{
		ID:          "epic-1",
		Type:        models.TaskTypeEpicPlanning,
		RolePair:    "epic-planning-pair",
		Description: "Partition the capability",
		Status:      models.TaskStatus("PLANNING_EPIC"),
		SpecRef:     "specs/goal.md",
		DoneWhen:    "The capability partition is reviewable",
		Scope:       "CAP-001",
	})
	prompt, err := buildPromptWithContext(state, SupervisorConfig{
		AgentID: "epic-planner-1", Role: models.RoleEpicPlanner, ProjectRoot: repo, SpecsDir: "specs",
	}, "epic-1", testResolver(t))
	if err != nil {
		t.Fatalf("buildPromptWithContext: %v", err)
	}
	for _, want := range []string{
		"=== RESOLVED REFERENCE CONTEXT ===",
		"Local decomposition decision.",
		"REQ-1: preserve bounded behavior.",
		"DEC-1: explicit later authority.",
		"Règle `X` — débit",
		"do not re-read",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if strings.Contains(prompt, "SIBLING-MUST-NOT-LOAD") {
		t.Error("prompt included an undeclared sibling heading span")
	}

	writeReferenceFixture(t, repo, "specs/requirements.md", "# Requirements\n\n## Règle `X` — débit\nREQ-1 changed without an owner correction.\n")
	commitReferenceFixture(t, repo, "test: make inherited source stale")
	_, err = buildPromptWithContext(state, SupervisorConfig{
		AgentID: "epic-planner-1", Role: models.RoleEpicPlanner, ProjectRoot: repo, SpecsDir: "specs",
	}, "epic-1", testResolver(t))
	if err == nil || !errors.Is(err, precommit.ErrContextBuild) || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale strict reference error = %v, want context-build stale error", err)
	}
}

func TestResolvedReferenceContextLegacyScalarFragmentIsDisplayOnly(t *testing.T) {
	repo := t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)
	writeReferenceFixture(t, repo, "specs/api.md", "# API\n\n## Rate Limiting\nLegacy content.\n")
	commitReferenceFixture(t, repo, "test: add legacy artifact")

	task := models.Task{ID: "legacy", SpecRef: "specs/api.md#rate-limiting"}
	state := referenceTestState(task)
	context, err := buildResolvedReferenceContext(&state.Tasks[0], state, SupervisorConfig{ProjectRoot: repo}, "doer")
	if err != nil {
		t.Fatalf("buildResolvedReferenceContext: %v", err)
	}
	if context != "" {
		t.Fatalf("legacy context = %q, want empty strict block", context)
	}
}

func TestResolvedReferenceContextSkipsGitWhenNoCarrierCanExist(t *testing.T) {
	t.Parallel()

	repo := &countingReferenceRepository{}
	task := models.Task{ID: "legacy"}
	state := referenceTestState(task)
	context, err := buildResolvedReferenceContextWithRepository(repo, &state.Tasks[0], state, SupervisorConfig{ProjectRoot: "/repo"}, "doer")
	if err != nil || context != "" {
		t.Fatalf("carrier-free context = (%q, %v), want empty legacy context", context, err)
	}
	if got := repo.resolveCalls["main"]; got != 0 {
		t.Fatalf("integration HEAD resolved %d times, want zero without carriers", got)
	}
}

func TestResolvedReferenceContextKeepsMissingScalarCarrierOnLegacyPath(t *testing.T) {
	repo := t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)
	task := models.Task{ID: "missing", SpecRef: "specs/missing.md"}
	state := referenceTestState(task)

	context, legacy, err := buildReferenceContext(&state.Tasks[0], state, SupervisorConfig{ProjectRoot: repo}, "doer")
	if err != nil || context != "" {
		t.Fatalf("missing scalar carrier = (%q, %v), want empty strict context on legacy path", context, err)
	}
	if len(legacy) != 1 || legacy[0].Field != "spec_ref" || legacy[0].Ref != "specs/missing.md" {
		t.Fatalf("legacy references = %#v, want missing spec_ref", legacy)
	}
}

func TestResolvedReferenceContextStillRejectsPresentMalformedStrictCarrier(t *testing.T) {
	repo := t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)
	writeReferenceFixture(t, repo, "specs/malformed.md", "# Plan\n\n## Source References\nmalformed\n")
	commitReferenceFixture(t, repo, "test: add malformed strict carrier")
	task := models.Task{ID: "malformed", SpecRef: "specs/malformed.md"}
	state := referenceTestState(task)

	_, err := buildResolvedReferenceContext(&state.Tasks[0], state, SupervisorConfig{ProjectRoot: repo}, "doer")
	if err == nil || !strings.Contains(err.Error(), "parse scalar carrier") {
		t.Fatalf("malformed strict carrier error = %v", err)
	}
}

func TestResolvedReferenceContextPropagatesPresentScalarReadFailure(t *testing.T) {
	t.Parallel()

	head := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	repo := &countingReferenceRepository{
		resolved:  map[string]string{"main": head},
		treeModes: map[string]string{head + ":specs/present.md": "100644"},
	}
	task := models.Task{ID: "present", SpecRef: "specs/present.md"}
	state := referenceTestState(task)

	_, err := buildResolvedReferenceContextWithRepository(repo, &state.Tasks[0], state, SupervisorConfig{ProjectRoot: "/repo"}, "doer")
	if err == nil || !strings.Contains(err.Error(), "missing blob") {
		t.Fatalf("present scalar read failure = %v, want propagated read error", err)
	}
}

func TestBuildPromptPreservesMixedStrictAndLegacyScalarRoutes(t *testing.T) {
	repo := t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)
	writeReferenceFixture(t, repo, "specs/source.md", "# Source\n\n## Contract\nInherited authority.\n")
	sourceRevision := commitReferenceFixture(t, repo, "test: add mixed source")
	writeReferenceFixture(t, repo, "specs/goal.md", strictCarrier(sourceRevision, "CONTRACT", "specs/source.md", "Contract", "# Goal\n\nStrict task authority.\n"))
	writeReferenceFixture(t, repo, "specs/legacy-plan.md", "# Legacy Plan\n\n## Task Plan\nLegacy implementation detail.\n")
	commitReferenceFixture(t, repo, "test: add mixed carriers")

	worktree := ".worktrees/coder-1"
	state := referenceTestState(models.Task{
		ID:       "coder-1",
		Status:   models.TaskStatusImplementing,
		RolePair: "coding-pair",
		SpecRef:  "specs/goal.md",
		PlanRef:  "specs/legacy-plan.md#task-plan",
		Worktree: &worktree,
	})
	prompt, err := buildPromptWithContext(state, SupervisorConfig{
		AgentID: "coder-1", Role: "coder", ProjectRoot: repo, SpecsDir: "specs",
	}, "coder-1", testResolver(t))
	if err != nil {
		t.Fatalf("buildPromptWithContext: %v", err)
	}
	for _, want := range []string{
		"=== RESOLVED REFERENCE CONTEXT ===",
		"Strict task authority.",
		"=== LEGACY ARTIFACT REFERENCES ===",
		"plan_ref specs/legacy-plan.md#task-plan: read " + filepath.Join(repo, worktree) + "/specs/legacy-plan.md first",
		"show main:specs/legacy-plan.md",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("mixed prompt missing %q", want)
		}
	}
	if count := strings.Count(prompt, "plan_ref specs/legacy-plan.md#task-plan: read "); count != 1 {
		t.Errorf("legacy plan route rendered %d times, want once", count)
	}
	if strings.Contains(prompt, "spec_ref specs/goal.md: read ") {
		t.Error("strict spec was also rendered as a legacy read route")
	}
}

func TestResolvedReferenceContextStrictScalarFragmentSelection(t *testing.T) {
	repo := t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)
	writeReferenceFixture(t, repo, "specs/source.md", "# Source\n\n## Contract\nInherited authority.\n")
	sourceRevision := commitReferenceFixture(t, repo, "test: add fragment source")
	writeReferenceFixture(t, repo, "specs/carrier.md", strictCarrier(sourceRevision, "CONTRACT", "specs/source.md", "Contract",
		"# Carrier\n\n## Exact `α` — Decision\nSELECTED LOCAL SPAN\n\n### Child\nSELECTED CHILD\n\n## Sibling\nEXCLUDED SIBLING\n"))
	commitReferenceFixture(t, repo, "test: add strict fragment carrier")

	task := models.Task{ID: "fragment", SpecRef: "specs/carrier.md#Exact `α` — Decision"}
	state := referenceTestState(task)
	context, err := buildResolvedReferenceContext(&state.Tasks[0], state, SupervisorConfig{ProjectRoot: repo}, "doer")
	if err != nil {
		t.Fatalf("buildResolvedReferenceContext: %v", err)
	}
	for _, want := range []string{"SELECTED LOCAL SPAN", "SELECTED CHILD", "Inherited authority."} {
		if !strings.Contains(context, want) {
			t.Errorf("strict fragment context missing %q", want)
		}
	}
	if strings.Contains(context, "EXCLUDED SIBLING") {
		t.Error("strict scalar fragment included a sibling span")
	}

	state.Tasks[0].SpecRef = "specs/carrier.md#Missing"
	_, err = buildResolvedReferenceContext(&state.Tasks[0], state, SupervisorConfig{ProjectRoot: repo}, "doer")
	if err == nil || !strings.Contains(err.Error(), "is missing") {
		t.Fatalf("missing strict scalar fragment error = %v", err)
	}

	writeReferenceFixture(t, repo, "specs/carrier.md", strictCarrier(sourceRevision, "CONTRACT", "specs/source.md", "Contract",
		"# Carrier\n\n## Duplicate\nfirst\n\n### Duplicate\nsecond\n"))
	commitReferenceFixture(t, repo, "test: make strict fragment ambiguous")
	state.Tasks[0].SpecRef = "specs/carrier.md#Duplicate"
	_, err = buildResolvedReferenceContext(&state.Tasks[0], state, SupervisorConfig{ProjectRoot: repo}, "doer")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous strict scalar fragment error = %v", err)
	}
}

func TestResolvedReferenceContextRequiresReadableCurrentReviewRange(t *testing.T) {
	t.Parallel()

	repo := &countingReferenceRepository{resolved: map[string]string{"main": "head"}, diffErr: errors.New("unreadable range")}
	base, review := "base", "review"
	task := models.Task{ID: "review", BaseCommit: &base, ReviewCommit: &review}
	state := referenceTestState(task)
	_, err := buildResolvedReferenceContextWithRepository(repo, &state.Tasks[0], state, SupervisorConfig{ProjectRoot: "/repo"}, "reviewer")
	if err == nil || !strings.Contains(err.Error(), "unreadable range") {
		t.Fatalf("unreadable current-review range error = %v", err)
	}

	task.BaseCommit = nil
	_, err = buildResolvedReferenceContextWithRepository(repo, &task, state, SupervisorConfig{ProjectRoot: "/repo"}, "reviewer")
	if err == nil || !strings.Contains(err.Error(), "lacks BaseCommit..ReviewCommit attribution") {
		t.Fatalf("missing current-review range error = %v", err)
	}
}

func TestResolvedReferenceContextLoadsAllScalarCarrierClasses(t *testing.T) {
	repo := t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)
	writeReferenceFixture(t, repo, "specs/source.md", "# Source\n\n## Shared\nShared authority.\n")
	sourceRevision := commitReferenceFixture(t, repo, "test: add shared authority")

	refs := []struct {
		path string
		body string
	}{
		{path: "specs/goal.md", body: "goal carrier"},
		{path: "specs/epic.md", body: "epic carrier"},
		{path: "specs/plan.md", body: "plan carrier"},
		{path: "specs/arch.md", body: "architecture carrier"},
	}
	for _, ref := range refs {
		writeReferenceFixture(t, repo, ref.path, strictCarrier(sourceRevision, "SHARED", "specs/source.md", "Shared", "# Local\n\n"+ref.body+"\n"))
	}
	commitReferenceFixture(t, repo, "test: add scalar carriers")

	task := models.Task{
		ID: "all-scalars", SpecRef: refs[0].path, EpicRef: refs[1].path,
		PlanRef: refs[2].path, ArchRef: refs[3].path,
	}
	state := referenceTestState(task)
	context, err := buildResolvedReferenceContext(&state.Tasks[0], state, SupervisorConfig{ProjectRoot: repo}, "doer")
	if err != nil {
		t.Fatalf("buildResolvedReferenceContext: %v", err)
	}
	for _, ref := range refs {
		if count := strings.Count(context, ref.body); count != 1 {
			t.Errorf("%q rendered %d times, want once", ref.body, count)
		}
	}
	if count := strings.Count(context, "Shared authority."); count != 1 {
		t.Errorf("deduplicated direct reference rendered %d times, want once", count)
	}
}

func TestResolvedReferenceContextReviewCandidateMayBeAbsentAtHead(t *testing.T) {
	repo := t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)
	writeReferenceFixture(t, repo, "specs/source.md", "# Source\n\n## Contract\nInherited contract.\n")
	sourceRevision := commitReferenceFixture(t, repo, "test: add source")
	base := testhelpers.MustGit(t, repo, "rev-parse", "main")
	testhelpers.MustGit(t, repo, "checkout", "-b", "candidate")
	writeReferenceFixture(t, repo, "specs/candidate.md", strictCarrier(sourceRevision, "CONTRACT", "specs/source.md", "Contract", "# Candidate\n\nCandidate-only decision.\n"))
	review := commitReferenceFixture(t, repo, "test: add review candidate")
	testhelpers.MustGit(t, repo, "checkout", "main")

	task := models.Task{ID: "review-1", BaseCommit: &base, ReviewCommit: &review}
	state := referenceTestState(task)
	context, err := buildResolvedReferenceContext(&state.Tasks[0], state, SupervisorConfig{ProjectRoot: repo}, "reviewer")
	if err != nil {
		t.Fatalf("buildResolvedReferenceContext: %v", err)
	}
	if !strings.Contains(context, "Candidate-only decision.") || !strings.Contains(context, "Inherited contract.") {
		t.Fatalf("review context omitted candidate or inherited Span:\n%s", context)
	}
	if _, err := os.Stat(filepath.Join(repo, "specs", "candidate.md")); !os.IsNotExist(err) {
		t.Fatalf("candidate unexpectedly exists at integration HEAD: %v", err)
	}
}

func TestBuildPromptStrictReviewCandidateSuppressesSamePathLegacyRoute(t *testing.T) {
	repo := t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)
	writeReferenceFixture(t, repo, "specs/source.md", "# Source\n\n## Contract\nInherited authority.\n")
	sourceRevision := commitReferenceFixture(t, repo, "test: add promotion source")
	writeReferenceFixture(t, repo, "specs/candidate.md", "# Candidate\n\nMarker-free baseline.\n")
	base := commitReferenceFixture(t, repo, "test: add legacy candidate baseline")
	testhelpers.MustGit(t, repo, "checkout", "-b", "candidate")
	writeReferenceFixture(t, repo, "specs/candidate.md", strictCarrier(sourceRevision, "CONTRACT", "specs/source.md", "Contract", "# Candidate\n\nPROMOTED STRICT CANDIDATE\n"))
	review := commitReferenceFixture(t, repo, "test: promote candidate to strict")
	testhelpers.MustGit(t, repo, "checkout", "main")

	worktree := ".worktrees/review"
	state := referenceTestState(models.Task{
		ID:           "review-1",
		Status:       models.TaskStatusReadyForReview,
		RolePair:     "coding-pair",
		SpecRef:      "specs/candidate.md",
		BaseCommit:   &base,
		ReviewCommit: &review,
		Worktree:     &worktree,
	})
	prompt, err := buildPromptWithContext(state, SupervisorConfig{
		AgentID: "code-reviewer-1", Role: "code-reviewer", ProjectRoot: repo, SpecsDir: "specs",
	}, "review-1", testResolver(t))
	if err != nil {
		t.Fatalf("buildPromptWithContext: %v", err)
	}
	if count := strings.Count(prompt, "PROMOTED STRICT CANDIDATE"); count != 1 {
		t.Fatalf("strict promoted candidate rendered %d times, want once", count)
	}
	for _, forbidden := range []string{"=== LEGACY ARTIFACT REFERENCES ===", "spec_ref specs/candidate.md: read "} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("promoted strict candidate retained legacy route %q", forbidden)
		}
	}
}

func TestResolvedReferenceContextDirectParentFanIn(t *testing.T) {
	repo := t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)
	writeReferenceFixture(t, repo, "specs/source.md", "# Source\n\n## Shared Contract\nShared inherited text.\n")
	sourceRevision := commitReferenceFixture(t, repo, "test: add shared source")
	base := testhelpers.MustGit(t, repo, "rev-parse", "main")
	writeReferenceFixture(t, repo, "specs/story.md", strictCarrier(sourceRevision, "SHARED", "specs/source.md", "Shared Contract", "# Story\n\nStory-owned behavior and AC.\n"))
	review := commitReferenceFixture(t, repo, "test: add reviewed story")
	merge := review
	parentID := "story-parent"
	parent := models.Task{ID: parentID, Status: models.TaskStatusMerged, BaseCommit: &base, ReviewCommit: &review, MergeCommit: &merge}
	child := models.Task{ID: "architecture-child", ParentTask: &parentID}
	state := referenceTestState(parent, child)

	context, err := buildResolvedReferenceContext(&state.Tasks[1], state, SupervisorConfig{ProjectRoot: repo}, "doer")
	if err != nil {
		t.Fatalf("buildResolvedReferenceContext: %v", err)
	}
	for _, text := range []string{"Story-owned behavior and AC.", "Shared inherited text."} {
		if count := strings.Count(context, text); count != 1 {
			t.Errorf("%q rendered %d times, want once:\n%s", text, count, context)
		}
	}
}

// Ancestor scalar carriers keep their declared references as pointers; the
// assigned carrier — the merged parent's plan — keeps them inlined.
func TestResolvedReferenceContextAncestorReferencesArePointers(t *testing.T) {
	repo := t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)
	writeReferenceFixture(t, repo, "specs/goal.md", "# Goal\n\n## Scope\nGOAL-SCOPE-TEXT\n")
	writeReferenceFixture(t, repo, "specs/story.md", "# Story\n\n## Acceptance\nSTORY-AC-TEXT\n")
	sourceRevision := commitReferenceFixture(t, repo, "test: add upstream sources")
	writeReferenceFixture(t, repo, "specs/epic.md", strictCarrier(sourceRevision, "SCOPE", "specs/goal.md", "Scope", "# Epic\n\nEPIC-LOCAL-TEXT\n"))
	writeReferenceFixture(t, repo, "specs/arch.md", strictCarrier(sourceRevision, "SCOPE", "specs/goal.md", "Scope", "# Architecture\n\nARCH-LOCAL-TEXT\n"))
	commitReferenceFixture(t, repo, "test: add ancestor carriers")
	base := testhelpers.MustGit(t, repo, "rev-parse", "main")
	writeReferenceFixture(t, repo, "specs/plan.md", strictCarrier(sourceRevision, "AC", "specs/story.md", "Acceptance", "# Plan\n\nPLAN-LOCAL-TEXT\n"))
	review := commitReferenceFixture(t, repo, "test: add reviewed plan")
	planner := models.Task{ID: "planner", Status: models.TaskStatusMerged, BaseCommit: &base, ReviewCommit: &review, MergeCommit: &review}
	plannerID := planner.ID
	coder := models.Task{ID: "coder", ParentTask: &plannerID, PlanRef: "specs/plan.md", ArchRef: "specs/arch.md", EpicRef: "specs/epic.md"}
	state := referenceTestState(planner, coder)

	context, err := buildResolvedReferenceContext(&state.Tasks[1], state, SupervisorConfig{ProjectRoot: repo}, "doer")
	if err != nil {
		t.Fatalf("buildResolvedReferenceContext: %v", err)
	}
	for _, text := range []string{"EPIC-LOCAL-TEXT", "ARCH-LOCAL-TEXT", "PLAN-LOCAL-TEXT", "STORY-AC-TEXT"} {
		if count := strings.Count(context, text); count != 1 {
			t.Errorf("%q rendered %d times, want once:\n%s", text, count, context)
		}
	}
	if strings.Contains(context, "GOAL-SCOPE-TEXT") {
		t.Errorf("ancestor reference rendered in full:\n%s", context)
	}
	pointer := `DIRECT REFERENCE "specs/goal.md#Scope" @ ` + sourceRevision + ` — not inlined; read with git show "` + sourceRevision + `:specs/goal.md" if needed`
	if count := strings.Count(context, pointer); count != 1 {
		t.Errorf("ancestor pointer rendered %d times, want once:\n%s", count, context)
	}
}

// With no merged parent, the most specific scalar carrier is the assigned
// artifact: its references render in full, less specific ones as pointers.
func TestResolvedReferenceContextMostSpecificScalarIsAssigned(t *testing.T) {
	repo := t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)
	writeReferenceFixture(t, repo, "specs/goal.md", "# Goal\n\n## Scope\nGOAL-SCOPE-TEXT\n")
	writeReferenceFixture(t, repo, "specs/story.md", "# Story\n\n## Acceptance\nSTORY-AC-TEXT\n")
	sourceRevision := commitReferenceFixture(t, repo, "test: add upstream sources")
	writeReferenceFixture(t, repo, "specs/epic.md", strictCarrier(sourceRevision, "SCOPE", "specs/goal.md", "Scope", "# Epic\n\nEPIC-LOCAL-TEXT\n"))
	writeReferenceFixture(t, repo, "specs/plan.md", strictCarrier(sourceRevision, "AC", "specs/story.md", "Acceptance", "# Plan\n\nPLAN-LOCAL-TEXT\n"))
	commitReferenceFixture(t, repo, "test: add scalar carriers")
	plannerID := "planner"
	planner := models.Task{ID: plannerID, Status: models.TaskStatus("REVIEW")}
	coder := models.Task{ID: "coder", ParentTask: &plannerID, PlanRef: "specs/plan.md", EpicRef: "specs/epic.md"}
	state := referenceTestState(planner, coder)

	context, err := buildResolvedReferenceContext(&state.Tasks[1], state, SupervisorConfig{ProjectRoot: repo}, "doer")
	if err != nil {
		t.Fatalf("buildResolvedReferenceContext: %v", err)
	}
	if count := strings.Count(context, "STORY-AC-TEXT"); count != 1 {
		t.Errorf("assigned plan reference rendered %d times, want once:\n%s", count, context)
	}
	if strings.Contains(context, "GOAL-SCOPE-TEXT") || !strings.Contains(context, `"specs/goal.md#Scope" @ `+sourceRevision+` — not inlined`) {
		t.Errorf("epic reference should be a pointer:\n%s", context)
	}
}

func TestResolvedReferenceContextParentReviewedRangeCompatibility(t *testing.T) {
	repo := t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)

	parentID := "legacy-parent"
	legacyParent := models.Task{ID: parentID, Status: models.TaskStatusMerged}
	child := models.Task{ID: "child", ParentTask: &parentID}
	state := referenceTestState(legacyParent, child)
	context, err := buildResolvedReferenceContext(&state.Tasks[1], state, SupervisorConfig{ProjectRoot: repo}, "doer")
	if err != nil || context != "" {
		t.Fatalf("all-absent legacy parent = (%q, %v), want empty legacy context", context, err)
	}

	partialBase := testhelpers.MustGit(t, repo, "rev-parse", "main")
	state.Tasks[0].BaseCommit = &partialBase
	_, err = buildResolvedReferenceContext(&state.Tasks[1], state, SupervisorConfig{ProjectRoot: repo}, "doer")
	if err == nil || !strings.Contains(err.Error(), "lacks reviewed change attribution") {
		t.Fatalf("partial parent attribution error = %v", err)
	}

	state.Tasks[0].Status = models.TaskStatus("SLICE_INTEGRATION_ANALYSIS_CLEAN")
	context, err = buildResolvedReferenceContext(&state.Tasks[1], state, SupervisorConfig{ProjectRoot: repo}, "doer")
	if err != nil || context != "" {
		t.Fatalf("non-merged parent context = (%q, %v), want excluded parent", context, err)
	}
}

func TestReconcileCarrierPrecedenceAndParentConflict(t *testing.T) {
	t.Parallel()

	context, err := referencecontract.RenderCarriers([]referencecontract.Carrier{
		{Path: "specs/carrier.md", Span: "scalar content\n", Revision: "head", Class: referencecontract.CarrierScalar, BlobOID: "scalar"},
		{Path: "specs/carrier.md", Span: "review content\n", Revision: "review", Class: referencecontract.CarrierReview, BlobOID: "review"},
	})
	if err != nil {
		t.Fatalf("reconcileAndRenderCarriers: %v", err)
	}
	if !strings.Contains(context, "review content") || strings.Contains(context, "scalar content") {
		t.Fatalf("review candidate did not win precedence:\n%s", context)
	}

	_, err = referencecontract.RenderCarriers([]referencecontract.Carrier{
		{Path: "specs/carrier.md", Span: "parent one\n", Revision: "one", Class: referencecontract.CarrierParent, BlobOID: "one"},
		{Path: "specs/carrier.md", Span: "parent two\n", Revision: "two", Class: referencecontract.CarrierParent, BlobOID: "two"},
	})
	if err == nil || !strings.Contains(err.Error(), "conflicting carrier provenance") {
		t.Fatalf("parent conflict error = %v", err)
	}
}

func TestReconcileCarrierDeduplicatesEquivalentDirectReferenceRevisions(t *testing.T) {
	t.Parallel()

	shared := referencecontract.Reference{Path: "specs/source.md", Heading: "Contract", Revision: "revision-one", BlobOID: "same-blob", Span: "## Contract\nshared span\n"}
	alias := shared
	alias.Revision = "revision-two"
	context, err := referencecontract.RenderCarriers([]referencecontract.Carrier{
		{Path: "specs/a.md", Span: "carrier a\n", Revision: "head", Class: referencecontract.CarrierScalar, BlobOID: "a", Refs: []referencecontract.Reference{shared}},
		{Path: "specs/b.md", Span: "carrier b\n", Revision: "head", Class: referencecontract.CarrierScalar, BlobOID: "b", Refs: []referencecontract.Reference{alias}},
	})
	if err != nil {
		t.Fatalf("reconcileAndRenderCarriers: %v", err)
	}
	if count := strings.Count(context, "shared span"); count != 1 {
		t.Fatalf("equivalent direct reference rendered %d times, want once:\n%s", count, context)
	}
}

func TestResolvedReferenceContextValidatesLosingParentBeforeReviewPrecedence(t *testing.T) {
	repo := t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)
	writeReferenceFixture(t, repo, "specs/source.md", "# Source\n\n## Shared\nCurrent source.\n")
	sourceRevision := commitReferenceFixture(t, repo, "test: add source")
	writeReferenceFixture(t, repo, "specs/carrier.md", strictCarrier(sourceRevision, "SHARED", "specs/source.md", "Shared", "# Carrier\n\nbase\n"))
	base := commitReferenceFixture(t, repo, "test: add base carrier")

	testhelpers.MustGit(t, repo, "checkout", "-b", "parent-review")
	writeReferenceFixture(t, repo, "specs/carrier.md", strictCarrier(sourceRevision, "SHARED", "specs/source.md", "Shared", "# Carrier\n\nstale parent\n"))
	parentReview := commitReferenceFixture(t, repo, "test: parent candidate")
	testhelpers.MustGit(t, repo, "checkout", "main")
	writeReferenceFixture(t, repo, "specs/carrier.md", strictCarrier(sourceRevision, "SHARED", "specs/source.md", "Shared", "# Carrier\n\ncurrent scalar\n"))
	commitReferenceFixture(t, repo, "test: current carrier")
	currentBase := testhelpers.MustGit(t, repo, "rev-parse", "main")
	testhelpers.MustGit(t, repo, "checkout", "-b", "current-review")
	writeReferenceFixture(t, repo, "specs/carrier.md", strictCarrier(sourceRevision, "SHARED", "specs/source.md", "Shared", "# Carrier\n\nwinning review\n"))
	currentReview := commitReferenceFixture(t, repo, "test: current review candidate")
	testhelpers.MustGit(t, repo, "checkout", "main")

	merge := parentReview
	parentID := "parent"
	parent := models.Task{ID: parentID, Status: models.TaskStatusMerged, BaseCommit: &base, ReviewCommit: &parentReview, MergeCommit: &merge}
	task := models.Task{ID: "review", SpecRef: "specs/carrier.md", ParentTask: &parentID, BaseCommit: &currentBase, ReviewCommit: &currentReview}
	state := referenceTestState(parent, task)
	_, err := buildResolvedReferenceContext(&state.Tasks[1], state, SupervisorConfig{ProjectRoot: repo}, "reviewer")
	if err == nil || !strings.Contains(err.Error(), "reviewed carrier \"specs/carrier.md\" is stale") {
		t.Fatalf("losing parent freshness error = %v", err)
	}
}

func TestResolvedReferenceContextCapturesIntegrationHeadOnce(t *testing.T) {
	t.Parallel()

	head := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	source := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	repo := &countingReferenceRepository{
		resolved: map[string]string{"main": head, source: source},
		blobs: map[string]string{
			head + ":specs/carrier.md":  strictCarrier(source, "OBL", "specs/source.md", "Contract", "# Carrier\n\nlocal\n"),
			source + ":specs/source.md": "# Source\n\n## Contract\ninherited\n",
		},
		oids: map[string]string{
			head + ":specs/carrier.md":  "carrier-oid",
			source + ":specs/source.md": "source-oid",
			head + ":specs/source.md":   "source-oid",
		},
	}
	task := models.Task{ID: "task", SpecRef: "specs/carrier.md"}
	state := referenceTestState(task)
	context, err := buildResolvedReferenceContextWithRepository(repo, &state.Tasks[0], state, SupervisorConfig{ProjectRoot: "/repo"}, "doer")
	if err != nil {
		t.Fatalf("buildResolvedReferenceContextWithRepository: %v", err)
	}
	if !strings.Contains(context, "inherited") {
		t.Fatalf("context missing resolved reference: %s", context)
	}
	if got := repo.resolveCalls["main"]; got != 1 {
		t.Fatalf("integration HEAD resolved %d times, want once", got)
	}

	base := head
	task.BaseCommit = &base
	_, err = buildResolvedReferenceContextWithRepository(repo, &task, state, SupervisorConfig{ProjectRoot: "/repo"}, "reviewer")
	if err == nil || !strings.Contains(err.Error(), "lacks BaseCommit..ReviewCommit attribution") {
		t.Fatalf("strict partial current-review attribution error = %v", err)
	}
}

func referenceTestState(tasks ...models.Task) *models.State {
	return &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Description: "Reference-first pilot", SpecRef: "specs/goal.md", Status: models.GoalStatusInProgress},
		Tasks:   tasks,
		Config:  models.Config{IntegrationBranch: "main"},
	}
}

func strictCarrier(sourceRevision, obligationID, sourcePath, heading, localContent string) string {
	return localContent + fmt.Sprintf("\n## Source References\nSource revision: %s\n\n### Direct References\n- %s: %s\n\n### Obligation Coverage\n- %s -> %s\n",
		strconv.Quote(sourceRevision), strconv.Quote("source"), strconv.Quote(sourcePath+"#"+heading), strconv.Quote(obligationID), strconv.Quote("source"))
}

func strictCarrierWithOverride(sourceRevision, overrideRevision string) string {
	return "# Goal\n\nLocal decomposition decision.\n\n## Source References\n" +
		"Source revision: " + strconv.Quote(sourceRevision) + "\n\n" +
		"### Direct References\n" +
		"- \"requirement\": \"specs/requirements.md#Règle `X` — débit\"\n" +
		"- \"decision\": \"specs/override.md#Human Decision\" @ " + strconv.Quote(overrideRevision) + "\n\n" +
		"### Obligation Coverage\n" +
		"- \"REQ-1\" -> \"requirement\"\n" +
		"- \"DEC-1\" -> \"decision\"\n"
}

func writeReferenceFixture(t *testing.T, repo, relativePath, content string) {
	t.Helper()
	path := filepath.Join(repo, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func commitReferenceFixture(t *testing.T, repo, message string) string {
	t.Helper()
	testhelpers.MustGit(t, repo, "add", "--all")
	testhelpers.MustGit(t, repo, "commit", "-m", message)
	return testhelpers.MustGit(t, repo, "rev-parse", "HEAD")
}

type countingReferenceRepository struct {
	resolved     map[string]string
	resolveCalls map[string]int
	blobs        map[string]string
	oids         map[string]string
	treeModes    map[string]string
	diffErr      error
}

func (r *countingReferenceRepository) ResolveCommit(ref string) (string, error) {
	if r.resolveCalls == nil {
		r.resolveCalls = make(map[string]int)
	}
	r.resolveCalls[ref]++
	resolved, ok := r.resolved[ref]
	if !ok {
		return "", fmt.Errorf("unknown revision %q", ref)
	}
	return resolved, nil
}

func (r *countingReferenceRepository) ReadBlob(revision, path string) (string, error) {
	content, ok := r.blobs[revision+":"+path]
	if !ok {
		return "", fmt.Errorf("missing blob %s:%s", revision, path)
	}
	return content, nil
}

func (r *countingReferenceRepository) BlobOID(revision, path string) (string, error) {
	oid, ok := r.oids[revision+":"+path]
	if !ok {
		return "", fmt.Errorf("missing blob OID %s:%s", revision, path)
	}
	return oid, nil
}

func (r *countingReferenceRepository) DiffFiles(_, _, _ string) ([]string, error) {
	return nil, r.diffErr
}

func (r *countingReferenceRepository) TreePathMode(treeish, path string) (string, bool, error) {
	key := treeish + ":" + path
	if mode, ok := r.treeModes[key]; ok {
		return mode, true, nil
	}
	if _, ok := r.blobs[key]; ok {
		return "100644", true, nil
	}
	return "", false, nil
}
