package agent

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const blockedUnresolvedAlert = "BLOCKED_TASKS UNRESOLVED"

func runBlockedTurn(t *testing.T, strategy *orchestratorStrategy, bb *db.Blackboard, projectRoot string) {
	t.Helper()
	strategy.wake = &OrchestratorWakeResult{Trigger: WakeTriggerBlocked, Count: 1}
	if err := strategy.PostExecution(bb, orchestratorScipConfig(projectRoot), "", "", mustReadState(t, bb)); err != nil {
		t.Fatalf("PostExecution() error = %v", err)
	}
}

func countBlockedUnresolvedAlerts(t *testing.T, projectRoot string) int {
	t.Helper()
	data, err := os.ReadFile(paths.New(projectRoot).AlertsLogPath())
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), blockedUnresolvedAlert)
}

// D45: consecutive BLOCKED_TASKS turns that leave the blocker material
// exactly as it was are a loop signal worth one alert, not a log line only.
func TestOrchestratorBlockedTurns_UnchangedBlockerMaterialAlertsOnce(t *testing.T) {
	now := time.Now().UTC()
	projectRoot := t.TempDir()
	bb := newOrchestratorScipTestBlackboard(t, projectRoot, func(state *models.State) {
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)}
		state.Sprint.Scope.Planned = []string{"task-1"}
	})
	strategy := &orchestratorStrategy{}

	runBlockedTurn(t, strategy, bb, projectRoot)
	if got := countBlockedUnresolvedAlerts(t, projectRoot); got != 0 {
		t.Fatalf("first unresolved turn alerted %d times; one turn is not a loop", got)
	}
	runBlockedTurn(t, strategy, bb, projectRoot)
	if got := countBlockedUnresolvedAlerts(t, projectRoot); got != 1 {
		t.Fatalf("second identical unresolved turn: %d alerts, want 1", got)
	}
	runBlockedTurn(t, strategy, bb, projectRoot)
	if got := countBlockedUnresolvedAlerts(t, projectRoot); got != 1 {
		t.Fatalf("third identical unresolved turn: %d alerts, want still 1 (once per identity)", got)
	}
}

// A repeated hold whose blocker input changed between turns is legitimate:
// the same blocked IDs must not be read as a loop.
func TestOrchestratorBlockedTurns_ChangedBlockerInputDoesNotAlert(t *testing.T) {
	now := time.Now().UTC()
	for _, change := range []struct {
		name  string
		apply func(*models.State)
	}{
		{"dependency outcome", func(s *models.State) { s.FindTask("dep").Status = models.TaskStatusMerged }},
		{"human note", func(s *models.State) {
			s.HumanNotes = append(s.HumanNotes, models.HumanNote{Timestamp: now, For: "task-1", Message: "decision: option B"})
		}},
		{"blocker reason", func(s *models.State) {
			reason := "Blocked on a different clarification"
			s.FindTask("task-1").BlockedReason = &reason
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			bb := newOrchestratorScipTestBlackboard(t, projectRoot, func(state *models.State) {
				blocked := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
				blocked.DependsOn = []string{"dep"}
				state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("dep", models.TaskStatusImplementing, now), blocked}
				state.Sprint.Scope.Planned = []string{"dep", "task-1"}
			})
			strategy := &orchestratorStrategy{}

			runBlockedTurn(t, strategy, bb, projectRoot)
			if err := bb.Modify(func(s *models.State) error { change.apply(s); return nil }); err != nil {
				t.Fatal(err)
			}
			runBlockedTurn(t, strategy, bb, projectRoot)

			if got := countBlockedUnresolvedAlerts(t, projectRoot); got != 0 {
				t.Fatalf("hold with changed %s alerted %d times; want 0", change.name, got)
			}
		})
	}
}
