package agent

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestTaskPromptProviderDependencies(t *testing.T) {
	resolver := testResolver(t)
	for _, role := range []string{"coder", "code-reviewer"} {
		for _, declared := range []bool{true, false} {
			name := role + "/legacy"
			if declared {
				name = role + "/declared"
			}
			t.Run(name, func(t *testing.T) {
				state := testhelpers.CreateValidState()
				state.Config.IntegrationBranch = "main"
				state.Tasks = []models.Task{{
					ID: "consumer", Description: "Consume a reviewed provider plan",
					Status: models.TaskStatusImplementing, RolePair: "coding-pair",
				}}
				task := &state.Tasks[0]
				if declared {
					task.ProviderDependencies = []models.ProviderDependency{{
						ProviderTask: "provider-architecture",
						Transition:   "architecture-to-code-plan",
						Outputs:      []int{0, 2},
					}}
				}
				config := SupervisorConfig{Role: role, AgentID: role + "-1", ProjectRoot: t.TempDir()}
				data, err := testBuildTaskRoleContextData(t, task, state, config, resolver)
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
				if !declared {
					if strings.Contains(output, "PROVIDER DEPENDENCIES") || strings.Contains(output, "provider-architecture") {
						t.Fatalf("legacy %s prompt invents provider prerequisites", role)
					}
					return
				}
				for _, want := range []string{
					"PROVIDER DEPENDENCIES (claim requires provider and selected children MERGED):",
					"- provider-architecture / architecture-to-code-plan / output indexes [0 2]",
				} {
					if !strings.Contains(output, want) {
						t.Errorf("%s prompt missing %q", role, want)
					}
				}
				data.ProviderDependencies[0].Outputs[0] = 7
				if task.ProviderDependencies[0].Outputs[0] != 0 {
					t.Fatal("render context aliases the task's provider output selection")
				}
			})
		}
	}
}
