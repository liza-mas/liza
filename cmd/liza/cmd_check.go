package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/spf13/cobra"
)

// checkCommitAllowedCmd is the git pre-commit hook's decision backend for task
// worktrees. Exit codes are deliberately tri-state-collapsed-to-bi-state:
//
//	0 — allow: either policy permits the commit, or evaluation hit a fail-safe
//	    path (state unreadable, task not found, resolver unavailable).
//	1 — reject: policy definitively says the task is in a state that forbids
//	    coder commits (READY_FOR_REVIEW, REVIEWING, APPROVED, terminal, etc).
//
// No other exit codes are returned. This is load-bearing: future contributors
// must NOT introduce a "2 = unknown/error" code — doing so would flip the hook
// from fail-safe-allow to fail-safe-reject when state is briefly unreadable,
// blocking commits during normal concurrent writes.
var checkCommitAllowedCmd = &cobra.Command{
	Use:   "check-commit-allowed <task-id>",
	Short: "Decide whether a commit is allowed in a task worktree (pre-commit hook backend)",
	Long: `Evaluate whether a git commit should be allowed for the given task.

Used by the per-worktree pre-commit hook installed by wt-create. Exits 0 to
allow the commit and 1 to reject it. On any evaluation error the command exits
0 (fail-safe allow) — the hook is a guard against stale-state mutations, not
an authoritative lock.`,
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
			// Fail-safe: can't find project root, allow the commit.
			if isJSON(cmd) {
				return jsonout.WriteResult(os.Stdout, &ops.CheckCommitAllowedResult{
					Allowed: true,
					Reason:  "project root not found; fail-safe allow",
				}, nil, nil)
			}
			return nil
		}

		result := ops.CheckCommitAllowed(projectRoot, taskID)

		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, result, nil, nil)
		}

		if result.Allowed {
			return nil
		}

		fmt.Fprintln(os.Stderr, brand.BinaryName+": "+result.Reason)
		fmt.Fprintln(os.Stderr, brand.BinaryName+": if you must commit despite the task state, re-run with --no-verify.")
		// Return ErrAlreadyWritten so main() exits 1 without prefixing the
		// message with "Error: " (our two stderr lines above are the message).
		// Keeps cobra's normal error-return flow intact — deferred log
		// restorers still fire — unlike a bare os.Exit.
		return jsonout.ErrAlreadyWritten
	},
}

func init() {
	rootCmd.AddCommand(checkCommitAllowedCmd)
	addJSONFlag(checkCommitAllowedCmd)
}
