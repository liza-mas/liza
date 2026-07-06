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
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent <role> [initial-task-id]",
	Short: "Run agent supervisor loop",
	Long: fmt.Sprintf(`Start an agent supervisor for a specific role.

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
%[1]s. The resolved ID is exported as %[1]s and the legacy env alias to spawned
provider CLIs so hooks select Multi-Agent mode.

Example:
  # Auto-assigned agent ID (simplest)
  %[2]s agent coder
  %[2]s agent code-reviewer --cli codex
  %[2]s agent orchestrator --interactive

  # Explicit agent ID
  %[2]s agent coder --agent-id coder-1
  %[2]s agent code-reviewer --agent-id code-reviewer-2 --cli gemini

  # Disable saving agent output to %[3]s/agent-outputs/
  %[2]s agent coder --no-log

  # Using %[1]s environment variable
  %[1]s=coder-1 %[2]s agent coder`, brand.EnvName("AGENT_ID"), brand.BinaryName, paths.ProjectDirName()),
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
		roleType, err := pipeline.NewResolver(pipelineCfg).RoleType(role)
		if err != nil {
			return err
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
		profileName, _ := cmd.Flags().GetString("profile")
		goalID, _ := cmd.Flags().GetString("goal-id")
		interactive, _ := cmd.Flags().GetBool("interactive")
		explainLaunch, _ := cmd.Flags().GetBool("explain-launch")
		noLog, _ := cmd.Flags().GetBool("no-log")

		lizaPaths := paths.New(projectRoot)
		statePath := lizaPaths.StatePath()
		bb := db.For(statePath)
		state, stateErr := bb.Read()
		if stateErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not read state for agent metadata: %v\n", stateErr)
		} else {
			stateGoalID := state.Goal.ID
			if goalID != "" && stateGoalID != "" && goalID != stateGoalID {
				return fmt.Errorf("--goal-id %q does not match state goal.id %q", goalID, stateGoalID)
			}
		}

		var runtimeConfig models.Config
		if state != nil {
			runtimeConfig = state.Config
		}

		resolvedProfile, err := agent.ResolveProfileForRole(profileName, roleType, runtimeConfig)
		if err != nil {
			return err
		}

		// Resolve default CLI from state config when --cli is not explicitly set
		flagChanged := cmd.Flags().Changed("cli")
		cliName, err = agent.ResolveCLIWithProfile(flagChanged, cliName, resolvedProfile, roleType, runtimeConfig)
		if err != nil {
			return err
		}

		availableCLIs := agent.AvailableCLIs(runtimeConfig)
		if !slices.Contains(availableCLIs, cliName) {
			return fmt.Errorf("invalid CLI: %s (must be %s)", cliName, strings.Join(availableCLIs, ", "))
		}
		if err := agent.CheckCLIPrerequisitesWithConfig(cliName, runtimeConfig); err != nil {
			return err
		}

		shouldLog := !noLog && !interactive

		contractKey := agent.ContractKeyForCLI(cliName, runtimeConfig)
		// Warn if no product contract symlink is configured for this CLI.
		if contractKey != "" && contractKey != "none" && commands.CheckContractConfigured(projectRoot, contractKey) == "" {
			fmt.Fprintf(os.Stderr, "Warning: no %s contract symlink found for %s. Agents may not find the behavioral contract.\n", brand.NameTitle, cliName)
			fmt.Fprintf(os.Stderr, "  Run '%s' to create one.\n", contractInitCommandForMissingContract(cliName, contractKey))
		}

		specsLookup := brand.LookupEnv(os.Getenv, "SPECS")
		if specsLookup.Warning != "" {
			fmt.Fprintf(os.Stderr, "Warning: %s\n", specsLookup.Warning)
		}
		specsDir := specsLookup.Value
		if specsDir == "" {
			specsDir = filepath.Join(projectRoot, "specs")
		}

		// Set up paths for agent outputs (enabled by default, disabled by --no-log or -i)
		var outputsDir string
		if shouldLog {
			outputsDir = lizaPaths.AgentOutputsDir()
		}

		if explainLaunch {
			return explainAgentLaunch(cmd, cliName, resolvedProfile, projectRoot, outputsDir, runtimeConfig)
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
					LogPath:     lizaPaths.LogPath(),
					SpecsDir:    specsDir,
					CLIName:     cliName,
					ProfileName: resolvedProfile.Name,
					ProfileVars: resolvedProfile.Vars,
					Interactive: interactive,
					InitialTask: initialTask,
					LLMAgent:    agent.NewLLMAgentForCLIWithConfig(cliName, outputsDir, runtimeConfig),
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
			LogPath:     lizaPaths.LogPath(),
			SpecsDir:    specsDir,
			CLIName:     cliName,
			ProfileName: resolvedProfile.Name,
			ProfileVars: resolvedProfile.Vars,
			Interactive: interactive,
			InitialTask: initialTask,
			LLMAgent:    agent.NewLLMAgentForCLIWithConfig(cliName, outputsDir, runtimeConfig),
		}

		return agent.RunSupervisor(ctx, config)
	},
}

func explainAgentLaunch(cmd *cobra.Command, cliName string, profile agent.ResolvedProfile, projectRoot, outputsDir string, runtimeConfig models.Config) error {
	promptFile := ""
	if tool, ok := agent.AgentToolRegistry(runtimeConfig)[cliName]; ok && tool.PromptTransport == agent.PromptTransportFile {
		promptFile = "<prompt-file>"
	}
	plan, err := agent.ResolveLaunchPlan(agent.LaunchPlanRequest{
		ToolName:      cliName,
		ProfileName:   profile.Name,
		ProfileVars:   profile.Vars,
		Prompt:        "<prompt>",
		PromptFile:    promptFile,
		ProjectRoot:   projectRoot,
		OutputsDir:    outputsDir,
		RuntimeConfig: runtimeConfig,
	})
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "tool: %s\n", plan.ToolName)
	if plan.ProfileName != "" {
		fmt.Fprintf(out, "profile: %s\n", plan.ProfileName)
	}
	fmt.Fprintf(out, "backend: %s\n", plan.Backend)
	fmt.Fprintf(out, "executable: %s\n", plan.Executable)
	fmt.Fprintf(out, "args: %s\n", strings.Join(plan.Args, " "))
	fmt.Fprintf(out, "prompt_transport: %s\n", plan.PromptTransport)
	if plan.PromptFile != "" {
		fmt.Fprintf(out, "prompt_file: %s\n", plan.PromptFile)
	}
	if len(plan.EnvFiles) > 0 {
		fmt.Fprintf(out, "env_files: %s\n", strings.Join(plan.EnvFiles, ", "))
	}
	if plan.ContractKey != "" {
		fmt.Fprintf(out, "contract_key: %s\n", plan.ContractKey)
	}
	return nil
}

func contractInitCommandForProvider(cliName string) string {
	switch cliName {
	case "kimi":
		return "liza init --claude" // kimi uses Claude's config
	case "codex-acp":
		return "liza init --codex" // codex-acp uses Codex's config
	case "cursor-acp":
		return "liza init --cursor" // cursor-acp uses the shared AGENTS.md contract setup plus Cursor hooks
	case "opencode-acp":
		return "liza init --opencode" // opencode-acp uses OpenCode's config
	case "claude", "codex", "cursor", "opencode", "gemini", "mistral":
		return "liza init --" + cliName
	default:
		return "liza init --provider " + cliName
	}
}

func contractInitCommandForMissingContract(cliName, contractKey string) string {
	if cliName != "" {
		return contractInitCommandForProvider(cliName)
	}
	return contractInitCommandForProvider(contractKey)
}

var recoverTaskCmd = &cobra.Command{
	Use:   "recover-task <task-id>",
	Short: "Recover a task while preserving recoverable work by default",
	Long: `Recover a task while preserving recoverable work by default:

- Release agent claims (doer and/or reviewer)
- Preserve or reattach the task worktree/branch when coherent
- Validate submitted review candidates before restoring review flow
- Recover the claiming agent from state

Normal mode (no --force): requires the task to exist in state. Refuses if the
claiming agent's PID is still alive.

Fresh mode (--fresh): explicitly discards the task branch/worktree and resets a
non-terminal in-state task to its role-pair initial status with a fresh worktree
from the integration branch. If a claiming agent PID is still alive, use
--fresh --force.

Fresh mode keeps BLOCKED tasks BLOCKED: it repairs the substrate and preserves
blocked_reason / blocked_questions. Use unblock-task to restore claimability.

Force mode (--force): bypasses live-PID checks for in-state tasks. It also cleans
up git artifacts (worktree + branch) when the task is not in state. Use absent-
state cleanup when state is already clean but orphaned git artifacts remain after
a hard crash.

Idempotent: safe to run multiple times.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := args[0]
		force, _ := cmd.Flags().GetBool("force")
		fresh, _ := cmd.Flags().GetBool("fresh")
		reason, _ := cmd.Flags().GetString("reason")
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		return commands.RecoverTaskCommand(projectRoot, taskID, force, fresh, reason)
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

var repairAgentPoolCmd = &cobra.Command{
	Use:   "repair-agent-pool",
	Short: "Repair missing agent roles for claimable work",
	Long: fmt.Sprintf(`Repair the runtime agent pool for claimable work.

By default, detects roles with immediately claimable tasks but no live registered
agent, then spawns one agent process per missing role. The --missing flag is kept
as an explicit spelling of the default behavior.

Use --cli to choose the backend for newly spawned agents. When omitted, the CLI
defaults to role-specific config, role-specific env, config.default_cli,
%s, then claude.

Examples:
  %s repair-agent-pool --dry-run
  %s repair-agent-pool --cli codex`, brand.EnvName("DEFAULT_CLI"), brand.BinaryName, brand.BinaryName),
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		missing, _ := cmd.Flags().GetBool("missing")
		cli, _ := cmd.Flags().GetString("cli")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		return commands.RepairAgentPoolCommand(commands.RepairAgentPoolOptions{
			ProjectRoot: projectRoot,
			Missing:     missing,
			CLI:         cli,
			DryRun:      dryRun,
		})
	},
}

var markAgentDegradedCmd = &cobra.Command{
	Use:   "mark-agent-degraded <agent-id>",
	Short: "Mark an agent as degraded for role-capacity health",
	Long: `Mark an agent as degraded for role-capacity health.

This records diagnostic health separate from agent lifecycle status and task
state. repair-agent-pool ignores current degraded agent epochs when computing
effective capacity.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		role, _ := cmd.Flags().GetString("role")
		reason, _ := cmd.Flags().GetString("reason")
		task, _ := cmd.Flags().GetString("task")
		candidates, _ := cmd.Flags().GetStringSlice("candidate-task")
		lastError, _ := cmd.Flags().GetString("error")
		hint, _ := cmd.Flags().GetString("recover-hint")
		return commands.MarkAgentDegradedCommand(commands.MarkAgentDegradedOptions{
			ProjectRoot:    projectRoot,
			AgentID:        args[0],
			Role:           role,
			Reason:         reason,
			LastTask:       task,
			CandidateTasks: candidates,
			LastError:      lastError,
			RecoverHint:    hint,
		})
	},
}

var clearAgentDegradedCmd = &cobra.Command{
	Use:   "clear-agent-degraded <agent-id>",
	Short: "Clear degraded health for an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		return commands.ClearAgentDegradedCommand(projectRoot, args[0])
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
	rootCmd.AddCommand(repairAgentPoolCmd)
	rootCmd.AddCommand(markAgentDegradedCmd)
	rootCmd.AddCommand(clearAgentDegradedCmd)
	deleteCmd.AddCommand(deleteAgentCmd)

	agentCmd.ValidArgsFunction = completeAgentArgs
	recoverTaskCmd.ValidArgsFunction = completeTaskIDArgs(1)
	recoverAgentCmd.ValidArgsFunction = completeAgentIDArgs(1)
	markAgentDegradedCmd.ValidArgsFunction = completeAgentIDArgs(1)
	clearAgentDegradedCmd.ValidArgsFunction = completeAgentIDArgs(1)
	deleteAgentCmd.ValidArgsFunction = completeAgentIDArgs(1)

	// Agent command flags
	addAgentIDFlag(agentCmd)
	agentCmd.Flags().String("cli", "", "CLI to use; defaults by role-specific then global config/env ("+providerCLIHelpHint+")")
	agentCmd.Flags().String("profile", "", "structured launch profile from config.agent_profiles")
	agentCmd.Flags().Bool("explain-launch", false, "resolve and print provider launch configuration without running an agent")
	agentCmd.Flags().String("goal-id", "", "goal identifier marker for process diagnostics (must match state goal.id when supplied)")
	agentCmd.Flags().BoolP("interactive", "i", false, "Print prompt location, don't execute CLI")
	agentCmd.Flags().Bool("no-log", false, "Disable saving agent output to "+paths.ProjectDirName()+"/agent-outputs/")

	// Recover-task command flags
	recoverTaskCmd.Flags().Bool("force", false, "bypass live-PID checks; also clean git artifacts when task is not in state")
	recoverTaskCmd.Flags().Bool("fresh", false, "discard task branch/worktree and reset in-state task from integration")
	recoverTaskCmd.Flags().String("reason", "task recovery", "reason for recovering the task")

	// Recover-agent command flags
	recoverAgentCmd.Flags().Bool("force", false, "override PID liveness check (refuse by default if process is alive)")
	recoverAgentCmd.Flags().String("cli", "", "respawn the agent after cleanup using this CLI (e.g., claude, codex)")
	recoverAgentCmd.Flags().String("reason", "agent recovery", "reason for recovering the agent")

	// Repair agent pool command flags
	repairAgentPoolCmd.Flags().Bool("missing", false, "spawn roles with claimable work and no registered agent")
	repairAgentPoolCmd.Flags().String("cli", "", "CLI to use for spawned agents; defaults by role-specific then global config/env; see docs")
	repairAgentPoolCmd.Flags().Bool("dry-run", false, "print missing roles and spawn commands without launching agents")
	registerCompletion(agentCmd, "cli", completeCLINames)
	registerCompletion(recoverAgentCmd, "cli", completeCLINames)
	registerCompletion(repairAgentPoolCmd, "cli", completeCLINames)

	// Agent health command flags
	markAgentDegradedCmd.Flags().String("role", "", "runtime role whose capacity is degraded")
	markAgentDegradedCmd.Flags().String("reason", "", "structured degraded reason")
	markAgentDegradedCmd.Flags().String("task", "", "last task associated with the degraded signal")
	markAgentDegradedCmd.Flags().StringSlice("candidate-task", nil, "candidate task considered during the degraded signal")
	markAgentDegradedCmd.Flags().String("error", "", "last observed error")
	markAgentDegradedCmd.Flags().String("recover-hint", "", "operator recovery hint")
	markAgentDegradedCmd.MarkFlagRequired("role")
	markAgentDegradedCmd.MarkFlagRequired("reason")
	markAgentDegradedCmd.MarkFlagRequired("error")
	registerCompletion(markAgentDegradedCmd, "role", completeAgentRoles)
	registerCompletion(markAgentDegradedCmd, "task", completeTaskIDs)
	registerCompletion(markAgentDegradedCmd, "candidate-task", completeTaskIDs)

	// Delete agent command flags
	deleteAgentCmd.Flags().Bool("force", false, "force deletion even if agent has active lease or current task")
	deleteAgentCmd.Flags().String("reason", "manual deletion", "reason for deleting the agent")
}
