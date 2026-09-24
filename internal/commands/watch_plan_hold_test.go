package commands

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
)

// D73: a plan held for a human action raises one AWAITING HUMAN alert naming
// the ask and the operator command that releases it.
func TestCheckAwaitingHuman_HeldPlan(t *testing.T) {
	held := models.Task{
		ID: "plan-1", Status: models.TaskStatusMerged, RolePair: "code-planning-pair",
		PlanCheck: &models.PlanCheck{Verdict: models.PlanCheckHeld, Ask: "inject smoke credentials", By: "orchestrator-1", At: time.Now().UTC()},
	}
	passed := held
	passed.ID = "plan-2"
	passed.PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckPassed, By: "orchestrator-1", At: time.Now().UTC()}
	state := &models.State{Sprint: models.Sprint{Status: models.SprintStatusInProgress}, Tasks: []models.Task{held, passed}}

	alerts := checkAwaitingHuman(state)
	if len(alerts) != 1 {
		t.Fatalf("alerts = %v, want one for the held plan", alerts)
	}
	a := alerts[0]
	if a.Category != "AWAITING HUMAN" || a.Level != AlertLevelCritical {
		t.Errorf("alert = %s %s, want critical AWAITING HUMAN", a.Level, a.Category)
	}
	for _, want := range []string{"plan-1", "inject smoke credentials", brand.Command("plan-check", "plan-1", "--clear")} {
		if !strings.Contains(a.Message, want) {
			t.Errorf("message = %q, want %q", a.Message, want)
		}
	}

	cache := map[string]time.Time{}
	if first := reconcileStuckAlerts(alerts, cache); len(first) != 1 {
		t.Fatalf("first reconcile = %v, want the alert", first)
	}
	if again := reconcileStuckAlerts(checkAwaitingHuman(state), cache); len(again) != 0 {
		t.Fatalf("repeat reconcile = %v, want the alert deduped", again)
	}
}
