package ops

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// ErrSprintAlreadyCheckpoint indicates the sprint is already at CHECKPOINT.
var ErrSprintAlreadyCheckpoint = errors.New("sprint is already at CHECKPOINT")

// SprintCheckpointResult contains the outcome of creating a sprint checkpoint.
type SprintCheckpointResult struct {
	CheckpointAt time.Time `json:"checkpoint_at"`
	ReportPath   string    `json:"report_path"`
}

// SprintCheckpoint transitions sprint status to CHECKPOINT, causing agents to pause,
// and writes a sprint summary report. The trigger parameter records why the checkpoint
// was created (e.g. "PLANNING_COMPLETE", "SPRINT_COMPLETE", or "" for manual). No terminal I/O.
func SprintCheckpoint(projectRoot string, trigger string) (*SprintCheckpointResult, error) {
	lizaPaths := paths.New(projectRoot)
	statePath := lizaPaths.StatePath()
	reportPath := lizaPaths.SprintSummaryPath()

	blackboard := db.For(statePath)

	state, err := blackboard.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read state: %w", err)
	}

	switch state.Sprint.Status {
	case models.SprintStatusCheckpoint:
		return nil, ErrSprintAlreadyCheckpoint
	case models.SprintStatusCompleted:
		return nil, &PreconditionError{Reason: "cannot checkpoint: sprint is COMPLETED"}
	case models.SprintStatusAborted:
		return nil, &PreconditionError{Reason: "cannot checkpoint: sprint is ABORTED"}
	}

	// Auto-detect PLANNING_COMPLETE when trigger is empty.
	// This makes the system resilient to LLM omission of the trigger parameter.
	if trigger == "" {
		if detCtx, detErr := LoadDetectionContext(projectRoot); detErr == nil {
			for _, taskID := range state.Sprint.Scope.Planned {
				task := state.FindTask(taskID)
				if IsUnconsumedPlanningOutput(task, detCtx.PlanningPairs) {
					trigger = models.CheckpointTriggerPlanningComplete
					break
				}
			}
		}
		// If LoadDetectionContext fails (no pipeline config), trigger stays empty.
		// This is fine for legacy projects without pipelines.
	}

	timestamp := time.Now()
	report := generateSprintSummary(state, timestamp)

	if err := os.WriteFile(reportPath, []byte(report), 0644); err != nil {
		return nil, fmt.Errorf("failed to write sprint summary: %w", err)
	}

	err = blackboard.Modify(func(s *models.State) error {
		s.Sprint.Status = models.SprintStatusCheckpoint
		s.Sprint.Timeline.CheckpointAt = &timestamp
		s.Sprint.CheckpointTrigger = trigger
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to update sprint status: %w", err)
	}

	return &SprintCheckpointResult{
		CheckpointAt: timestamp,
		ReportPath:   reportPath,
	}, nil
}

// generateSprintSummary creates a markdown report of the sprint status.
func generateSprintSummary(state *models.State, timestamp time.Time) string {
	report := sprintHeader(timestamp)
	report += sprintStatusSection(&state.Sprint, timestamp)
	report += taskStatusSection(state.Tasks)
	report += sprintMetricsSection(&state.Sprint.Metrics)
	report += activeAgentsSection(state.Agents)

	if len(state.Anomalies) > 0 {
		report += anomaliesSection(state.Anomalies)
	}

	report += circuitBreakerSection(&state.CircuitBreaker)
	report += nextStepsSection()

	return report
}

func sprintHeader(timestamp time.Time) string {
	return "# Sprint Summary\n\n" +
		fmt.Sprintf("**Generated:** %s\n\n", timestamp.Format(time.RFC3339))
}

func sprintStatusSection(sprint *models.Sprint, timestamp time.Time) string {
	s := "## Sprint Status\n\n"
	s += fmt.Sprintf("- **Sprint ID:** %s\n", sprint.ID)
	s += fmt.Sprintf("- **Status:** %s → %s\n", sprint.Status, models.SprintStatusCheckpoint)
	s += fmt.Sprintf("- **Started:** %s\n", sprint.Timeline.Started.Format(time.RFC3339))
	s += fmt.Sprintf("- **Deadline:** %s\n", sprint.Timeline.Deadline.Format(time.RFC3339))

	elapsed := timestamp.Sub(sprint.Timeline.Started)
	s += fmt.Sprintf("- **Elapsed:** %s\n", formatDuration(elapsed))

	remaining := sprint.Timeline.Deadline.Sub(timestamp)
	if remaining > 0 {
		s += fmt.Sprintf("- **Remaining:** %s\n", formatDuration(remaining))
	} else {
		s += fmt.Sprintf("- **Overdue by:** %s\n", formatDuration(-remaining))
	}

	s += "\n"
	return s
}

func taskStatusSection(tasks []models.Task) string {
	tasksByStatus := make(map[models.TaskStatus]int)
	for _, task := range tasks {
		tasksByStatus[task.Status]++
	}

	s := "## Task Status\n\n"
	s += "| Status | Count |\n"
	s += "|--------|-------|\n"
	s += fmt.Sprintf("| MERGED | %d |\n", tasksByStatus[models.TaskStatusMerged])
	s += fmt.Sprintf("| APPROVED | %d |\n", tasksByStatus[models.TaskStatusApproved])
	s += fmt.Sprintf("| READY_FOR_REVIEW | %d |\n", tasksByStatus[models.TaskStatusReadyForReview])
	s += fmt.Sprintf("| IMPLEMENTING | %d |\n", tasksByStatus[models.TaskStatusImplementing])
	s += fmt.Sprintf("| REJECTED | %d |\n", tasksByStatus[models.TaskStatusRejected])
	s += fmt.Sprintf("| BLOCKED | %d |\n", tasksByStatus[models.TaskStatusBlocked])
	s += fmt.Sprintf("| READY | %d |\n", tasksByStatus[models.TaskStatusReady])
	s += fmt.Sprintf("| ABANDONED | %d |\n", tasksByStatus[models.TaskStatusAbandoned])
	s += fmt.Sprintf("| SUPERSEDED | %d |\n", tasksByStatus[models.TaskStatusSuperseded])
	s += fmt.Sprintf("| INTEGRATION_FAILED | %d |\n", tasksByStatus[models.TaskStatusIntegrationFailed])
	s += "\n"
	return s
}

func sprintMetricsSection(metrics *models.SprintMetrics) string {
	s := "## Sprint Metrics\n\n"
	s += fmt.Sprintf("- **Tasks Done:** %d\n", metrics.TasksDone)
	s += fmt.Sprintf("- **Tasks In Progress:** %d\n", metrics.TasksInProgress)
	s += fmt.Sprintf("- **Tasks Blocked:** %d\n", metrics.TasksBlocked)
	s += fmt.Sprintf("- **Total Iterations:** %d\n", metrics.IterationsTotal)
	s += fmt.Sprintf("- **Total Review Cycles:** %d\n", metrics.ReviewCyclesTotal)
	s += fmt.Sprintf("- **Review Verdict Approvals:** %d\n", metrics.ReviewVerdictApprovals)
	s += fmt.Sprintf("- **Review Verdict Rejections:** %d\n", metrics.ReviewVerdictRejections)
	s += fmt.Sprintf("- **Review Verdict Approval Rate:** %d%%\n", metrics.ReviewVerdictApprovalRatePercent)
	s += fmt.Sprintf("- **Task Outcome Approval Rate:** %d%%\n", metrics.TaskOutcomeApprovalRatePercent)
	s += "\n"
	return s
}

func activeAgentsSection(agents map[string]models.Agent) string {
	s := "## Active Agents\n\n"
	if len(agents) == 0 {
		s += "*No active agents*\n\n"
		return s
	}

	s += "| Agent ID | Role | Status | Current Task |\n"
	s += "|----------|------|--------|-------------|\n"
	for agentID, agent := range agents {
		currentTask := "—"
		if agent.CurrentTask != nil {
			currentTask = *agent.CurrentTask
		}
		s += fmt.Sprintf("| %s | %s | %s | %s |\n", agentID, agent.Role, agent.Status, currentTask)
	}
	s += "\n"
	return s
}

func anomaliesSection(anomalies []models.Anomaly) string {
	s := "## Anomalies\n\n"
	s += fmt.Sprintf("**Total:** %d anomalies detected\n\n", len(anomalies))

	anomaliesByType := make(map[string]int)
	for _, anomaly := range anomalies {
		anomaliesByType[anomaly.Type]++
	}

	s += "| Type | Count |\n"
	s += "|------|-------|\n"
	for anomalyType, count := range anomaliesByType {
		s += fmt.Sprintf("| %s | %d |\n", anomalyType, count)
	}
	s += "\n"
	return s
}

func circuitBreakerSection(cb *models.CircuitBreaker) string {
	s := "## Circuit Breaker\n\n"
	s += fmt.Sprintf("- **Status:** %s\n", cb.Status)
	if cb.Status == "TRIGGERED" && cb.CurrentTrigger != nil {
		s += fmt.Sprintf("- **Pattern:** %s\n", cb.CurrentTrigger.Pattern)
		s += fmt.Sprintf("- **Severity:** %s\n", cb.CurrentTrigger.Severity)
	}
	s += "\n"
	return s
}

func nextStepsSection() string {
	s := "## Next Steps\n\n"
	s += "- [ ] Review sprint progress and metrics\n"
	s += "- [ ] Address any blocked tasks\n"
	s += "- [ ] Review anomalies and circuit breaker status\n"
	s += "- [ ] Adjust sprint scope if needed\n"
	s += "- [ ] Resume sprint with `liza resume`\n"
	s += "\n"
	return s
}

// formatDuration formats a duration in a human-readable format.
func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60

	if hours >= 24 {
		days := hours / 24
		hours = hours % 24
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}

	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}

	return fmt.Sprintf("%dm", minutes)
}
