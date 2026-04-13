package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/spf13/cobra"
)

var claimTaskCmd = &cobra.Command{
	Use:   "claim-task <task-id> <agent-id>",
	Short: "Claim a task for a doer agent",
	Long: `Claim a task for a doer agent using the three-phase claim pattern.

Supports claiming from multiple source states:
  - Initial state: normal new claim (e.g. DRAFT_CODE, DRAFT_CODING_PLAN, DRAFT_EPIC_PLAN, DRAFT_US)
  - Rejected state: re-claim (same doer preserves worktree, different doer gets fresh)
  - INTEGRATION_FAILED: any doer can claim (worktree preserved for conflict resolution)

Phase 1: Validate under lock (check status, deps, agent availability)
Phase 2: Handle worktree outside lock (create/preserve/delete as needed)
Phase 3: Re-validate and commit under lock (atomic state update)

This pattern prevents TOCTOU races in multi-agent scenarios.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		taskID := args[0]
		agentID := args[1]

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

		resolver, err := loadResolverForRBAC(projectRoot)
		if err != nil {
			return err
		}
		if err := validateRoleType(resolver, agentID, "doer"); err != nil {
			return err
		}

		if isJSON(cmd) {
			result, err := ops.ClaimTask(projectRoot, taskID, agentID)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.ClaimTaskCommand(projectRoot, taskID, agentID)
	},
}

var addTaskCmd = &cobra.Command{
	Use:   "add-task",
	Short: "Add a new task to the state",
	Long: `Add a new task to state.yaml with the specified properties.

Task details can be provided via CLI flags or loaded from a YAML file using --file.
When using --file, CLI flags can override specific fields from the file.

Updates sprint.scope.planned, goal.alignment_history, and logs the action.
Runs validation after adding the task.

Example YAML file format:
  id: task-1
  description: Implement feature X
  spec_ref: specs/vision.md
  done_when: Feature X is implemented and tested
  scope: Add feature X to the codebase
  priority: 1
  depends_on:
    - task-0`,
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

		statePath, _ := cmd.Flags().GetString("state")
		logPath, _ := cmd.Flags().GetString("log")

		if statePath == "" && logPath == "" {
			statePath = filepath.Join(paths.LizaDirName, paths.StateFileName)
			logPath = filepath.Join(paths.LizaDirName, paths.LogFileName)
		} else if statePath != "" && logPath == "" {
			return fmt.Errorf("if --state is provided, --log must also be provided")
		} else if statePath == "" && logPath != "" {
			return fmt.Errorf("if --log is provided, --state must also be provided")
		}

		filePath, _ := cmd.Flags().GetString("file")
		var input *commands.TaskInput

		if filePath != "" {
			var err error
			input, err = commands.LoadTaskInputFromFile(filePath)
			if err != nil {
				return err
			}
		} else {
			input = &commands.TaskInput{}
		}

		if cmd.Flags().Changed("id") {
			input.ID, _ = cmd.Flags().GetString("id")
		}
		if cmd.Flags().Changed("desc") {
			input.Description, _ = cmd.Flags().GetString("desc")
		}
		if cmd.Flags().Changed("spec") {
			specVal, _ := cmd.Flags().GetString("spec")
			absSpec, err := filepath.Abs(specVal)
			if err != nil {
				return fmt.Errorf("failed to resolve spec path: %w", err)
			}
			input.SpecRef = absSpec
		}
		if cmd.Flags().Changed("done") {
			input.DoneWhen, _ = cmd.Flags().GetString("done")
		}
		if cmd.Flags().Changed("scope") {
			input.Scope, _ = cmd.Flags().GetString("scope")
		}
		if cmd.Flags().Changed("priority") {
			input.Priority, _ = cmd.Flags().GetInt("priority")
		}
		if cmd.Flags().Changed("depends") {
			dependsStr, _ := cmd.Flags().GetString("depends")
			if dependsStr != "" {
				input.DependsOn = strings.Split(dependsStr, ",")
			} else {
				input.DependsOn = []string{}
			}
		}
		if cmd.Flags().Changed("type") {
			input.Type, _ = cmd.Flags().GetString("type")
		}

		if input.Priority == 0 {
			input.Priority = 1
		}

		orchestratorID, err := resolveOrchestratorID(cmd)
		if err != nil {
			return err
		}

		resolver, err := loadResolverFromDir(filepath.Dir(statePath))
		if err != nil {
			return err
		}
		if err := validateRoleType(resolver, orchestratorID, "orchestrator"); err != nil {
			return err
		}

		if isJSON(cmd) {
			opsInput := &ops.AddTaskInput{
				ID:          input.ID,
				Type:        input.Type,
				RolePair:    input.RolePair,
				Description: input.Description,
				SpecRef:     input.SpecRef,
				DoneWhen:    input.DoneWhen,
				Scope:       input.Scope,
				Priority:    input.Priority,
				DependsOn:   input.DependsOn,
			}
			result, err := ops.AddTask(statePath, logPath, opsInput, orchestratorID)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.AddTaskCommand(statePath, logPath, input, orchestratorID)
	},
}

var supersedeTaskCmd = &cobra.Command{
	Use:   "supersede-task <task-id> [replacement-task-ids] --reason <reason>",
	Short: "Mark a task as SUPERSEDED, optionally by replacement tasks",
	Long: `Mark a task as SUPERSEDED when it is replaced by new task(s) or completed externally.

Used by orchestrator when rescoping blocked, rejected, or problematic tasks,
or when a task's work was already completed outside the current sprint.

Requirements:
  - Task must be in BLOCKED, rejected, or initial status
  - --reason is always required

Replacement task IDs are optional and should be comma-separated.
When no replacements are given, the task's branch is deleted immediately.

Examples:
  liza supersede-task task-3 task-4,task-5 --reason "Split into smaller tasks"
  liza supersede-task task-3 --reason "Work already merged in prior sprint"`,
	Args: cobra.RangeArgs(1, 2),
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

		taskID := args[0]

		reason, _ := cmd.Flags().GetString("reason")

		var replacementIDs []string
		if len(args) == 2 {
			for _, id := range strings.Split(args[1], ",") {
				replacementIDs = append(replacementIDs, strings.TrimSpace(id))
			}
		}

		agentID, err := resolveOrchestratorID(cmd)
		if err != nil {
			return err
		}

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		resolver, err := loadResolverForRBAC(projectRoot)
		if err != nil {
			return err
		}
		if err := validateAllowedOperation(resolver, agentID, "supersede-task"); err != nil {
			return err
		}

		if isJSON(cmd) {
			result, err := ops.SupersedeTask(projectRoot, taskID, replacementIDs, reason, agentID)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.SupersedeTaskCommand(projectRoot, taskID, replacementIDs, reason, agentID)
	},
}

var markBlockedCmd = &cobra.Command{
	Use:   "mark-blocked <task-id>",
	Short: "Mark a task as BLOCKED due to unresolvable blocker",
	Long: `Mark a task as BLOCKED when work cannot proceed.

Per the blocking protocol (specs/architecture/roles.md), use this when:
  - Spec ambiguity prevents implementation
  - Missing external dependency blocks progress
  - Design conflict discovered that requires rescoping

Requirements:
  - Agent ID must be provided (via --agent-id flag or LIZA_AGENT_ID env var)
  - Task must be in an executing status (e.g. IMPLEMENTING_CODE, CODE_PLANNING)
  - Only the assigned agent can mark a task as blocked
  - Requires a reason and 1-3 clarifying questions

Effects:
  - status = BLOCKED
  - blocked_reason = <reason>
  - blocked_questions = [<questions>]
  - Clear assigned_to
  - Clear lease_expires
  - Add history entry with event "blocked"
  - Triggers orchestrator wake`,
	Args: cobra.ExactArgs(1),
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

		taskID := args[0]

		reason, _ := cmd.Flags().GetString("reason")
		questions, _ := cmd.Flags().GetStringSlice("questions")

		agentID, err := requireAgentID(cmd)
		if err != nil {
			return err
		}

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		resolver, err := loadResolverForRBAC(projectRoot)
		if err != nil {
			return err
		}
		if err := validateAllowedOperation(resolver, agentID, "mark-blocked"); err != nil {
			return err
		}

		if isJSON(cmd) {
			result, err := ops.MarkBlocked(projectRoot, taskID, reason, questions, agentID)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.MarkBlockedCommand(projectRoot, taskID, reason, questions, agentID)
	},
}

var assessBlockedCmd = &cobra.Command{
	Use:   "assess-blocked <task-id>",
	Short: "Record orchestrator assessment of a BLOCKED task",
	Long: `Record that the orchestrator has assessed a BLOCKED task.

This prevents the orchestrator re-wake loop where blocked tasks that have
already been triaged continue to trigger new orchestrator sessions.

After assessing, the task remains BLOCKED but won't trigger further wakes
unless new activity occurs (dependency changes, human notes, etc.).

Requirements:
  - Agent ID must be provided (via --agent-id flag)
  - Task must be in BLOCKED status`,
	Args: cobra.ExactArgs(1),
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

		taskID := args[0]

		note, _ := cmd.Flags().GetString("note")

		agentID, err := resolveOrchestratorID(cmd)
		if err != nil {
			return err
		}

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		resolver, err := loadResolverForRBAC(projectRoot)
		if err != nil {
			return err
		}
		if err := validateRoleType(resolver, agentID, "orchestrator"); err != nil {
			return err
		}

		if isJSON(cmd) {
			result, err := ops.AssessBlocked(projectRoot, taskID, note, agentID)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.AssessBlockedCommand(projectRoot, taskID, note, agentID)
	},
}

var assessHypothesisExhaustedCmd = &cobra.Command{
	Use:   "assess-hypothesis-exhausted <task-id>",
	Short: "Record orchestrator assessment of a hypothesis-exhausted task",
	Long: `Record that the orchestrator has assessed a hypothesis-exhausted task
(2+ coders failed on it).

This prevents the orchestrator re-wake loop where hypothesis-exhausted tasks
that have already been triaged continue to trigger new orchestrator sessions.

After assessing, the task keeps its current status but won't trigger further
wakes unless new activity occurs (new failures, human notes, etc.).

Requirements:
  - Agent ID must be provided (via --agent-id flag)
  - Task must have 2+ entries in failed_by and not be in terminal status`,
	Args: cobra.ExactArgs(1),
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

		taskID := args[0]

		note, _ := cmd.Flags().GetString("note")

		agentID, err := resolveOrchestratorID(cmd)
		if err != nil {
			return err
		}

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		resolver, err := loadResolverForRBAC(projectRoot)
		if err != nil {
			return err
		}
		if err := validateRoleType(resolver, agentID, "orchestrator"); err != nil {
			return err
		}

		if isJSON(cmd) {
			result, err := ops.AssessHypothesisExhausted(projectRoot, taskID, note, agentID)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.AssessHypothesisExhaustedCommand(projectRoot, taskID, note, agentID)
	},
}

var cancelTaskCmd = &cobra.Command{
	Use:   "cancel-task <task-id> <reason>",
	Short: "Cancel a task (transition to ABANDONED)",
	Long: `Cancel a task by transitioning it to ABANDONED status with a reason.

Unlike delete-task (removes from state) or supersede-task (marks as replaced/completed externally),
cancel-task simply stops the task while preserving full audit trail.

Cancellable states are determined by the pipeline transition map. Generally:
  - Initial states: DRAFT_CODE, DRAFT_CODING_PLAN, DRAFT_EPIC_PLAN, DRAFT_US
  - Rejected states: CODE_REJECTED, CODING_PLAN_REJECTED, etc.
  - BLOCKED, INTEGRATION_FAILED

Not cancellable: executing, submitted, reviewing, approved, or terminal states.

Example:
  liza cancel-task task-3 "Requirements no longer valid"`,
	Args: cobra.ExactArgs(2),
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

		taskID := args[0]
		reason := args[1]

		agentID, err := resolveOrchestratorID(cmd)
		if err != nil {
			return err
		}

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		resolver, err := loadResolverForRBAC(projectRoot)
		if err != nil {
			return err
		}
		if err := validateAllowedOperation(resolver, agentID, "cancel-task"); err != nil {
			return err
		}

		if isJSON(cmd) {
			result, err := ops.CancelTask(projectRoot, taskID, reason, agentID)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.CancelTaskCommand(projectRoot, taskID, reason, agentID)
	},
}

var deleteTaskCmd = &cobra.Command{
	Use:   "task <task-id>",
	Short: "Delete a task from the state database",
	Long: `Remove a task from the state database.

Useful for removing tasks that were created but are no longer needed. Tasks
in MERGED state cannot be deleted by default (as they represent integrated work).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID := args[0]
		force, _ := cmd.Flags().GetBool("force")
		deleteWorktree, _ := cmd.Flags().GetBool("delete-worktree")
		reason, _ := cmd.Flags().GetString("reason")
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		return commands.DeleteTaskCommand(projectRoot, taskID, force, deleteWorktree, reason, os.Stdin)
	},
}

var writeCheckpointCmd = &cobra.Command{
	Use:   "write-checkpoint <task-id>",
	Short: "Write pre-execution checkpoint before submitting for review",
	Long: `Record implementation intent, validation plan, and scope before submission.

Requirements:
  - Agent ID must be provided (via --agent-id flag or LIZA_AGENT_ID env var)
  - Task must be in an executing status (resolved from pipeline config)
  - Task must be assigned to the submitting agent

Updates:
  - Appends pre_execution_checkpoint event to task history
  - Does not change task status`,
	Args: cobra.ExactArgs(1),
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

		taskID := args[0]

		agentID, err := requireAgentID(cmd)
		if err != nil {
			return err
		}

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		resolver, err := loadResolverForRBAC(projectRoot)
		if err != nil {
			return err
		}
		if err := validateAllowedOperation(resolver, agentID, "write-checkpoint"); err != nil {
			return err
		}

		intent, _ := cmd.Flags().GetString("intent")
		validationPlan, _ := cmd.Flags().GetString("validation-plan")
		filesToModify, _ := cmd.Flags().GetStringSlice("files-to-modify")
		assumptions, _ := cmd.Flags().GetStringSlice("assumptions")
		risks, _ := cmd.Flags().GetString("risks")
		tddNotRequired, _ := cmd.Flags().GetString("tdd-not-required")
		impact, _ := cmd.Flags().GetString("impact")

		input := &ops.WriteCheckpointInput{
			TaskID:         taskID,
			AgentID:        agentID,
			Intent:         intent,
			ValidationPlan: validationPlan,
			FilesToModify:  filesToModify,
			Assumptions:    assumptions,
			Risks:          risks,
			TDDNotRequired: tddNotRequired,
			Impact:         impact,
		}

		// Parse scope-extensions from JSON if provided
		if scopeJSON, _ := cmd.Flags().GetString("scope-extensions"); scopeJSON != "" {
			var entries []ops.ScopeExtensionEntry
			if err := json.Unmarshal([]byte(scopeJSON), &entries); err != nil {
				return &ops.PreconditionError{Reason: fmt.Sprintf(
					"--scope-extensions must be a JSON array: %v", err)}
			}
			input.ScopeExtensions = entries
		}

		if isJSON(cmd) {
			err := ops.WriteCheckpoint(projectRoot, input)
			return jsonout.WriteResult(os.Stdout, nil, nil, err)
		}
		return commands.WriteCheckpointCommand(projectRoot, input)
	},
}

var setTaskOutputCmd = &cobra.Command{
	Use:   "set-task-output <task-id> --output <path>",
	Short: "Set output entries for downstream task generation",
	Long: `Define output entries that will become downstream tasks after merge.

Reads output entries from a JSON file. Each entry must have desc, done_when,
and scope. Optional fields: spec_ref, plan_ref, arch_ref, depends_on.

Requirements:
  - Agent ID must be provided (via --agent-id flag or LIZA_AGENT_ID env var)
  - Task must be in an executing status
  - Task must be assigned to the submitting agent
  - At least one output entry required

Updates:
  - Sets task.output to provided entries (overwrites existing, idempotent)

Example:
  cat > outputs.json <<'EOF'
  [
    {"desc": "Subtask 1", "done_when": "Tests pass", "scope": "internal/pkg"},
    {"desc": "Subtask 2", "done_when": "API works", "scope": "internal/api", "depends_on": ["0"]}
  ]
  EOF
  liza set-task-output task-1 --output outputs.json`,
	Args: cobra.ExactArgs(1),
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

		taskID := args[0]

		agentID, err := requireAgentID(cmd)
		if err != nil {
			return err
		}

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		resolver, err := loadResolverForRBAC(projectRoot)
		if err != nil {
			return err
		}
		if err := validateAllowedOperation(resolver, agentID, "set-task-output"); err != nil {
			return err
		}

		outputFile, _ := cmd.Flags().GetString("output")
		if outputFile == "" {
			return fmt.Errorf("--output is required")
		}

		data, err := os.ReadFile(outputFile)
		if err != nil {
			return fmt.Errorf("reading output file: %w", err)
		}

		var entries []models.OutputEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			hint := "output file must contain a JSON array [...]"
			if len(data) > 0 && data[0] == '{' {
				hint += "; got a JSON object — remove the wrapper and pass a bare array"
			}
			return &ops.PreconditionError{Reason: fmt.Sprintf("%s: %v", hint, err)}
		}

		input := &ops.SetTaskOutputInput{
			TaskID:  taskID,
			AgentID: agentID,
			Output:  entries,
		}

		if isJSON(cmd) {
			err := ops.SetTaskOutput(projectRoot, input)
			return jsonout.WriteResult(os.Stdout, nil, nil, err)
		}
		return commands.SetTaskOutputCommand(projectRoot, input)
	},
}

var addTasksCmd = &cobra.Command{
	Use:   "add-tasks --tasks-file <path>",
	Short: "Add multiple tasks in batch from a JSON file",
	Long: `Add multiple tasks to state.yaml in a single batch operation.

Reads task definitions from a JSON file. Each task must have id, desc, spec,
done, and scope. Optional fields: priority, depends, type, role_pair, plan_ref.

Tasks are added independently; failed tasks don't block subsequent ones.

Example:
  cat > tasks.json <<'EOF'
  [
    {"id": "task-1", "desc": "Implement X", "spec": "specs/x.md", "done": "X works", "scope": "internal/x"},
    {"id": "task-2", "desc": "Implement Y", "spec": "specs/y.md", "done": "Y works", "scope": "internal/y", "depends": ["task-1"]}
  ]
  EOF
  liza add-tasks --tasks-file tasks.json`,
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

		filePath, _ := cmd.Flags().GetString("tasks-file")
		if filePath == "" {
			return fmt.Errorf("--tasks-file is required")
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("reading tasks file: %w", err)
		}

		var tasks []ops.AddTaskInput
		if err := json.Unmarshal(data, &tasks); err != nil {
			hint := "tasks file must contain a JSON array [...]"
			if len(data) > 0 && data[0] == '{' {
				hint += "; got a JSON object — remove the wrapper and pass a bare array"
			}
			return &ops.PreconditionError{Reason: fmt.Sprintf("%s: %v", hint, err)}
		}

		orchestratorID, err := resolveOrchestratorID(cmd)
		if err != nil {
			return err
		}

		statePath := filepath.Join(paths.LizaDirName, paths.StateFileName)
		logPath := filepath.Join(paths.LizaDirName, paths.LogFileName)

		resolver, err := loadResolverFromDir(filepath.Dir(statePath))
		if err != nil {
			return err
		}
		if err := validateAllowedOperation(resolver, orchestratorID, "add-tasks"); err != nil {
			return err
		}

		input := &ops.AddTasksInput{
			Tasks:          tasks,
			OrchestratorID: orchestratorID,
		}

		if isJSON(cmd) {
			result, err := ops.AddTasks(statePath, logPath, input)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.AddTasksCommand(statePath, logPath, input)
	},
}

var setDiscoveryDispositionCmd = &cobra.Command{
	Use:   "set-discovery-disposition <discovery-id> <disposition>",
	Short: "Set the disposition of a discovered item",
	Long: `Set how a discovered item should be handled.

Disposition values:
  - A task ID (e.g. "task-5"): converts the discovery into that task
  - "deferred": defer for later consideration
  - "dismissed": dismiss the discovery`,
	Args: cobra.ExactArgs(2),
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

		discoveryID := args[0]
		disposition := args[1]

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		if isJSON(cmd) {
			err := ops.SetDiscoveryDisposition(projectRoot, discoveryID, disposition)
			return jsonout.WriteResult(os.Stdout, nil, nil, err)
		}
		return commands.SetDiscoveryDispositionCommand(projectRoot, discoveryID, disposition)
	},
}

func init() {
	rootCmd.AddCommand(claimTaskCmd)
	rootCmd.AddCommand(addTaskCmd)
	rootCmd.AddCommand(addTasksCmd)
	rootCmd.AddCommand(supersedeTaskCmd)
	rootCmd.AddCommand(cancelTaskCmd)
	rootCmd.AddCommand(markBlockedCmd)
	rootCmd.AddCommand(assessBlockedCmd)
	rootCmd.AddCommand(assessHypothesisExhaustedCmd)
	rootCmd.AddCommand(writeCheckpointCmd)
	rootCmd.AddCommand(setTaskOutputCmd)
	rootCmd.AddCommand(setDiscoveryDispositionCmd)
	deleteCmd.AddCommand(deleteTaskCmd)

	addJSONFlag(claimTaskCmd)
	addJSONFlag(addTaskCmd)
	addJSONFlag(addTasksCmd)
	addJSONFlag(supersedeTaskCmd)
	addJSONFlag(cancelTaskCmd)
	addJSONFlag(markBlockedCmd)
	addJSONFlag(assessBlockedCmd)
	addJSONFlag(assessHypothesisExhaustedCmd)
	addJSONFlag(writeCheckpointCmd)
	addJSONFlag(setTaskOutputCmd)
	addJSONFlag(setDiscoveryDispositionCmd)

	addAgentIDFlag(addTaskCmd)
	addAgentIDFlag(supersedeTaskCmd)
	supersedeTaskCmd.Flags().String("reason", "", "reason for superseding (required)")
	supersedeTaskCmd.MarkFlagRequired("reason")
	addAgentIDFlag(cancelTaskCmd)

	// Mark-blocked command flags
	markBlockedCmd.Flags().String("reason", "", "reason why the task is blocked (required)")
	markBlockedCmd.Flags().StringSlice("questions", nil, "clarifying questions (1-3 required)")
	markBlockedCmd.Flags().String("agent-id", "", "agent ID marking the task as blocked")
	markBlockedCmd.MarkFlagRequired("reason")
	markBlockedCmd.MarkFlagRequired("questions")

	// Assess-blocked command flags
	assessBlockedCmd.Flags().String("agent-id", "", "orchestrator agent ID (auto-resolved if not provided)")
	assessBlockedCmd.Flags().String("note", "", "optional note about the assessment outcome")

	// Assess-hypothesis-exhausted command flags
	assessHypothesisExhaustedCmd.Flags().String("agent-id", "", "orchestrator agent ID (auto-resolved if not provided)")
	assessHypothesisExhaustedCmd.Flags().String("note", "", "optional note about the assessment outcome")

	// Add-task command flags
	addTaskCmd.Flags().String("file", "", "path to YAML file containing task details")
	addTaskCmd.Flags().String("id", "", "task ID (required unless using --file)")
	addTaskCmd.Flags().String("desc", "", "task description (required unless using --file)")
	addTaskCmd.Flags().String("spec", "", "spec reference (required unless using --file)")
	addTaskCmd.Flags().String("done", "", "done-when criteria (required unless using --file)")
	addTaskCmd.Flags().String("scope", "", "task scope (required unless using --file)")
	addTaskCmd.Flags().Int("priority", 0, "task priority (default: 1, overrides file value)")
	addTaskCmd.Flags().String("depends", "", "comma-separated list of task IDs this task depends on (overrides file value)")
	addTaskCmd.Flags().String("type", "", "task type determining role workflow (default: coding)")
	addTaskCmd.Flags().String("state", "", "path to state.yaml (default: .liza/state.yaml)")
	addTaskCmd.Flags().String("log", "", "path to log.yaml (default: .liza/log.yaml)")

	// Add-tasks (batch) command flags
	addAgentIDFlag(addTasksCmd)
	addTasksCmd.Flags().String("tasks-file", "", "path to JSON file with task definitions array (required)")
	addTasksCmd.MarkFlagRequired("tasks-file")

	// Write-checkpoint command flags
	addAgentIDFlag(writeCheckpointCmd)
	writeCheckpointCmd.Flags().String("intent", "", "specific, observable intent of implementation (required)")
	writeCheckpointCmd.Flags().String("validation-plan", "", "concrete validation command and expected output (required)")
	writeCheckpointCmd.Flags().StringSlice("files-to-modify", nil, "files that will be modified (omit for read-only analysis tasks)")
	writeCheckpointCmd.Flags().StringSlice("assumptions", nil, "tagged assumptions")
	writeCheckpointCmd.Flags().String("risks", "", "identified risks")
	writeCheckpointCmd.Flags().String("tdd-not-required", "", "justification for skipping new test files")
	writeCheckpointCmd.Flags().String("impact", "", "impact classification (standard, significant, architecture)")
	writeCheckpointCmd.Flags().String("scope-extensions", "", `scope extensions as JSON array, e.g. [{"file":"path","justification":"why"}]`)
	writeCheckpointCmd.MarkFlagRequired("intent")
	writeCheckpointCmd.MarkFlagRequired("validation-plan")

	// Set-task-output command flags
	addAgentIDFlag(setTaskOutputCmd)
	setTaskOutputCmd.Flags().String("output", "", "path to JSON file with output entries array (required)")
	setTaskOutputCmd.MarkFlagRequired("output")

	// Delete task command flags
	deleteTaskCmd.Flags().Bool("force", false, "force deletion even if task has dependencies or is in restricted state")
	deleteTaskCmd.Flags().Bool("delete-worktree", false, "also delete the associated git worktree and branch")
	deleteTaskCmd.Flags().String("reason", "manual deletion", "reason for deleting the task")
}
