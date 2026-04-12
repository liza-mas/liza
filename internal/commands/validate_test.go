package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestValidateCommand_RequiredFields(t *testing.T) {
	tests := []struct {
		name        string
		setupState  func() *models.State
		wantErr     bool
		errContains string
	}{
		{
			name: "valid complete state",
			setupState: func() *models.State {
				return testhelpers.CreateValidState()
			},
			wantErr: false,
		},
		{
			name: "missing version",
			setupState: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Version = 0
				return state
			},
			wantErr:     true,
			errContains: "version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			state := tt.setupState()
			testhelpers.WriteInitialState(t, statePath, state)

			// Skip spec file checks for most tests
			err := ValidateCommand(statePath, true)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				testhelpers.AssertErrorContains(t, err, tt.errContains)
			}
		})
	}
}

func TestValidateCommand_TaskStateInvariants(t *testing.T) {
	tests := []struct {
		name        string
		setupTask   func() models.Task
		wantErr     bool
		errContains string
	}{
		{
			name: "DRAFT_CODE with assigned_to",
			setupTask: func() models.Task {
				agent := "coder-1"
				return models.Task{
					ID:          "task-1",
					Description: "Test",
					Status:      models.TaskStatusReady,
					RolePair:    "coding-pair",
					AssignedTo:  &agent,
					Created:     time.Now().UTC(),
					SpecRef:     "specs/test.md",
					DoneWhen:    "Complete",
					History:     []models.TaskHistoryEntry{},
				}
			},
			wantErr:     true,
			errContains: "DRAFT_CODE task with assigned_to",
		},
		{
			name: "IMPLEMENTING without assigned_to",
			setupTask: func() models.Task {
				return models.Task{
					ID:          "task-1",
					Description: "Test",
					Status:      models.TaskStatusImplementing,
					RolePair:    "coding-pair",
					Created:     time.Now().UTC(),
					SpecRef:     "specs/test.md",
					DoneWhen:    "Complete",
					History:     []models.TaskHistoryEntry{},
				}
			},
			wantErr:     true,
			errContains: "IMPLEMENTING_CODE task without assigned_to",
		},
		{
			name: "IMPLEMENTING without worktree",
			setupTask: func() models.Task {
				agent := "coder-1"
				leaseExpires := time.Now().UTC().Add(30 * time.Minute)
				baseCommit := "abc123"
				return models.Task{
					ID:           "task-1",
					Description:  "Test",
					Status:       models.TaskStatusImplementing,
					RolePair:     "coding-pair",
					AssignedTo:   &agent,
					LeaseExpires: &leaseExpires,
					BaseCommit:   &baseCommit,
					Created:      time.Now().UTC(),
					SpecRef:      "specs/test.md",
					DoneWhen:     "Complete",
					History:      []models.TaskHistoryEntry{},
				}
			},
			wantErr:     true,
			errContains: "IMPLEMENTING_CODE task without worktree",
		},
		{
			name: "READY_FOR_REVIEW without review_commit",
			setupTask: func() models.Task {
				return models.Task{
					ID:          "task-1",
					Description: "Test",
					Status:      models.TaskStatusReadyForReview,
					RolePair:    "coding-pair",
					Created:     time.Now().UTC(),
					SpecRef:     "specs/test.md",
					DoneWhen:    "Complete",
					History:     []models.TaskHistoryEntry{},
				}
			},
			wantErr:     true,
			errContains: "READY_FOR_REVIEW task without review_commit",
		},
		{
			name: "APPROVED without review_commit",
			setupTask: func() models.Task {
				approvedBy := "code-reviewer-1"
				return models.Task{
					ID:          "task-1",
					Description: "Test",
					Status:      models.TaskStatusApproved,
					RolePair:    "coding-pair",
					ApprovedBy:  &approvedBy,
					// ReviewCommit intentionally nil
					Created:  time.Now().UTC(),
					SpecRef:  "specs/test.md",
					DoneWhen: "Complete",
					History:  []models.TaskHistoryEntry{},
				}
			},
			wantErr:     true,
			errContains: "APPROVED task without review_commit",
		},
		{
			name: "BLOCKED without blocked_reason",
			setupTask: func() models.Task {
				return models.Task{
					ID:               "task-1",
					Description:      "Test",
					Status:           models.TaskStatusBlocked,
					RolePair:         "coding-pair",
					Created:          time.Now().UTC(),
					SpecRef:          "specs/test.md",
					DoneWhen:         "Complete",
					BlockedQuestions: []string{"How to proceed?"},
					History:          []models.TaskHistoryEntry{},
				}
			},
			wantErr:     true,
			errContains: "BLOCKED task without blocked_reason",
		},
		{
			name: "REJECTED without rejection_reason",
			setupTask: func() models.Task {
				return models.Task{
					ID:          "task-1",
					Description: "Test",
					Status:      models.TaskStatusRejected,
					RolePair:    "coding-pair",
					Created:     time.Now().UTC(),
					SpecRef:     "specs/test.md",
					DoneWhen:    "Complete",
					History:     []models.TaskHistoryEntry{},
				}
			},
			wantErr:     true,
			errContains: "REJECTED task without rejection_reason",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{tt.setupTask()}

			testhelpers.WriteInitialState(t, statePath, state)

			err := ValidateCommand(statePath, true) // Skip spec file check
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				testhelpers.AssertErrorContains(t, err, tt.errContains)
			}
		})
	}
}

func TestValidateCommand_Dependencies(t *testing.T) {
	tests := []struct {
		name        string
		setupTasks  func() []models.Task
		wantErr     bool
		errContains string
	}{
		{
			name: "depends_on references non-existent task",
			setupTasks: func() []models.Task {
				return []models.Task{
					{
						ID:          "task-1",
						Description: "Test",
						Status:      models.TaskStatusReady,
						RolePair:    "coding-pair",
						DependsOn:   []string{"task-nonexistent"},
						Created:     time.Now().UTC(),
						SpecRef:     "specs/test.md",
						DoneWhen:    "Complete",
						History:     []models.TaskHistoryEntry{},
					},
				}
			},
			wantErr:     true,
			errContains: "non-existent task",
		},
		{
			name: "IMPLEMENTING task with unmet dependencies",
			setupTasks: func() []models.Task {
				agent := "coder-1"
				worktree := "wt-task-1"
				baseCommit := "abc123"
				leaseExpires := time.Now().UTC().Add(30 * time.Minute)
				return []models.Task{
					{
						ID:          "task-2",
						Description: "Dependency",
						Status:      models.TaskStatusReady, // Not MERGED
						RolePair:    "coding-pair",
						Created:     time.Now().UTC(),
						SpecRef:     "specs/test.md",
						DoneWhen:    "Complete",
						History:     []models.TaskHistoryEntry{},
					},
					{
						ID:           "task-1",
						Description:  "Test",
						Status:       models.TaskStatusImplementing,
						RolePair:     "coding-pair",
						AssignedTo:   &agent,
						Worktree:     &worktree,
						BaseCommit:   &baseCommit,
						LeaseExpires: &leaseExpires,
						DependsOn:    []string{"task-2"},
						Created:      time.Now().UTC(),
						SpecRef:      "specs/test.md",
						DoneWhen:     "Complete",
						History:      []models.TaskHistoryEntry{},
					},
				}
			},
			wantErr:     true,
			errContains: "unmet dependencies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			state := testhelpers.CreateValidState()
			state.Tasks = tt.setupTasks()

			// Create worktree directories if tasks have them
			for _, task := range state.Tasks {
				if task.Worktree != nil {
					wtPath := filepath.Join(tmpDir, *task.Worktree)
					if err := os.MkdirAll(wtPath, 0755); err != nil {
						t.Fatal(err)
					}
				}
			}

			testhelpers.WriteInitialState(t, statePath, state)

			err := ValidateCommand(statePath, true)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				testhelpers.AssertErrorContains(t, err, tt.errContains)
			}
		})
	}
}

func TestValidateCommand_AgentInvariants(t *testing.T) {
	tests := []struct {
		name        string
		setupAgent  func() map[string]models.Agent
		wantErr     bool
		errContains string
	}{
		{
			name: "WORKING agent without current_task",
			setupAgent: func() map[string]models.Agent {
				return map[string]models.Agent{
					"coder-1": {
						Role:      "coder",
						Status:    models.AgentStatusWorking,
						Heartbeat: time.Now().UTC(),
						Terminal:  "term-1",
					},
				}
			},
			wantErr:     true,
			errContains: "WORKING but no current_task",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			state := testhelpers.CreateValidState()
			state.Agents = tt.setupAgent()

			testhelpers.WriteInitialState(t, statePath, state)

			err := ValidateCommand(statePath, true)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				testhelpers.AssertErrorContains(t, err, tt.errContains)
			}
		})
	}
}

func TestValidateAgentInvariants_LeaseExpiryGracePeriod(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name        string
		leaseExpiry time.Time
		wantWarning bool
	}{
		{
			name:        "within grace period",
			leaseExpiry: now.Add(-(models.LeaseExpiryGracePeriod - 30*time.Second)),
			wantWarning: false,
		},
		{
			// Lease expired exactly at the grace boundary should not warn.
			// Before() is strict <, so equal-to-deadline is not "before" it.
			// 100ms buffer accounts for wall-clock drift between test and function
			// (1ms was insufficient on CI under load).
			name:        "exactly at grace period boundary",
			leaseExpiry: now.Add(-models.LeaseExpiryGracePeriod + 100*time.Millisecond),
			wantWarning: false,
		},
		{
			name:        "past grace period",
			leaseExpiry: now.Add(-(models.LeaseExpiryGracePeriod + 30*time.Second)),
			wantWarning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentTask := "task-1"
			state := &models.State{
				Agents: map[string]models.Agent{
					"coder-1": {
						Role:         "coder",
						Status:       models.AgentStatusWorking,
						CurrentTask:  &currentTask,
						LeaseExpires: &tt.leaseExpiry,
						Heartbeat:    now,
						Terminal:     "term-1",
					},
				},
			}

			var buf bytes.Buffer
			warnWriter = &buf
			defer func() { warnWriter = os.Stderr }()

			validateErr := validateAgentInvariants(state, "", true)
			if validateErr != nil {
				t.Fatalf("validateAgentInvariants() error = %v", validateErr)
			}

			hasWarning := strings.Contains(buf.String(), "lease expired")
			if hasWarning != tt.wantWarning {
				t.Errorf("warning present = %v, want %v; output=%q", hasWarning, tt.wantWarning, buf.String())
			}
		})
	}
}

func TestValidateCommand_DuplicateAssignments(t *testing.T) {
	tests := []struct {
		name        string
		setupTasks  func() []models.Task
		wantErr     bool
		errContains string
	}{
		{
			name: "agent with multiple IMPLEMENTING tasks fails",
			setupTasks: func() []models.Task {
				agent := "coder-1"
				worktree1 := "wt-task-1"
				worktree2 := "wt-task-2"
				baseCommit := "abc123"
				leaseExpires := time.Now().UTC().Add(30 * time.Minute)
				return []models.Task{
					{
						ID:           "task-1",
						Description:  "Test 1",
						Status:       models.TaskStatusImplementing,
						RolePair:     "coding-pair",
						AssignedTo:   &agent,
						Worktree:     &worktree1,
						BaseCommit:   &baseCommit,
						LeaseExpires: &leaseExpires,
						Created:      time.Now().UTC(),
						SpecRef:      "specs/test.md",
						DoneWhen:     "Complete",
						History:      []models.TaskHistoryEntry{},
					},
					{
						ID:           "task-2",
						Description:  "Test 2",
						Status:       models.TaskStatusImplementing,
						RolePair:     "coding-pair",
						AssignedTo:   &agent,
						Worktree:     &worktree2,
						BaseCommit:   &baseCommit,
						LeaseExpires: &leaseExpires,
						Created:      time.Now().UTC(),
						SpecRef:      "specs/test.md",
						DoneWhen:     "Complete",
						History:      []models.TaskHistoryEntry{},
					},
				}
			},
			wantErr:     true,
			errContains: "assigned to multiple active tasks",
		},
		{
			name: "agent with REJECTED and IMPLEMENTING tasks passes",
			setupTasks: func() []models.Task {
				agent := "coder-1"
				worktree := "wt-task-2"
				baseCommit := "abc123"
				now := time.Now().UTC()
				leaseExpires := now.Add(30 * time.Minute)
				rejectionReason := "Not good enough"
				return []models.Task{
					{
						ID:              "task-1",
						Description:     "Rejected task",
						Status:          models.TaskStatusRejected,
						RolePair:        "coding-pair",
						AssignedTo:      &agent,
						RejectionReason: &rejectionReason,
						Created:         now,
						SpecRef:         "specs/test.md",
						DoneWhen:        "Complete",
						History:         []models.TaskHistoryEntry{},
						HandoffEvents: []models.HandoffEvent{
							{Timestamp: now, Agent: "coder-1", Trigger: models.HandoffTriggerSubmission},
						},
					},
					{
						ID:           "task-2",
						Description:  "Active task",
						Status:       models.TaskStatusImplementing,
						RolePair:     "coding-pair",
						AssignedTo:   &agent,
						Worktree:     &worktree,
						BaseCommit:   &baseCommit,
						LeaseExpires: &leaseExpires,
						Created:      time.Now().UTC(),
						SpecRef:      "specs/test.md",
						DoneWhen:     "Complete",
						History:      []models.TaskHistoryEntry{},
					},
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			state := testhelpers.CreateValidState()
			state.Tasks = tt.setupTasks()

			// Create worktree directories if tasks have them
			for _, task := range state.Tasks {
				if task.Worktree != nil {
					wtPath := filepath.Join(tmpDir, *task.Worktree)
					if err := os.MkdirAll(wtPath, 0755); err != nil {
						t.Fatal(err)
					}
				}
			}

			testhelpers.WriteInitialState(t, statePath, state)

			err := ValidateCommand(statePath, true)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				testhelpers.AssertErrorContains(t, err, tt.errContains)
			}
		})
	}
}

func TestValidateCommand_SpecFileValidation(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Create a spec file
	specFile := testhelpers.CreateSpecFile(t, tmpDir, "test.md", "# Test Spec\n")

	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "specs/test.md"

	testhelpers.WriteInitialState(t, statePath, state)

	// Should pass with spec file check
	if err := ValidateCommand(statePath, false); err != nil {
		t.Errorf("ValidateCommand() with existing spec file error = %v", err)
	}

	// Remove spec file
	os.Remove(specFile)

	// Should fail without skip flag
	if err := ValidateCommand(statePath, false); err == nil {
		t.Error("ValidateCommand() should fail for missing spec file")
	}

	// Should pass with skip flag
	if err := ValidateCommand(statePath, true); err != nil {
		t.Errorf("ValidateCommand() with skip spec check error = %v", err)
	}
}

func TestValidateAnomalies_RequiredDetailsByType(t *testing.T) {
	tests := []struct {
		name        string
		anomaly     models.Anomaly
		errContains string
	}{
		{
			name: "retry_loop missing required details fails",
			anomaly: models.Anomaly{
				Type:    "retry_loop",
				Details: map[string]any{"count": 3},
			},
			errContains: "retry_loop anomaly",
		},
		{
			name: "trade_off missing required details fails",
			anomaly: models.Anomaly{
				Type:    "trade_off",
				Details: map[string]any{"what": "faster claim path", "why": "reduce lock contention"},
			},
			errContains: "trade_off anomaly",
		},
		{
			name: "external_blocker missing required details fails",
			anomaly: models.Anomaly{
				Type:    "external_blocker",
				Details: map[string]any{"note": "service unavailable"},
			},
			errContains: "external_blocker anomaly",
		},
		{
			name: "assumption_violated missing required details fails",
			anomaly: models.Anomaly{
				Type:    "assumption_violated",
				Details: map[string]any{"assumption": "state file always present"},
			},
			errContains: "assumption_violated anomaly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			state.Anomalies = []models.Anomaly{tt.anomaly}

			err := validateAnomalies(state, t.TempDir(), true)
			if err == nil {
				t.Fatalf("validateAnomalies() error = nil, want error containing %q", tt.errContains)
			}
			testhelpers.AssertErrorContains(t, err, tt.errContains)
		})
	}
}

func TestValidateAnomalies_RequestedTypeBranchesPassWithValidDetails(t *testing.T) {
	tests := []struct {
		name    string
		anomaly models.Anomaly
	}{
		{
			name: "retry_loop branch",
			anomaly: models.Anomaly{
				Type:    "retry_loop",
				Details: map[string]any{"count": 3, "error_pattern": "timeout"},
			},
		},
		{
			name: "trade_off branch",
			anomaly: models.Anomaly{
				Type: "trade_off",
				Details: map[string]any{
					"what":         "skip cache warmup",
					"why":          "reduce startup time",
					"debt_created": "slower first request",
				},
			},
		},
		{
			name: "spec_ambiguity branch",
			anomaly: models.Anomaly{
				Type:    "spec_ambiguity",
				Details: map[string]any{},
			},
		},
		{
			name: "external_blocker branch",
			anomaly: models.Anomaly{
				Type:    "external_blocker",
				Details: map[string]any{"blocker_service": "github"},
			},
		},
		{
			name: "assumption_violated branch",
			anomaly: models.Anomaly{
				Type:    "assumption_violated",
				Details: map[string]any{"assumption": "single reviewer", "reality": "reviewer unavailable"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			state.Anomalies = []models.Anomaly{tt.anomaly}

			if err := validateAnomalies(state, t.TempDir(), true); err != nil {
				t.Fatalf("validateAnomalies() error = %v, want nil", err)
			}
		})
	}
}

func TestSetWarnWriter(t *testing.T) {
	// Save and restore original writer
	original := warnWriter
	defer func() { warnWriter = original }()

	var buf bytes.Buffer
	SetWarnWriter(&buf)

	if warnWriter != &buf {
		t.Fatal("SetWarnWriter did not update warnWriter")
	}

	// Restore to stderr
	SetWarnWriter(os.Stderr)
	if warnWriter != os.Stderr {
		t.Fatal("SetWarnWriter did not restore warnWriter to os.Stderr")
	}
}
