package commands

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestHandoffCommand(t *testing.T) {
	tests := []struct {
		name          string
		input         *ops.HandoffInput
		setupState    func(*models.State)
		wantErr       bool
		wantErrSubstr string
		validateState func(*testing.T, *models.State)
	}{
		{
			name: "successful handoff with legacy fields",
			input: &ops.HandoffInput{
				TaskID:     "task-1",
				Summary:    "Implemented parsing and validation",
				NextAction: "Finish edge case tests",
				AgentID:    "coder-1",
			},
			setupState: func(s *models.State) {
				now := time.Now().UTC()
				assigned := "coder-1"
				lease := now.Add(30 * time.Minute)
				s.Tasks = []models.Task{
					{
						ID:           "task-1",
						Description:  "Test task",
						Status:       models.TaskStatusImplementing,
						AssignedTo:   &assigned,
						LeaseExpires: &lease,
						Created:      now,
						SpecRef:      "README.md",
						DoneWhen:     "Done",
						Scope:        "Scope",
						History:      []models.TaskHistoryEntry{},
					},
				}
				s.Agents["coder-1"] = models.Agent{
					Role:      "coder",
					Status:    models.AgentStatusWorking,
					Heartbeat: now,
				}
			},
			validateState: func(t *testing.T, s *models.State) {
				task := &s.Tasks[0]
				if !task.HandoffPending {
					t.Fatalf("expected handoff_pending=true")
				}
				if len(task.History) != 1 {
					t.Fatalf("expected one history entry, got %d", len(task.History))
				}
				if task.History[0].Event != "handoff_initiated" {
					t.Fatalf("expected handoff_initiated event, got %s", task.History[0].Event)
				}

				// Verify HandoffEvent with backward compat mapping
				if len(task.HandoffEvents) != 1 {
					t.Fatalf("expected 1 handoff event, got %d", len(task.HandoffEvents))
				}
				he := task.HandoffEvents[0]
				if he.Agent != "coder-1" {
					t.Fatalf("expected handoff agent coder-1, got %s", he.Agent)
				}
				if len(he.Succeeded) != 1 || he.Succeeded[0] != "Implemented parsing and validation" {
					t.Fatalf("expected succeeded=[summary], got %v", he.Succeeded)
				}
				if he.NextStep != "Finish edge case tests" {
					t.Fatalf("expected next_step from next_action, got %s", he.NextStep)
				}

				agent, ok := s.Agents["coder-1"]
				if !ok {
					t.Fatalf("expected coder-1 agent in state")
				}
				if agent.Status != models.AgentStatusHandoff {
					t.Fatalf("expected agent status HANDOFF, got %s", agent.Status)
				}
				if agent.CurrentTask == nil || *agent.CurrentTask != "task-1" {
					t.Fatalf("expected agent current_task task-1, got %v", agent.CurrentTask)
				}
			},
		},
		{
			name: "successful handoff with structured fields",
			input: &ops.HandoffInput{
				TaskID:     "task-1",
				Summary:    "legacy summary",
				NextAction: "next step",
				AgentID:    "coder-1",
				Succeeded:  []string{"built parser", "added validation"},
				Failed:     []string{"edge case handling"},
				Hypothesis: "nil pointer on empty input",
				KeyFiles:   []string{"parser.go"},
				DeadEnds:   []string{"regex approach"},
			},
			setupState: func(s *models.State) {
				now := time.Now().UTC()
				assigned := "coder-1"
				lease := now.Add(30 * time.Minute)
				s.Tasks = []models.Task{
					{
						ID:           "task-1",
						Description:  "Test task",
						Status:       models.TaskStatusImplementing,
						AssignedTo:   &assigned,
						LeaseExpires: &lease,
						Created:      now,
						SpecRef:      "README.md",
						DoneWhen:     "Done",
						Scope:        "Scope",
						History:      []models.TaskHistoryEntry{},
					},
				}
				s.Agents["coder-1"] = models.Agent{
					Role:      "coder",
					Status:    models.AgentStatusWorking,
					Heartbeat: now,
				}
			},
			validateState: func(t *testing.T, s *models.State) {
				task := &s.Tasks[0]
				if len(task.HandoffEvents) != 1 {
					t.Fatalf("expected 1 handoff event, got %d", len(task.HandoffEvents))
				}
				he := task.HandoffEvents[0]
				// Succeeded should use explicit value, not legacy summary
				if len(he.Succeeded) != 2 || he.Succeeded[0] != "built parser" {
					t.Fatalf("expected explicit succeeded, got %v", he.Succeeded)
				}
				if len(he.Failed) != 1 || he.Failed[0] != "edge case handling" {
					t.Fatalf("expected failed, got %v", he.Failed)
				}
				if he.Hypothesis != "nil pointer on empty input" {
					t.Fatalf("expected hypothesis, got %s", he.Hypothesis)
				}
				if len(he.KeyFiles) != 1 || he.KeyFiles[0] != "parser.go" {
					t.Fatalf("expected key_files, got %v", he.KeyFiles)
				}
				if len(he.DeadEnds) != 1 || he.DeadEnds[0] != "regex approach" {
					t.Fatalf("expected dead_ends, got %v", he.DeadEnds)
				}
			},
		},
		{
			name: "missing task ID",
			input: &ops.HandoffInput{
				Summary:    "summary",
				NextAction: "next",
				AgentID:    "coder-1",
			},
			wantErr:       true,
			wantErrSubstr: "task ID is required",
		},
		{
			name: "missing summary",
			input: &ops.HandoffInput{
				TaskID:     "task-1",
				NextAction: "next",
				AgentID:    "coder-1",
			},
			wantErr:       true,
			wantErrSubstr: "/summary must not be empty",
		},
		{
			name: "missing next action",
			input: &ops.HandoffInput{
				TaskID:  "task-1",
				Summary: "summary",
				AgentID: "coder-1",
			},
			wantErr:       true,
			wantErrSubstr: "/next_action must not be empty",
		},
		{
			name: "missing agent",
			input: &ops.HandoffInput{
				TaskID:     "task-1",
				Summary:    "summary",
				NextAction: "next",
			},
			wantErr:       true,
			wantErrSubstr: brand.EnvName("AGENT_ID") + " is required",
		},
		{
			name: "task not found",
			input: &ops.HandoffInput{
				TaskID:     "missing",
				Summary:    "summary",
				NextAction: "next",
				AgentID:    "coder-1",
			},
			setupState: func(s *models.State) {
				s.Tasks = []models.Task{}
			},
			wantErr:       true,
			wantErrSubstr: "task not found",
		},
		{
			name: "task not claimed",
			input: &ops.HandoffInput{
				TaskID:     "task-1",
				Summary:    "summary",
				NextAction: "next",
				AgentID:    "coder-1",
			},
			setupState: func(s *models.State) {
				now := time.Now().UTC()
				s.Tasks = []models.Task{
					{
						ID:          "task-1",
						Description: "Test",
						Status:      models.TaskStatusReady,
						Created:     now,
						SpecRef:     "README.md",
						DoneWhen:    "Done",
						Scope:       "Scope",
						History:     []models.TaskHistoryEntry{},
					},
				}
			},
			wantErr:       true,
			wantErrSubstr: "is not in an executing status",
		},
		{
			name: "task assigned to different coder",
			input: &ops.HandoffInput{
				TaskID:     "task-1",
				Summary:    "summary",
				NextAction: "next",
				AgentID:    "coder-1",
			},
			setupState: func(s *models.State) {
				now := time.Now().UTC()
				assigned := "coder-2"
				s.Tasks = []models.Task{
					{
						ID:          "task-1",
						Description: "Test",
						Status:      models.TaskStatusImplementing,
						AssignedTo:  &assigned,
						Created:     now,
						SpecRef:     "README.md",
						DoneWhen:    "Done",
						Scope:       "Scope",
						History:     []models.TaskHistoryEntry{},
					},
				}
			},
			wantErr:       true,
			wantErrSubstr: "is not assigned to agent coder-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

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

			err := HandoffCommand(tmpDir, tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErrSubstr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if tt.validateState != nil {
				state, readErr := bb.Read()
				if readErr != nil {
					t.Fatalf("failed to read state: %v", readErr)
				}
				tt.validateState(t, state)
			}
		})
	}
}

// TestHandoffPreflightParity proves the preflight validates exactly the
// canonical object the handoff boundary builds from the same input.
func TestHandoffPreflightParity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     *ops.HandoffInput
		wantField string
	}{
		{
			name:  "required fields only",
			input: &ops.HandoffInput{Summary: "context at 90%", NextAction: "continue from the schema"},
		},
		{
			name: "every optional field supplied",
			input: &ops.HandoffInput{
				Summary: "s", NextAction: "n", Hypothesis: "the lock is held twice",
				Succeeded: []string{"schema"}, Failed: []string{"parity"},
				KeyFiles: []string{"internal/ops/handoff.go"}, DeadEnds: []string{"retry loop"},
			},
		},
		{
			name:      "missing next action",
			input:     &ops.HandoffInput{Summary: "s"},
			wantField: "/next_action",
		},
		{
			name:      "empty entry in an optional list",
			input:     &ops.HandoffInput{Summary: "s", NextAction: "n", DeadEnds: []string{""}},
			wantField: "/dead_ends/0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ValidatePayload("handoff", ops.HandoffPayload(tt.input))
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("ValidatePayload() error = %v, want the payload accepted", err)
				}
				if result.SchemaVersion != 1 {
					t.Errorf("schema_version = %d, want 1", result.SchemaVersion)
				}
				return
			}

			var lifecycleErr *ops.LifecycleError
			if !errors.As(err, &lifecycleErr) {
				t.Fatalf("ValidatePayload() error = %v (%T), want a *ops.LifecycleError", err, err)
			}
			if lifecycleErr.Outcome.Outcome != models.LifecycleInvalidInput {
				t.Errorf("outcome = %s, want INVALID_INPUT", lifecycleErr.Outcome.Outcome)
			}
			if len(lifecycleErr.Outcome.Diagnostics) != 1 || lifecycleErr.Outcome.Diagnostics[0].Field != tt.wantField {
				t.Errorf("diagnostics = %+v, want one entry naming %s", lifecycleErr.Outcome.Diagnostics, tt.wantField)
			}
		})
	}
}
