package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// Key bindings of the inline prompt, fixed regardless of the keymap.
var (
	inlineEscKey   = key.NewBinding(key.WithKeys("esc"))
	inlineEnterKey = key.NewBinding(key.WithKeys("enter"))
	inlineTabKey   = key.NewBinding(key.WithKeys("tab"))
	inlineYesKey   = key.NewBinding(key.WithKeys("y", "Y"))
	inlineNoKey    = key.NewBinding(key.WithKeys("n", "N"))
)

// onKey dismisses the alert banner and routes the key to the handler of the
// current input mode.
func (m Model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Dismiss alert banner on any keypress (spec §Alert Banner)
	if m.alertBanner != nil {
		m.alertBanner = nil
	}

	switch m.inputMode {
	case InputModeInline:
		return m.handleInlineKey(msg)
	default:
		return m.handleNormalKey(msg)
	}
}

// openInlinePrompt switches to inline input for action, with an empty, focused field.
func (m *Model) openInlinePrompt(action InlineAction, label string) {
	m.inputMode = InputModeInline
	m.inlineAction = action
	m.inlineLabel = label
	m.textInput.Reset()
	m.textInput.Focus()
}

// closeInlinePrompt returns to normal input mode.
func (m *Model) closeInlinePrompt() {
	m.inputMode = InputModeNormal
	m.inlineAction = InlineActionNone
	m.textInput.Blur()
}

// resetCompletion starts a fresh Tab-completion cycle.
func (m *Model) resetCompletion() {
	m.completionIdx = 0
	m.completionPrefix = ""
}

// handleNormalKey dispatches key events in normal mode.
func (m Model) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Spawn):
		return m.beginRolePrompt(InlineActionSpawn)

	case key.Matches(msg, m.keys.SpawnWith):
		return m.beginRolePrompt(InlineActionSpawnWith)

	case key.Matches(msg, m.keys.Terminate):
		return m.beginTerminatePrompt()

	case key.Matches(msg, m.keys.Pause):
		m.openInlinePrompt(InlineActionPause, "Reason: ")
		return m, nil

	case key.Matches(msg, m.keys.Resume):
		return m, resumeSystemCmd(m.projectRoot)

	case key.Matches(msg, m.keys.Checkpoint):
		return m, checkpointCmd(m.projectRoot)

	case key.Matches(msg, m.keys.Yolo):
		return m, toggleAutoResumeCmd(m.blackboard)

	case key.Matches(msg, m.keys.Filter):
		m.taskFilter = m.taskFilter.Next()
		return m, nil

	case key.Matches(msg, m.keys.Quit):
		m.closeWatcher()
		return m, tea.Quit

	case key.Matches(msg, m.keys.Stop):
		m.openInlinePrompt(InlineActionStopConfirm, "Stop? (y/n): ")
		return m, nil

	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
		return m, nil

	default:
		return m, nil
	}
}

// beginRolePrompt opens the role prompt of the spawn flows, loading the role
// completions when none are cached.
func (m Model) beginRolePrompt(action InlineAction) (tea.Model, tea.Cmd) {
	m.openInlinePrompt(action, "Role: ")
	m.resetCompletion()
	var cmd tea.Cmd
	if len(m.roleCompletions) == 0 {
		cmd = loadRolesCmd(m.projectRoot)
	}
	return m, cmd
}

// beginTerminatePrompt opens the agent prompt, completing over the agent IDs
// of the current state snapshot.
func (m Model) beginTerminatePrompt() (tea.Model, tea.Cmd) {
	var agentIDs []string
	if m.state != nil {
		for id := range m.state.Agents {
			agentIDs = append(agentIDs, id)
		}
		sort.Strings(agentIDs)
	}
	m.agentCompletions = agentIDs
	m.openInlinePrompt(InlineActionTerminate, "Agent ID: ")
	m.resetCompletion()
	return m, nil
}

// handleInlineKey handles key events in inline input mode.
// Delegates to textinput for character input. Handles Tab (completion),
// Enter (confirm action), and Esc (cancel) specially.
// For confirmation prompts (y/n), accepts single keypress without Enter.
func (m Model) handleInlineKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, inlineEscKey):
		m.closeInlinePrompt()
		m.spawnRole = ""
		m.terminateTarget = ""
		return m, nil

	case m.inlineAction.isConfirmation():
		return m.handleConfirmationKey(msg)

	case key.Matches(msg, inlineEnterKey):
		return m.confirmInline(m.textInput.Value())

	case key.Matches(msg, inlineTabKey):
		if m.inlineAction.hasCompletions() {
			m = m.cycleCompletion()
		}
		return m, nil

	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		m.resetCompletion()
		return m, cmd
	}
}

// handleConfirmationKey answers a y/n prompt: y confirms, n cancels, other keys
// are ignored. Enter confirms with the typed value, for backward compatibility.
func (m Model) handleConfirmationKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, inlineYesKey):
		return m.confirmInline(inlineConfirmYes)
	case key.Matches(msg, inlineNoKey):
		m.closeInlinePrompt()
		m.terminateTarget = ""
		return m, nil
	case key.Matches(msg, inlineEnterKey):
		return m.confirmInline(m.textInput.Value())
	default:
		return m, nil
	}
}

// confirmInline leaves the prompt and executes its action with value.
func (m Model) confirmInline(value string) (tea.Model, tea.Cmd) {
	action := m.inlineAction
	m.closeInlinePrompt()
	return m.executeInlineAction(action, value)
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
