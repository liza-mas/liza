package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderActivityPanel renders the activity feed as a bordered panel.
// Displays the tail of m.activities (most recent entries that fit in height).
// Three source formats per spec §Activity Panel.
func (m Model) renderActivityPanel(height int) string {
	title := m.styles.PanelTitle.Render("⚡ ACTIVITY")
	contentWidth := max(m.width-panelChromeWidth, 1)

	if len(m.activities) == 0 {
		return m.styles.ActivityPanel.Height(max(height-2, 1)).Render(title)
	}

	// Available content lines: height minus border (2) and title (1)
	maxLines := max(height-3, 0)

	// Tail: show last N entries
	start := max(len(m.activities)-maxLines, 0)
	visible := m.activities[start:]

	var rows []string
	for _, e := range visible {
		rows = append(rows, truncateActivityLine(m.formatActivityEntry(e), contentWidth))
	}

	content := title + "\n" + strings.Join(rows, "\n")
	return m.styles.ActivityPanel.Height(max(height-2, 1)).Render(content)
}

func truncateActivityLine(line string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	return lipgloss.NewStyle().Inline(true).MaxWidth(maxWidth).Render(line)
}

// formatActivityEntry formats a single activity entry based on its source type.
func (m Model) formatActivityEntry(e ActivityEntry) string {
	ts := e.Timestamp.UTC().Format("15:04:05")

	switch e.Source {
	case ActivityKindAlert:
		// HH:MM:SS  {level}  {category}: {message}
		levelStyled := m.colorLevel(e.Level)
		if e.Resolved {
			levelStyled = lipgloss.NewStyle().Foreground(ColorTerminal).Render("✓")
			return m.styles.Dimmed.Render(fmt.Sprintf("  %s  %s  resolved %s: %s", ts, levelStyled, e.Action, e.Detail))
		}
		return fmt.Sprintf("  %s  %s  %s: %s", ts, levelStyled, e.Action, e.Detail)

	case ActivityKindAnomaly:
		// HH:MM:SS  ⚠️  {reporter}: {type} [{task}]  {details}
		levelStyled := m.colorLevel(levelWarning)
		if e.Task != "" {
			return fmt.Sprintf("  %s  %s  %s: %s [%s]  %s", ts, levelStyled, e.Agent, e.Action, e.Task, e.Detail)
		}
		return fmt.Sprintf("  %s  %s  %s: %s  %s", ts, levelStyled, e.Agent, e.Action, e.Detail)

	default: // ActivityKindLog
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
	case levelCritical:
		return lipgloss.NewStyle().Foreground(ColorRejected).Render(level)
	case levelWarning:
		return lipgloss.NewStyle().Foreground(ColorPlanning).Render(level)
	default:
		return level
	}
}
