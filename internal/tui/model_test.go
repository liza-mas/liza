package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestColumnTierForWidth(t *testing.T) {
	tests := []struct {
		width int
		want  ColumnTier
	}{
		{0, ColumnTierMinimal},
		{79, ColumnTierMinimal},
		{80, ColumnTierStandard},
		{119, ColumnTierStandard},
		{120, ColumnTierWide},
		{159, ColumnTierWide},
		{160, ColumnTierFull},
		{200, ColumnTierFull},
	}

	for _, tt := range tests {
		got := ColumnTierForWidth(tt.width)
		if got != tt.want {
			t.Errorf("ColumnTierForWidth(%d) = %d, want %d", tt.width, got, tt.want)
		}
	}
}

func TestNewReturnsTEAModel(t *testing.T) {
	m := New("/tmp/test-project")

	// Verify it satisfies tea.Model interface at compile time and runtime.
	var _ tea.Model = m

	// Verify Init, Update, View are callable.
	cmd := m.Init()
	if cmd != nil {
		t.Error("Init() should return nil (stub)")
	}

	updated, updateCmd := m.Update(nil)
	if updateCmd != nil {
		t.Error("Update() should return nil cmd (stub)")
	}
	if _, ok := updated.(Model); !ok {
		t.Error("Update() should return a Model")
	}

	view := m.View()
	if view != "Loading..." {
		t.Errorf("View() = %q, want %q", view, "Loading...")
	}
}

func TestNewDefaults(t *testing.T) {
	m := New("/tmp/test-project")

	// Initialized fields
	if m.inputMode != InputModeNormal {
		t.Errorf("inputMode = %d, want InputModeNormal", m.inputMode)
	}
	if m.showHelp {
		t.Error("showHelp should be false by default")
	}
	if m.ready {
		t.Error("ready should be false by default")
	}
	if m.stateCache == nil {
		t.Error("stateCache should be initialized (non-nil)")
	}
	if m.activities == nil {
		t.Error("activities should be initialized (non-nil)")
	}
	if m.projectRoot != "/tmp/test-project" {
		t.Errorf("projectRoot = %q, want %q", m.projectRoot, "/tmp/test-project")
	}

	// Zero-value fields (set by Update handlers in later phases)
	if m.state != nil {
		t.Error("state should be nil by default")
	}
	if m.logPosition != 0 {
		t.Error("logPosition should be 0 by default")
	}
	if m.width != 0 {
		t.Error("width should be 0 by default")
	}
	if m.height != 0 {
		t.Error("height should be 0 by default")
	}
	if m.columnTier != ColumnTierMinimal {
		t.Errorf("columnTier = %d, want ColumnTierMinimal", m.columnTier)
	}
	if m.alertBanner != nil {
		t.Error("alertBanner should be nil by default")
	}
	if !m.alertExpiry.IsZero() {
		t.Error("alertExpiry should be zero time by default")
	}
	if m.cmdResult != nil {
		t.Error("cmdResult should be nil by default")
	}
	if !m.cmdExpiry.IsZero() {
		t.Error("cmdExpiry should be zero time by default")
	}
}

func TestInputModeEnum(t *testing.T) {
	if InputModeNormal != 0 {
		t.Errorf("InputModeNormal = %d, want 0", InputModeNormal)
	}
	if InputModeInline != 1 {
		t.Errorf("InputModeInline = %d, want 1", InputModeInline)
	}
	if InputModeForm != 2 {
		t.Errorf("InputModeForm = %d, want 2", InputModeForm)
	}
}

func TestColumnTierEnum(t *testing.T) {
	if ColumnTierMinimal != 0 {
		t.Errorf("ColumnTierMinimal = %d, want 0", ColumnTierMinimal)
	}
	if ColumnTierStandard != 1 {
		t.Errorf("ColumnTierStandard = %d, want 1", ColumnTierStandard)
	}
	if ColumnTierWide != 2 {
		t.Errorf("ColumnTierWide = %d, want 2", ColumnTierWide)
	}
	if ColumnTierFull != 3 {
		t.Errorf("ColumnTierFull = %d, want 3", ColumnTierFull)
	}
}
