package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/spf13/cobra"
)

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

		if operatorInvocation(cmd) {
			projectRoot, err := requireProjectRoot()
			if err != nil {
				return err
			}
			actor := resolveChangedBy(cmd)
			invocation.calledOps = true
			if isJSON(cmd) {
				result, err := ops.CancelTaskWithOptions(projectRoot, taskID, reason, actor, requestOpts)
				return jsonout.WriteResult(os.Stdout, result, nil, err)
			}
			return commands.CancelTaskWithOptionsCommand(projectRoot, taskID, reason, actor, requestOpts)
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

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		var result *ops.ReconcileMergedResult
		if operatorInvocation(cmd) {
			result, err = ops.ReconcileMerged(projectRoot, taskID, mergeCommit, prURL, reason, resolveChangedBy(cmd))
		} else {
			authority, authErr := resolveOrchestratorAuthority(cmd)
			if authErr != nil {
				return authErr
			}
			resolver, rbacErr := loadResolverForRBAC(projectRoot)
			if rbacErr != nil {
				return rbacErr
			}
			if rbacErr := validateAllowedOperation(resolver, authority.ID, "reconcile-merged"); rbacErr != nil {
				return rbacErr
			}
			result, err = ops.ReconcileMergedWithAuthority(projectRoot, taskID, mergeCommit, prURL, reason, authority)
		}
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

// registerTaskRetirementCommands registers the task retirement commands; the single init in cmd_task.go calls it in the original registration order.
func registerTaskRetirementCommands() {
	addLifecycleFlags(cancelTaskCmd)
	rootCmd.AddCommand(cancelTaskCmd)
	rootCmd.AddCommand(reconcileMergedCmd)
	deleteCmd.AddCommand(deleteTaskCmd)

	cancelTaskCmd.ValidArgsFunction = completeTaskIDArgs(1)
	reconcileMergedCmd.ValidArgsFunction = completeTaskIDArgs(1)
	deleteTaskCmd.ValidArgsFunction = completeTaskIDArgs(1)

	addJSONFlag(cancelTaskCmd)
	addJSONFlag(reconcileMergedCmd)

	addAgentIDFlag(cancelTaskCmd)
	addAgentIDFlag(reconcileMergedCmd)
	reconcileMergedCmd.Flags().String("merge-commit", "", "merge commit that completed the task externally (required)")
	reconcileMergedCmd.Flags().String("pr-url", "", "pull request URL for the external merge")
	reconcileMergedCmd.Flags().String("reason", "", "reason for reconciliation (required)")
	reconcileMergedCmd.MarkFlagRequired("merge-commit")
	reconcileMergedCmd.MarkFlagRequired("reason")
	for _, cmd := range []*cobra.Command{cancelTaskCmd, reconcileMergedCmd} {
		addChangedByFlag(cmd)
		cmd.Long += "\n\n" + operatorInvocationHelp
	}

	// Delete task command flags
	deleteTaskCmd.Flags().Bool("force", false, "force deletion even if task has dependencies or is in restricted state")
	deleteTaskCmd.Flags().Bool("delete-worktree", false, "also delete the associated git worktree and branch")
	deleteTaskCmd.Flags().String("reason", "manual deletion", "reason for deleting the task")
}
