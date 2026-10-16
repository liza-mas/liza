package prompts

import (
	"regexp"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/pipeline"
)

// D-209: planners freeze oracles about delivered behavior (response shapes,
// read keys, refusal text, validation commands) that nobody ran, and coders
// then block on contradictions only a human can settle. Each author of frozen
// child assertions records a probe observed at an integration SHA, never with
// authority it lacks, and holds what it cannot probe; its reviewer rejects an
// unprobed delivered premise and re-runs one eligible probe. Epics state needs,
// not oracles about delivered code, so they carry neither duty.
func TestRealityProbesPlannerAndReviewerDuties(t *testing.T) {
	withPromptBrandValues(t, func() {
		brand.NameTitle = "Acme"
		brand.BinaryName = "acme-cli"
		brand.GlobalDirName = ".acme"
		brand.ProjectDirName = ".acme-run"
	})
	cfg, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		t.Fatal(err)
	}
	resolver := pipeline.NewResolver(cfg)
	rawDefaultBrand := regexp.MustCompile(`(?i)(^|[^A-Za-z])liza($|[^A-Za-z0-9])|\{\{binaryName\}\}`)

	author := []string{"## Reality Probes", "records the integration SHA", "A command need not finish",
		"Never run one needing a runtime input", "cite an existing gate receipt", "mark it UNPROBED",
		"hold the affected scope on an allocated investigation or correction", "a reason or owner alone is no hold",
		"missing access is no evidence against delivered behavior", "PROSPECTIVE with its producer named"}
	reviewer := []string{"| Reality probes |", "a hold of the affected scope", "delivered behavior is labelled PROSPECTIVE",
		"none eligible: say so", "Never re-run one needing a runtime input"}

	cases := []struct {
		role, roleType string
		root, direct   bool
		required       []string
	}{
		{"code-planner", "doer", false, false, author},
		{"architect", "doer", false, false, author},
		{"architect", "doer", false, true, author},
		{"architect", "doer", true, false, author},
		{"code-plan-reviewer", "reviewer", false, false, reviewer},
		{"architecture-reviewer", "reviewer", false, false, reviewer},
		{"architecture-reviewer", "reviewer", false, true, reviewer},
		{"epic-planner", "doer", false, false, nil},
		{"epic-plan-reviewer", "reviewer", false, false, nil},
	}
	for _, tc := range cases {
		name := tc.role
		if tc.root {
			name += "-root"
		}
		if tc.direct {
			name += "-direct"
		}
		t.Run(name, func(t *testing.T) {
			sections, err := resolver.ContextSections(tc.role)
			if err != nil {
				t.Fatal(err)
			}
			data := &RoleContextData{
				Role: tc.role, AgentID: tc.role + "-1", RoleType: tc.roleType,
				TaskID: "task-1", Description: "d", DoneWhen: "w", SpecRef: "specs/goal.md",
				Worktree: "/repo/.worktrees/task-1", IntegrationBranch: "main",
				BaseCommit: strings.Repeat("a", 40), ReviewCommit: strings.Repeat("b", 40),
				ArchRef: "specs/arch.md", PlanRef: "specs/plan.md", EpicRef: "specs/epic.md",
				GoalSlug: "goal", EpicSlug: "epic",
				DirectCodingAllocationAvailable: tc.direct,
			}
			if tc.root {
				data.DecompositionRoot = true
				data.MasterOutputRefField = "plan_ref"
				sections = append(sections, map[string]string{"doer": "master-decomposition-mandate", "reviewer": "master-decomposition-review"}[tc.roleType])
			}
			rendered, err := BuildRoleContext(tc.role, sections, data)
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range tc.required {
				if !strings.Contains(rendered, required) {
					t.Errorf("missing %q", required)
				}
			}
			if tc.required == nil && strings.Contains(strings.ToLower(rendered), "reality probes") {
				t.Error("epic roles render the reality-probe duty")
			}
			if m := rawDefaultBrand.FindString(rendered); m != "" {
				t.Errorf("non-default rendering leaks %q", m)
			}
		})
	}
}
