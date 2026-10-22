package prompts

import (
	"regexp"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/pipeline"
)

// D-121: coders submitted negative and isolation proofs that stayed green with
// the guarded rule removed: the fixture tripped another rule first, never
// reached the state the rule decides, or derived its oracle from the input.
// Review caught them one rejection at a time. The coder now checks, before
// submit, that each such proof fails at its intended assertion without its
// refusal path and passes once restored, or argues why it cannot pass
// otherwise; the testing skill states what the fixture must do. Only the coder
// carries the pre-submit duty.
func TestDiscriminatingNegativeProofsCoderDuty(t *testing.T) {
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

	coder := []string{"name the refusal path", "the intended assertion fail for that reason",
		"not an error or another assertion", "restore it and see the proof green", "argue from the fixture"}

	cases := []struct {
		role, roleType string
		required       []string
	}{
		{"coder", "doer", coder},
		{"code-reviewer", "reviewer", nil},
		{"code-planner", "doer", nil},
		{"architect", "doer", nil},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
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
			rendered, err := BuildRoleContext(tc.role, sections, data)
			if err != nil {
				t.Fatal(err)
			}
			flat := strings.Join(strings.Fields(rendered), " ")
			for _, required := range tc.required {
				if !strings.Contains(flat, required) {
					t.Errorf("missing %q", required)
				}
			}
			if tc.required == nil && strings.Contains(flat, "name the refusal path") {
				t.Errorf("%s renders the coder's pre-submit proof check", tc.role)
			}
			if m := rawDefaultBrand.FindString(rendered); m != "" {
				t.Errorf("non-default rendering leaks %q", m)
			}
		})
	}

	skill := strings.Join(strings.Fields(readContractFixture(t, "../../skills/testing/SKILL.md")), " ")
	for _, required := range []string{"guards one refusal path", "breaks only that path", "not derived from the input under test"} {
		if !strings.Contains(skill, required) {
			t.Errorf("testing skill missing %q", required)
		}
	}
}
