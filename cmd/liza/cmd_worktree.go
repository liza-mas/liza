package main

import (
	"errors"
	"io"
	"log"
	"os"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/spf13/cobra"
)

var wtCreateCmd = &cobra.Command{
	Use:   "wt-create <task-id>",
	Short: "Create a worktree for a claimed task",
	Long: `Create a git worktree for a task in executing status from the integration branch.

The worktree is created in .worktrees/<task-id> and a new branch task/<task-id>
is created from the integration branch. The task's base_commit is recorded for
drift tracking.

If the worktree already exists and --fresh is not specified, the command succeeds
without error. With --fresh, any existing worktree is deleted before creating
a new one (useful for task reassignment).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		taskID := args[0]
		fresh, _ := cmd.Flags().GetBool("fresh")

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
			result, err := ops.CreateWorktree(projectRoot, taskID, fresh)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.WtCreateCommand(projectRoot, taskID, fresh)
	},
}

var wtDeleteCmd = &cobra.Command{
	Use:   "wt-delete <task-id>",
	Short: "Delete a worktree for a completed/abandoned task",
	Long: `Delete a git worktree and branch for a task.

For safety, deletion is only allowed for tasks in the following states:
  - BLOCKED: Task is blocked and cannot proceed
  - ABANDONED: Task has been abandoned
  - SUPERSEDED: Task has been superseded by another task
  - MERGED: Task is complete (worktree should already be cleaned)

This prevents accidental destruction of in-progress work. If the task is in
an executing or submitted status, deletion is not allowed as the coder may be
actively working in the worktree.

The worktree directory and branch are removed, and task.worktree is set to null.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		taskID := args[0]

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
			result, err := ops.DeleteWorktree(projectRoot, taskID)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.WtDeleteCommand(projectRoot, taskID)
	},
}

var wtMergeCmd = &cobra.Command{
	Use:   "wt-merge <task-id>",
	Short: "Merge an approved task into the integration branch",
	Long: `Merge an approved task's worktree into the integration branch.

This is the final step in the task lifecycle, integrating completed and approved
work back into the main codebase.

Requirements:
  - Task must be in an approved status (resolved from pipeline config or legacy statuses)
  - Agent ID must be provided (via --agent-id flag or LIZA_AGENT_ID env var)
  - Agent must be a reviewer role (code-reviewer, code-plan-reviewer, epic-plan-reviewer, or us-reviewer)
  - Worktree HEAD must match the task's review_commit

Process:
  1. Validates task status and review_commit
  2. Attempts fast-forward merge (or merge commit if needed)
  3. Handles merge conflicts by marking task as INTEGRATION_FAILED
  4. Runs integration tests if scripts/integration-test.sh exists
  5. On success: cleans up worktree, marks task as MERGED, updates sprint metrics
  6. On failure: preserves worktree for conflict resolution

The worktree and branch are automatically cleaned up after a successful merge.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		taskID := args[0]

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
		if err := validateRoleType(resolver, agentID, "reviewer"); err != nil {
			return err
		}

		if isJSON(cmd) {
			result, err := ops.MergeWorktree(projectRoot, taskID, agentID)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.WtMergeCommand(projectRoot, taskID, agentID)
	},
}

func init() {
	rootCmd.AddCommand(wtCreateCmd)
	rootCmd.AddCommand(wtDeleteCmd)
	rootCmd.AddCommand(wtMergeCmd)

	addAgentIDFlag(wtMergeCmd)

	// Wt-create command flags
	wtCreateCmd.Flags().Bool("fresh", false, "delete existing worktree before creating (for task reassignment)")

	// JSON output flags
	addJSONFlag(wtCreateCmd)
	addJSONFlag(wtDeleteCmd)
	addJSONFlag(wtMergeCmd)
}
