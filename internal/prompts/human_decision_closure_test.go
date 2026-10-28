package prompts

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/pipeline"
)

// D-94: these tests establish rendered duties and routing for settling
// human-owned decisions before freeze and for reviewed corrective-writer
// placement. They do not prove agent compliance or a lower rework rate.

const humanActionOption = `[--human-action "<decision only a human can make>"]`

var correctivePlacementDuty = []string{
	"Correction writer (fixes a provider or contract other writers rely on)",
	"its plan or architecture carries `## Placement` under the governing rule's correction case",
	"predecessors = every started or merged writer, as declared waits",
	"successors = the unstarted writers its done_when names",
	"the earliest slot after those predecessors and before those successors that the rule allows, clear of fixed and merged slots",
	"No correction case, no legal slot, or a started successor → mark-blocked --human-action",
	"Ordinary later writers keep the rule's admission default",
}

const correctivePlacementRow = "| Corrective placement | A correction writer in an ordered shared-file chain lacks Placement, omits a started or merged predecessor or a named successor, breaks a fixed or merged slot or the rule, or is not the earliest legal slot |"

func renderRoleForTest(t *testing.T, role, roleType string, root, direct bool) string {
	t.Helper()
	cfg, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		t.Fatal(err)
	}
	sections, err := pipeline.NewResolver(cfg).ContextSections(role)
	if err != nil {
		t.Fatal(err)
	}
	if root {
		sections = append(sections, map[string]string{"doer": "master-decomposition-mandate", "reviewer": "master-decomposition-review"}[roleType])
	}
	rendered, err := BuildRoleContext(role, sections, &RoleContextData{
		Role: role, AgentID: role + "-1", RoleType: roleType,
		TaskID: "task-1", Worktree: "/repo/.worktrees/task-1", IntegrationBranch: "main",
		SpecRef: "specs/goal.md", ArchRef: "specs/arch.md#Scope", EpicRef: "specs/epic.md",
		GoalSlug: "goal", BaseCommit: strings.Repeat("a", 40), ReviewCommit: strings.Repeat("b", 40),
		DecompositionRoot: root, MasterOutputRefField: "arch_ref",
		DirectCodingAllocationAvailable: direct,
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(strings.Fields(rendered), " ")
}

func TestPlanningDoersRenderHumanActionChannel(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"architect", "code-planner", "epic-planner", "us-writer"} {
		text := renderRoleForTest(t, role, "doer", false, false)
		for _, want := range []string{"--human-action raises AWAITING HUMAN", humanActionOption} {
			if !strings.Contains(text, want) {
				t.Errorf("%s missing %q", role, want)
			}
		}
	}
	// The coder keeps its existing blocking-protocol channel unchanged.
	if text := renderRoleForTest(t, "coder", "doer", false, false); strings.Contains(text, humanActionOption) {
		t.Errorf("coder tools changed: %q present", humanActionOption)
	}
}

func TestReviewersRenderHumanOwnedDecisionRow(t *testing.T) {
	t.Parallel()
	row := "| Human-owned decision | An output relies on a human-owned decision without a recorded ruling or a declared wait on an allocated investigation, correction or decision; asking is no hold |"
	for _, tc := range []struct {
		role        string
		writerOrder bool
	}{
		{"code-plan-reviewer", false},
		{"epic-plan-reviewer", false},
		{"architecture-reviewer", true},
	} {
		text := renderRoleForTest(t, tc.role, "reviewer", false, false)
		if !strings.Contains(text, row) {
			t.Errorf("%s missing human-owned decision row", tc.role)
		}
		hasWriterOrder := strings.Contains(text, "| Writer-order completeness | A rule ordering shared-file writers gives no position for a later correction writer and names no human ruling |")
		if hasWriterOrder != tc.writerOrder {
			t.Errorf("%s writer-order completeness row present=%v, want %v", tc.role, hasWriterOrder, tc.writerOrder)
		}
	}
	architect := renderRoleForTest(t, "architect", "doer", false, false)
	if !strings.Contains(architect, "A rule ordering shared-file writers states where a later correction writer goes, or its human ruling.") {
		t.Error("architect shared-file audit missing writer-order completeness duty")
	}
}

func TestCorrectivePlacementRendersForBothAllocators(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, role, roleType string
		root, direct         bool
		duty, row            bool
	}{
		{"code-planner", "code-planner", "doer", false, false, true, false},
		{"code-planner-direct-topology", "code-planner", "doer", false, true, true, false},
		{"architect-direct", "architect", "doer", false, true, true, false},
		{"architect-legacy", "architect", "doer", false, false, false, false},
		{"master-architect-direct", "architect", "doer", true, true, true, false},
		{"master-architect-legacy", "architect", "doer", true, false, false, false},
		{"code-plan-reviewer", "code-plan-reviewer", "reviewer", false, false, false, true},
		{"architecture-reviewer-direct", "architecture-reviewer", "reviewer", false, true, false, true},
		{"architecture-reviewer-legacy", "architecture-reviewer", "reviewer", false, false, false, false},
		{"master-architecture-reviewer-direct", "architecture-reviewer", "reviewer", true, true, false, true},
		{"master-architecture-reviewer-legacy", "architecture-reviewer", "reviewer", true, false, false, false},
	} {
		text := renderRoleForTest(t, tc.role, tc.roleType, tc.root, tc.direct)
		for _, want := range correctivePlacementDuty {
			if got := strings.Contains(text, want); got != tc.duty {
				t.Errorf("%s: placement duty %q present=%v, want %v", tc.name, want, got, tc.duty)
			}
		}
		if got := strings.Contains(text, correctivePlacementRow); got != tc.row {
			t.Errorf("%s: corrective placement row present=%v, want %v", tc.name, got, tc.row)
		}
	}
}

func TestBlockedWakeLeavesCorrectivePlacementToReviewedPlanner(t *testing.T) {
	t.Parallel()
	rendered, err := RenderWakeInstructions("BLOCKED_TASKS", "orchestrator-1", testMatcherText())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered, "never leave the order to its planner") {
		t.Error("blocked wake still withholds placement from the planner")
	}
	for _, want := range []string{
		"commission it alone (add-tasks: max_outputs, reserve_successors naming the unstarted writers it precedes, which await all its children)",
		"must cover all max_outputs",
		"Started writer or no rule correction case → assess-blocked --human-action; else planner records reviewed Placement",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("blocked wake missing %q", want)
		}
	}
}

func TestHumanOwnedDecisionAuthoringContract(t *testing.T) {
	t.Parallel()
	for path, wants := range map[string][]string{
		"../../skills/shared/references/reference-first-authoring.md": {
			"human-owned one (behavior, NFR, policy, a rule's missing case) is a Design prerequisite of its affected outputs; asking neither settles nor holds it.",
			"| Design prerequisite | Resolve before affected outputs freeze, or allocate a bounded investigation, correction or decision they legally await. |",
			"Unsettled needs are Design prerequisites, not frozen gap notes.",
		},
		"../../skills/architecture-planning/SKILL.md": {
			"- [ ] Provider asks, freeze duty, human-owned prerequisites met",
			"unresolved needs are Design prerequisites, not gap notes.",
		},
		"../../skills/architecture-planning/references/arch-plan-format.md": {
			"*Owner*: human | <stage>",
			"*Impact*: <affected outputs>",
			"*Settles*: <stage | decision ref | output indexes awaiting its allocated investigation, correction or decision>",
		},
	} {
		text := strings.Join(strings.Fields(readContractFixture(t, path)), " ")
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s missing %q", path, want)
			}
		}
	}
}
