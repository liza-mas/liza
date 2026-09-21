package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestPrintAssessBlockedResult(t *testing.T) {
	root := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("target", models.TaskStatusBlocked, time.Now().UTC())}
	bb := testhelpers.WriteInitialState(t, stateFile, state)
	captureAssessBlockedStdout(t, func() error { return AssessBlockedCommand(root, "target", "await provider", "orchestrator-1") })
	before, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ops.AssessBlocked(root, "target", "await provider", "orchestrator-1")
	if err != nil || result.Outcome != models.LifecycleNoChange {
		t.Fatalf("repeat: %+v %v", result, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	avoided, ok := fields["suppressed_entry_bytes"].(float64)
	if !ok || avoided <= 0 || fields["changed"] != false {
		t.Fatalf("JSON suppression evidence: %s", encoded)
	}
	got := captureAssessBlockedStdout(t, func() error { return printAssessBlockedResult(result, nil) })
	var expected bytes.Buffer
	WriteLifecycleOutcome(&expected, result.LifecycleOutcome)
	fmt.Fprintf(&expected, "suppressed_entry_bytes: %.0f\n", avoided)
	if got != expected.String() {
		t.Fatalf("text=%q, want=%q", got, expected.String())
	}
	got = captureAssessBlockedStdout(t, func() error { return AssessBlockedCommand(root, "target", "await provider", "orchestrator-1") })
	if !strings.Contains(got, "outcome: NO_CHANGE\n") || !strings.Contains(got, "suppressed_entry_bytes: ") || strings.Contains(got, "await provider") {
		t.Fatalf("command output: %q", got)
	}
	after, err := os.ReadFile(stateFile)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("duplicate changed state: %v", err)
	}
	stored, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []string{models.LifecycleAlreadyCompleted, models.LifecycleAlreadyTransitioned, models.LifecycleStaleCaller, models.LifecycleStateChanged, models.LifecycleRetryable, models.LifecycleInvalidInput, models.LifecycleForbidden, models.LifecycleConflict} {
		t.Run(outcome, func(t *testing.T) {
			result := &ops.AssessBlockedResult{TaskID: "target", LifecycleOutcome: ops.NewLifecycleOutcome("assess-blocked", stored.FindTask("target"), outcome, "stop", "none")}
			var expected bytes.Buffer
			WriteLifecycleOutcome(&expected, result.LifecycleOutcome)
			got := captureAssessBlockedStdout(t, func() error { return printAssessBlockedResult(result, nil) })
			if got != expected.String() {
				t.Fatalf("changed existing output: %q, want=%q", got, expected.String())
			}
			encoded, err := json.Marshal(result)
			if err != nil || bytes.Contains(encoded, []byte("suppressed_entry_bytes")) {
				t.Fatalf("unexpected suppression field: %s %v", encoded, err)
			}
		})
	}
}

func TestAssessBlockedCommand_ReconcilesCanonicalMetadata(t *testing.T) {
	tests := []struct {
		name          string
		taskID        string
		repairRequest *models.RepairRequest
		wantRepair    string
	}{
		{
			name:   "command-style repair request",
			taskID: "task-command-repair",
			repairRequest: &models.RepairRequest{
				Operation:  "retarget-dependency",
				Target:     "task-command-repair",
				Command:    "liza retarget-dependency task-command-repair stale replacement --json",
				Evidence:   []string{"command=retarget exit_code=1 stderr=orchestrator authority required"},
				Validation: []string{"liza validate --json"},
			},
			wantRepair: `{"operation":"retarget-dependency","target":"task-command-repair","command":"liza retarget-dependency task-command-repair stale replacement --json","evidence":["command=retarget exit_code=1 stderr=orchestrator authority required"],"validation":["liza validate --json"]}`,
		},
		{
			name:   "declarative repair request",
			taskID: "task-declarative-repair",
			repairRequest: &models.RepairRequest{
				Operation: "apply-dependency-repair",
				Target:    "task-declarative-repair",
				DependencyUpdates: []models.DependencyUpdate{{
					TaskID:            "consumer",
					ExpectedDependsOn: []string{},
					DesiredDependsOn:  []string{"producer"},
				}},
				Evidence:   []string{"error=dependency repair requires orchestrator authority"},
				Validation: []string{"liza validate --json"},
			},
			wantRepair: `{"operation":"apply-dependency-repair","target":"task-declarative-repair","dependency_updates":[{"task_id":"consumer","expected_depends_on":[],"desired_depends_on":["producer"]}],"evidence":["error=dependency repair requires orchestrator authority"],"validation":["liza validate --json"]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
			now := time.Now().UTC()
			oldReason := "obsolete blocker"
			state := testhelpers.CreateValidState()
			task := testhelpers.BuildTaskByStatus(tt.taskID, models.TaskStatusBlocked, now)
			task.AssignedTo = nil
			task.Worktree = nil
			task.SpecRef = state.Goal.SpecRef
			task.BlockedReason = &oldReason
			task.BlockedQuestions = []string{"obsolete question"}
			state.Tasks = []models.Task{task}
			testhelpers.WriteInitialState(t, statePath, state)

			stdout := captureAssessBlockedStdout(t, func() error {
				return AssessBlockedWithOptionsCommand(tmpDir, tt.taskID, "current assessment", "orchestrator-1", ops.AssessBlockedOptions{
					Reason:        "current blocker",
					Questions:     []string{"Can the orchestrator repair it?"},
					RepairRequest: tt.repairRequest,
				})
			})

			for _, want := range []string{
				"Reason: current blocker\n",
				`Questions: ["Can the orchestrator repair it?"]` + "\n",
				"Repair request: " + tt.wantRepair + "\n",
			} {
				if !strings.Contains(stdout, want) {
					t.Fatalf("output = %q, want exact fragment %q", stdout, want)
				}
			}
		})
	}
}

func captureAssessBlockedStdout(t *testing.T, run func() error) string {
	t.Helper()
	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = oldStdout })

	runErr := run()
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("close stdout writer: %v", closeErr)
	}
	os.Stdout = oldStdout
	if runErr != nil {
		t.Fatalf("AssessBlockedWithOptionsCommand() error = %v", runErr)
	}

	var output bytes.Buffer
	if _, err := io.Copy(&output, reader); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	return output.String()
}

func TestAssessBlockedCommand(t *testing.T) {
	tests := []struct {
		name          string
		taskID        string
		note          string
		agentID       string
		setupState    func(*models.State)
		wantErr       bool
		wantErrMsg    string
		validateState func(*testing.T, *models.State)
	}{
		{
			name:    "Success - assess BLOCKED task",
			taskID:  "task-1",
			note:    "Cannot resolve without external API",
			agentID: "orchestrator-1",
			wantErr: false,
			setupState: func(s *models.State) {
				now := time.Now().UTC()
				s.Tasks = []models.Task{
					{
						ID:          "task-1",
						Description: "Test task",
						Status:      models.TaskStatusBlocked,
						Created:     now,
						History:     []models.TaskHistoryEntry{},
					},
				}
			},
			validateState: func(t *testing.T, s *models.State) {
				task := &s.Tasks[0]
				if task.Status != models.TaskStatusBlocked {
					t.Errorf("expected status BLOCKED, got %s", task.Status)
				}
				if len(task.History) != 1 {
					t.Fatalf("expected 1 history entry, got %d", len(task.History))
				}
				if task.History[0].Event != "orchestrator_assessment" {
					t.Errorf("expected event orchestrator_assessment, got %s", task.History[0].Event)
				}
				if task.History[0].Agent == nil || *task.History[0].Agent != "orchestrator-1" {
					t.Errorf("expected agent orchestrator-1 in history, got %v", task.History[0].Agent)
				}
				if task.History[0].Note == nil || *task.History[0].Note != "Cannot resolve without external API" {
					t.Errorf("expected note in history, got %v", task.History[0].Note)
				}
			},
		},
		{
			name:       "Error - Empty task ID",
			taskID:     "",
			agentID:    "orchestrator-1",
			wantErr:    true,
			wantErrMsg: "/task_id must not be empty",
		},
		{
			name:       "Error - Empty agent ID",
			taskID:     "task-1",
			agentID:    "",
			wantErr:    true,
			wantErrMsg: "agent ID is required",
		},
		{
			name:       "Error - Task not found",
			taskID:     "nonexistent",
			agentID:    "orchestrator-1",
			wantErr:    true,
			wantErrMsg: "task not found",
			setupState: func(s *models.State) {
				s.Tasks = []models.Task{}
			},
		},
		{
			name:       "Error - Task not in BLOCKED status",
			taskID:     "task-1",
			agentID:    "orchestrator-1",
			wantErr:    true,
			wantErrMsg: "BLOCKED status",
			setupState: func(s *models.State) {
				now := time.Now().UTC()
				s.Tasks = []models.Task{
					{
						ID:          "task-1",
						Description: "Test task",
						Status:      models.TaskStatusReady,
						Created:     now,
						History:     []models.TaskHistoryEntry{},
					},
				}
			},
		},
		{
			name:    "Success - idempotent (two calls = two entries)",
			taskID:  "task-1",
			note:    "second assessment",
			agentID: "orchestrator-1",
			wantErr: false,
			setupState: func(s *models.State) {
				now := time.Now().UTC()
				agent := "orchestrator-1"
				s.Tasks = []models.Task{
					{
						ID:          "task-1",
						Description: "Test task",
						Status:      models.TaskStatusBlocked,
						Created:     now,
						History: []models.TaskHistoryEntry{
							{
								Time:  now.Add(-10 * time.Minute),
								Event: "orchestrator_assessment",
								Agent: &agent,
							},
						},
					},
				}
			},
			validateState: func(t *testing.T, s *models.State) {
				task := &s.Tasks[0]
				assessmentCount := 0
				for _, entry := range task.History {
					if entry.Event == "orchestrator_assessment" {
						assessmentCount++
					}
				}
				if assessmentCount != 2 {
					t.Errorf("expected 2 assessment entries, got %d", assessmentCount)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

			initialState := &models.State{
				Config: models.Config{
					IntegrationBranch: "integration",
					LeaseDuration:     1800,
				},
				Tasks:  []models.Task{},
				Agents: make(map[string]models.Agent),
			}

			if tt.setupState != nil {
				tt.setupState(initialState)
			}

			bb := testhelpers.WriteInitialState(t, statePath, initialState)

			err := AssessBlockedCommand(tmpDir, tt.taskID, tt.note, tt.agentID)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				} else {
					testhelpers.AssertErrorContains(t, err, tt.wantErrMsg)
				}
			} else {
				testhelpers.AssertNoError(t, err)
			}

			if !tt.wantErr && tt.validateState != nil {
				state, err := bb.Read()
				if err != nil {
					t.Fatalf("failed to read state: %v", err)
				}
				tt.validateState(t, state)
			}
		})
	}
}
