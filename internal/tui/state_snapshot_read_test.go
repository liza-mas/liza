package tui

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D69 Step 2: the TUI renders from a lock-free snapshot, so a state change is
// shown while a writer holds the state lock. Not parallel: it shortens the
// ordinary lock timeout so the pre-snapshot behavior fails fast.
func TestReadStateCmdReadsSnapshotWhileStateLockHeld(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(200 * time.Millisecond))
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Goal.Description = "snapshot-visible goal"
	bb := testhelpers.WriteInitialState(t, statePath, state)
	testhelpers.HoldFileLock(t, statePath)

	msg := readStateCmd(bb)()
	stateMsg, ok := msg.(StateMsg)
	if !ok {
		t.Fatalf("readStateCmd while the state lock is held returned %T %+v, want StateMsg", msg, msg)
	}
	if stateMsg.State.Goal.Description != "snapshot-visible goal" {
		t.Fatalf("StateMsg goal = %q", stateMsg.State.Goal.Description)
	}
}
