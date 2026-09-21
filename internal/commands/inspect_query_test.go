package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func inspectQueryFixture(t *testing.T, tasks []models.Task) (string, string) {
	t.Helper()
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.SetupPipelineConfig(t, root)
	state := testhelpers.CreateValidState()
	state.Tasks = tasks
	testhelpers.WriteInitialState(t, statePath, state)
	return root, statePath
}

func TestInspectQueryTaskAliasesAndDottedIDs(t *testing.T) {
	now := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC)
	var tasks []models.Task
	for _, id := range []string{"work", "work.part", "work.part.status", "config.mode", "completion_rate", "work.age", "work.time_in_status", "work.transition_id"} {
		tasks = append(tasks, models.Task{ID: id, Description: "Task " + id, Status: models.TaskStatusImplementing, Created: now})
	}
	root, _ := inspectQueryFixture(t, tasks)
	for _, format := range []string{"json", "yaml", "value", "table"} {
		for _, id := range []string{"work", "work.part", "work.part.status"} {
			t.Run(format+"/"+id, func(t *testing.T) {
				opts := InspectOptions{ProjectRoot: root, Format: format, Summary: true}
				want, err := InspectCommand([]string{"tasks", id}, opts)
				if err != nil {
					t.Fatal(err)
				}
				for _, alias := range []string{id, "tasks." + id} {
					got, err := InspectCommand([]string{alias}, opts)
					if err != nil || got != want {
						t.Fatalf("%s: got %q, %v; want %q", alias, got, err, want)
					}
				}
			})
		}
	}
	for _, query := range []string{"work.part.status.status", "tasks.work.part.status.status", "task.work.part.status.status"} {
		got, err := InspectCommand([]string{query}, InspectOptions{ProjectRoot: root, Format: "json"})
		if err != nil || got != `"IMPLEMENTING_CODE"` {
			t.Fatalf("longest-ID query %s = %q, %v", query, got, err)
		}
	}
	for _, query := range []string{"config.mode", "goal.description", "tasks.completion_rate", "task.work.transition_id", "task.work.part.transition_id"} {
		got, err := InspectCommand([]string{query}, InspectOptions{ProjectRoot: root, Format: "json"})
		if err != nil || strings.Contains(got, `"description":`) {
			t.Fatalf("reserved/computed query %s routed as a task: %q, %v", query, got, err)
		}
	}
	for _, field := range []string{"age", "time_in_status", "transition_id"} {
		got, err := InspectCommand([]string{"task.work." + field}, InspectOptions{ProjectRoot: root, Format: "json"})
		var computed string
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(got), &computed); err != nil || computed == "" {
			t.Fatalf("legacy computed field %s shadowed by literal ID: %q, %v", field, got, err)
		}
		got, err = InspectCommand([]string{"tasks.work." + field}, InspectOptions{ProjectRoot: root, Format: "json", Fields: []string{"id"}})
		if err != nil || !strings.Contains(got, `"id": "work.`+field+`"`) {
			t.Fatalf("plural literal alias %s = %q, %v", field, got, err)
		}
	}
	got, err := InspectCommand([]string{"tasks", "config.mode"}, InspectOptions{ProjectRoot: root, Format: "json", Fields: []string{"id"}})
	if err != nil || !strings.Contains(got, `"id": "config.mode"`) {
		t.Fatalf("literal reserved ID is inaccessible: %q, %v", got, err)
	}
}

func TestInspectQueryHistoryAndRejectionFields(t *testing.T) {
	reason := "Missing validation.\nProvide race-test evidence."
	now := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC)
	history := []models.TaskHistoryEntry{{Time: now, Event: models.TaskEventRejected, Reason: &reason}}
	root, _ := inspectQueryFixture(t, []models.Task{
		{ID: "work.part", Status: models.TaskStatusImplementing, Created: now, History: history, RejectionReason: &reason,
			RejectionRCA: &models.RejectionRCARecord{Summary: "Evidence was absent"}},
		{ID: "fresh", Status: models.TaskStatusReady, Created: now},
	})
	for _, prefix := range []string{"tasks.work.part", "work.part", "task.work.part"} {
		output, err := InspectCommand([]string{prefix + ".history"}, InspectOptions{ProjectRoot: root, Format: "json"})
		if err != nil {
			t.Fatal(err)
		}
		var got []map[string]any
		want := []map[string]any{{"time": now.Format(time.RFC3339), "event": models.TaskEventRejected, "reason": reason}}
		if err := json.Unmarshal([]byte(output), &got); err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("history JSON = %#v, %v; want %#v", got, err, want)
		}
	}
	output, err := InspectCommand([]string{"tasks", "work.part"}, InspectOptions{ProjectRoot: root, Format: "json", Fields: []string{"history"}})
	if err != nil {
		t.Fatal(err)
	}
	var projection map[string][]map[string]any
	want := map[string][]map[string]any{"history": {{"time": now.Format(time.RFC3339), "event": models.TaskEventRejected, "reason": reason}}}
	if err := json.Unmarshal([]byte(output), &projection); err != nil || !reflect.DeepEqual(projection, want) {
		t.Fatalf("history projection JSON = %#v, %v; want %#v", projection, err, want)
	}
	for _, tc := range []struct{ query, want string }{
		{"tasks.work.part.rejection_reason", reason},
		{"work.part.rejection_rca.summary", "Evidence was absent"},
		{"fresh.rejection_reason", ""},
	} {
		output, err := InspectCommand([]string{tc.query}, InspectOptions{ProjectRoot: root, Format: "value"})
		if err != nil || output != tc.want {
			t.Fatalf("%s = %q, %v; want %q", tc.query, output, err, tc.want)
		}
	}
	for _, query := range []string{"fresh.rejection_reason", "fresh.rejection_rca.summary", "fresh.rejection_rca.schema_version"} {
		output, err := InspectCommand([]string{query}, InspectOptions{ProjectRoot: root, Format: "json"})
		if err != nil || output != "null" {
			t.Fatalf("absent %s = %q, %v; want null", query, output, err)
		}
	}
}

func TestInspectQueryProjection(t *testing.T) {
	reason := "Add coverage\nfor rejected submissions"
	root, _ := inspectQueryFixture(t, []models.Task{
		{ID: "work", Status: models.TaskStatusImplementing, RejectionReason: &reason},
		{ID: "done", Status: models.TaskStatusMerged},
	})
	opts := InspectOptions{ProjectRoot: root, Format: "json", Fields: []string{"id,rejection_reason", "rejection_rca.summary", "id"}}
	output, err := InspectCommand([]string{"tasks"}, opts)
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"id": "work", "rejection_reason": reason, "rejection_rca.summary": nil},
		{"id": "done", "rejection_reason": nil, "rejection_rca.summary": nil},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projection = %#v; want %#v", got, want)
	}
	opts.Active = true
	output, err = InspectCommand([]string{"tasks"}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(output), &got); err != nil || !reflect.DeepEqual(got, want[:1]) {
		t.Fatalf("active projection = %#v, %v; want %#v", got, err, want[:1])
	}
	for _, alias := range [][]string{{"tasks", "done"}, {"tasks.done"}, {"done"}} {
		output, err = InspectCommand(alias, opts)
		if err != nil || output != "null" {
			t.Fatalf("filtered single %v = %q, %v; want null", alias, output, err)
		}
	}
	output, err = InspectCommand([]string{"tasks", "work"}, opts)
	var single map[string]any
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(output), &single); err != nil || !reflect.DeepEqual(single, want[0]) {
		t.Fatalf("single projection = %#v, %v; want %#v", single, err, want[0])
	}
	for _, format := range []string{"yaml", "value"} {
		opts.Format = format
		output, err := InspectCommand([]string{"tasks", "work"}, opts)
		if err != nil || !strings.Contains(output, "rejection_reason") || !strings.Contains(output, "for rejected submissions") || strings.Contains(output, "description") {
			t.Fatalf("%s projection = %q, %v", format, output, err)
		}
	}
}

func TestInspectQueryRejectsUnsupportedPathsAndOptions(t *testing.T) {
	root, _ := inspectQueryFixture(t, []models.Task{{ID: "work", Status: models.TaskStatusImplementing}})
	for _, query := range []string{
		"tasks.work.", "work..status", "tasks.work.nonexistent", "work.history.0",
		"work.rejection_rca.nonexistent", "work.rejection_reason.invalid", "work.integration_failure.key",
		"config.mode.extra", "sprint.elapsed.extra", "tasks.completion_rate.extra", "task.work.age.extra", "tasks.missing",
	} {
		if _, err := InspectCommand([]string{query}, InspectOptions{ProjectRoot: root, Format: "json"}); err == nil {
			t.Errorf("unsupported path accepted: %s", query)
		}
	}
	for _, args := range [][]string{
		{"tasks", "work", "extra"}, {"agents", "coder-1", "extra"}, {"work", "extra"},
		{"config.mode", "extra"}, {"metrics", "extra"}, {"config", "extra"},
	} {
		if _, err := InspectCommand(args, InspectOptions{ProjectRoot: root}); err == nil {
			t.Errorf("surplus arguments accepted: %v", args)
		}
	}
	for _, opts := range []InspectOptions{
		{Fields: []string{""}}, {Fields: []string{"id,"}}, {Fields: []string{"id,,status"}},
		{Fields: []string{"unknown"}}, {Fields: []string{"id"}, Summary: true},
		{Fields: []string{"id"}, OutputSummary: true}, {Fields: []string{"id"}, Zombies: true},
		{Fields: []string{"id"}, Format: "table"},
	} {
		opts.ProjectRoot = root
		if _, err := InspectCommand([]string{"tasks"}, opts); err == nil {
			t.Errorf("invalid projection accepted: %+v", opts)
		}
	}
	for _, query := range []string{"agents", "config", "config.mode", "work.status", "tasks.work.status", "tasks.completion_rate"} {
		if _, err := InspectCommand([]string{query}, InspectOptions{ProjectRoot: root, Fields: []string{"id"}}); err == nil {
			t.Errorf("projection accepted for non-task query: %s", query)
		}
	}
	for _, opts := range []InspectOptions{{Active: true}, {Summary: true}, {OutputSummary: true}, {Zombies: true}} {
		opts.ProjectRoot = root
		if _, err := InspectCommand([]string{"work.status"}, opts); err == nil {
			t.Errorf("entity options accepted for scalar query: %+v", opts)
		}
	}
	emptyRoot, _ := inspectQueryFixture(t, nil)
	for _, field := range []string{"unknown", "rejection_rca.unknown", "history.0", "id."} {
		if _, err := InspectCommand([]string{"tasks"}, InspectOptions{ProjectRoot: emptyRoot, Fields: []string{field}}); err == nil {
			t.Errorf("invalid field accepted on empty tasks: %s", field)
		}
	}
	output, err := InspectCommand([]string{"tasks"}, InspectOptions{ProjectRoot: emptyRoot, Format: "json", Fields: []string{"rejection_rca.summary"}})
	if err != nil || output != "[]" {
		t.Fatalf("valid empty projection = %q, %v; want []", output, err)
	}
}

func TestInspectQueryLifecycleRedaction(t *testing.T) {
	task := lifecycleInspectionTask()
	root, statePath := inspectQueryFixture(t, []models.Task{task})
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"json", "yaml", "value"} {
		for _, field := range []string{"lifecycle", "lifecycle.preparation", "lifecycle.receipts"} {
			for _, projection := range []bool{false, true} {
				opts := InspectOptions{ProjectRoot: root, Format: format}
				args := []string{"tasks." + task.ID + "." + field}
				if projection {
					args = []string{"tasks", task.ID}
					opts.Fields = []string{field}
				}
				output, err := InspectCommand(args, opts)
				if err != nil {
					t.Fatal(err)
				}
				for _, forbidden := range []string{"test-receipt-generation-digest", "test-preparation-generation-digest", "generation_digest"} {
					if strings.Contains(output, forbidden) {
						t.Errorf("%s %v leaks %q: %s", format, args, forbidden, output)
					}
				}
			}
		}
		for _, field := range []string{"lifecycle.preparation.generation_digest", "lifecycle.receipts.0.generation_digest"} {
			for _, args := range [][]string{{"tasks." + task.ID + "." + field}, {task.ID + "." + field}, {"task." + task.ID + "." + field}} {
				if _, err := InspectCommand(args, InspectOptions{ProjectRoot: root, Format: format}); err == nil {
					t.Errorf("hidden field accepted: %v", args)
				}
			}
			if _, err := InspectCommand([]string{"tasks"}, InspectOptions{ProjectRoot: root, Format: format, Fields: []string{field}}); err == nil {
				t.Errorf("hidden projection accepted: %s", field)
			}
		}
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("query inspection mutated persisted state")
	}
	if task.Lifecycle.Preparation.GenerationDigest != "test-preparation-generation-digest" || task.Lifecycle.Receipts[0].GenerationDigest != "test-receipt-generation-digest" {
		t.Fatal("inspection mutated lifecycle authority")
	}
}
