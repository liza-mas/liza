package commands

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D69 Step 2: observation-only reads use lock-free snapshots, so they complete
// while a writer holds the state lock. These tests shorten the ordinary lock
// timeout so the pre-snapshot behavior fails fast; they must not run in parallel.
const snapshotReadLockTimeout = 200 * time.Millisecond

func TestWatchRunChecksReadsSnapshotWhileStateLockHeld(t *testing.T) {
	t.Setenv(EnvAutoRepairAgentPool, "0")
	t.Cleanup(db.SetDefaultLockTimeoutForTest(snapshotReadLockTimeout))
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusIntegrationFailed, time.Now().UTC()),
	}
	testhelpers.WriteInitialState(t, stateFile, state)
	alertsLog := paths.New(tmpDir).AlertsLogPath()
	testhelpers.HoldFileLock(t, stateFile)

	err := runChecks(context.Background(), WatchConfig{
		ProjectRoot: tmpDir,
		AlertsLog:   alertsLog,
		StateCache:  make(map[string]time.Time),
		WarnWriter:  io.Discard,
	})
	if err != nil {
		t.Fatalf("watch checks while the state lock is held: %v", err)
	}
	alerts, err := os.ReadFile(alertsLog)
	if err != nil {
		t.Fatalf("read alerts log: %v", err)
	}
	if !strings.Contains(string(alerts), "task-1") {
		t.Fatalf("alerts log does not report the integration-failed fixture task:\n%s", alerts)
	}
}

func TestUsageReportReadsSnapshotWhileStateLockHeld(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(snapshotReadLockTimeout))
	projectRoot := urProject(t, nil)
	testhelpers.HoldFileLock(t, paths.New(projectRoot).StatePath())

	report := urReport(t, UsageReportOptions{ProjectRoot: projectRoot})
	if !report.Window.Since.Equal(urT0) {
		t.Fatalf("report window starts at %v, want the fixture sprint start %v", report.Window.Since, urT0)
	}
}

func TestValidateReadsSnapshotWhileStateLockHeld(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*models.State)
		wantErr string
	}{
		{name: "valid state"},
		{name: "invalid state", mutate: func(s *models.State) {
			s.Tasks = []models.Task{{ID: "task-1", Status: "NOT_A_STATUS"}}
		}, wantErr: "NOT_A_STATUS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(db.SetDefaultLockTimeoutForTest(snapshotReadLockTimeout))
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)
			state := testhelpers.CreateValidState()
			if tc.mutate != nil {
				tc.mutate(state)
			}
			testhelpers.WriteInitialState(t, statePath, state)
			testhelpers.HoldFileLock(t, statePath)

			err := ValidateCommandWithOptions(statePath, ValidateOptions{
				SkipSpecFileCheck: true, SkipProcessChecks: true, WarnWriter: io.Discard,
			})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validate a valid state while the state lock is held: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) || strings.Contains(err.Error(), "lock") {
				t.Fatalf("validate an invalid state while the state lock is held: err=%v, want a validation error naming %q", err, tc.wantErr)
			}
		})
	}
}
