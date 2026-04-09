package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/huh"
	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// InputMode represents the current input mode of the TUI.
type InputMode int

const (
	InputModeNormal InputMode = iota // Normal keybinding mode
	InputModeInline                  // Inline text prompt (spawn role, pause reason)
	InputModeForm                    // Huh form overlay (add task)
)

// InlineAction identifies which command the inline input will execute on confirm.
type InlineAction int

const (
	InlineActionNone             InlineAction = iota
	InlineActionSpawn                         // s — collecting role name (spawns with default CLI)
	InlineActionSpawnWith                     // S — collecting role name (transitions to CLI prompt)
	InlineActionSpawnCLI                      // S (phase 2) — collecting CLI name
	InlineActionPause                         // p — collecting optional reason
	InlineActionStopConfirm                   // Q — collecting y/n confirmation
	InlineActionTerminate                     // t — collecting agent ID
	InlineActionTerminateConfirm              // t (phase 2) — collecting y/n confirmation
)

// rolesMsg carries loaded role and role-pair names from pipeline config.
type rolesMsg struct {
	Roles     []string
	RolePairs []string
}

// stopDoneMsg signals that StopCommand completed and the TUI should quit.
type stopDoneMsg struct{}

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
	Entries     []log.Entry
	NewPosition int64 // byte offset after last read entry — caller updates m.logPosition
}

// StateWatcher abstracts the fsnotify-based state watcher from internal/db.
// Satisfied by *db.stateWatcher returned from Blackboard.WatchForChanges().
type StateWatcher interface {
	Events() <-chan struct{}
	Errors() <-chan error
	Close() error
}

// stateChangedMsg signals that the state file was modified (from fsnotify watcher).
// Triggers a state re-read and log re-read.
//
//lint:ignore U1000 used by commands.go and update.go
type stateChangedMsg struct{}

// watcherClosedMsg signals the fsnotify watcher channel closed unexpectedly.
// The TUI falls back to tick-only refresh.
//
//lint:ignore U1000 used by commands.go and update.go
type watcherClosedMsg struct{}

// alertsMsg carries a batch of alerts from anomaly checks.
// Includes the updated state cache to avoid data races (cache is copied
// before the Cmd goroutine runs, modified by checks, returned here).
//
//lint:ignore U1000 used by commands.go and update.go
type alertsMsg struct {
	Alerts     []AlertMsg
	StateCache map[string]time.Time
	WriteErr   error // non-nil if alert persistence to alerts.log failed
}

// errMsg carries an error from an async Cmd function.
//
//lint:ignore U1000 used by commands.go and update.go
type errMsg struct {
	err error
}

//lint:ignore U1000 used by commands.go and update.go
func (e errMsg) Error() string { return e.err.Error() }

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
	ColumnTierWide                       // ≥ 120 cols: + LAST_HEARTBEAT / AGE, DESCRIPTION
	ColumnTierFull                       // ≥ 160 cols: + PID / REVIEWING_BY, DEPS, TIME_IN_STATUS
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
	inputMode        InputMode        // current input mode
	keys             KeyMap           // key bindings
	textInput        textinput.Model  // Bubbles text input for inline prompts
	huhForm          *huh.Form        // active Huh form (nil when no form)
	formData         *addTaskFormData // bound form data for Huh form fields
	inlineAction     InlineAction     // which action inline input serves
	inlineLabel      string           // prompt label shown before textinput (e.g., "Role: ")
	roleCompletions  []string         // cached role names from pipeline config for tab-completion
	rolePairNames    []string         // cached role-pair names from pipeline config for add-task form
	agentCompletions []string         // snapshot of agent IDs for tab-completion (built on 't' press)
	completionIdx    int              // current position in tab-completion cycle
	completionPrefix string           // text prefix when Tab was first pressed (filters completions)
	spawnRole        string           // role name pending CLI selection (S flow)
	terminateTarget  string           // agent ID pending termination confirmation

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

	// Data layer
	watcher          StateWatcher   // fsnotify subscription; nil after close
	blackboard       *db.Blackboard // for state reads
	logPath          string         // absolute path to log.yaml
	alertsLogPath    string         // absolute path to alerts.log
	lastAnomalyCount int            // tracks processed state.Anomalies for incremental sync

	// Lifecycle
	ready       bool   // true after first state load
	projectRoot string // root directory for state.yaml, log.yaml, alerts.log
}

// resolvedDefaultCLI returns the effective default CLI from config, with nil-safe fallback.
// Uses the full resolution chain (config → LIZA_DEFAULT_CLI env → const) even when state is nil.
func (m Model) resolvedDefaultCLI() string {
	if m.state == nil {
		return agent.ResolveDefaultCLI("")
	}
	return agent.ResolveDefaultCLI(m.state.Config.DefaultCLI)
}

// New creates a new Model. Creates the fsnotify watcher and blackboard.
// Returns error if watcher creation fails (e.g., non-existent project root).
func New(projectRoot string) (Model, error) {
	p := paths.New(projectRoot)
	bb := db.For(p.StatePath())
	w, err := bb.WatchForChanges()
	if err != nil {
		return Model{}, err
	}
	return Model{
		activities:    make([]ActivityEntry, 0),
		keys:          NewKeyMap(),
		styles:        NewStyles(0),
		stateCache:    make(map[string]time.Time),
		textInput:     textinput.New(),
		projectRoot:   projectRoot,
		watcher:       w,
		blackboard:    bb,
		logPath:       p.LogPath(),
		alertsLogPath: p.AlertsLogPath(),
	}, nil
}
