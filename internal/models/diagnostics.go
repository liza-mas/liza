package models

import (
	"fmt"
	"strings"
	"time"
)

// CountClaimableTasks counts tasks claimable by the given role.
// Uses IsClaimable which checks task type, status, and dependencies.
func CountClaimableTasks(state *State, role string, pr PipelineResolver) int {
	count := 0
	for i := range state.Tasks {
		if state.Tasks[i].IsClaimable(role, state.Tasks, pr) {
			count++
		}
	}
	return count
}

// CountReviewableTasks counts tasks immediately claimable by the reviewer role.
// Uses IsClaimable so each role-pair's reviewer states are honored.
func CountReviewableTasks(state *State, role string, pr PipelineResolver) int {
	count := 0
	for i := range state.Tasks {
		if state.Tasks[i].IsClaimable(role, state.Tasks, pr) {
			count++
		}
	}
	return count
}

// GetCoderWorkDiagnostics returns detailed diagnostic information about task availability for coders.
func GetCoderWorkDiagnostics(state *State, pr PipelineResolver) string {
	claimable := CountClaimableTasks(state, RoleCoder, pr)

	if claimable > 0 {
		return fmt.Sprintf("Found %d claimable task(s)", claimable)
	}

	blockedByDeps := 0
	inProgress := 0

	satisfiedIDs := make(map[string]bool)
	for _, task := range state.Tasks {
		if task.Status == TaskStatusMerged || task.Status == TaskStatusSuperseded {
			satisfiedIDs[task.ID] = true
		}
	}

	for _, task := range state.Tasks {
		// Pipeline path: use resolver to classify statuses dynamically.
		if task.RolePair != "" && pr != nil {
			if isBlockedByDepsPipeline(&task, pr, satisfiedIDs) {
				blockedByDeps++
			}
			if isInProgressPipeline(&task, pr) {
				inProgress++
			}
			continue
		}

		// Fallback: hardcoded status checks when resolver is unavailable.
		if task.Status == TaskStatusReady ||
			task.Status == TaskStatusRejected ||
			task.Status == TaskStatusIntegrationFailed {
			hasUnsatisfiedDeps := false
			for _, depID := range task.DependsOn {
				if !satisfiedIDs[depID] {
					hasUnsatisfiedDeps = true
					break
				}
			}
			if hasUnsatisfiedDeps {
				blockedByDeps++
			}
		}

		if task.Status == TaskStatusImplementing ||
			task.Status == TaskStatusReadyForReview ||
			task.Status == TaskStatusReviewing ||
			task.Status == TaskStatusApproved {
			inProgress++
		}
	}

	parts := []string{"No claimable tasks"}
	if blockedByDeps > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked by dependencies", blockedByDeps))
	}
	if inProgress > 0 {
		parts = append(parts, fmt.Sprintf("%d in progress", inProgress))
	}

	return strings.Join(parts, "; ")
}

// isBlockedByDepsPipeline checks if a pipeline task is in an initial/rejected status
// with unsatisfied dependencies.
func isBlockedByDepsPipeline(task *Task, pr PipelineResolver, satisfiedIDs map[string]bool) bool {
	initial, err := pr.InitialStatus(task.RolePair)
	if err != nil {
		return false
	}
	rejected, err := pr.RejectedStatus(task.RolePair)
	if err != nil {
		return false
	}
	if task.Status != initial && task.Status != rejected && task.Status != TaskStatusIntegrationFailed {
		return false
	}
	for _, depID := range task.DependsOn {
		if !satisfiedIDs[depID] {
			return true
		}
	}
	return false
}

// isInProgressPipeline checks if a pipeline task is in a pipeline-defined in-progress state.
func isInProgressPipeline(task *Task, pr PipelineResolver) bool {
	executing, _ := pr.ExecutingStatus(task.RolePair)
	submitted, _ := pr.SubmittedStatus(task.RolePair)
	reviewing, _ := pr.ReviewingStatus(task.RolePair)
	if task.Status == executing || task.Status == submitted || task.Status == reviewing {
		return true
	}
	// Quorum states are also in-progress (task is in the review pipeline).
	partiallyApproved, err := pr.PartiallyApprovedStatus(task.RolePair)
	if err == nil && task.Status == partiallyApproved {
		return true
	}
	reviewing2, err := pr.Reviewing2Status(task.RolePair)
	if err == nil && task.Status == reviewing2 {
		return true
	}
	return false
}

// GetReviewerWorkDiagnostics returns detailed diagnostic information about review availability.
// For pipeline tasks, filters by the resolved reviewer role to avoid counting tasks
// belonging to a different reviewer role (e.g. code-plan-reviewer).
func GetReviewerWorkDiagnostics(state *State, pr PipelineResolver) string {
	now := time.Now().UTC()

	unassigned := 0
	expiredLeases := 0
	activelyReviewing := 0
	awaitingSecondReview := 0
	inSecondReview := 0

	for _, task := range state.Tasks {
		// Pipeline path: use resolver to classify statuses dynamically.
		if task.RolePair != "" && pr != nil {
			// Gate by reviewer role: only count tasks whose pipeline reviewer
			// matches the code-reviewer runtime role.
			reviewerRole, err := pr.ReviewerRole(task.RolePair)
			if err != nil {
				continue
			}
			if reviewerRole != RoleCodeReviewer {
				continue
			}

			submitted, _ := pr.SubmittedStatus(task.RolePair)
			reviewing, _ := pr.ReviewingStatus(task.RolePair)
			partiallyApproved, errPA := pr.PartiallyApprovedStatus(task.RolePair)
			reviewing2, errR2 := pr.Reviewing2Status(task.RolePair)

			switch {
			case task.Status == submitted && task.ReviewCommit != nil:
				unassigned++
			case task.Status == reviewing:
				if task.ReviewLeaseExpires != nil && task.ReviewLeaseExpires.Before(now) {
					expiredLeases++
				} else {
					activelyReviewing++
				}
			case errPA == nil && task.Status == partiallyApproved && task.ReviewCommit != nil:
				awaitingSecondReview++
			case errR2 == nil && task.Status == reviewing2:
				if task.ReviewLeaseExpires != nil && task.ReviewLeaseExpires.Before(now) {
					expiredLeases++
				} else {
					inSecondReview++
				}
			}
			continue
		}

		// Fallback: hardcoded status checks when resolver is unavailable.
		if task.Status == TaskStatusReadyForReview && task.EffectiveType().HasRole(RoleCodeReviewer) && task.ReviewCommit != nil {
			unassigned++
		}
		if task.Status == TaskStatusReviewing && task.EffectiveType().HasRole(RoleCodeReviewer) {
			if task.ReviewLeaseExpires != nil && task.ReviewLeaseExpires.Before(now) {
				expiredLeases++
			} else {
				activelyReviewing++
			}
		}
	}

	if unassigned > 0 || awaitingSecondReview > 0 {
		parts := []string{fmt.Sprintf("Found %d reviewable task(s)", unassigned+awaitingSecondReview)}
		if awaitingSecondReview > 0 {
			parts = append(parts, fmt.Sprintf("%d awaiting second review", awaitingSecondReview))
		}
		if expiredLeases > 0 {
			parts = append(parts, fmt.Sprintf("%d with stale leases (pending reclamation)", expiredLeases))
		}
		if inSecondReview > 0 {
			parts = append(parts, fmt.Sprintf("%d in second review", inSecondReview))
		}
		return strings.Join(parts, "; ")
	}

	parts := []string{"No reviewable tasks"}
	if expiredLeases > 0 {
		parts = append(parts, fmt.Sprintf("%d with stale leases (pending reclamation)", expiredLeases))
	}
	if activelyReviewing > 0 {
		parts = append(parts, fmt.Sprintf("%d actively being reviewed", activelyReviewing))
	}
	if inSecondReview > 0 {
		parts = append(parts, fmt.Sprintf("%d in second review", inSecondReview))
	}

	return strings.Join(parts, "; ")
}
