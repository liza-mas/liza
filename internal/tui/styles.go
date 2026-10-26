package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// Semantic color constants from the unified status color palette.
// Uses ANSI 256 color codes for terminal compatibility.
var (
	ColorActive          = lipgloss.Color("6")   // Cyan — active work
	ColorPlanning        = lipgloss.Color("3")   // Yellow — planning/draft
	ColorReview          = lipgloss.Color("4")   // Blue — review
	ColorToReview        = lipgloss.Color("12")  // Dim blue — submitted / awaiting review
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
	string(models.AgentStatusWorking):   ColorActive,
	string(models.AgentStatusPlanning):  ColorPlanning,
	string(models.AgentStatusStarting):  ColorPlanning,
	string(models.AgentStatusReviewing): ColorReview,
	string(models.AgentStatusIdle):      ColorIdle,
	string(models.AgentStatusWaiting):   ColorIdle,
	string(models.AgentStatusHandoff):   ColorHandoff,

	// System statuses
	string(models.SystemModeRunning):      ColorActive,
	string(models.SystemModePaused):       ColorPlanning,
	string(models.SprintStatusCheckpoint): ColorHandoff,
	string(models.SystemModeStopped):      ColorRejected,

	// Sprint statuses
	string(models.SprintStatusCompleted):  ColorApproved,
	string(models.SprintStatusInProgress): ColorActive,
	string(models.SprintStatusAborted):    ColorRejected,

	// Task statuses (exact)
	string(models.TaskStatusMerged):            ColorApproved,
	string(models.TaskStatusBlocked):           ColorRejected,
	string(models.TaskStatusIntegrationFailed): ColorRejected,
	string(models.TaskStatusAbandoned):         ColorTerminal,
	string(models.TaskStatusSuperseded):        ColorTerminal,
	string(models.TaskStatusDraft):             ColorBareDraft,
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

// TaskStatusColor returns the color for a task status, preferring pipeline
// lifecycle categories over legacy status-name pattern matching.
func TaskStatusColor(status models.TaskStatus, categories map[models.TaskStatus]pipeline.StateCategory) lipgloss.Color {
	if category, ok := categories[status]; ok {
		return CategoryColor(category)
	}
	return StatusColor(string(status))
}

// CategoryColor returns the TUI color for a pipeline lifecycle category.
func CategoryColor(category pipeline.StateCategory) lipgloss.Color {
	switch category {
	case pipeline.StateCategoryInitial:
		return ColorBareDraft
	case pipeline.StateCategoryExecuting:
		return ColorActive
	case pipeline.StateCategorySubmitted:
		return ColorToReview
	case pipeline.StateCategoryReviewing, pipeline.StateCategoryReviewing2:
		return ColorReview
	case pipeline.StateCategoryApproved, pipeline.StateCategoryClean:
		return ColorApproved
	case pipeline.StateCategoryRejected:
		return ColorRejected
	case pipeline.StateCategoryPartiallyApproved:
		return ColorPartialApproved
	default:
		return ColorFallback
	}
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
		return ColorActive, true
	case strings.HasSuffix(status, "_TO_REVIEW"):
		return ColorToReview, true
	case strings.HasSuffix(status, "_READY_FOR_REVIEW"):
		return ColorToReview, true
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
		return ColorBareDraft, true
	default:
		return "", false
	}
}

// idleDotStatuses are statuses that display hollow ○ instead of filled ●.
var idleDotStatuses = map[string]bool{
	string(models.AgentStatusIdle):    true,
	string(models.AgentStatusWaiting): true,
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
	StatusToReview  lipgloss.Style
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

		// Panels — Width is content width; border adds 2, so use width-2
		// to keep total rendered width == terminal width.
		AgentPanel: lipgloss.NewStyle().
			Width(max(width-2, 0)).
			Border(panelBorder).
			BorderForeground(lipgloss.Color("8")),
		TaskPanel: lipgloss.NewStyle().
			Width(max(width-2, 0)).
			Border(panelBorder).
			BorderForeground(lipgloss.Color("8")),
		ActivityPanel: lipgloss.NewStyle().
			Width(max(width-2, 0)).
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
		StatusToReview:  lipgloss.NewStyle().Foreground(ColorToReview),
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
