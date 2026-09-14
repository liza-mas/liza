package ops

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func TestMarkHumanNotesSeen_StampsOnlyUnseen(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	earlier := models.HumanNote{Timestamp: now.Add(-time.Hour), For: "all", Message: "old"}
	earlier.MarkSeenByOrchestrator(now.Add(-30 * time.Minute))
	state := &models.State{HumanNotes: []models.HumanNote{
		earlier,
		{Timestamp: now, For: "task-1", Message: "new"},
		{Timestamp: now, For: "all", Message: "newer"},
	}}
	if got := CountUnseenHumanNotes(state); got != 2 {
		t.Fatalf("CountUnseenHumanNotes = %d, want 2", got)
	}
	if got := len(UnseenHumanNotes(state)); got != 2 {
		t.Fatalf("UnseenHumanNotes = %d, want 2", got)
	}
	all := func(*models.HumanNote) bool { return true }
	if stamped := MarkHumanNotesSeen(state, now, 1, all); stamped != 0 {
		t.Fatalf("MarkHumanNotesSeen bounded to the already-seen note = %d, want 0", stamped)
	}
	none := func(*models.HumanNote) bool { return false }
	if stamped := MarkHumanNotesSeen(state, now, len(state.HumanNotes), none); stamped != 0 {
		t.Fatalf("MarkHumanNotesSeen with nothing consumed = %d, want 0", stamped)
	}
	if stamped := MarkHumanNotesSeen(state, now, len(state.HumanNotes), all); stamped != 2 {
		t.Fatalf("MarkHumanNotesSeen = %d, want 2", stamped)
	}
	if got := CountUnseenHumanNotes(state); got != 0 {
		t.Fatalf("after stamping CountUnseenHumanNotes = %d, want 0", got)
	}
	if state.HumanNotes[0].Extra[models.HumanNoteOrchestratorSeenKey] != now.Add(-30*time.Minute).UTC().Format(time.RFC3339) {
		t.Fatalf("earlier seen stamp was overwritten: %v", state.HumanNotes[0].Extra)
	}
}

func TestTasksAssessedBetween(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	assessment := func(at time.Time) models.TaskHistoryEntry {
		return models.TaskHistoryEntry{Time: at, Event: models.TaskEventOrchestratorAssessment}
	}
	before := &models.State{Tasks: []models.Task{
		{ID: "assessed-again", History: []models.TaskHistoryEntry{assessment(now.Add(-time.Hour))}},
		{ID: "unchanged", History: []models.TaskHistoryEntry{assessment(now.Add(-time.Hour))}},
		{ID: "first-assessment"},
	}}
	after := &models.State{Tasks: []models.Task{
		{ID: "assessed-again", History: []models.TaskHistoryEntry{assessment(now.Add(-time.Hour)), assessment(now.Add(-time.Hour))}},
		{ID: "unchanged", History: []models.TaskHistoryEntry{assessment(now.Add(-time.Hour))}},
		{ID: "first-assessment", History: []models.TaskHistoryEntry{assessment(now)}},
		{ID: "created-during-turn", History: []models.TaskHistoryEntry{assessment(now)}},
	}}
	got := TasksAssessedBetween(before, after)
	for _, id := range []string{"assessed-again", "first-assessment", "created-during-turn"} {
		if !got[id] {
			t.Errorf("%s missing from assessed set %v", id, got)
		}
	}
	if got["unchanged"] {
		t.Errorf("unchanged task reported as assessed: %v", got)
	}
}
