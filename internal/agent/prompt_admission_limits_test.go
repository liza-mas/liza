package agent

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Task prompts name every valid configured match substring once, so local
// runs of a limited command go through admit (ADR-0209, D-191).
func TestTaskPromptAdmissionLimits(t *testing.T) {
	resolver := testResolver(t)
	for _, role := range []string{"coder", "code-reviewer"} {
		t.Run(role, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			state.Config.IntegrationBranch = "main"
			state.Config.AdmissionLimits = []models.AdmissionLimit{
				{Class: "db-suite", MaxConcurrent: 2, Match: []string{"--decision-model-db", "frames"}},
				{Class: "cpu", MaxConcurrent: 1, Match: []string{"frames"}},
				{Class: "Invalid Class", MaxConcurrent: 1, Match: []string{"never-rendered"}},
			}
			state.Tasks = []models.Task{{ID: "task-db", Description: "Change the model", Status: models.TaskStatusImplementing, RolePair: "coding-pair",
				Validation: []string{"pytest tests/model --decision-model-db"}}}
			config := SupervisorConfig{Role: role, AgentID: role + "-1", ProjectRoot: t.TempDir()}
			data, err := testBuildTaskRoleContextData(t, &state.Tasks[0], state, config, resolver)
			if err != nil {
				t.Fatal(err)
			}
			sections, err := resolver.ContextSections(role)
			if err != nil {
				t.Fatal(err)
			}
			output, err := prompts.BuildRoleContext(role, sections, data)
			if err != nil {
				t.Fatal(err)
			}
			if want := "run any local command containing `--decision-model-db`, `frames` as"; !strings.Contains(output, want) {
				t.Fatalf("%s prompt missing %q:\n%s", role, want, output)
			}
			if strings.Contains(output, "never-rendered") {
				t.Fatalf("%s prompt renders an invalid stored class", role)
			}
		})
	}
}
