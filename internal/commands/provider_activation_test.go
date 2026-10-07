package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providers"
)

func TestInitPairingCommand_GlobalFirstDeduplicatesUnownedRepoFallback(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	fakeHome := setupGlobalLiza(t)
	contractTarget := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	globalPath := filepath.Join(fakeHome, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(globalPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalPath, []byte("user-owned\n"), 0644); err != nil {
		t.Fatal(err)
	}

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDir) })
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}
	if err := InitPairingCommand(InitPairingParams{Agents: []string{"codex"}}); err != nil {
		t.Fatalf("first InitPairingCommand() error = %v", err)
	}
	repoPath := filepath.Join(gitDir, "AGENTS.md")
	if target, err := os.Readlink(repoPath); err != nil || target != contractTarget {
		t.Fatalf("repo fallback target = %q, err = %v; want %q", target, err, contractTarget)
	}

	if err := os.Remove(globalPath); err != nil {
		t.Fatal(err)
	}
	repairOutput := captureStdout(t, func() {
		if err := InitPairingCommand(InitPairingParams{Agents: []string{"codex"}}); err != nil {
			t.Fatalf("second InitPairingCommand() error = %v", err)
		}
	})
	if strings.Contains(repairOutput, "retaining repo symlink required by another provider") {
		t.Fatalf("repair output falsely attributes Codex's prior activation to another provider:\n%s", repairOutput)
	}
	if !strings.Contains(repairOutput, "removed redundant repo activation") {
		t.Fatalf("repair output missing reconciled activation removal:\n%s", repairOutput)
	}
	if _, err := os.Lstat(repoPath); !os.IsNotExist(err) {
		t.Fatalf("repo fallback should be removed after global activation; got %v", err)
	}
	if target, err := os.Readlink(globalPath); err != nil || target != contractTarget {
		t.Fatalf("global target = %q, err = %v; want %q", target, err, contractTarget)
	}
}

func TestInitPairingCommand_MixedGlobalOutcomesRecordOnlyRepoFallbackOwner(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	fakeHome := setupGlobalLiza(t)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv(providers.EnvCatalogURL, "://invalid")
	contractTarget := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	codexGlobalPath := filepath.Join(fakeHome, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(codexGlobalPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexGlobalPath, []byte("user-owned\n"), 0644); err != nil {
		t.Fatal(err)
	}

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDir) })
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}
	if err := InitPairingCommand(InitPairingParams{Agents: []string{"codex", "opencode"}}); err != nil {
		t.Fatalf("first InitPairingCommand() error = %v", err)
	}

	repoPath := filepath.Join(gitDir, "AGENTS.md")
	if target, err := os.Readlink(repoPath); err != nil || target != contractTarget {
		t.Fatalf("repo fallback target = %q, err = %v; want %q", target, err, contractTarget)
	}
	opencodeGlobalPath := filepath.Join(fakeHome, ".config", "opencode", "AGENTS.md")
	if target, err := os.Readlink(opencodeGlobalPath); err != nil || target != contractTarget {
		t.Fatalf("OpenCode global target = %q, err = %v; want %q", target, err, contractTarget)
	}
	statePath, err := repoContractActivationStatePath(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	state, _, err := readRepoContractActivationState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.ProviderPaths["codex"]; got != "AGENTS.md" {
		t.Fatalf("Codex activation = %q, want AGENTS.md; state = %+v", got, state.ProviderPaths)
	}
	if _, recorded := state.ProviderPaths["opencode"]; recorded {
		t.Fatalf("OpenCode should not own shared repo fallback after global activation; state = %+v", state.ProviderPaths)
	}

	if err := os.Remove(codexGlobalPath); err != nil {
		t.Fatal(err)
	}
	if err := InitPairingCommand(InitPairingParams{Agents: []string{"codex"}}); err != nil {
		t.Fatalf("second InitPairingCommand() error = %v", err)
	}
	if _, err := os.Lstat(repoPath); !os.IsNotExist(err) {
		t.Fatalf("repo fallback should be removed after Codex global repair; got %v", err)
	}
	if target, err := os.Readlink(codexGlobalPath); err != nil || target != contractTarget {
		t.Fatalf("Codex global target after repair = %q, err = %v; want %q", target, err, contractTarget)
	}
	if target, err := os.Readlink(opencodeGlobalPath); err != nil || target != contractTarget {
		t.Fatalf("OpenCode global target after Codex repair = %q, err = %v; want %q", target, err, contractTarget)
	}
	state, _, err = readRepoContractActivationState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.ProviderPaths) != 0 {
		t.Fatalf("activation state = %+v, want no repo owners after repair", state.ProviderPaths)
	}
}

func TestBootstrapLegacyRepoContractActivationsSkipsAmbiguousManagedLinkWithoutEvidence(t *testing.T) {
	projectRoot := setupGitRepo(t)
	defer os.RemoveAll(projectRoot)
	contractTarget := filepath.Join(t.TempDir(), "CORE.md")
	if err := os.Symlink(contractTarget, filepath.Join(projectRoot, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	state := repoContractActivationState{
		Version:       repoContractActivationStateVersion,
		ProviderPaths: make(map[string]string),
	}
	bootstrapLegacyRepoContractActivations(&state, projectRoot, contractTarget, providers.EmbeddedCatalog())

	if len(state.ProviderPaths) != 0 {
		t.Fatalf("legacy activation state = %+v, want no owner for ambiguous AGENTS.md", state.ProviderPaths)
	}
}

func TestBootstrapLegacyRepoContractActivationsPreservesAmbiguousManagedLinkWithEvidence(t *testing.T) {
	projectRoot := setupGitRepo(t)
	defer os.RemoveAll(projectRoot)
	contractTarget := filepath.Join(t.TempDir(), "CORE.md")
	if err := os.Symlink(contractTarget, filepath.Join(projectRoot, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	cursorDir := filepath.Join(projectRoot, ".cursor")
	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cursorDir, "hooks.json"), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state := repoContractActivationState{
		Version:       repoContractActivationStateVersion,
		ProviderPaths: make(map[string]string),
	}

	bootstrapLegacyRepoContractActivations(&state, projectRoot, contractTarget, providers.EmbeddedCatalog())

	if got := state.ProviderPaths["cursor"]; got != "AGENTS.md" {
		t.Fatalf("legacy activation state = %+v, want Cursor owner for AGENTS.md", state.ProviderPaths)
	}
}

func TestRepoContractActivationStatePrunesMissingManagedLinks(t *testing.T) {
	state := repoContractActivationState{
		Version:       repoContractActivationStateVersion,
		ProviderPaths: map[string]string{"cursor": "AGENTS.md"},
	}
	state.pruneMissingLinks(t.TempDir(), filepath.Join(t.TempDir(), "CORE.md"))
	if len(state.ProviderPaths) != 0 {
		t.Fatalf("stale activation state = %+v, want empty", state.ProviderPaths)
	}
}

func TestCreateContractSymlinksRecordsGlobalFirstLocalFallbackActivation(t *testing.T) {
	projectRoot := setupGitRepo(t)
	t.Cleanup(func() { os.RemoveAll(projectRoot) })
	contractTarget := filepath.Join(t.TempDir(), "CORE.md")
	if err := os.WriteFile(filepath.Join(projectRoot, "CUSTOM.md"), []byte("user-owned\n"), 0644); err != nil {
		t.Fatal(err)
	}
	preferGlobal := true
	provider := providers.Provider{
		ID: "custom",
		Setup: providers.Setup{Contract: providers.ContractLinks{
			RepoFile:      "CUSTOM.md",
			LocalFallback: "CUSTOM.local.md",
			PreferGlobal:  &preferGlobal,
		}},
	}

	activations, err := createContractSymlinksForProviders(projectRoot, contractTarget, []providers.Provider{provider}, contractSymlinkOptions{
		ProviderActions: map[string]string{provider.ID: "local"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := activations["custom"]; got != "CUSTOM.local.md" {
		t.Fatalf("local fallback activations = %+v, want custom owner at CUSTOM.local.md", activations)
	}
}

func TestInitPairingCommand_IdempotentRepoOnlyActivationRetainsOwnership(t *testing.T) {
	projectRoot := setupGitRepo(t)
	defer os.RemoveAll(projectRoot)
	fakeHome := setupGlobalLiza(t)
	t.Setenv(providers.EnvCatalogURL, "://invalid")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDir) })
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := InitPairingCommand(InitPairingParams{Agents: []string{"kimi"}}); err != nil {
			t.Fatalf("InitPairingCommand() run %d error = %v", i+1, err)
		}
	}

	statePath, err := repoContractActivationStatePath(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	state, _, err := readRepoContractActivationState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.ProviderPaths["kimi"]; got != "CLAUDE.md" {
		t.Fatalf("Kimi activation after idempotent init = %q, want CLAUDE.md; state = %+v", got, state.ProviderPaths)
	}
	contractTarget := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	if target, err := os.Readlink(filepath.Join(projectRoot, "CLAUDE.md")); err != nil || target != contractTarget {
		t.Fatalf("Kimi contract target after idempotent init = %q, err = %v; want %q", target, err, contractTarget)
	}
}

func TestActivateProviderContractsPreservesRecordedGlobalFirstLocalFallback(t *testing.T) {
	projectRoot := setupGitRepo(t)
	defer os.RemoveAll(projectRoot)
	fakeHome := setupGlobalLiza(t)
	contractTarget := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	if err := os.WriteFile(filepath.Join(projectRoot, "CLAUDE.md"), []byte("user-owned\n"), 0644); err != nil {
		t.Fatal(err)
	}
	preferGlobal := true
	claude := providers.Provider{
		ID: "claude-custom",
		Setup: providers.Setup{Contract: providers.ContractLinks{
			RepoFile:       "CLAUDE.md",
			GlobalFallback: filepath.Join(".claude", "CLAUDE.md"),
			LocalFallback:  "CLAUDE.local.md",
			PreferGlobal:   &preferGlobal,
		}},
	}
	if err := activateProviderContracts(projectRoot, contractTarget, []providers.Provider{claude}, providers.EmbeddedCatalog(), contractSymlinkOptions{
		ProviderActions: map[string]string{claude.ID: "local"},
	}); err != nil {
		t.Fatalf("first activateProviderContracts() error = %v", err)
	}

	statePath, err := repoContractActivationStatePath(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	state, _, err := readRepoContractActivationState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.ProviderPaths[claude.ID]; got != "CLAUDE.local.md" {
		t.Fatalf("activation state = %+v, want %s owner at CLAUDE.local.md", state.ProviderPaths, claude.ID)
	}

	collider := providers.Provider{
		ID: "collider",
		Setup: providers.Setup{Contract: providers.ContractLinks{
			RepoFile: "CLAUDE.local.md",
		}},
	}
	if err := activateProviderContracts(projectRoot, contractTarget, []providers.Provider{collider}, providers.EmbeddedCatalog(), contractSymlinkOptions{}); err != nil {
		t.Fatalf("second activateProviderContracts() error = %v", err)
	}
	localPath := filepath.Join(projectRoot, "CLAUDE.local.md")
	if target, err := os.Readlink(localPath); err != nil || target != contractTarget {
		t.Fatalf("recorded local fallback target = %q, err = %v; want %q", target, err, contractTarget)
	}
	state, _, err = readRepoContractActivationState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.ProviderPaths[collider.ID]; got != "CLAUDE.local.md" {
		t.Fatalf("collider activation = %q, want CLAUDE.local.md; state = %+v", got, state.ProviderPaths)
	}

	if err := activateProviderContracts(projectRoot, contractTarget, []providers.Provider{claude}, providers.EmbeddedCatalog(), contractSymlinkOptions{}); err != nil {
		t.Fatalf("global repair activateProviderContracts() error = %v", err)
	}
	if target, err := os.Readlink(localPath); err != nil || target != contractTarget {
		t.Fatalf("shared local fallback after global repair = %q, err = %v; want %q", target, err, contractTarget)
	}
	globalPath := filepath.Join(fakeHome, ".claude", "CLAUDE.md")
	if target, err := os.Readlink(globalPath); err != nil || target != contractTarget {
		t.Fatalf("Claude global target after repair = %q, err = %v; want %q", target, err, contractTarget)
	}
	state, _, err = readRepoContractActivationState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, recorded := state.ProviderPaths[claude.ID]; recorded {
		t.Fatalf("Claude should release shared local fallback after global activation; state = %+v", state.ProviderPaths)
	}
	if got := state.ProviderPaths[collider.ID]; got != "CLAUDE.local.md" {
		t.Fatalf("collider activation after Claude repair = %q, want CLAUDE.local.md; state = %+v", got, state.ProviderPaths)
	}
}

func TestActivateProviderContractsReconcilesRecordedLocalFallbackAcrossReruns(t *testing.T) {
	projectRoot := setupGitRepo(t)
	defer os.RemoveAll(projectRoot)
	fakeHome := setupGlobalLiza(t)
	contractTarget := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	repoPath := filepath.Join(projectRoot, "CLAUDE.md")
	localPath := filepath.Join(projectRoot, "CLAUDE.local.md")
	globalPath := filepath.Join(fakeHome, ".claude", "CLAUDE.md")
	if err := os.WriteFile(repoPath, []byte("user-owned\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(globalPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalPath, []byte("user-owned\n"), 0644); err != nil {
		t.Fatal(err)
	}
	preferGlobal := true
	claude := providers.Provider{
		ID: "claude-custom",
		Setup: providers.Setup{Contract: providers.ContractLinks{
			RepoFile:       "CLAUDE.md",
			GlobalFallback: filepath.Join(".claude", "CLAUDE.md"),
			LocalFallback:  "CLAUDE.local.md",
			PreferGlobal:   &preferGlobal,
		}},
	}
	statePath, err := repoContractActivationStatePath(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	assertActivation := func(want string) {
		t.Helper()
		state, _, err := readRepoContractActivationState(statePath)
		if err != nil {
			t.Fatal(err)
		}
		got, recorded := state.ProviderPaths[claude.ID]
		if want == "" {
			if recorded {
				t.Fatalf("activation state = %+v, want no %s repo ownership", state.ProviderPaths, claude.ID)
			}
			return
		}
		if !recorded || got != want {
			t.Fatalf("activation state = %+v, want %s owner at %s", state.ProviderPaths, claude.ID, want)
		}
	}

	if err := activateProviderContracts(projectRoot, contractTarget, []providers.Provider{claude}, providers.EmbeddedCatalog(), contractSymlinkOptions{
		ProviderActions: map[string]string{claude.ID: "local"},
	}); err != nil {
		t.Fatalf("local activateProviderContracts() error = %v", err)
	}
	assertActivation("CLAUDE.local.md")
	if target, err := os.Readlink(localPath); err != nil || target != contractTarget {
		t.Fatalf("local fallback target = %q, err = %v; want %q", target, err, contractTarget)
	}

	if err := activateProviderContracts(projectRoot, contractTarget, []providers.Provider{claude}, providers.EmbeddedCatalog(), contractSymlinkOptions{}); err != nil {
		t.Fatalf("blocked rerun activateProviderContracts() error = %v", err)
	}
	assertActivation("CLAUDE.local.md")
	if target, err := os.Readlink(localPath); err != nil || target != contractTarget {
		t.Fatalf("local fallback after blocked rerun = %q, err = %v; want %q", target, err, contractTarget)
	}

	if err := os.Remove(globalPath); err != nil {
		t.Fatal(err)
	}
	if err := activateProviderContracts(projectRoot, contractTarget, []providers.Provider{claude}, providers.EmbeddedCatalog(), contractSymlinkOptions{}); err != nil {
		t.Fatalf("global repair activateProviderContracts() error = %v", err)
	}
	assertActivation("")
	if target, err := os.Readlink(globalPath); err != nil || target != contractTarget {
		t.Fatalf("global target after repair = %q, err = %v; want %q", target, err, contractTarget)
	}
	if _, err := os.Lstat(localPath); !os.IsNotExist(err) {
		t.Fatalf("local fallback should be removed after global repair; got %v", err)
	}
}

func TestActivateProviderContractsReplacesRecordedLocalFallbackWithRepoPath(t *testing.T) {
	tests := []struct {
		name          string
		sharedOldPath bool
	}{
		{name: "removes unshared old path"},
		{name: "preserves shared old path", sharedOldPath: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := setupGitRepo(t)
			defer os.RemoveAll(projectRoot)
			fakeHome := setupGlobalLiza(t)
			contractTarget := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
			repoPath := filepath.Join(projectRoot, "CLAUDE.md")
			localPath := filepath.Join(projectRoot, "CLAUDE.local.md")
			if err := os.WriteFile(repoPath, []byte("user-owned\n"), 0644); err != nil {
				t.Fatal(err)
			}
			preferGlobal := true
			claude := providers.Provider{
				ID: "claude-custom",
				Setup: providers.Setup{Contract: providers.ContractLinks{
					RepoFile:       "CLAUDE.md",
					GlobalFallback: filepath.Join(".claude", "CLAUDE.md"),
					LocalFallback:  "CLAUDE.local.md",
					PreferGlobal:   &preferGlobal,
				}},
			}
			if err := activateProviderContracts(projectRoot, contractTarget, []providers.Provider{claude}, providers.EmbeddedCatalog(), contractSymlinkOptions{
				ProviderActions: map[string]string{claude.ID: "local"},
			}); err != nil {
				t.Fatalf("local activateProviderContracts() error = %v", err)
			}

			collider := providers.Provider{
				ID: "collider",
				Setup: providers.Setup{Contract: providers.ContractLinks{
					RepoFile: "CLAUDE.local.md",
				}},
			}
			if tt.sharedOldPath {
				if err := activateProviderContracts(projectRoot, contractTarget, []providers.Provider{collider}, providers.EmbeddedCatalog(), contractSymlinkOptions{}); err != nil {
					t.Fatalf("collider activateProviderContracts() error = %v", err)
				}
			}

			if err := activateProviderContracts(projectRoot, contractTarget, []providers.Provider{claude}, providers.EmbeddedCatalog(), contractSymlinkOptions{
				ProviderActions: map[string]string{claude.ID: "rename"},
			}); err != nil {
				t.Fatalf("repo replacement activateProviderContracts() error = %v", err)
			}
			if target, err := os.Readlink(repoPath); err != nil || target != contractTarget {
				t.Fatalf("repo replacement target = %q, err = %v; want %q", target, err, contractTarget)
			}

			statePath, err := repoContractActivationStatePath(projectRoot)
			if err != nil {
				t.Fatal(err)
			}
			state, _, err := readRepoContractActivationState(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if got := state.ProviderPaths[claude.ID]; got != "CLAUDE.md" {
				t.Fatalf("Claude replacement activation = %q, want CLAUDE.md; state = %+v", got, state.ProviderPaths)
			}

			if tt.sharedOldPath {
				if target, err := os.Readlink(localPath); err != nil || target != contractTarget {
					t.Fatalf("shared old activation target = %q, err = %v; want %q", target, err, contractTarget)
				}
				if got := state.ProviderPaths[collider.ID]; got != "CLAUDE.local.md" {
					t.Fatalf("collider activation = %q, want CLAUDE.local.md; state = %+v", got, state.ProviderPaths)
				}
				return
			}
			if _, err := os.Lstat(localPath); !os.IsNotExist(err) {
				t.Fatalf("unshared old activation should be removed; got %v", err)
			}
		})
	}
}

func TestActivateProviderContractsReportsSharedPreviousRepoActivation(t *testing.T) {
	projectRoot := setupGitRepo(t)
	defer os.RemoveAll(projectRoot)
	fakeHome := setupGlobalLiza(t)
	contractTarget := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	repoPath := filepath.Join(projectRoot, "AGENTS.md")
	if err := os.Symlink(contractTarget, repoPath); err != nil {
		t.Fatal(err)
	}
	preferGlobal := true
	codex := providers.Provider{
		ID: "codex-custom",
		Setup: providers.Setup{Contract: providers.ContractLinks{
			RepoFile:       "AGENTS.md",
			GlobalFallback: filepath.Join(".codex", "AGENTS.md"),
			PreferGlobal:   &preferGlobal,
		}},
	}
	statePath, err := repoContractActivationStatePath(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	state := repoContractActivationState{
		Version: repoContractActivationStateVersion,
		ProviderPaths: map[string]string{
			codex.ID: "AGENTS.md",
			"cursor": "AGENTS.md",
		},
	}
	if err := writeRepoContractActivationState(statePath, state); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := activateProviderContracts(projectRoot, contractTarget, []providers.Provider{codex}, providers.EmbeddedCatalog(), contractSymlinkOptions{}); err != nil {
			t.Fatalf("activateProviderContracts() error = %v", err)
		}
	})
	if !strings.Contains(output, "AGENTS.md: retaining repo symlink required by another provider") {
		t.Fatalf("shared activation output missing accurate retention reason:\n%s", output)
	}
	if target, err := os.Readlink(repoPath); err != nil || target != contractTarget {
		t.Fatalf("shared repo activation target = %q, err = %v; want %q", target, err, contractTarget)
	}
	state, _, err = readRepoContractActivationState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, recorded := state.ProviderPaths[codex.ID]; recorded {
		t.Fatalf("Codex should release shared repo ownership after global activation; state = %+v", state.ProviderPaths)
	}
	if got := state.ProviderPaths["cursor"]; got != "AGENTS.md" {
		t.Fatalf("Cursor activation = %q, want AGENTS.md; state = %+v", got, state.ProviderPaths)
	}
}

func TestInitPairingCommand_PreservesManagedLinksForUntrustedActivationState(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "malformed JSON", data: []byte("{\n")},
		{name: "future version", data: []byte("{\"version\":2,\"provider_paths\":{\"cursor\":\"AGENTS.md\"}}\n")},
		{name: "empty provider ID", data: []byte("{\"version\":1,\"provider_paths\":{\"\":\"AGENTS.md\"}}\n")},
		{name: "invalid repo path", data: []byte("{\"version\":1,\"provider_paths\":{\"cursor\":\"../AGENTS.md\"}}\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := setupGitRepo(t)
			defer os.RemoveAll(projectRoot)
			fakeHome := setupGlobalLiza(t)
			contractTarget := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
			repoPath := filepath.Join(projectRoot, "AGENTS.md")
			if err := os.Symlink(contractTarget, repoPath); err != nil {
				t.Fatal(err)
			}
			statePath, err := repoContractActivationStatePath(projectRoot)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(statePath, tt.data, 0600); err != nil {
				t.Fatal(err)
			}

			originalDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chdir(originalDir) })
			if err := os.Chdir(projectRoot); err != nil {
				t.Fatal(err)
			}
			if err := InitPairingCommand(InitPairingParams{Agents: []string{"codex"}}); err != nil {
				t.Fatalf("InitPairingCommand() error = %v", err)
			}

			if target, err := os.Readlink(repoPath); err != nil || target != contractTarget {
				t.Fatalf("managed repo link target = %q, err = %v; want %q", target, err, contractTarget)
			}
			gotState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if string(gotState) != string(tt.data) {
				t.Fatalf("activation state changed to %q, want original %q", gotState, tt.data)
			}
		})
	}
}

func TestReadRepoContractActivationStateRejectsMalformedData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provider-activations.json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err := readRepoContractActivationState(path)
	if err == nil || !strings.Contains(err.Error(), "decode provider contract activation state") {
		t.Fatalf("readRepoContractActivationState() error = %v, want decode diagnostic", err)
	}
}
