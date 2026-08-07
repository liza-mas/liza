package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/stacklit"
)

func TestIndexingActivationSemblePairingInitCreatesProjectRootIgnoreBeforeSessionStartAdvertises(t *testing.T) {
	projectDir := newIndexingActivationProject(t)
	binDir := writeIndexingActivationFakeSembleTools(t, true)
	disableStacklitAndScipForIndexingActivation(t)
	enableSemblePairingInitForTest(t, binDir)

	if err := commands.InitPairingCommand(commands.InitPairingParams{
		Agents:         []string{"claude"},
		Stdin:          strings.NewReader(""),
		ContractAction: "global",
	}); err != nil {
		t.Fatalf("InitPairingCommand(): %v", err)
	}

	ignorePath := filepath.Join(projectDir, ".sembleignore")
	content, err := os.ReadFile(ignorePath)
	if err != nil {
		t.Fatalf("ReadFile(.sembleignore): %v", err)
	}
	if got, want := string(content), semble.DefaultIgnorePayload(); got != want {
		t.Fatalf(".sembleignore content mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}

	context := runPairingSessionStartContext(t, projectDir, sembleSessionStartOverrides(binDir))

	assertIndexingActivationContainsAll(t, context,
		"Semble semantic search is available for this repo root: "+emittedPath(projectDir),
		"semble search",
		"Semble returns candidate chunks, not proof",
	)
	assertIndexingActivationContainsNone(t, context,
		"env HF_HUB_OFFLINE=1 semble search",
		"env HF_HUB_OFFLINE=1 semble find-related",
	)
	assertIndexingActivationContainsNone(t, context,
		"Stacklit index:",
		"SCIP indexes:",
	)
}

func TestIndexingActivationSemblePairingInitVerifiesExistingSafeProjectRootIgnoreBeforeAdvertisement(t *testing.T) {
	projectDir := newIndexingActivationProject(t)
	binDir := writeIndexingActivationFakeSembleTools(t, true)
	existingPayload := "# existing project policy\n" + semble.DefaultIgnorePayload()
	writeIndexingActivationFile(t, filepath.Join(projectDir, ".sembleignore"), existingPayload)
	disableStacklitAndScipForIndexingActivation(t)
	enableSemblePairingInitForTest(t, binDir)

	if err := commands.InitPairingCommand(commands.InitPairingParams{
		Agents:         []string{"codex"},
		Stdin:          strings.NewReader(""),
		ContractAction: "global",
	}); err != nil {
		t.Fatalf("InitPairingCommand(): %v", err)
	}

	content, err := os.ReadFile(filepath.Join(projectDir, ".sembleignore"))
	if err != nil {
		t.Fatalf("ReadFile(.sembleignore): %v", err)
	}
	if got := string(content); got != existingPayload {
		t.Fatalf("InitPairingCommand() rewrote existing safe .sembleignore:\n%s", got)
	}

	context := runPairingSessionStartContext(t, projectDir, sembleSessionStartOverrides(binDir))
	assertIndexingActivationContainsAll(t, context,
		"Semble semantic search is available for this repo root: "+emittedPath(projectDir),
		"semble find-related",
	)
	assertIndexingActivationContainsNone(t, context,
		"env HF_HUB_OFFLINE=1 semble search",
		"env HF_HUB_OFFLINE=1 semble find-related",
	)
}

func TestIndexingActivationSemblePairingInitReportsUnsafeProjectRootIgnore(t *testing.T) {
	projectDir := newIndexingActivationProject(t)
	writeIndexingActivationFile(t, filepath.Join(projectDir, ".sembleignore"), ".liza/\n")
	disableStacklitAndScipForIndexingActivation(t)
	t.Setenv(semble.EnvEnableSemble, "true")

	err := commands.InitPairingCommand(commands.InitPairingParams{
		Agents:         []string{"claude"},
		Stdin:          strings.NewReader(""),
		ContractAction: "global",
	})
	if err == nil {
		t.Fatal("InitPairingCommand() error = nil, want unsafe .sembleignore diagnostic")
	}
	for _, want := range []string{
		"semble project-root safety failed",
		"semble project root .sembleignore missing required patterns",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("InitPairingCommand() error missing %q:\n%s", want, err)
		}
	}

	context := runPairingSessionStartContext(t, projectDir, sembleSessionStartOverrides(writeIndexingActivationFakeSembleTools(t, true)))
	assertIndexingActivationContainsNone(t, context, "Semble semantic search is available")
}

func TestIndexingActivationSessionStartAdvertisesOnlyReadyRepoRootOptionalTools(t *testing.T) {
	projectDir := newIndexingActivationProject(t)
	writeIndexingActivationLizaIndexHook(t, projectDir)

	context := runPairingSessionStartContext(t, projectDir, nil)
	assertIndexingActivationContainsNone(t, context,
		"Stacklit index:",
		"SCIP indexes:",
		"Semble semantic search is available",
	)

	writeIndexingActivationFile(t, filepath.Join(projectDir, "stacklit.json"), "{}\n")
	context = runPairingSessionStartContext(t, projectDir, nil)
	assertIndexingActivationContainsAll(t, context,
		"Stacklit index: "+emittedPath(filepath.Join(projectDir, "stacklit.json")),
		"stacklit derive --ai-summary -i",
	)
	assertIndexingActivationContainsNone(t, context,
		"SCIP indexes:",
		"Semble semantic search is available",
	)

	writeIndexingActivationFile(t, filepath.Join(projectDir, "go.scip"), "index\n")
	context = runPairingSessionStartContext(t, projectDir, nil)
	assertIndexingActivationContainsAll(t, context,
		"SCIP indexes:",
		"Go index: "+emittedPath(filepath.Join(projectDir, "go.scip")),
		"scip-search symbols --index <index-path>",
		"scip-search impact --index <index-path>",
	)
	assertIndexingActivationContainsNone(t, context, "Semble semantic search is available")

	writeIndexingActivationFile(t, filepath.Join(projectDir, ".sembleignore"), semble.DefaultIgnorePayload())
	readyBinDir := writeIndexingActivationFakeSembleTools(t, true)
	context = runPairingSessionStartContext(t, projectDir, sembleSessionStartOverrides(readyBinDir))
	assertIndexingActivationContainsAll(t, context,
		"Stacklit index: "+emittedPath(filepath.Join(projectDir, "stacklit.json")),
		"Go index: "+emittedPath(filepath.Join(projectDir, "go.scip")),
		"Semble semantic search is available for this repo root: "+emittedPath(projectDir),
	)

	notReadyBinDir := writeIndexingActivationFakeSembleTools(t, false)
	context = runPairingSessionStartContext(t, projectDir, sembleSessionStartOverrides(notReadyBinDir))
	assertIndexingActivationContainsAll(t, context,
		"Stacklit index: "+emittedPath(filepath.Join(projectDir, "stacklit.json")),
		"Go index: "+emittedPath(filepath.Join(projectDir, "go.scip")),
	)
	assertIndexingActivationContainsNone(t, context, "Semble semantic search is available")
}

func disableStacklitAndScipForIndexingActivation(t *testing.T) {
	t.Helper()
	t.Setenv(stacklit.EnvEnableStacklit, "false")
	t.Setenv(scipsearch.EnvEnableScipSearch, "false")
}

func enableSemblePairingInitForTest(t *testing.T, binDir string) {
	t.Helper()
	t.Setenv(semble.EnvEnableSemble, "true")
	t.Setenv("PATH", pathWithIndexingActivationPrefix(binDir))
}

func sembleSessionStartOverrides(binDir string) map[string]string {
	return map[string]string{
		"PATH":                 pathWithIndexingActivationPrefix(binDir),
		semble.EnvEnableSemble: "true",
	}
}

func pathWithIndexingActivationPrefix(binDir string) string {
	return binDir + string(os.PathListSeparator) + os.Getenv("PATH")
}

func runPairingSessionStartContext(t *testing.T, projectDir string, env map[string]string) string {
	t.Helper()

	hookPath := renderedSessionContextHookPath(t)
	payload, err := json.Marshal(map[string]string{"cwd": projectDir})
	if err != nil {
		t.Fatalf("Marshal(SessionStart payload): %v", err)
	}

	cmd := exec.Command("bash", hookPath)
	cmd.Dir = projectDir
	cmd.Stdin = strings.NewReader(string(payload))
	cmd.Env = pairingSessionStartEnv(projectDir, env)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("session-context.sh failed: %v\n%s", err, string(out))
	}

	var got struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("session context output is not JSON: %v\n%s", err, string(out))
	}
	return got.HookSpecificOutput.AdditionalContext
}

func pairingSessionStartEnv(projectDir string, overrides map[string]string) []string {
	blocked := []string{
		"CLAUDE_PROJECT_DIR",
		"LIZA_AGENT_ID",
		stacklit.EnvEnableStacklit,
		scipsearch.EnvEnableScipSearch,
		semble.EnvEnableSemble,
		"PATH",
	}
	values := map[string]string{
		"CLAUDE_PROJECT_DIR":           projectDir,
		stacklit.EnvEnableStacklit:     "false",
		scipsearch.EnvEnableScipSearch: "false",
		semble.EnvEnableSemble:         "false",
		"PATH":                         os.Getenv("PATH"),
	}
	for name, value := range overrides {
		values[name] = value
	}

	env := make([]string, 0, len(os.Environ())+len(values))
	for _, value := range os.Environ() {
		name := strings.SplitN(value, "=", 2)[0]
		if slices.Contains(blocked, name) {
			continue
		}
		env = append(env, value)
	}
	for _, name := range []string{
		"CLAUDE_PROJECT_DIR",
		stacklit.EnvEnableStacklit,
		scipsearch.EnvEnableScipSearch,
		semble.EnvEnableSemble,
		"PATH",
	} {
		env = append(env, name+"="+values[name])
	}
	return env
}

// emittedPath renders a path the way the session context hook prints it. The
// hook canonicalises the project directory so that git, which reports
// forward-slashed paths even on Windows, and the runtime, which supplies a
// native one, cannot each impose a different separator on the same output. On
// Unix this is the identity. internal/embedded has the same helper for the same
// reason.
func emittedPath(path string) string {
	return filepath.ToSlash(path)
}

func writeIndexingActivationLizaIndexHook(t *testing.T, projectDir string) {
	t.Helper()
	hookPath := filepath.Join(projectDir, ".git", "hooks", "post-commit")
	writeIndexingActivationFile(t, hookPath, "#!/bin/sh\nliza-index\n")
	if err := os.Chmod(hookPath, 0o755); err != nil {
		t.Fatalf("Chmod(%q): %v", hookPath, err)
	}
}

func writeIndexingActivationFakeSembleTools(t *testing.T, validationSucceeds bool) string {
	t.Helper()
	binDir := t.TempDir()
	writeIndexingActivationFile(t, filepath.Join(binDir, "timeout"), "#!/bin/sh\nshift\nexec \"$@\"\n")
	if err := os.Chmod(filepath.Join(binDir, "timeout"), 0o755); err != nil {
		t.Fatalf("Chmod(timeout): %v", err)
	}

	exitCode := "0"
	if !validationSucceeds {
		exitCode = "42"
	}
	sembleScript := strings.Join([]string{
		"#!/bin/sh",
		"test \"$1\" = \"search\" || exit 18",
		"test \"${HF_HUB_OFFLINE:-}\" = \"1\" || exit 0",
		"exit " + exitCode,
		"",
	}, "\n")
	writeIndexingActivationFile(t, filepath.Join(binDir, "semble"), sembleScript)
	if err := os.Chmod(filepath.Join(binDir, "semble"), 0o755); err != nil {
		t.Fatalf("Chmod(semble): %v", err)
	}
	return binDir
}
