package tui

import (
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

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
