package commands

import (
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// calculateTaskAge returns duration since task was created.
func calculateTaskAge(task *models.Task) time.Duration {
	return time.Since(task.Created)
}

// calculateTimeOnTask returns how long the agent has been on the current task
// by finding the most recent "claimed" event in task history.
func calculateTimeOnTask(task *models.Task) time.Duration {
	if len(task.History) == 0 {
		return 0
	}

	var claimedTime time.Time
	for _, entry := range task.History {
		if entry.Event == models.TaskEventClaimed {
			if claimedTime.IsZero() || entry.Time.After(claimedTime) {
				claimedTime = entry.Time
			}
		}
	}

	if claimedTime.IsZero() {
		return 0
	}

	return time.Since(claimedTime)
}

// AgentTimeOnTask returns how long the agent has been on the current
// task by finding its most recent actor-bearing assignment-start event.
func AgentTimeOnTask(task *models.Task, agentID string) time.Duration {
	var assignmentStart time.Time
	for _, entry := range task.History {
		if entry.Agent == nil || *entry.Agent != agentID || !isAssignmentStartEvent(entry.Event) {
			continue
		}
		if assignmentStart.IsZero() || entry.Time.After(assignmentStart) {
			assignmentStart = entry.Time
		}
	}

	if assignmentStart.IsZero() {
		return 0
	}
	return time.Since(assignmentStart)
}

func isAssignmentStartEvent(event models.TaskEventName) bool {
	switch event {
	case models.TaskEventClaimed,
		models.TaskEventReclaimedAfterRejection,
		models.TaskEventReassignedAfterRejection,
		models.TaskEventClaimedForIntegrationFix:
		return true
	default:
		return false
	}
}

// calculateTimeSinceHeartbeat returns duration since agent's last heartbeat.
func calculateTimeSinceHeartbeat(agent *models.Agent) time.Duration {
	return time.Since(agent.Heartbeat)
}

// calculateSprintElapsed returns duration since sprint started.
func calculateSprintElapsed(sprint *models.Sprint) time.Duration {
	return time.Since(sprint.Timeline.Started)
}

// calculateSprintRemaining returns duration until deadline.
// Returns negative duration if overdue.
func calculateSprintRemaining(sprint *models.Sprint) time.Duration {
	return time.Until(sprint.Timeline.Deadline)
}
