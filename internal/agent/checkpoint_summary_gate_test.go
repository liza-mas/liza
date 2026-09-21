package agent

import (
	"context"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// checkpointStateAt writes a project whose sprint sits at a checkpoint with
// the steering-report obligation outstanding, and returns its root.
func checkpointStateAt(t *testing.T, at *time.Time, trigger string, autoResume bool) string {
	t.Helper()
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.Timeline.CheckpointAt = at
	state.Sprint.CheckpointTrigger = trigger
	state.Config.AutoResume = autoResume
	state.PendingCheckpointSummary = &models.PendingCheckpointSummary{At: *at, Trigger: trigger}

	if err := db.New(statePath).Write(state); err != nil {
		t.Fatalf("write state: %v", err)
	}
	return projectRoot
}

// pendingSummaryProject returns a project carrying an outstanding obligation
// and the blackboard over it.
func pendingSummaryProject(t *testing.T, at time.Time, trigger string) (string, *db.Blackboard) {
	t.Helper()
	projectRoot := checkpointStateAt(t, &at, trigger, false)
	return projectRoot, db.New(paths.New(projectRoot).StatePath())
}

// The blocker this replaces: emission used to hang off an orchestrator turn,
// but the pause gate parks the orchestrator at every checkpoint, so a
// checkpoint created from the TUI or CLI while it was idle — or one present
// when the supervisor started — never produced a report. Driving the real gate
// proves the emission is reachable with no turn at all.
func TestWaitWhilePaused_EmitsCheckpointSummaryWithoutAnOrchestratorTurn(t *testing.T) {
	at := time.Now().UTC()
	projectRoot := checkpointStateAt(t, &at, models.CheckpointTriggerSprintComplete, false)

	var calls int
	var gotPrompt string
	withFakeCheckpointSummaryRunner(t, func(_, _, prompt string, _ models.Config) error {
		calls++
		gotPrompt = prompt
		return nil
	})

	// The gate blocks at a checkpoint, so let it park and then give up.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = waitWhilePaused(ctx, projectRoot, "orchestrator")

	if calls != 1 {
		t.Fatalf("calls = %d, want 1 from the gate alone", calls)
	}
	if gotPrompt == "" {
		t.Error("emitted an empty prompt")
	}
}

// A restarted supervisor reaches the same gate on its first poll, so a
// checkpoint that predates the process is still summarized.
func TestWaitWhilePaused_EmitsForACheckpointThatPredatesTheProcess(t *testing.T) {
	at := time.Now().UTC().Add(-2 * time.Hour)
	projectRoot := checkpointStateAt(t, &at, models.CheckpointTriggerPlanningComplete, false)

	called := false
	withFakeCheckpointSummaryRunner(t, func(string, string, string, models.Config) error {
		called = true
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = waitWhilePaused(ctx, projectRoot, "orchestrator")

	if !called {
		t.Error("a checkpoint older than the process produced no report")
	}
}

// Only the orchestrator emits; otherwise every parked role would spawn a CLI
// for the same checkpoint.
func TestWaitWhilePaused_OnlyTheOrchestratorEmits(t *testing.T) {
	at := time.Now().UTC()
	projectRoot := checkpointStateAt(t, &at, models.CheckpointTriggerSprintComplete, false)

	called := false
	withFakeCheckpointSummaryRunner(t, func(string, string, string, models.Config) error {
		called = true
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = waitWhilePaused(ctx, projectRoot, "doer")

	if called {
		t.Error("a doer supervisor emitted a checkpoint summary")
	}
}

// The gate polls every few seconds while parked, and the work poll runs on
// every tick. One report per checkpoint, not one per poll — otherwise this
// reintroduces the repetition that moving off per-merge emission removed.
func TestMaybeEmitCheckpointSummary_OncePerCheckpoint(t *testing.T) {
	projectRoot, bb := pendingSummaryProject(t, time.Now().UTC(), models.CheckpointTriggerSprintComplete)

	var calls int
	withFakeCheckpointSummaryRunner(t, func(string, string, string, models.Config) error {
		calls++
		return nil
	})

	for range 5 {
		state, err := bb.Read()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		maybeEmitCheckpointSummary(bb, projectRoot, "orchestrator", state)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 across repeated polls", calls)
	}

	// A later checkpoint is a new steering point and gets its own report.
	if err := bb.Modify(func(s *models.State) error {
		s.PendingCheckpointSummary = &models.PendingCheckpointSummary{At: time.Now().UTC()}
		return nil
	}); err != nil {
		t.Fatalf("modify: %v", err)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	maybeEmitCheckpointSummary(bb, projectRoot, "orchestrator", state)
	if calls != 2 {
		t.Errorf("calls = %d, want 2 after a new checkpoint", calls)
	}
}

// An ordinary running sprint carries no obligation and must stay quiet — no
// emission and no repeated warning on every work poll.
func TestMaybeEmitCheckpointSummary_QuietWithNoPendingObligation(t *testing.T) {
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	bb := db.New(statePath)
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	if err := bb.Write(state); err != nil {
		t.Fatalf("write: %v", err)
	}

	called := false
	withFakeCheckpointSummaryRunner(t, func(string, string, string, models.Config) error {
		called = true
		return nil
	})

	for range 10 {
		maybeEmitCheckpointSummary(bb, projectRoot, "orchestrator", state)
	}
	if called {
		t.Error("emitted for a sprint that never reached a checkpoint")
	}
}

// The opt-out still governs the emission at its new site, and the obligation
// is consumed rather than left to accumulate.
func TestMaybeEmitCheckpointSummary_RespectsOptOut(t *testing.T) {
	projectRoot, bb := pendingSummaryProject(t, time.Now().UTC(), models.CheckpointTriggerSprintComplete)
	if err := bb.Modify(func(s *models.State) error {
		off := false
		s.Config.AutoCheckpointSummary = &off
		return nil
	}); err != nil {
		t.Fatalf("modify: %v", err)
	}

	called := false
	withFakeCheckpointSummaryRunner(t, func(string, string, string, models.Config) error {
		called = true
		return nil
	})

	state, err := bb.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	maybeEmitCheckpointSummary(bb, projectRoot, "orchestrator", state)
	if called {
		t.Error("runner fired despite auto_checkpoint_summary: false")
	}

	after, err := bb.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if after.PendingCheckpointSummary != nil {
		t.Error("obligation left outstanding while summaries are disabled")
	}
}

// Round-2 blocker 1: an orchestrator that is already idle sits in WaitForWork,
// not in the pause gate. A checkpoint suppresses the planning and completion
// wake triggers, so that wait runs to timeout and the supervisor exits — the
// gate is never reached. Driving the strategy's real WaitForWork proves the
// checkpoint is observed there, with no restart, resume or other work trigger.
func TestOrchestratorWaitForWork_EmitsForACheckpointCreatedWhileIdle(t *testing.T) {
	at := time.Now().UTC()
	projectRoot := checkpointStateAt(t, &at, models.CheckpointTriggerSprintComplete, false)
	statePath := paths.New(projectRoot).StatePath()

	called := false
	withFakeCheckpointSummaryRunner(t, func(string, string, string, models.Config) error {
		called = true
		return nil
	})

	// No wake trigger: a checkpointed sprint yields none, which is exactly the
	// condition that starves the gate.
	prev := orchestratorWaitForWorkDetector
	orchestratorWaitForWorkDetector = func(string, *models.State, []models.TaskStatus, map[string]bool, []ops.ManyToOneTransitionInfo) OrchestratorWakeResult {
		return OrchestratorWakeResult{Trigger: WakeTriggerNone}
	}
	t.Cleanup(func() { orchestratorWaitForWorkDetector = prev })

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	s := &orchestratorStrategy{}
	hasWork, _ := s.WaitForWork(ctx, db.New(statePath), SupervisorConfig{ProjectRoot: projectRoot},
		50*time.Millisecond, 200*time.Millisecond)

	if hasWork {
		t.Fatal("fixture produced work; the starved-wait condition was not reproduced")
	}
	if !called {
		t.Error("a checkpoint created while the orchestrator waited produced no report")
	}
}

// Round-2 blocker 2, round-3 correction: auto-resume is role-generic, so a
// doer or reviewer can carry a terminal checkpoint all the way through
// COMPLETED into a new sprint while the orchestrator is still busy.
// applySprintAdvance replaces Sprint wholesale — its timeline keeps only
// Started — so an emitter keyed on Sprint.Timeline.CheckpointAt finds nothing
// left to report on. The obligation is durable and outside Sprint precisely
// so it survives that rollover.
func TestMaybeEmitCheckpointSummary_SurvivesSprintRollover(t *testing.T) {
	projectRoot, bb := pendingSummaryProject(t, time.Now().UTC(), models.CheckpointTriggerSprintComplete)

	var calls int
	withFakeCheckpointSummaryRunner(t, func(string, string, string, models.Config) error {
		calls++
		return nil
	})

	// A doer reaches the gate first: it must not emit.
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	maybeEmitCheckpointSummary(bb, projectRoot, "doer", state)
	if calls != 0 {
		t.Fatalf("calls = %d, want 0; a doer emitted", calls)
	}

	// Its auto-resume advances the sprint: the checkpoint's own identity is
	// destroyed, exactly as applySprintAdvance does it.
	if err := bb.Modify(func(s *models.State) error {
		s.Sprint = models.Sprint{
			ID:       "sprint-2",
			Number:   s.Sprint.Number + 1,
			GoalRef:  s.Goal.ID,
			Timeline: models.SprintTimeline{Started: time.Now().UTC()},
			Status:   models.SprintStatusInProgress,
		}
		return nil
	}); err != nil {
		t.Fatalf("modify: %v", err)
	}

	rolled, err := bb.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if rolled.Sprint.Timeline.CheckpointAt != nil {
		t.Fatal("fixture did not clear CheckpointAt; the rollover was not reproduced")
	}

	maybeEmitCheckpointSummary(bb, projectRoot, "orchestrator", rolled)
	if calls != 1 {
		t.Errorf("calls = %d, want 1; the terminal checkpoint lost its report to the rollover", calls)
	}
}

// Round-4 blocker: another role can auto-resume a terminal checkpoint through
// COMPLETED into a new sprint and stop the completed goal. isSystemStopped is
// checked before checkWork on every wait branch, so from that moment the
// orchestrator's in-loop observation points are unreachable, and no later
// orchestrator will run to pick the obligation up. This drives the real
// supervisor lifecycle from that state and requires the report before exit.
func TestRunSupervisor_EmitsPendingCheckpointSummaryBeforeFinalShutdown(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)

	at := time.Now().UTC()
	state := testhelpers.CreateValidState()
	// The state another role's goal-complete auto-resume leaves behind: the
	// sprint has rolled past the checkpoint and the system is STOPPED, but the
	// obligation the checkpoint recorded is still outstanding.
	state.Config.Mode = models.SystemModeStopped
	state.Config.CoderPollInterval = 1
	state.Config.DoerMaxWait = 1
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.Timeline.CheckpointAt = nil
	state.PendingCheckpointSummary = &models.PendingCheckpointSummary{
		At:      at,
		Trigger: models.CheckpointTriggerSprintComplete,
	}
	testhelpers.WriteInitialState(t, statePath, state)

	var calls int
	withFakeCheckpointSummaryRunner(t, func(string, string, string, models.Config) error {
		calls++
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := RunSupervisor(ctx, SupervisorConfig{
		AgentID:     "orchestrator-1",
		Role:        "orchestrator",
		ProjectRoot: projectRoot,
		StatePath:   statePath,
		CLIName:     "codex",
		LLMAgent:    &MockLLMAgent{ExitCode: 0},
	}); err != nil {
		t.Fatalf("RunSupervisor: %v", err)
	}

	if calls != 1 {
		t.Errorf("calls = %d, want 1 report before the run's final shutdown", calls)
	}

	bb := db.New(statePath)
	after, err := bb.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if after.PendingCheckpointSummary != nil {
		t.Error("obligation still outstanding after the supervisor exited")
	}
}
