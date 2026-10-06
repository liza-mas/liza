package agent

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/embedded"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/referencecontract"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// The producer manifests, rather than manually assigned child refs, determine
// both planning prompts. Real earlier commits make same-file reference pins
// exercise the same validation boundary as reviewed architecture corrections.
func TestPlannerScopePromptRuntimeExpansion(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)
	statePath, _ := testhelpers.SetupLizaDir(t, repo)
	frozen := embedded.PipelineConfigContent()
	writeReferenceFixture(t, repo, paths.ProjectDirName()+"/pipeline.yaml", string(frozen))
	resolver := embeddedPipelineResolver(t)

	writeReferenceFixture(t, repo, "specs/requirements.md", "# Requirements\n\n## Required\nREQUIRED-OBLIGATION: retain task identity.\n\n## Other\nUNSELECTED-OBLIGATION: preserve another feature.\n")
	requirementsSHA := commitReferenceFixture(t, repo, "test: commit requirements before their consumers")
	goal := strictCarrier(requirementsSHA, "REQ-1", "specs/requirements.md", "Required", "# Goal\nANCESTOR-BULK\n"+strings.Repeat("ancestor detail\n", 4000))
	writeReferenceFixture(t, repo, "specs/goal.md", goal)
	commitReferenceFixture(t, repo, "test: commit the ancestor carrier")
	masterPath := "specs/master-architecture.md"
	shared := "## Shared\nSHARED-ARCHITECTURE: use the identity boundary.\n"
	writeReferenceFixture(t, repo, masterPath, "# Architecture\n\n"+shared)
	masterBase := commitReferenceFixture(t, repo, "test: establish a same-file shared reference pin")
	master := plannerScopeCarrier(requirementsSHA, masterPath, masterBase, shared,
		"## Scope 0: Foundation\n**Direct references:** []\nFOUNDATION-BULK\n"+strings.Repeat("foundation detail\n", 4000)+
			"\n## Scope 1: Assigned\n**Direct references:** [\"requirement\", \"shared\"]\nARCHITECT-ASSIGNED: freeze identity membership.\n")
	writeReferenceFixture(t, repo, masterPath, master)
	masterSHA := commitReferenceFixture(t, repo, "test: commit scoped master architecture")

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Goal.SpecRef = "specs/goal.md"
	state.Config.IntegrationBranch = "main"
	state.Sprint.Status = models.SprintStatusCompleted
	masterTask := testhelpers.BuildTaskByStatus("architecture-master", models.TaskStatus("ARCHITECTING_MAIN"), time.Now().UTC())
	masterTask.Type, masterTask.RolePair = models.TaskTypeArchitecture, "architecture-main-pair"
	masterTask.SpecRef = "specs/goal.md"
	masterTask.AssignedTo = testhelpers.StringPtr("architect-master")
	state.Tasks = []models.Task{masterTask}
	state.Sprint.Scope.Planned = []string{masterTask.ID}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	output := []models.OutputEntry{
		plannerScopeOutput("Plan foundation", masterPath+"#Scope 0: Foundation", "foundation.py"),
		plannerScopeOutput("Plan identity membership", masterPath+"#Scope 1: Assigned", "membership.py"),
	}
	children := plannerScopeExpand(t, repo, bb, masterTask.ID, "architect-master", "ARCHITECTING_MAIN", masterBase, masterSHA, "arch-decompose", output)
	if len(children) != 2 {
		t.Fatalf("architecture children = %v", children)
	}
	foundationID, architectID := children[0], children[1]

	// Scope 0 is deliberately not a dependency or interface provider edge.
	providerArchPath := "specs/foundation-architecture.md"
	providerArch := strictCarrier(requirementsSHA, "REQ-1", "specs/requirements.md", "Required", "# Foundation architecture\n\n## Scope 0: Runtime\nFoundation contract.\n")
	providerBase := masterSHA
	writeReferenceFixture(t, repo, providerArchPath, providerArch)
	providerArchSHA := commitReferenceFixture(t, repo, "test: commit provider architecture separately")
	foundationPlans := plannerScopeExpand(t, repo, bb, foundationID, "architect-foundation", "ARCHITECTING", providerBase, providerArchSHA, "architecture-to-code-plan",
		[]models.OutputEntry{plannerScopeOutput("Plan foundation implementation", providerArchPath+"#Scope 0: Runtime", "foundation.py")})
	if len(foundationPlans) != 1 {
		t.Fatalf("foundation plan children = %v", foundationPlans)
	}
	providerPlanPath := "specs/foundation-plan.md"
	var unitSections strings.Builder
	var units []models.OutputEntry
	for i := 1; i <= 26; i++ {
		heading := fmt.Sprintf("Unit %d", i)
		if i == 26 {
			heading = "Unit 26: Held observation"
		}
		fmt.Fprintf(&unitSections, "\n## %s\nProof allocation for foundation unit %d.\n", heading, i)
		unit := plannerScopeOutput(fmt.Sprintf("Implement foundation unit %d", i), providerArchPath+"#Scope 0: Runtime", fmt.Sprintf("foundation_%d.py", i))
		unit.PlanRef = providerPlanPath + "#" + heading
		units = append(units, unit)
	}
	providerPlan := strictCarrier(providerArchSHA, "REQ-1", providerArchPath, "Scope 0: Runtime", "# Foundation plan\n"+unitSections.String())
	writeReferenceFixture(t, repo, providerPlanPath, providerPlan)
	providerPlanSHA := commitReferenceFixture(t, repo, "test: commit provider plan at its own revision")
	unitIDs := plannerScopeExpand(t, repo, bb, foundationPlans[0], "code-planner-foundation", "CODE_PLANNING", providerArchSHA, providerPlanSHA, "code-plan-to-coding", units)
	if len(unitIDs) != 26 {
		t.Fatalf("generated unit count = %d, want 26", len(unitIDs))
	}

	readState := plannerScopeRead(t, bb)
	if got := readState.FindTask(architectID); got.ArchRef != output[1].ArchRef || len(got.DependsOn) != 0 {
		t.Fatalf("expanded architect ref/dependencies = %q/%v", got.ArchRef, got.DependsOn)
	}
	architectPrompt := plannerScopeBuild(t, repo, statePath, readState, resolver, architectID, models.RoleArchitect)
	plannerScopeAssertPrompt(t, architectPrompt, "ARCHITECT-ASSIGNED", foundationID, providerArchPath, providerArchSHA, providerPlanPath, providerPlanSHA, unitIDs)
	assertNotContains(t, architectPrompt, "FOUNDATION-BULK")

	// The specialized architecture itself also refers to a shared section from
	// an earlier same-file commit, then expands through the real transition.
	specialPath := "specs/membership-architecture.md"
	writeReferenceFixture(t, repo, specialPath, "# Membership architecture\n\n"+shared)
	specialBase := commitReferenceFixture(t, repo, "test: pin specialized shared architecture first")
	special := plannerScopeCarrier(requirementsSHA, specialPath, specialBase, shared,
		"## Scope 2: Membership\n**Direct references:** [\"requirement\", \"shared\"]\nCODE-PLANNER-ASSIGNED: allocate identity proofs.\n\n"+
			"## Scope 3: Peer\nPEER-BULK\n"+strings.Repeat("peer detail\n", 4000))
	writeReferenceFixture(t, repo, specialPath, special)
	specialSHA := commitReferenceFixture(t, repo, "test: commit specialized architecture for expansion")
	plannerIDs := plannerScopeExpand(t, repo, bb, architectID, "architect-membership", "ARCHITECTING", specialBase, specialSHA, "architecture-to-code-plan",
		[]models.OutputEntry{plannerScopeOutput("Allocate identity proofs", specialPath+"#Scope 2: Membership", "membership.py")})
	if len(plannerIDs) != 1 {
		t.Fatalf("code planner children = %v", plannerIDs)
	}
	readState = plannerScopeRead(t, bb)
	if ref := readState.FindTask(plannerIDs[0]).ArchRef; ref != specialPath+"#Scope 2: Membership" {
		t.Fatalf("expanded planner arch_ref = %q", ref)
	}
	plannerPrompt := plannerScopeBuild(t, repo, statePath, readState, resolver, plannerIDs[0], models.RoleCodePlanner)
	plannerScopeAssertPrompt(t, plannerPrompt, "CODE-PLANNER-ASSIGNED", foundationID, providerArchPath, providerArchSHA, providerPlanPath, providerPlanSHA, unitIDs)
	assertNotContains(t, plannerPrompt, "PEER-BULK")
	assertNotContains(t, plannerPrompt, "FOUNDATION-BULK")
	t.Logf("complete corrected fixture prompt bytes: architect=%d code-planner=%d", len(architectPrompt), len(plannerPrompt))

	// Absence of the declaration retains the approved legacy presentation.
	legacySpecial := strings.Replace(special, "**Direct references:** [\"requirement\", \"shared\"]\n", "", 1)
	writeReferenceFixture(t, repo, specialPath, legacySpecial)
	commitReferenceFixture(t, repo, "test: observe legacy carrier without a section read set")
	legacyPrompt := plannerScopeBuild(t, repo, statePath, readState, resolver, plannerIDs[0], models.RoleCodePlanner)
	assertContainsAll(t, legacyPrompt, "ANCESTOR-BULK", "CODE-PLANNER-ASSIGNED")
	if len(plannerPrompt) >= len(legacyPrompt) {
		t.Fatalf("scoped complete prompt %d bytes did not shrink legacy %d bytes", len(plannerPrompt), len(legacyPrompt))
	}
	t.Logf("complete legacy fixture code-planner bytes=%d", len(legacyPrompt))

	// Unselected references remain validation inputs: review-time drift still
	// refuses submission, while inherited drift remains individually disclosed.
	writeReferenceFixture(t, repo, "specs/requirements.md", "# Requirements\n\n## Required\nREQUIRED-OBLIGATION: retain task identity.\n\n## Other\nUNSELECTED-OBLIGATION: changed independently.\n")
	driftSHA := commitReferenceFixture(t, repo, "test: drift an unselected obligation")
	_, err := referencecontract.LoadDiffCarriers(gitpkg.New(repo), repo, masterBase, masterSHA, driftSHA, referencecontract.CarrierReview, false, nil)
	if err == nil || !strings.Contains(err.Error(), `direct reference "other" is stale`) {
		t.Fatalf("unselected reviewed reference drift error = %v", err)
	}
	writeReferenceFixture(t, repo, specialPath, special)
	commitReferenceFixture(t, repo, "test: restore the reviewed section read set")
	driftPrompt := plannerScopeBuild(t, repo, statePath, readState, resolver, plannerIDs[0], models.RoleCodePlanner)
	assertContainsAll(t, driftPrompt, "section changed since its pinned revision", requirementsSHA, "specs/requirements.md#Other")
	assertNotContains(t, driftPrompt, "UNSELECTED-OBLIGATION: changed independently.")
}

func plannerScopeCarrier(sourceSHA, path, sharedSHA, shared, scopes string) string {
	return fmt.Sprintf("# Architecture\n\n## Source References\nSource revision: %q\n\n### Direct References\n- \"requirement\": \"specs/requirements.md#Required\"\n- \"shared\": %q @ %q\n- \"other\": \"specs/requirements.md#Other\"\n\n### Obligation Coverage\n- \"REQ-1\" -> \"requirement\"\n- \"SHARED-1\" -> \"shared\"\n- \"OTHER-1\" -> \"other\"\n\n%s%s", sourceSHA, path+"#Shared", sharedSHA, shared, scopes)
}

func plannerScopeOutput(description, archRef, file string) models.OutputEntry {
	rca := false
	return models.OutputEntry{Desc: description, DoneWhen: "Named obligations have concrete proof allocations", Scope: file, SpecRef: "specs/goal.md", ArchRef: archRef, RCARequired: &rca,
		Decomposition: &models.DecompositionManifest{OwnedFiles: []string{file}, CoverageNotes: "REQ-1 identity boundary"}}
}

func plannerScopeExpand(t *testing.T, repo string, bb *db.Blackboard, taskID, agentID, executing, base, commit, transition string, output []models.OutputEntry) []string {
	t.Helper()
	if _, err := referencecontract.LoadDiffCarriers(gitpkg.New(repo), repo, base, commit, commit, referencecontract.CarrierReview, false, nil); err != nil {
		t.Fatalf("validate committed producer %s: %v", taskID, err)
	}
	for _, entry := range output {
		for _, ref := range []string{entry.SpecRef, entry.ArchRef, entry.PlanRef} {
			if err := ops.ResolveRefFragmentAt(gitpkg.New(repo), commit, ref); err != nil {
				t.Fatalf("validate output anchor %s: %v", ref, err)
			}
		}
	}
	if err := bb.Modify(func(s *models.State) error {
		task := s.FindTask(taskID)
		task.Status, task.AssignedTo = models.TaskStatus(executing), &agentID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := ops.SetTaskOutput(repo, &ops.SetTaskOutputInput{TaskID: taskID, AgentID: agentID, Output: output}); err != nil {
		t.Fatalf("SetTaskOutput(%s): %v", taskID, err)
	}
	// Review/merge records are fixture setup; output persistence and expansion
	// run through their real operations rather than fabricating child tasks.
	if err := bb.Modify(func(s *models.State) error {
		task := s.FindTask(taskID)
		task.Status = models.TaskStatusMerged
		task.BaseCommit, task.ReviewCommit, task.MergeCommit = &base, &commit, &commit
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if transition == "arch-decompose" {
		results, err := ops.ExecuteAvailableTransitions(repo, "auto")
		if err != nil {
			t.Fatalf("ExecuteAvailableTransitions(%s): %v", taskID, err)
		}
		for _, result := range results {
			if result.SourceTaskID == taskID && result.TransitionName == transition {
				return result.ChildTaskIDs
			}
		}
		t.Fatalf("automatic %s produced no expansion for %s", transition, taskID)
	}
	result, err := ops.Proceed(repo, taskID, transition)
	if err != nil {
		t.Fatalf("Proceed(%s, %s): %v", taskID, transition, err)
	}
	return result.ChildTaskIDs
}

func plannerScopeRead(t *testing.T, bb *db.Blackboard) *models.State {
	t.Helper()
	s, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func plannerScopeBuild(t *testing.T, repo, statePath string, state *models.State, resolver *pipeline.Resolver, taskID, role string) string {
	t.Helper()
	// Model the claimed task's local checkout so generated navigation commands
	// address this isolated repository, rather than an unclaimed child's nil path.
	worktree := "."
	state.FindTask(taskID).Worktree = &worktree
	prompt, err := buildPromptWithContext(state, SupervisorConfig{AgentID: role + "-1", Role: role, ProjectRoot: repo, SpecsDir: filepath.Join(repo, "specs"), StatePath: statePath}, taskID, resolver)
	if err != nil {
		t.Fatalf("build complete %s prompt: %v", role, err)
	}
	assertContainsAll(t, prompt, brand.Command("get"))
	assertNotContains(t, prompt, "{{brand.")
	assertNotContains(t, prompt, "git -C ''")
	return prompt
}

func plannerScopeAssertPrompt(t *testing.T, prompt, assigned, providerID, archPath, archSHA, planPath, planSHA string, unitIDs []string) {
	t.Helper()
	assertContainsAll(t, prompt, assigned, "REQUIRED-OBLIGATION", "SHARED-ARCHITECTURE", providerID,
		archPath, planPath, "show '"+archSHA+":"+archPath+"'", "show '"+planSHA+":"+planPath+"'", "Unit 26: Held observation")
	assertNotContains(t, prompt, "ANCESTOR-BULK")
	assertNotContains(t, prompt, "UNSELECTED-OBLIGATION")
	for _, id := range unitIDs {
		assertContainsAll(t, prompt, id+" [DRAFT_CODE]")
	}
}
