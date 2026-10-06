package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestMasterReferenceContextSecondHopPointers(t *testing.T) {
	for _, role := range []string{models.RoleArchitect, models.RoleArchitectureReviewer} {
		t.Run(role, func(t *testing.T) {
			repo, state, sourceRevision := masterReferenceFixture(t, 1)
			config := SupervisorConfig{ProjectRoot: repo, Role: role, AgentID: role + "-1", SpecsDir: "specs"}
			prompt, err := buildPromptWithContext(state, config, "master", embeddedPipelineResolver(t))
			if err != nil {
				t.Fatalf("master prompt: %v", err)
			}
			for _, text := range []string{"EPIC-LOCAL", "EPIC-REQUIRED", "STORY-0-LOCAL", "STORY-1-LOCAL", "## Source References"} {
				if !strings.Contains(prompt, text) {
					t.Errorf("master prompt missing required carrier/authority %q", text)
				}
			}
			for i := range 2 {
				if strings.Contains(prompt, fmt.Sprintf("CHILD-%d-ONLY", i)) {
					t.Errorf("child %d second-hop span was inlined", i)
				}
				pointer := fmt.Sprintf("DIRECT REFERENCE \"specs/source.md#Child %d\" @ %s — not inlined; read with git show '%s:specs/source.md' if needed", i, sourceRevision, sourceRevision)
				if !strings.Contains(prompt, pointer) {
					t.Errorf("missing pinned second-hop pointer for child %d", i)
				}
			}
			if role == models.RoleArchitectureReviewer {
				if got := strings.Count(prompt, "MASTER-SHARED-REQUIRED"); got != 1 {
					t.Errorf("master-declared span shared with child rendered %d times, want once", got)
				}
				if !strings.Contains(prompt, "MASTER-LOCAL") {
					t.Error("current-review master carrier missing")
				}
			}

			// The same parent range remains full context for an ordinary task.
			state.Tasks[1].RolePair = "architecture-pair"
			ordinary, err := buildPromptWithContext(state, config, "master", embeddedPipelineResolver(t))
			if err != nil {
				t.Fatalf("ordinary prompt: %v", err)
			}
			for i := range 2 {
				if !strings.Contains(ordinary, fmt.Sprintf("CHILD-%d-ONLY", i)) {
					t.Errorf("ordinary task lost assigned parent reference %d", i)
				}
			}
		})
	}
}

func TestMasterReferenceContextCustomTopology(t *testing.T) {
	for _, role := range []string{models.RoleCodePlanner, models.RoleCodePlanReviewer} {
		t.Run(role, func(t *testing.T) {
			repo, state, _ := masterReferenceFixture(t, 1)
			state.Tasks[1].RolePair = "custom-main-pair"
			config := SupervisorConfig{ProjectRoot: repo, Role: role, AgentID: role + "-1", SpecsDir: "specs"}
			// The shared fixture omits reviewer sections because its original
			// tests only need the root mandate. Include the review task here so
			// this presentation test actually renders resolved references.
			pipelineYAML := strings.Replace(customMasterRefPromptPipelineYAML,
				"allowed-operations: [submit-verdict]",
				"allowed-operations: [submit-verdict]\n      context-sections: [review-task]", 1)
			prompt, err := buildPromptWithContext(state, config, "master", loadTestResolver(t, pipelineYAML))
			if err != nil {
				t.Fatalf("custom master prompt: %v", err)
			}
			if strings.Contains(prompt, "CHILD-0-ONLY") || !strings.Contains(prompt, `"specs/source.md#Child 0"`) {
				t.Error("custom decomposition root did not render child reference as a pointer")
			}
		})
	}
}

func TestMasterReferenceContextHiddenReferenceValidation(t *testing.T) {
	for _, mutation := range []string{"missing pin", "missing heading", "drift"} {
		t.Run(mutation, func(t *testing.T) {
			repo, state, sourceRevision := masterReferenceFixture(t, 1)
			var expectedError string
			switch mutation {
			case "missing pin":
				writeReferenceFixture(t, repo, "specs/story-0.md", strictCarrier(strings.Repeat("0", 40), "CHILD", "specs/source.md", "Child 0", "# Story\n"))
				expectedError = "resolve commit"
			case "missing heading":
				writeReferenceFixture(t, repo, "specs/story-0.md", strictCarrier(sourceRevision, "CHILD", "specs/source.md", "Missing Section", "# Story\n"))
				expectedError = "Missing Section"
			case "drift":
				writeReferenceFixture(t, repo, "specs/source.md", "# Source\n\n## Epic\nEPIC-REQUIRED\n\n## Shared\nMASTER-SHARED-REQUIRED\n\n## Child 0\nCHILD-DRIFTED\n\n## Child 1\nCHILD-1-ONLY\n")
			}
			commitReferenceFixture(t, repo, "test: mutate inherited child authority")
			config := SupervisorConfig{ProjectRoot: repo, Role: models.RoleArchitect, AgentID: "architect-1", SpecsDir: "specs"}
			prompt, err := buildPromptWithContext(state, config, "master", embeddedPipelineResolver(t))
			if expectedError != "" {
				if err == nil || !strings.Contains(err.Error(), expectedError) {
					t.Fatalf("hidden reference error = %v, want %q", err, expectedError)
				}
				return
			}
			if err != nil {
				t.Fatalf("disclosed drift prompt: %v", err)
			}
			if strings.Contains(prompt, "CHILD-DRIFTED") {
				t.Error("drifted child-only reference was inlined")
			}
			if !strings.Contains(prompt, `"specs/source.md#Child 0"`) || !strings.Contains(prompt, "section changed since its pinned revision "+sourceRevision) || !strings.Contains(prompt, "git diff ") {
				t.Error("hidden child reference lost its drift disclosure/comparison")
			}
		})
	}
}

func TestMasterPromptReferenceBytes(t *testing.T) {
	for _, role := range []string{models.RoleArchitect, models.RoleArchitectureReviewer} {
		t.Run(role, func(t *testing.T) {
			repo, state, _ := masterReferenceFixture(t, 10000)
			config := SupervisorConfig{ProjectRoot: repo, Role: role, AgentID: role + "-1", SpecsDir: "specs"}
			state.Tasks[1].RolePair = "architecture-pair"
			before, err := buildPromptWithContext(state, config, "master", embeddedPipelineResolver(t))
			if err != nil {
				t.Fatal(err)
			}
			state.Tasks[1].RolePair = "architecture-main-pair"
			after, err := buildPromptWithContext(state, config, "master", embeddedPipelineResolver(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("complete %s prompt: full-parent baseline=%d bytes; master=%d bytes", role, len(before), len(after))
			if len(before) < 450000 || len(after) >= 450000 || len(after)*2 >= len(before) {
				t.Errorf("expected high-fan-out master prompt below 450KB with >50%% reduction: before=%d after=%d", len(before), len(after))
			}
			for _, text := range []string{"EPIC-LOCAL", "EPIC-REQUIRED", "STORY-0-LOCAL", "STORY-1-LOCAL", `"specs/source.md#Child 0"`, `"specs/source.md#Child 1"`} {
				if !strings.Contains(after, text) {
					t.Errorf("size reduction lost %q", text)
				}
			}
		})
	}
}

// masterReferenceFixture models a merged epic whose reviewed range contains
// child stories, followed by a master submission declaring one shared span.
// repeat scales only child-only external authority, never required carrier text.
func masterReferenceFixture(t *testing.T, repeat int) (string, *models.State, string) {
	t.Helper()
	repo := t.TempDir()
	testhelpers.SetupTestGitRepo(t, repo)
	source := "# Source\n\n## Epic\nEPIC-REQUIRED\n\n## Shared\nMASTER-SHARED-REQUIRED\n"
	for i := range 2 {
		source += fmt.Sprintf("\n## Child %d\n", i) + strings.Repeat(fmt.Sprintf("CHILD-%d-ONLY external authority padding.\n", i), repeat)
	}
	writeReferenceFixture(t, repo, "specs/source.md", source)
	sourceRevision := commitReferenceFixture(t, repo, "test: source authority")
	writeReferenceFixture(t, repo, "specs/epic.md", strictCarrier(sourceRevision, "EPIC", "specs/source.md", "Epic", "# Epic\n\nEPIC-LOCAL\n"))
	for i := range 2 {
		story := strictCarrier(sourceRevision, "CHILD", "specs/source.md", fmt.Sprintf("Child %d", i), fmt.Sprintf("# Story\n\nSTORY-%d-LOCAL\n", i))
		if i == 0 {
			story = strings.Replace(story, "### Direct References\n", "### Direct References\n- \"shared\": \"specs/source.md#Shared\"\n", 1)
		}
		writeReferenceFixture(t, repo, fmt.Sprintf("specs/story-%d.md", i), story)
	}
	parentReview := commitReferenceFixture(t, repo, "test: epic and child stories")
	parent := models.Task{ID: "parent", Status: models.TaskStatusMerged, BaseCommit: &sourceRevision, ReviewCommit: &parentReview, MergeCommit: &parentReview}
	testhelpers.MustGit(t, repo, "checkout", "-b", "master-review")
	writeReferenceFixture(t, repo, "specs/master.md", strictCarrier(sourceRevision, "SHARED", "specs/source.md", "Shared", "# Master\n\nMASTER-LOCAL\n"))
	masterReview := commitReferenceFixture(t, repo, "test: master declares shared authority")
	testhelpers.MustGit(t, repo, "checkout", "main")
	master := models.Task{ID: "master", RolePair: "architecture-main-pair", ParentTasks: []string{parent.ID}, EpicRef: "specs/epic.md", BaseCommit: &parentReview, ReviewCommit: &masterReview, Status: models.TaskStatus("ARCHITECTING"), Description: "Decompose the epic", DoneWhen: "All stories assigned", Scope: "Epic"}
	return repo, referenceTestState(parent, master), sourceRevision
}
