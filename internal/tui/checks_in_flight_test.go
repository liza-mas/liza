package tui

import (
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// batchMsgs runs every command of a batch concurrently and returns the
// messages that arrive within wait. Commands that block (the tick timer, the
// watcher subscription) are never awaited.
func batchMsgs(t *testing.T, cmd tea.Cmd, wait time.Duration) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	// tea.Batch returns a lone command unwrapped.
	first := cmd()
	cmds, ok := first.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{first}
	}
	results := make(chan tea.Msg, len(cmds))
	for _, sub := range cmds {
		if sub == nil {
			continue
		}
		go func(c tea.Cmd) { results <- c() }(sub)
	}
	var msgs []tea.Msg
	deadline := time.After(wait)
	for {
		select {
		case msg := <-results:
			msgs = append(msgs, msg)
		case <-deadline:
			return msgs
		}
	}
}

func checksIn(msgs []tea.Msg) (alertsMsg, bool) {
	for _, msg := range msgs {
		if checks, ok := msg.(alertsMsg); ok {
			return checks, true
		}
	}
	return alertsMsg{}, false
}

// checksTestModel is a model whose state reads and checks run against a
// valid state file in a temporary project, with pool auto-repair disabled.
func checksTestModel(t *testing.T) Model {
	t.Helper()
	t.Setenv(commands.EnvAutoRepairAgentPool, "false")
	dir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, dir)
	testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())
	m := testModel()
	m.blackboard = db.For(statePath)
	m.logPath = filepath.Join(dir, "log.yaml")
	m.alertsLogPath = filepath.Join(dir, "alerts.log")
	m.projectRoot = dir
	return m
}

func TestUpdateTickRunsOneAnomalyCheckAtATime(t *testing.T) {
	m := checksTestModel(t)
	state := &models.State{}

	next, _ := m.Update(TickMsg(time.Now()))
	next, cmd := next.(Model).Update(StateMsg{State: state})
	inFlight, launched := checksIn(batchMsgs(t, cmd, time.Second))
	if !launched {
		t.Fatal("the read completing after the first tick did not launch an anomaly check")
	}

	next, _ = next.(Model).Update(TickMsg(time.Now()))
	next, cmd = next.(Model).Update(StateMsg{State: state})
	if _, launched := checksIn(batchMsgs(t, cmd, 200*time.Millisecond)); launched {
		t.Fatal("a second check launched while the first was in flight")
	}

	next, _ = next.(Model).Update(inFlight)
	_, cmd = next.(Model).Update(StateMsg{State: state})
	if _, launched := checksIn(batchMsgs(t, cmd, time.Second)); !launched {
		t.Fatal("the tick still due after the in-flight check completed did not launch a check")
	}
}
