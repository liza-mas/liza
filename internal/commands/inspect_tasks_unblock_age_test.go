package commands

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func TestCalculateTimeInStatus_UnblockedEvent(t *testing.T) {
	now := time.Now()
	task := &models.Task{
		Created: now.Add(-4 * time.Hour),
		History: []models.TaskHistoryEntry{
			{Time: now.Add(-3 * time.Hour), Event: models.TaskEventBlocked},
			{Time: now.Add(-2 * time.Minute), Event: models.TaskEventUnblocked},
		},
	}

	duration := models.TimeInStatus(task, time.Now())
	if duration < time.Minute || duration >= 3*time.Minute {
		t.Fatalf("expected duration since recent unblock event, got %s", duration)
	}
}
