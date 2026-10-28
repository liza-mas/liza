package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/providers"
)

func TestContractActionValuesKeepFormerSpelling(t *testing.T) {
	for want, got := range map[string]ContractAction{
		"":       ContractActionDefault,
		"global": ContractActionGlobal,
		"rename": ContractActionRename,
		"local":  ContractActionLocal,
		"skip":   ContractActionSkip,
	} {
		if string(got) != want {
			t.Fatalf("contract action %q changed spelling", want)
		}
	}
}

func TestContractActionPredicatesKeepFormerDecisions(t *testing.T) {
	cases := []struct {
		action          ContractAction
		placesRepo      bool
		allowsPreferred bool
	}{
		{ContractActionDefault, false, true},
		{ContractActionGlobal, false, true},
		{ContractActionRename, true, false},
		{ContractActionLocal, true, false},
		{ContractActionSkip, false, false},
	}
	for _, tc := range cases {
		if got := contractActionPlacesRepoContract(tc.action); got != tc.placesRepo {
			t.Fatalf("contractActionPlacesRepoContract(%q) = %v, want %v", tc.action, got, tc.placesRepo)
		}
		if got := contractActionAllowsPreferredGlobal(tc.action); got != tc.allowsPreferred {
			t.Fatalf("contractActionAllowsPreferredGlobal(%q) = %v, want %v", tc.action, got, tc.allowsPreferred)
		}
	}
}

func TestPairingAndSetupNamedValuesKeepFormerLiterals(t *testing.T) {
	if globalContractFileName != "CORE.md" {
		t.Fatalf("globalContractFileName changed")
	}
	if setupAgentToolsFile != "AGENT_TOOLS.md" || !userCustomizableFiles["AGENT_TOOLS.md"] {
		t.Fatalf("setup agent-tools file name changed")
	}
	if mistralConfigDirName != ".vibe" || mistralPromptsDirName != "prompts" || mistralConfigFileName != "config.toml" || mistralSystemPromptKey != "system_prompt_id" {
		t.Fatalf("mistral path or key names changed")
	}
	if initDirMode != 0755 || initFileMode != 0644 {
		t.Fatalf("file modes changed: dir %o file %o", initDirMode, initFileMode)
	}
	if catalogContractPolicyVersion != 2 {
		t.Fatalf("catalog contract policy version changed")
	}
}

func TestClassifyPairingProvidersMapsActivationAssets(t *testing.T) {
	catalog := providers.EmbeddedCatalog()
	selected, err := resolveCatalogProviders(catalog, []string{"claude", "codex", "mistral", "opencode", "copilot", "pi"})
	if err != nil {
		t.Fatalf("resolve providers: %v", err)
	}

	selection := classifyPairingProviders(selected)

	flags := map[string]bool{
		"claude":   selection.hasClaude,
		"codex":    selection.hasCodex,
		"mistral":  selection.hasMistral,
		"opencode": selection.hasOpenCode,
		"copilot":  selection.hasCopilot,
		"pi":       selection.hasPi,
	}
	for name, got := range flags {
		if !got {
			t.Fatalf("selecting %s did not set its asset flag", name)
		}
	}
	if selection.hasCursor {
		t.Fatalf("cursor hooks selected without the cursor provider")
	}
	if len(selection.repoRootAgents) == 0 {
		t.Fatalf("no repo-root contract provider classified")
	}
	for _, provider := range selection.repoRootAgents {
		if provider.Setup.Contract.RepoFile == "" {
			t.Fatalf("provider %s classified as repo-root without a repo contract file", provider.ID)
		}
	}
}

func TestClassifyPairingProvidersEmptySelection(t *testing.T) {
	if got := classifyPairingProviders(nil); !reflect.DeepEqual(got, pairingSelection{}) {
		t.Fatalf("empty selection classified as non-empty")
	}
}

func TestRequirePairingGlobalContractNamesSetupWhenMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	_, err := requirePairingGlobalContract()
	if err == nil || !strings.Contains(err.Error(), "global config not found at") || !strings.Contains(err.Error(), "setup' first") {
		t.Fatalf("missing global contract error changed: %v", err)
	}
}

func TestContractPlacerActionPrefersProviderEntry(t *testing.T) {
	p := &contractPlacer{options: contractSymlinkOptions{
		DefaultAction:   ContractActionGlobal,
		ProviderActions: map[string]ContractAction{"claude": ContractActionSkip, "codex": ContractActionDefault},
	}}

	if got := p.actionFor("claude"); got != ContractActionSkip {
		t.Fatalf("provider action not preferred: %q", got)
	}
	if got := p.actionFor("codex"); got != ContractActionDefault {
		t.Fatalf("explicit empty provider action must override the default: %q", got)
	}
	if got := p.actionFor("gemini"); got != ContractActionGlobal {
		t.Fatalf("default action not applied: %q", got)
	}
}

func TestCreateRepoContractReportsOccupiedPath(t *testing.T) {
	projectRoot := t.TempDir()
	target := filepath.Join(t.TempDir(), "CORE.md")
	p := &contractPlacer{projectRoot: projectRoot, contractTarget: target, repoActivations: map[string]string{}}
	agent := providers.Provider{ID: "claude"}

	free := filepath.Join(projectRoot, "CLAUDE.md")
	if !p.createRepoContract(agent, "CLAUDE.md", free) {
		t.Fatalf("free repo path must be handled by the link step")
	}
	if p.repoActivations["claude"] != "CLAUDE.md" {
		t.Fatalf("created link not recorded: %v", p.repoActivations)
	}

	occupied := filepath.Join(projectRoot, "AGENTS.md")
	if err := os.WriteFile(occupied, []byte("user-owned\n"), initFileMode); err != nil {
		t.Fatal(err)
	}
	if p.createRepoContract(providers.Provider{ID: "codex"}, "AGENTS.md", occupied) {
		t.Fatalf("occupied repo path must fall through to the action step")
	}
	if _, recorded := p.repoActivations["codex"]; recorded {
		t.Fatalf("occupied path recorded as an activation")
	}
	if content, err := os.ReadFile(occupied); err != nil || string(content) != "user-owned\n" {
		t.Fatalf("user-owned file was modified")
	}
}

func TestPrepareSetupRefusesUnreadableAgentToolsBeforeCreatingTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "global")

	_, err := prepareSetup(SetupParams{TargetDir: target, AgentToolsPath: filepath.Join(t.TempDir(), "missing.md")})

	if err == nil || !strings.Contains(err.Error(), "failed to read custom agent-tools file") {
		t.Fatalf("expected custom agent-tools read error, got %v", err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("target directory created before the early validation failed")
	}
}

func TestPrepareSetupAutoSkipsEmbeddedAgentToolsOnlyWithCustomFile(t *testing.T) {
	home := t.TempDir()
	custom := filepath.Join(t.TempDir(), "tools.md")
	if err := os.WriteFile(custom, []byte("custom\n"), initFileMode); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "global")

	with, err := prepareSetup(SetupParams{TargetDir: target, HomeDir: home, AgentToolsPath: custom})
	if err != nil {
		t.Fatalf("prepareSetup with custom file: %v", err)
	}
	without, err := prepareSetup(SetupParams{TargetDir: target, HomeDir: home})
	if err != nil {
		t.Fatalf("prepareSetup without custom file: %v", err)
	}

	if !with.autoSkip[filepath.Join(target, "AGENT_TOOLS.md")] {
		t.Fatalf("embedded AGENT_TOOLS.md not auto-skipped for a custom file")
	}
	if without.autoSkip != nil {
		t.Fatalf("auto-skip set without a custom file")
	}
}

func TestWriteCustomAgentToolsBacksUpExistingFile(t *testing.T) {
	target := t.TempDir()
	existing := filepath.Join(target, "AGENT_TOOLS.md")
	if err := os.WriteFile(existing, []byte("previous\n"), initFileMode); err != nil {
		t.Fatal(err)
	}

	written, err := writeCustomAgentTools(target, []byte("custom\n"))
	if err != nil {
		t.Fatalf("writeCustomAgentTools: %v", err)
	}

	if written != existing {
		t.Fatalf("written path = %q, want the AGENT_TOOLS.md target", written)
	}
	content, err := os.ReadFile(existing)
	if err != nil || !strings.Contains(string(content), "custom") {
		t.Fatalf("custom content not written: %v", err)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	backups := 0
	for _, entry := range entries {
		if entry.Name() != "AGENT_TOOLS.md" && strings.HasPrefix(entry.Name(), "AGENT_TOOLS.md") {
			backups++
		}
	}
	if backups != 1 {
		t.Fatalf("expected one backup of the previous AGENT_TOOLS.md, found %d", backups)
	}
}
