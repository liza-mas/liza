package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent <role> [initial-task-id]",
	Short: "Run agent supervisor loop",
	Long: `Start an agent supervisor for a specific role.

The supervisor:
- Registers the agent with collision detection
- Polls for role-specific work (all doer, reviewer, and orchestrator roles)
- Claims tasks (doer/reviewer roles only)
- Builds and executes prompts with the specified CLI
- Manages heartbeats to keep lease alive
- Handles restarts on exit code 42
- Loops until work is exhausted or ABORT signal

Roles:
  orchestrator        - Creates and manages task breakdown

  Specification phase:
  epic-planner        - Decomposes vision into epics
  epic-plan-reviewer  - Reviews epic decomposition
  us-writer           - Writes user stories from epics
  us-reviewer         - Reviews user stories

  Coding phase:
  code-planner        - Claims and produces coding plans
  code-plan-reviewer  - Reviews coding plans and submits verdicts
  coder               - Claims and implements coding tasks
  code-reviewer       - Reviews coding tasks and submits verdicts

Agent ID defaults to the first <role>-N not already registered with a valid lease
(e.g. coder-1, or coder-2 if coder-1 is active). Override with --agent-id or
LIZA_AGENT_ID.

Example:
  # Auto-assigned agent ID (simplest)
  liza agent coder
  liza agent code-reviewer --cli codex
  liza agent orchestrator --interactive

  # Explicit agent ID
  liza agent coder --agent-id coder-1
  liza agent code-reviewer --agent-id code-reviewer-2 --cli gemini

  # Disable saving agent output to .liza/agent-outputs/
  liza agent coder --no-log

  # Using LIZA_AGENT_ID environment variable
  LIZA_AGENT_ID=coder-1 liza agent coder`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		role := args[0]

		initialTask := ""
		if len(args) == 2 {
			initialTask = args[1]
		}

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		pipelineCfg, pipelineErr := pipeline.LoadFrozen(projectRoot)
		if pipelineErr != nil {
			return fmt.Errorf("failed to load pipeline config: %w", pipelineErr)
		}
		validRoles := pipeline.NewResolver(pipelineCfg).AllRoleNames()
		if !slices.Contains(validRoles, role) {
			return fmt.Errorf("invalid role: %s (valid: %s)", role, strings.Join(validRoles, ", "))
		}

		// Resolve agent ID: flag > env var > auto-generate from state
		flagValue, _ := cmd.Flags().GetString("agent-id")
		agentID, err := identity.Resolve(identity.Config{
			FlagValue: flagValue,
			Required:  false,
		})
		if err != nil {
			return err
		}
		autoAssigned := agentID == ""

		if err := identity.ValidateRole(agentID, role); !autoAssigned && err != nil {
			return err
		}

		cliName, _ := cmd.Flags().GetString("cli")
		interactive, _ := cmd.Flags().GetBool("interactive")
		noLog, _ := cmd.Flags().GetBool("no-log")

		statePath := filepath.Join(projectRoot, ".liza", "state.yaml")

		// Resolve default CLI from state config when --cli is not explicitly set
		flagChanged := cmd.Flags().Changed("cli")
		var stateConfigCLI string
		if !flagChanged {
			bb := db.For(statePath)
			if state, err := bb.Read(); err == nil {
				stateConfigCLI = state.Config.DefaultCLI
			} else {
				fmt.Fprintf(os.Stderr, "Warning: could not read state for default CLI: %v\n", err)
			}
		}
		cliName = agent.ResolveCLIFromState(flagChanged, cliName, stateConfigCLI)

		if !slices.Contains(agent.ValidCLIs(), cliName) {
			return fmt.Errorf("invalid CLI: %s (must be %s)", cliName, strings.Join(agent.ValidCLIs(), ", "))
		}

		shouldLog := !noLog && !interactive

		// Warn if no Liza contract symlink is configured for this CLI
		if commands.CheckContractConfigured(projectRoot, cliName) == "" {
			initFlag := cliName
			if cliName == "kimi" {
				initFlag = "claude" // kimi uses Claude's config
			}
			fmt.Fprintf(os.Stderr, "Warning: no Liza contract symlink found for %s. Agents may not find the behavioral contract.\n", cliName)
			fmt.Fprintf(os.Stderr, "  Run 'liza init --%s' to create one.\n", initFlag)
		}

		specsDir := os.Getenv("LIZA_SPECS")
		if specsDir == "" {
			specsDir = filepath.Join(projectRoot, "specs")
		}

		// Set up paths for agent outputs (enabled by default, disabled by --no-log or -i)
		var outputsDir string
		if shouldLog {
			lizaPaths := paths.New(projectRoot)
			outputsDir = lizaPaths.AgentOutputsDir()
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		if autoAssigned {
			bb := db.For(statePath)
			_, err = agent.AutoAssignAgentID(bb, role, 5, func(candidateID string) error {
				fmt.Fprintf(os.Stderr, "Auto-assigned agent ID: %s\n", candidateID)
				config := agent.SupervisorConfig{
					AgentID:     candidateID,
					Role:        role,
					ProjectRoot: projectRoot,
					StatePath:   statePath,
					LogPath:     filepath.Join(projectRoot, ".liza", "log.yaml"),
					SpecsDir:    specsDir,
					CLIName:     cliName,
					Interactive: interactive,
					InitialTask: initialTask,
					Executor:    agent.NewDefaultCLIExecutor(outputsDir),
				}
				return agent.RunSupervisor(ctx, config)
			})
			return err
		}

		config := agent.SupervisorConfig{
			AgentID:     agentID,
			Role:        role,
			ProjectRoot: projectRoot,
			StatePath:   statePath,
			LogPath:     filepath.Join(projectRoot, ".liza", "log.yaml"),
			SpecsDir:    specsDir,
			CLIName:     cliName,
			Interactive: interactive,
			InitialTask: initialTask,
			Executor:    agent.NewDefaultCLIExecutor(outputsDir),
		}

		return agent.RunSupervisor(ctx, config)
	},
}

var recoverTaskCmd = &cobra.Command{
	Use:   "recover-task <task-id>",
	Short: "Recover a task (release claims, remove worktree and branch)",
	Long: `Recover a task by performing full cleanup:

- Release agent claims (doer and/or reviewer)
- Remove git worktree and branch
- Recover the claiming agent from state

Normal mode (no --force): requires the task to exist in state. Refuses if the
claiming agent's PID is still alive.

Force mode (--force): cleans up git artifacts (worktree + branch) even if the
task is not in state. Use this when state is already clean but orphaned git
artifacts remain after a hard crash.

Idempotent: safe to run multiple times.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := args[0]
		force, _ := cmd.Flags().GetBool("force")
		reason, _ := cmd.Flags().GetString("reason")
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		return commands.RecoverTaskCommand(projectRoot, taskID, force, reason)
	},
}

var recoverAgentCmd = &cobra.Command{
	Use:   "recover-agent <agent-id>",
	Short: "Recover a crashed agent (release claims, remove worktree, delete agent)",
	Long: `Recover a crashed agent by performing full cleanup:

- Release task claims (executing → initial for doers, reviewing → submitted for reviewers)
- Remove git worktree and branch (doers only)
- Delete agent from state

Idempotent: safe to run multiple times (no error if agent already gone).
By default, refuses to recover agents whose PID is still alive.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		agentID := args[0]
		force, _ := cmd.Flags().GetBool("force")
		cli, _ := cmd.Flags().GetString("cli")
		reason, _ := cmd.Flags().GetString("reason")
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		return commands.RecoverAgentCommand(projectRoot, agentID, force, cli, reason)
	},
}

var deleteAgentCmd = &cobra.Command{
	Use:   "agent <agent-id>",
	Short: "Delete an agent from the state database",
	Long: `Remove an agent from the state database.

Useful when an agent has crashed or shutdown uncleanly and needs to be removed
from the system. By default, refuses to delete agents with active leases or
current tasks.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		agentID := args[0]
		force, _ := cmd.Flags().GetBool("force")
		reason, _ := cmd.Flags().GetString("reason")
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		return commands.DeleteAgentCommand(projectRoot, agentID, force, reason, os.Stdin)
	},
}

func init() {
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(recoverTaskCmd)
	rootCmd.AddCommand(recoverAgentCmd)
	deleteCmd.AddCommand(deleteAgentCmd)

	// Agent command flags
	addAgentIDFlag(agentCmd)
	agentCmd.Flags().String("cli", "", "CLI to use; defaults to config default_cli, then LIZA_DEFAULT_CLI env, then claude ("+strings.Join(agent.ValidCLIs(), ", ")+")")
	agentCmd.Flags().BoolP("interactive", "i", false, "Print prompt location, don't execute CLI")
	agentCmd.Flags().Bool("no-log", false, "Disable saving agent output to .liza/agent-outputs/")

	// Recover-task command flags
	recoverTaskCmd.Flags().Bool("force", false, "clean up git artifacts even if task is not in state")
	recoverTaskCmd.Flags().String("reason", "task recovery", "reason for recovering the task")

	// Recover-agent command flags
	recoverAgentCmd.Flags().Bool("force", false, "override PID liveness check (refuse by default if process is alive)")
	recoverAgentCmd.Flags().String("cli", "", "respawn the agent after cleanup using this CLI (e.g., claude, codex)")
	recoverAgentCmd.Flags().String("reason", "agent recovery", "reason for recovering the agent")

	// Delete agent command flags
	deleteAgentCmd.Flags().Bool("force", false, "force deletion even if agent has active lease or current task")
	deleteAgentCmd.Flags().String("reason", "manual deletion", "reason for deleting the agent")
}
