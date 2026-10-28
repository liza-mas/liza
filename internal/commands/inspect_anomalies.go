package commands

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/render"
	"github.com/liza-mas/liza/internal/secretmask"
)

const anomalyDetailsSummaryLimitBytes = 200

// inspectAnomaliesOptions contains options for anomaly inspection
type inspectAnomaliesOptions struct {
	Format         inspectFormat // Output format: json, yaml, table, value
	TypeFilter     string        // Filter by anomaly type
	TaskFilter     string        // Filter by task ID
	ReporterFilter string        // Filter by reporter (agent ID)
	Internal       bool          // Return structured data for composition
}

// anomalyInfo represents anomaly information with computed fields
type anomalyInfo struct {
	Timestamp time.Time      `json:"timestamp" yaml:"timestamp"`
	Age       string         `json:"age" yaml:"age"` // Computed: time since anomaly occurred
	Task      string         `json:"task" yaml:"task"`
	Reporter  string         `json:"reporter" yaml:"reporter"`
	Type      string         `json:"type" yaml:"type"`
	Details   map[string]any `json:"details,omitempty" yaml:"details,omitempty"`
}

// inspectAnomalies lists all anomalies or filters by criteria
func inspectAnomalies(state *models.State, opts inspectAnomaliesOptions) (any, error) {
	// Get all anomalies
	anomalies := state.Anomalies

	// Apply filters
	filtered := filterAnomalies(anomalies, opts)

	// Build anomalyInfo with computed fields
	anomalyInfos := make([]anomalyInfo, len(filtered))
	for i, anomaly := range filtered {
		anomalyInfos[i] = buildAnomalyInfo(anomaly)
	}

	// If called internally (for composition), return structured data
	if opts.Internal {
		return anomalyInfos, nil
	}

	// Otherwise, format for output
	return formatAnomaliesOutput(anomalyInfos, opts.Format)
}

// buildAnomalyInfo converts an Anomaly to anomalyInfo with computed fields
func buildAnomalyInfo(anomaly models.Anomaly) anomalyInfo {
	info := anomalyInfo{
		Timestamp: anomaly.Timestamp,
		Task:      anomaly.Task,
		Reporter:  anomaly.Reporter,
		Type:      string(anomaly.Type),
		Details:   anomaly.Details,
	}

	// Compute age (time since anomaly occurred)
	age := time.Since(anomaly.Timestamp)
	info.Age = render.FormatDuration(age)

	return info
}

// filterAnomalies applies filters to anomaly list
func filterAnomalies(anomalies []models.Anomaly, opts inspectAnomaliesOptions) []models.Anomaly {
	var filtered []models.Anomaly

	for _, anomaly := range anomalies {
		// Apply type filter
		if opts.TypeFilter != "" && string(anomaly.Type) != opts.TypeFilter {
			continue
		}

		// Apply task filter
		if opts.TaskFilter != "" && anomaly.Task != opts.TaskFilter {
			continue
		}

		// Apply reporter filter
		if opts.ReporterFilter != "" && anomaly.Reporter != opts.ReporterFilter {
			continue
		}

		filtered = append(filtered, anomaly)
	}

	return filtered
}

// formatAnomaliesOutput formats a list of anomalies for output
func formatAnomaliesOutput(anomalies []anomalyInfo, format inspectFormat) (string, error) {
	// Default to table format
	if format == "" {
		format = inspectFormatTable
	}

	switch format {
	case inspectFormatJSON:
		return render.FormatJSON(anomalies)
	case inspectFormatYAML:
		return render.FormatYAML(anomalies)
	case inspectFormatTable:
		return formatAnomaliesTable(anomalies), nil
	case inspectFormatValue:
		// Value format doesn't make sense for multiple anomalies
		return "", fmt.Errorf("value format not supported for anomaly lists (use json, yaml, or table)")
	default:
		return "", fmt.Errorf("invalid format: %s", format)
	}
}

// formatAnomaliesTable formats anomalies as a table
func formatAnomaliesTable(anomalies []anomalyInfo) string {
	if len(anomalies) == 0 {
		return "No anomalies found"
	}

	headers := []string{"AGE", "TYPE", "TASK", "REPORTER", "TIMESTAMP", "DETAILS"}
	var rows [][]string

	for _, anomaly := range anomalies {
		// Format timestamp for display
		timestampStr := anomaly.Timestamp.Format("2006-01-02 15:04:05")

		rows = append(rows, []string{
			anomaly.Age + " ago",
			anomaly.Type,
			anomaly.Task,
			anomaly.Reporter,
			timestampStr,
			formatAnomalyDetailsSummary(anomaly.Details),
		})
	}

	return render.FormatTable(headers, rows)
}

func formatAnomalyDetailsSummary(details map[string]any) string {
	if len(details) == 0 {
		return ""
	}

	keys := make([]string, 0, len(details))
	for key := range details {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value, _ := render.FormatValue(details[key])
		parts = append(parts, fmt.Sprintf("%s=%s", key, value))
	}

	return boundAnomalyDetailsSummary(secretmask.New().MaskText(strings.Join(parts, ", ")))
}

func boundAnomalyDetailsSummary(text string) string {
	bytes := []byte(text)
	if len(bytes) <= anomalyDetailsSummaryLimitBytes {
		return text
	}
	return string(bytes[:anomalyDetailsSummaryLimitBytes]) + "... [truncated]"
}
