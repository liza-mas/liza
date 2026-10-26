package commands

import (
	"github.com/liza-mas/liza/internal/models"
)

// StatusOptions contains options for the status command
type StatusOptions struct {
	Format      string // "dashboard", "json", "yaml"
	Detailed    bool   // Include anomalies and circuit breaker
	ProjectRoot string
}

// StatusCommand returns a comprehensive system status: one snapshot, assembled
// into sections, rendered in the requested format.
func StatusCommand(opts StatusOptions) (string, error) {
	snapshot, err := readStatusSnapshot(opts.ProjectRoot)
	if err != nil {
		return "", err
	}
	status := BuildStatusData(snapshot.State, opts.Detailed, opts.ProjectRoot, snapshot.Resolver, nil)
	return renderStatus(status, statusFormat(opts.Format))
}

// BuildStatusData populates the statusData structure from state.
// The caller is responsible for loading the pipeline resolver and passing it in.
// prWarnings is not used internally — it exists so JSON callers can thread
// resolver-load warnings through the response envelope.
func BuildStatusData(state *models.State, detailed bool, projectRoot string, pr models.PipelineResolver, prWarnings []string) statusData {
	data := statusData{
		Goal:   buildGoalStatus(state),
		Sprint: buildSprintStatus(state),
		Config: buildConfigStatus(state),
	}

	data.Tasks = buildTaskStatus(state, pr)
	data.Agents = buildAgentStatuses(state)
	data.AgentHealth = currentAgentHealth(state)
	data.AgentCapacity = buildAgentCapacityStatus(state, pr)
	data.OrchestratorState = buildOrchestratorStatus(state, projectRoot)
	data.Integration = buildIntegrationGateStatus(state, projectRoot)
	data.WorkQueues = buildWorkQueuesStatus(state, data.Tasks.LegacyCoderClaimable, data.Tasks.LegacyCodeReviewerReviewable, pr)
	data.PhaseHandoff = buildPhaseHandoffStatus(state, projectRoot)
	data.PendingTransitions = buildPendingTransitions(state, projectRoot)

	if detailed {
		data.Anomalies = buildAnomalyLines(state)
		data.CircuitBreaker = buildCircuitBreakerStatus(state)
	}

	return data
}
