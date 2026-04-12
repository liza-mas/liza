package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"time"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/spf13/cobra"
)

var submitForReviewCmd = &cobra.Command{
	Use:   "submit-for-review <task-id> <commit-sha>",
	Short: "Submit a task for review",
	Long: `Validate a task worktree commit and submit it for review.

Used by doer agents to submit completed work for review.

Requirements:
  - Agent ID must be provided (via --agent-id flag or LIZA_AGENT_ID env var)
  - Task must be in an executing status (resolved from pipeline config)
  - Task must be assigned to the submitting agent
  - <commit-sha> must exactly match current worktree HEAD before rebase

Updates:
  - status = role-pair's submitted status (e.g. CODE_READY_FOR_REVIEW, CODING_PLAN_TO_REVIEW)
  - review_commit = post-rebase worktree HEAD
  - Adds history entry with event "submitted_for_review"`,
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
		commitSHA := args[1]

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
		if err := validateAllowedOperation(resolver, agentID, "submit-for-review"); err != nil {
			return err
		}

		if isJSON(cmd) {
			result, err := ops.SubmitForReview(projectRoot, taskID, commitSHA, agentID)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.SubmitForReviewCommand(projectRoot, taskID, commitSHA, agentID)
	},
}

var handoffCmd = &cobra.Command{
	Use:   "handoff <task-id> <summary> <next-action>",
	Short: "Initiate context-exhaustion handoff for a claimed task",
	Long: `Atomically initiate handoff when a doer agent is nearing context exhaustion.

Requirements:
  - Agent ID must be provided (via --agent-id flag or LIZA_AGENT_ID env var)
  - Task must be in an executing status (resolved from pipeline config)
  - Task must be assigned to the submitting agent

Updates:
  - task.handoff_pending = true
  - task history appends handoff_initiated event
  - handoff.<task-id> note is recorded with summary and next_action
  - agent status = HANDOFF`,
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

		taskID := args[0]
		summary := args[1]
		nextAction := args[2]

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
		if err := validateAllowedOperation(resolver, agentID, "handoff"); err != nil {
			return err
		}

		if isJSON(cmd) {
			result, err := ops.Handoff(&ops.HandoffInput{
				ProjectRoot: projectRoot,
				TaskID:      taskID,
				Summary:     summary,
				NextAction:  nextAction,
				AgentID:     agentID,
			})
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.HandoffCommand(projectRoot, &ops.HandoffInput{
			TaskID:     taskID,
			Summary:    summary,
			NextAction: nextAction,
			AgentID:    agentID,
		})
	},
}

var submitVerdictCmd = &cobra.Command{
	Use:   "submit-verdict <task-id> <APPROVED|REJECTED> [rejection-reason]",
	Short: "Submit a review verdict",
	Long: `Atomically submit a review verdict (APPROVED or REJECTED) for a task.

Used by reviewer agents to approve or reject work.

Requirements:
  - Agent ID must be provided (via --agent-id flag or LIZA_AGENT_ID env var)
  - Task must be in a reviewing status (resolved from pipeline config)
  - For REJECTED verdicts, a rejection reason is required

For APPROVED verdict:
  - status = role-pair's approved status (e.g. CODE_APPROVED, CODING_PLAN_APPROVED)
  - approved_by = <agent-id>
  - Clear rejection_reason
  - Clear reviewing_by and review_lease_expires
  - Add history entry with event "approved"

For REJECTED verdict:
  - status = role-pair's rejected status (e.g. CODE_REJECTED, CODING_PLAN_REJECTED)
  - rejection_reason = <reason>
  - Increment review_cycles_current and review_cycles_total
  - Clear reviewing_by and review_lease_expires
  - Add history entry with event "rejected" and reason`,
	Args: cobra.RangeArgs(2, 3),
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
		verdict := args[1]
		reason := ""
		if len(args) == 3 {
			reason = args[2]
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
		if err := validateAllowedOperation(resolver, agentID, "submit-verdict"); err != nil {
			return err
		}

		impact, _ := cmd.Flags().GetString("impact")

		if isJSON(cmd) {
			result, err := ops.SubmitVerdict(projectRoot, taskID, verdict, reason, agentID, impact)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.SubmitVerdictCommand(projectRoot, taskID, verdict, reason, agentID, impact)
	},
}

var releaseClaimCmd = &cobra.Command{
	Use:   "release-claim <task-id>",
	Short: "Manually release claims on a task",
	Long: `Manually release claims on a task (doer, reviewer, or both).

Used to release task claims manually when needed, such as when an agent
crashes or a lease needs to be freed.

Claim types:
  - reviewer: Release review claim (reviewing_by, review_lease_expires). Works for any reviewer role.
  - doer: Release doer claim (assigned_to, lease_expires) and reset to initial state. Works for any doer role.
  - both: Release both reviewer and doer claims

Safety:
  - By default, refuses to release claims with valid (non-expired) leases
  - Use --force to override lease expiry checks
  - Warns if no claims exist to release

Agent ID for audit trail:
  - Can be specified via --changed-by flag or LIZA_AGENT_ID env var
  - Defaults to "human" if not provided`,
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
		role, _ := cmd.Flags().GetString("role")
		force, _ := cmd.Flags().GetBool("force")
		reason, _ := cmd.Flags().GetString("reason")
		full, _ := cmd.Flags().GetBool("full")

		if full { // --full is an alias for --role both
			role = roles.ClaimBoth
		}

		agentID := resolveChangedBy(cmd)

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		if isJSON(cmd) {
			result, err := ops.ReleaseClaim(projectRoot, taskID, role, force, reason, agentID)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.ReleaseClaimCommand(projectRoot, taskID, role, force, reason, agentID)
	},
}

var awaitVerdictCmd = &cobra.Command{
	Use:   "await-verdict <task-id>",
	Short: "Block until a review verdict arrives for a submitted task",
	Long: `Block until a reviewer approves or rejects a submitted task.

Used by doer agents after submit-for-review to wait for the review outcome.

Requirements:
  - Agent ID must be provided (via --agent-id flag or LIZA_AGENT_ID env var)
  - Task must be in a submitted/reviewing status

Possible outcomes:
  - APPROVED: work accepted, agent can exit
  - REJECTED: work needs revision, reason provided
  - TIMEOUT: no verdict within timeout period
  - NEW_ATTEMPT: task reassigned for fresh attempt
  - ABORTED: task was superseded or cancelled`,
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
		if err := validateAllowedOperation(resolver, agentID, "await-verdict"); err != nil {
			return err
		}

		timeoutSec, _ := cmd.Flags().GetInt("timeout-seconds")
		timeout := time.Duration(timeoutSec) * time.Second

		if isJSON(cmd) {
			result, err := ops.AwaitVerdict(context.Background(), projectRoot, taskID, agentID, timeout)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.AwaitVerdictCommand(projectRoot, taskID, agentID, timeout)
	},
}

var awaitResubmissionCmd = &cobra.Command{
	Use:   "await-resubmission <task-id>",
	Short: "Block until a doer resubmits after a rejection",
	Long: `Block until a doer agent resubmits work after a reviewer rejected it.

Used by reviewer agents after submit-verdict REJECTED to wait for the revised submission.

Requirements:
  - Agent ID must be provided (via --agent-id flag or LIZA_AGENT_ID env var)
  - Task must have been rejected by the calling reviewer

Possible outcomes:
  - RESUBMITTED: doer submitted new changes, reviewer should re-review
  - TIMEOUT: no resubmission within timeout period
  - TERMINAL: task reached a terminal state (superseded, abandoned)
  - ABORTED: task was cancelled or reassigned`,
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
		if err := validateAllowedOperation(resolver, agentID, "await-resubmission"); err != nil {
			return err
		}

		timeoutSec, _ := cmd.Flags().GetInt("timeout-seconds")
		timeout := time.Duration(timeoutSec) * time.Second

		if isJSON(cmd) {
			result, err := ops.AwaitResubmission(context.Background(), projectRoot, taskID, agentID, timeout)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.AwaitResubmissionCommand(projectRoot, taskID, agentID, timeout)
	},
}

func init() {
	rootCmd.AddCommand(submitForReviewCmd)
	rootCmd.AddCommand(handoffCmd)
	rootCmd.AddCommand(submitVerdictCmd)
	rootCmd.AddCommand(releaseClaimCmd)
	rootCmd.AddCommand(awaitVerdictCmd)
	rootCmd.AddCommand(awaitResubmissionCmd)

	addAgentIDFlag(submitForReviewCmd)
	addAgentIDFlag(handoffCmd)
	addAgentIDFlag(submitVerdictCmd)
	addChangedByFlag(releaseClaimCmd)

	// Await-verdict flags
	addAgentIDFlag(awaitVerdictCmd)
	awaitVerdictCmd.Flags().Int("timeout-seconds", 1500, "total blocking timeout in seconds")

	// Await-resubmission flags
	addAgentIDFlag(awaitResubmissionCmd)
	awaitResubmissionCmd.Flags().Int("timeout-seconds", 1500, "total blocking timeout in seconds")

	// Submit-verdict flags
	submitVerdictCmd.Flags().String("impact", "", "impact classification (standard, significant, architecture)")

	// Release-claim command flags
	releaseClaimCmd.Flags().String("role", roles.ClaimReviewer, "claim type to release (doer, reviewer, both)")
	releaseClaimCmd.Flags().Bool("full", false, "release both doer and reviewer claims (alias for --role both)")
	releaseClaimCmd.Flags().Bool("force", false, "force release even if lease is still valid")
	releaseClaimCmd.Flags().String("reason", "manual release", "reason for releasing the claim")

	// JSON output flags
	addJSONFlag(submitForReviewCmd)
	addJSONFlag(handoffCmd)
	addJSONFlag(submitVerdictCmd)
	addJSONFlag(releaseClaimCmd)
	addJSONFlag(awaitVerdictCmd)
	addJSONFlag(awaitResubmissionCmd)
}
