package ops

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestFormatDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{name: "minutes only", duration: 45 * time.Minute, expected: "45m"},
		{name: "hours and minutes", duration: 3*time.Hour + 15*time.Minute, expected: "3h 15m"},
		{name: "days hours minutes", duration: 50 * time.Hour, expected: "2d 2h 0m"},
		{name: "zero", duration: 0, expected: "0m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.duration)
			if result != tt.expected {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestGenerateSprintSummary_WithAnomalies(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Timeline.Started = now.Add(-1 * time.Hour)
	state.Sprint.Timeline.Deadline = now.Add(5 * time.Hour)
	state.Anomalies = []models.Anomaly{
		{Type: "retry_loop", Task: "task-1", Reporter: "coder-1", Details: map[string]any{"error_pattern": "test anomaly"}},
		{Type: "retry_loop", Task: "task-1", Reporter: "coder-1", Details: map[string]any{"error_pattern": "another"}},
	}

	report := generateSprintSummary(state, now)

	if !strings.Contains(report, "Anomalies") {
		t.Error("Report should contain Anomalies section")
	}
	if !strings.Contains(report, "retry_loop") {
		t.Error("Report should contain anomaly type")
	}
}

func TestGenerateSprintSummary_Overdue(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Timeline.Started = now.Add(-10 * time.Hour)
	state.Sprint.Timeline.Deadline = now.Add(-2 * time.Hour) // Overdue

	report := generateSprintSummary(state, now)

	if !strings.Contains(report, "Overdue") {
		t.Error("Report should contain 'Overdue' for past deadlines")
	}
}

func TestGenerateSprintSummary_WithAgents(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Timeline.Started = now.Add(-1 * time.Hour)
	state.Sprint.Timeline.Deadline = now.Add(5 * time.Hour)
	taskRef := "task-1"
	state.Agents["coder-1"] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: &taskRef,
	}

	report := generateSprintSummary(state, now)

	if !strings.Contains(report, "coder-1") {
		t.Error("Report should contain agent ID")
	}
	if !strings.Contains(report, "task-1") {
		t.Error("Report should contain agent's current task")
	}
}

func TestGenerateSprintSummary_CircuitBreakerTriggered(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Timeline.Started = now.Add(-1 * time.Hour)
	state.Sprint.Timeline.Deadline = now.Add(5 * time.Hour)
	state.CircuitBreaker.Status = "TRIGGERED"
	state.CircuitBreaker.CurrentTrigger = &models.CircuitBreakerTrigger{
		Pattern:  "retry_cluster",
		Severity: "HIGH",
	}

	report := generateSprintSummary(state, now)

	if !strings.Contains(report, "TRIGGERED") {
		t.Error("Report should contain circuit breaker status")
	}
	if !strings.Contains(report, "retry_cluster") {
		t.Error("Report should contain trigger pattern")
	}
}
