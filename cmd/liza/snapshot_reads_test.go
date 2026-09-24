package main

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D69 Step 2: completion values and launch-input validation read lock-free
// snapshots, so they complete while a writer holds the state lock. These tests
// shorten the ordinary lock timeout so the pre-snapshot behavior fails fast;
// they must not run in parallel.
const snapshotReadLockTimeout = 200 * time.Millisecond

func TestCompletionStateReadsSnapshotWhileStateLockHeld(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(snapshotReadLockTimeout))
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC())}

	root := &cobra.Command{Use: "root"}
	root.PersistentFlags().String("project-root", "", "")
	if err := root.PersistentFlags().Set("project-root", tmpDir); err != nil {
		t.Fatal(err)
	}
	child := &cobra.Command{Use: "child"}
	root.AddCommand(child)
	projectRoot, ok := completionProjectRoot(child)
	if !ok {
		t.Fatal("completion could not resolve the fixture project root")
	}
	statePath := paths.New(projectRoot).StatePath()
	testhelpers.WriteInitialState(t, statePath, state)
	testhelpers.HoldFileLock(t, statePath)

	got, ok := completionState(child)
	if !ok {
		t.Fatal("completion state unavailable while the state lock is held")
	}
	if got.FindTask("task-1") == nil {
		t.Fatal("completion state is missing the fixture task")
	}
}

func TestLaunchAvailableCLIsReadsSnapshotWhileStateLockHeld(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(snapshotReadLockTimeout))
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Config.AgentTools = map[string]models.AgentToolConfig{"snapshot-custom-cli": {}}
	testhelpers.WriteInitialState(t, statePath, state)
	testhelpers.HoldFileLock(t, statePath)

	if got := launchAvailableCLIs(tmpDir); !slices.Contains(got, "snapshot-custom-cli") {
		t.Fatalf("launch CLIs while the state lock is held = %v, want the configured custom tool", got)
	}
}
