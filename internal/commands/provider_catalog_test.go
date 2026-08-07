package commands

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestSetupCommand_ProviderFromCatalog(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	useTestProviderCatalog(t)
	lizaDir := t.TempDir()
	homeDir := t.TempDir()

	if err := SetupCommand(SetupParams{
		TargetDir:   lizaDir,
		HomeDir:     homeDir,
		ProviderIDs: []string{"qwen"},
	}); err != nil {
		t.Fatalf("SetupCommand() error = %v", err)
	}

	linkPath := filepath.Join(homeDir, ".qwen", "skills", "code-review")
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("qwen skill link missing: %v", err)
	}
	if want := filepath.Join(lizaDir, "skills", "code-review"); target != want {
		t.Fatalf("qwen skill link = %q, want %q", target, want)
	}
}

func TestInitPairingCommand_ProviderFromCatalog(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	useTestProviderCatalog(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	fakeHome := setupGlobalLiza(t)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}

	if err := InitPairingCommand(InitPairingParams{Agents: []string{"qwen"}}); err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}

	if _, err := os.Lstat(filepath.Join(gitDir, "QWEN.md")); !os.IsNotExist(err) {
		t.Fatalf("repo QWEN.md should be absent for preferred global activation; got %v", err)
	}
	target, err := os.Readlink(filepath.Join(fakeHome, ".qwen", "QWEN.md"))
	if err != nil {
		t.Fatalf("global QWEN.md not a symlink: %v", err)
	}
	if want := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md"); target != want {
		t.Fatalf("QWEN.md = %q, want %q", target, want)
	}
}

func TestInitPairingCommand_ProviderFromCatalogCreatesNestedContractParent(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	useTestProviderCatalog(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	fakeHome := setupGlobalLiza(t)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}

	if err := InitPairingCommand(InitPairingParams{Agents: []string{"devin"}}); err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}

	linkPath := filepath.Join(gitDir, ".windsurf", "rules", brand.NameLower+".md")
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("%s not a symlink: %v", path.Join(".windsurf", "rules", brand.NameLower+".md"), err)
	}
	if want := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md"); target != want {
		t.Fatalf("Windsurf contract target = %q, want %q", target, want)
	}
}

func TestCachedLegacyCatalogMigratesBuiltInContractPolicy(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	cachePath, metaPath := providers.CachePaths(homeDir)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatalf("create provider cache directory: %v", err)
	}
	legacyCatalog := `version: 1
providers:
  - id: claude
    display_name: Claude
    backend: cli
    setup:
      contract:
        repo_file: CLAUDE.md
        global_fallback: .claude/CLAUDE.md
    runtime:
      executable: claude
  - id: codex
    display_name: Codex
    backend: cli
    setup:
      contract:
        repo_file: AGENTS.md
        global_fallback: .codex/AGENTS.md
    runtime:
      executable: codex
`
	if err := os.WriteFile(cachePath, []byte(legacyCatalog), 0644); err != nil {
		t.Fatalf("write legacy provider cache: %v", err)
	}
	const catalogURL = "https://example.test/provider-catalog.yaml"
	meta, err := json.Marshal(providers.CacheMeta{URL: catalogURL, FetchedAt: time.Now()})
	if err != nil {
		t.Fatalf("marshal provider cache metadata: %v", err)
	}
	if err := os.WriteFile(metaPath, meta, 0644); err != nil {
		t.Fatalf("write provider cache metadata: %v", err)
	}
	t.Setenv(providers.EnvCatalogURL, catalogURL)

	catalog := loadProviderCatalog(homeDir)
	selected, err := resolveCatalogProviders(catalog, []string{"claude", "codex"})
	if err != nil {
		t.Fatalf("resolve cached providers: %v", err)
	}
	if len(selected) != 2 {
		t.Fatalf("resolved providers = %+v, want Claude and Codex", selected)
	}
	for _, provider := range selected {
		if !provider.Setup.Contract.PrefersGlobal() {
			t.Fatalf("cached %s contract = %+v, want embedded v2 policy", provider.ID, provider.Setup.Contract)
		}
	}

	projectRoot := t.TempDir()
	contractTarget := filepath.Join(homeDir, paths.GlobalDirName(), "CORE.md")
	links := []struct {
		repoFile  string
		globalRel string
	}{
		{repoFile: "CLAUDE.md", globalRel: filepath.Join(".claude", "CLAUDE.md")},
		{repoFile: "AGENTS.md", globalRel: filepath.Join(".codex", "AGENTS.md")},
	}
	for _, link := range links {
		repoPath := filepath.Join(projectRoot, link.repoFile)
		globalPath := filepath.Join(homeDir, link.globalRel)
		if err := os.MkdirAll(filepath.Dir(globalPath), 0755); err != nil {
			t.Fatalf("create global contract directory: %v", err)
		}
		if err := os.Symlink(contractTarget, repoPath); err != nil {
			t.Fatalf("create repo %s symlink: %v", link.repoFile, err)
		}
		if err := os.Symlink(contractTarget, globalPath); err != nil {
			t.Fatalf("create global %s symlink: %v", link.repoFile, err)
		}
	}

	createContractSymlinksForProviders(projectRoot, contractTarget, selected, contractSymlinkOptions{})
	for _, link := range links {
		repoPath := filepath.Join(projectRoot, link.repoFile)
		if _, err := os.Lstat(repoPath); !os.IsNotExist(err) {
			t.Errorf("cached legacy catalog should remove duplicate repo %s; got %v", link.repoFile, err)
		}
		globalPath := filepath.Join(homeDir, link.globalRel)
		if target, err := os.Readlink(globalPath); err != nil || target != contractTarget {
			t.Errorf("global %s changed; target = %q, err = %v", link.repoFile, target, err)
		}
	}
}

func TestResolveCatalogProvidersPreservesCustomVersionOneBuiltInContract(t *testing.T) {
	cat, err := providers.ParseCatalog([]byte(`version: 1
providers:
  - id: codex
    display_name: Custom Codex
    backend: cli
    setup:
      contract:
        repo_file: CUSTOM_AGENTS.md
        global_fallback: .custom-codex/instructions.md
        local_fallback: .custom-codex/local.md
        prefer_global: false
    runtime:
      executable: codex
`))
	if err != nil {
		t.Fatalf("ParseCatalog() error = %v", err)
	}

	selected, err := resolveCatalogProviders(cat, []string{"codex"})
	if err != nil {
		t.Fatalf("resolveCatalogProviders() error = %v", err)
	}
	if len(selected) != 1 {
		t.Fatalf("resolved providers = %+v, want one Codex provider", selected)
	}
	contract := selected[0].Setup.Contract
	if contract.RepoFile != "CUSTOM_AGENTS.md" ||
		contract.GlobalFallback != ".custom-codex/instructions.md" ||
		contract.LocalFallback != ".custom-codex/local.md" {
		t.Fatalf("custom v1 contract paths changed: %+v", contract)
	}
	if contract.PreferGlobal == nil || contract.PrefersGlobal() {
		t.Fatalf("custom v1 prefer_global changed: %+v", contract.PreferGlobal)
	}
	if contract.GlobalFallbackEnv != "" || contract.GlobalFallbackEnvSuffix != "" || contract.GlobalFallbackEnvExpandHome {
		t.Fatalf("custom v1 path inherited incompatible environment policy: %+v", contract)
	}
}

func TestResolveCatalogProvidersMigratesKnownLegacyRepoOnlyContract(t *testing.T) {
	cat, err := providers.ParseCatalog([]byte(`version: 1
providers:
  - id: cursor-acp
    display_name: Cursor ACP
    backend: acpx
    setup:
      contract:
        repo_file: AGENTS.md
        global_fallback: .codex/AGENTS.md
    runtime:
      executable: acpx
`))
	if err != nil {
		t.Fatalf("ParseCatalog() error = %v", err)
	}

	selected, err := resolveCatalogProviders(cat, []string{"cursor-acp"})
	if err != nil {
		t.Fatalf("resolveCatalogProviders() error = %v", err)
	}
	if len(selected) != 1 {
		t.Fatalf("resolved providers = %+v, want one Cursor ACP provider", selected)
	}
	contract := selected[0].Setup.Contract
	if contract.RepoFile != "AGENTS.md" || contract.GlobalFallback != "" || contract.PreferGlobal != nil {
		t.Fatalf("known legacy Cursor contract was not migrated to repo-only: %+v", contract)
	}
}

func TestResolveCatalogProvidersMigratesLegacyDevinRepoOnlyContract(t *testing.T) {
	cat, err := providers.ParseCatalog([]byte(`version: 1
providers:
  - id: devin
    display_name: Devin
    backend: cli
    setup:
      contract:
        repo_file: .windsurf/rules/liza.md
        global_fallback: .config/devin/liza.md
    runtime:
      executable: devin
`))
	if err != nil {
		t.Fatalf("ParseCatalog() error = %v", err)
	}

	selected, err := resolveCatalogProviders(cat, []string{"devin"})
	if err != nil {
		t.Fatalf("resolveCatalogProviders() error = %v", err)
	}
	if len(selected) != 1 {
		t.Fatalf("resolved providers = %+v, want one Devin provider", selected)
	}
	contract := selected[0].Setup.Contract
	if contract.GlobalFallback != "" || contract.GlobalFallbackEnv != "" || contract.GlobalFallbackEnvSuffix != "" || contract.PreferGlobal != nil {
		t.Fatalf("legacy Devin contract was not migrated to repo-only: %+v", contract)
	}
}

func TestResolveCatalogProvidersPreservesEmbeddedBrandOwnedDevinRepoFile(t *testing.T) {
	previousNameLower := brand.NameLower
	brand.NameLower = "acme"
	t.Cleanup(func() { brand.NameLower = previousNameLower })

	cat, err := providers.ParseCatalog([]byte(`version: 2
providers:
  - id: devin
    display_name: Devin
    backend: cli
    setup:
      contract:
        repo_file: .windsurf/rules/liza.md
    runtime:
      executable: devin
    acp_runtime:
      provider_key: devin
      executable: acpx
      prompt_transport: stdin
      required_executables: [acpx, devin]
      contract_key: devin
      acpx_agent: devin acp
      acpx_session_name: catalog-devin-{{agentID}}
      acpx_show_args: [--cwd, "{{projectRoot}}", --agent, "{{acpxAgent}}", sessions, show, --name, "{{sessionName}}"]
      acpx_ensure_args: [--cwd, "{{projectRoot}}", --agent, "{{acpxAgent}}", sessions, ensure, --name, "{{sessionName}}"]
      acpx_prompt_args: [--cwd, "{{projectRoot}}", --format, json, --approve-all, --agent, "{{acpxAgent}}", prompt, -s, "{{sessionName}}", --file, "-"]
      acpx_event_mode: json
  - id: custom-provider
    display_name: Custom Provider
    backend: cli
    setup:
      contract:
        repo_file: .custom/provider.md
    runtime:
      executable: custom-provider
`))
	if err != nil {
		t.Fatalf("ParseCatalog() error = %v", err)
	}

	selected, err := resolveCatalogProviders(cat, []string{"devin", "devin-acp", "custom-provider"})
	if err != nil {
		t.Fatalf("resolveCatalogProviders() error = %v", err)
	}
	if got := selected[0].Setup.Contract.RepoFile; got != ".windsurf/rules/acme.md" {
		t.Errorf("Devin repo file = %q, want active-brand embedded path", got)
	}
	if got := selected[1].Setup.Contract.RepoFile; got != ".windsurf/rules/acme.md" {
		t.Errorf("Devin ACP repo file = %q, want active-brand embedded path", got)
	}
	if got := selected[2].Setup.Contract.RepoFile; got != ".custom/provider.md" {
		t.Errorf("custom provider repo file = %q, want loaded catalog value", got)
	}
}

func TestResolveCatalogProvidersPreservesCustomVersionOneRepoOnlyBuiltInContract(t *testing.T) {
	cat, err := providers.ParseCatalog([]byte(`version: 1
providers:
  - id: cursor-acp
    display_name: Custom Cursor ACP
    backend: acpx
    setup:
      contract:
        repo_file: CUSTOM_AGENTS.md
        global_fallback: .custom-cursor/AGENTS.md
        global_fallback_env: CUSTOM_CURSOR_HOME
        global_fallback_env_suffix: AGENTS.md
        global_fallback_env_expand_home: true
        local_fallback: CUSTOM_AGENTS.local.md
        prefer_global: true
    runtime:
      executable: acpx
`))
	if err != nil {
		t.Fatalf("ParseCatalog() error = %v", err)
	}

	selected, err := resolveCatalogProviders(cat, []string{"cursor-acp"})
	if err != nil {
		t.Fatalf("resolveCatalogProviders() error = %v", err)
	}
	if len(selected) != 1 {
		t.Fatalf("resolved providers = %+v, want one Cursor ACP provider", selected)
	}
	contract := selected[0].Setup.Contract
	if contract.RepoFile != "CUSTOM_AGENTS.md" ||
		contract.GlobalFallback != ".custom-cursor/AGENTS.md" ||
		contract.GlobalFallbackEnv != "CUSTOM_CURSOR_HOME" ||
		contract.GlobalFallbackEnvSuffix != "AGENTS.md" ||
		!contract.GlobalFallbackEnvExpandHome ||
		contract.LocalFallback != "CUSTOM_AGENTS.local.md" ||
		!contract.PrefersGlobal() {
		t.Fatalf("custom v1 Cursor contract changed: %+v", contract)
	}
}

func TestResolveCatalogProvidersPreservesBaseCursorCodexPath(t *testing.T) {
	cat, err := providers.ParseCatalog([]byte(`version: 1
providers:
  - id: cursor
    display_name: Custom Cursor
    backend: cli
    setup:
      contract:
        repo_file: AGENTS.md
        global_fallback: .codex/AGENTS.md
        prefer_global: true
    runtime:
      executable: cursor-agent
`))
	if err != nil {
		t.Fatalf("ParseCatalog() error = %v", err)
	}

	selected, err := resolveCatalogProviders(cat, []string{"cursor"})
	if err != nil {
		t.Fatalf("resolveCatalogProviders() error = %v", err)
	}
	if len(selected) != 1 {
		t.Fatalf("resolved providers = %+v, want one Cursor provider", selected)
	}
	contract := selected[0].Setup.Contract
	if contract.GlobalFallback != ".codex/AGENTS.md" || !contract.PrefersGlobal() {
		t.Fatalf("base Cursor custom Codex path changed: %+v", contract)
	}
}

func TestDuplicateNonPreferGlobalSymlinksWarnsAndRetainsBoth(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	projectRoot := t.TempDir()
	contractTarget := filepath.Join(homeDir, paths.GlobalDirName(), "CORE.md")
	repoPath := filepath.Join(projectRoot, "CUSTOM.md")
	globalPath := filepath.Join(homeDir, ".custom", "CUSTOM.md")
	if err := os.MkdirAll(filepath.Dir(globalPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(contractTarget, repoPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(contractTarget, globalPath); err != nil {
		t.Fatal(err)
	}
	provider := providers.Provider{
		ID: "custom",
		Setup: providers.Setup{Contract: providers.ContractLinks{
			RepoFile:       "CUSTOM.md",
			GlobalFallback: ".custom/CUSTOM.md",
		}},
	}

	stderr, err := captureStderrForTest(func() error {
		createContractSymlinksForProviders(projectRoot, contractTarget, []providers.Provider{provider}, contractSymlinkOptions{})
		return nil
	})
	if err != nil {
		t.Fatalf("capture stderr: %v", err)
	}
	if !strings.Contains(stderr, "symlinks at both") {
		t.Fatalf("stderr = %q, want duplicate-symlink warning", stderr)
	}
	for _, path := range []string{repoPath, globalPath} {
		if target, err := os.Readlink(path); err != nil || target != contractTarget {
			t.Errorf("managed link %s changed; target = %q, err = %v", path, target, err)
		}
	}
}

func TestProviderScopedWizardActionOverridesManagedGlobalForNonPreferProvider(t *testing.T) {
	for _, action := range []string{"rename", "local"} {
		t.Run(action, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			projectRoot := t.TempDir()
			contractTarget := filepath.Join(homeDir, paths.GlobalDirName(), "CORE.md")
			repoPath := filepath.Join(projectRoot, "CUSTOM.md")
			globalPath := filepath.Join(homeDir, ".custom", "CUSTOM.md")
			if err := os.MkdirAll(filepath.Dir(globalPath), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(repoPath, []byte("user-owned\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(contractTarget, globalPath); err != nil {
				t.Fatal(err)
			}
			provider := providers.Provider{
				ID: "custom",
				Setup: providers.Setup{Contract: providers.ContractLinks{
					RepoFile:       "CUSTOM.md",
					GlobalFallback: ".custom/CUSTOM.md",
					LocalFallback:  "CUSTOM.local.md",
				}},
			}

			createContractSymlinksForProviders(projectRoot, contractTarget, []providers.Provider{provider}, contractSymlinkOptions{
				ProviderActions: map[string]string{"custom": action},
			})

			if target, err := os.Readlink(globalPath); err != nil || target != contractTarget {
				t.Fatalf("managed global target = %q, err = %v; want %q", target, err, contractTarget)
			}
			switch action {
			case "rename":
				if target, err := os.Readlink(repoPath); err != nil || target != contractTarget {
					t.Fatalf("renamed repo action target = %q, err = %v; want %q", target, err, contractTarget)
				}
				if content, err := os.ReadFile(repoPath + ".bak"); err != nil || string(content) != "user-owned\n" {
					t.Fatalf("repo backup = %q, err = %v; want preserved content", content, err)
				}
			case "local":
				if content, err := os.ReadFile(repoPath); err != nil || string(content) != "user-owned\n" {
					t.Fatalf("repo file = %q, err = %v; want preserved content", content, err)
				}
				localPath := filepath.Join(projectRoot, "CUSTOM.local.md")
				if target, err := os.Readlink(localPath); err != nil || target != contractTarget {
					t.Fatalf("local action target = %q, err = %v; want %q", target, err, contractTarget)
				}
			}
		})
	}
}

func useTestProviderCatalog(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if _, err := providers.ParseCatalog([]byte(testProviderCatalogYAML)); err != nil {
		t.Fatalf("test provider catalog YAML invalid: %v", err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(testProviderCatalogYAML))
	}))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on localhost: %v", err)
	}
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	t.Setenv(providers.EnvCatalogURL, server.URL)
	cat, _ := providers.Load(context.Background(), providers.LoadOptions{URL: server.URL, HomeDir: t.TempDir(), Force: true})
	if _, ok := cat.Resolve("qwen"); !ok {
		t.Fatalf("test provider catalog at %s did not load qwen", server.URL)
	}
}

var testProviderCatalogYAML = `version: 1
providers:
  - id: qwen
    display_name: Qwen
    aliases: [qwen-code]
    backend: cli
    detection:
      binaries: [qwen]
      version_args: [--version]
    setup:
      config_dir: .qwen
      skills_dir: skills
      contract:
        repo_file: QWEN.md
        global_fallback: .qwen/QWEN.md
    runtime:
      provider_key: qwen
      executable: qwen
      prompt_transport: stdin
      run_args: [-p]
      logged_run_args: [-p, --output-format, stream-json]
      contract_key: qwen
  - id: devin
    display_name: Devin
    backend: cli
    detection:
      binaries: [devin]
      version_args: [--version]
    setup:
      config_dir: .config/devin
      skills_dir: skills
      contract:
        repo_file: .windsurf/rules/` + brand.NameLower + `.md
        global_fallback: .config/devin/liza.md
    runtime:
      provider_key: devin
      executable: devin
      prompt_transport: arg
      run_args: [--permission-mode, dangerous, -p, "{{prompt}}"]
      logged_run_args: [--permission-mode, dangerous, -p, "{{prompt}}"]
      contract_key: devin
`
