package agent

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D-49 gap: a PLANNING_COMPLETE turn that leaves a wake-time plan undecided
// must not re-wake the same unchanged plan; only new input re-admits it.

// undecidedTurn runs the PostExecution of a PLANNING_COMPLETE turn that
// changed nothing, through a fresh strategy as after a supervisor restart.
func undecidedTurn(t *testing.T, root string, bb *db.Blackboard) {
	t.Helper()
	before, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	strategy, err := NewRoleStrategy("orchestrator", testResolver(t))
	if err != nil {
		t.Fatal(err)
	}
	authority := models.AgentAuthority{ID: "orchestrator-1", Generation: testhelpers.TestAgentGeneration}
	config := SupervisorConfig{AgentID: authority.ID, Authority: authority, ProjectRoot: root}
	if err := strategy.PostExecution(bb, config, "", "", before); err != nil {
		t.Fatalf("PostExecution: %v", err)
	}
}

// undecidedCycle is undecidedTurn followed by what an auto-resume run does
// with any checkpoint the turn left: resume it and run the next PreWork.
func undecidedCycle(t *testing.T, root string, bb *db.Blackboard) {
	t.Helper()
	undecidedTurn(t, root, bb)
	autoResumeCheckpoint(t, root, bb)
}

// autoResumeCheckpoint auto-resumes a pending checkpoint and runs the next PreWork.
func autoResumeCheckpoint(t *testing.T, root string, bb *db.Blackboard) {
	t.Helper()
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.Sprint.Status != models.SprintStatusCheckpoint {
		return
	}
	if _, err := ops.AutoResume(root, "auto-resume"); err != nil {
		t.Fatal(err)
	}
	strategy, err := NewRoleStrategy("orchestrator", testResolver(t))
	if err != nil {
		t.Fatal(err)
	}
	authority := models.AgentAuthority{ID: "orchestrator-1", Generation: testhelpers.TestAgentGeneration}
	if _, err := strategy.PreWork(context.Background(), bb, SupervisorConfig{AgentID: authority.ID, Authority: authority, ProjectRoot: root}); err != nil {
		t.Fatal(err)
	}
}

func persistedWake(t *testing.T, root string, bb *db.Blackboard) OrchestratorWakeResult {
	t.Helper()
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	detCtx, err := ops.LoadDetectionContext(root)
	if err != nil {
		t.Fatal(err)
	}
	return DetectOrchestratorWakeTriggersForProject(root, state, detCtx.SprintTerminals, detCtx.PlanningPairs, detCtx.ManyToOneTransitions)
}

func dispositionAlerts(t *testing.T, root string) int {
	t.Helper()
	data, err := os.ReadFile(paths.New(root).AlertsLogPath())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Count(string(data), "PLAN DISPOSITION MISSING")
}

// undecidedProject persists a needs_review plan beside unrelated active work,
// so neither completion nor an empty sprint is in play.
func undecidedProject(t *testing.T) (string, *db.Blackboard) {
	t.Helper()
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	active := testhelpers.BuildTaskByStatus("active", models.TaskStatusCodePlanning, time.Now().UTC())
	active.RolePair = "code-planning-pair"
	state := handoffState(handoffPlanTask("a"), active)
	state.Config.AutoResume = true
	return root, testhelpers.WriteInitialState(t, statePath, state)
}

// R1: the undecided turn records its verdict: no self-heal checkpoint, one
// alert, and neither a restart nor a checkpoint/auto-resume/PreWork cycle
// re-wakes PLANNING_COMPLETE or expands the plan.
func TestUndecidedPlanningTurnDoesNotReWake(t *testing.T) {
	// GIVEN a persisted plan awaiting the orchestrator's disposition
	root, bb := undecidedProject(t)
	if result := persistedWake(t, root, bb); result.Trigger != WakeTriggerPlanningComplete {
		t.Fatalf("precondition: wake = %s, want PLANNING_COMPLETE", result.Trigger)
	}

	// WHEN a PLANNING_COMPLETE turn leaves it undecided
	undecidedTurn(t, root, bb)

	// THEN no checkpoint is made for it, one alert names it, and it does not re-wake
	state, _ := bb.Read()
	if state.Sprint.Status != models.SprintStatusInProgress {
		t.Fatalf("sprint = %s, want IN_PROGRESS: a checkpoint cannot expand an undispositioned plan", state.Sprint.Status)
	}
	if n := dispositionAlerts(t, root); n != 1 {
		t.Fatalf("PLAN DISPOSITION MISSING alerts = %d, want 1", n)
	}
	if result := persistedWake(t, root, bb); result.Trigger == WakeTriggerPlanningComplete {
		t.Fatalf("unchanged undecided plan re-woke %+v", result)
	}

	// WHEN the orchestrator checkpoints anyway (I-383) and the sprint auto-resumes
	if _, err := ops.SprintCheckpoint(root, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.AutoResume(root, "auto-resume"); err != nil {
		t.Fatal(err)
	}
	strategy, err := NewRoleStrategy("orchestrator", testResolver(t))
	if err != nil {
		t.Fatal(err)
	}
	authority := models.AgentAuthority{ID: "orchestrator-1", Generation: testhelpers.TestAgentGeneration}
	if _, err := strategy.PreWork(context.Background(), bb, SupervisorConfig{AgentID: authority.ID, Authority: authority, ProjectRoot: root}); err != nil {
		t.Fatal(err)
	}

	// THEN the plan is still unexpanded and still does not re-wake
	state, _ = bb.Read()
	if plan := state.FindTask("a"); len(plan.TransitionsExecuted) != 0 || len(state.Tasks) != 2 {
		t.Fatalf("undecided plan expanded: transitions %v, %d tasks", plan.TransitionsExecuted, len(state.Tasks))
	}
	if result := persistedWake(t, root, bb); result.Trigger == WakeTriggerPlanningComplete {
		t.Fatalf("checkpoint cycle re-woke the unchanged plan: %+v", result)
	}
	if n := dispositionAlerts(t, root); n != 1 {
		t.Fatalf("PLAN DISPOSITION MISSING alerts = %d after the cycle, want still 1", n)
	}
}

// R2: the stale-provider route (ADR-0189) wakes first, but an undecided turn
// bounds it like any other plan.
func TestUndecidedStaleProviderTurnDoesNotReWake(t *testing.T) {
	// GIVEN a refused stale-provider consumer routed for its replan or hold
	root, bb := staleRefusedConsumerProject(t, false)
	if result := persistedWake(t, root, bb); result.Trigger != WakeTriggerPlanningComplete {
		t.Fatalf("precondition: wake = %s, want PLANNING_COMPLETE", result.Trigger)
	}

	// WHEN the turn neither replans nor holds it
	undecidedCycle(t, root, bb)

	// THEN the unchanged consumer does not re-wake
	if result := persistedWake(t, root, bb); result.Trigger == WakeTriggerPlanningComplete {
		t.Fatalf("unchanged stale consumer re-woke %+v", result)
	}
}

// R3: a suppressed childless plan keeps the sprint open on the wake path and
// on the resume path.
func TestUndecidedPlanKeepsSprintOpen(t *testing.T) {
	// GIVEN an undecided plan that is the sprint's only task
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := handoffState(handoffPlanTask("a"))
	state.Config.AutoResume = true
	bb := testhelpers.WriteInitialState(t, statePath, state)

	// WHEN a turn leaves it undecided
	undecidedCycle(t, root, bb)

	// THEN completion does not wake past it
	if result := persistedWake(t, root, bb); result.ShouldWake() {
		t.Fatalf("wake = %+v, want none while the plan awaits a disposition", result)
	}

	// WHEN a sprint-complete checkpoint is attempted anyway
	_, err := ops.SprintCheckpoint(root, models.CheckpointTriggerSprintComplete)

	// THEN integration settlement refuses it: the plan's children are unborn
	// (the resume-side barrier is covered in ops)
	if err == nil || !strings.Contains(err.Error(), "planning_unsettled") {
		t.Fatalf("sprint-complete checkpoint = %v, want planning_unsettled refusal", err)
	}
	if state, _ := bb.Read(); state.Sprint.Status == models.SprintStatusCompleted || state.Sprint.Status == models.SprintStatusCheckpoint {
		t.Fatalf("sprint = %s past an undecided plan", state.Sprint.Status)
	}
}

// R4: new input re-admits the plan for one more turn; a second undecided turn
// suppresses it again; unrelated work does not re-admit it.
func TestUndecidedPlanReadmittedByNewInput(t *testing.T) {
	// GIVEN a plan suppressed after an undecided turn
	root, bb := undecidedProject(t)
	undecidedCycle(t, root, bb)

	// WHEN unrelated work merges
	if err := bb.Modify(func(s *models.State) error {
		other := testhelpers.BuildTaskByStatus("unrelated", models.TaskStatusMerged, time.Now().UTC())
		s.Tasks = append(s.Tasks, other)
		s.Sprint.Scope.Planned = append(s.Sprint.Scope.Planned, other.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// THEN the plan stays suppressed
	if result := persistedWake(t, root, bb); result.Trigger == WakeTriggerPlanningComplete {
		t.Fatalf("unrelated merge re-admitted the plan: %+v", result)
	}

	// WHEN the operator asks for a disposition and the note's own turn consumes it
	if _, err := ops.AddHumanNote(root, "a", "pass, hold or replan a"); err != nil {
		t.Fatal(err)
	}
	if result := persistedWake(t, root, bb); result.Trigger != WakeTriggerHumanNote {
		t.Fatalf("wake = %s, want HUMAN_NOTE first", result.Trigger)
	}
	undecidedCycle(t, root, bb)

	// THEN the plan is re-admitted for one PLANNING_COMPLETE turn
	if result := persistedWake(t, root, bb); result.Trigger != WakeTriggerPlanningComplete {
		t.Fatalf("wake after the note = %s, want PLANNING_COMPLETE", result.Trigger)
	}

	// WHEN that turn also leaves it undecided
	undecidedCycle(t, root, bb)

	// THEN a new observation suppresses it again, with its own alert
	if result := persistedWake(t, root, bb); result.Trigger == WakeTriggerPlanningComplete {
		t.Fatalf("second undecided turn re-woke %+v", result)
	}
	if n := dispositionAlerts(t, root); n != 2 {
		t.Fatalf("PLAN DISPOSITION MISSING alerts = %d, want 2 (one per observation)", n)
	}
}
