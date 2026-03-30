package ops

import (
	stderrors "errors"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// Verdict constants for AwaitVerdictResult.Verdict.
const (
	VerdictApproved   = "APPROVED"
	VerdictRejected   = "REJECTED"
	VerdictNewAttempt = "NEW_ATTEMPT"
	VerdictTerminal   = "TERMINAL"
	VerdictTimeout    = "TIMEOUT"
	VerdictAborted    = "ABORTED"
)

// ErrBudgetExhausted is returned when the iteration/review-cycle budget
// would be exceeded on rejection — the agent should exit normally.
var ErrBudgetExhausted = stderrors.New("budget exhausted: iteration or review-cycle limit reached")

// AwaitVerdictResult holds the outcome of blocking on a review verdict.
type AwaitVerdictResult struct {
	Verdict       string            // One of the Verdict* constants
	Reason        string            // Rejection reason or terminal explanation
	ReviewerAgent string            // Agent ID that issued the verdict (empty if timeout/abort)
	TaskStatus    models.TaskStatus // Final observed task status
	Iteration     int               // Current iteration number (post-reclaim if rejected)
	Guidance      string            // Inline guidance for the agent on rejection
}

// AwaitVerdict blocks until a review verdict arrives for a submitted task.
// It validates preconditions, acquires ownership (agent status=WAITING,
// CurrentTask=taskID), then waits for the verdict. The event loop and
// result mapping are added in subsequent tasks; this implementation
// returns a placeholder error after ownership acquisition.
func AwaitVerdict(projectRoot, taskID, agentID string, timeout time.Duration) (*AwaitVerdictResult, error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "agent ID is required"}
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	// Read state and find task.
	state, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}

	// Resolve pipeline statuses for the task's role-pair.
	if task.RolePair == "" {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s has no role_pair set", taskID)}
	}
	resolver, _, resolverErr := loadResolver(projectRoot)
	if resolverErr != nil {
		return nil, &OperationalError{Message: "failed to load pipeline config", Err: resolverErr}
	}

	// Check task status is in the awaitable set.
	if err := checkAwaitableStatus(task, resolver); err != nil {
		return nil, err
	}

	// Verify agent was the last submitter.
	if err := checkLastSubmitter(task, agentID); err != nil {
		return nil, err
	}

	// Acquire ownership atomically.
	if err := acquireAwaitOwnership(bb, agentID, taskID); err != nil {
		return nil, err
	}

	// Budget gate: simulate what would happen on rejection. If limits are
	// already at capacity, release ownership and return immediately rather
	// than blocking for up to 25 minutes only to discover we can't iterate.
	iterLimit := effectiveCoderIterationLimit(task, state.Config)
	reviewLimit := effectiveReviewCycleLimit(state.Config)
	_, shouldEscalate := classifyLimitEscalation(
		task.ReviewCyclesCurrent, reviewLimit,
		task.Iteration, iterLimit,
		task.EffectiveAttempt(),
	)
	if shouldEscalate {
		releaseOwnership(bb, agentID)
		return nil, ErrBudgetExhausted
	}

	// Placeholder: event loop added in subsequent task.
	return nil, fmt.Errorf("await-verdict event loop not yet implemented")
}

// checkAwaitableStatus verifies the task is in a status where awaiting a
// verdict is valid: submitted, reviewing, or partially-approved.
func checkAwaitableStatus(task *models.Task, resolver interface {
	SubmittedStatus(string) (models.TaskStatus, error)
	ReviewingStatus(string) (models.TaskStatus, error)
	PartiallyApprovedStatus(string) (models.TaskStatus, error)
}) error {
	submitted, err := resolver.SubmittedStatus(task.RolePair)
	if err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q — check pipeline.yaml config", task.RolePair)}
	}
	reviewing, err := resolver.ReviewingStatus(task.RolePair)
	if err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q — check pipeline.yaml config", task.RolePair)}
	}

	if task.Status == submitted || task.Status == reviewing {
		return nil
	}

	// PartiallyApproved may not exist for all role-pairs — that's fine.
	partiallyApproved, paErr := resolver.PartiallyApprovedStatus(task.RolePair)
	if paErr == nil && task.Status == partiallyApproved {
		return nil
	}

	return &PreconditionError{
		Reason: fmt.Sprintf("task %s is not in an awaitable status (current: %s, expected: %s or %s)",
			task.ID, task.Status, submitted, reviewing),
	}
}

// checkLastSubmitter verifies the agent was the last to submit this task for review.
func checkLastSubmitter(task *models.Task, agentID string) error {
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		if entry.Event == models.TaskEventSubmittedForReview {
			if entry.Agent != nil && *entry.Agent == agentID {
				return nil
			}
			return &PreconditionError{
				Reason: fmt.Sprintf("agent %s is not the last submitter of task %s", agentID, task.ID),
			}
		}
	}
	return &PreconditionError{
		Reason: fmt.Sprintf("task %s has no submission history", task.ID),
	}
}

// acquireAwaitOwnership atomically sets the agent's status to WAITING and
// CurrentTask to taskID. This prevents other supervisors from claiming the
// task if it gets rejected while we're waiting.
func acquireAwaitOwnership(bb *db.Blackboard, agentID, taskID string) error {
	return bb.Modify(func(s *models.State) error {
		agent, ok := s.Agents[agentID]
		if !ok {
			return &errors.NotFoundError{Entity: "agent", ID: agentID}
		}
		agent.Status = models.AgentStatusWaiting
		agent.CurrentTask = &taskID
		s.Agents[agentID] = agent
		return nil
	})
}

// releaseOwnership clears the agent's CurrentTask, relinquishing ownership
// of the task. Status is left unchanged — the supervisor's resetAgentAfterExit
// handles status transitions when the CLI session ends.
func releaseOwnership(bb *db.Blackboard, agentID string) error {
	return bb.Modify(func(s *models.State) error {
		if agent, ok := s.Agents[agentID]; ok {
			agent.CurrentTask = nil
			s.Agents[agentID] = agent
		}
		return nil
	})
}
