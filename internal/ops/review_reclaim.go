package ops

import (
	stderrors "errors"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// reclaimTransaction is one reclaim of a resubmitted task for re-review: the
// candidate observed before the transaction, the inputs the transaction applies
// and what it reports back.
type reclaimTransaction struct {
	projectRoot string
	bb          *db.Blackboard
	taskID      string
	agentID     string
	authority   *models.AgentAuthority
	reservation *reviewReservation
	reviewing   models.TaskStatus
	transitions map[models.TaskStatus][]models.TaskStatus
	now         time.Time // read before the snapshot; the fresh lease counts from it
	freshLease  time.Time

	candidate *models.Task
	preflight *ValidationPreflight

	reviewBoundaryErr error
	lostStatus        models.TaskStatus
	reviewCommit      string
	baseCommit        string
	reviewCycle       int
}

// reclaimForReview atomically transitions the task from submitted to reviewing,
// refreshes the review lease, and sets the agent to reviewing status.
func reclaimForReview(projectRoot string, bb *db.Blackboard, taskID, agentID string, authority *models.AgentAuthority, resolver *pipeline.Resolver, rolePair string, reservation *reviewReservation) (*AwaitResubmissionResult, error) {
	reviewing, err := resolver.ReviewingStatus(rolePair)
	if err != nil {
		return finishAwaitResubmission(bb, agentID, taskID, authority, reservation, nil,
			&OperationalError{Message: "failed to resolve reviewing status", Err: err})
	}

	r := &reclaimTransaction{
		projectRoot: projectRoot,
		bb:          bb,
		taskID:      taskID,
		agentID:     agentID,
		authority:   authority,
		reservation: reservation,
		reviewing:   reviewing,
		transitions: BuildPipelineTransitions(resolver),
		now:         time.Now(),
	}
	if err := r.snapshotCandidate(); err != nil {
		return finishAwaitResubmission(bb, agentID, taskID, authority, reservation, nil, err)
	}
	return r.project(modifyLifecycleState(bb, authority, r.apply))
}

// snapshotCandidate reads the task the reclaim will act on and prepares its
// validation context, both outside the transaction. The snapshot never
// authorizes the write: apply revalidates it.
func (r *reclaimTransaction) snapshotCandidate() error {
	state, candidate, err := readTaskState(r.bb, r.taskID)
	if err != nil {
		return err
	}
	r.candidate = candidate
	r.freshLease = r.now.Add(time.Duration(LeaseDurationSeconds(state.Config)) * time.Second)
	if len(candidate.ValidationPrerequisites) == 0 {
		return nil
	}
	if candidate.Worktree == nil || *candidate.Worktree == "" {
		return validationError(validationCodeWorktreeUnavailable)
	}
	r.preflight, err = prepareResumedValidation(r.projectRoot, r.taskID, r.agentID, *candidate.Worktree, nil, r.authority)
	return err
}

// apply is the reclaim transaction. The inherited Git revalidation stays inside
// it, in this order: the validation preflight (K1-E3), then the review boundary
// (K1-E4).
func (r *reclaimTransaction) apply(s *models.State) error {
	task, err := r.admit(s)
	if err != nil {
		return err
	}
	holds, err := r.checkBoundary(s, task)
	if err != nil || !holds {
		return err
	}
	return r.assign(s, task)
}

// admit revalidates, in the locked state, everything the snapshot observed.
func (r *reclaimTransaction) admit(s *models.State) (*models.Task, error) {
	if err := RequireWorkAdmitted(s, awaitResubmissionReclaimOperation); err != nil {
		return nil, err
	}
	if err := r.preflight.CheckCurrent(s); err != nil {
		return nil, err
	}
	task := s.FindTask(r.taskID)
	if task == nil {
		return nil, &errors.NotFoundError{Entity: notFoundEntityTask, ID: r.taskID}
	}
	if r.reservation != nil && !r.reservation.heldBy(s, task, r.agentID) {
		r.lostStatus = task.Status
		return nil, errReviewReservationLost
	}
	if len(task.ValidationPrerequisites) > 0 && r.preflight == nil {
		return nil, validationError(validationCodeContextChanged)
	}
	// Early resubmission has no reservation yet. Preserve the ownership
	// observed before preflight and never take another reviewer's claim.
	if optionalValue(task.ReviewingBy) != optionalValue(r.candidate.ReviewingBy) ||
		(task.ReviewingBy != nil && *task.ReviewingBy != r.agentID) {
		return nil, validationError(validationCodeOwnershipChanged)
	}
	return task, nil
}

// checkBoundary reports whether the review boundary holds. Evidence and repair
// refusals are returned as errors and leave the task as it is. Any other
// failure routes the task to INTEGRATION_FAILED: the transaction then commits
// that routing without an error and without the reclaim, and the failure is
// reported after the commit.
func (r *reclaimTransaction) checkBoundary(s *models.State, task *models.Task) (holds bool, err error) {
	boundaryErr := validateReviewBoundaryForAssignment(r.projectRoot, s, task)
	if boundaryErr == nil {
		return true, nil
	}
	r.reviewBoundaryErr = boundaryErr
	var evidenceErr *AcceptanceEvidenceError
	if stderrors.As(boundaryErr, &evidenceErr) {
		return false, boundaryErr
	}
	var repairNeeded *ReviewBoundaryRepairNeededError
	if stderrors.As(boundaryErr, &repairNeeded) {
		return false, boundaryErr
	}
	return false, markReviewBoundaryIntegrationFailed(s, task, r.agentID, r.transitions, boundaryErr)
}

// assign moves the task to reviewing under a fresh lease and the agent to
// reviewing it, and records the commits the reviewer will examine.
func (r *reclaimTransaction) assign(s *models.State, task *models.Task) error {
	if err := task.TransitionWith(r.reviewing, r.transitions); err != nil {
		return fmt.Errorf("reclaim transition failed: %w", err)
	}
	models.AdvanceLifecycle(task)

	task.ReviewLeaseExpires = &r.freshLease
	task.ReviewingBy = &r.agentID

	if task.ReviewCommit != nil {
		r.reviewCommit = *task.ReviewCommit
	}
	if task.BaseCommit != nil {
		r.baseCommit = *task.BaseCommit
	}
	r.reviewCycle = task.ReviewCyclesCurrent

	agent, ok := s.Agents[r.agentID]
	if !ok {
		return &errors.NotFoundError{Entity: notFoundEntityAgent, ID: r.agentID}
	}
	agent.Status = models.AgentStatusReviewing
	agent.CurrentTask = &r.taskID
	s.Agents[r.agentID] = agent
	return nil
}

// project reports the transaction's outcome: the RESUBMITTED result, or the
// refusal with the reservation released where the wait still holds one.
func (r *reclaimTransaction) project(modErr error) (*AwaitResubmissionResult, error) {
	if stderrors.Is(modErr, errReviewReservationLost) {
		return reviewOwnershipLostResult(r.lostStatus), nil
	}
	if modErr != nil {
		return r.projectFailure(modErr)
	}
	if r.reviewBoundaryErr != nil {
		return nil, &IntegrationFailedError{Reason: IntegrationReasonReviewBoundaryMismatch}
	}
	return &AwaitResubmissionResult{
		Verdict:      string(ResubmissionResubmitted),
		TaskStatus:   r.reviewing,
		BaseCommit:   r.baseCommit,
		ReviewCommit: r.reviewCommit,
		ReviewCycle:  r.reviewCycle,
	}, nil
}

func (r *reclaimTransaction) projectFailure(modErr error) (*AwaitResubmissionResult, error) {
	var halted *SystemHaltedError
	if stderrors.As(modErr, &halted) {
		return finishPausedAwaitResubmission(r.bb, r.candidate, r.agentID, r.authority, r.reservation, halted.Mode)
	}
	var evidenceErr *AcceptanceEvidenceError
	if stderrors.As(modErr, &evidenceErr) {
		return nil, evidenceErr
	}
	var repairNeeded *ReviewBoundaryRepairNeededError
	if stderrors.As(modErr, &repairNeeded) {
		return finishAwaitResubmission(r.bb, r.agentID, r.taskID, r.authority, r.reservation, nil, repairNeeded)
	}
	return finishAwaitResubmission(r.bb, r.agentID, r.taskID, r.authority, r.reservation, nil,
		&OperationalError{Message: "failed to reclaim task for review", Err: modErr})
}
