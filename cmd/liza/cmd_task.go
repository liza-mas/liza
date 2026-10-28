package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/spf13/cobra"
)

// defaultTaskPriority is the priority an added task gets when neither the task
// file nor --priority sets one; the --priority help text states it.
const defaultTaskPriority = 1

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

		invocation := beginLifecycleCLI(cmd, args)
		defer invocation.finish(&retErr)
		requestOpts, err := lifecycleRequestOptions(cmd)
		if err != nil {
			return err
		}

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		authority, err := requireAgentAuthorityForID(cmd, agentID)
		if err != nil {
			return err
		}

		resolver, err := loadResolverForRBAC(projectRoot)
		if err != nil {
			return err
		}
		if err := validateRoleType(resolver, agentID, roles.RoleTypeDoer); err != nil {
			return err
		}

		invocation.calledOps = true
		if isJSON(cmd) {
			result, err := ops.ClaimTaskWithRequest(projectRoot, taskID, authority.ID, &authority, requestOpts)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.ClaimTaskWithAuthorityAndOptionsCommand(projectRoot, taskID, authority, requestOpts)
	},
}

var addTaskCmd = &cobra.Command{
	Use:   "add-task",
	Short: "Add a new task to the state",
	Long: `Add a new task to state.yaml with the specified properties.

Task details can be provided via CLI flags or loaded from a YAML file using --file.
When using --file, CLI flags can override specific fields from the file.
provider_dependencies is a YAML-file field for selected provider outputs,
including unborn children. It requires an existing provider and its configured
per-subtask transition; claim waits for that provider and selected children
MERGED plus the executed transition. Ordinary downstream depends_on stays invalid.

Updates sprint.scope.planned, goal.alignment_history, and logs the action.
Validates the added task and reports a warning if unrelated existing state
corruption keeps full-state validation degraded after the add.

For explicitly commissioned corrective planning work, supply planning_change
with kind (correction or replan), trigger, and original_task_id. CLI overrides
are --planning-change-kind, --trigger, and --planning-change-original.

Example YAML file format:
  id: task-1
  description: Implement feature X
  spec_ref: specs/vision.md
  done_when: Feature X is implemented and tested
  validation:
    - make test
  destructive_db: false
  scope: Add feature X to the codebase
  role_pair: coding-pair
  priority: 1
  depends_on:
    - task-0
  provider_dependencies:
    - provider_task: provider-architecture
      transition: architecture-to-code-plan
      outputs: [0, 2]`,
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
			statePath = filepath.Join(paths.ProjectDirName(), paths.StateFileName)
			logPath = filepath.Join(paths.ProjectDirName(), paths.LogFileName)
		} else if statePath != "" && logPath == "" {
			return cliValidationError("if --state is provided, --log must also be provided")
		} else if statePath == "" && logPath != "" {
			return cliValidationError("if --log is provided, --state must also be provided")
		}

		filePath, _ := cmd.Flags().GetString("file")
		var input *commands.TaskInput

		if filePath != "" {
			var err error
			input, err = commands.LoadTaskInputFromFile(filePath)
			if err != nil {
				return cliValidationError(err.Error())
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
				return cliValidationWrap("failed to resolve spec path", err)
			}
			input.SpecRef = absSpec
		}
		if cmd.Flags().Changed("done") {
			input.DoneWhen, _ = cmd.Flags().GetString("done")
		}
		if cmd.Flags().Changed("validation") {
			input.Validation, _ = cmd.Flags().GetStringArray("validation")
		}
		if cmd.Flags().Changed("destructive-db") {
			input.DestructiveDB, _ = cmd.Flags().GetBool("destructive-db")
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
		if cmd.Flags().Changed("role-pair") {
			input.RolePair, _ = cmd.Flags().GetString("role-pair")
		}
		if cmd.Flags().Changed("planning-change-kind") || cmd.Flags().Changed("planning-change-original") || cmd.Flags().Changed("trigger") {
			if input.PlanningChange == nil {
				input.PlanningChange = &models.PlanningChange{}
			}
			if cmd.Flags().Changed("planning-change-kind") {
				input.PlanningChange.Kind, _ = cmd.Flags().GetString("planning-change-kind")
			}
			if cmd.Flags().Changed("planning-change-original") {
				input.PlanningChange.OriginalTaskID, _ = cmd.Flags().GetString("planning-change-original")
			}
			if cmd.Flags().Changed("trigger") {
				input.PlanningChange.Trigger, _ = cmd.Flags().GetString("trigger")
			}
		}

		if input.Priority == 0 {
			input.Priority = defaultTaskPriority
		}

		authority, err := resolveOrchestratorAuthority(cmd)
		if err != nil {
			return err
		}

		resolver, err := loadResolverFromDir(filepath.Dir(statePath))
		if err != nil {
			return err
		}
		if err := validateRoleType(resolver, authority.ID, roles.RoleTypeOrchestrator); err != nil {
			return err
		}

		if isJSON(cmd) {
			opsInput := &ops.AddTaskInput{
				PlanningChange:          input.PlanningChange,
				ID:                      input.ID,
				Type:                    input.Type,
				RolePair:                input.RolePair,
				Description:             input.Description,
				SpecRef:                 input.SpecRef,
				DoneWhen:                input.DoneWhen,
				Validation:              input.Validation,
				ValidationPrerequisites: models.CloneValidationPrerequisites(input.ValidationPrerequisites),
				RuntimeInputs:           models.CloneRuntimeInputs(input.RuntimeInputs),
				ProviderDependencies:    models.CloneProviderDependencies(input.ProviderDependencies),
				DestructiveDB:           input.DestructiveDB,
				Scope:                   input.Scope,
				Priority:                input.Priority,
				DependsOn:               input.DependsOn,
				ReserveSuccessors:       slices.Clone(input.ReserveSuccessors),
				MaxOutputs:              input.MaxOutputs,
			}
			result, err := ops.AddTaskWithAuthority(statePath, logPath, opsInput, authority)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.AddTaskWithAuthorityCommand(statePath, logPath, input, authority)
	},
}

func resultWarnings(result interface{ GetWarnings() []string }) []string {
	if result == nil {
		return nil
	}
	return result.GetWarnings()
}

// operatorInvocation reports whether an orchestrator-or-operator command runs
// from an operator shell: no agent identity in the environment or flags. The
// operator holds no registration, so no generation fences its writes.
func operatorInvocation(cmd *cobra.Command) bool {
	return brand.LookupEnv(os.Getenv, "AGENT_ID").Value == "" && !cmd.Flags().Changed("agent-id")
}

// operatorInvocationHelp closes the help of every command that takes
// operatorInvocation's path.
var operatorInvocationHelp = `Without an agent identity (no ` + brand.EnvName("AGENT_ID") + `, no --agent-id) it runs as the
operator: no registration generation is needed, no role permission applies,
and --changed-by (default "human") is the audit actor.`

func resolveOrchestratorAuthority(cmd *cobra.Command) (models.AgentAuthority, error) {
	agentID, err := resolveOrchestratorID(cmd)
	if err != nil {
		return models.AgentAuthority{}, err
	}
	return requireAgentAuthorityForID(cmd, agentID)
}

var addTasksCmd = &cobra.Command{
	Use:   "add-tasks --tasks-file <path>",
	Short: "Add multiple tasks in batch from a JSON file",
	Long: `Add multiple tasks to state.yaml in a single batch operation.

Reads task definitions from a JSON file. Each task must have id, desc, spec,
done, and scope. Optional fields: priority, depends, type, role_pair, plan_ref,
validation, destructive_db, rca_required, provider_dependencies,
descendant_dependencies.

provider_dependencies uses the same shape as output declarations:
  [{"provider_task":"provider-architecture","transition":"architecture-to-code-plan","outputs":[0,2]}]
The provider must exist; transition names come from its configured per-subtask
transitions. Selected children may be unborn, but claim requires the provider
MERGED, transition executed, and every selected child MERGED. Ordinary downstream
depends stays invalid. Use reviewed replacement/reauthoring for legacy prose-only
preconditions; unblocking alone does not repair their missing declaration.
descendant_dependencies uses the set-task-output shape; the task does not wait
on them but adds them to every output it writes at its per-subtask transition.

Set task-level rca_required when a direct planning objective is a defect fix.
It is the default inherited by generated children only when an output entry does
not provide an explicit override. Mapped code-planning decomposition roots use
false at task level and classify rca_required independently on every output.

Tasks are added independently; failed tasks don't block subsequent ones.
Each added task is scoped-validated before persistence. If unrelated existing
state corruption keeps full-state validation degraded after an add, the item
succeeds with a warning so repair tasks can still be created.

Example:
  cat > tasks.json <<'EOF'
  [
    {"id": "task-1", "desc": "Implement X", "spec": "specs/x.md", "done": "X works", "scope": "internal/x"},
    {"id": "task-2", "desc": "Implement Y", "spec": "specs/y.md", "done": "Y works", "scope": "internal/y", "validation": ["make test"], "destructive_db": false, "depends": ["task-1"]}
  ]
  EOF
  ` + brand.Command("add-tasks") + ` --tasks-file tasks.json`,
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
			return cliValidationError("--tasks-file is required")
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			return cliValidationWrap("reading tasks file", err)
		}

		var tasks []ops.AddTaskInput
		if err := json.Unmarshal(data, &tasks); err != nil {
			hint := "tasks file must contain a JSON array [...]"
			if len(data) > 0 && data[0] == '{' {
				hint += "; got a JSON object — remove the wrapper and pass a bare array"
			}
			return &ops.PreconditionError{Reason: fmt.Sprintf("%s: %v", hint, err)}
		}

		authority, err := resolveOrchestratorAuthority(cmd)
		if err != nil {
			return err
		}

		statePath := filepath.Join(paths.ProjectDirName(), paths.StateFileName)
		logPath := filepath.Join(paths.ProjectDirName(), paths.LogFileName)

		resolver, err := loadResolverFromDir(filepath.Dir(statePath))
		if err != nil {
			return err
		}
		if err := validateAllowedOperation(resolver, authority.ID, "add-tasks"); err != nil {
			return err
		}

		input := &ops.AddTasksInput{
			Tasks:          tasks,
			OrchestratorID: authority.ID,
		}

		if isJSON(cmd) {
			result, err := ops.AddTasksWithAuthority(statePath, logPath, input, authority)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.AddTasksWithAuthorityCommand(statePath, logPath, input, authority)
	},
}

// init registers every task command family in the order they were registered
// before the families moved to their own files.
func init() {
	registerTaskAuthoringCommands()
	registerTaskDependencyCommands()
	registerTaskRetirementCommands()
	registerTaskBlockedCommands()
	registerTaskRecordCommands()
}

// registerTaskAuthoringCommands registers claim-task, add-task and add-tasks.
func registerTaskAuthoringCommands() {
	addLifecycleFlags(claimTaskCmd)
	rootCmd.AddCommand(claimTaskCmd)
	rootCmd.AddCommand(addTaskCmd)
	rootCmd.AddCommand(addTasksCmd)

	claimTaskCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return completeTaskIDs(cmd, args, toComplete)
		}
		if len(args) == 1 {
			return completeAgentIDs(cmd, args, toComplete)
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	addJSONFlag(claimTaskCmd)
	addJSONFlag(addTaskCmd)
	addJSONFlag(addTasksCmd)

	addAgentIDFlag(addTaskCmd)

	// Add-task command flags
	addTaskCmd.Flags().String("file", "", "path to YAML file containing task details")
	addTaskCmd.Flags().String("id", "", "task ID (required unless using --file)")
	addTaskCmd.Flags().String("desc", "", "task description (required unless using --file)")
	addTaskCmd.Flags().String("spec", "", "spec reference (required unless using --file)")
	addTaskCmd.Flags().String("done", "", "done-when criteria (required unless using --file)")
	addTaskCmd.Flags().StringArray("validation", nil, "canonical validation command; repeat for multiple commands (overrides file value)")
	addTaskCmd.Flags().Bool("destructive-db", false, fmt.Sprintf("mark task validation as destructive to DB state; commands must start with %s=1", brand.EnvName("ALLOW_DESTRUCTIVE_DB")))
	addTaskCmd.Flags().String("scope", "", "task scope (required unless using --file)")
	addTaskCmd.Flags().Int("priority", 0, "task priority (default: 1, overrides file value)")
	addTaskCmd.Flags().String("depends", "", "comma-separated list of task IDs this task depends on (overrides file value)")
	addTaskCmd.Flags().String("type", "", "optional task type override (default: derived from --role-pair or file role_pair)")
	addTaskCmd.Flags().String("role-pair", "", "task role-pair used for pipeline state and default type (required unless provided by --file)")
	addTaskCmd.Flags().String("planning-change-kind", "", "corrective planning work kind: correction or replan")
	addTaskCmd.Flags().String("planning-change-original", "", "existing original plan for corrective planning metrics")
	addTaskCmd.Flags().String("trigger", "", "explicit cause of corrective planning work")
	addTaskCmd.Flags().String("state", "", fmt.Sprintf("path to state.yaml (default: %s/state.yaml)", paths.ProjectDirName()))
	addTaskCmd.Flags().String("log", "", fmt.Sprintf("path to log.yaml (default: %s/log.yaml)", paths.ProjectDirName()))
	registerCompletion(addTaskCmd, "depends", completeTaskIDs)

	// Add-tasks (batch) command flags
	addAgentIDFlag(addTasksCmd)
	addTasksCmd.Flags().String("tasks-file", "", "path to JSON file with task definitions array (required)")
}
