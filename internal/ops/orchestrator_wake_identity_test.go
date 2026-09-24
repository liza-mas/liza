package ops

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func blockedIdentityState() *models.State {
	now := time.Date(2026, time.September, 24, 9, 0, 0, 0, time.UTC)
	state := testhelpers.CreateValidState()
	blocked := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	blocked.DependsOn = []string{"dep"}
	other := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusBlocked, now)
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("dep", models.TaskStatusImplementing, now), blocked, other}
	return state
}

// The identity changes with blocker material and ignores the orchestrator's
// own restatement of the hold.
func TestBlockedMaterialIdentity(t *testing.T) {
	baseline := BlockedMaterialIdentity(blockedIdentityState())
	if baseline == "" {
		t.Fatal("blocked tasks must yield a non-empty identity")
	}
	orchestrator := "orchestrator-1"

	for _, tc := range []struct {
		name    string
		mutate  func(*models.State)
		changes bool
	}{
		{"assessment note only", func(s *models.State) {
			note := "still waiting on dep"
			s.FindTask("task-1").History = append(s.FindTask("task-1").History, models.TaskHistoryEntry{
				Time: time.Now().UTC(), Event: models.TaskEventOrchestratorAssessment, Agent: &orchestrator, Note: &note,
			})
		}, false},
		{"task order", func(s *models.State) { s.Tasks[1], s.Tasks[2] = s.Tasks[2], s.Tasks[1] }, false},
		{"dependency outcome", func(s *models.State) { s.FindTask("dep").Status = models.TaskStatusMerged }, true},
		{"targeted human note", func(s *models.State) {
			s.HumanNotes = append(s.HumanNotes, models.HumanNote{Timestamp: time.Now().UTC(), For: "task-1", Message: "decision"})
		}, true},
		{"blocker reason", func(s *models.State) {
			reason := "a different blocker"
			s.FindTask("task-2").BlockedReason = &reason
		}, true},
		{"awaited set", func(s *models.State) {
			s.FindTask("task-2").History = append(s.FindTask("task-2").History, models.TaskHistoryEntry{
				Time: time.Now().UTC(), Event: models.TaskEventOrchestratorAssessment, Agent: &orchestrator,
				Extra: map[string]any{AwaitedTasksExtraKey: []string{"dep"}},
			})
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := blockedIdentityState()
			tc.mutate(state)
			if changed := BlockedMaterialIdentity(state) != baseline; changed != tc.changes {
				t.Fatalf("identity changed = %v, want %v", changed, tc.changes)
			}
		})
	}

	empty := testhelpers.CreateValidState()
	empty.Tasks = nil
	if got := BlockedMaterialIdentity(empty); got != "" {
		t.Fatalf("no blocked tasks must yield an empty identity, got %q", got)
	}
}
