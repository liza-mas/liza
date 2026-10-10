package prompts

import (
	"regexp"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/pipeline"
)

// These tests establish rendered admission duties, not semantic inventory
// completeness: the author and independent reviewer still compare actual epics.
func TestEarlyConsumerInventoryAndProviderFreezeGuidance(t *testing.T) {
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
	rawBrand := regexp.MustCompile(`(?i)(^|[^A-Za-z])liza($|[^A-Za-z0-9])|\{\{binaryName\}\}`)
	for _, tc := range []struct {
		role, roleType string
		root           bool
		required       []string
	}{
		{"epic-planner", "doer", false, []string{"Before downstream decomposition", "Provider Ask Inventory", "interface ID, consumer epic/ref", "operations, fields and statuses", "explicitly record none", "not invented provider designs or executable dependency edges"}},
		{"epic-plan-reviewer", "reviewer", false, []string{"Before decomposition", "Provider Ask Inventory (or explicit none)", "misses an inherited consumer obligation"}},
		{"architect", "doer", false, []string{"obtain the early epic", "reconcile its coverage", "provider answer", "concrete deferral", "Missing declared consumer asks block", "allocate its correction or bounded", "hold the affected scope on the corrected contract", "Child feasibility", "acceptance manifest", "checked before it restricts a child", "#### CONTRACT", "metadata outside it"}},
		{"architecture-reviewer", "reviewer", false, []string{"Provider freeze", "coverage is not reconciled", "declared asks are unavailable", "legal affected-scope wait", "Child feasibility", "only an unfinished provider's delivery is deferrable"}},
		{"architect", "doer", true, []string{"all assigned consumer epics", "source obligations", "Unavailable declared asks block only the affected scope", "Do not infer executable edges", "Preserve the child-feasibility checks"}},
		{"architecture-reviewer", "reviewer", true, []string{"all assigned consumer epics", "unavailable declared asks", "legal affected-scope wait", "Interface strings do not establish executable edges", "preserve child-feasibility checks"}},
	} {
		name := tc.role
		if tc.root {
			name += "-master"
		}
		t.Run(name, func(t *testing.T) {
			sections, err := resolver.ContextSections(tc.role)
			if err != nil {
				t.Fatal(err)
			}
			if tc.root {
				sections = append(sections, map[string]string{"doer": "master-decomposition-mandate", "reviewer": "master-decomposition-review"}[tc.roleType])
			}
			rendered, err := BuildRoleContext(tc.role, sections, &RoleContextData{
				Role: tc.role, AgentID: tc.role + "-1", RoleType: tc.roleType,
				TaskID: "task-1", Worktree: "/repo/.worktrees/task-1", IntegrationBranch: "main",
				SpecRef: "specs/goal.md", ArchRef: "specs/arch.md#Scope", EpicRef: "specs/epic.md",
				GoalSlug: "goal", BaseCommit: strings.Repeat("a", 40), ReviewCommit: strings.Repeat("b", 40),
				DecompositionRoot: tc.root, MasterOutputRefField: "arch_ref",
				DirectCodingAllocationAvailable: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Join(strings.Fields(rendered), " ")
			for _, required := range tc.required {
				if !strings.Contains(text, required) {
					t.Errorf("missing admission duty %q", required)
				}
			}
			if strings.HasPrefix(tc.role, "architect") {
				for _, required := range []string{"one exact Scope", "coding_allocation: true", "plan_ref", "strict Acceptance Contract", "future coder-authored manifest path", "owned_files", "nonempty canonical validation", "writer waits", "manual", "handoff disposition"} {
					if !strings.Contains(text, required) {
						t.Errorf("missing direct allocation boundary %q", required)
					}
				}
			}
			if match := rawBrand.FindString(rendered); match != "" {
				t.Errorf("non-default render leaks %q", match)
			}
		})
	}
}

func TestUnavailableDirectCodingRouteDoesNotEncourageAllocation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ role, roleType, section string }{
		{"architect", "doer", "implementation-phase"},
		{"architect", "doer", "master-decomposition-mandate"},
		{"architecture-reviewer", "reviewer", "review-instructions"},
		{"architecture-reviewer", "reviewer", "master-decomposition-review"},
	} {
		rendered, err := BuildRoleContext(tc.role, []string{tc.section}, &RoleContextData{
			Role: tc.role, RoleType: tc.roleType, TaskID: "legacy-plan", Worktree: "/repo/.worktrees/legacy-plan",
			DecompositionRoot: strings.HasPrefix(tc.section, "master-"), MasterOutputRefField: "arch_ref",
		})
		if err != nil {
			t.Fatalf("render %s: %v", tc.section, err)
		}
		if strings.Contains(rendered, "coding_allocation: true") {
			t.Errorf("%s encourages an unavailable direct route", tc.section)
		}
		if tc.roleType == "reviewer" && !strings.Contains(rendered, "no direct coding route") {
			t.Errorf("%s omits unsupported allocation refusal", tc.section)
		}
	}
}

func TestEarlyConsumerInventoryAuthoringContract(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"../../skills/epic-writing/SKILL.md", "../../skills/architecture-planning/SKILL.md", "../../skills/shared/references/reference-first-authoring.md"} {
		text := strings.Join(strings.Fields(readContractFixture(t, path)), " ")
		for _, required := range []string{"Provider Ask Inventory", "consumer epic/ref", "operations, fields and statuses", "executable dependency edges"} {
			if !strings.Contains(text, required) {
				t.Errorf("%s missing inventory contract %q", path, required)
			}
		}
	}
}
