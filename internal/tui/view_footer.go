package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// renderFooter renders the context-sensitive footer bar.
// Normal mode: keybinding hints. Inline mode: input-mode hints.
// Includes transient command result display (cmdResultFlashDelay visibility).
func (m Model) renderFooter() string {
	var hints string

	switch m.inputMode {
	case InputModeInline:
		leftContent := m.inlineLabel + m.textInput.View()
		rightContent := m.renderInlineHints()
		hints = leftContent + "  " + rightContent
	default:
		hints = m.renderNormalHints()
	}

	// Append command result if active and not expired
	if m.cmdResult != nil && time.Now().Before(m.cmdExpiry) {
		var resultStyle lipgloss.Style
		if m.cmdResult.Success {
			resultStyle = lipgloss.NewStyle().Foreground(ColorApproved)
		} else {
			resultStyle = lipgloss.NewStyle().Foreground(ColorRejected)
		}
		hints += "  " + resultStyle.Render(m.cmdResult.Message)
	}

	return m.styles.FooterBar.Render(hints)
}

// renderNormalHints renders the keybinding hints for normal mode.
func (m Model) renderNormalHints() string {
	bindings := m.keys.ShortHelp()
	parts := make([]string, len(bindings))
	for i, b := range bindings {
		key := b.Help().Key
		desc := b.Help().Desc
		parts[i] = m.styles.FooterKey.Render("["+key+"]") + " " + m.styles.FooterDesc.Render(desc)
	}
	return strings.Join(parts, "  ")
}

// renderInlineHints renders hints for inline input mode.
func (m Model) renderInlineHints() string {
	// Show different hints for confirmation mode
	if m.inlineAction.isConfirmation() {
		return m.renderHints([][2]string{{"y/n", "confirm"}, {"Esc", "cancel"}})
	}
	return m.renderHints([][2]string{{"Tab", "complete"}, {"Enter", "confirm"}, {"Esc", "cancel"}})
}

// renderHints formats key-description pairs as styled footer hints.
func (m Model) renderHints(hints [][2]string) string {
	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = m.styles.FooterKey.Render("["+h[0]+"]") + " " + m.styles.FooterDesc.Render(h[1])
	}
	return strings.Join(parts, "  ")
}
