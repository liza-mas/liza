package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Flash delays: how long a transient notice stays visible.
const (
	cmdResultFlashDelay   = 3 * time.Second  // command result in the footer
	alertBannerFlashDelay = 10 * time.Second // critical alert banner
)

// Update handles all incoming messages and returns the updated model + next Cmd.
// It routes each message type to its handler; key messages go to update_keys.go.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case stateChangedMsg:
		return m.onStateChanged()
	case StateMsg:
		return m.onState(msg)
	case tea.KeyMsg:
		return m.onKey(msg)
	case CmdResultMsg:
		return m.onCmdResult(msg)
	case rolesMsg:
		return m.onRoles(msg)
	case stopDoneMsg:
		m.closeWatcher()
		return m, tea.Quit
	case TickMsg:
		return m.onTick()
	case alertsMsg:
		return m.onAlerts(msg)
	case LogEntriesMsg:
		return m.onLogEntries(msg)
	case tea.WindowSizeMsg:
		return m.onWindowSize(msg)
	case errMsg:
		return m.onError(msg)
	case watcherClosedMsg:
		m.watcher = nil // prevent re-subscribe attempts
		return m, nil
	default:
		return m, nil
	}
}

// closeWatcher releases the state watcher, if one is subscribed.
func (m Model) closeWatcher() {
	if m.watcher != nil {
		m.watcher.Close()
	}
}

// requestStateRead starts a state read, or queues one behind the read in
// flight; the queued read starts when that read completes. Unless forced, the
// read skips parsing a file unchanged since the last read (readStateCmd).
// Watcher events force: they report a write.
func (m *Model) requestStateRead(force bool) tea.Cmd {
	if m.stateReadInFlight {
		m.stateReadQueued = true
		m.stateReadQueuedForce = m.stateReadQueuedForce || force
		return nil
	}
	m.stateReadInFlight = true
	return readStateCmd(m.blackboard, m.state, m.stateFile, force)
}

func (m Model) onStateChanged() (tea.Model, tea.Cmd) {
	// Call before the return: Go leaves unspecified whether m is copied
	// before or after a call in the same return statement mutates it.
	read := m.requestStateRead(true)
	return m, tea.Batch(read, readLogCmd(m.logPath, m.logPosition), watchStateCmd(m.watcher))
}

func (m Model) onState(msg StateMsg) (tea.Model, tea.Cmd) {
	m.stateReadInFlight = false
	if msg.Err == nil {
		// Before starting a queued read, which compares against them.
		m.state = msg.State
		m.stateFile = msg.File
		m.ready = true
	}
	var cmds []tea.Cmd
	if m.stateReadQueued {
		force := m.stateReadQueuedForce
		m.stateReadQueued, m.stateReadQueuedForce = false, false
		cmds = append(cmds, m.requestStateRead(force))
	}
	if msg.Err != nil {
		m.activities = appendActivity(m.activities, warningActivity(activityActionStateReadError, msg.Err.Error()))
		// Checks never run against the stale snapshot: they stay due for
		// the next good read, and meanwhile the unreadable state is reported.
		if m.checksDue && !m.checksInFlight {
			m.checksInFlight = true
			cmds = append(cmds, reportStateReadFailureCmd(m.alertsLogPath, msg.Err, m.stateCache))
		}
		return m, tea.Batch(cmds...)
	}
	if m.checksDue && !m.checksInFlight {
		m.checksDue = false
		m.checksInFlight = true
		cmds = append(cmds, runChecksCmd(m.projectRoot, m.alertsLogPath, m.state, m.stateCache))
	}
	m.syncAnomalyActivities()
	return m, tea.Batch(cmds...)
}

// syncAnomalyActivities feeds new blackboard anomalies to the activity feed
// (incremental). state.Anomalies is append-only; lastAnomalyCount tracks the
// delta.
func (m *Model) syncAnomalyActivities() {
	if m.state == nil || len(m.state.Anomalies) <= m.lastAnomalyCount {
		return
	}
	for _, a := range m.state.Anomalies[m.lastAnomalyCount:] {
		m.activities = appendActivity(m.activities, anomalyActivity(a))
	}
	m.lastAnomalyCount = len(m.state.Anomalies)
}

func (m Model) onCmdResult(msg CmdResultMsg) (tea.Model, tea.Cmd) {
	m.cmdResult = &msg
	m.cmdExpiry = time.Now().Add(cmdResultFlashDelay)
	return m, nil
}

func (m Model) onRoles(msg rolesMsg) (tea.Model, tea.Cmd) {
	m.roleCompletions = msg.Roles
	m.roleTypes = msg.RoleTypes
	m.sprintTerminals = msg.SprintTerminals
	m.stateCategories = msg.StateCategories
	return m, nil
}

func (m Model) onTick() (tea.Model, tea.Cmd) {
	// Clear expired command result
	if m.cmdResult != nil && time.Now().After(m.cmdExpiry) {
		m.cmdResult = nil
	}
	// Checks run once this tick's read completes, against its result.
	m.checksDue = true
	read := m.requestStateRead(false)
	return m, tea.Batch(read, readLogCmd(m.logPath, m.logPosition), tickCmd())
}

func (m Model) onAlerts(msg alertsMsg) (tea.Model, tea.Cmd) {
	// Update state cache with modified copy from check goroutine
	m.checksInFlight = false
	m.stateCache = msg.StateCache
	m.activities = resolveInactiveAlerts(m.activities, msg.ActiveAlertKeys)
	m.activities = dropResolvedTransientAlerts(m.activities)
	if shouldClearResolvedBanner(m.alertBanner, msg.ActiveAlertKeys) {
		m.alertBanner = nil
		m.alertExpiry = time.Time{}
	}

	for _, a := range msg.Alerts {
		entry := alertActivity(a)
		m.activities = appendActivity(m.activities, entry)

		// Critical alerts set the alert banner
		if a.Level == levelCritical {
			bannerCopy := entry
			m.alertBanner = &bannerCopy
			m.alertExpiry = time.Now().Add(alertBannerFlashDelay)
		}
	}
	if msg.WriteErr != nil {
		m.activities = appendActivity(m.activities, warningActivity(activityActionWriteError, msg.WriteErr.Error()))
	}
	return m, nil
}

func (m Model) onLogEntries(msg LogEntriesMsg) (tea.Model, tea.Cmd) {
	if msg.NewPosition > 0 {
		m.logPosition = msg.NewPosition
	}
	for _, e := range msg.Entries {
		m.activities = appendActivity(m.activities, logActivity(e))
	}
	return m, nil
}

func (m Model) onWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.columnTier = ColumnTierForWidth(msg.Width)
	m.styles = NewStyles(msg.Width)
	return m, nil
}

func (m Model) onError(msg errMsg) (tea.Model, tea.Cmd) {
	m.activities = appendActivity(m.activities, warningActivity(activityActionWatcherError, msg.Error()))
	if m.watcher != nil {
		return m, watchStateCmd(m.watcher)
	}
	return m, nil
}
