package commands

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/perm"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestResumeCommand(t *testing.T) {
	tests := []struct {
		name        string
		initialMode models.SystemMode
		changedBy   string
		wantErr     bool
		errContains string
		wantMode    models.SystemMode
	}{
		{
			name:        "resume from PAUSED mode",
			initialMode: models.SystemModePaused,
			changedBy:   "human",
			wantErr:     false,
			wantMode:    models.SystemModeRunning,
		},
		{
			name:        "resume from PAUSED with agent ID",
			initialMode: models.SystemModePaused,
			changedBy:   "admin-1",
			wantErr:     false,
			wantMode:    models.SystemModeRunning,
		},
		{
			name:        "cannot resume from RUNNING",
			initialMode: models.SystemModeRunning,
			changedBy:   "human",
			wantErr:     true,
			errContains: "not PAUSED",
			wantMode:    models.SystemModeRunning,
		},
		{
			name:        "cannot resume from STOPPED",
			initialMode: models.SystemModeStopped,
			changedBy:   "human",
			wantErr:     true,
			errContains: "cannot resume from STOPPED",
			wantMode:    models.SystemModeStopped,
		},
		{
			name:        "cannot resume from empty mode",
			initialMode: "",
			changedBy:   "human",
			wantErr:     true,
			errContains: "not PAUSED",
			wantMode:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			// Create initial state with specified mode
			state := testhelpers.CreateValidState()
			state.Config.Mode = tt.initialMode

			bb := testhelpers.WriteInitialState(t, stateFile, state)

			// Run resume command
			err := ResumeCommand(tmpDir, tt.changedBy)

			// Check error
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				testhelpers.AssertErrorContains(t, err, tt.errContains)

				// Verify mode didn't change
				updatedState, readErr := bb.Read()
				if readErr != nil {
					t.Fatalf("Failed to read state: %v", readErr)
				}
				if updatedState.Config.Mode != tt.wantMode {
					t.Errorf("Mode should not have changed: got %v, want %v", updatedState.Config.Mode, tt.wantMode)
				}
				return
			}

			testhelpers.AssertNoError(t, err)

			// Verify mode was updated
			updatedState, err := bb.Read()
			if err != nil {
				t.Fatalf("Failed to read state: %v", err)
			}

			if updatedState.Config.Mode != tt.wantMode {
				t.Errorf("Mode = %v, want %v", updatedState.Config.Mode, tt.wantMode)
			}

			// Verify mode_changed_at was set
			if updatedState.Config.ModeChangedAt == nil {
				t.Error("ModeChangedAt should be set")
			}

			// Verify mode_changed_by was set
			if updatedState.Config.ModeChangedBy == nil {
				t.Error("ModeChangedBy should be set")
			} else if *updatedState.Config.ModeChangedBy != tt.changedBy {
				t.Errorf("ModeChangedBy = %v, want %v", *updatedState.Config.ModeChangedBy, tt.changedBy)
			}
		})
	}
}

func TestResumeCommand_AcknowledgesStoppedHaltWithoutRestarting(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	triggeredAt := time.Now().UTC()
	pattern := "retry_cluster"
	severity := "ARCHITECTURE_FLAW"
	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeStopped
	state.CircuitBreaker.Status = "TRIGGERED"
	state.CircuitBreaker.CurrentTrigger = &models.CircuitBreakerTrigger{
		Timestamp: triggeredAt,
		Pattern:   pattern,
		Severity:  severity,
	}
	state.CircuitBreaker.CurrentResponse = &models.CircuitBreakerResponse{
		Timestamp: triggeredAt,
		Pattern:   pattern,
		Severity:  severity,
		Response:  models.CircuitBreakerResponseHalt,
	}
	state.CircuitBreaker.History = append(state.CircuitBreaker.History, models.CircuitBreakerHistory{
		Timestamp: triggeredAt,
		Pattern:   &pattern,
		Severity:  &severity,
		Result:    "TRIGGERED",
		Response:  models.CircuitBreakerResponseHalt,
	})
	testhelpers.WriteInitialState(t, stateFile, state)

	stdout := captureStdout(t, func() {
		if err := ResumeCommand(tmpDir, "human"); err != nil {
			t.Fatalf("ResumeCommand() error = %v", err)
		}
	})

	for _, want := range []string{"HALT response acknowledged", "System remains STOPPED", "start"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}

	updated, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if updated.Config.Mode != models.SystemModeStopped {
		t.Errorf("mode = %s, want STOPPED", updated.Config.Mode)
	}
}

func TestResumeCommand_WarnsOnProviderSignalClearFailure(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModePaused
	testhelpers.WriteInitialState(t, stateFile, state)

	signalPath := agent.ProviderUnavailableSignalPath(tmpDir, "codex")
	if err := os.MkdirAll(signalPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(signalPath, "blocker"), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	stdout := captureStdout(t, func() {
		if err := ResumeCommand(tmpDir, "human"); err != nil {
			t.Fatalf("ResumeCommand() error = %v", err)
		}
	})

	if !strings.Contains(stdout, "Warning: failed to clear provider signals") {
		t.Fatalf("stdout missing provider signal warning:\n%s", stdout)
	}
	if !strings.Contains(stdout, "unavailable/codex") {
		t.Fatalf("stdout missing provider name:\n%s", stdout)
	}
	if _, err := os.Stat(signalPath); err != nil {
		t.Fatalf("provider unavailable signal should remain after failed clear: %v", err)
	}
}

func TestResumeCommand_PrintsMidSprintTransitions(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.CheckpointTrigger = models.CheckpointTriggerPlanningComplete

	readyPlan := testhelpers.BuildTaskByStatus("plan-ready", models.TaskStatusMerged, now)
	readyPlan.Type = models.TaskTypePlanning
	readyPlan.RolePair = "code-planning-pair"
	readyPlan.Output = []models.OutputEntry{{
		Desc:     "Implement X",
		DoneWhen: "tests pass",
		Scope:    "pkg/x",
		SpecRef:  "specs/x.md",
	}}
	activePlan := testhelpers.BuildTaskByStatus("plan-active", models.TaskStatusCodePlanning, now)
	activePlan.Type = models.TaskTypePlanning
	activePlan.RolePair = "code-planning-pair"
	state.Tasks = []models.Task{readyPlan, activePlan}
	state.Sprint.Scope.Planned = []string{"plan-ready", "plan-active"}
	testhelpers.WriteInitialState(t, stateFile, state)

	stdout := captureStdout(t, func() {
		if err := ResumeCommand(tmpDir, "human"); err != nil {
			t.Fatalf("ResumeCommand() error = %v", err)
		}
	})

	if !strings.Contains(stdout, "Transitions executed: 1 (child tasks created)") {
		t.Fatalf("stdout missing transition count:\n%s", stdout)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	w.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read stdout: %v", err)
	}
	return string(data)
}

// TestResumeCommand_ArchiveWriteFailure verifies that when the archive file
// cannot be written, resume fails and state remains unchanged (no data loss).
// Uses COMPLETED sprint status because the two-step flow is:
//
//	CHECKPOINT + all terminal → COMPLETED (no archive), then
//	COMPLETED → archive + new sprint (archive write happens here).
func TestResumeCommand_ArchiveWriteFailure(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Make archive directory unwritable so writeSprintArchive fails.
	archiveDir := filepath.Join(tmpDir, paths.ProjectDirName(), "archive")
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		t.Fatalf("Failed to create archive dir: %v", err)
	}
	restore, err := perm.DenyWrites(archiveDir)
	if err != nil {
		t.Fatalf("deny writes on archive dir: %v", err)
	}
	t.Cleanup(func() {
		if err := restore(); err != nil {
			t.Errorf("restore write access: %v", err)
		}
	})

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	state.Sprint.Status = models.SprintStatusCompleted
	state.Sprint.Number = 1

	mergedTask := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
	state.Tasks = []models.Task{mergedTask}
	state.Sprint.Scope.Planned = []string{"task-1"}

	testhelpers.WriteInitialState(t, stateFile, state)

	// Resume should fail because archive write fails before state mutation.
	err = ResumeCommand(tmpDir, "human")
	if err == nil {
		t.Fatal("ResumeCommand() should fail when archive write fails")
	}

	// Verify state is unchanged — sprint was NOT advanced.
	bb := db.New(stateFile)
	readState, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("Failed to read state: %v", readErr)
	}
	if readState.Sprint.ID != "sprint-1" {
		t.Errorf("Sprint.ID = %q, want %q (should be unchanged)", readState.Sprint.ID, "sprint-1")
	}
	if readState.Sprint.Number != 1 {
		t.Errorf("Sprint.Number = %d, want 1 (should be unchanged)", readState.Sprint.Number)
	}
	if readState.Sprint.Status != models.SprintStatusCompleted {
		t.Errorf("Sprint.Status = %v, want COMPLETED (should be unchanged)", readState.Sprint.Status)
	}
	if len(readState.SprintHistory) != 0 {
		t.Errorf("SprintHistory length = %d, want 0 (should be unchanged)", len(readState.SprintHistory))
	}
}
