package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Update handles all incoming messages and returns the updated model + next Cmd.
// Phase 3 covers data messages only. Phase 4 adds key dispatch.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case stateChangedMsg:
		return m, tea.Batch(
			readStateCmd(m.blackboard),
			readLogCmd(m.logPath, m.logPosition),
			watchStateCmd(m.watcher),
		)

	case StateMsg:
		m.state = msg.State
		m.ready = true

		// Sync new blackboard anomalies to activity feed (incremental).
		// state.Anomalies is append-only; track count for delta processing.
		if m.state != nil && len(m.state.Anomalies) > m.lastAnomalyCount {
			for _, a := range m.state.Anomalies[m.lastAnomalyCount:] {
				entry := ActivityEntry{
					Timestamp: a.Timestamp,
					Source:    "anomaly",
					Agent:     a.Reporter,
					Action:    a.Type,
					Task:      a.Task,
					Detail:    formatAnomalyDetails(a.Details),
					Level:     "⚠️",
				}
				m.activities = appendActivity(m.activities, entry)
			}
			m.lastAnomalyCount = len(m.state.Anomalies)
		}
		return m, nil

	case TickMsg:
		return m, tea.Batch(
			readStateCmd(m.blackboard),
			runChecksCmd(m.projectRoot, m.alertsLogPath, m.state, m.stateCache),
			readLogCmd(m.logPath, m.logPosition),
			tickCmd(),
		)

	case alertsMsg:
		// Update state cache with modified copy from check goroutine
		m.stateCache = msg.StateCache

		for _, a := range msg.Alerts {
			entry := ActivityEntry{
				Timestamp: a.Timestamp,
				Source:    "alert",
				Action:    a.Category,
				Detail:    a.Message,
				Level:     a.Level,
			}
			m.activities = appendActivity(m.activities, entry)

			// Critical alerts (🚨) set the alert banner
			if a.Level == "🚨" {
				bannerCopy := entry
				m.alertBanner = &bannerCopy
				m.alertExpiry = time.Now().Add(10 * time.Second)
			}
		}
		return m, nil

	case LogEntriesMsg:
		if msg.NewPosition > 0 {
			m.logPosition = msg.NewPosition
		}
		for _, e := range msg.Entries {
			task := ""
			if e.Task != nil {
				task = *e.Task
			}
			entry := ActivityEntry{
				Timestamp: e.Timestamp,
				Source:    "log",
				Agent:     e.Agent,
				Action:    e.Action,
				Task:      task,
				Detail:    e.Detail,
			}
			m.activities = appendActivity(m.activities, entry)
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.columnTier = ColumnTierForWidth(msg.Width)
		m.styles = NewStyles(msg.Width)
		return m, nil

	case errMsg:
		// No user-visible action for now; future: show in status bar
		return m, nil

	case watcherClosedMsg:
		m.watcher = nil // prevent re-subscribe attempts
		return m, nil

	default:
		return m, nil
	}
}

// appendActivity appends an entry to the activity slice, capping at 200 entries.
func appendActivity(activities []ActivityEntry, entry ActivityEntry) []ActivityEntry {
	activities = append(activities, entry)
	if len(activities) > 200 {
		activities = activities[len(activities)-200:]
	}
	return activities
}

// formatAnomalyDetails converts an anomaly's Details map to a compact display string.
// Keys are sorted alphabetically, formatted as key=value pairs separated by spaces.
func formatAnomalyDetails(details map[string]any) string {
	if len(details) == 0 {
		return ""
	}

	keys := make([]string, 0, len(details))
	for k := range details {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%v", k, details[k])
	}
	return strings.Join(parts, " ")
}
