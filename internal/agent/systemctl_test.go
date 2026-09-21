package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/paths"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/testhelpers"
	"github.com/liza-mas/liza/internal/usage"
)

// TestAutoResumeAction tests the pure decision function for auto-resume.
func TestAutoResumeAction(t *testing.T) {
	tests := []struct {
		name       string
		autoResume bool
		status     models.SprintStatus
		want       models.SprintStatus
	}{
		{"off_checkpoint", false, models.SprintStatusCheckpoint, ""},
		{"off_completed", false, models.SprintStatusCompleted, ""},
		{"on_checkpoint", true, models.SprintStatusCheckpoint, models.SprintStatusCheckpoint},
		{"on_completed", true, models.SprintStatusCompleted, models.SprintStatusCompleted},
		{"on_in_progress", true, models.SprintStatusInProgress, ""},
		{"on_empty", true, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &models.State{
				Config: models.Config{AutoResume: tt.autoResume},
				Sprint: models.Sprint{Status: tt.status},
			}
			got := autoResumeAction(state)
			if got != tt.want {
				t.Errorf("autoResumeAction() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheckpointBlocksRole(t *testing.T) {
	tests := []struct {
		name     string
		trigger  string
		roleType string
		wantWait bool
	}{
		{"planning checkpoint allows doer", models.CheckpointTriggerPlanningComplete, "doer", false},
		{"planning checkpoint allows reviewer", models.CheckpointTriggerPlanningComplete, "reviewer", false},
		{"many-to-one checkpoint allows doer", models.CheckpointTriggerManyToOneReady, "doer", false},
		{"many-to-one checkpoint allows reviewer", models.CheckpointTriggerManyToOneReady, "reviewer", false},
		{"planning checkpoint blocks orchestrator", models.CheckpointTriggerPlanningComplete, "orchestrator", true},
		{"many-to-one checkpoint blocks orchestrator", models.CheckpointTriggerManyToOneReady, "orchestrator", true},
		{"planning checkpoint blocks unknown role", models.CheckpointTriggerPlanningComplete, "observer", true},
		{"manual checkpoint blocks doer", "", "doer", true},
		{"manual checkpoint blocks reviewer", "", "reviewer", true},
		{"manual checkpoint blocks orchestrator", "", "orchestrator", true},
		{"sprint-complete checkpoint blocks doer", models.CheckpointTriggerSprintComplete, "doer", true},
		{"sprint-complete checkpoint blocks reviewer", models.CheckpointTriggerSprintComplete, "reviewer", true},
		{"sprint-complete checkpoint blocks orchestrator", models.CheckpointTriggerSprintComplete, "orchestrator", true},
		{"non-checkpoint does not block unknown role", models.CheckpointTriggerPlanningComplete, "observer", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := models.SprintStatusCheckpoint
			if tt.name == "non-checkpoint does not block unknown role" {
				status = models.SprintStatusInProgress
			}
			state := &models.State{
				Sprint: models.Sprint{
					Status:            status,
					CheckpointTrigger: tt.trigger,
				},
			}
			gotWait, _ := checkpointBlocksRole(state, tt.roleType)
			if gotWait != tt.wantWait {
				t.Errorf("checkpointBlocksRole() wait = %v, want %v", gotWait, tt.wantWait)
			}
		})
	}
}

func TestWaitWhilePausedAutoResumePrecedesTransitionCheckpointRoleException(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.AutoResume = true
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.CheckpointTrigger = models.CheckpointTriggerPlanningComplete
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC()),
	}
	state.Sprint.Scope.Planned = []string{"task-1"}
	testhelpers.WriteInitialState(t, statePath, state)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := waitWhilePaused(ctx, tmpDir, "doer"); err != nil {
		t.Fatalf("waitWhilePaused() error = %v", err)
	}

	updated, err := db.For(statePath).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if updated.Sprint.Status != models.SprintStatusInProgress {
		t.Fatalf("sprint status = %s, want %s", updated.Sprint.Status, models.SprintStatusInProgress)
	}
}

func TestWaitWhilePausedAutoResumePreservesStoppedActiveHalt(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Anomalies = []models.Anomaly{
		{Timestamp: now, Reporter: "coder-1", Type: "retry_loop", Details: map[string]any{"error_pattern": "connection refused"}},
		{Timestamp: now.Add(time.Minute), Reporter: "coder-1", Type: "retry_loop", Details: map[string]any{"error_pattern": "connection refused"}},
		{Timestamp: now.Add(2 * time.Minute), Reporter: "coder-1", Type: "retry_loop", Details: map[string]any{"error_pattern": "connection refused"}},
	}
	testhelpers.WriteInitialState(t, statePath, state)

	if _, err := ops.Analyze(tmpDir); err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}
	if err := db.For(statePath).Modify(func(current *models.State) error {
		current.Config.AutoResume = true
		current.Sprint.Status = models.SprintStatusCheckpoint
		return nil
	}); err != nil {
		t.Fatalf("prepare stopped checkpoint state: %v", err)
	}
	if _, err := ops.Stop(tmpDir, "maintenance", "human"); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	before, err := db.For(statePath).Read()
	if err != nil {
		t.Fatalf("read before automatic resume: %v", err)
	}
	if before.CircuitBreaker.CurrentTrigger == nil || before.CircuitBreaker.CurrentResponse == nil {
		t.Fatalf("missing active HALT before automatic resume: %+v", before.CircuitBreaker)
	}
	if before.CircuitBreaker.CurrentResponse.ReportFile == "" {
		t.Fatal("active HALT has no report reference")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := waitWhilePaused(ctx, tmpDir, "doer"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waitWhilePaused() error = %v, want context deadline while HALT remains blocked", err)
	}

	after, err := db.For(statePath).Read()
	if err != nil {
		t.Fatalf("read after automatic resume attempt: %v", err)
	}
	if after.Config.Mode != models.SystemModeStopped {
		t.Errorf("mode = %s, want STOPPED", after.Config.Mode)
	}
	if !reflect.DeepEqual(after.CircuitBreaker.CurrentTrigger, before.CircuitBreaker.CurrentTrigger) {
		t.Errorf("current trigger changed: got %+v, want %+v", after.CircuitBreaker.CurrentTrigger, before.CircuitBreaker.CurrentTrigger)
	}
	if !reflect.DeepEqual(after.CircuitBreaker.CurrentResponse, before.CircuitBreaker.CurrentResponse) {
		t.Errorf("current response changed: got %+v, want %+v", after.CircuitBreaker.CurrentResponse, before.CircuitBreaker.CurrentResponse)
	}
	if !reflect.DeepEqual(after.CircuitBreaker.History, before.CircuitBreaker.History) {
		t.Errorf("circuit-breaker history changed: got %+v, want %+v", after.CircuitBreaker.History, before.CircuitBreaker.History)
	}
}

func TestWaitWhilePausedHardModesBlockTransitionCheckpointRoleException(t *testing.T) {
	tests := []struct {
		name string
		mode models.SystemMode
	}{
		{"paused mode", models.SystemModePaused},
		{"circuit breaker mode", models.SystemModeCircuitBreakerTripped},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

			state := testhelpers.CreateValidState()
			state.Config.Mode = tt.mode
			state.Sprint.Status = models.SprintStatusCheckpoint
			state.Sprint.CheckpointTrigger = models.CheckpointTriggerPlanningComplete
			testhelpers.WriteInitialState(t, statePath, state)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()

			if err := waitWhilePaused(ctx, tmpDir, "doer"); err == nil {
				t.Fatal("waitWhilePaused() error = nil, want context timeout while hard mode blocks")
			}
		})
	}
}

// TestIsGoalComplete tests the pure decision function for goal completion detection.
func TestIsGoalComplete(t *testing.T) {
	tests := []struct {
		name   string
		result *ops.ResumeResult
		want   bool
	}{
		{
			name: "goal_complete",
			result: &ops.ResumeResult{
				SprintAdvanced:      &ops.AdvanceSprintResult{CarriedTasks: nil},
				TransitionsExecuted: 0,
				TransitionError:     "",
			},
			want: true,
		},
		{
			name: "carried_tasks_remain",
			result: &ops.ResumeResult{
				SprintAdvanced:      &ops.AdvanceSprintResult{CarriedTasks: []string{"task-1"}},
				TransitionsExecuted: 0,
				TransitionError:     "",
			},
			want: false,
		},
		{
			name: "transitions_executed",
			result: &ops.ResumeResult{
				SprintAdvanced:      &ops.AdvanceSprintResult{CarriedTasks: nil},
				TransitionsExecuted: 2,
				TransitionError:     "",
			},
			want: false,
		},
		{
			name: "transition_error_not_goal_complete",
			result: &ops.ResumeResult{
				SprintAdvanced:      &ops.AdvanceSprintResult{CarriedTasks: nil},
				TransitionsExecuted: 0,
				TransitionError:     "failed to load pipeline config",
			},
			want: false,
		},
		{
			name: "no_sprint_advance",
			result: &ops.ResumeResult{
				SprintAdvanced:      nil,
				TransitionsExecuted: 0,
				TransitionError:     "",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isGoalComplete(tt.result)
			if got != tt.want {
				t.Errorf("isGoalComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEffectiveIntegrationCompletionConsumers(t *testing.T) {
	t.Run("all-terminal wake consumes authoritative projection and preserves priority", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		baseCommit := "base"
		state.Goal.BaseCommit = &baseCommit
		state.Tasks = []models.Task{
			testhelpers.BuildTaskByStatus("coding-1", models.TaskStatusMerged, time.Now().UTC()),
		}
		state.Sprint.Scope.Planned = []string{"coding-1"}

		requested := prompts.EffectiveIntegrationCompletion{
			WakeTrigger: "CODING_COMPLETE",
			Status:      "reconciliation_needed",
			RequestKeys: []string{"global:2", "slice:plan-a"},
		}
		result := DetectOrchestratorWakeTriggersWithIntegrationProjection(state, nil, nil, nil, requested)
		if result.Trigger != WakeTriggerCodingComplete || result.Count != 2 || !result.ShouldWake() {
			t.Fatalf("requested result = %#v, want actionable two-analysis reconciliation", result)
		}
		if result.Integration.Status != requested.Status || len(result.Integration.RequestKeys) != 2 {
			t.Fatalf("integration projection = %#v, want %#v", result.Integration, requested)
		}

		for _, tt := range []struct {
			name       string
			projection prompts.EffectiveIntegrationCompletion
			trigger    OrchestratorWakeTrigger
		}{
			{
				name:       "blocked",
				projection: prompts.EffectiveIntegrationCompletion{WakeTrigger: "INTEGRATION_BLOCKED", Status: "blocked", ReasonCode: "slice_blocked"},
				trigger:    WakeTriggerIntegrationBlocked,
			},
			{
				name:       "exhausted",
				projection: prompts.EffectiveIntegrationCompletion{WakeTrigger: "INTEGRATION_EXHAUSTED", Status: "exhausted", ReasonCode: "global_generations_exhausted"},
				trigger:    WakeTriggerIntegrationExhausted,
			},
			{
				name:       "malformed",
				projection: prompts.EffectiveIntegrationCompletion{WakeTrigger: "NOT_A_TRIGGER", Status: "unknown"},
				trigger:    WakeTriggerIntegrationUnavailable,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				got := DetectOrchestratorWakeTriggersWithIntegrationProjection(state, nil, nil, nil, tt.projection)
				if got.Trigger != tt.trigger || got.ShouldWake() {
					t.Fatalf("stable terminal result = %#v, want non-actionable %s", got, tt.trigger)
				}
			})
		}

		clean := prompts.EffectiveIntegrationCompletion{WakeTrigger: "SPRINT_COMPLETE", Status: "complete"}
		cleanResult := DetectOrchestratorWakeTriggersWithIntegrationProjection(state, nil, nil, nil, clean)
		if cleanResult.Trigger != WakeTriggerSprintComplete || !cleanResult.ShouldWake() {
			t.Fatalf("clean result = %#v, want actionable sprint completion", cleanResult)
		}

		blockedTask := testhelpers.BuildTaskByStatus("blocked-1", models.TaskStatusBlocked, time.Now().UTC())
		state.Tasks = append(state.Tasks, blockedTask)
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, blockedTask.ID)
		priority := DetectOrchestratorWakeTriggersWithIntegrationProjection(state, nil, nil, nil, requested)
		if priority.Trigger != WakeTriggerBlocked {
			t.Fatalf("non-integration priority trigger = %s, want %s", priority.Trigger, WakeTriggerBlocked)
		}

		projectRoot := t.TempDir()
		statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
		testhelpers.SetupPipelineConfig(t, projectRoot)
		state.Tasks = state.Tasks[:1]
		state.Sprint.Scope.Planned = []string{"coding-1"}
		testhelpers.WriteInitialState(t, statePath, state)

		originalDetector := orchestratorWaitForWorkDetector
		t.Cleanup(func() { orchestratorWaitForWorkDetector = originalDetector })
		detectorCalls := 0
		orchestratorWaitForWorkDetector = func(_ string, _ *models.State, _ []models.TaskStatus, _ map[string]bool, _ []ops.ManyToOneTransitionInfo) OrchestratorWakeResult {
			detectorCalls++
			return result
		}
		woke, err := (&orchestratorStrategy{}).WaitForWork(
			context.Background(), db.For(statePath), SupervisorConfig{ProjectRoot: projectRoot}, time.Millisecond, time.Second,
		)
		if err != nil || !woke || detectorCalls != 1 {
			t.Fatalf("production WaitForWork woke=%t detector_calls=%d error=%v, want true, 1, nil", woke, detectorCalls, err)
		}
	})

	t.Run("public reconciliation is idempotent across restart", func(t *testing.T) {
		projectRoot, statePath := newAgentReconciliationProject(t)
		projection := prompts.EffectiveIntegrationCompletion{
			WakeTrigger: "CODING_COMPLETE", Status: "reconciliation_needed", RequestKeys: []string{"global:1"},
		}
		bb := db.For(statePath)
		for attempt := 1; attempt <= 2; attempt++ {
			if err := reconcileEffectiveIntegrationOutcome(projectRoot, bb, projection, ops.ReconcileIntegrationAnalyses); err != nil {
				t.Fatalf("reconciliation attempt %d: %s", attempt, deepestTestError(err))
			}
		}
		state, err := bb.Read()
		if err != nil {
			t.Fatalf("read reconciled state: %v", err)
		}
		if err := verifyEffectiveIntegrationOutcome(state, projection, &ops.ReconcileIntegrationAnalysesResult{}); err != nil {
			t.Fatalf("restart membership verification: %v", err)
		}
	})

	t.Run("restart reconciliation accepts exact membership and rejects duplicates", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		analysis := testhelpers.BuildTaskByStatus("integration-global-2", models.TaskStatusReady, time.Now().UTC())
		analysis.IntegrationAnalysis = &models.IntegrationAnalysisMetadata{
			Key: "global:2", Phase: models.IntegrationAnalysisPhaseGlobal, Generation: 2, SourceCommit: "head-2",
		}
		state.Tasks = []models.Task{analysis}
		state.Sprint.Scope.Planned = []string{analysis.ID}
		projection := prompts.EffectiveIntegrationCompletion{
			WakeTrigger: "CODING_COMPLETE", Status: "reconciliation_needed", RequestKeys: []string{"global:2"},
		}

		if err := verifyEffectiveIntegrationOutcome(state, projection, &ops.ReconcileIntegrationAnalysesResult{}); err != nil {
			t.Fatalf("existing deterministic membership rejected after restart: %v", err)
		}
		state.Tasks = append(state.Tasks, analysis)
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, analysis.ID)
		if err := verifyEffectiveIntegrationOutcome(state, projection, &ops.ReconcileIntegrationAnalysesResult{}); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("duplicate membership error = %v, want duplicate rejection", err)
		}

		blocked := prompts.EffectiveIntegrationCompletion{WakeTrigger: "INTEGRATION_BLOCKED", Status: "blocked", ReasonCode: "slice_blocked"}
		if err := verifyEffectiveIntegrationOutcome(state, blocked, nil); err != nil {
			t.Fatalf("explicit blocked outcome rejected: %v", err)
		}
		exhausted := prompts.EffectiveIntegrationCompletion{WakeTrigger: "INTEGRATION_EXHAUSTED", Status: "exhausted", ReasonCode: "global_generations_exhausted"}
		if err := verifyEffectiveIntegrationOutcome(state, exhausted, nil); err != nil {
			t.Fatalf("explicit exhausted outcome rejected: %v", err)
		}
		waiting := prompts.EffectiveIntegrationCompletion{WakeTrigger: "INTEGRATION_WAITING", Status: "waiting", ReasonCode: "repair_pending"}
		if err := verifyEffectiveIntegrationOutcome(state, waiting, nil); err == nil {
			t.Fatal("waiting outcome accepted as completed orchestrator reconciliation")
		}
	})

	t.Run("automatic completion delegates to race-safe terminal stop", func(t *testing.T) {
		completeResume := &ops.ResumeResult{
			SprintAdvanced: &ops.AdvanceSprintResult{},
		}
		calls := 0
		if err := stopAfterCompletedResume("/project", &ops.ResumeResult{}, func(string, string) (*ops.ModeChangeResult, error) {
			calls++
			return nil, nil
		}); err != nil || calls != 0 {
			t.Fatalf("non-complete resume error=%v calls=%d, want nil and zero", err, calls)
		}

		projectRoot := t.TempDir()
		statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
		testhelpers.SetupPipelineConfig(t, projectRoot)
		state := testhelpers.CreateValidState()
		state.Config.AutoResume = true
		state.Sprint.Status = models.SprintStatusCompleted
		testhelpers.WriteInitialState(t, statePath, state)

		originalResume := resumeCompletedSprint
		originalStop := stopCompletedGoal
		t.Cleanup(func() {
			resumeCompletedSprint = originalResume
			stopCompletedGoal = originalStop
		})
		resumeCalls := 0
		stopCalls := 0
		resumeCompletedSprint = func(string, string) (*ops.ResumeResult, error) {
			resumeCalls++
			return completeResume, nil
		}
		stopCompletedGoal = func(string, string) (*ops.ModeChangeResult, error) {
			stopCalls++
			return &ops.ModeChangeResult{}, nil
		}
		if err := waitWhilePaused(context.Background(), projectRoot, "orchestrator"); !errors.Is(err, errGoalComplete) {
			t.Fatalf("production clean completion error = %v, want %v", err, errGoalComplete)
		}
		if resumeCalls != 1 || stopCalls != 1 {
			t.Fatalf("production completion calls resume=%d stop_for_goal_completion=%d, want 1 and 1", resumeCalls, stopCalls)
		}

		stopFailure := errors.New("clean current-HEAD precondition rejected")
		stopCompletedGoal = func(string, string) (*ops.ModeChangeResult, error) {
			stopCalls++
			return nil, stopFailure
		}
		if err := waitWhilePaused(context.Background(), projectRoot, "orchestrator"); !errors.Is(err, stopFailure) || errors.Is(err, errGoalComplete) {
			t.Fatalf("production rejected completion error = %v, want wrapped stop failure", err)
		}
		if resumeCalls != 2 || stopCalls != 2 {
			t.Fatalf("production rejected calls resume=%d stop_for_goal_completion=%d, want 2 and 2", resumeCalls, stopCalls)
		}

		cleanRoot := newAgentCleanCompletionProject(t, false)
		if err := stopAfterCompletedResume(cleanRoot, completeResume, ops.StopForGoalCompletion); !errors.Is(err, errGoalComplete) {
			t.Fatalf("current-HEAD clean completion error: %s", deepestTestError(err))
		}
		cleanState, err := db.For(filepath.Join(cleanRoot, paths.ProjectDirName(), "state.yaml")).Read()
		if err != nil || cleanState.Config.Mode != models.SystemModeStopped {
			t.Fatalf("clean completion state mode=%s error=%v", cleanState.Config.Mode, err)
		}

		staleRoot := newAgentCleanCompletionProject(t, true)
		if err := stopAfterCompletedResume(staleRoot, completeResume, ops.StopForGoalCompletion); err == nil || errors.Is(err, errGoalComplete) {
			t.Fatalf("stale clean evidence error = %v, want non-terminal precondition failure", err)
		}
		staleState, err := db.For(filepath.Join(staleRoot, paths.ProjectDirName(), "state.yaml")).Read()
		if err != nil || staleState.Config.Mode != models.SystemModeRunning {
			t.Fatalf("stale completion state mode=%s error=%v", staleState.Config.Mode, err)
		}
	})
}

func deepestTestError(err error) string {
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return err.Error()
		}
		err = unwrapped
	}
}

func newAgentReconciliationProject(t *testing.T) (string, string) {
	t.Helper()
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)

	baseCommit := testhelpers.MustGit(t, projectRoot, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(projectRoot, "change.go"), []byte("package fixture\n"), 0o644); err != nil {
		t.Fatalf("write coding change: %v", err)
	}
	testhelpers.MustGit(t, projectRoot, "add", "change.go")
	testhelpers.MustGit(t, projectRoot, "commit", "-m", "add coding change")
	mergeCommit := testhelpers.MustGit(t, projectRoot, "rev-parse", "HEAD")
	testhelpers.MustGit(t, projectRoot, "update-ref", "refs/heads/integration", mergeCommit)

	now := time.Now().UTC()
	plan := testhelpers.BuildTaskByStatus("plan-1", models.TaskStatusMerged, now)
	plan.Type = models.TaskTypePlanning
	plan.RolePair = "code-planning-pair"
	plan.Output = []models.OutputEntry{{Desc: "coding", DoneWhen: "coded", Scope: "change.go", SpecRef: "README.md"}}
	plan.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
	plan.ReviewCommit = testhelpers.StringPtr("plan-review")

	coding := testhelpers.BuildTaskByStatus("coding-1", models.TaskStatusMerged, now)
	coding.RolePair = "coding-pair"
	coding.ParentTask = testhelpers.StringPtr(plan.ID)
	coding.BaseCommit = &baseCommit
	coding.ReviewCommit = &mergeCommit
	coding.MergeCommit = &mergeCommit
	coding.Validation = []string{"go test ./..."}

	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	state.Goal.BaseCommit = &baseCommit
	state.Tasks = []models.Task{plan, coding}
	state.Sprint.Scope.Planned = []string{plan.ID, coding.ID}
	testhelpers.WriteInitialState(t, statePath, state)
	return projectRoot, statePath
}

func newAgentCleanCompletionProject(t *testing.T, stale bool) string {
	t.Helper()
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)
	head := testhelpers.MustGit(t, projectRoot, "rev-parse", "integration")

	analysis := testhelpers.BuildTaskByStatus("integration-global-1", models.TaskStatus("INTEGRATION_ANALYSIS_CLEAN"), time.Now().UTC())
	analysis.RolePair = "integration-pair"
	analysis.IntegrationAnalysis = &models.IntegrationAnalysisMetadata{
		Key: "global:1", Phase: models.IntegrationAnalysisPhaseGlobal, Generation: 1, SourceCommit: head,
	}
	analysis.ReviewCommit = testhelpers.StringPtr("global-report")
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
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
	testhelpers.WriteInitialState(t, statePath, state)

	if stale {
		if err := os.WriteFile(filepath.Join(projectRoot, "stale.txt"), []byte("stale\n"), 0o644); err != nil {
			t.Fatalf("write stale marker: %v", err)
		}
		testhelpers.MustGit(t, projectRoot, "add", "stale.txt")
		testhelpers.MustGit(t, projectRoot, "commit", "-m", "advance integration")
		newHead := testhelpers.MustGit(t, projectRoot, "rev-parse", "HEAD")
		testhelpers.MustGit(t, projectRoot, "update-ref", "refs/heads/integration", newHead)
	}
	return projectRoot
}

// TestIsSystemStopped tests the isSystemStopped helper function
func TestIsSystemStopped(t *testing.T) {
	tests := []struct {
		name         string
		stateMode    models.SystemMode
		wantStopped  bool
		wantReasonRe string
	}{
		{
			name:         "state-based STOPPED mode",
			stateMode:    models.SystemModeStopped,
			wantStopped:  true,
			wantReasonRe: "STOPPED",
		},
		{
			name:        "not stopped",
			stateMode:   models.SystemModeRunning,
			wantStopped: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			state.Config.Mode = tt.stateMode

			stopped, reason := isSystemStopped(state)

			if stopped != tt.wantStopped {
				t.Errorf("isSystemStopped() stopped = %v, want %v", stopped, tt.wantStopped)
			}

			if tt.wantStopped && tt.wantReasonRe != "" && !strings.Contains(reason, tt.wantReasonRe) {
				t.Errorf("isSystemStopped() reason = %q, should contain %q", reason, tt.wantReasonRe)
			}

			if !tt.wantStopped && reason != "" {
				t.Errorf("isSystemStopped() reason should be empty when not stopped, got %q", reason)
			}
		})
	}
}

// TestVerifyOrchestratorStateChanges_BlockedNotResolved verifies that
// the orchestrator validation accepts when blocked tasks remain unchanged (no-op exit)
func TestVerifyOrchestratorStateChanges_BlockedNotResolved(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()

	// State before: task is BLOCKED
	stateBefore := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Agents: map[string]models.Agent{
			"orchestrator-1": {Role: "orchestrator", Status: models.AgentStatusPlanning, Heartbeat: now},
		},
		Tasks: []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
		},
		Config: models.Config{IntegrationBranch: "main"},
	}

	// State after: task STILL BLOCKED (orchestrator couldn't resolve)
	stateAfter := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Agents: map[string]models.Agent{
			"orchestrator-1": {Role: "orchestrator", Status: models.AgentStatusIdle, Heartbeat: now},
		},
		Tasks: []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
		},
		Config: models.Config{IntegrationBranch: "main"},
	}

	testhelpers.WriteInitialState(t, statePath, stateAfter)

	bb := db.New(statePath)

	err := verifyOrchestratorStateChanges(bb, stateBefore, nil, nil, nil)
	if err != nil {
		t.Errorf("Expected no error for no-op BLOCKED exit (may require human intervention), got: %v", err)
	}
}

// TestVerifyOrchestratorStateChanges_HypothesisExhaustedNotResolved verifies that
// the orchestrator validation rejects when exhausted tasks remain claimable.
func TestVerifyOrchestratorStateChanges_HypothesisExhaustedNotResolved(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()

	// State before: task has 2+ failed_by (hypothesis exhausted)
	exhaustedTask := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
	exhaustedTask.FailedBy = []string{"coder-1", "coder-2"}
	stateBefore := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Agents: map[string]models.Agent{
			"orchestrator-1": {Role: "orchestrator", Status: models.AgentStatusPlanning, Heartbeat: now},
		},
		Tasks:  []models.Task{exhaustedTask},
		Config: models.Config{IntegrationBranch: "main"},
	}

	// State after: task STILL exhausted (orchestrator couldn't resolve)
	exhaustedTaskAfter := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
	exhaustedTaskAfter.FailedBy = []string{"coder-1", "coder-2"}
	stateAfter := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Agents: map[string]models.Agent{
			"orchestrator-1": {Role: "orchestrator", Status: models.AgentStatusIdle, Heartbeat: now},
		},
		Tasks:  []models.Task{exhaustedTaskAfter},
		Config: models.Config{IntegrationBranch: "main"},
	}

	testhelpers.WriteInitialState(t, statePath, stateAfter)

	bb := db.New(statePath)

	err := verifyOrchestratorStateChanges(bb, stateBefore, nil, nil, nil)
	if err == nil {
		t.Fatal("Expected error for no-op HYPOTHESIS_EXHAUSTED exit, got nil")
	}
	if !strings.Contains(err.Error(), "unresolved exhausted count didn't decrease") {
		t.Fatalf("error = %v, want unresolved exhausted count message", err)
	}
}

func TestVerifyOrchestratorStateChanges_HypothesisExhaustedBlocked(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()

	exhaustedTask := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
	exhaustedTask.FailedBy = []string{"coder-1", "coder-2"}
	stateBefore := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Agents: map[string]models.Agent{
			"orchestrator-1": {Role: "orchestrator", Status: models.AgentStatusPlanning, Heartbeat: now},
		},
		Tasks:  []models.Task{exhaustedTask},
		Config: models.Config{IntegrationBranch: "main"},
	}

	blockedTask := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	blockedTask.FailedBy = []string{"coder-1", "coder-2"}
	stateAfter := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Agents: map[string]models.Agent{
			"orchestrator-1": {Role: "orchestrator", Status: models.AgentStatusIdle, Heartbeat: now},
		},
		Tasks:  []models.Task{blockedTask},
		Config: models.Config{IntegrationBranch: "main"},
	}

	testhelpers.WriteInitialState(t, statePath, stateAfter)

	bb := db.New(statePath)

	err := verifyOrchestratorStateChanges(bb, stateBefore, nil, nil, nil)
	if err != nil {
		t.Errorf("Expected no error for blocked HYPOTHESIS_EXHAUSTED exit, got: %v", err)
	}
}

// TestVerifyOrchestratorStateChanges_IntegrationUnavailable verifies that
// post-run validation fails closed when authoritative progress cannot be read.
func TestVerifyOrchestratorStateChanges_IntegrationUnavailable(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	baseCommit := "abc123"

	// State before: all tasks terminal, base_commit set, no integration task
	stateBefore := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			Status:      models.GoalStatusInProgress,
			Created:     now,
			BaseCommit:  &baseCommit,
		},
		Agents: map[string]models.Agent{
			"orchestrator-1": {Role: "orchestrator", Status: models.AgentStatusPlanning, Heartbeat: now},
		},
		Sprint: models.Sprint{
			Number: 1,
			Status: models.SprintStatusInProgress,
			Scope:  models.SprintScope{Planned: []string{"task-1"}},
		},
		Tasks: []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
		},
		Config: models.Config{IntegrationBranch: "main"},
	}

	// State after: unchanged, with no Git repository from which to read integration HEAD.
	stateAfter := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			Status:      models.GoalStatusInProgress,
			Created:     now,
			BaseCommit:  &baseCommit,
		},
		Agents: map[string]models.Agent{
			"orchestrator-1": {Role: "orchestrator", Status: models.AgentStatusIdle, Heartbeat: now},
		},
		Sprint: models.Sprint{
			Number: 1,
			Status: models.SprintStatusInProgress,
			Scope:  models.SprintScope{Planned: []string{"task-1"}},
		},
		Tasks: []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
		},
		Config: models.Config{IntegrationBranch: "main"},
	}

	testhelpers.WriteInitialState(t, statePath, stateAfter)

	bb := db.New(statePath)

	err := verifyOrchestratorStateChanges(bb, stateBefore, nil, nil, nil)
	if err == nil {
		t.Error("Expected error when authoritative integration progress is unavailable")
	}
	if err != nil && !strings.Contains(err.Error(), "integration_progress_unavailable") {
		t.Errorf("Expected fail-closed integration progress error, got: %v", err)
	}
}

// TestVerifyOrchestratorStateChanges_ManyToOneReady verifies that
// MANY_TO_ONE_READY trigger passes verification when sprint is checkpointed.
func TestVerifyOrchestratorStateChanges_ManyToOneReady(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	checkpointAt := now
	parentID := "epic-1"

	m2oTransitions := []ops.ManyToOneTransitionInfo{
		{Name: "us-to-coding", SourceRolePair: "us-writing-pair"},
	}

	// Build MERGED us-writing-pair tasks sharing a parent
	us1 := testhelpers.BuildTaskByStatus("us-1", models.TaskStatusMerged, now)
	us1.RolePair = "us-writing-pair"
	us1.ParentTask = &parentID
	us2 := testhelpers.BuildTaskByStatus("us-2", models.TaskStatusMerged, now)
	us2.RolePair = "us-writing-pair"
	us2.ParentTask = &parentID

	// State before: complete m2o cohort, sprint in progress
	stateBefore := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Agents: map[string]models.Agent{
			"orchestrator-1": {Role: "orchestrator", Status: models.AgentStatusPlanning, Heartbeat: now},
		},
		Sprint: models.Sprint{
			Number: 1,
			Status: models.SprintStatusInProgress,
			Scope:  models.SprintScope{Planned: []string{"us-1", "us-2"}},
		},
		Tasks:  []models.Task{us1, us2},
		Config: models.Config{IntegrationBranch: "main"},
	}

	// State after: sprint checkpointed (orchestrator did its job)
	stateAfter := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Agents: map[string]models.Agent{
			"orchestrator-1": {Role: "orchestrator", Status: models.AgentStatusIdle, Heartbeat: now},
		},
		Sprint: models.Sprint{
			Number:   1,
			Status:   models.SprintStatusCheckpoint,
			Scope:    models.SprintScope{Planned: []string{"us-1", "us-2"}},
			Timeline: models.SprintTimeline{CheckpointAt: &checkpointAt},
		},
		Tasks:  []models.Task{us1, us2},
		Config: models.Config{IntegrationBranch: "main"},
	}

	testhelpers.WriteInitialState(t, statePath, stateAfter)
	bb := db.New(statePath)

	// Pipeline terminals: MERGED is terminal for this test
	pipelineTerminals := []models.TaskStatus{models.TaskStatusMerged}

	err := verifyOrchestratorStateChanges(bb, stateBefore, pipelineTerminals, nil, m2oTransitions)
	if err != nil {
		t.Errorf("Expected no error when sprint checkpointed for MANY_TO_ONE_READY trigger, got: %v", err)
	}
}

// TestSelfHealCheckpoint_SprintComplete verifies that selfHealCheckpoint
// creates a checkpoint when the orchestrator agent failed to do so.
func TestSelfHealCheckpoint_SprintComplete(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Sprint: models.Sprint{
			ID:     "sprint-1",
			Number: 1,
			Status: models.SprintStatusInProgress,
			Scope:  models.SprintScope{Planned: []string{"task-1"}},
			Timeline: models.SprintTimeline{
				Started:  now.Add(-1 * time.Hour),
				Deadline: now.Add(1 * time.Hour),
			},
		},
		Tasks: []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
		},
		Config:         models.Config{IntegrationBranch: "main"},
		CircuitBreaker: models.CircuitBreaker{Status: "OK"},
	}

	testhelpers.WriteInitialState(t, statePath, state)

	healed := selfHealCheckpoint(tmpDir, WakeTriggerSprintComplete)
	if !healed {
		t.Fatal("Expected selfHealCheckpoint to succeed for SPRINT_COMPLETE")
	}

	// Verify sprint is now at CHECKPOINT
	bb := db.New(statePath)
	after, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if after.Sprint.Status != models.SprintStatusCheckpoint {
		t.Errorf("Sprint status = %s, want CHECKPOINT", after.Sprint.Status)
	}
}

// TestSelfHealCheckpoint_AlreadyCheckpointed verifies that selfHealCheckpoint
// returns true when the sprint is already at CHECKPOINT.
func TestSelfHealCheckpoint_AlreadyCheckpointed(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	checkpointAt := now.Add(-10 * time.Second)
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Sprint: models.Sprint{
			ID:     "sprint-1",
			Number: 1,
			Status: models.SprintStatusCheckpoint,
			Scope:  models.SprintScope{Planned: []string{"task-1"}},
			Timeline: models.SprintTimeline{
				Started:      now.Add(-1 * time.Hour),
				Deadline:     now.Add(1 * time.Hour),
				CheckpointAt: &checkpointAt,
			},
		},
		Tasks: []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
		},
		Config:         models.Config{IntegrationBranch: "main"},
		CircuitBreaker: models.CircuitBreaker{Status: "OK"},
	}

	testhelpers.WriteInitialState(t, statePath, state)

	healed := selfHealCheckpoint(tmpDir, WakeTriggerSprintComplete)
	if !healed {
		t.Fatal("Expected selfHealCheckpoint to return true for already-checkpointed sprint")
	}
}

// TestSelfHealCheckpoint_ManyToOneReady verifies that selfHealCheckpoint
// handles the MANY_TO_ONE_READY trigger (same checkpoint-only pattern).
func TestSelfHealCheckpoint_ManyToOneReady(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Sprint: models.Sprint{
			ID:     "sprint-1",
			Number: 1,
			Status: models.SprintStatusInProgress,
			Scope:  models.SprintScope{Planned: []string{"task-1"}},
			Timeline: models.SprintTimeline{
				Started:  now.Add(-1 * time.Hour),
				Deadline: now.Add(1 * time.Hour),
			},
		},
		Tasks: []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
		},
		Config:         models.Config{IntegrationBranch: "main"},
		CircuitBreaker: models.CircuitBreaker{Status: "OK"},
	}

	testhelpers.WriteInitialState(t, statePath, state)

	healed := selfHealCheckpoint(tmpDir, WakeTriggerManyToOneReady)
	if !healed {
		t.Fatal("Expected selfHealCheckpoint to succeed for MANY_TO_ONE_READY")
	}

	bb := db.New(statePath)
	after, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if after.Sprint.Status != models.SprintStatusCheckpoint {
		t.Errorf("Sprint status = %s, want CHECKPOINT", after.Sprint.Status)
	}
}

// TestSelfHealCheckpoint_NonMechanicalTrigger verifies that selfHealCheckpoint
// does nothing for triggers that require LLM creativity.
func TestSelfHealCheckpoint_NonMechanicalTrigger(t *testing.T) {
	nonMechanical := []OrchestratorWakeTrigger{
		WakeTriggerInitialPlanning,
		WakeTriggerBlocked,
		WakeTriggerHypothesisExhausted,
		WakeTriggerImmediateDiscovery,
		WakeTriggerCodingComplete,
		WakeTriggerNone,
	}

	for _, trigger := range nonMechanical {
		t.Run(string(trigger), func(t *testing.T) {
			// projectRoot doesn't matter — function should return false before touching disk
			if selfHealCheckpoint("/nonexistent", trigger) {
				t.Errorf("Expected selfHealCheckpoint to return false for trigger %s", trigger)
			}
		})
	}
}

// TestOrchestratorProgressSignature verifies signature changes when state changes.
func TestOrchestratorProgressSignature(t *testing.T) {
	now := time.Now().UTC()

	base := &models.State{
		Sprint: models.Sprint{
			Status: models.SprintStatusInProgress,
			Number: 1,
			Scope:  models.SprintScope{Planned: []string{"task-1"}},
		},
		Tasks: []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
		},
	}

	baseSig := orchestratorProgressSignature(base)

	// Same state → same signature
	sameSig := orchestratorProgressSignature(base)
	if sameSig != baseSig {
		t.Errorf("Same state should produce same signature: got %q vs %q", sameSig, baseSig)
	}

	// Sprint status change → different signature
	withCheckpoint := &models.State{
		Sprint: models.Sprint{
			Status: models.SprintStatusCheckpoint,
			Number: 1,
			Scope:  models.SprintScope{Planned: []string{"task-1"}},
		},
		Tasks: []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
		},
	}
	if orchestratorProgressSignature(withCheckpoint) == baseSig {
		t.Error("Sprint status change should produce different signature")
	}

	// Sprint number change → different signature
	withNewSprint := &models.State{
		Sprint: models.Sprint{
			Status: models.SprintStatusInProgress,
			Number: 2,
			Scope:  models.SprintScope{Planned: []string{"task-1"}},
		},
		Tasks: []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
		},
	}
	if orchestratorProgressSignature(withNewSprint) == baseSig {
		t.Error("Sprint number change should produce different signature")
	}

	// Task count change → different signature
	withMoreTasks := &models.State{
		Sprint: models.Sprint{
			Status: models.SprintStatusInProgress,
			Number: 1,
			Scope:  models.SprintScope{Planned: []string{"task-1"}},
		},
		Tasks: []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
			testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, now),
		},
	}
	if orchestratorProgressSignature(withMoreTasks) == baseSig {
		t.Error("Task count change should produce different signature")
	}

	// Planned count change → different signature
	withMorePlanned := &models.State{
		Sprint: models.Sprint{
			Status: models.SprintStatusInProgress,
			Number: 1,
			Scope:  models.SprintScope{Planned: []string{"task-1", "task-2"}},
		},
		Tasks: []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
		},
	}
	if orchestratorProgressSignature(withMorePlanned) == baseSig {
		t.Error("Planned count change should produce different signature")
	}

	// Task status change (same count) → different signature
	// This catches the blocker: resolving a blocked task changes status distribution
	withBlockedResolved := &models.State{
		Sprint: models.Sprint{
			Status: models.SprintStatusInProgress,
			Number: 1,
			Scope:  models.SprintScope{Planned: []string{"task-1"}},
		},
		Tasks: []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
		},
	}
	if orchestratorProgressSignature(withBlockedResolved) == baseSig {
		t.Error("Task status distribution change should produce different signature")
	}

	// Discovery count change → different signature
	withDiscovery := &models.State{
		Sprint: models.Sprint{
			Status: models.SprintStatusInProgress,
			Number: 1,
			Scope:  models.SprintScope{Planned: []string{"task-1"}},
		},
		Tasks: []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
		},
		Discovered: []models.Discovery{
			{ID: "disc-1", Urgency: "immediate"},
		},
	}
	if orchestratorProgressSignature(withDiscovery) == baseSig {
		t.Error("Discovery count change should produce different signature")
	}
}

// TestOrchestratorSpinningTracker verifies spinning detection for orchestrator.
func TestOrchestratorSpinningTracker(t *testing.T) {
	tracker := newSpinningTracker()
	sig := "sprint:IN_PROGRESS:1:tasks:3:planned:3"

	// Same signature N times → count increases
	for i := 1; i <= 5; i++ {
		count := tracker.Track("orchestrator", sig)
		if count != i {
			t.Errorf("Track() = %d, want %d", count, i)
		}
	}

	// Different signature → resets
	count := tracker.Track("orchestrator", "sprint:CHECKPOINT:1:tasks:3:planned:3")
	if count != 1 {
		t.Errorf("Track() after signature change = %d, want 1", count)
	}
}

type interactiveUsageAgent struct {
	t        *testing.T
	started  time.Time
	exitCode int
	err      error
	calls    int
	sinkSet  bool
}

func (a *interactiveUsageAgent) Run(context.Context, LLMAgentRunRequest) (LLMAgentRunResult, error) {
	a.t.Fatal("interactive supervisor called Run")
	return LLMAgentRunResult{}, nil
}

func (a *interactiveUsageAgent) RunInteractive(ctx context.Context, req LLMAgentInteractiveRequest) (int, error) {
	a.calls++
	a.sinkSet = req.EventSink != nil
	// Interactive providers emit lifecycle events without task attribution or usage.
	// Leave event identities empty to verify attribution comes from the call site.
	emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
		Kind: LLMAgentEventStarted, Time: a.started,
	})
	emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
		Kind: LLMAgentEventCompleted, Time: a.started.Add(2 * time.Second),
		Payload: map[string]any{"exit_code": a.exitCode},
	})
	return a.exitCode, a.err
}

func TestExecuteAgentInteractiveUsageCapture(t *testing.T) {
	providerErr := errors.New("interactive provider failed")
	for _, tc := range []struct {
		name        string
		blockAppend bool
		exitCode    int
		err         error
	}{
		{name: "successful_turns"},
		{name: "failed_turns", exitCode: 7, err: providerErr},
		{name: "append_failure_preserves_success", blockAppend: true},
		{name: "append_failure_preserves_error", blockAppend: true, exitCode: 7, err: providerErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
			const taskID = "interactive-task"
			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, time.Now().UTC())}
			testhelpers.WriteInitialState(t, statePath, state)
			if tc.blockAppend {
				// A file at the store directory deterministically rejects appends.
				if err := os.WriteFile(usage.Dir(projectRoot), []byte("not a directory"), 0o600); err != nil {
					t.Fatalf("block usage store: %v", err)
				}
			}
			started := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
			fake := &interactiveUsageAgent{t: t, exitCode: tc.exitCode, err: tc.err}
			config := SupervisorConfig{
				ProjectRoot: projectRoot, StatePath: statePath,
				AgentID: "coder-interactive", Role: models.RoleCoder, CLIName: "codex",
				Interactive: true, LLMAgent: fake,
			}
			for turn := 0; turn < 2; turn++ {
				fake.started = started.Add(time.Duration(turn) * time.Minute)
				exitCode, output, err := executeAgent(context.Background(), config, "prompt", nil, taskID, state.Config)
				if exitCode != tc.exitCode || err != tc.err || output != "" {
					t.Fatalf("executeAgent = (%d, %q, %v), want (%d, empty, %v)", exitCode, output, err, tc.exitCode, tc.err)
				}
				if fake.calls != turn+1 {
					t.Fatalf("interactive calls = %d, want %d", fake.calls, turn+1)
				}
				if tc.blockAppend {
					if !fake.sinkSet {
						t.Fatal("append failure was not exercised: interactive request has no event sink")
					}
					// Assert the write failure directly: Windows can classify a
					// directory read of a regular file as an unavailable store.
					if appendErr := usage.Append(projectRoot, usage.Record{
						TaskID: taskID, Role: config.Role, AgentID: config.AgentID,
						SupervisorRunID: supervisorRunID(), Provider: config.CLIName, SessionID: taskID,
						StartedAt: fake.started, EndedAt: fake.started.Add(2 * time.Second),
						Provenance: usage.ProvenanceUnknown, ExitCode: tc.exitCode,
					}); appendErr == nil {
						t.Fatal("blocked usage store unexpectedly accepted an append")
					}
					if records, stats, _ := usage.Load(projectRoot, time.Time{}, time.Time{}); len(records) != 0 || stats.Records != 0 {
						t.Fatalf("blocked usage store contains records: records=%d stats=%+v", len(records), stats)
					}
					continue
				}
				// Load the real store: removing EventSink from executeAgent must fail here.
				records, stats, loadErr := usage.Load(projectRoot, time.Time{}, time.Time{})
				if loadErr != nil || !stats.Available || stats.DuplicatesCollapsed != 0 || stats.MalformedLines != 0 {
					t.Fatalf("usage store = %+v, error = %v; want available with no duplicate or malformed records", stats, loadErr)
				}
				if len(records) != turn+1 {
					t.Fatalf("records = %d, want one per interactive turn (%d)", len(records), turn+1)
				}
				for i, record := range records {
					if record.TaskID != taskID || record.Role != config.Role || record.AgentID != config.AgentID ||
						record.SupervisorRunID != supervisorRunID() || record.SupervisorRunID == "" ||
						record.Provider != config.CLIName || record.SessionID != taskID {
						t.Fatalf("incorrect supervisor attribution: %+v", record)
					}
					if record.Provenance != usage.ProvenanceUnknown {
						t.Fatalf("provenance = %q, want unknown, never terminal_authoritative", record.Provenance)
					}
					wantStart := started.Add(time.Duration(i) * time.Minute)
					if !record.StartedAt.Equal(wantStart) || !record.EndedAt.Equal(wantStart.Add(2*time.Second)) {
						t.Fatalf("incorrect turn interval: %+v", record)
					}
					if record.ExitCode != tc.exitCode {
						t.Fatalf("record exit code = %d, want %d", record.ExitCode, tc.exitCode)
					}
				}
			}
		})
	}
}

// newUsageSinkTestConfig builds a sink config over a throwaway project root.
func newUsageSinkTestConfig(t *testing.T, runID string) UsageSinkConfig {
	t.Helper()
	return UsageSinkConfig{
		ProjectRoot:     t.TempDir(),
		AgentID:         "coder-1",
		Role:            "coder",
		Provider:        "claude",
		SessionID:       "task-usage",
		TaskID:          "task-usage",
		SupervisorRunID: runID,
	}
}

// driveUsageTurn feeds one provider turn (started, optional usage, completed)
// through the sink exactly as cli_agent and acpx_agent emit it.
func driveUsageTurn(sink LLMAgentEventSink, cfg UsageSinkConfig, started time.Time, reported *LLMAgentUsage, exitCode int) {
	ctx := context.Background()
	base := LLMAgentEvent{BackendName: cfg.Provider, AgentID: cfg.AgentID, TaskID: cfg.TaskID, SessionID: cfg.SessionID}
	startEvent := base
	startEvent.Kind = LLMAgentEventStarted
	startEvent.Time = started
	sink.RecordLLMAgentEvent(ctx, startEvent)
	if reported != nil {
		usageEvent := base
		usageEvent.Kind = LLMAgentEventUsage
		usageEvent.Time = started.Add(time.Second)
		usageEvent.Payload = map[string]any{"usage": *reported}
		sink.RecordLLMAgentEvent(ctx, usageEvent)
	}
	completed := base
	completed.Kind = LLMAgentEventCompleted
	completed.Time = started.Add(2 * time.Second)
	completed.Payload = map[string]any{"exit_code": exitCode}
	sink.RecordLLMAgentEvent(ctx, completed)
}

func loadUsageRecordsForTest(t *testing.T, projectRoot string) []usage.Record {
	t.Helper()
	records, stats, err := usage.Load(projectRoot, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("usage.Load: %v", err)
	}
	if !stats.Available {
		t.Fatalf("usage store unavailable: %+v", stats)
	}
	return records
}

func TestSupervisorUsageSinkRecordsOneRecordPerRun(t *testing.T) {
	cfg := newUsageSinkTestConfig(t, "run-fixed-0001")
	started := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	reported := LLMAgentUsage{InputTokens: 120, OutputTokens: 9, CachedReadTokens: 4000, CachedWriteTokens: 30}

	driveUsageTurn(NewUsageEventSink(cfg), cfg, started, &reported, 0)

	records := loadUsageRecordsForTest(t, cfg.ProjectRoot)
	if len(records) != 1 {
		t.Fatalf("records = %d, want exactly one per provider turn: %+v", len(records), records)
	}
	got := records[0]
	want := usage.Record{
		SchemaVersion:    usage.SchemaVersion,
		RecordID:         usage.NewRecordID(cfg.AgentID, cfg.SupervisorRunID, cfg.SessionID, cfg.Provider, started),
		TaskID:           cfg.TaskID,
		Role:             cfg.Role,
		AgentID:          cfg.AgentID,
		SupervisorRunID:  cfg.SupervisorRunID,
		SessionID:        cfg.SessionID,
		Provider:         cfg.Provider,
		StartedAt:        started,
		EndedAt:          started.Add(2 * time.Second),
		FreshInputTokens: 120,
		CacheReadTokens:  4000,
		CacheWriteTokens: 30,
		OutputTokens:     9,
		Provenance:       usage.ProvenanceTerminalAuthoritative,
	}
	if !got.StartedAt.Equal(want.StartedAt) || !got.EndedAt.Equal(want.EndedAt) {
		t.Fatalf("interval = [%s, %s], want [%s, %s]", got.StartedAt, got.EndedAt, want.StartedAt, want.EndedAt)
	}
	got.StartedAt, got.EndedAt = want.StartedAt, want.EndedAt
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("record = %+v, want %+v", got, want)
	}

	// A second run of the same supervisor process appends a second record
	// carrying the same run identity.
	second := started.Add(time.Hour)
	driveUsageTurn(NewUsageEventSink(cfg), cfg, second, &reported, 0)
	records = loadUsageRecordsForTest(t, cfg.ProjectRoot)
	if len(records) != 2 {
		t.Fatalf("records after second run = %d, want 2: %+v", len(records), records)
	}
	for _, r := range records {
		if r.SupervisorRunID != cfg.SupervisorRunID {
			t.Fatalf("record %s supervisor_run_id = %q, want %q", r.RecordID, r.SupervisorRunID, cfg.SupervisorRunID)
		}
	}
	if records[0].RecordID == records[1].RecordID {
		t.Fatalf("two turns collapsed onto one record id %q", records[0].RecordID)
	}
}

func TestSupervisorUsageSinkRunID(t *testing.T) {
	first := supervisorRunID()
	if first == "" {
		t.Fatal("supervisorRunID() is empty; a record must never carry an empty identity")
	}
	if second := supervisorRunID(); second != first {
		t.Fatalf("supervisorRunID() = %q then %q, want one stable value per process", first, second)
	}
	if a, b := newSupervisorRunID(), newSupervisorRunID(); a == b || a == "" || b == "" {
		t.Fatalf("newSupervisorRunID() = %q and %q, want two distinct non-empty identities", a, b)
	}

	// Two sinks built in one process without an explicit id share the accessor's
	// value; an explicit id is honoured verbatim.
	fallback := newUsageSinkTestConfig(t, "")
	started := time.Date(2026, 9, 18, 11, 0, 0, 0, time.UTC)
	driveUsageTurn(NewUsageEventSink(fallback), fallback, started, &LLMAgentUsage{InputTokens: 1, OutputTokens: 1}, 0)
	driveUsageTurn(NewUsageEventSink(fallback), fallback, started.Add(time.Minute), &LLMAgentUsage{InputTokens: 1, OutputTokens: 1}, 0)
	records := loadUsageRecordsForTest(t, fallback.ProjectRoot)
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2: %+v", len(records), records)
	}
	for _, r := range records {
		if r.SupervisorRunID != first {
			t.Fatalf("empty SupervisorRunID recorded %q, want the process accessor value %q", r.SupervisorRunID, first)
		}
	}

	explicit := newUsageSinkTestConfig(t, "restarted-supervisor-0002")
	driveUsageTurn(NewUsageEventSink(explicit), explicit, started, &LLMAgentUsage{InputTokens: 1, OutputTokens: 1}, 0)
	records = loadUsageRecordsForTest(t, explicit.ProjectRoot)
	if len(records) != 1 || records[0].SupervisorRunID != explicit.SupervisorRunID {
		t.Fatalf("records = %+v, want one record carrying %q", records, explicit.SupervisorRunID)
	}
	// Two supervisor processes on one task therefore yield two record identities.
	if records[0].RecordID == usage.NewRecordID(explicit.AgentID, first, explicit.SessionID, explicit.Provider, started) {
		t.Fatal("records from two supervisor run identities share one record id")
	}
}

func TestSupervisorUsageSinkProvenance(t *testing.T) {
	cases := []struct {
		name     string
		reported *LLMAgentUsage
		want     usage.Provenance
	}{
		{"authoritative", &LLMAgentUsage{InputTokens: 10, OutputTokens: 2}, usage.ProvenanceTerminalAuthoritative},
		{"partial", &LLMAgentUsage{CachedReadTokens: 900}, usage.ProvenancePartial},
		{"no_usage_event", nil, usage.ProvenanceUnknown},
		{"empty_usage_object", &LLMAgentUsage{}, usage.ProvenanceUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newUsageSinkTestConfig(t, "run-provenance")
			started := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
			driveUsageTurn(NewUsageEventSink(cfg), cfg, started, tc.reported, 0)
			records := loadUsageRecordsForTest(t, cfg.ProjectRoot)
			if len(records) != 1 {
				t.Fatalf("records = %d, want 1: %+v", len(records), records)
			}
			if records[0].Provenance != tc.want {
				t.Fatalf("provenance = %q, want %q", records[0].Provenance, tc.want)
			}
		})
	}

	t.Run("completed_without_started", func(t *testing.T) {
		cfg := newUsageSinkTestConfig(t, "run-launch-gate-failure")
		sink := NewUsageEventSink(cfg)
		at := time.Date(2026, 9, 18, 13, 0, 0, 0, time.UTC)
		sink.RecordLLMAgentEvent(context.Background(), LLMAgentEvent{
			Kind: LLMAgentEventCompleted, Time: at, BackendName: cfg.Provider,
			AgentID: cfg.AgentID, TaskID: cfg.TaskID, SessionID: cfg.SessionID,
			Message: "launch gate rejected the start",
			Payload: map[string]any{"error": "launch gate rejected the start"},
		})
		records := loadUsageRecordsForTest(t, cfg.ProjectRoot)
		if len(records) != 1 {
			t.Fatalf("records = %d, want one record for the launch-gate failure: %+v", len(records), records)
		}
		got := records[0]
		if got.Provenance != usage.ProvenanceUnknown {
			t.Fatalf("provenance = %q, want %q", got.Provenance, usage.ProvenanceUnknown)
		}
		if !got.StartedAt.Equal(at) || !got.EndedAt.Equal(at) {
			t.Fatalf("interval = [%s, %s], want the zero-length interval at %s", got.StartedAt, got.EndedAt, at)
		}
	})

	t.Run("append_failure_leaves_run_result_unchanged", func(t *testing.T) {
		projectRoot := t.TempDir()
		// A regular file where the store directory belongs makes every append fail.
		if err := os.MkdirAll(paths.New(projectRoot).LizaDir(), 0o755); err != nil {
			t.Fatalf("create runtime dir: %v", err)
		}
		if err := os.WriteFile(usage.Dir(projectRoot), []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("block usage store: %v", err)
		}
		binDir := t.TempDir()
		testhelpers.WriteShellStub(t, filepath.Join(binDir, "gemini"), "#!/bin/sh\nprintf 'provider output\\n'\n")
		t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

		sink := NewUsageEventSink(UsageSinkConfig{
			ProjectRoot: projectRoot, AgentID: "coder-1", Role: "coder",
			Provider: "gemini", SessionID: "task-usage", TaskID: "task-usage",
			SupervisorRunID: "run-append-failure",
		})
		result, err := NewCLIAgent("").Run(context.Background(), LLMAgentRunRequest{
			BackendName: "gemini", AgentID: "coder-1", TaskID: "task-usage", SessionID: "task-usage",
			Prompt: "prompt body", ProjectRoot: projectRoot, EventSink: sink, LaunchGate: immediateLaunchGate,
		})
		if err != nil || result.ExitCode != 0 {
			t.Fatalf("Run = (%d, %v), want the unchanged successful result despite the failing store", result.ExitCode, err)
		}
		if !strings.Contains(result.Output, "provider output") {
			t.Fatalf("Output = %q, want the provider output unchanged", result.Output)
		}
		if _, stats, err := usage.Load(projectRoot, time.Time{}, time.Time{}); err == nil && stats.Records != 0 {
			t.Fatalf("blocked store reported %d records", stats.Records)
		}
	})
}
