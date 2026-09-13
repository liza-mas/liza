package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"gopkg.in/yaml.v3"
)

func TestGetHumanNotesCLI(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "append then agent reads", true: "empty list"}[empty], func(t *testing.T) {
			root, notePath := setupHumanNoteCLI(t)
			if !empty {
				for _, target := range []string{"target", "all"} {
					if _, err := executeRootCommandCapture(t, root, "add-human-note", target, "--note-file", notePath, "--json"); err != nil {
						t.Fatal(err)
					}
				}
			}
			lp := paths.New(root)
			before, err := os.ReadFile(lp.StatePath())
			if err != nil {
				t.Fatal(err)
			}
			beforeLog, err := os.ReadFile(lp.LogPath())
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			logAbsent := os.IsNotExist(err)
			t.Setenv(brand.EnvName("AGENT_ID"), "orchestrator-1")
			stdout, err := executeRootCommandCapture(t, root, "get", "human_notes", "--json")
			if err != nil {
				t.Fatal(err)
			}
			envelope := parseEnvelope(t, stdout)
			notes, ok := envelope["result"].([]any)
			if envelope["ok"] != true || !ok {
				t.Fatalf("expected successful list: %s", stdout)
			}
			if empty {
				if len(notes) != 0 {
					t.Fatal("empty history returned notes")
				}
			} else {
				if len(notes) != 2 {
					t.Fatalf("notes=%v; want both appended notes", notes)
				}
				for i, target := range []string{"target", "all"} {
					note := notes[i].(map[string]any)
					if note["message"] != "Recovery guidance, not an approval.\n" || note["for"] != target || note["source"] != "operator_cli" || note["operation"] != "add-human-note" {
						t.Fatalf("append/read mismatch: %v", note)
					}
					stamp, ok := note["timestamp"].(string)
					if !ok {
						t.Fatal("timestamp missing")
					}
					if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
						t.Fatal(err)
					}
				}
			}
			after, err := os.ReadFile(lp.StatePath())
			if err != nil {
				t.Fatal(err)
			}
			afterLog, err := os.ReadFile(lp.LogPath())
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) || !bytes.Equal(beforeLog, afterLog) || logAbsent != os.IsNotExist(err) {
				t.Fatal("read changed state or activity log")
			}
		})
	}
}

func TestGetCommand(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir, err := os.MkdirTemp("", "liza-get-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize git repo so paths.GetProjectRoot() works
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init git repo: %v\n%s", err, out)
	}

	// Create the project runtime directory.
	lizaDir := filepath.Join(tmpDir, paths.LizaDirName)
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatalf("failed to create project runtime dir: %v", err)
	}

	// Write pipeline config so role-based agent ID detection works
	if err := embedded.WritePipelineConfig(lizaDir, nil); err != nil {
		t.Fatalf("failed to write pipeline config: %v", err)
	}

	// Create a minimal test state
	now := time.Now()
	coder1 := "coder-1"
	reviewer1 := "code-reviewer-1"
	orchestrator1 := "orchestrator-1"
	task1 := "task-1"
	task2 := "fix-auth-bug"    // Non-standard task ID
	task3 := "feature-xyz-123" // Another non-standard task ID
	task4 := "task-repair-request"
	sequentialReviewer := "code-reviewer-2"
	sequentialDoer := "coder-2"
	previousDoer := "coder-previous"
	reviewerTaskA := "reviewer-sequence-task-a"
	reviewerTaskB := "reviewer-sequence-task-b"
	doerTaskA := "doer-sequence-task-a"
	doerTaskB := "doer-sequence-task-b"
	blockedReason := "Required state repair is orchestrator-only"

	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "specs/vision.md",
			Created:     now,
			Status:      models.GoalStatusInProgress,
		},
		Sprint: models.Sprint{
			ID:      "sprint-1",
			GoalRef: "goal-1",
			Status:  models.SprintStatusInProgress,
			Timeline: models.SprintTimeline{
				Started:  now.Add(-24 * time.Hour),
				Deadline: now.Add(6 * 24 * time.Hour),
			},
			Metrics: models.SprintMetrics{
				TasksDone:       2,
				TasksInProgress: 1,
				TasksBlocked:    0,
			},
		},
		Tasks: []models.Task{
			{
				ID:          task1,
				Description: "Test task",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				AssignedTo:  &coder1,
				SpecRef:     "specs/vision.md",
				DoneWhen:    "Task is complete",
				Scope:       "Test scope",
				Created:     now.Add(-2 * time.Hour),
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-1 * time.Hour), Event: "claimed"},
				},
			},
			{
				ID:          task2,
				Description: "Fix authentication bug",
				Status:      models.TaskStatusReady,
				Priority:    2,
				SpecRef:     "specs/auth.md",
				DoneWhen:    "Auth bug is fixed",
				Scope:       "Authentication module",
				Created:     now.Add(-3 * time.Hour),
			},
			{
				ID:          task3,
				Description: "Implement feature XYZ",
				Status:      models.TaskStatusReady,
				Priority:    3,
				SpecRef:     "specs/features.md",
				DoneWhen:    "Feature is implemented",
				Scope:       "Feature module",
				Created:     now.Add(-4 * time.Hour),
			},
			{
				ID:               task4,
				Description:      "Restore missing architecture task",
				Status:           models.TaskStatusBlocked,
				Priority:         1,
				BlockedReason:    &blockedReason,
				BlockedQuestions: []string{"Can the orchestrator restore the missing architecture task?"},
				RepairRequest: &models.RepairRequest{
					Operation:  "add-task",
					Target:     "architecture-2",
					Command:    "liza add-task --id architecture-2 --agent-id orchestrator-1 --json",
					Evidence:   []string{"command requires role type [orchestrator]"},
					Validation: []string{"go test ./cmd/liza -run TestWorkflowContract"},
				},
				SpecRef:  "specs/architecture.md",
				DoneWhen: "Architecture task exists in state",
				Scope:    "Blackboard task state",
				Created:  now.Add(-5 * time.Hour),
			},
			{
				ID:          reviewerTaskA,
				Description: "Completed reviewer task",
				Status:      models.TaskStatusMerged,
				Priority:    4,
				SpecRef:     "specs/reviewer-sequence.md",
				DoneWhen:    "Review is complete",
				Scope:       "Reviewer sequence task A",
				Created:     now.Add(-6 * time.Hour),
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-5 * time.Hour), Event: models.TaskEventClaimed, Agent: &sequentialReviewer},
				},
			},
			{
				ID:          reviewerTaskB,
				Description: "Current reviewer task",
				Status:      models.TaskStatusReviewing,
				Priority:    4,
				ReviewingBy: &sequentialReviewer,
				SpecRef:     "specs/reviewer-sequence.md",
				DoneWhen:    "Review is complete",
				Scope:       "Reviewer sequence task B",
				Created:     now.Add(-3 * time.Hour),
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-2 * time.Hour), Event: models.TaskEventClaimed, Agent: &previousDoer},
					{Time: now.Add(-17 * time.Minute), Event: models.TaskEventClaimed, Agent: &sequentialReviewer},
				},
			},
			{
				ID:          doerTaskA,
				Description: "Completed doer task",
				Status:      models.TaskStatusMerged,
				Priority:    4,
				AssignedTo:  &sequentialDoer,
				SpecRef:     "specs/doer-sequence.md",
				DoneWhen:    "Implementation is complete",
				Scope:       "Doer sequence task A",
				Created:     now.Add(-5 * time.Hour),
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-4 * time.Hour), Event: models.TaskEventClaimed, Agent: &sequentialDoer},
				},
			},
			{
				ID:          doerTaskB,
				Description: "Current doer task",
				Status:      models.TaskStatusIntegrationFailed,
				Priority:    4,
				AssignedTo:  &sequentialDoer,
				SpecRef:     "specs/doer-sequence.md",
				DoneWhen:    "Implementation is complete",
				Scope:       "Doer sequence task B",
				Created:     now.Add(-2 * time.Hour),
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-70 * time.Minute), Event: models.TaskEventClaimed, Agent: &sequentialDoer},
					{Time: now.Add(-11 * time.Minute), Event: models.TaskEventClaimedForIntegrationFix, Agent: &sequentialDoer},
				},
			},
		},
		Agents: map[string]models.Agent{
			coder1: {
				Role:            "coder",
				Status:          models.AgentStatusWorking,
				CurrentTask:     &task1,
				Heartbeat:       now,
				Terminal:        "terminal1",
				IterationsTotal: 5,
				ContextPercent:  45,
			},
			reviewer1: {
				Role:            "code-reviewer",
				Status:          models.AgentStatusIdle,
				Heartbeat:       now,
				Terminal:        "terminal2",
				IterationsTotal: 2,
				ContextPercent:  20,
			},
			orchestrator1: {
				Role:      "orchestrator",
				Status:    models.AgentStatusIdle,
				Heartbeat: now,
				Terminal:  "terminal3",
			},
			sequentialReviewer: {
				Role:        "code-reviewer",
				Status:      models.AgentStatusReviewing,
				CurrentTask: &reviewerTaskB,
				Heartbeat:   now,
				Terminal:    "terminal4",
			},
			sequentialDoer: {
				Role:        "coder",
				Status:      models.AgentStatusWorking,
				CurrentTask: &doerTaskB,
				Heartbeat:   now,
				Terminal:    "terminal5",
			},
		},
		Anomalies: []models.Anomaly{
			{
				Timestamp: now.Add(-1 * time.Hour),
				Task:      task1,
				Reporter:  coder1,
				Type:      "retry_loop",
				Details:   map[string]any{},
			},
		},
		CircuitBreaker: models.CircuitBreaker{
			Status:    "OK",
			LastCheck: now,
		},
		Config: models.Config{
			MaxCoderIterations: 10,
			MaxReviewCycles:    5,
			HeartbeatInterval:  60,
			LeaseDuration:      300,
			CoderPollInterval:  10,
			DoerMaxWait:        60,
			IntegrationBranch:  "main",
			Mode:               models.SystemModeRunning,
		},
	}

	// Write state to file
	statePath := filepath.Join(lizaDir, paths.StateFileName)
	stateData, err := yaml.Marshal(state)
	if err != nil {
		t.Fatalf("failed to marshal state: %v", err)
	}
	if err := os.WriteFile(statePath, stateData, 0644); err != nil {
		t.Fatalf("failed to write state file: %v", err)
	}

	// Change to the temp directory
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current directory: %v", err)
	}
	defer os.Chdir(oldDir)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to temp directory: %v", err)
	}

	tests := []struct {
		name           string
		args           []string
		wantContains   []string
		wantNotContain []string
		wantErr        bool
	}{
		{
			name:         "get config.mode",
			args:         []string{"get", "config.mode"},
			wantContains: []string{"RUNNING"},
		},
		{
			name:         "get sprint.status",
			args:         []string{"get", "sprint.status"},
			wantContains: []string{"IN_PROGRESS"},
		},
		{
			name:         "get sprint.metrics.tasks_done",
			args:         []string{"get", "sprint.metrics.tasks_done"},
			wantContains: []string{"2"},
		},
		{
			name:         "get tasks - table format",
			args:         []string{"get", "tasks", "--format", "table"},
			wantContains: []string{"task-1", "IMPLEMENTING_CODE", "Test task"},
		},
		{
			name:         "get specific task",
			args:         []string{"get", "tasks", "task-1", "--format", "value"},
			wantContains: []string{"ID: task-1", "Status: IMPLEMENTING_CODE", "Description: Test task"},
		},
		{
			name:         "get agents - table format",
			args:         []string{"get", "agents", "--format", "table"},
			wantContains: []string{"coder-1", "WORKING"},
		},
		{
			name:         "get specific agent",
			args:         []string{"get", "agents", "coder-1", "--format", "value"},
			wantContains: []string{"ID: coder-1", "Role: coder", "Status: WORKING"},
		},
		{
			name:           "reviewer sequential assignment - value",
			args:           []string{"get", "agents", sequentialReviewer, "--format", "value"},
			wantContains:   []string{"Current Task: " + reviewerTaskB, "Time on Task: 17m"},
			wantNotContain: []string{"5h 0m", "2h 0m"},
		},
		{
			name:           "reviewer sequential assignment - table",
			args:           []string{"get", "agents", sequentialReviewer, "--format", "table"},
			wantContains:   []string{sequentialReviewer, reviewerTaskB, "17m"},
			wantNotContain: []string{"5h 0m", "2h 0m"},
		},
		{
			name:           "reviewer sequential assignment - JSON",
			args:           []string{"get", "agents", sequentialReviewer, "--format", "json"},
			wantContains:   []string{`"current_task": "` + reviewerTaskB + `"`, `"time_on_task": "17m"`},
			wantNotContain: []string{"5h 0m", "2h 0m"},
		},
		{
			name:           "reviewer sequential assignment - direct field",
			args:           []string{"get", "agent." + sequentialReviewer + ".time_on_task"},
			wantContains:   []string{"17m"},
			wantNotContain: []string{"5h 0m", "2h 0m"},
		},
		{
			name:           "doer sequential assignment - value",
			args:           []string{"get", "agents", sequentialDoer, "--format", "value"},
			wantContains:   []string{"Current Task: " + doerTaskB, "Time on Task: 11m"},
			wantNotContain: []string{"4h 0m", "1h 10m"},
		},
		{
			name:           "doer sequential assignment - table",
			args:           []string{"get", "agents", sequentialDoer, "--format", "table"},
			wantContains:   []string{sequentialDoer, doerTaskB, "11m"},
			wantNotContain: []string{"4h 0m", "1h 10m"},
		},
		{
			name:           "doer sequential assignment - JSON",
			args:           []string{"get", "agents", sequentialDoer, "--format", "json"},
			wantContains:   []string{`"current_task": "` + doerTaskB + `"`, `"time_on_task": "11m"`},
			wantNotContain: []string{"4h 0m", "1h 10m"},
		},
		{
			name:           "doer sequential assignment - direct field",
			args:           []string{"get", "agent." + sequentialDoer + ".time_on_task"},
			wantContains:   []string{"11m"},
			wantNotContain: []string{"4h 0m", "1h 10m"},
		},
		{
			name:         "get metrics",
			args:         []string{"get", "metrics", "--format", "value"},
			wantContains: []string{"Tasks Done: 2", "Tasks In Progress: 1"},
		},
		{
			name:         "get anomalies",
			args:         []string{"get", "anomalies", "--format", "table"},
			wantContains: []string{"retry_loop", "task-1", "coder-1"},
		},
		{
			name:         "get tasks - JSON format",
			args:         []string{"get", "tasks", "--format", "json"},
			wantContains: []string{`"id": "task-1"`, `"status": "IMPLEMENTING_CODE"`},
		},
		{
			name:           "get active task summary - JSON format",
			args:           []string{"get", "tasks", "--active", "--summary", "--format", "json"},
			wantContains:   []string{`"id": "task-1"`, `"status": "IMPLEMENTING_CODE"`, `"attempt": 1`},
			wantNotContain: []string{`"done_when"`, `"scope"`, `"output"`},
		},
		{
			name:         "get task by ID shorthand",
			args:         []string{"get", "task-1", "--format", "value"},
			wantContains: []string{"ID: task-1", "Status: IMPLEMENTING", "Description: Test task"},
		},
		{
			name:         "get task by ID shorthand - JSON",
			args:         []string{"get", "task-1", "--format", "json"},
			wantContains: []string{`"id": "task-1"`, `"status": "IMPLEMENTING_CODE"`},
		},
		{
			name: "get blocked task by ID shorthand - JSON includes repair request",
			args: []string{"get", "task-repair-request", "--format", "json"},
			wantContains: []string{
				`"id": "task-repair-request"`,
				`"blocked_questions": [`,
				`"repair_request": {`,
				`"operation": "add-task"`,
				`"target": "architecture-2"`,
				`"command": "liza add-task --id architecture-2 --agent-id orchestrator-1 --json"`,
				`"evidence": [`,
				`"validation": [`,
			},
		},
		{
			name:         "get agent by ID shorthand",
			args:         []string{"get", "coder-1", "--format", "value"},
			wantContains: []string{"ID: coder-1", "Role: coder", "Status: WORKING"},
		},
		{
			name:         "get agent by ID shorthand - JSON",
			args:         []string{"get", "coder-1", "--format", "json"},
			wantContains: []string{`"role": "coder"`, `"status": "WORKING"`},
		},
		{
			name:         "get code-reviewer by ID shorthand",
			args:         []string{"get", "code-reviewer-1", "--format", "value"},
			wantContains: []string{"ID: code-reviewer-1", "Role: code-reviewer", "Status: IDLE"},
		},
		{
			name:         "get orchestrator by ID shorthand",
			args:         []string{"get", "orchestrator-1", "--format", "value"},
			wantContains: []string{"ID: orchestrator-1", "Role: orchestrator", "Status: IDLE"},
		},
		{
			name:         "get task with non-standard ID",
			args:         []string{"get", "fix-auth-bug", "--format", "value"},
			wantContains: []string{"ID: fix-auth-bug", "Status: DRAFT_CODE", "Description: Fix authentication bug"},
		},
		{
			name:         "get task with alphanumeric ID",
			args:         []string{"get", "feature-xyz-123", "--format", "value"},
			wantContains: []string{"ID: feature-xyz-123", "Status: DRAFT_CODE", "Description: Implement feature XYZ"},
		},
		{
			name:         "get nonexistent task by ID shorthand",
			args:         []string{"get", "task-999"},
			wantErr:      true,
			wantContains: []string{"not found"},
		},
		{
			name:         "get nonexistent agent by ID shorthand",
			args:         []string{"get", "coder-999"},
			wantErr:      true,
			wantContains: []string{"not found"},
		},
		{
			name:         "get nonexistent field",
			args:         []string{"get", "config.nonexistent"},
			wantErr:      true,
			wantContains: []string{"not found"},
		},
		{
			name:    "no args",
			args:    []string{"get"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// rootCmd is a package-level singleton; reset command/flag state so
			// repeated runs (e.g. -count=2) do not leak prior executions.
			resetRootCmdForTest(t)
			rootCmd.SetArgs(tt.args)

			// Capture output
			var outBuf bytes.Buffer
			rootCmd.SetOut(&outBuf)
			rootCmd.SetErr(&outBuf)

			// Execute command
			err := rootCmd.Execute()

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			output := outBuf.String()

			// Check expected content
			for _, want := range tt.wantContains {
				if !strings.Contains(output, want) {
					t.Errorf("expected output to contain %q\nGot:\n%s", want, output)
				}
			}

			// Check unexpected content
			for _, notWant := range tt.wantNotContain {
				if strings.Contains(output, notWant) {
					t.Errorf("expected output to NOT contain %q\nGot:\n%s", notWant, output)
				}
			}
		})
	}
}

func TestGetCommandHelp(t *testing.T) {
	resetRootCmdForTest(t)

	// Test that help output is generated correctly
	rootCmd.SetArgs([]string{"get", "--help"})

	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)
	rootCmd.SetErr(&outBuf)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := outBuf.String()

	expectedSections := []string{
		"Query Types:",
		"Field queries:",
		"Entity queries:",
		"Formats:",
		"Examples:",
		"config.mode",
		"tasks",
		"agents",
		"metrics",
		"anomalies",
	}

	for _, section := range expectedSections {
		if !strings.Contains(output, section) {
			t.Errorf("expected help output to contain %q", section)
		}
	}
}
