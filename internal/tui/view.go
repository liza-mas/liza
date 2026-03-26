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

// renderTaskPanel renders the task panel as a bordered table.
// Columns adapt to terminal width per spec §Task Panel column priority table.
// Terminal tasks (MERGED, ABANDONED, SUPERSEDED) shown dimmed at bottom.
// Panel header includes sprint metrics.
func (m Model) renderTaskPanel(height int) string {
	// Build title with sprint metrics
	titleText := "✔ TASKS"
	metrics := ""
	if m.state != nil {
		total := len(m.state.Sprint.Scope.Planned)
		if total == 0 {
			total = len(m.state.Tasks)
		}
		sm := m.state.Sprint.Metrics
		metrics = fmt.Sprintf("%d/%d done │ %d blocked │ %d%% approval",
			sm.TasksDone, total, sm.TasksBlocked, sm.TaskOutcomeApprovalRatePercent)
	}

	title := m.styles.PanelTitle.Render(titleText)
	if metrics != "" {
		// Right-align metrics on the title line
		metricsStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(metrics)
		titleWidth := lipgloss.Width(title)
		metricsWidth := lipgloss.Width(metricsStyled)
		// Account for panel border padding (2 chars each side)
		availWidth := m.width - 4
		gap := availWidth - titleWidth - metricsWidth
		if gap < 2 {
			gap = 2
		}
		title = title + strings.Repeat(" ", gap) + metricsStyled
	}

	// Handle empty/nil state
	if m.state == nil || len(m.state.Tasks) == 0 {
		content := title + "\n  No tasks"
		return m.styles.TaskPanel.Render(content)
	}

	// Partition tasks into active and terminal
	var active, terminal []models.Task
	for _, t := range m.state.Tasks {
		if t.Status.IsTerminal() {
			terminal = append(terminal, t)
		} else {
			active = append(active, t)
		}
	}

	// Sort active by priority (ascending) then ID
	sort.Slice(active, func(i, j int) bool {
		if active[i].Priority != active[j].Priority {
			return active[i].Priority < active[j].Priority
		}
		return active[i].ID < active[j].ID
	})

	// Sort terminal by ID
	sort.Slice(terminal, func(i, j int) bool {
		return terminal[i].ID < terminal[j].ID
	})

	// Define columns per tier
	type column struct {
		header string
		width  int
		value  func(t models.Task) string
	}

	statusVal := func(t models.Task) string {
		dot := StatusDot(string(t.Status))
		color := StatusColor(string(t.Status))
		return lipgloss.NewStyle().Foreground(color).Render(dot + " " + string(t.Status))
	}

	attemptVal := func(t models.Task) string {
		return fmt.Sprintf("%d.%d", t.EffectiveAttempt(), t.Iteration)
	}

	assignedVal := func(t models.Task) string {
		if t.AssignedTo != nil {
			return *t.AssignedTo
		}
		return "—"
	}

	ageVal := func(t models.Task) string {
		if t.Created.IsZero() {
			return "—"
		}
		return render.FormatDuration(time.Since(t.Created))
	}

	descVal := func(t models.Task) string {
		if t.Description == "" {
			return "—"
		}
		return t.Description
	}

	reviewingByVal := func(t models.Task) string {
		if t.ReviewingBy != nil {
			return *t.ReviewingBy
		}
		return "—"
	}

	depsVal := func(t models.Task) string {
		if len(t.DependsOn) == 0 {
			return "—"
		}
		return strings.Join(t.DependsOn, ",")
	}

	timeInStatusVal := func(t models.Task) string {
		// Use last history entry timestamp if available
		if len(t.History) > 0 {
			last := t.History[len(t.History)-1]
			return render.FormatDuration(time.Since(last.Time))
		}
		// Fallback to task age
		if t.Created.IsZero() {
			return "—"
		}
		return render.FormatDuration(time.Since(t.Created))
	}

	// Build column list based on tier
	cols := []column{
		{"ID", 26, func(t models.Task) string { return t.ID }},
		{"STATUS", 22, statusVal},
	}

	if m.columnTier >= ColumnTierStandard {
		cols = append(cols,
			column{"ATT", 6, attemptVal},
			column{"ASSIGNED_TO", 16, assignedVal},
		)
	}

	if m.columnTier >= ColumnTierWide {
		cols = append(cols,
			column{"AGE", 8, ageVal},
			column{"DESCRIPTION", 24, descVal},
		)
	}

	if m.columnTier >= ColumnTierFull {
		cols = append(cols,
			column{"REVIEWING_BY", 16, reviewingByVal},
			column{"DEPS", 16, depsVal},
			column{"TIME_IN_STATUS", 16, timeInStatusVal},
		)
	}

	// Build header row
	var headerParts []string
	for _, c := range cols {
		headerParts = append(headerParts, fmt.Sprintf("%-*s", c.width, c.header))
	}
	headerRow := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("8")).
		Render("  " + strings.Join(headerParts, ""))

	// Build rows from ordered tasks
	ordered := append(active, terminal...)
	maxRows := max(height-3, 0) // border (2) + title (1)

	var rows []string
	for i, t := range ordered {
		if i >= maxRows {
			break
		}
		isTerminal := t.Status.IsTerminal()
		var parts []string
		for _, c := range cols {
			val := c.value(t)
			if c.header == "STATUS" {
				// STATUS value is already ANSI-styled; pad by raw text length
				rawLen := len(StatusDot(string(t.Status))) + 1 + len(string(t.Status))
				padding := max(c.width-rawLen, 0)
				parts = append(parts, val+strings.Repeat(" ", padding))
			} else {
				// Truncate if needed
				if len(val) > c.width-1 && c.width > 1 {
					val = val[:c.width-2] + "…"
				}
				parts = append(parts, fmt.Sprintf("%-*s", c.width, val))
			}
		}
		row := "  " + strings.Join(parts, "")
		if isTerminal {
			row = m.styles.Dimmed.Render(row)
		}
		rows = append(rows, row)
	}

	content := title + "\n" + headerRow + "\n" + strings.Join(rows, "\n")
	return m.styles.TaskPanel.Render(content)
}

// renderActivityPanel renders the activity feed as a bordered panel.
// Displays the tail of m.activities (most recent entries that fit in height).
// Three source formats per spec §Activity Panel.
func (m Model) renderActivityPanel(height int) string {
	title := m.styles.PanelTitle.Render("⚡ ACTIVITY")

	if len(m.activities) == 0 {
		return m.styles.ActivityPanel.Render(title)
	}

	// Available content lines: height minus border (2) and title (1)
	maxLines := max(height-3, 0)

	// Tail: show last N entries
	start := max(len(m.activities)-maxLines, 0)
	visible := m.activities[start:]

	var rows []string
	for _, e := range visible {
		rows = append(rows, m.formatActivityEntry(e))
	}

	content := title + "\n" + strings.Join(rows, "\n")
	return m.styles.ActivityPanel.Render(content)
}

// formatActivityEntry formats a single activity entry based on its source type.
func (m Model) formatActivityEntry(e ActivityEntry) string {
	ts := e.Timestamp.UTC().Format("15:04:05")

	switch e.Source {
	case "alert":
		// HH:MM:SS  {level}  {category}: {message}
		levelStyled := m.colorLevel(e.Level)
		return fmt.Sprintf("  %s  %s  %s: %s", ts, levelStyled, e.Action, e.Detail)

	case "anomaly":
		// HH:MM:SS  ⚠️  {reporter}: {type} [{task}]  {details}
		levelStyled := m.colorLevel("⚠️")
		if e.Task != "" {
			return fmt.Sprintf("  %s  %s  %s: %s [%s]  %s", ts, levelStyled, e.Agent, e.Action, e.Task, e.Detail)
		}
		return fmt.Sprintf("  %s  %s  %s: %s  %s", ts, levelStyled, e.Agent, e.Action, e.Detail)

	default: // "log"
		// HH:MM:SS  {agent}  {action}  [{task}]  {detail}
		task := ""
		if e.Task != "" {
			task = fmt.Sprintf("[%s]", e.Task)
		}
		return fmt.Sprintf("  %s  %-16s  %-14s  %-20s  %s", ts, e.Agent, e.Action, task, e.Detail)
	}
}

// colorLevel applies color to alert level icons.
func (m Model) colorLevel(level string) string {
	switch level {
	case "🚨":
		return lipgloss.NewStyle().Foreground(ColorRejected).Render(level)
	case "⚠️":
		return lipgloss.NewStyle().Foreground(ColorPlanning).Render(level)
	default:
		return level
	}
}
func (m Model) renderAlertBanner() string           { return "" }
func (m Model) renderFooter() string                { return "" }
func (m Model) renderHelpOverlay(height int) string { return "" }
