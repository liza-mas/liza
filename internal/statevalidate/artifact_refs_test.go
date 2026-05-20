package statevalidate

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestCollectArtifactRefsNormalizesAndSortsWithOwnerProvenance(t *testing.T) {
	projectRoot := t.TempDir()
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()
	state.Goal.SpecRef = "specs/z-goal.md#intro"
	state.Tasks = []models.Task{
		{
			ID:          "task-b",
			Description: "Task B",
			Status:      models.TaskStatusReady,
			Priority:    1,
			Created:     now,
			SpecRef:     filepath.Join(projectRoot, "specs", "a-shared.md") + "#absolute",
			EpicRef:     "specs/c-epic.md#chapter",
			PlanRef:     "specs/a-shared.md#plan",
			ArchRef:     "specs/b-arch.md",
			DoneWhen:    "Done",
			Scope:       "test",
			Iteration:   1,
			Output: []models.OutputEntry{
				{
					Desc:     "First output",
					DoneWhen: "Done",
					Scope:    "test",
					SpecRef:  "specs/a-shared.md#output-spec",
					EpicRef:  "specs/a-output-epic.md#details",
					PlanRef:  "specs/a-output-plan.md#details",
					ArchRef:  "specs/a-shared.md#output-arch",
				},
				{
					Desc:     "Second output",
					DoneWhen: "Done",
					Scope:    "test",
					SpecRef:  "specs/d-output.md#section",
					EpicRef:  "specs/e-output-epic.md#section",
					ArchRef:  "specs/a-shared.md#output-arch",
				},
			},
		},
		{
			ID:          "task-a",
			Description: "Task A",
			Status:      models.TaskStatusReady,
			Priority:    1,
			Created:     now,
			SpecRef:     "specs/a-shared.md#task-a",
			DoneWhen:    "Done",
			Scope:       "test",
			Iteration:   1,
		},
	}

	refs, err := CollectArtifactRefs(state, projectRoot)
	if err != nil {
		t.Fatalf("CollectArtifactRefs returned error: %v", err)
	}

	got := make([]string, 0, len(refs))
	for _, ref := range refs {
		got = append(got, refSnapshot(ref))
	}
	want := []struct {
		requirement string
		snapshot    string
	}{
		{
			requirement: "FR-001-3 output owner provenance",
			snapshot:    "specs/a-output-epic.md|specs/a-output-epic.md#details|output[0].epic_ref|task-b|0",
		},
		{
			requirement: "FR-001-3 output owner provenance",
			snapshot:    "specs/a-output-plan.md|specs/a-output-plan.md#details|output[0].plan_ref|task-b|0",
		},
		{
			requirement: "FR-001-3 task owner provenance",
			snapshot:    "specs/a-shared.md|specs/a-shared.md#task-a|spec_ref|task-a|",
		},
		{
			requirement: "FR-001-4 deterministic task owner ordering",
			snapshot:    "specs/a-shared.md|specs/a-shared.md#plan|plan_ref|task-b|",
		},
		{
			requirement: "FR-001-7 valid absolute refs normalize under project root",
			snapshot:    "specs/a-shared.md|" + filepath.Join(projectRoot, "specs", "a-shared.md") + "#absolute|spec_ref|task-b|",
		},
		{
			requirement: "FR-001-3 output owner provenance",
			snapshot:    "specs/a-shared.md|specs/a-shared.md#output-arch|output[0].arch_ref|task-b|0",
		},
		{
			requirement: "FR-001-3 output owner provenance",
			snapshot:    "specs/a-shared.md|specs/a-shared.md#output-spec|output[0].spec_ref|task-b|0",
		},
		{
			requirement: "FR-001-4 deterministic output index ordering",
			snapshot:    "specs/a-shared.md|specs/a-shared.md#output-arch|output[1].arch_ref|task-b|1",
		},
		{
			requirement: "FR-001-3 task owner provenance",
			snapshot:    "specs/b-arch.md|specs/b-arch.md|arch_ref|task-b|",
		},
		{
			requirement: "FR-001-3 task owner provenance",
			snapshot:    "specs/c-epic.md|specs/c-epic.md#chapter|epic_ref|task-b|",
		},
		{
			requirement: "FR-001-3 output owner provenance",
			snapshot:    "specs/d-output.md|specs/d-output.md#section|output[1].spec_ref|task-b|1",
		},
		{
			requirement: "FR-001-3 output owner provenance",
			snapshot:    "specs/e-output-epic.md|specs/e-output-epic.md#section|output[1].epic_ref|task-b|1",
		},
		{
			requirement: "FR-001-3 goal owner provenance",
			snapshot:    "specs/z-goal.md|specs/z-goal.md#intro|goal.spec_ref||",
		},
	}
	if len(got) != len(want) {
		t.Fatalf("ref count = %d, want %d\n%#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i].snapshot {
			t.Errorf("%s: refs[%d] = %#v, want %#v", want[i].requirement, i, got[i], want[i].snapshot)
		}
	}
}

func TestCollectArtifactRefsRejectsInvalidRefsWithOwnerDiagnostics(t *testing.T) {
	projectRoot := t.TempDir()
	outsideRoot := t.TempDir()
	tests := []struct {
		name        string
		configure   func(*models.State)
		wantCause   string
		wantField   string
		wantValue   string
		wantTaskID  string
		wantOutput  *int
		wantText    []string
		requirement string
	}{
		{
			name: "semicolon joined refs",
			configure: func(state *models.State) {
				state.Tasks = []models.Task{artifactRefTask("task-1", "specs/a.md; specs/b.md")}
			},
			wantCause:   artifactRefMultipleRefsCause,
			wantField:   "spec_ref",
			wantValue:   "specs/a.md; specs/b.md",
			wantTaskID:  "task-1",
			wantText:    []string{"spec_ref", "multiple refs", "task-1"},
			requirement: "FR-001-6 semicolon-joined refs fail closed",
		},
		{
			name: "fragment only ref",
			configure: func(state *models.State) {
				state.Goal.SpecRef = "#intro"
			},
			wantCause:   artifactRefEmptyPathCause,
			wantField:   "goal.spec_ref",
			wantValue:   "#intro",
			wantText:    []string{"goal.spec_ref", "empty path", "#intro"},
			requirement: "FR-001-6 empty paths after fragment stripping fail closed",
		},
		{
			name: "path traversal outside repo",
			configure: func(state *models.State) {
				state.Tasks = []models.Task{artifactRefTaskWithOutput("task-2", models.OutputEntry{
					Desc:     "Output",
					DoneWhen: "Done",
					Scope:    "test",
					SpecRef:  "../outside.md",
				})}
			},
			wantCause:   artifactRefPathTraversalCause,
			wantField:   "output[0].spec_ref",
			wantValue:   "../outside.md",
			wantTaskID:  "task-2",
			wantOutput:  intPtr(0),
			wantText:    []string{"output[0].spec_ref", "outside repository", "task-2", "output: 0"},
			requirement: "FR-001-6 traversal outside repository fails closed",
		},
		{
			name: "absolute path outside repo",
			configure: func(state *models.State) {
				state.Tasks = []models.Task{artifactRefTaskWithOutput("task-3", models.OutputEntry{
					Desc:     "Output",
					DoneWhen: "Done",
					Scope:    "test",
					SpecRef:  "specs/task.md",
					ArchRef:  filepath.Join(outsideRoot, "specs", "arch.md"),
				})}
			},
			wantCause:   artifactRefAbsoluteOutsideRepoCause,
			wantField:   "output[0].arch_ref",
			wantValue:   filepath.Join(outsideRoot, "specs", "arch.md"),
			wantTaskID:  "task-3",
			wantOutput:  intPtr(0),
			wantText:    []string{"output[0].arch_ref", "outside repository", "task-3", "output: 0"},
			requirement: "FR-001-7 unsafe absolute refs reject with diagnostics",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			tt.configure(state)

			_, err := CollectArtifactRefs(state, projectRoot)
			if err == nil {
				t.Fatal("CollectArtifactRefs returned nil error")
			}

			var refErr *ArtifactRefError
			if !errors.As(err, &refErr) {
				t.Fatalf("error type = %T, want *ArtifactRefError", err)
			}
			if refErr.Cause != tt.wantCause {
				t.Errorf("Cause = %q, want %q", refErr.Cause, tt.wantCause)
			}
			if refErr.Field != tt.wantField {
				t.Errorf("Field = %q, want %q", refErr.Field, tt.wantField)
			}
			if refErr.Value != tt.wantValue {
				t.Errorf("Value = %q, want %q", refErr.Value, tt.wantValue)
			}
			if refErr.TaskID != tt.wantTaskID {
				t.Errorf("TaskID = %q, want %q", refErr.TaskID, tt.wantTaskID)
			}
			if !sameOutputIndex(refErr.OutputIndex, tt.wantOutput) {
				t.Errorf("OutputIndex = %v, want %v", refErr.OutputIndex, tt.wantOutput)
			}
			for _, want := range tt.wantText {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Error = %q, want to contain %q", err.Error(), want)
				}
			}

			details := refErr.SafeDetails()
			if details["cause"] != tt.wantCause {
				t.Errorf("%s: SafeDetails cause = %v, want %q", tt.requirement, details["cause"], tt.wantCause)
			}
			if details["field"] != tt.wantField {
				t.Errorf("%s: SafeDetails field = %v, want %q", tt.requirement, details["field"], tt.wantField)
			}
			if details["value"] != tt.wantValue {
				t.Errorf("%s: SafeDetails value = %v, want %q", tt.requirement, details["value"], tt.wantValue)
			}
			if tt.wantOutput != nil && details["output_index"] != *tt.wantOutput {
				t.Errorf("SafeDetails output_index = %v, want %d", details["output_index"], *tt.wantOutput)
			}
		})
	}
}

func refSnapshot(ref ArtifactRef) string {
	outputIndex := ""
	if ref.Owner.OutputIndex != nil {
		outputIndex = fmt.Sprintf("%d", *ref.Owner.OutputIndex)
	}
	return strings.Join([]string{ref.Path, ref.Raw, ref.Owner.Field, ref.Owner.TaskID, outputIndex}, "|")
}

func artifactRefTask(id, specRef string) models.Task {
	now := time.Now().UTC()
	return models.Task{
		ID:          id,
		Description: "Test task",
		Status:      models.TaskStatusReady,
		Priority:    1,
		Created:     now,
		SpecRef:     specRef,
		DoneWhen:    "Done",
		Scope:       "test",
		Iteration:   1,
	}
}

func artifactRefTaskWithOutput(id string, output models.OutputEntry) models.Task {
	task := artifactRefTask(id, "specs/task.md")
	task.Output = []models.OutputEntry{output}
	return task
}

func intPtr(value int) *int {
	return &value
}

func sameOutputIndex(got, want *int) bool {
	if got == nil || want == nil {
		return got == want
	}
	return *got == *want
}
