package ops

import "github.com/liza-mas/liza/internal/models"

type attemptStateCleanupProfile int

const (
	attemptStateReviewRejection attemptStateCleanupProfile = iota
	attemptStateClaimReleaseReset
	attemptStateInitialReset
	attemptStateRetire
	attemptStateIntegrationFixClaim
)

func clearAttemptState(task *models.Task, profile attemptStateCleanupProfile) {
	switch profile {
	case attemptStateReviewRejection:
		// Rejection and retirement currently clear the same submitted-attempt
		// fields, but they remain separate profiles because one returns to
		// rework and the other closes the task.
		clearSubmittedAttemptState(task)
	case attemptStateClaimReleaseReset:
		task.Output = nil
		clearSubmittedAttemptState(task)
	case attemptStateInitialReset:
		task.Output = nil
		clearSubmittedAttemptState(task)
		task.FailedBy = nil
	case attemptStateRetire:
		clearSubmittedAttemptState(task)
	case attemptStateIntegrationFixClaim:
		// Repair reuses the worktree and its structured deliverables. Clearing
		// output here silently loses downstream tasks when the plan is resubmitted.
		clearSubmittedAttemptState(task)
	}
}

func clearSubmittedAttemptState(task *models.Task) {
	task.AcceptanceReceipt = nil
	task.ReviewCommit = nil
	task.ApprovedBy = nil
	task.ClearApprovals()
	task.MergeCommit = nil
	task.IntegrationFailure = nil
}
