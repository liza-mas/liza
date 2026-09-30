package statevalidate

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestValidateDependencies_DependencyCyclePath(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	taskA := testhelpers.BuildTaskByStatus("A", models.TaskStatusReady, now)
	taskA.DependsOn = []string{"B"}
	taskB := testhelpers.BuildTaskByStatus("B", models.TaskStatusReady, now)
	taskB.DependsOn = []string{"C"}
	taskC := testhelpers.BuildTaskByStatus("C", models.TaskStatusReady, now)
	taskC.DependsOn = []string{"A"}
	state.Tasks = []models.Task{taskA, taskB, taskC}

	err := dependenciesErr(state, "", true, nil, nil, nil)
	var cycleErr *DependencyCycleError
	if !errors.As(err, &cycleErr) {
		t.Fatalf("dependenciesErr() error = %T %v, want *DependencyCycleError", err, err)
	}
	// One violation per cyclic edge, in task order (ADR-0165): each edge is
	// an identity, so a new cycle cannot hide behind an old one.
	want := strings.Join([]string{
		"circular dependency detected: A eventually depends on itself",
		"circular dependency detected: B eventually depends on itself",
		"circular dependency detected: C eventually depends on itself",
	}, "\n")
	if got := err.Error(); got != want {
		t.Fatalf("dependenciesErr() error = %q, want %q", got, want)
	}
	wantPath := []string{"A", "B", "C", "A"}
	if !slices.Equal(cycleErr.CyclePath, wantPath) {
		t.Fatalf("DependencyCycleError.CyclePath = %v, want %v", cycleErr.CyclePath, wantPath)
	}
}

func TestValidateDependencies_RejectsMalformedDependsOn(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name    string
		task    models.Task
		wantErr string
	}{
		{
			name: "untrimmed",
			task: func() models.Task {
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
				task.DependsOn = []string{" dep-1 "}
				return task
			}(),
			wantErr: "must be non-empty and trimmed",
		},
		{
			name: "duplicate",
			task: func() models.Task {
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
				task.DependsOn = []string{"dep-1", "dep-1"}
				return task
			}(),
			wantErr: "duplicate depends_on entry",
		},
		{
			name: "self",
			task: func() models.Task {
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
				task.DependsOn = []string{"task-1"}
				return task
			}(),
			wantErr: "referencing itself",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			dep := testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusMerged, now)
			state.Tasks = []models.Task{tt.task, dep}

			err := dependenciesErr(state, "", true, nil, nil, nil)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateDependencies_RejectsNonTerminalSupersededDependency(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
	task.DependsOn = []string{"dep-1"}
	dep := models.Task{ID: "dep-1", Status: models.TaskStatusSuperseded}
	state.Tasks = []models.Task{task, dep}

	err := dependenciesErr(state, "", true, nil, nil, nil)
	if err == nil {
		t.Fatal("dependenciesErr() error = nil, want terminal dependency error")
	}
	if !strings.Contains(err.Error(), "non-terminal task task-1 depends on terminal non-merged task dep-1") {
		t.Fatalf("error = %q, want terminal dependency error", err.Error())
	}
}

func TestValidateDependencies_DoesNotWarnForNonExecutingDirectPendingDependency(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
	task.DependsOn = []string{"dep-1"}
	dep := testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusReady, now)
	state.Tasks = []models.Task{task, dep}

	var warnings bytes.Buffer
	if err := dependenciesErr(state, "", true, nil, nil, &warnings); err != nil {
		t.Fatalf("dependenciesErr() error = %v", err)
	}
	if warnings.Len() != 0 {
		t.Fatalf("warnings = %q, want none for ordinary pending dependency", warnings.String())
	}
}

func TestValidateDependencies_RejectsDownstreamDependency(t *testing.T) {
	now := time.Now().UTC()
	resolver, cfg := dependencyDirectionResolver(t)

	task := testhelpers.BuildTaskByStatus("plan-1", models.TaskStatusDraftCodingPlan, now)
	task.RolePair = "code-planning-pair"
	task.DependsOn = []string{"coding-1"}
	dep := testhelpers.BuildTaskByStatus("coding-1", models.TaskStatusMerged, now)
	dep.RolePair = "coding-pair"
	state := &models.State{Tasks: []models.Task{task, dep}}

	err := dependenciesErr(state, "", true, resolver, cfg, nil)
	if err == nil {
		t.Fatal("dependenciesErr() error = nil, want downstream dependency error")
	}
	if !strings.Contains(err.Error(), "role_pair coding-pair is downstream of code-planning-pair") {
		t.Fatalf("error = %q, want downstream role-pair error", err.Error())
	}
}

func TestValidateDependencies_RejectsNonTerminalSupersededDependencyWithReplacement(t *testing.T) {
	now := time.Now().UTC()
	resolver, cfg := dependencyDirectionResolver(t)

	task := testhelpers.BuildTaskByStatus("plan-1", models.TaskStatusDraftCodingPlan, now)
	task.RolePair = "code-planning-pair"
	task.DependsOn = []string{"old-plan"}
	oldPlan := testhelpers.BuildTaskByStatus("old-plan", models.TaskStatusSuperseded, now)
	oldPlan.RolePair = "code-planning-pair"
	oldPlan.SupersededBy = []string{"coding-1"}
	coding := testhelpers.BuildTaskByStatus("coding-1", models.TaskStatusMerged, now)
	coding.RolePair = "coding-pair"
	state := &models.State{Tasks: []models.Task{task, oldPlan, coding}}

	err := dependenciesErr(state, "", true, resolver, cfg, nil)
	if err == nil {
		t.Fatal("dependenciesErr() error = nil, want terminal dependency error")
	}
	if !strings.Contains(err.Error(), "non-terminal task plan-1 depends on terminal non-merged task old-plan") {
		t.Fatalf("error = %q, want terminal dependency error", err.Error())
	}
}

func dependencyDirectionResolver(t *testing.T) (*pipeline.Resolver, *pipeline.PipelineConfig) {
	t.Helper()
	tmpDir := t.TempDir()
	testhelpers.SetupPipelineConfig(t, tmpDir)
	cfg, err := pipeline.LoadFrozen(tmpDir)
	if err != nil {
		t.Fatalf("LoadFrozen: %v", err)
	}
	return pipeline.NewResolver(cfg), cfg
}

func TestValidateState_WarnsBlockedReasonReferencesTaskWithoutDependsOn(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	reason := "Waiting on dep-1 to merge first"
	blocked := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	blocked.BlockedReason = &reason
	blocked.DependsOn = nil
	dep := testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusReady, now)
	state.Tasks = []models.Task{blocked, dep}

	var warnings bytes.Buffer
	tmpDir := t.TempDir()
	testhelpers.SetupPipelineConfig(t, tmpDir)
	if err := ValidateState(state, tmpDir, true, &warnings); err != nil {
		t.Fatalf("ValidateState() error: %v", err)
	}
	if !strings.Contains(warnings.String(), "blocked_reason references task dep-1 but depends_on is empty") {
		t.Fatalf("warnings = %q, want blocked dependency warning", warnings.String())
	}
}

func TestValidateState_WarnsBlockedReasonMatchesWholeTaskID(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	reason := "Waiting on task-10 to merge first"
	blocked := testhelpers.BuildTaskByStatus("blocked-task", models.TaskStatusBlocked, now)
	blocked.BlockedReason = &reason
	blocked.DependsOn = nil
	task1 := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
	task10 := testhelpers.BuildTaskByStatus("task-10", models.TaskStatusReady, now)
	state.Tasks = []models.Task{blocked, task1, task10}

	var warnings bytes.Buffer
	tmpDir := t.TempDir()
	testhelpers.SetupPipelineConfig(t, tmpDir)
	if err := ValidateState(state, tmpDir, true, &warnings); err != nil {
		t.Fatalf("ValidateState() error: %v", err)
	}
	if !strings.Contains(warnings.String(), "blocked_reason references task task-10 but depends_on is empty") {
		t.Fatalf("warnings = %q, want task-10 dependency warning", warnings.String())
	}
	if strings.Contains(warnings.String(), "blocked_reason references task task-1 but depends_on is empty") {
		t.Fatalf("warnings = %q, should not match task-1 as a substring of task-10", warnings.String())
	}
}
