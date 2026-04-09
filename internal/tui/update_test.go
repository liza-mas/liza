package tui

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
)

// stubWatcher implements StateWatcher for testing.
type stubWatcher struct {
	events chan struct{}
	errors chan error
}

func newStubWatcher() *stubWatcher {
	return &stubWatcher{
		events: make(chan struct{}, 1),
		errors: make(chan error, 1),
	}
}

func (w *stubWatcher) Events() <-chan struct{} { return w.events }
func (w *stubWatcher) Errors() <-chan error    { return w.errors }
func (w *stubWatcher) Close() error            { return nil }

// testModel creates a minimal Model for testing with zero-value defaults.
// Does not call New() to avoid filesystem dependencies (watcher, blackboard).
func testModel() Model {
	return Model{
		keys:       NewKeyMap(),
		styles:     NewStyles(0),
		activities: make([]ActivityEntry, 0),
		stateCache: make(map[string]time.Time),
		textInput:  textinput.New(),
	}
}

// ============================================================
// Phase 3: Data message handler tests
// ============================================================

func TestUpdateStateChangedMsgReturnsCmd(t *testing.T) {
	w := newStubWatcher()
	m := testModel()
	m.watcher = w

	result, cmd := m.Update(stateChangedMsg{})
	if cmd == nil {
		t.Fatal("stateChangedMsg handler must return a non-nil tea.Cmd")
	}
	_ = result
}

func TestUpdateStateMsgSetsReadyAndState(t *testing.T) {
	m := testModel()
	state := &models.State{
		Goal: models.Goal{Description: "test goal"},
	}

	result, _ := m.Update(StateMsg{State: state})
	updated := result.(Model)
	if !updated.ready {
		t.Error("StateMsg must set m.ready to true")
	}
	if updated.state != state {
		t.Error("StateMsg must set m.state to the provided state")
	}
}

func TestUpdateStateMsgProcessesNewAnomalies(t *testing.T) {
	ts := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)

	// Model has already processed 1 anomaly
	m := testModel()
	m.lastAnomalyCount = 1

	state := &models.State{
		Anomalies: []models.Anomaly{
			{Timestamp: ts, Reporter: "coder-1", Type: "retry_loop", Task: "task-1", Details: map[string]any{"count": 3}},
			{Timestamp: ts.Add(time.Minute), Reporter: "coder-2", Type: "trade_off", Task: "task-2", Details: map[string]any{"what": "perf"}},
			{Timestamp: ts.Add(2 * time.Minute), Reporter: "coder-3", Type: "spec_gap", Task: "task-3", Details: map[string]any{"note": "unclear"}},
		},
	}

	result, _ := m.Update(StateMsg{State: state})
	updated := result.(Model)

	// 3 anomalies total, 1 already processed → 2 new entries
	if len(updated.activities) != 2 {
		t.Fatalf("expected 2 new ActivityEntry items, got %d", len(updated.activities))
	}
	for _, entry := range updated.activities {
		if entry.Source != "anomaly" {
			t.Errorf("expected Source 'anomaly', got %q", entry.Source)
		}
	}
	if updated.lastAnomalyCount != 3 {
		t.Errorf("expected lastAnomalyCount 3, got %d", updated.lastAnomalyCount)
	}
}

func TestUpdateTickMsgReturnsCmd(t *testing.T) {
	m := testModel()

	_, cmd := m.Update(TickMsg(time.Now()))
	if cmd == nil {
		t.Fatal("TickMsg handler must return a non-nil tea.Cmd")
	}
}

func TestUpdateAlertsMsgCriticalSetsBanner(t *testing.T) {
	m := testModel()

	now := time.Now()
	alerts := alertsMsg{
		Alerts: []AlertMsg{
			{Timestamp: now, Level: "⚠️", Category: "BLOCKED", Message: "task blocked"},
			{Timestamp: now, Level: "🚨", Category: "CIRCUIT_BREAKER", Message: "escalated to WARNING"},
		},
		StateCache: map[string]time.Time{"key": now},
	}

	result, _ := m.Update(alerts)
	updated := result.(Model)

	if updated.alertBanner == nil {
		t.Fatal("alertsMsg with critical alert must set m.alertBanner")
	}
	if updated.alertBanner.Action != "CIRCUIT_BREAKER" {
		t.Errorf("expected banner Action CIRCUIT_BREAKER, got %q", updated.alertBanner.Action)
	}

	// Alert expiry should be ~10s in the future
	expectedExpiry := time.Now().Add(10 * time.Second)
	diff := updated.alertExpiry.Sub(expectedExpiry)
	if diff < -2*time.Second || diff > 2*time.Second {
		t.Errorf("alertExpiry should be ~10s in the future, got diff %v", diff)
	}
}

func TestUpdateAlertsMsgReplacesStateCache(t *testing.T) {
	m := testModel()
	m.stateCache = map[string]time.Time{"old": time.Now()}

	newCache := map[string]time.Time{"new": time.Now()}
	alerts := alertsMsg{
		Alerts:     []AlertMsg{},
		StateCache: newCache,
	}

	result, _ := m.Update(alerts)
	updated := result.(Model)

	if _, exists := updated.stateCache["old"]; exists {
		t.Error("alertsMsg must replace stateCache, not merge")
	}
	if _, exists := updated.stateCache["new"]; !exists {
		t.Error("alertsMsg must set stateCache to the returned cache")
	}
}

func TestUpdateAlertsMsgWriteErrSurfacesInActivityFeed(t *testing.T) {
	m := testModel()

	alerts := alertsMsg{
		Alerts:     []AlertMsg{},
		StateCache: map[string]time.Time{},
		WriteErr:   fmt.Errorf("write /tmp/alerts.log: no space left on device"),
	}

	result, _ := m.Update(alerts)
	updated := result.(Model)

	found := false
	for _, a := range updated.activities {
		if a.Action == "write_error" {
			found = true
			if a.Level != "⚠️" {
				t.Errorf("expected Level ⚠️, got %q", a.Level)
			}
			if a.Detail == "" {
				t.Error("expected non-empty Detail for write error")
			}
			break
		}
	}
	if !found {
		t.Error("WriteErr should surface as write_error entry in activity feed")
	}
}

func TestUpdateLogEntriesMsgUpdatesPositionAndAppends(t *testing.T) {
	m := testModel()
	m.logPosition = 100

	ts := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	task := "task-1"
	msg := LogEntriesMsg{
		Entries: []log.Entry{
			{Timestamp: ts, Agent: "coder-1", Action: "started", Task: &task, Detail: "working"},
			{Timestamp: ts.Add(time.Minute), Agent: "coder-2", Action: "claimed", Detail: "claimed it"},
			{Timestamp: ts.Add(2 * time.Minute), Agent: "coder-3", Action: "done", Detail: "finished"},
		},
		NewPosition: 500,
	}

	result, _ := m.Update(msg)
	updated := result.(Model)

	if updated.logPosition != 500 {
		t.Errorf("expected logPosition 500, got %d", updated.logPosition)
	}
	if len(updated.activities) != 3 {
		t.Fatalf("expected 3 activity entries, got %d", len(updated.activities))
	}
}

func TestUpdateWindowSizeMsgSetsColumnTier(t *testing.T) {
	m := testModel()

	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	updated := result.(Model)

	if updated.width != 120 {
		t.Errorf("expected width 120, got %d", updated.width)
	}
	if updated.height != 40 {
		t.Errorf("expected height 40, got %d", updated.height)
	}
	if updated.columnTier != ColumnTierWide {
		t.Errorf("expected ColumnTierWide for width 120, got %d", updated.columnTier)
	}
}

func TestUpdateErrMsg_RearmsWatcher(t *testing.T) {
	w := newStubWatcher()
	m := testModel()
	m.watcher = w

	result, cmd := m.Update(errMsg{err: fmt.Errorf("fsnotify: short read")})
	updated := result.(Model)

	if cmd == nil {
		t.Fatal("errMsg with non-nil watcher must return a non-nil cmd (re-arm watchStateCmd)")
	}

	// Verify activity feed contains the watcher_error entry
	found := false
	for _, a := range updated.activities {
		if a.Source == "alert" && a.Action == "watcher_error" && a.Level == "⚠️" && strings.Contains(a.Detail, "fsnotify: short read") {
			found = true
			break
		}
	}
	if !found {
		t.Error("errMsg must append ActivityEntry with Source=alert, Action=watcher_error, Level=⚠️, Detail containing the error")
	}
}

func TestUpdateErrMsg_NilWatcher(t *testing.T) {
	m := testModel()
	m.watcher = nil

	result, cmd := m.Update(errMsg{err: fmt.Errorf("some error")})
	updated := result.(Model)

	if cmd != nil {
		t.Error("errMsg with nil watcher must return nil cmd")
	}

	// Verify activity feed still contains the watcher_error entry
	found := false
	for _, a := range updated.activities {
		if a.Source == "alert" && a.Action == "watcher_error" && a.Level == "⚠️" && strings.Contains(a.Detail, "some error") {
			found = true
			break
		}
	}
	if !found {
		t.Error("errMsg with nil watcher must still append ActivityEntry with watcher_error")
	}
}

func TestUpdateWatcherClosedMsg(t *testing.T) {
	w := newStubWatcher()
	m := testModel()
	m.watcher = w

	result, cmd := m.Update(watcherClosedMsg{})
	updated := result.(Model)
	if cmd != nil {
		t.Error("watcherClosedMsg handler should return nil cmd")
	}
	if updated.watcher != nil {
		t.Error("watcherClosedMsg must set watcher to nil")
	}
}

func TestAppendActivityCapsAt200(t *testing.T) {
	ts := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)

	// Test: 199 + 1 = 200 (no drop)
	activities := make([]ActivityEntry, 199)
	for i := range activities {
		activities[i] = ActivityEntry{Timestamp: ts, Source: "log", Action: fmt.Sprintf("action-%d", i)}
	}
	newEntry := ActivityEntry{Timestamp: ts, Source: "log", Action: "action-199"}
	result := appendActivity(activities, newEntry)
	if len(result) != 200 {
		t.Errorf("expected 200 entries with 199+1, got %d", len(result))
	}

	// Test: 200 + 1 = 200 (oldest dropped)
	activities = make([]ActivityEntry, 200)
	for i := range activities {
		activities[i] = ActivityEntry{Timestamp: ts, Source: "log", Action: fmt.Sprintf("action-%d", i)}
	}
	newEntry = ActivityEntry{Timestamp: ts, Source: "log", Action: "newest"}
	result = appendActivity(activities, newEntry)
	if len(result) != 200 {
		t.Errorf("expected 200 entries with 200+1, got %d", len(result))
	}
	// Verify oldest dropped and newest present
	if result[len(result)-1].Action != "newest" {
		t.Errorf("expected last entry to be 'newest', got %q", result[len(result)-1].Action)
	}
	if result[0].Action != "action-1" {
		t.Errorf("expected oldest surviving entry to be 'action-1', got %q", result[0].Action)
	}
}

func TestFormatAnomalyDetails(t *testing.T) {
	// Nil map
	if got := formatAnomalyDetails(nil); got != "" {
		t.Errorf("expected empty string for nil map, got %q", got)
	}

	// Empty map
	if got := formatAnomalyDetails(map[string]any{}); got != "" {
		t.Errorf("expected empty string for empty map, got %q", got)
	}

	// Sorted keys
	details := map[string]any{"z_key": "last", "a_key": "first", "m_key": 42}
	got := formatAnomalyDetails(details)
	// Keys should be sorted alphabetically
	expected := "a_key=first m_key=42 z_key=last"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}

	// Verify sort order independently
	keys := []string{"z_key", "a_key", "m_key"}
	sort.Strings(keys)
	if keys[0] != "a_key" || keys[1] != "m_key" || keys[2] != "z_key" {
		t.Errorf("sort sanity check failed: %v", keys)
	}
}

// ============================================================
// Phase 4: Key dispatch and interactivity tests
// ============================================================

func TestUpdate_HelpToggle(t *testing.T) {
	m := testModel()
	if m.showHelp {
		t.Fatal("showHelp should start false")
	}

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}
	result, _ := m.Update(msg)
	m2 := result.(Model)
	if !m2.showHelp {
		t.Error("pressing ? should toggle showHelp to true")
	}

	// Toggle back
	result, _ = m2.Update(msg)
	m3 := result.(Model)
	if m3.showHelp {
		t.Error("pressing ? again should toggle showHelp to false")
	}
}

func TestUpdate_QuitReturnsTeaQuit(t *testing.T) {
	m := testModel()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	_, cmd := m.Update(msg)
	if cmd == nil {
		t.Fatal("pressing q should return a non-nil cmd")
	}
	result := cmd()
	if _, ok := result.(tea.QuitMsg); !ok {
		t.Errorf("pressing q should return tea.Quit, got %T", result)
	}
}

func TestUpdate_SpawnSetsInlineMode(t *testing.T) {
	m := testModel()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}
	result, _ := m.Update(msg)
	m2 := result.(Model)

	if m2.inputMode != InputModeInline {
		t.Errorf("inputMode = %d, want InputModeInline(%d)", m2.inputMode, InputModeInline)
	}
	if m2.inlineAction != InlineActionSpawn {
		t.Errorf("inlineAction = %d, want InlineActionSpawn(%d)", m2.inlineAction, InlineActionSpawn)
	}
	if m2.inlineLabel != "Role: " {
		t.Errorf("inlineLabel = %q, want %q", m2.inlineLabel, "Role: ")
	}
}

func TestUpdate_PauseSetsInlineMode(t *testing.T) {
	m := testModel()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}}
	result, _ := m.Update(msg)
	m2 := result.(Model)

	if m2.inputMode != InputModeInline {
		t.Errorf("inputMode = %d, want InputModeInline(%d)", m2.inputMode, InputModeInline)
	}
	if m2.inlineAction != InlineActionPause {
		t.Errorf("inlineAction = %d, want InlineActionPause(%d)", m2.inlineAction, InlineActionPause)
	}
	if m2.inlineLabel != "Reason: " {
		t.Errorf("inlineLabel = %q, want %q", m2.inlineLabel, "Reason: ")
	}
}

func TestUpdate_StopSetsInlineConfirm(t *testing.T) {
	m := testModel()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}}
	result, _ := m.Update(msg)
	m2 := result.(Model)

	if m2.inputMode != InputModeInline {
		t.Errorf("inputMode = %d, want InputModeInline(%d)", m2.inputMode, InputModeInline)
	}
	if m2.inlineAction != InlineActionStopConfirm {
		t.Errorf("inlineAction = %d, want InlineActionStopConfirm(%d)", m2.inlineAction, InlineActionStopConfirm)
	}
	if m2.inlineLabel != "Stop? (y/n): " {
		t.Errorf("inlineLabel = %q, want %q", m2.inlineLabel, "Stop? (y/n): ")
	}
}

func TestUpdate_ResumeReturnsCmd(t *testing.T) {
	m := testModel()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}
	_, cmd := m.Update(msg)
	if cmd == nil {
		t.Error("pressing r should return a non-nil Cmd")
	}
}

func TestUpdate_CheckpointReturnsCmd(t *testing.T) {
	m := testModel()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}}
	_, cmd := m.Update(msg)
	if cmd == nil {
		t.Error("pressing c should return a non-nil Cmd")
	}
}

func TestUpdate_AddTaskSetsFormMode(t *testing.T) {
	m := testModel()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	result, _ := m.Update(msg)
	m2 := result.(Model)

	if m2.inputMode != InputModeForm {
		t.Errorf("inputMode = %d, want InputModeForm(%d)", m2.inputMode, InputModeForm)
	}
}

func TestUpdate_KeypressClearsAlertBanner(t *testing.T) {
	m := testModel()
	alert := ActivityEntry{
		Timestamp: time.Now(),
		Source:    "alert",
		Level:     "🚨",
		Detail:    "test alert",
	}
	m.alertBanner = &alert

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}
	result, _ := m.Update(msg)
	m2 := result.(Model)

	if m2.alertBanner != nil {
		t.Error("any keypress should clear alertBanner to nil")
	}
}

func TestUpdate_CmdResultMsg(t *testing.T) {
	m := testModel()
	before := time.Now()
	msg := CmdResultMsg{Success: true, Message: "ok"}
	result, _ := m.Update(msg)
	m2 := result.(Model)

	if m2.cmdResult == nil {
		t.Fatal("CmdResultMsg should set cmdResult")
	}
	if m2.cmdResult.Message != "ok" {
		t.Errorf("cmdResult.Message = %q, want %q", m2.cmdResult.Message, "ok")
	}
	if !m2.cmdResult.Success {
		t.Error("cmdResult.Success should be true")
	}
	expectedExpiry := before.Add(3 * time.Second)
	if m2.cmdExpiry.Before(expectedExpiry.Add(-1*time.Second)) || m2.cmdExpiry.After(expectedExpiry.Add(1*time.Second)) {
		t.Errorf("cmdExpiry = %v, expected ~%v", m2.cmdExpiry, expectedExpiry)
	}
}

func TestUpdate_TickMsgClearsExpiredCmdResult(t *testing.T) {
	m := testModel()
	m.cmdResult = &CmdResultMsg{Success: true, Message: "done"}
	m.cmdExpiry = time.Now().Add(-1 * time.Second)

	msg := TickMsg(time.Now())
	result, _ := m.Update(msg)
	m2 := result.(Model)

	if m2.cmdResult != nil {
		t.Error("TickMsg should clear expired cmdResult to nil")
	}
}

func TestUpdate_TickMsgKeepsUnexpiredCmdResult(t *testing.T) {
	m := testModel()
	m.cmdResult = &CmdResultMsg{Success: true, Message: "done"}
	m.cmdExpiry = time.Now().Add(10 * time.Second)

	msg := TickMsg(time.Now())
	result, _ := m.Update(msg)
	m2 := result.(Model)

	if m2.cmdResult == nil {
		t.Error("TickMsg should not clear unexpired cmdResult")
	}
}

func TestUpdate_StopDoneMsgReturnsQuit(t *testing.T) {
	m := testModel()
	msg := stopDoneMsg{}
	_, cmd := m.Update(msg)
	if cmd == nil {
		t.Fatal("stopDoneMsg should return a non-nil cmd")
	}
	result := cmd()
	if _, ok := result.(tea.QuitMsg); !ok {
		t.Errorf("stopDoneMsg should return tea.Quit, got %T", result)
	}
}

func TestUpdate_RolesMsg(t *testing.T) {
	m := testModel()
	msg := rolesMsg{Roles: []string{"coder", "reviewer"}}
	result, _ := m.Update(msg)
	m2 := result.(Model)

	if len(m2.roleCompletions) != 2 {
		t.Fatalf("roleCompletions length = %d, want 2", len(m2.roleCompletions))
	}
	if m2.roleCompletions[0] != "coder" || m2.roleCompletions[1] != "reviewer" {
		t.Errorf("roleCompletions = %v, want [coder reviewer]", m2.roleCompletions)
	}
}

func TestUpdate_InlineKeyEscCancels(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionSpawn

	msg := tea.KeyMsg{Type: tea.KeyEsc}
	result, _ := m.Update(msg)
	m2 := result.(Model)

	if m2.inputMode != InputModeNormal {
		t.Errorf("Esc in inline mode should return to InputModeNormal, got %d", m2.inputMode)
	}
	if m2.inlineAction != InlineActionNone {
		t.Errorf("Esc should reset inlineAction to None, got %d", m2.inlineAction)
	}
}

func TestUpdate_FormKeyEscCancels(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeForm

	msg := tea.KeyMsg{Type: tea.KeyEsc}
	result, _ := m.Update(msg)
	m2 := result.(Model)

	if m2.inputMode != InputModeNormal {
		t.Errorf("Esc in form mode should return to InputModeNormal, got %d", m2.inputMode)
	}
	if m2.huhForm != nil {
		t.Error("Esc in form mode should clear huhForm to nil")
	}
}

// ============================================================
// Phase 4 Task 4: Inline input mode tests
// ============================================================

func TestHandleInlineKey_EscReturnsToNormal(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionSpawn
	m.textInput.Focus()

	msg := tea.KeyMsg{Type: tea.KeyEsc}
	result, cmd := m.Update(msg)
	m2 := result.(Model)

	if m2.inputMode != InputModeNormal {
		t.Errorf("inputMode = %d, want InputModeNormal(%d)", m2.inputMode, InputModeNormal)
	}
	if m2.inlineAction != InlineActionNone {
		t.Errorf("inlineAction = %d, want InlineActionNone(%d)", m2.inlineAction, InlineActionNone)
	}
	if cmd != nil {
		t.Error("Esc should return nil cmd")
	}
}

func TestHandleInlineKey_EnterSpawnWithValueReturnsCmd(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionSpawn
	m.textInput.Focus()
	m.textInput.SetValue("coder")

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	result, cmd := m.Update(msg)
	m2 := result.(Model)

	if m2.inputMode != InputModeNormal {
		t.Errorf("inputMode = %d, want InputModeNormal after Enter", m2.inputMode)
	}
	if cmd == nil {
		t.Fatal("Enter with spawn action and value 'coder' should return a non-nil tea.Cmd")
	}
}

func TestHandleInlineKey_EnterSpawnEmptyValueReturnsNilCmd(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionSpawn
	m.textInput.Focus()
	m.textInput.SetValue("")

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	_, cmd := m.Update(msg)

	if cmd != nil {
		t.Error("Enter with spawn action and empty value should return nil cmd (cancelled)")
	}
}

func TestHandleInlineKey_EnterStopConfirmYReturnsCmd(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionStopConfirm
	m.textInput.Focus()
	m.textInput.SetValue("y")

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Fatal("Enter with stop confirm and value 'y' should return a non-nil tea.Cmd")
	}
}

func TestHandleInlineKey_EnterStopConfirmNReturnsNilCmd(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionStopConfirm
	m.textInput.Focus()
	m.textInput.SetValue("n")

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	_, cmd := m.Update(msg)

	if cmd != nil {
		t.Error("Enter with stop confirm and value 'n' should return nil cmd (cancelled)")
	}
}

func TestHandleInlineKey_EnterPauseReturnsCmd(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionPause
	m.textInput.Focus()
	m.textInput.SetValue("")

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Fatal("Enter with pause action should return a non-nil tea.Cmd (even with empty reason)")
	}
}

func TestHandleInlineKey_TabCyclesCompletion(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionSpawn
	m.roleCompletions = []string{"coder", "code-reviewer", "orchestrator"}
	m.textInput.Focus()
	m.textInput.SetValue("co")

	// First Tab: should set value to "coder"
	msg := tea.KeyMsg{Type: tea.KeyTab}
	result, _ := m.Update(msg)
	m2 := result.(Model)
	if m2.textInput.Value() != "coder" {
		t.Errorf("first Tab: value = %q, want %q", m2.textInput.Value(), "coder")
	}

	// Second Tab: should set value to "code-reviewer"
	result, _ = m2.Update(msg)
	m3 := result.(Model)
	if m3.textInput.Value() != "code-reviewer" {
		t.Errorf("second Tab: value = %q, want %q", m3.textInput.Value(), "code-reviewer")
	}
}

func TestHandleInlineKey_TabEmptyPrefixCyclesAll(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionSpawn
	m.roleCompletions = []string{"coder", "code-reviewer", "orchestrator"}
	m.textInput.Focus()
	m.textInput.SetValue("")

	msg := tea.KeyMsg{Type: tea.KeyTab}
	result, _ := m.Update(msg)
	m2 := result.(Model)
	if m2.textInput.Value() != "coder" {
		t.Errorf("first Tab with empty prefix: value = %q, want %q", m2.textInput.Value(), "coder")
	}

	result, _ = m2.Update(msg)
	m3 := result.(Model)
	if m3.textInput.Value() != "code-reviewer" {
		t.Errorf("second Tab: value = %q, want %q", m3.textInput.Value(), "code-reviewer")
	}

	result, _ = m3.Update(msg)
	m4 := result.(Model)
	if m4.textInput.Value() != "orchestrator" {
		t.Errorf("third Tab: value = %q, want %q", m4.textInput.Value(), "orchestrator")
	}
}

func TestHandleInlineKey_TabWithEmptyRolesNoChange(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionSpawn
	m.roleCompletions = nil
	m.textInput.Focus()
	m.textInput.SetValue("co")

	msg := tea.KeyMsg{Type: tea.KeyTab}
	result, _ := m.Update(msg)
	m2 := result.(Model)
	if m2.textInput.Value() != "co" {
		t.Errorf("Tab with empty roles: value = %q, want %q (unchanged)", m2.textInput.Value(), "co")
	}
}

func TestHandleInlineKey_TypingAfterTabResetsCompletionIdx(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionSpawn
	m.roleCompletions = []string{"coder", "code-reviewer", "orchestrator"}
	m.textInput.Focus()
	m.textInput.SetValue("co")

	// Press Tab to trigger completion
	tabMsg := tea.KeyMsg{Type: tea.KeyTab}
	result, _ := m.Update(tabMsg)
	m2 := result.(Model)

	// Type a regular character
	charMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}
	result, _ = m2.Update(charMsg)
	m3 := result.(Model)

	if m3.completionIdx != 0 {
		t.Errorf("completionIdx = %d after typing, want 0", m3.completionIdx)
	}
}

func TestHandleInlineKey_TabNotInSpawnModeIgnored(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionPause
	m.roleCompletions = []string{"coder"}
	m.textInput.Focus()
	m.textInput.SetValue("test")

	msg := tea.KeyMsg{Type: tea.KeyTab}
	result, _ := m.Update(msg)
	m2 := result.(Model)
	// Tab in non-spawn mode should not cycle completion
	if m2.textInput.Value() == "coder" {
		t.Error("Tab in pause mode should not trigger role completion")
	}
}

// ============================================================
// Phase 4 Task 5: Huh form overlay tests
// ============================================================

func TestBuildAddTaskForm_ReturnsNonNil(t *testing.T) {
	m := testModel()
	form, data := m.buildAddTaskForm()
	if form == nil {
		t.Fatal("buildAddTaskForm must return a non-nil *huh.Form")
	}
	if data == nil {
		t.Fatal("buildAddTaskForm must return a non-nil *addTaskFormData")
	}
}

func TestBuildAddTaskForm_IncludesTaskIDsInDependsOn(t *testing.T) {
	m := testModel()
	m.state = &models.State{
		Tasks: []models.Task{
			{ID: "task-1"},
			{ID: "task-2"},
		},
	}
	form, _ := m.buildAddTaskForm()
	if form == nil {
		t.Fatal("buildAddTaskForm must return a non-nil form")
	}

	// Initialize the form so it can render
	form.Init()
	view := form.View()
	if !strings.Contains(view, "task-1") || !strings.Contains(view, "task-2") {
		t.Errorf("form view should contain task IDs 'task-1' and 'task-2' in depends-on options, got:\n%s", view)
	}
}

func TestExtractFormData_MapsToTaskInput(t *testing.T) {
	m := testModel()
	m.formData = &addTaskFormData{
		ID:          "my-task",
		Description: "test",
		SpecRef:     "specs/test.md",
		DoneWhen:    "it works",
		DependsOn:   []string{"dep-1"},
		Priority:    2,
	}
	input := m.extractFormData()
	if input == nil {
		t.Fatal("extractFormData must return a non-nil *commands.TaskInput")
	}
	if input.ID != "my-task" {
		t.Errorf("ID = %q, want %q", input.ID, "my-task")
	}
	if input.Description != "test" {
		t.Errorf("Description = %q, want %q", input.Description, "test")
	}
	if input.SpecRef != "specs/test.md" {
		t.Errorf("SpecRef = %q, want %q", input.SpecRef, "specs/test.md")
	}
	if input.DoneWhen != "it works" {
		t.Errorf("DoneWhen = %q, want %q", input.DoneWhen, "it works")
	}
	if len(input.DependsOn) != 1 || input.DependsOn[0] != "dep-1" {
		t.Errorf("DependsOn = %v, want [dep-1]", input.DependsOn)
	}
	if input.Priority != 2 {
		t.Errorf("Priority = %d, want %d", input.Priority, 2)
	}
}

func TestValidateKebabCase(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"my-task", true},
		{"task", true},
		{"my-long-task-name", true},
		{"task123", true},
		{"a1-b2", true},
		{"My Task", false},
		{"MyTask", false},
		{"my_task", false},
		{"", false},
		{"-leading", false},
		{"trailing-", false},
		{"UPPERCASE", false},
	}
	for _, tt := range tests {
		err := validateKebabCase(tt.input)
		if tt.valid && err != nil {
			t.Errorf("validateKebabCase(%q) = error %v, want nil", tt.input, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("validateKebabCase(%q) = nil, want error", tt.input)
		}
	}
}

func TestValidateRequired(t *testing.T) {
	if err := validateRequired("something"); err != nil {
		t.Errorf("validateRequired(%q) = error %v, want nil", "something", err)
	}
	if err := validateRequired(""); err == nil {
		t.Error("validateRequired(\"\") = nil, want error")
	}
}

func TestHandleFormKey_EscSetsNormalModeAndNilForm(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeForm
	form, data := m.buildAddTaskForm()
	form.Init()
	m.huhForm = form
	m.formData = data

	msg := tea.KeyMsg{Type: tea.KeyEsc}
	result, cmd := m.handleFormKey(msg)
	m2 := result.(Model)

	if m2.inputMode != InputModeNormal {
		t.Errorf("inputMode = %d, want InputModeNormal(%d)", m2.inputMode, InputModeNormal)
	}
	if m2.huhForm != nil {
		t.Error("Esc should set huhForm to nil")
	}
	if cmd != nil {
		t.Error("Esc should return nil cmd")
	}
}

func TestHandleNormalKey_AddTaskBuildsFormAndSetsMode(t *testing.T) {
	m := testModel()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	result, cmd := m.Update(msg)
	m2 := result.(Model)

	if m2.inputMode != InputModeForm {
		t.Errorf("inputMode = %d, want InputModeForm(%d)", m2.inputMode, InputModeForm)
	}
	if m2.huhForm == nil {
		t.Error("pressing 'a' should set huhForm to non-nil")
	}
	if m2.formData == nil {
		t.Error("pressing 'a' should set formData to non-nil")
	}
	if cmd == nil {
		t.Error("pressing 'a' should return huhForm.Init() cmd (non-nil)")
	}
}

func TestRenderFooter_InlineModeShowsLabel(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineLabel = "Role: "
	m.width = 120
	m.styles = NewStyles(120)

	output := m.renderFooter()
	if !strings.Contains(output, "Role: ") {
		t.Errorf("renderFooter in inline mode should contain %q, got %q", "Role: ", output)
	}
}

// ============================================================
// Terminate agent tests
// ============================================================

func TestUpdate_TerminateSetsInlineMode(t *testing.T) {
	m := testModel()
	m.state = &models.State{
		Agents: map[string]models.Agent{
			"coder-1":         {Role: "coder"},
			"code-reviewer-1": {Role: "code-reviewer"},
		},
	}
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}}
	result, _ := m.Update(msg)
	m2 := result.(Model)

	if m2.inputMode != InputModeInline {
		t.Errorf("inputMode = %d, want InputModeInline(%d)", m2.inputMode, InputModeInline)
	}
	if m2.inlineAction != InlineActionTerminate {
		t.Errorf("inlineAction = %d, want InlineActionTerminate(%d)", m2.inlineAction, InlineActionTerminate)
	}
	if m2.inlineLabel != "Agent ID: " {
		t.Errorf("inlineLabel = %q, want %q", m2.inlineLabel, "Agent ID: ")
	}
	// Agent completions should be populated and sorted
	if len(m2.agentCompletions) != 2 {
		t.Fatalf("agentCompletions length = %d, want 2", len(m2.agentCompletions))
	}
	if m2.agentCompletions[0] != "code-reviewer-1" || m2.agentCompletions[1] != "coder-1" {
		t.Errorf("agentCompletions = %v, want [code-reviewer-1 coder-1]", m2.agentCompletions)
	}
}

func TestUpdate_TerminateWithNoAgentsSetsInlineMode(t *testing.T) {
	m := testModel()
	m.state = &models.State{
		Agents: map[string]models.Agent{},
	}
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}}
	result, _ := m.Update(msg)
	m2 := result.(Model)

	if m2.inputMode != InputModeInline {
		t.Errorf("inputMode = %d, want InputModeInline(%d)", m2.inputMode, InputModeInline)
	}
	if len(m2.agentCompletions) != 0 {
		t.Errorf("agentCompletions should be empty, got %v", m2.agentCompletions)
	}
}

func TestHandleInlineKey_EnterTerminateUnknownAgentReturnsError(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionTerminate
	m.agentCompletions = []string{"coder-1", "coder-2"}
	m.textInput.Focus()
	m.textInput.SetValue("nonexistent")

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	result, cmd := m.Update(msg)
	m2 := result.(Model)

	// Should return to normal mode with an error cmd, not enter confirmation
	if m2.inlineAction == InlineActionTerminateConfirm {
		t.Error("unknown agent should not transition to confirmation")
	}
	if cmd == nil {
		t.Fatal("unknown agent should return error CmdResultMsg")
	}
	// Execute the cmd to verify it's an error
	cmdMsg := cmd()
	resultMsg, ok := cmdMsg.(CmdResultMsg)
	if !ok {
		t.Fatalf("expected CmdResultMsg, got %T", cmdMsg)
	}
	if resultMsg.Success {
		t.Error("unknown agent should produce a failure CmdResultMsg")
	}
}

func TestHandleInlineKey_EscDuringTerminateConfirmClearsTarget(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionTerminateConfirm
	m.terminateTarget = "coder-1"
	m.textInput.Focus()

	msg := tea.KeyMsg{Type: tea.KeyEscape}
	result, _ := m.Update(msg)
	m2 := result.(Model)

	if m2.terminateTarget != "" {
		t.Errorf("terminateTarget should be cleared on Esc, got %q", m2.terminateTarget)
	}
	if m2.inputMode != InputModeNormal {
		t.Errorf("inputMode should be Normal after Esc, got %d", m2.inputMode)
	}
}

func TestHandleInlineKey_EnterTerminateTransitionsToConfirm(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionTerminate
	m.textInput.Focus()
	m.textInput.SetValue("coder-1")

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	result, cmd := m.Update(msg)
	m2 := result.(Model)

	// Should transition to confirmation phase, not execute yet
	if m2.inlineAction != InlineActionTerminateConfirm {
		t.Errorf("inlineAction = %d, want InlineActionTerminateConfirm(%d)", m2.inlineAction, InlineActionTerminateConfirm)
	}
	if m2.terminateTarget != "coder-1" {
		t.Errorf("terminateTarget = %q, want %q", m2.terminateTarget, "coder-1")
	}
	if !strings.Contains(m2.inlineLabel, "coder-1") {
		t.Errorf("inlineLabel = %q, should contain agent ID", m2.inlineLabel)
	}
	if cmd != nil {
		t.Error("should not return a cmd during confirmation phase")
	}
}

func TestHandleInlineKey_EnterTerminateEmptyValueReturnsNilCmd(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionTerminate
	m.textInput.Focus()
	m.textInput.SetValue("")

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	_, cmd := m.Update(msg)

	if cmd != nil {
		t.Error("Enter with terminate action and empty value should return nil cmd")
	}
}

func TestHandleInlineKey_TerminateConfirmYReturnsCmd(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionTerminateConfirm
	m.terminateTarget = "coder-1"
	m.textInput.Focus()
	m.textInput.SetValue("y")

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	result, cmd := m.Update(msg)
	m2 := result.(Model)

	if cmd == nil {
		t.Fatal("Enter with terminate confirm and value 'y' should return a non-nil tea.Cmd")
	}
	if m2.terminateTarget != "" {
		t.Errorf("terminateTarget should be cleared after confirmation, got %q", m2.terminateTarget)
	}
}

func TestHandleInlineKey_TerminateConfirmNReturnsNilCmd(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionTerminateConfirm
	m.terminateTarget = "coder-1"
	m.textInput.Focus()
	m.textInput.SetValue("n")

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	result, cmd := m.Update(msg)
	m2 := result.(Model)

	if cmd != nil {
		t.Error("Enter with terminate confirm and value 'n' should return nil cmd")
	}
	if m2.terminateTarget != "" {
		t.Errorf("terminateTarget should be cleared after rejection, got %q", m2.terminateTarget)
	}
}

func TestHandleInlineKey_TabCyclesAgentCompletion(t *testing.T) {
	m := testModel()
	m.inputMode = InputModeInline
	m.inlineAction = InlineActionTerminate
	m.agentCompletions = []string{"code-reviewer-1", "coder-1", "orchestrator-1"}
	m.textInput.Focus()
	m.textInput.SetValue("coder")

	// First Tab: should complete to "coder-1"
	msg := tea.KeyMsg{Type: tea.KeyTab}
	result, _ := m.Update(msg)
	m2 := result.(Model)

	if m2.textInput.Value() != "coder-1" {
		t.Errorf("first Tab got %q, want %q", m2.textInput.Value(), "coder-1")
	}
}

// ============================================================
// resolvedDefaultCLI tests
// ============================================================

func TestResolvedDefaultCLI_NilState(t *testing.T) {
	t.Setenv("LIZA_DEFAULT_CLI", "")
	m := testModel()
	// m.state is nil
	got := m.resolvedDefaultCLI()
	if got != agent.DefaultCLI {
		t.Errorf("resolvedDefaultCLI() with nil state = %q, want %q", got, agent.DefaultCLI)
	}
}

func TestResolvedDefaultCLI_NilStateWithEnv(t *testing.T) {
	t.Setenv("LIZA_DEFAULT_CLI", "codex")
	m := testModel()
	// m.state is nil but env var set
	got := m.resolvedDefaultCLI()
	if got != "codex" {
		t.Errorf("resolvedDefaultCLI() with nil state + env = %q, want %q", got, "codex")
	}
}

func TestResolvedDefaultCLI_EmptyConfig(t *testing.T) {
	t.Setenv("LIZA_DEFAULT_CLI", "")
	m := testModel()
	m.state = &models.State{}
	got := m.resolvedDefaultCLI()
	if got != agent.DefaultCLI {
		t.Errorf("resolvedDefaultCLI() with empty config = %q, want %q", got, agent.DefaultCLI)
	}
}

func TestResolvedDefaultCLI_ConfigSet(t *testing.T) {
	t.Setenv("LIZA_DEFAULT_CLI", "")
	m := testModel()
	m.state = &models.State{
		Config: models.Config{DefaultCLI: "codex"},
	}
	got := m.resolvedDefaultCLI()
	if got != "codex" {
		t.Errorf("resolvedDefaultCLI() with config codex = %q, want %q", got, "codex")
	}
}
