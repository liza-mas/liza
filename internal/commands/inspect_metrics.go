package commands

import (
	"fmt"
	"sort"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/render"
)

// inspectMetricsOptions contains options for metrics inspection
type inspectMetricsOptions struct {
	Format       string // Output format: json, yaml, table, value
	AgentMetrics bool   // If true, show per-agent metrics instead of sprint metrics
	Internal     bool   // Return structured data for composition
}

// metricsInfo represents sprint metrics information
type metricsInfo struct {
	LifecycleOutcomes                *models.LifecycleOutcomeMetrics `json:"lifecycle_outcomes,omitempty" yaml:"lifecycle_outcomes,omitempty"`
	TasksDone                        int                             `json:"tasks_done" yaml:"tasks_done"`
	TasksInProgress                  int                             `json:"tasks_in_progress" yaml:"tasks_in_progress"`
	TasksBlocked                     int                             `json:"tasks_blocked" yaml:"tasks_blocked"`
	IterationsTotal                  int                             `json:"iterations_total" yaml:"iterations_total"`
	ReviewCyclesTotal                int                             `json:"review_cycles_total" yaml:"review_cycles_total"`
	ReviewVerdictApprovals           int                             `json:"review_verdict_approvals" yaml:"review_verdict_approvals"`
	ReviewVerdictRejections          int                             `json:"review_verdict_rejections" yaml:"review_verdict_rejections"`
	ReviewVerdictCount               int                             `json:"review_verdict_count" yaml:"review_verdict_count"`
	ReviewVerdictApprovalRatePercent int                             `json:"review_verdict_approval_rate_percent" yaml:"review_verdict_approval_rate_percent"`
	TaskSubmittedForReviewCount      int                             `json:"task_submitted_for_review_count" yaml:"task_submitted_for_review_count"`
	TaskOutcomeApprovalRatePercent   int                             `json:"task_outcome_approval_rate_percent" yaml:"task_outcome_approval_rate_percent"`
}

// AgentMetricsInfo represents per-agent performance metrics
type AgentMetricsInfo struct {
	AgentID            string `json:"agent_id" yaml:"agent_id"`
	Role               string `json:"role" yaml:"role"`
	TasksCompleted     int    `json:"tasks_completed" yaml:"tasks_completed"`
	TasksInProgress    int    `json:"tasks_in_progress" yaml:"tasks_in_progress"`
	TasksFailed        int    `json:"tasks_failed" yaml:"tasks_failed"`
	TotalIterations    int    `json:"total_iterations" yaml:"total_iterations"`
	SuccessRatePercent int    `json:"success_rate_percent" yaml:"success_rate_percent"`
}

// inspectMetrics retrieves sprint metrics or per-agent metrics
func inspectMetrics(state *models.State, opts inspectMetricsOptions) (any, error) {
	if opts.AgentMetrics {
		// Calculate per-agent metrics
		agentMetrics := calculateAgentMetrics(state.Tasks, state.Agents)

		// Sort by agent ID for consistent output
		sort.Slice(agentMetrics, func(i, j int) bool {
			return agentMetrics[i].AgentID < agentMetrics[j].AgentID
		})

		// If called internally, return structured data
		if opts.Internal {
			return agentMetrics, nil
		}

		// Otherwise, format for output
		return formatAgentMetricsOutput(agentMetrics, opts.Format)
	}

	// Get sprint metrics
	metricsInfo := buildMetricsInfo(state.Sprint.Metrics)

	// If called internally, return structured data
	if opts.Internal {
		return metricsInfo, nil
	}

	// Otherwise, format for output
	return formatMetricsOutput(metricsInfo, opts.Format)
}

// buildMetricsInfo converts SprintMetrics to metricsInfo
func buildMetricsInfo(metrics models.SprintMetrics) metricsInfo {
	return metricsInfo{
		LifecycleOutcomes:                metrics.LifecycleOutcomes,
		TasksDone:                        metrics.TasksDone,
		TasksInProgress:                  metrics.TasksInProgress,
		TasksBlocked:                     metrics.TasksBlocked,
		IterationsTotal:                  metrics.IterationsTotal,
		ReviewCyclesTotal:                metrics.ReviewCyclesTotal,
		ReviewVerdictApprovals:           metrics.ReviewVerdictApprovals,
		ReviewVerdictRejections:          metrics.ReviewVerdictRejections,
		ReviewVerdictCount:               metrics.ReviewVerdictCount,
		ReviewVerdictApprovalRatePercent: metrics.ReviewVerdictApprovalRatePercent,
		TaskSubmittedForReviewCount:      metrics.TaskSubmittedForReviewCount,
		TaskOutcomeApprovalRatePercent:   metrics.TaskOutcomeApprovalRatePercent,
	}
}

// calculateAgentMetrics computes per-agent statistics from tasks
func calculateAgentMetrics(tasks []models.Task, agents map[string]models.Agent) []AgentMetricsInfo {
	// Build a map to track metrics per agent
	agentStats := make(map[string]*AgentMetricsInfo)

	// Initialize metrics for all registered agents
	for agentID, agent := range agents {
		agentStats[agentID] = &AgentMetricsInfo{
			AgentID: agentID,
			Role:    agent.Role,
		}
	}

	// Iterate through tasks and accumulate stats
	for _, task := range tasks {
		if task.AssignedTo == nil {
			continue
		}

		agentID := *task.AssignedTo

		// Create entry if agent is not registered (shouldn't happen in practice)
		if _, exists := agentStats[agentID]; !exists {
			agentStats[agentID] = &AgentMetricsInfo{
				AgentID: agentID,
				Role:    "unknown",
			}
		}

		stats := agentStats[agentID]

		// Count tasks by status
		switch task.Status {
		case models.TaskStatusMerged:
			stats.TasksCompleted++
		case models.TaskStatusAbandoned, models.TaskStatusSuperseded:
			stats.TasksFailed++
		case models.TaskStatusImplementing, models.TaskStatusReviewing, models.TaskStatusRejected,
			models.TaskStatusApproved, models.TaskStatusBlocked,
			models.TaskStatusIntegrationFailed:
			stats.TasksInProgress++
		default:
			if task.Status.IsReadyForReviewStatus() {
				stats.TasksInProgress++
			}
		}

		// Accumulate iterations
		stats.TotalIterations += task.Iteration
	}

	// Calculate success rates
	for _, stats := range agentStats {
		totalTasks := stats.TasksCompleted + stats.TasksInProgress + stats.TasksFailed
		if totalTasks > 0 {
			stats.SuccessRatePercent = (stats.TasksCompleted * 100) / totalTasks
		}
	}

	// Convert map to slice
	result := make([]AgentMetricsInfo, 0, len(agentStats))
	for _, stats := range agentStats {
		result = append(result, *stats)
	}

	return result
}

// formatMetricsOutput formats sprint metrics for output
func formatMetricsOutput(metrics metricsInfo, format string) (string, error) {
	// Default to value format
	if format == "" {
		format = "value"
	}

	switch format {
	case "json":
		return render.FormatJSON(metrics)
	case "yaml":
		return render.FormatYAML(metrics)
	case "value":
		return formatMetricsValue(metrics)
	case "table":
		// Table format doesn't make sense for single metrics object
		return "", fmt.Errorf("table format not supported for metrics (use json, yaml, or value)")
	default:
		return "", fmt.Errorf("invalid format: %s", format)
	}
}

// formatAgentMetricsOutput formats per-agent metrics for output
func formatAgentMetricsOutput(metrics []AgentMetricsInfo, format string) (string, error) {
	// Default to table format
	if format == "" {
		format = "table"
	}

	switch format {
	case "json":
		return render.FormatJSON(metrics)
	case "yaml":
		return render.FormatYAML(metrics)
	case "table":
		return formatAgentMetricsTable(metrics), nil
	case "value":
		// Value format doesn't make sense for multiple agents
		return "", fmt.Errorf("value format not supported for agent metrics (use json, yaml, or table)")
	default:
		return "", fmt.Errorf("invalid format: %s", format)
	}
}

// formatMetricsValue formats sprint metrics as key-value pairs
func formatMetricsValue(metrics metricsInfo) (string, error) {
	text, err := render.ExecuteTemplate("metrics_value", metrics)
	if err != nil {
		return "", err
	}
	return text + "\n" + formatLifecycleMetrics(metrics.LifecycleOutcomes), nil
}

// formatAgentMetricsTable formats per-agent metrics as a table
func formatAgentMetricsTable(metrics []AgentMetricsInfo) string {
	if len(metrics) == 0 {
		return "No agent metrics found"
	}

	headers := []string{"AGENT_ID", "ROLE", "COMPLETED", "IN_PROGRESS", "FAILED", "ITERATIONS", "SUCCESS_RATE"}
	var rows [][]string

	for _, m := range metrics {
		rows = append(rows, []string{
			m.AgentID,
			m.Role,
			fmt.Sprintf("%d", m.TasksCompleted),
			fmt.Sprintf("%d", m.TasksInProgress),
			fmt.Sprintf("%d", m.TasksFailed),
			fmt.Sprintf("%d", m.TotalIterations),
			fmt.Sprintf("%d%%", m.SuccessRatePercent),
		})
	}

	return render.FormatTable(headers, rows)
}
