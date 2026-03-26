package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
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

	case tea.KeyMsg:
		// Dismiss alert banner on any keypress (spec §Alert Banner)
		if m.alertBanner != nil {
			m.alertBanner = nil
		}

		// Route to mode-specific handler
		switch m.inputMode {
		case InputModeInline:
			return m.handleInlineKey(msg)
		case InputModeForm:
			return m.handleFormKey(msg)
		default:
			return m.handleNormalKey(msg)
		}

	case CmdResultMsg:
		m.cmdResult = &msg
		m.cmdExpiry = time.Now().Add(3 * time.Second)
		return m, nil

	case rolesMsg:
		m.roleCompletions = msg.Roles
		return m, nil

	case stopDoneMsg:
		if m.watcher != nil {
			m.watcher.Close()
		}
		return m, tea.Quit

	case TickMsg:
		// Clear expired command result
		if m.cmdResult != nil && time.Now().After(m.cmdExpiry) {
			m.cmdResult = nil
		}
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

// handleNormalKey dispatches key events in normal mode.
func (m Model) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Spawn):
		m.inputMode = InputModeInline
		m.inlineAction = InlineActionSpawn
		m.inlineLabel = "Role: "
		m.textInput.Reset()
		m.textInput.Focus()
		m.completionIdx = 0
		m.completionPrefix = ""
		var cmd tea.Cmd
		if len(m.roleCompletions) == 0 {
			cmd = loadRolesCmd(m.projectRoot)
		}
		return m, cmd

	case key.Matches(msg, m.keys.Pause):
		m.inputMode = InputModeInline
		m.inlineAction = InlineActionPause
		m.inlineLabel = "Reason: "
		m.textInput.Reset()
		m.textInput.Focus()
		return m, nil

	case key.Matches(msg, m.keys.Resume):
		return m, resumeSystemCmd(m.projectRoot)

	case key.Matches(msg, m.keys.AddTask):
		// Form construction deferred to Task 5
		m.inputMode = InputModeForm
		return m, nil

	case key.Matches(msg, m.keys.Checkpoint):
		return m, checkpointCmd(m.projectRoot)

	case key.Matches(msg, m.keys.Quit):
		if m.watcher != nil {
			m.watcher.Close()
		}
		return m, tea.Quit

	case key.Matches(msg, m.keys.Stop):
		m.inputMode = InputModeInline
		m.inlineAction = InlineActionStopConfirm
		m.inlineLabel = "Stop? (y/n): "
		m.textInput.Reset()
		m.textInput.Focus()
		return m, nil

	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
		return m, nil

	default:
		return m, nil
	}
}

// handleInlineKey handles key events in inline input mode.
// Delegates to textinput for character input. Handles Tab (completion),
// Enter (confirm action), and Esc (cancel) specially.
func (m Model) handleInlineKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		m.inputMode = InputModeNormal
		m.inlineAction = InlineActionNone
		m.textInput.Blur()
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		value := m.textInput.Value()
		action := m.inlineAction
		m.inputMode = InputModeNormal
		m.inlineAction = InlineActionNone
		m.textInput.Blur()
		return m.executeInlineAction(action, value)

	case key.Matches(msg, key.NewBinding(key.WithKeys("tab"))):
		if m.inlineAction == InlineActionSpawn {
			m = m.cycleCompletion()
		}
		return m, nil

	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		m.completionIdx = 0
		m.completionPrefix = ""
		return m, cmd
	}
}

// executeInlineAction executes the appropriate command based on the inline action.
func (m Model) executeInlineAction(action InlineAction, value string) (tea.Model, tea.Cmd) {
	switch action {
	case InlineActionSpawn:
		if value == "" {
			return m, nil
		}
		return m, spawnAgentCmd(m.projectRoot, value)
	case InlineActionPause:
		return m, pauseSystemCmd(m.projectRoot, value)
	case InlineActionStopConfirm:
		if strings.HasPrefix(strings.ToLower(value), "y") {
			return m, stopSystemCmd(m.projectRoot)
		}
		return m, nil
	default:
		return m, nil
	}
}

// cycleCompletion cycles through role names matching the current input prefix.
func (m Model) cycleCompletion() Model {
	if len(m.roleCompletions) == 0 {
		return m
	}

	// Capture prefix on first Tab press (completionIdx == 0 means fresh start)
	if m.completionIdx == 0 {
		m.completionPrefix = m.textInput.Value()
	}

	// Filter roles matching prefix (case-insensitive)
	prefix := strings.ToLower(m.completionPrefix)
	var matches []string
	for _, role := range m.roleCompletions {
		if prefix == "" || strings.HasPrefix(strings.ToLower(role), prefix) {
			matches = append(matches, role)
		}
	}

	if len(matches) == 0 {
		return m
	}

	selected := matches[m.completionIdx%len(matches)]
	m.textInput.SetValue(selected)
	m.completionIdx++
	return m
}

// handleFormKey handles key events in form mode.
// Stub: cancels on Esc, otherwise returns model unchanged.
// Full implementation in Task 5.
func (m Model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		m.inputMode = InputModeNormal
		m.huhForm = nil
	}
	return m, nil
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
