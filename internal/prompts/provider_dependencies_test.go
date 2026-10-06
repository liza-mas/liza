package prompts

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestProviderDependencyGuidanceBrandedRolePrompts(t *testing.T) {
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
	for _, tc := range []struct {
		role     string
		roleType string
		root     bool
	}{
		{"architect", "doer", false},
		{"code-planner", "doer", false},
		{"architecture-reviewer", "reviewer", false},
		{"code-plan-reviewer", "reviewer", false},
		{"architecture-reviewer", "reviewer", true},
		{"code-plan-reviewer", "reviewer", true},
	} {
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
				TaskID: "consumer", Description: "Plan a bounded consumer", DoneWhen: "Reviewed plan",
				SpecRef: "specs/goal.md", Worktree: "/repo/.worktrees/consumer",
				IntegrationBranch: "main", BaseCommit: strings.Repeat("a", 40), ReviewCommit: strings.Repeat("b", 40),
				ArchRef: "specs/arch.md", PlanRef: "specs/plan.md", GoalSlug: "goal",
				DecompositionRoot: tc.root, MasterOutputRefField: "plan_ref",
			}
			if tc.root {
				sections = append(sections, "master-decomposition-review")
			}
			output, err := BuildRoleContext(tc.role, sections, data)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				"Scope task/code-plan",
				"provider_dependencies",
				"ordinary downstream edges",
				"acme-cli",
			} {
				if !strings.Contains(output, want) {
					t.Errorf("%s prompt missing %q", name, want)
				}
			}
			if tc.roleType == "doer" {
				for _, want := range []string{
					"acme-cli set-task-output consumer",
					"Encode Scope task/code-plan waits in task_depends_on or provider_dependencies (unborn/cross-stage)",
					"Before provider declarations, read acme-cli set-task-output --help for schema/readiness",
					"Only typed waits permit cross-stage",
					"reject ordinary downstream edges and future production/inheritance cycles",
				} {
					if !strings.Contains(output, want) {
						t.Errorf("%s authoring prompt missing %q", name, want)
					}
				}
			} else {
				for _, want := range []string{
					"Reject prose-only waits",
					"Reconcile Scope task/code-plan preconditions with output[] task_depends_on or provider_dependencies",
					"MERGED provider",
					"executed transition",
					"every selected child MERGED",
					"APPROVED is insufficient",
				} {
					if !strings.Contains(output, want) {
						t.Errorf("%s reviewer prompt missing %q", name, want)
					}
				}
			}
			if tc.root && !strings.Contains(output, "cycles through future production/inheritance") {
				t.Errorf("%s master review omits latent-cycle reconciliation", name)
			}
			if match := rawDefaultBrand.FindString(output); match != "" {
				t.Errorf("non-default %s prompt leaks %q", name, match)
			}
		})
	}
}

func TestProviderDependencyPlanningCompleteGuidanceBranded(t *testing.T) {
	withPromptBrandValues(t, func() {
		brand.NameTitle = "Acme"
		brand.BinaryName = "acme-cli"
		brand.GlobalDirName = ".acme"
		brand.ProjectDirName = ".acme-run"
	})
	state := testhelpers.CreateValidState()
	plan := testhelpers.BuildTaskByStatus("consumer-plan", models.TaskStatusMerged, time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC))
	plan.RolePair = "code-planning-pair"
	plan.Output = []models.OutputEntry{{
		Desc: "Consumer", DoneWhen: "Tests pass", Scope: "bounded consumer",
		PlanRef: "specs/plans/consumer.md#Task 1",
	}}
	state.Tasks = []models.Task{plan}
	state.Sprint.Scope.Planned = []string{plan.ID}
	_, output, err := RenderOrchestratorDashboard(state, setupPipelineConfig(t), "orchestrator-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"provider_dependencies (existing provider, configured per-subtask transition, selected outputs)",
		"unborn children and cross-stage waits",
		"reviewed reauthoring/replacement before restoring a legacy blocked consumer",
		"merely unblocking against a merged architecture or APPROVED plan is insufficient",
		"future production/inheritance cycles",
		"runtime does not parse prose",
		"acme-cli plan-check <task-id> --pass",
		"acme-cli replan <task-id>",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("planning-complete guidance missing %q", want)
		}
	}
	for _, literal := range []string{"Liza", "LIZA", "liza", "{{binaryName}}"} {
		if strings.Contains(output, literal) {
			t.Errorf("non-default wake guidance leaks %q", literal)
		}
	}
}
