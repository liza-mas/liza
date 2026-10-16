package tui

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// A trigger arriving while a read is in flight queues one read instead of
// starting a concurrent decode; completing the read starts the queued one,
// and the tick's checks run against the read that just completed.
func TestStateReadsRunOneAtATimeAndChecksUseTheCompletedRead(t *testing.T) {
	m := checksTestModel(t)

	next, _ := m.Update(TickMsg(time.Now()))
	m = next.(Model)
	if !m.stateReadInFlight || !m.checksDue || m.checksInFlight {
		t.Fatalf("tick: inFlight=%t checksDue=%t checksInFlight=%t, want a read started and checks waiting for it",
			m.stateReadInFlight, m.checksDue, m.checksInFlight)
	}

	next, _ = m.Update(stateChangedMsg{})
	m = next.(Model)
	if !m.stateReadInFlight || !m.stateReadQueued {
		t.Fatalf("change during a read: inFlight=%t queued=%t, want the change queued behind the read", m.stateReadInFlight, m.stateReadQueued)
	}

	// The completed read holds an agent lease long expired: its checks must see
	// it, which the previous (nil) snapshot cannot produce.
	expired := time.Now().UTC().Add(-time.Hour)
	fresh := &models.State{Agents: map[string]models.Agent{
		"coder-1": {Role: "coder", Status: models.AgentStatusWorking, CurrentTask: strPtr("task-1"), LeaseExpires: &expired},
	}}
	next, cmd := m.Update(StateMsg{State: fresh})
	m = next.(Model)
	if m.state != fresh || !m.stateReadInFlight || m.stateReadQueued || m.checksDue || !m.checksInFlight {
		t.Fatalf("read completed: fresh=%t inFlight=%t queued=%t checksDue=%t checksInFlight=%t, want the queued read started and checks launched",
			m.state == fresh, m.stateReadInFlight, m.stateReadQueued, m.checksDue, m.checksInFlight)
	}

	msgs := batchMsgs(t, cmd, 2*time.Second)
	checks, launched := checksIn(msgs)
	if !launched {
		t.Fatal("completed read did not launch the due checks")
	}
	found := false
	for _, a := range checks.Alerts {
		found = found || a.Category == "LEASE EXPIRED"
	}
	if !found {
		t.Fatalf("checks did not run against the completed read: alerts %+v", checks.Alerts)
	}
	queuedRead := false
	for _, msg := range msgs {
		_, ok := msg.(StateMsg)
		queuedRead = queuedRead || ok
	}
	if !queuedRead {
		t.Fatal("completing the read did not start the queued read")
	}
}

// A failed read keeps the last good snapshot and, where checks are due, raises
// the critical INVALID STATE alert once per episode (alerts.log and banner)
// without resolving alerts earlier checks raised. Checks stay due for the next
// good read, whose clean validation resolves the episode, so a recurrence
// alerts again.
func TestStateReadFailureRaisesInvalidStateOnceAndRecovers(t *testing.T) {
	m := checksTestModel(t)
	good, err := m.blackboard.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	m.state = good
	m.ready = true
	m.activities = appendActivity(m.activities, ActivityEntry{Source: "alert", Action: "BLOCKED", AlertKey: "BLOCKED:task-9", Level: "⚠️"})
	readErr := errors.New("terminal archive object missing")

	fail := func(m Model) (Model, []AlertMsg) {
		t.Helper()
		next, _ := m.Update(TickMsg(time.Now()))
		next, cmd := next.(Model).Update(StateMsg{Err: readErr})
		m = next.(Model)
		if m.state != good || !m.checksDue {
			t.Fatalf("failed read: kept snapshot=%t checksDue=%t, want the good snapshot kept and checks still due", m.state == good, m.checksDue)
		}
		report, ok := checksIn(batchMsgs(t, cmd, time.Second))
		if !ok {
			t.Fatal("failed read did not report the unreadable state")
		}
		next, _ = m.Update(report)
		return next.(Model), report.Alerts
	}

	m, raised := fail(m)
	if len(raised) != 1 || raised[0].Category != "INVALID STATE" || raised[0].Level != "🚨" || raised[0].Message != readErr.Error() || raised[0].Key == "" {
		t.Fatalf("first failure alerts = %+v, want one keyed critical INVALID STATE for the read error", raised)
	}
	if m.alertBanner == nil || m.alertBanner.Action != "INVALID STATE" {
		t.Fatalf("banner = %+v, want INVALID STATE", m.alertBanner)
	}
	logged, err := os.ReadFile(m.alertsLogPath)
	if err != nil || !strings.Contains(string(logged), readErr.Error()) {
		t.Fatalf("alerts.log = %q (%v), want the read error persisted", logged, err)
	}
	if m.activities[0].Resolved {
		t.Fatal("the failure report resolved an alert no check re-evaluated")
	}

	m, raised = fail(m)
	if len(raised) != 0 {
		t.Fatalf("persisting failure re-announced: %+v", raised)
	}

	// Recovery: the next good read runs the due checks; clean validation
	// resolves the episode.
	next, cmd := m.Update(StateMsg{State: good})
	m = next.(Model)
	checks, ok := checksIn(batchMsgs(t, cmd, 2*time.Second))
	if !ok {
		t.Fatal("good read after the failure did not run the due checks")
	}
	for _, a := range checks.Alerts {
		if a.Category == "INVALID STATE" {
			t.Fatalf("fixture state is not clean: %+v", a)
		}
	}
	next, _ = m.Update(checks)
	m = next.(Model)
	for _, a := range m.activities {
		if a.Action == "INVALID STATE" && !a.Resolved {
			t.Fatal("clean validation did not resolve the INVALID STATE entry")
		}
	}
	if m.alertBanner != nil {
		t.Fatalf("banner = %+v, want cleared on recovery", m.alertBanner)
	}

	if _, raised = fail(m); len(raised) != 1 {
		t.Fatalf("recurrence after recovery alerts = %+v, want INVALID STATE again", raised)
	}
}
