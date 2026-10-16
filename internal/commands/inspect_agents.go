package commands

import (
	stderrors "errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/procscan"
	"github.com/liza-mas/liza/internal/render"
)

// inspectAgentsOptions contains options for agent inspection
type inspectAgentsOptions struct {
	Format       string // Output format: json, yaml, table, value
	RoleFilter   string // Filter by role
	StatusFilter string // Filter by status
	Internal     bool   // Return structured data for composition
	Zombies      bool   // Return live liza agent processes missing from state
	ProjectRoot  string // Project root for live process scope checks
	WarnWriter   io.Writer
}

// agentInfo represents agent information with computed fields
type agentInfo struct {
	ID                  string  `json:"id" yaml:"id"`
	Role                string  `json:"role" yaml:"role"`
	Status              string  `json:"status" yaml:"status"`
	Health              string  `json:"health,omitempty" yaml:"health,omitempty"`
	HealthReason        string  `json:"health_reason,omitempty" yaml:"health_reason,omitempty"`
	RecoverHint         string  `json:"recover_hint,omitempty" yaml:"recover_hint,omitempty"`
	Provider            string  `json:"provider,omitempty" yaml:"provider,omitempty"`
	PID                 int     `json:"pid" yaml:"pid"`
	ProcessStatus       string  `json:"process_status" yaml:"process_status"`
	ProcessStatusSource string  `json:"process_status_source,omitempty" yaml:"process_status_source,omitempty"`
	ProcessStatusDetail string  `json:"process_status_detail,omitempty" yaml:"process_status_detail,omitempty"`
	CurrentTask         *string `json:"current_task,omitempty" yaml:"current_task,omitempty"`
	TimeOnTask          string  `json:"time_on_task,omitempty" yaml:"time_on_task,omitempty"`   // Computed
	TimeSinceHeartbeat  string  `json:"time_since_heartbeat" yaml:"time_since_heartbeat"`       // Computed
	LeaseExpires        *string `json:"lease_expires,omitempty" yaml:"lease_expires,omitempty"` // Computed (formatted)
	Terminal            string  `json:"terminal" yaml:"terminal"`
	IterationsTotal     int     `json:"iterations_total" yaml:"iterations_total"`
	ContextPercent      int     `json:"context_percent" yaml:"context_percent"`
}

var findZombieAgents = procscan.FindZombieAgents

// inspectAgents lists all agents or filters by criteria
func inspectAgents(state *models.State, opts inspectAgentsOptions) (any, error) {
	if opts.Zombies {
		return inspectZombieAgents(state, opts)
	}

	// Get all agents as a slice (agents are stored in a map)
	agents := make([]agentInfo, 0, len(state.Agents))
	for agentID, agent := range state.Agents {
		// Apply filters
		if opts.RoleFilter != "" && agent.Role != opts.RoleFilter {
			continue
		}
		if opts.StatusFilter != "" && string(agent.Status) != opts.StatusFilter {
			continue
		}

		// Find the task this agent is working on (if any)
		var currentTask *models.Task
		if agent.CurrentTask != nil {
			currentTask = state.FindTask(*agent.CurrentTask)
		}

		// Build agentInfo with computed fields
		info := buildAgentInfoWithHealth(agentID, &agent, currentTask, state.AgentHealth[agentID])
		agents = append(agents, info)
	}

	// Sort agents by ID for consistent output
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].ID < agents[j].ID
	})

	// If called internally (for composition), return structured data
	if opts.Internal {
		return agents, nil
	}

	// Otherwise, format for output
	return formatAgentsOutput(agents, opts.Format)
}

func inspectZombieAgents(state *models.State, opts inspectAgentsOptions) (any, error) {
	scan, err := findZombieAgents(procscan.ZombieScanOptions{
		ProjectRoot:    opts.ProjectRoot,
		GoalID:         state.Goal.ID,
		RegisteredPIDs: registeredAgentPIDs(state),
	})
	if stderrors.Is(err, procscan.ErrProcessScanUnavailable) {
		return nil, fmt.Errorf("zombie agent process scanning unavailable: procfs not found on this host")
	}
	if err != nil {
		return nil, err
	}

	sort.Slice(scan.Zombies, func(i, j int) bool {
		return scan.Zombies[i].PID < scan.Zombies[j].PID
	})
	sort.Slice(scan.UnknownScope, func(i, j int) bool {
		return scan.UnknownScope[i].PID < scan.UnknownScope[j].PID
	})
	writeUnknownScopeWarning(opts.WarnWriter, scan.UnknownScope)

	if opts.Internal {
		return scan.Zombies, nil
	}
	return formatZombieAgentsOutput(scan.Zombies, opts.Format)
}

func registeredAgentPIDs(state *models.State) map[int]bool {
	pids := make(map[int]bool, len(state.Agents))
	for _, agent := range state.Agents {
		if agent.PID > 0 {
			pids[agent.PID] = true
		}
	}
	return pids
}

// inspectAgent shows details for a single agent
func inspectAgent(state *models.State, agentID string, opts inspectAgentsOptions) (any, error) {
	// Find the agent
	agent, exists := state.Agents[agentID]
	if !exists {
		return nil, &errors.NotFoundError{Entity: "agent", ID: agentID}
	}

	// Find the task this agent is working on (if any)
	var currentTask *models.Task
	if agent.CurrentTask != nil {
		currentTask = state.FindTask(*agent.CurrentTask)
	}

	// Build agentInfo with computed fields
	info := buildAgentInfoWithHealth(agentID, &agent, currentTask, state.AgentHealth[agentID])

	// If called internally, return structured data
	if opts.Internal {
		return info, nil
	}

	// Otherwise, format for output
	return formatAgentOutput(info, opts.Format)
}

// buildAgentInfo converts an Agent to agentInfo with computed fields
func buildAgentInfo(agentID string, agent *models.Agent, currentTask *models.Task) agentInfo {
	return buildAgentInfoWithHealth(agentID, agent, currentTask, models.AgentHealth{})
}

func buildAgentInfoWithHealth(agentID string, agent *models.Agent, currentTask *models.Task, health models.AgentHealth) agentInfo {
	info := agentInfo{
		ID:              agentID,
		Role:            agent.Role,
		Status:          string(agent.Status),
		Provider:        agent.Provider,
		CurrentTask:     agent.CurrentTask,
		Terminal:        agent.Terminal,
		IterationsTotal: agent.IterationsTotal,
		ContextPercent:  agent.ContextPercent,
	}
	if agentHealthIsCurrentDegraded(health, *agent) {
		info.Health = string(health.State)
		info.HealthReason = health.Reason
		info.RecoverHint = health.RecoverHint
	}

	// Copy PID and determine process status.
	info.PID = agent.PID
	processInfo := getAgentProcessStatusInfo(agentID, *agent)
	info.ProcessStatus = processInfo.Status
	info.ProcessStatusSource = processInfo.Source
	info.ProcessStatusDetail = processInfo.Detail

	// Compute time since last heartbeat
	timeSinceHeartbeat := calculateTimeSinceHeartbeat(agent)
	info.TimeSinceHeartbeat = render.FormatDuration(timeSinceHeartbeat)

	// Compute time on task (if agent is working on a task)
	if currentTask != nil {
		timeOnTask := AgentTimeOnTask(currentTask, agentID)
		info.TimeOnTask = render.FormatDuration(timeOnTask)
	}

	// Format lease expiry if present
	if agent.LeaseExpires != nil {
		remaining := time.Until(*agent.LeaseExpires)
		formatted := render.FormatDuration(remaining)
		info.LeaseExpires = &formatted
	}

	return info
}

// formatAgentsOutput formats a list of agents for output
func formatAgentsOutput(agents []agentInfo, format string) (string, error) {
	// Default to table format
	if format == "" {
		format = "table"
	}

	switch format {
	case "json":
		return render.FormatJSON(agents)
	case "yaml":
		return render.FormatYAML(agents)
	case "table":
		return formatAgentsTable(agents), nil
	case "value":
		// Value format doesn't make sense for multiple agents
		return "", fmt.Errorf("value format not supported for agent lists (use json, yaml, or table)")
	default:
		return "", fmt.Errorf("invalid format: %s", format)
	}
}

func formatZombieAgentsOutput(zombies []procscan.ZombieProcess, format string) (string, error) {
	if format == "" {
		format = "table"
	}

	switch format {
	case "json":
		return render.FormatJSON(zombies)
	case "yaml":
		return render.FormatYAML(zombies)
	case "table":
		return formatZombieAgentsTable(zombies), nil
	case "value":
		return "", fmt.Errorf("value format not supported for zombie agent lists (use json, yaml, or table)")
	default:
		return "", fmt.Errorf("invalid format: %s", format)
	}
}

func formatZombieAgentsTable(zombies []procscan.ZombieProcess) string {
	if len(zombies) == 0 {
		return "No verified zombie agents found"
	}

	headers := []string{"PID", "ROLE", "CLI", "GOAL", "CWD", "REASON"}
	rows := make([][]string, 0, len(zombies))
	for _, zombie := range zombies {
		rows = append(rows, []string{
			fmt.Sprintf("%d", zombie.PID),
			zombie.Role,
			zombie.CLI,
			zombie.GoalID,
			zombie.CWD,
			zombie.Reason,
		})
	}
	return render.FormatTable(headers, rows) + "\n\nCMDLINE:\n" + formatZombieCmdlines(zombies)
}

func formatZombieCmdlines(zombies []procscan.ZombieProcess) string {
	lines := make([]string, 0, len(zombies))
	for _, zombie := range zombies {
		lines = append(lines, fmt.Sprintf("%d: %s", zombie.PID, strings.Join(zombie.Cmdline, " ")))
	}
	return strings.Join(lines, "\n")
}

// formatAgentOutput formats a single agent for output
func formatAgentOutput(agent agentInfo, format string) (string, error) {
	// Default to value format for single agent
	if format == "" {
		format = "value"
	}

	switch format {
	case "json":
		return render.FormatJSON(agent)
	case "yaml":
		return render.FormatYAML(agent)
	case "value":
		return formatAgentValue(agent)
	case "table":
		// Single agent in table format
		return formatAgentsTable([]agentInfo{agent}), nil
	default:
		return "", fmt.Errorf("invalid format: %s", format)
	}
}

// formatAgentsTable formats agents as a table
func formatAgentsTable(agents []agentInfo) string {
	if len(agents) == 0 {
		return "No agents found"
	}

	headers := []string{"ID", "ROLE", "PROVIDER", "STATUS", "HEALTH", "PID", "CURRENT_TASK", "TIME_ON_TASK", "HEARTBEAT", "CONTEXT"}
	var rows [][]string

	for _, agent := range agents {
		currentTask := "-"
		if agent.CurrentTask != nil {
			currentTask = *agent.CurrentTask
		}

		provider := "-"
		if agent.Provider != "" {
			provider = agent.Provider
		}

		timeOnTask := "-"
		if agent.TimeOnTask != "" {
			timeOnTask = agent.TimeOnTask
		}

		var pidDisplay string
		switch {
		case agent.PID == 0:
			pidDisplay = "- n/a"
		case agent.ProcessStatus == "running":
			pidDisplay = fmt.Sprintf("%d ✓", agent.PID)
		default:
			pidDisplay = fmt.Sprintf("%d ✗", agent.PID)
		}

		rows = append(rows, []string{
			agent.ID,
			agent.Role,
			provider,
			agent.Status,
			formatAgentHealthForTable(agent),
			pidDisplay,
			currentTask,
			timeOnTask,
			agent.TimeSinceHeartbeat + " ago",
			fmt.Sprintf("%d%%", agent.ContextPercent),
		})
	}

	return render.FormatTable(headers, rows)
}

func formatAgentHealthForTable(agent agentInfo) string {
	if agent.Health == "" {
		return "-"
	}
	if agent.HealthReason == "" {
		return agent.Health
	}
	return agent.Health + ":" + agent.HealthReason
}

// formatAgentValue formats a single agent as key-value pairs
func formatAgentValue(agent agentInfo) (string, error) {
	return render.ExecuteTemplate("agent_value", agent)
}
