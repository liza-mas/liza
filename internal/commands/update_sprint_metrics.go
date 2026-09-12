package commands

import (
	"fmt"

	"github.com/liza-mas/liza/internal/ops"
)

// UpdateSprintMetricsCommand recomputes sprint metrics and prints a summary.
// Delegates computation to ops.UpdateSprintMetrics.
func UpdateSprintMetricsCommand(projectRoot string) error {
	metrics, err := ops.UpdateSprintMetrics(projectRoot)
	if err != nil {
		return err
	}

	fmt.Println("Sprint metrics updated:")
	fmt.Printf("  Tasks done: %d\n", metrics.TasksDone)
	fmt.Printf("  Tasks in progress: %d\n", metrics.TasksInProgress)
	fmt.Printf("  Tasks blocked: %d\n", metrics.TasksBlocked)
	fmt.Printf("  Total iterations: %d\n", metrics.IterationsTotal)
	fmt.Printf("  Total review cycles: %d\n", metrics.ReviewCyclesTotal)
	fmt.Printf("  Review verdicts: %d approvals, %d rejections (%d%%)\n",
		metrics.ReviewVerdictApprovals,
		metrics.ReviewVerdictRejections,
		metrics.ReviewVerdictApprovalRatePercent)
	fmt.Printf("  Task outcomes: %d submitted, %d%% approval rate\n",
		metrics.TaskSubmittedForReviewCount,
		metrics.TaskOutcomeApprovalRatePercent)

	fmt.Print(formatLifecycleMetrics(metrics.LifecycleOutcomes))

	// Check for suspicious approval rates (>95%)
	warnings := ops.CheckSuspiciousRates(metrics)
	if len(warnings) > 0 {
		fmt.Println()
		for _, warning := range warnings {
			fmt.Println(warning)
		}
	}

	return nil
}
