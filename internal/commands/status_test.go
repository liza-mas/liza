package commands

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func setupPipelineRoot(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	return tmpDir
}

func TestBuildStatusData(t *testing.T) {
	now := time.Now().UTC()
	pipelineRoot := setupPipelineRoot(t)
	pr, _ := ops.LoadResolverForModels(pipelineRoot)

	tests := []struct {
		name        string
		state       *models.State
		detailed    bool
		projectRoot string
		pr          models.PipelineResolver
		validate    func(t *testing.T, data statusData)
	}{
		{
			name: "empty state with no agents",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{}
				state.Agents = make(map[string]models.Agent)
				return state
			}(),
			detailed: false,
			validate: func(t *testing.T, data statusData) {
				if len(data.Agents) != 0 {
					t.Errorf("expected 0 agents, got %d", len(data.Agents))
				}
				if data.Tasks.Total != 0 {
					t.Errorf("expected 0 total tasks, got %d", data.Tasks.Total)
				}
			},
		},
		{
			name: "state with tasks by various statuses",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusImplementing, now),
					testhelpers.BuildTaskByStatus("task-3", models.TaskStatusReadyForReview, now),
					testhelpers.BuildTaskByStatus("task-4", models.TaskStatusMerged, now),
					testhelpers.BuildTaskByStatus("task-5", models.TaskStatusRejected, now),
				}
				return state
			}(),
			detailed:    false,
			projectRoot: pipelineRoot,
			pr:          pr,
			validate: func(t *testing.T, data statusData) {
				if data.Tasks.Total != 5 {
					t.Errorf("expected 5 total tasks, got %d", data.Tasks.Total)
				}
				if data.Tasks.Active != 4 {
					t.Errorf("expected 4 active tasks, got %d", data.Tasks.Active)
				}
				if data.Tasks.Terminal != 1 {
					t.Errorf("expected 1 terminal task, got %d", data.Tasks.Terminal)
				}
				if data.Tasks.Claimable != 2 {
					t.Errorf("expected 2 claimable tasks (READY + REJECTED), got %d", data.Tasks.Claimable)
				}
				if data.Tasks.Reviewable != 1 {
					t.Errorf("expected 1 reviewable task, got %d", data.Tasks.Reviewable)
				}
			},
		},
		{
			name: "tasks blocked by dependencies",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task1 := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
				task1.DependsOn = []string{"task-0"}
				task2 := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, now)
				task2.DependsOn = []string{"task-0"}
				state.Tasks = []models.Task{task1, task2}
				return state
			}(),
			detailed: false,
			validate: func(t *testing.T, data statusData) {
				if data.Tasks.BlockedByDeps != 2 {
					t.Errorf("expected 2 tasks blocked by deps, got %d", data.Tasks.BlockedByDeps)
				}
				if data.Tasks.Claimable != 0 {
					t.Errorf("expected 0 claimable tasks (all blocked), got %d", data.Tasks.Claimable)
				}
			},
		},
		{
			name: "state with active agents",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Agents = map[string]models.Agent{
					"coder-1": {
						Role:      "coder",
						Status:    models.AgentStatusWorking,
						Heartbeat: now.Add(-30 * time.Second),
						PID:       12345,
					},
					"code-reviewer-1": {
						Role:      "code-reviewer",
						Status:    models.AgentStatusIdle,
						Heartbeat: now.Add(-10 * time.Second),
						PID:       12346,
					},
				}
				return state
			}(),
			detailed: false,
			validate: func(t *testing.T, data statusData) {
				if len(data.Agents) != 2 {
					t.Errorf("expected 2 agents, got %d", len(data.Agents))
				}
				// Check that agents are present
				foundCoder := false
				foundReviewer := false
				for _, agent := range data.Agents {
					if agent.ID == "coder-1" {
						foundCoder = true
						if agent.Role != "coder" {
							t.Errorf("expected coder role, got %s", agent.Role)
						}
						if agent.Status != string(models.AgentStatusWorking) {
							t.Errorf("expected WORKING status, got %s", agent.Status)
						}
					}
					if agent.ID == "code-reviewer-1" {
						foundReviewer = true
					}
				}
				if !foundCoder {
					t.Error("coder-1 not found in agents")
				}
				if !foundReviewer {
					t.Error("code-reviewer-1 not found in agents")
				}
			},
		},
		{
			name: "orchestrator wake trigger detected",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusBlocked, now),
				}
				return state
			}(),
			detailed: false,
			validate: func(t *testing.T, data statusData) {
				if data.OrchestratorState.Trigger != "BLOCKED_TASKS" {
					t.Errorf("expected BLOCKED_TASKS trigger, got %s", data.OrchestratorState.Trigger)
				}
				if data.OrchestratorState.TriggerCount != 2 {
					t.Errorf("expected trigger count 2, got %d", data.OrchestratorState.TriggerCount)
				}
			},
		},
		{
			name: "sprint complete trigger",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Sprint.Scope.Planned = []string{"task-1", "task-2"}
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusMerged, now),
				}
				return state
			}(),
			detailed: false,
			validate: func(t *testing.T, data statusData) {
				if data.OrchestratorState.Trigger != "SPRINT_COMPLETE" {
					t.Errorf("expected SPRINT_COMPLETE trigger, got %s", data.OrchestratorState.Trigger)
				}
				if data.OrchestratorState.TriggerCount != 2 {
					t.Errorf("expected trigger count 2, got %d", data.OrchestratorState.TriggerCount)
				}
				if data.OrchestratorState.Reason != "All 2 planned task(s) reached terminal state; sprint complete" {
					t.Errorf("unexpected reason: %s", data.OrchestratorState.Reason)
				}
			},
		},
		{
			name: "hypothesis exhaustion ignores terminal tasks",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				// Terminal task with 2+ failures should NOT trigger HYPOTHESIS_EXHAUSTED
				task1 := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
				task1.FailedBy = []string{"coder-1", "coder-2"}
				state.Tasks = []models.Task{task1}
				return state
			}(),
			detailed: false,
			validate: func(t *testing.T, data statusData) {
				if data.OrchestratorState.Trigger == "HYPOTHESIS_EXHAUSTED" {
					t.Error("terminal task with 2+ failures should not trigger HYPOTHESIS_EXHAUSTED")
				}
				if data.OrchestratorState.Trigger != "NONE" {
					t.Errorf("expected NONE trigger, got %s", data.OrchestratorState.Trigger)
				}
			},
		},
		{
			name: "work queues status",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReadyForReview, now),
				}
				return state
			}(),
			detailed:    false,
			projectRoot: pipelineRoot,
			pr:          pr,
			validate: func(t *testing.T, data statusData) {
				if data.WorkQueues.Coder.Available != 1 {
					t.Errorf("expected 1 available coder task, got %d", data.WorkQueues.Coder.Available)
				}
				if data.WorkQueues.Reviewer.Available != 1 {
					t.Errorf("expected 1 available reviewer task, got %d", data.WorkQueues.Reviewer.Available)
				}
			},
		},
		{
			name: "goal and sprint information",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Goal = models.Goal{
					ID:          "goal-1",
					Description: "Test goal",
					SpecRef:     "spec.md",
					Status:      models.GoalStatusInProgress,
					Created:     now,
				}
				state.Sprint = models.Sprint{
					ID:     "sprint-1",
					Status: models.SprintStatusInProgress,
					Timeline: models.SprintTimeline{
						Started: now,
					},
					Metrics: models.SprintMetrics{
						TasksDone: 3,
					},
				}
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusMerged, now),
					testhelpers.BuildTaskByStatus("task-3", models.TaskStatusMerged, now),
					testhelpers.BuildTaskByStatus("task-4", models.TaskStatusImplementing, now),
					testhelpers.BuildTaskByStatus("task-5", models.TaskStatusReady, now),
				}
				return state
			}(),
			detailed: false,
			validate: func(t *testing.T, data statusData) {
				if data.Goal.Description != "Test goal" {
					t.Errorf("expected goal description 'Test goal', got %s", data.Goal.Description)
				}
				if data.Goal.Status != string(models.GoalStatusInProgress) {
					t.Errorf("expected goal status IN_PROGRESS, got %s", data.Goal.Status)
				}
				if data.Sprint.ID != "sprint-1" {
					t.Errorf("expected sprint ID 'sprint-1', got %s", data.Sprint.ID)
				}
				if data.Sprint.TasksDone != 3 {
					t.Errorf("expected 3 tasks done, got %d", data.Sprint.TasksDone)
				}
				if data.Sprint.TasksTotal != 5 {
					t.Errorf("expected 5 total tasks, got %d", data.Sprint.TasksTotal)
				}
			},
		},
		{
			name: "config mode information",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Config.Mode = models.SystemModePaused
				pausedBy := "human"
				state.Config.ModeChangedBy = &pausedBy
				return state
			}(),
			detailed: false,
			validate: func(t *testing.T, data statusData) {
				if data.Config.Mode != string(models.SystemModePaused) {
					t.Errorf("expected PAUSED mode, got %s", data.Config.Mode)
				}
				if data.Config.PausedBy == nil || *data.Config.PausedBy != "human" {
					t.Error("expected PausedBy to be 'human'")
				}
			},
		},
		{
			name: "detailed mode includes anomalies",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Anomalies = []models.Anomaly{
					{
						Timestamp: now,
						Task:      "task-1",
						Reporter:  "coder-1",
						Type:      "retry_loop",
						Details:   map[string]any{"count": 3},
					},
				}
				return state
			}(),
			detailed: true,
			validate: func(t *testing.T, data statusData) {
				if data.Anomalies == nil {
					t.Error("expected anomalies to be included in detailed mode")
				} else if len(*data.Anomalies) != 1 {
					t.Errorf("expected 1 anomaly, got %d", len(*data.Anomalies))
				}
			},
		},
		{
			name: "non-detailed mode excludes anomalies",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Anomalies = []models.Anomaly{
					{
						Timestamp: now,
						Task:      "task-1",
						Reporter:  "coder-1",
						Type:      "retry_loop",
						Details:   map[string]any{"count": 3},
					},
				}
				return state
			}(),
			detailed: false,
			validate: func(t *testing.T, data statusData) {
				if data.Anomalies != nil {
					t.Error("expected anomalies to be nil in non-detailed mode")
				}
			},
		},
		{
			name: "detailed mode includes circuit breaker",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.CircuitBreaker = models.CircuitBreaker{
					Status:    "TRIGGERED",
					LastCheck: now,
					CurrentTrigger: &models.CircuitBreakerTrigger{
						Timestamp:  now,
						Pattern:    "retry_loop_detected",
						Severity:   "high",
						ReportFile: "report.md",
					},
				}
				return state
			}(),
			detailed: true,
			validate: func(t *testing.T, data statusData) {
				if data.CircuitBreaker == nil {
					t.Error("expected circuit breaker to be included in detailed mode")
				} else if data.CircuitBreaker.Status != "TRIGGERED" {
					t.Errorf("expected TRIGGERED status, got %s", data.CircuitBreaker.Status)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := BuildStatusData(tt.state, tt.detailed, tt.projectRoot, tt.pr, nil)
			tt.validate(t, data)
		})
	}
}

func TestBuildStatusData_ByStatusMap(t *testing.T) {
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("task-3", models.TaskStatusImplementing, now),
		testhelpers.BuildTaskByStatus("task-4", models.TaskStatusReadyForReview, now),
		testhelpers.BuildTaskByStatus("task-5", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("task-6", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("task-7", models.TaskStatusMerged, now),
	}

	data := BuildStatusData(state, false, "", nil, nil)

	// Check ByStatus map
	if data.Tasks.ByStatus == nil {
		t.Fatal("ByStatus map is nil")
	}

	expectedCounts := map[models.TaskStatus]int{
		models.TaskStatusReady:          2,
		models.TaskStatusImplementing:   1,
		models.TaskStatusReadyForReview: 1,
		models.TaskStatusMerged:         3,
	}

	for status, expectedCount := range expectedCounts {
		actualCount := data.Tasks.ByStatus[string(status)]
		if actualCount != expectedCount {
			t.Errorf("status %s: expected count %d, got %d", status, expectedCount, actualCount)
		}
	}
}

func TestBuildStatusData_AgentProcessStatus(t *testing.T) {
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	state.Agents = map[string]models.Agent{
		"coder-1": {
			Role:      "coder",
			Status:    models.AgentStatusWorking,
			Heartbeat: now.Add(-30 * time.Second),
			PID:       12345,
		},
	}

	data := BuildStatusData(state, false, "", nil, nil)

	if len(data.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(data.Agents))
	}

	agent := data.Agents[0]

	// PID should be populated
	if agent.PID != 12345 {
		t.Errorf("expected PID to be 12345, got %d", agent.PID)
	}

	// ProcessStatus should indicate if process is running
	// This is just checking the field is populated
	if agent.ProcessStatus == "" {
		t.Error("expected ProcessStatus to be populated")
	}

	// TimeSinceHeartbeat should be populated
	if agent.TimeSinceHeartbeat == "" {
		t.Error("expected TimeSinceHeartbeat to be populated")
	}

	// Should mention seconds since it's 30 seconds ago
	if !strings.Contains(agent.TimeSinceHeartbeat, "s") {
		t.Errorf("expected TimeSinceHeartbeat to contain time unit, got %s", agent.TimeSinceHeartbeat)
	}
}

func TestBuildStatusData_WorkQueuesReason(t *testing.T) {
	now := time.Now().UTC()
	pipelineRoot := setupPipelineRoot(t)
	pr, _ := ops.LoadResolverForModels(pipelineRoot)

	tests := []struct {
		name           string
		state          *models.State
		projectRoot    string
		pr             models.PipelineResolver
		expectCoderMsg string
	}{
		{
			name: "no claimable tasks",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
				}
				return state
			}(),
			projectRoot:    pipelineRoot,
			pr:             pr,
			expectCoderMsg: "No claimable tasks",
		},
		{
			name: "tasks available",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
				}
				return state
			}(),
			projectRoot:    pipelineRoot,
			pr:             pr,
			expectCoderMsg: "Found 1 claimable task(s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := BuildStatusData(tt.state, false, tt.projectRoot, tt.pr, nil)

			if !strings.Contains(data.WorkQueues.Coder.Reason, tt.expectCoderMsg) {
				t.Errorf("expected coder reason to contain %q, got %q", tt.expectCoderMsg, data.WorkQueues.Coder.Reason)
			}
		})
	}
}

func TestFormatStatusDashboard(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name           string
		data           statusData
		expectSections []string
		notExpect      []string
	}{
		{
			name: "basic dashboard with all sections",
			data: statusData{
				Goal: goalStatus{
					Description: "Test goal",
					Status:      "IN_PROGRESS",
					SpecRef:     "spec.md",
				},
				Sprint: sprintStatus{
					ID:         "sprint-1",
					Status:     "IN_PROGRESS",
					StartTime:  now.Format(time.RFC3339),
					TasksDone:  5,
					TasksTotal: 10,
				},
				Config: configStatus{
					Mode: "RUNNING",
				},
				Tasks: taskStatus{
					Total:    10,
					Active:   7,
					Terminal: 3,
					ByStatus: map[string]int{
						"DRAFT_CODE":            2,
						"IMPLEMENTING_CODE":     3,
						"CODE_READY_FOR_REVIEW": 2,
						"MERGED":                3,
					},
					Claimable:     2,
					Reviewable:    2,
					BlockedByDeps: 0,
				},
				Agents: []agentStatus{
					{
						ID:                 "coder-1",
						Role:               "coder",
						Status:             "WORKING",
						PID:                12345,
						CurrentTask:        "task-1",
						TimeSinceHeartbeat: "30s ago",
						ProcessStatus:      "running",
					},
				},
				OrchestratorState: orchestratorStatus{
					Trigger:      "NONE",
					TriggerCount: 0,
					Reason:       "No triggers; orchestrator is idle",
				},
				WorkQueues: workQueuesStatus{
					Coder: queueStatus{
						Available: 2,
						Reason:    "Found 2 claimable task(s)",
					},
					Reviewer: queueStatus{
						Available: 2,
						Reason:    "Found 2 reviewable task(s): 2 unassigned",
					},
				},
			},
			expectSections: []string{
				"=== GOAL ===",
				"Description: Test goal",
				"Status: IN_PROGRESS",
				"Spec: spec.md",
				"=== SPRINT ===",
				"ID: sprint-1",
				"Progress: 5/10 tasks complete",
				"=== SYSTEM ===",
				"Mode: RUNNING",
				"=== TASKS ===",
				"Total: 10 (7 active, 3 terminal)",
				"By Status:",
				"IMPLEMENTING_CODE: 3",
				"Claimable: 2 tasks",
				"Reviewable: 2 tasks",
				"=== AGENTS ===",
				"coder-1",
				"WORKING",
				"12345",
				"=== ORCHESTRATOR ===",
				"Wake Trigger: NONE",
				"Explanation: No triggers; orchestrator is idle",
				"=== WORK QUEUES ===",
				"Coder: 2 available",
				"Reviewer: 2 available",
			},
			notExpect: []string{
				"=== ANOMALIES ===",
				"=== CIRCUIT BREAKER ===",
			},
		},
		{
			name: "paused system with reason",
			data: statusData{
				Goal: goalStatus{
					Description: "Test",
					Status:      "IN_PROGRESS",
					SpecRef:     "spec.md",
				},
				Sprint: sprintStatus{
					ID:         "sprint-1",
					Status:     "IN_PROGRESS",
					StartTime:  now.Format(time.RFC3339),
					TasksDone:  0,
					TasksTotal: 0,
				},
				Config: configStatus{
					Mode:     "PAUSED",
					PausedBy: stringPtr("human"),
				},
				Tasks: taskStatus{
					Total:    0,
					ByStatus: map[string]int{},
				},
				Agents:            []agentStatus{},
				OrchestratorState: orchestratorStatus{Trigger: "NONE", Reason: "No triggers"},
				WorkQueues: workQueuesStatus{
					Coder:    queueStatus{Available: 0, Reason: "No claimable tasks"},
					Reviewer: queueStatus{Available: 0, Reason: "No reviewable tasks"},
				},
			},
			expectSections: []string{
				"Mode: PAUSED",
				"Paused By: human",
			},
		},
		{
			name: "no agents",
			data: statusData{
				Goal: goalStatus{
					Description: "Test",
					Status:      "IN_PROGRESS",
					SpecRef:     "spec.md",
				},
				Sprint: sprintStatus{
					ID:         "sprint-1",
					Status:     "IN_PROGRESS",
					StartTime:  now.Format(time.RFC3339),
					TasksDone:  0,
					TasksTotal: 0,
				},
				Config: configStatus{Mode: "RUNNING"},
				Tasks: taskStatus{
					Total:    0,
					ByStatus: map[string]int{},
				},
				Agents:            []agentStatus{},
				OrchestratorState: orchestratorStatus{Trigger: "NONE", Reason: "No triggers"},
				WorkQueues: workQueuesStatus{
					Coder:    queueStatus{Available: 0, Reason: "No claimable tasks"},
					Reviewer: queueStatus{Available: 0, Reason: "No reviewable tasks"},
				},
			},
			expectSections: []string{
				"=== AGENTS ===",
				"No active agents",
			},
		},
		{
			name: "detailed mode with anomalies and circuit breaker",
			data: statusData{
				Goal: goalStatus{
					Description: "Test",
					Status:      "IN_PROGRESS",
					SpecRef:     "spec.md",
				},
				Sprint: sprintStatus{
					ID:         "sprint-1",
					Status:     "IN_PROGRESS",
					StartTime:  now.Format(time.RFC3339),
					TasksDone:  0,
					TasksTotal: 0,
				},
				Config: configStatus{Mode: "RUNNING"},
				Tasks: taskStatus{
					Total:    0,
					ByStatus: map[string]int{},
				},
				Agents:            []agentStatus{},
				OrchestratorState: orchestratorStatus{Trigger: "NONE", Reason: "No triggers"},
				WorkQueues: workQueuesStatus{
					Coder:    queueStatus{Available: 0, Reason: "No claimable tasks"},
					Reviewer: queueStatus{Available: 0, Reason: "No reviewable tasks"},
				},
				Anomalies: &[]string{
					"[2024-01-01 12:00] retry_loop by coder-1: task-1",
					"[2024-01-01 12:05] trade_off by coder-2: task-2",
				},
				CircuitBreaker: &circuitBreakerStatus{
					Status:   "TRIGGERED",
					Triggers: []string{"retry_loop_detected (severity: high)"},
				},
			},
			expectSections: []string{
				"=== ANOMALIES ===",
				"⚠  [2024-01-01 12:00] retry_loop by coder-1: task-1",
				"⚠  [2024-01-01 12:05] trade_off by coder-2: task-2",
				"=== CIRCUIT BREAKER ===",
				"Status: TRIGGERED",
				"Triggers:",
				"- retry_loop_detected (severity: high)",
			},
		},
		{
			name: "tasks blocked by dependencies",
			data: statusData{
				Goal: goalStatus{
					Description: "Test",
					Status:      "IN_PROGRESS",
					SpecRef:     "spec.md",
				},
				Sprint: sprintStatus{
					ID:         "sprint-1",
					Status:     "IN_PROGRESS",
					StartTime:  now.Format(time.RFC3339),
					TasksDone:  0,
					TasksTotal: 3,
				},
				Config: configStatus{Mode: "RUNNING"},
				Tasks: taskStatus{
					Total:         3,
					Active:        3,
					Terminal:      0,
					ByStatus:      map[string]int{"DRAFT_CODE": 3},
					Claimable:     0,
					Reviewable:    0,
					BlockedByDeps: 3,
				},
				Agents:            []agentStatus{},
				OrchestratorState: orchestratorStatus{Trigger: "NONE", Reason: "No triggers"},
				WorkQueues: workQueuesStatus{
					Coder:    queueStatus{Available: 0, Reason: "No claimable tasks; 3 blocked by dependencies"},
					Reviewer: queueStatus{Available: 0, Reason: "No reviewable tasks"},
				},
			},
			expectSections: []string{
				"Blocked by dependencies: 3 tasks",
			},
		},
		{
			name: "many-to-one ready trigger",
			data: statusData{
				Goal: goalStatus{
					Description: "Test",
					Status:      "IN_PROGRESS",
					SpecRef:     "spec.md",
				},
				Sprint: sprintStatus{
					ID:         "sprint-1",
					Status:     "IN_PROGRESS",
					StartTime:  now.Format(time.RFC3339),
					TasksDone:  0,
					TasksTotal: 0,
				},
				Config: configStatus{Mode: "RUNNING"},
				Tasks: taskStatus{
					Total:    0,
					ByStatus: map[string]int{},
				},
				Agents:            []agentStatus{},
				OrchestratorState: orchestratorStatus{Trigger: "MANY_TO_ONE_READY", TriggerCount: 3, Reason: "3 many-to-one cohort(s) ready for consolidation transition"},
				WorkQueues: workQueuesStatus{
					Coder:    queueStatus{Available: 0, Reason: "No claimable tasks"},
					Reviewer: queueStatus{Available: 0, Reason: "No reviewable tasks"},
				},
			},
			expectSections: []string{
				"MANY_TO_ONE_READY",
				"many-to-one cohort(s) ready for consolidation transition",
			},
			notExpect: []string{
				"Unknown trigger",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := formatStatusDashboard(tt.data)
			if err != nil {
				t.Fatalf("formatStatusDashboard() error = %v", err)
			}

			// Check that all expected sections are present
			for _, expected := range tt.expectSections {
				if !strings.Contains(output, expected) {
					t.Errorf("expected output to contain %q, but it didn't.\nOutput:\n%s", expected, output)
				}
			}

			// Check that sections we don't expect are absent
			for _, notExpected := range tt.notExpect {
				if strings.Contains(output, notExpected) {
					t.Errorf("expected output NOT to contain %q, but it did.\nOutput:\n%s", notExpected, output)
				}
			}
		})
	}
}

func TestWriteTasksSection(t *testing.T) {
	tests := []struct {
		name   string
		tasks  taskStatus
		expect string
	}{
		{
			name: "statuses sorted alphabetically",
			tasks: taskStatus{
				Total: 5, Active: 3, Terminal: 2,
				ByStatus: map[string]int{
					"DRAFT_CODE":        2,
					"IMPLEMENTING_CODE": 1,
					"MERGED":            2,
				},
				Claimable: 2, Reviewable: 0, BlockedByDeps: 0,
			},
			expect: "=== TASKS ===\n" +
				"Total: 5 (3 active, 2 terminal)\n" +
				"\nBy Status:\n" +
				"  DRAFT_CODE: 2\n" +
				"  IMPLEMENTING_CODE: 1\n" +
				"  MERGED: 2\n" +
				"\nClaimable: 2 tasks\n" +
				"Reviewable: 0 tasks\n" +
				"\n",
		},
		{
			name: "blocked by deps line appears when nonzero",
			tasks: taskStatus{
				Total: 3, Active: 3, Terminal: 0,
				ByStatus:      map[string]int{"DRAFT_CODE": 3},
				Claimable:     0,
				Reviewable:    0,
				BlockedByDeps: 2,
			},
			expect: "=== TASKS ===\n" +
				"Total: 3 (3 active, 0 terminal)\n" +
				"\nBy Status:\n" +
				"  DRAFT_CODE: 3\n" +
				"\nClaimable: 0 tasks\n" +
				"Reviewable: 0 tasks\n" +
				"Blocked by dependencies: 2 tasks\n" +
				"\n",
		},
		{
			name: "empty status map omits By Status subsection",
			tasks: taskStatus{
				Total: 0, Active: 0, Terminal: 0,
				ByStatus:      map[string]int{},
				Claimable:     0,
				Reviewable:    0,
				BlockedByDeps: 0,
			},
			expect: "=== TASKS ===\n" +
				"Total: 0 (0 active, 0 terminal)\n" +
				"\nClaimable: 0 tasks\n" +
				"Reviewable: 0 tasks\n" +
				"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b strings.Builder
			writeTasksSection(&b, tt.tasks)
			if got := b.String(); got != tt.expect {
				t.Errorf("output mismatch:\n--- got ---\n%s--- expect ---\n%s", got, tt.expect)
			}
		})
	}
}

func TestWriteAgentsSection(t *testing.T) {
	t.Run("no agents", func(t *testing.T) {
		var b strings.Builder
		writeAgentsSection(&b, []agentStatus{})
		expect := "=== AGENTS ===\nNo active agents\n\n"
		if got := b.String(); got != expect {
			t.Errorf("output mismatch:\n--- got ---\n%q\n--- expect ---\n%q", got, expect)
		}
	})

	t.Run("agent table structure", func(t *testing.T) {
		agents := []agentStatus{{
			ID: "c-1", Role: "coder", Status: "WORKING",
			PID: 123, CurrentTask: "t-1",
			TimeSinceHeartbeat: "30s", ProcessStatus: "running",
		}}

		var b strings.Builder
		writeAgentsSection(&b, agents)
		got := b.String()

		expect := "=== AGENTS ===\n" +
			"ID   Role   Status   PID  Task  Heartbeat  Process\n" +
			"c-1  coder  WORKING  123  t-1   30s        running\n\n"

		if got != expect {
			t.Errorf("output mismatch:\n--- got ---\n%q\n--- expect ---\n%q", got, expect)
		}
	})

	t.Run("PID zero renders as dash", func(t *testing.T) {
		agents := []agentStatus{{
			ID: "c-1", Role: "coder", Status: "IDLE",
			PID: 0, CurrentTask: "",
			TimeSinceHeartbeat: "10s", ProcessStatus: "unknown",
		}}

		var b strings.Builder
		writeAgentsSection(&b, agents)
		got := b.String()

		expect := "=== AGENTS ===\n" +
			"ID   Role   Status  PID  Task  Heartbeat  Process\n" +
			"c-1  coder  IDLE    -          10s        unknown\n\n"

		if got != expect {
			t.Errorf("output mismatch:\n--- got ---\n%q\n--- expect ---\n%q", got, expect)
		}
	})
}
