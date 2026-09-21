package models

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

func allLifecycleOutcomes() []string {
	return []string{
		LifecycleCompleted, LifecycleAlreadyCompleted, LifecycleAlreadyTransitioned,
		LifecycleStaleCaller, LifecycleStateChanged, LifecycleRetryable,
		LifecycleInvalidInput, LifecycleForbidden, LifecycleNoChange, LifecycleConflict,
	}
}

func TestLifecycleOutcomeJSONRendering(t *testing.T) {
	t.Parallel()
	wantChanged := map[string]bool{LifecycleCompleted: true, LifecycleNoChange: false, LifecycleAlreadyCompleted: false}
	for _, outcome := range allLifecycleOutcomes() {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			// GIVEN an outcome whose changed flag derives from its classification
			result := LifecycleOutcome{Operation: "op", TaskID: "t", Outcome: outcome, SafeAction: "stop", Effects: "none", Changed: LifecycleChanged(outcome)}
			data, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var rendered map[string]any
			if err := json.Unmarshal(data, &rendered); err != nil {
				t.Fatal(err)
			}
			// THEN changed is present exactly for COMPLETED, NO_CHANGE and ALREADY_COMPLETED
			value, present := rendered["changed"]
			want, expected := wantChanged[outcome]
			if present != expected {
				t.Fatalf("changed key presence = %v, want %v: %s", present, expected, data)
			}
			if expected && value != want {
				t.Fatalf("changed = %v, want %v", value, want)
			}
			if _, present := rendered["diagnostics"]; present {
				t.Fatalf("empty diagnostics rendered: %s", data)
			}
		})
	}
	// GIVEN an outcome with diagnostics
	result := LifecycleOutcome{Outcome: LifecycleInvalidInput, SafeAction: "correct_input", Effects: "none", Diagnostics: []FieldDiagnostic{
		{SchemaVersion: 2, Field: "/validation/0", Constraint: "non-empty", ValueClass: FieldValueClassMissing, SafeAction: FieldDiagnosticCorrectInput},
	}}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var rendered struct {
		Diagnostics []map[string]any `json:"diagnostics"`
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatal(err)
	}
	if len(rendered.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %s", data)
	}
	// THEN each entry renders exactly the five contract keys and no raw value
	wantKeys := map[string]any{"schema_version": float64(2), "field": "/validation/0", "constraint": "non-empty", "value_class": "missing", "safe_action": "correct_input"}
	if !reflect.DeepEqual(rendered.Diagnostics[0], wantKeys) {
		t.Fatalf("diagnostic keys = %v, want %v", rendered.Diagnostics[0], wantKeys)
	}
	for _, forbidden := range []string{"\"value\"", "raw_value"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("diagnostic exposed %s: %s", forbidden, data)
		}
	}
}

func TestLifecycleOutcomeVocabulary(t *testing.T) {
	t.Parallel()
	for _, outcome := range allLifecycleOutcomes() {
		if !IsLifecycleOutcome(outcome) {
			t.Fatalf("%s rejected", outcome)
		}
	}
	for _, invalid := range []string{"", "no_change", "conflict", "NOCHANGE"} {
		if IsLifecycleOutcome(invalid) {
			t.Fatalf("%q accepted", invalid)
		}
	}
	if LifecycleNoChange != "NO_CHANGE" || LifecycleConflict != "CONFLICT" {
		t.Fatal("outcome constants renamed")
	}
	for _, action := range []string{FieldDiagnosticCorrectInput, FieldDiagnosticRequery} {
		if !IsFieldDiagnosticSafeAction(action) {
			t.Fatalf("%s rejected", action)
		}
	}
	for _, invalid := range []string{"", "stop", "retry", "continue", "Requery"} {
		if IsFieldDiagnosticSafeAction(invalid) {
			t.Fatalf("%q accepted as diagnostic safe action", invalid)
		}
	}
	// THEN changed derives only from the three change-classified outcomes
	for _, outcome := range allLifecycleOutcomes() {
		changed := LifecycleChanged(outcome)
		switch outcome {
		case LifecycleCompleted:
			if changed == nil || !*changed {
				t.Fatalf("%s changed = %v", outcome, changed)
			}
		case LifecycleNoChange, LifecycleAlreadyCompleted:
			if changed == nil || *changed {
				t.Fatalf("%s changed = %v", outcome, changed)
			}
		default:
			if changed != nil {
				t.Fatalf("%s changed = %v, want nil", outcome, *changed)
			}
		}
	}
	if LifecycleChanged("") != nil {
		t.Fatal("unknown outcome derived changed")
	}
}

func TestLifecycleOutcomeNormalizeDiagnostics(t *testing.T) {
	t.Parallel()
	if NormalizeFieldDiagnostics(nil) != nil || NormalizeFieldDiagnostics([]FieldDiagnostic{}) != nil {
		t.Fatal("empty input did not normalize to nil")
	}
	// GIVEN one entry over the cap, a multibyte oversized field and unknown safe actions
	input := make([]FieldDiagnostic, LifecycleDiagnosticsMaxEntries+1)
	for i := range input {
		input[i] = FieldDiagnostic{SchemaVersion: 1, Field: "/f", Constraint: "c", ValueClass: FieldValueClassMalformed, SafeAction: "bogus"}
	}
	long := strings.Repeat("é", 150) // 300 bytes, 2 bytes per rune
	input[0].Field = long
	input[0].Constraint = long
	input[0].ValueClass = long
	input[1].SafeAction = ""
	input[2].SafeAction = FieldDiagnosticRequery
	snapshot := make([]FieldDiagnostic, len(input))
	copy(snapshot, input)

	got := NormalizeFieldDiagnostics(input)

	// THEN the result is capped, clipped on rune boundaries and normalized
	if len(got) != LifecycleDiagnosticsMaxEntries {
		t.Fatalf("len = %d, want %d", len(got), LifecycleDiagnosticsMaxEntries)
	}
	for _, clipped := range []string{got[0].Field, got[0].Constraint, got[0].ValueClass} {
		if len(clipped) > LifecycleDiagnosticMaxBytes || len(clipped) < LifecycleDiagnosticMaxBytes-3 || !utf8.ValidString(clipped) || !strings.HasPrefix(long, clipped) {
			t.Fatalf("clip = %d bytes valid=%v", len(clipped), utf8.ValidString(clipped))
		}
	}
	if got[0].SafeAction != FieldDiagnosticCorrectInput || got[1].SafeAction != FieldDiagnosticCorrectInput {
		t.Fatalf("unknown/empty safe_action not normalized: %q %q", got[0].SafeAction, got[1].SafeAction)
	}
	if got[2].SafeAction != FieldDiagnosticRequery {
		t.Fatalf("requery lost: %q", got[2].SafeAction)
	}
	if got[3].SchemaVersion != 1 || got[3].Field != "/f" || got[3].Constraint != "c" || got[3].ValueClass != FieldValueClassMalformed {
		t.Fatalf("in-bounds entry altered: %+v", got[3])
	}
	if !reflect.DeepEqual(input, snapshot) {
		t.Fatal("input slice mutated")
	}
	got[3].Field = "mutated"
	if input[3].Field != "/f" {
		t.Fatal("result aliases input")
	}
}
