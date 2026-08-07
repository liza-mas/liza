package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/pairingindex"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/stacklit"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestIndexingActivationStacklitPairingInitInstallsLifecycleRefreshWithoutGlobalToolsMutation(t *testing.T) {
	projectDir := newIndexingActivationProject(t)
	fakeStacklitDir := writeIndexingActivationFakeStacklit(t)
	t.Setenv("PATH", fakeStacklitDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(stacklit.EnvEnableStacklit, "true")
	t.Setenv(scipsearch.EnvEnableScipSearch, "false")
	t.Setenv(semble.EnvEnableSemble, "false")

	toolsPath := filepath.Join(os.Getenv("HOME"), paths.GlobalDirName(), "AGENT_TOOLS.md")
	writeIndexingActivationFile(t, toolsPath, "global tools sentinel\n")
	beforeTools := readIndexingActivationFile(t, toolsPath)

	if err := commands.InitPairingCommand(commands.InitPairingParams{
		Agents:         []string{"claude", "codex"},
		Stdin:          strings.NewReader(""),
		ContractAction: "global",
	}); err != nil {
		t.Fatalf("InitPairingCommand(): %v", err)
	}

	scriptPath := filepath.Join(projectDir, ".git", "hooks", brand.BinaryName+"-index.sh")
	script := readIndexingActivationFile(t, scriptPath)
	assertIndexingActivationContainsAll(t, script,
		pairingindex.ManagedIndexScriptMarker,
		"stacklit diff -i stacklit.json",
		"stacklit generate-json -o stacklit.json --parse-workers 3",
		"stacklit init-insights -i stacklit.json -o stacklit-insights.json",
		"stacklit ai-summary",
	)
	for _, hook := range pairingindex.DefaultLifecycleHooks() {
		content := readIndexingActivationFile(t, filepath.Join(projectDir, ".git", "hooks", hook))
		assertIndexingActivationContainsAll(t, content,
			pairingindex.ManagedHookMarker,
			brand.BinaryName+"-index.sh",
		)
		assertIndexingActivationContainsNone(t, content, brand.BinaryName+"-index.sh ai")
	}
	if got := readIndexingActivationFile(t, toolsPath); got != beforeTools {
		t.Fatalf("pairing init modified AGENT_TOOLS.md: before %q after %q", beforeTools, got)
	}

	autoLogPath := filepath.Join(t.TempDir(), "stacklit-auto.log")
	runIndexingActivationGit(t, projectDir, autoLogPath, "commit", "--allow-empty", "-m", "Trigger Stacklit lifecycle")

	wantAutoCalls := "generate-json -o stacklit.json --parse-workers 3\ninit-insights -i stacklit.json -o stacklit-insights.json\ngenerate-json -o stacklit.json --parse-workers 3\n"
	if got := readIndexingActivationFile(t, autoLogPath); got != wantAutoCalls {
		t.Fatalf("automatic Stacklit calls = %q, want lifecycle refresh without AI-summary", got)
	}
	if got := readIndexingActivationFile(t, filepath.Join(projectDir, "stacklit.json")); got != "generated index\n" {
		t.Fatalf("stacklit.json = %q, want generated Stacklit index", got)
	}

	writeIndexingActivationFile(t, filepath.Join(projectDir, "stacklit.json"), "stale index\n")
	manualLogPath := filepath.Join(t.TempDir(), "stacklit-manual-ai.log")
	// Windows cannot fork/exec a .sh: the hook has to be handed to a shell, the
	// way git hands it to one when it runs the hook itself.
	runIndexingActivationCommand(t, projectDir, manualLogPath,
		testhelpers.ResolveBashForScripts(t), filepath.ToSlash(scriptPath), "ai")

	wantManualCalls := "diff -i stacklit.json\ngenerate-json -o stacklit.json --parse-workers 3\ninit-insights -i stacklit.json -o stacklit-insights.json\nai-summary\ngenerate-json -o stacklit.json --parse-workers 3\n"
	if got := readIndexingActivationFile(t, manualLogPath); got != wantManualCalls {
		t.Fatalf("manual Stacklit calls = %q, want %q", got, wantManualCalls)
	}
	if got := readIndexingActivationFile(t, filepath.Join(projectDir, "stacklit.json")); got != "generated index\n" {
		t.Fatalf("manual Stacklit refresh left stacklit.json = %q, want generated Stacklit index", got)
	}
}

func writeIndexingActivationFakeStacklit(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "stacklit")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$LIZA_TEST_STACKLIT_LOG"
if [ "$1" = "diff" ]; then
	exit "${LIZA_TEST_STACKLIT_DIFF_EXIT:-1}"
fi
if [ "$1" = "generate-json" ]; then
	printf '%s\n' "generated index" > "$PWD/stacklit.json"
fi
if [ "$1" = "init-insights" ]; then
	printf '%s\n' "$LIZA_TEST_STACKLIT_LOG" > "$PWD/stacklit-insights.json"
fi
if [ "$1" = "ai-summary" ] && [ ! -f "$PWD/stacklit.json" ]; then
	echo "stacklit generate-json must run before ai-summary" >&2
	exit 7
fi
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(fake stacklit): %v", err)
	}
	return dir
}

func runIndexingActivationGit(t *testing.T, dir, stacklitLogPath string, args ...string) {
	t.Helper()

	runIndexingActivationCommand(t, dir, stacklitLogPath, "git", args...)
}

func runIndexingActivationCommand(t *testing.T, dir, stacklitLogPath, name string, args ...string) {
	t.Helper()

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LIZA_TEST_STACKLIT_LOG="+stacklitLogPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, output)
	}
}

func readIndexingActivationFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	return string(content)
}
