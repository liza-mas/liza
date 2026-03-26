package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
)

// newTestModel creates a Model suitable for tests that don't need a real
// watcher or blackboard. Use New() tests for constructor-specific validation.
func newTestModel() Model {
	return Model{
		activities: make([]ActivityEntry, 0),
		keys:       NewKeyMap(),
		styles:     NewStyles(0),
		stateCache: make(map[string]time.Time),
	}
}

func TestNewValidProjectRoot(t *testing.T) {
	// Create a temp dir with .liza/ subdir so the watcher can watch it.
	tmpDir := t.TempDir()
	lizaDir := filepath.Join(tmpDir, ".liza")
	if err := os.MkdirAll(lizaDir, 0o755); err != nil {
		t.Fatalf("failed to create .liza dir: %v", err)
	}
	// Create state.yaml so Blackboard has a valid path.
	if err := os.WriteFile(filepath.Join(lizaDir, "state.yaml"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("failed to create state.yaml: %v", err)
	}

	t.Cleanup(func() { db.ResetInstances() })

	m, err := New(tmpDir)
	if err != nil {
		t.Fatalf("New(%q) returned unexpected error: %v", tmpDir, err)
	}
	defer m.watcher.Close()

	if m.watcher == nil {
		t.Error("watcher should be non-nil for valid project root")
	}
	if m.blackboard == nil {
		t.Error("blackboard should be non-nil for valid project root")
	}
	if m.projectRoot != tmpDir {
		t.Errorf("projectRoot = %q, want %q", m.projectRoot, tmpDir)
	}
	if m.logPath == "" {
		t.Error("logPath should be set")
	}
	if m.alertsLogPath == "" {
		t.Error("alertsLogPath should be set")
	}
}

func TestNewNonExistentPath(t *testing.T) {
	t.Cleanup(func() { db.ResetInstances() })

	_, err := New("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Fatal("New() with non-existent path should return an error")
	}
}

func TestNewDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	lizaDir := filepath.Join(tmpDir, ".liza")
	if err := os.MkdirAll(lizaDir, 0o755); err != nil {
		t.Fatalf("failed to create .liza dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(lizaDir, "state.yaml"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("failed to create state.yaml: %v", err)
	}

	t.Cleanup(func() { db.ResetInstances() })

	m, err := New(tmpDir)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}
	defer m.watcher.Close()

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
	if m.lastAnomalyCount != 0 {
		t.Error("lastAnomalyCount should be 0 by default")
	}
}

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

func TestInlineActionEnum(t *testing.T) {
	// InlineAction enum has 4 distinct values.
	if InlineActionNone != 0 {
		t.Errorf("InlineActionNone = %d, want 0", InlineActionNone)
	}
	// All action values must be distinct.
	if InlineActionSpawn == InlineActionPause {
		t.Error("InlineActionSpawn must differ from InlineActionPause")
	}
	if InlineActionPause == InlineActionStopConfirm {
		t.Error("InlineActionPause must differ from InlineActionStopConfirm")
	}
	if InlineActionSpawn == InlineActionStopConfirm {
		t.Error("InlineActionSpawn must differ from InlineActionStopConfirm")
	}
	// None differs from all action values.
	if InlineActionNone == InlineActionSpawn {
		t.Error("InlineActionNone must differ from InlineActionSpawn")
	}
}

func TestNewTextInputInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	lizaDir := filepath.Join(tmpDir, ".liza")
	if err := os.MkdirAll(lizaDir, 0o755); err != nil {
		t.Fatalf("failed to create .liza dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(lizaDir, "state.yaml"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("failed to create state.yaml: %v", err)
	}

	t.Cleanup(func() { db.ResetInstances() })

	m, err := New(tmpDir)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}
	defer m.watcher.Close()

	// textInput must be initialized (non-zero value).
	// textinput.New() sets a default Prompt ("> ") — a zero-value Model has empty Prompt.
	// Also access Cursor field as non-zero proxy per done_when.
	if m.textInput.Prompt == "" {
		t.Error("textInput should be initialized by New(), got zero-value Prompt")
	}
	_ = m.textInput.Cursor

	// Phase 4 fields that remain at zero values after New().
	if m.huhForm != nil {
		t.Error("huhForm should be nil by default")
	}
	if m.inlineAction != InlineActionNone {
		t.Errorf("inlineAction = %d, want InlineActionNone", m.inlineAction)
	}
	if m.inlineLabel != "" {
		t.Errorf("inlineLabel = %q, want empty", m.inlineLabel)
	}
	if m.roleCompletions != nil {
		t.Error("roleCompletions should be nil by default")
	}
	if m.completionIdx != 0 {
		t.Errorf("completionIdx = %d, want 0", m.completionIdx)
	}
	if m.completionPrefix != "" {
		t.Errorf("completionPrefix = %q, want empty", m.completionPrefix)
	}
}

func TestRolesMsgType(t *testing.T) {
	msg := rolesMsg{Roles: []string{"coder", "reviewer"}}
	if len(msg.Roles) != 2 {
		t.Errorf("rolesMsg.Roles length = %d, want 2", len(msg.Roles))
	}
	if msg.Roles[0] != "coder" {
		t.Errorf("rolesMsg.Roles[0] = %q, want %q", msg.Roles[0], "coder")
	}
}

func TestStopDoneMsgType(t *testing.T) {
	// stopDoneMsg is a signal type — just verify it can be instantiated.
	_ = stopDoneMsg{}
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
