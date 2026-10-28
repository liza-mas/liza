package models

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestEffectiveAttempt_ZeroReturnsOne(t *testing.T) {
	task := &Task{Attempt: 0}
	if got := task.EffectiveAttempt(); got != 1 {
		t.Errorf("EffectiveAttempt() = %d, want 1 for Attempt=0", got)
	}
}

func TestEffectiveAttempt_ReturnsActualValue(t *testing.T) {
	tests := []struct {
		attempt int
		want    int
	}{
		{1, 1},
		{2, 2},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("Attempt=%d", tt.attempt), func(t *testing.T) {
			task := &Task{Attempt: tt.attempt}
			if got := task.EffectiveAttempt(); got != tt.want {
				t.Errorf("EffectiveAttempt() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMigrateAttemptedField_ConvertsLegacyList(t *testing.T) {
	task := &Task{
		Extra: map[string]any{
			"attempted": []any{"agent-1"},
		},
	}
	changed := task.MigrateAttemptedField()

	if !changed {
		t.Error("MigrateAttemptedField() = false, want true")
	}
	if task.Attempt != 2 {
		t.Errorf("Attempt = %d, want 2", task.Attempt)
	}
	if _, exists := task.Extra["attempted"]; exists {
		t.Error("Extra[\"attempted\"] should be absent after migration")
	}
}

func TestMigrateAttemptedField_EmptyList(t *testing.T) {
	task := &Task{
		Extra: map[string]any{
			"attempted": []any{},
		},
	}
	changed := task.MigrateAttemptedField()

	if !changed {
		t.Error("MigrateAttemptedField() = false, want true (key deleted)")
	}
	if task.Attempt != 0 {
		t.Errorf("Attempt = %d, want 0 (unchanged)", task.Attempt)
	}
	if _, exists := task.Extra["attempted"]; exists {
		t.Error("Extra[\"attempted\"] should be deleted even for empty list")
	}
}

func TestMigrateAttemptedField_AlreadyMigrated(t *testing.T) {
	task := &Task{
		Attempt: 2,
		Extra: map[string]any{
			"attempted": []any{"agent-1"},
		},
	}
	changed := task.MigrateAttemptedField()

	if changed {
		t.Error("MigrateAttemptedField() = true, want false for already-migrated task")
	}
	if task.Attempt != 2 {
		t.Errorf("Attempt = %d, want 2 (unchanged)", task.Attempt)
	}
}

func TestMigrateAttemptedField_NoLegacyField(t *testing.T) {
	task := &Task{
		Extra: map[string]any{"other": "value"},
	}
	changed := task.MigrateAttemptedField()

	if changed {
		t.Error("MigrateAttemptedField() = true, want false when no legacy field")
	}
	if task.Attempt != 0 {
		t.Errorf("Attempt = %d, want 0 (unchanged)", task.Attempt)
	}
}

func TestMigrateAttemptedField_CapsAtTwo(t *testing.T) {
	task := &Task{
		Extra: map[string]any{
			"attempted": []any{"a", "b", "c"},
		},
	}
	changed := task.MigrateAttemptedField()

	if !changed {
		t.Error("MigrateAttemptedField() = false, want true")
	}
	if task.Attempt != 2 {
		t.Errorf("Attempt = %d, want 2 (capped)", task.Attempt)
	}
	if _, exists := task.Extra["attempted"]; exists {
		t.Error("Extra[\"attempted\"] should be absent after migration")
	}
}

func TestMigrateAttemptedField_WrongType(t *testing.T) {
	task := &Task{
		Extra: map[string]any{
			"attempted": "not-a-list",
		},
	}
	changed := task.MigrateAttemptedField()

	if !changed {
		t.Error("MigrateAttemptedField() = false, want true (key deleted)")
	}
	if task.Attempt != 0 {
		t.Errorf("Attempt = %d, want 0 (unchanged)", task.Attempt)
	}
	if _, exists := task.Extra["attempted"]; exists {
		t.Error("Extra[\"attempted\"] should be deleted even for wrong type")
	}
}

func TestEffectiveAttempt_LegacyFallback(t *testing.T) {
	task := &Task{
		Attempt: 0,
		Extra:   map[string]any{"attempted": []any{"agent-1"}},
	}
	if got := task.EffectiveAttempt(); got != 2 {
		t.Errorf("EffectiveAttempt() = %d, want 2", got)
	}
}

func TestEffectiveAttempt_LegacyFallbackEmptyList(t *testing.T) {
	task := &Task{
		Attempt: 0,
		Extra:   map[string]any{"attempted": []any{}},
	}
	if got := task.EffectiveAttempt(); got != 1 {
		t.Errorf("EffectiveAttempt() = %d, want 1 (empty list falls through to default)", got)
	}
}

func TestApprovalHelpers(t *testing.T) {
	t.Run("ApprovalCount", func(t *testing.T) {
		t.Run("empty list", func(t *testing.T) {
			task := &Task{}
			if got := task.ApprovalCount(); got != 0 {
				t.Errorf("ApprovalCount() = %d, want 0", got)
			}
		})

		t.Run("nil list", func(t *testing.T) {
			task := &Task{Approvals: nil}
			if got := task.ApprovalCount(); got != 0 {
				t.Errorf("ApprovalCount() = %d, want 0", got)
			}
		})

		t.Run("single approval", func(t *testing.T) {
			task := &Task{
				Approvals: []Approval{
					{Agent: "reviewer-1", Provider: "claude", Timestamp: time.Now()},
				},
			}
			if got := task.ApprovalCount(); got != 1 {
				t.Errorf("ApprovalCount() = %d, want 1", got)
			}
		})

		t.Run("multiple approvals", func(t *testing.T) {
			task := &Task{
				Approvals: []Approval{
					{Agent: "reviewer-1", Provider: "claude", Timestamp: time.Now()},
					{Agent: "reviewer-2", Provider: "codex", Timestamp: time.Now()},
				},
			}
			if got := task.ApprovalCount(); got != 2 {
				t.Errorf("ApprovalCount() = %d, want 2", got)
			}
		})
	})

	t.Run("HasProviderDiversity", func(t *testing.T) {
		t.Run("empty list", func(t *testing.T) {
			task := &Task{}
			if task.HasProviderDiversity() {
				t.Error("HasProviderDiversity() = true, want false for empty list")
			}
		})

		t.Run("single approval", func(t *testing.T) {
			task := &Task{
				Approvals: []Approval{
					{Agent: "reviewer-1", Provider: "claude", Timestamp: time.Now()},
				},
			}
			if task.HasProviderDiversity() {
				t.Error("HasProviderDiversity() = true, want false for single approval")
			}
		})

		t.Run("same provider", func(t *testing.T) {
			task := &Task{
				Approvals: []Approval{
					{Agent: "reviewer-1", Provider: "claude", Timestamp: time.Now()},
					{Agent: "reviewer-2", Provider: "claude", Timestamp: time.Now()},
				},
			}
			if task.HasProviderDiversity() {
				t.Error("HasProviderDiversity() = true, want false for same provider")
			}
		})

		t.Run("diverse providers", func(t *testing.T) {
			task := &Task{
				Approvals: []Approval{
					{Agent: "reviewer-1", Provider: "claude", Timestamp: time.Now()},
					{Agent: "reviewer-2", Provider: "codex", Timestamp: time.Now()},
				},
			}
			if !task.HasProviderDiversity() {
				t.Error("HasProviderDiversity() = false, want true for diverse providers")
			}
		})

		t.Run("three approvals mixed providers", func(t *testing.T) {
			task := &Task{
				Approvals: []Approval{
					{Agent: "reviewer-1", Provider: "claude", Timestamp: time.Now()},
					{Agent: "reviewer-2", Provider: "claude", Timestamp: time.Now()},
					{Agent: "reviewer-3", Provider: "codex", Timestamp: time.Now()},
				},
			}
			if !task.HasProviderDiversity() {
				t.Error("HasProviderDiversity() = false, want true when at least 2 distinct providers exist")
			}
		})
	})

	t.Run("ClearApprovals", func(t *testing.T) {
		t.Run("clears non-empty list", func(t *testing.T) {
			task := &Task{
				Approvals: []Approval{
					{Agent: "reviewer-1", Provider: "claude", Timestamp: time.Now()},
					{Agent: "reviewer-2", Provider: "codex", Timestamp: time.Now()},
				},
			}
			task.ClearApprovals()
			if len(task.Approvals) != 0 {
				t.Errorf("ClearApprovals() left %d approvals, want 0", len(task.Approvals))
			}
		})

		t.Run("clears empty list", func(t *testing.T) {
			task := &Task{}
			task.ClearApprovals()
			if task.Approvals != nil {
				t.Error("ClearApprovals() on empty task should leave nil")
			}
		})

		t.Run("clears nil list", func(t *testing.T) {
			task := &Task{Approvals: nil}
			task.ClearApprovals()
			if task.Approvals != nil {
				t.Error("ClearApprovals() on nil should leave nil")
			}
		})
	})

	t.Run("LastApprover", func(t *testing.T) {
		t.Run("empty list", func(t *testing.T) {
			task := &Task{}
			if got := task.LastApprover(); got != "" {
				t.Errorf("LastApprover() = %q, want empty string", got)
			}
		})

		t.Run("single approval", func(t *testing.T) {
			task := &Task{
				Approvals: []Approval{
					{Agent: "reviewer-1", Provider: "claude", Timestamp: time.Now()},
				},
			}
			if got := task.LastApprover(); got != "reviewer-1" {
				t.Errorf("LastApprover() = %q, want %q", got, "reviewer-1")
			}
		})

		t.Run("multiple approvals returns last", func(t *testing.T) {
			task := &Task{
				Approvals: []Approval{
					{Agent: "reviewer-1", Provider: "claude", Timestamp: time.Now()},
					{Agent: "reviewer-2", Provider: "codex", Timestamp: time.Now()},
				},
			}
			if got := task.LastApprover(); got != "reviewer-2" {
				t.Errorf("LastApprover() = %q, want %q", got, "reviewer-2")
			}
		})
	})
}

func TestTask_KindYAMLRoundTrip(t *testing.T) {
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)

	t.Run("populated kind round-trips", func(t *testing.T) {
		original := Task{
			ID:          "task-1",
			Description: "do the thing",
			Status:      "DRAFT_CODE",
			Priority:    1,
			SpecRef:     "specs/x.md",
			Kind:        "bootstrap-precommit",
			DoneWhen:    "tests pass",
			Scope:       "internal/",
			Created:     now,
			History:     []TaskHistoryEntry{},
		}
		data, err := yaml.Marshal(&original)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded Task
		if err := yaml.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if decoded.Kind != "bootstrap-precommit" {
			t.Errorf("decoded Kind = %q, want %q", decoded.Kind, "bootstrap-precommit")
		}
	})

	t.Run("empty kind omitted from YAML", func(t *testing.T) {
		task := Task{
			ID:          "task-1",
			Description: "do the thing",
			Status:      "DRAFT_CODE",
			Priority:    1,
			SpecRef:     "specs/x.md",
			Kind:        "",
			DoneWhen:    "tests pass",
			Scope:       "internal/",
			Created:     now,
			History:     []TaskHistoryEntry{},
		}
		data, err := yaml.Marshal(&task)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if strings.Contains(string(data), "kind:") {
			t.Errorf("empty Kind should be omitted from YAML, got:\n%s", string(data))
		}
	})
}

func TestTask_DecompositionYAMLRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)

	t.Run("populated decomposition round-trips", func(t *testing.T) {
		original := Task{
			ID:            "task-1",
			Description:   "do the thing",
			Status:        "DRAFT_CODE",
			Priority:      1,
			SpecRef:       "specs/x.md",
			Decomposition: testDecompositionManifest(),
			DoneWhen:      "tests pass",
			Scope:         "internal/",
			Created:       now,
			History:       []TaskHistoryEntry{},
		}
		data, err := yaml.Marshal(&original)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		assertYAMLDecompositionKeys(t, string(data))
		var decoded Task
		if err := yaml.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if !reflect.DeepEqual(original.Decomposition, decoded.Decomposition) {
			t.Errorf("decoded Decomposition = %#v, want %#v", decoded.Decomposition, original.Decomposition)
		}
	})

	t.Run("omitted decomposition remains absent", func(t *testing.T) {
		task := Task{
			ID:          "task-1",
			Description: "do the thing",
			Status:      "DRAFT_CODE",
			Priority:    1,
			SpecRef:     "specs/x.md",
			DoneWhen:    "tests pass",
			Scope:       "internal/",
			Created:     now,
			History:     []TaskHistoryEntry{},
		}
		data, err := yaml.Marshal(&task)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if strings.Contains(string(data), "decomposition:") {
			t.Errorf("nil Decomposition should be omitted from YAML, got:\n%s", string(data))
		}
		var decoded Task
		if err := yaml.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if decoded.Decomposition != nil {
			t.Errorf("decoded Decomposition = %#v, want nil", decoded.Decomposition)
		}
	})
}

func TestTask_DecompositionJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)

	t.Run("populated decomposition round-trips", func(t *testing.T) {
		original := Task{
			ID:            "task-1",
			Description:   "do the thing",
			Status:        "DRAFT_CODE",
			Priority:      1,
			SpecRef:       "specs/x.md",
			Decomposition: testDecompositionManifest(),
			DoneWhen:      "tests pass",
			Scope:         "internal/",
			Created:       now,
			History:       []TaskHistoryEntry{},
		}
		data, err := json.Marshal(&original)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		assertJSONDecompositionKeys(t, string(data))
		var decoded Task
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if !reflect.DeepEqual(original.Decomposition, decoded.Decomposition) {
			t.Errorf("decoded Decomposition = %#v, want %#v", decoded.Decomposition, original.Decomposition)
		}
	})

	t.Run("omitted decomposition remains absent", func(t *testing.T) {
		task := Task{
			ID:          "task-1",
			Description: "do the thing",
			Status:      "DRAFT_CODE",
			Priority:    1,
			SpecRef:     "specs/x.md",
			DoneWhen:    "tests pass",
			Scope:       "internal/",
			Created:     now,
			History:     []TaskHistoryEntry{},
		}
		data, err := json.Marshal(&task)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if strings.Contains(string(data), `"decomposition"`) {
			t.Errorf("nil Decomposition should be omitted from JSON, got: %s", string(data))
		}
		var decoded Task
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if decoded.Decomposition != nil {
			t.Errorf("decoded Decomposition = %#v, want nil", decoded.Decomposition)
		}
	})
}

func TestTask_ValidationRoundTripAndOmitEmpty(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	original := Task{
		ID:            "task-1",
		Description:   "do the thing",
		Status:        "DRAFT_CODE",
		Priority:      1,
		SpecRef:       "specs/x.md",
		DoneWhen:      "tests pass",
		Validation:    []string{"make test", "pre-commit run --files docs/USAGE.md"},
		DestructiveDB: true,
		Scope:         "internal/",
		Created:       now,
		History:       []TaskHistoryEntry{},
	}

	yamlData, err := yaml.Marshal(&original)
	if err != nil {
		t.Fatalf("YAML Marshal: %v", err)
	}
	if !strings.Contains(string(yamlData), "validation:") {
		t.Fatalf("YAML missing validation key:\n%s", string(yamlData))
	}
	if !strings.Contains(string(yamlData), "destructive_db: true") {
		t.Fatalf("YAML missing destructive_db key:\n%s", string(yamlData))
	}
	var yamlDecoded Task
	if err := yaml.Unmarshal(yamlData, &yamlDecoded); err != nil {
		t.Fatalf("YAML Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(original.Validation, yamlDecoded.Validation) {
		t.Errorf("YAML Validation = %v, want %v", yamlDecoded.Validation, original.Validation)
	}
	if !yamlDecoded.DestructiveDB {
		t.Errorf("YAML DestructiveDB = false, want true")
	}

	jsonData, err := json.Marshal(&original)
	if err != nil {
		t.Fatalf("JSON Marshal: %v", err)
	}
	if !strings.Contains(string(jsonData), `"validation"`) || strings.Contains(string(jsonData), `"Validation"`) {
		t.Fatalf("JSON validation key mismatch: %s", string(jsonData))
	}
	if !strings.Contains(string(jsonData), `"destructive_db":true`) || strings.Contains(string(jsonData), `"DestructiveDB"`) {
		t.Fatalf("JSON destructive_db key mismatch: %s", string(jsonData))
	}
	var jsonDecoded Task
	if err := json.Unmarshal(jsonData, &jsonDecoded); err != nil {
		t.Fatalf("JSON Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(original.Validation, jsonDecoded.Validation) {
		t.Errorf("JSON Validation = %v, want %v", jsonDecoded.Validation, original.Validation)
	}
	if !jsonDecoded.DestructiveDB {
		t.Errorf("JSON DestructiveDB = false, want true")
	}

	empty := original
	empty.Validation = nil
	empty.DestructiveDB = false
	emptyYAML, err := yaml.Marshal(&empty)
	if err != nil {
		t.Fatalf("empty YAML Marshal: %v", err)
	}
	if strings.Contains(string(emptyYAML), "validation:") {
		t.Errorf("empty Validation should be omitted from YAML, got:\n%s", string(emptyYAML))
	}
	if strings.Contains(string(emptyYAML), "destructive_db:") {
		t.Errorf("false DestructiveDB should be omitted from YAML, got:\n%s", string(emptyYAML))
	}
	emptyJSON, err := json.Marshal(&empty)
	if err != nil {
		t.Fatalf("empty JSON Marshal: %v", err)
	}
	if strings.Contains(string(emptyJSON), `"validation"`) || strings.Contains(string(emptyJSON), `"Validation"`) {
		t.Errorf("empty Validation should be omitted from JSON, got: %s", string(emptyJSON))
	}
	if strings.Contains(string(emptyJSON), `"destructive_db"`) || strings.Contains(string(emptyJSON), `"DestructiveDB"`) {
		t.Errorf("false DestructiveDB should be omitted from JSON, got: %s", string(emptyJSON))
	}
}

func assertYAMLDecompositionKeys(t *testing.T, data string) {
	t.Helper()
	for _, key := range []string{
		"decomposition:",
		"owned_files:",
		"owned_modules:",
		"read_only_depends_on:",
		"read_only_task_depends_on:",
		"interfaces_owned:",
		"interfaces_consumed:",
		"coverage_notes:",
	} {
		if !strings.Contains(data, key) {
			t.Errorf("YAML output missing %q:\n%s", key, data)
		}
	}
}

func assertJSONDecompositionKeys(t *testing.T, data string) {
	t.Helper()
	for _, key := range []string{
		`"decomposition"`,
		`"owned_files"`,
		`"owned_modules"`,
		`"read_only_depends_on"`,
		`"read_only_task_depends_on"`,
		`"interfaces_owned"`,
		`"interfaces_consumed"`,
		`"coverage_notes"`,
	} {
		if !strings.Contains(data, key) {
			t.Errorf("JSON output missing %q: %s", key, data)
		}
	}
}

func testDecompositionManifest() *DecompositionManifest {
	return &DecompositionManifest{
		OwnedFiles:            []string{"internal/auth/middleware.go"},
		OwnedModules:          []string{"internal/auth"},
		ReadOnlyDependsOn:     []int{0, 2},
		ReadOnlyTaskDependsOn: []string{"architecture-auth"},
		InterfacesOwned:       []string{"auth.Middleware"},
		InterfacesConsumed:    []string{"sessions.Store"},
		CoverageNotes:         "Auth middleware boundary is complete.",
	}
}

func TestTask_KindBackwardCompat(t *testing.T) {
	// Represents a Task persisted before the Kind field existed — no `kind:` key.
	legacy := `id: task-legacy
description: legacy task
status: DRAFT_CODE
priority: 1
spec_ref: specs/x.md
done_when: tests pass
scope: internal/
created: 2026-04-17T12:00:00Z
history: []
`
	var task Task
	if err := yaml.Unmarshal([]byte(legacy), &task); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if task.Kind != "" {
		t.Errorf("legacy Task without kind: should decode to Kind == \"\", got %q", task.Kind)
	}
}

// baseRejectionRCARequest is the canonical caller payload the fingerprint tests
// vary: two contributions, a mixed-cause one, and bounded evidence.
