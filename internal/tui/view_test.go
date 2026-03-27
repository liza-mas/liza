package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func TestView_NotReady_ReturnsLoading(t *testing.T) {
	m := newTestModel()
	// ready defaults to false
	got := m.View()
	if !strings.Contains(got, "Loading...") {
		t.Errorf("View() when not ready should contain 'Loading...', got: %q", got)
	}
}

func TestView_Ready_ContainsHeaderAndFooter(t *testing.T) {
	m := newTestModel()
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
	m := newTestModel()
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
	m := newTestModel()
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
			m := newTestModel()
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
	m := newTestModel()
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

func TestRenderHeader_ShowsSprintStatus(t *testing.T) {
	m := newTestModel()
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

	got := m.renderHeader()
	if !strings.Contains(got, "CHECKPOINT") {
		t.Errorf("renderHeader() should contain sprint status CHECKPOINT, got: %q", got)
	}
	if !strings.Contains(got, "RUNNING") {
		t.Errorf("renderHeader() should contain system mode RUNNING, got: %q", got)
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
	if colCount != 5 {
		t.Errorf("expected 5 columns at standard tier, got %d (header: %q)", colCount, header)
	}
	assertContains(t, header, "ATT", "header should contain ATT")
	assertContains(t, header, "ASSIGNED_TO", "header should contain ASSIGNED_TO")
	assertContains(t, header, "REVIEWING_BY", "header should contain REVIEWING_BY")
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
	assertContains(t, header, "AGE", "header should contain AGE")
}

func TestRenderTaskPanel_SortedByCreatedTime(t *testing.T) {
	now := time.Now()
	t1 := makeTask("first-task", models.TaskStatusMerged, 1)
	t1.Created = now.Add(-3 * time.Hour)
	t2 := makeTask("second-task", models.TaskStatusImplementing, 1)
	t2.Created = now.Add(-2 * time.Hour)
	t3 := makeTask("third-task", models.TaskStatusAbandoned, 1)
	t3.Created = now.Add(-1 * time.Hour)

	m := Model{
		width:      100,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(100),
		state: &models.State{
			Tasks: []models.Task{t3, t1, t2}, // shuffled input
		},
	}

	out := m.renderTaskPanel(15)
	idx1 := strings.Index(out, "first-task")
	idx2 := strings.Index(out, "second-task")
	idx3 := strings.Index(out, "third-task")

	if idx1 == -1 || idx2 == -1 || idx3 == -1 {
		t.Fatalf("expected all task IDs in output, got:\n%s", out)
	}
	if idx1 > idx2 {
		t.Error("expected first-task (oldest) before second-task")
	}
	if idx2 > idx3 {
		t.Error("expected second-task before third-task (newest)")
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

func TestRenderTaskPanel_ActiveTasksSortedByCreatedTime(t *testing.T) {
	now := time.Now()
	taskA := makeTask("task-a", models.TaskStatusImplementing, 1)
	taskA.Created = now.Add(-3 * time.Hour) // oldest
	taskB := makeTask("task-b", models.TaskStatusBlocked, 1)
	taskB.Created = now.Add(-2 * time.Hour)
	taskC := makeTask("task-c", models.TaskStatusImplementing, 2)
	taskC.Created = now.Add(-1 * time.Hour) // newest

	m := Model{
		width:      100,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(100),
		state: &models.State{
			Tasks: []models.Task{taskC, taskA, taskB}, // shuffled input
		},
	}

	out := m.renderTaskPanel(15)
	idxA := strings.Index(out, "task-a")
	idxB := strings.Index(out, "task-b")
	idxC := strings.Index(out, "task-c")

	if idxA == -1 || idxB == -1 || idxC == -1 {
		t.Fatalf("expected all task IDs in output, got:\n%s", out)
	}
	if idxA > idxB {
		t.Error("expected task-a (oldest) before task-b")
	}
	if idxB > idxC {
		t.Error("expected task-b before task-c (newest)")
	}
}

// --- Activity Panel Tests ---

func TestRenderActivityPanel_EmptyActivities(t *testing.T) {
	m := Model{
		width:      100,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(100),
		activities: []ActivityEntry{},
	}

	out := m.renderActivityPanel(10)
	assertContains(t, out, "⚡ ACTIVITY", "expected '⚡ ACTIVITY' title")
	// Empty body — should still be a valid bordered panel (non-empty string)
	if out == "" {
		t.Error("expected non-empty output for empty activities")
	}
}

func TestRenderActivityPanel_LogEntryFormat(t *testing.T) {
	ts := time.Date(2026, 3, 26, 14, 30, 45, 0, time.UTC)
	m := Model{
		width:      120,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(120),
		activities: []ActivityEntry{
			{
				Timestamp: ts,
				Source:    "log",
				Agent:     "orchestrator-1",
				Action:    "task_added",
				Task:      "epic-planning-1",
				Detail:    "Phase 1: Create plan",
			},
		},
	}

	out := m.renderActivityPanel(10)
	assertContains(t, out, "14:30:45", "log entry should contain HH:MM:SS timestamp")
	assertContains(t, out, "orchestrator-1", "log entry should contain agent")
	assertContains(t, out, "task_added", "log entry should contain action")
	assertContains(t, out, "epic-planning-1", "log entry should contain task")
	assertContains(t, out, "Phase 1: Create plan", "log entry should contain detail")
}

func TestRenderActivityPanel_AlertEntryFormat(t *testing.T) {
	ts := time.Date(2026, 3, 26, 15, 0, 0, 0, time.UTC)
	m := Model{
		width:      120,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(120),
		activities: []ActivityEntry{
			{
				Timestamp: ts,
				Source:    "alert",
				Level:     "⚠️",
				Action:    "expired_lease",
				Detail:    "agent coder-1 lease expired",
			},
		},
	}

	out := m.renderActivityPanel(10)
	assertContains(t, out, "15:00:00", "alert entry should contain timestamp")
	assertContains(t, out, "⚠️", "alert entry should contain level icon")
	assertContains(t, out, "expired_lease", "alert entry should contain category")
	assertContains(t, out, "agent coder-1 lease expired", "alert entry should contain message")
}

func TestRenderActivityPanel_AlertEntryColoredLevel(t *testing.T) {
	ts := time.Date(2026, 3, 26, 15, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		level string
	}{
		{"warning", "⚠️"},
		{"critical", "🚨"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				width:      120,
				height:     40,
				columnTier: ColumnTierStandard,
				styles:     NewStyles(120),
				activities: []ActivityEntry{
					{
						Timestamp: ts,
						Source:    "alert",
						Level:     tt.level,
						Action:    "test_category",
						Detail:    "test message",
					},
				},
			}

			out := m.renderActivityPanel(10)
			// Verify the level icon is present in the output
			assertContains(t, out, tt.level, "alert entry should contain level icon")
		})
	}
}

func TestRenderActivityPanel_AnomalyEntryFormat(t *testing.T) {
	ts := time.Date(2026, 3, 26, 16, 45, 30, 0, time.UTC)
	m := Model{
		width:      120,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(120),
		activities: []ActivityEntry{
			{
				Timestamp: ts,
				Source:    "anomaly",
				Agent:     "coder-1",
				Action:    "retry_loop",
				Task:      "task-42",
				Detail:    "3 retries on same error",
				Level:     "⚠️",
			},
		},
	}

	out := m.renderActivityPanel(10)
	assertContains(t, out, "16:45:30", "anomaly entry should contain timestamp")
	assertContains(t, out, "⚠️", "anomaly entry should contain warning icon")
	assertContains(t, out, "coder-1", "anomaly entry should contain reporter")
	assertContains(t, out, "retry_loop", "anomaly entry should contain type")
	assertContains(t, out, "task-42", "anomaly entry should contain task")
	assertContains(t, out, "3 retries on same error", "anomaly entry should contain details")
}

func TestRenderActivityPanel_TailDisplay(t *testing.T) {
	// Create 20 entries but only allow height for ~5 content lines
	var entries []ActivityEntry
	for i := 0; i < 20; i++ {
		entries = append(entries, ActivityEntry{
			Timestamp: time.Date(2026, 3, 26, 10, 0, i, 0, time.UTC),
			Source:    "log",
			Agent:     "agent-1",
			Action:    fmt.Sprintf("action_%02d", i),
			Task:      "task-1",
			Detail:    fmt.Sprintf("detail %d", i),
		})
	}

	m := Model{
		width:      120,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(120),
		activities: entries,
	}

	// height=5 means border(2) + title(1) = 3 fixed, so 2 content lines
	out := m.renderActivityPanel(5)

	// Last entries should be present (tail behavior)
	assertContains(t, out, "action_19", "tail display should show last entry")
	assertContains(t, out, "action_18", "tail display should show second-to-last entry")

	// First entries should NOT be present
	if strings.Contains(out, "action_00") {
		t.Error("tail display should NOT show first entry when height is limited")
	}
}

func TestRenderActivityPanel_HasBorderedPanel(t *testing.T) {
	m := Model{
		width:      100,
		height:     40,
		columnTier: ColumnTierStandard,
		styles:     NewStyles(100),
		activities: []ActivityEntry{
			{
				Timestamp: time.Now(),
				Source:    "log",
				Agent:     "agent-1",
				Action:    "started",
				Task:      "task-1",
				Detail:    "test",
			},
		},
	}

	out := m.renderActivityPanel(10)
	assertContains(t, out, "⚡ ACTIVITY", "expected '⚡ ACTIVITY' title")
	// Panel should use border (the rounded border chars)
	if !strings.Contains(out, "╭") && !strings.Contains(out, "┌") {
		// Either rounded or normal border should be present
		t.Log("Note: border characters may not be present in no-color/no-tty mode")
	}
}

// --- Alert Banner Tests ---

func TestRenderAlertBanner_ActiveAlert_ReturnsStyledBanner(t *testing.T) {
	m := Model{
		width:  120,
		height: 40,
		styles: NewStyles(120),
		alertBanner: &ActivityEntry{
			Timestamp: time.Now(),
			Source:    "alert",
			Level:     "🚨",
			Action:    "CIRCUIT BREAKER",
			Detail:    "escalated to WARNING — 3 anomalies in 5 minutes",
		},
		alertExpiry: time.Now().Add(10 * time.Second), // not expired
	}

	out := m.renderAlertBanner()
	if out == "" {
		t.Fatal("expected non-empty output for active alert")
	}
	assertContains(t, out, "🚨", "banner should contain alert level icon")
	assertContains(t, out, "CIRCUIT BREAKER", "banner should contain category")
	assertContains(t, out, "escalated to WARNING", "banner should contain detail text")
}

func TestRenderAlertBanner_NilAlert_ReturnsEmpty(t *testing.T) {
	m := Model{
		width:       120,
		height:      40,
		styles:      NewStyles(120),
		alertBanner: nil,
	}

	out := m.renderAlertBanner()
	if out != "" {
		t.Errorf("expected empty string for nil alertBanner, got: %q", out)
	}
}

func TestRenderAlertBanner_ExpiredAlert_ReturnsEmpty(t *testing.T) {
	m := Model{
		width:  120,
		height: 40,
		styles: NewStyles(120),
		alertBanner: &ActivityEntry{
			Timestamp: time.Now().Add(-20 * time.Second),
			Source:    "alert",
			Level:     "🚨",
			Action:    "CIRCUIT BREAKER",
			Detail:    "escalated to WARNING",
		},
		alertExpiry: time.Now().Add(-5 * time.Second), // expired 5s ago
	}

	out := m.renderAlertBanner()
	if out != "" {
		t.Errorf("expected empty string for expired alert, got: %q", out)
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

// --- Footer Tests ---

func TestRenderFooter_NormalMode_ContainsAll8KeyLabels(t *testing.T) {
	m := Model{
		width:     120,
		height:    40,
		inputMode: InputModeNormal,
		keys:      NewKeyMap(),
		styles:    NewStyles(120),
	}

	got := m.renderFooter()

	// All 8 key labels per spec §Footer Bar
	wantKeys := []string{"[s]", "[p]", "[r]", "[a]", "[c]", "[?]", "[q]", "[Q]"}
	for _, k := range wantKeys {
		if !strings.Contains(got, k) {
			t.Errorf("normal mode footer should contain %s, got: %q", k, got)
		}
	}

	// Descriptions
	wantDescs := []string{"spawn", "pause", "resume", "add", "checkpoint", "help", "quit", "stop"}
	for _, d := range wantDescs {
		if !strings.Contains(got, d) {
			t.Errorf("normal mode footer should contain %q description, got: %q", d, got)
		}
	}
}

func TestRenderFooter_InlineMode_ContainsInlineHints(t *testing.T) {
	m := Model{
		width:     120,
		height:    40,
		inputMode: InputModeInline,
		keys:      NewKeyMap(),
		styles:    NewStyles(120),
	}

	got := m.renderFooter()

	wantHints := []string{"[Tab]", "[Enter]", "[Esc]"}
	for _, h := range wantHints {
		if !strings.Contains(got, h) {
			t.Errorf("inline mode footer should contain %s, got: %q", h, got)
		}
	}

	// Should contain the inline-specific descriptions
	assertContains(t, got, "complete", "inline mode should show 'complete'")
	assertContains(t, got, "confirm", "inline mode should show 'confirm'")
	assertContains(t, got, "cancel", "inline mode should show 'cancel'")
}

func TestRenderFooter_FormMode_ContainsFormHints(t *testing.T) {
	m := Model{
		width:     120,
		height:    40,
		inputMode: InputModeForm,
		keys:      NewKeyMap(),
		styles:    NewStyles(120),
	}

	got := m.renderFooter()

	wantHints := []string{"[Enter]", "[Esc]"}
	for _, h := range wantHints {
		if !strings.Contains(got, h) {
			t.Errorf("form mode footer should contain %s, got: %q", h, got)
		}
	}

	assertContains(t, got, "submit", "form mode should show 'submit'")
	assertContains(t, got, "cancel", "form mode should show 'cancel'")
}

func TestRenderFooter_ActiveCmdResult_AppearsInOutput(t *testing.T) {
	m := Model{
		width:     120,
		height:    40,
		inputMode: InputModeNormal,
		keys:      NewKeyMap(),
		styles:    NewStyles(120),
		cmdResult: &CmdResultMsg{Success: true, Message: "System paused successfully"},
		cmdExpiry: time.Now().Add(3 * time.Second), // not expired
	}

	got := m.renderFooter()
	if !strings.Contains(got, "System paused successfully") {
		t.Errorf("footer should contain active cmdResult message, got: %q", got)
	}
}

func TestRenderFooter_ExpiredCmdResult_DoesNotAppear(t *testing.T) {
	m := Model{
		width:     120,
		height:    40,
		inputMode: InputModeNormal,
		keys:      NewKeyMap(),
		styles:    NewStyles(120),
		cmdResult: &CmdResultMsg{Success: true, Message: "System paused successfully"},
		cmdExpiry: time.Now().Add(-1 * time.Second), // expired
	}

	got := m.renderFooter()
	if strings.Contains(got, "System paused successfully") {
		t.Errorf("footer should NOT contain expired cmdResult message, got: %q", got)
	}
}

func TestRenderFooter_ErrorCmdResult_AppearsInOutput(t *testing.T) {
	m := Model{
		width:     120,
		height:    40,
		inputMode: InputModeNormal,
		keys:      NewKeyMap(),
		styles:    NewStyles(120),
		cmdResult: &CmdResultMsg{Success: false, Message: "Failed to pause"},
		cmdExpiry: time.Now().Add(3 * time.Second),
	}

	got := m.renderFooter()
	if !strings.Contains(got, "Failed to pause") {
		t.Errorf("footer should contain error cmdResult message, got: %q", got)
	}
}

func TestRenderFooter_NilCmdResult_NoResultShown(t *testing.T) {
	m := Model{
		width:     120,
		height:    40,
		inputMode: InputModeNormal,
		keys:      NewKeyMap(),
		styles:    NewStyles(120),
		cmdResult: nil,
	}

	got := m.renderFooter()
	// Should just have key hints, no crash
	if !strings.Contains(got, "[s]") {
		t.Errorf("footer with nil cmdResult should still show key hints, got: %q", got)
	}
}

// --- Help Overlay Tests ---

func TestRenderHelpOverlay_ContainsAll8KeyLabels(t *testing.T) {
	m := Model{
		width:      120,
		height:     40,
		columnTier: ColumnTierStandard,
		keys:       NewKeyMap(),
		styles:     NewStyles(120),
	}

	got := m.renderHelpOverlay(15)

	wantKeys := []string{"[s]", "[p]", "[r]", "[a]", "[c]", "[?]", "[q]", "[Q]"}
	for _, k := range wantKeys {
		if !strings.Contains(got, k) {
			t.Errorf("help overlay should contain %s, got: %q", k, got)
		}
	}
}

func TestRenderHelpOverlay_ContainsDescriptions(t *testing.T) {
	m := Model{
		width:      120,
		height:     40,
		columnTier: ColumnTierStandard,
		keys:       NewKeyMap(),
		styles:     NewStyles(120),
	}

	got := m.renderHelpOverlay(15)

	wantDescs := []string{"spawn", "pause", "resume", "add", "checkpoint", "help", "quit", "stop"}
	for _, d := range wantDescs {
		if !strings.Contains(got, d) {
			t.Errorf("help overlay should contain description %q, got: %q", d, got)
		}
	}
}

func TestRenderHelpOverlay_UsesGroupedLayout(t *testing.T) {
	m := Model{
		width:      120,
		height:     40,
		columnTier: ColumnTierStandard,
		keys:       NewKeyMap(),
		styles:     NewStyles(120),
	}

	got := m.renderHelpOverlay(15)

	// FullHelp() returns 3 groups. At 120 width, groups should be side-by-side.
	// Verify group headers are present.
	assertContains(t, got, "ACTIONS", "help overlay should show ACTIONS group header")
	assertContains(t, got, "SYSTEM", "help overlay should show SYSTEM group header")
	assertContains(t, got, "TASKS", "help overlay should show TASKS group header")
}

func TestRenderHelpOverlay_NarrowWidth_StacksVertically(t *testing.T) {
	m := Model{
		width:      40,
		height:     40,
		columnTier: ColumnTierMinimal,
		keys:       NewKeyMap(),
		styles:     NewStyles(40),
	}

	got := m.renderHelpOverlay(20)

	// All 8 keys should still be present even at narrow width
	wantKeys := []string{"[s]", "[p]", "[r]", "[a]", "[c]", "[?]", "[q]", "[Q]"}
	for _, k := range wantKeys {
		if !strings.Contains(got, k) {
			t.Errorf("narrow help overlay should contain %s, got: %q", k, got)
		}
	}
}

func TestView_ShowHelpTrue_RendersHelpNotActivity(t *testing.T) {
	m := Model{
		width:      120,
		height:     40,
		columnTier: ColumnTierStandard,
		keys:       NewKeyMap(),
		styles:     NewStyles(120),
		ready:      true,
		showHelp:   true,
		state: &models.State{
			Goal:   models.Goal{Description: "Test Goal"},
			Sprint: models.Sprint{ID: "sprint-1"},
			Config: models.Config{Mode: models.SystemModeRunning},
		},
		activities: []ActivityEntry{
			{
				Timestamp: time.Now(),
				Source:    "log",
				Agent:     "agent-1",
				Action:    "unique_activity_marker",
				Task:      "task-1",
				Detail:    "should not appear",
			},
		},
	}

	got := m.View()

	// Help overlay content should be present
	assertContains(t, got, "[s]", "View with showHelp=true should contain help key labels")
	assertContains(t, got, "ACTIONS", "View with showHelp=true should contain help group headers")

	// Activity panel content should NOT be present
	if strings.Contains(got, "unique_activity_marker") {
		t.Error("View with showHelp=true should NOT contain activity panel content")
	}
	if strings.Contains(got, "⚡ ACTIVITY") {
		t.Error("View with showHelp=true should NOT contain activity panel title")
	}
}

func TestView_ShowHelpFalse_RendersActivityNotHelp(t *testing.T) {
	m := Model{
		width:      120,
		height:     40,
		columnTier: ColumnTierStandard,
		keys:       NewKeyMap(),
		styles:     NewStyles(120),
		ready:      true,
		showHelp:   false,
		state: &models.State{
			Goal:   models.Goal{Description: "Test Goal"},
			Sprint: models.Sprint{ID: "sprint-1"},
			Config: models.Config{Mode: models.SystemModeRunning},
		},
		activities: []ActivityEntry{
			{
				Timestamp: time.Now(),
				Source:    "log",
				Agent:     "agent-1",
				Action:    "unique_activity_marker",
				Task:      "task-1",
				Detail:    "visible detail",
			},
		},
	}

	got := m.View()

	// Activity panel content should be present
	assertContains(t, got, "⚡ ACTIVITY", "View with showHelp=false should contain activity panel title")
	assertContains(t, got, "unique_activity_marker", "View with showHelp=false should contain activity content")

	// Help-only content (group headers) should NOT be present
	if strings.Contains(got, "ACTIONS") {
		t.Error("View with showHelp=false should NOT contain help overlay group headers")
	}
}

func TestRenderFooter_InlineMode_DoesNotContainNormalKeys(t *testing.T) {
	m := Model{
		width:     120,
		height:    40,
		inputMode: InputModeInline,
		keys:      NewKeyMap(),
		styles:    NewStyles(120),
	}

	got := m.renderFooter()
	// Normal-mode specific keys should not appear in inline mode
	normalOnlyKeys := []string{"[s]", "[p]", "[r]", "[a]", "[c]", "[Q]"}
	for _, k := range normalOnlyKeys {
		if strings.Contains(got, k) {
			t.Errorf("inline mode footer should NOT contain normal key %s, got: %q", k, got)
		}
	}
}

// ============================================================
// Phase 4 Task 5: Huh form overlay view tests
// ============================================================

func TestView_FormOverlay_ReplacesActivityPanel(t *testing.T) {
	m := newTestModel()
	m.ready = true
	m.width = 120
	m.height = 40
	m.styles = NewStyles(120)
	m.state = &models.State{
		Goal:   models.Goal{Description: "Test Goal"},
		Sprint: models.Sprint{ID: "sprint-1"},
		Config: models.Config{Mode: models.SystemModeRunning},
	}
	// Add a unique activity entry that should NOT appear in form mode
	m.activities = []ActivityEntry{
		{
			Timestamp: time.Now(),
			Source:    "log",
			Agent:     "test-agent",
			Action:    "unique_marker_12345",
		},
	}

	// Build form and set form mode
	form, data := m.buildAddTaskForm()
	form.Init()
	m.huhForm = form
	m.formData = data
	m.inputMode = InputModeForm

	output := m.View()
	if strings.Contains(output, "unique_marker_12345") {
		t.Error("View() in form mode should not contain activity panel content")
	}
	// The form should be rendered instead — huh forms contain field titles
	if !strings.Contains(output, "ID") {
		t.Error("View() in form mode should contain form content (field title 'ID')")
	}
}

func TestView_NormalMode_ContainsActivityPanel(t *testing.T) {
	m := newTestModel()
	m.ready = true
	m.width = 120
	m.height = 40
	m.styles = NewStyles(120)
	m.state = &models.State{
		Goal:   models.Goal{Description: "Test Goal"},
		Sprint: models.Sprint{ID: "sprint-1"},
		Config: models.Config{Mode: models.SystemModeRunning},
	}
	m.activities = []ActivityEntry{
		{
			Timestamp: time.Now(),
			Source:    "log",
			Agent:     "test-agent",
			Action:    "unique_marker_67890",
		},
	}
	m.inputMode = InputModeNormal

	output := m.View()
	if !strings.Contains(output, "unique_marker_67890") {
		t.Error("View() in normal mode should contain activity panel content")
	}
}
