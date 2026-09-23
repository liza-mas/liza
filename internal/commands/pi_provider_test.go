package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/paths"
)

func TestEnsureProviderSpawnAssetsRepairsPiGate(t *testing.T) {
	useTestProviderCatalog(t)
	fakeHome := setupGlobalLiza(t)

	// Unknown CLIs are a no-op.
	if err := EnsureProviderSpawnAssets("totally-unknown-cli"); err != nil {
		t.Fatalf("EnsureProviderSpawnAssets(unknown) error = %v", err)
	}

	// A deleted gate self-heals on the spawn path.
	gatePath := filepath.Join(fakeHome, paths.GlobalDirName(), "extensions", "init-gate.ts")
	if err := os.Remove(gatePath); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove gate: %v", err)
	}
	if err := EnsureProviderSpawnAssets("pi"); err != nil {
		t.Fatalf("EnsureProviderSpawnAssets(pi) error = %v", err)
	}
	if _, err := os.Stat(gatePath); err != nil {
		t.Fatalf("pi gate not redeployed: %v", err)
	}

	// A foreign (user-edited) gate is never clobbered.
	if err := os.WriteFile(gatePath, []byte("// hand-tuned\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProviderSpawnAssets("pi"); err != nil {
		t.Fatalf("EnsureProviderSpawnAssets(pi over foreign) error = %v", err)
	}
	content, err := os.ReadFile(gatePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "// hand-tuned\n" {
		t.Fatalf("foreign gate was overwritten:\n%s", string(content))
	}
}

func TestInitPairingCommand_PiProvider(t *testing.T) {
	useTestProviderCatalog(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	fakeHome := setupGlobalLiza(t)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}

	if err := InitPairingCommand(InitPairingParams{Agents: []string{"pi"}}); err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}

	// pi prefers the global contract: ~/.pi/agent/AGENTS.md -> ~/.liza/CORE.md,
	// and the repo AGENTS.md stays clear for providers that need it (none here).
	if _, err := os.Lstat(filepath.Join(gitDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("repo AGENTS.md should be absent for preferred global activation; got %v", err)
	}
	target, err := os.Readlink(filepath.Join(fakeHome, ".pi", "agent", "AGENTS.md"))
	if err != nil {
		t.Fatalf("global pi AGENTS.md not a symlink: %v", err)
	}
	if want := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md"); target != want {
		t.Fatalf("pi AGENTS.md = %q, want %q", target, want)
	}

	// The pi init-gate extension is deployed globally so the pi provider's
	// run_args reference an existing file in every workspace.
	gatePath := filepath.Join(fakeHome, paths.GlobalDirName(), "extensions", "init-gate.ts")
	gate, err := os.ReadFile(gatePath)
	if err != nil {
		t.Fatalf("pi init gate not written: %v", err)
	}
	if !strings.Contains(string(gate), `pi.on("tool_call"`) {
		t.Fatalf("pi init gate missing tool_call enforcement hook:\n%s", string(gate))
	}
	if !strings.Contains(string(gate), "GUARDRAILS.md") {
		t.Fatalf("pi init gate does not enforce GUARDRAILS.md:\n%s", string(gate))
	}
}
