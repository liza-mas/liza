package agent

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// An operator note wakes an idle orchestrator exactly once: the completed
// turn stamps it seen, and the detector goes quiet again.
func TestOrchestratorHumanNote_WakesOnceAndIsStampedAfterTurn(t *testing.T) {
	now := time.Now().UTC()
	projectRoot := t.TempDir()
	bb := newOrchestratorScipTestBlackboard(t, projectRoot, func(state *models.State) {
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)}
		state.Sprint.Scope.Planned = []string{"task-1"}
		state.HumanNotes = []models.HumanNote{{Timestamp: now, For: "all", Message: "apply the narrowing"}}
	})

	before := mustReadState(t, bb)
	if result := DetectOrchestratorWakeTriggers(before, nil, nil, nil); result.Trigger != WakeTriggerHumanNote || result.Count != 1 {
		t.Fatalf("trigger = %s (count %d), want HUMAN_NOTE/1", result.Trigger, result.Count)
	}

	strategy := &orchestratorStrategy{}
	if err := strategy.PostExecution(bb, orchestratorScipConfig(projectRoot), "", "", before); err != nil {
		t.Fatalf("PostExecution() error = %v", err)
	}

	after := mustReadState(t, bb)
	if got := ops.CountUnseenHumanNotes(after); got != 0 {
		t.Fatalf("unseen notes after turn = %d, want 0", got)
	}
	if result := DetectOrchestratorWakeTriggers(after, nil, nil, nil); result.Trigger == WakeTriggerHumanNote {
		t.Fatal("a seen note re-woke the orchestrator")
	}
}

// A turn woken by a higher-ranked trigger never renders note content, so it
// must not stamp the note: the note stays unseen and wakes HUMAN_NOTE next.
func TestOrchestratorHumanNote_HigherRankedTurnLeavesNoteUnseen(t *testing.T) {
	now := time.Now().UTC()
	projectRoot := t.TempDir()
	bb := newOrchestratorScipTestBlackboard(t, projectRoot, func(state *models.State) {
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)}
		state.Sprint.Scope.Planned = []string{"task-1"}
		state.HumanNotes = []models.HumanNote{{Timestamp: now, For: "all", Message: "apply the narrowing"}}
	})

	before := mustReadState(t, bb)
	if result := DetectOrchestratorWakeTriggers(before, nil, nil, nil); result.Trigger != WakeTriggerBlocked {
		t.Fatalf("trigger = %s, want BLOCKED_TASKS to outrank the note", result.Trigger)
	}

	strategy := &orchestratorStrategy{}
	if err := strategy.PostExecution(bb, orchestratorScipConfig(projectRoot), "", "", before); err != nil {
		t.Fatalf("PostExecution() error = %v", err)
	}

	if got := ops.CountUnseenHumanNotes(mustReadState(t, bb)); got != 1 {
		t.Fatalf("unseen notes after a BLOCKED_TASKS turn = %d, want 1 (note was never rendered)", got)
	}
}

// A note recorded while the HUMAN_NOTE turn was running was not in the
// rendered prompt, so the stamp must skip it.
func TestOrchestratorHumanNote_NoteAddedDuringTurnStaysUnseen(t *testing.T) {
	now := time.Now().UTC()
	projectRoot := t.TempDir()
	bb := newOrchestratorScipTestBlackboard(t, projectRoot, func(state *models.State) {
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)}
		state.Sprint.Scope.Planned = []string{"task-1"}
		state.HumanNotes = []models.HumanNote{{Timestamp: now, For: "all", Message: "rendered"}}
	})

	before := mustReadState(t, bb)
	if err := bb.Modify(func(state *models.State) error {
		state.HumanNotes = append(state.HumanNotes, models.HumanNote{Timestamp: now.Add(time.Second), For: "all", Message: "arrived mid-turn"})
		return nil
	}); err != nil {
		t.Fatalf("append mid-turn note: %v", err)
	}

	strategy := &orchestratorStrategy{}
	if err := strategy.PostExecution(bb, orchestratorScipConfig(projectRoot), "", "", before); err != nil {
		t.Fatalf("PostExecution() error = %v", err)
	}

	after := mustReadState(t, bb)
	if after.HumanNotes[0].SeenByOrchestrator() != true || after.HumanNotes[1].SeenByOrchestrator() != false {
		t.Fatalf("seen flags = [%v %v], want [true false]", after.HumanNotes[0].SeenByOrchestrator(), after.HumanNotes[1].SeenByOrchestrator())
	}
}

// The primary use of add-human-note: a note newer than a blocked task's
// assessment wakes BLOCKED_TASKS, whose instructions read the note before
// assessing again. That assessment consumes the note; it must not be
// re-delivered as a HUMAN_NOTE request afterwards.
func TestOrchestratorHumanNote_ConsumedByAssessmentIsNotRedelivered(t *testing.T) {
	for _, target := range []string{"task-1", "all"} {
		t.Run("for="+target, func(t *testing.T) {
			now := time.Now().UTC()
			orchestrator := "orchestrator-1"
			projectRoot := t.TempDir()
			bb := newOrchestratorScipTestBlackboard(t, projectRoot, func(state *models.State) {
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now.Add(-2*time.Hour))
				task.History = append(task.History, models.TaskHistoryEntry{
					Time: now.Add(-time.Hour), Event: models.TaskEventOrchestratorAssessment, Agent: &orchestrator,
				})
				state.Tasks = []models.Task{task}
				state.Sprint.Scope.Planned = []string{"task-1"}
				state.HumanNotes = []models.HumanNote{{Timestamp: now.Add(-time.Minute), For: target, Message: "retarget the blocker"}}
			})

			before := mustReadState(t, bb)
			if result := DetectOrchestratorWakeTriggers(before, nil, nil, nil); result.Trigger != WakeTriggerBlocked {
				t.Fatalf("trigger = %s, want BLOCKED_TASKS (note newer than assessment)", result.Trigger)
			}

			// The turn records a new assessment on the note's target.
			if err := bb.Modify(func(state *models.State) error {
				task := state.FindTask("task-1")
				task.History = append(task.History, models.TaskHistoryEntry{
					Time: now, Event: models.TaskEventOrchestratorAssessment, Agent: &orchestrator,
				})
				return nil
			}); err != nil {
				t.Fatalf("record assessment: %v", err)
			}

			strategy := &orchestratorStrategy{}
			if err := strategy.PostExecution(bb, orchestratorScipConfig(projectRoot), "", "", before); err != nil {
				t.Fatalf("PostExecution() error = %v", err)
			}

			after := mustReadState(t, bb)
			if got := ops.CountUnseenHumanNotes(after); got != 0 {
				t.Fatalf("unseen notes after the assessing turn = %d, want 0", got)
			}
			if result := DetectOrchestratorWakeTriggers(after, nil, nil, nil); result.Trigger != WakeTriggerNone {
				t.Fatalf("trigger after consumption = %s, want NONE", result.Trigger)
			}
		})
	}
}

// An assessment on some other task does not consume a note targeting a
// specific task.
func TestOrchestratorHumanNote_AssessmentOfOtherTaskDoesNotConsume(t *testing.T) {
	now := time.Now().UTC()
	orchestrator := "orchestrator-1"
	projectRoot := t.TempDir()
	bb := newOrchestratorScipTestBlackboard(t, projectRoot, func(state *models.State) {
		state.Tasks = []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
			testhelpers.BuildTaskByStatus("task-2", models.TaskStatusImplementing, now),
		}
		state.Sprint.Scope.Planned = []string{"task-1", "task-2"}
		state.HumanNotes = []models.HumanNote{{Timestamp: now, For: "task-2", Message: "for the other task"}}
	})
	before := mustReadState(t, bb)
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("task-1")
		task.History = append(task.History, models.TaskHistoryEntry{
			Time: now.Add(time.Second), Event: models.TaskEventOrchestratorAssessment, Agent: &orchestrator,
		})
		return nil
	}); err != nil {
		t.Fatalf("record assessment: %v", err)
	}

	strategy := &orchestratorStrategy{}
	if err := strategy.PostExecution(bb, orchestratorScipConfig(projectRoot), "", "", before); err != nil {
		t.Fatalf("PostExecution() error = %v", err)
	}
	if got := ops.CountUnseenHumanNotes(mustReadState(t, bb)); got != 1 {
		t.Fatalf("unseen notes = %d, want 1 (assessment was on a different task)", got)
	}
}
