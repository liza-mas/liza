package agent

import (
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// The declaration instruction is rendered only when both roles of the pair that
// consumes the plan's output[] can run declared checks; otherwise declarations
// would fail closed at the coder's or the reviewer's claim.
func TestBuildPromptWithContext_ValidationPrerequisitesFollowConsumerPolicy(t *testing.T) {
	local := models.AgentToolConfig{ValidationExecution: ValidationExecutionLocal}
	pairCLIs := models.Config{DefaultDoerCLI: "claude", DefaultReviewerCLI: "codex"}
	withTools := func(tools map[string]models.AgentToolConfig) models.Config {
		c := pairCLIs
		c.AgentTools = tools
		return c
	}
	plannerLines := []string{
		"validation_prerequisites: REQUIRED, one entry per validation[] command",
		"Never declare what the task builds: for `./bin/tool --self-test` declare the build tool",
		"Verify each validation[] command has its validation_prerequisites entry",
	}
	reviewerLines := []string{"| validation prerequisites | A coding child's validation[] command lacks exactly one validation_prerequisites entry"}
	tests := []struct {
		name     string
		role     string
		rolePair string
		config   models.Config
		want     bool
	}{
		{name: "planner, coder and reviewer local", role: models.RoleCodePlanner, rolePair: "code-planning-pair",
			config: withTools(map[string]models.AgentToolConfig{"claude": local, "codex": local}), want: true},
		{name: "reviewer, coder and reviewer local", role: models.RoleCodePlanReviewer, rolePair: "code-planning-pair",
			config: withTools(map[string]models.AgentToolConfig{"claude": local, "codex": local}), want: true},
		{name: "coder local but reviewer unset", role: models.RoleCodePlanner, rolePair: "code-planning-pair",
			config: withTools(map[string]models.AgentToolConfig{"claude": local})},
		{name: "reviewer prompt, reviewer unset", role: models.RoleCodePlanReviewer, rolePair: "code-planning-pair",
			config: withTools(map[string]models.AgentToolConfig{"claude": local})},
		{name: "no policy", role: models.RoleCodePlanner, rolePair: "code-planning-pair", config: pairCLIs},
		{name: "unresolvable selection", role: models.RoleCodePlanner, rolePair: "code-planning-pair",
			config: models.Config{DefaultDoerProfile: "missing", AgentTools: map[string]models.AgentToolConfig{"claude": local, "codex": local}}},
		{name: "master planner children are plans, not code", role: models.RoleCodePlanner, rolePair: "code-planning-main-pair",
			config: withTools(map[string]models.AgentToolConfig{"claude": local, "codex": local})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// GIVEN a code-planning task and a policy for the consuming pair's CLIs
			clearCLIEnv(t)
			projectRoot := t.TempDir()
			testhelpers.SetupTestGitRepo(t, projectRoot)
			config := tt.config
			config.IntegrationBranch = "main"
			task := models.Task{ID: "task-1", Description: "Plan scope", Status: models.TaskStatus("EXECUTING"),
				DoneWhen: "Plan is complete", Scope: "scope", RolePair: tt.rolePair}
			if tt.role == models.RoleCodePlanReviewer {
				task.Status = models.TaskStatus("REVIEWING")
				task.BaseCommit, task.ReviewCommit, task.AssignedTo = ptrString("base-sha"), ptrString("review-sha"), ptrString("planner-1")
			}
			state := &models.State{Goal: models.Goal{Description: "goal", SpecRef: "specs/goals/g.md"}, Tasks: []models.Task{task}, Config: config}
			supervisor := SupervisorConfig{Role: tt.role, AgentID: tt.role + "-1", ProjectRoot: projectRoot,
				SpecsDir: filepath.Join(projectRoot, "specs"), StatePath: filepath.Join(projectRoot, paths.ProjectDirName(), "state.yaml")}

			// WHEN the prompt is built
			prompt, err := testBuildPromptWithContext(t, state, supervisor, "task-1", embeddedPipelineResolver(t))
			if err != nil {
				t.Fatalf("buildPromptWithContext() error = %v", err)
			}

			// THEN the declaration rules appear exactly when the consumers validate locally
			lines := plannerLines
			if tt.role == models.RoleCodePlanReviewer {
				lines = reviewerLines
			}
			for _, line := range lines {
				if tt.want {
					assertContainsAll(t, prompt, line)
				} else {
					assertNotContains(t, prompt, line)
				}
			}
		})
	}
}
