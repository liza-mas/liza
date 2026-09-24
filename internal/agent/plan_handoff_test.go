package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D73: the orchestrator's PLANNING_COMPLETE turn dispositions each merged
// plan; automatic paths expand only passed plans.

func handoffPlanTask(id string, deps ...string) models.Task {
	task := testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, time.Now().UTC())
	task.RolePair = "code-planning-pair"
	task.DependsOn = deps
	task.Output = []models.OutputEntry{{Desc: "implement " + id, DoneWhen: "tests pass", Scope: "pkg/" + id, SpecRef: "README.md"}}
	return task
}

func withDisposition(task models.Task, verdict models.PlanCheckVerdict, ask string) models.Task {
	task.PlanCheck = &models.PlanCheck{Verdict: verdict, Ask: ask, By: "orchestrator-1", At: time.Now().UTC()}
	return task
}

func asReplanned(task models.Task) models.Task {
	task.TransitionsExecuted = map[string]bool{"replanned": true, "code-plan-to-coding": true}
	return task
}

func handoffState(tasks ...models.Task) *models.State {
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Agents["orchestrator-1"] = testhelpers.RegisteredTestAgent("orchestrator")
	state.Tasks = tasks
	for _, task := range tasks {
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
	}
	return state
}

func checkpointed(state *models.State) *models.State {
	at := time.Now().UTC()
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.CheckpointTrigger = models.CheckpointTriggerPlanningComplete
	state.Sprint.Timeline.CheckpointAt = &at
	return state
}

func TestVerifyPlanningCompleteTurn(t *testing.T) {
	root := t.TempDir()
	testhelpers.SetupLizaDir(t, root)

	autoResumedAfterCheckpoint := func(state *models.State) *models.State {
		checkpointed(state)
		state.Sprint.Status = models.SprintStatusInProgress
		state.Sprint.CheckpointTrigger = ""
		return state
	}
	lateArrival := handoffPlanTask("late")

	cases := []struct {
		name    string
		before  *models.State
		after   *models.State
		wantErr string
	}{
		{name: "review left undecided", before: handoffState(handoffPlanTask("a")), after: handoffState(handoffPlanTask("a")), wantErr: "a (no disposition)"},
		{name: "passed and checkpointed", before: handoffState(handoffPlanTask("a")), after: checkpointed(handoffState(withDisposition(handoffPlanTask("a"), models.PlanCheckPassed, "")))},
		{name: "checkpoint already auto-resumed", before: handoffState(handoffPlanTask("a")), after: autoResumedAfterCheckpoint(handoffState(withDisposition(handoffPlanTask("a"), models.PlanCheckPassed, "")))},
		{name: "passed without checkpoint", before: handoffState(handoffPlanTask("a")), after: handoffState(withDisposition(handoffPlanTask("a"), models.PlanCheckPassed, "")), wantErr: "no checkpoint"},
		{name: "held needs no checkpoint", before: handoffState(handoffPlanTask("a")), after: handoffState(withDisposition(handoffPlanTask("a"), models.PlanCheckHeld, "provision"))},
		{name: "replanned needs no checkpoint", before: handoffState(handoffPlanTask("a")), after: handoffState(asReplanned(handoffPlanTask("a")))},
		{
			name:   "all replanned with a late arrival",
			before: handoffState(handoffPlanTask("a")),
			after:  handoffState(asReplanned(handoffPlanTask("a")), lateArrival),
		},
		{
			name:    "reconciliation ignored",
			before:  handoffState(asReplanned(handoffPlanTask("a")), withDisposition(handoffPlanTask("b", "a"), models.PlanCheckPassed, "")),
			after:   checkpointed(handoffState(asReplanned(handoffPlanTask("a")), withDisposition(handoffPlanTask("b", "a"), models.PlanCheckPassed, ""))),
			wantErr: "b (upstream changed",
		},
		{
			name:   "reconciliation by hold",
			before: handoffState(asReplanned(handoffPlanTask("a")), withDisposition(handoffPlanTask("b", "a"), models.PlanCheckPassed, "")),
			after:  handoffState(asReplanned(handoffPlanTask("a")), withDisposition(handoffPlanTask("b", "a"), models.PlanCheckHeld, "upstream a replanned")),
		},
		{name: "ready plan needs its checkpoint", before: handoffState(withDisposition(handoffPlanTask("a"), models.PlanCheckPassed, "")), after: handoffState(withDisposition(handoffPlanTask("a"), models.PlanCheckPassed, "")), wantErr: "no checkpoint"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyPlanningCompleteTurn(root, tc.before, tc.after)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("verify = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("verify = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

// R2: a turn that decides nothing fails verification; the self-heal
// checkpoint, auto-resume and the next PreWork must still create no children.
func TestPlanningCompleteUndecidedTurnCreatesNoChildren(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := handoffState(handoffPlanTask("a"))
	state.Config.AutoResume = true
	testhelpers.WriteInitialState(t, statePath, state)

	bb := db.New(statePath)
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
	healed, _ := bb.Read()
	if healed.Sprint.Status != models.SprintStatusCheckpoint {
		t.Fatalf("sprint = %s after failed verification, want the self-heal checkpoint", healed.Sprint.Status)
	}
	if _, err := ops.AutoResume(root, "auto-resume"); err != nil {
		t.Fatalf("AutoResume: %v", err)
	}
	if _, err := strategy.PreWork(context.Background(), bb, config); err != nil {
		t.Fatalf("PreWork: %v", err)
	}
	after, _ := bb.Read()
	if plan := after.FindTask("a"); len(plan.TransitionsExecuted) != 0 || len(after.Tasks) != 1 {
		t.Fatalf("undecided plan expanded: transitions %v, %d tasks", plan.TransitionsExecuted, len(after.Tasks))
	}
	detCtx, err := ops.LoadDetectionContext(root)
	if err != nil {
		t.Fatal(err)
	}
	if countMergedPlanningTasksWithOutput(after, detCtx.PlanHandoff) != 1 {
		t.Error("undecided plan must stay eligible for its own review")
	}
}

// R5: a plan passed before a crash, with no checkpoint trigger and unrelated
// planning still active, re-wakes PLANNING_COMPLETE as checkpoint-only work.
func TestPassedPlanWithoutCheckpointRecoversAfterRestart(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	active := testhelpers.BuildTaskByStatus("b", models.TaskStatusCodePlanning, time.Now().UTC())
	active.RolePair = "code-planning-pair"
	state := handoffState(withDisposition(handoffPlanTask("a"), models.PlanCheckPassed, ""), active)
	testhelpers.WriteInitialState(t, statePath, state)

	detCtx, err := ops.LoadDetectionContext(root)
	if err != nil {
		t.Fatal(err)
	}
	persisted, _ := db.New(statePath).Read()
	if result := DetectOrchestratorWakeTriggers(persisted, detCtx.SprintTerminals, detCtx.PlanningPairs, detCtx.ManyToOneTransitions); result.Trigger != WakeTriggerPlanningComplete {
		t.Fatalf("wake = %s, want PLANNING_COMPLETE for the passed plan", result.Trigger)
	}
	_, instruction, err := prompts.RenderOrchestratorDashboard(persisted, root, "orchestrator-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(instruction, "READY — already passed") || !strings.Contains(instruction, "\n- a\n") || strings.Contains(instruction, "PLANS TO REVIEW") {
		t.Fatalf("passed plan must render checkpoint-only, not for review:\n%s", instruction)
	}
}

// R4/R5: a passed plan whose upstream was replanned renders for
// reconciliation, never as checkpoint-only, and a held plan does not wake.
func TestReconciliationAndHeldPlansRouting(t *testing.T) {
	root := t.TempDir()
	testhelpers.SetupLizaDir(t, root)
	state := handoffState(
		asReplanned(handoffPlanTask("a")),
		withDisposition(handoffPlanTask("b", "a"), models.PlanCheckPassed, ""),
		withDisposition(handoffPlanTask("h"), models.PlanCheckHeld, "inject credentials"),
	)
	_, instruction, err := prompts.RenderOrchestratorDashboard(state, root, "orchestrator-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(instruction, "PLANS TO RECONCILE") || !strings.Contains(instruction, "- b: upstream a was replanned") {
		t.Fatalf("b must render for reconciliation:\n%s", instruction)
	}
	if strings.Contains(instruction, "READY") || strings.Contains(instruction, "- h") {
		t.Fatalf("neither b nor the held plan may render checkpoint-only:\n%s", instruction)
	}

	detCtx, err := ops.LoadDetectionContext(root)
	if err != nil {
		t.Fatal(err)
	}
	held := handoffState(withDisposition(handoffPlanTask("h"), models.PlanCheckHeld, "inject credentials"))
	if result := DetectOrchestratorWakeTriggersForProject(root, held, detCtx.SprintTerminals, detCtx.PlanningPairs, detCtx.ManyToOneTransitions); result.ShouldWake() {
		t.Fatalf("a held-only sprint must wait for the human, woke %s", result.Trigger)
	}
}

// Code review R1: with only held work left, real checkpoint and auto-resume
// cycles never wake the orchestrator; unrelated work still does, and clearing
// the hold restores the review.
func TestHeldPlanWaitsForHumanAcrossAutoResume(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.CreateSpecFile(t, root, "vision.md", "# Vision\n") // plan-check validates state after its write
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# README\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := handoffState(withDisposition(handoffPlanTask("h"), models.PlanCheckHeld, "provision runtime"))
	state.Config.AutoResume = true
	testhelpers.WriteInitialState(t, statePath, state)
	bb := db.New(statePath)
	detCtx, err := ops.LoadDetectionContext(root)
	if err != nil {
		t.Fatal(err)
	}
	wake := func() OrchestratorWakeResult {
		t.Helper()
		current, err := bb.Read()
		if err != nil {
			t.Fatal(err)
		}
		return DetectOrchestratorWakeTriggersForProject(root, current, detCtx.SprintTerminals, detCtx.PlanningPairs, detCtx.ManyToOneTransitions)
	}

	for cycle := 0; cycle < 2; cycle++ {
		if result := wake(); result.ShouldWake() {
			t.Fatalf("cycle %d: held-only sprint woke %s", cycle, result.Trigger)
		}
		if _, err := ops.SprintCheckpoint(root, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := ops.AutoResume(root, "auto-resume"); err != nil {
			t.Fatal(err)
		}
	}

	if err := bb.Modify(func(s *models.State) error {
		s.Tasks = append(s.Tasks, handoffPlanTask("other"))
		s.Sprint.Scope.Planned = append(s.Sprint.Scope.Planned, "other")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if result := wake(); result.Trigger != WakeTriggerPlanningComplete || result.Count != 1 {
		t.Fatalf("unrelated plan: wake = %+v, want PLANNING_COMPLETE for it alone", result)
	}
	if err := bb.Modify(func(s *models.State) error {
		s.Tasks = s.Tasks[:1]
		s.Sprint.Scope.Planned = s.Sprint.Scope.Planned[:1]
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := ops.RecordPlanCheck(root, ops.PlanCheckInput{TaskID: "h", Action: ops.PlanCheckActionClear, ChangedBy: "human"}); err != nil {
		t.Fatal(err)
	}
	if result := wake(); result.Trigger != WakeTriggerPlanningComplete {
		t.Fatalf("after clear: wake = %s, want PLANNING_COMPLETE", result.Trigger)
	}
}
