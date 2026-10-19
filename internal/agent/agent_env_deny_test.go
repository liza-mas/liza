package agent

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const deniedHostName = "HOST_ONLY_KEY"

func writeAgentEnvDenyState(t *testing.T, root string, mutate func(*models.State)) string {
	t.Helper()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Config.AgentEnvDeny = []string{deniedHostName}
	if mutate != nil {
		mutate(state)
	}
	testhelpers.WriteInitialState(t, statePath, state)
	return statePath
}

// A denied host variable never reaches a session snapshot, fresh or frozen,
// and an agent env file setting it refuses a fresh preparation by name.
func TestResolveLaunchEnvironmentWithholdsAgentEnvDeny(t *testing.T) {
	root := t.TempDir()
	writeAgentEnvDenyState(t, root, nil)
	t.Setenv(deniedHostName, "host-value")
	if err := os.WriteFile(filepath.Join(root, "agent.env"), []byte("OTHER=kept\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The fresh snapshot starts from the real process environment: assert on
	// names only and never format the environment into a failure message.
	env, err := resolveLaunchEnvironment(LaunchPlan{EnvFiles: []string{"agent.env"}}, root, "coder-1", "gen-1", nil)
	if err != nil {
		t.Fatalf("fresh resolve: %v", err)
	}
	if envHasName(env, deniedHostName) || !slices.Contains(env, "OTHER=kept") {
		t.Fatalf("fresh env: %s present=%v, overlay kept=%v; want withheld and kept", deniedHostName, envHasName(env, deniedHostName), slices.Contains(env, "OTHER=kept"))
	}

	frozen, err := resolveLaunchEnvironment(LaunchPlan{}, root, "coder-1", "gen-1", []string{deniedHostName + "=frozen-value", "OTHER=kept"})
	if err != nil || envHasName(frozen, deniedHostName) {
		t.Fatalf("frozen env: %s present=%v, err %v; want the denied name re-scrubbed", deniedHostName, envHasName(frozen, deniedHostName), err)
	}

	if err := os.WriteFile(filepath.Join(root, "agent.env"), []byte(deniedHostName+"=agent-file-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = resolveLaunchEnvironment(LaunchPlan{EnvFiles: []string{"agent.env"}}, root, "coder-1", "gen-1", nil)
	if err == nil || !strings.Contains(err.Error(), "agent_env_denied:"+deniedHostName) || !strings.Contains(err.Error(), "config.agent_env_deny") || strings.Contains(err.Error(), "agent-file-value") {
		t.Fatalf("err = %v, want a named agent_env_denied refusal without the value", err)
	}
}

func envHasName(env []string, name string) bool {
	return slices.ContainsFunc(env, func(entry string) bool { return strings.HasPrefix(entry, name+"=") })
}

// The supervisor withholds denied names from its own environment before it
// registers, so nothing it starts can inherit them.
func TestRunSupervisorWithholdsAgentEnvDenyBeforeRegistration(t *testing.T) {
	root := t.TempDir()
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	testhelpers.SetupTestGitRepo(t, root)
	statePath := writeAgentEnvDenyState(t, root, func(state *models.State) {
		state.Agents["orchestrator-1"] = models.Agent{
			Role: "orchestrator", Status: models.AgentStatusPlanning,
			LeaseExpires: testhelpers.TimePtr(time.Now().UTC().Add(10 * time.Minute)),
			Heartbeat:    time.Now().UTC(), PID: os.Getpid(),
		}
	})
	t.Setenv(deniedHostName, "host-value")

	err := RunSupervisor(context.Background(), SupervisorConfig{
		AgentID: "orchestrator-2", Role: "orchestrator", ProjectRoot: root,
		StatePath: statePath, LogPath: filepath.Join(root, paths.ProjectDirName(), "log.yaml"),
		SpecsDir: filepath.Join(root, "specs"), CLIName: "codex", LLMAgent: &MockLLMAgent{},
	})
	if err == nil || !strings.Contains(err.Error(), "already has") {
		t.Fatalf("RunSupervisor() error = %v, want the duplicate rejected at registration", err)
	}
	if _, ok := os.LookupEnv(deniedHostName); ok {
		t.Fatalf("%s still set after supervisor start, want it withheld before registration", deniedHostName)
	}
}
