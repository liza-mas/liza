package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

const taskInputProviderYAML = `id: consumer
description: Consume the selected provider plans
spec_ref: specs/vision.md
done_when: Consumer plan reviewed
scope: bounded consumer
role_pair: architecture-pair
priority: 1
provider_dependencies:
  - provider_task: provider
    transition: architecture-to-code-plan
    outputs: [2, 0]
`

func TestLoadTaskInputProviderDependenciesReachCommandBoundary(t *testing.T) {
	file := filepath.Join(t.TempDir(), "task.yaml")
	if err := os.WriteFile(file, []byte(taskInputProviderYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	input, err := LoadTaskInputFromFile(file)
	if err != nil {
		t.Fatal(err)
	}
	want := []models.ProviderDependency{{
		ProviderTask: "provider", Transition: "architecture-to-code-plan", Outputs: []int{2, 0},
	}}
	called := false
	err = addTaskCommand("", "", input, func(got *ops.AddTaskInput) (*ops.AddTaskResult, error) {
		called = true
		if !reflect.DeepEqual(got.ProviderDependencies, want) {
			t.Fatalf("YAML declaration lost at command boundary: got %+v, want %+v", got.ProviderDependencies, want)
		}
		got.ProviderDependencies[0].Outputs[0] = 99
		if input.ProviderDependencies[0].Outputs[0] != 2 {
			t.Fatal("command mapping aliases the YAML input's output selection")
		}
		return &ops.AddTaskResult{TaskID: input.ID}, nil
	})
	if err != nil || !called {
		t.Fatalf("addTaskCommand called=%v err=%v", called, err)
	}
}

func TestLoadTaskInputProviderDependenciesRejectRawNonintegerIndexes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outputs string
		message string
	}{
		{"null", "[null]", "integer scalar"},
		{"fraction", "[0.5]", "integer scalar"},
		{"integral_float", "[1.0]", "integer scalar"},
		{"string", `["0"]`, "cannot unmarshal"},
		{"boolean", "[true]", "cannot unmarshal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "task.yaml")
			content := strings.Replace(taskInputProviderYAML, "[2, 0]", tc.outputs, 1)
			if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadTaskInputFromFile(file)
			if err == nil || !strings.Contains(err.Error(), "failed to parse task file") || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("LoadTaskInputFromFile error=%v, want raw %s rejection containing %q", err, tc.name, tc.message)
			}
		})
	}
}
