package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/liza-mas/liza/internal/statehygiene"
	"github.com/spf13/cobra"
)

// awaitBudgetSecondsDefault and awaitBudgetFlagUsage derive from the single
// ops-side ceiling so the flag default and the enforced maximum cannot drift.
var awaitBudgetSecondsDefault = int(ops.DefaultAwaitBudget.Seconds())

var awaitBudgetFlagUsage = fmt.Sprintf(
	"total wait budget in seconds, measured from submission/rejection; "+
		"each invocation waits at most 100 seconds; maximum %d", awaitBudgetSecondsDefault)

var awaitVerdict = commands.AwaitVerdictWithAuthority
var awaitResubmission = commands.AwaitResubmissionWithAuthority

var submitForReviewCmd = &cobra.Command{
	Use:   "submit-for-review <task-id> [commit-ref]",
	Short: "Submit a task for review",
	Long: `Validate a task worktree commit and submit it for review.

Used by doer agents to submit completed work for review.

Requirements:
  - Agent ID must be provided (via --agent-id flag or ` + brand.EnvName("AGENT_ID") + ` env var)
  - Task must be in an executing status (resolved from pipeline config)
  - Task must be assigned to the submitting agent
  - [commit-ref] resolves in the task worktree and must match current worktree HEAD before rebase
  - If [commit-ref] is omitted, HEAD is used

Updates:
  - status = role-pair's submitted status (e.g. CODE_READY_FOR_REVIEW, CODING_PLAN_TO_REVIEW)
  - review_commit = post-rebase worktree HEAD
  - Adds history entry with event "submitted_for_review"`,
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
		commitRef := "HEAD"
		if len(args) == 2 {
			commitRef = args[1]
		}

		authority, err := requireAgentAuthority(cmd)
		if err != nil {
			return err
		}
		agentID := authority.ID

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
			result, err := ops.SubmitForReviewWithAuthority(projectRoot, taskID, commitRef, authority)
			var warnings []string
			if result != nil {
				warnings = result.Warnings
			}
			return jsonout.WriteResult(os.Stdout, result, warnings, err)
		}
		return commands.SubmitForReviewCommandWithAuthority(projectRoot, taskID, commitRef, authority)
	},
}

var handoffCmd = &cobra.Command{
	Use:   "handoff <task-id> <summary> <next-action>",
	Short: "Initiate context-exhaustion handoff for a claimed task",
	Long: `Atomically initiate handoff when a doer agent is nearing context exhaustion.

Requirements:
  - Agent ID must be provided (via --agent-id flag or ` + brand.EnvName("AGENT_ID") + ` env var)
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

		authority, err := requireAgentAuthority(cmd)
		if err != nil {
			return err
		}
		agentID := authority.ID

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
				Authority:   &authority,
			})
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.HandoffCommand(projectRoot, &ops.HandoffInput{
			TaskID:     taskID,
			Summary:    summary,
			NextAction: nextAction,
			AgentID:    agentID,
			Authority:  &authority,
		})
	},
}

var submitVerdictCmd = &cobra.Command{
	Use:   "submit-verdict <task-id> <APPROVED|REJECTED> [rejection-reason]",
	Short: "Submit a review verdict",
	Long: `Atomically submit a review verdict (APPROVED or REJECTED) for a task.

Used by reviewer agents to approve or reject work.

Requirements:
  - Agent ID must be provided (via --agent-id flag or ` + brand.EnvName("AGENT_ID") + ` env var)
  - Task must be in a reviewing status (resolved from pipeline config)
  - --review-commit must be the full immutable commit SHA actually reviewed
  - For REJECTED verdicts, a rejection reason of at most 4096 bytes is required
    (via --reason, --reason-file, or positional arg)

A generation-fenced substantive verdict is retained as quarantined evidence;
it cannot change task or agent state. Conflicting evidence requires an
authorized orchestrator's reconcile-verdict decision before approval or merge.

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
		reason, err := verdictReason(cmd, args)
		if err != nil {
			return err
		}

		authority, err := requireAgentAuthority(cmd)
		if err != nil {
			return err
		}
		agentID := authority.ID

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
		reviewCommit, _ := cmd.Flags().GetString("review-commit")

		if isJSON(cmd) {
			result, err := ops.SubmitVerdictWithAuthority(projectRoot, taskID, verdict, reason, authority, impact, reviewCommit)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.SubmitVerdictCommandWithAuthority(projectRoot, taskID, verdict, reason, authority, impact, reviewCommit)
	},
}

func verdictReason(cmd *cobra.Command, args []string) (string, error) {
	reason := ""
	if len(args) == 3 {
		reason = args[2]
	}

	// --reason overrides the positional argument for backward compatibility.
	if flagReason, _ := cmd.Flags().GetString("reason"); flagReason != "" {
		reason = flagReason
	}

	reasonFile, _ := cmd.Flags().GetString("reason-file")
	if reasonFile == "" {
		return reason, nil
	}
	if len(args) == 3 || cmd.Flags().Changed("reason") {
		return "", cliValidationError("--reason-file cannot be combined with a positional rejection reason or --reason")
	}

	reader := cmd.InOrStdin()
	var file *os.File
	if reasonFile != "-" {
		var err error
		file, err = os.Open(reasonFile)
		if err != nil {
			return "", cliValidationWrap("opening --reason-file", err)
		}
		defer file.Close()
		reader = file
	}

	data, err := io.ReadAll(io.LimitReader(reader, statehygiene.MaxStateTextBytes+1))
	if err != nil {
		return "", cliValidationWrap("reading --reason-file", err)
	}
	if len(data) > statehygiene.MaxStateTextBytes {
		return "", cliValidationError(fmt.Sprintf(
			"rejection reason exceeds %d-byte limit", statehygiene.MaxStateTextBytes,
		))
	}
	return string(data), nil
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
  - Can be specified via --changed-by flag or ` + brand.EnvName("AGENT_ID") + ` env var
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

// awaitBudgetFromFlag reads --timeout-seconds as a total wait allowance and
// refuses a value above the hard ceiling rather than silently shortening it. The
// ops layer clamps as well, as defence for non-CLI callers; at this boundary an
// operator who asks for longer is told instead of quietly given less.
func awaitBudgetFromFlag(cmd *cobra.Command) (time.Duration, error) {
	seconds, _ := cmd.Flags().GetInt("timeout-seconds")
	budget := time.Duration(seconds) * time.Second
	if budget > ops.DefaultAwaitBudget {
		return 0, fmt.Errorf(
			"--timeout-seconds %d exceeds the %d-second maximum wait budget",
			seconds, int(ops.DefaultAwaitBudget.Seconds()))
	}
	return budget, nil
}

var awaitVerdictCmd = &cobra.Command{
	Use:   "await-verdict <task-id>",
	Short: "Block until a review verdict arrives for a submitted task",
	Long: `Block until a reviewer approves or rejects a submitted task.

Used by doer agents after submit-for-review to wait for the review outcome.

Requirements:
  - Agent ID must be provided (via --agent-id flag or ` + brand.EnvName("AGENT_ID") + ` env var)
  - Task must be in a submitted/reviewing status

Possible outcomes:
  - APPROVED: work accepted, agent can exit
  - REJECTED: work needs revision, reason provided
  - ALREADY_TRANSITIONED: verdict was recovered after task moved onward; follow safe_action
  - POLL: the 100-second call cap expired; run the same command again (no argument to carry over)
  - TIMEOUT: the total wait budget expired without a verdict
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

		authority, err := requireAgentAuthority(cmd)
		if err != nil {
			return err
		}
		agentID := authority.ID

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

		timeout, err := awaitBudgetFromFlag(cmd)
		if err != nil {
			return err
		}

		result, err := awaitVerdict(projectRoot, taskID, authority, timeout)
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		if err != nil {
			return fmt.Errorf("await verdict: %w", err)
		}
		printAwaitVerdictResult(result)
		return nil
	},
}

var awaitResubmissionCmd = &cobra.Command{
	Use:   "await-resubmission <task-id>",
	Short: "Block until a doer resubmits after a rejection",
	Long: `Block until a doer agent resubmits work after a reviewer rejected it.

Used by reviewer agents after submit-verdict REJECTED to wait for the revised submission.

Requirements:
  - Agent ID must be provided (via --agent-id flag or ` + brand.EnvName("AGENT_ID") + ` env var)
  - Task must have been rejected by the calling reviewer

Possible outcomes:
  - RESUBMITTED: doer submitted new changes; use returned base_commit..review_commit for re-review
  - POLL: the 100-second call cap expired; run the same command again (no argument to carry over)
  - TIMEOUT: the total wait budget expired without a resubmission
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

		authority, err := requireAgentAuthority(cmd)
		if err != nil {
			return err
		}
		agentID := authority.ID

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

		timeout, err := awaitBudgetFromFlag(cmd)
		if err != nil {
			return err
		}

		result, err := awaitResubmission(projectRoot, taskID, authority, timeout)
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		if err != nil {
			return fmt.Errorf("await resubmission: %w", err)
		}
		printAwaitResubmissionResult(result)
		return nil
	},
}

func printAwaitVerdictResult(result *commands.AwaitVerdictResult) {
	fmt.Printf("Verdict: %s\nStatus: %s\n", result.Verdict, result.TaskStatus)
	if result.Reason != "" {
		fmt.Printf("Reason: %s\n", result.Reason)
	}
	if result.ReviewerAgent != "" {
		fmt.Printf("Reviewer: %s\n", result.ReviewerAgent)
	}
	if result.ReviewCommit != "" {
		fmt.Printf("Review commit: %s\n", result.ReviewCommit)
	}
	if result.CurrentAssignee != "" {
		fmt.Printf("Current assignee: %s\n", result.CurrentAssignee)
	}
	if result.SafeAction != "" {
		fmt.Printf("Safe action: %s\n", result.SafeAction)
	}
	if result.TimeoutSeconds > 0 {
		fmt.Printf("Timeout seconds: %d\n", result.TimeoutSeconds)
	}
	if result.Guidance != "" {
		fmt.Printf("\n%s\n", result.Guidance)
	}
}

func printAwaitResubmissionResult(result *commands.AwaitResubmissionResult) {
	fmt.Printf("Verdict: %s\nStatus: %s\n", result.Verdict, result.TaskStatus)
	if result.ReviewCommit != "" {
		if result.BaseCommit != "" {
			fmt.Printf("Base commit: %s\n", result.BaseCommit)
		}
		fmt.Printf("Review commit: %s\nReview cycle: %d\n", result.ReviewCommit, result.ReviewCycle)
	}
	if result.Reason != "" {
		fmt.Printf("Reason: %s\n", result.Reason)
	}
	if result.TimeoutSeconds > 0 {
		fmt.Printf("Timeout seconds: %d\n", result.TimeoutSeconds)
	}
}

// updateReviewCommitCmd is RBAC-exempt (--changed-by): operator recovery action,
// same category as release-claim. See specs/goals/20260412-cli-native-access-control.md.
var updateReviewCommitCmd = &cobra.Command{
	Use:   "update-review-commit <task-id>",
	Short: "Update review boundary to current worktree HEAD after external rebase",
	Long: `Update a task's review boundary to the current worktree HEAD.

	Use this after manually rebasing a worktree that is already submitted for review,
	or to repair a submitted task whose review_commit is missing while its worktree exists.
	This is an explicit resubmission boundary: if a reviewer has claimed the task,
	their claim is released and the task returns to submitted state for a fresh review.
	The command updates review_commit to worktree HEAD and base_commit to the
	effective merge-base between worktree HEAD and the configured integration branch.

Requirements:
	  - Task must be in a submitted or reviewing state
	  - Worktree must exist on disk
	  - review_commit may be missing, or review_commit/base_commit must differ from the current effective review boundary`,
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
		changedBy := resolveChangedBy(cmd)

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		if isJSON(cmd) {
			result, err := ops.UpdateReviewCommit(projectRoot, taskID, changedBy)
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return commands.UpdateReviewCommitCommand(projectRoot, taskID, changedBy)
	},
}

func init() {
	rootCmd.AddCommand(submitForReviewCmd)
	rootCmd.AddCommand(handoffCmd)
	rootCmd.AddCommand(submitVerdictCmd)
	rootCmd.AddCommand(releaseClaimCmd)
	rootCmd.AddCommand(awaitVerdictCmd)
	rootCmd.AddCommand(awaitResubmissionCmd)
	rootCmd.AddCommand(updateReviewCommitCmd)
	submitForReviewCmd.ValidArgsFunction = completeTaskIDArgs(1)
	handoffCmd.ValidArgsFunction = completeTaskIDArgs(1)
	submitVerdictCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return completeTaskIDs(cmd, args, toComplete)
		}
		if len(args) == 1 {
			return completeValues("APPROVED", "REJECTED")(cmd, args, toComplete)
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	releaseClaimCmd.ValidArgsFunction = completeTaskIDArgs(1)
	awaitVerdictCmd.ValidArgsFunction = completeTaskIDArgs(1)
	awaitResubmissionCmd.ValidArgsFunction = completeTaskIDArgs(1)
	updateReviewCommitCmd.ValidArgsFunction = completeTaskIDArgs(1)

	addAgentIDFlag(submitForReviewCmd)
	addAgentIDFlag(handoffCmd)
	addAgentIDFlag(submitVerdictCmd)
	addChangedByFlag(releaseClaimCmd)
	addChangedByFlag(updateReviewCommitCmd)

	// Await-verdict flags
	addAgentIDFlag(awaitVerdictCmd)
	awaitVerdictCmd.Flags().Int("timeout-seconds", awaitBudgetSecondsDefault, awaitBudgetFlagUsage)

	// Await-resubmission flags
	addAgentIDFlag(awaitResubmissionCmd)
	awaitResubmissionCmd.Flags().Int("timeout-seconds", awaitBudgetSecondsDefault, awaitBudgetFlagUsage)

	// Submit-verdict flags
	submitVerdictCmd.Flags().String("review-commit", "", "required full commit SHA actually reviewed")
	submitVerdictCmd.Flags().String("impact", "", "impact classification (standard, significant, architecture)")
	submitVerdictCmd.Flags().String("reason", "", "rejection reason, at most 4096 bytes (alternative to positional argument, avoids shell quoting issues)")
	submitVerdictCmd.Flags().String("reason-file", "", "read rejection reason from a file, or - for stdin (mutually exclusive with --reason and positional argument)")

	// Release-claim command flags
	releaseClaimCmd.Flags().String("role", roles.ClaimReviewer, "claim type to release (doer, reviewer, both)")
	releaseClaimCmd.Flags().Bool("full", false, "release both doer and reviewer claims (alias for --role both)")
	registerCompletion(releaseClaimCmd, "role", completeValues(roles.ClaimDoer, roles.ClaimReviewer, roles.ClaimBoth))
	releaseClaimCmd.Flags().Bool("force", false, "force release even if lease is still valid")
	releaseClaimCmd.Flags().String("reason", "manual release", "reason for releasing the claim")

	// JSON output flags
	addJSONFlag(submitForReviewCmd)
	addJSONFlag(handoffCmd)
	addJSONFlag(submitVerdictCmd)
	addJSONFlag(releaseClaimCmd)
	addJSONFlag(awaitVerdictCmd)
	addJSONFlag(awaitResubmissionCmd)
	addJSONFlag(updateReviewCommitCmd)
}
