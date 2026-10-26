package commands

import (
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/prompts"
)

// integrationStatusComplete is the status of an integration completion that is
// clean for the current integration HEAD. internal/prompts writes it as a literal
// and names no constant.
const integrationStatusComplete = "complete"

// buildOrchestratorStatus determines orchestrator state
func buildOrchestratorStatus(state *models.State, projectRoot string) orchestratorStatus {
	detCtx, detErr := ops.LoadDetectionContext(projectRoot)
	var pipelineTerminals []models.TaskStatus
	var planningPairs map[string]bool
	var m2oTransitions []ops.ManyToOneTransitionInfo
	if detErr == nil {
		pipelineTerminals = detCtx.SprintTerminals
		planningPairs = detCtx.PlanningPairs
		m2oTransitions = detCtx.ManyToOneTransitions
	}
	result := agent.DetectOrchestratorWakeTriggersForProject(projectRoot, state, pipelineTerminals, planningPairs, m2oTransitions)
	return buildOrchestratorStatusFromWakeResult(state, result)
}

func buildOrchestratorStatusFromWakeResult(state *models.State, result agent.OrchestratorWakeResult) orchestratorStatus {
	count := result.Count

	ps := orchestratorStatus{
		Trigger:      string(result.Trigger),
		TriggerCount: count,
	}
	if result.Integration.Status != "" {
		ps.Integration = integrationStatusFromProjection(result.Integration)
	}

	switch result.Trigger {
	case agent.WakeTriggerInitialPlanning:
		ps.Reason = "No tasks exist; initial planning needed"
	case agent.WakeTriggerBlocked:
		ps.Reason = fmt.Sprintf("%d task(s) are blocked and need attention", count)
		if state.SprintStalled() {
			ps.Reason += " (\u26a0\ufe0f sprint stalled \u2014 all non-terminal tasks blocked)"
		}
	case wakeTriggerIntegrationFailed:
		ps.Reason = fmt.Sprintf("%d task(s) failed integration", count)
	case agent.WakeTriggerHypothesisExhausted:
		ps.Reason = fmt.Sprintf("%d task(s) exhausted hypotheses (2+ failures)", count)
	case agent.WakeTriggerImmediateDiscovery:
		ps.Reason = fmt.Sprintf("%d immediate discovery(ies) need to be converted to tasks", count)
	case agent.WakeTriggerHumanNote:
		ps.Reason = fmt.Sprintf("%d operator note(s) not yet rendered in a completed turn", count)
	case agent.WakeTriggerPlanningComplete:
		ps.Reason = fmt.Sprintf("%d planning task(s) merged with output[]; ready for coding task expansion", count)
	case agent.WakeTriggerManyToOneReady:
		ps.Reason = fmt.Sprintf("%d many-to-one cohort(s) ready for consolidation transition", count)
	case agent.WakeTriggerCodingComplete, agent.WakeTriggerIntegrationWaiting, agent.WakeTriggerIntegrationBlocked, agent.WakeTriggerIntegrationExhausted, agent.WakeTriggerIntegrationUnavailable:
		ps.Reason = formatIntegrationLifecycleReason(ps.Integration)
	case agent.WakeTriggerSprintComplete:
		if ps.Integration != nil && ps.Integration.Status == integrationStatusComplete {
			ps.Reason = "Authoritative integration completion is clean for current integration HEAD"
		} else {
			ps.Reason = fmt.Sprintf("All %d planned task(s) reached terminal state; sprint complete", count)
		}
	case agent.WakeTriggerNone:
		ps.Reason = "No triggers; orchestrator is idle"
	default:
		ps.Reason = "Unknown trigger"
	}

	return ps
}

// buildIntegrationGateStatus reports the live integration gate whichever
// orchestrator wake trigger wins, so a planning wait stays visible (D-225).
func buildIntegrationGateStatus(state *models.State, projectRoot string) *integrationLifecycleStatus {
	decision, err := ops.EvaluateLiveIntegrationProgress(state, projectRoot)
	return integrationStatusFromProjection(prompts.ProjectEffectiveIntegrationCompletion(decision, nil, err))
}

func integrationStatusFromProjection(projection prompts.EffectiveIntegrationCompletion) *integrationLifecycleStatus {
	return &integrationLifecycleStatus{
		Status:         string(projection.Status),
		ReasonCode:     projection.ReasonCode,
		TaskIDs:        append([]string(nil), projection.TaskIDs...),
		RequestKeys:    append([]string(nil), projection.RequestKeys...),
		CreatedTaskIDs: append([]string(nil), projection.CreatedTaskIDs...),
		Guidance:       projection.Guidance,
	}
}

func formatIntegrationLifecycleReason(status *integrationLifecycleStatus) string {
	if status == nil {
		return "Authoritative integration progress is unavailable"
	}
	parts := []string{"Integration status: " + status.Status}
	if status.ReasonCode != "" {
		parts = append(parts, "reason: "+status.ReasonCode)
	}
	if len(status.TaskIDs) > 0 {
		parts = append(parts, "related tasks: "+strings.Join(status.TaskIDs, ", "))
	}
	if len(status.RequestKeys) > 0 {
		parts = append(parts, "requested analyses: "+strings.Join(status.RequestKeys, ", "))
	}
	if len(status.CreatedTaskIDs) > 0 {
		parts = append(parts, "reconciled tasks: "+strings.Join(status.CreatedTaskIDs, ", "))
	}
	if status.Guidance != "" {
		parts = append(parts, "guidance: "+status.Guidance)
	}
	return strings.Join(parts, "; ")
}
