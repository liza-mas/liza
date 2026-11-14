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

// TestGenerateSprintSummary_Deadline checks optional deadlines and elapsed time.
func TestGenerateSprintSummary_Deadline(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		deadline time.Time
		want     string
	}{
		{
			name: "unset",
			want: "- **Started:** 2026-11-14T10:00:00Z\n" +
				"- **Elapsed:** 2h 0m\n\n## Task Status\n",
		},
		{
			name:     "future",
			deadline: time.Date(2026, 11, 14, 14, 0, 0, 0, time.UTC),
			want: "- **Started:** 2026-11-14T10:00:00Z\n" +
				"- **Deadline:** 2026-11-14T14:00:00Z\n" +
				"- **Elapsed:** 2h 0m\n" +
				"- **Remaining:** 2h 0m\n\n## Task Status\n",
		},
		{
			name:     "past",
			deadline: time.Date(2026, 11, 14, 11, 0, 0, 0, time.UTC),
			want: "- **Started:** 2026-11-14T10:00:00Z\n" +
				"- **Deadline:** 2026-11-14T11:00:00Z\n" +
				"- **Elapsed:** 2h 0m\n" +
				"- **Overdue by:** 1h 0m\n\n## Task Status\n",
		},
		{
			name:     "now",
			deadline: now,
			want: "- **Started:** 2026-11-14T10:00:00Z\n" +
				"- **Deadline:** 2026-11-14T12:00:00Z\n" +
				"- **Elapsed:** 2h 0m\n" +
				"- **Overdue by:** 0m\n\n## Task Status\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			state.Sprint.Timeline.Started = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
			state.Sprint.Timeline.Deadline = tt.deadline

			report := generateSprintSummary(state, now)
			if !strings.Contains(report, tt.want) {
				t.Errorf("summary missing expected timeline:\n%s\nreport:\n%s", tt.want, report)
			}
		})
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
