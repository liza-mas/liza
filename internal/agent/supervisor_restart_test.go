package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// An orchestrator re-executing without state progress stops the system once
// it passes the spinning threshold, and not before.
func TestStopSpinningOrchestrator_StopsSystemPastThreshold(t *testing.T) {
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	state.Config.SpinningRestartThreshold = 1
	testhelpers.WriteInitialState(t, statePath, state)
	config := SupervisorConfig{AgentID: "orchestrator-1", ProjectRoot: projectRoot}
	tracker := newSpinningTracker()

	if stopSpinningOrchestrator(config, state, tracker) {
		t.Fatal("first execution stopped the system; the threshold allows one")
	}
	if !stopSpinningOrchestrator(config, state, tracker) {
		t.Fatal("second execution without progress did not stop the system")
	}

	after, err := db.For(statePath).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if after.Config.Mode != models.SystemModeStopped {
		t.Fatalf("system mode = %s, want %s", after.Config.Mode, models.SystemModeStopped)
	}
	alerts, err := os.ReadFile(paths.New(projectRoot).AlertsLogPath())
	if err != nil {
		t.Fatalf("read alerts log: %v", err)
	}
	if !strings.Contains(string(alerts), "ORCHESTRATOR_SPINNING") {
		t.Fatalf("alerts log lacks ORCHESTRATOR_SPINNING:\n%s", alerts)
	}
}
