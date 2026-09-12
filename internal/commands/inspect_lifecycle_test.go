package commands

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func lifecycleInspectionTask() models.Task {
	return models.Task{
		ID: "task-lifecycle", Description: "Inspect lifecycle boundary",
		Status: models.TaskStatusImplementing, AssignedTo: stringPtr("coder-1"),
		Created: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC),
		Lifecycle: &models.TaskLifecycle{
			Revision: 2,
			Receipts: []models.LifecycleReceipt{{
				LifecycleIdentity: models.LifecycleIdentity{
					Operation: "submit-for-review", Actor: "coder-1",
					GenerationDigest: "test-receipt-generation-digest",
					RequestID:        "request-1", ExpectedTransition: "old-boundary",
					PayloadDigest: "test-payload-digest",
				},
				Sequence: 1, TransitionID: "completed-boundary",
			}},
			Preparation: &models.LifecyclePreparation{
				LifecycleIdentity: models.LifecycleIdentity{
					Operation: "submit-for-review", Actor: "coder-1",
					GenerationDigest: "test-preparation-generation-digest",
					RequestID:        "request-2", ExpectedTransition: "next-boundary",
					PayloadDigest: "test-payload-digest",
				},
				Boundary: "next-boundary",
			},
		},
	}
}

func TestLifecycleInspection_TaskViewsExposeCurrentToken(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "with-receipts"
		if legacy {
			name = "legacy"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, root)
			testhelpers.SetupPipelineConfig(t, root)
			task := lifecycleInspectionTask()
			if legacy {
				task.Lifecycle = nil
			}
			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{task}
			testhelpers.WriteInitialState(t, statePath, state)
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			token := models.TaskTransitionID(&task)
			if token == "" {
				t.Fatal("fixture must have a current transition token")
			}
			queries := []struct {
				name          string
				args          []string
				summary       bool
				outputSummary bool
			}{
				{name: "shorthand", args: []string{task.ID}},
				{name: "single", args: []string{"tasks", task.ID}},
				{name: "list", args: []string{"tasks"}},
				{name: "summary", args: []string{"tasks"}, summary: true},
				{name: "output-summary", args: []string{"tasks", task.ID}, outputSummary: true},
				{name: "field", args: []string{"task." + task.ID + ".transition_id"}},
			}
			for _, query := range queries {
				for _, format := range []string{"json", "yaml"} {
					t.Run(query.name+"/"+format, func(t *testing.T) {
						output, err := InspectCommand(query.args, InspectOptions{
							Format: format, ProjectRoot: root,
							Summary: query.summary, OutputSummary: query.outputSummary,
						})
						if err != nil {
							t.Fatal(err)
						}
						if !strings.Contains(output, token) {
							t.Errorf("inspection omits current boundary token")
						}
						if query.name != "field" && !strings.Contains(output, "transition_id") {
							t.Error("task projection must name the additive transition_id field")
						}
						for _, forbidden := range []string{"test-receipt-generation-digest", "test-preparation-generation-digest", "generation_digest"} {
							if strings.Contains(output, forbidden) {
								t.Errorf("inspection exposes persistence-only field %q", forbidden)
							}
						}
					})
				}
			}
			after, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Error("inspection changed persisted task state")
			}
		})
	}
}

func TestLifecycleInspection_RawTaskFieldsRedactWithoutMutation(t *testing.T) {
	task := lifecycleInspectionTask()
	task.Extra = map[string]any{"custom_field": "preserve-me"}
	state := &models.State{Tasks: []models.Task{task}}
	before, err := formatOutput(state.Tasks, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := getField(state, "tasks")
	if err != nil {
		t.Fatal(err)
	}
	single := normalizeFieldValue(reflect.ValueOf(&state.Tasks[0]))
	for _, value := range []any{raw, single} {
		for _, format := range []string{"json", "yaml", "value"} {
			output, err := formatOutput(value, format)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"test-receipt-generation-digest", "test-preparation-generation-digest", "generation_digest"} {
				if strings.Contains(output, forbidden) {
					t.Errorf("%s raw task output exposes %q", format, forbidden)
				}
			}
			if !strings.Contains(output, "preserve-me") {
				t.Errorf("%s raw task output dropped unrelated data", format)
			}
		}
	}
	after, err := formatOutput(state.Tasks, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Error("redaction mutated original receipt/preparation metadata")
	}
}

func TestLifecycleInspection_InternalDigestSubfieldsDenied(t *testing.T) {
	state := &models.State{Tasks: []models.Task{lifecycleInspectionTask()}}
	for _, path := range []string{
		"tasks.0.lifecycle.preparation.generation_digest",
		"tasks.0.lifecycle.receipts.0.generation_digest",
		"task.task-lifecycle.lifecycle.preparation.generation_digest",
		"task.task-lifecycle.lifecycle.receipts.0.generation_digest",
	} {
		for _, format := range []string{"json", "yaml", "value"} {
			output, err := handleFieldQuery(state, path, InspectOptions{Format: format})
			if err == nil {
				t.Errorf("%s query %q must not expose unsupported internal fields", format, path)
			}
			for _, forbidden := range []string{"test-receipt-generation-digest", "test-preparation-generation-digest"} {
				if strings.Contains(output, forbidden) || err != nil && strings.Contains(err.Error(), forbidden) {
					t.Errorf("%s query %q leaked internal digest", format, path)
				}
			}
		}
	}
}

func TestLifecycleInspection_TokenTracksBoundaryNotLeaseRenewal(t *testing.T) {
	task := lifecycleInspectionTask()
	first := buildTaskInfo(&task, "").TransitionID
	renewed := time.Date(2026, 9, 12, 1, 0, 0, 0, time.UTC)
	task.LeaseExpires = &renewed
	if got := buildTaskInfo(&task, "").TransitionID; got != first {
		t.Error("passive lease renewal changed inspection token")
	}
	task.History = append(task.History, models.TaskHistoryEntry{
		Time: renewed, Event: models.TaskEventClaimed, Agent: task.AssignedTo,
	})
	if got := buildTaskInfo(&task, "").TransitionID; got == first {
		t.Error("new claim identity reused the old inspection token")
	}
}
