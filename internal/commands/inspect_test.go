package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestInspectHumanNotes(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.HumanNotes = []models.HumanNote{{
		Timestamp: time.Date(2026, 9, 13, 16, 30, 0, 0, time.UTC),
		For:       "task-1",
		Message:   "Read docs/recovery.md before reassessment.\nPreserve the current worktree.",
		Extra:     map[string]any{"source": "operator_cli", "operation": "add-human-note", "private_metadata": "not part of query"},
	}}
	testhelpers.WriteInitialState(t, statePath, state)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"json", "yaml", "value"} {
		t.Run(format, func(t *testing.T) {
			output, err := InspectCommand([]string{"human_notes"}, InspectOptions{ProjectRoot: root, Format: format})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output, "Preserve the current worktree.") || strings.Contains(output, "private_metadata") {
				t.Fatalf("incorrect note projection: %s", output)
			}
			if format == "json" {
				var notes []map[string]any
				if err := json.Unmarshal([]byte(output), &notes); err != nil {
					t.Fatal(err)
				}
				if len(notes) != 1 || notes[0]["message"] != state.HumanNotes[0].Message || notes[0]["for"] != "task-1" || notes[0]["timestamp"] != "2026-09-13T16:30:00Z" || notes[0]["source"] != "operator_cli" || notes[0]["operation"] != "add-human-note" {
					t.Fatalf("missing note content/provenance: %v", notes)
				}
			}
		})
	}
	for _, tc := range []struct {
		args []string
		opts InspectOptions
	}{
		{[]string{"human_notes", "task-1"}, InspectOptions{Format: "json"}},
		{[]string{"human_notes"}, InspectOptions{Format: "json", Summary: true}},
		{[]string{"human_notes"}, InspectOptions{Format: "json", Zombies: true}},
	} {
		tc.opts.ProjectRoot = root
		if _, err := InspectCommand(tc.args, tc.opts); err == nil {
			t.Fatalf("unsupported query accepted: %v %+v", tc.args, tc.opts)
		}
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("inspection changed state")
	}
}

func TestInspectCommand(t *testing.T) {
	// Create test state
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Create a valid state
	now := time.Now()
	state := &models.State{
		Version: 1,
		Config: models.Config{
			Mode:               models.SystemModeRunning,
			MaxCoderIterations: 10,
			MaxReviewCycles:    5,
			IntegrationBranch:  "main",
		},
		Sprint: models.Sprint{
			ID:     "sprint-1",
			Status: models.SprintStatusInProgress,
			Timeline: models.SprintTimeline{
				Started:  now.Add(-2 * time.Hour),
				Deadline: now.Add(6 * time.Hour),
			},
			Metrics: models.SprintMetrics{
				TasksDone:       5,
				TasksInProgress: 3,
				TasksBlocked:    1,
			},
		},
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			Status:      models.GoalStatusInProgress,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Test task 1",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				Created:     now.Add(-1 * time.Hour),
			},
			{
				ID:          "task-2",
				Description: "Test task 2",
				Status:      models.TaskStatusMerged,
				Priority:    2,
				Created:     now.Add(-2 * time.Hour),
			},
		},
		Agents: map[string]models.Agent{
			"coder-1": {
				Role:      "coder",
				Status:    models.AgentStatusIdle,
				Heartbeat: now,
			},
		},
		CircuitBreaker: models.CircuitBreaker{
			Status:    "OK",
			LastCheck: now,
		},
	}

	// Write state to file
	testhelpers.WriteInitialState(t, statePath, state)

	tests := []struct {
		name         string
		args         []string
		opts         InspectOptions
		wantContains []string
		wantErr      bool
		wantNotFound bool
	}{
		{
			name: "inspect config.mode - value format",
			args: []string{"config.mode"},
			opts: InspectOptions{
				Format:      "value",
				ProjectRoot: tmpDir,
			},
			wantContains: []string{"RUNNING"},
			wantErr:      false,
		},
		{
			name: "inspect config.mode - json format",
			args: []string{"config.mode"},
			opts: InspectOptions{
				Format:      "json",
				ProjectRoot: tmpDir,
			},
			wantContains: []string{`"RUNNING"`},
			wantErr:      false,
		},
		{
			name: "inspect sprint.status",
			args: []string{"sprint.status"},
			opts: InspectOptions{
				Format:      "value",
				ProjectRoot: tmpDir,
			},
			wantContains: []string{"IN_PROGRESS"},
			wantErr:      false,
		},
		{
			name: "inspect sprint.metrics.tasks_done",
			args: []string{"sprint.metrics.tasks_done"},
			opts: InspectOptions{
				Format:      "value",
				ProjectRoot: tmpDir,
			},
			wantContains: []string{"5"},
			wantErr:      false,
		},
		{
			name: "inspect computed field - sprint.elapsed",
			args: []string{"sprint.elapsed"},
			opts: InspectOptions{
				Format:      "value",
				ProjectRoot: tmpDir,
			},
			wantContains: []string{"h"}, // Should contain "h" for hours
			wantErr:      false,
		},
		{
			name: "inspect invalid field",
			args: []string{"config.nonexistent"},
			opts: InspectOptions{
				Format:      "value",
				ProjectRoot: tmpDir,
			},
			wantErr:      true,
			wantNotFound: true,
		},
		{
			name: "no args",
			args: []string{},
			opts: InspectOptions{
				Format:      "value",
				ProjectRoot: tmpDir,
			},
			wantErr: true,
		},
		{
			name: "inspect tasks - table format",
			args: []string{"tasks"},
			opts: InspectOptions{
				Format:      "table",
				ProjectRoot: tmpDir,
			},
			wantContains: []string{"task-1", "task-2", "IMPLEMENTING_CODE", "MERGED"},
			wantErr:      false,
		},
		{
			name: "inspect specific task",
			args: []string{"tasks", "task-1"},
			opts: InspectOptions{
				Format:      "value",
				ProjectRoot: tmpDir,
			},
			wantContains: []string{"task-1", "Test task 1", "IMPLEMENTING_CODE"},
			wantErr:      false,
		},
		{
			name: "inspect nonexistent task",
			args: []string{"tasks", "nonexistent"},
			opts: InspectOptions{
				Format:      "value",
				ProjectRoot: tmpDir,
			},
			wantErr:      true,
			wantNotFound: true,
		},
		{
			name: "inspect agents - table format",
			args: []string{"agents"},
			opts: InspectOptions{
				Format:      "table",
				ProjectRoot: tmpDir,
			},
			wantContains: []string{"coder-1", "coder", "IDLE"},
			wantErr:      false,
		},
		{
			name: "summary flag rejects non-task entity",
			args: []string{"agents"},
			opts: InspectOptions{
				Format:      "json",
				ProjectRoot: tmpDir,
				Summary:     true,
			},
			wantErr: true,
		},
		{
			name: "inspect specific agent",
			args: []string{"agents", "coder-1"},
			opts: InspectOptions{
				Format:      "value",
				ProjectRoot: tmpDir,
			},
			wantContains: []string{"coder-1", "coder", "IDLE"},
			wantErr:      false,
		},
		{
			name: "inspect nonexistent agent",
			args: []string{"agents", "nonexistent"},
			opts: InspectOptions{
				Format:      "value",
				ProjectRoot: tmpDir,
			},
			wantErr:      true,
			wantNotFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := InspectCommand(tt.args, tt.opts)

			if (err != nil) != tt.wantErr {
				t.Errorf("InspectCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(output, want) {
					t.Errorf("InspectCommand() output missing %q\nGot:\n%s", want, output)
				}
			}
		})
	}
}

func TestInspectOptions(t *testing.T) {
	tests := []struct {
		name    string
		opts    InspectOptions
		wantErr bool
	}{
		{
			name: "valid format - json",
			opts: InspectOptions{
				Format: "json",
			},
			wantErr: false,
		},
		{
			name: "valid format - yaml",
			opts: InspectOptions{
				Format: "yaml",
			},
			wantErr: false,
		},
		{
			name: "valid format - value",
			opts: InspectOptions{
				Format: "value",
			},
			wantErr: false,
		},
		{
			name: "invalid format",
			opts: InspectOptions{
				Format: "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("InspectOptions.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
