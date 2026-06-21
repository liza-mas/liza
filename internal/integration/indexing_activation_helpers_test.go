package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/stacklit"
)

func newIndexingActivationProject(t *testing.T) string {
	t.Helper()

	projectDir, cleanup := setupTestProject(t)
	t.Cleanup(cleanup)
	return projectDir
}

func disableOptionalIndexingForTest(t *testing.T) {
	t.Helper()

	t.Setenv(stacklit.EnvEnableStacklit, "false")
	t.Setenv(scipsearch.EnvEnableScipSearch, "false")
	t.Setenv(semble.EnvEnableSemble, "false")
}

func writeIndexingActivationFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func runSessionStartContextHook(t *testing.T, projectDir string) string {
	t.Helper()

	hookPath := renderedSessionContextHookPath(t)
	payload, err := json.Marshal(map[string]string{"cwd": projectDir})
	if err != nil {
		t.Fatalf("Marshal(SessionStart payload): %v", err)
	}

	cmd := exec.Command("bash", hookPath)
	cmd.Dir = projectDir
	cmd.Stdin = strings.NewReader(string(payload))
	cmd.Env = sessionStartEnv(projectDir)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("session-context.sh failed: %v\n%s", err, string(out))
	}
	return string(out)
}

func renderedSessionContextHookPath(t *testing.T) string {
	t.Helper()

	hooksRoot := t.TempDir()
	if err := embedded.WriteHooks(hooksRoot); err != nil {
		t.Fatalf("WriteHooks(%q): %v", hooksRoot, err)
	}
	return filepath.Join(hooksRoot, ".claude", "hooks", "session-context.sh")
}

func sessionStartEnv(projectDir string) []string {
	blocked := []string{
		"CLAUDE_PROJECT_DIR",
		"LIZA_AGENT_ID",
		stacklit.EnvEnableStacklit,
		scipsearch.EnvEnableScipSearch,
		semble.EnvEnableSemble,
	}
	env := make([]string, 0, len(os.Environ())+4)
	for _, value := range os.Environ() {
		name := strings.SplitN(value, "=", 2)[0]
		if slices.Contains(blocked, name) {
			continue
		}
		env = append(env, value)
	}
	env = append(env,
		"CLAUDE_PROJECT_DIR="+projectDir,
		stacklit.EnvEnableStacklit+"=false",
		scipsearch.EnvEnableScipSearch+"=false",
		semble.EnvEnableSemble+"=false",
	)
	return env
}

func buildDisabledOptionalIndexPrompt(t *testing.T, projectRoot string) string {
	t.Helper()

	disableOptionalIndexingForTest(t)
	worktree := ".worktrees/task-1"
	taskWorktree := filepath.Join(projectRoot, worktree)
	writeIndexingActivationFile(t, filepath.Join(taskWorktree, "stacklit.json"), `{"project":{"name":"stale"}}`)
	writeIndexingActivationFile(t, filepath.Join(taskWorktree, ".liza", "scip", "go.scip"), "stale go index")
	writeIndexingActivationFile(t, filepath.Join(taskWorktree, ".sembleignore"), semble.DefaultIgnorePayload())

	state := &models.State{
		Goal: models.Goal{
			Description: "Indexing activation",
			SpecRef:     "specs/goals/indexing.md",
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Disabled optional-index prompt coverage",
				Status:      models.TaskStatusImplementing,
				DoneWhen:    "Optional tool sections are omitted",
				Scope:       "Integration",
				SpecRef:     "specs/goals/indexing.md",
				RolePair:    "coding-pair",
				Worktree:    &worktree,
			},
		},
		Config: models.Config{
			IntegrationBranch: "main",
			ScipSearch:        []string{"go"},
		},
	}

	resolver := embeddedPipelineResolver(t)
	strategy, err := agent.NewRoleStrategy("coder", resolver)
	if err != nil {
		t.Fatalf("NewRoleStrategy(coder): %v", err)
	}
	prompt, err := strategy.BuildPrompt(state, agent.SupervisorConfig{
		Role:        "coder",
		AgentID:     "coder-1",
		ProjectRoot: projectRoot,
		SpecsDir:    filepath.Join(projectRoot, "specs"),
		StatePath:   filepath.Join(projectRoot, ".liza", "state.yaml"),
	}, "task-1")
	if err != nil {
		t.Fatalf("BuildPrompt(): %v", err)
	}
	return prompt
}

func embeddedPipelineResolver(t *testing.T) *pipeline.Resolver {
	t.Helper()

	cfg, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		t.Fatalf("LoadEmbeddedReference(): %v", err)
	}
	return pipeline.NewResolver(cfg)
}

func assertIndexingActivationContainsAll(t *testing.T, text string, wants ...string) {
	t.Helper()

	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("missing expected content %q:\n%s", want, text)
		}
	}
}

func assertIndexingActivationContainsNone(t *testing.T, text string, forbidden ...string) {
	t.Helper()

	for _, notWant := range forbidden {
		if strings.Contains(text, notWant) {
			t.Fatalf("unexpected content %q:\n%s", notWant, text)
		}
	}
}
