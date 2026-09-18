package prompts

import (
	"regexp"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/pipeline"
)

// The prevent-blocked-task-chains instruction release: every planning role
// receives its handoff, priority, and commitment duties, rendered under a
// non-default brand with no raw default-brand literal.
func TestBlockedTaskChainsPlannerAndReviewerDuties(t *testing.T) {
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

	const findingRule = "A finding cannot create a commitment"
	rawDefaultBrand := regexp.MustCompile(`(?i)(^|[^A-Za-z])liza($|[^A-Za-z0-9])|\{\{binaryName\}\}`)
	handoffRows := []string{"| Handoff usable |", "| Priority propagation |", "| Consequential commitment |", "| Proof stage |"}

	cases := []struct {
		role     string
		roleType string
		root     bool
		required []string
	}{
		{"epic-planner", "doer", false, []string{"keep their priority at item level", "expose a missing product policy"}},
		{"us-writer", "doer", false, []string{"Carry each story's inherited priority", "cannot fail a Must story"}},
		{"architect", "doer", false, []string{"minimum mechanism Must scope", "targeted probe", "never a pre-coding gate without a concrete dependency reason"}},
		{"code-planner", "doer", false, []string{"effective priority", "state in the plan what the input supplies", "must not inherit its whole barrier"}},
		{"architect", "doer", true, []string{"group a shared uncertainty"}},
		{"code-planner", "doer", true, []string{"group a shared uncertainty"}},
		{"epic-plan-reviewer", "reviewer", false, append([]string{findingRule}, handoffRows...)},
		{"us-reviewer", "reviewer", false, []string{findingRule, "inherited priority", "without inventing policy"}},
		{"architecture-reviewer", "reviewer", false, append([]string{findingRule, "| Minimal Must mechanism |"}, handoffRows...)},
		{"code-plan-reviewer", "reviewer", false, append([]string{findingRule, "| Dependency meaning |"}, handoffRows...)},
		{"architecture-reviewer", "reviewer", true, []string{"7. Priority and uncertainty."}},
		{"code-plan-reviewer", "reviewer", true, []string{"7. Priority and uncertainty."}},
		{"code-reviewer", "reviewer", false, []string{findingRule, "label the finding a contract defect naming the owning artifact and role"}},
	}
	for _, tc := range cases {
		name := tc.role
		if tc.root {
			name += "-root"
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
			if m := rawDefaultBrand.FindString(rendered); m != "" {
				t.Errorf("non-default rendering leaks %q", m)
			}
		})
	}
}
