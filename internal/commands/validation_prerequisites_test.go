package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

func TestTaskInputValidationPrerequisitesReachOps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.yaml")
	if err := os.WriteFile(path, []byte("id: protected\nvalidation: [check]\nvalidation_prerequisites:\n  - command: check\n    env: [URL]\n    probes: [[tool, --check]]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	input, err := LoadTaskInputFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []models.ValidationPrerequisite{{Command: "check", Env: []string{"URL"}, Probes: [][]string{{"tool", "--check"}}}}
	if !reflect.DeepEqual(input.ValidationPrerequisites, want) {
		t.Fatal("YAML loader lost contract")
	}
	called := false
	err = addTaskCommand("unused", "unused", input, func(persisted *ops.AddTaskInput) (*ops.AddTaskResult, error) {
		called = true
		if !reflect.DeepEqual(persisted.ValidationPrerequisites, want) {
			t.Fatal("command adapter lost contract")
		}
		persisted.ValidationPrerequisites[0].Probes[0][0] = "changed"
		return &ops.AddTaskResult{}, nil
	})
	if err != nil || !called {
		t.Fatalf("command delegation: called=%v err=%v", called, err)
	}
	if input.ValidationPrerequisites[0].Probes[0][0] != "tool" {
		t.Fatal("command adapter shares mutable probe argv")
	}
}
