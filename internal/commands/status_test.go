package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/render"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func setupPipelineRoot(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	return tmpDir
}

func TestCheckpointNotice(t *testing.T) {
	tests := []struct {
		name    string
		sprint  models.Sprint
		want    string
		wantNot string
	}{
		{
			name: "transition checkpoint allows doer reviewer work",
			sprint: models.Sprint{
				Status:            models.SprintStatusCheckpoint,
				CheckpointTrigger: models.CheckpointTriggerPlanningComplete,
			},
			want:    "doer/reviewer work may continue",
			wantNot: "agents paused",
		},
		{
			name: "many-to-one checkpoint allows doer reviewer work",
			sprint: models.Sprint{
				Status:            models.SprintStatusCheckpoint,
				CheckpointTrigger: models.CheckpointTriggerManyToOneReady,
			},
			want:    "doer/reviewer work may continue",
			wantNot: "agents paused",
		},
		{
			name: "manual checkpoint pauses agents",
			sprint: models.Sprint{
				Status: models.SprintStatusCheckpoint,
			},
			want:    "agents paused",
			wantNot: "doer/reviewer work may continue",
		},
		{
			name: "sprint-complete checkpoint pauses agents",
			sprint: models.Sprint{
				Status:            models.SprintStatusCheckpoint,
				CheckpointTrigger: models.CheckpointTriggerSprintComplete,
			},
			want:    "agents paused",
			wantNot: "doer/reviewer work may continue",
		},
		{
			name: "non-checkpoint has no notice",
			sprint: models.Sprint{
				Status:            models.SprintStatusInProgress,
				CheckpointTrigger: models.CheckpointTriggerPlanningComplete,
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkpointNotice(tt.sprint)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("checkpointNotice() = %q, want to contain %q", got, tt.want)
			}
			if tt.wantNot != "" && strings.Contains(got, tt.wantNot) {
				t.Fatalf("checkpointNotice() = %q, must not contain %q", got, tt.wantNot)
			}
		})
	}
}

func TestEffectiveIntegrationCompletionConsumers(t *testing.T) {
	terminalState := testhelpers.CreateValidState()
	baseCommit := "base"
	terminalState.Goal.BaseCommit = &baseCommit
	terminalState.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("coding-1", models.TaskStatusMerged, time.Now().UTC()),
	}
	terminalState.Sprint.Scope.Planned = []string{"coding-1"}
	originalState := mustMarshalStatusState(t, terminalState)

	tests := []struct {
		name       string
		projection prompts.EffectiveIntegrationCompletion
		wantStatus string
		wantCode   string
		wantDetail string
	}{
		{
			name: "reconciliation needed",
			projection: prompts.ProjectEffectiveIntegrationCompletion(ops.IntegrationProgressDecision{
				GlobalRequest: &ops.IntegrationAnalysisRequest{Key: "global:2"},
			}, nil, nil),
			wantStatus: "reconciliation_needed",
			wantDetail: "global:2",
		},
		{
			name: "waiting",
			projection: prompts.ProjectEffectiveIntegrationCompletion(ops.IntegrationProgressDecision{
				Waiting: &ops.IntegrationProgressReason{
					Code: "integration_repairs_pending", TaskIDs: []string{"repair-z", "repair-a"}, Guidance: "wait for merged repairs",
				},
			}, nil, nil),
			wantStatus: "waiting",
			wantCode:   "integration_repairs_pending",
			wantDetail: "repair-a, repair-z",
		},
		{
			name: "blocked",
			projection: prompts.ProjectEffectiveIntegrationCompletion(ops.IntegrationProgressDecision{
				Blocked: &ops.IntegrationProgressReason{
					Code: "integration_analysis_blocked", TaskIDs: []string{"slice-z", "slice-a"}, Guidance: "resolve the blocked analysis",
				},
			}, nil, nil),
			wantStatus: "blocked",
			wantCode:   "integration_analysis_blocked",
			wantDetail: "slice-a, slice-z",
		},
		{
			name: "exhausted",
			projection: prompts.ProjectEffectiveIntegrationCompletion(ops.IntegrationProgressDecision{
				Exhausted: true,
				Blocked: &ops.IntegrationProgressReason{
					Code: "global_generations_exhausted", Guidance: "generation limit reached",
				},
			}, nil, nil),
			wantStatus: "exhausted",
			wantCode:   "global_generations_exhausted",
			wantDetail: "generation limit reached",
		},
		{
			name:       "unavailable",
			projection: prompts.ProjectEffectiveIntegrationCompletion(ops.IntegrationProgressDecision{}, nil, os.ErrNotExist),
			wantStatus: "unavailable",
			wantCode:   "integration_progress_unavailable",
			wantDetail: os.ErrNotExist.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wake := agent.DetectOrchestratorWakeTriggersWithIntegrationProjection(terminalState, nil, nil, nil, tt.projection)
			got := buildOrchestratorStatusFromWakeResult(terminalState, wake)
			repeated := buildOrchestratorStatusFromWakeResult(terminalState, wake)
			if !reflect.DeepEqual(got, repeated) {
				t.Fatalf("repeated status projection changed: first=%#v second=%#v", got, repeated)
			}
			if got.Integration == nil {
				t.Fatal("integration diagnostics are absent")
			}
			if got.Integration.Status != tt.wantStatus || got.Integration.ReasonCode != tt.wantCode {
				t.Fatalf("integration diagnostics = %#v, want status=%q reason=%q", got.Integration, tt.wantStatus, tt.wantCode)
			}

			data := statusData{OrchestratorState: got}
			dashboard, err := formatStatusDashboard(data)
			if err != nil {
				t.Fatalf("format dashboard: %v", err)
			}
			jsonOutput, err := render.FormatJSON(data)
			if err != nil {
				t.Fatalf("format JSON: %v", err)
			}
			yamlOutput, err := render.FormatYAML(data)
			if err != nil {
				t.Fatalf("format YAML: %v", err)
			}
			for format, output := range map[string]string{"dashboard": dashboard, "json": jsonOutput, "yaml": yamlOutput} {
				for _, want := range []string{tt.wantStatus, tt.wantDetail} {
					if !strings.Contains(output, want) {
						t.Errorf("%s output missing %q:\n%s", format, want, output)
					}
				}
				if strings.Contains(output, "SPRINT_COMPLETE") {
					t.Errorf("ineffective %s output reported SPRINT_COMPLETE:\n%s", format, output)
				}
			}
		})
	}

	if after := mustMarshalStatusState(t, terminalState); after != originalState {
		t.Fatalf("status projection mutated lifecycle state:\nbefore=%s\nafter=%s", originalState, after)
	}

	t.Run("production evaluation fails closed and accepts only current HEAD clean evidence", func(t *testing.T) {
		projectRoot, state, reviewedHEAD := setupStatusCleanIntegrationProject(t)
		before := mustMarshalStatusState(t, state)

		clean := buildOrchestratorStatus(state, projectRoot)
		if clean.Trigger != "SPRINT_COMPLETE" || clean.Integration == nil || clean.Integration.Status != "complete" {
			t.Fatalf("current-HEAD clean status = %#v, want SPRINT_COMPLETE", clean)
		}
		if currentHEAD := testhelpers.MustGit(t, projectRoot, "rev-parse", "integration"); currentHEAD != reviewedHEAD {
			t.Fatalf("status read changed integration HEAD: got %q want %q", currentHEAD, reviewedHEAD)
		}

		if err := os.WriteFile(filepath.Join(projectRoot, "advance.txt"), []byte("advance\n"), 0o644); err != nil {
			t.Fatalf("write integration advance: %v", err)
		}
		testhelpers.MustGit(t, projectRoot, "add", "advance.txt")
		testhelpers.MustGit(t, projectRoot, "commit", "-m", "advance integration")
		advancedHEAD := testhelpers.MustGit(t, projectRoot, "rev-parse", "HEAD")
		testhelpers.MustGit(t, projectRoot, "update-ref", "refs/heads/integration", advancedHEAD)

		stale := buildOrchestratorStatus(state, projectRoot)
		if stale.Trigger == "SPRINT_COMPLETE" || stale.Integration == nil || stale.Integration.Status != "reconciliation_needed" {
			t.Fatalf("stale clean status = %#v, want reconciliation-needed non-terminal status", stale)
		}
		if after := mustMarshalStatusState(t, state); after != before {
			t.Fatalf("production status read mutated lifecycle state:\nbefore=%s\nafter=%s", before, after)
		}
		if currentHEAD := testhelpers.MustGit(t, projectRoot, "rev-parse", "integration"); currentHEAD != advancedHEAD {
			t.Fatalf("stale status read changed integration HEAD: got %q want %q", currentHEAD, advancedHEAD)
		}

		unavailableRoot := t.TempDir()
		testhelpers.SetupPipelineConfig(t, unavailableRoot)
		unavailable := buildOrchestratorStatus(state, unavailableRoot)
		if unavailable.Trigger == "SPRINT_COMPLETE" || unavailable.Integration == nil || unavailable.Integration.Status != "unavailable" {
			t.Fatalf("unavailable status = %#v, want fail-closed diagnostics", unavailable)
		}
	})
}

func mustMarshalStatusState(t *testing.T, state *models.State) string {
	t.Helper()
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal status state: %v", err)
	}
	return string(encoded)
}

func setupStatusCleanIntegrationProject(t *testing.T) (string, *models.State, string) {
	t.Helper()
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)
	head := testhelpers.MustGit(t, projectRoot, "rev-parse", "integration")

	analysis := testhelpers.BuildTaskByStatus("integration-global-1", models.TaskStatus("INTEGRATION_ANALYSIS_CLEAN"), time.Now().UTC())
	analysis.RolePair = "integration-pair"
	analysis.IntegrationAnalysis = &models.IntegrationAnalysisMetadata{
		Key: "global:1", Phase: models.IntegrationAnalysisPhaseGlobal, Generation: 1, SourceCommit: head,
	}
	analysis.ReviewCommit = testhelpers.StringPtr("global-report")
	state := testhelpers.CreateValidState()
	state.Goal.BaseCommit = &head
	state.Goal.Integration = &models.IntegrationLifecycle{
		ContributingSet: &models.IntegrationContributingSet{Scopes: []models.IntegrationScopeSnapshot{}},
		GlobalGenerations: []models.IntegrationGlobalGeneration{{
			Generation: 1, AnalysisTaskID: analysis.ID, AnalysisKey: "global:1",
			Verdict: models.IntegrationAnalysisVerdictClean, SourceCommit: head, ReportCommit: "global-report",
		}},
		Closure: &models.IntegrationClosure{
			Status: models.IntegrationClosureStatusClean, Generation: 1, AnalysisKey: "global:1", SourceCommit: head,
		},
	}
	state.Tasks = []models.Task{analysis}
	state.Sprint.Scope.Planned = []string{analysis.ID}
	return projectRoot, state, head
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
				if data.Tasks.Claimable != 1 {
					t.Errorf("expected 1 role-ready task (READY; REJECTED is ownership-reserved), got %d", data.Tasks.Claimable)
				}
				if data.Tasks.LegacyCoderClaimable != 2 {
					t.Errorf("expected legacy lifecycle count 2 (READY + REJECTED), got %d", data.Tasks.LegacyCoderClaimable)
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
				if data.Tasks.Blocked != 0 {
					t.Errorf("expected 0 explicit blocked tasks, got %d", data.Tasks.Blocked)
				}
				if data.Tasks.Claimable != 0 {
					t.Errorf("expected 0 claimable tasks (all blocked), got %d", data.Tasks.Claimable)
				}
			},
		},
		{
			name: "explicit blocked tasks counted separately from dependency blockers",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				blocked1 := testhelpers.BuildTaskByStatus("blocked-1", models.TaskStatusBlocked, now)
				blocked2 := testhelpers.BuildTaskByStatus("blocked-2", models.TaskStatusBlocked, now)
				blockedByDep := testhelpers.BuildTaskByStatus("blocked-by-dep", models.TaskStatusReady, now)
				blockedByDep.DependsOn = []string{"missing-dep"}
				state.Tasks = []models.Task{blocked1, blocked2, blockedByDep}
				return state
			}(),
			detailed: false,
			validate: func(t *testing.T, data statusData) {
				if data.Tasks.Blocked != 2 {
					t.Errorf("expected 2 explicit blocked tasks, got %d", data.Tasks.Blocked)
				}
				if data.Tasks.BlockedByDeps != 1 {
					t.Errorf("expected 1 task blocked by deps, got %d", data.Tasks.BlockedByDeps)
				}
				if data.Tasks.Claimable != 0 {
					t.Errorf("expected 0 claimable tasks, got %d", data.Tasks.Claimable)
				}
			},
		},
		{
			name: "legacy dependency blockers do not follow superseded replacement resolution",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				waiting := testhelpers.BuildTaskByStatus("waiting", models.TaskStatusReady, now)
				waiting.RolePair = ""
				waiting.DependsOn = []string{"old-dep"}
				oldDep := testhelpers.BuildTaskByStatus("old-dep", models.TaskStatusSuperseded, now)
				oldDep.RolePair = ""
				oldDep.SupersededBy = []string{"new-dep"}
				newDep := testhelpers.BuildTaskByStatus("new-dep", models.TaskStatusMerged, now)
				newDep.RolePair = ""
				state.Tasks = []models.Task{waiting, oldDep, newDep}
				return state
			}(),
			detailed: false,
			validate: func(t *testing.T, data statusData) {
				if data.Tasks.BlockedByDeps != 1 {
					t.Errorf("expected stale superseded dependency to block, got %d", data.Tasks.BlockedByDeps)
				}
				if data.Tasks.Blocked != 0 {
					t.Errorf("expected 0 explicit blocked tasks, got %d", data.Tasks.Blocked)
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
			name: "phase handoff identifies partial planning output and stale assigned blocker",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				readyPlan := testhelpers.BuildTaskByStatus("plan-ready", models.TaskStatusMerged, now)
				readyPlan.RolePair = "code-planning-pair"
				readyPlan.Output = []models.OutputEntry{
					{Desc: "implement X", DoneWhen: "tests pass", Scope: "pkg/x"},
				}

				activePlan := testhelpers.BuildTaskByStatus("plan-active", models.TaskStatusCodePlanning, now)
				activePlan.RolePair = "code-planning-pair"
				activePlan.AssignedTo = stringPtr("code-planner-2")
				lease := now.Add(30 * time.Minute)
				activePlan.LeaseExpires = &lease

				state.Tasks = []models.Task{readyPlan, activePlan}
				state.Sprint.Scope.Planned = []string{"plan-ready", "plan-active"}
				state.Agents = map[string]models.Agent{
					"code-planner-2": {
						Role:        models.RoleCodePlanner,
						Status:      models.AgentStatusWorking,
						CurrentTask: stringPtr("plan-active"),
						Heartbeat:   now.Add(-10 * time.Second),
						PID:         999999,
					},
				}
				return state
			}(),
			detailed:    false,
			projectRoot: pipelineRoot,
			pr:          pr,
			validate: func(t *testing.T, data statusData) {
				if data.PhaseHandoff == nil {
					t.Fatal("expected phase handoff diagnostic")
				}
				if data.PhaseHandoff.State != "PARTIAL_READY" {
					t.Fatalf("handoff state = %q, want PARTIAL_READY", data.PhaseHandoff.State)
				}
				if len(data.PhaseHandoff.ReadyPlanningTasks) != 1 || data.PhaseHandoff.ReadyPlanningTasks[0] != "plan-ready" {
					t.Fatalf("ready planning tasks = %v, want [plan-ready]", data.PhaseHandoff.ReadyPlanningTasks)
				}
				if len(data.PhaseHandoff.BlockingTasks) != 1 || data.PhaseHandoff.BlockingTasks[0].ID != "plan-active" {
					t.Fatalf("blocking tasks = %+v, want plan-active", data.PhaseHandoff.BlockingTasks)
				}
				if len(data.PhaseHandoff.StaleAssignedAgents) != 1 {
					t.Fatalf("stale assigned agents = %+v, want one stale assignment", data.PhaseHandoff.StaleAssignedAgents)
				}
				stale := data.PhaseHandoff.StaleAssignedAgents[0]
				if stale.ProcessStatusSource == "" || stale.ProcessStatusDetail == "" {
					t.Fatalf("stale process diagnostics missing: %+v", stale)
				}
				if !strings.Contains(data.PhaseHandoff.Explanation, "create implementation tasks after resume") {
					t.Fatalf("handoff explanation did not describe implementation handoff: %q", data.PhaseHandoff.Explanation)
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

func TestBuildStatusDataPipelineReadinessMatchesClaimsAndMissingRoles(t *testing.T) {
	pr := statusReadinessResolver()
	state := statusReadinessState()

	data := BuildStatusData(state, false, "", pr, nil)

	wantClaimable := []models.RoleTaskReadiness{
		{Role: "coder", Count: 1},
		{Role: "custom-doer", Count: 2},
		{Role: "idle-doer", Count: 0},
	}
	wantReviewable := []models.RoleTaskReadiness{
		{Role: "code-reviewer", Count: 1},
		{Role: "custom-reviewer", Count: 2},
		{Role: "idle-reviewer", Count: 0},
	}
	if !reflect.DeepEqual(data.Tasks.ClaimableByRole, wantClaimable) {
		t.Fatalf("ClaimableByRole = %#v, want %#v", data.Tasks.ClaimableByRole, wantClaimable)
	}
	if !reflect.DeepEqual(data.Tasks.ReviewableByRole, wantReviewable) {
		t.Fatalf("ReviewableByRole = %#v, want %#v", data.Tasks.ReviewableByRole, wantReviewable)
	}
	if data.Tasks.Claimable != 3 || data.Tasks.Reviewable != 3 {
		t.Fatalf("aggregate readiness = (%d, %d), want (3, 3)", data.Tasks.Claimable, data.Tasks.Reviewable)
	}
	if data.Tasks.LegacyCoderClaimable != 1 || data.Tasks.LegacyCodeReviewerReviewable != 1 {
		t.Fatalf("legacy readiness = (%d, %d), want (1, 1)", data.Tasks.LegacyCoderClaimable, data.Tasks.LegacyCodeReviewerReviewable)
	}
	if data.WorkQueues.Coder.Available != 1 || data.WorkQueues.Reviewer.Available != 1 {
		t.Fatalf("legacy work queues = (%d, %d), want (1, 1)", data.WorkQueues.Coder.Available, data.WorkQueues.Reviewer.Available)
	}
	claimableSum := 0
	for _, role := range data.Tasks.ClaimableByRole {
		claimableSum += role.Count
	}
	reviewableSum := 0
	for _, role := range data.Tasks.ReviewableByRole {
		reviewableSum += role.Count
	}
	if data.Tasks.Claimable != claimableSum || data.Tasks.Reviewable != reviewableSum {
		t.Fatalf("aggregate readiness (%d, %d) does not match role sums (%d, %d)", data.Tasks.Claimable, data.Tasks.Reviewable, claimableSum, reviewableSum)
	}

	missing := FindMissingRolesWithClaimableWork(state, pr)
	missingCounts := make(map[string]int, len(missing))
	for _, entry := range missing {
		missingCounts[entry.Role] = entry.TaskCount
	}
	selectionNow := time.Now().UTC()
	for _, roleReadiness := range append(data.Tasks.ClaimableByRole, data.Tasks.ReviewableByRole...) {
		direct := 0
		for i := range state.Tasks {
			if models.IsRoleTaskReady(state, &state.Tasks[i], roleReadiness.Role, pr, selectionNow) {
				direct++
			}
		}
		if roleReadiness.Count != direct {
			t.Errorf("readiness for %q = %d, direct role-ready count = %d", roleReadiness.Role, roleReadiness.Count, direct)
		}
		if roleReadiness.Count != missingCounts[roleReadiness.Role] {
			t.Errorf("readiness for %q = %d, missing-role count = %d", roleReadiness.Role, roleReadiness.Count, missingCounts[roleReadiness.Role])
		}
	}

	dashboard, err := formatStatusDashboard(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Claimable by role:", "custom-doer: 2", "Reviewable by role:", "custom-reviewer: 2",
		"Legacy coder claimable: 1 tasks", "Legacy code-reviewer reviewable: 1 tasks",
	} {
		if !strings.Contains(dashboard, want) {
			t.Errorf("dashboard missing %q:\n%s", want, dashboard)
		}
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"claimable_by_role"`, `"reviewable_by_role"`,
		`"legacy_coder_claimable":1`, `"legacy_code_reviewer_reviewable":1`,
	} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("structured status missing %s: %s", want, encoded)
		}
	}
	yamlStatus, err := render.FormatYAML(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"claimable_by_role:", "reviewable_by_role:", "legacy_coder_claimable: 1", "agent_capacity:"} {
		if !strings.Contains(yamlStatus, want) {
			t.Errorf("YAML status missing %q:\n%s", want, yamlStatus)
		}
	}
}

func TestBuildStatusDataAgentCapacityIsSeparateFromTaskReadiness(t *testing.T) {
	pr := statusReadinessResolver()
	state := statusReadinessState()
	readinessBefore := BuildStatusData(state, false, "", pr, nil).Tasks
	now := time.Now().UTC()
	liveUntil := now.Add(time.Hour)
	expired := now.Add(-time.Hour)
	registered := now.Add(-time.Minute)
	state.Agents = map[string]models.Agent{
		"coder-1": {
			Role: "coder", Status: models.AgentStatusIdle, LeaseExpires: &liveUntil,
			Heartbeat: now, RegisteredAt: registered,
		},
		"custom-doer-1": {
			Role: "custom-doer", Status: models.AgentStatusWorking, LeaseExpires: &liveUntil,
			Heartbeat: now, RegisteredAt: registered,
		},
		"custom-reviewer-1": {
			Role: "custom-reviewer", Status: models.AgentStatusIdle, LeaseExpires: &liveUntil,
			Heartbeat: now, RegisteredAt: registered,
		},
		"code-reviewer-1": {
			Role: "code-reviewer", Status: models.AgentStatusIdle, LeaseExpires: &expired,
			Heartbeat: now.Add(-24 * time.Hour), RegisteredAt: registered,
		},
	}
	state.AgentHealth = map[string]models.AgentHealth{
		"custom-reviewer-1": {
			State: models.AgentHealthDegraded, Role: "custom-reviewer", RegisteredAt: &registered,
		},
		"idle-reviewer-1": {
			State: models.AgentHealthDegraded, Role: "idle-reviewer",
		},
	}

	data := BuildStatusData(state, false, "", pr, nil)
	if !reflect.DeepEqual(data.Tasks, readinessBefore) {
		t.Fatalf("agent capacity changed task readiness:\nbefore: %#v\nafter:  %#v", readinessBefore, data.Tasks)
	}
	if data.AgentCapacity.Live != 3 || data.AgentCapacity.Free != 1 || data.AgentCapacity.Degraded != 2 {
		t.Fatalf("AgentCapacity totals = %+v, want live=3 free=1 degraded=2", data.AgentCapacity)
	}
	wantByRole := []roleAgentCapacity{
		{Role: "code-reviewer"},
		{Role: "coder", Live: 1, Free: 1},
		{Role: "custom-doer", Live: 1},
		{Role: "custom-reviewer", Live: 1, Degraded: 1},
		{Role: "idle-doer"},
		{Role: "idle-reviewer", Degraded: 1},
	}
	if !reflect.DeepEqual(data.AgentCapacity.ByRole, wantByRole) {
		t.Fatalf("AgentCapacity.ByRole = %#v, want %#v", data.AgentCapacity.ByRole, wantByRole)
	}
	dashboard, err := formatStatusDashboard(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"=== AGENT CAPACITY ===", "Live: 3, Free: 1, Degraded: 2", "custom-reviewer"} {
		if !strings.Contains(dashboard, want) {
			t.Errorf("dashboard missing %q:\n%s", want, dashboard)
		}
	}
}

func TestBuildStatusDataCompletedSprintRequiresPlanningMerges(t *testing.T) {
	projectRoot := setupPipelineRoot(t)
	pr, err := ops.LoadResolverForModels(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	originalBinaryName := brand.BinaryName
	brand.BinaryName = "acme"
	t.Cleanup(func() { brand.BinaryName = originalBinaryName })

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCompleted
	state.Sprint.Scope.Planned = []string{"plan-b", "plan-a"}
	state.Tasks = []models.Task{
		planningTaskWithOutput("plan-a", models.TaskStatusCodingPlanApproved),
		planningTaskWithOutput("plan-b", models.TaskStatusCodingPlanApproved),
	}
	before, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}

	data := BuildStatusData(state, false, projectRoot, pr, nil)
	after, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("BuildStatusData mutated state while reporting merge blockers")
	}
	if data.OrchestratorState.Trigger != "NONE" {
		t.Fatalf("wake trigger = %q, want NONE", data.OrchestratorState.Trigger)
	}
	if data.PhaseHandoff == nil || data.PhaseHandoff.State != "MERGE_REQUIRED" {
		t.Fatalf("PhaseHandoff = %+v, want MERGE_REQUIRED", data.PhaseHandoff)
	}
	wantMerges := []phaseMergeRequired{
		{TaskID: "plan-b", Action: "acme wt-merge plan-b"},
		{TaskID: "plan-a", Action: "acme wt-merge plan-a"},
	}
	if !reflect.DeepEqual(data.PhaseHandoff.MergeRequired, wantMerges) {
		t.Fatalf("MergeRequired = %#v, want %#v", data.PhaseHandoff.MergeRequired, wantMerges)
	}
	yamlStatus, err := render.FormatYAML(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"phase_handoff:", "merge_required:", "task_id: plan-b", "action: acme wt-merge plan-b"} {
		if !strings.Contains(yamlStatus, want) {
			t.Errorf("YAML merge handoff missing %q:\n%s", want, yamlStatus)
		}
	}
	dashboard, err := formatStatusDashboard(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"State: MERGE_REQUIRED", "plan-b: acme wt-merge plan-b", "plan-a: acme wt-merge plan-a", "Wake Trigger: NONE"} {
		if !strings.Contains(dashboard, want) {
			t.Errorf("dashboard missing %q:\n%s", want, dashboard)
		}
	}

	for i := range state.Tasks {
		state.Tasks[i].Status = models.TaskStatusMerged
	}
	afterMerge := BuildStatusData(state, false, projectRoot, pr, nil)
	if afterMerge.PhaseHandoff == nil {
		t.Fatal("merged planning output should retain the existing phase handoff")
	}
	if len(afterMerge.PhaseHandoff.MergeRequired) != 0 {
		t.Fatalf("MergeRequired after merge = %#v, want none", afterMerge.PhaseHandoff.MergeRequired)
	}
	if !reflect.DeepEqual(afterMerge.PhaseHandoff.ReadyPlanningTasks, []string{"plan-b", "plan-a"}) {
		t.Fatalf("ReadyPlanningTasks after merge = %v, want sprint-scope order", afterMerge.PhaseHandoff.ReadyPlanningTasks)
	}
	if afterMerge.OrchestratorState.Trigger != data.OrchestratorState.Trigger {
		t.Fatalf("wake trigger changed after merge: before=%q after=%q", data.OrchestratorState.Trigger, afterMerge.OrchestratorState.Trigger)
	}

	single := testhelpers.CreateValidState()
	single.Sprint.Status = models.SprintStatusCompleted
	single.Sprint.Scope.Planned = []string{"plan-only"}
	single.Tasks = []models.Task{planningTaskWithOutput("plan-only", models.TaskStatusCodingPlanApproved)}
	singleData := BuildStatusData(single, false, projectRoot, pr, nil)
	wantSingle := []phaseMergeRequired{{TaskID: "plan-only", Action: "acme wt-merge plan-only"}}
	if singleData.PhaseHandoff == nil || !reflect.DeepEqual(singleData.PhaseHandoff.MergeRequired, wantSingle) {
		t.Fatalf("single approved planning handoff = %+v, want %#v", singleData.PhaseHandoff, wantSingle)
	}
}

func statusReadinessResolver() models.PipelineResolver {
	roles := map[string]pipeline.RoleDef{
		"coder":           {Type: "doer"},
		"code-reviewer":   {Type: "reviewer"},
		"custom-doer":     {Type: "doer"},
		"custom-reviewer": {Type: "reviewer"},
		"idle-doer":       {Type: "doer"},
		"idle-reviewer":   {Type: "reviewer"},
	}
	pairs := map[string]pipeline.RolePairDef{
		"coding-pair":           statusRolePair("coder", "code-reviewer", "DRAFT_CODE", "IMPLEMENTING_CODE", "CODE_TO_REVIEW", "REVIEWING_CODE", "CODE_APPROVED", "CODE_REJECTED"),
		"custom-pair":           statusRolePair("custom-doer", "custom-reviewer", "CUSTOM_READY", "CUSTOM_EXECUTING", "CUSTOM_TO_REVIEW", "CUSTOM_REVIEWING", "CUSTOM_APPROVED", "CUSTOM_REJECTED"),
		"alternate-custom-pair": statusRolePair("custom-doer", "custom-reviewer", "ALT_CUSTOM_READY", "ALT_CUSTOM_EXECUTING", "ALT_CUSTOM_TO_REVIEW", "ALT_CUSTOM_REVIEWING", "ALT_CUSTOM_APPROVED", "ALT_CUSTOM_REJECTED"),
		"idle-pair":             statusRolePair("idle-doer", "idle-reviewer", "IDLE_READY", "IDLE_EXECUTING", "IDLE_TO_REVIEW", "IDLE_REVIEWING", "IDLE_APPROVED", "IDLE_REJECTED"),
	}
	return pipeline.NewResolver(&pipeline.PipelineConfig{Pipeline: pipeline.Pipeline{Roles: roles, RolePairs: pairs}})
}

func operationalTerminalResolver() models.PipelineResolver {
	return pipeline.NewResolver(&pipeline.PipelineConfig{Pipeline: pipeline.Pipeline{
		Roles: map[string]pipeline.RoleDef{
			"integration-analyst":  {Type: "doer"},
			"integration-reviewer": {Type: "reviewer"},
		},
		RolePairs: map[string]pipeline.RolePairDef{
			"integration-pair": {
				Doer: "integration-analyst", Reviewer: "integration-reviewer",
				States: pipeline.RolePairStates{
					Initial: "DRAFT_INTEGRATION_ANALYSIS", Executing: "ANALYZING_INTEGRATION",
					Submitted: "INTEGRATION_ANALYSIS_TO_REVIEW", Reviewing: "REVIEWING_INTEGRATION_ANALYSIS",
					Approved: "INTEGRATION_ANALYSIS_APPROVED", Rejected: "INTEGRATION_ANALYSIS_REJECTED",
					Clean: "INTEGRATION_ANALYSIS_CLEAN",
				},
			},
		},
	}})
}

func TestBuildTaskStatusUsesOperationalTerminalStates(t *testing.T) {
	state := &models.State{Tasks: []models.Task{
		{ID: "clean", RolePair: "integration-pair", Status: "INTEGRATION_ANALYSIS_CLEAN"},
		{ID: "approved", RolePair: "integration-pair", Status: "INTEGRATION_ANALYSIS_APPROVED"},
	}}

	got := buildTaskStatus(state, operationalTerminalResolver())
	if got.Total != 2 || got.Terminal != 1 || got.Active != 1 {
		t.Fatalf("task status = total:%d terminal:%d active:%d, want 2/1/1", got.Total, got.Terminal, got.Active)
	}
}

func statusRolePair(doer, reviewer, initial, executing, submitted, reviewing, approved, rejected string) pipeline.RolePairDef {
	return pipeline.RolePairDef{
		Doer: doer, Reviewer: reviewer,
		States: pipeline.RolePairStates{
			Initial: initial, Executing: executing, Submitted: submitted,
			Reviewing: reviewing, Approved: approved, Rejected: rejected,
		},
	}
}

func statusReadinessState() *models.State {
	reservedOwner := "custom-doer-1"
	reservedUntil := time.Now().UTC().Add(time.Hour)
	return &models.State{
		Tasks: []models.Task{
			{ID: "merged-dependency", Status: models.TaskStatusMerged, RolePair: "coding-pair"},
			{ID: "coding-ready", Status: "DRAFT_CODE", RolePair: "coding-pair"},
			{ID: "coding-submitted", Status: "CODE_TO_REVIEW", RolePair: "coding-pair", ReviewCommit: stringPtr("coding-sha")},
			{ID: "custom-ready", Status: "CUSTOM_READY", RolePair: "custom-pair", DependsOn: []string{"merged-dependency"}},
			{ID: "custom-blocked", Status: "CUSTOM_READY", RolePair: "custom-pair", DependsOn: []string{"missing"}},
			{ID: "custom-executing", Status: "CUSTOM_EXECUTING", RolePair: "custom-pair"},
			{ID: "custom-submitted", Status: "CUSTOM_TO_REVIEW", RolePair: "custom-pair", ReviewCommit: stringPtr("custom-sha")},
			{ID: "alternate-custom-ready", Status: "ALT_CUSTOM_READY", RolePair: "alternate-custom-pair"},
			{ID: "alternate-custom-submitted", Status: "ALT_CUSTOM_TO_REVIEW", RolePair: "alternate-custom-pair", ReviewCommit: stringPtr("alternate-custom-sha")},
			{ID: "custom-rejected-reserved", Status: "CUSTOM_REJECTED", RolePair: "custom-pair", AssignedTo: &reservedOwner, LeaseExpires: &reservedUntil},
		},
		Agents:      map[string]models.Agent{},
		AgentHealth: map[string]models.AgentHealth{},
	}
}

func planningTaskWithOutput(id string, status models.TaskStatus) models.Task {
	return models.Task{
		ID: id, Status: status, RolePair: "code-planning-pair",
		Output: []models.OutputEntry{{Desc: "implement", DoneWhen: "done", Scope: "scope"}},
	}
}

func TestBuildStatusData_NoFollowUpHidesPipelineTransitions(t *testing.T) {
	now := time.Now().UTC()
	projectRoot := setupPipelineRoot(t)
	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")

	state := testhelpers.CreateValidState()
	state.Config.NoFollowUp = true
	state.Tasks = []models.Task{
		{
			ID:          "us-1",
			Type:        models.TaskTypePlanning,
			RolePair:    "us-writing-pair",
			Description: "US task",
			Status:      models.TaskStatus("US_APPROVED"),
			Priority:    1,
			Created:     now,
			SpecRef:     "README.md",
			DoneWhen:    "Done",
			Scope:       "scope",
			History:     []models.TaskHistoryEntry{},
		},
	}
	testhelpers.WriteInitialState(t, statePath, state)

	// Pending transitions are populated via ops.AvailableManualTransitions,
	// which applies the runtime no_follow_up policy from state.yaml. The
	// PipelineResolver passed here is used for lifecycle/status rendering only.
	pr, _ := ops.LoadResolverForModels(projectRoot)
	data := BuildStatusData(state, false, projectRoot, pr, nil)

	if len(data.PendingTransitions) != 0 {
		t.Fatalf("PendingTransitions = %v, want none", data.PendingTransitions)
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
	if agent.ProcessStatusSource == "" {
		t.Error("expected ProcessStatusSource to be populated")
	}
	if agent.ProcessStatusDetail == "" {
		t.Error("expected ProcessStatusDetail to be populated")
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

func TestGetProcessStatusInfo_ProcfsFallback(t *testing.T) {
	oldProcRoot := processStatusProcRoot
	processStatusProcRoot = t.TempDir()
	t.Cleanup(func() { processStatusProcRoot = oldProcRoot })

	pid := 999999
	writeStatusProcCmdline(t, processStatusProcRoot, pid, []string{"liza", "agent", "coder", "--agent-id", "coder-1", "--cli", "codex"})

	info := getProcessStatusInfoForAgent(pid, "coder", "coder-1")
	if info.Status != "running" || info.Source != "procfs" {
		t.Fatalf("process status = %+v, want running from procfs", info)
	}

	autoAssignedPID := 999998
	writeStatusProcCmdline(t, processStatusProcRoot, autoAssignedPID, []string{"liza", "agent", "coder", "--cli", "codex"})

	autoAssigned := getProcessStatusInfoForAgent(autoAssignedPID, "coder", "coder-1")
	if autoAssigned.Status != "running" || autoAssigned.Source != "procfs" {
		t.Fatalf("auto-assigned process status = %+v, want running from procfs", autoAssigned)
	}

	mismatch := getProcessStatusInfoForAgent(pid, "code-reviewer", "code-reviewer-1")
	if mismatch.Status != "mismatched" || mismatch.Source != "procfs" {
		t.Fatalf("mismatched process status = %+v, want mismatched from procfs", mismatch)
	}

	wrongExplicitID := getProcessStatusInfoForAgent(pid, "coder", "coder-2")
	if wrongExplicitID.Status != "mismatched" || wrongExplicitID.Source != "procfs" {
		t.Fatalf("wrong explicit ID status = %+v, want mismatched from procfs", wrongExplicitID)
	}
}

func writeStatusProcCmdline(t *testing.T, procRoot string, pid int, argv []string) {
	t.Helper()

	procDir := filepath.Join(procRoot, strconv.Itoa(pid))
	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatal(err)
	}
	cmdline := strings.Join(argv, "\x00") + "\x00"
	if err := os.WriteFile(filepath.Join(procDir, "cmdline"), []byte(cmdline), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildStatusData_AgentHealth(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusIdle,
		Provider:     "codex",
		PID:          1234,
		Heartbeat:    now,
		RegisteredAt: now,
	}
	state.AgentHealth = map[string]models.AgentHealth{
		"coder-1": {
			State:        models.AgentHealthDegraded,
			Role:         "coder",
			Provider:     "codex",
			PID:          1234,
			RegisteredAt: &now,
			DegradedAt:   now,
			Reason:       "claim_worktree_create_failed",
			RecoverHint:  "restart elsewhere",
		},
	}

	data := BuildStatusData(state, false, "", nil, nil)

	if data.AgentHealth == nil {
		t.Fatal("AgentHealth = nil, want current degraded health")
	}
	if got := data.AgentHealth["coder-1"].Reason; got != "claim_worktree_create_failed" {
		t.Fatalf("AgentHealth[coder-1].Reason = %q", got)
	}
	if len(data.Agents) != 1 || data.Agents[0].Health != string(models.AgentHealthDegraded) {
		t.Fatalf("Agents = %+v, want degraded health on coder-1", data.Agents)
	}
	if data.AgentCapacity.Degraded != 1 {
		t.Fatalf("AgentCapacity.Degraded = %d, want 1", data.AgentCapacity.Degraded)
	}
}

func TestBuildStatusData_OrphanedAgentHealth(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.AgentHealth = map[string]models.AgentHealth{
		"coder-1": {
			State:       models.AgentHealthDegraded,
			Role:        "coder",
			Provider:    "codex",
			PID:         1234,
			DegradedAt:  now,
			Reason:      "claim_worktree_create_failed",
			RecoverHint: "restart outside sandbox",
		},
	}

	data := BuildStatusData(state, false, "", nil, nil)

	if data.AgentHealth == nil {
		t.Fatal("AgentHealth = nil, want orphaned degraded health")
	}
	if got := data.AgentHealth["coder-1"].RecoverHint; got != "restart outside sandbox" {
		t.Fatalf("AgentHealth[coder-1].RecoverHint = %q", got)
	}
	if len(data.Agents) != 0 {
		t.Fatalf("Agents = %+v, want no active agents", data.Agents)
	}
	if data.AgentCapacity.Degraded != 1 {
		t.Fatalf("AgentCapacity.Degraded = %d, want 1", data.AgentCapacity.Degraded)
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

func TestBuildStatusDataCheckpointNoticeReadsSprintTrigger(t *testing.T) {
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.CheckpointTrigger = models.CheckpointTriggerManyToOneReady
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC()),
	}
	state.Sprint.Scope.Planned = []string{"task-1"}

	data := BuildStatusData(state, false, "", nil, nil)

	if data.Sprint.CheckpointTrigger != models.CheckpointTriggerManyToOneReady {
		t.Fatalf("checkpoint trigger = %q, want %q", data.Sprint.CheckpointTrigger, models.CheckpointTriggerManyToOneReady)
	}
	if !strings.Contains(data.Sprint.CheckpointNotice, "doer/reviewer work may continue") {
		t.Fatalf("checkpoint notice = %q, want transition checkpoint guidance", data.Sprint.CheckpointNotice)
	}
	if data.OrchestratorState.Trigger != "NONE" {
		t.Fatalf("wake trigger = %q, want NONE while checkpointed", data.OrchestratorState.Trigger)
	}
}

func TestFormatStatusDashboardCheckpointNotice(t *testing.T) {
	now := time.Now().UTC()
	data := statusData{
		Goal:   goalStatus{Description: "Test", Status: "IN_PROGRESS", SpecRef: "spec.md"},
		Sprint: sprintStatus{ID: "sprint-1", Status: "CHECKPOINT", CheckpointNotice: "CHECKPOINT: transition gate pending; doer/reviewer work may continue; run 'liza resume' to create downstream tasks", StartTime: now.Format(time.RFC3339)},
		Config: configStatus{Mode: "RUNNING"},
		Tasks:  taskStatus{ByStatus: map[string]int{}},
		OrchestratorState: orchestratorStatus{
			Trigger: "NONE",
			Reason:  "No triggers; orchestrator is idle",
		},
		WorkQueues: workQueuesStatus{
			Coder:    queueStatus{Reason: "No claimable tasks"},
			Reviewer: queueStatus{Reason: "No reviewable tasks"},
		},
	}

	out, err := formatStatusDashboard(data)
	if err != nil {
		t.Fatalf("formatStatusDashboard() error = %v", err)
	}
	if !strings.Contains(out, "Checkpoint: CHECKPOINT: transition gate pending; doer/reviewer work may continue") {
		t.Fatalf("dashboard did not render checkpoint notice:\n%s", out)
	}
	if !strings.Contains(out, "Wake Trigger: NONE") {
		t.Fatalf("dashboard did not preserve wake trigger output:\n%s", out)
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
					Total:         5,
					Active:        5,
					Terminal:      0,
					ByStatus:      map[string]int{"BLOCKED": 2, "DRAFT_CODE": 3},
					Claimable:     0,
					Reviewable:    0,
					Blocked:       2,
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
				"Blocked: 2 tasks",
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
		{
			name: "phase handoff section",
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
					TasksDone:  1,
					TasksTotal: 2,
				},
				Config: configStatus{Mode: "RUNNING"},
				Tasks: taskStatus{
					Total:    2,
					Active:   1,
					Terminal: 1,
					ByStatus: map[string]int{"CODE_PLANNING": 1, "MERGED": 1},
				},
				Agents:            []agentStatus{},
				OrchestratorState: orchestratorStatus{Trigger: "PLANNING_COMPLETE", TriggerCount: 1, Reason: "1 planning task(s) merged with output[]; ready for coding task expansion"},
				WorkQueues: workQueuesStatus{
					Coder:    queueStatus{Available: 0, Reason: "No claimable tasks"},
					Reviewer: queueStatus{Available: 0, Reason: "No reviewable tasks"},
				},
				PhaseHandoff: &phaseHandoffStatus{
					State:              "PARTIAL_READY",
					Explanation:        "1 merged planning task(s) have unconsumed output; 1 non-terminal planned task(s) are still active.",
					ReadyPlanningTasks: []string{"plan-ready"},
					BlockingTasks: []phaseHandoffTask{{
						ID:                 "plan-active",
						Status:             "CODE_PLANNING",
						RolePair:           "code-planning-pair",
						AssignedTo:         "code-planner-2",
						AgentProcessStatus: "stopped",
					}},
					StaleAssignedAgents: []phaseHandoffTask{{
						ID:                 "plan-active",
						AssignedTo:         "code-planner-2",
						AgentStatus:        "WORKING",
						AgentProcessStatus: "stopped",
						LeaseExpires:       "2026-05-15T20:00:00Z",
					}},
				},
			},
			expectSections: []string{
				"=== PHASE HANDOFF ===",
				"State: PARTIAL_READY",
				"Ready planning tasks:",
				"plan-ready",
				"Non-terminal planned tasks:",
				"plan-active",
				"Stale assigned agents:",
				"code-planner-2",
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
				"Legacy coder claimable: 0 tasks\n" +
				"Legacy code-reviewer reviewable: 0 tasks\n" +
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
				"Legacy coder claimable: 0 tasks\n" +
				"Legacy code-reviewer reviewable: 0 tasks\n" +
				"Blocked by dependencies: 2 tasks\n" +
				"\n",
		},
		{
			name: "blocked line appears separately from dependency blockers",
			tasks: taskStatus{
				Total: 5, Active: 5, Terminal: 0,
				ByStatus:      map[string]int{"BLOCKED": 2, "DRAFT_CODE": 3},
				Claimable:     0,
				Reviewable:    0,
				Blocked:       2,
				BlockedByDeps: 3,
			},
			expect: "=== TASKS ===\n" +
				"Total: 5 (5 active, 0 terminal)\n" +
				"\nBy Status:\n" +
				"  BLOCKED: 2\n" +
				"  DRAFT_CODE: 3\n" +
				"\nClaimable: 0 tasks\n" +
				"Reviewable: 0 tasks\n" +
				"Legacy coder claimable: 0 tasks\n" +
				"Legacy code-reviewer reviewable: 0 tasks\n" +
				"Blocked: 2 tasks\n" +
				"Blocked by dependencies: 3 tasks\n" +
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
				"Legacy coder claimable: 0 tasks\n" +
				"Legacy code-reviewer reviewable: 0 tasks\n" +
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
			"ID   Role   Status   Health  PID  Task  Heartbeat  Process\n" +
			"c-1  coder  WORKING  -       123  t-1   30s        running\n\n"

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
			"ID   Role   Status  Health  PID  Task  Heartbeat  Process\n" +
			"c-1  coder  IDLE    -       -          10s        unknown\n\n"

		if got != expect {
			t.Errorf("output mismatch:\n--- got ---\n%q\n--- expect ---\n%q", got, expect)
		}
	})
}

func TestWriteAgentHealthSection(t *testing.T) {
	health := map[string]models.AgentHealth{
		"coder-1": {
			State:       models.AgentHealthDegraded,
			Role:        "coder",
			Reason:      "claim_worktree_create_failed",
			RecoverHint: "restart outside sandbox",
		},
	}

	var b strings.Builder
	writeAgentHealthSection(&b, health)
	got := b.String()

	if !strings.Contains(got, "=== AGENT HEALTH ===") {
		t.Fatalf("expected health section, got:\n%s", got)
	}
	if !strings.Contains(got, "coder-1") || !strings.Contains(got, "restart outside sandbox") {
		t.Fatalf("expected degraded agent details, got:\n%s", got)
	}
}
