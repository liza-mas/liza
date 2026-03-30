package ops

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

// Resubmission verdict constants for AwaitResubmissionResult.Verdict.
const (
	ResubmissionResubmitted = "RESUBMITTED"
	ResubmissionTerminal    = "TERMINAL"
	ResubmissionTimeout     = "TIMEOUT"
	ResubmissionAborted     = "ABORTED"
)

// AwaitResubmissionResult holds the outcome of blocking on a doer resubmission.
type AwaitResubmissionResult struct {
	Verdict      string            // One of the Resubmission* constants
	TaskStatus   models.TaskStatus // Final observed task status
	Reason       string            // Terminal explanation (empty on RESUBMITTED)
	ReviewCommit string            // New commit SHA to review (on RESUBMITTED)
	ReviewCycle  int               // Current review cycle count
}

// reviewOwnershipLeaseMargin is added beyond the await deadline so the lease
// outlives the blocking call, preventing premature stale-claim cleanup.
const reviewOwnershipLeaseMargin = 5 * time.Minute

// reclaimReviewLeaseDuration is the fresh lease set when reclaiming the task
// for re-review after a resubmission.
const reclaimReviewLeaseDuration = 30 * time.Minute

// AwaitResubmission blocks until a doer resubmits after a rejection.
// It validates preconditions, acquires review ownership (ReviewingBy on task,
// agent status=WAITING), then blocks on an event loop until the task status
// leaves the waiting set (rejected/implementing/executing).
func AwaitResubmission(ctx context.Context, projectRoot, taskID, agentID string, timeout time.Duration) (*AwaitResubmissionResult, error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "agent ID is required"}
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	// Read state and find task.
	_, task, err := readTaskState(bb, taskID)
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

	// Check task status is rejected or already submitted (fast-doer edge case).
	if err := checkResubmissionPrecondition(task, resolver); err != nil {
		return nil, err
	}

	// Verify agent was the last rejecting reviewer.
	if err := checkLastRejectingReviewer(task, agentID); err != nil {
		return nil, err
	}

	// Acquire review ownership atomically.
	if err := acquireReviewOwnership(bb, agentID, taskID, timeout); err != nil {
		return nil, err
	}

	// Early resubmission: if task is already in submitted status, skip the
	// wait loop and reclaim immediately.
	submitted, _ := resolver.SubmittedStatus(task.RolePair)
	if task.Status == submitted {
		return reclaimForReview(bb, taskID, agentID, resolver, task.RolePair)
	}

	// --- Event loop: block until resubmission or terminal state ---
	rolePair := task.RolePair

	watcher, watchErr := bb.WatchForChanges()
	if watchErr != nil {
		return awaitResubmissionPolling(ctx, bb, taskID, agentID, timeout, resolver, rolePair)
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
			releaseReviewOwnership(bb, agentID, taskID)
			return nil, ctx.Err()

		case <-abortTicker.C:
			abortState, abortErr := bb.ReadCached()
			if abortErr != nil {
				continue
			}
			if abortState.Config.Mode == models.SystemModeStopped {
				releaseReviewOwnership(bb, agentID, taskID)
				return &AwaitResubmissionResult{Verdict: ResubmissionAborted, TaskStatus: task.Status}, nil
			}

		case <-watcher.Events():
			evState, evErr := bb.ReadCached()
			if evErr != nil {
				continue
			}
			if evState.Config.Mode == models.SystemModeStopped {
				releaseReviewOwnership(bb, agentID, taskID)
				return &AwaitResubmissionResult{Verdict: ResubmissionAborted, TaskStatus: task.Status}, nil
			}
			currentTask := evState.FindTask(taskID)
			if currentTask == nil {
				releaseReviewOwnership(bb, agentID, taskID)
				return &AwaitResubmissionResult{
					Verdict: ResubmissionTerminal,
					Reason:  "task disappeared from state",
				}, nil
			}
			if rc := checkResubmissionStatus(currentTask, resolver, rolePair); rc != nil {
				return handleResubmissionResult(bb, currentTask, agentID, resolver, rolePair)
			}

		case watcherErr := <-watcher.Errors():
			log.Printf("Watcher error, falling back to polling: %v", watcherErr)
			watcher.Close()
			return awaitResubmissionPolling(ctx, bb, taskID, agentID, timeout, resolver, rolePair)

		case <-deadlineTimer.C:
			releaseReviewOwnership(bb, agentID, taskID)
			return &AwaitResubmissionResult{Verdict: ResubmissionTimeout, TaskStatus: task.Status}, nil
		}
	}
}

// checkResubmissionPrecondition verifies the task is in a status where
// awaiting resubmission is valid: rejected or already submitted (fast-doer).
func checkResubmissionPrecondition(task *models.Task, resolver *pipeline.Resolver) error {
	rejected, err := resolver.RejectedStatus(task.RolePair)
	if err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q — check pipeline.yaml config", task.RolePair)}
	}
	submitted, err := resolver.SubmittedStatus(task.RolePair)
	if err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q — check pipeline.yaml config", task.RolePair)}
	}

	if task.Status == rejected || task.Status == submitted {
		return nil
	}

	return &PreconditionError{
		Reason: fmt.Sprintf("task %s is not in a rejected or submitted status (current: %s, expected: %s or %s)",
			task.ID, task.Status, rejected, submitted),
	}
}

// checkLastRejectingReviewer verifies the agent was the last to reject this task.
func checkLastRejectingReviewer(task *models.Task, agentID string) error {
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		if entry.Event == models.TaskEventRejected {
			if entry.Agent != nil && *entry.Agent == agentID {
				return nil
			}
			return &PreconditionError{
				Reason: fmt.Sprintf("agent %s is not the last rejecting reviewer of task %s", agentID, task.ID),
			}
		}
	}
	return &PreconditionError{
		Reason: fmt.Sprintf("task %s has no rejection history", task.ID),
	}
}

// acquireReviewOwnership atomically sets ReviewingBy and ReviewLeaseExpires on
// the task, and sets the agent's status to WAITING with CurrentTask.
func acquireReviewOwnership(bb *db.Blackboard, agentID, taskID string, timeout time.Duration) error {
	leaseExpiry := time.Now().Add(timeout + reviewOwnershipLeaseMargin)
	return bb.Modify(func(s *models.State) error {
		agent, ok := s.Agents[agentID]
		if !ok {
			return &errors.NotFoundError{Entity: "agent", ID: agentID}
		}
		task := s.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}

		task.ReviewingBy = &agentID
		task.ReviewLeaseExpires = &leaseExpiry
		agent.Status = models.AgentStatusWaiting
		agent.CurrentTask = &taskID
		s.Agents[agentID] = agent
		return nil
	})
}

// releaseReviewOwnership clears the reviewer's ownership from both the task
// and the agent. Agent status is intentionally left unchanged — the
// supervisor's resetAgentAfterExit handles status transitions.
func releaseReviewOwnership(bb *db.Blackboard, agentID, taskID string) error {
	return bb.Modify(func(s *models.State) error {
		if agent, ok := s.Agents[agentID]; ok {
			agent.CurrentTask = nil
			s.Agents[agentID] = agent
		}
		task := s.FindTask(taskID)
		if task != nil {
			task.ReviewingBy = nil
			task.ReviewLeaseExpires = nil
		}
		return nil
	})
}

// resubmissionCheck holds the result of checking whether a resubmission has arrived.
type resubmissionCheck struct {
	status models.TaskStatus
}

// checkResubmissionStatus determines if the task has left the waiting set.
// Returns nil if still waiting; returns the observed status if a resubmission
// or terminal state has been reached.
func checkResubmissionStatus(task *models.Task, resolver *pipeline.Resolver, rolePair string) *resubmissionCheck {
	submitted, _ := resolver.SubmittedStatus(rolePair)
	approved, _ := resolver.ApprovedStatus(rolePair)

	// Resubmission detected.
	if task.Status == submitted {
		return &resubmissionCheck{status: task.Status}
	}

	// Terminal states.
	if task.Status == approved ||
		task.Status == models.TaskStatusBlocked ||
		task.Status == models.TaskStatusSuperseded ||
		task.Status == models.TaskStatusAbandoned ||
		task.Status == models.TaskStatusIntegrationFailed ||
		task.Status == models.TaskStatusMerged {
		return &resubmissionCheck{status: task.Status}
	}

	// All other statuses (rejected, implementing, executing, etc.) — keep waiting.
	return nil
}

// handleResubmissionResult maps the observed task status to an AwaitResubmissionResult.
func handleResubmissionResult(bb *db.Blackboard, task *models.Task, agentID string, resolver *pipeline.Resolver, rolePair string) (*AwaitResubmissionResult, error) {
	submitted, _ := resolver.SubmittedStatus(rolePair)

	if task.Status == submitted {
		return reclaimForReview(bb, task.ID, agentID, resolver, rolePair)
	}

	// Terminal state — release ownership and report.
	releaseReviewOwnership(bb, agentID, task.ID)
	return &AwaitResubmissionResult{
		Verdict:    ResubmissionTerminal,
		TaskStatus: task.Status,
		Reason:     fmt.Sprintf("task entered terminal status: %s", task.Status),
	}, nil
}

// reclaimForReview atomically transitions the task from submitted to reviewing,
// refreshes the review lease, and sets the agent to reviewing status.
func reclaimForReview(bb *db.Blackboard, taskID, agentID string, resolver *pipeline.Resolver, rolePair string) (*AwaitResubmissionResult, error) {
	reviewing, err := resolver.ReviewingStatus(rolePair)
	if err != nil {
		releaseReviewOwnership(bb, agentID, taskID)
		return nil, &OperationalError{Message: "failed to resolve reviewing status", Err: err}
	}

	transitions := BuildPipelineTransitions(resolver)
	freshLease := time.Now().Add(reclaimReviewLeaseDuration)

	var reviewCommit string
	var reviewCycle int

	modErr := bb.Modify(func(s *models.State) error {
		task := s.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}

		if err := task.TransitionWith(reviewing, transitions); err != nil {
			return fmt.Errorf("reclaim transition failed: %w", err)
		}

		task.ReviewLeaseExpires = &freshLease
		// ReviewingBy stays set from acquireReviewOwnership.

		if task.ReviewCommit != nil {
			reviewCommit = *task.ReviewCommit
		}
		reviewCycle = task.ReviewCyclesCurrent

		agent, ok := s.Agents[agentID]
		if !ok {
			return &errors.NotFoundError{Entity: "agent", ID: agentID}
		}
		agent.Status = models.AgentStatusReviewing
		agent.CurrentTask = &taskID
		s.Agents[agentID] = agent
		return nil
	})
	if modErr != nil {
		releaseReviewOwnership(bb, agentID, taskID)
		return nil, &OperationalError{Message: "failed to reclaim task for review", Err: modErr}
	}

	return &AwaitResubmissionResult{
		Verdict:      ResubmissionResubmitted,
		TaskStatus:   reviewing,
		ReviewCommit: reviewCommit,
		ReviewCycle:  reviewCycle,
	}, nil
}

// awaitResubmissionPolling is the polling fallback for when fsnotify is unavailable.
// It checks state every 5 seconds until a resubmission arrives or the deadline expires.
func awaitResubmissionPolling(ctx context.Context, bb *db.Blackboard, taskID, agentID string, timeout time.Duration, resolver *pipeline.Resolver, rolePair string) (*AwaitResubmissionResult, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			releaseReviewOwnership(bb, agentID, taskID)
			return nil, ctx.Err()

		case <-ticker.C:
			state, err := bb.ReadCached()
			if err != nil {
				continue
			}
			if state.Config.Mode == models.SystemModeStopped {
				releaseReviewOwnership(bb, agentID, taskID)
				return &AwaitResubmissionResult{Verdict: ResubmissionAborted}, nil
			}
			currentTask := state.FindTask(taskID)
			if currentTask == nil {
				releaseReviewOwnership(bb, agentID, taskID)
				return &AwaitResubmissionResult{
					Verdict: ResubmissionTerminal,
					Reason:  "task disappeared from state",
				}, nil
			}
			if rc := checkResubmissionStatus(currentTask, resolver, rolePair); rc != nil {
				return handleResubmissionResult(bb, currentTask, agentID, resolver, rolePair)
			}
			if time.Now().After(deadline) {
				releaseReviewOwnership(bb, agentID, taskID)
				return &AwaitResubmissionResult{Verdict: ResubmissionTimeout, TaskStatus: currentTask.Status}, nil
			}
		}
	}
}
