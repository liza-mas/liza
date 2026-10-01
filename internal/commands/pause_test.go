package commands

import (
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestPauseCommand(t *testing.T) {
	tests := []struct {
		name         string
		initialMode  models.SystemMode
		reason       string
		changedBy    string
		wantErr      bool
		errContains  string
		wantMode     models.SystemMode
		checkMessage bool
	}{
		{
			name:         "pause from RUNNING mode",
			initialMode:  models.SystemModeRunning,
			reason:       "Manual intervention",
			changedBy:    "human",
			wantErr:      false,
			wantMode:     models.SystemModePaused,
			checkMessage: true,
		},
		{
			name:         "pause from RUNNING with agent ID",
			initialMode:  models.SystemModeRunning,
			reason:       "Testing",
			changedBy:    "coder-1",
			wantErr:      false,
			wantMode:     models.SystemModePaused,
			checkMessage: true,
		},
		{
			name:         "pause from RUNNING without reason",
			initialMode:  models.SystemModeRunning,
			reason:       "",
			changedBy:    "human",
			wantErr:      false,
			wantMode:     models.SystemModePaused,
			checkMessage: true,
		},
		{
			name:        "cannot pause when STOPPED",
			initialMode: models.SystemModeStopped,
			reason:      "Test",
			changedBy:   "human",
			wantErr:     true,
			errContains: "cannot pause",
			wantMode:    models.SystemModeStopped,
		},
		{
			name:         "pause from empty mode (default to RUNNING)",
			initialMode:  "",
			reason:       "Test",
			changedBy:    "human",
			wantErr:      false,
			wantMode:     models.SystemModePaused,
			checkMessage: true,
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

			// Run pause command
			err := PauseCommand(tmpDir, tt.reason, tt.changedBy)

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

// Pausing an already PAUSED system succeeds as a retry (D86) and leaves the
// recorded pause untouched.
func TestPauseCommand_AlreadyPausedIsIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModePaused
	changedBy := "operator"
	state.Config.ModeChangedBy = &changedBy
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	testhelpers.AssertNoError(t, PauseCommand(tmpDir, "retry", "human"))

	updated, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if updated.Config.Mode != models.SystemModePaused {
		t.Errorf("Mode = %v, want PAUSED", updated.Config.Mode)
	}
	if updated.Config.ModeChangedBy == nil || *updated.Config.ModeChangedBy != changedBy {
		t.Errorf("ModeChangedBy = %v, want unchanged %q", updated.Config.ModeChangedBy, changedBy)
	}
}
