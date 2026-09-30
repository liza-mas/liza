package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Runtime-input names never reach a provider session, whoever set them
// (ADR-0169); the unrelated environment passes through untouched.
func TestScrubRuntimeInputNamesRemovesDeclaredNames(t *testing.T) {
	root := t.TempDir()
	env := []string{"PATH=/bin", "W03_FIXTURE=ambient", "MEMBER_CREDENTIAL=ambient", "OTHER=kept"}
	if got, err := scrubRuntimeInputNames(root, env); err != nil || strings.Join(got, ",") != strings.Join(env, ",") {
		t.Fatalf("without a state file got %v, %v; want the environment unchanged", got, err)
	}

	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("live", models.TaskStatusReady, time.Now().UTC())
	task.Validation = []string{"sh boundary_test.sh"}
	task.RuntimeInputs = []models.RuntimeInput{{ID: "w03", Commands: task.Validation, Recipe: "project.w03", Consumption: models.RuntimeInputSingleUse, Env: []string{"W03_FIXTURE"}}}
	state.Tasks = append(state.Tasks, task)
	state.RuntimeInputs = map[string]models.RuntimeInputInstance{}
	testhelpers.WriteInitialState(t, statePath, state)
	got, err := scrubRuntimeInputNames(root, env)
	if err != nil || strings.Join(got, ",") != "PATH=/bin,MEMBER_CREDENTIAL=ambient,OTHER=kept" {
		t.Fatalf("got %v, %v; want only the declared name removed", got, err)
	}
}

// An agent env file setting a runtime-input name refuses the launch with a
// named diagnostic instead of silently stripping, say, the provider's own key.
func TestResolveLaunchEnvironmentRefusesAnEnvFileRuntimeInputCollision(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("live", models.TaskStatusReady, time.Now().UTC())
	task.Validation = []string{"sh boundary_test.sh"}
	task.RuntimeInputs = []models.RuntimeInput{{ID: "llm", Commands: task.Validation, Recipe: "project.llm-key", Consumption: models.RuntimeInputReusable, Secret: true, Env: []string{"PROVIDER_API_KEY"}}}
	state.Tasks = append(state.Tasks, task)
	testhelpers.WriteInitialState(t, statePath, state)
	if err := os.WriteFile(filepath.Join(root, "agent.env"), []byte("PROVIDER_API_KEY=agent-key-value\nOTHER=kept\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := resolveLaunchEnvironment(LaunchPlan{EnvFiles: []string{"agent.env"}}, root, "coder-1", "gen-1", nil)
	if err == nil || !strings.Contains(err.Error(), "runtime_input_provider_collision:PROVIDER_API_KEY") || strings.Contains(err.Error(), "agent-key-value") {
		t.Fatalf("err = %v, want a named collision without the value", err)
	}
	if err := os.WriteFile(filepath.Join(root, "agent.env"), []byte("OTHER=kept\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROVIDER_API_KEY", "ambient-value")
	env, err := resolveLaunchEnvironment(LaunchPlan{EnvFiles: []string{"agent.env"}}, root, "coder-1", "gen-1", nil)
	if err != nil || strings.Contains(strings.Join(env, "\n"), "PROVIDER_API_KEY") || !strings.Contains(strings.Join(env, "\n"), "OTHER=kept") {
		t.Fatalf("env = %v, %v; want the ambient copy stripped and the overlay kept", env, err)
	}
}
