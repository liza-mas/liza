package ops

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func c6ResumedCheckpoint(t *testing.T) (*c6Fixture, models.Sprint) {
	t.Helper()
	now := time.Now().UTC()
	f := newC6Fixture(t, func(s *models.State) {
		s.Agents["orchestrator-1"] = c6LiveAgent("orchestrator", now)
		s.Sprint.Status = models.SprintStatusInProgress
		s.Sprint.CheckpointTrigger = models.CheckpointTriggerPlanningComplete
		at := now.Add(-time.Minute)
		s.Sprint.Timeline.CheckpointAt = &at
	})
	return f, f.read(t).Sprint
}

func TestC6ClearResumedTransitionCheckpointClearsOnlyTheObservedCheckpoint(t *testing.T) {
	f, observed := c6ResumedCheckpoint(t)

	if err := ClearResumedTransitionCheckpoint(f.bb, c6Authority("orchestrator-1"), observed); err != nil {
		t.Fatalf("ClearResumedTransitionCheckpoint: %v", err)
	}
	if got := f.read(t).Sprint.CheckpointTrigger; got != "" {
		t.Fatalf("checkpoint trigger = %q, want cleared for the same resumed checkpoint", got)
	}
}

func TestC6ClearResumedTransitionCheckpointKeepsAReplacedOrHardCheckpoint(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*models.State)
	}{
		{"different checkpoint time", func(s *models.State) {
			at := time.Now().UTC()
			s.Sprint.Timeline.CheckpointAt = &at
		}},
		{"different trigger", func(s *models.State) { s.Sprint.CheckpointTrigger = models.CheckpointTriggerManyToOneReady }},
		{"sprint no longer resumed", func(s *models.State) { s.Sprint.Status = models.SprintStatusCheckpoint }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, observed := c6ResumedCheckpoint(t)
			if err := f.bb.Modify(func(s *models.State) error { tc.mutate(s); return nil }); err != nil {
				t.Fatalf("seed replaced checkpoint: %v", err)
			}
			before := f.bytes(t)

			if err := ClearResumedTransitionCheckpoint(f.bb, c6Authority("orchestrator-1"), observed); err != nil {
				t.Fatalf("ClearResumedTransitionCheckpoint: %v", err)
			}
			f.requireUnchanged(t, before, "clearing a checkpoint the turn did not observe")
		})
	}

	hard, _ := c6ResumedCheckpoint(t)
	observed := hard.read(t).Sprint
	observed.CheckpointTrigger = models.CheckpointTriggerSprintComplete
	before := hard.bytes(t)
	if err := ClearResumedTransitionCheckpoint(hard.bb, c6Authority("orchestrator-1"), observed); err != nil {
		t.Fatalf("hard checkpoint: %v", err)
	}
	hard.requireUnchanged(t, before, "clearing against a non-transition checkpoint")
}

func TestC6ClearResumedTransitionCheckpointRefusesAStaleGeneration(t *testing.T) {
	f, observed := c6ResumedCheckpoint(t)
	before := f.bytes(t)

	stale := models.AgentAuthority{ID: "orchestrator-1", Generation: "replaced"}
	requireAuthorityError(t, ClearResumedTransitionCheckpoint(f.bb, stale, observed), "orchestrator-1")
	f.requireUnchanged(t, before, "clearing by a replaced generation")
}

// c6NotesFixture holds one assessable task and three notes: for it, for another
// task, and for all targets. A fourth note is appended after the prompt was built.
func c6NotesFixture(t *testing.T) (*c6Fixture, *models.State) {
	t.Helper()
	now := time.Now().UTC()
	f := newC6Fixture(t, func(s *models.State) {
		s.Agents["orchestrator-1"] = c6LiveAgent("orchestrator", now)
		s.Tasks = []models.Task{{ID: "task-1", Status: models.TaskStatusBlocked}, {ID: "task-2", Status: models.TaskStatusBlocked}}
		s.HumanNotes = []models.HumanNote{
			{Timestamp: now, Message: "for task-1", For: "task-1"},
			{Timestamp: now, Message: "for task-2", For: "task-2"},
			{Timestamp: now, Message: "for all", For: humanNoteForAll},
		}
	})
	return f, f.read(t)
}

func c6SeenNotes(state *models.State) []bool {
	seen := make([]bool, len(state.HumanNotes))
	for i := range state.HumanNotes {
		seen[i] = state.HumanNotes[i].SeenByOrchestrator()
	}
	return seen
}

func requireSeen(t *testing.T, state *models.State, want ...bool) {
	t.Helper()
	got := c6SeenNotes(state)
	if len(got) != len(want) {
		t.Fatalf("notes seen = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("notes seen = %v, want %v", got, want)
		}
	}
}

func TestC6ConsumeRenderedHumanNotesAllRenderedStampsEveryRenderedNote(t *testing.T) {
	f, before := c6NotesFixture(t)
	late := models.HumanNote{Timestamp: time.Now().UTC(), Message: "added during the turn", For: "task-1"}
	if err := f.bb.Modify(func(s *models.State) error { s.HumanNotes = append(s.HumanNotes, late); return nil }); err != nil {
		t.Fatalf("add late note: %v", err)
	}

	stamped, err := ConsumeRenderedHumanNotes(f.bb, c6Authority("orchestrator-1"), before, len(before.HumanNotes), true)
	if err != nil || stamped != 3 {
		t.Fatalf("ConsumeRenderedHumanNotes = (%d, %v), want 3 stamped", stamped, err)
	}
	requireSeen(t, f.read(t), true, true, true, false)
}

func TestC6ConsumeRenderedHumanNotesAssessmentConsumesItsTargetAndAll(t *testing.T) {
	f, before := c6NotesFixture(t)
	if err := f.bb.Modify(func(s *models.State) error {
		task := s.FindTask("task-1")
		task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventOrchestratorAssessment})
		return nil
	}); err != nil {
		t.Fatalf("record assessment: %v", err)
	}

	stamped, err := ConsumeRenderedHumanNotes(f.bb, c6Authority("orchestrator-1"), before, len(before.HumanNotes), false)
	if err != nil || stamped != 2 {
		t.Fatalf("ConsumeRenderedHumanNotes = (%d, %v), want the assessed target's note and the note for all", stamped, err)
	}
	requireSeen(t, f.read(t), true, false, true)
}

func TestC6ConsumeRenderedHumanNotesWithoutAssessmentStampsNothing(t *testing.T) {
	f, before := c6NotesFixture(t)
	bytesBefore := f.bytes(t)

	stamped, err := ConsumeRenderedHumanNotes(f.bb, c6Authority("orchestrator-1"), before, len(before.HumanNotes), false)
	if err != nil || stamped != 0 {
		t.Fatalf("ConsumeRenderedHumanNotes = (%d, %v), want nothing stamped without an assessment", stamped, err)
	}
	f.requireUnchanged(t, bytesBefore, "a turn that assessed nothing")
}

func TestC6ConsumeRenderedHumanNotesRefusesAStaleGeneration(t *testing.T) {
	f, before := c6NotesFixture(t)
	bytesBefore := f.bytes(t)

	stale := models.AgentAuthority{ID: "orchestrator-1", Generation: "replaced"}
	stamped, err := ConsumeRenderedHumanNotes(f.bb, stale, before, len(before.HumanNotes), true)
	requireAuthorityError(t, err, "orchestrator-1")
	if stamped != 0 {
		t.Fatalf("stamped = %d on a refused transaction, want 0", stamped)
	}
	f.requireUnchanged(t, bytesBefore, "consuming notes by a replaced generation")
}

func c6UndecidedFixture(t *testing.T) (*c6Fixture, PlanHandoffDomain, *models.State) {
	t.Helper()
	f := newC6Fixture(t, func(s *models.State) {
		*s = *undecidedState()
		s.Agents["orchestrator-1"] = c6LiveAgent("orchestrator", time.Now().UTC())
	})
	domain, err := LoadPlanHandoffDomain(f.root)
	if err != nil {
		t.Fatalf("load plan hand-off domain: %v", err)
	}
	return f, domain, f.read(t)
}

func TestC6RecordUndecidedPlansRecordsOncePerObservationAndReturnsThem(t *testing.T) {
	f, domain, before := c6UndecidedFixture(t)
	now := time.Now().UTC()

	records, err := RecordUndecidedPlans(f.bb, c6Authority("orchestrator-1"), domain, before, []string{"p"}, now)
	if err != nil || len(records) != 1 {
		t.Fatalf("RecordUndecidedPlans = (%+v, %v), want one record", records, err)
	}
	record := records[0]
	if record.Observation.TaskID != "p" || record.Observation.Class == "" || record.Task.ID != "p" {
		t.Fatalf("record = %+v, want the observation and the committed task", record)
	}
	plan := f.read(t).FindTask("p")
	last := plan.History[len(plan.History)-1]
	if last.Event != models.TaskEventPlanHandoffUndecided || !last.Time.Equal(now) {
		t.Fatalf("last history entry = %+v, want plan_handoff_undecided at the caller's time", last)
	}
	if len(record.Task.History) != len(plan.History) {
		t.Fatalf("returned task history = %d entries, want the committed %d", len(record.Task.History), len(plan.History))
	}

	again, err := RecordUndecidedPlans(f.bb, c6Authority("orchestrator-1"), domain, before, []string{"p"}, now)
	if err != nil || len(again) != 0 {
		t.Fatalf("second RecordUndecidedPlans = (%+v, %v), want a matching observation never duplicated", again, err)
	}
}

func TestC6RecordUndecidedPlansSkipsAPlanWhoseInputsMovedDuringTheTurn(t *testing.T) {
	f, domain, before := c6UndecidedFixture(t)
	if err := f.bb.Modify(func(s *models.State) error {
		s.HumanNotes = append(s.HumanNotes, models.HumanNote{For: "p", Message: "replan", Timestamp: time.Now().UTC()})
		return nil
	}); err != nil {
		t.Fatalf("add note during the turn: %v", err)
	}
	bytesBefore := f.bytes(t)

	records, err := RecordUndecidedPlans(f.bb, c6Authority("orchestrator-1"), domain, before, []string{"p", "no-such-plan"}, time.Now().UTC())
	if err != nil || len(records) != 0 {
		t.Fatalf("RecordUndecidedPlans = (%+v, %v), want nothing recorded over an input the turn never saw", records, err)
	}
	f.requireUnchanged(t, bytesBefore, "recording over moved inputs")
}

func TestC6RecordUndecidedPlansRefusesAStaleGeneration(t *testing.T) {
	f, domain, before := c6UndecidedFixture(t)
	bytesBefore := f.bytes(t)

	stale := models.AgentAuthority{ID: "orchestrator-1", Generation: "replaced"}
	records, err := RecordUndecidedPlans(f.bb, stale, domain, before, []string{"p"}, time.Now().UTC())
	requireAuthorityError(t, err, "orchestrator-1")
	if records != nil {
		t.Fatalf("records = %+v on a refused transaction, want none", records)
	}
	f.requireUnchanged(t, bytesBefore, "recording by a replaced generation")
}
