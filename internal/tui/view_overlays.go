package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/commands"
)

// renderAwaitingHumanNotice renders a persistent line while the run is parked
// until a human acts. Unlike the alert banner it is derived from state, so it
// neither expires nor clears on keypress; the header's status word alone reads
// as healthy next to "system: RUNNING".
func (m Model) renderAwaitingHumanNotice() string {
	if m.state == nil {
		return ""
	}
	notice := commands.AwaitingHumanNotice(m.state)
	if notice == "" {
		return ""
	}
	return m.styles.AlertBanner.Render(truncateVisual("🚨 "+notice, max(m.width-2, 0)))
}

// alertBannerRepeatsNotice reports whether the alert banner shows the awaiting
// human ask that notice already states.
func (m Model) alertBannerRepeatsNotice(notice string) bool {
	return notice != "" && m.alertBanner != nil && m.alertBanner.Action == alerts.CategoryAwaitingHuman.String()
}

// renderAlertBanner renders the critical alert banner.
// Returns empty string if no active alert (m.alertBanner == nil or expired).
// Only one banner visible at a time (latest wins) per spec §Alert Banner.
func (m Model) renderAlertBanner() string {
	if m.alertBanner == nil {
		return ""
	}
	if time.Now().After(m.alertExpiry) {
		return ""
	}
	content := fmt.Sprintf("%s %s: %s", m.alertBanner.Level, m.alertBanner.Action, m.alertBanner.Detail)
	return m.styles.AlertBanner.Render(content)
}

// helpOverlayColumnWidth is the width each keybinding group takes in the help overlay.
const helpOverlayColumnWidth = 28

// renderHelpOverlay renders the help overlay showing all keybindings.
// Displayed over the activity panel area when m.showHelp is true.
// Uses FullHelp() from KeyMap for grouped binding display.
func (m Model) renderHelpOverlay(height int) string {
	groups := m.keys.FullHelp()
	groupNames := []string{"ACTIONS", "SYSTEM"}

	// Render each group as a column
	var columns []string
	for i, bindings := range groups {
		name := ""
		if i < len(groupNames) {
			name = groupNames[i]
		}

		var lines []string
		lines = append(lines, m.styles.PanelTitle.Render(name))
		for _, b := range bindings {
			h := b.Help()
			line := m.styles.FooterKey.Render("["+h.Key+"]") + " " + m.styles.FooterDesc.Render(h.Desc)
			lines = append(lines, line)
		}
		columns = append(columns, strings.Join(lines, "\n"))
	}

	// Join side-by-side if width permits, otherwise stack vertically
	// Each column needs helpOverlayColumnWidth chars; borders+padding take ~8
	availWidth := m.width - overlayChromeWidth
	maxSideBySide := availWidth / helpOverlayColumnWidth
	if maxSideBySide < 1 {
		maxSideBySide = 1
	}

	var content string
	if maxSideBySide >= len(columns) {
		// All groups side-by-side
		styled := make([]string, len(columns))
		for i, col := range columns {
			styled[i] = lipgloss.NewStyle().Width(helpOverlayColumnWidth).Render(col)
		}
		content = lipgloss.JoinHorizontal(lipgloss.Top, styled...)
	} else {
		// Stack vertically
		content = strings.Join(columns, "\n\n")
	}

	return m.styles.HelpOverlay.Width(m.width).Height(height).Render(content)
}
