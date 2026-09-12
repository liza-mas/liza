package ops

import "github.com/liza-mas/liza/internal/models"

// ReleaseAgentsForTask releases all agent-side assignments to taskID. Call it
// inside the authorized state transaction that clears the task-side ownership.
func ReleaseAgentsForTask(state *models.State, taskID string) {
	releaseAgentsForTask(state, taskID)
}

func releaseAgentsForTask(state *models.State, taskID string) {
	for agentID, agent := range state.Agents {
		if agent.CurrentTask != nil && *agent.CurrentTask == taskID {
			// ReleaseAgent updates this map entry in place; mutating values during
			// map iteration is safe and keeps release atomic with task transition.
			state.ReleaseAgent(agentID)
		}
	}
}
