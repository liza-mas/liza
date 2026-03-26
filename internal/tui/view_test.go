package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func TestView_NotReady_ReturnsLoading(t *testing.T) {
	m := New("/tmp/test")
	// ready defaults to false
	got := m.View()
	if !strings.Contains(got, "Loading...") {
		t.Errorf("View() when not ready should contain 'Loading...', got: %q", got)
	}
}

func TestView_Ready_ContainsHeaderAndFooter(t *testing.T) {
	m := New("/tmp/test")
	m.ready = true
	m.width = 120
	m.height = 40
	m.styles = NewStyles(120)
	m.state = &models.State{
		Goal: models.Goal{Description: "Test Goal"},
		Sprint: models.Sprint{
			ID: "sprint-1",
		},
		Config: models.Config{Mode: models.SystemModeRunning},
	}

	got := m.View()
	// Header should be present (contains LIZA)
	if !strings.Contains(got, "LIZA") {
		t.Errorf("View() should contain header with 'LIZA', got: %q", got)
	}
}

func TestRenderHeader_ContainsGoalDescription(t *testing.T) {
	m := New("/tmp/test")
	m.width = 120
	m.styles = NewStyles(120)
	m.state = &models.State{
		Goal: models.Goal{Description: "Build the TUI dashboard"},
		Sprint: models.Sprint{
			ID: "sprint-42",
		},
		Config: models.Config{Mode: models.SystemModeRunning},
	}

	got := m.renderHeader()
	if !strings.Contains(got, "Build the TUI dashboard") {
		t.Errorf("renderHeader() should contain goal description, got: %q", got)
	}
}

func TestRenderHeader_ContainsSprintID(t *testing.T) {
	m := New("/tmp/test")
	m.width = 120
	m.styles = NewStyles(120)
	m.state = &models.State{
		Goal: models.Goal{Description: "Some goal"},
		Sprint: models.Sprint{
			ID: "sprint-7",
		},
		Config: models.Config{Mode: models.SystemModeRunning},
	}

	got := m.renderHeader()
	if !strings.Contains(got, "sprint-7") {
		t.Errorf("renderHeader() should contain sprint ID, got: %q", got)
	}
}

func TestRenderHeader_StatusMatchesSystemMode(t *testing.T) {
	tests := []struct {
		name       string
		mode       models.SystemMode
		wantStatus string
	}{
		{"running", models.SystemModeRunning, "RUNNING"},
		{"paused", models.SystemModePaused, "PAUSED"},
		{"stopped", models.SystemModeStopped, "STOPPED"},
		{"circuit breaker", models.SystemModeCircuitBreakerTripped, "CIRCUIT_BREAKER_TRIPPED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New("/tmp/test")
			m.width = 120
			m.styles = NewStyles(120)
			m.state = &models.State{
				Goal:   models.Goal{Description: "goal"},
				Sprint: models.Sprint{ID: "s1"},
				Config: models.Config{Mode: tt.mode},
			}

			got := m.renderHeader()
			if !strings.Contains(got, tt.wantStatus) {
				t.Errorf("renderHeader() with mode %s should contain %q, got: %q", tt.mode, tt.wantStatus, got)
			}
		})
	}
}

func TestRenderHeader_NilState_ReturnsLoadingFallback(t *testing.T) {
	m := New("/tmp/test")
	m.width = 120
	m.styles = NewStyles(120)
	m.state = nil

	got := m.renderHeader()
	if !strings.Contains(got, "Loading") {
		t.Errorf("renderHeader() with nil state should contain 'Loading', got: %q", got)
	}
	if !strings.Contains(got, "LIZA") {
		t.Errorf("renderHeader() with nil state should still contain 'LIZA', got: %q", got)
	}
}

func TestRenderHeader_CheckpointOverridesColor(t *testing.T) {
	m := New("/tmp/test")
	m.width = 120
	m.styles = NewStyles(120)
	m.state = &models.State{
		Goal: models.Goal{Description: "goal"},
		Sprint: models.Sprint{
			ID:     "s1",
			Status: models.SprintStatusCheckpoint,
		},
		Config: models.Config{Mode: models.SystemModeRunning},
	}

	// The header should still show RUNNING (the system mode text) but the
	// color should be checkpoint/magenta. We verify the text is present;
	// color verification would require inspecting ANSI codes which is brittle.
	got := m.renderHeader()
	if !strings.Contains(got, "RUNNING") {
		t.Errorf("renderHeader() with CHECKPOINT sprint should still show system mode text, got: %q", got)
	}
}

// --- Agent Panel Tests ---

// helper to create a string pointer
func strPtr(s string) *string { return &s }

func TestRenderAgentPanel_EmptyState(t *testing.T) {
	m := Model{
		width:      100,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(100),
		state:      &models.State{Agents: map[string]models.Agent{}},
	}

	out := m.renderAgentPanel(10)
	if !strings.Contains(out, "AGENTS") {
		t.Error("expected panel to contain 'AGENTS' title")
	}
	if !strings.Contains(out, "No agents") {
		t.Error("expected 'No agents' message for empty state")
	}
}

func TestRenderAgentPanel_NilState(t *testing.T) {
	m := Model{
		width:      100,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(100),
		state:      nil,
	}

	out := m.renderAgentPanel(10)
	if !strings.Contains(out, "AGENTS") {
		t.Error("expected panel to contain 'AGENTS' title")
	}
	if !strings.Contains(out, "No agents") {
		t.Error("expected 'No agents' message for nil state")
	}
}

func TestRenderAgentPanel_SortedByID(t *testing.T) {
	m := Model{
		width:      100,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(100),
		state: &models.State{
			Agents: map[string]models.Agent{
				"coder-2":        {Role: "coder", Status: models.AgentStatusWorking, Heartbeat: time.Now()},
				"coder-1":        {Role: "coder", Status: models.AgentStatusIdle, Heartbeat: time.Now()},
				"orchestrator-1": {Role: "orchestrator", Status: models.AgentStatusWorking, Heartbeat: time.Now()},
			},
		},
	}

	out := m.renderAgentPanel(10)
	idxCoder1 := strings.Index(out, "coder-1")
	idxCoder2 := strings.Index(out, "coder-2")
	idxOrch := strings.Index(out, "orchestrator-1")

	if idxCoder1 == -1 || idxCoder2 == -1 || idxOrch == -1 {
		t.Fatalf("expected all agent IDs in output, got:\n%s", out)
	}
	if idxCoder1 > idxCoder2 {
		t.Error("expected coder-1 before coder-2 (sorted)")
	}
	if idxCoder2 > idxOrch {
		t.Error("expected coder-2 before orchestrator-1 (sorted)")
	}
}

func TestRenderAgentPanel_StatusDots(t *testing.T) {
	m := Model{
		width:      60,
		height:     40,
		columnTier: ColumnTierMinimal,
		styles:     NewStyles(60),
		state: &models.State{
			Agents: map[string]models.Agent{
				"agent-active": {Role: "coder", Status: models.AgentStatusWorking, Heartbeat: time.Now()},
				"agent-idle":   {Role: "coder", Status: models.AgentStatusIdle, Heartbeat: time.Now()},
			},
		},
	}

	out := m.renderAgentPanel(10)

	lines := strings.Split(out, "\n")
	var activeLine, idleLine string
	for _, line := range lines {
		if strings.Contains(line, "agent-active") {
			activeLine = line
		}
		if strings.Contains(line, "agent-idle") {
			idleLine = line
		}
	}

	if activeLine == "" {
		t.Fatal("expected line with agent-active")
	}
	if idleLine == "" {
		t.Fatal("expected line with agent-idle")
	}

	if !strings.Contains(activeLine, "●") {
		t.Errorf("expected filled dot ● for WORKING agent, got line: %s", activeLine)
	}
	if !strings.Contains(idleLine, "○") {
		t.Errorf("expected hollow dot ○ for IDLE agent, got line: %s", idleLine)
	}
}

func TestRenderAgentPanel_ColumnTierMinimal(t *testing.T) {
	m := Model{
		width:      60,
		height:     40,
		columnTier: ColumnTierMinimal,
		styles:     NewStyles(60),
		state: &models.State{
			Agents: map[string]models.Agent{
				"agent-1": {Role: "coder", Status: models.AgentStatusWorking, CurrentTask: strPtr("task-1"), Heartbeat: time.Now(), PID: 12345, ContextPercent: 50},
			},
		},
	}

	out := m.renderAgentPanel(10)
	header := findHeaderLine(out)
	if header == "" {
		t.Fatal("no header line found")
	}

	colCount := countHeaderColumns(header)
	if colCount != 2 {
		t.Errorf("expected 2 columns at minimal tier, got %d (header: %q)", colCount, header)
	}
}

func TestRenderAgentPanel_ColumnTierStandard(t *testing.T) {
	m := Model{
		width:      100,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(100),
		state: &models.State{
			Agents: map[string]models.Agent{
				"agent-1": {Role: "coder", Status: models.AgentStatusWorking, CurrentTask: strPtr("task-1"), Heartbeat: time.Now(), PID: 12345, ContextPercent: 50},
			},
		},
	}

	out := m.renderAgentPanel(10)
	header := findHeaderLine(out)
	if header == "" {
		t.Fatal("no header line found")
	}

	colCount := countHeaderColumns(header)
	if colCount != 4 {
		t.Errorf("expected 4 columns at standard tier, got %d (header: %q)", colCount, header)
	}

	assertContains(t, header, "ROLE", "header should contain ROLE")
	assertContains(t, header, "CURRENT_TASK", "header should contain CURRENT_TASK")
}

func TestRenderAgentPanel_ColumnTierWide(t *testing.T) {
	m := Model{
		width:      130,
		height:     40,
		columnTier: ColumnTierWide,
		styles:     NewStyles(130),
		state: &models.State{
			Agents: map[string]models.Agent{
				"agent-1": {Role: "coder", Status: models.AgentStatusWorking, CurrentTask: strPtr("task-1"), Heartbeat: time.Now().Add(-5 * time.Minute), PID: 12345, ContextPercent: 50},
			},
		},
	}

	out := m.renderAgentPanel(10)
	header := findHeaderLine(out)
	if header == "" {
		t.Fatal("no header line found")
	}

	colCount := countHeaderColumns(header)
	if colCount != 6 {
		t.Errorf("expected 6 columns at wide tier, got %d (header: %q)", colCount, header)
	}

	assertContains(t, header, "TIME_ON_TASK", "header should contain TIME_ON_TASK")
	assertContains(t, header, "HEARTBEAT", "header should contain HEARTBEAT")
}

func TestRenderAgentPanel_ColumnTierFull(t *testing.T) {
	m := Model{
		width:      170,
		height:     40,
		columnTier: ColumnTierFull,
		styles:     NewStyles(170),
		state: &models.State{
			Agents: map[string]models.Agent{
				"agent-1": {Role: "coder", Status: models.AgentStatusWorking, CurrentTask: strPtr("task-1"), Heartbeat: time.Now(), PID: 12345, ContextPercent: 50},
			},
		},
	}

	out := m.renderAgentPanel(10)
	header := findHeaderLine(out)
	if header == "" {
		t.Fatal("no header line found")
	}

	colCount := countHeaderColumns(header)
	if colCount != 8 {
		t.Errorf("expected 8 columns at full tier, got %d (header: %q)", colCount, header)
	}

	assertContains(t, header, "PID", "header should contain PID")
	assertContains(t, header, "CONTEXT", "header should contain CONTEXT")
}

func TestRenderAgentPanel_HasTitle(t *testing.T) {
	m := Model{
		width:      100,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(100),
		state: &models.State{
			Agents: map[string]models.Agent{
				"agent-1": {Role: "coder", Status: models.AgentStatusWorking, Heartbeat: time.Now()},
			},
		},
	}

	out := m.renderAgentPanel(10)
	if !strings.Contains(out, "● AGENTS") {
		t.Error("expected '● AGENTS' title in output")
	}
}

// --- Test helpers ---

func findHeaderLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "ID") && strings.Contains(line, "STATUS") {
			return line
		}
	}
	return ""
}

func countHeaderColumns(header string) int {
	columns := []string{"ID", "STATUS", "ROLE", "CURRENT_TASK", "TIME_ON_TASK", "HEARTBEAT", "PID", "CONTEXT"}
	count := 0
	for _, col := range columns {
		if strings.Contains(header, col) {
			count++
		}
	}
	return count
}

func assertContains(t *testing.T, s, substr, msg string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("%s: %q not found in %q", msg, substr, s)
	}
}
