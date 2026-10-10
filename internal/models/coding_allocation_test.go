package models

import "testing"

func TestCodingAllocationAdmission(t *testing.T) {
	valid := func() Task {
		return Task{Type: TaskTypeArchitecture, Output: []OutputEntry{{
			CodingAllocation: true, ArchRef: "specs/arch.md#Scope One", PlanRef: "specs/arch.md#Unit One",
			Validation: []string{"go test ./internal/example"}, Decomposition: &DecompositionManifest{OwnedFiles: []string{"internal/example.go"}},
		}}}
	}
	for _, tc := range []struct {
		name   string
		change func(*Task)
	}{
		{"mixed markers", func(task *Task) { task.Output = append(task.Output, OutputEntry{}) }},
		{"multiple scopes", func(task *Task) {
			second := task.Output[0]
			second.ArchRef = "specs/arch.md#Scope Two"
			second.PlanRef = "specs/arch.md#Unit Two"
			task.Output = append(task.Output, second)
		}},
		{"RCA owner", func(task *Task) { task.RCARequired = true }},
		{"RCA allocation", func(task *Task) { required := true; task.Output[0].RCARequired = &required }},
		{"missing files", func(task *Task) { task.Output[0].Decomposition = nil }},
		{"missing commands", func(task *Task) { task.Output[0].Validation = nil }},
		{"wrong task type", func(task *Task) { task.Type = TaskTypePlanning }},
		{"deferred writer waits", func(task *Task) {
			task.DescendantDependencies = []DescendantDependency{{AtTransition: "code-plan-to-coding"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := valid()
			tc.change(&task)
			if err := ValidateCodingAllocationOutput(&task, task.Output); err == nil {
				t.Fatal("invalid direct allocation admitted")
			}
		})
	}
	task := valid()
	if err := ValidateCodingAllocationOutput(&task, task.Output); err != nil {
		t.Fatal(err)
	}
	task.Output[0].CodingAllocation = false
	task.Output[0].PlanRef = ""
	task.Output[0].Decomposition = nil
	if err := ValidateCodingAllocationOutput(&task, task.Output); err != nil {
		t.Fatalf("legacy output refused: %v", err)
	}
}
