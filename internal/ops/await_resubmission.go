package ops

import (
	"context"
	stderrors "errors"
	"fmt"
	"log"
	"reflect"
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
	ResubmissionPoll        = "POLL" // Blocking interval expired, caller should retry
	ResubmissionPaused      = "PAUSED"
)

// AwaitResubmissionResult holds the outcome of blocking on a doer resubmission.
type AwaitResubmissionResult struct {
	Verdict      string            `json:"verdict"`       // One of the Resubmission* constants
	TaskStatus   models.TaskStatus `json:"task_status"`   // Final observed task status
	Reason       string            `json:"reason"`        // Terminal explanation (empty on RESUBMITTED)
	BaseCommit   string            `json:"base_commit"`   // Fresh review base SHA to use on RESUBMITTED
	ReviewCommit string            `json:"review_commit"` // New commit SHA to review (on RESUBMITTED)
	ReviewCycle  int               `json:"review_cycle"`  // Current review cycle count
	SafeAction   string            `json:"safe_action,omitempty"`
}

// reviewOwnershipLeaseMargin is added beyond the await deadline so the lease
// outlives the blocking call, preventing premature stale-claim cleanup.
const reviewOwnershipLeaseMargin = 5 * time.Minute

// reclaimReviewLeaseDuration is the fresh lease set when reclaiming the task
// for re-review after a resubmission.
const reclaimReviewLeaseDuration = 30 * time.Minute

type awaitResubmissionWatcher interface {
	Events() <-chan struct{}
	Errors() <-chan error
	Close() error
}

var newAwaitResubmissionWatcher = func(bb *db.Blackboard) (awaitResubmissionWatcher, error) {
	return bb.WatchForChanges()
}

// AwaitResubmissionOptions controls the periodic checks used while waiting.
// The zero value uses the project's shared await polling policy.
type AwaitResubmissionOptions struct {
	AbortPollInterval    time.Duration
	FallbackPollInterval time.Duration
	// PollOnTimeout distinguishes a foreground slice from final budget expiry.
	// Only the command layer sets it after deriving the remaining total budget.
	PollOnTimeout bool
}

func (opts AwaitResubmissionOptions) normalized(config models.Config) (AwaitResubmissionOptions, error) {
	var err error
	opts.AbortPollInterval, opts.FallbackPollInterval, err = normalizeAwaitPolling(config, opts.AbortPollInterval, opts.FallbackPollInterval)
	return opts, err
}

// AwaitResubmission blocks until a doer resubmits after a rejection.
// It validates preconditions, acquires review ownership (ReviewingBy on task,
// agent status=WAITING), then blocks on an event loop until the task status
// leaves the waiting set (rejected/implementing/executing).
func AwaitResubmission(ctx context.Context, projectRoot, taskID, agentID string, timeout time.Duration) (*AwaitResubmissionResult, error) {
	return awaitResubmissionWithOptions(ctx, projectRoot, taskID, agentID, nil, timeout, AwaitResubmissionOptions{})
}

// AwaitResubmissionWithOptions is AwaitResubmission with configurable polling intervals.
func AwaitResubmissionWithOptions(ctx context.Context, projectRoot, taskID, agentID string, timeout time.Duration, opts AwaitResubmissionOptions) (*AwaitResubmissionResult, error) {
	return awaitResubmissionWithOptions(ctx, projectRoot, taskID, agentID, nil, timeout, opts)
}

// AwaitResubmissionWithAuthority is the authenticated command entry point.
func AwaitResubmissionWithAuthority(ctx context.Context, projectRoot, taskID string, authority models.AgentAuthority, timeout time.Duration) (*AwaitResubmissionResult, error) {
	return awaitResubmissionWithOptions(ctx, projectRoot, taskID, authority.ID, &authority, timeout, AwaitResubmissionOptions{})
}

// AwaitResubmissionWithAuthorityOptions is AwaitResubmissionWithAuthority with
// configurable polling intervals.
func AwaitResubmissionWithAuthorityOptions(ctx context.Context, projectRoot, taskID string, authority models.AgentAuthority, timeout time.Duration, opts AwaitResubmissionOptions) (*AwaitResubmissionResult, error) {
	return awaitResubmissionWithOptions(ctx, projectRoot, taskID, authority.ID, &authority, timeout, opts)
}

func awaitResubmissionWithOptions(ctx context.Context, projectRoot, taskID, agentID string, authority *models.AgentAuthority, timeout time.Duration, opts AwaitResubmissionOptions) (result *AwaitResubmissionResult, resultErr error) {
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
	opts, err = opts.normalized(state.Config)
	if err != nil {
		return nil, err
	}

	// Verify agent exists in state (early exits below bypass acquireReviewOwnership
	// which would otherwise catch nonexistent agents).
	if _, ok := state.Agents[agentID]; !ok {
		return nil, &errors.NotFoundError{Entity: "agent", ID: agentID}
	}

	// Verify agent was the last rejecting reviewer (even for early exits).
	if err := checkLastRejectingReviewer(task, agentID); err != nil {
		return nil, err
	}
	var retained *reviewReservation
	observed := &reviewReservation{historyLen: len(task.History)}
	if observed.heldBy(state, task, agentID) {
		retained = observed
	}
	if state.Config.Mode.HaltsWork() {
		return finishPausedAwaitResubmission(bb, task, agentID, authority, retained, state.Config.Mode)
	}

	// If the task was escalated (BLOCKED or terminal) by the verdict, return
	// immediately so the reviewer can exit cleanly without parsing verdict details.
	if task.Status == models.TaskStatusBlocked || task.Status.IsTerminal() {
		return &AwaitResubmissionResult{
			Verdict:    ResubmissionAborted,
			TaskStatus: task.Status,
			Reason:     fmt.Sprintf("task already %s — no resubmission expected", task.Status),
		}, nil
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

	// Early resubmission: if task is already in submitted status, skip the
	// wait loop and validate evidence before acquiring any ownership.
	submitted, _ := resolver.SubmittedStatus(task.RolePair)
	if task.Status == submitted {
		result, err := reclaimForReview(projectRoot, bb, taskID, agentID, authority, resolver, task.RolePair, retained)
		var evidenceErr *AcceptanceEvidenceError
		if retained != nil && stderrors.As(err, &evidenceErr) {
			return finishAwaitResubmission(bb, agentID, taskID, authority, retained, result, err)
		}
		return result, err
	}

	// Acquire ownership only when actually waiting for the doer's submission.
	ownership, err := acquireReviewOwnershipSnapshotChecked(bb, agentID, taskID, authority, timeout, retained)
	if err != nil {
		var halted *SystemHaltedError
		if stderrors.As(err, &halted) {
			return finishPausedAwaitResubmission(bb, task, agentID, authority, retained, halted.Mode)
		}
		if stderrors.Is(err, errReviewReservationLost) {
			return reviewOwnershipLostResult(task.Status), nil
		}
		return nil, &OperationalError{Message: "failed to acquire review ownership", Err: err}
	}
	reservation := ownership.reservation
	defer func() {
		var evidenceErr *AcceptanceEvidenceError
		if !stderrors.As(resultErr, &evidenceErr) {
			return
		}
		if ownership.task.ReviewingBy != nil {
			_, cleanupErr := releaseReviewOwnership(bb, agentID, taskID, authority, reservation, false)
			resultErr = joinAwaitCleanupError(resultErr, cleanupErr)
			return
		}
		// Evidence refusal must undo the wait's ownership, including WAITING,
		// while retaining the doer's resubmission and any subsequent reassignment.
		cleanupErr := modifyLifecycleState(bb, authority, func(s *models.State) error {
			currentTask := s.FindTask(taskID)
			restoredOwnership := false
			if currentTask != nil && reservation.heldBy(s, currentTask, agentID) {
				// Undo only our temporary revision. A changed status keeps both the
				// pre-wait and waiting tokens stale; never revive an old preparation
				// or overwrite a completion written during the wait. Otherwise
				// retain receipts and retire pending work at the ownership boundary.
				if currentTask.Status != ownership.task.Status &&
					(ownership.task.Lifecycle == nil || ownership.task.Lifecycle.Preparation == nil) &&
					reflect.DeepEqual(currentTask.Lifecycle, ownership.lifecycle) {
					currentTask.Lifecycle = ownership.task.Lifecycle
				} else {
					models.AdvanceLifecycle(currentTask)
				}
				currentTask.ReviewingBy = ownership.task.ReviewingBy
				currentTask.ReviewLeaseExpires = ownership.task.ReviewLeaseExpires
				restoredOwnership = true
			}
			if agent, ok := s.Agents[agentID]; restoredOwnership && ok && agent.Status == models.AgentStatusWaiting &&
				agent.CurrentTask != nil && *agent.CurrentTask == taskID {
				agent.Status = ownership.agent.Status
				agent.CurrentTask = ownership.agent.CurrentTask
				s.Agents[agentID] = agent
			}
			return nil
		})
		resultErr = joinAwaitCleanupError(resultErr, cleanupErr)
	}()

	// --- Event loop: block until resubmission or terminal state ---
	rolePair := task.RolePair
	deadline := time.Now().Add(timeout)

	watcher, watchErr := newAwaitResubmissionWatcher(bb)
	if watchErr != nil {
		return awaitResubmissionPolling(ctx, projectRoot, bb, taskID, agentID, authority, reservation, deadline, task.Status, resolver, rolePair, opts.FallbackPollInterval, opts.PollOnTimeout)
	}
	defer watcher.Close()

	deadlineTimer := time.NewTimer(time.Until(deadline))
	defer deadlineTimer.Stop()

	abortTicker := time.NewTicker(opts.AbortPollInterval)
	defer abortTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return finishAwaitResubmission(bb, agentID, taskID, authority, reservation, nil, ctx.Err())

		case <-abortTicker.C:
			abortState, abortErr := bb.Read()
			if abortErr != nil {
				continue
			}
			if abortState.Config.Mode == models.SystemModeStopped {
				return finishAwaitResubmission(bb, agentID, taskID, authority, reservation,
					&AwaitResubmissionResult{Verdict: ResubmissionAborted, TaskStatus: task.Status}, nil)
			}
			currentTask := abortState.FindTask(taskID)
			if currentTask == nil {
				return finishAwaitResubmission(bb, agentID, taskID, authority, reservation, &AwaitResubmissionResult{
					Verdict: ResubmissionTerminal,
					Reason:  "task disappeared from state",
				}, nil)
			}
			if abortState.Config.Mode.HaltsWork() {
				return finishPausedAwaitResubmission(bb, currentTask, agentID, authority, reservation, abortState.Config.Mode)
			}
			if rc := checkResubmissionStatus(currentTask, resolver, rolePair); rc != nil {
				return handleResubmissionResult(projectRoot, bb, currentTask, agentID, authority, resolver, rolePair, reservation)
			}
			if reservationLost(bb, reservation, abortState, currentTask, agentID) {
				return finishAwaitResubmission(bb, agentID, taskID, authority, reservation, reviewOwnershipLostResult(currentTask.Status), nil)
			}

		case <-watcher.Events():
			evState, evErr := bb.ReadCached()
			if evErr != nil {
				continue
			}
			if evState.Config.Mode == models.SystemModeStopped {
				return finishAwaitResubmission(bb, agentID, taskID, authority, reservation,
					&AwaitResubmissionResult{Verdict: ResubmissionAborted, TaskStatus: task.Status}, nil)
			}
			currentTask := evState.FindTask(taskID)
			if currentTask == nil {
				return finishAwaitResubmission(bb, agentID, taskID, authority, reservation, &AwaitResubmissionResult{
					Verdict: ResubmissionTerminal,
					Reason:  "task disappeared from state",
				}, nil)
			}
			if evState.Config.Mode.HaltsWork() {
				return finishPausedAwaitResubmission(bb, currentTask, agentID, authority, reservation, evState.Config.Mode)
			}
			if rc := checkResubmissionStatus(currentTask, resolver, rolePair); rc != nil {
				return handleResubmissionResult(projectRoot, bb, currentTask, agentID, authority, resolver, rolePair, reservation)
			}
			if reservationLost(bb, reservation, evState, currentTask, agentID) {
				return finishAwaitResubmission(bb, agentID, taskID, authority, reservation, reviewOwnershipLostResult(currentTask.Status), nil)
			}

		case watcherErr := <-watcher.Errors():
			log.Printf("Watcher error, falling back to polling: %v", watcherErr)
			watcher.Close()
			return awaitResubmissionPolling(ctx, projectRoot, bb, taskID, agentID, authority, reservation, deadline, task.Status, resolver, rolePair, opts.FallbackPollInterval, opts.PollOnTimeout)

		case <-deadlineTimer.C:
			return finishResubmissionInterval(bb, taskID, agentID, authority, reservation, task.Status, opts.PollOnTimeout)
		}
	}
}

// checkResubmissionPrecondition verifies the task is in a status where
// awaiting resubmission is valid: rejected, executing (fast-coder claimed
// before reviewer called await), or already submitted (fast-doer).
func checkResubmissionPrecondition(task *models.Task, resolver *pipeline.Resolver) error {
	rejected, err := resolver.RejectedStatus(task.RolePair)
	if err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q — check pipeline.yaml config", task.RolePair)}
	}
	submitted, err := resolver.SubmittedStatus(task.RolePair)
	if err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q — check pipeline.yaml config", task.RolePair)}
	}
	executing, err := resolver.ExecutingStatus(task.RolePair)
	if err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q — check pipeline.yaml config", task.RolePair)}
	}

	if task.Status == rejected || task.Status == submitted || task.Status == executing {
		return nil
	}

	return &PreconditionError{
		Reason: fmt.Sprintf("task %s is not in a rejected, executing, or submitted status (current: %s, expected: %s, %s, or %s)",
			task.ID, task.Status, rejected, executing, submitted),
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
func acquireReviewOwnership(bb *db.Blackboard, agentID, taskID string, authority *models.AgentAuthority, timeout time.Duration) error {
	_, err := acquireReviewOwnershipSnapshot(bb, agentID, taskID, authority, timeout)
	return err
}

type reviewOwnershipSnapshot struct {
	task        models.Task
	agent       models.Agent
	lifecycle   *models.TaskLifecycle
	reservation *reviewReservation
}

// reviewReservation is the WAITING ownership one await acquired. A process
// outliving its cancelled provider turn keeps its reservation after the same
// registration has released it and claimed the task again; cleanup and reclaim
// must act only on this await's own reservation.
type reviewReservation struct {
	historyLen int // Task history is append-only; later events postdate acquisition.
}

// supersededIn reports whether a later claim replaced the reservation: another
// reviewer holds the task, or a release or same-agent claim was recorded since
// acquisition. The doer's claim events do not touch review ownership, and a
// task transition that merely releases the waiting agent is no later claim.
func (r *reviewReservation) supersededIn(task *models.Task, agentID string) bool {
	if (task.ReviewingBy != nil && *task.ReviewingBy != agentID) || len(task.History) < r.historyLen {
		return true
	}
	for _, event := range task.History[r.historyLen:] {
		switch event.Event {
		case models.TaskEventReviewClaimReleased:
			return true
		case models.TaskEventClaimed, models.TaskEventClaimReleased:
			if event.Agent != nil && *event.Agent == agentID {
				return true
			}
		}
	}
	return false
}

// heldBy reports whether the reservation still stands: not superseded, and its
// claim fields and the agent's WAITING hold on the task are intact. Anything
// but a terminal outcome requires it, since ownership can also be cleared
// without a history event (stale-claim cleanup).
func (r *reviewReservation) heldBy(state *models.State, task *models.Task, agentID string) bool {
	agent, ok := state.Agents[agentID]
	return ok && task.ReviewingBy != nil && *task.ReviewingBy == agentID && agent.Status == models.AgentStatusWaiting &&
		task.ReviewLeaseExpires != nil && task.ReviewLeaseExpires.After(time.Now()) &&
		agent.CurrentTask != nil && *agent.CurrentTask == task.ID && !r.supersededIn(task, agentID)
}

// reservationLost confirms a loss seen in a cached observation on a fresh
// read: mtime caching can lag a write, and the wait must only end on a real loss.
func reservationLost(bb *db.Blackboard, reservation *reviewReservation, observed *models.State, task *models.Task, agentID string) bool {
	if reservation.heldBy(observed, task, agentID) {
		return false
	}
	fresh, err := bb.Read()
	if err != nil {
		return false
	}
	freshTask := fresh.FindTask(task.ID)
	return freshTask != nil && !reservation.heldBy(fresh, freshTask, agentID)
}

var errReviewReservationLost = stderrors.New("review reservation lost")

// Ownership loss is final: the caller must not retry and reacquire a claim
// that now belongs to another turn.
func reviewOwnershipLostResult(status models.TaskStatus) *AwaitResubmissionResult {
	return &AwaitResubmissionResult{Verdict: ResubmissionAborted, TaskStatus: status, Reason: "review ownership lost"}
}

// Capture the rollback boundary in the acquisition transaction, so concurrent
// changes between admission and ownership acquisition cannot be undone later.
func acquireReviewOwnershipSnapshot(bb *db.Blackboard, agentID, taskID string, authority *models.AgentAuthority, timeout time.Duration) (*reviewOwnershipSnapshot, error) {
	return acquireReviewOwnershipSnapshotChecked(bb, agentID, taskID, authority, timeout, nil)
}

func acquireReviewOwnershipSnapshotChecked(bb *db.Blackboard, agentID, taskID string, authority *models.AgentAuthority, timeout time.Duration, retained *reviewReservation) (*reviewOwnershipSnapshot, error) {
	leaseExpiry := time.Now().Add(timeout + reviewOwnershipLeaseMargin)
	var snapshot reviewOwnershipSnapshot
	err := modifyLifecycleState(bb, authority, func(s *models.State) error {
		agent, ok := s.Agents[agentID]
		if !ok {
			return &errors.NotFoundError{Entity: "agent", ID: agentID}
		}
		task := s.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		if err := RequireWorkAdmitted(s, "await-resubmission"); err != nil {
			return err
		}

		if task.ReviewingBy != nil && *task.ReviewingBy != agentID {
			return WrapLifecycleError("claim-reviewer-task", task, fmt.Errorf("review ownership changed"), models.LifecycleStateChanged, "requery", "none")
		}
		if (task.ReviewingBy != nil && (retained == nil || !retained.heldBy(s, task, agentID))) ||
			(task.ReviewingBy == nil && retained != nil) {
			return errReviewReservationLost
		}
		snapshot = reviewOwnershipSnapshot{task: *task, agent: agent, reservation: &reviewReservation{historyLen: len(task.History)}}
		if task.Lifecycle != nil {
			snapshot.task.Lifecycle = cloneTaskLifecycle(task.Lifecycle)
		}
		if task.ReviewingBy == nil {
			models.AdvanceLifecycle(task)
		}
		snapshot.lifecycle = cloneTaskLifecycle(task.Lifecycle)
		if task.Lifecycle == nil {
			snapshot.lifecycle = nil
		}
		task.ReviewingBy = &agentID
		task.ReviewLeaseExpires = &leaseExpiry
		agent.Status = models.AgentStatusWaiting
		agent.CurrentTask = &taskID
		s.Agents[agentID] = agent
		return nil
	})
	return &snapshot, err
}

// releaseReviewOwnership clears the wait's reservation from both the task and
// the agent. Without a reservation (early resubmission) there is nothing to
// release. A lost reservation leaves state untouched and yields the final
// ownership-lost result. A terminal outcome loses it only to a later claim: a
// terminal transition may release the waiting agent itself (mark-blocked).
// Agent status is intentionally left unchanged — the supervisor's
// resetAgentAfterExit handles status transitions.
func releaseReviewOwnership(bb *db.Blackboard, agentID, taskID string, authority *models.AgentAuthority, reservation *reviewReservation, terminal bool) (*AwaitResubmissionResult, error) {
	if reservation == nil {
		return nil, nil
	}
	var lost *AwaitResubmissionResult
	err := modifyLifecycleState(bb, authority, func(s *models.State) error {
		task := s.FindTask(taskID)
		if task != nil && ((terminal && reservation.supersededIn(task, agentID)) || (!terminal && !reservation.heldBy(s, task, agentID))) {
			lost = reviewOwnershipLostResult(task.Status)
			return errReviewReservationLost
		}
		if agent, ok := s.Agents[agentID]; ok && agent.CurrentTask != nil && *agent.CurrentTask == taskID {
			agent.CurrentTask = nil
			s.Agents[agentID] = agent
		}
		if task != nil && task.ReviewingBy != nil && *task.ReviewingBy == agentID {
			models.AdvanceLifecycle(task)
			task.ReviewingBy = nil
			task.ReviewLeaseExpires = nil
		}
		return nil
	})
	if lost != nil {
		return lost, nil
	}
	return nil, err
}

// finishAwaitResubmission releases the reservation and reports the outcome.
// Confirmed ownership loss replaces any pending outcome, so a TIMEOUT never
// becomes a retryable POLL for a claim this wait no longer holds.
func finishAwaitResubmission(
	bb *db.Blackboard,
	agentID, taskID string,
	authority *models.AgentAuthority,
	reservation *reviewReservation,
	result *AwaitResubmissionResult,
	primaryErr error,
) (*AwaitResubmissionResult, error) {
	if result != nil && result.Verdict == ResubmissionPoll {
		var halted *SystemHaltedError
		err := modifyLifecycleState(bb, authority, func(s *models.State) error {
			task := s.FindTask(taskID)
			if task == nil || reservation == nil || !reservation.heldBy(s, task, agentID) {
				return errReviewReservationLost
			}
			result.TaskStatus = task.Status
			if s.Config.Mode == models.SystemModeStopped {
				result.Verdict = ResubmissionAborted
				return nil
			}
			return RequireWorkAdmitted(s, "await-resubmission")
		})
		if stderrors.Is(err, errReviewReservationLost) {
			return reviewOwnershipLostResult(result.TaskStatus), nil
		}
		if stderrors.As(err, &halted) {
			return finishAwaitResubmission(bb, agentID, taskID, authority, reservation,
				&AwaitResubmissionResult{Verdict: ResubmissionPaused, TaskStatus: result.TaskStatus,
					Reason: pausedAwaitReason(halted.Mode), SafeAction: SafeActionStop}, nil)
		}
		if err == nil && result.Verdict != ResubmissionPoll {
			return finishAwaitResubmission(bb, agentID, taskID, authority, reservation, result, nil)
		}
		return result, err
	}
	terminal := result != nil && result.Verdict == ResubmissionTerminal
	lost, releaseErr := releaseReviewOwnership(bb, agentID, taskID, authority, reservation, terminal)
	if lost != nil {
		return lost, nil
	}
	return result, joinAwaitCleanupError(primaryErr, releaseErr)
}

func finishPausedAwaitResubmission(bb *db.Blackboard, task *models.Task, agentID string, authority *models.AgentAuthority, reservation *reviewReservation, mode models.SystemMode) (*AwaitResubmissionResult, error) {
	return finishAwaitResubmission(bb, agentID, task.ID, authority, reservation,
		&AwaitResubmissionResult{Verdict: ResubmissionPaused, TaskStatus: task.Status,
			Reason: pausedAwaitReason(mode), SafeAction: SafeActionStop}, nil)
}

func finishResubmissionInterval(bb *db.Blackboard, taskID, agentID string, authority *models.AgentAuthority, reservation *reviewReservation, status models.TaskStatus, poll bool) (*AwaitResubmissionResult, error) {
	state, task, err := readTaskState(bb, taskID)
	if err == nil && state.Config.Mode.HaltsWork() {
		return finishPausedAwaitResubmission(bb, task, agentID, authority, reservation, state.Config.Mode)
	}
	verdict := ResubmissionTimeout
	if err == nil && poll {
		verdict = ResubmissionPoll
	}
	return finishAwaitResubmission(bb, agentID, taskID, authority, reservation,
		&AwaitResubmissionResult{Verdict: verdict, TaskStatus: status}, err)
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
func handleResubmissionResult(projectRoot string, bb *db.Blackboard, task *models.Task, agentID string, authority *models.AgentAuthority, resolver *pipeline.Resolver, rolePair string, reservation *reviewReservation) (*AwaitResubmissionResult, error) {
	submitted, _ := resolver.SubmittedStatus(rolePair)

	if task.Status == submitted {
		return reclaimForReview(projectRoot, bb, task.ID, agentID, authority, resolver, rolePair, reservation)
	}

	// Terminal state — release ownership and report.
	return finishAwaitResubmission(bb, agentID, task.ID, authority, reservation, &AwaitResubmissionResult{
		Verdict:    ResubmissionTerminal,
		TaskStatus: task.Status,
		Reason:     fmt.Sprintf("task entered terminal status: %s", task.Status),
	}, nil)
}

// reclaimForReview atomically transitions the task from submitted to reviewing,
// refreshes the review lease, and sets the agent to reviewing status.
func reclaimForReview(projectRoot string, bb *db.Blackboard, taskID, agentID string, authority *models.AgentAuthority, resolver *pipeline.Resolver, rolePair string, reservation *reviewReservation) (*AwaitResubmissionResult, error) {
	reviewing, err := resolver.ReviewingStatus(rolePair)
	if err != nil {
		return finishAwaitResubmission(bb, agentID, taskID, authority, reservation, nil,
			&OperationalError{Message: "failed to resolve reviewing status", Err: err})
	}

	transitions := BuildPipelineTransitions(resolver)
	freshLease := time.Now().Add(reclaimReviewLeaseDuration)

	var reviewCommit string
	var baseCommit string
	var reviewCycle int
	var reviewBoundaryErr error
	var lostStatus models.TaskStatus
	_, candidate, readErr := readTaskState(bb, taskID)
	if readErr != nil {
		return finishAwaitResubmission(bb, agentID, taskID, authority, reservation, nil, readErr)
	}
	var preflight *ValidationPreflight
	if len(candidate.ValidationPrerequisites) > 0 {
		if candidate.Worktree == nil || *candidate.Worktree == "" {
			return finishAwaitResubmission(bb, agentID, taskID, authority, reservation, nil, validationError("worktree_unavailable"))
		}
		preflight, err = prepareResumedValidation(projectRoot, taskID, agentID, *candidate.Worktree, nil, authority)
		if err != nil {
			return finishAwaitResubmission(bb, agentID, taskID, authority, reservation, nil, err)
		}
	}

	modErr := modifyLifecycleState(bb, authority, func(s *models.State) error {
		if err := RequireWorkAdmitted(s, "await-resubmission reclaim"); err != nil {
			return err
		}
		if err := preflight.CheckCurrent(s); err != nil {
			return err
		}
		task := s.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		if reservation != nil && !reservation.heldBy(s, task, agentID) {
			lostStatus = task.Status
			return errReviewReservationLost
		}
		if len(task.ValidationPrerequisites) > 0 && preflight == nil {
			return validationError("context_changed")
		}
		// Early resubmission has no reservation yet. Preserve the ownership
		// observed before preflight and never take another reviewer's claim.
		if optionalValue(task.ReviewingBy) != optionalValue(candidate.ReviewingBy) ||
			(task.ReviewingBy != nil && *task.ReviewingBy != agentID) {
			return validationError("ownership_changed")
		}

		if err := validateReviewBoundaryForAssignment(projectRoot, s, task); err != nil {
			reviewBoundaryErr = err
			var evidenceErr *AcceptanceEvidenceError
			if stderrors.As(err, &evidenceErr) {
				return err
			}
			var repairNeeded *ReviewBoundaryRepairNeededError
			if stderrors.As(err, &repairNeeded) {
				return err
			}
			return markReviewBoundaryIntegrationFailed(s, task, agentID, transitions, err)
		}

		if err := task.TransitionWith(reviewing, transitions); err != nil {
			return fmt.Errorf("reclaim transition failed: %w", err)
		}
		models.AdvanceLifecycle(task)

		task.ReviewLeaseExpires = &freshLease
		task.ReviewingBy = &agentID

		if task.ReviewCommit != nil {
			reviewCommit = *task.ReviewCommit
		}
		if task.BaseCommit != nil {
			baseCommit = *task.BaseCommit
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
	if stderrors.Is(modErr, errReviewReservationLost) {
		return reviewOwnershipLostResult(lostStatus), nil
	}
	if modErr != nil {
		var halted *SystemHaltedError
		if stderrors.As(modErr, &halted) {
			return finishPausedAwaitResubmission(bb, candidate, agentID, authority, reservation, halted.Mode)
		}
		var evidenceErr *AcceptanceEvidenceError
		if stderrors.As(modErr, &evidenceErr) {
			return nil, evidenceErr
		}
		var repairNeeded *ReviewBoundaryRepairNeededError
		if stderrors.As(modErr, &repairNeeded) {
			return finishAwaitResubmission(bb, agentID, taskID, authority, reservation, nil, repairNeeded)
		}
		return finishAwaitResubmission(bb, agentID, taskID, authority, reservation, nil,
			&OperationalError{Message: "failed to reclaim task for review", Err: modErr})
	}
	if reviewBoundaryErr != nil {
		return nil, &IntegrationFailedError{Reason: IntegrationReasonReviewBoundaryMismatch}
	}

	return &AwaitResubmissionResult{
		Verdict:      ResubmissionResubmitted,
		TaskStatus:   reviewing,
		BaseCommit:   baseCommit,
		ReviewCommit: reviewCommit,
		ReviewCycle:  reviewCycle,
	}, nil
}

// awaitResubmissionPolling is the polling fallback for when fsnotify is unavailable.
// It checks state at the resolved interval until a resubmission or the deadline.
func awaitResubmissionPolling(ctx context.Context, projectRoot string, bb *db.Blackboard, taskID, agentID string, authority *models.AgentAuthority, reservation *reviewReservation, deadline time.Time, taskStatus models.TaskStatus, resolver *pipeline.Resolver, rolePair string, pollInterval time.Duration, pollOnTimeout bool) (*AwaitResubmissionResult, error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	deadlineTimer := time.NewTimer(time.Until(deadline))
	defer deadlineTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return finishAwaitResubmission(bb, agentID, taskID, authority, reservation, nil, ctx.Err())

		case <-ticker.C:
			state, err := bb.ReadCached()
			if err != nil {
				continue
			}
			if state.Config.Mode == models.SystemModeStopped {
				return finishAwaitResubmission(bb, agentID, taskID, authority, reservation,
					&AwaitResubmissionResult{Verdict: ResubmissionAborted}, nil)
			}
			currentTask := state.FindTask(taskID)
			if currentTask == nil {
				return finishAwaitResubmission(bb, agentID, taskID, authority, reservation, &AwaitResubmissionResult{
					Verdict: ResubmissionTerminal,
					Reason:  "task disappeared from state",
				}, nil)
			}
			taskStatus = currentTask.Status
			if state.Config.Mode.HaltsWork() {
				return finishPausedAwaitResubmission(bb, currentTask, agentID, authority, reservation, state.Config.Mode)
			}
			if rc := checkResubmissionStatus(currentTask, resolver, rolePair); rc != nil {
				return handleResubmissionResult(projectRoot, bb, currentTask, agentID, authority, resolver, rolePair, reservation)
			}
			if reservationLost(bb, reservation, state, currentTask, agentID) {
				return finishAwaitResubmission(bb, agentID, taskID, authority, reservation, reviewOwnershipLostResult(currentTask.Status), nil)
			}

		case <-deadlineTimer.C:
			return finishResubmissionInterval(bb, taskID, agentID, authority, reservation, taskStatus, pollOnTimeout)
		}
	}
}

// latestRejectionByAgent returns when this agent last rejected the task.
func latestRejectionByAgent(task *models.Task, agentID string) (time.Time, bool) {
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		if entry.Event != models.TaskEventRejected {
			continue
		}
		if entry.Agent == nil || *entry.Agent != agentID {
			continue
		}
		return entry.Time, true
	}
	return time.Time{}, false
}

// AwaitResubmissionRemainingBudget reports how much of total is left for this
// wait, measured from the reviewer's most recent rejection. Mirrors
// AwaitVerdictRemainingBudget: the bound holds without the caller carrying a
// value between invocations.
func AwaitResubmissionRemainingBudget(projectRoot, taskID, agentID string, total time.Duration) time.Duration {
	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	_, task, err := readTaskState(bb, taskID)
	if err != nil || task == nil {
		return cappedAwaitBudget(total)
	}
	rejectedAt, ok := latestRejectionByAgent(task, agentID)
	if !ok {
		return cappedAwaitBudget(total)
	}
	return remainingFromAnchor(rejectedAt, total)
}
