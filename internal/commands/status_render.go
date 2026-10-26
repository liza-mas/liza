package commands

import (
	"fmt"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/render"
)

// renderStatus renders the assembled status in the requested format.
func renderStatus(status statusData, format statusFormat) (string, error) {
	switch format {
	case statusFormatJSON:
		return render.FormatJSON(status)
	case statusFormatYAML:
		return render.FormatYAML(status)
	default: // "dashboard" or empty
		return formatStatusDashboard(status)
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
		pidStr := emptyCell
		if agent.PID != 0 {
			pidStr = fmt.Sprintf("%d", agent.PID)
		}
		health := agent.Health
		if health == "" {
			health = emptyCell
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
	if len(handoff.FailedPlanningTasks) > 0 {
		b.WriteString("Planning hand-offs requiring repair:\n")
		for _, failure := range handoff.FailedPlanningTasks {
			fmt.Fprintf(b, "  %s (%s): %s\n", failure.TaskID, failure.Transition, failure.Error)
		}
	}
	if len(handoff.UndecidedPlanningTasks) > 0 {
		b.WriteString("Plans awaiting a disposition:\n")
		for _, observation := range handoff.UndecidedPlanningTasks {
			fmt.Fprintf(b, "  %s (%s)", observation.TaskID, observation.Class)
			if observation.Blocker != "" {
				fmt.Fprintf(b, ": %s", observation.Blocker)
			}
			b.WriteString("\n")
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

// maxIntegrationSectionIDs bounds each ID list in the dashboard; JSON and YAML
// carry the full lists.
const maxIntegrationSectionIDs = 10

func writeIntegrationSection(b *strings.Builder, status *integrationLifecycleStatus) {
	if status == nil {
		return
	}
	b.WriteString("=== INTEGRATION ===\n")
	fmt.Fprintf(b, "Status: %s\n", status.Status)
	if status.ReasonCode != "" {
		fmt.Fprintf(b, "Reason: %s\n", status.ReasonCode)
	}
	for _, list := range []struct {
		label string
		ids   []string
	}{
		{"Tasks", status.TaskIDs},
		{"Requested analyses", status.RequestKeys},
		{"Reconciled tasks", status.CreatedTaskIDs},
	} {
		if len(list.ids) > 0 {
			fmt.Fprintf(b, "%s: %s\n", list.label, boundedIDList(list.ids, maxIntegrationSectionIDs))
		}
	}
	if status.Guidance != "" {
		fmt.Fprintf(b, "Guidance: %s\n", status.Guidance)
	}
	b.WriteString("\n")
}

func boundedIDList(ids []string, limit int) string {
	if len(ids) <= limit {
		return strings.Join(ids, ", ")
	}
	return fmt.Sprintf("%s (+%d more)", strings.Join(ids[:limit], ", "), len(ids)-limit)
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
	IntegrationSection string
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
	var integrationBuf strings.Builder
	writeIntegrationSection(&integrationBuf, data.Integration)

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
		IntegrationSection: integrationBuf.String(),
		AnomalyList:        anomalyList,
		TransitionsSection: transitionsBuf.String(),
	}
	return render.ExecuteTemplate("status_dashboard", tmplData)
}
