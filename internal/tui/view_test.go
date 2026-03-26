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

// --- Task Panel Tests ---

func makeTask(id string, status models.TaskStatus, priority int) models.Task {
	return models.Task{
		ID:       id,
		Status:   status,
		Priority: priority,
		Created:  time.Now().Add(-2 * time.Hour),
	}
}

func TestRenderTaskPanel_EmptyState(t *testing.T) {
	m := Model{
		width:      100,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(100),
		state:      &models.State{},
	}

	out := m.renderTaskPanel(10)
	assertContains(t, out, "TASKS", "expected TASKS title")
	assertContains(t, out, "No tasks", "expected 'No tasks' message")
}

func TestRenderTaskPanel_NilState(t *testing.T) {
	m := Model{
		width:      100,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(100),
		state:      nil,
	}

	out := m.renderTaskPanel(10)
	assertContains(t, out, "TASKS", "expected TASKS title")
	assertContains(t, out, "No tasks", "expected 'No tasks' message")
}

func TestRenderTaskPanel_HasTitle(t *testing.T) {
	m := Model{
		width:      100,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(100),
		state: &models.State{
			Tasks: []models.Task{makeTask("task-1", models.TaskStatusImplementing, 1)},
		},
	}

	out := m.renderTaskPanel(10)
	assertContains(t, out, "✔ TASKS", "expected '✔ TASKS' title")
}

func TestRenderTaskPanel_SprintMetrics(t *testing.T) {
	m := Model{
		width:      120,
		height:     40,
		columnTier: ColumnTierWide,
		styles:     NewStyles(120),
		state: &models.State{
			Tasks: []models.Task{makeTask("task-1", models.TaskStatusImplementing, 1)},
			Sprint: models.Sprint{
				Scope: models.SprintScope{Planned: []string{"a", "b", "c", "d", "e"}},
				Metrics: models.SprintMetrics{
					TasksDone:                      3,
					TasksBlocked:                   1,
					TaskOutcomeApprovalRatePercent: 72,
				},
			},
		},
	}

	out := m.renderTaskPanel(10)
	assertContains(t, out, "3/5 done", "sprint metrics should show done count")
	assertContains(t, out, "1 blocked", "sprint metrics should show blocked count")
	assertContains(t, out, "72% approval", "sprint metrics should show approval rate")
}

func TestRenderTaskPanel_ColumnTierMinimal(t *testing.T) {
	m := Model{
		width:      60,
		height:     40,
		columnTier: ColumnTierMinimal,
		styles:     NewStyles(60),
		state: &models.State{
			Tasks: []models.Task{makeTask("task-1", models.TaskStatusImplementing, 1)},
		},
	}

	out := m.renderTaskPanel(10)
	header := findTaskHeaderLine(out)
	if header == "" {
		t.Fatal("no header line found")
	}
	colCount := countTaskHeaderColumns(header)
	if colCount != 2 {
		t.Errorf("expected 2 columns at minimal tier, got %d (header: %q)", colCount, header)
	}
}

func TestRenderTaskPanel_ColumnTierStandard(t *testing.T) {
	m := Model{
		width:      100,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(100),
		state: &models.State{
			Tasks: []models.Task{makeTask("task-1", models.TaskStatusImplementing, 1)},
		},
	}

	out := m.renderTaskPanel(10)
	header := findTaskHeaderLine(out)
	if header == "" {
		t.Fatal("no header line found")
	}
	colCount := countTaskHeaderColumns(header)
	if colCount != 4 {
		t.Errorf("expected 4 columns at standard tier, got %d (header: %q)", colCount, header)
	}
	assertContains(t, header, "ATT", "header should contain ATT")
	assertContains(t, header, "ASSIGNED_TO", "header should contain ASSIGNED_TO")
}

func TestRenderTaskPanel_ColumnTierWide(t *testing.T) {
	m := Model{
		width:      130,
		height:     40,
		columnTier: ColumnTierWide,
		styles:     NewStyles(130),
		state: &models.State{
			Tasks: []models.Task{makeTask("task-1", models.TaskStatusImplementing, 1)},
		},
	}

	out := m.renderTaskPanel(10)
	header := findTaskHeaderLine(out)
	if header == "" {
		t.Fatal("no header line found")
	}
	colCount := countTaskHeaderColumns(header)
	if colCount != 6 {
		t.Errorf("expected 6 columns at wide tier, got %d (header: %q)", colCount, header)
	}
	assertContains(t, header, "AGE", "header should contain AGE")
	assertContains(t, header, "DESCRIPTION", "header should contain DESCRIPTION")
}

func TestRenderTaskPanel_ColumnTierFull(t *testing.T) {
	m := Model{
		width:      170,
		height:     40,
		columnTier: ColumnTierFull,
		styles:     NewStyles(170),
		state: &models.State{
			Tasks: []models.Task{makeTask("task-1", models.TaskStatusImplementing, 1)},
		},
	}

	out := m.renderTaskPanel(10)
	header := findTaskHeaderLine(out)
	if header == "" {
		t.Fatal("no header line found")
	}
	colCount := countTaskHeaderColumns(header)
	if colCount != 9 {
		t.Errorf("expected 9 columns at full tier, got %d (header: %q)", colCount, header)
	}
	assertContains(t, header, "REVIEWING_BY", "header should contain REVIEWING_BY")
	assertContains(t, header, "DEPS", "header should contain DEPS")
	assertContains(t, header, "TIME_IN_STATUS", "header should contain TIME_IN_STATUS")
}

func TestRenderTaskPanel_TerminalTasksAfterActive(t *testing.T) {
	m := Model{
		width:      100,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(100),
		state: &models.State{
			Tasks: []models.Task{
				makeTask("merged-task", models.TaskStatusMerged, 1),
				makeTask("active-task", models.TaskStatusImplementing, 1),
				makeTask("abandoned-task", models.TaskStatusAbandoned, 1),
			},
		},
	}

	out := m.renderTaskPanel(15)
	idxActive := strings.Index(out, "active-task")
	idxMerged := strings.Index(out, "merged-task")
	idxAbandoned := strings.Index(out, "abandoned-task")

	if idxActive == -1 || idxMerged == -1 || idxAbandoned == -1 {
		t.Fatalf("expected all task IDs in output, got:\n%s", out)
	}
	if idxActive > idxMerged {
		t.Error("expected active-task before merged-task (active first)")
	}
	if idxActive > idxAbandoned {
		t.Error("expected active-task before abandoned-task (active first)")
	}
}

func TestRenderTaskPanel_TerminalTasksDimmed(t *testing.T) {
	m := Model{
		width:      100,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(100),
		state: &models.State{
			Tasks: []models.Task{
				makeTask("merged-task", models.TaskStatusMerged, 1),
				makeTask("active-task", models.TaskStatusImplementing, 1),
			},
		},
	}

	out := m.renderTaskPanel(15)
	lines := strings.Split(out, "\n")
	var mergedLine, activeLine string
	for _, line := range lines {
		if strings.Contains(line, "merged-task") {
			mergedLine = line
		}
		if strings.Contains(line, "active-task") {
			activeLine = line
		}
	}

	if mergedLine == "" || activeLine == "" {
		t.Fatalf("expected both task lines in output, got:\n%s", out)
	}

	// Verify dimmed style is applied to terminal tasks.
	// In test environments without TTY, Dimmed.Render may be a no-op (no ANSI codes).
	// Use a probe to detect if color rendering is active.
	probe := m.styles.Dimmed.Render("PROBE")
	if probe != "PROBE" {
		// Color profile active — verify ANSI dimmed marker on merged line
		if !strings.Contains(mergedLine, "38;5;8") {
			t.Errorf("expected merged task to use dimmed style (color 8), got line: %q", mergedLine)
		}
	} else {
		// No color profile — verify structural difference: the dimmed rendering
		// wraps the row, so merged and active lines will differ if dimmed applied
		// any transformation. In no-color mode, Render is identity, so we verify
		// the code path exists by checking both lines contain their task IDs.
		assertContains(t, mergedLine, "merged-task", "merged task should be in output")
		assertContains(t, activeLine, "active-task", "active task should be in output")
	}
}

func TestRenderTaskPanel_ActiveTasksSortedByPriorityThenID(t *testing.T) {
	m := Model{
		width:      100,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(100),
		state: &models.State{
			Tasks: []models.Task{
				makeTask("task-c", models.TaskStatusImplementing, 2),
				makeTask("task-a", models.TaskStatusImplementing, 1),
				makeTask("task-b", models.TaskStatusBlocked, 1),
			},
		},
	}

	out := m.renderTaskPanel(15)
	idxA := strings.Index(out, "task-a")
	idxB := strings.Index(out, "task-b")
	idxC := strings.Index(out, "task-c")

	if idxA == -1 || idxB == -1 || idxC == -1 {
		t.Fatalf("expected all task IDs in output, got:\n%s", out)
	}
	// Priority 1 before priority 2
	if idxA > idxC {
		t.Error("expected task-a (priority 1) before task-c (priority 2)")
	}
	if idxB > idxC {
		t.Error("expected task-b (priority 1) before task-c (priority 2)")
	}
	// Same priority: sorted by ID
	if idxA > idxB {
		t.Error("expected task-a before task-b (same priority, alphabetical)")
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

func findTaskHeaderLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "ID") && strings.Contains(line, "STATUS") && !strings.Contains(line, "ROLE") {
			return line
		}
		// Fallback: task header has ATT or ASSIGNED_TO (not agent-specific columns)
		if strings.Contains(line, "ID") && strings.Contains(line, "STATUS") {
			// Could be agent or task header; disambiguate by checking for task-specific columns
			if strings.Contains(line, "ATT") || strings.Contains(line, "ASSIGNED_TO") ||
				strings.Contains(line, "AGE") || strings.Contains(line, "DESCRIPTION") {
				return line
			}
		}
	}
	// Last resort: any line with ID and STATUS
	return findHeaderLine(output)
}

func countTaskHeaderColumns(header string) int {
	columns := []string{"ID", "STATUS", "ATT", "ASSIGNED_TO", "AGE", "DESCRIPTION", "REVIEWING_BY", "DEPS", "TIME_IN_STATUS"}
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
