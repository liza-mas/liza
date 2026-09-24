package tui

import (
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/liza-mas/liza/internal/db"
)

// tickLaunchedChecks reports whether the command returned for a tick includes
// an anomaly-check run. With no state loaded, the check command returns an
// alertsMsg immediately; the tick command itself blocks and is never awaited.
func tickLaunchedChecks(t *testing.T, cmd tea.Cmd) (alertsMsg, bool) {
	t.Helper()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("tick command did not return a batch")
	}
	results := make(chan tea.Msg, len(batch))
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		go func(c tea.Cmd) { results <- c() }(sub)
	}
	deadline := time.After(time.Second)
	for {
		select {
		case msg := <-results:
			if checks, ok := msg.(alertsMsg); ok {
				return checks, true
			}
		case <-deadline:
			return alertsMsg{}, false
		}
	}
}

func TestUpdateTickMsgRunsOneAnomalyCheckAtATime(t *testing.T) {
	dir := t.TempDir()
	m := testModel()
	m.blackboard = db.For(filepath.Join(dir, "state.yaml"))
	m.logPath = filepath.Join(dir, "log.yaml")
	m.alertsLogPath = filepath.Join(dir, "alerts.log")
	m.projectRoot = dir

	next, cmd := m.Update(TickMsg(time.Now()))
	inFlight, launched := tickLaunchedChecks(t, cmd)
	if !launched {
		t.Fatal("first tick did not launch an anomaly check")
	}

	next, cmd = next.(Model).Update(TickMsg(time.Now()))
	if _, launched := tickLaunchedChecks(t, cmd); launched {
		t.Fatal("second tick launched an anomaly check while the first was in flight")
	}

	next, _ = next.(Model).Update(inFlight)
	_, cmd = next.(Model).Update(TickMsg(time.Now()))
	if _, launched := tickLaunchedChecks(t, cmd); !launched {
		t.Fatal("tick after the in-flight check completed did not launch a new check")
	}
}
