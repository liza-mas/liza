package ops

import (
	"context"
	stderrors "errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
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
// CurrentTask=taskID), checks budget, then blocks on an event loop until
// the task status leaves the submitted/reviewing/partially-approved set.
func AwaitVerdict(ctx context.Context, projectRoot, taskID, agentID string, timeout time.Duration) (*AwaitVerdictResult, error) {
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

	// --- Event loop: block until verdict arrives ---
	rolePair := task.RolePair

	watcher, watchErr := bb.WatchForChanges()
	if watchErr != nil {
		return awaitVerdictPolling(ctx, bb, taskID, agentID, timeout, resolver, rolePair, projectRoot)
	}
	defer watcher.Close()

	deadline := time.Now().Add(timeout)
	deadlineTimer := time.NewTimer(time.Until(deadline))
	defer deadlineTimer.Stop()

	abortTicker := time.NewTicker(1 * time.Second)
	defer abortTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			releaseOwnership(bb, agentID)
			return nil, ctx.Err()

		case <-abortTicker.C:
			abortState, abortErr := bb.ReadCached()
			if abortErr != nil {
				continue
			}
			if abortState.Config.Mode == models.SystemModeStopped {
				releaseOwnership(bb, agentID)
				return &AwaitVerdictResult{Verdict: VerdictAborted, TaskStatus: task.Status}, nil
			}

		case <-watcher.Events():
			evState, evErr := bb.ReadCached()
			if evErr != nil {
				continue
			}
			if evState.Config.Mode == models.SystemModeStopped {
				releaseOwnership(bb, agentID)
				return &AwaitVerdictResult{Verdict: VerdictAborted, TaskStatus: task.Status}, nil
			}
			currentTask := evState.FindTask(taskID)
			if currentTask == nil {
				releaseOwnership(bb, agentID)
				return &AwaitVerdictResult{
					Verdict: VerdictTerminal,
					Reason:  "task disappeared from state",
				}, nil
			}
			if vc := checkVerdictStatus(currentTask, resolver, rolePair); vc != nil {
				return handleVerdictResult(bb, currentTask, agentID, projectRoot, resolver, rolePair)
			}

		case watcherErr := <-watcher.Errors():
			log.Printf("Watcher error, falling back to polling: %v", watcherErr)
			watcher.Close()
			return awaitVerdictPolling(ctx, bb, taskID, agentID, timeout, resolver, rolePair, projectRoot)

		case <-deadlineTimer.C:
			releaseOwnership(bb, agentID)
			return &AwaitVerdictResult{Verdict: VerdictTimeout, TaskStatus: task.Status}, nil
		}
	}
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

// verdictCheck holds the result of checking whether a verdict has arrived.
type verdictCheck struct {
	status models.TaskStatus
}

// checkVerdictStatus determines if the task has left the awaitable set
// (submitted/reviewing/partially-approved). Returns nil if still waiting;
// returns the observed status if a verdict has arrived.
func checkVerdictStatus(task *models.Task, resolver *pipeline.Resolver, rolePair string) *verdictCheck {
	submitted, _ := resolver.SubmittedStatus(rolePair)
	reviewing, _ := resolver.ReviewingStatus(rolePair)

	if task.Status == submitted || task.Status == reviewing {
		return nil
	}

	// PartiallyApproved keeps waiting — quorum not yet met.
	partiallyApproved, paErr := resolver.PartiallyApprovedStatus(rolePair)
	if paErr == nil && task.Status == partiallyApproved {
		return nil
	}

	return &verdictCheck{status: task.Status}
}

// handleVerdictResult maps the final task status to an AwaitVerdictResult.
// For rejections within budget, it attempts auto-reclaim via ClaimTask.
func handleVerdictResult(bb *db.Blackboard, task *models.Task, agentID, projectRoot string, resolver *pipeline.Resolver, rolePair string) (*AwaitVerdictResult, error) {
	approved, _ := resolver.ApprovedStatus(rolePair)
	rejected, _ := resolver.RejectedStatus(rolePair)

	switch task.Status {
	case approved:
		releaseOwnership(bb, agentID)
		return &AwaitVerdictResult{
			Verdict:       VerdictApproved,
			TaskStatus:    task.Status,
			ReviewerAgent: extractReviewerFromHistory(task),
		}, nil

	case rejected:
		reviewer := extractReviewerFromHistory(task)
		reason := ""
		if task.RejectionReason != nil {
			reason = *task.RejectionReason
		}

		// Attempt auto-reclaim via ClaimTask. ClaimTask internally checks
		// limits via classifyLimitEscalation — AwaitVerdict doesn't need
		// its own same-vs-new-attempt detection.
		_, claimErr := ClaimTask(projectRoot, task.ID, agentID)
		if claimErr != nil {
			var pe *PreconditionError
			if stderrors.As(claimErr, &pe) {
				if strings.Contains(pe.Reason, "transitioned to attempt") {
					releaseOwnership(bb, agentID)
					return &AwaitVerdictResult{
						Verdict:       VerdictNewAttempt,
						Reason:        reason,
						ReviewerAgent: reviewer,
						TaskStatus:    task.Status,
					}, nil
				}
				// Blocked or other limit exhaustion.
				releaseOwnership(bb, agentID)
				return &AwaitVerdictResult{
					Verdict:       VerdictTerminal,
					Reason:        reason,
					ReviewerAgent: reviewer,
					TaskStatus:    task.Status,
				}, nil
			}
			// Infrastructure error during reclaim.
			releaseOwnership(bb, agentID)
			return nil, fmt.Errorf("auto-reclaim failed: %w", claimErr)
		}

		// Reclaim succeeded — re-read task to get updated iteration.
		_, updatedTask, readErr := readTaskState(bb, task.ID)
		iteration := task.Iteration
		if readErr == nil && updatedTask != nil {
			iteration = updatedTask.Iteration
		}

		return &AwaitVerdictResult{
			Verdict:       VerdictRejected,
			Reason:        reason,
			ReviewerAgent: reviewer,
			TaskStatus:    task.Status,
			Iteration:     iteration,
			Guidance:      buildRejectionGuidance(reason, task),
		}, nil

	default:
		// BLOCKED, SUPERSEDED, INTEGRATION_FAILED, etc.
		releaseOwnership(bb, agentID)
		return &AwaitVerdictResult{
			Verdict:    VerdictTerminal,
			TaskStatus: task.Status,
			Reason:     fmt.Sprintf("task entered terminal status: %s", task.Status),
		}, nil
	}
}

// awaitVerdictPolling is the polling fallback for when fsnotify is unavailable.
// It checks state every 5 seconds until a verdict arrives or the deadline expires.
func awaitVerdictPolling(ctx context.Context, bb *db.Blackboard, taskID, agentID string, timeout time.Duration, resolver *pipeline.Resolver, rolePair, projectRoot string) (*AwaitVerdictResult, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			releaseOwnership(bb, agentID)
			return nil, ctx.Err()

		case <-ticker.C:
			state, err := bb.ReadCached()
			if err != nil {
				continue
			}
			if state.Config.Mode == models.SystemModeStopped {
				releaseOwnership(bb, agentID)
				return &AwaitVerdictResult{Verdict: VerdictAborted}, nil
			}
			currentTask := state.FindTask(taskID)
			if currentTask == nil {
				releaseOwnership(bb, agentID)
				return &AwaitVerdictResult{
					Verdict: VerdictTerminal,
					Reason:  "task disappeared from state",
				}, nil
			}
			if vc := checkVerdictStatus(currentTask, resolver, rolePair); vc != nil {
				return handleVerdictResult(bb, currentTask, agentID, projectRoot, resolver, rolePair)
			}
			if time.Now().After(deadline) {
				releaseOwnership(bb, agentID)
				return &AwaitVerdictResult{Verdict: VerdictTimeout, TaskStatus: currentTask.Status}, nil
			}
		}
	}
}

// buildRejectionGuidance constructs inline guidance for the agent on rejection,
// equivalent to the prior_rejection.tmpl content.
func buildRejectionGuidance(reason string, task *models.Task) string {
	var b strings.Builder
	b.WriteString("## Rejection Feedback\n\n")
	b.WriteString("You MUST ADDRESS the following rejection feedback before resubmitting:\n\n")
	b.WriteString(reason)
	b.WriteString("\n\n")
	if task.Scope != "" {
		b.WriteString("If the fix requires changes outside your declared scope, use scope_extensions in your checkpoint.\n\n")
	}
	fmt.Fprintf(&b, "Current iteration: %d\n", task.Iteration)
	return b.String()
}

// extractReviewerFromHistory scans task history in reverse for the most recent
// review verdict and returns the reviewer agent ID.
func extractReviewerFromHistory(task *models.Task) string {
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		if entry.Event == models.TaskEventApproved ||
			entry.Event == models.TaskEventRejected {
			if entry.Agent != nil {
				return *entry.Agent
			}
			return ""
		}
	}
	return ""
}
