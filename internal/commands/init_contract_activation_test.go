package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gitpkg "github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestInitContractActivation_DefaultLocalFallback(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	projectRoot := setupGitRepo(t)
	t.Cleanup(func() { os.RemoveAll(projectRoot) })
	home := setupGlobalLiza(t)
	t.Chdir(projectRoot)
	global := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(global), 0755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(projectRoot, "CLAUDE.md"), global} {
		if err := os.WriteFile(path, []byte("user-owned\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := InitPairingCommand(InitPairingParams{Agents: []string{"claude"}, Stdin: strings.NewReader(""), AutoConfirm: true}); err != nil {
			t.Fatalf("init with available local fallback: %v", err)
		}
		local := filepath.Join(projectRoot, "CLAUDE.local.md")
		target := filepath.Join(home, paths.GlobalDirName(), "CORE.md")
		if !isLizaSymlink(local, target) {
			t.Fatal("init succeeded without activating the declared local fallback")
		}
		statePath, err := repoContractActivationStatePath(projectRoot)
		if err != nil {
			t.Fatal(err)
		}
		state, _, err := readRepoContractActivationState(statePath)
		if err != nil || state.ProviderPaths["claude"] != "CLAUDE.local.md" {
			t.Fatalf("fallback activation not recorded: %+v, %v", state, err)
		}
		if out, err := gitpkg.Output(projectRoot, "check-ignore", "CLAUDE.local.md"); err != nil || strings.TrimSpace(string(out)) != "CLAUDE.local.md" {
			t.Fatalf("managed local fallback not excluded: %q, %v", out, err)
		}
	}
	for _, path := range []string{filepath.Join(projectRoot, "CLAUDE.md"), global} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "user-owned\n" {
			t.Fatalf("user contract changed at %s: %q, %v", path, data, err)
		}
	}
}

func TestInitContractActivation_UnplacedStopsBeforeHooks(t *testing.T) {
	for _, full := range []bool{false, true} {
		name := "pairing"
		if full {
			name = "full"
		}
		t.Run(name, func(t *testing.T) {
			projectRoot := setupGitRepo(t)
			t.Cleanup(func() { os.RemoveAll(projectRoot) })
			home := setupGlobalLiza(t)
			t.Chdir(projectRoot)
			global := filepath.Join(home, ".claude", "CLAUDE.md")
			if err := os.MkdirAll(filepath.Dir(global), 0755); err != nil {
				t.Fatal(err)
			}
			occupied := []string{filepath.Join(projectRoot, "CLAUDE.md"), global, filepath.Join(projectRoot, "CLAUDE.local.md")}
			for _, path := range occupied {
				if err := os.WriteFile(path, []byte("user-owned\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			statePath, err := repoContractActivationStatePath(projectRoot)
			if err != nil {
				t.Fatal(err)
			}
			previous := []byte("{\"version\":1,\"provider_paths\":{}}\n")
			if err := os.WriteFile(statePath, previous, 0644); err != nil {
				t.Fatal(err)
			}
			if full {
				testhelpers.CreateCommittedSpecFile(t, projectRoot, "vision.md", "# Vision\n")
				err = InitCommandWithConfig(InitParams{Description: "Test goal", SpecRef: "specs/vision.md", Agents: []string{"claude"}, Stdin: strings.NewReader(""), AutoConfirm: true, PostWorktreeCmd: "true"})
			} else {
				err = InitPairingCommand(InitPairingParams{Agents: []string{"claude"}, Stdin: strings.NewReader(""), AutoConfirm: true})
			}
			if err == nil || !strings.Contains(err.Error(), "not active") || !strings.Contains(err.Error(), "claude") {
				t.Errorf("want actionable inactive-contract error for claude, got %v", err)
			}
			for _, relative := range []string{".claude/settings.json", ".claude/hooks/enforce-init.sh", ".claude/hooks/session-context.sh"} {
				if _, err := os.Lstat(filepath.Join(projectRoot, relative)); !os.IsNotExist(err) {
					t.Errorf("failed activation deployed %s: %v", relative, err)
				}
			}
			current, err := os.ReadFile(statePath)
			if err != nil || !bytes.Equal(current, previous) {
				t.Errorf("failed activation changed metadata: %q, %v", current, err)
			}
			for _, path := range occupied {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "user-owned\n" {
					t.Errorf("user contract changed at %s: %q, %v", path, data, err)
				}
			}
		})
	}
}

func TestInitContractActivation_MixedFailurePreservesPreviousFallback(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	projectRoot := setupGitRepo(t)
	t.Cleanup(func() { os.RemoveAll(projectRoot) })
	home := setupGlobalLiza(t)
	core := filepath.Join(home, paths.GlobalDirName(), "CORE.md")
	claude, _ := providers.EmbeddedCatalog().Resolve("claude")
	if err := os.WriteFile(filepath.Join(projectRoot, "CLAUDE.md"), []byte("user\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := activateProviderContracts(projectRoot, core, []providers.Provider{claude}, providers.EmbeddedCatalog(), contractSymlinkOptions{DefaultAction: "local"}); err != nil {
		t.Fatal(err)
	}
	statePath, err := repoContractActivationStatePath(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	blocked := providers.Provider{ID: "blocked", Setup: providers.Setup{Contract: providers.ContractLinks{RepoFile: "BLOCKED.md"}}}
	if err := os.WriteFile(filepath.Join(projectRoot, "BLOCKED.md"), []byte("user\n"), 0644); err != nil {
		t.Fatal(err)
	}
	err = activateProviderContracts(projectRoot, core, []providers.Provider{claude, blocked}, providers.EmbeddedCatalog(), contractSymlinkOptions{})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Errorf("want failure naming blocked provider, got %v", err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil || !bytes.Equal(after, before) {
		t.Errorf("mixed failure replaced previous metadata: %q, %v", after, err)
	}
	if !isLizaSymlink(filepath.Join(projectRoot, "CLAUDE.local.md"), core) {
		t.Error("mixed failure removed previous valid fallback")
	}
}

func TestInitContractActivation_ExplicitLocalFailureIsNotGlobalSuccess(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	projectRoot := setupGitRepo(t)
	t.Cleanup(func() { os.RemoveAll(projectRoot) })
	home := setupGlobalLiza(t)
	core := filepath.Join(home, paths.GlobalDirName(), "CORE.md")
	global := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(global), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(core, global); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CLAUDE.md", "CLAUDE.local.md"} {
		if err := os.WriteFile(filepath.Join(projectRoot, name), []byte("user-owned\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	claude, _ := providers.EmbeddedCatalog().Resolve("claude")
	err := activateProviderContracts(projectRoot, core, []providers.Provider{claude}, providers.EmbeddedCatalog(), contractSymlinkOptions{DefaultAction: "local"})
	if err == nil || !strings.Contains(err.Error(), "CLAUDE.local.md") {
		t.Fatalf("existing global link must not hide explicit local placement failure: %v", err)
	}
}

func TestInitContractActivation_SkipRequiresExistingContract(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "inactive", true: "existing-local"}[active], func(t *testing.T) {
			projectRoot := setupGitRepo(t)
			t.Cleanup(func() { os.RemoveAll(projectRoot) })
			home := setupGlobalLiza(t)
			core := filepath.Join(home, paths.GlobalDirName(), "CORE.md")
			claude, _ := providers.EmbeddedCatalog().Resolve("claude")
			if active {
				if err := os.Symlink(core, filepath.Join(projectRoot, "CLAUDE.local.md")); err != nil {
					t.Fatal(err)
				}
			}
			err := activateProviderContracts(projectRoot, core, []providers.Provider{claude}, providers.EmbeddedCatalog(), contractSymlinkOptions{DefaultAction: "skip"})
			if active && err != nil {
				t.Fatalf("skip must retain an existing active fallback: %v", err)
			}
			if !active && (err == nil || !strings.Contains(err.Error(), "not active")) {
				t.Fatalf("skip with no active contract must fail: %v", err)
			}
			for _, path := range []string{filepath.Join(projectRoot, "CLAUDE.md"), filepath.Join(home, ".claude", "CLAUDE.md")} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Errorf("skip created a contract at %s: %v", path, err)
				}
			}
		})
	}
}
