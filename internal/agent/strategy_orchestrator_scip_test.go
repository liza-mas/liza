package agent

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestOrchestratorPreExecutionScipRefreshRunsBeforePromptStateReadForWakeTriggers(t *testing.T) {
	now := time.Now().UTC()
	baseCommit := "abc123"

	manyToOneTransitions := []ops.ManyToOneTransitionInfo{{
		Name:           "us-to-coding",
		SourceRolePair: "us-writing-pair",
	}}

	tests := []struct {
		name              string
		wantTrigger       OrchestratorWakeTrigger
		pipelineTerminals []models.TaskStatus
		planningPairs     map[string]bool
		m2oTransitions    []ops.ManyToOneTransitionInfo
		mutateState       func(*models.State)
	}{
		{
			name:        "initial planning",
			wantTrigger: WakeTriggerInitialPlanning,
			mutateState: func(state *models.State) {
				state.Tasks = []models.Task{}
			},
		},
		{
			name:        "blocked task",
			wantTrigger: WakeTriggerBlocked,
			mutateState: func(state *models.State) {
				state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)}
			},
		},
		{
			name:        "hypothesis exhausted",
			wantTrigger: WakeTriggerHypothesisExhausted,
			mutateState: func(state *models.State) {
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
				task.FailedBy = []string{"coder-1", "coder-2"}
				state.Tasks = []models.Task{task}
			},
		},
		{
			name:        "immediate discovery",
			wantTrigger: WakeTriggerImmediateDiscovery,
			mutateState: func(state *models.State) {
				state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)}
				state.Discovered = []models.Discovery{{
					ID:             "disc-1",
					By:             "coder-1",
					During:         "task-1",
					Description:    "needs triage",
					Severity:       "critical",
					Urgency:        "immediate",
					Recommendation: "Fix now",
					Created:        now,
				}}
			},
		},
		{
			name:          "planning complete",
			wantTrigger:   WakeTriggerPlanningComplete,
			planningPairs: map[string]bool{"code-planning-pair": true},
			mutateState: func(state *models.State) {
				task := testhelpers.BuildTaskByStatus("plan-1", models.TaskStatusMerged, now)
				task.RolePair = "code-planning-pair"
				task.Output = []models.OutputEntry{{Desc: "implement X", DoneWhen: "tests pass", Scope: "pkg/x"}}
				state.Tasks = []models.Task{task}
				state.Sprint.Scope.Planned = []string{"plan-1"}
			},
		},
		{
			name:              "many to one ready",
			wantTrigger:       WakeTriggerManyToOneReady,
			pipelineTerminals: []models.TaskStatus{models.TaskStatusMerged},
			m2oTransitions:    manyToOneTransitions,
			mutateState: func(state *models.State) {
				parentID := "epic-1"
				us1 := testhelpers.BuildTaskByStatus("us-1", models.TaskStatusMerged, now)
				us1.RolePair = "us-writing-pair"
				us1.ParentTask = &parentID
				us2 := testhelpers.BuildTaskByStatus("us-2", models.TaskStatusMerged, now)
				us2.RolePair = "us-writing-pair"
				us2.ParentTask = &parentID
				state.Tasks = []models.Task{us1, us2}
				state.Sprint.Scope.Planned = []string{"us-1", "us-2"}
			},
		},
		{
			name:        "coding complete",
			wantTrigger: WakeTriggerCodingComplete,
			mutateState: func(state *models.State) {
				state.Goal.BaseCommit = &baseCommit
				state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)}
				state.Sprint.Scope.Planned = []string{"task-1"}
			},
		},
		{
			name:        "sprint complete",
			wantTrigger: WakeTriggerSprintComplete,
			mutateState: func(state *models.State) {
				state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)}
				state.Sprint.Scope.Planned = []string{"task-1"}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			bb := newOrchestratorScipTestBlackboard(t, projectRoot, tt.mutateState)

			result := DetectOrchestratorWakeTriggers(mustReadState(t, bb), tt.pipelineTerminals, tt.planningPairs, tt.m2oTransitions)
			if result.Trigger != tt.wantTrigger {
				t.Fatalf("DetectOrchestratorWakeTriggers() trigger = %s, want %s", result.Trigger, tt.wantTrigger)
			}

			t.Setenv(scipsearch.EnvEnableScipSearch, "true")
			var events []string
			restore := replaceOrchestratorScipRefreshForTest(t, func(opts scipsearch.RefreshOptions) (scipsearch.RefreshResult, error) {
				events = append(events, "refresh")
				assertOrchestratorRefreshOptions(t, opts, projectRoot, []string{"go"})
				indexPath := writeProjectRootScipIndex(t, projectRoot)
				return scipsearch.RefreshResult{
					Successes: []scipsearch.IndexRef{{Language: "go", Path: indexPath}},
				}, nil
			})
			defer restore()

			strategy := &orchestratorStrategy{}
			if err := strategy.PreExecution(bb, SupervisorConfig{AgentID: "orchestrator-1", ProjectRoot: projectRoot}); err != nil {
				t.Fatalf("PreExecution() error = %v", err)
			}

			events = append(events, "read-state-for-prompt")
			if len(events) != 2 || events[0] != "refresh" || events[1] != "read-state-for-prompt" {
				t.Fatalf("events = %v, want refresh before prompt-state read", events)
			}
			if _, err := os.Stat(filepath.Join(projectRoot, ".liza", "scip", "go.scip")); err != nil {
				t.Fatalf("project-root index missing before prompt-state read: %v", err)
			}
		})
	}
}

func TestOrchestratorPreExecutionScipRefreshDisabledActivationNoOp(t *testing.T) {
	projectRoot := t.TempDir()
	bb := newOrchestratorScipTestBlackboard(t, projectRoot, nil)
	t.Setenv(scipsearch.EnvEnableScipSearch, "false")

	restore := replaceOrchestratorScipRefreshForTest(t, func(opts scipsearch.RefreshOptions) (scipsearch.RefreshResult, error) {
		t.Fatalf("orchestrator SCIP refresh called with disabled activation: %#v", opts)
		return scipsearch.RefreshResult{}, nil
	})
	defer restore()

	strategy := &orchestratorStrategy{}
	if err := strategy.PreExecution(bb, SupervisorConfig{AgentID: "orchestrator-1", ProjectRoot: projectRoot}); err != nil {
		t.Fatalf("PreExecution() error = %v", err)
	}
	assertNoScipIndexDir(t, projectRoot)
}

func TestOrchestratorPreExecutionScipRefreshEmptyAllowlistNoOp(t *testing.T) {
	projectRoot := t.TempDir()
	bb := newOrchestratorScipTestBlackboard(t, projectRoot, func(state *models.State) {
		state.Config.ScipSearch = nil
	})
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")

	restore := replaceOrchestratorScipRefreshForTest(t, func(opts scipsearch.RefreshOptions) (scipsearch.RefreshResult, error) {
		t.Fatalf("orchestrator SCIP refresh called with empty config.scip_search: %#v", opts)
		return scipsearch.RefreshResult{}, nil
	})
	defer restore()

	strategy := &orchestratorStrategy{}
	if err := strategy.PreExecution(bb, SupervisorConfig{AgentID: "orchestrator-1", ProjectRoot: projectRoot}); err != nil {
		t.Fatalf("PreExecution() error = %v", err)
	}
	assertNoScipIndexDir(t, projectRoot)
}

func TestOrchestratorPreExecutionScipRefreshFailureIsWarningOnly(t *testing.T) {
	projectRoot := t.TempDir()
	bb := newOrchestratorScipTestBlackboard(t, projectRoot, nil)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	logs := captureAgentLogsForTest(t)

	restore := replaceOrchestratorScipRefreshForTest(t, func(opts scipsearch.RefreshOptions) (scipsearch.RefreshResult, error) {
		assertOrchestratorRefreshOptions(t, opts, projectRoot, []string{"go"})
		return scipsearch.RefreshResult{
			Failures: []scipsearch.RefreshFailure{{Language: "go", Diagnostic: "indexer failed"}},
		}, nil
	})
	defer restore()

	strategy := &orchestratorStrategy{}
	if err := strategy.PreExecution(bb, SupervisorConfig{AgentID: "orchestrator-1", ProjectRoot: projectRoot}); err != nil {
		t.Fatalf("PreExecution() error = %v", err)
	}
	if got := mustReadState(t, bb).Agents["orchestrator-1"].Status; got != models.AgentStatusPlanning {
		t.Fatalf("agent status = %s, want %s", got, models.AgentStatusPlanning)
	}
	assertLogContains(t, logs, "Orchestrator SCIP indexer failed", "language=go", "diagnostic=\"indexer failed\"")
}

func TestOrchestratorPreExecutionScipRefreshErrorDoesNotPreventExecution(t *testing.T) {
	projectRoot := t.TempDir()
	bb := newOrchestratorScipTestBlackboard(t, projectRoot, nil)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	logs := captureAgentLogsForTest(t)

	restore := replaceOrchestratorScipRefreshForTest(t, func(opts scipsearch.RefreshOptions) (scipsearch.RefreshResult, error) {
		assertOrchestratorRefreshOptions(t, opts, projectRoot, []string{"go"})
		return scipsearch.RefreshResult{}, errors.New("git ls-files failed")
	})
	defer restore()

	strategy := &orchestratorStrategy{}
	if err := strategy.PreExecution(bb, SupervisorConfig{AgentID: "orchestrator-1", ProjectRoot: projectRoot}); err != nil {
		t.Fatalf("PreExecution() error = %v", err)
	}
	if got := mustReadState(t, bb).Agents["orchestrator-1"].Status; got != models.AgentStatusPlanning {
		t.Fatalf("agent status = %s, want %s", got, models.AgentStatusPlanning)
	}
	assertLogContains(t, logs, "Orchestrator SCIP refresh failed", "git ls-files failed")
}

func TestOrchestratorPreExecutionScipRefreshUsesProjectRootNotTaskWorktree(t *testing.T) {
	projectRoot := t.TempDir()
	taskWorktree := filepath.Join(projectRoot, ".worktrees", "task-1")
	if err := os.MkdirAll(filepath.Join(taskWorktree, ".liza", "scip"), 0o755); err != nil {
		t.Fatalf("MkdirAll(task worktree scip dir) error = %v", err)
	}
	bb := newOrchestratorScipTestBlackboard(t, projectRoot, func(state *models.State) {
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC())}
		state.Tasks[0].Worktree = testhelpers.StringPtr(".worktrees/task-1")
	})
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")

	restore := replaceOrchestratorScipRefreshForTest(t, func(opts scipsearch.RefreshOptions) (scipsearch.RefreshResult, error) {
		assertOrchestratorRefreshOptions(t, opts, projectRoot, []string{"go"})
		if opts.TargetRoot == taskWorktree {
			t.Fatalf("orchestrator refresh used task worktree path %q", opts.TargetRoot)
		}
		indexPath := writeProjectRootScipIndex(t, opts.TargetRoot)
		return scipsearch.RefreshResult{
			Successes: []scipsearch.IndexRef{{Language: "go", Path: indexPath}},
		}, nil
	})
	defer restore()

	strategy := &orchestratorStrategy{}
	if err := strategy.PreExecution(bb, SupervisorConfig{AgentID: "orchestrator-1", ProjectRoot: projectRoot}); err != nil {
		t.Fatalf("PreExecution() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".liza", "scip", "go.scip")); err != nil {
		t.Fatalf("project-root index missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(taskWorktree, ".liza", "scip", "go.scip")); !os.IsNotExist(err) {
		t.Fatalf("task-worktree index stat error = %v, want not exist", err)
	}
}

func newOrchestratorScipTestBlackboard(t *testing.T, projectRoot string, mutate func(*models.State)) *db.Blackboard {
	t.Helper()

	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	state.Config.ScipSearch = []string{"go"}
	state.Agents["orchestrator-1"] = models.Agent{
		Role:      "orchestrator",
		Status:    models.AgentStatusIdle,
		Terminal:  "terminal-1",
		Heartbeat: time.Now().UTC(),
	}
	if mutate != nil {
		mutate(state)
	}
	return testhelpers.WriteInitialState(t, statePath, state)
}

func writeProjectRootScipIndex(t *testing.T, projectRoot string) string {
	t.Helper()

	indexPath := filepath.Join(projectRoot, ".liza", "scip", "go.scip")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(index dir) error = %v", err)
	}
	if err := os.WriteFile(indexPath, []byte("index"), 0o644); err != nil {
		t.Fatalf("WriteFile(index) error = %v", err)
	}
	return indexPath
}

func mustReadState(t *testing.T, bb *db.Blackboard) *models.State {
	t.Helper()

	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	return state
}

func replaceOrchestratorScipRefreshForTest(t *testing.T, refresh func(scipsearch.RefreshOptions) (scipsearch.RefreshResult, error)) func() {
	t.Helper()

	previous := orchestratorScipRefresh
	orchestratorScipRefresh = refresh
	return func() {
		orchestratorScipRefresh = previous
	}
}

func captureAgentLogsForTest(t *testing.T) *bytes.Buffer {
	t.Helper()

	var logs bytes.Buffer
	previous := logger
	logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	t.Cleanup(func() {
		logger = previous
	})
	return &logs
}

func assertLogContains(t *testing.T, logs *bytes.Buffer, fragments ...string) {
	t.Helper()

	output := logs.String()
	for _, fragment := range fragments {
		if !strings.Contains(output, fragment) {
			t.Fatalf("log output %q does not contain %q", output, fragment)
		}
	}
}

func assertOrchestratorRefreshOptions(t *testing.T, opts scipsearch.RefreshOptions, projectRoot string, languages []string) {
	t.Helper()

	if opts.TargetRoot != projectRoot {
		t.Fatalf("TargetRoot = %q, want project root %q", opts.TargetRoot, projectRoot)
	}
	if opts.TargetKind != scipsearch.TargetKindProjectRoot {
		t.Fatalf("TargetKind = %q, want %q", opts.TargetKind, scipsearch.TargetKindProjectRoot)
	}
	if len(opts.ConfiguredLanguages) != len(languages) {
		t.Fatalf("ConfiguredLanguages = %v, want %v", opts.ConfiguredLanguages, languages)
	}
	for i, language := range languages {
		if opts.ConfiguredLanguages[i] != language {
			t.Fatalf("ConfiguredLanguages = %v, want %v", opts.ConfiguredLanguages, languages)
		}
	}
	if opts.GitFiles != nil {
		t.Fatalf("GitFiles override = %v, want nil", opts.GitFiles)
	}
	if opts.Runner != nil {
		t.Fatalf("Runner override = %v, want nil", opts.Runner)
	}
}

func assertNoScipIndexDir(t *testing.T, projectRoot string) {
	t.Helper()

	if _, err := os.Stat(filepath.Join(projectRoot, ".liza", "scip")); !os.IsNotExist(err) {
		t.Fatalf(".liza/scip stat error = %v, want not exist", err)
	}
}
