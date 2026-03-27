package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Semantic color constants from the unified status color palette.
// Uses ANSI 256 color codes for terminal compatibility.
var (
	ColorActive          = lipgloss.Color("6")   // Cyan — active work
	ColorPlanning        = lipgloss.Color("3")   // Yellow — planning/draft
	ColorReview          = lipgloss.Color("4")   // Blue — review
	ColorIdle            = lipgloss.Color("8")   // Gray — idle/waiting
	ColorHandoff         = lipgloss.Color("5")   // Magenta — handoff
	ColorApproved        = lipgloss.Color("2")   // Green — approved/done
	ColorPartialApproved = lipgloss.Color("22")  // Green dim — partially done
	ColorRejected        = lipgloss.Color("1")   // Red — rejected/blocked
	ColorTerminal        = lipgloss.Color("8")   // Gray — terminal/inactive
	ColorBareDraft       = lipgloss.Color("250") // Dim white — bare draft
	ColorFallback        = lipgloss.Color("15")  // White — fallback
)

// exactStatusColors maps exact status strings to their color.
var exactStatusColors = map[string]lipgloss.Color{
	// Agent statuses (direct mapping)
	"WORKING":   ColorActive,
	"PLANNING":  ColorPlanning,
	"STARTING":  ColorPlanning,
	"REVIEWING": ColorReview,
	"IDLE":      ColorIdle,
	"WAITING":   ColorIdle,
	"HANDOFF":   ColorHandoff,

	// System statuses
	"RUNNING":    ColorActive,
	"PAUSED":     ColorPlanning,
	"CHECKPOINT": ColorHandoff,
	"STOPPED":    ColorRejected,

	// Sprint statuses
	"COMPLETED":   ColorApproved,
	"IN_PROGRESS": ColorActive,
	"ABORTED":     ColorRejected,

	// Task statuses (exact)
	"MERGED":             ColorApproved,
	"BLOCKED":            ColorRejected,
	"INTEGRATION_FAILED": ColorRejected,
	"ABANDONED":          ColorTerminal,
	"SUPERSEDED":         ColorTerminal,
	"DRAFT":              ColorBareDraft,
}

// StatusColor returns the lipgloss.Color for a given status string.
// Matching order: exact match → suffix → prefix → fallback.
// This ensures pipeline-configurable statuses render correctly without TUI changes.
func StatusColor(status string) lipgloss.Color {
	// Exact match
	if c, ok := exactStatusColors[status]; ok {
		return c
	}

	// Suffix match
	if c, ok := matchSuffix(status); ok {
		return c
	}

	// Prefix match
	if c, ok := matchPrefix(status); ok {
		return c
	}

	// Fallback
	return ColorFallback
}

func matchSuffix(status string) (lipgloss.Color, bool) {
	switch {
	case strings.HasSuffix(status, "_PARTIALLY_APPROVED"):
		return ColorPartialApproved, true
	case strings.HasSuffix(status, "_APPROVED"):
		return ColorApproved, true
	case strings.HasSuffix(status, "_REJECTED"):
		return ColorRejected, true
	case strings.HasSuffix(status, "_PLANNING"):
		return ColorPlanning, true
	case strings.HasSuffix(status, "_TO_REVIEW"):
		return ColorReview, true
	case strings.HasSuffix(status, "_READY_FOR_REVIEW"):
		return ColorReview, true
	default:
		return "", false
	}
}

func matchPrefix(status string) (lipgloss.Color, bool) {
	switch {
	case strings.HasPrefix(status, "IMPLEMENTING_"):
		return ColorActive, true
	case strings.HasPrefix(status, "REVIEWING_"):
		return ColorReview, true
	case strings.HasPrefix(status, "DRAFT_"):
		return ColorPlanning, true
	default:
		return "", false
	}
}

// idleDotStatuses are statuses that display hollow ○ instead of filled ●.
var idleDotStatuses = map[string]bool{
	"IDLE":    true,
	"WAITING": true,
}

// StatusDot returns "●" for active statuses or "○" for idle/waiting.
func StatusDot(status string) string {
	if idleDotStatuses[status] {
		return "○"
	}
	return "●"
}

// Styles holds all Lipgloss style definitions for the TUI.
// Constructed via NewStyles(width int) to adapt to terminal width.
type Styles struct {
	// Header bar: full-width, background-colored
	HeaderBar    lipgloss.Style
	HeaderTitle  lipgloss.Style
	HeaderLabel  lipgloss.Style // bold white — survives ANSI resets from colored substrings
	HeaderStatus lipgloss.Style

	// Panel borders: rounded border, full-width
	AgentPanel    lipgloss.Style
	TaskPanel     lipgloss.Style
	ActivityPanel lipgloss.Style

	// Panel titles
	PanelTitle lipgloss.Style

	// Footer bar: full-width
	FooterBar  lipgloss.Style
	FooterKey  lipgloss.Style
	FooterDesc lipgloss.Style

	// Alert banner: highlighted bar
	AlertBanner lipgloss.Style

	// Help overlay
	HelpOverlay lipgloss.Style

	// Status text: colored per palette
	StatusActive    lipgloss.Style
	StatusPlanning  lipgloss.Style
	StatusReview    lipgloss.Style
	StatusIdle      lipgloss.Style
	StatusHandoff   lipgloss.Style
	StatusApproved  lipgloss.Style
	StatusRejected  lipgloss.Style
	StatusTerminal  lipgloss.Style
	StatusBareDraft lipgloss.Style
	StatusFallback  lipgloss.Style

	// Dim style for terminal tasks (MERGED, ABANDONED, SUPERSEDED shown dimmed)
	Dimmed lipgloss.Style
}

// NewStyles creates a Styles instance adapted to the given terminal width.
func NewStyles(width int) Styles {
	panelBorder := lipgloss.RoundedBorder()

	return Styles{
		// Header bar
		HeaderBar: lipgloss.NewStyle().
			Width(width).
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("15")).
			Bold(true).
			Padding(0, 1),
		HeaderTitle: lipgloss.NewStyle().
			Foreground(ColorActive).
			Bold(true),
		HeaderLabel: lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Bold(true),
		HeaderStatus: lipgloss.NewStyle().
			Bold(true),

		// Panels
		AgentPanel: lipgloss.NewStyle().
			Width(width).
			Border(panelBorder).
			BorderForeground(lipgloss.Color("8")),
		TaskPanel: lipgloss.NewStyle().
			Width(width).
			Border(panelBorder).
			BorderForeground(lipgloss.Color("8")),
		ActivityPanel: lipgloss.NewStyle().
			Width(width).
			Border(panelBorder).
			BorderForeground(lipgloss.Color("8")),

		// Panel titles
		PanelTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")),

		// Footer bar
		FooterBar: lipgloss.NewStyle().
			Width(width).
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("8")).
			Padding(0, 1),
		FooterKey: lipgloss.NewStyle().
			Foreground(ColorActive).
			Bold(true),
		FooterDesc: lipgloss.NewStyle().
			Foreground(lipgloss.Color("250")),

		// Alert banner
		AlertBanner: lipgloss.NewStyle().
			Width(width).
			Background(lipgloss.Color("1")).
			Foreground(lipgloss.Color("15")).
			Bold(true).
			Padding(0, 1),

		// Help overlay
		HelpOverlay: lipgloss.NewStyle().
			Border(panelBorder).
			BorderForeground(ColorActive).
			Padding(1, 2),

		// Status styles
		StatusActive:    lipgloss.NewStyle().Foreground(ColorActive),
		StatusPlanning:  lipgloss.NewStyle().Foreground(ColorPlanning),
		StatusReview:    lipgloss.NewStyle().Foreground(ColorReview),
		StatusIdle:      lipgloss.NewStyle().Foreground(ColorIdle),
		StatusHandoff:   lipgloss.NewStyle().Foreground(ColorHandoff),
		StatusApproved:  lipgloss.NewStyle().Foreground(ColorApproved),
		StatusRejected:  lipgloss.NewStyle().Foreground(ColorRejected),
		StatusTerminal:  lipgloss.NewStyle().Foreground(ColorTerminal),
		StatusBareDraft: lipgloss.NewStyle().Foreground(ColorBareDraft),
		StatusFallback:  lipgloss.NewStyle().Foreground(ColorFallback),

		// Dimmed
		Dimmed: lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
	}
}
