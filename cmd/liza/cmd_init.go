package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/interactive"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/paths"
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
		fmt.Printf("liza version %s\n", Version)
		fmt.Printf("  commit: %s\n", GitCommit)
		fmt.Printf("  built:  %s\n", BuildDate)
	},
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "One-time global setup of Liza contracts and skills",
	Long: `Write Liza contracts and skills to ~/.liza/ for global access.

This is a one-time setup step that populates the global config directory.
Contracts are written flat (e.g., ~/.liza/CORE.md) and skills are written
to ~/.liza/skills/.

After running setup, use 'liza init' in each project to create the
project-local blackboard and symlinks.

Use --force to overwrite an existing global config.
Use --agent-tools to install a custom AGENT_TOOLS.md instead of the embedded default.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir, err := paths.GlobalLizaDir()
		if err != nil {
			return err
		}
		force, _ := cmd.Flags().GetBool("force")
		agentToolsPath, _ := cmd.Flags().GetString("agent-tools")

		agents := collectAgentFlags(cmd)

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
	Short: "Initialize a new Liza workspace or enable pairing",
	Long: `Initialize a new Liza workspace by creating .liza directory structure,
generating initial state.yaml, and setting up the integration branch.

The description argument is required and describes the goal.
The spec file (default: specs/vision.md) must exist before initialization.

Use --config to provide a pipeline YAML file (defaults to ~/.liza/pipeline.yaml).
The config is validated and frozen into .liza/pipeline.yaml. Use --entry-point to
specify which entry-point to use (must be defined in the config).

Use --branch to set the integration branch name (default: "integration").
All worktrees branch from and merge back to this branch.

Use --post-worktree-cmd to specify a shell command that runs after every worktree
creation (e.g. 'make setup', 'npm install'). This ensures worktrees are
build/test-ready without hardcoding project-specific tooling into Liza.
Existing workspaces can add post_worktree_cmd to state.yaml's config section.

PAIRING MODE: Use agent flags without a description to create only the contract
symlinks needed for pairing (no .liza/ workspace):
  liza init --claude           # creates CLAUDE.md → ~/.liza/CORE.md
  liza init --claude --codex   # creates CLAUDE.md + AGENTS.md`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		agents := collectAgentFlags(cmd)
		autoResume, _ := cmd.Flags().GetBool("auto-resume")
		defaultCLI, _ := cmd.Flags().GetString("default-cli")
		if defaultCLI != "" && !slices.Contains(agent.ValidCLIs(), defaultCLI) {
			return fmt.Errorf("invalid --default-cli: %s (must be %s)", defaultCLI, strings.Join(agent.ValidCLIs(), ", "))
		}

		// Interactive wizard: no args, no agent flags, no explicit workspace flags, TTY
		if len(args) == 0 && len(agents) == 0 && !hasExplicitInitFlags(cmd) {
			if !interactive.IsInteractive() {
				return fmt.Errorf("requires a description argument or at least one agent flag (--claude, --codex, --gemini, --mistral)\nSee: liza init --help")
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
				if err := commands.InitPairingCommand(commands.InitPairingParams{
					Agents:         result.Agents,
					Stdin:          os.Stdin,
					ContractAction: result.ContractAction,
				}); err != nil {
					return err
				}
				interactive.PrintPostInitSummary("pairing", result.Agents)
				return nil
			}
			if err := commands.InitCommandWithConfig(commands.InitParams{
				Description:     result.Description,
				SpecRef:         result.SpecRef,
				ConfigPath:      configPath,
				EntryPoint:      result.EntryPoint,
				Branch:          branch,
				PostWorktreeCmd: postWorktreeCmd,
				AutoResume:      autoResume,
				DefaultCLI:      defaultCLI,
				Agents:          result.Agents,
				Stdin:           os.Stdin,
				ContractAction:  result.ContractAction,
			}); err != nil {
				return err
			}
			interactive.PrintPostInitSummary("full", result.Agents)
			return nil
		}

		// Pairing mode: agent flags without description
		if len(args) == 0 {
			if len(agents) == 0 {
				return fmt.Errorf("requires a description argument or at least one agent flag (--claude, --codex, --gemini, --mistral)\nSee: liza init --help")
			}
			if autoResume {
				return fmt.Errorf("--auto-resume requires full workspace init (provide a description)")
			}
			if hasExplicitInitFlags(cmd) {
				return fmt.Errorf("workspace flags (--branch, --config, --spec, --entry-point, --post-worktree-cmd, --default-cli) require a description argument for full workspace init")
			}
			if err := commands.InitPairingCommand(commands.InitPairingParams{
				Agents: agents,
				Stdin:  os.Stdin,
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
			Description:     description,
			SpecRef:         specRef,
			ConfigPath:      configPath,
			EntryPoint:      entryPoint,
			Branch:          branch,
			PostWorktreeCmd: postCreateCmd,
			AutoResume:      autoResume,
			DefaultCLI:      defaultCLI,
			Agents:          agents,
			Stdin:           os.Stdin,
		}); err != nil {
			return err
		}
		interactive.PrintPostInitSummary("full", agents)
		return nil
	},
}

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
		statePath := ""
		if len(args) > 0 {
			statePath = args[0]
		} else {
			statePath = filepath.Join(paths.LizaDirName, paths.StateFileName)
		}

		skipSpecCheck, _ := cmd.Flags().GetBool("skip-spec-check")

		if isJSON(cmd) {
			log.SetOutput(io.Discard)
			defer log.SetOutput(os.Stderr)
			defer func() {
				if retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
					_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
					retErr = jsonout.ErrAlreadyWritten
				}
			}()

			var warnBuf bytes.Buffer
			commands.SetWarnWriter(&warnBuf)
			defer commands.SetWarnWriter(os.Stderr)

			err := commands.ValidateCommand(statePath, skipSpecCheck)
			var warnings []string
			if warnBuf.Len() > 0 {
				for _, line := range strings.Split(strings.TrimSpace(warnBuf.String()), "\n") {
					if line != "" {
						warnings = append(warnings, line)
					}
				}
			}
			if err != nil {
				return err // deferred guard classifies as validation error
			}
			return jsonout.WriteResult(os.Stdout, map[string]bool{"valid": true}, warnings, nil)
		}

		err := commands.ValidateCommand(statePath, skipSpecCheck)
		if err != nil {
			return err
		}
		fmt.Println("VALID")
		return nil
	},
}

var migrateCmd = &cobra.Command{
	Use:   "migrate [state-file]",
	Short: "Normalize role names in state.yaml",
	Long: `Migrate state.yaml by normalizing underscore-form role names to
their canonical hyphenated form (e.g. code_reviewer → code-reviewer).

If no state-file argument is provided, defaults to .liza/state.yaml.
Reports whether any changes were made.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		statePath := ""
		if len(args) > 0 {
			statePath = args[0]
		} else {
			statePath = filepath.Join(paths.LizaDirName, paths.StateFileName)
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
var agentFlagNames = []string{"claude", "codex", "gemini", "mistral"}

// hasExplicitInitFlags returns true if any workspace-specific flag was explicitly set.
// This prevents the interactive wizard from silently swallowing CLI flags it doesn't collect.
func hasExplicitInitFlags(cmd *cobra.Command) bool {
	for _, name := range []string{"spec", "config", "entry-point", "branch", "post-worktree-cmd", "default-cli"} {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

// collectAgentFlags returns the agent names whose boolean flags are set on cmd.
func collectAgentFlags(cmd *cobra.Command) []string {
	var agents []string
	for _, name := range agentFlagNames {
		if v, _ := cmd.Flags().GetBool(name); v {
			agents = append(agents, name)
		}
	}
	return agents
}

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(migrateCmd)

	// Setup command flags
	setupCmd.Flags().Bool("force", false, "overwrite existing global config")
	setupCmd.Flags().String("agent-tools", "", "path to custom AGENT_TOOLS.md (replaces embedded default)")
	setupCmd.Flags().Bool("claude", false, "create skill symlinks in ~/.claude/")
	setupCmd.Flags().Bool("codex", false, "create skill symlinks in ~/.codex/")
	setupCmd.Flags().Bool("gemini", false, "create skill symlinks in ~/.gemini/")
	setupCmd.Flags().Bool("mistral", false, "create skill symlinks in ~/.vibe/")

	// Init command flags
	initCmd.Flags().String("spec", "specs/vision.md", "path to goal spec file")
	initCmd.Flags().String("config", defaultPipelineConfigPath(), "path to pipeline YAML config file")
	initCmd.Flags().String("entry-point", "", `entry-point name: "general-objective" or "detailed-spec" in default pipeline (default: auto-classified by orchestrator)`)
	initCmd.Flags().String("branch", "integration", "integration branch name")
	initCmd.Flags().String("post-worktree-cmd", "", "shell command to run after worktree creation (e.g. 'make setup')")
	initCmd.Flags().Bool("auto-resume", false, "automatically resume at checkpoint and sprint completion")
	initCmd.Flags().String("default-cli", "", "default CLI for agent spawning ("+strings.Join(agent.ValidCLIs(), ", ")+")")
	initCmd.Flags().Bool("claude", false, "create CLAUDE.md symlink to ~/.liza/CORE.md")
	initCmd.Flags().Bool("codex", false, "create AGENTS.md symlink to ~/.liza/CORE.md")
	initCmd.Flags().Bool("gemini", false, "create GEMINI.md symlink to ~/.liza/CORE.md")
	initCmd.Flags().Bool("mistral", false, "set up ~/.vibe/ for Liza contract")

	// Validate command flags
	validateCmd.Flags().Bool("skip-spec-check", false, "skip spec file existence check")

	// JSON output flags
	addJSONFlag(versionCmd)
	addJSONFlag(validateCmd)
}
