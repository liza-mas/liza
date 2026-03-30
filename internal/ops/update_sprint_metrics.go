package ops

import (
	"fmt"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// UpdateSprintMetrics recomputes sprint metrics from current task and agent state.
// Returns the computed metrics. No terminal I/O.
func UpdateSprintMetrics(projectRoot string) (models.SprintMetrics, error) {
	statePath := paths.New(projectRoot).StatePath()
	blackboard := db.For(statePath)

	state, err := blackboard.Read()
	if err != nil {
		return models.SprintMetrics{}, fmt.Errorf("failed to read state: %w", err)
	}

	metrics := ComputeSprintMetrics(state)

	err = blackboard.Modify(func(s *models.State) error {
		s.Sprint.Metrics = metrics
		return nil
	})

	if err != nil {
		return models.SprintMetrics{}, fmt.Errorf("failed to update sprint metrics: %w", err)
	}

	return metrics, nil
}

// ComputeSprintMetrics calculates all sprint metrics from current state.
func ComputeSprintMetrics(state *models.State) models.SprintMetrics {
	metrics := models.SprintMetrics{}
	approvedOrMerged := 0

	for _, task := range state.Tasks {
		if task.Status.IsTerminal() {
			metrics.TasksDone++
		}

		if task.Status == models.TaskStatusImplementing ||
			task.Status == models.TaskStatusReadyForReview ||
			task.Status == models.TaskStatusReviewing ||
			task.Status == models.TaskStatusRejected ||
			task.Status == models.TaskStatusIntegrationFailed {
			metrics.TasksInProgress++
		}

		if task.Status == models.TaskStatusBlocked {
			metrics.TasksBlocked++
		}

		metrics.ReviewCyclesTotal += task.ReviewCyclesTotal

		hasSubmitted := false
		for _, entry := range task.History {
			switch entry.Event {
			case models.TaskEventSubmittedForReview:
				hasSubmitted = true
			case models.TaskEventApproved:
				metrics.ReviewVerdictApprovals++
				metrics.ReviewVerdictCount++
			case models.TaskEventRejected:
				metrics.ReviewVerdictRejections++
				metrics.ReviewVerdictCount++
			}
		}

		if hasSubmitted {
			metrics.TaskSubmittedForReviewCount++
			if task.Status == models.TaskStatusApproved || task.Status == models.TaskStatusMerged {
				approvedOrMerged++
			}
		}
	}

	// Aggregate iterations from agents
	for _, agent := range state.Agents {
		metrics.IterationsTotal += agent.IterationsTotal
	}

	if metrics.ReviewVerdictCount > 0 {
		metrics.ReviewVerdictApprovalRatePercent = (metrics.ReviewVerdictApprovals * 100) / metrics.ReviewVerdictCount
	}

	// Task outcome approval rate: submitted tasks that ended up approved or merged
	if metrics.TaskSubmittedForReviewCount > 0 {
		metrics.TaskOutcomeApprovalRatePercent = (approvedOrMerged * 100) / metrics.TaskSubmittedForReviewCount
	}

	return metrics
}

// CheckSuspiciousRates returns warnings if approval rates are suspiciously high (>95%).
func CheckSuspiciousRates(metrics models.SprintMetrics) []string {
	var warnings []string

	// Check review verdict approval rate
	if metrics.ReviewVerdictCount >= 3 && metrics.ReviewVerdictApprovalRatePercent > 95 {
		warnings = append(warnings, fmt.Sprintf(
			"⚠️  Review verdict approval rate is %d%% (%d/%d) - suspiciously high",
			metrics.ReviewVerdictApprovalRatePercent,
			metrics.ReviewVerdictApprovals,
			metrics.ReviewVerdictCount,
		))
	}

	// Check task outcome approval rate
	if metrics.TaskSubmittedForReviewCount >= 3 && metrics.TaskOutcomeApprovalRatePercent > 95 {
		// Calculate approved/merged count for the message
		approvedOrMergedCount := (metrics.TaskOutcomeApprovalRatePercent * metrics.TaskSubmittedForReviewCount) / 100
		warnings = append(warnings, fmt.Sprintf(
			"⚠️  Task outcome approval rate is %d%% (%d/%d) - suspiciously high",
			metrics.TaskOutcomeApprovalRatePercent,
			approvedOrMergedCount,
			metrics.TaskSubmittedForReviewCount,
		))
	}

	return warnings
}
