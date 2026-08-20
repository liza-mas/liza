package commands

import (
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/procscan"
	"github.com/liza-mas/liza/internal/render"
)

// StatusOptions contains options for the status command
type StatusOptions struct {
	Format      string // "dashboard", "json", "yaml"
	Detailed    bool   // Include anomalies and circuit breaker
	ProjectRoot string
}

// statusData contains all status information
type statusData struct {
	Goal               goalStatus                    `json:"goal"`
	Sprint             sprintStatus                  `json:"sprint"`
	Config             configStatus                  `json:"config"`
	Tasks              taskStatus                    `json:"tasks"`
	Agents             []agentStatus                 `json:"agents"`
	AgentHealth        map[string]models.AgentHealth `json:"agent_health,omitempty"`
	AgentCapacity      agentCapacityStatus           `json:"agent_capacity" yaml:"agent_capacity"`
	OrchestratorState  orchestratorStatus            `json:"orchestrator_state"`
	WorkQueues         workQueuesStatus              `json:"work_queues"`
	PendingTransitions []pendingTransition           `json:"pending_transitions,omitempty"`
	PhaseHandoff       *phaseHandoffStatus           `json:"phase_handoff,omitempty" yaml:"phase_handoff,omitempty"`
	Anomalies          *[]string                     `json:"anomalies,omitempty"`
	CircuitBreaker     *circuitBreakerStatus         `json:"circuit_breaker,omitempty"`
}

type pendingTransition struct {
	TaskID      string   `json:"task_id"`
	Transitions []string `json:"transitions"`
}

type phaseHandoffStatus struct {
	State               string               `json:"state" yaml:"state"`
	Explanation         string               `json:"explanation" yaml:"explanation"`
	ReadyPlanningTasks  []string             `json:"ready_planning_tasks" yaml:"ready_planning_tasks"`
	MergeRequired       []phaseMergeRequired `json:"merge_required,omitempty" yaml:"merge_required,omitempty"`
	BlockingTasks       []phaseHandoffTask   `json:"blocking_tasks,omitempty" yaml:"blocking_tasks,omitempty"`
	StaleAssignedAgents []phaseHandoffTask   `json:"stale_assigned_agents,omitempty" yaml:"stale_assigned_agents,omitempty"`
}

type phaseMergeRequired struct {
	TaskID string `json:"task_id" yaml:"task_id"`
	Action string `json:"action" yaml:"action"`
}

type phaseHandoffTask struct {
	ID                  string `json:"id"`
	Status              string `json:"status"`
	RolePair            string `json:"role_pair,omitempty"`
	AssignedTo          string `json:"assigned_to,omitempty"`
	AgentStatus         string `json:"agent_status,omitempty"`
	AgentProcessStatus  string `json:"agent_process_status,omitempty"`
	ProcessStatusSource string `json:"process_status_source,omitempty"`
	ProcessStatusDetail string `json:"process_status_detail,omitempty"`
	LeaseExpires        string `json:"lease_expires,omitempty"`
}

type goalStatus struct {
	Description string `json:"description"`
	Status      string `json:"status"`
	SpecRef     string `json:"spec_ref"`
}

type sprintStatus struct {
	ID                string `json:"id"`
	Number            int    `json:"number"`
	Status            string `json:"status"`
	CheckpointTrigger string `json:"checkpoint_trigger,omitempty"`
	CheckpointNotice  string `json:"checkpoint_notice,omitempty"`
	StartTime         string `json:"start_time"`
	TasksDone         int    `json:"tasks_done"`
	TasksTotal        int    `json:"tasks_total"`
}

type configStatus struct {
	Mode        string  `json:"mode"`
	PausedBy    *string `json:"paused_by,omitempty"`
	PauseReason *string `json:"pause_reason,omitempty"`
}

type taskStatus struct {
	Total                        int                        `json:"total"`
	Active                       int                        `json:"active"`
	Terminal                     int                        `json:"terminal"`
	ByStatus                     map[string]int             `json:"by_status"`
	Claimable                    int                        `json:"claimable"`
	Reviewable                   int                        `json:"reviewable"`
	ClaimableByRole              []models.RoleTaskReadiness `json:"claimable_by_role" yaml:"claimable_by_role"`
	ReviewableByRole             []models.RoleTaskReadiness `json:"reviewable_by_role" yaml:"reviewable_by_role"`
	LegacyCoderClaimable         int                        `json:"legacy_coder_claimable" yaml:"legacy_coder_claimable"`
	LegacyCodeReviewerReviewable int                        `json:"legacy_code_reviewer_reviewable" yaml:"legacy_code_reviewer_reviewable"`
	Blocked                      int                        `json:"blocked"`
	BlockedByDeps                int                        `json:"blocked_by_deps"`
}

type agentCapacityStatus struct {
	Live     int                 `json:"live"`
	Free     int                 `json:"free"`
	Degraded int                 `json:"degraded"`
	ByRole   []roleAgentCapacity `json:"by_role" yaml:"by_role"`
}

type roleAgentCapacity struct {
	Role     string `json:"role" yaml:"role"`
	Live     int    `json:"live" yaml:"live"`
	Free     int    `json:"free" yaml:"free"`
	Degraded int    `json:"degraded" yaml:"degraded"`
}

type agentStatus struct {
	ID                  string `json:"id"`
	Role                string `json:"role"`
	Status              string `json:"status"`
	Health              string `json:"health,omitempty"`
	HealthReason        string `json:"health_reason,omitempty"`
	RecoverHint         string `json:"recover_hint,omitempty"`
	PID                 int    `json:"pid"`
	CurrentTask         string `json:"current_task"`
	TimeSinceHeartbeat  string `json:"time_since_heartbeat"`
	ProcessStatus       string `json:"process_status"`
	ProcessStatusSource string `json:"process_status_source"`
	ProcessStatusDetail string `json:"process_status_detail"`
}

type orchestratorStatus struct {
	Trigger      string                      `json:"trigger"`
	TriggerCount int                         `json:"trigger_count"`
	Reason       string                      `json:"reason"`
	Integration  *integrationLifecycleStatus `json:"integration,omitempty" yaml:"integration,omitempty"`
}

type integrationLifecycleStatus struct {
	Status         string   `json:"status" yaml:"status"`
	ReasonCode     string   `json:"reason_code,omitempty" yaml:"reason_code,omitempty"`
	TaskIDs        []string `json:"task_ids,omitempty" yaml:"task_ids,omitempty"`
	RequestKeys    []string `json:"request_keys,omitempty" yaml:"request_keys,omitempty"`
	CreatedTaskIDs []string `json:"created_task_ids,omitempty" yaml:"created_task_ids,omitempty"`
	Guidance       string   `json:"guidance,omitempty" yaml:"guidance,omitempty"`
}

type workQueuesStatus struct {
	Coder    queueStatus `json:"coder"`
	Reviewer queueStatus `json:"reviewer"`
}

type queueStatus struct {
	Available int    `json:"available"`
	Reason    string `json:"reason"`
}

type circuitBreakerStatus struct {
	Status   string   `json:"status"`
	Triggers []string `json:"triggers,omitempty"`
}

// StatusCommand returns a comprehensive system status
func StatusCommand(opts StatusOptions) (string, error) {
	statePath := paths.New(opts.ProjectRoot).StatePath()
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		return "", fmt.Errorf("failed to read state: %w", err)
	}

	pr, prErr := ops.LoadResolverForModels(opts.ProjectRoot)
	if prErr != nil {
		log.Printf("WARNING: status: failed to load pipeline resolver: %v", prErr)
	}
	status := BuildStatusData(state, opts.Detailed, opts.ProjectRoot, pr, nil)

	switch opts.Format {
	case "json":
		return render.FormatJSON(status)
	case "yaml":
		return render.FormatYAML(status)
	default: // "dashboard" or empty
		return formatStatusDashboard(status)
	}
}

// BuildStatusData populates the statusData structure from state.
// The caller is responsible for loading the pipeline resolver and passing it in.
// prWarnings is not used internally — it exists so JSON callers can thread
// resolver-load warnings through the response envelope.
func BuildStatusData(state *models.State, detailed bool, projectRoot string, pr models.PipelineResolver, prWarnings []string) statusData {
	data := statusData{}

	data.Goal = goalStatus{
		Description: state.Goal.Description,
		Status:      string(state.Goal.Status),
		SpecRef:     state.Goal.SpecRef,
	}

	data.Sprint = sprintStatus{
		ID:                state.Sprint.ID,
		Number:            state.Sprint.Number,
		Status:            string(state.Sprint.Status),
		CheckpointTrigger: state.Sprint.CheckpointTrigger,
		CheckpointNotice:  checkpointNotice(state.Sprint),
		StartTime:         state.Sprint.Timeline.Started.Format(time.RFC3339),
		TasksDone:         state.Sprint.Metrics.TasksDone,
		TasksTotal:        len(state.Tasks),
	}

	data.Config = configStatus{
		Mode: string(state.Config.Mode),
	}
	if state.Config.Mode == models.SystemModePaused {
		data.Config.PausedBy = state.Config.ModeChangedBy
	}

	data.Tasks = buildTaskStatus(state, pr)
	data.Agents = buildAgentStatuses(state)
	data.AgentHealth = currentAgentHealth(state)
	data.AgentCapacity = buildAgentCapacityStatus(state, pr)
	data.OrchestratorState = buildOrchestratorStatus(state, projectRoot)
	data.WorkQueues = buildWorkQueuesStatus(state, data.Tasks.LegacyCoderClaimable, data.Tasks.LegacyCodeReviewerReviewable, pr)
	data.PhaseHandoff = buildPhaseHandoffStatus(state, projectRoot)

	for i := range state.Tasks {
		avail := ops.AvailableManualTransitions(&state.Tasks[i], projectRoot)
		if len(avail) > 0 {
			data.PendingTransitions = append(data.PendingTransitions, pendingTransition{
				TaskID:      state.Tasks[i].ID,
				Transitions: avail,
			})
		}
	}

	if detailed {
		if len(state.Anomalies) > 0 {
			anomalies := make([]string, len(state.Anomalies))
			for i, anomaly := range state.Anomalies {
				anomalies[i] = fmt.Sprintf("[%s] %s by %s: %s",
					anomaly.Timestamp.Format("2006-01-02 15:04"),
					anomaly.Type,
					anomaly.Reporter,
					anomaly.Task)
			}
			data.Anomalies = &anomalies
		}

		if state.CircuitBreaker.Status != "" && state.CircuitBreaker.Status != "OK" {
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
			data.CircuitBreaker = cb
		}
	}

	return data
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

func buildPhaseHandoffStatus(state *models.State, projectRoot string) *phaseHandoffStatus {
	detCtx, err := ops.LoadPhaseHandoffDetectionContext(projectRoot)
	if err != nil {
		return nil
	}

	var ready []string
	var blockers []phaseHandoffTask
	var stale []phaseHandoffTask
	seenStale := make(map[string]bool)
	approvedUnmerged := ops.ApprovedPlanningTasksWithUnmergedOutput(
		state,
		detCtx.PlanningPairs,
		detCtx.PlanningApprovedStatuses,
	)

	for _, taskID := range state.Sprint.Scope.Planned {
		task := state.FindTask(taskID)
		if task == nil {
			blockers = append(blockers, phaseHandoffTask{ID: taskID, Status: "MISSING"})
			continue
		}

		if ops.IsPlanningCompleteEligible(task, detCtx.PlanningPairs, state) {
			ready = append(ready, task.ID)
		}

		if isSprintTerminal(task, detCtx.SprintTerminals) {
			continue
		}

		blocker := phaseHandoffTask{
			ID:       task.ID,
			Status:   string(task.Status),
			RolePair: task.RolePair,
		}
		if task.AssignedTo != nil {
			blocker.AssignedTo = *task.AssignedTo
			if task.LeaseExpires != nil {
				blocker.LeaseExpires = task.LeaseExpires.Format(time.RFC3339)
			}
			if assignedAgent, ok := state.Agents[*task.AssignedTo]; ok {
				blocker.AgentStatus = string(assignedAgent.Status)
				processInfo := getAgentProcessStatusInfo(*task.AssignedTo, assignedAgent)
				blocker.AgentProcessStatus = processInfo.Status
				blocker.ProcessStatusSource = processInfo.Source
				blocker.ProcessStatusDetail = processInfo.Detail
				if assignedAgent.CurrentTask != nil &&
					*assignedAgent.CurrentTask == task.ID &&
					assignedAgent.Status != models.AgentStatusIdle &&
					blocker.AgentProcessStatus != "running" &&
					!seenStale[*task.AssignedTo] {
					stale = append(stale, blocker)
					seenStale[*task.AssignedTo] = true
				}
			}
		}
		blockers = append(blockers, blocker)
	}

	if state.Sprint.Status == models.SprintStatusCompleted && len(approvedUnmerged) > 0 {
		mergeRequired := make([]phaseMergeRequired, 0, len(approvedUnmerged))
		for _, taskID := range approvedUnmerged {
			mergeRequired = append(mergeRequired, phaseMergeRequired{
				TaskID: taskID,
				Action: brand.Command("wt-merge", taskID),
			})
		}
		return &phaseHandoffStatus{
			State:               "MERGE_REQUIRED",
			Explanation:         fmt.Sprintf("%d approved planning task(s) must be merged before the completed sprint can advance.", len(mergeRequired)),
			ReadyPlanningTasks:  ready,
			MergeRequired:       mergeRequired,
			BlockingTasks:       blockers,
			StaleAssignedAgents: stale,
		}
	}

	if len(ready) == 0 {
		return nil
	}

	stateName := "READY"
	explanation := fmt.Sprintf("%d merged planning task(s) have unconsumed output and are ready for transition execution.", len(ready))
	if len(blockers) > 0 {
		stateName = "PARTIAL_READY"
		explanation = fmt.Sprintf("%d merged planning task(s) have unconsumed output; %d non-terminal planned task(s) are still active. %s can checkpoint PLANNING_COMPLETE and create implementation tasks after resume without waiting for the active tasks to finish.", len(ready), len(blockers), brand.NameTitle)
	}
	if state.Sprint.Status == models.SprintStatusCheckpoint {
		stateName = "CHECKPOINTED"
		explanation = fmt.Sprintf("%d merged planning task(s) are waiting behind a checkpoint; resume the sprint to execute their pipeline transitions.", len(ready))
	}
	if state.Sprint.Status == models.SprintStatusCompleted {
		stateName = "COMPLETED"
		explanation = fmt.Sprintf("%d merged planning task(s) are waiting in a completed sprint; resume/advance to execute their pipeline transitions.", len(ready))
	}

	return &phaseHandoffStatus{
		State:               stateName,
		Explanation:         explanation,
		ReadyPlanningTasks:  ready,
		BlockingTasks:       blockers,
		StaleAssignedAgents: stale,
	}
}

func isSprintTerminal(task *models.Task, pipelineTerminals []models.TaskStatus) bool {
	if task == nil {
		return false
	}
	if task.RolePair != "" {
		return task.Status.IsPipelineSprintTerminal(pipelineTerminals)
	}
	return task.Status.IsSprintTerminal()
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
	trigger := string(result.Trigger)
	count := result.Count

	ps := orchestratorStatus{
		Trigger:      trigger,
		TriggerCount: count,
	}
	if result.Integration.Status != "" {
		ps.Integration = &integrationLifecycleStatus{
			Status:         result.Integration.Status,
			ReasonCode:     result.Integration.ReasonCode,
			TaskIDs:        append([]string(nil), result.Integration.TaskIDs...),
			RequestKeys:    append([]string(nil), result.Integration.RequestKeys...),
			CreatedTaskIDs: append([]string(nil), result.Integration.CreatedTaskIDs...),
			Guidance:       result.Integration.Guidance,
		}
	}

	switch trigger {
	case "INITIAL_PLANNING":
		ps.Reason = "No tasks exist; initial planning needed"
	case "BLOCKED_TASKS":
		ps.Reason = fmt.Sprintf("%d task(s) are blocked and need attention", count)
		if state.SprintStalled() {
			ps.Reason += " (\u26a0\ufe0f sprint stalled \u2014 all non-terminal tasks blocked)"
		}
	case "INTEGRATION_FAILED":
		ps.Reason = fmt.Sprintf("%d task(s) failed integration", count)
	case "HYPOTHESIS_EXHAUSTED":
		ps.Reason = fmt.Sprintf("%d task(s) exhausted hypotheses (2+ failures)", count)
	case "IMMEDIATE_DISCOVERY":
		ps.Reason = fmt.Sprintf("%d immediate discovery(ies) need to be converted to tasks", count)
	case "PLANNING_COMPLETE":
		ps.Reason = fmt.Sprintf("%d planning task(s) merged with output[]; ready for coding task expansion", count)
	case "MANY_TO_ONE_READY":
		ps.Reason = fmt.Sprintf("%d many-to-one cohort(s) ready for consolidation transition", count)
	case "CODING_COMPLETE", "INTEGRATION_WAITING", "INTEGRATION_BLOCKED", "INTEGRATION_EXHAUSTED", "INTEGRATION_UNAVAILABLE":
		ps.Reason = formatIntegrationLifecycleReason(ps.Integration)
	case "SPRINT_COMPLETE":
		if ps.Integration != nil && ps.Integration.Status == "complete" {
			ps.Reason = "Authoritative integration completion is clean for current integration HEAD"
		} else {
			ps.Reason = fmt.Sprintf("All %d planned task(s) reached terminal state; sprint complete", count)
		}
	case "NONE":
		ps.Reason = "No triggers; orchestrator is idle"
	default:
		ps.Reason = "Unknown trigger"
	}

	return ps
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

func writeTasksSection(b *strings.Builder, tasks taskStatus) {
	b.WriteString("=== TASKS ===\n")
	fmt.Fprintf(b, "Total: %d (%d active, %d terminal)\n",
		tasks.Total, tasks.Active, tasks.Terminal)

	if len(tasks.ByStatus) > 0 {
		b.WriteString("\nBy Status:\n")
		statuses := make([]string, 0, len(tasks.ByStatus))
		for status := range tasks.ByStatus {
			statuses = append(statuses, status)
		}
		slices.Sort(statuses)
		for _, status := range statuses {
			fmt.Fprintf(b, "  %s: %d\n", status, tasks.ByStatus[status])
		}
	}

	fmt.Fprintf(b, "\nClaimable: %d tasks\n", tasks.Claimable)
	fmt.Fprintf(b, "Reviewable: %d tasks\n", tasks.Reviewable)
	writeRoleTaskReadiness(b, "Claimable by role", tasks.ClaimableByRole)
	writeRoleTaskReadiness(b, "Reviewable by role", tasks.ReviewableByRole)
	fmt.Fprintf(b, "Legacy coder claimable: %d tasks\n", tasks.LegacyCoderClaimable)
	fmt.Fprintf(b, "Legacy code-reviewer reviewable: %d tasks\n", tasks.LegacyCodeReviewerReviewable)
	if tasks.Blocked > 0 {
		fmt.Fprintf(b, "Blocked: %d tasks\n", tasks.Blocked)
	}
	if tasks.BlockedByDeps > 0 {
		fmt.Fprintf(b, "Blocked by dependencies: %d tasks\n", tasks.BlockedByDeps)
	}
	b.WriteString("\n")
}

func writeRoleTaskReadiness(b *strings.Builder, label string, readiness []models.RoleTaskReadiness) {
	if len(readiness) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", label)
	for _, role := range readiness {
		fmt.Fprintf(b, "  %s: %d\n", role.Role, role.Count)
	}
}

func writeAgentCapacitySection(b *strings.Builder, capacity agentCapacityStatus) {
	b.WriteString("=== AGENT CAPACITY ===\n")
	fmt.Fprintf(b, "Live: %d, Free: %d, Degraded: %d\n", capacity.Live, capacity.Free, capacity.Degraded)
	if len(capacity.ByRole) > 0 {
		rows := make([][]string, 0, len(capacity.ByRole))
		for _, role := range capacity.ByRole {
			rows = append(rows, []string{
				role.Role,
				fmt.Sprintf("%d", role.Live),
				fmt.Sprintf("%d", role.Free),
				fmt.Sprintf("%d", role.Degraded),
			})
		}
		b.WriteString(render.FormatTable([]string{"Role", "Live", "Free", "Degraded"}, rows))
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func writeAgentsSection(b *strings.Builder, agents []agentStatus) {
	b.WriteString("=== AGENTS ===\n")
	if len(agents) == 0 {
		b.WriteString("No active agents\n\n")
		return
	}
	headers := []string{"ID", "Role", "Status", "Health", "PID", "Task", "Heartbeat", "Process"}
	rows := make([][]string, len(agents))
	for i, agent := range agents {
		pidStr := "-"
		if agent.PID != 0 {
			pidStr = fmt.Sprintf("%d", agent.PID)
		}
		health := agent.Health
		if health == "" {
			health = "-"
		}
		rows[i] = []string{
			agent.ID,
			agent.Role,
			agent.Status,
			health,
			pidStr,
			agent.CurrentTask,
			agent.TimeSinceHeartbeat,
			agent.ProcessStatus,
		}
	}
	b.WriteString(render.FormatTable(headers, rows))
	b.WriteString("\n\n")
}

func writeAgentHealthSection(b *strings.Builder, health map[string]models.AgentHealth) {
	if len(health) == 0 {
		return
	}
	b.WriteString("=== AGENT HEALTH ===\n")
	ids := make([]string, 0, len(health))
	for agentID := range health {
		ids = append(ids, agentID)
	}
	slices.Sort(ids)

	rows := make([][]string, 0, len(ids))
	for _, agentID := range ids {
		h := health[agentID]
		rows = append(rows, []string{
			agentID,
			h.Role,
			string(h.State),
			h.Reason,
			h.RecoverHint,
		})
	}
	b.WriteString(render.FormatTable([]string{"ID", "Role", "Health", "Reason", "Recover Hint"}, rows))
	b.WriteString("\n\n")
}

func writePhaseHandoffSection(b *strings.Builder, handoff *phaseHandoffStatus) {
	if handoff == nil {
		return
	}
	b.WriteString("=== PHASE HANDOFF ===\n")
	fmt.Fprintf(b, "State: %s\n", handoff.State)
	fmt.Fprintf(b, "Explanation: %s\n", handoff.Explanation)
	if len(handoff.ReadyPlanningTasks) > 0 {
		b.WriteString("Ready planning tasks:\n")
		for _, taskID := range handoff.ReadyPlanningTasks {
			fmt.Fprintf(b, "  - %s\n", taskID)
		}
	}
	if len(handoff.MergeRequired) > 0 {
		b.WriteString("Merge required:\n")
		for _, merge := range handoff.MergeRequired {
			fmt.Fprintf(b, "  %s: %s\n", merge.TaskID, merge.Action)
		}
	}
	if len(handoff.BlockingTasks) > 0 {
		b.WriteString("Non-terminal planned tasks:\n")
		b.WriteString(render.FormatTable(
			[]string{"ID", "Status", "Role Pair", "Agent", "Process"},
			phaseHandoffTaskRows(handoff.BlockingTasks),
		))
		b.WriteString("\n")
	}
	if len(handoff.StaleAssignedAgents) > 0 {
		b.WriteString("Stale assigned agents:\n")
		b.WriteString(render.FormatTable(
			[]string{"Task", "Agent", "Agent Status", "Process", "Lease Expires"},
			phaseHandoffStaleRows(handoff.StaleAssignedAgents),
		))
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func phaseHandoffTaskRows(tasks []phaseHandoffTask) [][]string {
	rows := make([][]string, 0, len(tasks))
	for _, task := range tasks {
		rows = append(rows, []string{
			task.ID,
			task.Status,
			task.RolePair,
			task.AssignedTo,
			task.AgentProcessStatus,
		})
	}
	return rows
}

func phaseHandoffStaleRows(tasks []phaseHandoffTask) [][]string {
	rows := make([][]string, 0, len(tasks))
	for _, task := range tasks {
		rows = append(rows, []string{
			task.ID,
			task.AssignedTo,
			task.AgentStatus,
			task.AgentProcessStatus,
			task.LeaseExpires,
		})
	}
	return rows
}

// statusDashboardData is the template data for status_dashboard.tmpl
type statusDashboardData struct {
	statusData
	TasksSection       string
	AgentsSection      string
	CapacitySection    string
	HandoffSection     string
	AnomalyList        []string
	TransitionsSection string
}

// formatStatusDashboard renders the status as a dashboard
func formatStatusDashboard(data statusData) (string, error) {
	// Pre-render imperative sections (table formatters stay as-is)
	var tasksBuf, agentsBuf, capacityBuf strings.Builder
	writeTasksSection(&tasksBuf, data.Tasks)
	writeAgentsSection(&agentsBuf, data.Agents)
	writeAgentHealthSection(&agentsBuf, data.AgentHealth)
	writeAgentCapacitySection(&capacityBuf, data.AgentCapacity)
	var handoffBuf strings.Builder
	writePhaseHandoffSection(&handoffBuf, data.PhaseHandoff)

	var anomalyList []string
	if data.Anomalies != nil {
		anomalyList = *data.Anomalies
	}

	var transitionsBuf strings.Builder
	if len(data.PendingTransitions) > 0 {
		transitionsBuf.WriteString("=== PENDING TRANSITIONS ===\n")
		for _, pt := range data.PendingTransitions {
			for _, tr := range pt.Transitions {
				fmt.Fprintf(&transitionsBuf, "  %s: %s\n", pt.TaskID, brand.Command("proceed", pt.TaskID, tr))
			}
		}
		transitionsBuf.WriteString("\n")
	}

	tmplData := statusDashboardData{
		statusData:         data,
		TasksSection:       tasksBuf.String(),
		AgentsSection:      agentsBuf.String(),
		CapacitySection:    capacityBuf.String(),
		HandoffSection:     handoffBuf.String(),
		AnomalyList:        anomalyList,
		TransitionsSection: transitionsBuf.String(),
	}
	return render.ExecuteTemplate("status_dashboard", tmplData)
}
