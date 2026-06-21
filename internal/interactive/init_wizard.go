package interactive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/paths"
)

// InitWizardResult holds all choices made during the interactive init wizard.
type InitWizardResult struct {
	Mode           string   // "pairing" or "full"
	Agents         []string // selected agents (e.g. "claude", "codex")
	Description    string   // project goal (full mode only)
	SpecRef        string   // spec file path (full mode only)
	EntryPoint     string   // entry point (full mode only)
	ContractAction string   // "global", "rename", "skip" (only if conflict detected)
}

// RunInitWizard runs the interactive init wizard and returns the user's choices.
// Returns (nil, nil) if user aborts (Ctrl+C / Esc).
func RunInitWizard(projectRoot string) (*InitWizardResult, error) {
	result := &InitWizardResult{
		SpecRef: "specs/vision.md",
	}

	// Screen 1: Mode selection
	err := huh.NewSelect[string]().
		Title("How would you like to use Liza?").
		Options(
			huh.NewOption("Start with Pairing — AI agents follow Liza quality contracts (recommended for first use)", "pairing"),
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

// DetectContractConflict checks whether any selected agent's contract file
// conflicts with an existing non-Liza file at the project root.
// Returns the conflicting filename (e.g. "CLAUDE.md") or "" if no conflict.
func DetectContractConflict(projectRoot string, agents []string, contractTarget string) string {
	for _, agent := range agents {
		fileName, ok := commands.InitAgentRepoSymlinks[agent]
		if !ok {
			continue // mistral doesn't use repo-root symlinks
		}

		repoPath := filepath.Join(projectRoot, fileName)

		// Check if file exists and is NOT already a Liza symlink
		fi, err := os.Lstat(repoPath)
		if err != nil {
			continue // doesn't exist, no conflict
		}

		// If it's already a Liza symlink, skip
		if fi.Mode()&os.ModeSymlink != 0 {
			target, readErr := os.Readlink(repoPath)
			if readErr == nil && target == contractTarget {
				continue // already correct
			}
		}

		return fileName
	}
	return ""
}

// resolveContractConflicts checks if any contract files conflict and prompts the user.
func resolveContractConflicts(projectRoot string, result *InitWizardResult) error {
	if projectRoot == "" {
		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil // non-fatal, let the init command handle it
	}
	contractTarget := filepath.Join(homeDir, paths.GlobalDirName(), "CORE.md")

	conflicting := DetectContractConflict(projectRoot, result.Agents, contractTarget)
	if conflicting == "" {
		return nil
	}

	// Conflict detected — ask user
	var action string
	options := []huh.Option[string]{
		huh.NewOption(fmt.Sprintf("Use global config instead (keeps your existing %s)", conflicting), "global"),
		huh.NewOption(fmt.Sprintf("Rename existing to %s.bak and place Liza contract at repo root", conflicting), "rename"),
	}
	if conflicting == "CLAUDE.md" {
		options = append(options, huh.NewOption("Use CLAUDE.local.md (local override, should be gitignored)", "local"))
	}
	options = append(options, huh.NewOption("Skip — don't create this contract", "skip"))

	err = huh.NewSelect[string]().
		Title(fmt.Sprintf("%s already exists. Where should Liza place its contract?", conflicting)).
		Options(options...).
		Value(&action).
		Run()
	if err != nil {
		return err
	}

	// Use the first conflict's action for all (they'll likely all have the same issue)
	result.ContractAction = action
	return nil
}
