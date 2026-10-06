package commands

import (
	"context"
	"log"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// AwaitVerdictResult adds the remaining total wait budget to a POLL outcome.
type AwaitVerdictResult struct {
	*ops.AwaitVerdictResult
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

var runAwaitVerdictWithAuthorityOptions = ops.AwaitVerdictWithAuthorityOptions

// AwaitVerdictWithAuthority applies the bounded foreground interval while
// preserving caller-held generation authority through ownership and cleanup.
func AwaitVerdictWithAuthority(projectRoot, taskID string, authority models.AgentAuthority, budget time.Duration) (*AwaitVerdictResult, error) {
	return AwaitVerdictWithAuthorityOptions(projectRoot, taskID, authority, budget, AwaitVerdictOptions{})
}

// AwaitVerdictWithAuthorityOptions is AwaitVerdictWithAuthority with
// configurable polling intervals.
func AwaitVerdictWithAuthorityOptions(projectRoot, taskID string, authority models.AgentAuthority, budget time.Duration, opts AwaitVerdictOptions) (*AwaitVerdictResult, error) {
	remaining := ops.AwaitVerdictRemainingBudget(projectRoot, taskID, authority.ID, budget)
	result, err := awaitVerdictWithBudget(remaining, agentAwaitInterval(projectRoot, authority.ID), func(interval time.Duration) (*ops.AwaitVerdictResult, error) {
		return runAwaitVerdictWithAuthorityOptions(context.Background(), projectRoot, taskID, authority, interval, opts)
	})
	if err == nil && result.Verdict == ops.VerdictTimeout {
		if releaseErr := ops.ReleaseDepartedDoerAssignmentWithAuthority(projectRoot, taskID, authority); releaseErr != nil {
			return result, releaseErr
		}
	}
	return result, err
}

// AwaitVerdictOptions configures the operation's periodic checks.
type AwaitVerdictOptions = ops.AwaitVerdictOptions

// AwaitVerdict applies the bounded foreground interval to one verdict wait.
// budget is the total wait allowance, not a remainder the caller tracks: the
// remaining share is derived from the submission recorded in task history, so
// repeated invocations converge on TIMEOUT whether or not the caller passes the
// reported remainder back.
func AwaitVerdict(projectRoot, taskID, agentID string, budget time.Duration) (*AwaitVerdictResult, error) {
	return AwaitVerdictWithOptions(projectRoot, taskID, agentID, budget, AwaitVerdictOptions{})
}

// AwaitVerdictWithOptions is AwaitVerdict with configurable polling intervals.
func AwaitVerdictWithOptions(projectRoot, taskID, agentID string, budget time.Duration, opts AwaitVerdictOptions) (*AwaitVerdictResult, error) {
	remaining := ops.AwaitVerdictRemainingBudget(projectRoot, taskID, agentID, budget)
	return awaitVerdictWithIntervalAndOptions(projectRoot, taskID, agentID, remaining, agentAwaitInterval(projectRoot, agentID), opts)
}

func awaitVerdictWithInterval(
	projectRoot, taskID, agentID string,
	remaining, maxInterval time.Duration,
) (*AwaitVerdictResult, error) {
	return awaitVerdictWithIntervalAndOptions(projectRoot, taskID, agentID, remaining, maxInterval, AwaitVerdictOptions{})
}

func awaitVerdictWithIntervalAndOptions(
	projectRoot, taskID, agentID string,
	remaining, maxInterval time.Duration,
	opts AwaitVerdictOptions,
) (*AwaitVerdictResult, error) {
	result, err := awaitVerdictWithBudget(remaining, maxInterval, func(interval time.Duration) (*ops.AwaitVerdictResult, error) {
		return ops.AwaitVerdictWithOptions(context.Background(), projectRoot, taskID, agentID, interval, opts)
	})
	if err == nil && result.Verdict == ops.VerdictTimeout {
		releaseExhaustedDoerClaim(projectRoot, taskID, agentID)
	}
	return result, err
}

// releaseExhaustedDoerClaim drops the doer's assignment when the wait budget runs
// out. The session ends here and never resumes, so the assignment would otherwise
// pin the task to a departed agent until its lease lapses. Only the assignment and
// lease go: the submitted attempt (worktree, commits, review boundary) belongs to
// the review still in flight, not to the doer.
// Best-effort: a failure here costs a delayed reclaim, not correctness, and must
// not mask the TIMEOUT the caller needs to act on.
func releaseExhaustedDoerClaim(projectRoot, taskID, agentID string) {
	if err := ops.ReleaseDepartedDoerAssignment(projectRoot, taskID, agentID); err != nil {
		log.Printf("WARN: could not release doer assignment for %s after budget exhaustion: %v", taskID, err)
	}
}

func awaitVerdictWithBudget(
	remaining time.Duration,
	maxInterval time.Duration,
	await func(time.Duration) (*ops.AwaitVerdictResult, error),
) (*AwaitVerdictResult, error) {
	result, timeoutSeconds, err := awaitWithBudget(
		remaining,
		maxInterval,
		await,
		func(result *ops.AwaitVerdictResult) bool { return result.Verdict == ops.VerdictTimeout },
		func(result *ops.AwaitVerdictResult) { result.Verdict = ops.VerdictPoll },
	)
	if err != nil {
		return nil, err
	}
	if result.Verdict != ops.VerdictPoll {
		timeoutSeconds = 0
	}
	return &AwaitVerdictResult{AwaitVerdictResult: result, TimeoutSeconds: timeoutSeconds}, nil
}
