package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/embedded"
	gitpkg "github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

// InitParams holds the parameters for InitCommand.
type InitParams struct {
	Description      string
	SpecRef          string
	ConfigPath       string   // --config: path to pipeline YAML
	EntryPoint       string   // --entry-point: name of entry-point in config
	Branch           string   // --branch: integration branch name (default: "integration")
	PostWorktreeCmd  string   // --post-worktree-cmd: shell command to run after worktree creation
	AutoResume       bool     // --auto-resume: automatically resume at checkpoint and sprint completion
	Agents           []string // --claude, --codex, --gemini, --mistral
	Stdin            io.Reader
	ForceInteractive bool   // bypass TTY check (for testing)
	ContractAction   string // "global", "rename", "skip", or "" (default behavior)
}

// InitAgentRepoSymlinks maps agent flag names to the repo-root symlink filename.
// These symlinks point to ~/.liza/CORE.md and enable pairing mode.
var InitAgentRepoSymlinks = map[string]string{
	"claude": "CLAUDE.md",
	"codex":  "AGENTS.md",
	"gemini": "GEMINI.md",
}

// globalFallbacks maps repo-root filenames to their CLI global fallback paths
// (relative to home directory). Used when the repo root already has a non-Liza file.
var globalFallbacks = map[string]string{
	"CLAUDE.md": filepath.Join(".claude", "CLAUDE.md"),
	"AGENTS.md": filepath.Join(".codex", "AGENTS.md"),
	"GEMINI.md": filepath.Join(".gemini", "GEMINI.md"),
}

// InitPairingParams holds the parameters for InitPairingCommand.
type InitPairingParams struct {
	Agents         []string  // agent names (e.g. "claude", "codex", "gemini", "mistral")
	Stdin          io.Reader // input for interactive prompts (nil = os.Stdin)
	ContractAction string    // "global", "rename", "skip", or "" (default behavior)
}

// InitPairingCommand creates agent-specific contract symlinks without
// initializing a full Liza workspace. This enables pairing mode.
//
// For claude/codex/gemini: creates repo-root symlinks (e.g. CLAUDE.md → ~/.liza/CORE.md).
// For mistral: creates ~/.vibe/prompts/liza.md → ~/.liza/CORE.md and sets system_prompt_id in config.toml.
func InitPairingCommand(params InitPairingParams) error {
	rawStdin := params.Stdin
	if rawStdin == nil {
		rawStdin = os.Stdin
	}
	stdin := bufio.NewReader(rawStdin)

	globalDir, err := paths.GlobalLizaDir()
	if err != nil {
		return fmt.Errorf("failed to determine global config path: %w", err)
	}
	coreFile := filepath.Join(globalDir, "CORE.md")
	if _, err := os.Stat(coreFile); os.IsNotExist(err) {
		return fmt.Errorf("global config not found at %s\nRun 'liza setup' first", globalDir)
	}

	// Classify agents
	var repoRootNames []string
	hasClaude := false
	hasMistral := false
	for _, agent := range params.Agents {
		if name, ok := InitAgentRepoSymlinks[agent]; ok {
			repoRootNames = append(repoRootNames, name)
		}
		switch agent {
		case "claude":
			hasClaude = true
		case "mistral":
			hasMistral = true
		case "codex", "gemini":
			// handled by repoRootNames above
		default:
			return fmt.Errorf("unknown agent: %s", agent)
		}
	}

	// Resolve project root for repo-root operations
	var projectRoot string
	if len(repoRootNames) > 0 || hasClaude {
		lizaPaths, err := paths.LizaPathsFromGit()
		if err != nil {
			return fmt.Errorf("failed to determine project root: %w", err)
		}
		projectRoot = lizaPaths.ProjectRoot()
	}

	if len(repoRootNames) > 0 {
		createContractSymlinksFiltered(projectRoot, coreFile, repoRootNames, params.ContractAction)
	}

	// Write/merge .claude/settings.json and deploy hooks
	if hasClaude {
		if err := embedded.WriteClaudeSettings(projectRoot, stdin); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write claude-settings.json: %v\n", err)
		}
	}

	if hasMistral {
		if err := setupMistralContract(coreFile, stdin); err != nil {
			return fmt.Errorf("mistral setup failed: %w", err)
		}
	}

	return nil
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
	// Map CLI name to expected filename (kimi uses claude's config)
	effectiveCLI := cliName
	if cliName == "kimi" {
		effectiveCLI = "claude"
	}

	fileName, ok := InitAgentRepoSymlinks[effectiveCLI]
	if !ok {
		// Mistral uses ~/.vibe/prompts/liza.md instead of a repo-root symlink
		if effectiveCLI == "mistral" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return ""
			}
			contractTarget := filepath.Join(homeDir, ".liza", "CORE.md")
			mistralPath := filepath.Join(homeDir, ".vibe", "prompts", "liza.md")
			if isLizaSymlink(mistralPath, contractTarget) {
				return mistralPath
			}
		}
		return ""
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	contractTarget := filepath.Join(homeDir, ".liza", "CORE.md")

	// Check repo root
	repoPath := filepath.Join(projectRoot, fileName)
	if isLizaSymlink(repoPath, contractTarget) {
		return repoPath
	}

	// Check global fallback
	if globalRel, ok := globalFallbacks[fileName]; ok {
		globalPath := filepath.Join(homeDir, globalRel)
		if isLizaSymlink(globalPath, contractTarget) {
			return globalPath
		}
	}

	return ""
}

// createContractSymlinksFiltered creates repo-root symlinks to the contract.
// When a non-Liza file already exists at the repo root, it falls back to the
// CLI's global config directory (e.g. ~/.claude/CLAUDE.md).
//
// The contractAction parameter controls conflict resolution when set by the
// interactive wizard: "rename" backs up the existing file, "global" uses the
// global fallback, "skip" skips creation. Empty string uses default behavior.
func createContractSymlinksFiltered(projectRoot, contractTarget string, names []string, contractAction string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot determine home directory: %v\n", err)
		return
	}

	for _, name := range names {
		repoPath := filepath.Join(projectRoot, name)
		globalRel, hasGlobal := globalFallbacks[name]
		globalPath := filepath.Join(homeDir, globalRel)

		// Step 1: Liza symlink already exists at either location?
		repoIsLiza := isLizaSymlink(repoPath, contractTarget)
		globalIsLiza := hasGlobal && isLizaSymlink(globalPath, contractTarget)

		if repoIsLiza && globalIsLiza {
			fmt.Fprintf(os.Stderr, "Warning: %s has Liza symlinks at both %s and %s — remove one to avoid confusion.\n", name, repoPath, globalPath)
			continue
		}
		if repoIsLiza {
			fmt.Printf("%s: already correct\n", name)
			continue
		}
		if globalIsLiza {
			fmt.Printf("%s: skipping — Liza symlink already exists at %s\n", name, globalPath)
			continue
		}

		// Step 2: repo root free → create there (happy path)
		_, repoErr := os.Lstat(repoPath)
		if repoErr != nil && !os.IsNotExist(repoErr) {
			fmt.Fprintf(os.Stderr, "Warning: cannot stat %s: %v\n", repoPath, repoErr)
			continue
		}
		if os.IsNotExist(repoErr) {
			if err := os.Symlink(contractTarget, repoPath); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to create %s symlink: %v\n", name, err)
				fmt.Fprintf(os.Stderr, "  On Windows: enable Developer Mode (Settings > System > For developers) or run the shell as Administrator, then retry.\n")
			} else {
				fmt.Printf("%s → %s\n", name, contractTarget)
			}
			continue
		}

		// Step 3: repo root occupied by non-Liza file — apply contract action
		if contractAction == "skip" {
			fmt.Printf("%s: skipped (user choice)\n", name)
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

		// Both locations occupied by non-Liza files
		fmt.Fprintf(os.Stderr, "Warning: %s exists at both repo root and %s — cannot place Liza contract. Remove or rename one, then re-run.\n", name, globalPath)
	}
}

// setupMistralContract creates ~/.vibe/prompts/liza.md → CORE.md and sets system_prompt_id in config.toml.
func setupMistralContract(coreFile string, reader *bufio.Reader) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to determine home directory: %w", err)
	}

	vibeDir := filepath.Join(homeDir, ".vibe")
	promptsDir := filepath.Join(vibeDir, "prompts")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", promptsDir, err)
	}

	// Create prompts/liza.md symlink (with confirmation for overwrites)
	linkPath := filepath.Join(promptsDir, "liza.md")
	if err := createSymlinkIdempotent(coreFile, linkPath, reader, true); err != nil {
		return fmt.Errorf("failed to create liza.md symlink: %w", err)
	}

	// Update config.toml: system_prompt_id = "liza"
	configPath := filepath.Join(vibeDir, "config.toml")
	if err := setMistralSystemPrompt(configPath, reader); err != nil {
		return err
	}

	return nil
}

// setMistralSystemPrompt ensures system_prompt_id = "liza" in ~/.vibe/config.toml.
// Prompts user before modifying an existing file.
func setMistralSystemPrompt(configPath string, reader *bufio.Reader) error {
	content, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create new config with just the prompt ID — no confirmation needed
			if err := os.WriteFile(configPath, []byte("system_prompt_id = \"liza\"\n"), 0644); err != nil {
				return fmt.Errorf("failed to create %s: %w", configPath, err)
			}
			fmt.Printf("Created %s with system_prompt_id = \"liza\"\n", configPath)
			return nil
		}
		return fmt.Errorf("failed to read %s: %w", configPath, err)
	}

	text := string(content)

	// Already set correctly
	if strings.Contains(text, `system_prompt_id = "liza"`) {
		fmt.Printf("%s: system_prompt_id already set to \"liza\"\n", configPath)
		return nil
	}

	// Needs modification — ask user
	fmt.Fprintf(os.Stderr, "%s exists and system_prompt_id is not set to \"liza\".\n", configPath)
	fmt.Fprintf(os.Stderr, "Set system_prompt_id = \"liza\"? (y/n): ")
	response, err := reader.ReadString('\n')
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to read input, skipping config.toml update\n")
		return nil
	}
	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		fmt.Fprintf(os.Stderr, "  Skipped %s\n", configPath)
		return nil
	}

	// Replace existing system_prompt_id line
	lines := strings.Split(text, "\n")
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "system_prompt_id") && strings.Contains(trimmed, "=") {
			lines[i] = `system_prompt_id = "liza"`
			found = true
			break
		}
	}

	if !found {
		// Prepend to file
		lines = append([]string{`system_prompt_id = "liza"`}, lines...)
	}

	if err := os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}
	fmt.Printf("%s: set system_prompt_id = \"liza\"\n", configPath)
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

	// Validate .liza doesn't already exist
	if _, err := os.Stat(lizaPaths.LizaDir()); !os.IsNotExist(err) {
		return fmt.Errorf(".liza already exists at %s, remove or use existing", lizaPaths.LizaDir())
	}

	// Resolve spec file relative to cwd (where user ran the command), not project root
	specPath, err := filepath.Abs(specRef)
	if err != nil {
		return fmt.Errorf("failed to resolve spec path: %w", err)
	}
	if _, err := os.Stat(specPath); os.IsNotExist(err) {
		return fmt.Errorf("spec file does not exist: %s\nCreate spec document first. See templates/vision-template.md", specRef)
	}

	// Validate global config exists (liza setup must have been run)
	globalDir, err := paths.GlobalLizaDir()
	if err != nil {
		return fmt.Errorf("failed to determine global config path: %w", err)
	}
	globalCoreFile := filepath.Join(globalDir, "CORE.md")
	if _, err := os.Stat(globalCoreFile); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("global config not found at %s\nRun 'liza setup' first to install contracts and skills", globalDir)
		}
		return fmt.Errorf("cannot access global config at %s: %w\nCheck permissions on %s", globalCoreFile, err, globalDir)
	}

	// Create directory structure
	if err := os.MkdirAll(lizaPaths.LizaDir(), 0755); err != nil {
		return fmt.Errorf("failed to create .liza directory: %w", err)
	}

	archiveDir := lizaPaths.ArchiveDir()
	if err := os.Mkdir(archiveDir, 0755); err != nil {
		return fmt.Errorf("failed to create archive directory: %w", err)
	}

	cleanupInit := func() {
		os.RemoveAll(lizaPaths.LizaDir())
	}

	// Write support doc to .liza/SUPPORT.md (non-fatal)
	if err := embedded.WriteSupportDoc(lizaPaths.LizaDir()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write SUPPORT.md: %v\n", err)
	}

	// Freeze pipeline config into .liza/pipeline.yaml
	frozenPath := filepath.Join(lizaPaths.LizaDir(), "pipeline.yaml")
	if err := os.WriteFile(frozenPath, pipelineData, 0644); err != nil {
		cleanupInit()
		return fmt.Errorf("failed to freeze pipeline config: %w", err)
	}

	// Write/merge Claude Code settings and deploy hooks to .claude/
	// This is non-fatal - if it fails, just warn
	// Note: This may prompt user for input if settings file exists
	if err := embedded.WriteClaudeSettings(lizaPaths.ProjectRoot(), stdin); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write claude-settings.json: %v\n", err)
	}

	// Write/merge MCP server configuration to .mcp.json
	// This is non-fatal - if it fails, just warn
	// Note: This may prompt user for input if settings file exists
	if err := embedded.WriteMCPSettings(lizaPaths.ProjectRoot(), stdin); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write .mcp.json: %v\n", err)
	}

	// Create contract symlinks only for explicitly requested providers
	if len(params.Agents) > 0 {
		var names []string
		for _, agent := range params.Agents {
			if name, ok := InitAgentRepoSymlinks[agent]; ok {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			createContractSymlinksFiltered(lizaPaths.ProjectRoot(), filepath.Join(globalDir, "CORE.md"), names, params.ContractAction)
		}
	}

	// Write GUARDRAILS.md template to project root (non-fatal, like claude-settings)
	if err := embedded.WriteGuardrails(lizaPaths.ProjectRoot()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write GUARDRAILS.md: %v\n", err)
	}

	// Write console.sh to project root (non-fatal, prompts if exists)
	consolePath := filepath.Join(lizaPaths.ProjectRoot(), "console.sh")
	writeConsole := true
	if _, err := os.Stat(consolePath); err == nil {
		fmt.Fprintf(os.Stderr, "console.sh already exists. Overwrite? (y/n): ")
		response, err := stdin.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if err != nil || (response != "y" && response != "yes") {
			writeConsole = false
		}
	}
	if writeConsole {
		if err := embedded.WriteConsoleScript(lizaPaths.ProjectRoot()); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write console.sh: %v\n", err)
		}
	}

	// Auto-suggest post_worktree_cmd if not explicitly set and stdin is a terminal
	postWorktreeCmd := params.PostWorktreeCmd
	if postWorktreeCmd == "" && (params.ForceInteractive || isInteractive(rawStdin)) {
		if suggested := detectPostWorktreeCmd(lizaPaths.ProjectRoot()); suggested != "" {
			fmt.Fprintf(os.Stderr, "Detected %s — set post_worktree_cmd to %q?\n", detectPkgManagerContext(lizaPaths.ProjectRoot()), suggested)
			fmt.Fprintf(os.Stderr, "This runs after every worktree creation so agents have dependencies. (y/n): ")
			response, err := stdin.ReadString('\n')
			if err == nil {
				response = strings.TrimSpace(strings.ToLower(response))
				if response == "y" || response == "yes" {
					postWorktreeCmd = suggested
				}
			}
		}
	}

	// Generate IDs and timestamps
	timestamp := time.Now().UTC()
	goalID := fmt.Sprintf("goal-%d", timestamp.Unix())

	// Pipeline version (always v2 — pipeline is mandatory)
	pipelineVersion := 2

	// Create initial state
	state := &models.State{
		Version:         1,
		PipelineVersion: pipelineVersion,
		Goal: models.Goal{
			ID:          goalID,
			Description: description,
			SpecRef:     specPath,
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
			MaxCoderIterations:       10,
			MaxReviewCycles:          5,
			HeartbeatInterval:        60,
			LeaseDuration:            1800,
			CoderPollInterval:        30,
			CoderMaxWait:             7200,
			OrchestratorPollInterval: 60,
			OrchestratorMaxWait:      7200,
			ReviewerPollInterval:     30,
			ReviewerMaxWait:          7200,
			IntegrationBranch:        branch,
			EscalationWebhook:        nil,
			Mode:                     models.SystemModeRunning,
			AutoResume:               params.AutoResume,
			PostWorktreeCmd:          stringPtrOrNil(postWorktreeCmd),
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
		// Don't fail the entire init if branch creation fails
		// Just log the error - this is what bash version does
		fmt.Fprintf(os.Stderr, "Warning: failed to create integration branch: %v\n", err)
	}

	fmt.Printf("Liza initialized at %s\n", lizaPaths.LizaDir())
	fmt.Printf("Integration branch: %s\n", branch)
	fmt.Println("\nNote: MCP tools and personal permissions belong in ~/.claude/settings.json (global).")

	hasNonClaude := false
	for _, a := range params.Agents {
		if a != "claude" {
			hasNonClaude = true
			break
		}
	}
	if hasNonClaude {
		fmt.Println("Some agents require manual configuration.")
		fmt.Println("See: https://github.com/liza-mas/liza/blob/main/contracts/contract-activation.md")
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
	cmd := gitpkg.Command("rev-parse", "--verify", name)
	if err := cmd.Run(); err == nil {
		return nil
	}

	cmd = gitpkg.Command("branch", name, "HEAD")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git branch failed: %w: %s", err, string(output))
	}

	return nil
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

// detectPostWorktreeCmd checks the project root for package.json and returns
// the appropriate install command based on which lockfile is present.
// Returns "" if no package.json is found.
func detectPostWorktreeCmd(projectRoot string) string {
	if _, err := os.Stat(filepath.Join(projectRoot, "package.json")); os.IsNotExist(err) {
		return ""
	}

	// Check lockfiles in specificity order (most specific first)
	lockfiles := []struct {
		file string
		cmd  string
	}{
		{"pnpm-lock.yaml", "pnpm install"},
		{"yarn.lock", "yarn install"},
		{"bun.lockb", "bun install"},
		{"bun.lock", "bun install"},
		{"package-lock.json", "npm install"},
	}

	for _, lf := range lockfiles {
		if _, err := os.Stat(filepath.Join(projectRoot, lf.file)); err == nil {
			return lf.cmd
		}
	}

	// package.json exists but no lockfile — default to npm
	return "npm install"
}

// detectPkgManagerContext returns a human-readable description of what was detected
// (e.g. "package.json + yarn.lock") for the suggestion prompt.
func detectPkgManagerContext(projectRoot string) string {
	lockfiles := []string{"pnpm-lock.yaml", "yarn.lock", "bun.lockb", "bun.lock", "package-lock.json"}
	for _, lf := range lockfiles {
		if _, err := os.Stat(filepath.Join(projectRoot, lf)); err == nil {
			return "package.json + " + lf
		}
	}
	return "package.json"
}

func entryPointNames(cfg *pipeline.PipelineConfig) string {
	names := make([]string, 0, len(cfg.Pipeline.EntryPoints))
	for name := range cfg.Pipeline.EntryPoints {
		names = append(names, name)
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}
