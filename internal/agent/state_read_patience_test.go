package agent

import (
	"context"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D69: supervisor-path state reads must outlast a transient state-lock hold
// longer than the ordinary lock timeout. These tests shorten the ordinary
// timeout, so they must not run in parallel.
const (
	patienceTestLockTimeout = 200 * time.Millisecond
	patienceTestLockHold    = 600 * time.Millisecond
)

// holdStateLockTransiently holds the state lock for patienceTestLockHold,
// starting before it returns.
func holdStateLockTransiently(t *testing.T, statePath string) {
	t.Helper()
	release := testhelpers.HoldFileLock(t, statePath)
	time.AfterFunc(patienceTestLockHold, release)
}

func TestAutoAssignAgentIDOutlastsStateLockTimeout(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(patienceTestLockTimeout))
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	bb := testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())
	holdStateLockTransiently(t, statePath)

	assignedID, err := AutoAssignAgentID(bb, "coder", 1, func(string) error { return nil })
	if err != nil {
		t.Fatalf("AutoAssignAgentID under a transient state-lock hold: %v", err)
	}
	if assignedID != "coder-1" {
		t.Fatalf("assigned %q, want coder-1", assignedID)
	}
}

func TestProviderLaunchGateOutlastsStateLockTimeout(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(patienceTestLockTimeout))
	fixture := newProviderGenerationFixture(t)
	holdStateLockTransiently(t, fixture.statePath)

	started := false
	err := newProviderLaunchGate(fixture.config(fixture.authorityA))(context.Background(), func() error {
		started = true
		return nil
	})
	if err != nil || !started {
		t.Fatalf("provider launch under a transient state-lock hold: err=%v started=%v", err, started)
	}
}

func TestReviewExecutionWatchdogStartOutlastsStateLockTimeout(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(patienceTestLockTimeout))
	config, _, _ := reviewExecutionFixture(t)
	holdStateLockTransiently(t, config.StatePath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop, err := startReviewExecutionWatchdog(ctx, config, "review-task", time.Hour, cancel)
	if err != nil {
		t.Fatalf("review watchdog start under a transient state-lock hold: %v", err)
	}
	if stop() {
		t.Fatal("review ownership reported lost")
	}
}
