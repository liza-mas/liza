package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/render"
)

// View renders the complete TUI dashboard.
// Vertical stack: Header → Alert banner → Agent panel → Task panel → Activity → Footer.
// When m.ready is false, returns a centered "Loading..." message.
func (m Model) View() string {
	if !m.ready {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, "Loading...")
	}

	header := m.renderHeader()
	footer := m.renderFooter()
	alertBanner := m.renderAlertBanner()

	// Fixed-height elements: header (1), footer (1), alert banner (0 or 1)
	fixedHeight := 2 // header + footer
	if alertBanner != "" {
		fixedHeight++
	}

	remaining := m.height - fixedHeight
	if remaining < 0 {
		remaining = 0
	}

	// Distribute remaining height among panels
	agentCount := 0
	taskCount := 0
	if m.state != nil {
		agentCount = len(m.state.Agents)
		taskCount = len(m.state.Tasks)
	}

	// Agent panel: min(len(agents)+3, remaining/3) — +3 for border+title+header row
	agentHeight := agentCount + 3
	if maxAgent := remaining / 3; agentHeight > maxAgent {
		agentHeight = maxAgent
	}
	if agentHeight < 3 {
		agentHeight = 3
	}

	// Task panel: min(len(tasks)+3, remaining/3)
	taskHeight := taskCount + 3
	if maxTask := remaining / 3; taskHeight > maxTask {
		taskHeight = maxTask
	}
	if taskHeight < 3 {
		taskHeight = 3
	}

	// Activity panel: remaining height after agents+tasks
	activityHeight := remaining - agentHeight - taskHeight
	if activityHeight < 3 {
		activityHeight = 3
	}

	agents := m.renderAgentPanel(agentHeight)
	tasks := m.renderTaskPanel(taskHeight)

	var activity string
	if m.showHelp {
		activity = m.renderHelpOverlay(activityHeight)
	} else {
		activity = m.renderActivityPanel(activityHeight)
	}

	// Compose vertical stack
	sections := []string{header}
	if alertBanner != "" {
		sections = append(sections, alertBanner)
	}
	sections = append(sections, agents, tasks, activity, footer)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderHeader renders the header bar:
//
//	⚡  LIZA  |  {goal.description}  |  sprint: {sprint.id}  |  {STATUS}
//
// Full-width, background-colored. STATUS colored per system mode.
// If m.state is nil, renders "⚡  LIZA  |  Loading..."
func (m Model) renderHeader() string {
	if m.state == nil {
		return m.styles.HeaderBar.Render("⚡  LIZA  |  Loading...")
	}

	statusText := string(m.state.Config.Mode)
	statusColor := StatusColor(statusText)

	// Sprint checkpoint overrides color to magenta
	if m.state.Sprint.Status == models.SprintStatusCheckpoint {
		statusColor = ColorHandoff
	}

	coloredStatus := m.styles.HeaderStatus.
		Foreground(statusColor).
		Render(statusText)

	content := fmt.Sprintf("⚡  LIZA  |  %s  |  sprint: %s  |  %s",
		m.state.Goal.Description,
		m.state.Sprint.ID,
		coloredStatus,
	)

	return m.styles.HeaderBar.Render(content)
}

// Sub-renderer stubs — implemented by subsequent tasks.

// renderAgentPanel renders the agent panel as a bordered table.
// Columns adapt to terminal width per spec §Agent Panel column priority table.
// Agents sorted by ID for stable display order.
func (m Model) renderAgentPanel(height int) string {
	title := m.styles.PanelTitle.Render("● AGENTS")

	// Handle empty/nil state
	if m.state == nil || len(m.state.Agents) == 0 {
		content := title + "\n  No agents"
		return m.styles.AgentPanel.Render(content)
	}

	// Sort agent IDs for stable ordering
	ids := make([]string, 0, len(m.state.Agents))
	for id := range m.state.Agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// Define columns per tier
	type column struct {
		header string
		width  int
		value  func(id string, a models.Agent) string
	}

	statusVal := func(_ string, a models.Agent) string {
		dot := StatusDot(string(a.Status))
		color := StatusColor(string(a.Status))
		return lipgloss.NewStyle().Foreground(color).Render(dot + " " + string(a.Status))
	}

	currentTaskVal := func(_ string, a models.Agent) string {
		if a.CurrentTask != nil {
			return *a.CurrentTask
		}
		return "—"
	}

	timeOnTaskVal := func(_ string, a models.Agent) string {
		if a.CurrentTask == nil {
			return "—"
		}
		// Approximate using heartbeat as proxy (task creation time not directly available on Agent)
		return render.FormatDuration(time.Since(a.Heartbeat))
	}

	heartbeatVal := func(_ string, a models.Agent) string {
		if a.Heartbeat.IsZero() {
			return "—"
		}
		return render.FormatDuration(time.Since(a.Heartbeat)) + " ago"
	}

	pidVal := func(_ string, a models.Agent) string {
		if a.PID == 0 {
			return "—"
		}
		return fmt.Sprintf("%d", a.PID)
	}

	contextVal := func(_ string, a models.Agent) string {
		if a.ContextPercent == 0 {
			return "—"
		}
		return fmt.Sprintf("%d%%", a.ContextPercent)
	}

	// Build column list based on tier
	cols := []column{
		{"ID", 24, func(id string, _ models.Agent) string { return id }},
		{"STATUS", 16, statusVal},
	}

	if m.columnTier >= ColumnTierStandard {
		cols = append(cols,
			column{"ROLE", 18, func(_ string, a models.Agent) string { return a.Role }},
			column{"CURRENT_TASK", 36, currentTaskVal},
		)
	}

	if m.columnTier >= ColumnTierWide {
		cols = append(cols,
			column{"TIME_ON_TASK", 14, timeOnTaskVal},
			column{"HEARTBEAT", 14, heartbeatVal},
		)
	}

	if m.columnTier >= ColumnTierFull {
		cols = append(cols,
			column{"PID", 10, pidVal},
			column{"CONTEXT", 10, contextVal},
		)
	}

	// Build header row
	var headerParts []string
	for _, c := range cols {
		headerParts = append(headerParts, fmt.Sprintf("%-*s", c.width, c.header))
	}
	headerRow := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("8")).
		Render("  " + strings.Join(headerParts, ""))

	// Build data rows
	maxRows := max(height-3, 0) // border (2) + title (1)

	var rows []string
	for i, id := range ids {
		if i >= maxRows {
			break
		}
		agent := m.state.Agents[id]
		var parts []string
		for _, c := range cols {
			val := c.value(id, agent)
			// For STATUS column, the value is already styled (contains ANSI),
			// so we pad based on raw text length
			if c.header == "STATUS" {
				rawLen := len(StatusDot(string(agent.Status))) + 1 + len(string(agent.Status))
				padding := max(c.width-rawLen, 0)
				parts = append(parts, val+strings.Repeat(" ", padding))
			} else {
				// Truncate if needed
				if len(val) > c.width {
					val = val[:c.width-1] + "…"
				}
				parts = append(parts, fmt.Sprintf("%-*s", c.width, val))
			}
		}
		rows = append(rows, "  "+strings.Join(parts, ""))
	}

	content := title + "\n" + headerRow + "\n" + strings.Join(rows, "\n")
	return m.styles.AgentPanel.Render(content)
}
func (m Model) renderTaskPanel(height int) string     { return "" }
func (m Model) renderActivityPanel(height int) string { return "" }
func (m Model) renderAlertBanner() string             { return "" }
func (m Model) renderFooter() string                  { return "" }
func (m Model) renderHelpOverlay(height int) string   { return "" }
