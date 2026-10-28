package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/worktreeexclude"
)

// globalContractFileName is the contract file every provider contract symlink
// points at inside the branded global directory.
const globalContractFileName = "CORE.md"

// Mistral keeps its prompt and configuration under the user's .vibe directory.
const (
	mistralConfigDirName  = ".vibe"
	mistralPromptsDirName = "prompts"
	mistralConfigFileName = "config.toml"
)

// ContractAction is the resolution chosen for a contract path that is already
// occupied by a file the tool does not own. The wizard selects one per provider;
// ContractActionDefault leaves the choice to the placement rules.
type ContractAction string

const (
	// ContractActionDefault applies the placement rules without a user choice.
	ContractActionDefault ContractAction = ""
	// ContractActionGlobal places the contract at the provider's global location.
	ContractActionGlobal ContractAction = "global"
	// ContractActionRename backs the existing file up and places the contract at the repo root.
	ContractActionRename ContractAction = "rename"
	// ContractActionLocal places the contract at the provider's local fallback.
	ContractActionLocal ContractAction = "local"
	// ContractActionSkip creates no contract.
	ContractActionSkip ContractAction = "skip"
)

// contractSymlinkOptions carries the conflict resolution for a placement run:
// DefaultAction applies to every provider that has no ProviderActions entry.
type contractSymlinkOptions struct {
	DefaultAction           ContractAction
	ProviderActions         map[string]ContractAction
	PreserveRepoPaths       map[string]bool
	SilentPreserveRepoPaths map[string]bool
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
		// Backfill embedded built-ins so freshly added providers warn and
		// resolve correctly even while the fetched catalog predates them.
		provider, ok = providers.EmbeddedCatalog().Resolve(cliName)
	}
	if !ok {
		return ""
	}
	contract := provider.Setup.Contract
	if contract.RepoFile == "" && provider.Setup.ActivationAssets.MistralPromptConfig {
		contract.GlobalFallback = filepath.Join(mistralConfigDirName, mistralPromptsDirName, brand.CanonicalMistralPromptID+".md")
	}
	if contract.RepoFile == "" && contract.GlobalFallback == "" {
		return ""
	}

	homeDir, err := paths.UserHomeDir()
	if err != nil {
		return ""
	}
	contractTarget := filepath.Join(homeDir, paths.GlobalDirName(), globalContractFileName)

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

func contractActionPlacesRepoContract(action ContractAction) bool {
	return action == ContractActionRename || action == ContractActionLocal
}

func contractActionAllowsPreferredGlobal(action ContractAction) bool {
	return !contractActionPlacesRepoContract(action) && action != ContractActionSkip
}

// contractPlacer carries the fixed inputs of one placement run and accumulates
// the repo-relative path each provider's active repo or local contract ended up at.
type contractPlacer struct {
	projectRoot            string
	contractTarget         string
	homeDir                string
	options                contractSymlinkOptions
	repoPathSelectionCount map[string]int
	repoActivations        map[string]string
}

func (p *contractPlacer) recordRepoActivation(providerID, candidate string) {
	cleaned, err := normalizeRepoContractPath(candidate)
	if err == nil {
		p.repoActivations[providerID] = cleaned
	}
}

// actionFor returns the provider's own action, else the run's default action.
func (p *contractPlacer) actionFor(providerID string) ContractAction {
	if action, ok := p.options.ProviderActions[providerID]; ok {
		return action
	}
	return p.options.DefaultAction
}

// createContractSymlinksForProviders activates each provider's contract at its
// catalog-declared location. Global-first providers establish their active
// global link before removing a managed repo link; repo-only providers retain
// the repo link. User-owned files are never overwritten.
//
// The options carry the conflict resolution chosen by the interactive wizard:
// "rename" backs up the existing file, "global" uses the global fallback,
// "skip" skips creation. The empty action uses default behavior.
//
// It returns canonical provider ID → normalized repo-relative path effectively
// used after placement. Every repo or local outcome must be recorded, including
// idempotent no-op branches; successful global outcomes record nothing. Unplaced
// providers return an error so callers cannot publish activation metadata or
// deploy hooks for an inactive contract.
func createContractSymlinksForProviders(projectRoot, contractTarget string, agents []providers.Provider, options contractSymlinkOptions) (map[string]string, error) {
	p := &contractPlacer{
		projectRoot:            projectRoot,
		contractTarget:         contractTarget,
		options:                options,
		repoPathSelectionCount: make(map[string]int),
		repoActivations:        make(map[string]string),
	}
	// paths.UserHomeDir rather than os.UserHomeDir: it honours an injected HOME,
	// which os.UserHomeDir ignores on Windows. Without it the tests write into
	// the developer's real profile.
	homeDir, err := paths.UserHomeDir()
	if err != nil {
		return p.repoActivations, fmt.Errorf("cannot determine home directory: %w", err)
	}
	p.homeDir = homeDir

	for _, agent := range agents {
		if repoFile := agent.Setup.Contract.RepoFile; repoFile != "" {
			p.repoPathSelectionCount[filepath.Join(projectRoot, repoFile)]++
		}
	}

	for _, agent := range agents {
		if agent.Setup.Contract.RepoFile == "" {
			continue
		}
		if err := p.placeProvider(agent); err != nil {
			return p.repoActivations, err
		}
	}
	if err := p.verifyPlacement(agents); err != nil {
		return p.repoActivations, err
	}
	return p.repoActivations, nil
}

// placeProvider decides where one provider's contract goes: an existing product
// symlink or a chosen skip ends the decision, otherwise the contract is placed
// at the repo root, or resolved against the file occupying it.
func (p *contractPlacer) placeProvider(agent providers.Provider) error {
	name := agent.Setup.Contract.RepoFile
	repoPath := filepath.Join(p.projectRoot, name)
	contractAction := p.actionFor(agent.ID)
	globalPath, globalPathErr := agent.Setup.Contract.GlobalPath(p.homeDir)
	if globalPathErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot resolve %s global contract path: %v; retaining repo activation.\n", agent.ID, globalPathErr)
		globalPath = ""
	}
	hasGlobal := globalPath != ""

	// Step 1: product symlink already exists at either location?
	repoIsLiza := isLizaSymlink(repoPath, p.contractTarget)
	globalIsLiza := hasGlobal && isLizaSymlink(globalPath, p.contractTarget)
	if agent.Setup.Contract.PrefersGlobal() && contractActionAllowsPreferredGlobal(contractAction) &&
		ensurePreferredGlobalContract(name, repoPath, globalPath, p.contractTarget, repoIsLiza, globalIsLiza,
			p.repoPathSelectionCount[repoPath] > 1 || p.options.PreserveRepoPaths[repoPath], p.options.SilentPreserveRepoPaths[repoPath]) {
		return nil
	}

	if repoIsLiza && globalIsLiza {
		if !agent.Setup.Contract.PrefersGlobal() {
			p.recordRepoActivation(agent.ID, name)
		}
		fmt.Fprintf(os.Stderr, "Warning: %s has %s symlinks at both %s and %s; remove one to avoid confusion.\n", name, brand.NameTitle, repoPath, globalPath)
		return nil
	}
	if repoIsLiza {
		p.recordRepoActivation(agent.ID, name)
		fmt.Printf("%s: already correct\n", name)
		return nil
	}
	if globalIsLiza && !contractActionPlacesRepoContract(contractAction) {
		fmt.Printf("%s: skipping; %s symlink already exists at %s\n", name, brand.NameTitle, globalPath)
		return nil
	}
	if contractAction == ContractActionSkip {
		fmt.Printf("%s: skipped (user choice)\n", name)
		return nil
	}

	// Step 2: create the repo activation when global preference is absent
	// or could not be established safely.
	if p.createRepoContract(agent, name, repoPath) {
		return nil
	}

	// Step 3: repo root occupied by non-product file; apply contract action
	return p.resolveOccupiedRepoPath(agent, name, repoPath, globalPath, contractAction)
}

// createRepoContract links the repo-root contract when nothing occupies the path.
// It reports whether the provider needs no further placement: true when the link
// was attempted or the path could not be inspected, false when a file occupies it.
func (p *contractPlacer) createRepoContract(agent providers.Provider, name, repoPath string) bool {
	_, repoErr := os.Lstat(repoPath)
	if repoErr != nil && !os.IsNotExist(repoErr) {
		fmt.Fprintf(os.Stderr, "Warning: cannot stat %s: %v\n", repoPath, repoErr)
		return true
	}
	if !os.IsNotExist(repoErr) {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(repoPath), initDirMode); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to create parent directory for %s: %v\n", name, err)
		return true
	}
	if err := os.Symlink(p.contractTarget, repoPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to create %s symlink: %v\n", name, err)
		fmt.Fprintf(os.Stderr, "  On Windows: enable Developer Mode (Settings > System > For developers) or run the shell as Administrator, then retry.\n")
	} else {
		p.recordRepoActivation(agent.ID, name)
		fmt.Printf("%s → %s\n", name, p.contractTarget)
	}
	return true
}

// resolveOccupiedRepoPath applies the contract action to a repo-root path held by
// a file the tool does not own: the local fallback, a backup rename, or by default
// the global location.
func (p *contractPlacer) resolveOccupiedRepoPath(agent providers.Provider, name, repoPath, globalPath string, contractAction ContractAction) error {
	if contractAction == ContractActionLocal && agent.Setup.Contract.LocalFallback != "" {
		if err := placeLocalContract(p.projectRoot, p.contractTarget, agent.Setup.Contract.LocalFallback); err != nil {
			return fmt.Errorf("activate local contract for %s: %w", agent.ID, err)
		}
		p.recordRepoActivation(agent.ID, agent.Setup.Contract.LocalFallback)
		return nil
	}

	if contractAction == ContractActionRename {
		p.renameExistingContract(agent, name, repoPath)
		return nil
	}

	// Default behavior (the default action or "global"): try global fallback
	placeGlobalFallbackContract(name, globalPath, p.contractTarget)
	return nil
}

// renameExistingContract moves the occupying file to the first free .bak name and
// links the contract in its place, restoring the original if the link fails.
func (p *contractPlacer) renameExistingContract(agent providers.Provider, name, repoPath string) {
	bakPath := repoPath + ".bak"
	for i := 1; ; i++ {
		if _, err := os.Lstat(bakPath); os.IsNotExist(err) {
			break
		}
		bakPath = fmt.Sprintf("%s.bak.%d", repoPath, i)
	}
	if err := os.Rename(repoPath, bakPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to rename %s to %s: %v\n", name, bakPath, err)
		return
	}
	fmt.Printf("%s: renamed existing to %s\n", name, filepath.Base(bakPath))
	if err := os.Symlink(p.contractTarget, repoPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to create %s symlink: %v\n", name, err)
		// Restore original to avoid leaving the user with no file at the path
		if restoreErr := os.Rename(bakPath, repoPath); restoreErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to restore %s from backup: %v\n", name, restoreErr)
		}
		return
	}
	p.recordRepoActivation(agent.ID, name)
	fmt.Printf("%s → %s\n", name, p.contractTarget)
}

// placeGlobalFallbackContract links the contract at the global location when
// the repo root is unavailable. It warns when no global location is configured
// or both locations are held by files the tool does not own.
func placeGlobalFallbackContract(name, globalPath, contractTarget string) {
	if globalPath == "" {
		fmt.Fprintf(os.Stderr, "Warning: %s already exists and no global fallback configured.\n", name)
		return
	}

	_, globalErr := os.Lstat(globalPath)
	if globalErr != nil && !os.IsNotExist(globalErr) {
		fmt.Fprintf(os.Stderr, "Warning: cannot stat %s: %v\n", globalPath, globalErr)
		return
	}
	if os.IsNotExist(globalErr) {
		if err := os.MkdirAll(filepath.Dir(globalPath), initDirMode); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create directory %s: %v\n", filepath.Dir(globalPath), err)
			return
		}
		if err := os.Symlink(contractTarget, globalPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create %s symlink: %v\n", globalPath, err)
			fmt.Fprintf(os.Stderr, "  On Windows: enable Developer Mode (Settings > System > For developers) or run the shell as Administrator, then retry.\n")
		} else {
			fmt.Printf("%s → %s (repo root has existing %s)\n", globalPath, contractTarget, name)
		}
		return
	}

	// Both locations occupied by non-product files.
	fmt.Fprintf(os.Stderr, "Warning: %s exists at both repo root and %s; repo and global contract locations are unavailable.\n", name, globalPath)
}

// verifyPlacement checks every selected provider before reconciliation can remove
// previous links. Empty repo metadata alone cannot distinguish global success from
// failure. Existing local links also count, including after a skip action.
func (p *contractPlacer) verifyPlacement(agents []providers.Provider) error {
	var unplaced []string
	for _, provider := range agents {
		contract := provider.Setup.Contract
		if contract.RepoFile == "" {
			continue
		}
		if _, recorded := p.repoActivations[provider.ID]; recorded {
			continue
		}
		globalPath, err := contract.GlobalPath(p.homeDir)
		if err == nil && globalPath != "" && isLizaSymlink(globalPath, p.contractTarget) {
			continue
		}
		localActive := contract.LocalFallback != "" && isLizaSymlink(filepath.Join(p.projectRoot, contract.LocalFallback), p.contractTarget)
		action := p.actionFor(provider.ID)
		if contract.LocalFallback != "" && (localActive || action == ContractActionDefault) {
			if err := placeLocalContract(p.projectRoot, p.contractTarget, contract.LocalFallback); err == nil {
				p.recordRepoActivation(provider.ID, contract.LocalFallback)
				continue
			} else {
				fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
			}
		}
		unplaced = append(unplaced, provider.ID)
	}
	if len(unplaced) > 0 {
		return fmt.Errorf("%s contract is not active for providers: %s; free a supported contract location and re-run %s", brand.NameTitle, strings.Join(unplaced, ", "), brand.Command("init"))
	}
	return nil
}

func placeLocalContract(projectRoot, contractTarget, name string) error {
	cleaned, err := normalizeRepoContractPath(name)
	if err != nil {
		return err
	}
	localPath := filepath.Join(projectRoot, cleaned)
	if !isLizaSymlink(localPath, contractTarget) {
		if _, err := os.Lstat(localPath); !os.IsNotExist(err) {
			if err != nil {
				return fmt.Errorf("cannot stat %s: %w", localPath, err)
			}
			return fmt.Errorf("%s already exists and is not a %s symlink", localPath, brand.NameTitle)
		}
		if err := os.MkdirAll(filepath.Dir(localPath), initDirMode); err != nil {
			return fmt.Errorf("create local contract directory: %w", err)
		}
		if err := os.Symlink(contractTarget, localPath); err != nil {
			return fmt.Errorf("create %s symlink: %w", localPath, err)
		}
		fmt.Printf("%s → %s\n", name, contractTarget)
	}
	if err := worktreeexclude.EnsureRepoExclude(projectRoot, "/"+filepath.ToSlash(cleaned)); err != nil {
		return fmt.Errorf("exclude local contract %s: %w", name, err)
	}
	return nil
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
		if err := os.MkdirAll(filepath.Dir(globalPath), initDirMode); err != nil {
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
