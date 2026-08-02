package commands

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	bashpolicycli "github.com/liza-mas/liza/internal/bash-policy-cli"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/envgate"
	"github.com/liza-mas/liza/internal/functionalclusters"
	gitpkg "github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/initcheck"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pairingindex"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/projectdetect"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/stacklit"
	"github.com/liza-mas/liza/internal/termutil"
)

var (
	initSembleLookPath semble.ExecutableLookup
	initSembleRunner   semble.CommandRunner

	initBashPolicyLookPath bashpolicycli.ExecutableLookup
	initBashPolicyRunner   bashpolicycli.CommandRunner
)

// InitParams holds the parameters for InitCommand.
type InitParams struct {
	Description                     string
	SpecRef                         string
	ConfigPath                      string // --config: path to pipeline YAML
	EntryPoint                      string // --entry-point: name of entry-point in config
	Branch                          string // --branch: integration branch name (default: "integration")
	PostWorktreeCmd                 string // --post-worktree-cmd: shell command to run after worktree creation
	CopyWorktreeEnvFiles            bool   // --copy-worktree-env-files: copy ignored root env files into task worktrees
	AutoResume                      bool   // --auto-resume: automatically resume at checkpoint and sprint completion
	NoFollowUp                      bool   // --no-follow-up: suppress top-level pipeline-transitions after the entry subpipeline
	MaxGlobalIntegrationGenerations int
	DefaultCLI                      string   // --default-cli: default CLI for agent spawning
	DefaultDoerCLI                  string   // --default-doer-cli: default CLI for doer and orchestrator agent spawning
	DefaultReviewerCLI              string   // --default-reviewer-cli: default CLI for reviewer agent spawning
	ScipSearch                      []string // --scip-search: enabled SCIP languages
	ScipSearchPlans                 []string // --scip-search-plan: pairing SCIP root overrides
	Agents                          []string // --claude, --codex, --cursor, --opencode, --gemini, --mistral
	Stdin                           io.Reader
	ForceInteractive                bool              // bypass TTY check (for testing)
	ContractActions                 map[string]string // provider-scoped wizard actions keyed by canonical provider ID
	AutoConfirm                     bool              // auto-confirm interactive approval prompts
}

// InitPairingParams holds the parameters for InitPairingCommand.
type InitPairingParams struct {
	Agents          []string          // agent names (e.g. "claude", "codex", "cursor", "opencode", "gemini", "mistral")
	ScipSearch      []string          // --scip-search: enabled pairing SCIP languages
	ScipSearchPlans []string          // --scip-search-plan: pairing SCIP root overrides
	Stdin           io.Reader         // input for interactive prompts (nil = os.Stdin)
	ContractAction  string            // default action: "global", "rename", "skip", or ""
	ContractActions map[string]string // provider-scoped wizard actions keyed by canonical provider ID
	AutoConfirm     bool              // auto-confirm interactive approval prompts
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
	confirmOptions := embedded.ConfirmOptions{AutoConfirm: params.AutoConfirm}

	globalDir, err := paths.GlobalLizaDir()
	if err != nil {
		return fmt.Errorf("failed to determine global config path: %w", err)
	}
	coreFile := filepath.Join(globalDir, "CORE.md")
	if _, err := os.Stat(coreFile); os.IsNotExist(err) {
		return fmt.Errorf("global config not found at %s\nRun '%s setup' first", globalDir, brand.BinaryName)
	}
	catalog := loadProviderCatalog("")
	selectedProviders, err := resolveCatalogProviders(catalog, canonicalInitProviderIDs(params.Agents))
	if err != nil {
		return err
	}

	// Classify agents
	var repoRootAgents []providers.Provider
	hasClaude := false
	hasCodex := false
	hasCursor := false
	hasOpenCode := false
	hasMistral := false
	hasBashPolicyClaude := false
	hasBashPolicyCodex := false
	for _, provider := range selectedProviders {
		if provider.Setup.Contract.RepoFile != "" {
			repoRootAgents = append(repoRootAgents, provider)
		}
		assets := provider.Setup.ActivationAssets
		if assets.ClaudeSettings {
			hasClaude = true
		}
		if assets.CodexConfig || assets.CodexHooks {
			hasCodex = true
		}
		if providerNeedsCursorHooks(provider) {
			hasCursor = true
		}
		if assets.OpenCodeExecTool {
			hasOpenCode = true
		}
		if assets.MistralPromptConfig {
			hasMistral = true
		}
		if assets.BashPolicyClaude {
			hasBashPolicyClaude = true
		}
		if assets.BashPolicyCodex {
			hasBashPolicyCodex = true
		}
	}

	// Resolve project root for repo-root operations
	var projectRoot string
	stacklitEnabled := stacklit.RuntimeEnabled()
	scipEnabled := pairingScipEnabled()
	functionalClustersEnabled := functionalclusters.RuntimeEnabled()
	sembleEnabled := semble.RuntimeEnabled()
	if len(repoRootAgents) > 0 || hasClaude {
		lizaPaths, err := paths.LizaPathsFromGit()
		if err != nil {
			return fmt.Errorf("failed to determine project root: %w", err)
		}
		projectRoot = lizaPaths.ProjectRoot()
		warnLegacyProjectRoot(projectRoot, lizaPaths.LizaDir())
	}

	if projectRoot != "" && sembleEnabled {
		safety := semble.EnsureProjectRootIgnore(projectRoot)
		if safety.Diagnostic != (semble.Diagnostic{}) {
			return fmt.Errorf("semble project-root safety failed: %s", safety.Diagnostic.Message)
		}
		runSembleInitPrewarm(projectRoot)
	}

	if projectRoot != "" && (stacklitEnabled || scipEnabled) {
		var scipPlans []scipsearch.LanguageAggregatePlan
		if scipEnabled {
			overrides, err := scipsearch.ParsePairingCommandOverrides(projectRoot, params.ScipSearchPlans)
			if err != nil {
				return fmt.Errorf("scip-search pairing plan failed: %w", err)
			}
			planResult, err := scipsearch.PlanPairingCommands(scipsearch.PairingPlanOptions{
				ProjectRoot:       projectRoot,
				ExplicitLanguages: params.ScipSearch,
				CommandOverrides:  overrides,
				SkipUnresolved:    len(params.ScipSearch) == 0,
			})
			if err != nil {
				return fmt.Errorf("scip-search pairing plan failed: %w", err)
			}
			writePairingScipSkipDiagnostics(planResult.Skips)
			scipPlans = planResult.Plans
		}
		if stacklitEnabled || len(scipPlans) > 0 {
			if _, err := pairingindex.InstallActivation(pairingindex.InstallActivationOptions{
				RepoRoot:                 projectRoot,
				EnableStacklit:           stacklitEnabled,
				EnableFunctionalClusters: functionalClustersEnabled && stacklitEnabled,
				ScipPlans:                scipPlans,
			}); err != nil {
				return fmt.Errorf("pairing index activation failed: %w", err)
			}
		}
	}

	if len(repoRootAgents) > 0 {
		if err := activateProviderContracts(projectRoot, coreFile, repoRootAgents, catalog, contractSymlinkOptions{
			DefaultAction:   params.ContractAction,
			ProviderActions: params.ContractActions,
		}); err != nil {
			return fmt.Errorf("activate provider contracts: %w", err)
		}
	}

	// Write/merge .claude/settings.json and deploy hooks
	if hasClaude {
		if err := embedded.WriteClaudeSettingsWithOptions(projectRoot, stdin, confirmOptions); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write claude-settings.json: %v\n", err)
		}
	}

	if hasCodex {
		if err := embedded.WriteCodexProjectPermissionsWithOptions(projectRoot, stdin, confirmOptions); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write codex config: %v\n", err)
		}
		if err := embedded.WriteCodexProjectHooksWithOptions(projectRoot, stdin, confirmOptions); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write codex hooks: %v\n", err)
		}
	}

	bashPolicyProviderNames := bashPolicyProviders(hasBashPolicyClaude, hasBashPolicyCodex, hasCursor)
	if projectRoot != "" && len(bashPolicyProviderNames) > 0 && bashpolicycli.RuntimeEnabled() {
		if err := embedded.WriteBashPolicyConfigWithOptions(projectRoot, stdin, confirmOptions); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write .bash-policy.yaml: %v\n", err)
		}
	}

	bashPolicyStdin := bashPolicySubprocessStdin(rawStdin, stdin)
	runBashPolicyInits(projectRoot, bashPolicyProviderNames, bashPolicyStdin, params.AutoConfirm)

	if hasOpenCode {
		if err := embedded.WriteOpenCodeExecTool(projectRoot); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write opencode exec tool: %v\n", err)
		}
	}

	// Remove stale liza MCP server entry from .mcp.json (written by older Liza versions)
	if projectRoot != "" {
		if err := embedded.CleanStaleMCPEntry(projectRoot); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to clean stale .mcp.json entry: %v\n", err)
		}
	}

	// Write .claudeignore template (Claude-specific, non-fatal, prompts if exists)
	if hasClaude {
		if err := embedded.WriteClaudeIgnoreWithOptions(projectRoot, stdin, confirmOptions); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write .claudeignore: %v\n", err)
		}
	}

	if hasMistral {
		if err := setupMistralContract(coreFile, stdin, params.AutoConfirm); err != nil {
			return fmt.Errorf("mistral setup failed: %w", err)
		}
	}

	return nil
}

func pairingScipEnabled() bool {
	return scipsearch.ParseEnvGate(envgate.Value(scipsearch.EnvEnableScipSearch))
}

func writePairingScipSkipDiagnostics(skips []scipsearch.PairingPlanSkip) {
	for _, skip := range skips {
		fmt.Fprintf(os.Stderr, "Warning: skipped scip-search %s: %s\n", skip.Language, pairingScipSkipReasonText(skip.Reason))
		if len(skip.Candidates) > 0 {
			fmt.Fprintf(os.Stderr, "  Candidate roots: %s\n", strings.Join(skip.Candidates, ", "))
		}
		fmt.Fprintf(os.Stderr, "  To require this language, rerun with --scip-search %s and add an explicit --scip-search-plan.\n", skip.Language)
		fmt.Fprintf(os.Stderr, "  Plan syntax: %s\n", pairingScipPlanSyntax(skip.Language))
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
	switch language {
	case "go":
		return "--scip-search-plan go=<module-root>"
	case "typescript":
		return "--scip-search-plan typescript=<cwd>,<project-root>"
	case "python":
		return "--scip-search-plan python=<cwd>[,<target-only>]"
	default:
		return "--scip-search-plan <language>=<values>"
	}
}

// isLizaSymlink returns true if path exists, is a symlink, and points to contractTarget.
func isLizaSymlink(path, contractTarget string) bool {
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return false
	}
	target, err := os.Readlink(path)
	return err == nil && target == contractTarget
}

// CheckContractConfigured checks whether a Liza contract symlink exists for the
// given CLI name, at either the repo root or the CLI's global config directory.
// Returns the path where it was found, or "" if not found.
func CheckContractConfigured(projectRoot, cliName string) string {
	catalog := loadProviderCatalog("")
	provider, ok := catalog.Resolve(cliName)
	if !ok {
		return ""
	}
	contract := provider.Setup.Contract
	if contract.RepoFile == "" && provider.Setup.ActivationAssets.MistralPromptConfig {
		contract.GlobalFallback = filepath.Join(".vibe", "prompts", brand.CanonicalMistralPromptID+".md")
	}
	if contract.RepoFile == "" && contract.GlobalFallback == "" {
		return ""
	}

	homeDir, err := paths.UserHomeDir()
	if err != nil {
		return ""
	}
	contractTarget := filepath.Join(homeDir, paths.GlobalDirName(), "CORE.md")

	// Check repo root
	if contract.RepoFile != "" {
		repoPath := filepath.Join(projectRoot, contract.RepoFile)
		if isLizaSymlink(repoPath, contractTarget) {
			return repoPath
		}
	}

	if contract.LocalFallback != "" {
		localPath := filepath.Join(projectRoot, contract.LocalFallback)
		if isLizaSymlink(localPath, contractTarget) {
			return localPath
		}
	}

	// Check global fallback
	if contract.GlobalFallback != "" {
		globalPath, err := contract.GlobalPath(homeDir)
		if err == nil && isLizaSymlink(globalPath, contractTarget) {
			return globalPath
		}
	}

	return ""
}

// createContractSymlinksForProviders activates each provider's contract at its
// catalog-declared location. Global-first providers establish their active
// global link before removing a managed repo link; repo-only providers retain
// the repo link. User-owned files are never overwritten.
//
// The contractAction parameter controls conflict resolution when set by the
// interactive wizard: "rename" backs up the existing file, "global" uses the
// global fallback, "skip" skips creation. Empty string uses default behavior.
type contractSymlinkOptions struct {
	DefaultAction           string
	ProviderActions         map[string]string
	PreserveRepoPaths       map[string]bool
	SilentPreserveRepoPaths map[string]bool
}

func contractActionPlacesRepoContract(action string) bool {
	return action == "rename" || action == "local"
}

func contractActionAllowsPreferredGlobal(action string) bool {
	return !contractActionPlacesRepoContract(action) && action != "skip"
}

// createContractSymlinksForProviders returns canonical provider ID → normalized
// repo-relative path effectively used after placement. Every repo or local
// outcome must be recorded, including idempotent no-op branches; successful
// global outcomes record nothing.
func createContractSymlinksForProviders(projectRoot, contractTarget string, agents []providers.Provider, options contractSymlinkOptions) map[string]string {
	repoActivations := make(map[string]string)
	recordRepoActivation := func(providerID, candidate string) {
		cleaned, err := normalizeRepoContractPath(candidate)
		if err == nil {
			repoActivations[providerID] = cleaned
		}
	}
	// paths.UserHomeDir rather than os.UserHomeDir: it honours an injected HOME,
	// which os.UserHomeDir ignores on Windows. Without it the tests write into
	// the developer's real profile.
	homeDir, err := paths.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot determine home directory: %v\n", err)
		return repoActivations
	}

	repoPathSelectionCount := make(map[string]int)
	for _, agent := range agents {
		if repoFile := agent.Setup.Contract.RepoFile; repoFile != "" {
			repoPathSelectionCount[filepath.Join(projectRoot, repoFile)]++
		}
	}

	for _, agent := range agents {
		name := agent.Setup.Contract.RepoFile
		if name == "" {
			continue
		}
		repoPath := filepath.Join(projectRoot, name)
		contractAction := options.DefaultAction
		if action, ok := options.ProviderActions[agent.ID]; ok {
			contractAction = action
		}
		globalPath, globalPathErr := agent.Setup.Contract.GlobalPath(homeDir)
		if globalPathErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: cannot resolve %s global contract path: %v; retaining repo activation.\n", agent.ID, globalPathErr)
			globalPath = ""
		}
		hasGlobal := globalPath != ""

		// Step 1: product symlink already exists at either location?
		repoIsLiza := isLizaSymlink(repoPath, contractTarget)
		globalIsLiza := hasGlobal && isLizaSymlink(globalPath, contractTarget)
		if agent.Setup.Contract.PrefersGlobal() && contractActionAllowsPreferredGlobal(contractAction) &&
			ensurePreferredGlobalContract(name, repoPath, globalPath, contractTarget, repoIsLiza, globalIsLiza,
				repoPathSelectionCount[repoPath] > 1 || options.PreserveRepoPaths[repoPath], options.SilentPreserveRepoPaths[repoPath]) {
			continue
		}

		if repoIsLiza && globalIsLiza {
			if !agent.Setup.Contract.PrefersGlobal() {
				recordRepoActivation(agent.ID, name)
			}
			fmt.Fprintf(os.Stderr, "Warning: %s has %s symlinks at both %s and %s; remove one to avoid confusion.\n", name, brand.NameTitle, repoPath, globalPath)
			continue
		}
		if repoIsLiza {
			recordRepoActivation(agent.ID, name)
			fmt.Printf("%s: already correct\n", name)
			continue
		}
		if globalIsLiza && !contractActionPlacesRepoContract(contractAction) {
			fmt.Printf("%s: skipping; %s symlink already exists at %s\n", name, brand.NameTitle, globalPath)
			continue
		}

		// Step 2: create the repo activation when global preference is absent
		// or could not be established safely.
		_, repoErr := os.Lstat(repoPath)
		if repoErr != nil && !os.IsNotExist(repoErr) {
			fmt.Fprintf(os.Stderr, "Warning: cannot stat %s: %v\n", repoPath, repoErr)
			continue
		}
		if os.IsNotExist(repoErr) {
			if err := os.MkdirAll(filepath.Dir(repoPath), 0755); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to create parent directory for %s: %v\n", name, err)
				continue
			}
			if err := os.Symlink(contractTarget, repoPath); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to create %s symlink: %v\n", name, err)
				fmt.Fprintf(os.Stderr, "  On Windows: enable Developer Mode (Settings > System > For developers) or run the shell as Administrator, then retry.\n")
			} else {
				recordRepoActivation(agent.ID, name)
				fmt.Printf("%s → %s\n", name, contractTarget)
			}
			continue
		}

		// Step 3: repo root occupied by non-product file; apply contract action
		if contractAction == "skip" {
			fmt.Printf("%s: skipped (user choice)\n", name)
			continue
		}

		if contractAction == "local" && agent.Setup.Contract.LocalFallback != "" {
			localPath := filepath.Join(projectRoot, agent.Setup.Contract.LocalFallback)
			if _, err := os.Lstat(localPath); err == nil {
				if isLizaSymlink(localPath, contractTarget) {
					recordRepoActivation(agent.ID, agent.Setup.Contract.LocalFallback)
					fmt.Printf("%s: already correct\n", agent.Setup.Contract.LocalFallback)
				} else {
					fmt.Fprintf(os.Stderr, "Warning: %s already exists and is not a %s symlink.\n", agent.Setup.Contract.LocalFallback, brand.NameTitle)
				}
				continue
			}
			if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to create parent directory for %s: %v\n", agent.Setup.Contract.LocalFallback, err)
				continue
			}
			if err := os.Symlink(contractTarget, localPath); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to create %s symlink: %v\n", agent.Setup.Contract.LocalFallback, err)
			} else {
				recordRepoActivation(agent.ID, agent.Setup.Contract.LocalFallback)
				fmt.Printf("%s → %s\n", agent.Setup.Contract.LocalFallback, contractTarget)
			}
			continue
		}

		if contractAction == "rename" {
			bakPath := repoPath + ".bak"
			for i := 1; ; i++ {
				if _, err := os.Lstat(bakPath); os.IsNotExist(err) {
					break
				}
				bakPath = fmt.Sprintf("%s.bak.%d", repoPath, i)
			}
			if err := os.Rename(repoPath, bakPath); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to rename %s to %s: %v\n", name, bakPath, err)
				continue
			}
			fmt.Printf("%s: renamed existing to %s\n", name, filepath.Base(bakPath))
			if err := os.Symlink(contractTarget, repoPath); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to create %s symlink: %v\n", name, err)
				// Restore original to avoid leaving the user with no file at the path
				if restoreErr := os.Rename(bakPath, repoPath); restoreErr != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to restore %s from backup: %v\n", name, restoreErr)
				}
			} else {
				recordRepoActivation(agent.ID, name)
				fmt.Printf("%s → %s\n", name, contractTarget)
			}
			continue
		}

		// Default behavior (contractAction == "" or "global"): try global fallback
		if !hasGlobal {
			fmt.Fprintf(os.Stderr, "Warning: %s already exists and no global fallback configured.\n", name)
			continue
		}

		_, globalErr := os.Lstat(globalPath)
		if globalErr != nil && !os.IsNotExist(globalErr) {
			fmt.Fprintf(os.Stderr, "Warning: cannot stat %s: %v\n", globalPath, globalErr)
			continue
		}
		if os.IsNotExist(globalErr) {
			if err := os.MkdirAll(filepath.Dir(globalPath), 0755); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to create directory %s: %v\n", filepath.Dir(globalPath), err)
				continue
			}
			if err := os.Symlink(contractTarget, globalPath); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to create %s symlink: %v\n", globalPath, err)
				fmt.Fprintf(os.Stderr, "  On Windows: enable Developer Mode (Settings > System > For developers) or run the shell as Administrator, then retry.\n")
			} else {
				fmt.Printf("%s → %s (repo root has existing %s)\n", globalPath, contractTarget, name)
			}
			continue
		}

		// Both locations occupied by non-product files.
		fmt.Fprintf(os.Stderr, "Warning: %s exists at both repo root and %s; cannot place %s contract. Remove or rename one, then re-run.\n", name, globalPath, brand.NameTitle)
	}
	return repoActivations
}

// ensurePreferredGlobalContract makes the active global link authoritative. It
// removes a managed repo link only when no catalog provider requires that repo
// path, and returns false when global activation is unavailable.
func ensurePreferredGlobalContract(name, repoPath, globalPath, contractTarget string, repoIsLiza, globalIsLiza, preserveRepo, silentPreserve bool) bool {
	if globalPath == "" {
		return false
	}
	if !globalIsLiza {
		info, err := os.Lstat(globalPath)
		if err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Warning: cannot stat %s: %v; retaining repo activation.\n", globalPath, err)
			return false
		}
		if err == nil {
			kind := "file"
			if info.Mode()&os.ModeSymlink != 0 {
				kind = "symlink"
			}
			fmt.Fprintf(os.Stderr, "Warning: %s is occupied by a non-%s %s; retaining repo activation.\n", globalPath, brand.NameTitle, kind)
			return false
		}
		if err := os.MkdirAll(filepath.Dir(globalPath), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create directory %s: %v; retaining repo activation.\n", filepath.Dir(globalPath), err)
			return false
		}
		if err := os.Symlink(contractTarget, globalPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create %s symlink: %v; retaining repo activation.\n", globalPath, err)
			fmt.Fprintf(os.Stderr, "  On Windows: enable Developer Mode (Settings > System > For developers) or run the shell as Administrator, then retry.\n")
			return false
		}
		fmt.Printf("%s → %s (preferred global contract)\n", globalPath, contractTarget)
	}
	if repoIsLiza {
		if preserveRepo {
			if !silentPreserve {
				fmt.Printf("%s: retaining repo symlink required by another provider\n", name)
			}
		} else if err := os.Remove(repoPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove redundant %s symlink at %s: %v\n", name, repoPath, err)
		} else {
			fmt.Printf("%s: removed redundant repo symlink; using %s\n", name, globalPath)
		}
	} else if globalIsLiza {
		fmt.Printf("%s: skipping; %s symlink already exists at %s\n", name, brand.NameTitle, globalPath)
	}
	return true
}

// setupMistralContract creates a Mistral prompt symlink to CORE.md and sets system_prompt_id in config.toml.
func setupMistralContract(coreFile string, reader *bufio.Reader, autoConfirm bool) error {
	homeDir, err := paths.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to determine home directory: %w", err)
	}

	vibeDir := filepath.Join(homeDir, ".vibe")
	promptsDir := filepath.Join(vibeDir, "prompts")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", promptsDir, err)
	}

	promptID := brand.CanonicalMistralPromptID

	// Create prompts/<prompt-id>.md symlink (with confirmation for overwrites)
	linkPath := filepath.Join(promptsDir, promptID+".md")
	if err := createSymlinkIdempotent(coreFile, linkPath, reader, true, autoConfirm); err != nil {
		return fmt.Errorf("failed to create %s.md symlink: %w", promptID, err)
	}

	// Update config.toml with the canonical provider prompt ID.
	configPath := filepath.Join(vibeDir, "config.toml")
	if err := setMistralSystemPrompt(configPath, reader, promptID, autoConfirm); err != nil {
		return err
	}

	return nil
}

// setMistralSystemPrompt ensures system_prompt_id is set in ~/.vibe/config.toml.
// Prompts user before modifying an existing file.
func setMistralSystemPrompt(configPath string, reader *bufio.Reader, promptID string, autoConfirm bool) error {
	promptLine := fmt.Sprintf("system_prompt_id = %q", promptID)
	content, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create new config with just the prompt ID — no confirmation needed
			if err := os.WriteFile(configPath, []byte(promptLine+"\n"), 0644); err != nil {
				return fmt.Errorf("failed to create %s: %w", configPath, err)
			}
			fmt.Printf("Created %s with %s\n", configPath, promptLine)
			return nil
		}
		return fmt.Errorf("failed to read %s: %w", configPath, err)
	}

	text := string(content)

	// Already set correctly
	if strings.Contains(text, promptLine) {
		fmt.Printf("%s: system_prompt_id already set to %q\n", configPath, promptID)
		return nil
	}

	// Needs modification — ask user
	fmt.Fprintf(os.Stderr, "%s exists and system_prompt_id is not set to %q.\n", configPath, promptID)
	fmt.Fprintf(os.Stderr, "Set system_prompt_id = %q? (y/n): ", promptID)
	if autoConfirm {
		fmt.Fprintln(os.Stderr, "yes")
	} else {
		response, err := termutil.ReadSingleKey(reader)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to read input, skipping config.toml update\n")
			return nil
		}
		if response != "y" {
			fmt.Fprintln(os.Stderr) // Print newline for clean terminal output
			fmt.Fprintf(os.Stderr, "  Skipped %s\n", configPath)
			return nil
		}
	}

	// Replace existing system_prompt_id line
	lines := strings.Split(text, "\n")
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "system_prompt_id") && strings.Contains(trimmed, "=") {
			lines[i] = promptLine
			found = true
			break
		}
	}

	if !found {
		// Prepend to file
		lines = append([]string{promptLine}, lines...)
	}

	if err := os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}
	fmt.Printf("%s: set system_prompt_id = %q\n", configPath, promptID)
	return nil
}

// InitCommand initializes a new Liza workspace.
// It creates the .liza directory structure, generates initial state.yaml,
// validates the spec file exists, and creates the integration branch.
//
// Prerequisite: 'liza setup' must have been run to populate ~/.liza/.
// The stdin parameter allows for injected input in tests; pass os.Stdin for CLI usage.
func InitCommand(description string, specRef string, stdin io.Reader) error {
	return InitCommandWithConfig(InitParams{
		Description: description,
		SpecRef:     specRef,
		Stdin:       stdin,
	})
}

func runSembleInitPrewarm(projectRoot string) {
	opts := semble.ValidationOptions{
		TargetRoot: projectRoot,
		LookPath:   initSembleLookPath,
		Runner:     initSembleRunner,
	}
	prewarm := semble.ExecutePrewarm(opts)
	if !prewarm.Enabled {
		return
	}
	if prewarm.Diagnostic != (semble.Diagnostic{}) {
		writeSembleDiagnostic(prewarm.Diagnostic)
		return
	}
	if !prewarm.Ready {
		return
	}
	offline := semble.CheckOfflineReadiness(opts)
	if offline.Diagnostic != (semble.Diagnostic{}) {
		writeSembleDiagnostic(offline.Diagnostic)
	}
}

func writeSembleDiagnostic(diagnostic semble.Diagnostic) {
	message := strings.TrimSpace(diagnostic.Message)
	if message == "" {
		return
	}
	if !strings.HasPrefix(message, "semble:") {
		message = "semble: " + message
	}
	fmt.Fprintln(os.Stderr, message)
}

func bashPolicyProviders(hasClaude, hasCodex, hasCursor bool) []string {
	providers := []string{}
	if hasClaude {
		providers = append(providers, bashpolicycli.ProviderClaude)
	}
	if hasCodex {
		providers = append(providers, bashpolicycli.ProviderCodex)
	}
	if hasCursor {
		providers = append(providers, bashpolicycli.ProviderCursor)
	}
	return providers
}

func providerHasAsset(items []providers.Provider, match func(providers.ActivationAssets) bool) bool {
	for _, item := range items {
		if match(item.Setup.ActivationAssets) {
			return true
		}
	}
	return false
}

func providerHas(items []providers.Provider, match func(providers.Provider) bool) bool {
	for _, item := range items {
		if match(item) {
			return true
		}
	}
	return false
}

func providerNeedsCursorHooks(provider providers.Provider) bool {
	return provider.ID == "cursor-acp" || provider.Setup.ActivationAssets.CursorHooks
}

func canonicalInitProviderIDs(ids []string) []string {
	out := make([]string, 0, len(ids)+2)
	seen := make(map[string]bool, len(ids)+2)
	appendID := func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, id := range ids {
		// The --cursor convenience flag prepares Cursor's harness dependencies;
		// cursor-acp is a synthesized ACP provider resolved from the base
		// cursor provider's acp_runtime block.
		if id == "cursor" {
			appendID("claude")
			appendID("codex")
			appendID("cursor-acp")
			continue
		}
		appendID(id)
	}
	return out
}

func bashPolicySubprocessStdin(rawStdin io.Reader, bufferedStdin *bufio.Reader) io.Reader {
	file, ok := rawStdin.(*os.File)
	if !ok {
		return bufferedStdin
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return bufferedStdin
	}
	return file
}

func runBashPolicyInits(projectRoot string, providers []string, stdin io.Reader, autoConfirm bool) {
	for _, provider := range providers {
		if runBashPolicyInit(projectRoot, provider, stdin, autoConfirm) == bashpolicycli.StatusMissing {
			return
		}
	}
}

func runBashPolicyInit(projectRoot, provider string, stdin io.Reader, autoConfirm bool) bashpolicycli.Status {
	result := bashpolicycli.InitHooks(bashpolicycli.InitHooksOptions{
		ProjectRoot: projectRoot,
		Provider:    provider,
		Stdin:       stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		LookPath:    initBashPolicyLookPath,
		Runner:      initBashPolicyRunner,
		AutoConfirm: autoConfirm,
	})
	switch result.Status {
	case bashpolicycli.StatusMissing:
		fmt.Fprintf(os.Stderr, "Warning: bash-policy requested by %s but bash-policy was not found on PATH; run '%s toolchain install --profile full --yes' and source ~/%s/toolchain/env.sh before re-running %s init.\n", bashpolicycli.EnvEnableBashPolicy, brand.BinaryName, brand.GlobalDirName, brand.BinaryName)
	case bashpolicycli.StatusFailed:
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize or activate bash-policy hooks: %s\n", result.Diagnostic())
	}
	return result.Status
}

// InitCommandWithConfig initializes a workspace with optional pipeline config.
func InitCommandWithConfig(params InitParams) error {
	description := params.Description
	specRef := params.SpecRef
	rawStdin := params.Stdin
	configPath := params.ConfigPath
	entryPoint := params.EntryPoint
	branch := params.Branch
	if branch == "" {
		branch = "integration"
	}
	if len(params.ScipSearchPlans) > 0 {
		return fmt.Errorf("--scip-search-plan is only supported for pairing init without a description")
	}

	// Validate branch name using git's own ref format rules
	if err := validateBranchName(branch); err != nil {
		return fmt.Errorf("invalid branch name %q: %w", branch, err)
	}
	if rawStdin == nil {
		rawStdin = os.Stdin
	}
	// Single shared buffered reader — avoids multiple bufio.NewReader instances
	// consuming from the same underlying reader (which causes EOF for later readers).
	stdin := bufio.NewReader(rawStdin)
	confirmOptions := embedded.ConfirmOptions{AutoConfirm: params.AutoConfirm}
	catalog := loadProviderCatalog("")
	selectedProviders, err := resolveCatalogProviders(catalog, canonicalInitProviderIDs(params.Agents))
	if err != nil {
		return err
	}

	// Validate and load pipeline config early (before creating .liza dir)
	var pipelineCfg *pipeline.PipelineConfig
	var pipelineData []byte
	if configPath != "" {
		absConfigPath, err := filepath.Abs(configPath)
		if err != nil {
			return fmt.Errorf("failed to resolve config path: %w", err)
		}
		pipelineCfg, err = pipeline.Load(absConfigPath)
		if err != nil {
			return fmt.Errorf("invalid pipeline config: %w", err)
		}
		pipelineData, err = os.ReadFile(absConfigPath)
		if err != nil {
			return fmt.Errorf("failed to read config file: %w", err)
		}
	} else {
		// Auto-freeze embedded pipeline config when --config is not provided
		pipelineData = embedded.PipelineConfigContent()
		var err error
		pipelineCfg, err = pipeline.LoadFromBytes(pipelineData)
		if err != nil {
			return fmt.Errorf("invalid embedded pipeline config: %w", err)
		}
	}

	// Validate entry-point if provided
	if entryPoint != "" {
		if _, ok := pipelineCfg.Pipeline.EntryPoints[entryPoint]; !ok {
			return fmt.Errorf("entry-point %q not found in pipeline config (available: %s)",
				entryPoint, entryPointNames(pipelineCfg))
		}
	}

	// Get project paths
	lizaPaths, err := paths.LizaPathsFromGit()
	if err != nil {
		return fmt.Errorf("failed to setup paths: %w", err)
	}

	warnLegacyProjectRoot(lizaPaths.ProjectRoot(), lizaPaths.LizaDir())

	// Resolve spec file relative to cwd (where user ran the command), not project root
	specPath, err := filepath.Abs(specRef)
	if err != nil {
		return fmt.Errorf("failed to resolve spec path: %w", err)
	}
	if _, err := os.Stat(specPath); os.IsNotExist(err) {
		return fmt.Errorf("spec file does not exist: %s\nCreate spec document first. See templates/vision-template.md", specRef)
	}
	specRepoRel, err := initcheck.EnsureSpecCommittedClean(lizaPaths.ProjectRoot(), specPath)
	if err != nil {
		return err
	}
	if _, err := initcheck.EnsurePreCommitConfigCommittedClean(lizaPaths.ProjectRoot(), branch); err != nil {
		return err
	}

	// Validate global config exists (setup must have been run).
	globalDir, err := paths.GlobalLizaDir()
	if err != nil {
		return fmt.Errorf("failed to determine global config path: %w", err)
	}
	globalCoreFile := filepath.Join(globalDir, "CORE.md")
	if _, err := os.Stat(globalCoreFile); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("global config not found at %s\nRun '%s setup' first to install contracts, skills, and support docs", globalDir, brand.BinaryName)
		}
		return fmt.Errorf("cannot access global config at %s: %w\nCheck permissions on %s", globalCoreFile, err, globalDir)
	}

	if _, err := integrationBranchNeedsCreate(branch); err != nil {
		return fmt.Errorf("failed to create integration branch %q: %w", branch, err)
	}

	scipSearchConfig, err := scipsearch.ResolveInitConfig(scipsearch.InitOptions{
		ProjectRoot:       lizaPaths.ProjectRoot(),
		ExplicitLanguages: params.ScipSearch,
		EnvValue:          envgate.Value(scipsearch.EnvEnableScipSearch),
	})
	if err != nil {
		return err
	}
	for _, diagnostic := range scipSearchConfig.Diagnostics {
		fmt.Fprintf(os.Stderr, "scip-search: %s\n", diagnostic)
	}
	for _, warning := range scipSearchConfig.Warnings {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", warning)
	}

	if err := confirmMissingPostWorktreeCmd(params, lizaPaths.ProjectRoot(), stdin, rawStdin); err != nil {
		return err
	}

	if _, err := CleanupProjectCommand(CleanupParams{
		ProjectRoot: lizaPaths.ProjectRoot(),
		Stdin:       stdin,
		Stderr:      os.Stderr,
		AutoConfirm: params.AutoConfirm,
	}); err != nil {
		if errors.Is(err, ErrProjectCleanupDeclined) {
			return fmt.Errorf("initialization cancelled by user")
		}
		return err
	}
	runSembleInitPrewarm(lizaPaths.ProjectRoot())

	// Create directory structure
	if err := os.MkdirAll(lizaPaths.LizaDir(), 0755); err != nil {
		return fmt.Errorf("failed to create %s directory: %w", paths.ProjectDirName(), err)
	}

	archiveDir := lizaPaths.ArchiveDir()
	if err := os.Mkdir(archiveDir, 0755); err != nil {
		return fmt.Errorf("failed to create archive directory: %w", err)
	}

	cleanupInit := func() {
		os.RemoveAll(lizaPaths.LizaDir())
	}

	// Write support doc to the project runtime directory (non-fatal).
	if err := embedded.WriteSupportDoc(lizaPaths.LizaDir()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write SUPPORT.md: %v\n", err)
	}

	// Freeze pipeline config into the project runtime directory.
	frozenPath := filepath.Join(lizaPaths.LizaDir(), "pipeline.yaml")
	if err := os.WriteFile(frozenPath, pipelineData, 0644); err != nil {
		cleanupInit()
		return fmt.Errorf("failed to freeze pipeline config: %w", err)
	}

	// Write/merge Claude Code settings and deploy hooks to .claude/
	// This is non-fatal - if it fails, just warn
	// Note: This may prompt user for input if settings file exists
	if err := embedded.WriteClaudeSettingsWithOptions(lizaPaths.ProjectRoot(), stdin, confirmOptions); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write claude-settings.json: %v\n", err)
	}

	hasCursor := providerHas(selectedProviders, providerNeedsCursorHooks)
	hasCodex := providerHasAsset(selectedProviders, func(a providers.ActivationAssets) bool {
		return a.CodexConfig || a.CodexHooks
	})
	if hasCodex {
		if err := embedded.WriteCodexProjectPermissionsWithOptions(lizaPaths.ProjectRoot(), stdin, confirmOptions); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write codex config: %v\n", err)
		}
		if err := embedded.WriteCodexProjectHooksWithOptions(lizaPaths.ProjectRoot(), stdin, confirmOptions); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write codex hooks: %v\n", err)
		}
	}

	bashPolicyProviderNames := bashPolicyProviders(true, hasCodex, hasCursor)
	if lizaPaths.ProjectRoot() != "" && len(bashPolicyProviderNames) > 0 && bashpolicycli.RuntimeEnabled() {
		if err := embedded.WriteBashPolicyConfigWithOptions(lizaPaths.ProjectRoot(), stdin, confirmOptions); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write .bash-policy.yaml: %v\n", err)
		}
	}

	bashPolicyStdin := bashPolicySubprocessStdin(rawStdin, stdin)
	runBashPolicyInits(lizaPaths.ProjectRoot(), bashPolicyProviderNames, bashPolicyStdin, params.AutoConfirm)

	if providerHasAsset(selectedProviders, func(a providers.ActivationAssets) bool { return a.OpenCodeExecTool }) {
		if err := embedded.WriteOpenCodeExecTool(lizaPaths.ProjectRoot()); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write opencode exec tool: %v\n", err)
		}
	}

	// Remove stale liza MCP server entry from .mcp.json (written by older Liza versions)
	if err := embedded.CleanStaleMCPEntry(lizaPaths.ProjectRoot()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to clean stale .mcp.json entry: %v\n", err)
	}

	// Create contract symlinks only for explicitly requested providers
	if len(selectedProviders) > 0 {
		var agents []providers.Provider
		for _, provider := range selectedProviders {
			if provider.Setup.Contract.RepoFile != "" {
				agents = append(agents, provider)
			}
		}
		if len(agents) > 0 {
			if err := activateProviderContracts(lizaPaths.ProjectRoot(), filepath.Join(globalDir, "CORE.md"), agents, catalog, contractSymlinkOptions{
				ProviderActions: params.ContractActions,
			}); err != nil {
				return fmt.Errorf("activate provider contracts: %w", err)
			}
		}
	}

	// Write GUARDRAILS.md template to project root (non-fatal, like claude-settings)
	if err := embedded.WriteGuardrails(lizaPaths.ProjectRoot()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write GUARDRAILS.md: %v\n", err)
	}

	// Write .claudeignore template (non-fatal, prompts if exists)
	if err := embedded.WriteClaudeIgnoreWithOptions(lizaPaths.ProjectRoot(), stdin, confirmOptions); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write .claudeignore: %v\n", err)
	}

	// Auto-suggest post_worktree_cmd if not explicitly set and stdin is a terminal
	postWorktreeCmd := params.PostWorktreeCmd
	if postWorktreeCmd == "" && (params.ForceInteractive || params.AutoConfirm || isInteractive(rawStdin)) {
		root := lizaPaths.ProjectRoot()
		if suggested := detectPostWorktreeCmd(root); suggested != "" {
			fmt.Fprintf(os.Stderr, "Detected %s — set post_worktree_cmd to %q?\n", detectPkgManagerContext(root), suggested)
			fmt.Fprintf(os.Stderr, "This runs after every worktree creation so agents have dependencies. (y/n): ")
			if params.AutoConfirm {
				fmt.Fprintln(os.Stderr, "yes")
				postWorktreeCmd = suggested
			} else {
				response, err := termutil.ReadSingleKey(stdin)
				if err == nil && response == "y" {
					postWorktreeCmd = suggested
				}
			}
		} else if subdirs := detectNodeSubdirs(root); len(subdirs) > 1 {
			fmt.Fprintf(os.Stderr, "Detected Node projects in: %s. Configure --post-worktree-cmd manually.\n", strings.Join(subdirs, ", "))
		}
	}

	// Warn if Node.js project lacks installed dependencies.
	// post_worktree_cmd runs npm/yarn/pnpm install in worktrees, but it can
	// fail silently if the main repo's deps aren't installed (no cache, missing
	// native modules, etc.). Catching it here prevents agents from discovering
	// the problem 17 turns into a review session.
	root := lizaPaths.ProjectRoot()
	if _, err := os.Stat(filepath.Join(root, "package.json")); err == nil {
		if _, err := os.Stat(filepath.Join(root, "node_modules")); os.IsNotExist(err) {
			installCmd := detectInstallCmdInDir(root)
			fmt.Fprintf(os.Stderr, "⚠️  package.json found but node_modules/ is missing. Run %q before starting agents.\n", installCmd)
		}
	}
	for _, dir := range detectNodeSubdirs(root) {
		dirPath := filepath.Join(root, dir)
		if _, err := os.Stat(filepath.Join(dirPath, "node_modules")); os.IsNotExist(err) {
			installCmd := detectInstallCmdInDir(dirPath)
			fmt.Fprintf(os.Stderr, "⚠️  %s/package.json found but %s/node_modules/ is missing. Run %q in %s/ before starting agents.\n", dir, dir, installCmd, dir)
		}
	}

	// Generate IDs and timestamps
	timestamp := time.Now().UTC()
	goalID := fmt.Sprintf("goal-%d", timestamp.Unix())

	// Pipeline version (always v3 — pipeline is mandatory)
	pipelineVersion := 3

	copyWorktreeEnvFiles := params.CopyWorktreeEnvFiles || envgate.TruthyEnv(models.EnvEnableCopyWorktreeEnvFiles)

	// Create initial state
	state := &models.State{
		Version:         1,
		PipelineVersion: pipelineVersion,
		Goal: models.Goal{
			ID:          goalID,
			Description: description,
			SpecRef:     specRepoRel,
			EntryPoint:  entryPoint,
			Created:     timestamp,
			Status:      models.GoalStatusInProgress,
			AlignmentHistory: []models.AlignmentHistory{
				{
					Timestamp: timestamp,
					Event:     models.TaskEventInitialization,
					Summary:   "Initial goal. No tasks defined yet.",
				},
			},
		},
		Tasks:       []models.Task{},
		Agents:      make(map[string]models.Agent),
		Discovered:  []models.Discovery{},
		HumanNotes:  []models.HumanNote{},
		SpecChanges: []models.SpecChange{},
		Anomalies:   []models.Anomaly{},
		Sprint: models.Sprint{
			ID:      "sprint-1",
			Number:  1,
			GoalRef: goalID,
			Scope: models.SprintScope{
				Planned: []string{},
				Stretch: []string{},
			},
			Timeline: models.SprintTimeline{
				Started:      timestamp,
				Deadline:     time.Time{}, // zero value for null
				CheckpointAt: nil,
				Ended:        nil,
			},
			Status: models.SprintStatusInProgress,
			Metrics: models.SprintMetrics{
				TasksDone:         0,
				TasksInProgress:   0,
				TasksBlocked:      0,
				IterationsTotal:   0,
				ReviewCyclesTotal: 0,
			},
			Retrospective: nil,
		},
		CircuitBreaker: models.CircuitBreaker{
			LastCheck:      time.Time{}, // zero value for null
			Status:         "OK",
			CurrentTrigger: nil,
			History:        []models.CircuitBreakerHistory{},
		},
		Config: models.Config{
			MaxCoderIterations:              10,
			MaxReviewCycles:                 5,
			MaxGlobalIntegrationGenerations: models.NormalizeGlobalIntegrationGenerationLimit(params.MaxGlobalIntegrationGenerations),
			HeartbeatInterval:               60,
			LeaseDuration:                   1800,
			CoderPollInterval:               30,
			DoerMaxWait:                     18000,
			OrchestratorPollInterval:        60,
			OrchestratorMaxWait:             18000,
			ReviewerPollInterval:            30,
			ReviewerMaxWait:                 18000,
			AgentProgressTimeout:            models.DefaultAgentProgressTimeoutSec,
			DefaultCLI:                      params.DefaultCLI,
			DefaultDoerCLI:                  params.DefaultDoerCLI,
			DefaultReviewerCLI:              params.DefaultReviewerCLI,
			ScipSearch:                      scipSearchConfig.Languages,
			IntegrationBranch:               branch,
			EscalationWebhook:               nil,
			Mode:                            models.SystemModeRunning,
			AutoResume:                      params.AutoResume,
			NoFollowUp:                      params.NoFollowUp,
			PostWorktreeCmd:                 stringPtrOrNil(postWorktreeCmd),
			CopyWorktreeEnvFiles:            copyWorktreeEnvFiles,
		},
	}

	// Write state file
	bb := db.For(lizaPaths.StatePath())
	if err := bb.Write(state); err != nil {
		cleanupInit()
		return fmt.Errorf("failed to write state file: %w", err)
	}

	// Create log file
	// Note: Using simple file write for log since it's not managed by blackboard
	logPath := lizaPaths.LogPath()
	logContent := fmt.Sprintf(`- timestamp: %s
  agent: system
  action: initialized
  detail: %s
`, timestamp.Format(time.RFC3339), description)

	if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
		cleanupInit()
		return fmt.Errorf("failed to write log file: %w", err)
	}

	// Create supporting files
	alertsPath := lizaPaths.AlertsLogPath()
	if err := os.WriteFile(alertsPath, []byte{}, 0644); err != nil {
		cleanupInit()
		return fmt.Errorf("failed to create alerts.log: %w", err)
	}

	// Create lock file
	if err := os.WriteFile(lizaPaths.LockPath(), []byte{}, 0644); err != nil {
		cleanupInit()
		return fmt.Errorf("failed to create lock file: %w", err)
	}

	// Create integration branch if it doesn't exist
	if err := createIntegrationBranch(branch); err != nil {
		cleanupInit()
		return fmt.Errorf("failed to create integration branch %q: %w", branch, err)
	}

	fmt.Printf("%s initialized at %s\n", brand.NameTitle, lizaPaths.LizaDir())
	fmt.Printf("Integration branch: %s\n", branch)

	hasNonClaude := false
	for _, a := range params.Agents {
		if a != "claude" {
			hasNonClaude = true
			break
		}
	}
	if hasNonClaude {
		fmt.Println("Some agents require manual configuration.")
		fmt.Printf("See: https://github.com/%s/blob/main/GETTING_STARTED.md\n", brand.Repo)
	}

	return nil
}

// validateBranchName checks that name is a valid git branch name.
func validateBranchName(name string) error {
	cmd := gitpkg.Command("check-ref-format", "--branch", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("not a valid git branch name: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func createIntegrationBranch(name string) error {
	needsCreate, err := integrationBranchNeedsCreate(name)
	if err != nil {
		return err
	}
	if !needsCreate {
		return nil
	}

	cmd := gitpkg.Command("branch", name, "HEAD")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git branch failed: %w: %s", err, string(output))
	}

	return nil
}

func integrationBranchNeedsCreate(name string) (bool, error) {
	cmd := gitpkg.Command("rev-parse", "--verify", name)
	if err := cmd.Run(); err == nil {
		return false, nil
	}

	cmd = gitpkg.Command("rev-parse", "--verify", "HEAD")
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("repo has no commits (HEAD is unborn)")
	}

	return true, nil
}

// stringPtrOrNil returns a pointer to s if non-empty, otherwise nil.
func stringPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// isInteractive returns true if r is connected to a terminal.
// Returns false for pipes, redirected input, or non-file readers (e.g. strings.Reader in tests).
func isInteractive(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// detectInstallCmdInDir checks a single directory for package.json and returns
// the appropriate install command based on which lockfile is present.
// Returns "" if no package.json is found.
func detectInstallCmdInDir(dir string) string {
	return projectdetect.DetectInstallCmdInDir(dir)
}

// detectNodeSubdirs returns sorted subdirectory names (depth 1) containing
// package.json. Dotfile-prefixed directories and node_modules are skipped —
// these commonly contain stray package.json files (build outputs, vendored
// deps) that don't represent real project directories.
func detectNodeSubdirs(projectRoot string) []string {
	return projectdetect.DetectNodeSubdirs(projectRoot)
}

// detectPostWorktreeCmd checks the project root (and immediate subdirectories
// if nothing at root) for package.json, returning the appropriate install
// command. For multiple subdirectories, returns "" — the caller should print
// a manual-configuration message instead of guessing at a compound command.
func detectPostWorktreeCmd(projectRoot string) string {
	return projectdetect.DetectPostWorktreeCmd(projectRoot)
}

// confirmMissingPostWorktreeCmd guards against forgetting --post-worktree-cmd.
// Auto-detection only covers Node layouts; every other stack would otherwise
// initialize silently and hand agents worktrees with no dependencies or build
// artifacts. Interactive callers must confirm; non-interactive callers get the
// warning only, since there is no one to answer.
func confirmMissingPostWorktreeCmd(params InitParams, projectRoot string, stdin *bufio.Reader, rawStdin io.Reader) error {
	if params.PostWorktreeCmd != "" || detectPostWorktreeCmd(projectRoot) != "" {
		return nil
	}
	nodeSubdirs := detectNodeSubdirs(projectRoot)
	if len(nodeSubdirs) > 1 {
		fmt.Fprintf(os.Stderr, "⚠️  No --post-worktree-cmd set, and auto-detection found multiple Node.js projects (%s), so no single setup command could be selected.\n", strings.Join(nodeSubdirs, ", "))
	} else {
		fmt.Fprintln(os.Stderr, "⚠️  No --post-worktree-cmd set, and auto-detection found no Node.js project layout.")
	}
	fmt.Fprintln(os.Stderr, "Task worktrees are fresh checkouts: no installed dependencies, no build artifacts. Agent builds and tests can fail for environment reasons and burn iterations.")
	fmt.Fprintf(os.Stderr, "Set one with: %s init \"<goal>\" --post-worktree-cmd \"make setup\", or add post_worktree_cmd to %s/state.yaml later.\n", brand.BinaryName, paths.ProjectDirName())
	if !params.ForceInteractive && !params.AutoConfirm && !isInteractive(rawStdin) {
		return nil
	}
	fmt.Fprint(os.Stderr, "Continue without a post-worktree setup command? (y/n): ")
	if params.AutoConfirm {
		fmt.Fprintln(os.Stderr, "yes")
		return nil
	}
	response, err := termutil.ReadSingleKey(stdin)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		// No answerable input (isInteractive also accepts /dev/null, so
		// scripted callers reach this prompt). Nobody is there to answer:
		// the warning stands and initialization continues, as it does on
		// the non-interactive path above.
		return nil
	}
	if response != "y" {
		return fmt.Errorf("initialization cancelled by user")
	}
	return nil
}

// detectPkgManagerContext returns a human-readable description of what was
// detected (e.g. "package.json + yarn.lock") for the suggestion prompt.
func detectPkgManagerContext(projectRoot string) string {
	return projectdetect.DetectPkgManagerContext(projectRoot)
}

func entryPointNames(cfg *pipeline.PipelineConfig) string {
	names := make([]string, 0, len(cfg.Pipeline.EntryPoints))
	for name := range cfg.Pipeline.EntryPoints {
		names = append(names, name)
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}
