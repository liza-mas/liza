package promptbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/prompts"
)

// FixtureRoleContext builds the role-context data the benchmark renders:
// the calibrated reference context plus every payload-carrying dependency
// site populated to the fixture shape. Role identity is a parameter so the
// same fixture can be rendered through any role's section list.
func FixtureRoleContext(shape Shape, role, agentID, roleType string) *prompts.RoleContextData {
	phaseDeps := make([]prompts.SiblingTaskSummary, 0, shape.PhaseDependencies)
	for i := range shape.PhaseDependencies {
		phaseDeps = append(phaseDeps, prompts.SiblingTaskSummary{
			ID:       fmt.Sprintf("fixture-plan-%d", i),
			Status:   "MERGED",
			PlanRef:  fmt.Sprintf("specs/generated/plan-%d.md", i),
			RolePair: "code-planning-pair",
		})
	}

	descendants := make([]prompts.IntegrationDescendantSummary, 0, shape.Descendants)
	for i := range shape.Descendants {
		deps := make([]string, 0, shape.DepsPerDescendant)
		for j := range shape.DepsPerDescendant {
			deps = append(deps, fmt.Sprintf("fixture-cp-%d-code-%d", i%3, j))
		}
		descendants = append(descendants, prompts.IntegrationDescendantSummary{
			ID:          fmt.Sprintf("fixture-cp-1-code-%d", i),
			Description: "generated descendant standing in for an attributed task",
			DoneWhen:    "generated acceptance criteria",
			SpecRef:     "specs/generated/goal.md",
			Commit:      fmt.Sprintf("%040x", i+1),
			DependsOn:   deps,
		})
	}

	return &prompts.RoleContextData{
		Role: role, AgentID: agentID, RoleType: roleType,
		TaskID: "fixture-cp-1", Description: "generated task", DoneWhen: "generated",
		SpecRef: "specs/generated/goal.md", GoalSpecRef: "specs/generated/goal.md",
		Worktree: "/generated/worktree", IntegrationBranch: "integration",
		BaseCommit: strings.Repeat("a", 40), ReviewCommit: strings.Repeat("b", 40),

		ResolvedReferenceContext: GenerateResolvedReferenceContext(shape),

		TaskRolePair:         "code-planning-pair",
		PhaseDependencyTasks: phaseDeps,
		TotalPlanTasks:       19,
		TaskOrdinal:          3,

		// IntegrationPhase "slice" is what gates the descendant block. The
		// measured run contained no slice-integration prompts, so site 2 is
		// zero there; the fixture still renders it so a reduction targeting
		// site 2 is measurable rather than structurally invisible.
		IntegrationPhase:        models.IntegrationAnalysisPhaseSlice,
		IntegrationDescendants:  descendants,
		IntegrationSourceCommit: strings.Repeat("c", 40),
		IntegrationRootTaskIDs:  []string{"fixture-cp-1", "fixture-cp-4"},
		IntegrationOriginatingPlan: &prompts.IntegrationPlanSummary{
			ID: "fixture-cp-1", Description: "generated originating plan",
			DoneWhen: "generated", SpecRef: "specs/generated/goal.md",
			PlanRef: "specs/generated/plan.md", ArchRef: "specs/generated/arch.md",
		},
	}
}

// WriteFixtureProjectRoot lays out the minimum on-disk workspace the
// dashboard renderer needs under dir, using the embedded default pipeline so
// the harness measures the shipped topology rather than a hand-written
// stand-in.
func WriteFixtureProjectRoot(dir string) error {
	runtimeDir := filepath.Join(dir, paths.ProjectDirName())
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return fmt.Errorf("mkdir project runtime directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "pipeline.yaml"), embedded.PipelineConfigContent(), 0o644); err != nil {
		return fmt.Errorf("write pipeline.yaml: %w", err)
	}
	return nil
}

// FixtureState builds the active-task set the orchestrator dashboard renders
// (site 4), at the fixture's dependency fan-out.
func FixtureState(shape Shape) *models.State {
	state := &models.State{}
	for i := range shape.ActiveTasks {
		deps := make([]string, 0, shape.DepsPerActiveTask)
		for j := range shape.DepsPerActiveTask {
			deps = append(deps, fmt.Sprintf("fixture-cp-%d-code-%d", i%3, j))
		}
		state.Tasks = append(state.Tasks, models.Task{
			ID:        fmt.Sprintf("fixture-active-%d", i),
			Status:    models.TaskStatusReady,
			RolePair:  "coding-pair",
			DependsOn: deps,
		})
	}
	return state
}
