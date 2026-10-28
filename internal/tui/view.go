package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/mattn/go-runewidth"
)

// Panel layout, in terminal rows.
const (
	panelOverhead     = 4 // border(2) + title(1) + header(1)
	minPanelHeight    = 4 // overhead only — shows title+header, zero data rows
	minActivityHeight = 5 // border(2) + title(1) + at least 2 entries
)

// Panel layout, in terminal columns and rows.
const (
	// panelChromeWidth is a panel's border plus its default horizontal padding.
	panelChromeWidth = 4
	// overlayChromeWidth is an overlay's border plus its padding.
	overlayChromeWidth = 8
	// headerFooterHeight is the rows the header and the footer take.
	headerFooterHeight = 2
)

// emptyCell is the table cell of an absent value.
const emptyCell = "—"

func (m Model) isOperationallyTerminal(task models.Task) bool {
	return task.Status.IsTerminal() || m.stateCategories[task.Status] == pipeline.StateCategoryClean
}

// taskVisible reports whether the current task filter lists task.
func (m Model) taskVisible(task models.Task) bool {
	switch m.taskFilter {
	case TaskFilterActive:
		return !m.isOperationallyTerminal(task) && task.Status != models.TaskStatusBlocked
	case TaskFilterActiveBlocked:
		return !m.isOperationallyTerminal(task)
	default:
		return true
	}
}

// View renders the complete TUI dashboard.
// Vertical stack: Header → Alert banner → Agent panel → Task panel → Activity → Footer.
// When m.ready is false, returns a centered "Loading..." message.
func (m Model) View() string {
	if !m.ready {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, "Loading...")
	}

	header := m.renderHeader()
	footer := m.renderFooter()
	awaitingHuman := m.renderAwaitingHumanNotice()
	alertBanner := ""
	if !m.alertBannerRepeatsNotice(awaitingHuman) {
		alertBanner = m.renderAlertBanner()
	}

	// Fixed-height elements: header (1), footer (1), notice and alert banner (0 or 1 each)
	fixedHeight := headerFooterHeight
	if awaitingHuman != "" {
		fixedHeight++
	}
	if alertBanner != "" {
		fixedHeight++
	}
	remaining := max(m.height-fixedHeight, 0)

	// Render task and agent panels with MaxHeight ceiling (no blank padding).
	taskBudget, agentBudget := m.panelBudgets(remaining)
	tasks := m.renderTaskPanel(taskBudget)
	agents := m.renderAgentPanel(agentBudget)

	// Activity fills remaining space (Height pads to fill).
	activityHeight := max(remaining-lipgloss.Height(tasks)-lipgloss.Height(agents), minActivityHeight)

	// Activity panel slot: help overlay > activity panel
	var activity string
	if m.showHelp {
		activity = m.renderHelpOverlay(activityHeight)
	} else {
		activity = m.renderActivityPanel(activityHeight)
	}

	// Compose vertical stack
	sections := []string{header}
	if awaitingHuman != "" {
		sections = append(sections, awaitingHuman)
	}
	if alertBanner != "" {
		sections = append(sections, alertBanner)
	}
	sections = append(sections, tasks, agents, activity, footer)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// panelBudgets distributes the remaining height between the task and agent
// panels; the activity panel takes what is left.
// Strategy: task and agent panels render at natural (content-fitting) height
// using MaxHeight as a ceiling — no blank-line padding. When space is tight,
// terminal tasks (MERGED, ABANDONED, SUPERSEDED) shed first — they are the
// lowest-value rows.
func (m Model) panelBudgets(remaining int) (taskBudget, agentBudget int) {
	var agentCount, activeTaskCount, terminalTaskCount int
	if m.state != nil {
		agentCount = len(m.state.Agents)
		for _, t := range m.state.Tasks {
			if !m.taskVisible(t) {
				continue
			}
			if m.isOperationallyTerminal(t) {
				terminalTaskCount++
			} else {
				activeTaskCount++
			}
		}
	}

	// Natural (content-fitting) heights
	agentNatural := agentCount + panelOverhead + currentAgentHealthDetailLineCount(m.state)
	fullTaskNatural := (activeTaskCount + terminalTaskCount) + panelOverhead
	activeTaskNatural := activeTaskCount + panelOverhead

	switch {
	case fullTaskNatural+agentNatural+minActivityHeight <= remaining:
		// Normal: everything fits including terminal tasks
		taskBudget = fullTaskNatural
		agentBudget = agentNatural
	case activeTaskNatural+agentNatural+minActivityHeight <= remaining:
		// Shed some terminal tasks, keep all agents
		taskBudget = remaining - agentNatural - minActivityHeight
		agentBudget = agentNatural
	default:
		// Tight: agents and active tasks split available space evenly
		avail := remaining - minActivityHeight
		agentBudget = min(agentNatural, max(avail/2, minPanelHeight))
		taskBudget = min(activeTaskNatural, max(avail-agentBudget, minPanelHeight))
	}

	// On tiny terminals, total may exceed remaining — layout already degraded.
	return max(taskBudget, minPanelHeight), max(agentBudget, minPanelHeight)
}

// renderHeader renders the header bar:
//
//	⚡  {BRAND}  |  {goal.description}  |  sprint: {sprint.id} {sprint.status}  |  system: {STATUS}
//
// Full-width, background-colored. STATUS colored per system mode.
// If m.state is nil, renders "⚡  {BRAND}  |  Loading..."
func (m Model) renderHeader() string {
	nameUpper := brand.RuntimeValues().NameUpper
	if m.state == nil {
		return m.styles.HeaderBar.Render(fmt.Sprintf("⚡  %s  |  Loading...", nameUpper))
	}

	sprintStatusText := strings.ToUpper(string(m.state.Sprint.Status))
	coloredSprintStatus := m.styles.HeaderStatus.
		Foreground(StatusColor(sprintStatusText)).
		Render(sprintStatusText)

	systemStatusText := string(m.state.Config.Mode)
	coloredSystemStatus := m.styles.HeaderStatus.
		Foreground(StatusColor(systemStatusText)).
		Render(systemStatusText)

	sprintLabel := m.styles.HeaderLabel.Render("sprint:")
	systemLabel := m.styles.HeaderLabel.Render("system:")

	prefix := fmt.Sprintf("⚡  %s  |  ", nameUpper)
	suffix := fmt.Sprintf("  |  %s %s %s  |  %s %s",
		sprintLabel,
		m.state.Sprint.ID,
		coloredSprintStatus,
		systemLabel,
		coloredSystemStatus,
	)
	contentWidth := max(m.width-2, 0) // HeaderBar adds one-cell left/right padding.
	goalWidth := max(contentWidth-lipgloss.Width(prefix)-lipgloss.Width(suffix), 0)
	goal := truncateVisual(m.state.Goal.Description, goalWidth)

	content := prefix + goal + suffix

	return m.styles.HeaderBar.Render(content)
}

// truncateVisual truncates s to at most maxWidth visual cells, appending "…" if truncated.
// Whitespace runs, including line breaks, collapse to one space: callers render single-line cells.
func truncateVisual(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	s = strings.Join(strings.Fields(s), " ")
	if runewidth.StringWidth(s) <= maxWidth {
		return s
	}
	if maxWidth == 1 {
		return "…"
	}
	return runewidth.Truncate(s, maxWidth-1, "…")
}

// padRight pads s with spaces to reach the given visual width.
func padRight(s string, width int) string {
	visWidth := runewidth.StringWidth(s)
	if visWidth >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visWidth)
}
