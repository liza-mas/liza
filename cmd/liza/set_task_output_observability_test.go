package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestJSON_SetTaskOutput_AssignmentMismatchPreservesValidation(t *testing.T) {
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		state.Agents["code-planner-1"] = mutationTestAgent("code-planner")
		state.Agents["code-planner-2"] = mutationTestAgent("code-planner")
		task := testhelpers.BuildTaskByStatus("output-plan", models.TaskStatusCodePlanning, time.Now().UTC())
		task.RolePair = "code-planning-pair"
		task.AssignedTo = testhelpers.StringPtr("code-planner-2")
		state.Tasks = []models.Task{task}
	})
	outputFile := filepath.Join(projectRoot, "output.json")
	if err := os.WriteFile(outputFile, []byte(`[]`), 0644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := executeRootCommandCapture(t, projectRoot, "set-task-output", "output-plan", "--agent-id", "code-planner-1", "--output", outputFile, "--json")
	if err == nil {
		t.Fatalf("expected assignment mismatch, got %s", stdout)
	}
	env := parseEnvelope(t, stdout)
	errObj, ok := env["error"].(map[string]any)
	if env["ok"] != false || !ok || errObj["code"] != "stale_caller" {
		t.Fatalf("expected stale-caller envelope, got %s", stdout)
	}
	assertLifecycleFailurePolicy(t, env, models.LifecycleStaleCaller, "stop")
	message, _ := errObj["message"].(string)
	if !strings.Contains(message, "not assigned to agent code-planner-1 (currently assigned to: code-planner-2)") {
		t.Fatalf("missing assignment diagnostic: %s", stdout)
	}
	details, ok := errObj["details"].(map[string]any)
	if !ok || details["operation"] != "set-task-output" || details["phase"] != "persist-output" || details["task_id"] != "output-plan" {
		t.Fatalf("missing operation context: %s", stdout)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("rejected output write changed task state")
	}
}

func TestJSON_SetTaskOutput_ReceiptAndHistory(t *testing.T) {
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		state.Agents["code-planner-1"] = mutationTestAgent("code-planner")
		task := testhelpers.BuildTaskByStatus("output-plan", models.TaskStatusCodePlanning, time.Now().UTC())
		task.RolePair = "code-planning-pair"
		task.AssignedTo = testhelpers.StringPtr("code-planner-1")
		state.Tasks = []models.Task{task}
	})
	outputFile := filepath.Join(projectRoot, "output.json")
	if err := os.WriteFile(outputFile, []byte(`[{"desc":"Implement feature","done_when":"Tests pass","scope":"src/feature","spec_ref":"specs/vision.md","plan_ref":"specs/plan.md"}]`), 0644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		stdout, err := executeRootCommandCapture(t, projectRoot, "set-task-output", "output-plan", "--agent-id", "code-planner-1", "--output", outputFile, "--json")
		if err != nil {
			t.Fatalf("set-task-output: %v; response: %s", err, stdout)
		}
		env := parseEnvelope(t, stdout)
		result, ok := env["result"].(map[string]any)
		if env["ok"] != true || !ok {
			t.Fatalf("expected successful write receipt, got %s", stdout)
		}
		if result["task_id"] != "output-plan" || result["output_count"] != float64(1) || result["state_path"] != statePath {
			t.Fatalf("unexpected receipt: %#v", result)
		}
		task := mustFindTask(t, readState(t, statePath), "output-plan")
		if len(task.Output) != 1 || task.Output[0].Desc != "Implement feature" {
			t.Fatalf("receipt does not match persisted output: %#v", task.Output)
		}
		event := task.History[len(task.History)-1]
		if event.Event != "task_output_set" || event.Agent == nil || *event.Agent != "code-planner-1" || event.Time.IsZero() || event.Extra["output_count"] != 1 || event.Extra["previous_output_count"] != i {
			t.Fatalf("missing write audit evidence: %#v", event)
		}
	}
}
