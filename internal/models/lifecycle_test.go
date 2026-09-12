package models

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestTaskTransitionIDTracksOwnershipBoundaries(t *testing.T) {
	t.Parallel()
	owner := "coder-1"
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	original := Task{ID: "task-1", Created: now, Status: TaskStatusImplementing, AssignedTo: &owner, History: []TaskHistoryEntry{{Time: now, Event: "claimed"}}}
	token := TaskTransitionID(&original)
	tests := []struct {
		name    string
		mutate  func(*Task)
		changes bool
	}{
		{"lease renewal", func(task *Task) {
			next := now.Add(time.Hour)
			task.LeaseExpires = &next
			task.ReviewLeaseExpires = &next
		}, false},
		{"initialize metadata", func(task *Task) { task.Lifecycle = &TaskLifecycle{} }, false},
		{"prepare effects", func(task *Task) { task.Lifecycle = &TaskLifecycle{Preparation: &LifecyclePreparation{Boundary: token}} }, false},
		{"recreated task", func(task *Task) { task.Created = now.Add(time.Second) }, true},
		{"claim release", func(task *Task) { task.AssignedTo = nil }, true},
		{"review claim", func(task *Task) { reviewer := "reviewer-1"; task.ReviewingBy = &reviewer }, true},
		{"status", func(task *Task) { task.Status = TaskStatusReadyForReview }, true},
		{"same event appended", func(task *Task) { task.History = append(task.History, task.History[0]) }, true},
		{"same owner reacquired", func(task *Task) { AdvanceLifecycle(task) }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := original
			task.History = append([]TaskHistoryEntry(nil), original.History...)
			tt.mutate(&task)
			if changed := TaskTransitionID(&task) != token; changed != tt.changes {
				t.Fatalf("transition changed = %v, want %v", changed, tt.changes)
			}
		})
	}
}

func TestAdvanceLifecycleRetiresOnlyPreparation(t *testing.T) {
	t.Parallel()
	receipts := []LifecycleReceipt{{Sequence: 1, TransitionID: "completed-boundary"}}
	task := Task{Lifecycle: &TaskLifecycle{Revision: 2, CompletionSequence: 1, Receipts: receipts, Preparation: &LifecyclePreparation{Boundary: "abandoned-boundary"}}}
	AdvanceLifecycle(&task)
	if task.Lifecycle.Revision != 3 || task.Lifecycle.CompletionSequence != 1 || task.Lifecycle.Preparation != nil || !reflect.DeepEqual(task.Lifecycle.Receipts, receipts) {
		t.Fatalf("retirement changed completed evidence: %#v", task.Lifecycle)
	}
}

func TestLifecyclePersistenceKeepsGenerationOutOfJSON(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("b", 64)
	task := Task{Lifecycle: &TaskLifecycle{Preparation: &LifecyclePreparation{LifecycleIdentity: LifecycleIdentity{GenerationDigest: digest}}}}
	data, err := yaml.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	var restored Task
	if err := yaml.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Lifecycle.Preparation.GenerationDigest != digest {
		t.Fatal("persistence lost authority identity")
	}
	response, err := json.Marshal(restored.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(response), digest) || strings.Contains(string(response), "generation_digest") {
		t.Fatal("JSON exposed authority fingerprint")
	}
}

func TestTaskTransitionIDTracksValidationPrerequisiteContract(t *testing.T) {
	t.Parallel()
	original := Task{
		ID: "task-1", Status: TaskStatusImplementing, Validation: []string{"canonical check"},
		ValidationPrerequisites: []ValidationPrerequisite{{
			Command: "canonical check", Env: []string{"CHECK_ENDPOINT"},
			Executables: []string{"checker"}, Probes: [][]string{{"checker", "--ready"}},
		}},
	}
	token := TaskTransitionID(&original)
	for _, test := range []struct {
		name   string
		change func(*Task)
	}{
		{"canonical command", func(task *Task) {
			task.Validation[0] = "new canonical check"
			task.ValidationPrerequisites[0].Command = task.Validation[0]
		}},
		{"environment declaration", func(task *Task) { task.ValidationPrerequisites[0].Env[0] = "NEW_CHECK_ENDPOINT" }},
		{"executable declaration", func(task *Task) { task.ValidationPrerequisites[0].Executables[0] = "new-checker" }},
		{"probe argument", func(task *Task) { task.ValidationPrerequisites[0].Probes[0][1] = "--new-ready" }},
		{"contract removed", func(task *Task) { task.ValidationPrerequisites = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			task := original
			task.Validation = append([]string(nil), original.Validation...)
			task.ValidationPrerequisites = CloneValidationPrerequisites(original.ValidationPrerequisites)
			test.change(&task)
			if TaskTransitionID(&task) == token {
				t.Fatal("changed validation contract retained the old transition")
			}
			if TaskTransitionID(&original) != token {
				t.Fatal("contract clone changed the original task")
			}
		})
	}
	legacy := original
	legacy.ValidationPrerequisites = nil
	legacyToken := TaskTransitionID(&legacy)
	legacy.ValidationPrerequisites = []ValidationPrerequisite{}
	if TaskTransitionID(&legacy) != legacyToken || legacyToken == token {
		t.Fatal("empty declarations changed legacy identity or adding a contract failed to invalidate it")
	}
}

func TestTaskPersistsLifecycleAndValidationPrerequisitesTogether(t *testing.T) {
	t.Parallel()
	checks := []ValidationPrerequisite{{Command: "check", Probes: [][]string{{"checker", "--ready"}}}}
	original := Task{
		Validation: []string{"check"}, ValidationPrerequisites: CloneValidationPrerequisites(checks),
		Output:    []OutputEntry{{Validation: []string{"check"}, ValidationPrerequisites: CloneValidationPrerequisites(checks)}},
		Lifecycle: &TaskLifecycle{Revision: 3, Preparation: &LifecyclePreparation{Boundary: "pending-boundary"}},
	}
	for _, format := range []struct {
		name      string
		marshal   func(any) ([]byte, error)
		unmarshal func([]byte, any) error
	}{
		{"yaml", yaml.Marshal, yaml.Unmarshal},
		{"json", json.Marshal, json.Unmarshal},
	} {
		t.Run(format.name, func(t *testing.T) {
			data, err := format.marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var restored Task
			if err := format.unmarshal(data, &restored); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(restored.Lifecycle, original.Lifecycle) || !reflect.DeepEqual(restored.ValidationPrerequisites, checks) || !reflect.DeepEqual(restored.Output, original.Output) {
				t.Fatal("round trip dropped lifecycle or prerequisite metadata")
			}
		})
	}
}
