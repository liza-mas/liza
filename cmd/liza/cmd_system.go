package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/interactive"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/tui"
	"github.com/spf13/cobra"
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Run circuit breaker pattern detection analysis",
	Long: fmt.Sprintf(`Analyze anomalies in the blackboard and detect systemic failure patterns.

Detects the following patterns:
  - retry_cluster: 3+ retry_loops with similar error patterns (ARCHITECTURE_FLAW)
  - debt_accumulation: 3+ trade_offs creating technical debt (SCOPE_FLAW)
  - assumption_cascade: 2+ assumption violations with same assumption (SPEC_FLAW)
  - spec_gap_cluster: 2+ spec ambiguities hitting same spec reference (SPEC_FLAW)
  - workaround_pattern: 2+ workarounds/trade-offs with similar root causes (ARCHITECTURE_FLAW)
  - external_service_outage: 2+ external blockers from same service (EXTERNAL_DEPENDENCY)
  - provider_audit_degradation: 2+ agents or 3+ hits for same provider (OBSERVABILITY_DEGRADED)

If a pattern is detected:
  - Updates circuit_breaker.status to TRIGGERED
  - Generates %[1]s/circuit_breaker_report.md with evidence
  - Sets sprint.status to CHECKPOINT (equivalent to '%[2]s')
  - Requires human review and resolution

If no patterns are detected:
  - Updates circuit_breaker.status to OK
  - Continues normal operation`, paths.ProjectDirName(), brand.Command("checkpoint")),
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		if isJSON(cmd) {
			log.SetOutput(io.Discard)
			defer log.SetOutput(os.Stderr)
			defer func() {
				if retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
					_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
					retErr = jsonout.ErrAlreadyWritten
				}
			}()
		}

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		if isJSON(cmd) {
			result, err := ops.Analyze(projectRoot)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.AnalyzeCommand(projectRoot)
	},
}

var updateSprintMetricsCmd = &cobra.Command{
	Use:   "update-sprint-metrics",
	Short: "Recompute sprint metrics from current state",
	Long: `Recompute sprint metrics by aggregating data from task and agent state.

Metrics computed:
  - tasks_done: Count of terminal tasks (MERGED, ABANDONED, SUPERSEDED)
  - tasks_in_progress: Count of active tasks (executing, submitted, rejected, INTEGRATION_FAILED)
  - tasks_blocked: Count of BLOCKED tasks
  - iterations_total: Sum of iterations_total from all agents
  - review_cycles_total: Sum of review_cycles_total from all tasks
  - review_verdict_approvals: Count of approval verdicts in task history
  - review_verdict_rejections: Count of rejection verdicts in task history
  - review_verdict_count: Total review verdicts
  - review_verdict_approval_rate_percent: Percentage of approvals
  - task_submitted_for_review_count: Count of tasks that have been submitted
  - task_outcome_approval_rate_percent: Percentage of submitted tasks that ended up approved/merged

Warnings:
  - Alerts if review verdict approval rate >95% (suspiciously high)
  - Alerts if task outcome approval rate >95% (suspiciously high)

The metrics are used to track sprint progress and detect quality issues.`,
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		if isJSON(cmd) {
			log.SetOutput(io.Discard)
			defer log.SetOutput(os.Stderr)
			defer func() {
				if retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
					_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
					retErr = jsonout.ErrAlreadyWritten
				}
			}()
		}

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		if isJSON(cmd) {
			metrics, err := ops.UpdateSprintMetrics(projectRoot)
			if err != nil {
				return err // deferred guard handles JSON
			}
			warnings := ops.CheckSuspiciousRates(metrics)
			return jsonout.WriteResult(os.Stdout, metrics, warnings, nil)
		}
		return commands.UpdateSprintMetricsCommand(projectRoot)
	},
}

var tuiCmd = &cobra.Command{
	Use:     "tui",
	Aliases: []string{"watch"},
	Short:   fmt.Sprintf("Interactive TUI dashboard for monitoring %s", brand.NameTitle),
	Long: fmt.Sprintf(`Launch an interactive TUI dashboard that monitors the %s blackboard.

The TUI provides:
  - Live dashboard with color-coded status indicators for agents and tasks
  - Keyboard commands to operate the system (spawn, pause, resume, add task, checkpoint, stop)
  - Inline anomaly monitoring with alerts in the activity feed
  - Reactive updates via fsnotify with 10s poll fallback

Use --headless for non-interactive monitoring (alerts to stderr + alerts.log).
This is suitable for CI, cron, or running in a secondary terminal.

Press '?' in the TUI for a full keybinding reference.`,
		brand.NameTitle),
	RunE: func(cmd *cobra.Command, args []string) error {
		headless, _ := cmd.Flags().GetBool("headless")

		if !headless && !interactive.IsInteractive() {
			headless = true
			fmt.Fprintf(cmd.ErrOrStderr(), "%s: stdin is not a terminal, falling back to headless mode\n", brand.BinaryName)
		}

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		if headless {
			interval, _ := cmd.Flags().GetInt("interval")
			if interval <= 0 {
				return fmt.Errorf("interval must be positive")
			}

			config := commands.WatchConfig{
				ProjectRoot:   projectRoot,
				CheckInterval: time.Duration(interval) * time.Second,
				StateCache:    make(map[string]time.Time),
			}

			return commands.WatchCommand(cmd.Context(), config)
		}

		model, err := tui.New(projectRoot)
		if err != nil {
			return fmt.Errorf("failed to initialize TUI: %w", err)
		}

		p := tea.NewProgram(model, tea.WithAltScreen())
		_, err = p.Run()
		return err
	},
}

var clearStaleReviewClaimsCmd = &cobra.Command{
	Use:   "clear-stale-review-claims",
	Short: "Clear expired review leases",
	Long: `Find and clear expired review leases on tasks in reviewing status.

When a Code Reviewer crashes mid-review, reviewing_by and review_lease_expires
remain set. This command clears expired claims so other reviewers can claim the task.

Typically called by:
  - Code Reviewer supervisor on startup
  - Periodically by cron or monitoring
  - TUI dashboard (though tui shouldn't mutate state by default)

Reports the number of claims cleared and logs each cleanup action.`,
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		if isJSON(cmd) {
			log.SetOutput(io.Discard)
			defer log.SetOutput(os.Stderr)
			defer func() {
				if retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
					_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
					retErr = jsonout.ErrAlreadyWritten
				}
			}()
		}

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		cleared, err := commands.ClearStaleReviewClaimsCommand(projectRoot)
		if err != nil {
			return err
		}

		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, map[string]int{"cleared": cleared}, nil, nil)
		}

		if cleared > 0 {
			fmt.Printf("Cleared %d stale review claim(s)\n", cleared)
		} else {
			fmt.Println("No stale review claims found")
		}
		return nil
	},
}

var pauseCmd = &cobra.Command{
	Use:   "pause",
	Short: fmt.Sprintf("Pause the %s system", brand.NameTitle),
	Long: fmt.Sprintf(`Pause the %s system by setting config.mode to PAUSED in state.yaml.

Agents will detect the PAUSED mode and block at their next check. They will
continue sending heartbeats but will not claim new tasks or make progress
until the system is resumed.

This is useful for:
- Making manual adjustments to state.yaml
- Investigating issues
- Coordinated maintenance

Use '%s' to continue normal operation.`, brand.NameTitle, brand.Command("resume")),
	RunE: func(cmd *cobra.Command, args []string) error {
		reason, _ := cmd.Flags().GetString("reason")
		changedBy := resolveChangedBy(cmd)

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		return commands.PauseCommand(projectRoot, reason, changedBy)
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: fmt.Sprintf("Stop the %s system", brand.NameTitle),
	Long: fmt.Sprintf(`Stop the %s system by setting config.mode to STOPPED in state.yaml.

Agents will detect the STOPPED mode and exit cleanly at their next check.
This provides a graceful shutdown of all agents.

This is different from pause:
- PAUSED: Agents block and wait for resume
- STOPPED: Agents exit (must be restarted manually)

Use this for:
- Ending a work session
- System maintenance requiring agent restart
- Shutting down before system updates`, brand.NameTitle),
	RunE: func(cmd *cobra.Command, args []string) error {
		reason, _ := cmd.Flags().GetString("reason")
		changedBy := resolveChangedBy(cmd)

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		return commands.StopCommand(projectRoot, reason, changedBy)
	},
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: fmt.Sprintf("Start the %s system from STOPPED mode", brand.NameTitle),
	Long: fmt.Sprintf(`Start the %s system by setting config.mode to RUNNING in state.yaml.

This command transitions from STOPPED back to RUNNING mode.
After starting, you must manually restart agent processes to resume work.

Difference from resume:
- RESUME: For PAUSED or CHECKPOINT states (agents still running)
- START: For STOPPED state (agents have exited)

Use this for:
- Beginning a new work session after stopping
- Restarting the system after maintenance
- Recovering from a graceful shutdown

After running this command, restart agents manually:
  %[2]s=coder-1 %[3]s &
  %[2]s=code-reviewer-1 %[4]s &`, brand.NameTitle, brand.EnvName("AGENT_ID"), brand.Command("agent", "coder"), brand.Command("agent", "code-reviewer")),
	RunE: func(cmd *cobra.Command, args []string) error {
		reason, _ := cmd.Flags().GetString("reason")
		changedBy := resolveChangedBy(cmd)

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		return commands.StartCommand(projectRoot, reason, changedBy)
	},
}

var proceedCmd = &cobra.Command{
	Use:   "proceed <task-id> <transition-name>",
	Short: "Execute a manual inter-pair transition on a task",
	Long: `Execute a manual transition between role pairs.

This command creates child tasks from a source task based on the transition's
cardinality. The source task must be at the transition's required status
and the preceding sprint must be COMPLETED.

Cardinality modes:
  per-subtask  Creates one child per entry in the source task's output[].
               Child IDs: <parent-id>-<transition-name>-<index>
  one-to-one   Creates a single child task. The parent task itself is the input.
               Child ID: <parent-id>-<transition-name>

Available transitions are defined in the frozen pipeline config (` + paths.ProjectDirName() + `/pipeline.yaml).
Use '` + brand.Command("status") + `' to see available transitions for tasks at terminal states.

After running proceed, use '` + brand.Command("resume") + `' to start a new sprint with
the child tasks.

Idempotent: repeated calls for the same transition are rejected.
Crash-safe: if interrupted mid-creation, re-running creates only
missing children.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := args[0]
		transitionName := args[1]

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		return commands.ProceedCommand(projectRoot, taskID, transitionName)
	},
}

var replanCmd = &cobra.Command{
	Use:   "replan [task-id]",
	Short: "Re-invoke planner after amending a plan file",
	Long: `Re-invoke a planner agent after amending a plan file at CHECKPOINT.

This command invalidates the old planning task's output and creates a new
planning task with the same role_pair and spec_ref. The sprint is set back
to IN_PROGRESS so the planner agent picks up the new task.

If task-id is omitted, the command auto-detects the single planning task
with unconsumed output in the current sprint. If multiple planning tasks
match, specify the task ID explicitly.

Preconditions:
  - Sprint must be at CHECKPOINT
  - Target task must be MERGED with output[]
  - No child tasks already created (TransitionsExecuted must be empty)
  - Task must belong to a planning role-pair

Example workflow:
  1. Planner produces output → sprint checkpoints at PLANNING_COMPLETE
  2. Human reviews and edits the plan markdown file
  3. ` + brand.Command("replan") + `                    # auto-detect planning task
  4. Planner agent claims new task, re-reads plan, regenerates output`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		changedBy := resolveChangedBy(cmd)
		var taskID string
		if len(args) > 0 {
			taskID = args[0]
		}

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		return commands.ReplanCommand(projectRoot, taskID, changedBy)
	},
}

var resumeCmd = &cobra.Command{
	Use:   "resume",
	Short: fmt.Sprintf("Resume the %s system from PAUSED mode, CHECKPOINT, or COMPLETED sprint", brand.NameTitle),
	Long: fmt.Sprintf(`Resume the %s system by setting config.mode to RUNNING and sprint.status to IN_PROGRESS.

This command can be used when:
- System is in PAUSED mode (sets mode to RUNNING)
- Sprint is at CHECKPOINT status (sets status to IN_PROGRESS)
- Both (resumes from both states)

Agents will detect the status changes and resume normal operation at their next check.

If the system is STOPPED, agents must be restarted manually - resume
cannot be used to restart stopped agents.`, brand.NameTitle),
	RunE: func(cmd *cobra.Command, args []string) error {
		changedBy := resolveChangedBy(cmd)

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		return commands.ResumeCommand(projectRoot, changedBy)
	},
}

var sprintCheckpointCmd = &cobra.Command{
	Use:     "sprint-checkpoint",
	Aliases: []string{"checkpoint"},
	Short:   "Create a checkpoint for the sprint",
	Long: `Create a checkpoint by setting sprint.status to CHECKPOINT in state.yaml.

This pauses all agents and generates a sprint summary report with:
- Current task status and distribution
- Sprint metrics and progress
- Active agents
- Anomalies and circuit breaker status

The sprint summary is written to ` + paths.ProjectDirName() + `/sprint_summary.md.

Agents will pause at their next check. After reviewing the summary,
use '` + brand.Command("resume") + `' to continue the sprint.

This is useful for:
- Sprint review meetings
- Progress assessment
- Decision points (continue vs pivot)
- Coordinated team synchronization`,
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		if isJSON(cmd) {
			log.SetOutput(io.Discard)
			defer log.SetOutput(os.Stderr)
			defer func() {
				if retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
					_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
					retErr = jsonout.ErrAlreadyWritten
				}
			}()
		}

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		if isJSON(cmd) {
			result, err := ops.SprintCheckpoint(projectRoot, "")
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.SprintCheckpointCommand(projectRoot)
	},
}

var getCmd = &cobra.Command{
	Use:   "get <query>",
	Short: "Query and get state data",
	Long: fmt.Sprintf(`Query and retrieve %s state data with flexible formatting.

Query Types:
  Field queries:
    config.mode                    - Get a specific field value
    sprint.status                  - Direct field access
    sprint.metrics.tasks_done      - Nested field access
    sprint.elapsed                 - Computed field (time since started)

  Entity queries:
    tasks                          - List all tasks
    tasks <task-id>                - Show specific task
    agents                         - List all agents
    agents <agent-id>              - Show specific agent
    metrics                        - Show sprint metrics
    anomalies                      - List all anomalies

  ID shorthand:
    <task-id>                      - Show specific task (any ID format, e.g., task-1, fix-auth-bug)
    <agent-id>                     - Show specific agent (e.g., coder-1, code-reviewer-1, orchestrator-1)

Formats:
  --format json       - JSON output
  --format yaml       - YAML output
  --format table      - Table format (for lists)
  --format value      - Key-value pairs (default for fields)

Examples:
  %[2]s config.mode
  %[2]s sprint.elapsed
  %[2]s tasks --format table
  %[2]s tasks --active --summary --json
  %[2]s tasks task-1 --output-summary --json
  %[2]s tasks task-1 --format json
  %[2]s task-1                  # Shorthand for tasks task-1
  %[2]s fix-auth-bug            # Shorthand for tasks fix-auth-bug (any task ID)
  %[2]s coder-1                 # Shorthand for agents coder-1
  %[2]s code-reviewer-1         # Shorthand for agents code-reviewer-1
  %[2]s agents --zombies        # Show live %[3]s agent processes missing from state
  %[2]s agents --format yaml
  %[2]s metrics
  %[2]s anomalies`, brand.NameTitle, brand.Command("get"), brand.BinaryName),
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		if isJSON(cmd) {
			log.SetOutput(io.Discard)
			defer log.SetOutput(os.Stderr)
			defer func() {
				if retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
					_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
					retErr = jsonout.ErrAlreadyWritten
				}
			}()
		}

		format, _ := cmd.Flags().GetString("format")
		summary, _ := cmd.Flags().GetBool("summary")
		active, _ := cmd.Flags().GetBool("active")
		zombies, _ := cmd.Flags().GetBool("zombies")
		outputSummary, _ := cmd.Flags().GetBool("output-summary")

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		if isJSON(cmd) {
			opts := commands.InspectOptions{
				Format:        "json",
				ProjectRoot:   projectRoot,
				Summary:       summary,
				OutputSummary: outputSummary,
				Active:        active,
				Zombies:       zombies,
			}
			resultStr, err := commands.InspectCommand(args, opts)
			if err != nil {
				return err // deferred guard handles JSON
			}
			var parsed any
			_ = json.Unmarshal([]byte(resultStr), &parsed)
			return jsonout.WriteResult(os.Stdout, parsed, nil, nil)
		}

		opts := commands.InspectOptions{
			Format:        format,
			ProjectRoot:   projectRoot,
			Summary:       summary,
			OutputSummary: outputSummary,
			Active:        active,
			Zombies:       zombies,
		}

		result, err := commands.InspectCommand(args, opts)
		if err != nil {
			return err
		}

		cmd.Println(result)
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show system and task status at a glance",
	Long: fmt.Sprintf(`Display a comprehensive overview of the %s system state including:
- Goal and sprint progress
- System mode (running, paused, stopped)
- Task distribution and availability
- Active agents and their health
- Orchestrator wake triggers
- Work queue status for each role

Formats:
  --format json       - JSON output
  --format yaml       - YAML output
  (default)           - Dashboard format

Use --detailed to include anomalies and circuit breaker status.

Examples:
  %[2]s
  %[2]s --format json
  %[2]s --format yaml
  %[2]s --detailed`, brand.NameTitle, brand.Command("status")),
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		if isJSON(cmd) {
			log.SetOutput(io.Discard)
			defer log.SetOutput(os.Stderr)
			defer func() {
				if retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
					_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
					retErr = jsonout.ErrAlreadyWritten
				}
			}()
		}

		format, _ := cmd.Flags().GetString("format")
		detailed, _ := cmd.Flags().GetBool("detailed")

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		if isJSON(cmd) {
			statePath := paths.New(projectRoot).StatePath()
			bb := db.For(statePath)
			state, err := bb.Read()
			if err != nil {
				return err // deferred guard handles JSON
			}
			pr, prErr := ops.LoadResolverForModels(projectRoot)
			var warnings []string
			if prErr != nil {
				warnings = append(warnings, fmt.Sprintf("failed to load pipeline resolver: %v", prErr))
			}
			data := commands.BuildStatusData(state, detailed, projectRoot, pr, warnings)
			return jsonout.WriteResult(os.Stdout, data, warnings, nil)
		}

		opts := commands.StatusOptions{
			Format:      format,
			Detailed:    detailed,
			ProjectRoot: projectRoot,
		}

		result, err := commands.StatusCommand(opts)
		if err != nil {
			return err
		}

		cmd.Println(result)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(analyzeCmd)
	rootCmd.AddCommand(updateSprintMetricsCmd)
	rootCmd.AddCommand(tuiCmd)
	rootCmd.AddCommand(clearStaleReviewClaimsCmd)
	rootCmd.AddCommand(pauseCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(proceedCmd)
	rootCmd.AddCommand(replanCmd)
	rootCmd.AddCommand(resumeCmd)
	rootCmd.AddCommand(sprintCheckpointCmd)
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(statusCmd)

	proceedCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		switch len(args) {
		case 0:
			return completeTaskIDs(cmd, args, toComplete)
		case 1:
			return completePipelineTransitions(cmd, args, toComplete)
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}
	replanCmd.ValidArgsFunction = completeTaskIDs
	getCmd.ValidArgsFunction = completeGetQueries

	addChangedByFlag(pauseCmd)
	addChangedByFlag(stopCmd)
	addChangedByFlag(startCmd)
	addChangedByFlag(replanCmd)
	addChangedByFlag(resumeCmd)

	// JSON output flags
	addJSONFlag(analyzeCmd)
	addJSONFlag(updateSprintMetricsCmd)
	addJSONFlag(clearStaleReviewClaimsCmd)
	addJSONFlag(sprintCheckpointCmd)
	addJSONFlag(getCmd)
	addJSONFlag(statusCmd)

	// Get command flags
	getCmd.Flags().String("format", "", "output format: json, yaml, table, value (default varies by query type)")
	getCmd.Flags().Bool("summary", false, "return compact task summaries")
	getCmd.Flags().Bool("output-summary", false, "return compact task output summaries")
	getCmd.Flags().Bool("active", false, "return only non-terminal tasks")
	getCmd.Flags().Bool("zombies", false, fmt.Sprintf("return live %s agent processes for this goal that are missing from state", brand.BinaryName))
	registerCompletion(getCmd, "format", completeValues("json", "yaml", "table", "value"))

	// Status command flags
	statusCmd.Flags().String("format", "", "output format: json, yaml, or dashboard (default)")
	statusCmd.Flags().Bool("detailed", false, "include anomalies and circuit breaker status")
	registerCompletion(statusCmd, "format", completeValues("json", "yaml", "dashboard"))

	// TUI command flags
	tuiCmd.Flags().Bool("headless", false, "run in headless mode (no TUI, alerts to stderr + alerts.log)")
	tuiCmd.Flags().Int("interval", 10, "check interval in seconds")

	// Pause command flags
	pauseCmd.Flags().String("reason", "", "reason for pausing the system")

	// Stop command flags
	stopCmd.Flags().String("reason", "", "reason for stopping the system")

	// Start command flags
	startCmd.Flags().String("reason", "", "optional reason for starting the system")
}
