package prompts

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// FR10: authors learn of the local gate check from their prompt. The pointer
// renders exactly once for the coder (with and without an acceptance source),
// the integration analyst and the four planning doers, and never for a
// reviewer, who has nothing to submit.
func TestCheckSubmissionPointerRendersOncePerDoerPrompt(t *testing.T) {
	withPromptBrandValues(t, func() { brand.BinaryName = "acme-cli" })
	cfg, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		t.Fatal(err)
	}
	resolver := pipeline.NewResolver(cfg)
	source := &models.AcceptanceSource{Ref: "specs/plan.md#Task", Commit: "c", Blob: "b", ParentTask: "plan-1", ParentReviewCommit: "p"}

	cases := []struct {
		name, role, roleType string
		source               *models.AcceptanceSource
		want                 int
	}{
		{"coder-with-acceptance-source", "coder", "doer", source, 1},
		{"coder-without-acceptance-source", "coder", "doer", nil, 1},
		{"integration-analyst", "integration-analyst", "doer", nil, 1},
		{"code-planner", "code-planner", "doer", nil, 1},
		{"epic-planner", "epic-planner", "doer", nil, 1},
		{"us-writer", "us-writer", "doer", nil, 1},
		{"architect", "architect", "doer", nil, 1},
		{"code-reviewer", "code-reviewer", "reviewer", nil, 0},
	}
	const commandLine = "acme-cli check-submission task-1"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
				GoalSlug: "goal", EpicSlug: "epic", AcceptanceSource: tc.source,
			}
			rendered, err := BuildRoleContext(tc.role, sections, data)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(rendered, commandLine); got != tc.want {
				t.Errorf("command line %q renders %d times, want %d", commandLine, got, tc.want)
			}
			if got := strings.Count(rendered, "check-submission"); got != tc.want {
				t.Errorf("check-submission mentioned %d times, want %d", got, tc.want)
			}
		})
	}
}
