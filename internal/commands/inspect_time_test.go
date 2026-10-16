package commands

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func TestCalculateTaskAge(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		task    *models.Task
		wantMin time.Duration
		wantMax time.Duration
	}{
		{
			name: "recent task",
			task: &models.Task{
				ID:      "task-1",
				Created: now.Add(-2 * time.Hour),
			},
			wantMin: 2 * time.Hour,
			wantMax: 2*time.Hour + time.Second, // Allow small drift
		},
		{
			name: "old task",
			task: &models.Task{
				ID:      "task-2",
				Created: now.Add(-48 * time.Hour),
			},
			wantMin: 48 * time.Hour,
			wantMax: 48*time.Hour + time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateTaskAge(tt.task)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("calculateTaskAge() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestCalculateTimeOnTask(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		task    *models.Task
		wantMin time.Duration
		wantMax time.Duration
	}{
		{
			name: "task claimed 1 hour ago",
			task: &models.Task{
				ID:     "task-1",
				Status: models.TaskStatusImplementing,
				History: []models.TaskHistoryEntry{
					{
						Time:  now.Add(-1 * time.Hour),
						Event: models.TaskEventClaimed,
					},
				},
			},
			wantMin: 1 * time.Hour,
			wantMax: 1*time.Hour + time.Second,
		},
		{
			name: "task with no history",
			task: &models.Task{
				ID:      "task-2",
				Status:  models.TaskStatusReady,
				History: []models.TaskHistoryEntry{},
			},
			wantMin: 0,
			wantMax: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateTimeOnTask(tt.task)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("calculateTimeOnTask() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestCalculateAgentTimeOnTask(t *testing.T) {
	now := time.Now()
	currentAgent := "agent-current"
	otherAgent := "agent-other"

	tests := []struct {
		name      string
		history   []models.TaskHistoryEntry
		wantStart time.Duration
	}{
		{
			name: "fresh doer claim ignores older actor claim",
			history: []models.TaskHistoryEntry{
				{Time: now.Add(-2 * time.Hour), Event: models.TaskEventClaimed, Agent: &otherAgent},
				{Time: now.Add(-15 * time.Minute), Event: models.TaskEventClaimed, Agent: &currentAgent},
			},
			wantStart: 15 * time.Minute,
		},
		{
			name: "preserved claim selects newest claim by same agent",
			history: []models.TaskHistoryEntry{
				{Time: now.Add(-90 * time.Minute), Event: models.TaskEventClaimed, Agent: &currentAgent},
				{Time: now.Add(-12 * time.Minute), Event: models.TaskEventClaimed, Agent: &currentAgent},
			},
			wantStart: 12 * time.Minute,
		},
		{
			name: "same-agent rejected-task reclaim",
			history: []models.TaskHistoryEntry{
				{Time: now.Add(-80 * time.Minute), Event: models.TaskEventClaimed, Agent: &currentAgent},
				{Time: now.Add(-10 * time.Minute), Event: models.TaskEventReclaimedAfterRejection, Agent: &currentAgent},
			},
			wantStart: 10 * time.Minute,
		},
		{
			name: "different-agent reassignment",
			history: []models.TaskHistoryEntry{
				{Time: now.Add(-70 * time.Minute), Event: models.TaskEventClaimed, Agent: &otherAgent},
				{Time: now.Add(-8 * time.Minute), Event: models.TaskEventReassignedAfterRejection, Agent: &currentAgent},
			},
			wantStart: 8 * time.Minute,
		},
		{
			name: "integration-fix claim",
			history: []models.TaskHistoryEntry{
				{Time: now.Add(-60 * time.Minute), Event: models.TaskEventClaimed, Agent: &currentAgent},
				{Time: now.Add(-6 * time.Minute), Event: models.TaskEventClaimedForIntegrationFix, Agent: &currentAgent},
			},
			wantStart: 6 * time.Minute,
		},
		{
			name: "legacy task without matching actor-bearing start",
			history: []models.TaskHistoryEntry{
				{Time: now.Add(-45 * time.Minute), Event: models.TaskEventClaimed},
				{Time: now.Add(-5 * time.Minute), Event: models.TaskEventClaimed, Agent: &otherAgent},
			},
			wantStart: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &models.Task{ID: "task-b", History: tt.history}
			got := AgentTimeOnTask(task, currentAgent)
			if tt.wantStart == 0 {
				if got != 0 {
					t.Fatalf("AgentTimeOnTask() = %v, want 0", got)
				}
				return
			}

			if got < tt.wantStart || got > tt.wantStart+time.Second {
				t.Fatalf("AgentTimeOnTask() = %v, want between %v and %v", got, tt.wantStart, tt.wantStart+time.Second)
			}
		})
	}
}

func TestCalculateTimeSinceHeartbeat(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		agent   *models.Agent
		wantMin time.Duration
		wantMax time.Duration
	}{
		{
			name: "recent heartbeat",
			agent: &models.Agent{
				Role:      "coder",
				Status:    models.AgentStatusWorking,
				Heartbeat: now.Add(-5 * time.Second),
			},
			wantMin: 5 * time.Second,
			wantMax: 6 * time.Second,
		},
		{
			name: "old heartbeat",
			agent: &models.Agent{
				Role:      "coder",
				Status:    models.AgentStatusIdle,
				Heartbeat: now.Add(-10 * time.Minute),
			},
			wantMin: 10 * time.Minute,
			wantMax: 10*time.Minute + time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateTimeSinceHeartbeat(tt.agent)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("calculateTimeSinceHeartbeat() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestCalculateSprintElapsed(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		sprint  *models.Sprint
		wantMin time.Duration
		wantMax time.Duration
	}{
		{
			name: "sprint started 3 hours ago",
			sprint: &models.Sprint{
				ID: "sprint-1",
				Timeline: models.SprintTimeline{
					Started:  now.Add(-3 * time.Hour),
					Deadline: now.Add(5 * time.Hour),
				},
			},
			wantMin: 3 * time.Hour,
			wantMax: 3*time.Hour + time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateSprintElapsed(tt.sprint)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("calculateSprintElapsed() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestCalculateSprintRemaining(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		sprint  *models.Sprint
		wantMin time.Duration
		wantMax time.Duration
	}{
		{
			name: "5 hours remaining",
			sprint: &models.Sprint{
				ID: "sprint-1",
				Timeline: models.SprintTimeline{
					Started:  now.Add(-3 * time.Hour),
					Deadline: now.Add(5 * time.Hour),
				},
			},
			wantMin: 5*time.Hour - time.Second, // Allow small timing drift
			wantMax: 5*time.Hour + time.Second,
		},
		{
			name: "overdue sprint",
			sprint: &models.Sprint{
				ID: "sprint-2",
				Timeline: models.SprintTimeline{
					Started:  now.Add(-10 * time.Hour),
					Deadline: now.Add(-2 * time.Hour),
				},
			},
			wantMin: -2*time.Hour - time.Second,
			wantMax: -2 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateSprintRemaining(tt.sprint)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("calculateSprintRemaining() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}
