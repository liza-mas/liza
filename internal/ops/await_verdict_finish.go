package ops

import (
	stderrors "errors"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

const (
	// awaitBudgetExhaustedReason is the audit reason when a wait ends at budget
	// exhaustion and the departed doer's assignment is dropped.
	awaitBudgetExhaustedReason = "await budget exhausted; doer session ended"

	// awaitUnfencedSubmissionIndex marks a release that is not bound to one
	// submission of the await it ends.
	awaitUnfencedSubmissionIndex = -1
)

// DefaultAwaitBudget is the total wait allowance for one await, and also the
// hard ceiling: a caller cannot raise it. Without the ceiling, an invocation
// passing an ever-larger total would offset its own elapsed time and hold the
// remainder constant, so the derived bound would stop being monotonic.
const DefaultAwaitBudget = 30 * time.Minute

// awaitBudgetExhausted simulates what a rejection would do: when the limits are
// already at capacity the await returns at once instead of blocking for the full
// budget only to discover that the doer cannot iterate.
func awaitBudgetExhausted(task *models.Task, config models.Config) bool {
	_, exhausted := classifyLimitEscalation(
		task.ReviewCyclesCurrent, effectiveReviewCycleLimit(config),
		task.Iteration, effectiveCoderIterationLimit(task, config),
		task.EffectiveAttempt(),
	)
	return exhausted
}

// releaseOwnership clears the agent's CurrentTask, relinquishing ownership
// of the task. Status is left unchanged — the supervisor's resetAgentAfterExit
// handles status transitions when the CLI session ends.
func releaseOwnership(bb *db.Blackboard, agentID string, authority *models.AgentAuthority) error {
	return modifyLifecycleState(bb, authority, func(s *models.State) error {
		if agent, ok := s.Agents[agentID]; ok {
			agent.CurrentTask = nil
			s.Agents[agentID] = agent
		}
		return nil
	})
}

func finishAwaitVerdict(
	bb *db.Blackboard,
	agentID string,
	authority *models.AgentAuthority,
	result *AwaitVerdictResult,
	primaryErr error,
) (*AwaitVerdictResult, error) {
	return result, joinAwaitCleanupError(primaryErr, releaseOwnership(bb, agentID, authority))
}

func (r awaitVerdictRun) finish(result *AwaitVerdictResult, err error) (*AwaitVerdictResult, error) {
	return finishAwaitVerdict(r.bb, r.agentID, r.authority, result, err)
}

func (r awaitVerdictRun) finishPaused(task *models.Task, mode models.SystemMode) (*AwaitVerdictResult, error) {
	return finishPausedAwaitVerdict(r.projectRoot, task, r.agentID, r.authority, mode, r.submissionIndex)
}

func pausedAwaitReason(mode models.SystemMode) string {
	return fmt.Sprintf("system is %s; stop this session; an operator can use %q to resume", mode, brand.Command("resume"))
}

func finishPausedAwaitVerdict(projectRoot string, task *models.Task, agentID string, authority *models.AgentAuthority, mode models.SystemMode, submissionIndex int) (*AwaitVerdictResult, error) {
	reason := pausedAwaitReason(mode)
	err := releaseDepartedDoerAssignment(projectRoot, task.ID, agentID, authority, reason, models.TaskTransitionID(task), submissionIndex)
	return &AwaitVerdictResult{Verdict: string(VerdictPaused), TaskStatus: task.Status, Reason: reason,
		Guidance: stopVerdictGuidance(task.Status), SafeAction: string(SafeActionStop)}, err
}

func finishAwaitVerdictTimeout(projectRoot string, bb *db.Blackboard, taskID, agentID string, authority *models.AgentAuthority, status models.TaskStatus, submissionIndex int) (*AwaitVerdictResult, error) {
	state, task, err := readTaskState(bb, taskID)
	if err == nil && state.Config.Mode.HaltsWork() {
		return finishPausedAwaitVerdict(projectRoot, task, agentID, authority, state.Config.Mode, submissionIndex)
	}
	return finishAwaitVerdict(bb, agentID, authority, &AwaitVerdictResult{Verdict: string(VerdictTimeout), TaskStatus: status}, err)
}

func joinAwaitCleanupError(primaryErr, cleanupErr error) error {
	switch {
	case primaryErr == nil:
		return cleanupErr
	case cleanupErr == nil:
		return primaryErr
	default:
		return stderrors.Join(primaryErr, cleanupErr)
	}
}

func cappedAwaitBudget(total time.Duration) time.Duration {
	if total <= 0 {
		return 0
	}
	return min(total, DefaultAwaitBudget)
}

// AwaitVerdictRemainingBudget reports how much of total is left for this wait,
// measured from the agent's most recent submission rather than from a value the
// agent carries between calls. A caller that never passes back the reported
// remainder therefore still converges on TIMEOUT instead of looping until the
// session ceiling. Falls back to total when no anchor exists — the await's own
// precondition checks own that error.
func AwaitVerdictRemainingBudget(projectRoot, taskID, agentID string, total time.Duration) time.Duration {
	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	_, task, err := readTaskState(bb, taskID)
	if err != nil || task == nil {
		return cappedAwaitBudget(total)
	}
	submission, ok := latestSubmissionByAgent(task, agentID)
	if !ok {
		return cappedAwaitBudget(total)
	}
	return remainingFromAnchor(submission.at, total)
}

// remainingFromAnchor clamps elapsed-time subtraction into [0, total]. A future
// anchor (clock skew) yields total, never more.
func remainingFromAnchor(anchor time.Time, total time.Duration) time.Duration {
	total = cappedAwaitBudget(total)
	if anchor.IsZero() {
		return total
	}
	elapsed := awaitVerdictNow().Sub(anchor)
	if elapsed <= 0 {
		return total
	}
	if elapsed >= total {
		return 0
	}
	return total - elapsed
}

// ReleaseDepartedDoerAssignment drops only the doer's assignment and lease when a
// wait ends at budget exhaustion. Deliberately narrower than ReleaseClaim, whose
// doer profile also clears Worktree, BaseCommit, Iteration, Output and the whole
// submitted-attempt block (ReviewCommit, approvals, MergeCommit). Applying that
// here would strip the review boundary from a task still sitting in review, so a
// later verdict would fail validation and the reviewer would face an empty
// worktree. The submitted attempt must outlive the doer that produced it.
//
// No-op unless agentID still holds the assignment, so a task already reclaimed by
// someone else is left alone.
func ReleaseDepartedDoerAssignment(projectRoot, taskID, agentID string) error {
	return releaseDepartedDoerAssignment(projectRoot, taskID, agentID, nil, awaitBudgetExhaustedReason, "", awaitUnfencedSubmissionIndex)
}

// ReleaseDepartedDoerAssignmentWithAuthority fences the budget-exhaustion
// cleanup performed by an authenticated await-verdict command.
func ReleaseDepartedDoerAssignmentWithAuthority(projectRoot, taskID string, authority models.AgentAuthority) error {
	return releaseDepartedDoerAssignment(projectRoot, taskID, authority.ID, &authority, awaitBudgetExhaustedReason, "", awaitUnfencedSubmissionIndex)
}

func releaseDepartedDoerAssignment(projectRoot, taskID, agentID string, authority *models.AgentAuthority, reason, expectedTransition string, submissionIndex int) error {
	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	now := awaitVerdictNow().UTC()
	var resolver *pipeline.Resolver
	if expectedTransition != "" {
		var err error
		resolver, _, err = loadResolver(projectRoot)
		if err != nil {
			return &OperationalError{Message: "failed to load pipeline config", Err: err}
		}
	}

	return modifyLifecycleState(bb, authority, func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		if expectedTransition != "" {
			currentSubmission, found := latestSubmissionByAgent(task, agentID)
			if !found || currentSubmission.index != submissionIndex {
				return nil
			}
			// Pause ends a pending verdict hold, never a resumed executing claim.
			// Check the current pipeline status under the lock: a stale await may
			// already have observed the newer claim before capturing its token.
			rejected, err := resolver.RejectedStatus(task.RolePair)
			if checkAwaitableStatus(task, resolver) != nil && (err != nil || task.Status != rejected) {
				return nil
			}
			if models.TaskTransitionID(task) != expectedTransition {
				return WrapLifecycleError("await-verdict", task, fmt.Errorf("await ownership changed before paused exit"), models.LifecycleStateChanged, LifecycleSafeActionStop, LifecycleEffectsNone)
			}
		}
		if agent, ok := state.Agents[agentID]; ok && agent.Status == models.AgentStatusWaiting &&
			agent.CurrentTask != nil && *agent.CurrentTask == taskID {
			agent.CurrentTask = nil
			state.Agents[agentID] = agent
		}
		if task.AssignedTo == nil || *task.AssignedTo != agentID {
			return nil
		}

		task.AssignedTo = nil
		task.LeaseExpires = nil
		models.AdvanceLifecycle(task)

		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventDoerClaimReleased,
			Agent:  &agentID,
			Reason: &reason,
		})
		return nil
	})
}
