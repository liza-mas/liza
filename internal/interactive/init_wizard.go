package interactive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providers"
)

// InitWizardResult holds all choices made during the interactive init wizard.
type InitWizardResult struct {
	Mode            string            // "pairing" or "full"
	Agents          []string          // selected agents (e.g. "claude", "codex", "cursor")
	Description     string            // project goal (full mode only)
	SpecRef         string            // spec file path (full mode only)
	EntryPoint      string            // entry point (full mode only)
	ContractActions map[string]string // provider-scoped conflict actions keyed by canonical provider ID
}

// RunInitWizard runs the interactive init wizard and returns the user's choices.
// Returns (nil, nil) if user aborts (Ctrl+C / Esc).
func RunInitWizard(projectRoot string) (*InitWizardResult, error) {
	result := &InitWizardResult{
		SpecRef: "specs/vision.md",
	}

	// Screen 1: Mode selection
	err := huh.NewSelect[string]().
		Title(fmt.Sprintf("How would you like to use %s?", brand.NameTitle)).
		Options(
			huh.NewOption(fmt.Sprintf("Start with Pairing — AI agents follow %s quality contracts (recommended for first use)", brand.NameTitle), "pairing"),
			huh.NewOption("Full Multi-Agent System — Orchestrated workspace with sprints, reviews, and task decomposition", "full"),
		).
		Value(&result.Mode).
		Run()
	if err != nil {
		return nil, abortOrError(err)
	}

	// Screen 2: Agent selection
	err = huh.NewMultiSelect[string]().
		Title("Which agents do you want to enable?").
		Options(
			huh.NewOption("Claude  (creates CLAUDE.md)", "claude").Selected(true),
			huh.NewOption("Codex   (creates AGENTS.md)", "codex"),
			huh.NewOption("Cursor  (creates Claude/Codex setup Cursor relies on)", "cursor"),
			huh.NewOption("OpenCode (creates AGENTS.md)", "opencode"),
			huh.NewOption("Gemini  (creates GEMINI.md)", "gemini"),
			huh.NewOption("Mistral (sets up ~/.vibe/)", "mistral"),
		).
		Value(&result.Agents).
		Validate(func(agents []string) error {
			if len(agents) == 0 {
				return fmt.Errorf("select at least one agent")
			}
			return nil
		}).
		Run()
	if err != nil {
		return nil, abortOrError(err)
	}

	// Screen 3 (full mode only): Project details
	if result.Mode == "full" {
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Project description").
					Placeholder("e.g., Build a REST API for task management").
					Value(&result.Description).
					Validate(func(s string) error {
						if s == "" {
							return fmt.Errorf("description is required")
						}
						return nil
					}),
				huh.NewInput().
					Title("Spec file path").
					Value(&result.SpecRef),
				huh.NewSelect[string]().
					Title("Entry point").
					Description("How should the orchestrator classify your spec?").
					Options(
						huh.NewOption("Auto — let the orchestrator decide", ""),
						huh.NewOption("General Objective — full pipeline (epics → stories → code)", "general-objective"),
						huh.NewOption("Functional Spec — architecture → code planning → coding", "functional-spec"),
						huh.NewOption("Technical Spec — code planning → coding", "technical-spec"),
					).
					Value(&result.EntryPoint),
			),
		).Run()
		if err != nil {
			return nil, abortOrError(err)
		}
	}

	// Screen 4: Contract conflict resolution (if needed)
	if err := resolveContractConflicts(projectRoot, result); err != nil {
		return nil, abortOrError(err)
	}

	return result, nil
}

// abortOrError returns nil for user abort (Ctrl+C / Esc), passes through other errors.
func abortOrError(err error) error {
	if errors.Is(err, huh.ErrUserAborted) {
		return nil
	}
	return err
}

type ContractConflict struct {
	RepoPath        string
	FileName        string
	Providers       []providers.Provider
	GlobalAvailable bool
	LocalAvailable  bool
	LocalFallback   string
}

// DetectContractConflicts returns only repo-file conflicts that can affect the
// selected provider. A usable preferred global path makes an occupied repo file
// irrelevant, while repo-only providers and unavailable global paths still
// require an explicit choice.
func DetectContractConflicts(projectRoot, homeDir string, agents []providers.Provider, contractTarget string) []ContractConflict {
	if projectRoot == "" {
		return nil
	}
	conflicts := make([]ContractConflict, 0)
	conflictByPath := make(map[string]int)
	for _, agent := range agents {
		contract := agent.Setup.Contract
		if contract.RepoFile == "" {
			continue
		}
		repoPath := filepath.Clean(filepath.Join(projectRoot, contract.RepoFile))
		if _, err := os.Lstat(repoPath); err != nil {
			continue
		}
		if isManagedContractSymlink(repoPath, contractTarget) {
			continue
		}
		globalPath, globalErr := contract.GlobalPath(homeDir)
		globalAvailable := globalErr == nil && contractPathAvailable(globalPath, contractTarget)
		localAvailable := false
		if contract.LocalFallback != "" {
			localPath := filepath.Clean(filepath.Join(projectRoot, contract.LocalFallback))
			localAvailable = contractPathAvailable(localPath, contractTarget)
		}
		if contract.PrefersGlobal() {
			if globalAvailable {
				continue
			}
		}
		if index, ok := conflictByPath[repoPath]; ok {
			conflict := &conflicts[index]
			conflict.Providers = append(conflict.Providers, agent)
			conflict.GlobalAvailable = conflict.GlobalAvailable && globalAvailable
			conflict.LocalAvailable = conflict.LocalAvailable && localAvailable
			if conflict.LocalFallback != contract.LocalFallback {
				conflict.LocalFallback = ""
			}
			continue
		}
		conflictByPath[repoPath] = len(conflicts)
		conflicts = append(conflicts, ContractConflict{
			RepoPath:        repoPath,
			FileName:        contract.RepoFile,
			Providers:       []providers.Provider{agent},
			GlobalAvailable: globalAvailable,
			LocalAvailable:  localAvailable,
			LocalFallback:   contract.LocalFallback,
		})
	}
	return conflicts
}

func contractPathAvailable(path, contractTarget string) bool {
	if path == "" {
		return false
	}
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return true
	}
	return isManagedContractSymlink(path, contractTarget)
}

func isManagedContractSymlink(path, contractTarget string) bool {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	target, err := os.Readlink(path)
	return err == nil && target == contractTarget
}

func contractConflictActions(conflict ContractConflict) []string {
	actions := make([]string, 0, 4)
	if conflict.GlobalAvailable {
		actions = append(actions, "global")
	}
	actions = append(actions, "rename")
	if conflict.LocalAvailable {
		actions = append(actions, "local")
	}
	return append(actions, "skip")
}

func contractConflictProviderNames(conflict ContractConflict) string {
	names := make([]string, 0, len(conflict.Providers))
	for _, provider := range conflict.Providers {
		name := provider.DisplayName
		if name == "" {
			name = provider.ID
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

// resolveContractConflicts checks if any contract files conflict and prompts the user.
func resolveContractConflicts(projectRoot string, result *InitWizardResult) error {
	if projectRoot == "" {
		return nil
	}

	homeDir, err := paths.UserHomeDir()
	if err != nil {
		return nil // non-fatal, let the init command handle it
	}
	contractTarget := filepath.Join(homeDir, paths.GlobalDirName(), "CORE.md")

	agents, err := commands.ResolveInitProviders(homeDir, result.Agents)
	if err != nil {
		return err
	}
	conflicts := DetectContractConflicts(projectRoot, homeDir, agents, contractTarget)
	if len(conflicts) == 0 {
		return nil
	}

	result.ContractActions = make(map[string]string, len(conflicts))
	for _, conflict := range conflicts {
		var action string
		options := make([]huh.Option[string], 0, 4)
		for _, candidate := range contractConflictActions(conflict) {
			switch candidate {
			case "global":
				options = append(options, huh.NewOption(fmt.Sprintf("Use global config instead (keeps your existing %s)", conflict.FileName), candidate))
			case "rename":
				options = append(options, huh.NewOption(fmt.Sprintf("Rename existing to %s.bak and place %s contract at repo root", conflict.FileName, brand.NameTitle), candidate))
			case "local":
				label := "Use each provider's local fallback"
				if conflict.LocalFallback != "" {
					label = fmt.Sprintf("Use %s", conflict.LocalFallback)
				}
				options = append(options, huh.NewOption(label+" (local override, should be gitignored)", candidate))
			case "skip":
				options = append(options, huh.NewOption("Skip — don't create this contract", candidate))
			}
		}

		if err := huh.NewSelect[string]().
			Title(fmt.Sprintf("%s already exists for %s. Where should %s place its contract?", conflict.FileName, contractConflictProviderNames(conflict), brand.NameTitle)).
			Options(options...).
			Value(&action).
			Run(); err != nil {
			return err
		}
		for _, provider := range conflict.Providers {
			result.ContractActions[provider.ID] = action
		}
	}
	return nil
}
