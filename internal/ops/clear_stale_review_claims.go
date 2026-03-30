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
					var expiredAt string
					switch {
					case task.ReviewLeaseExpires == nil:
						expiredAt = "unknown (lease missing)"
					case !task.ReviewLeaseExpires.After(now):
						expiredAt = task.ReviewLeaseExpires.Format(time.RFC3339)
					default:
						continue // lease still valid
					}

					staleReviewer := *task.ReviewingBy
					task.ReviewingBy = nil
					task.ReviewLeaseExpires = nil

					if a, ok := state.Agents[staleReviewer]; ok {
						if a.CurrentTask != nil && *a.CurrentTask == task.ID {
							state.ReleaseAgent(staleReviewer)
						}
					}

					detail := fmt.Sprintf("Orphaned ReviewingBy cleared (expired at %s, reviewer: %s, task status: %s)",
						expiredAt, staleReviewer, task.Status)
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

			var expiredAt string
			switch {
			case task.ReviewLeaseExpires == nil:
				expiredAt = "unknown (lease missing)"
			case !task.ReviewLeaseExpires.After(now):
				expiredAt = task.ReviewLeaseExpires.Format(time.RFC3339)
			default:
				continue
			}

			staleReviewer := *task.ReviewingBy

			// Revert to submitted state and clear the stale claim.
			if err := task.TransitionWith(match.revertStatus, pb.transitions); err != nil {
				return err
			}
			task.ReviewingBy = nil
			task.ReviewLeaseExpires = nil

			if a, ok := state.Agents[staleReviewer]; ok {
				if a.CurrentTask != nil && *a.CurrentTask == task.ID {
					state.ReleaseAgent(staleReviewer)
				}
			}

			detail := fmt.Sprintf("Review claim expired at %s (reviewer: %s)", expiredAt, staleReviewer)
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
