package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/liza-mas/liza/internal/models"
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

func (m Model) renderAgentPanel(height int) string    { return "" }
func (m Model) renderTaskPanel(height int) string     { return "" }
func (m Model) renderActivityPanel(height int) string { return "" }
func (m Model) renderAlertBanner() string             { return "" }
func (m Model) renderFooter() string                  { return "" }
func (m Model) renderHelpOverlay(height int) string   { return "" }
