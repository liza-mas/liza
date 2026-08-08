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
)

func TestIndexingActivationGeneratedArtifactsStayOutOfGitStatusWhenUntracked(t *testing.T) {
	projectDir := newIndexingActivationProject(t)
	fakeBinDir := writeIndexingActivationFakeScipGo(t)
	fakeStacklitDir := writeIndexingActivationFakeStacklit(t)
	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+fakeStacklitDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(stacklit.EnvEnableStacklit, "true")
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	t.Setenv(semble.EnvEnableSemble, "false")

	writeIndexingActivationFile(t, filepath.Join(projectDir, "go.mod"), "module example.com/project\n\ngo 1.22\n")
	runIndexingActivationGitPlain(t, projectDir, "add", "go.mod")
	runIndexingActivationGitPlain(t, projectDir, "commit", "-m", "Add Go module")

	if err := commands.InitPairingCommand(commands.InitPairingParams{
		Agents:     []string{"codex"},
		ScipSearch: []string{"go"},
	}); err != nil {
		t.Fatalf("InitPairingCommand(): %v", err)
	}
	statusAfterInit := runIndexingActivationGitOutput(t, projectDir, "status", "--short")

	logPath := filepath.Join(t.TempDir(), "index-refresh.log")

	runIndexingActivationGit(t, projectDir, logPath, "commit", "--allow-empty", "-m", "Trigger index hooks")

	statusAfterRefresh := runIndexingActivationGitOutput(t, projectDir, "status", "--short")
	if statusAfterRefresh != statusAfterInit {
		t.Fatalf("git status --short changed from %q to %q, want generated index artifacts hidden", statusAfterInit, statusAfterRefresh)
	}
	assertIndexingActivationContainsNone(t, statusAfterInit,
		"?? stacklit.json",
		"?? go.scip",
		"?? "+paths.ProjectDirName()+"/scip/",
	)
	assertIndexingActivationContainsNone(t, statusAfterRefresh,
		"?? stacklit.json",
		"?? go.scip",
		"?? "+paths.ProjectDirName()+"/scip/",
	)
	if got := runIndexingActivationGitOutput(t, projectDir, "check-ignore", "stacklit.json"); got != "stacklit.json" {
		t.Fatalf("git check-ignore stacklit.json = %q, want private exclude", got)
	}
	if got := runIndexingActivationGitOutput(t, projectDir, "check-ignore", "go.scip"); got != "go.scip" {
		t.Fatalf("git check-ignore go.scip = %q, want private exclude", got)
	}
	assertIndexingActivationContainsAll(t, readIndexingActivationFile(t, filepath.Join(projectDir, ".git", "info", "exclude")),
		"stacklit.json",
		"go.scip",
	)
	assertIndexingActivationContainsAll(t, readIndexingActivationFile(t, filepath.Join(projectDir, "go.scip")),
		"scip-go index",
	)
}

func TestIndexingActivationTrackedGeneratedArtifactMayAppearInGitStatus(t *testing.T) {
	projectDir := newIndexingActivationProject(t)
	fakeStacklitDir := writeIndexingActivationFakeStacklit(t)
	t.Setenv("PATH", fakeStacklitDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(stacklit.EnvEnableStacklit, "true")
	t.Setenv(scipsearch.EnvEnableScipSearch, "false")
	t.Setenv(semble.EnvEnableSemble, "false")

	writeIndexingActivationFile(t, filepath.Join(projectDir, "stacklit.json"), "tracked index\n")
	runIndexingActivationGitPlain(t, projectDir, "add", "stacklit.json")
	runIndexingActivationGitPlain(t, projectDir, "commit", "-m", "Track Stacklit index")

	if err := commands.InitPairingCommand(commands.InitPairingParams{Agents: []string{"codex"}}); err != nil {
		t.Fatalf("InitPairingCommand(): %v", err)
	}

	runIndexingActivationGit(t, projectDir, filepath.Join(t.TempDir(), "stacklit.log"), "commit", "--allow-empty", "-m", "Refresh tracked index")

	status := runIndexingActivationGitOutput(t, projectDir, "status", "--short")
	if strings.Contains(status, "?? stacklit.json") {
		t.Fatalf("git status --short = %q, want tracked Stacklit artifact rather than accidental untracked file", status)
	}
	if !strings.Contains(status, "stacklit.json") {
		t.Fatalf("git status --short = %q, want intentionally tracked generated artifact to remain visible when changed", status)
	}
}

func TestIndexingActivationNonDefaultHooksPathUsesEffectiveHookDirectory(t *testing.T) {
	projectDir := newIndexingActivationProject(t)
	fakeStacklitDir := writeIndexingActivationFakeStacklit(t)
	t.Setenv("PATH", fakeStacklitDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(stacklit.EnvEnableStacklit, "true")
	t.Setenv(scipsearch.EnvEnableScipSearch, "false")
	t.Setenv(semble.EnvEnableSemble, "false")

	runIndexingActivationGitPlain(t, projectDir, "config", "core.hooksPath", ".githooks")
	if err := os.MkdirAll(filepath.Join(projectDir, ".githooks"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.githooks): %v", err)
	}

	if err := commands.InitPairingCommand(commands.InitPairingParams{Agents: []string{"codex"}}); err != nil {
		t.Fatalf("InitPairingCommand(): %v", err)
	}

	scriptPath := filepath.Join(projectDir, ".githooks", brand.BinaryName+"-index.sh")
	assertIndexingActivationContainsAll(t, readIndexingActivationFile(t, scriptPath), pairingindex.ManagedIndexScriptMarker)
	for _, hook := range pairingindex.DefaultLifecycleHooks() {
		assertIndexingActivationContainsAll(t, readIndexingActivationFile(t, filepath.Join(projectDir, ".githooks", hook)),
			pairingindex.ManagedHookMarker,
			brand.BinaryName+"-index.sh",
		)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".git", "hooks", brand.BinaryName+"-index.sh")); err == nil {
		t.Fatal("InitPairingCommand installed inert .git/hooks/" + brand.BinaryName + "-index.sh despite non-default core.hooksPath")
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect .git/hooks/liza-index.sh: %v", err)
	}
	assertIndexingActivationDefaultHooksUnmanaged(t, projectDir)

	logPath := filepath.Join(t.TempDir(), "custom-hooks-stacklit.log")
	runIndexingActivationGit(t, projectDir, logPath, "commit", "--allow-empty", "-m", "Trigger effective hooks path")
	want := "generate-json -o stacklit.json --parse-workers 3\ninit-insights -i stacklit.json -o stacklit-insights.json\ngenerate-json -o stacklit.json --parse-workers 3\n"
	if got := readIndexingActivationFile(t, logPath); got != want {
		t.Fatalf("custom hooksPath Stacklit calls = %q, want effective hook path to run Liza indexing hook", got)
	}
}

func TestIndexingActivationUnsafeHooksPathReportsDiagnosticWithoutInertDefaultHook(t *testing.T) {
	projectDir := newIndexingActivationProject(t)
	t.Setenv(stacklit.EnvEnableStacklit, "true")
	t.Setenv(scipsearch.EnvEnableScipSearch, "false")
	t.Setenv(semble.EnvEnableSemble, "false")

	hooksPath := filepath.Join(projectDir, ".git", "hooks-file")
	writeIndexingActivationFile(t, hooksPath, "not a directory\n")
	runIndexingActivationGitPlain(t, projectDir, "config", "core.hooksPath", hooksPath)

	err := commands.InitPairingCommand(commands.InitPairingParams{Agents: []string{"codex"}})
	if err == nil {
		t.Fatal("InitPairingCommand() error = nil, want clear hooksPath diagnostic")
	}
	assertIndexingActivationContainsAll(t, err.Error(), "hooks-file", "not a directory")
	if _, statErr := os.Stat(filepath.Join(projectDir, ".git", "hooks", brand.BinaryName+"-index.sh")); statErr == nil {
		t.Fatal("InitPairingCommand installed inert .git/hooks/" + brand.BinaryName + "-index.sh after unsafe core.hooksPath failure")
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("inspect .git/hooks/liza-index.sh: %v", statErr)
	}
	assertIndexingActivationDefaultHooksUnmanaged(t, projectDir)
}

func assertIndexingActivationDefaultHooksUnmanaged(t *testing.T, projectDir string) {
	t.Helper()

	for _, hook := range pairingindex.DefaultLifecycleHooks() {
		path := filepath.Join(projectDir, ".git", "hooks", hook)
		content, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("read default hook %s: %v", path, err)
		}
		assertIndexingActivationContainsNone(t, string(content),
			pairingindex.ManagedHookMarker,
			brand.BinaryName+"-index.sh",
		)
	}
}

func writeIndexingActivationFakeScipGo(t *testing.T) string {
	t.Helper()

	// Write over the placeholder indexers SetupGlobalLiza installs in $HOME/bin
	// rather than into a directory of our own. Those placeholders answer "usage"
	// to everything and produce no index, and they win the PATH lookup here, so
	// a second copy elsewhere is never reached — the hook then reports failure
	// to index and the test looks for an artifact nothing wrote.
	dir := filepath.Join(os.Getenv("HOME"), "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", dir, err)
	}
	scipGoPath := filepath.Join(dir, "scip-go")
	scipGoScript := `#!/bin/sh
args="$*"
output=""
while [ "$#" -gt 0 ]; do
	if [ "$1" = "--output" ]; then
		shift
		output="$1"
	fi
	shift
done
if [ -n "$output" ]; then
	printf '%s\n' "scip-go $args" > "$output"
fi
`
	if err := os.WriteFile(scipGoPath, []byte(scipGoScript), 0o755); err != nil {
		t.Fatalf("WriteFile(fake scip-go): %v", err)
	}
	scipSearchPath := filepath.Join(dir, "scip-search")
	scipSearchScript := `#!/bin/sh
args="$*"
indexes=""
output=""
while [ "$#" -gt 0 ]; do
	case "$1" in
		--index)
			shift
			indexes="${indexes}
$1"
			;;
		--out)
			shift
			output="$1"
			;;
	esac
	shift
done
if [ -n "$output" ]; then
	: > "$output"
	printf '%s\n' "$indexes" | while IFS= read -r index; do
		if [ -n "$index" ] && [ -f "$index" ]; then
			cat "$index" >> "$output"
		fi
	done
	printf '%s\n' "scip-search $args" >> "$output"
fi
`
	if err := os.WriteFile(scipSearchPath, []byte(scipSearchScript), 0o755); err != nil {
		t.Fatalf("WriteFile(fake scip-search): %v", err)
	}
	return dir
}

func runIndexingActivationGitPlain(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func runIndexingActivationGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
