package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/spf13/cobra"
)

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
  - --changed is required when replacing a BLOCKED task: state what differs
    from the blocked attempt. Agents may replace a lineage's BLOCKED tasks at
    most %[4]d times; the next replacement is refused until a human releases
    that blocked episode (release-blocked-chain, or resolving the %[5]s
    circuit-breaker halt)

Replacement task IDs are optional and should be comma-separated.
When no replacements are given, the task's branch is deleted immediately after
recording pre-supersession branch/worktree evidence and the operator-provided
recoverability audit command. %[3]s records that command but does not execute it.

Examples:
  %[1]s task-3 task-4,task-5 --reason "Split into smaller tasks" --changed "Interface contract now fixed by task-2"
  %[1]s task-3 --reason "Work already merged in prior sprint" --recoverability-command "%[2]s"`, brand.Command("supersede-task"), brand.Command("recover-task", "task-3"), brand.NameTitle, models.MaxAgentBlockedRecoveries, models.BlockedReplacementChainPattern),
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
		changed, _ := cmd.Flags().GetString("changed")

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
				Changed:               changed,
			})
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.SupersedeTaskWithAuthorityAndOptionsCommand(projectRoot, taskID, replacementIDs, reason, recoverabilityCommand, changed, authority, requestOpts)
	},
}

var retargetDependencyCmd = &cobra.Command{
	Use:   "retarget-dependency <task-id> <old-dep-id> <new-dep-ids> --reason <reason>",
	Short: "Retarget one non-terminal task dependency edge",
	Long: fmt.Sprintf(`Retarget one non-terminal task's direct depends_on edge.

This is a metadata repair operation for cases where one task
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

		if operatorInvocation(cmd) {
			projectRoot, err := requireProjectRoot()
			if err != nil {
				return err
			}
			actor := resolveChangedBy(cmd)
			invocation.calledOps = true
			if isJSON(cmd) {
				result, err := ops.RetargetDependencyWithOptions(projectRoot, taskID, oldDependency, newDependencies, reason, actor, requestOpts)
				if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
					writeRetargetDependencyVerboseDiagnostic(os.Stderr, err)
				}
				return jsonout.WriteResult(os.Stdout, result, resultWarnings(result), err)
			}
			return commands.RetargetDependencyWithOptionsCommand(projectRoot, taskID, oldDependency, newDependencies, reason, actor, requestOpts)
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

This operation verifies every expected dependency list, computes
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

		if operatorInvocation(cmd) {
			projectRoot, err := requireProjectRoot()
			if err != nil {
				return err
			}
			actor := resolveChangedBy(cmd)
			invocation.calledOps = true
			if isJSON(cmd) {
				result, err := ops.ApplyDependencyRepairWithOptions(projectRoot, sourceTaskID, reason, actor, requestOpts)
				return jsonout.WriteResult(os.Stdout, result, resultWarnings(result), err)
			}
			return commands.ApplyDependencyRepairWithOptionsCommand(projectRoot, sourceTaskID, reason, actor, requestOpts)
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

This repair preserves legal dependencies and all terminal task
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

		if operatorInvocation(cmd) {
			projectRoot, err := requireProjectRoot()
			if err != nil {
				return err
			}
			actor := resolveChangedBy(cmd)
			invocation.calledOps = true
			if isJSON(cmd) {
				result, err := ops.RepairSupersededDependenciesWithOptions(projectRoot, taskID, reason, actor, requestOpts)
				return jsonout.WriteResult(os.Stdout, result, resultWarnings(result), err)
			}
			return commands.RepairSupersededDependenciesWithOptionsCommand(projectRoot, taskID, reason, actor, requestOpts)
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

// registerTaskDependencyCommands registers the task dependency repair commands; the single init in cmd_task.go calls it in the original registration order.
func registerTaskDependencyCommands() {
	for _, cmd := range []*cobra.Command{supersedeTaskCmd, retargetDependencyCmd, narrowInheritedDependenciesCmd, applyDependencyRepairCmd, repairSupersededDependenciesCmd} {
		addLifecycleFlags(cmd)
	}
	rootCmd.AddCommand(supersedeTaskCmd)
	rootCmd.AddCommand(retargetDependencyCmd)
	rootCmd.AddCommand(narrowInheritedDependenciesCmd)
	rootCmd.AddCommand(applyDependencyRepairCmd)
	rootCmd.AddCommand(repairSupersededDependenciesCmd)

	supersedeTaskCmd.ValidArgsFunction = completeTaskIDArgs(2)
	retargetDependencyCmd.ValidArgsFunction = completeTaskIDArgs(3)
	narrowInheritedDependenciesCmd.ValidArgsFunction = completeTaskIDArgs(1)
	applyDependencyRepairCmd.ValidArgsFunction = completeTaskIDArgs(1)
	repairSupersededDependenciesCmd.ValidArgsFunction = completeTaskIDArgs(1)

	addJSONFlag(supersedeTaskCmd)
	addJSONFlag(retargetDependencyCmd)
	addJSONFlag(narrowInheritedDependenciesCmd)
	addJSONFlag(applyDependencyRepairCmd)
	addJSONFlag(repairSupersededDependenciesCmd)

	addAgentIDFlag(supersedeTaskCmd)
	supersedeTaskCmd.Flags().String("reason", "", "reason for superseding (required)")
	supersedeTaskCmd.Flags().String("recoverability-command", "", "operator audit command recorded before superseding without replacements")
	supersedeTaskCmd.Flags().String("changed", "", "what differs from the blocked attempt (required when replacing a BLOCKED task)")
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
	for _, cmd := range []*cobra.Command{retargetDependencyCmd, repairSupersededDependenciesCmd, applyDependencyRepairCmd} {
		addChangedByFlag(cmd)
		cmd.Long += "\n\n" + operatorInvocationHelp
	}
}
