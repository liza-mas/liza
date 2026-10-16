package tui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// loadedRefreshModel is a model that has completed its initial read of a
// state whose agent holds a lease that expired an hour ago.
func loadedRefreshModel(t *testing.T) (Model, string) {
	t.Helper()
	t.Setenv(commands.EnvAutoRepairAgentPool, "false")
	dir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, dir)
	state := testhelpers.CreateValidState()
	expired := time.Now().UTC().Add(-time.Hour)
	task := "task-1"
	state.Agents["coder-1"] = models.Agent{
		Role: "coder", Status: models.AgentStatusWorking, Generation: "gen-1",
		CurrentTask: &task, Heartbeat: expired, LeaseExpires: &expired,
	}
	testhelpers.WriteInitialState(t, statePath, state)

	m := testModel()
	m.blackboard = db.For(statePath)
	m.projectRoot = dir
	m.alertsLogPath = filepath.Join(dir, "alerts.log")
	m.logPath = filepath.Join(dir, "log.yaml")
	m.stateReadInFlight = true
	next, _ := m.Update(readStateCmd(m.blackboard, nil, nil, true)())
	return next.(Model), statePath
}

// tickRead drives one tick and returns the completed read's message, plus
// the model after it.
func tickRead(t *testing.T, m Model) (Model, StateMsg, []AlertMsg) {
	t.Helper()
	next, _ := m.Update(TickMsg(time.Now()))
	m = next.(Model)
	read := readStateCmd(m.blackboard, m.state, m.stateFile, false)().(StateMsg)
	next, cmd := m.Update(read)
	checks, ok := checksIn(batchMsgs(t, cmd, 2*time.Second))
	if !ok {
		t.Fatal("completed tick read did not run the due checks")
	}
	next, _ = next.(Model).Update(checks)
	return next.(Model), read, checks.Alerts
}

// An unchanged state file is refreshed, not reparsed, and the tick's checks
// see liveness published since: a lease renewed only in the side record is
// not reported expired.
func TestUnchangedTickRefreshesLivenessBeforeChecks(t *testing.T) {
	m, statePath := loadedRefreshModel(t)
	if lease := m.state.Agents["coder-1"].LeaseExpires; lease == nil || !lease.Before(time.Now().Add(-models.LeaseExpiryGracePeriod)) {
		t.Fatalf("fixture lease %v is not expired past grace", lease)
	}

	renewed := time.Now().UTC().Add(30 * time.Minute)
	if err := db.WriteLivenessRecord(statePath, db.LivenessRecord{
		AgentID: "coder-1", Generation: "gen-1", Seq: 1, Heartbeat: time.Now().UTC(), LeaseExpires: renewed,
	}); err != nil {
		t.Fatal(err)
	}

	m, read, alerts := tickRead(t, m)
	if !read.Unchanged {
		t.Fatal("tick over an unchanged state file reparsed it")
	}
	if lease := m.state.Agents["coder-1"].LeaseExpires; lease == nil || !lease.Equal(renewed) {
		t.Fatalf("refreshed lease = %v, want %v", lease, renewed)
	}
	for _, a := range alerts {
		if a.Category == "LEASE EXPIRED" {
			t.Fatalf("checks ran on the pre-refresh snapshot: %+v", a)
		}
	}
}

// A write between ticks makes the next tick parse the new publication.
func TestTickAfterWriteReparses(t *testing.T) {
	m, _ := loadedRefreshModel(t)
	if err := m.blackboard.Modify(func(s *models.State) error {
		s.Goal.Description = "changed goal"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	m, read, _ := tickRead(t, m)
	if read.Unchanged || m.state.Goal.Description != "changed goal" {
		t.Fatalf("tick after a write: unchanged=%t goal=%q, want the new publication parsed", read.Unchanged, m.state.Goal.Description)
	}
}
