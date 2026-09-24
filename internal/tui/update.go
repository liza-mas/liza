package tui

import (
	"fmt"
	"slices"
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
		default:
			return m.handleNormalKey(msg)
		}

	case CmdResultMsg:
		m.cmdResult = &msg
		m.cmdExpiry = time.Now().Add(3 * time.Second)
		return m, nil

	case rolesMsg:
		m.roleCompletions = msg.Roles
		m.roleTypes = msg.RoleTypes
		m.sprintTerminals = msg.SprintTerminals
		m.stateCategories = msg.StateCategories
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
		cmds := []tea.Cmd{readStateCmd(m.blackboard), readLogCmd(m.logPath, m.logPosition), tickCmd()}
		if !m.checksInFlight {
			m.checksInFlight = true
			cmds = append(cmds, runChecksCmd(m.projectRoot, m.alertsLogPath, m.state, m.stateCache))
		}
		return m, tea.Batch(cmds...)

	case alertsMsg:
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
			entry := ActivityEntry{
				Timestamp: a.Timestamp,
				Source:    "alert",
				Action:    a.Category,
				Detail:    a.Message,
				Level:     a.Level,
				AlertKey:  a.Key,
			}
			m.activities = appendActivity(m.activities, entry)

			// Critical alerts (🚨) set the alert banner
			if a.Level == "🚨" {
				bannerCopy := entry
				m.alertBanner = &bannerCopy
				m.alertExpiry = time.Now().Add(10 * time.Second)
			}
		}
		if msg.WriteErr != nil {
			entry := ActivityEntry{
				Timestamp: time.Now(),
				Source:    "alert",
				Action:    "write_error",
				Level:     "⚠️",
				Detail:    msg.WriteErr.Error(),
			}
			m.activities = appendActivity(m.activities, entry)
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
		entry := ActivityEntry{
			Timestamp: time.Now(),
			Source:    "alert",
			Action:    "watcher_error",
			Level:     "⚠️",
			Detail:    msg.Error(),
		}
		m.activities = appendActivity(m.activities, entry)
		if m.watcher != nil {
			return m, watchStateCmd(m.watcher)
		}
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

	case key.Matches(msg, m.keys.SpawnWith):
		m.inputMode = InputModeInline
		m.inlineAction = InlineActionSpawnWith
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

	case key.Matches(msg, m.keys.Terminate):
		// Build agent ID completion list from current state snapshot
		var agentIDs []string
		if m.state != nil {
			for id := range m.state.Agents {
				agentIDs = append(agentIDs, id)
			}
			sort.Strings(agentIDs)
		}
		m.agentCompletions = agentIDs
		m.inputMode = InputModeInline
		m.inlineAction = InlineActionTerminate
		m.inlineLabel = "Agent ID: "
		m.textInput.Reset()
		m.textInput.Focus()
		m.completionIdx = 0
		m.completionPrefix = ""
		return m, nil

	case key.Matches(msg, m.keys.Pause):
		m.inputMode = InputModeInline
		m.inlineAction = InlineActionPause
		m.inlineLabel = "Reason: "
		m.textInput.Reset()
		m.textInput.Focus()
		return m, nil

	case key.Matches(msg, m.keys.Resume):
		return m, resumeSystemCmd(m.projectRoot)

	case key.Matches(msg, m.keys.Checkpoint):
		return m, checkpointCmd(m.projectRoot)

	case key.Matches(msg, m.keys.Yolo):
		return m, toggleAutoResumeCmd(m.blackboard)

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
// For confirmation prompts (y/n), accepts single keypress without Enter.
func (m Model) handleInlineKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		m.inputMode = InputModeNormal
		m.inlineAction = InlineActionNone
		m.spawnRole = ""
		m.terminateTarget = ""
		m.textInput.Blur()
		return m, nil

	// Single-key confirmation for y/n prompts
	case m.inlineAction == InlineActionStopConfirm || m.inlineAction == InlineActionTerminateConfirm:
		// Handle y/n keys directly without Enter
		if key.Matches(msg, key.NewBinding(key.WithKeys("y", "Y"))) {
			action := m.inlineAction
			m.inputMode = InputModeNormal
			m.inlineAction = InlineActionNone
			m.textInput.Blur()
			return m.executeInlineAction(action, "y")
		}
		if key.Matches(msg, key.NewBinding(key.WithKeys("n", "N"))) {
			m.inputMode = InputModeNormal
			m.inlineAction = InlineActionNone
			m.terminateTarget = ""
			m.textInput.Blur()
			return m, nil
		}
		// For confirmation mode, also allow Enter for backward compatibility
		if key.Matches(msg, key.NewBinding(key.WithKeys("enter"))) {
			value := m.textInput.Value()
			action := m.inlineAction
			m.inputMode = InputModeNormal
			m.inlineAction = InlineActionNone
			m.textInput.Blur()
			return m.executeInlineAction(action, value)
		}
		// Ignore other keys in confirmation mode
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		value := m.textInput.Value()
		action := m.inlineAction
		m.inputMode = InputModeNormal
		m.inlineAction = InlineActionNone
		m.textInput.Blur()
		return m.executeInlineAction(action, value)

	case key.Matches(msg, key.NewBinding(key.WithKeys("tab"))):
		if m.inlineAction == InlineActionSpawn || m.inlineAction == InlineActionSpawnWith ||
			m.inlineAction == InlineActionSpawnCLI || m.inlineAction == InlineActionTerminate {
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
		if len(m.roleCompletions) > 0 && !slices.Contains(m.roleCompletions, value) {
			return m, func() tea.Msg {
				return CmdResultMsg{Success: false, Message: fmt.Sprintf("unknown role %q", value)}
			}
		}
		cli, ok := m.resolvedDefaultCLIForRole(value)
		if !ok {
			return m, func() tea.Msg {
				return CmdResultMsg{Success: false, Message: fmt.Sprintf("role type for %q is not loaded yet", value)}
			}
		}
		return m, spawnAgentCmd(m.projectRoot, value, cli)
	case InlineActionSpawnWith:
		if value == "" {
			return m, nil
		}
		if len(m.roleCompletions) > 0 && !slices.Contains(m.roleCompletions, value) {
			return m, func() tea.Msg {
				return CmdResultMsg{Success: false, Message: fmt.Sprintf("unknown role %q", value)}
			}
		}
		cli, ok := m.resolvedDefaultCLIForRole(value)
		if !ok {
			return m, func() tea.Msg {
				return CmdResultMsg{Success: false, Message: fmt.Sprintf("role type for %q is not loaded yet", value)}
			}
		}
		// Phase 2: ask for CLI
		m.spawnRole = value
		m.inputMode = InputModeInline
		m.inlineAction = InlineActionSpawnCLI
		m.inlineLabel = fmt.Sprintf("CLI (%s): ", cli)
		m.textInput.Reset()
		m.textInput.Focus()
		m.completionIdx = 0
		m.completionPrefix = ""
		return m, nil
	case InlineActionSpawnCLI:
		cli := value
		if cli == "" {
			resolvedCLI, ok := m.resolvedDefaultCLIForRole(m.spawnRole)
			if !ok {
				role := m.spawnRole
				m.spawnRole = ""
				return m, func() tea.Msg {
					return CmdResultMsg{Success: false, Message: fmt.Sprintf("role type for %q is not loaded yet", role)}
				}
			}
			cli = resolvedCLI
		}
		if !slices.Contains(m.availableCLIs(), cli) {
			m.spawnRole = ""
			return m, func() tea.Msg {
				return CmdResultMsg{Success: false, Message: fmt.Sprintf("unknown CLI %q", cli)}
			}
		}
		role := m.spawnRole
		m.spawnRole = ""
		return m, spawnAgentCmd(m.projectRoot, role, cli)
	case InlineActionPause:
		return m, pauseSystemCmd(m.projectRoot, value)
	case InlineActionTerminate:
		if value == "" {
			return m, nil
		}
		if len(m.agentCompletions) > 0 && !slices.Contains(m.agentCompletions, value) {
			return m, func() tea.Msg {
				return CmdResultMsg{Success: false, Message: fmt.Sprintf("unknown agent %q", value)}
			}
		}
		// Phase 2: ask for confirmation
		m.terminateTarget = value
		m.inputMode = InputModeInline
		m.inlineAction = InlineActionTerminateConfirm
		m.inlineLabel = fmt.Sprintf("Terminate %s? (y/n): ", value)
		m.textInput.Reset()
		m.textInput.Focus()
		return m, nil
	case InlineActionTerminateConfirm:
		if strings.HasPrefix(strings.ToLower(value), "y") {
			target := m.terminateTarget
			m.terminateTarget = ""
			return m, terminateAgentCmd(m.projectRoot, target)
		}
		m.terminateTarget = ""
		return m, nil
	case InlineActionStopConfirm:
		if strings.HasPrefix(strings.ToLower(value), "y") {
			return m, stopSystemCmd(m.projectRoot)
		}
		return m, nil
	default:
		return m, nil
	}
}

// cycleCompletion cycles through completion candidates matching the current input prefix.
// Uses roleCompletions for spawn/spawnWith, ValidCLIs for spawnCLI, agentCompletions for terminate.
func (m Model) cycleCompletion() Model {
	// Select the right completion list based on active inline action
	var candidates []string
	switch m.inlineAction {
	case InlineActionSpawn, InlineActionSpawnWith:
		candidates = m.roleCompletions
	case InlineActionSpawnCLI:
		candidates = m.availableCLIs()
	case InlineActionTerminate:
		candidates = m.agentCompletions
	}
	if len(candidates) == 0 {
		return m
	}

	// Capture prefix on first Tab press (completionIdx == 0 means fresh start)
	if m.completionIdx == 0 {
		m.completionPrefix = m.textInput.Value()
	}

	// Filter candidates matching prefix (case-insensitive)
	prefix := strings.ToLower(m.completionPrefix)
	var matches []string
	for _, c := range candidates {
		if prefix == "" || strings.HasPrefix(strings.ToLower(c), prefix) {
			matches = append(matches, c)
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

// appendActivity appends an entry to the activity slice, capping at 200 entries.
func appendActivity(activities []ActivityEntry, entry ActivityEntry) []ActivityEntry {
	activities = append(activities, entry)
	if len(activities) > 200 {
		activities = activities[len(activities)-200:]
	}
	return activities
}

func resolveInactiveAlerts(activities []ActivityEntry, activeKeys map[string]bool) []ActivityEntry {
	if activeKeys == nil {
		return activities
	}
	for i := range activities {
		if activities[i].Source != "alert" || activities[i].AlertKey == "" {
			continue
		}
		activities[i].Resolved = !activeKeys[activities[i].AlertKey]
	}
	return activities
}

func dropResolvedTransientAlerts(activities []ActivityEntry) []ActivityEntry {
	kept := activities[:0]
	for _, activity := range activities {
		if activity.Source == "alert" && activity.Resolved && isTransientAlertCategory(activity.Action) {
			continue
		}
		kept = append(kept, activity)
	}
	return kept
}

func isTransientAlertCategory(category string) bool {
	switch category {
	case "LEASE EXPIRED", "REVIEW LEASE EXPIRED":
		return true
	default:
		return false
	}
}

func shouldClearResolvedBanner(alertBanner *ActivityEntry, activeKeys map[string]bool) bool {
	if alertBanner == nil || alertBanner.AlertKey == "" || activeKeys == nil {
		return false
	}
	return !activeKeys[alertBanner.AlertKey]
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
