package commands

import (
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
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
	Goal               goalStatus            `json:"goal"`
	Sprint             sprintStatus          `json:"sprint"`
	Config             configStatus          `json:"config"`
	Tasks              taskStatus            `json:"tasks"`
	Agents             []agentStatus         `json:"agents"`
	OrchestratorState  orchestratorStatus    `json:"orchestrator_state"`
	WorkQueues         workQueuesStatus      `json:"work_queues"`
	PendingTransitions []pendingTransition   `json:"pending_transitions,omitempty"`
	PhaseHandoff       *phaseHandoffStatus   `json:"phase_handoff,omitempty"`
	Anomalies          *[]string             `json:"anomalies,omitempty"`
	CircuitBreaker     *circuitBreakerStatus `json:"circuit_breaker,omitempty"`
}

type pendingTransition struct {
	TaskID      string   `json:"task_id"`
	Transitions []string `json:"transitions"`
}

type phaseHandoffStatus struct {
	State               string             `json:"state"`
	Explanation         string             `json:"explanation"`
	ReadyPlanningTasks  []string           `json:"ready_planning_tasks"`
	BlockingTasks       []phaseHandoffTask `json:"blocking_tasks,omitempty"`
	StaleAssignedAgents []phaseHandoffTask `json:"stale_assigned_agents,omitempty"`
}

type phaseHandoffTask struct {
	ID                 string `json:"id"`
	Status             string `json:"status"`
	RolePair           string `json:"role_pair,omitempty"`
	AssignedTo         string `json:"assigned_to,omitempty"`
	AgentStatus        string `json:"agent_status,omitempty"`
	AgentProcessStatus string `json:"agent_process_status,omitempty"`
	LeaseExpires       string `json:"lease_expires,omitempty"`
}

type goalStatus struct {
	Description string `json:"description"`
	Status      string `json:"status"`
	SpecRef     string `json:"spec_ref"`
}

type sprintStatus struct {
	ID         string `json:"id"`
	Number     int    `json:"number"`
	Status     string `json:"status"`
	StartTime  string `json:"start_time"`
	TasksDone  int    `json:"tasks_done"`
	TasksTotal int    `json:"tasks_total"`
}

type configStatus struct {
	Mode        string  `json:"mode"`
	PausedBy    *string `json:"paused_by,omitempty"`
	PauseReason *string `json:"pause_reason,omitempty"`
}

type taskStatus struct {
	Total         int            `json:"total"`
	Active        int            `json:"active"`
	Terminal      int            `json:"terminal"`
	ByStatus      map[string]int `json:"by_status"`
	Claimable     int            `json:"claimable"`
	Reviewable    int            `json:"reviewable"`
	BlockedByDeps int            `json:"blocked_by_deps"`
}

type agentStatus struct {
	ID                 string `json:"id"`
	Role               string `json:"role"`
	Status             string `json:"status"`
	PID                int    `json:"pid"`
	CurrentTask        string `json:"current_task"`
	TimeSinceHeartbeat string `json:"time_since_heartbeat"`
	ProcessStatus      string `json:"process_status"`
}

type orchestratorStatus struct {
	Trigger      string `json:"trigger"`
	TriggerCount int    `json:"trigger_count"`
	Reason       string `json:"reason"`
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
		ID:         state.Sprint.ID,
		Number:     state.Sprint.Number,
		Status:     string(state.Sprint.Status),
		StartTime:  state.Sprint.Timeline.Started.Format(time.RFC3339),
		TasksDone:  state.Sprint.Metrics.TasksDone,
		TasksTotal: len(state.Tasks),
	}

	data.Config = configStatus{
		Mode: string(state.Config.Mode),
	}
	if state.Config.Mode == models.SystemModePaused {
		data.Config.PausedBy = state.Config.ModeChangedBy
	}

	data.Tasks = buildTaskStatus(state, pr)
	data.Agents = buildAgentStatuses(state)
	data.OrchestratorState = buildOrchestratorStatus(state, projectRoot)
	data.WorkQueues = buildWorkQueuesStatus(state, data.Tasks.Claimable, data.Tasks.Reviewable, pr)
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

// buildTaskStatus calculates task statistics
func buildTaskStatus(state *models.State, pr models.PipelineResolver) taskStatus {
	ts := taskStatus{
		Total:    len(state.Tasks),
		ByStatus: make(map[string]int),
	}

	mergedIDs := make(map[string]bool)
	for _, task := range state.Tasks {
		if task.Status == models.TaskStatusMerged {
			mergedIDs[task.ID] = true
		}
	}

	for _, task := range state.Tasks {
		ts.ByStatus[string(task.Status)]++

		if task.Status.IsTerminal() {
			ts.Terminal++
		} else {
			ts.Active++
		}

		if task.Status == models.TaskStatusReady ||
			task.Status == models.TaskStatusRejected ||
			task.Status == models.TaskStatusIntegrationFailed {
			hasUnsatisfiedDeps := false
			for _, depID := range task.DependsOn {
				if !mergedIDs[depID] {
					hasUnsatisfiedDeps = true
					break
				}
			}
			if hasUnsatisfiedDeps {
				ts.BlockedByDeps++
			}
		}
	}

	ts.Claimable = models.CountClaimableTasks(state, models.RoleCoder, pr)
	ts.Reviewable = models.CountReviewableTasks(state, models.RoleCodeReviewer, pr)

	return ts
}

func buildPhaseHandoffStatus(state *models.State, projectRoot string) *phaseHandoffStatus {
	detCtx, err := ops.LoadDetectionContext(projectRoot)
	if err != nil {
		return nil
	}

	var ready []string
	var blockers []phaseHandoffTask
	var stale []phaseHandoffTask
	seenStale := make(map[string]bool)

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
				blocker.AgentProcessStatus = getProcessStatus(assignedAgent.PID)
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

	if len(ready) == 0 {
		return nil
	}

	stateName := "READY"
	explanation := fmt.Sprintf("%d merged planning task(s) have unconsumed output and are ready for transition execution.", len(ready))
	if len(blockers) > 0 {
		stateName = "PARTIAL_READY"
		explanation = fmt.Sprintf("%d merged planning task(s) have unconsumed output; %d non-terminal planned task(s) are still active. Liza can checkpoint PLANNING_COMPLETE and create implementation tasks after resume without waiting for the active tasks to finish.", len(ready), len(blockers))
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

		if agent.CurrentTask != nil {
			as.CurrentTask = *agent.CurrentTask
		}

		timeSince := now.Sub(agent.Heartbeat)
		as.TimeSinceHeartbeat = render.FormatDuration(timeSince)
		as.ProcessStatus = getProcessStatus(agent.PID)
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
	result := agent.DetectOrchestratorWakeTriggers(state, pipelineTerminals, planningPairs, m2oTransitions)
	trigger := string(result.Trigger)
	count := result.Count

	ps := orchestratorStatus{
		Trigger:      trigger,
		TriggerCount: count,
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
	case "SPRINT_COMPLETE":
		ps.Reason = fmt.Sprintf("All %d planned task(s) reached terminal state; sprint complete", count)
	case "NONE":
		ps.Reason = "No triggers; orchestrator is idle"
	default:
		ps.Reason = "Unknown trigger"
	}

	return ps
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

// getProcessStatus checks if a process is running
func getProcessStatus(pid int) string {
	if pid == 0 {
		return "unknown"
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return "not found"
	}

	// Signal 0 checks process existence without actually signaling
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return "running"
	}

	return "stopped"
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
	if tasks.BlockedByDeps > 0 {
		fmt.Fprintf(b, "Blocked by dependencies: %d tasks\n", tasks.BlockedByDeps)
	}
	b.WriteString("\n")
}

func writeAgentsSection(b *strings.Builder, agents []agentStatus) {
	b.WriteString("=== AGENTS ===\n")
	if len(agents) == 0 {
		b.WriteString("No active agents\n\n")
		return
	}
	headers := []string{"ID", "Role", "Status", "PID", "Task", "Heartbeat", "Process"}
	rows := make([][]string, len(agents))
	for i, agent := range agents {
		pidStr := "-"
		if agent.PID != 0 {
			pidStr = fmt.Sprintf("%d", agent.PID)
		}
		rows[i] = []string{
			agent.ID,
			agent.Role,
			agent.Status,
			pidStr,
			agent.CurrentTask,
			agent.TimeSinceHeartbeat,
			agent.ProcessStatus,
		}
	}
	b.WriteString(render.FormatTable(headers, rows))
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
	HandoffSection     string
	AnomalyList        []string
	TransitionsSection string
}

// formatStatusDashboard renders the status as a dashboard
func formatStatusDashboard(data statusData) (string, error) {
	// Pre-render imperative sections (table formatters stay as-is)
	var tasksBuf, agentsBuf strings.Builder
	writeTasksSection(&tasksBuf, data.Tasks)
	writeAgentsSection(&agentsBuf, data.Agents)
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
				fmt.Fprintf(&transitionsBuf, "  %s: liza proceed %s %s\n", pt.TaskID, pt.TaskID, tr)
			}
		}
		transitionsBuf.WriteString("\n")
	}

	tmplData := statusDashboardData{
		statusData:         data,
		TasksSection:       tasksBuf.String(),
		AgentsSection:      agentsBuf.String(),
		HandoffSection:     handoffBuf.String(),
		AnomalyList:        anomalyList,
		TransitionsSection: transitionsBuf.String(),
	}
	return render.ExecuteTemplate("status_dashboard", tmplData)
}
