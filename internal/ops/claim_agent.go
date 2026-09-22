package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

type reviewerCapacityInvalidReason string

const (
	reviewerCapacityMissingRole     reviewerCapacityInvalidReason = "missing_role"
	reviewerCapacityWrongRole       reviewerCapacityInvalidReason = "wrong_role"
	reviewerCapacityMissingProvider reviewerCapacityInvalidReason = "missing_provider"
	reviewerCapacityMissingPID      reviewerCapacityInvalidReason = "missing_pid"
	reviewerCapacityDeadPID         reviewerCapacityInvalidReason = "dead_pid"
	reviewerCapacityMissingLease    reviewerCapacityInvalidReason = "missing_lease"
	reviewerCapacityExpiredLease    reviewerCapacityInvalidReason = "expired_lease"
)

// HasValidClaimRegistration applies the same role, provider and process checks
// as claim admission. It does not imply task-specific eligibility.
func HasValidClaimRegistration(state *models.State, agentID, expectedRole string) bool {
	if state == nil {
		return false
	}
	_, err := requireRegisteredClaimAgent(state, agentID, expectedRole)
	return err == nil
}

func requireRegisteredClaimAgent(state *models.State, agentID, expectedRole string) (models.Agent, error) {
	agent, exists := state.Agents[agentID]
	if !exists {
		return models.Agent{}, &PreconditionError{Reason: fmt.Sprintf("agent %s is not registered", agentID)}
	}
	if agent.Role == "" {
		return models.Agent{}, &PreconditionError{Reason: fmt.Sprintf("agent %s has no registered role", agentID)}
	}
	if expectedRole != "" && agent.Role != expectedRole {
		return models.Agent{}, &PreconditionError{Reason: fmt.Sprintf("agent %s role %q does not match expected role %q", agentID, agent.Role, expectedRole)}
	}
	if agent.Provider == "" {
		return models.Agent{}, &PreconditionError{Reason: fmt.Sprintf("agent %s has no registered provider", agentID)}
	}
	if agent.PID <= 0 {
		return models.Agent{}, &PreconditionError{Reason: fmt.Sprintf("agent %s has no registered process PID", agentID)}
	}
	if !IsProcessAlive(agent.PID) {
		return models.Agent{}, &PreconditionError{Reason: fmt.Sprintf("agent %s registered process PID %d is not running", agentID, agent.PID)}
	}
	return agent, nil
}

func reviewerCapacityInvalidReasons(agent models.Agent, expectedRole string, now time.Time) []reviewerCapacityInvalidReason {
	var reasons []reviewerCapacityInvalidReason
	if agent.Role == "" {
		reasons = append(reasons, reviewerCapacityMissingRole)
	} else if expectedRole != "" && agent.Role != expectedRole {
		reasons = append(reasons, reviewerCapacityWrongRole)
	}
	if agent.Provider == "" {
		reasons = append(reasons, reviewerCapacityMissingProvider)
	}
	if agent.PID <= 0 {
		reasons = append(reasons, reviewerCapacityMissingPID)
	} else if !IsProcessAlive(agent.PID) {
		reasons = append(reasons, reviewerCapacityDeadPID)
	}
	if agent.LeaseExpires == nil {
		reasons = append(reasons, reviewerCapacityMissingLease)
	} else if agent.LeaseExpires.Before(now.Add(-models.LeaseExpiryGracePeriod)) {
		// Capacity follows the same grace model as validation/watch: a lease
		// that just expired may still represent a long-running live supervisor.
		reasons = append(reasons, reviewerCapacityExpiredLease)
	}
	return reasons
}

func hasReviewerCapacity(agent models.Agent, expectedRole string, now time.Time) bool {
	return len(reviewerCapacityInvalidReasons(agent, expectedRole, now)) == 0
}

// TaskClaimsForAgent returns task-side ownership fields that point at agentID.
// It deliberately does not consult the agent row so recovery still works when
// agent metadata is missing or corrupt.
func TaskClaimsForAgent(state *models.State, agentID string) (doerTaskIDs, reviewerTaskIDs []string) {
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.AssignedTo != nil && *task.AssignedTo == agentID {
			doerTaskIDs = append(doerTaskIDs, task.ID)
		}
		if task.ReviewingBy != nil && *task.ReviewingBy == agentID {
			reviewerTaskIDs = append(reviewerTaskIDs, task.ID)
		}
	}
	return doerTaskIDs, reviewerTaskIDs
}
