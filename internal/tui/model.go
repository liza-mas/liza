package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
)

// InputMode represents the current input mode of the TUI.
type InputMode int

const (
	InputModeNormal InputMode = iota // Normal keybinding mode
	InputModeInline                  // Inline text prompt (spawn role, pause reason)
	InputModeForm                    // Huh form overlay (add task)
)

// StateMsg carries a fresh state snapshot after a blackboard change.
type StateMsg struct {
	State *models.State
}

// TickMsg signals a periodic 10s poll tick for anomaly checks and heartbeat refresh.
type TickMsg time.Time

// AlertMsg carries an anomaly alert to display in the activity feed / banner.
type AlertMsg struct {
	Timestamp time.Time
	Level     string // "⚠️" or "🚨"
	Category  string
	Message   string
}

// CmdResultMsg carries the result of a command execution (success or error).
// Displayed as transient status in the footer for 3 seconds per spec.
type CmdResultMsg struct {
	Success bool
	Message string
}

// LogEntriesMsg carries new log entries from log.yaml.
type LogEntriesMsg struct {
	Entries []log.Entry
}

// ActivityEntry is a unified entry in the activity feed, merging log events,
// anomaly alerts, and blackboard anomalies into a single chronological list.
type ActivityEntry struct {
	Timestamp time.Time
	Source    string // "log", "alert", "anomaly"
	Agent     string // empty for alerts/anomalies
	Action    string
	Task      string // empty if not task-specific
	Detail    string
	Level     string // empty for log entries, "⚠️" or "🚨" for alerts
}

// ColumnTier defines which columns are visible at a given terminal width.
type ColumnTier int

const (
	ColumnTierMinimal  ColumnTier = iota // < 80 cols: ID, STATUS only
	ColumnTierStandard                   // ≥ 80 cols: + ROLE, CURRENT_TASK / ATTEMPT, ASSIGNED_TO
	ColumnTierWide                       // ≥ 120 cols: + TIME_ON_TASK, HEARTBEAT / AGE, DESCRIPTION
	ColumnTierFull                       // ≥ 160 cols: + PID, CONTEXT / REVIEWING_BY, DEPS, TIME_IN_STATUS
)

// ColumnTierForWidth returns the column tier for a given terminal width.
func ColumnTierForWidth(width int) ColumnTier {
	switch {
	case width >= 160:
		return ColumnTierFull
	case width >= 120:
		return ColumnTierWide
	case width >= 80:
		return ColumnTierStandard
	default:
		return ColumnTierMinimal
	}
}

// Model is the main Bubbletea model for the Liza TUI.
// It holds all state needed to render the dashboard and process input.
type Model struct {
	// State data
	state       *models.State   // current blackboard state snapshot
	activities  []ActivityEntry // merged activity feed (last 200 entries per spec)
	logPosition int64           // byte offset for incremental log.yaml reads

	// Layout
	width      int        // terminal width
	height     int        // terminal height
	columnTier ColumnTier // current column visibility tier

	// Input
	inputMode InputMode // current input mode
	keys      KeyMap    // key bindings

	// Visual
	styles Styles // Lipgloss styles (adapted to width)

	// Alerts
	alertBanner *ActivityEntry // current critical alert (auto-dismiss after 10s)
	alertExpiry time.Time      // when to auto-dismiss the alert banner

	// Command feedback
	cmdResult *CmdResultMsg // transient command result (3s display)
	cmdExpiry time.Time     // when to clear cmdResult

	// Help
	showHelp bool // help overlay visible

	// Watch state (for anomaly throttling, same as WatchConfig.StateCache)
	stateCache map[string]time.Time

	// Lifecycle
	ready       bool   // true after first state load
	projectRoot string // root directory for state.yaml, log.yaml, alerts.log
}

// New creates a new Model with default state.
// projectRoot is used to locate state.yaml, log.yaml, and alerts.log.
func New(projectRoot string) Model {
	return Model{
		activities:  make([]ActivityEntry, 0),
		keys:        NewKeyMap(),
		styles:      NewStyles(0),
		stateCache:  make(map[string]time.Time),
		projectRoot: projectRoot,
	}
}

// Init returns the initial command.
// Stub: returns nil. Actual implementation in commands.go phase.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles messages and returns the updated model.
// Stub: returns m, nil. Actual implementation in update.go phase.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

// View renders the TUI.
// Stub: returns "Loading...". Actual implementation in view.go phase.
func (m Model) View() string {
	return "Loading..."
}
