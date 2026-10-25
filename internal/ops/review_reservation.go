package ops

import (
	stderrors "errors"
	"fmt"
	"reflect"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
)

// reviewOwnershipLeaseMargin is added beyond the await deadline so the lease
// outlives the blocking call, preventing premature stale-claim cleanup.
const reviewOwnershipLeaseMargin = 5 * time.Minute

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

// retainedReservation is the reservation the agent already holds on the task in
// the observed state, or nil when it holds none. A wait that ended before its
// result was read leaves it behind, and the next await must neither reacquire nor
// release a claim it never made.
func retainedReservation(state *models.State, task *models.Task, agentID string) *reviewReservation {
	observed := &reviewReservation{historyLen: len(task.History)}
	if observed.heldBy(state, task, agentID) {
		return observed
	}
	return nil
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
	return &AwaitResubmissionResult{Verdict: string(ResubmissionAborted), TaskStatus: status, Reason: "review ownership lost"}
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
		if err := RequireWorkAdmitted(s, awaitResubmissionOperation); err != nil {
			return err
		}

		if task.ReviewingBy != nil && *task.ReviewingBy != agentID {
			return WrapLifecycleError("claim-reviewer-task", task, fmt.Errorf("review ownership changed"), models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone)
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

// undoAfterEvidenceRefusal ends the wait's ownership after the reclaim was
// refused on acceptance evidence, retaining the doer's resubmission and any
// subsequent reassignment.
func (o *reviewOwnershipSnapshot) undoAfterEvidenceRefusal(bb *db.Blackboard, agentID, taskID string, authority *models.AgentAuthority) error {
	if o.task.ReviewingBy != nil {
		_, err := releaseReviewOwnership(bb, agentID, taskID, authority, o.reservation, false)
		return err
	}
	// Evidence refusal must undo the wait's ownership, including WAITING,
	// while retaining the doer's resubmission and any subsequent reassignment.
	return modifyLifecycleState(bb, authority, func(s *models.State) error {
		o.restoreIn(s, agentID, taskID)
		return nil
	})
}

// restoreIn undoes the acquisition in the locked state when this wait's
// reservation still stands.
func (o *reviewOwnershipSnapshot) restoreIn(s *models.State, agentID, taskID string) {
	currentTask := s.FindTask(taskID)
	if currentTask == nil || !o.reservation.heldBy(s, currentTask, agentID) {
		return
	}
	// Undo only our temporary revision. A changed status keeps both the
	// pre-wait and waiting tokens stale; never revive an old preparation
	// or overwrite a completion written during the wait. Otherwise
	// retain receipts and retire pending work at the ownership boundary.
	if currentTask.Status != o.task.Status &&
		(o.task.Lifecycle == nil || o.task.Lifecycle.Preparation == nil) &&
		reflect.DeepEqual(currentTask.Lifecycle, o.lifecycle) {
		currentTask.Lifecycle = o.task.Lifecycle
	} else {
		models.AdvanceLifecycle(currentTask)
	}
	currentTask.ReviewingBy = o.task.ReviewingBy
	currentTask.ReviewLeaseExpires = o.task.ReviewLeaseExpires
	if agent, ok := s.Agents[agentID]; ok && agent.Status == models.AgentStatusWaiting &&
		agent.CurrentTask != nil && *agent.CurrentTask == taskID {
		agent.Status = o.agent.Status
		agent.CurrentTask = o.agent.CurrentTask
		s.Agents[agentID] = agent
	}
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
	if result != nil && result.Verdict == string(ResubmissionPoll) {
		var halted *SystemHaltedError
		err := modifyLifecycleState(bb, authority, func(s *models.State) error {
			task := s.FindTask(taskID)
			if task == nil || reservation == nil || !reservation.heldBy(s, task, agentID) {
				return errReviewReservationLost
			}
			result.TaskStatus = task.Status
			if s.Config.Mode == models.SystemModeStopped {
				result.Verdict = string(ResubmissionAborted)
				return nil
			}
			return RequireWorkAdmitted(s, awaitResubmissionOperation)
		})
		if stderrors.Is(err, errReviewReservationLost) {
			return reviewOwnershipLostResult(result.TaskStatus), nil
		}
		if stderrors.As(err, &halted) {
			return finishAwaitResubmission(bb, agentID, taskID, authority, reservation,
				&AwaitResubmissionResult{Verdict: string(ResubmissionPaused), TaskStatus: result.TaskStatus,
					Reason: pausedAwaitReason(halted.Mode), SafeAction: string(SafeActionStop)}, nil)
		}
		if err == nil && result.Verdict != string(ResubmissionPoll) {
			return finishAwaitResubmission(bb, agentID, taskID, authority, reservation, result, nil)
		}
		return result, err
	}
	terminal := result != nil && result.Verdict == string(ResubmissionTerminal)
	lost, releaseErr := releaseReviewOwnership(bb, agentID, taskID, authority, reservation, terminal)
	if lost != nil {
		return lost, nil
	}
	return result, joinAwaitCleanupError(primaryErr, releaseErr)
}

func finishPausedAwaitResubmission(bb *db.Blackboard, task *models.Task, agentID string, authority *models.AgentAuthority, reservation *reviewReservation, mode models.SystemMode) (*AwaitResubmissionResult, error) {
	return finishAwaitResubmission(bb, agentID, task.ID, authority, reservation,
		&AwaitResubmissionResult{Verdict: string(ResubmissionPaused), TaskStatus: task.Status,
			Reason: pausedAwaitReason(mode), SafeAction: string(SafeActionStop)}, nil)
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
		&AwaitResubmissionResult{Verdict: string(verdict), TaskStatus: status}, err)
}
