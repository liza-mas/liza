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

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/payloadschema"
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
		if err := validateRoleType(resolver, agentID, "doer"); err != nil {
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

Updates sprint.scope.planned, goal.alignment_history, and logs the action.
Validates the added task and reports a warning if unrelated existing state
corruption keeps full-state validation degraded after the add.

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

		if input.Priority == 0 {
			input.Priority = 1
		}

		authority, err := resolveOrchestratorAuthority(cmd)
		if err != nil {
			return err
		}

		resolver, err := loadResolverFromDir(filepath.Dir(statePath))
		if err != nil {
			return err
		}
		if err := validateRoleType(resolver, authority.ID, "orchestrator"); err != nil {
			return err
		}

		if isJSON(cmd) {
			opsInput := &ops.AddTaskInput{
				ID:                      input.ID,
				Type:                    input.Type,
				RolePair:                input.RolePair,
				Description:             input.Description,
				SpecRef:                 input.SpecRef,
				DoneWhen:                input.DoneWhen,
				Validation:              input.Validation,
				ValidationPrerequisites: models.CloneValidationPrerequisites(input.ValidationPrerequisites),
				DestructiveDB:           input.DestructiveDB,
				Scope:                   input.Scope,
				Priority:                input.Priority,
				DependsOn:               input.DependsOn,
			}
			result, err := ops.AddTaskWithAuthority(statePath, logPath, opsInput, authority)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.AddTaskWithAuthorityCommand(statePath, logPath, input, authority)
	},
}

var supersedeTaskCmd = &cobra.Command{
	Use:   "supersede-task <task-id> [replacement-task-ids] --reason <reason>",
	Short: "Mark a task as SUPERSEDED, optionally by replacement tasks",
	Long: fmt.Sprintf(`Mark a task as SUPERSEDED when it is replaced by new task(s) or completed externally.

Used by orchestrator when rescoping blocked, rejected, or problematic tasks,
or when a task's work was already completed outside the current sprint.

Requirements:
  - Task must be in BLOCKED, rejected, or initial status
  - --reason is always required
  - --recoverability-command is required when no replacements are given

Replacement task IDs are optional and should be comma-separated.
When no replacements are given, the task's branch is deleted immediately after
recording pre-supersession branch/worktree evidence and the operator-provided
recoverability audit command. %[3]s records that command but does not execute it.

Examples:
  %[1]s task-3 task-4,task-5 --reason "Split into smaller tasks"
  %[1]s task-3 --reason "Work already merged in prior sprint" --recoverability-command "%[2]s"`, brand.Command("supersede-task"), brand.Command("recover-task", "task-3"), brand.NameTitle),
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

		invocation := beginLifecycleCLI(cmd, args)
		defer invocation.finish(&retErr)
		requestOpts, err := lifecycleRequestOptions(cmd)
		if err != nil {
			return err
		}

		taskID := args[0]

		reason, _ := cmd.Flags().GetString("reason")
		recoverabilityCommand, _ := cmd.Flags().GetString("recoverability-command")

		var replacementIDs []string
		if len(args) == 2 {
			for _, id := range strings.Split(args[1], ",") {
				replacementIDs = append(replacementIDs, strings.TrimSpace(id))
			}
		}

		authority, err := resolveOrchestratorAuthority(cmd)
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
		if err := validateAllowedOperation(resolver, authority.ID, "supersede-task"); err != nil {
			return err
		}

		invocation.calledOps = true
		if isJSON(cmd) {
			result, err := ops.SupersedeTaskWithAuthority(projectRoot, taskID, replacementIDs, reason, authority, ops.SupersedeTaskOptions{
				Request:               requestOpts,
				RecoverabilityCommand: recoverabilityCommand,
			})
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.SupersedeTaskWithAuthorityAndOptionsCommand(projectRoot, taskID, replacementIDs, reason, recoverabilityCommand, authority, requestOpts)
	},
}

var retargetDependencyCmd = &cobra.Command{
	Use:   "retarget-dependency <task-id> <old-dep-id> <new-dep-ids> --reason <reason>",
	Short: "Retarget one non-terminal task dependency edge",
	Long: fmt.Sprintf(`Retarget one non-terminal task's direct depends_on edge.

This is an orchestrator-only metadata repair operation for cases where one task
has the wrong scheduler dependency but neither the dependent task nor the old
dependency should be superseded. It replaces exactly one old dependency with one
or more comma-separated new dependencies, canonicalizes the resulting dependency
list, validates the full candidate state, records audit history, and leaves task
status unchanged.

Examples:
  %[1]s task-3 old-task replacement-task --reason "Correct dependency after planning repair"
  %[1]s task-3 old-task repl-a,repl-b --reason "Split dependency into two prerequisites"`, brand.Command("retarget-dependency")),
	Args: cobra.ExactArgs(3),
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

		invocation := beginLifecycleCLI(cmd, args)
		defer invocation.finish(&retErr)
		requestOpts, err := lifecycleRequestOptions(cmd)
		if err != nil {
			return err
		}

		taskID := args[0]
		oldDependency := args[1]
		newDependencies := strings.Split(args[2], ",")
		reason, _ := cmd.Flags().GetString("reason")

		authority, err := resolveOrchestratorAuthority(cmd)
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
		if err := validateAllowedOperation(resolver, authority.ID, "retarget-dependency"); err != nil {
			return err
		}

		invocation.calledOps = true
		if isJSON(cmd) {
			result, err := ops.RetargetDependencyWithAuthorityAndOptions(projectRoot, taskID, oldDependency, newDependencies, reason, authority, requestOpts)
			verbose, _ := cmd.Flags().GetBool("verbose")
			if verbose {
				writeRetargetDependencyVerboseDiagnostic(os.Stderr, err)
			}
			return jsonout.WriteResult(os.Stdout, result, resultWarnings(result), err)
		}
		return commands.RetargetDependencyWithAuthorityAndOptionsCommand(projectRoot, taskID, oldDependency, newDependencies, reason, authority, requestOpts)
	},
}

func writeRetargetDependencyVerboseDiagnostic(stderr io.Writer, err error) {
	if err == nil {
		return
	}

	details := jsonout.ErrorDetails(err)
	if details["diagnostic_action"] != "retarget_dependency_rejected" {
		return
	}
	_, message := jsonout.ClassifyError(err)
	diagnostic := struct {
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	}{
		Message: message,
		Details: details,
	}
	// The diagnostic is secondary to the stdout envelope; a stderr write failure
	// must not replace the classified operation result.
	_ = json.NewEncoder(stderr).Encode(diagnostic)
}

var narrowInheritedDependenciesCmd = &cobra.Command{
	Use:   "narrow-inherited-dependencies <producer-task-id> --selections <file> --reason <reason>",
	Short: "Narrow generated children to selected upstream outputs",
	Long: fmt.Sprintf(`Apply inherit_inputs to a MERGED planning task after its children were generated.

ADR-0137 lets a plan output name the upstream outputs its child actually needs,
but only at generation time. This orchestrator-only metadata repair authors
that intent after the fact: it records inherit_inputs on the producer's
output[] entries named in the selections file, then removes from each generated
child still in its initial status the inherited phase-gate edges the selection
does not keep. Sibling, task_depends_on, and manually retargeted edges are never
removed. Terminal, claimed, executing, reviewing, and blocked children are left
untouched and reported as skipped.

The whole operation fails closed: an unresolvable or out-of-range selection
writes nothing. The transition is auto-detected when the producer executed
exactly one per-subtask transition; pass --transition otherwise.

Selections file (YAML or JSON):
  outputs:
    - index: 0
      inherit_inputs:
        mode: selected
        selections:
          - upstream_task: records-plan
            outputs: [0, 2]
    - index: 3
      inherit_inputs:
        mode: all      # restore the whole-phase barrier on this child

Example:
  %s records-plan-cp-0 --selections narrow.yaml --reason "Docs child needs only the schema contract"`, brand.Command("narrow-inherited-dependencies")),
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

		invocation := beginLifecycleCLI(cmd, args)
		defer invocation.finish(&retErr)
		requestOpts, err := lifecycleRequestOptions(cmd)
		if err != nil {
			return err
		}

		producerID := args[0]
		transition, _ := cmd.Flags().GetString("transition")
		selectionsPath, _ := cmd.Flags().GetString("selections")
		reason, _ := cmd.Flags().GetString("reason")

		selections, err := ops.LoadNarrowSelectionsFile(selectionsPath)
		if err != nil {
			return err
		}

		authority, err := resolveOrchestratorAuthority(cmd)
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
		if err := validateAllowedOperation(resolver, authority.ID, "narrow-inherited-dependencies"); err != nil {
			return err
		}

		invocation.calledOps = true
		if isJSON(cmd) {
			result, err := ops.NarrowInheritedDependenciesWithAuthorityAndOptions(projectRoot, producerID, transition, selections, reason, authority, requestOpts)
			return jsonout.WriteResult(os.Stdout, result, resultWarnings(result), err)
		}
		return commands.NarrowInheritedDependenciesWithAuthorityAndOptionsCommand(projectRoot, producerID, transition, selections, reason, authority, requestOpts)
	},
}

var applyDependencyRepairCmd = &cobra.Command{
	Use:   "apply-dependency-repair <blocked-task-id> --reason <reason>",
	Short: "Apply one stored dependency repair atomically",
	Long: fmt.Sprintf(`Apply the declarative dependency repair stored on one blocked task.

This orchestrator-only operation verifies every expected dependency list, computes
every canonical desired list, validates the complete candidate graph, and commits
the batch with audit history in one state transaction. The source task remains
blocked so repair validation and unblocking stay explicit.

Example:
  %s task-3 --reason "Apply the stored dependency graph repair"`, brand.Command("apply-dependency-repair")),
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

		invocation := beginLifecycleCLI(cmd, args)
		defer invocation.finish(&retErr)
		requestOpts, err := lifecycleRequestOptions(cmd)
		if err != nil {
			return err
		}

		sourceTaskID := args[0]
		reason, _ := cmd.Flags().GetString("reason")

		authority, err := resolveOrchestratorAuthority(cmd)
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
		if err := validateAllowedOperation(resolver, authority.ID, "apply-dependency-repair"); err != nil {
			return err
		}

		invocation.calledOps = true
		if isJSON(cmd) {
			result, err := ops.ApplyDependencyRepairWithAuthorityAndOptions(projectRoot, sourceTaskID, reason, authority, requestOpts)
			return jsonout.WriteResult(os.Stdout, result, resultWarnings(result), err)
		}
		return commands.ApplyDependencyRepairWithAuthorityAndOptionsCommand(projectRoot, sourceTaskID, reason, authority, requestOpts)
	},
}

var repairSupersededDependenciesCmd = &cobra.Command{
	Use:   "repair-superseded-dependencies <task-id> --reason <reason>",
	Short: "Repair illegal dependencies on one superseded task",
	Long: fmt.Sprintf(`Remove every illegal downstream dependency from one superseded task.

This orchestrator-only repair preserves legal dependencies and all terminal task
metadata, validates the full candidate state, and records the reason and caller in
task history and the activity log.

Example:
  %s task-3 --reason "Repair terminal dependency metadata"`, brand.Command("repair-superseded-dependencies")),
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

		invocation := beginLifecycleCLI(cmd, args)
		defer invocation.finish(&retErr)
		requestOpts, err := lifecycleRequestOptions(cmd)
		if err != nil {
			return err
		}

		taskID := args[0]
		reason, _ := cmd.Flags().GetString("reason")

		authority, err := resolveOrchestratorAuthority(cmd)
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
		if err := validateAllowedOperation(resolver, authority.ID, "repair-superseded-dependencies"); err != nil {
			return err
		}

		invocation.calledOps = true
		if isJSON(cmd) {
			result, err := ops.RepairSupersededDependenciesWithAuthorityAndOptions(projectRoot, taskID, reason, authority, requestOpts)
			return jsonout.WriteResult(os.Stdout, result, resultWarnings(result), err)
		}
		return commands.RepairSupersededDependenciesWithAuthorityAndOptionsCommand(projectRoot, taskID, reason, authority, requestOpts)
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
  - Agent ID must be provided (via --agent-id flag or ` + brand.EnvName("AGENT_ID") + ` env var)
  - Task must be in an executing status (e.g. IMPLEMENTING_CODE, CODE_PLANNING)
  - Only the assigned agent can mark a task as blocked
  - Requires a reason and 1-3 clarifying questions

Effects:
  - status = BLOCKED
  - blocked_reason = <reason>
  - blocked_questions = [<questions>]
  - repair_request = <structured orchestrator repair request> when --repair-* flags or --repair-request-file are provided
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

		invocation := beginLifecycleCLI(cmd, args)
		defer invocation.finish(&retErr)
		requestOpts, err := lifecycleRequestOptions(cmd)
		if err != nil {
			return err
		}

		taskID := args[0]

		reason, _ := cmd.Flags().GetString("reason")
		questions, _ := cmd.Flags().GetStringSlice("questions")
		opts, err := markBlockedOptionsFromFlags(cmd)
		if err != nil {
			return err
		}

		authority, err := requireAgentAuthority(cmd)
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
		if err := validateAllowedOperation(resolver, authority.ID, "mark-blocked"); err != nil {
			return err
		}

		opts.Request = requestOpts
		invocation.calledOps = true
		if isJSON(cmd) {
			result, err := ops.MarkBlockedWithAuthority(projectRoot, taskID, reason, questions, authority, opts)
			return jsonout.WriteResult(os.Stdout, result, resultWarnings(result), err)
		}
		return commands.MarkBlockedWithAuthorityCommand(projectRoot, taskID, reason, questions, authority, opts)
	},
}

var unblockTaskCmd = &cobra.Command{
	Use:   "unblock-task <task-id>",
	Short: "Restore a repaired BLOCKED task to its initial or executing state",
	Long: `Restore a BLOCKED task after the orchestrator has verified that the blocker is gone.

	This is for repair completion, not normal task claiming. Without --assign-to,
	it moves the task back to the initial status for its role_pair. Pending dependencies
	keep that task dependency-held and not immediately claimable. With --assign-to,
	it directly restores the executing status and assigns the requested doer agent.
	With --rebase-on, it rebases a preserved task worktree before unblocking, updates
	base_commit, and leaves rebase conflicts BLOCKED with fresh repair metadata.`,
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

		invocation := beginLifecycleCLI(cmd, args)
		defer invocation.finish(&retErr)
		requestOpts, err := lifecycleRequestOptions(cmd)
		if err != nil {
			return err
		}

		taskID := args[0]
		assignTo, _ := cmd.Flags().GetString("assign-to")
		if !cmd.Flags().Changed("assign-to") {
			assignTo = ""
		}
		reason, _ := cmd.Flags().GetString("reason")
		rebaseOn, _ := cmd.Flags().GetString("rebase-on")
		if !cmd.Flags().Changed("rebase-on") {
			rebaseOn = ""
		}
		allowDirty, _ := cmd.Flags().GetBool("allow-dirty")
		if !cmd.Flags().Changed("allow-dirty") {
			allowDirty = false
		}
		opts := ops.UnblockTaskOptions{
			Request:    requestOpts,
			AssignTo:   assignTo,
			RebaseOn:   rebaseOn,
			AllowDirty: allowDirty,
		}

		authority, err := resolveOrchestratorAuthority(cmd)
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
		if err := validateAllowedOperation(resolver, authority.ID, "unblock-task"); err != nil {
			return err
		}

		invocation.calledOps = true
		if isJSON(cmd) {
			result, err := ops.UnblockTaskWithAuthority(projectRoot, taskID, reason, authority, opts)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.UnblockTaskWithAuthorityCommand(projectRoot, taskID, reason, authority, opts)
	},
}

func markBlockedOptionsFromFlags(cmd *cobra.Command) (ops.MarkBlockedOptions, error) {
	dependsOn, _ := cmd.Flags().GetStringSlice("depends-on")
	repairRequest, err := repairRequestFromFlags(cmd)
	if err != nil {
		return ops.MarkBlockedOptions{}, err
	}
	return ops.MarkBlockedOptions{DependsOn: dependsOn, RepairRequest: repairRequest}, nil
}

func repairRequestFromFlags(cmd *cobra.Command) (*models.RepairRequest, error) {
	repairRequestFile, _ := cmd.Flags().GetString("repair-request-file")
	operation, _ := cmd.Flags().GetString("repair-operation")
	target, _ := cmd.Flags().GetString("repair-target")
	command, _ := cmd.Flags().GetString("repair-command")
	evidence, _ := cmd.Flags().GetStringArray("repair-evidence")
	validation, _ := cmd.Flags().GetStringArray("repair-validation")

	if cmd.Flags().Changed("repair-request-file") {
		if strings.TrimSpace(repairRequestFile) == "" {
			return nil, cliValidationError("--repair-request-file requires a path")
		}
		for _, name := range []string{"repair-operation", "repair-target", "repair-command", "repair-evidence", "repair-validation"} {
			if cmd.Flags().Changed(name) {
				return nil, cliValidationError("--repair-request-file cannot be combined with --repair-* fields")
			}
		}

		data, err := os.ReadFile(repairRequestFile)
		if err != nil {
			return nil, cliValidationWrap("reading repair request file", err)
		}
		if strings.TrimSpace(string(data)) == "" {
			return nil, cliValidationError("repair request file is empty")
		}
		var request models.RepairRequest
		if err := json.Unmarshal(data, &request); err != nil {
			return nil, cliValidationWrap("parsing repair request file", err)
		}
		return &request, nil
	}

	hasRepairRequest := false
	for _, name := range []string{"repair-operation", "repair-target", "repair-command", "repair-evidence", "repair-validation"} {
		hasRepairRequest = hasRepairRequest || cmd.Flags().Changed(name)
	}
	if !hasRepairRequest {
		return nil, nil
	}
	if strings.TrimSpace(operation) == "" {
		return nil, cliValidationError("--repair-operation is required when repair request fields are provided")
	}
	if strings.TrimSpace(target) == "" {
		return nil, cliValidationError("--repair-target is required when repair request fields are provided")
	}
	if strings.TrimSpace(command) == "" {
		return nil, cliValidationError("--repair-command is required when repair request fields are provided")
	}
	if !hasNonEmptyValue(evidence) {
		return nil, cliValidationError("--repair-evidence is required when repair request fields are provided")
	}
	if !hasNonEmptyValue(validation) {
		return nil, cliValidationError("--repair-validation is required when repair request fields are provided")
	}
	return &models.RepairRequest{
		Operation:  operation,
		Target:     target,
		Command:    command,
		Evidence:   evidence,
		Validation: validation,
	}, nil
}

func hasNonEmptyValue(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func resultWarnings(result interface{ GetWarnings() []string }) []string {
	if result == nil {
		return nil
	}
	return result.GetWarnings()
}

func resolveOrchestratorAuthority(cmd *cobra.Command) (models.AgentAuthority, error) {
	agentID, err := resolveOrchestratorID(cmd)
	if err != nil {
		return models.AgentAuthority{}, err
	}
	return requireAgentAuthorityForID(cmd, agentID)
}

var assessBlockedCmd = &cobra.Command{
	Use:   "assess-blocked <task-id>",
	Short: "Record orchestrator assessment of a BLOCKED task",
	Long: `Record that the orchestrator has assessed a BLOCKED task.

This prevents the orchestrator re-wake loop where blocked tasks that have
already been triaged continue to trigger new orchestrator sessions.

After assessing, the task remains BLOCKED but won't trigger further wakes
unless new activity occurs (dependency changes, human notes, etc.).

With --awaits, the task waits for all of the named existing, unfinished
tasks: it wakes once they have all merged, or as soon as one fails, instead of
on every generated task under its dependencies or each named task settling.
Use it when the block waits on work a dependency edge cannot express. An
assessment without --awaits keeps the current set, minus merged tasks;
--clear-awaits drops it.

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

		invocation := beginLifecycleCLI(cmd, args)
		defer invocation.finish(&retErr)
		requestOpts, err := lifecycleRequestOptions(cmd)
		if err != nil {
			return err
		}

		taskID := args[0]

		note, _ := cmd.Flags().GetString("note")
		opts, err := assessBlockedOptionsFromFlags(cmd)
		if err != nil {
			return err
		}

		authority, err := resolveOrchestratorAuthority(cmd)
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
		if err := validateRoleType(resolver, authority.ID, "orchestrator"); err != nil {
			return err
		}

		opts.Request = requestOpts
		invocation.calledOps = true
		if isJSON(cmd) {
			result, err := ops.AssessBlockedWithAuthority(projectRoot, taskID, note, authority, opts)
			return jsonout.WriteResult(os.Stdout, result, resultWarnings(result), err)
		}
		return commands.AssessBlockedWithAuthorityCommand(projectRoot, taskID, note, authority, opts)
	},
}

func assessBlockedOptionsFromFlags(cmd *cobra.Command) (ops.AssessBlockedOptions, error) {
	reason, _ := cmd.Flags().GetString("reason")
	questions, _ := cmd.Flags().GetStringArray("question")
	awaited, _ := cmd.Flags().GetStringSlice("awaits")
	clearAwaits, _ := cmd.Flags().GetBool("clear-awaits")
	if clearAwaits && len(awaited) > 0 {
		return ops.AssessBlockedOptions{}, cliValidationError("--clear-awaits cannot be combined with --awaits")
	}
	repairRequest, err := repairRequestFromFlags(cmd)
	if err != nil {
		return ops.AssessBlockedOptions{}, err
	}
	reconcile := cmd.Flags().Changed("reason") || cmd.Flags().Changed("question") || repairRequest != nil
	if !reconcile {
		return ops.AssessBlockedOptions{AwaitedTasks: awaited, ClearAwaited: clearAwaits}, nil
	}
	if strings.TrimSpace(reason) == "" {
		return ops.AssessBlockedOptions{}, cliValidationError("--reason is required for canonical metadata reconciliation")
	}
	if len(questions) == 0 {
		return ops.AssessBlockedOptions{}, cliValidationError("--question is required for canonical metadata reconciliation")
	}
	if len(questions) > 3 {
		return ops.AssessBlockedOptions{}, cliValidationError("--question may be repeated at most 3 times")
	}
	for _, question := range questions {
		if strings.TrimSpace(question) == "" {
			return ops.AssessBlockedOptions{}, cliValidationError("--question values must not be empty")
		}
	}
	return ops.AssessBlockedOptions{Reason: reason, Questions: questions, RepairRequest: repairRequest, AwaitedTasks: awaited, ClearAwaited: clearAwaits}, nil
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

		invocation := beginLifecycleCLI(cmd, args)
		defer invocation.finish(&retErr)
		requestOpts, err := lifecycleRequestOptions(cmd)
		if err != nil {
			return err
		}

		taskID := args[0]

		note, _ := cmd.Flags().GetString("note")

		authority, err := resolveOrchestratorAuthority(cmd)
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
		if err := validateRoleType(resolver, authority.ID, "orchestrator"); err != nil {
			return err
		}

		invocation.calledOps = true
		if isJSON(cmd) {
			result, err := ops.AssessHypothesisExhaustedWithAuthorityAndOptions(projectRoot, taskID, note, authority, requestOpts)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.AssessHypothesisExhaustedWithAuthorityAndOptionsCommand(projectRoot, taskID, note, authority, requestOpts)
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
  - Active states: executing, submitted, and reviewing states before approval
  - Rejected states: CODE_REJECTED, CODING_PLAN_REJECTED, etc.
  - BLOCKED, INTEGRATION_FAILED

Not cancellable: approved or terminal states. Cancelling releases ` + brand.NameTitle + `'s state
claims and removes the task worktree/branch best-effort; it does not kill a
live provider process. Stale agent commands fail once they observe the
ABANDONED task state or missing worktree.

Example:
  ` + brand.Command("cancel-task", "task-3") + ` "Requirements no longer valid"`,
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

		invocation := beginLifecycleCLI(cmd, args)
		defer invocation.finish(&retErr)
		requestOpts, err := lifecycleRequestOptions(cmd)
		if err != nil {
			return err
		}

		taskID := args[0]
		reason := args[1]

		authority, err := resolveOrchestratorAuthority(cmd)
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
		if err := validateAllowedOperation(resolver, authority.ID, "cancel-task"); err != nil {
			return err
		}

		invocation.calledOps = true
		if isJSON(cmd) {
			result, err := ops.CancelTaskWithAuthorityAndOptions(projectRoot, taskID, reason, authority, requestOpts)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.CancelTaskWithAuthorityAndOptionsCommand(projectRoot, taskID, reason, authority, requestOpts)
	},
}

var reconcileMergedCmd = &cobra.Command{
	Use:   "reconcile-merged <task-id>",
	Short: "Mark an externally merged integration failure as merged",
	Long: fmt.Sprintf(`Mark an INTEGRATION_FAILED task as MERGED after verifying it was completed outside %s.

This is intended for recovery from stale integration-failure state, such as when
a GitHub PR was merged manually and the task worktree is already gone.

Example:
  %s --merge-commit abc123 --pr-url https://github.com/org/repo/pull/17 --reason "PR merged externally"`, brand.NameTitle, brand.Command("reconcile-merged", "task-3")),
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
		mergeCommit, _ := cmd.Flags().GetString("merge-commit")
		prURL, _ := cmd.Flags().GetString("pr-url")
		reason, _ := cmd.Flags().GetString("reason")

		authority, err := resolveOrchestratorAuthority(cmd)
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
		if err := validateAllowedOperation(resolver, authority.ID, "reconcile-merged"); err != nil {
			return err
		}

		result, err := ops.ReconcileMergedWithAuthority(projectRoot, taskID, mergeCommit, prURL, reason, authority)
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "Reconciled task %s as MERGED at %s\n", result.TaskID, result.MergeCommit)
		for _, warning := range result.Warnings {
			fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
		}
		return nil
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
  - Agent ID must be provided (via --agent-id flag or ` + brand.EnvName("AGENT_ID") + ` env var)
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

		authority, err := requireAgentAuthority(cmd)
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
		if err := validateAllowedOperation(resolver, authority.ID, "write-checkpoint"); err != nil {
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
			AgentID:        authority.ID,
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
			err := ops.WriteCheckpointWithAuthority(projectRoot, input, authority)
			return jsonout.WriteResult(os.Stdout, nil, nil, err)
		}
		return commands.WriteCheckpointWithAuthorityCommand(projectRoot, input, authority)
	},
}

var setTaskOutputCmd = &cobra.Command{
	Use:   "set-task-output <task-id> --output <path>",
	Short: "Set output entries for downstream task generation",
	Long: `Define output entries that will become downstream tasks after merge.

Reads output entries from a JSON file. Each entry must have desc, done_when,
scope, and spec_ref. Optional fields: epic_ref, plan_ref, arch_ref, validation,
destructive_db, rca_required, depends_on, task_depends_on, decomposition.

depends_on contains sibling output indexes, e.g. "0" for output[0].
task_depends_on contains existing concrete task IDs to copy onto generated
child tasks.
inherit_inputs declares whether this child waits for a whole upstream phase or
only for selected upstream outputs. Omitting it inherits every child of every
upstream dependency, which is the default. To narrow it:
  {"mode": "selected", "selections": [{"upstream_task": "plan-1", "outputs": [0, 2]}]}
outputs are indexes into that upstream task's own output[], not task IDs — the
upstream's children usually do not exist yet. upstream_task must be a
dependency of the producing task. An unresolvable or out-of-range selection
fails the transition rather than dropping the dependency. Use
{"mode": "all"} to record a deliberate whole-phase barrier.
An explicit output value overrides the parent task's rca_required value; an
omitted value inherits the parent default. A decomposition root whose configured
consumer is code planning must provide rca_required on every output entry, along
with its required artifact reference and typed decomposition metadata.

Requirements:
  - Agent ID must be provided (via --agent-id flag or ` + brand.EnvName("AGENT_ID") + ` env var)
  - Task must be in an executing status
  - Task must be assigned to the submitting agent
  - At least one output entry required

Updates:
  - Sets task.output to provided entries (overwrites existing, idempotent)

Example:
  cat > outputs.json <<'EOF'
  [
    {"desc": "Subtask 1", "done_when": "Tests pass", "scope": "internal/pkg"},
    {"desc": "Subtask 2", "done_when": "API works", "scope": "internal/api", "validation": ["make test"], "destructive_db": false, "depends_on": ["0"], "task_depends_on": ["existing-task-id"]}
  ]
  EOF
  ` + brand.Command("set-task-output", "task-1") + ` --output outputs.json`,
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

		invocation := beginLifecycleCLI(cmd, args)
		defer invocation.finish(&retErr)
		requestOpts, err := lifecycleRequestOptions(cmd)
		if err != nil {
			return err
		}

		taskID := args[0]

		authority, err := requireAgentAuthority(cmd)
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
		if err := validateAllowedOperation(resolver, authority.ID, "set-task-output"); err != nil {
			return err
		}

		outputFile, _ := cmd.Flags().GetString("output")
		if outputFile == "" {
			return cliValidationError("--output is required")
		}

		data, err := os.ReadFile(outputFile)
		if err != nil {
			return cliValidationWrap("reading output file", err)
		}

		var entries []models.OutputEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			hint := "output file must contain a JSON array [...]"
			if len(data) > 0 && data[0] == '{' {
				hint += "; got a JSON object — remove the wrapper and pass a bare array"
			}
			return &ops.PreconditionError{Reason: fmt.Sprintf("%s: %v", hint, err)}
		}

		// Preserve the file's null/[] distinction before the typed API
		// normalizes nil slices into explicit empty manifests.
		_, diagnostics, err := payloadschema.Validate(payloadschema.SetTaskOutputOperation, map[string]any{"output": json.RawMessage(data)})
		if err != nil {
			return err
		}
		if len(diagnostics) > 0 {
			return ops.NewLifecycleInvalidInputError(payloadschema.SetTaskOutputOperation, nil, diagnostics,
				&ops.PreconditionError{Reason: "output file must contain a valid task-output manifest array"})
		}

		input := &ops.SetTaskOutputInput{
			Request: requestOpts,
			TaskID:  taskID,
			AgentID: authority.ID,
			Output:  entries,
		}

		invocation.calledOps = true
		if isJSON(cmd) {
			result, err := ops.SetTaskOutputWithAuthorityAndOptions(projectRoot, input, authority)
			var warnings []string
			if result != nil {
				warnings = result.Warnings
			}
			return jsonout.WriteResult(os.Stdout, result, warnings, err)
		}
		return commands.SetTaskOutputWithAuthorityCommand(projectRoot, input, authority)
	},
}

var addTasksCmd = &cobra.Command{
	Use:   "add-tasks --tasks-file <path>",
	Short: "Add multiple tasks in batch from a JSON file",
	Long: `Add multiple tasks to state.yaml in a single batch operation.

Reads task definitions from a JSON file. Each task must have id, desc, spec,
done, and scope. Optional fields: priority, depends, type, role_pair, plan_ref,
validation, destructive_db, rca_required.

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
	for _, cmd := range []*cobra.Command{cancelTaskCmd, supersedeTaskCmd, unblockTaskCmd, setTaskOutputCmd, claimTaskCmd, retargetDependencyCmd, narrowInheritedDependenciesCmd, applyDependencyRepairCmd, repairSupersededDependenciesCmd, assessHypothesisExhaustedCmd, markBlockedCmd, assessBlockedCmd} {
		addLifecycleFlags(cmd)
	}
	rootCmd.AddCommand(claimTaskCmd)
	rootCmd.AddCommand(addTaskCmd)
	rootCmd.AddCommand(addTasksCmd)
	rootCmd.AddCommand(supersedeTaskCmd)
	rootCmd.AddCommand(retargetDependencyCmd)
	rootCmd.AddCommand(narrowInheritedDependenciesCmd)
	rootCmd.AddCommand(applyDependencyRepairCmd)
	rootCmd.AddCommand(repairSupersededDependenciesCmd)
	rootCmd.AddCommand(cancelTaskCmd)
	rootCmd.AddCommand(reconcileMergedCmd)
	rootCmd.AddCommand(markBlockedCmd)
	rootCmd.AddCommand(unblockTaskCmd)
	rootCmd.AddCommand(assessBlockedCmd)
	rootCmd.AddCommand(assessHypothesisExhaustedCmd)
	rootCmd.AddCommand(writeCheckpointCmd)
	rootCmd.AddCommand(setTaskOutputCmd)
	rootCmd.AddCommand(setDiscoveryDispositionCmd)
	deleteCmd.AddCommand(deleteTaskCmd)

	claimTaskCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return completeTaskIDs(cmd, args, toComplete)
		}
		if len(args) == 1 {
			return completeAgentIDs(cmd, args, toComplete)
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	supersedeTaskCmd.ValidArgsFunction = completeTaskIDArgs(2)
	retargetDependencyCmd.ValidArgsFunction = completeTaskIDArgs(3)
	narrowInheritedDependenciesCmd.ValidArgsFunction = completeTaskIDArgs(1)
	applyDependencyRepairCmd.ValidArgsFunction = completeTaskIDArgs(1)
	repairSupersededDependenciesCmd.ValidArgsFunction = completeTaskIDArgs(1)
	cancelTaskCmd.ValidArgsFunction = completeTaskIDArgs(1)
	reconcileMergedCmd.ValidArgsFunction = completeTaskIDArgs(1)
	markBlockedCmd.ValidArgsFunction = completeTaskIDArgs(1)
	unblockTaskCmd.ValidArgsFunction = completeTaskIDArgs(1)
	assessBlockedCmd.ValidArgsFunction = completeTaskIDArgs(1)
	assessHypothesisExhaustedCmd.ValidArgsFunction = completeTaskIDArgs(1)
	writeCheckpointCmd.ValidArgsFunction = completeTaskIDArgs(1)
	setTaskOutputCmd.ValidArgsFunction = completeTaskIDArgs(1)
	deleteTaskCmd.ValidArgsFunction = completeTaskIDArgs(1)

	addJSONFlag(claimTaskCmd)
	addJSONFlag(addTaskCmd)
	addJSONFlag(addTasksCmd)
	addJSONFlag(supersedeTaskCmd)
	addJSONFlag(retargetDependencyCmd)
	addJSONFlag(narrowInheritedDependenciesCmd)
	addJSONFlag(applyDependencyRepairCmd)
	addJSONFlag(repairSupersededDependenciesCmd)
	addJSONFlag(cancelTaskCmd)
	addJSONFlag(reconcileMergedCmd)
	addJSONFlag(markBlockedCmd)
	addJSONFlag(unblockTaskCmd)
	addJSONFlag(assessBlockedCmd)
	addJSONFlag(assessHypothesisExhaustedCmd)
	addJSONFlag(writeCheckpointCmd)
	addJSONFlag(setTaskOutputCmd)
	addJSONFlag(setDiscoveryDispositionCmd)

	addAgentIDFlag(addTaskCmd)
	addAgentIDFlag(supersedeTaskCmd)
	supersedeTaskCmd.Flags().String("reason", "", "reason for superseding (required)")
	supersedeTaskCmd.Flags().String("recoverability-command", "", "operator audit command recorded before superseding without replacements")
	supersedeTaskCmd.MarkFlagRequired("reason")
	addAgentIDFlag(retargetDependencyCmd)
	retargetDependencyCmd.Flags().String("reason", "", "reason for retargeting this dependency (required)")
	retargetDependencyCmd.MarkFlagRequired("reason")
	addAgentIDFlag(narrowInheritedDependenciesCmd)
	narrowInheritedDependenciesCmd.Flags().String("selections", "", "YAML or JSON file naming output indexes and their inherit_inputs (required)")
	narrowInheritedDependenciesCmd.Flags().String("transition", "", "per-subtask transition that generated the children (auto-detected when unambiguous)")
	narrowInheritedDependenciesCmd.Flags().String("reason", "", "reason for narrowing these dependencies (required)")
	narrowInheritedDependenciesCmd.MarkFlagRequired("selections")
	narrowInheritedDependenciesCmd.MarkFlagRequired("reason")
	addAgentIDFlag(applyDependencyRepairCmd)
	applyDependencyRepairCmd.Flags().String("reason", "", "reason for applying the stored dependency repair (required)")
	applyDependencyRepairCmd.MarkFlagRequired("reason")
	addAgentIDFlag(repairSupersededDependenciesCmd)
	repairSupersededDependenciesCmd.Flags().String("reason", "", "reason for repairing superseded dependencies (required)")
	repairSupersededDependenciesCmd.MarkFlagRequired("reason")
	addAgentIDFlag(cancelTaskCmd)
	addAgentIDFlag(reconcileMergedCmd)
	reconcileMergedCmd.Flags().String("merge-commit", "", "merge commit that completed the task externally (required)")
	reconcileMergedCmd.Flags().String("pr-url", "", "pull request URL for the external merge")
	reconcileMergedCmd.Flags().String("reason", "", "reason for reconciliation (required)")
	reconcileMergedCmd.MarkFlagRequired("merge-commit")
	reconcileMergedCmd.MarkFlagRequired("reason")

	// Mark-blocked command flags
	markBlockedCmd.Flags().String("reason", "", "reason why the task is blocked (required)")
	markBlockedCmd.Flags().StringSlice("questions", nil, "clarifying questions (1-3 required)")
	markBlockedCmd.Flags().String("repair-operation", "", "orchestrator-only repair operation requested for this blocker")
	markBlockedCmd.Flags().String("repair-target", "", "task or state object the requested repair should modify")
	markBlockedCmd.Flags().String("repair-command", "", "exact command the orchestrator should run or adapt")
	markBlockedCmd.Flags().StringArray("repair-evidence", nil, "evidence gathered before requesting orchestrator repair")
	markBlockedCmd.Flags().StringArray("repair-validation", nil, "validation already run or required after orchestrator repair")
	markBlockedCmd.Flags().String("repair-request-file", "", "path to a complete JSON repair request; mutually exclusive with --repair-* fields")
	markBlockedCmd.Flags().StringSlice("depends-on", nil, "task IDs blocking this task; also used as the orchestrator re-wake signal")
	markBlockedCmd.Flags().String("agent-id", "", "agent ID marking the task as blocked")
	markBlockedCmd.MarkFlagRequired("reason")
	markBlockedCmd.MarkFlagRequired("questions")
	registerCompletion(markBlockedCmd, "depends-on", completeTaskIDs)
	registerCompletion(markBlockedCmd, "agent-id", completeAgentIDs)

	// Unblock-task command flags
	unblockTaskCmd.Flags().String("agent-id", "", "orchestrator agent ID (auto-resolved if not provided)")
	unblockTaskCmd.Flags().String("assign-to", "", "doer agent ID to resume the task directly; omitted restores initial status and may remain dependency-held")
	unblockTaskCmd.Flags().String("reason", "", "reason the blocked task can resume (required)")
	unblockTaskCmd.Flags().String("rebase-on", "", "branch or commit to rebase the task worktree onto before unblocking")
	unblockTaskCmd.Flags().Bool("allow-dirty", false, "allow tracked worktree changes during --rebase-on by using git rebase --autostash")
	unblockTaskCmd.MarkFlagRequired("reason")
	registerCompletion(unblockTaskCmd, "agent-id", completeAgentIDs)
	registerCompletion(unblockTaskCmd, "assign-to", completeAgentIDs)

	// Assess-blocked command flags
	assessBlockedCmd.Flags().String("agent-id", "", "orchestrator agent ID (auto-resolved if not provided)")
	assessBlockedCmd.Flags().String("note", "", "optional note about the assessment outcome")
	assessBlockedCmd.Flags().String("reason", "", "current canonical blocked reason; requires --question")
	assessBlockedCmd.Flags().StringArray("question", nil, "current canonical blocked question (repeat 1-3 times with --reason)")
	assessBlockedCmd.Flags().String("repair-operation", "", "orchestrator-only repair operation requested for the current blocker")
	assessBlockedCmd.Flags().String("repair-target", "", "task or state object the requested repair should modify")
	assessBlockedCmd.Flags().String("repair-command", "", "exact command the orchestrator should run or adapt")
	assessBlockedCmd.Flags().StringArray("repair-evidence", nil, "evidence gathered before requesting orchestrator repair")
	assessBlockedCmd.Flags().StringArray("repair-validation", nil, "validation already run or required after orchestrator repair")
	assessBlockedCmd.Flags().String("repair-request-file", "", "path to a complete JSON repair request; mutually exclusive with --repair-* fields")
	assessBlockedCmd.Flags().StringSlice("awaits", nil, "existing unfinished task IDs the block waits for, all of them (comma-separated or repeated); wakes when all merge or one fails")
	assessBlockedCmd.Flags().Bool("clear-awaits", false, "drop the awaited set instead of carrying it forward; not with --awaits")
	registerCompletion(assessBlockedCmd, "agent-id", completeAgentIDs)

	// Assess-hypothesis-exhausted command flags
	assessHypothesisExhaustedCmd.Flags().String("agent-id", "", "orchestrator agent ID (auto-resolved if not provided)")
	assessHypothesisExhaustedCmd.Flags().String("note", "", "optional note about the assessment outcome")
	registerCompletion(assessHypothesisExhaustedCmd, "agent-id", completeAgentIDs)

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
	addTaskCmd.Flags().String("state", "", fmt.Sprintf("path to state.yaml (default: %s/state.yaml)", paths.ProjectDirName()))
	addTaskCmd.Flags().String("log", "", fmt.Sprintf("path to log.yaml (default: %s/log.yaml)", paths.ProjectDirName()))
	registerCompletion(addTaskCmd, "depends", completeTaskIDs)

	// Add-tasks (batch) command flags
	addAgentIDFlag(addTasksCmd)
	addTasksCmd.Flags().String("tasks-file", "", "path to JSON file with task definitions array (required)")

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

	// Delete task command flags
	deleteTaskCmd.Flags().Bool("force", false, "force deletion even if task has dependencies or is in restricted state")
	deleteTaskCmd.Flags().Bool("delete-worktree", false, "also delete the associated git worktree and branch")
	deleteTaskCmd.Flags().String("reason", "manual deletion", "reason for deleting the task")
}
