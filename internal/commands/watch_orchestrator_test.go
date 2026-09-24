package commands

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers"
)

// D64: an orchestrator that exits and unregisters leaves no agent row, so no
// registered-agent or role-pair check can see it. The snapshot entry point must
// still raise one specific alert once the absence has outlasted the grace.
func TestRunChecksWithStateSnapshot_RaisesOrchestratorMissing(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState() // goal IN_PROGRESS, mode RUNNING
	state.Agents = nil                      // the orchestrator unregistered on exit

	// The production entry point reads the wall clock, so the episode is
	// pre-aged past the grace through the check's cache contract.
	cache := map[string]time.Time{
		"orchestrator-missing:since": time.Now().UTC().Add(-2 * time.Minute),
	}
	snapshot := RunChecksWithStateSnapshot(state, WatchConfig{ProjectRoot: tmpDir, StateCache: cache})

	if got := countAlertsByCategory(snapshot.Alerts, "ORCHESTRATOR MISSING"); got != 1 {
		t.Fatalf("ORCHESTRATOR MISSING alerts = %d, want 1; alerts: %v", got, snapshot.Alerts)
	}
	if alert := firstAlertByCategory(t, snapshot.Alerts, "ORCHESTRATOR MISSING"); alert.Level != AlertLevelCritical {
		t.Fatalf("Level = %q, want %q", alert.Level, AlertLevelCritical)
	}
}
