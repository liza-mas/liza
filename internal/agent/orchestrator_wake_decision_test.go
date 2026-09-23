package agent

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/stacklit"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Drive the real supervisor, including its post-pre-execution state read,
// prompt and post-execution self-heal. The pre-execution hook is a controlled
// concurrent-state boundary: it makes each condition appear after wake
// selection, which the pre-loop gates would otherwise intercept.
func TestSupervisorOrchestratorRevalidatesSelectedWake(t *testing.T) {
	for _, scenario := range []string{"new blocker", "human note", "completed human note", "aborted human note", "consumed output", "paused", "manual checkpoint", "tripped", "stopped", "completed", "quota", "provider unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			testhelpers.SetupTestGitRepo(t, root)
			statePath, _ := testhelpers.SetupLizaDir(t, root)
			state := testhelpers.CreateValidState()
			state.Config.ScipSearch = []string{"go"}
			state.Config.OrchestratorPollInterval, state.Config.OrchestratorMaxWait = 1, 1
			plan := testhelpers.BuildTaskByStatus("plan", models.TaskStatusMerged, time.Now().UTC())
			plan.RolePair = "code-planning-pair"
			plan.Output = []models.OutputEntry{{Desc: "implement X", DoneWhen: "tests pass", Scope: "pkg/x"}}
			provider := testhelpers.BuildTaskByStatus("provider", models.TaskStatusReady, plan.Created)
			state.Tasks = []models.Task{plan, provider}
			state.Sprint.Scope.Planned = []string{"plan", "provider"}
			wantTrigger := WakeTriggerPlanningComplete
			if strings.Contains(scenario, "human note") {
				state.HumanNotes = []models.HumanNote{{For: "all", Message: "Inspect the run", Timestamp: time.Now().UTC()}}
				wantTrigger = WakeTriggerHumanNote
			}
			bb := testhelpers.WriteInitialState(t, statePath, state)
			det, err := ops.LoadDetectionContext(root)
			if err != nil {
				t.Fatal(err)
			}
			if got := DetectOrchestratorWakeTriggers(state, det.SprintTerminals, det.PlanningPairs, det.ManyToOneTransitions); got.Trigger != wantTrigger {
				t.Fatalf("fixture wake = %s, want %s", got.Trigger, wantTrigger)
			}

			t.Setenv(scipsearch.EnvEnableScipSearch, "true")
			t.Setenv(stacklit.EnvEnableStacklit, "false")
			// Apply the condition after wake selection. Seeding it in the
			// initial state instead is intercepted by the pre-loop quota,
			// pause and checkpoint gates, which return or park before the
			// supervisor ever selects a wake.
			hookRuns := 0
			defer replaceOrchestratorPreExecutionHookForTest(t, func() error {
				hookRuns++
				return bb.Modify(func(s *models.State) error {
					switch scenario {
					case "new blocker", "human note":
						*s.FindTask("provider") = testhelpers.BuildTaskByStatus("provider", models.TaskStatusBlocked, plan.Created)
					case "consumed output":
						s.FindTask("plan").TransitionsExecuted = map[string]bool{"coding-plan-to-code": true}
					case "paused":
						s.Config.Mode = models.SystemModePaused
					case "manual checkpoint":
						s.Sprint.Status = models.SprintStatusCheckpoint
					case "tripped":
						s.Config.Mode = models.SystemModeCircuitBreakerTripped
					case "stopped":
						s.Config.Mode = models.SystemModeStopped
					case "completed", "completed human note":
						s.Sprint.Status = models.SprintStatusCompleted
					case "aborted human note":
						s.Sprint.Status = models.SprintStatusAborted
					case "quota":
						return WriteQuotaSignal(root, "codex", "test quota")
					case "provider unavailable":
						return WriteProviderUnavailableSignal(root, "codex", "test unavailable")
					}
					return nil
				})
			})()

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			previousGate := waitWhilePausedForSupervisor
			waitWhilePausedForSupervisor = func(ctx context.Context, root, role string) error {
				s := mustReadState(t, bb)
				if RolePauseReason(s, role) != "" {
					if s.Agents["orchestrator-1"].Status != models.AgentStatusIdle {
						t.Error("cancelled launch did not restore idle runtime status")
					}
					cancel() // The existing gate has control again; end this harness.
					return ctx.Err()
				}
				return previousGate(ctx, root, role)
			}
			t.Cleanup(func() { waitWhilePausedForSupervisor = previousGate })
			mock := &MockLLMAgent{ExitCode: 0}
			mock.OnExecute = func(_ context.Context, _, _, _, _ string, _ []string) error {
				cancel() // One provider turn; still runs real PostExecution.
				return nil
			}
			err = RunSupervisor(ctx, SupervisorConfig{
				AgentID: "orchestrator-1", Role: "orchestrator", ProjectRoot: root,
				StatePath: statePath, LogPath: filepath.Join(root, paths.ProjectDirName(), "log.yaml"),
				SpecsDir: filepath.Join(root, "specs"), CLIName: "codex", LLMAgent: mock,
			})
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if hookRuns != 1 {
				t.Fatalf("pre-execution hook runs = %d, want 1", hookRuns)
			}
			calls := mock.GetCalls()
			if scenario != "new blocker" && wantTrigger != WakeTriggerHumanNote {
				if len(calls) != 0 {
					t.Fatalf("launched %d provider turns after selected work disappeared", len(calls))
				}
				return
			}
			if len(calls) != 1 {
				t.Fatalf("provider calls = %d, want 1", len(calls))
			}
			if !strings.Contains(calls[0].Prompt, "WAKE TRIGGER: "+string(wantTrigger)) {
				t.Error("revalidation replaced the selected wake in the actual provider prompt")
			}
			after := mustReadState(t, bb)
			if wantTrigger == WakeTriggerHumanNote {
				if ops.CountUnseenHumanNotes(after) != 0 {
					t.Error("selected human-note turn failed to consume its rendered notes")
				}
				return
			}
			if after.Sprint.Status != models.SprintStatusCheckpoint || after.Sprint.CheckpointTrigger != models.CheckpointTriggerPlanningComplete {
				t.Errorf("missing planning self-heal: sprint=%s trigger=%s", after.Sprint.Status, after.Sprint.CheckpointTrigger)
			}
			if len(after.Tasks) != 2 || len(after.FindTask("plan").TransitionsExecuted) != 0 {
				t.Error("downstream tasks must wait for checkpoint resume")
			}
		})
	}
}

func TestRevalidateOrchestratorWakeDegradesWithoutDetectionContext(t *testing.T) {
	root := t.TempDir()
	bb := newOrchestratorScipTestBlackboard(t, root, func(state *models.State) {
		state.HumanNotes = []models.HumanNote{{For: "all", Message: "Inspect the run", Timestamp: time.Now().UTC()}}
	})
	config := orchestratorScipConfig(root)
	// No frozen pipeline at this root: exercise the real loader failure.
	config.ProjectRoot = t.TempDir()
	if _, err := ops.LoadDetectionContext(config.ProjectRoot); err == nil {
		t.Fatal("fixture must fail detection context loading")
	}
	strategy := &orchestratorStrategy{wake: &OrchestratorWakeResult{Trigger: WakeTriggerHumanNote, Count: 1}}
	launch, err := strategy.RevalidateWake(context.Background(), bb, mustReadState(t, bb), config)
	if err != nil || !launch || strategy.wake == nil || strategy.wake.Trigger != WakeTriggerHumanNote {
		t.Fatalf("context failure terminated valid human-note wake: launch=%v wake=%+v err=%v", launch, strategy.wake, err)
	}
}

func TestRevalidateOrchestratorWakeRefreshesOrReplacesDecision(t *testing.T) {
	state := testhelpers.CreateValidState()
	plan := testhelpers.BuildTaskByStatus("plan", models.TaskStatusMerged, time.Now().UTC())
	plan.RolePair = "code-planning-pair"
	plan.Output = []models.OutputEntry{{Desc: "implement", DoneWhen: "tests pass", Scope: "pkg/x"}}
	state.Tasks = []models.Task{plan, testhelpers.BuildTaskByStatus("blocked", models.TaskStatusBlocked, plan.Created)}
	state.Sprint.Scope.Planned = []string{"plan", "blocked"}
	pairs := map[string]bool{"code-planning-pair": true}
	selected := OrchestratorWakeResult{Trigger: WakeTriggerPlanningComplete, Count: 99}
	result, fresh := revalidateOrchestratorWake(state, selected, nil, pairs, nil, nil)
	if result.Trigger != WakeTriggerPlanningComplete || result.Count != 1 || fresh.Trigger != WakeTriggerBlocked {
		t.Fatalf("retained decision must refresh count: result=%+v fresh=%+v", result, fresh)
	}
	state.Tasks[0].Output = nil
	result, _ = revalidateOrchestratorWake(state, selected, nil, pairs, nil, nil)
	if result.Trigger != WakeTriggerBlocked {
		t.Fatalf("vanished selection must yield to available work: %+v", result)
	}
	state.Tasks = state.Tasks[:1]
	state.Sprint.Scope.Planned = []string{"plan"}
	state.Goal.BaseCommit = testhelpers.StringPtr("base")
	selected = OrchestratorWakeResult{Trigger: WakeTriggerCodingComplete, Count: 1}
	projection := prompts.EffectiveIntegrationCompletion{WakeTrigger: "INTEGRATION_WAITING", Status: "waiting"}
	result, _ = revalidateOrchestratorWake(state, selected, nil, pairs, nil, func() prompts.EffectiveIntegrationCompletion { return projection })
	if result.ShouldWake() || !reflect.DeepEqual(result.Integration, projection) {
		t.Fatalf("integration advancement must cancel obsolete coding-complete wake: %+v", result)
	}
}

func TestOrchestratorWakeSelectionIsOneTurn(t *testing.T) {
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	bb := newOrchestratorScipTestBlackboard(t, root, func(state *models.State) {
		plan := testhelpers.BuildTaskByStatus("plan", models.TaskStatusMerged, time.Now().UTC())
		plan.RolePair = "code-planning-pair"
		// Missing required code_plan_ref makes the transition fail, leaving output.
		plan.Output = []models.OutputEntry{{Desc: "implement X", DoneWhen: "tests pass", Scope: "pkg/x"}}
		state.Tasks = []models.Task{plan}
		state.Sprint.Scope.Planned = []string{"plan"}
	})
	config := orchestratorScipConfig(root)
	strategy := &orchestratorStrategy{resolver: testResolver(t)}
	ctx := context.Background()
	if work, err := strategy.WaitForWork(ctx, bb, config, time.Millisecond, time.Second); err != nil || !work {
		t.Fatalf("initial wait: work=%v err=%v", work, err)
	}
	if strategy.wake.Trigger != WakeTriggerPlanningComplete {
		t.Fatalf("selected %s", strategy.wake.Trigger)
	}
	if err := bb.Modify(func(s *models.State) error {
		s.Tasks = append(s.Tasks, testhelpers.BuildTaskByStatus("blocked", models.TaskStatusBlocked, time.Now().UTC()))
		s.Sprint.CheckpointTrigger = models.CheckpointTriggerPlanningComplete
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := strategy.PreWork(ctx, bb, config); err != nil {
		t.Fatal(err)
	}
	state := mustReadState(t, bb)
	if len(state.FindTask("plan").TransitionsExecuted) != 0 || len(state.Tasks) != 2 || state.Sprint.CheckpointTrigger != "" {
		t.Fatal("fixture must leave unsuccessful planning output after clearing the resumed trigger")
	}
	if work, err := strategy.WaitForWork(ctx, bb, config, time.Millisecond, time.Second); err != nil || !work {
		t.Fatalf("next wait: work=%v err=%v", work, err)
	}
	if strategy.wake.Trigger != WakeTriggerBlocked {
		t.Fatalf("failed handoff retained priority: %s, want BLOCKED_TASKS", strategy.wake.Trigger)
	}
	state.Config.Mode = models.SystemModePaused
	if launch, err := strategy.RevalidateWake(ctx, bb, state, config); err != nil || launch || strategy.wake != nil {
		t.Fatalf("cancelled selection leaked: launch=%v wake=%+v err=%v", launch, strategy.wake, err)
	}
}
