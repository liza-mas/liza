package ops

import (
	"errors"
	"fmt"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

// UpdateSprintMetrics recomputes sprint metrics from current task and agent state.
// Returns the computed metrics. No terminal I/O.
func UpdateSprintMetrics(projectRoot string) (models.SprintMetrics, error) {
	return updateSprintMetricsWithOptionalAuthority(projectRoot, nil)
}

// UpdateSprintMetricsWithAuthority fences the metrics follow-up write with the
// same registration generation as its parent lifecycle operation.
func UpdateSprintMetricsWithAuthority(projectRoot string, authority models.AgentAuthority) (models.SprintMetrics, error) {
	return updateSprintMetricsWithOptionalAuthority(projectRoot, &authority)
}

func updateSprintMetricsWithOptionalAuthority(projectRoot string, authority *models.AgentAuthority) (models.SprintMetrics, error) {
	statePath := paths.New(projectRoot).StatePath()
	blackboard := db.For(statePath)

	state, err := blackboard.Read()
	if err != nil {
		return models.SprintMetrics{}, fmt.Errorf("failed to read state: %w", err)
	}

	terminalStates, err := sprintTerminalStatesForMetrics(projectRoot)
	if err != nil {
		return models.SprintMetrics{}, err
	}
	metrics := state.ComputeSprintMetricsWithTerminalStates(terminalStates)
	sprint := CaptureLifecycleSprint(state.Sprint)
	metrics.LifecycleOutcomes = ReadLifecycleOutcomes(projectRoot, sprint)

	err = lifecycleMutation(blackboard, authority)(func(s *models.State) error {
		return applyLifecycleSprintMetrics(s, sprint, metrics)
	})

	if err != nil {
		return models.SprintMetrics{}, fmt.Errorf("failed to update sprint metrics: %w", err)
	}

	return metrics, nil
}

func sprintTerminalStatesForMetrics(projectRoot string) ([]models.TaskStatus, error) {
	terminalStates, err := SprintTerminalStates(projectRoot)
	if err != nil {
		if errors.Is(err, pipeline.ErrConfigNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("pipeline config failed to load: %w", err)
	}
	return terminalStates, nil
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
