package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/spf13/cobra"
)

var inspectCommand = commands.InspectCommand

var getCmd = &cobra.Command{
	Use:   "get <query>",
	Short: "Query and get state data",
	Long: fmt.Sprintf(`Query and retrieve %s state data with flexible formatting.

For runtime setup configuration, also use %[3]s config get config.post_worktree_cmd
or %[3]s config set config.post_worktree_cmd "<command>".

Query Types:
  Field queries:
    config.mode                    - Get a specific field value
    sprint.status                  - Direct field access
    sprint.metrics.tasks_done      - Nested field access
    sprint.elapsed                 - Computed field (time since started)
    tasks.<task-id>.history         - Task history
    <task-id>.rejection_reason      - Task rejection text (null when absent)

  Entity queries:
    tasks                          - List active work and unfinished handoffs (--all for history)
    tasks <task-id>                - Show specific task
    tasks.<task-id>                - Same task using dotted syntax
    agents                         - List all agents
    agents <agent-id>              - Show specific agent
    metrics                        - Show sprint metrics
    anomalies                      - List all anomalies
    human_notes                    - Read operator notes and their targets
    quarantined_verdicts            - List fenced review evidence and reconciliation history

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
  %[2]s tasks --all --format json
  %[2]s tasks --active --summary --json
  %[2]s tasks task-1 --output-summary --json
  %[2]s tasks task-1 --format json
  %[2]s tasks --field id,rejection_reason,rejection_rca --json
  %[2]s tasks.task-1.history --json
  %[2]s task-1                  # Shorthand for tasks task-1
  %[2]s fix-auth-bug            # Shorthand for tasks fix-auth-bug (any task ID)
  %[2]s coder-1                 # Shorthand for agents coder-1
  %[2]s code-reviewer-1         # Shorthand for agents code-reviewer-1
  %[2]s agents --zombies        # Show live %[3]s agent processes missing from state
  %[2]s agents --format yaml
  %[2]s metrics
  %[2]s human_notes --json
  %[2]s anomalies

Inspection reads a complete published snapshot without acquiring the state lock.
Repeat --field or separate fields with commas to project task fields; projections
support JSON, YAML and value formats, but not table/summary/output-summary.
Reserved state/computed queries take precedence. Within task queries, exact IDs
precede the longest matching ID prefix. Use tasks <literal-id> for dotted IDs.
Unknown fields, array/map traversal and surplus positional arguments are errors.

JSON/YAML task and --summary views add computed waiting_on to a non-terminal
task its dependencies hold: the claim predicate's unmet dependencies (depends_on
and provider prerequisites) and the chain heads they lead to (id, status,
role-level claimable, reason). Heads are diagnosis, not await targets. Absence
means no dependency hold, not claimability; without a workspace pipeline it is
omitted as unavailable.`, brand.NameTitle, brand.Command("get"), brand.BinaryName),
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
		all, _ := cmd.Flags().GetBool("all")
		if all && active {
			return &ops.PreconditionError{Reason: "--all and --active are mutually exclusive"}
		}
		taskList := len(args) == 1 && args[0] == "tasks"
		if all && !taskList {
			return &ops.PreconditionError{Reason: "--all is only supported for task listings"}
		}
		if taskList && !all {
			active = true
		}
		zombies, _ := cmd.Flags().GetBool("zombies")
		outputSummary, _ := cmd.Flags().GetBool("output-summary")
		fields, _ := cmd.Flags().GetStringArray("field")
		if cmd.Flags().Changed("field") && len(fields) == 0 {
			// pflag decodes --field= as an empty slice; retain the explicit
			// empty request so inspection validation can reject it.
			fields = []string{""}
		}

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		if isJSON(cmd) {
			var warnBuf bytes.Buffer
			opts := commands.InspectOptions{
				Format:        "json",
				ProjectRoot:   projectRoot,
				Summary:       summary,
				OutputSummary: outputSummary,
				Fields:        fields,
				Active:        active,
				Zombies:       zombies,
				WarnWriter:    &warnBuf,
			}
			resultStr, err := inspectCommand(args, opts)
			if err != nil {
				return err // deferred guard handles JSON
			}
			var parsed any
			_ = json.Unmarshal([]byte(resultStr), &parsed)
			return jsonout.WriteResult(os.Stdout, parsed, warningLines(warnBuf.String()), nil)
		}

		opts := commands.InspectOptions{
			Format:        format,
			ProjectRoot:   projectRoot,
			Summary:       summary,
			OutputSummary: outputSummary,
			Fields:        fields,
			Active:        active,
			Zombies:       zombies,
			WarnWriter:    cmd.ErrOrStderr(),
		}

		result, err := inspectCommand(args, opts)
		if err != nil {
			return err
		}

		cmd.Println(result)
		return nil
	},
}

var getTasksCmd = &cobra.Command{
	Use:   "get-tasks [task-id]",
	Short: "Query tasks with optional field projection",
	Long: fmt.Sprintf(`Query tasks using the same snapshot and formatting as %s.

Examples:
  %s --field id,rejection_reason,rejection_rca --json
  %s task-1 --field history --format yaml

Repeat --field or separate fields with commas. Only requested keys are returned;
absent optional values are null. See %s --help for query details.`,
		brand.Command("get", "tasks"), brand.Command("get-tasks"),
		brand.Command("get-tasks"), brand.Command("get")),
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getCmd.RunE(cmd, append([]string{"tasks"}, args...))
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
			state, err := bb.ReadSnapshot()
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
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(getTasksCmd)
	rootCmd.AddCommand(statusCmd)

	getCmd.ValidArgsFunction = completeGetQueries
	getTasksCmd.ValidArgsFunction = completeTaskIDArgs(1)

	addJSONFlag(statusCmd)

	// Both task entry points share execution and projection semantics.
	for _, cmd := range []*cobra.Command{getCmd, getTasksCmd} {
		addJSONFlag(cmd)
		cmd.Flags().String("format", "", "output format: json, yaml, table, value (default varies by query type)")
		cmd.Flags().Bool("summary", false, "return compact task summaries")
		cmd.Flags().Bool("output-summary", false, "return compact task output summaries")
		cmd.Flags().Bool("active", false, "return active work and unfinished handoffs")
		cmd.Flags().Bool("all", false, "include terminal task history (task listings default to active work)")
		cmd.Flags().StringArray("field", nil, "project task fields (repeat or comma-separate; e.g. id,rejection_reason,rejection_rca)")
		registerCompletion(cmd, "format", completeValues("json", "yaml", "table", "value"))
	}
	getCmd.Flags().Bool("zombies", false, fmt.Sprintf("return live %s agent processes for this goal that are missing from state", brand.BinaryName))

	// Status command flags
	statusCmd.Flags().String("format", "", "output format: json, yaml, or dashboard (default)")
	statusCmd.Flags().Bool("detailed", false, "include anomalies and circuit breaker status")
	registerCompletion(statusCmd, "format", completeValues("json", "yaml", "dashboard"))
}
