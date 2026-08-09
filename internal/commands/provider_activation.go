package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	gitpkg "github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providers"
)

const repoContractActivationStateVersion = 1

var errUntrustedRepoContractActivationState = errors.New("provider contract activation state cannot be trusted")

type repoContractActivationState struct {
	Version       int               `json:"version"`
	ProviderPaths map[string]string `json:"provider_paths"`
}

func activateProviderContracts(projectRoot, contractTarget string, agents []providers.Provider, catalog providers.Catalog, options contractSymlinkOptions) error {
	// Init is serial per repository. Atomic replacement protects against partial
	// writes; callers must not invoke this read-modify-write concurrently.
	statePath, err := repoContractActivationStatePath(projectRoot)
	if err != nil {
		return err
	}
	state, exists, err := readRepoContractActivationState(statePath)
	if err != nil {
		if !errors.Is(err, errUntrustedRepoContractActivationState) {
			return err
		}
		fmt.Fprintf(os.Stderr, "Warning: %v; preserving managed repo contract links and leaving activation metadata unchanged.\n", err)
		state = newRepoContractActivationState()
		exists = true
	}
	stateTrusted := err == nil
	if !exists {
		bootstrapLegacyRepoContractActivations(&state, projectRoot, contractTarget, catalog)
	}
	state.pruneMissingLinks(projectRoot, contractTarget)
	previousSelectedPaths := make(map[string]string)
	for _, provider := range agents {
		if repoPath, ok := state.ProviderPaths[provider.ID]; ok {
			previousSelectedPaths[provider.ID] = repoPath
		}
	}
	state.removeSelectedProviders(agents)

	preservedByOthers := state.preservedPaths(projectRoot)
	options.PreserveRepoPaths = make(map[string]bool, len(preservedByOthers)+len(previousSelectedPaths))
	options.SilentPreserveRepoPaths = make(map[string]bool, len(previousSelectedPaths))
	for path := range preservedByOthers {
		options.PreserveRepoPaths[path] = true
	}
	for _, repoPath := range previousSelectedPaths {
		absolutePath := filepath.Join(projectRoot, repoPath)
		options.PreserveRepoPaths[absolutePath] = true
		if !preservedByOthers[absolutePath] {
			options.SilentPreserveRepoPaths[absolutePath] = true
		}
	}
	if !stateTrusted {
		for path := range managedRepoContractPaths(projectRoot, contractTarget, agents) {
			options.PreserveRepoPaths[path] = true
		}
	}
	repoActivations := createContractSymlinksForProviders(projectRoot, contractTarget, agents, options)

	if !stateTrusted {
		return nil
	}
	// Shared-path ownership is complete only after every provider has finished
	// placement. Reconcile previous activations here so the placement loop cannot
	// remove a path before all current owners are known.
	reconcilePreviousProviderActivations(projectRoot, contractTarget, agents, options, previousSelectedPaths, preservedByOthers, repoActivations)
	state.recordProviderActivations(repoActivations)
	if err := writeRepoContractActivationState(statePath, state); err != nil {
		return fmt.Errorf("persist provider contract activation state: %w", err)
	}
	return nil
}

func newRepoContractActivationState() repoContractActivationState {
	return repoContractActivationState{
		Version:       repoContractActivationStateVersion,
		ProviderPaths: make(map[string]string),
	}
}

func repoContractActivationStatePath(projectRoot string) (string, error) {
	output, err := gitpkg.Output(projectRoot, "rev-parse", "--git-dir")
	if err != nil {
		return "", fmt.Errorf("resolve Git directory for provider activation state: %w", err)
	}
	gitDir := strings.TrimSpace(string(output))
	if gitDir == "" {
		return "", fmt.Errorf("resolve Git directory for provider activation state: empty path")
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(projectRoot, gitDir)
	}
	absGitDir, err := filepath.Abs(gitDir)
	if err != nil {
		return "", fmt.Errorf("resolve provider activation state directory: %w", err)
	}
	return filepath.Join(absGitDir, brand.NameLower+"-provider-activations.json"), nil
}

func readRepoContractActivationState(path string) (repoContractActivationState, bool, error) {
	state := newRepoContractActivationState()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return state, false, nil
	}
	if err != nil {
		return state, false, fmt.Errorf("read provider contract activation state: %w", err)
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, true, fmt.Errorf("%w: decode provider contract activation state: %v", errUntrustedRepoContractActivationState, err)
	}
	if state.Version != repoContractActivationStateVersion {
		return state, true, fmt.Errorf("%w: unsupported provider contract activation state version %d", errUntrustedRepoContractActivationState, state.Version)
	}
	if state.ProviderPaths == nil {
		state.ProviderPaths = make(map[string]string)
	}
	for providerID, repoPath := range state.ProviderPaths {
		if strings.TrimSpace(providerID) == "" {
			return state, true, fmt.Errorf("%w: provider contract activation state contains an empty provider ID", errUntrustedRepoContractActivationState)
		}
		cleaned, err := normalizeRepoContractPath(repoPath)
		if err != nil {
			return state, true, fmt.Errorf("%w: provider contract activation state for %s: %v", errUntrustedRepoContractActivationState, providerID, err)
		}
		state.ProviderPaths[providerID] = cleaned
	}
	return state, true, nil
}

func writeRepoContractActivationState(path string, state repoContractActivationState) error {
	state.Version = repoContractActivationStateVersion
	if state.ProviderPaths == nil {
		state.ProviderPaths = make(map[string]string)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return nil
}

func normalizeRepoContractPath(path string) (string, error) {
	cleaned := filepath.Clean(path)
	if cleaned == "." || filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid repo-relative contract path %q", path)
	}
	return cleaned, nil
}

func (state *repoContractActivationState) pruneMissingLinks(projectRoot, contractTarget string) {
	for providerID, repoPath := range state.ProviderPaths {
		if !isLizaSymlink(filepath.Join(projectRoot, repoPath), contractTarget) {
			delete(state.ProviderPaths, providerID)
		}
	}
}

func (state repoContractActivationState) preservedPaths(projectRoot string) map[string]bool {
	paths := make(map[string]bool, len(state.ProviderPaths))
	for _, repoPath := range state.ProviderPaths {
		paths[filepath.Join(projectRoot, repoPath)] = true
	}
	return paths
}

func (state *repoContractActivationState) removeSelectedProviders(agents []providers.Provider) {
	for _, provider := range agents {
		delete(state.ProviderPaths, provider.ID)
	}
}

func managedRepoContractPaths(projectRoot, contractTarget string, agents []providers.Provider) map[string]bool {
	paths := make(map[string]bool)
	for _, provider := range agents {
		contract := provider.Setup.Contract
		for _, candidate := range []string{contract.RepoFile, contract.LocalFallback} {
			if candidate == "" {
				continue
			}
			cleaned, err := normalizeRepoContractPath(candidate)
			if err != nil {
				continue
			}
			path := filepath.Join(projectRoot, cleaned)
			if isLizaSymlink(path, contractTarget) {
				paths[path] = true
			}
		}
	}
	return paths
}

func (state *repoContractActivationState) recordProviderActivations(activations map[string]string) {
	for providerID, repoPath := range activations {
		state.ProviderPaths[providerID] = repoPath
	}
}

func reconcilePreviousProviderActivations(
	projectRoot, contractTarget string,
	agents []providers.Provider,
	options contractSymlinkOptions,
	previousPaths map[string]string,
	preservedByOthers map[string]bool,
	activations map[string]string,
) {
	// paths.UserHomeDir rather than os.UserHomeDir: the rest of this flow
	// resolves the home that way, and on Windows os.UserHomeDir reads
	// USERPROFILE and ignores an injected HOME. The two disagreeing means this
	// function reconciles against a different home than the one the activation
	// was placed in — and in a test, against the developer's real profile.
	homeDir, homeErr := paths.UserHomeDir()
	// Values are display-only: repo-relative for repo placements and absolute
	// for global placements.
	verifiedReplacementDisplayPaths := make(map[string]string)
	for _, provider := range agents {
		previousPath, hadPrevious := previousPaths[provider.ID]
		if !hadPrevious {
			continue
		}
		if placedPath, placedInRepo := activations[provider.ID]; placedInRepo {
			if placedPath != previousPath {
				verifiedReplacementDisplayPaths[provider.ID] = placedPath
			}
			continue
		}

		contractAction := options.DefaultAction
		if action, ok := options.ProviderActions[provider.ID]; ok {
			contractAction = action
		}
		if homeErr == nil && provider.Setup.Contract.PrefersGlobal() && contractActionAllowsPreferredGlobal(contractAction) {
			globalPath, err := provider.Setup.Contract.GlobalPath(homeDir)
			if err == nil && globalPath != "" && isLizaSymlink(globalPath, contractTarget) {
				verifiedReplacementDisplayPaths[provider.ID] = globalPath
				continue
			}
		}

		if isLizaSymlink(filepath.Join(projectRoot, previousPath), contractTarget) {
			activations[provider.ID] = previousPath
		}
	}

	for providerID, replacementDisplayPath := range verifiedReplacementDisplayPaths {
		previousPath := previousPaths[providerID]
		absolutePath := filepath.Join(projectRoot, previousPath)
		if preservedByOthers[absolutePath] || repoActivationOwnedByAnotherProvider(activations, providerID, previousPath) {
			continue
		}
		if !isLizaSymlink(absolutePath, contractTarget) {
			continue
		}
		if err := os.Remove(absolutePath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove redundant repo activation at %s: %v; retaining recorded ownership.\n", absolutePath, err)
			// The state schema records one repo path per provider. Keep the old path
			// so a later init retries cleanup instead of orphaning it permanently.
			activations[providerID] = previousPath
			continue
		}
		fmt.Printf("%s: removed redundant repo activation; using %s\n", previousPath, replacementDisplayPath)
	}
}

func repoActivationOwnedByAnotherProvider(activations map[string]string, providerID, repoPath string) bool {
	for otherProviderID, otherRepoPath := range activations {
		if otherProviderID != providerID && otherRepoPath == repoPath {
			return true
		}
	}
	return false
}

func bootstrapLegacyRepoContractActivations(state *repoContractActivationState, projectRoot, contractTarget string, catalog providers.Catalog) {
	// Fresh activations record placement outcomes. Legacy links predate those
	// outcomes, so bootstrap necessarily infers ownership from filesystem presence
	// plus corroborating provider evidence. Bootstrap keys remain declared catalog
	// IDs, while normal selections use canonicalized IDs; the spaces intentionally
	// do not reconcile, conservatively pinning legacy repo-only activations such as
	// bare cursor until their managed link disappears.
	resolvedProviders := catalogContractProviders(catalog)
	claimCounts := repoContractPathClaimCounts(resolvedProviders)
	for _, provider := range resolvedProviders {
		contract := provider.Setup.Contract
		if contract.RepoFile == "" || contract.PrefersGlobal() {
			continue
		}
		cleaned, err := normalizeRepoContractPath(contract.RepoFile)
		if err != nil || !isLizaSymlink(filepath.Join(projectRoot, cleaned), contractTarget) {
			continue
		}
		if claimCounts[cleaned] > 1 && !hasRepoContractActivationEvidence(projectRoot, provider) {
			continue
		}
		state.ProviderPaths[provider.ID] = cleaned
	}
}

func catalogContractProviders(catalog providers.Catalog) []providers.Provider {
	providerIDs := make(map[string]bool)
	for _, provider := range catalog.ProvidersSorted() {
		providerIDs[provider.ID] = true
	}
	for _, provider := range providers.EmbeddedCatalog().ProvidersSorted() {
		providerIDs[provider.ID] = true
	}
	resolvedProviders := make([]providers.Provider, 0, len(providerIDs))
	for providerID := range providerIDs {
		resolved, err := resolveCatalogProviders(catalog, []string{providerID})
		if err != nil || len(resolved) != 1 {
			continue
		}
		resolvedProviders = append(resolvedProviders, resolved[0])
	}
	return resolvedProviders
}

func repoContractPathClaimCounts(items []providers.Provider) map[string]int {
	claimCounts := make(map[string]int)
	for _, provider := range items {
		providerClaims := make(map[string]bool)
		contract := provider.Setup.Contract
		for _, candidate := range []string{contract.RepoFile, contract.LocalFallback} {
			if candidate == "" {
				continue
			}
			cleaned, err := normalizeRepoContractPath(candidate)
			if err == nil {
				providerClaims[cleaned] = true
			}
		}
		for claim := range providerClaims {
			claimCounts[claim]++
		}
	}
	return claimCounts
}

func hasRepoContractActivationEvidence(projectRoot string, provider providers.Provider) bool {
	assets := provider.Setup.ActivationAssets
	var candidates []string
	if assets.ClaudeSettings {
		candidates = append(candidates, filepath.Join(".claude", "settings.json"))
	}
	if assets.CodexHooks {
		candidates = append(candidates, filepath.Join(".codex", "hooks.json"))
	}
	if assets.CursorHooks {
		candidates = append(candidates, filepath.Join(".cursor", "hooks.json"))
	}
	if assets.ClaudeIgnore {
		candidates = append(candidates, ".claudeignore")
	}
	if assets.OpenCodeExecTool {
		candidates = append(candidates, filepath.Join(".opencode", "tools", "exec.ts"))
	}
	for _, candidate := range candidates {
		if _, err := os.Lstat(filepath.Join(projectRoot, candidate)); err == nil {
			return true
		}
	}
	return false
}
