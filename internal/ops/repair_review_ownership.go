package ops

import (
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
)

// RepairInvalidReviewOwnership clears active review claims whose task-side
// reviewer and agent-side owner state cannot both be true.
func RepairInvalidReviewOwnership(statePath, projectRoot, logPath, reason string) (int, error) {
	if reason == "" {
		reason = "validate --repair"
	}

	bb := db.For(statePath)
	logger := log.New(logPath)

	pb, err := loadPipelineBundle(projectRoot)
	if err != nil {
		return 0, fmt.Errorf("failed to load pipeline config: %w", err)
	}

	repaired := 0
	now := time.Now().UTC()

	err = bb.Modify(func(state *models.State) error {
		for i := range state.Tasks {
			task := &state.Tasks[i]

			match, err := detectReviewingState(task, pb)
			if err != nil {
				return err
			}
			if match == nil {
				continue
			}

			repairReason := invalidActiveReviewOwnershipReason(state, task, pb.pr)
			if repairReason == "" {
				continue
			}

			reviewerID := ""
			if task.ReviewingBy != nil {
				reviewerID = *task.ReviewingBy
			}

			if err := task.TransitionWith(match.revertStatus, pb.transitions); err != nil {
				return err
			}
			task.ReviewingBy = nil
			task.ReviewLeaseExpires = nil
			models.AdvanceLifecycle(task)

			if reviewerID != "" {
				if agent, ok := state.Agents[reviewerID]; ok {
					if agent.CurrentTask != nil && *agent.CurrentTask == task.ID {
						state.ReleaseAgent(reviewerID)
					}
				}
			}

			detail := fmt.Sprintf("Invalid active review ownership repaired (%s; reason: %s)", reason, repairReason)
			if err := logger.Append(log.Entry{
				Timestamp: now,
				Agent:     "system",
				Action:    "invalid_review_ownership_repaired",
				Task:      &task.ID,
				Detail:    detail,
			}); err != nil {
				return fmt.Errorf("failed to log invalid review ownership repair for %s: %w", task.ID, err)
			}

			repaired++
		}

		return nil
	})

	if err != nil {
		return 0, fmt.Errorf("failed to repair invalid review ownership: %w", err)
	}

	return repaired, nil
}

func invalidActiveReviewOwnershipReason(state *models.State, task *models.Task, pr models.PipelineResolver) string {
	if task.ReviewingBy == nil || strings.TrimSpace(*task.ReviewingBy) == "" {
		return "missing reviewing_by"
	}
	if task.ReviewLeaseExpires == nil {
		return "missing review_lease_expires"
	}

	reviewerID := *task.ReviewingBy
	agent, exists := state.Agents[reviewerID]
	if !exists {
		return fmt.Sprintf("reviewing_by %s has no matching agent", reviewerID)
	}

	expectedRole, err := pr.ReviewerRole(task.RolePair)
	if err != nil {
		return fmt.Sprintf("reviewer role resolution failed for role_pair %q: %v", task.RolePair, err)
	}
	if agent.Role != expectedRole {
		return fmt.Sprintf("reviewing_by %s has role %q, want %q", reviewerID, agent.Role, expectedRole)
	}
	if agent.Status != models.AgentStatusReviewing {
		return fmt.Sprintf("reviewing_by %s has agent status %s, want REVIEWING", reviewerID, agent.Status)
	}
	if agent.CurrentTask == nil || *agent.CurrentTask != task.ID {
		return fmt.Sprintf("reviewing_by %s has mismatched current_task", reviewerID)
	}
	if agent.LeaseExpires == nil {
		return fmt.Sprintf("reviewing_by %s has agent without lease_expires", reviewerID)
	}
	if agent.PID <= 0 {
		return fmt.Sprintf("reviewing_by %s has agent without pid", reviewerID)
	}

	return ""
}
