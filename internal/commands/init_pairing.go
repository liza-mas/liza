package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	bashpolicycli "github.com/liza-mas/liza/internal/bash-policy-cli"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/envgate"
	"github.com/liza-mas/liza/internal/functionalclusters"
	"github.com/liza-mas/liza/internal/pairingindex"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/stacklit"
)

// InitPairingParams holds the parameters for InitPairingCommand.
type InitPairingParams struct {
	Agents          []string                  // agent names (e.g. "claude", "codex", "cursor", "opencode", "gemini", "mistral")
	ScipSearch      []string                  // --scip-search: enabled pairing SCIP languages
	ScipSearchPlans []string                  // --scip-search-plan: pairing SCIP root overrides
	Stdin           io.Reader                 // input for interactive prompts (nil = os.Stdin)
	ContractAction  ContractAction            // default action: "global", "rename", "skip", or ""
	ContractActions map[string]ContractAction // provider-scoped wizard actions keyed by canonical provider ID
	AutoConfirm     bool                      // auto-confirm interactive approval prompts
}

// pairingSelection is what the selected providers need from a pairing init:
// the providers that place a repo-root contract and the assets to install.
type pairingSelection struct {
	repoRootAgents      []providers.Provider
	hasClaude           bool
	hasCodex            bool
	hasCursor           bool
	hasOpenCode         bool
	hasCopilot          bool
	hasMistral          bool
	hasBashPolicyClaude bool
	hasBashPolicyCodex  bool
	hasPi               bool
}

// InitPairingCommand creates agent-specific contract symlinks without
// initializing a full branded workspace. This enables pairing mode.
//
// Contract location is provider-specific: documented global instruction paths
// are preferred, while repo-only providers use their repo contract filename.
// For mistral: creates a prompt symlink and sets system_prompt_id in config.toml.
func InitPairingCommand(params InitPairingParams) error {
	rawStdin := params.Stdin
	if rawStdin == nil {
		rawStdin = os.Stdin
	}
	stdin := bufio.NewReader(rawStdin)

	coreFile, err := requirePairingGlobalContract()
	if err != nil {
		return err
	}
	catalog := loadProviderCatalog("")
	selectedProviders, err := resolveCatalogProviders(catalog, canonicalInitProviderIDs(params.Agents))
	if err != nil {
		return err
	}
	selection := classifyPairingProviders(selectedProviders)

	projectRoot, err := activatePairingProjectRoot(params, selection)
	if err != nil {
		return err
	}

	if len(selection.repoRootAgents) > 0 {
		if err := activateProviderContracts(projectRoot, coreFile, selection.repoRootAgents, catalog, contractSymlinkOptions{
			DefaultAction:   params.ContractAction,
			ProviderActions: params.ContractActions,
		}); err != nil {
			return fmt.Errorf("activate provider contracts: %w", err)
		}
	}

	selection.installAssets(projectRoot, rawStdin, stdin, params.AutoConfirm)

	if selection.hasMistral {
		if err := setupMistralContract(coreFile, stdin, params.AutoConfirm); err != nil {
			return fmt.Errorf("mistral setup failed: %w", err)
		}
	}

	return nil
}

// requirePairingGlobalContract returns the global contract file pairing init
// links to, failing when setup has not created it.
func requirePairingGlobalContract() (string, error) {
	globalDir, err := paths.GlobalLizaDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine global config path: %w", err)
	}
	coreFile := filepath.Join(globalDir, globalContractFileName)
	if _, err := os.Stat(coreFile); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("global config not found at %s\nRun '%s setup' first", globalDir, brand.BinaryName)
		}
		return "", fmt.Errorf("cannot access global contract at %s: %w\nRun '%s setup' first", coreFile, err, brand.BinaryName)
	}
	return coreFile, nil
}

// classifyPairingProviders records which assets the selected providers activate.
func classifyPairingProviders(selected []providers.Provider) pairingSelection {
	var selection pairingSelection
	for _, provider := range selected {
		if provider.Setup.Contract.RepoFile != "" {
			selection.repoRootAgents = append(selection.repoRootAgents, provider)
		}
		assets := provider.Setup.ActivationAssets
		if assets.ClaudeSettings {
			selection.hasClaude = true
		}
		if assets.CodexConfig || assets.CodexHooks {
			selection.hasCodex = true
		}
		if providerNeedsCursorHooks(provider) {
			selection.hasCursor = true
		}
		if assets.OpenCodeExecTool {
			selection.hasOpenCode = true
		}
		if assets.CopilotHooks {
			selection.hasCopilot = true
		}
		if assets.MistralPromptConfig {
			selection.hasMistral = true
		}
		if assets.BashPolicyClaude {
			selection.hasBashPolicyClaude = true
		}
		if assets.BashPolicyCodex {
			selection.hasBashPolicyCodex = true
		}
		if assets.PiExtension {
			selection.hasPi = true
		}
	}
	return selection
}

// activatePairingProjectRoot resolves the project root when a selected provider
// needs repo-root operations, then prepares its Semble safety and prewarm and
// activates its index hooks. It returns "" when no repo-root operation applies.
func activatePairingProjectRoot(params InitPairingParams, selection pairingSelection) (string, error) {
	var projectRoot string
	stacklitEnabled := stacklit.RuntimeEnabled()
	scipEnabled := pairingScipEnabled()
	functionalClustersEnabled := functionalclusters.RuntimeEnabled()
	sembleEnabled := semble.RuntimeEnabled()
	if len(selection.repoRootAgents) > 0 || selection.hasClaude {
		lizaPaths, err := paths.LizaPathsFromGit()
		if err != nil {
			return "", fmt.Errorf("failed to determine project root: %w", err)
		}
		projectRoot = lizaPaths.ProjectRoot()
		warnLegacyProjectRoot(projectRoot, lizaPaths.LizaDir())
	}

	if projectRoot != "" && sembleEnabled {
		safety := semble.EnsureProjectRootIgnore(projectRoot)
		if safety.Diagnostic != (semble.Diagnostic{}) {
			return "", fmt.Errorf("semble project-root safety failed: %s", safety.Diagnostic.Message)
		}
		runSembleInitPrewarm(projectRoot)
	}

	if projectRoot != "" && (stacklitEnabled || scipEnabled || sembleEnabled) {
		if err := activateProjectRootIndexing(pairingindex.ActivationPlanOptions{
			RepoRoot:                 projectRoot,
			EnableStacklit:           stacklitEnabled,
			EnableScip:               scipEnabled,
			EnableSemble:             sembleEnabled,
			EnableFunctionalClusters: functionalClustersEnabled,
			ScipLanguages:            params.ScipSearch,
			ScipPlanOverrides:        params.ScipSearchPlans,
		}); err != nil {
			return "", err
		}
	}
	return projectRoot, nil
}

// installAssets writes the settings, hooks and policy files the selected
// providers activate. Every failure is a warning: the contract links are
// already placed.
func (s pairingSelection) installAssets(projectRoot string, rawStdin io.Reader, stdin *bufio.Reader, autoConfirm bool) {
	confirmOptions := embedded.ConfirmOptions{AutoConfirm: autoConfirm}

	// Write/merge .claude/settings.json and deploy hooks
	if s.hasClaude {
		if err := embedded.WriteClaudeSettingsWithOptions(projectRoot, stdin, confirmOptions); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write claude-settings.json: %v\n", err)
		}
	}

	if s.hasCodex {
		if err := embedded.WriteCodexProjectPermissionsWithOptions(projectRoot, stdin, confirmOptions); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write codex config: %v\n", err)
		}
		if err := embedded.WriteCodexProjectHooksWithOptions(projectRoot, stdin, confirmOptions); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write codex hooks: %v\n", err)
		}
	}

	bashPolicyProviderNames := bashPolicyProviders(s.hasBashPolicyClaude, s.hasBashPolicyCodex, s.hasCursor)
	if projectRoot != "" && len(bashPolicyProviderNames) > 0 && bashpolicycli.RuntimeEnabled() {
		if err := embedded.WriteBashPolicyConfigWithOptions(projectRoot, stdin, confirmOptions); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write .bash-policy.yaml: %v\n", err)
		}
	}

	bashPolicyStdin := bashPolicySubprocessStdin(rawStdin, stdin)
	runBashPolicyInits(projectRoot, bashPolicyProviderNames, bashPolicyStdin, autoConfirm)

	if s.hasOpenCode {
		if err := embedded.WriteOpenCodeExecTool(projectRoot); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write opencode exec tool: %v\n", err)
		}
	}

	if s.hasCopilot && projectRoot != "" {
		if err := embedded.WriteCopilotProjectHooksWithOptions(projectRoot, stdin, confirmOptions); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write copilot hooks: %v\n", err)
		}
	}

	if s.hasPi {
		if err := embedded.WritePiInitGate(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write pi init gate: %v\n", err)
		}
	}

	// Remove stale liza MCP server entry from .mcp.json (written by older Liza versions)
	if projectRoot != "" {
		if err := embedded.CleanStaleMCPEntry(projectRoot); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to clean stale .mcp.json entry: %v\n", err)
		}
	}

	// Write .claudeignore template (Claude-specific, non-fatal, prompts if exists)
	if s.hasClaude {
		if err := embedded.WriteClaudeIgnoreWithOptions(projectRoot, stdin, confirmOptions); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write .claudeignore: %v\n", err)
		}
	}
}

func pairingScipEnabled() bool {
	return envgate.Truthy(envgate.Value(scipsearch.EnvEnableScipSearch))
}

// activateProjectRootIndexing installs the lifecycle hooks and index script
// that keep repo-root indexes fresh, for both Pairing and MAS init.
func activateProjectRootIndexing(opts pairingindex.ActivationPlanOptions) error {
	plan, err := pairingindex.PlanActivation(opts)
	if err != nil {
		return err
	}
	writePairingScipSkipDiagnostics(plan.Skips)
	for _, language := range plan.Unindexable {
		fmt.Fprintf(os.Stderr, "Warning: scip-search %s is configured but has no indexable source root yet; the orchestrator adds it to the index hooks on start once its sources exist.\n", language)
	}
	if !plan.Active() {
		return nil
	}
	if _, err := pairingindex.InstallActivation(plan.Install); err != nil {
		return fmt.Errorf("index activation failed: %w", err)
	}
	return nil
}

func writePairingScipSkipDiagnostics(skips []scipsearch.PairingPlanSkip) {
	for _, skip := range skips {
		fmt.Fprintf(os.Stderr, "Warning: skipped scip-search %s: %s\n", skip.Language, pairingScipSkipReasonText(skip.Reason))
		if len(skip.Candidates) > 0 {
			fmt.Fprintf(os.Stderr, "  Candidate roots: %s\n", strings.Join(skip.Candidates, ", "))
		}
		fmt.Fprintf(os.Stderr, "  To require this language, rerun with --scip-search %s and add an explicit --scip-search-plan.\n", skip.Language)
		fmt.Fprintf(os.Stderr, "  Plan syntax: %s\n", pairingScipPlanSyntax(string(skip.Language)))
	}
}

func pairingScipSkipReasonText(reason scipsearch.PairingPlanSkipReason) string {
	switch reason {
	case scipsearch.PairingPlanSkipNoCandidates:
		return "no candidate roots found"
	default:
		return string(reason)
	}
}

func pairingScipPlanSyntax(language string) string {
	switch scipsearch.Language(language) {
	case scipsearch.LanguageGo:
		return "--scip-search-plan go=<module-root>"
	case scipsearch.LanguageTypeScript:
		return "--scip-search-plan typescript=<cwd>,<project-root>"
	case scipsearch.LanguagePython:
		return "--scip-search-plan python=<cwd>[,<target-only>]"
	default:
		return "--scip-search-plan <language>=<values>"
	}
}
