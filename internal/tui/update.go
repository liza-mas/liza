package tui

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/models"
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
		m.rolePairNames = msg.RolePairs
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
		if m.inputMode == InputModeForm && m.huhForm != nil {
			return m.handleFormUpdate(msg)
		}
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

	case key.Matches(msg, m.keys.AddTask):
		form, data := m.buildAddTaskForm()
		m.huhForm = form
		m.formData = data
		m.inputMode = InputModeForm
		return m, m.huhForm.Init()

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
func (m Model) handleInlineKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		m.inputMode = InputModeNormal
		m.inlineAction = InlineActionNone
		m.spawnRole = ""
		m.terminateTarget = ""
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
		return m, spawnAgentCmd(m.projectRoot, value, m.resolvedDefaultCLI())
	case InlineActionSpawnWith:
		if value == "" {
			return m, nil
		}
		if len(m.roleCompletions) > 0 && !slices.Contains(m.roleCompletions, value) {
			return m, func() tea.Msg {
				return CmdResultMsg{Success: false, Message: fmt.Sprintf("unknown role %q", value)}
			}
		}
		// Phase 2: ask for CLI
		m.spawnRole = value
		m.inputMode = InputModeInline
		m.inlineAction = InlineActionSpawnCLI
		m.inlineLabel = fmt.Sprintf("CLI (%s): ", m.resolvedDefaultCLI())
		m.textInput.Reset()
		m.textInput.Focus()
		m.completionIdx = 0
		m.completionPrefix = ""
		return m, nil
	case InlineActionSpawnCLI:
		cli := value
		if cli == "" {
			cli = m.resolvedDefaultCLI()
		}
		if !slices.Contains(agent.ValidCLIs(), cli) {
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
		candidates = agent.ValidCLIs()
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

// addTaskFormData holds the bound values for the Huh add-task form fields.
type addTaskFormData struct {
	ID          string
	Type        string
	RolePair    string
	Description string
	SpecRef     string
	DoneWhen    string
	Scope       string
	DependsOn   []string
	Priority    int
}

// kebabCaseRe validates kebab-case identifiers.
var kebabCaseRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// validateKebabCase returns an error if s is not valid kebab-case.
func validateKebabCase(s string) error {
	if !kebabCaseRe.MatchString(s) {
		return fmt.Errorf("must be kebab-case (e.g. my-task-id)")
	}
	return nil
}

// validateRequired returns an error if s is empty.
func validateRequired(s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("required")
	}
	return nil
}

// buildAddTaskForm creates a Huh form for adding a task.
// All fields required by ops.AddTask are collected here with inline validation
// so errors are surfaced before the form exits.
func (m Model) buildAddTaskForm() (*huh.Form, *addTaskFormData) {
	data := &addTaskFormData{
		Type:     string(models.TaskTypeCoding),
		Priority: 1,
	}

	// Collect existing task IDs for duplicate check and depends_on
	existingIDs := make(map[string]bool)
	var taskIDs []string
	if m.state != nil {
		for _, t := range m.state.Tasks {
			existingIDs[t.ID] = true
			taskIDs = append(taskIDs, t.ID)
		}
		sort.Strings(taskIDs)
	}

	depOptions := make([]huh.Option[string], len(taskIDs))
	for i, id := range taskIDs {
		depOptions[i] = huh.NewOption(id, id)
	}

	// Role pair options from pipeline config
	rpOptions := []huh.Option[string]{huh.NewOption("(none loaded)", "")}
	if len(m.rolePairNames) > 0 {
		rpOptions = make([]huh.Option[string], len(m.rolePairNames))
		for i, name := range m.rolePairNames {
			rpOptions[i] = huh.NewOption(name, name)
		}
	}

	// Task type options
	typeNames := models.ValidTaskTypeNames()
	typeOptions := make([]huh.Option[string], len(typeNames))
	for i, name := range typeNames {
		typeOptions[i] = huh.NewOption(name, name)
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("ID").Value(&data.ID).
				Validate(func(s string) error {
					if err := validateKebabCase(s); err != nil {
						return err
					}
					if existingIDs[s] {
						return fmt.Errorf("task %q already exists", s)
					}
					return nil
				}),
			huh.NewInput().Title("Description").Value(&data.Description).
				Validate(validateRequired),
			huh.NewSelect[string]().Title("Type").
				Options(typeOptions...).Value(&data.Type),
			huh.NewSelect[string]().Title("Role pair").
				Options(rpOptions...).Value(&data.RolePair),
			huh.NewInput().Title("Spec ref").Value(&data.SpecRef).
				Validate(validateRequired),
			huh.NewInput().Title("Done when").Value(&data.DoneWhen).
				Validate(validateRequired),
			huh.NewInput().Title("Scope").Value(&data.Scope).
				Validate(validateRequired),
			huh.NewMultiSelect[string]().Title("Depends on").
				Options(depOptions...).Value(&data.DependsOn),
			huh.NewSelect[int]().Title("Priority").
				Options(
					huh.NewOption("1 (default)", 1),
					huh.NewOption("2", 2),
					huh.NewOption("3", 3),
					huh.NewOption("4", 4),
					huh.NewOption("5", 5),
				).Value(&data.Priority),
		),
	)

	return form, data
}

// extractFormData reads the Huh form's bound values and returns a TaskInput.
func (m Model) extractFormData() *commands.TaskInput {
	if m.formData == nil {
		return nil
	}
	return &commands.TaskInput{
		ID:          m.formData.ID,
		Type:        m.formData.Type,
		RolePair:    m.formData.RolePair,
		Description: m.formData.Description,
		SpecRef:     m.formData.SpecRef,
		DoneWhen:    m.formData.DoneWhen,
		Scope:       m.formData.Scope,
		DependsOn:   m.formData.DependsOn,
		Priority:    m.formData.Priority,
	}
}

// handleFormKey handles key events in form mode.
// Intercepts Esc for cancellation, then delegates to handleFormUpdate.
func (m Model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Esc cancels the form
	if msg.String() == "esc" {
		m.inputMode = InputModeNormal
		m.huhForm = nil
		m.formData = nil
		return m, nil
	}

	return m.handleFormUpdate(msg)
}

// handleFormUpdate forwards any tea.Msg to the Huh form and checks for
// completion/cancellation. Called from handleFormKey for key events and
// from Update's default case for Huh internal messages (focus, blink, etc.).
func (m Model) handleFormUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.huhForm.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.huhForm = f
	}

	// Check if form completed (submitted)
	if m.huhForm.State == huh.StateCompleted {
		m.inputMode = InputModeNormal
		input := m.extractFormData()
		m.huhForm = nil
		m.formData = nil
		return m, addTaskCmd(m.projectRoot, input)
	}

	// Check if form was aborted
	if m.huhForm.State == huh.StateAborted {
		m.inputMode = InputModeNormal
		m.huhForm = nil
		m.formData = nil
		return m, nil
	}

	return m, cmd
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
