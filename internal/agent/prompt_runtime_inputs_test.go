package agent

import (
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Planners declare runtime inputs, and plan reviewers check them, only once a
// recipe registry is configured: without one every declaration is refused.
func TestBuildPromptWithContext_RuntimeInputsFollowRegistryConfig(t *testing.T) {
	plannerLine := "runtime_inputs: declare every live input a validation command needs"
	reviewerLine := "| runtime inputs | A validation command needs a live input"
	tests := []struct {
		name, role, rolePair, registry string
		want                           bool
	}{
		{name: "planner with registry", role: models.RoleCodePlanner, rolePair: "code-planning-pair", registry: "config/runtime-inputs.yaml", want: true},
		{name: "reviewer with registry", role: models.RoleCodePlanReviewer, rolePair: "code-planning-pair", registry: "config/runtime-inputs.yaml", want: true},
		{name: "planner without registry", role: models.RoleCodePlanner, rolePair: "code-planning-pair"},
		{name: "reviewer without registry", role: models.RoleCodePlanReviewer, rolePair: "code-planning-pair"},
		{name: "master planner children are plans", role: models.RoleCodePlanner, rolePair: "code-planning-main-pair", registry: "config/runtime-inputs.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearCLIEnv(t)
			projectRoot := t.TempDir()
			testhelpers.SetupTestGitRepo(t, projectRoot)
			config := models.Config{IntegrationBranch: "main", RuntimeInputRegistry: tt.registry}
			task := models.Task{ID: "task-1", Description: "Plan scope", Status: models.TaskStatus("EXECUTING"),
				DoneWhen: "Plan is complete", Scope: "scope", RolePair: tt.rolePair}
			if tt.role == models.RoleCodePlanReviewer {
				task.Status = models.TaskStatus("REVIEWING")
				task.BaseCommit, task.ReviewCommit, task.AssignedTo = ptrString("base-sha"), ptrString("review-sha"), ptrString("planner-1")
			}
			state := &models.State{Goal: models.Goal{Description: "goal", SpecRef: "specs/goals/g.md"}, Tasks: []models.Task{task}, Config: config}
			supervisor := SupervisorConfig{Role: tt.role, AgentID: tt.role + "-1", ProjectRoot: projectRoot,
				SpecsDir: filepath.Join(projectRoot, "specs"), StatePath: filepath.Join(projectRoot, paths.ProjectDirName(), "state.yaml")}

			prompt, err := testBuildPromptWithContext(t, state, supervisor, "task-1", embeddedPipelineResolver(t))
			if err != nil {
				t.Fatalf("buildPromptWithContext() error = %v", err)
			}
			line := plannerLine
			if tt.role == models.RoleCodePlanReviewer {
				line = reviewerLine
			}
			if tt.want {
				assertContainsAll(t, prompt, line)
			} else {
				assertNotContains(t, prompt, line)
			}
		})
	}
}
