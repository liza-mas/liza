package commands

import (
	"fmt"
	"slices"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/procscan"
	"github.com/liza-mas/liza/internal/render"
)

// anomalyTimestampLayout is the minute-resolution layout of the detailed anomaly lines.
const anomalyTimestampLayout = "2006-01-02 15:04"

func buildGoalStatus(state *models.State) goalStatus {
	return goalStatus{
		Description: state.Goal.Description,
		Status:      string(state.Goal.Status),
		SpecRef:     state.Goal.SpecRef,
	}
}

func buildSprintStatus(state *models.State) sprintStatus {
	return sprintStatus{
		ID:                state.Sprint.ID,
		Number:            state.Sprint.Number,
		Status:            string(state.Sprint.Status),
		CheckpointTrigger: state.Sprint.CheckpointTrigger,
		CheckpointNotice:  checkpointNotice(state.Sprint),
		StartTime:         state.Sprint.Timeline.Started.Format(time.RFC3339),
		TasksDone:         state.Sprint.Metrics.TasksDone,
		TasksTotal:        len(state.Tasks),
	}
}

func buildConfigStatus(state *models.State) configStatus {
	status := configStatus{
		Mode: string(state.Config.Mode),
	}
	if state.Config.Mode == models.SystemModePaused {
		status.PausedBy = state.Config.ModeChangedBy
	}
	return status
}

// buildPendingTransitions lists, per task, the manual transitions that can be
// run now.
func buildPendingTransitions(state *models.State, projectRoot string) []pendingTransition {
	// Resolve policy from this snapshot once. The mutation-side helper reloads
	// locked state, which would turn status into one lock acquisition per task.
	cfg, err := pipeline.LoadFrozen(projectRoot)
	if err != nil {
		return nil
	}
	var options []pipeline.ResolverOption
	if state.Config.NoFollowUp {
		options = append(options, pipeline.WithNoFollowUp())
	}
	resolver := pipeline.NewResolver(cfg, options...)
	var pending []pendingTransition
	for i := range state.Tasks {
		task := &state.Tasks[i]
		avail := resolver.AvailableManualTransitions(task.Status, task.TransitionsExecuted)
		avail = slices.DeleteFunc(avail, func(name string) bool {
			return !resolver.TransitionApplies(name, task.Output)
		})
		if len(avail) > 0 {
			pending = append(pending, pendingTransition{
				TaskID: task.ID, Transitions: avail,
			})
		}
	}
	return pending
}

// buildAnomalyLines renders the recorded anomalies for the detailed view, or
// nil when there are none.
func buildAnomalyLines(state *models.State) *[]string {
	if len(state.Anomalies) == 0 {
		return nil
	}
	anomalies := make([]string, len(state.Anomalies))
	for i, anomaly := range state.Anomalies {
		anomalies[i] = fmt.Sprintf("[%s] %s by %s: %s",
			anomaly.Timestamp.Format(anomalyTimestampLayout),
			anomaly.Type,
			anomaly.Reporter,
			anomaly.Task)
	}
	return &anomalies
}

// buildCircuitBreakerStatus reports a tripped circuit breaker for the detailed
// view, or nil when it is healthy.
func buildCircuitBreakerStatus(state *models.State) *circuitBreakerStatus {
	if state.CircuitBreaker.Status == "" || state.CircuitBreaker.Status == "OK" {
		return nil
	}
	cb := &circuitBreakerStatus{
		Status: state.CircuitBreaker.Status,
	}
	if state.CircuitBreaker.CurrentTrigger != nil {
		cb.Triggers = []string{
			fmt.Sprintf("%s (severity: %s)",
				state.CircuitBreaker.CurrentTrigger.Pattern,
				state.CircuitBreaker.CurrentTrigger.Severity),
		}
	}
	return cb
}

func checkpointNotice(sprint models.Sprint) string {
	if sprint.Status != models.SprintStatusCheckpoint {
		return ""
	}
	if models.IsTransitionCheckpointTrigger(sprint.CheckpointTrigger) {
		return fmt.Sprintf("CHECKPOINT: transition gate pending; doer/reviewer work may continue; run %q to create downstream tasks", brand.Command("resume"))
	}
	return fmt.Sprintf("CHECKPOINT: agents paused; run %q", brand.Command("resume"))
}

// buildTaskStatus calculates task statistics
func buildTaskStatus(state *models.State, pr models.PipelineResolver) taskStatus {
	ts := taskStatus{
		Total:    len(state.Tasks),
		ByStatus: make(map[string]int),
	}

	depResolver := models.NewDependencyResolver(state)

	for _, task := range state.Tasks {
		ts.ByStatus[string(task.Status)]++
		if task.Status == models.TaskStatusBlocked {
			ts.Blocked++
		}

		if models.IsOperationallyTerminal(&task, pr) {
			ts.Terminal++
		} else {
			ts.Active++
		}

		if models.BlockedByDependencies(&task, pr, depResolver) {
			ts.BlockedByDeps++
		}
	}

	readiness := models.GetTaskReadiness(state, pr)
	ts.Claimable = readiness.Claimable
	ts.Reviewable = readiness.Reviewable
	ts.ClaimableByRole = readiness.ClaimableByRole
	ts.ReviewableByRole = readiness.ReviewableByRole
	ts.LegacyCoderClaimable = models.CountClaimableTasks(state, models.RoleCoder, pr)
	ts.LegacyCodeReviewerReviewable = models.CountReviewableTasks(state, models.RoleCodeReviewer, pr)

	return ts
}

// buildAgentStatuses converts agent map to agent status list
func buildAgentStatuses(state *models.State) []agentStatus {
	agents := make([]agentStatus, 0, len(state.Agents))
	now := time.Now().UTC()

	for id, agent := range state.Agents {
		as := agentStatus{
			ID:          id,
			Role:        agent.Role,
			Status:      string(agent.Status),
			CurrentTask: "",
		}
		if health, ok := state.AgentHealth[id]; ok && agentHealthIsCurrentDegraded(health, agent) {
			as.Health = string(health.State)
			as.HealthReason = health.Reason
			as.RecoverHint = health.RecoverHint
		}

		if agent.CurrentTask != nil {
			as.CurrentTask = *agent.CurrentTask
		}

		timeSince := now.Sub(agent.Heartbeat)
		as.TimeSinceHeartbeat = render.FormatDuration(timeSince)
		processInfo := getAgentProcessStatusInfo(id, agent)
		as.ProcessStatus = processInfo.Status
		as.ProcessStatusSource = processInfo.Source
		as.ProcessStatusDetail = processInfo.Detail
		as.PID = agent.PID

		agents = append(agents, as)
	}

	return agents
}

// buildWorkQueuesStatus calculates work queue availability
func buildWorkQueuesStatus(state *models.State, claimable, reviewable int, pr models.PipelineResolver) workQueuesStatus {
	return workQueuesStatus{
		Coder: queueStatus{
			Available: claimable,
			Reason:    models.GetCoderWorkDiagnostics(state, pr),
		},
		Reviewer: queueStatus{
			Available: reviewable,
			Reason:    models.GetReviewerWorkDiagnostics(state, pr),
		},
	}
}

func buildAgentCapacityStatus(state *models.State, pr models.PipelineResolver) agentCapacityStatus {
	byRole := make(map[string]*roleAgentCapacity)
	ensureRole := func(role string) *roleAgentCapacity {
		capacity, ok := byRole[role]
		if !ok {
			capacity = &roleAgentCapacity{Role: role}
			byRole[role] = capacity
		}
		return capacity
	}
	if pr != nil {
		for _, role := range pr.AllRoleNames() {
			ensureRole(role)
		}
	}

	now := time.Now()
	nilLeaseHeartbeatWindow := models.NormalizeHeartbeatInterval(state.Config.HeartbeatInterval) + models.LeaseExpiryGracePeriod
	for agentID, agentState := range state.Agents {
		capacity := ensureRole(agentState.Role)
		if !agentHasLiveRegistration(agentState, now, nilLeaseHeartbeatWindow) {
			continue
		}
		capacity.Live++
		if agentState.Status == models.AgentStatusIdle && !agentHealthIsCurrentDegraded(state.AgentHealth[agentID], agentState) {
			capacity.Free++
		}
	}
	for _, health := range currentAgentHealth(state) {
		ensureRole(health.Role).Degraded++
	}

	roles := make([]string, 0, len(byRole))
	for role := range byRole {
		roles = append(roles, role)
	}
	slices.Sort(roles)

	result := agentCapacityStatus{ByRole: make([]roleAgentCapacity, 0, len(roles))}
	for _, role := range roles {
		capacity := *byRole[role]
		result.Live += capacity.Live
		result.Free += capacity.Free
		result.Degraded += capacity.Degraded
		result.ByRole = append(result.ByRole, capacity)
	}
	return result
}

func currentAgentHealth(state *models.State) map[string]models.AgentHealth {
	if state == nil || len(state.AgentHealth) == 0 {
		return nil
	}
	current := make(map[string]models.AgentHealth)
	for agentID, health := range state.AgentHealth {
		agentState, ok := state.Agents[agentID]
		if !agentHealthIsCurrentOrOrphanedDegraded(health, agentState, ok) {
			continue
		}
		current[agentID] = health
	}
	if len(current) == 0 {
		return nil
	}
	return current
}

type processStatusInfo struct {
	Status string
	Source string
	Detail string
}

var processStatusProcRoot = "/proc"

func getAgentProcessStatusInfo(agentID string, agent models.Agent) processStatusInfo {
	return getProcessStatusInfoForAgent(agent.PID, agent.Role, agentID)
}

func getProcessStatusInfoForAgent(pid int, role, agentID string) processStatusInfo {
	status := procscan.AgentProcessStatusForPID(pid, role, agentID, processStatusProcRoot)
	return processStatusInfo{
		Status: status.DisplayStatus(),
		Source: status.Source,
		Detail: status.Detail,
	}
}
