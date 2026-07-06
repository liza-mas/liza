package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/interactive"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/paths"
	providercatalog "github.com/liza-mas/liza/internal/providers"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		if isJSON(cmd) {
			result := map[string]string{
				"version": Version,
				"commit":  GitCommit,
				"built":   BuildDate,
			}
			jsonout.WriteResult(os.Stdout, result, nil, nil)
			return
		}
		fmt.Printf("%s version %s\n", brand.BinaryName, Version)
		fmt.Printf("  commit: %s\n", GitCommit)
		fmt.Printf("  built:  %s\n", BuildDate)
	},
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: fmt.Sprintf("One-time global setup of %s contracts, skills, and support docs", brand.NameTitle),
	Long: fmt.Sprintf(`Write %[1]s contracts, skills, and support docs to ~/%[2]s/ for global access.

This is a one-time setup step that populates the global config directory.
Contracts are written flat (e.g., ~/%[2]s/CORE.md) and skills are written
to ~/%[2]s/skills/. Installable support docs are written to
~/%[2]s/support-docs/.

After running setup, use '%[3]s init' in each project to create the
project-local blackboard and symlinks.

Use --force to overwrite an existing global config.
Use --agent-tools to install a custom AGENT_TOOLS.md instead of the embedded default.`, brand.NameTitle, brand.GlobalDirName, brand.BinaryName),
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir, err := paths.GlobalLizaDir()
		if err != nil {
			return err
		}
		force, _ := cmd.Flags().GetBool("force")
		agentToolsPath, _ := cmd.Flags().GetString("agent-tools")

		agents, err := collectSetupProviderFlags(cmd)
		if err != nil {
			return err
		}

		return commands.SetupCommand(commands.SetupParams{
			TargetDir:      targetDir,
			Force:          force,
			AgentToolsPath: agentToolsPath,
			Agents:         agents,
			Stdin:          os.Stdin,
		})
	},
}

var initCmd = &cobra.Command{
	Use:   "init [description]",
	Short: fmt.Sprintf("Initialize a new %s workspace or enable pairing", brand.NameTitle),
	Long: fmt.Sprintf(`Initialize a new %[1]s workspace by creating %[2]s directory structure,
generating initial state.yaml, and setting up the integration branch.

The description argument is required and describes the goal.
The spec file (default: specs/vision.md) must exist and be fully committed
before initialization.

Use --config to provide a pipeline YAML file (defaults to ~/%[3]s/pipeline.yaml).
The config is validated and frozen into %[2]s/pipeline.yaml. Use --entry-point to
specify which entry-point to use (must be defined in the config).

Use --branch to set the integration branch name (default: "integration").
All worktrees branch from and merge back to this branch.

Use --post-worktree-cmd to specify a shell command that runs after every worktree
creation (e.g. 'make setup', 'npm install'). This ensures worktrees are
build/test-ready without hardcoding project-specific tooling into %[1]s.
Existing workspaces can add post_worktree_cmd to state.yaml's config section.

Use --copy-worktree-env-files to explicitly authorize copying ignored root env
files into task worktrees before post-worktree setup runs.

PAIRING MODE: Use agent flags without a description to create only the contract
symlinks needed for pairing (no %[2]s/ workspace):
  %[4]s init --claude           # creates CLAUDE.md -> ~/%[3]s/CORE.md
  %[4]s init --claude --codex   # creates CLAUDE.md + AGENTS.md and repo hooks
  %[4]s init --cursor           # creates AGENTS.md and Cursor shell hooks
  %[4]s init --opencode         # creates AGENTS.md -> ~/%[3]s/CORE.md`, brand.NameTitle, brand.ProjectDirName, brand.GlobalDirName, brand.BinaryName),
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		agents := collectAgentFlags(cmd)
		autoResume, _ := cmd.Flags().GetBool("auto-resume")
		noFollowUp, _ := cmd.Flags().GetBool("no-follow-up")
		defaultCLI, _ := cmd.Flags().GetString("default-cli")
		defaultDoerCLI, _ := cmd.Flags().GetString("default-doer-cli")
		defaultReviewerCLI, _ := cmd.Flags().GetString("default-reviewer-cli")
		scipSearch, _ := cmd.Flags().GetStringArray("scip-search")
		scipSearchPlans, _ := cmd.Flags().GetStringArray("scip-search-plan")
		copyWorktreeEnvFiles, _ := cmd.Flags().GetBool("copy-worktree-env-files")
		if err := validateDefaultCLIFlag("default-cli", defaultCLI); err != nil {
			return err
		}
		if err := validateDefaultCLIFlag("default-doer-cli", defaultDoerCLI); err != nil {
			return err
		}
		if err := validateDefaultCLIFlag("default-reviewer-cli", defaultReviewerCLI); err != nil {
			return err
		}

		// Interactive wizard: no args, no agent flags, no explicit workspace flags, TTY
		if len(args) == 0 && len(agents) == 0 && !hasExplicitInitFlags(cmd) && !cmd.Flags().Changed("scip-search") && !cmd.Flags().Changed("scip-search-plan") {
			if !interactive.IsInteractive() {
				return fmt.Errorf("requires a description argument or at least one provider flag (--provider, --claude, --codex, --cursor, --opencode, --gemini, --mistral)\nSee: %s init --help", brand.BinaryName)
			}

			// Resolve project root for conflict detection
			var projectRoot string
			if lizaPaths, err := paths.LizaPathsFromGit(); err == nil {
				projectRoot = lizaPaths.ProjectRoot()
			}

			result, err := interactive.RunInitWizard(projectRoot)
			if err != nil {
				return err
			}
			if result == nil {
				return nil // user aborted
			}

			// Read cobra flag defaults so the wizard path achieves parity
			// with the CLI path (which reads these at lines 168-171).
			configPath, _ := cmd.Flags().GetString("config")
			branch, _ := cmd.Flags().GetString("branch")
			postWorktreeCmd, _ := cmd.Flags().GetString("post-worktree-cmd")

			if result.Mode == "pairing" {
				if autoResume {
					return fmt.Errorf("--auto-resume requires full workspace init (provide a description)")
				}
				if noFollowUp {
					return fmt.Errorf("--no-follow-up requires full workspace init (provide a description)")
				}
				if err := commands.InitPairingCommand(commands.InitPairingParams{
					Agents:          result.Agents,
					ScipSearch:      scipSearch,
					ScipSearchPlans: scipSearchPlans,
					Stdin:           os.Stdin,
					ContractAction:  result.ContractAction,
				}); err != nil {
					return err
				}
				interactive.PrintPostInitSummary("pairing", result.Agents)
				return nil
			}
			if err := commands.InitCommandWithConfig(commands.InitParams{
				Description:          result.Description,
				SpecRef:              result.SpecRef,
				ConfigPath:           configPath,
				EntryPoint:           result.EntryPoint,
				Branch:               branch,
				PostWorktreeCmd:      postWorktreeCmd,
				CopyWorktreeEnvFiles: copyWorktreeEnvFiles,
				AutoResume:           autoResume,
				NoFollowUp:           noFollowUp,
				DefaultCLI:           defaultCLI,
				DefaultDoerCLI:       defaultDoerCLI,
				DefaultReviewerCLI:   defaultReviewerCLI,
				ScipSearch:           scipSearch,
				ScipSearchPlans:      scipSearchPlans,
				Agents:               result.Agents,
				Stdin:                os.Stdin,
				ContractAction:       result.ContractAction,
			}); err != nil {
				return err
			}
			interactive.PrintPostInitSummary("full", result.Agents)
			return nil
		}

		// Pairing mode: agent flags without description
		if len(args) == 0 {
			if len(agents) == 0 {
				return fmt.Errorf("requires a description argument or at least one provider flag (--provider, --claude, --codex, --cursor, --opencode, --gemini, --mistral)\nSee: %s init --help", brand.BinaryName)
			}
			if autoResume {
				return fmt.Errorf("--auto-resume requires full workspace init (provide a description)")
			}
			if noFollowUp {
				return fmt.Errorf("--no-follow-up requires full workspace init (provide a description)")
			}
			if hasExplicitInitFlags(cmd) {
				return fmt.Errorf("workspace flags (--branch, --config, --spec, --entry-point, --post-worktree-cmd, --copy-worktree-env-files, --default-cli, --default-doer-cli, --default-reviewer-cli) require a description argument for full workspace init")
			}
			if err := commands.InitPairingCommand(commands.InitPairingParams{
				Agents:          agents,
				ScipSearch:      scipSearch,
				ScipSearchPlans: scipSearchPlans,
				Stdin:           os.Stdin,
			}); err != nil {
				return err
			}
			interactive.PrintPostInitSummary("pairing", agents)
			return nil
		}

		// Full workspace init
		description := args[0]
		specRef, _ := cmd.Flags().GetString("spec")
		configPath, _ := cmd.Flags().GetString("config")
		entryPoint, _ := cmd.Flags().GetString("entry-point")
		branch, _ := cmd.Flags().GetString("branch")
		postCreateCmd, _ := cmd.Flags().GetString("post-worktree-cmd")
		if err := commands.InitCommandWithConfig(commands.InitParams{
			Description:          description,
			SpecRef:              specRef,
			ConfigPath:           configPath,
			EntryPoint:           entryPoint,
			Branch:               branch,
			PostWorktreeCmd:      postCreateCmd,
			CopyWorktreeEnvFiles: copyWorktreeEnvFiles,
			AutoResume:           autoResume,
			NoFollowUp:           noFollowUp,
			DefaultCLI:           defaultCLI,
			DefaultDoerCLI:       defaultDoerCLI,
			DefaultReviewerCLI:   defaultReviewerCLI,
			ScipSearch:           scipSearch,
			ScipSearchPlans:      scipSearchPlans,
			Agents:               agents,
			Stdin:                os.Stdin,
		}); err != nil {
			return err
		}
		interactive.PrintPostInitSummary("full", agents)
		return nil
	},
}

var providersCmd = &cobra.Command{
	Use:   "providers",
	Short: "Inspect the provider catalog",
}

var providersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List catalog providers",
	RunE: func(cmd *cobra.Command, args []string) error {
		cat, err := providercatalog.Load(cmd.Context(), providercatalog.LoadOptions{})
		if err != nil {
			return err
		}
		for _, p := range cat.ProvidersSorted() {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", p.ID, p.DisplayName, p.Backend)
		}
		return nil
	},
}

var providersDetectCmd = &cobra.Command{
	Use:   "detect",
	Short: "Detect installed catalog providers",
	RunE: func(cmd *cobra.Command, args []string) error {
		cat, err := providercatalog.Load(cmd.Context(), providercatalog.LoadOptions{})
		if err != nil {
			return err
		}
		for _, result := range providercatalog.Detect(cat, nil) {
			status := "missing"
			if result.Installed {
				status = "installed"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s", result.ID, result.DisplayName, status)
			if result.Executable != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "\t%s", result.Executable)
			}
			fmt.Fprintln(cmd.OutOrStdout())
		}
		return nil
	},
}

var providersRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Refresh the provider catalog cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		cat, err := providercatalog.Load(cmd.Context(), providercatalog.LoadOptions{Force: true})
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "provider catalog refreshed (%d providers)\n", len(cat.Providers))
		return nil
	},
}

var providerCLIHelpHint = fmt.Sprintf("built-in and catalog-backed providers; see '%s providers list'", brand.BinaryName)

var validateCmd = &cobra.Command{
	Use:   "validate [state-file]",
	Short: "Validate state.yaml against schema rules",
	Long: `Validate the state.yaml file against all 43+ validation rules including:
- Required fields and task state invariants
- Dependency validation (existence, circularity, MERGED deps for executing tasks)
- Agent validation (WORKING must have current_task)
- Lease expiry checking with grace periods
- Spec file reference validation
Returns detailed error messages if validation fails.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		skipSpecCheck, _ := cmd.Flags().GetBool("skip-spec-check")
		skipProcessChecks, _ := cmd.Flags().GetBool("skip-process-checks")
		repair, _ := cmd.Flags().GetBool("repair")

		if isJSON(cmd) {
			log.SetOutput(io.Discard)
			defer log.SetOutput(os.Stderr)
			var warnBuf bytes.Buffer
			defer func() {
				if retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
					_ = jsonout.WriteResult(os.Stdout, nil, warningLines(warnBuf.String()), retErr)
					retErr = jsonout.ErrAlreadyWritten
				}
			}()

			commands.SetWarnWriter(&warnBuf)
			defer commands.SetWarnWriter(os.Stderr)

			statePath, err := resolveValidateStatePath(args)
			if err != nil {
				return err
			}
			err = commands.ValidateCommandWithOptions(statePath, commands.ValidateOptions{
				SkipSpecFileCheck: skipSpecCheck,
				SkipProcessChecks: skipProcessChecks,
				Repair:            repair,
			})
			warnings := warningLines(warnBuf.String())
			if err != nil {
				return err // deferred guard classifies as validation error
			}
			return jsonout.WriteResult(os.Stdout, map[string]bool{"valid": true}, warnings, nil)
		}

		statePath, err := resolveValidateStatePath(args)
		if err != nil {
			return err
		}
		err = commands.ValidateCommandWithOptions(statePath, commands.ValidateOptions{
			SkipSpecFileCheck: skipSpecCheck,
			SkipProcessChecks: skipProcessChecks,
			Repair:            repair,
		})
		if err != nil {
			return err
		}
		fmt.Println("VALID")
		return nil
	},
}

func resolveValidateStatePath(args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	projectRoot, err := requireProjectRoot()
	if err != nil {
		return "", err
	}
	return paths.New(projectRoot).StatePath(), nil
}

func warningLines(text string) []string {
	var warnings []string
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if line != "" {
			warnings = append(warnings, line)
		}
	}
	return warnings
}

var migrateCmd = &cobra.Command{
	Use:   "migrate [state-file]",
	Short: "Normalize role names in state.yaml",
	Long: `Migrate state.yaml by normalizing underscore-form role names to
their canonical hyphenated form (e.g. code_reviewer → code-reviewer).

If no state-file argument is provided, defaults to the project runtime state.yaml.
Reports whether any changes were made.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		statePath := ""
		if len(args) > 0 {
			statePath = args[0]
		} else {
			statePath = filepath.Join(paths.ProjectDirName(), paths.StateFileName)
		}

		changed, err := commands.MigrateCommand(statePath)
		if err != nil {
			return err
		}
		if changed {
			fmt.Println("Migration complete: role names normalized.")
		} else {
			fmt.Println("No changes needed: state already uses canonical role names.")
		}
		return nil
	},
}

// agentFlagNames is the canonical list of supported agent flag names.
var agentFlagNames = []string{"claude", "codex", "cursor", "opencode", "gemini", "mistral"}

// hasExplicitInitFlags returns true if any workspace-specific flag was explicitly set.
// This prevents the interactive wizard from silently swallowing CLI flags it doesn't collect.
func hasExplicitInitFlags(cmd *cobra.Command) bool {
	for _, name := range []string{"spec", "config", "entry-point", "branch", "post-worktree-cmd", "copy-worktree-env-files", "default-cli", "default-doer-cli", "default-reviewer-cli"} {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

func validateDefaultCLIFlag(name, value string) error {
	if value == "" {
		return nil
	}
	if !slices.Contains(agent.ValidCLIs(), value) {
		return fmt.Errorf("invalid --%s: %s (must be %s)", name, value, strings.Join(agent.ValidCLIs(), ", "))
	}
	return nil
}

// collectAgentFlags returns the agent names whose boolean flags are set on cmd.
func collectAgentFlags(cmd *cobra.Command) []string {
	var agents []string
	providers, _ := cmd.Flags().GetStringArray("provider")
	agents = append(agents, providers...)
	for _, name := range agentFlagNames {
		if v, _ := cmd.Flags().GetBool(name); v {
			agents = append(agents, name)
		}
	}
	return agents
}

func collectSetupProviderFlags(cmd *cobra.Command) ([]string, error) {
	agents := collectAgentFlags(cmd)
	if len(agents) > 0 || !interactive.IsInteractive() {
		return agents, nil
	}
	return promptDetectedProviders(os.Stdin, cmd.OutOrStdout())
}

func promptDetectedProviders(in io.Reader, out io.Writer) ([]string, error) {
	cat, _ := providercatalog.Load(cmdContext(), providercatalog.LoadOptions{})
	results := providercatalog.Detect(cat, nil)
	installed := setupDetectableProviders(cat, results)
	if len(installed) == 0 {
		return nil, nil
	}
	if in == os.Stdin {
		return promptDetectedProvidersInteractive(installed, in, out)
	}
	return promptDetectedProvidersText(installed, in, out)
}

func promptDetectedProvidersInteractive(installed []providercatalog.DetectionResult, in io.Reader, out io.Writer) ([]string, error) {
	selected, options := detectedProviderPickerOptions(installed)
	field := huh.NewMultiSelect[string]().
		Title("Detected providers").
		Description("Use Space to toggle providers, Enter to confirm.").
		Options(options...).
		Value(&selected)
	if err := huh.NewForm(huh.NewGroup(field)).WithInput(in).WithOutput(out).Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil, nil
		}
		return nil, err
	}
	return selected, nil
}

func setupDetectableProviders(cat providercatalog.Catalog, results []providercatalog.DetectionResult) []providercatalog.DetectionResult {
	var installed []providercatalog.DetectionResult
	for _, result := range results {
		if !result.Installed {
			continue
		}
		provider, ok := cat.Resolve(result.ID)
		if !ok || provider.Setup.ConfigDir == "" || provider.Setup.SkillsDir == "" {
			continue
		}
		installed = append(installed, result)
	}
	return installed
}

func detectedProviderPickerOptions(installed []providercatalog.DetectionResult) ([]string, []huh.Option[string]) {
	selected := make([]string, 0, len(installed))
	options := make([]huh.Option[string], 0, len(installed))
	for _, result := range installed {
		selected = append(selected, result.ID)
		label := fmt.Sprintf("%s (%s)", result.DisplayName, result.ID)
		options = append(options, huh.NewOption(label, result.ID).Selected(true))
	}
	return selected, options
}

func promptDetectedProvidersText(installed []providercatalog.DetectionResult, in io.Reader, out io.Writer) ([]string, error) {
	fmt.Fprintln(out, "Detected providers:")
	for i, result := range installed {
		fmt.Fprintf(out, "  %d. %s (%s)\n", i+1, result.DisplayName, result.ID)
	}
	fmt.Fprint(out, "Select providers by number or id, comma-separated (blank to skip): ")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, nil
	}
	var selected []string
	for _, part := range strings.Split(line, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if n, err := strconv.Atoi(part); err == nil {
			if n < 1 || n > len(installed) {
				return nil, fmt.Errorf("provider selection %d out of range", n)
			}
			selected = append(selected, installed[n-1].ID)
			continue
		}
		selected = append(selected, part)
	}
	return selected, nil
}

func cmdContext() context.Context {
	return context.Background()
}

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(providersCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(migrateCmd)
	providersCmd.AddCommand(providersListCmd)
	providersCmd.AddCommand(providersDetectCmd)
	providersCmd.AddCommand(providersRefreshCmd)

	// Setup command flags
	setupCmd.Flags().Bool("force", false, "overwrite existing global config")
	setupCmd.Flags().String("agent-tools", "", "path to custom AGENT_TOOLS.md (replaces embedded default)")
	setupCmd.Flags().StringArray("provider", nil, "create setup links for provider catalog id (repeatable)")
	setupCmd.Flags().Bool("claude", false, "create skill symlinks in ~/.claude/")
	setupCmd.Flags().Bool("codex", false, "create skill symlinks in ~/.codex/")
	setupCmd.Flags().Bool("opencode", false, "create skill symlinks in ~/.config/opencode/")
	setupCmd.Flags().Bool("gemini", false, "create skill symlinks in ~/.gemini/")
	setupCmd.Flags().Bool("mistral", false, "create skill symlinks in ~/.vibe/")

	// Init command flags
	initCmd.Flags().String("spec", "specs/vision.md", "path to goal spec file")
	initCmd.Flags().String("config", defaultPipelineConfigPath(), "path to pipeline YAML config file")
	initCmd.Flags().String("entry-point", "", `entry-point name: "general-objective", "functional-spec", "technical-spec", or legacy "detailed-spec" in default pipeline (default: auto-classified by orchestrator)`)
	initCmd.Flags().String("branch", "integration", "integration branch name")
	initCmd.Flags().String("post-worktree-cmd", "", "shell command to run after worktree creation (e.g. 'make setup')")
	initCmd.Flags().Bool("copy-worktree-env-files", false, "copy ignored root env files into worktrees before setup commands")
	initCmd.Flags().Bool("auto-resume", false, "automatically resume at checkpoint and sprint completion")
	initCmd.Flags().Bool("no-follow-up", false, "run only the entry-point subpipeline by suppressing top-level pipeline transitions")
	initCmd.Flags().String("default-cli", "", "default CLI for agent spawning ("+providerCLIHelpHint+")")
	initCmd.Flags().String("default-doer-cli", "", "default CLI for doer and orchestrator agent spawning ("+providerCLIHelpHint+")")
	initCmd.Flags().String("default-reviewer-cli", "", "default CLI for reviewer agent spawning ("+providerCLIHelpHint+")")
	initCmd.Flags().StringArray("scip-search", nil, "enable a SCIP language for indexing (repeatable)")
	initCmd.Flags().StringArray("scip-search-plan", nil, "pairing SCIP root override: go=<module-root>, typescript=<cwd>,<project-root>, or python=<cwd>[,<target-only>] (repeatable)")
	initCmd.Flags().StringArray("provider", nil, "activate provider catalog id (repeatable)")
	initCmd.Flags().Bool("claude", false, fmt.Sprintf("create CLAUDE.md symlink to ~/%s/CORE.md", brand.GlobalDirName))
	initCmd.Flags().Bool("codex", false, fmt.Sprintf("create AGENTS.md symlink to ~/%s/CORE.md and configure repo hooks", brand.GlobalDirName))
	initCmd.Flags().Bool("cursor", false, fmt.Sprintf("create AGENTS.md symlink to ~/%s/CORE.md and configure Cursor shell hooks", brand.GlobalDirName))
	initCmd.Flags().Bool("opencode", false, fmt.Sprintf("create AGENTS.md symlink to ~/%s/CORE.md", brand.GlobalDirName))
	initCmd.Flags().Bool("gemini", false, fmt.Sprintf("create GEMINI.md symlink to ~/%s/CORE.md", brand.GlobalDirName))
	initCmd.Flags().Bool("mistral", false, fmt.Sprintf("set up ~/.vibe/ for %s contract", brand.NameTitle))
	registerCompletion(initCmd, "entry-point", completeValues("general-objective", "functional-spec", "technical-spec", "detailed-spec"))
	registerCompletion(initCmd, "default-cli", completeCLINames)
	registerCompletion(initCmd, "default-doer-cli", completeCLINames)
	registerCompletion(initCmd, "default-reviewer-cli", completeCLINames)
	registerCompletion(initCmd, "scip-search", completeValues("go", "typescript", "python"))

	// Validate command flags
	validateCmd.Flags().Bool("skip-spec-check", false, "skip spec file existence check")
	validateCmd.Flags().Bool("skip-process-checks", false, fmt.Sprintf("skip live %s agent process checks for offline or archived state validation", brand.BinaryName))
	validateCmd.Flags().Bool("repair", false, "repair invalid active ownership before validating")

	// JSON output flags
	addJSONFlag(versionCmd)
	addJSONFlag(validateCmd)
}
