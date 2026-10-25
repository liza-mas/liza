package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func recoverTaskReleaseClaims(state *models.State, task *models.Task, resolver models.PipelineResolver, pipelineTransitions map[models.TaskStatus][]models.TaskStatus, statuses recoverTaskStatusSet, reason string, now time.Time, agentsToRecover map[string]bool, result *RecoverTaskResult) bool {
	claimReleaseFailed := false
	if task.AssignedTo != nil {
		currentCoderID := *task.AssignedTo
		if task.Status == models.TaskStatusBlocked || isRecoverTaskReviewCandidate(task.Status, statuses) || task.Status == models.TaskStatusIntegrationFailed {
			releaseRecoverTaskAgent(state, task, currentCoderID)
			result.ClaimReleased = true
		} else {
			effectiveCoderRelease := resolveDoerClaimReleaseStatus(task, resolver)
			released, err := releaseOneClaim(state, task, effectiveCoderRelease, pipelineTransitions, true, currentCoderID, reason, now)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("coder claim release: %v", err))
				delete(agentsToRecover, currentCoderID)
				claimReleaseFailed = true
			}
			if released {
				result.ClaimReleased = true
			}
		}
	}
	if task.ReviewingBy != nil {
		currentReviewerID := *task.ReviewingBy
		if task.Status == models.TaskStatusBlocked {
			releaseRecoverTaskReviewerAgent(state, task, currentReviewerID)
			result.ClaimReleased = true
			return claimReleaseFailed
		}
		effectiveReviewerRelease, err := resolveReviewerClaimReleaseStatus(task, resolver)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("reviewer claim release: %v", err))
			delete(agentsToRecover, currentReviewerID)
			claimReleaseFailed = true
		} else {
			released, err := releaseOneClaim(state, task, effectiveReviewerRelease, pipelineTransitions, true, currentReviewerID, reason, now)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("reviewer claim release: %v", err))
				delete(agentsToRecover, currentReviewerID)
				claimReleaseFailed = true
			}
			if released {
				result.ClaimReleased = true
			}
		}
	}
	return claimReleaseFailed
}

func releaseRecoverTaskAgent(state *models.State, task *models.Task, agentID string) {
	models.AdvanceLifecycle(task)
	if agent, ok := state.Agents[agentID]; ok {
		if agent.CurrentTask != nil && *agent.CurrentTask == task.ID {
			state.ReleaseAgent(agentID)
		}
	}
	task.AssignedTo = nil
	task.LeaseExpires = nil
}

func releaseRecoverTaskReviewerAgent(state *models.State, task *models.Task, agentID string) {
	models.AdvanceLifecycle(task)
	if agent, ok := state.Agents[agentID]; ok {
		if agent.CurrentTask != nil && *agent.CurrentTask == task.ID {
			state.ReleaseAgent(agentID)
		}
	}
	task.ReviewingBy = nil
	task.ReviewLeaseExpires = nil
}

func recoverTaskNeedsReviewCommitReset(task *models.Task, statuses recoverTaskStatusSet) bool {
	return task.ReviewCommit == nil && isRecoverTaskReviewCandidate(task.Status, statuses)
}

// recoverTaskResetMissingReviewCommit sends a review-candidate task that lost its
// review_commit back to its initial status.
func recoverTaskResetMissingReviewCommit(task *models.Task, statuses recoverTaskStatusSet, result *RecoverTaskResult) {
	if !recoverTaskNeedsReviewCommitReset(task, statuses) {
		return
	}
	task.Status = statuses.Initial
	task.ReviewingBy = nil
	task.ReviewLeaseExpires = nil
	clearAttemptState(task, attemptStateInitialReset)
	result.Warnings = append(result.Warnings,
		fmt.Sprintf("reset %s to %s: missing review_commit", task.ID, statuses.Initial))
}

func isRecoverTaskReviewCandidate(status models.TaskStatus, statuses recoverTaskStatusSet) bool {
	return status == statuses.Submitted ||
		status == statuses.Reviewing ||
		status == statuses.Approved ||
		(statuses.PartiallyApproved != "" && status == statuses.PartiallyApproved) ||
		(statuses.Reviewing2 != "" && status == statuses.Reviewing2)
}

func recoverTaskHumanNote(taskID, reason string, agentsToRecover map[string]bool, now time.Time) models.HumanNote {
	var recoveredAgents []string
	for agentID := range agentsToRecover {
		recoveredAgents = append(recoveredAgents, agentID)
	}
	msg := fmt.Sprintf("Task %s recovered: %s", taskID, reason)
	if len(recoveredAgents) == 1 {
		msg = fmt.Sprintf("Task %s recovered (was held by %s): %s", taskID, recoveredAgents[0], reason)
	} else if len(recoveredAgents) > 1 {
		msg = fmt.Sprintf("Task %s recovered (was held by %v): %s", taskID, recoveredAgents, reason)
	}
	note := models.HumanNote{
		Timestamp: now,
		Message:   msg,
		For:       taskID,
	}
	note.MarkSeenByOrchestrator(now) // audit record, not a request
	return note
}
