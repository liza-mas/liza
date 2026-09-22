package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// reviewMatch captures the detection result for a task in a reviewing state.
type reviewMatch struct {
	revertStatus models.TaskStatus
}

// ClearStaleReviewClaims finds and clears expired review leases on reviewing tasks.
// Returns the number of claims cleared.
func ClearStaleReviewClaims(projectRoot string) (int, error) {
	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	logger := log.New(lp.LogPath())

	// Load pipeline config for detection and transition.
	pb, err := loadPipelineBundle(projectRoot)
	if err != nil {
		return 0, fmt.Errorf("failed to load pipeline config: %w", err)
	}

	cleared := 0
	now := time.Now().UTC()

	err = bb.Modify(func(state *models.State) error {
		for i := range state.Tasks {
			task := &state.Tasks[i]

			// Determine if this task is in a reviewing state.
			match, err := detectReviewingState(task, pb)
			if err != nil {
				return err
			}
			if match == nil {
				// Not in a reviewing state — check for orphaned ReviewingBy
				// from an await_resubmission crash.
				if task.ReviewingBy != nil {
					staleReason := staleReviewClaimReason(state, task, now, pb, false)
					if staleReason == "" {
						continue
					}

					staleReviewer := *task.ReviewingBy
					task.ReviewingBy = nil
					task.ReviewLeaseExpires = nil
					models.AdvanceLifecycle(task)

					if a, ok := state.Agents[staleReviewer]; ok {
						if a.CurrentTask != nil && *a.CurrentTask == task.ID {
							state.ReleaseAgent(staleReviewer)
						}
					}

					detail := fmt.Sprintf("Orphaned ReviewingBy cleared (%s, reviewer: %s, task status: %s)",
						staleReason, staleReviewer, task.Status)
					logEntry := log.Entry{
						Timestamp: now,
						Agent:     "system",
						Action:    "stale_review_cleared",
						Task:      &task.ID,
						Detail:    detail,
					}
					if err := logger.Append(logEntry); err != nil {
						return fmt.Errorf("failed to log orphaned ReviewingBy cleanup for %s: %w", task.ID, err)
					}
					cleared++
				}
				continue
			}

			// Task is in a reviewing state — handle stale review claim.
			if task.ReviewingBy == nil {
				continue
			}

			staleReason := staleReviewClaimReason(state, task, now, pb, true)
			if staleReason == "" {
				continue
			}

			staleReviewer := *task.ReviewingBy

			// Revert to submitted state and clear the stale claim.
			if err := task.TransitionWith(match.revertStatus, pb.transitions); err != nil {
				return err
			}
			task.ReviewingBy = nil
			task.ReviewLeaseExpires = nil
			models.AdvanceLifecycle(task)

			if a, ok := state.Agents[staleReviewer]; ok {
				if a.CurrentTask != nil && *a.CurrentTask == task.ID {
					state.ReleaseAgent(staleReviewer)
				}
			}

			detail := fmt.Sprintf("Review claim cleared (%s, reviewer: %s)", staleReason, staleReviewer)
			logEntry := log.Entry{
				Timestamp: now,
				Agent:     "system",
				Action:    "stale_review_cleared",
				Task:      &task.ID,
				Detail:    detail,
			}
			if err := logger.Append(logEntry); err != nil {
				return fmt.Errorf("failed to log stale review cleanup for %s: %w", task.ID, err)
			}

			cleared++
		}

		return nil
	})

	if err != nil {
		return 0, fmt.Errorf("failed to clear stale review claims: %w", err)
	}

	return cleared, nil
}

func staleReviewClaimReason(state *models.State, task *models.Task, now time.Time, pb *pipelineBundle, activeReview bool) string {
	if task.ReviewingBy == nil {
		return ""
	}
	if task.ReviewLeaseExpires == nil {
		return "lease missing"
	}
	if !task.ReviewLeaseExpires.After(now) {
		return fmt.Sprintf("lease expired at %s", task.ReviewLeaseExpires.Format(time.RFC3339))
	}

	reviewerID := *task.ReviewingBy
	agent, ok := state.Agents[reviewerID]
	if !ok {
		return "reviewer agent missing"
	}
	if agent.PID <= 0 {
		return "reviewer agent has no usable pid"
	}
	expectedRole, err := pb.pr.ReviewerRole(task.RolePair)
	if err != nil {
		return fmt.Sprintf("reviewer role resolution failed: %v", err)
	}
	if agent.Role != expectedRole {
		return fmt.Sprintf("reviewer agent role is %s, want %s", agent.Role, expectedRole)
	}
	wantStatus := models.AgentStatusReviewing
	if !activeReview {
		if !isPassiveReviewOwnershipStatus(task, pb) {
			return fmt.Sprintf("task status %s is not passive review ownership", task.Status)
		}
		wantStatus = models.AgentStatusWaiting
	}
	if agent.Status != wantStatus {
		return fmt.Sprintf("reviewer agent status is %s, want %s", agent.Status, wantStatus)
	}
	if agent.CurrentTask == nil || *agent.CurrentTask != task.ID {
		return "reviewer agent current_task does not match task"
	}
	// A coherent claim with fresh registration evidence remains owned even
	// when the observer's PID namespace cannot identify its supervisor.
	if agent.LeaseExpires != nil && agent.LeaseExpires.After(now) && !agent.Heartbeat.IsZero() {
		return ""
	}
	if status := AgentProcessStatus(reviewerID, agent); !status.IsLiveOrUnknown() {
		return fmt.Sprintf("reviewer process is %s (%s)", status.State, status.Detail)
	}
	return ""
}

func isPassiveReviewOwnershipStatus(task *models.Task, pb *pipelineBundle) bool {
	if pb == nil || pb.pr == nil {
		return false
	}
	if models.IsSubmittedStatus(task, pb.pr) {
		return true
	}
	rejected, err := pb.pr.RejectedStatus(task.RolePair)
	if err == nil && task.Status == rejected {
		return true
	}
	return models.IsExecutingStatus(task, pb.pr)
}

// detectReviewingState checks whether a task is in a reviewing state
// (either reviewing or reviewing_2).
// Returns (nil, nil) if the task is not in a reviewing state.
// Returns a non-nil error if the task IS in a reviewing state but the
// revert status cannot be resolved — callers should surface this rather than
// silently skipping, as it would leave the task stuck.
func detectReviewingState(task *models.Task, pb *pipelineBundle) (*reviewMatch, error) {
	if task.RolePair == "" {
		return nil, nil
	}

	// Check reviewing (first review) → reverts to submitted.
	reviewing, err := pb.pr.ReviewingStatus(task.RolePair)
	if err != nil {
		return nil, nil // unknown role-pair, not a reviewing state
	}
	if task.Status == reviewing {
		submitted, err := pb.pr.SubmittedStatus(task.RolePair)
		if err != nil {
			return nil, fmt.Errorf("task %s is in reviewing state %s but submitted status resolution failed for role-pair %q: %w",
				task.ID, task.Status, task.RolePair, err)
		}
		return &reviewMatch{revertStatus: submitted}, nil
	}

	// Check reviewing_2 (second review) → reverts to partially_approved.
	reviewing2, err := pb.pr.Reviewing2Status(task.RolePair)
	if err != nil {
		return nil, nil // no reviewing-2 state configured, not applicable
	}
	if task.Status == reviewing2 {
		partiallyApproved, err := pb.pr.PartiallyApprovedStatus(task.RolePair)
		if err != nil {
			return nil, fmt.Errorf("task %s is in reviewing-2 state %s but partially-approved status resolution failed for role-pair %q: %w",
				task.ID, task.Status, task.RolePair, err)
		}
		return &reviewMatch{revertStatus: partiallyApproved}, nil
	}

	return nil, nil
}
