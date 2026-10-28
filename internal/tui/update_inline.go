package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// executeInlineAction executes the appropriate command based on the inline action.
func (m Model) executeInlineAction(action InlineAction, value string) (tea.Model, tea.Cmd) {
	switch action {
	case InlineActionSpawn:
		return m.executeSpawn(value)
	case InlineActionSpawnWith:
		return m.executeSpawnWith(value)
	case InlineActionSpawnCLI:
		return m.executeSpawnCLI(value)
	case InlineActionPause:
		return m, pauseSystemCmd(m.projectRoot, value)
	case InlineActionTerminate:
		return m.executeTerminate(value)
	case InlineActionTerminateConfirm:
		return m.executeTerminateConfirm(value)
	case InlineActionStopConfirm:
		return m.executeStopConfirm(value)
	default:
		return m, nil
	}
}

// failureCmd reports a failed command in the footer.
func failureCmd(format string, args ...any) tea.Cmd {
	message := fmt.Sprintf(format, args...)
	return func() tea.Msg {
		return CmdResultMsg{Success: false, Message: message}
	}
}

// unknownRoleFailure rejects a role missing from the loaded role names; nil
// when the role is known or the names are not loaded.
func (m Model) unknownRoleFailure(role string) tea.Cmd {
	if len(m.roleCompletions) > 0 && !slices.Contains(m.roleCompletions, role) {
		return failureCmd("unknown role %q", role)
	}
	return nil
}

func roleNotLoadedFailure(role string) tea.Cmd {
	return failureCmd("role type for %q is not loaded yet", role)
}

// executeSpawn spawns the named role with its default CLI.
func (m Model) executeSpawn(role string) (tea.Model, tea.Cmd) {
	if role == "" {
		return m, nil
	}
	if failure := m.unknownRoleFailure(role); failure != nil {
		return m, failure
	}
	cli, fromConfig, ok := m.roleLaunchDefault(role)
	if !ok {
		return m, roleNotLoadedFailure(role)
	}
	if fromConfig {
		return m, spawnConfiguredAgentCmd(m.projectRoot, role, cli)
	}
	return m, spawnAgentCmd(m.projectRoot, role, cli)
}

// executeSpawnWith takes the role of the S flow and moves on to its CLI prompt.
func (m Model) executeSpawnWith(role string) (tea.Model, tea.Cmd) {
	if role == "" {
		return m, nil
	}
	if failure := m.unknownRoleFailure(role); failure != nil {
		return m, failure
	}
	cli, ok := m.resolvedDefaultCLIForRole(role)
	if !ok {
		return m, roleNotLoadedFailure(role)
	}
	// Phase 2: ask for CLI
	m.spawnRole = role
	m.openInlinePrompt(InlineActionSpawnCLI, fmt.Sprintf("CLI (%s): ", cli))
	m.resetCompletion()
	return m, nil
}

// executeSpawnCLI spawns the pending role with the chosen CLI, or its default
// when none was typed.
func (m Model) executeSpawnCLI(cli string) (tea.Model, tea.Cmd) {
	role := m.spawnRole
	defaultCLI, fromConfig, ok := m.roleLaunchDefault(role)
	if cli == "" {
		if !ok {
			m.spawnRole = ""
			return m, roleNotLoadedFailure(role)
		}
		cli = defaultCLI
	}
	m.spawnRole = ""
	if !slices.Contains(m.availableCLIs(), cli) {
		return m, failureCmd("unknown CLI %q", cli)
	}
	// Keeping the models.yaml CLI keeps its entry, model included.
	if ok && fromConfig && cli == defaultCLI {
		return m, spawnConfiguredAgentCmd(m.projectRoot, role, cli)
	}
	return m, spawnAgentCmd(m.projectRoot, role, cli)
}

// executeTerminate takes the agent of the t flow and moves on to its confirmation.
func (m Model) executeTerminate(agentID string) (tea.Model, tea.Cmd) {
	if agentID == "" {
		return m, nil
	}
	if len(m.agentCompletions) > 0 && !slices.Contains(m.agentCompletions, agentID) {
		return m, failureCmd("unknown agent %q", agentID)
	}
	// Phase 2: ask for confirmation
	m.terminateTarget = agentID
	m.openInlinePrompt(InlineActionTerminateConfirm, fmt.Sprintf("Terminate %s? (y/n): ", agentID))
	return m, nil
}

func (m Model) executeTerminateConfirm(answer string) (tea.Model, tea.Cmd) {
	target := m.terminateTarget
	m.terminateTarget = ""
	if isYes(answer) {
		return m, terminateAgentCmd(m.projectRoot, target)
	}
	return m, nil
}

func (m Model) executeStopConfirm(answer string) (tea.Model, tea.Cmd) {
	if isYes(answer) {
		return m, stopSystemCmd(m.projectRoot)
	}
	return m, nil
}

// inlineConfirmYes is the answer a confirmation key press submits, and the
// prefix of a typed answer that confirms.
const inlineConfirmYes = "y"

// isYes reports whether a confirmation answer starts with y, in either case.
func isYes(answer string) bool {
	return strings.HasPrefix(strings.ToLower(answer), inlineConfirmYes)
}
