package ops

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestSetTaskOutput_Validation(t *testing.T) {
	tests := []struct {
		name        string
		input       SetTaskOutputInput
		errContains string
	}{
		{
			name:        "empty task ID",
			input:       SetTaskOutputInput{AgentID: "coder-1", Output: []models.OutputEntry{{Desc: "d", DoneWhen: "dw", Scope: "s"}}},
			errContains: "task_id is required",
		},
		{
			name:        "empty agent ID",
			input:       SetTaskOutputInput{TaskID: "t1", Output: []models.OutputEntry{{Desc: "d", DoneWhen: "dw", Scope: "s"}}},
			errContains: "agent_id is required",
		},
		{
			name:        "output entry missing desc",
			input:       SetTaskOutputInput{TaskID: "t1", AgentID: "coder-1", Output: []models.OutputEntry{{DoneWhen: "dw", Scope: "s"}}},
			errContains: "output[0].desc is required",
		},
		{
			name:        "output entry missing done_when",
			input:       SetTaskOutputInput{TaskID: "t1", AgentID: "coder-1", Output: []models.OutputEntry{{Desc: "d", Scope: "s"}}},
			errContains: "output[0].done_when is required",
		},
		{
			name:        "output entry missing scope",
			input:       SetTaskOutputInput{TaskID: "t1", AgentID: "coder-1", Output: []models.OutputEntry{{Desc: "d", DoneWhen: "dw"}}},
			errContains: "output[0].scope is required",
		},
		{
			name: "output entry semicolon-joined spec_ref",
			input: SetTaskOutputInput{
				TaskID:  "t1",
				AgentID: "coder-1",
				Output: []models.OutputEntry{{
					Desc:     "d",
					DoneWhen: "dw",
					Scope:    "s",
					SpecRef:  "specs/a.md; specs/b.md#section",
				}},
			},
			errContains: "multiple refs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SetTaskOutput("/nonexistent", &tt.input)
			testhelpers.RequireErrorContains(t, err, tt.errContains)
		})
	}
}

func TestSetTaskOutput_TaskNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)

	err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
		TaskID:  "nonexistent",
		AgentID: "coder-1",
		Output:  []models.OutputEntry{{Desc: "d", DoneWhen: "dw", Scope: "s"}},
	})
	testhelpers.RequireErrorContains(t, err, "task nonexistent not found")
}

func TestSetTaskOutput_WrongStatus(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
		TaskID:  "task-1",
		AgentID: "coder-1",
		Output:  []models.OutputEntry{{Desc: "d", DoneWhen: "dw", Scope: "s"}},
	})
	testhelpers.RequireErrorContains(t, err, "not in an executing state")
}

func TestSetTaskOutput_WrongAgent(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
		TaskID:  "task-1",
		AgentID: "coder-99",
		Output:  []models.OutputEntry{{Desc: "d", DoneWhen: "dw", Scope: "s"}},
	})
	testhelpers.RequireErrorContains(t, err, "not assigned to agent coder-99")
}

func TestSetTaskOutput_HappyPath(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	output := []models.OutputEntry{
		{Desc: "implement feature X", DoneWhen: "tests pass", Scope: "pkg/x", SpecRef: "specs/x.md"},
		{Desc: "implement feature Y", DoneWhen: "linter green", Scope: "pkg/y", SpecRef: "specs/y.md"},
	}

	err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
		TaskID:  "task-1",
		AgentID: "coder-1",
		Output:  output,
	})
	if err != nil {
		t.Fatalf("SetTaskOutput() unexpected error: %v", err)
	}

	// Verify output was persisted
	bb := db.For(stateFile)
	stateAfter, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := stateAfter.FindTask("task-1")
	if task == nil {
		t.Fatal("task-1 not found after SetTaskOutput")
	}
	if len(task.Output) != 2 {
		t.Fatalf("Expected 2 output entries, got %d", len(task.Output))
	}
	if task.Output[0].Desc != "implement feature X" {
		t.Errorf("Output[0].Desc = %q, want %q", task.Output[0].Desc, "implement feature X")
	}
	if task.Output[1].SpecRef != "specs/y.md" {
		t.Errorf("Output[1].SpecRef = %q, want %q", task.Output[1].SpecRef, "specs/y.md")
	}
}

func TestSetTaskOutput_EmptyOutput(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
		TaskID:  "task-1",
		AgentID: "coder-1",
		Output:  []models.OutputEntry{},
	})
	if err != nil {
		t.Fatalf("SetTaskOutput() with empty output: unexpected error: %v", err)
	}

	bb := db.For(stateFile)
	stateAfter, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := stateAfter.FindTask("task-1")
	if task == nil {
		t.Fatal("task-1 not found after SetTaskOutput")
	}
	if len(task.Output) != 0 {
		t.Fatalf("Expected 0 output entries, got %d", len(task.Output))
	}
}

func TestSetTaskOutput_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	first := []models.OutputEntry{
		{Desc: "old task", DoneWhen: "old", Scope: "old"},
	}
	second := []models.OutputEntry{
		{Desc: "new task A", DoneWhen: "new A", Scope: "scope A"},
		{Desc: "new task B", DoneWhen: "new B", Scope: "scope B"},
	}

	// First call
	if err := SetTaskOutput(tmpDir, &SetTaskOutputInput{TaskID: "task-1", AgentID: "coder-1", Output: first}); err != nil {
		t.Fatalf("First SetTaskOutput() error: %v", err)
	}

	// Second call overwrites
	if err := SetTaskOutput(tmpDir, &SetTaskOutputInput{TaskID: "task-1", AgentID: "coder-1", Output: second}); err != nil {
		t.Fatalf("Second SetTaskOutput() error: %v", err)
	}

	bb := db.For(stateFile)
	stateAfter, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := stateAfter.FindTask("task-1")
	if len(task.Output) != 2 {
		t.Fatalf("Expected 2 output entries (overwritten), got %d", len(task.Output))
	}
	if task.Output[0].Desc != "new task A" {
		t.Errorf("Output[0].Desc = %q, want %q", task.Output[0].Desc, "new task A")
	}
}

func TestSetTaskOutput_NormalizesWorktreeSpecRef(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	output := []models.OutputEntry{
		{Desc: "feature A", DoneWhen: "tests pass", Scope: "pkg/a", SpecRef: ".worktrees/task-1/specs/plan.md"},
		{Desc: "feature B", DoneWhen: "linter green", Scope: "pkg/b", SpecRef: "/home/user/project/.worktrees/task-1/specs/deep/b.md"},
		{Desc: "feature C", DoneWhen: "done", Scope: "pkg/c", SpecRef: "specs/already-clean.md"},
	}

	err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
		TaskID:  "task-1",
		AgentID: "coder-1",
		Output:  output,
	})
	if err != nil {
		t.Fatalf("SetTaskOutput() unexpected error: %v", err)
	}

	bb := db.For(stateFile)
	stateAfter, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := stateAfter.FindTask("task-1")
	if task == nil {
		t.Fatal("task-1 not found after SetTaskOutput")
	}

	wantRefs := []string{"specs/plan.md", "specs/deep/b.md", "specs/already-clean.md"}
	for i, want := range wantRefs {
		if task.Output[i].SpecRef != want {
			t.Errorf("Output[%d].SpecRef = %q, want %q", i, task.Output[i].SpecRef, want)
		}
	}
}

func TestSetTaskOutput_NormalizesWorktreePlanRef(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	output := []models.OutputEntry{
		{Desc: "feature A", DoneWhen: "tests pass", Scope: "pkg/a", SpecRef: "specs/a.md", PlanRef: ".worktrees/task-1/specs/plans/plan.md"},
		{Desc: "feature B", DoneWhen: "linter green", Scope: "pkg/b", SpecRef: "specs/b.md", PlanRef: "specs/plans/already-clean.md"},
		{Desc: "feature C", DoneWhen: "done", Scope: "pkg/c", SpecRef: "specs/c.md"},
	}

	err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
		TaskID:  "task-1",
		AgentID: "coder-1",
		Output:  output,
	})
	if err != nil {
		t.Fatalf("SetTaskOutput() unexpected error: %v", err)
	}

	bb := db.For(stateFile)
	stateAfter, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := stateAfter.FindTask("task-1")
	if task == nil {
		t.Fatal("task-1 not found after SetTaskOutput")
	}

	wantPlanRefs := []string{"specs/plans/plan.md", "specs/plans/already-clean.md", ""}
	for i, want := range wantPlanRefs {
		if task.Output[i].PlanRef != want {
			t.Errorf("Output[%d].PlanRef = %q, want %q", i, task.Output[i].PlanRef, want)
		}
	}
}

func TestSetTaskOutput_NormalizesWorktreeArchRef(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	output := []models.OutputEntry{
		{Desc: "feature A", DoneWhen: "tests pass", Scope: "pkg/a", SpecRef: "specs/a.md", ArchRef: ".worktrees/task-1/specs/arch-plan/feature.md"},
		{Desc: "feature B", DoneWhen: "linter green", Scope: "pkg/b", SpecRef: "specs/b.md", ArchRef: "specs/arch-plan/already-clean.md"},
		{Desc: "feature C", DoneWhen: "done", Scope: "pkg/c", SpecRef: "specs/c.md"},
	}

	err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
		TaskID:  "task-1",
		AgentID: "coder-1",
		Output:  output,
	})
	if err != nil {
		t.Fatalf("SetTaskOutput() unexpected error: %v", err)
	}

	bb := db.For(stateFile)
	stateAfter, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := stateAfter.FindTask("task-1")
	if task == nil {
		t.Fatal("task-1 not found after SetTaskOutput")
	}

	wantArchRefs := []string{"specs/arch-plan/feature.md", "specs/arch-plan/already-clean.md", ""}
	for i, want := range wantArchRefs {
		if task.Output[i].ArchRef != want {
			t.Errorf("Output[%d].ArchRef = %q, want %q", i, task.Output[i].ArchRef, want)
		}
	}
}

func TestSetTaskOutput_NormalizesWorktreeEpicRef(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	output := []models.OutputEntry{
		{Desc: "cap A", DoneWhen: "stories complete", Scope: "CAP-001", SpecRef: "specs/a.md", EpicRef: ".worktrees/task-1/specs/epics/ep-001.md#capability-cap-001"},
		{Desc: "cap B", DoneWhen: "stories complete", Scope: "CAP-002", SpecRef: "specs/b.md", EpicRef: "specs/epics/ep-001.md#capability-cap-002"},
		{Desc: "cap C", DoneWhen: "done", Scope: "CAP-003", SpecRef: "specs/c.md"},
	}

	err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
		TaskID:  "task-1",
		AgentID: "coder-1",
		Output:  output,
	})
	if err != nil {
		t.Fatalf("SetTaskOutput() unexpected error: %v", err)
	}

	bb := db.For(stateFile)
	stateAfter, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := stateAfter.FindTask("task-1")
	if task == nil {
		t.Fatal("task-1 not found after SetTaskOutput")
	}

	wantEpicRefs := []string{"specs/epics/ep-001.md#capability-cap-001", "specs/epics/ep-001.md#capability-cap-002", ""}
	for i, want := range wantEpicRefs {
		if task.Output[i].EpicRef != want {
			t.Errorf("Output[%d].EpicRef = %q, want %q", i, task.Output[i].EpicRef, want)
		}
	}
}

func TestSetTaskOutput_DependsOnRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
		testhelpers.BuildTaskByStatus("external-task", models.TaskStatusMerged, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	output := []models.OutputEntry{
		{Desc: "Setup", DoneWhen: "ready", Scope: "db", SpecRef: "specs/db.md"},
		{Desc: "Build", DoneWhen: "works", Scope: "api", SpecRef: "specs/api.md", DependsOn: []string{"0"}},
		{Desc: "Test", DoneWhen: "green", Scope: "test", SpecRef: "specs/test.md", DependsOn: []string{"0", "1"}, TaskDependsOn: []string{" external-task "}},
	}

	err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
		TaskID:  "task-1",
		AgentID: "coder-1",
		Output:  output,
	})
	if err != nil {
		t.Fatalf("SetTaskOutput() unexpected error: %v", err)
	}

	bb := db.For(stateFile)
	stateAfter, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := stateAfter.FindTask("task-1")
	if task == nil {
		t.Fatal("task-1 not found after SetTaskOutput")
	}
	if len(task.Output) != 3 {
		t.Fatalf("Expected 3 output entries, got %d", len(task.Output))
	}
	if len(task.Output[0].DependsOn) != 0 {
		t.Errorf("Output[0].DependsOn = %v, want empty", task.Output[0].DependsOn)
	}
	if len(task.Output[1].DependsOn) != 1 || task.Output[1].DependsOn[0] != "0" {
		t.Errorf("Output[1].DependsOn = %v, want [\"0\"]", task.Output[1].DependsOn)
	}
	if len(task.Output[2].DependsOn) != 2 || task.Output[2].DependsOn[0] != "0" || task.Output[2].DependsOn[1] != "1" {
		t.Errorf("Output[2].DependsOn = %v, want [\"0\", \"1\"]", task.Output[2].DependsOn)
	}
	if len(task.Output[2].TaskDependsOn) != 1 || task.Output[2].TaskDependsOn[0] != "external-task" {
		t.Errorf("Output[2].TaskDependsOn = %v, want [\"external-task\"]", task.Output[2].TaskDependsOn)
	}
}

func TestSetTaskOutput_DependsOnValidation(t *testing.T) {
	tests := []struct {
		name        string
		output      []models.OutputEntry
		errContains string
	}{
		{
			name: "non-numeric reference",
			output: []models.OutputEntry{
				{Desc: "d", DoneWhen: "dw", Scope: "s", DependsOn: []string{"abc"}},
			},
			errContains: "non-numeric",
		},
		{
			name: "out of range",
			output: []models.OutputEntry{
				{Desc: "d", DoneWhen: "dw", Scope: "s", DependsOn: []string{"5"}},
			},
			errContains: "out of range",
		},
		{
			name: "self reference",
			output: []models.OutputEntry{
				{Desc: "d", DoneWhen: "dw", Scope: "s", DependsOn: []string{"0"}},
			},
			errContains: "references itself",
		},
		{
			name: "negative index",
			output: []models.OutputEntry{
				{Desc: "d", DoneWhen: "dw", Scope: "s", DependsOn: []string{"-1"}},
			},
			errContains: "out of range",
		},
		{
			name: "unknown kind",
			output: []models.OutputEntry{
				{Desc: "d", DoneWhen: "dw", Scope: "s", Kind: "bootstrap-pre-commit"},
			},
			errContains: `unknown kind "bootstrap-pre-commit"`,
		},
		{
			name: "invalid task_depends_on ID",
			output: []models.OutputEntry{
				{Desc: "d", DoneWhen: "dw", Scope: "s", TaskDependsOn: []string{"../bad"}},
			},
			errContains: "task_depends_on contains invalid task ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SetTaskOutput("/nonexistent", &SetTaskOutputInput{
				TaskID:  "t1",
				AgentID: "coder-1",
				Output:  tt.output,
			})
			testhelpers.RequireErrorContains(t, err, tt.errContains)
		})
	}
}

func TestSetTaskOutput_TaskDependsOnRejectsMissingTask(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
		TaskID:  "task-1",
		AgentID: "coder-1",
		Output: []models.OutputEntry{{
			Desc:          "Validate existing work",
			DoneWhen:      "evidence recorded",
			Scope:         "specs/evidence",
			SpecRef:       "specs/evidence.md",
			TaskDependsOn: []string{"missing-task"},
		}},
	})
	testhelpers.RequireErrorContains(t, err, `output[0].task_depends_on references non-existent task "missing-task"`)
}

func TestSetTaskOutput_TaskDependsOnRejectsTerminalNonMergedTask(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	architect := "architect-1"
	architectureTask := testhelpers.BuildTaskByStatus("architecture-1", models.TaskStatus("ARCHITECTING"), now)
	architectureTask.RolePair = "architecture-pair"
	architectureTask.AssignedTo = &architect
	staleDep := testhelpers.BuildTaskByStatus("old-plan-dep", models.TaskStatusSuperseded, now)
	staleDep.RolePair = "code-planning-pair"
	staleDep.SupersededBy = []string{"new-plan-dep"}
	replacement := testhelpers.BuildTaskByStatus("new-plan-dep", models.TaskStatusMerged, now)
	replacement.RolePair = "code-planning-pair"

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{architectureTask, staleDep, replacement}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
		TaskID:  "architecture-1",
		AgentID: "architect-1",
		Output: []models.OutputEntry{{
			Desc:          "Plan work",
			DoneWhen:      "plan exists",
			Scope:         "specs/plan",
			SpecRef:       "specs/plan.md",
			TaskDependsOn: []string{"old-plan-dep"},
		}},
	})
	testhelpers.RequireErrorContains(t, err, `output[0].task_depends_on references terminal non-MERGED task "old-plan-dep"`)
}

func TestSetTaskOutput_TaskDependsOnRejectsDownstreamRolePair(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	architect := "architect-1"
	architectureTask := testhelpers.BuildTaskByStatus("architecture-1", models.TaskStatus("ARCHITECTING"), now)
	architectureTask.RolePair = "architecture-pair"
	architectureTask.AssignedTo = &architect
	codingTask := testhelpers.BuildTaskByStatus("coding-1", models.TaskStatusMerged, now)
	codingTask.RolePair = "coding-pair"

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{architectureTask, codingTask}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
		TaskID:  "architecture-1",
		AgentID: "architect-1",
		Output: []models.OutputEntry{{
			Desc:          "Plan work",
			DoneWhen:      "plan exists",
			Scope:         "specs/plan",
			SpecRef:       "specs/plan.md",
			TaskDependsOn: []string{"coding-1"},
		}},
	})
	testhelpers.RequireErrorContains(t, err, "role_pair coding-pair is downstream of code-planning-pair")
}

func TestSetTaskOutput_TaskDependsOnAllowsTargetRolePairPeer(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	planner := "code-planner-1"
	planningTask := testhelpers.BuildTaskByStatus("plan-1", models.TaskStatusCodePlanning, now)
	planningTask.AssignedTo = &planner
	codingTask := testhelpers.BuildTaskByStatus("coding-1", models.TaskStatusMerged, now)
	codingTask.RolePair = "coding-pair"

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{planningTask, codingTask}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
		TaskID:  "plan-1",
		AgentID: "code-planner-1",
		Output: []models.OutputEntry{{
			Desc:          "Continue coding work",
			DoneWhen:      "work is linked",
			Scope:         "internal/ops",
			SpecRef:       "specs/work.md",
			TaskDependsOn: []string{"coding-1"},
		}},
	})
	if err != nil {
		t.Fatalf("SetTaskOutput() unexpected error: %v", err)
	}
}

func TestSetTaskOutput_CodePlanningStatus(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusCodePlanning, now)
	agent := "code-planner-1"
	task.AssignedTo = &agent
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
		TaskID:  "task-1",
		AgentID: "code-planner-1",
		Output:  []models.OutputEntry{{Desc: "d", DoneWhen: "dw", Scope: "s"}},
	})
	if err != nil {
		t.Fatalf("SetTaskOutput() for CODE_PLANNING task: unexpected error: %v", err)
	}
}

func TestSetTaskOutput_DecompositionRootRequiresRoleArtifactRef(t *testing.T) {
	tests := []struct {
		name        string
		rolePair    string
		status      models.TaskStatus
		output      []models.OutputEntry
		errContains string
	}{
		{
			name:        "epic planning root requires plan_ref",
			rolePair:    "epic-planning-main-pair",
			status:      models.TaskStatus("EPIC_PLANNING_MAIN"),
			output:      validDecompositionRootOutput(""),
			errContains: "output[0].plan_ref is required",
		},
		{
			name:        "architecture root requires arch_ref",
			rolePair:    "architecture-main-pair",
			status:      models.TaskStatus("ARCHITECTING_MAIN"),
			output:      validArchitectureRootOutput(""),
			errContains: "output[0].arch_ref is required",
		},
		{
			name:        "code planning root requires plan_ref",
			rolePair:    "code-planning-main-pair",
			status:      models.TaskStatus("CODE_PLANNING_MAIN"),
			output:      validDecompositionRootOutput(""),
			errContains: "output[0].plan_ref is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := setupSetTaskOutputRootTask(t, tt.rolePair, tt.status)
			err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
				TaskID:  "root-task",
				AgentID: "master-agent",
				Output:  tt.output,
			})
			testhelpers.RequireErrorContains(t, err, tt.errContains)
		})
	}
}

func TestSetTaskOutput_DecompositionRootValidation(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func([]models.OutputEntry) []models.OutputEntry
		errContains string
	}{
		{
			name: "missing decomposition",
			mutate: func(output []models.OutputEntry) []models.OutputEntry {
				output[0].Decomposition = nil
				return output
			},
			errContains: "output[0].decomposition is required",
		},
		{
			name: "duplicate owned files across siblings",
			mutate: func(output []models.OutputEntry) []models.OutputEntry {
				output[1].Decomposition.OwnedFiles = []string{" internal/a.go "}
				return output
			},
			errContains: "owned_files duplicates",
		},
		{
			name: "duplicate owned interfaces across siblings",
			mutate: func(output []models.OutputEntry) []models.OutputEntry {
				output[1].Decomposition.InterfacesOwned = []string{"PlanContract"}
				return output
			},
			errContains: "interfaces_owned duplicates",
		},
		{
			name: "empty ownership declaration",
			mutate: func(output []models.OutputEntry) []models.OutputEntry {
				output[0].Decomposition.OwnedFiles = nil
				output[0].Decomposition.OwnedModules = nil
				output[0].Decomposition.InterfacesOwned = nil
				return output
			},
			errContains: "must declare ownership",
		},
		{
			name: "catch-all ownership declaration",
			mutate: func(output []models.OutputEntry) []models.OutputEntry {
				output[0].Decomposition.OwnedModules = []string{"everything else"}
				return output
			},
			errContains: "catch-all ownership",
		},
		{
			name: "read-only sibling dependency out of range",
			mutate: func(output []models.OutputEntry) []models.OutputEntry {
				output[0].Decomposition.ReadOnlyDependsOn = []int{2}
				return output
			},
			errContains: "read_only_depends_on reference 2 out of range",
		},
		{
			name: "read-only sibling dependency self reference",
			mutate: func(output []models.OutputEntry) []models.OutputEntry {
				output[1].Decomposition.ReadOnlyDependsOn = []int{1}
				return output
			},
			errContains: "read_only_depends_on references itself",
		},
		{
			name: "read-only sibling dependency not mirrored",
			mutate: func(output []models.OutputEntry) []models.OutputEntry {
				output[1].DependsOn = nil
				return output
			},
			errContains: `read_only_depends_on reference 0 must also appear in depends_on`,
		},
		{
			name: "invalid read-only task dependency ID",
			mutate: func(output []models.OutputEntry) []models.OutputEntry {
				output[0].Decomposition.ReadOnlyTaskDependsOn = []string{"../bad"}
				return output
			},
			errContains: "read_only_task_depends_on contains invalid task ID",
		},
		{
			name: "empty read-only task dependency ID",
			mutate: func(output []models.OutputEntry) []models.OutputEntry {
				output[0].Decomposition.ReadOnlyTaskDependsOn = []string{" "}
				return output
			},
			errContains: "read_only_task_depends_on contains invalid task ID",
		},
		{
			name: "missing read-only task dependency target",
			mutate: func(output []models.OutputEntry) []models.OutputEntry {
				output[0].Decomposition.ReadOnlyTaskDependsOn = []string{"missing-task"}
				output[0].TaskDependsOn = []string{"missing-task"}
				return output
			},
			errContains: `read_only_task_depends_on references non-existent task "missing-task"`,
		},
		{
			name: "read-only task dependency not mirrored",
			mutate: func(output []models.OutputEntry) []models.OutputEntry {
				output[0].Decomposition.ReadOnlyTaskDependsOn = []string{"existing-plan"}
				output[0].TaskDependsOn = nil
				return output
			},
			errContains: `read_only_task_depends_on reference "existing-plan" must also appear in task_depends_on`,
		},
		{
			name: "sibling dependency cycle",
			mutate: func(output []models.OutputEntry) []models.OutputEntry {
				output[0].DependsOn = []string{"1"}
				return output
			},
			errContains: "depends_on cycle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := setupSetTaskOutputRootTask(t, "code-planning-main-pair", models.TaskStatus("CODE_PLANNING_MAIN"))
			output := tt.mutate(validDecompositionRootOutput("specs/plans/master.md"))
			err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
				TaskID:  "root-task",
				AgentID: "master-agent",
				Output:  output,
			})
			testhelpers.RequireErrorContains(t, err, tt.errContains)
		})
	}
}

func TestSetTaskOutput_DecompositionRootAcceptsValidOutput(t *testing.T) {
	tmpDir := setupSetTaskOutputRootTask(t, "code-planning-main-pair", models.TaskStatus("CODE_PLANNING_MAIN"))

	err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
		TaskID:  "root-task",
		AgentID: "master-agent",
		Output:  validDecompositionRootOutput("specs/plans/master.md"),
	})
	if err != nil {
		t.Fatalf("SetTaskOutput() unexpected error: %v", err)
	}
}

func TestValidateDecompositionRootOutput_UsesConfiguredOutputRef(t *testing.T) {
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("existing-plan", models.TaskStatusMerged, time.Now().UTC()),
	}
	output := validDecompositionRootOutput("specs/plans/master.md")

	err := validateDecompositionRootOutput(state, stubDecompositionRootResolver{
		root:      true,
		outputRef: "plan_ref",
	}, "custom-main-pair", output)
	if err != nil {
		t.Fatalf("validateDecompositionRootOutput() unexpected error: %v", err)
	}
}

func TestValidateDependsOnAcyclicRejectsOutOfRangeReference(t *testing.T) {
	output := []models.OutputEntry{
		{
			Desc:      "Plan storage",
			DoneWhen:  "storage plan is complete",
			Scope:     "internal/storage",
			SpecRef:   "specs/master.md",
			DependsOn: []string{"99"},
		},
	}

	err := validateDependsOnAcyclic(output)
	testhelpers.RequireErrorContains(t, err, `output[0].depends_on reference "99" out of range`)
}

func TestSetTaskOutput_NonRootAllowsOutputWithoutDecomposition(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusCodePlanning, now)
	task.AssignedTo = testhelpers.StringPtr("code-planner-1")
	state.Tasks = []models.Task{
		task,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := SetTaskOutput(tmpDir, &SetTaskOutputInput{
		TaskID:  "task-1",
		AgentID: "code-planner-1",
		Output: []models.OutputEntry{{
			Desc:     "Plan a scoped change",
			DoneWhen: "plan is reviewed",
			Scope:    "internal/ops",
		}},
	})
	if err != nil {
		t.Fatalf("SetTaskOutput() unexpected error: %v", err)
	}
}

type stubDecompositionRootResolver struct {
	root      bool
	outputRef string
}

func (r stubDecompositionRootResolver) IsDecompositionRoot(string) (bool, error) {
	return r.root, nil
}

func (r stubDecompositionRootResolver) DecompositionOutputRef(string) (string, error) {
	return r.outputRef, nil
}

func setupSetTaskOutputRootTask(t *testing.T, rolePair string, status models.TaskStatus) string {
	t.Helper()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()

	root := testhelpers.BuildTaskByStatus("root-task", status, now)
	root.RolePair = rolePair
	root.AssignedTo = testhelpers.StringPtr("master-agent")
	existingPlan := testhelpers.BuildTaskByStatus("existing-plan", models.TaskStatusMerged, now)
	existingPlan.RolePair = "code-planning-pair"

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{root, existingPlan}
	testhelpers.WriteInitialState(t, stateFile, state)
	return tmpDir
}

func validDecompositionRootOutput(planRef string) []models.OutputEntry {
	return []models.OutputEntry{
		{
			Desc:     "Plan storage boundaries",
			DoneWhen: "storage plan is complete",
			Scope:    "internal/storage",
			SpecRef:  "specs/master.md",
			PlanRef:  planRef,
			TaskDependsOn: []string{
				"existing-plan",
			},
			Decomposition: &models.DecompositionManifest{
				OwnedFiles:            []string{"internal/a.go"},
				ReadOnlyTaskDependsOn: []string{"existing-plan"},
				InterfacesOwned:       []string{"PlanContract"},
				CoverageNotes:         "Storage scope is bounded.",
			},
		},
		{
			Desc:      "Plan ops boundaries",
			DoneWhen:  "ops plan is complete",
			Scope:     "internal/ops",
			SpecRef:   "specs/master.md",
			PlanRef:   planRef,
			DependsOn: []string{"0"},
			Decomposition: &models.DecompositionManifest{
				OwnedFiles:        []string{"internal/b.go"},
				ReadOnlyDependsOn: []int{0},
				CoverageNotes:     "Ops scope is bounded.",
			},
		},
	}
}

func validArchitectureRootOutput(archRef string) []models.OutputEntry {
	output := validDecompositionRootOutput("specs/plans/master.md")
	for i := range output {
		output[i].PlanRef = ""
		output[i].ArchRef = archRef
	}
	return output
}
