package models

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOutputEntry_JSONUnmarshal(t *testing.T) {
	// Wire format documented in doer_tools.tmpl and set-task-output CLI help.
	input := `[
		{
			"desc": "Implement auth middleware",
			"done_when": "GET /protected returns 401 without token",
			"scope": "internal/auth",
			"spec_ref": "specs/auth.md",
			"plan_ref": "specs/plans/plan-1.md",
			"arch_ref": "specs/arch-plan/arch-1.md",
			"validation": ["make -C services/auth test", "pre-commit run --files services/auth/README.md"],
			"destructive_db": true,
			"depends_on": ["0", "2"],
			"task_depends_on": ["github-175-runtime-capture-artifact-coverage"]
		}
	]`

	var entries []OutputEntry
	if err := json.Unmarshal([]byte(input), &entries); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Desc != "Implement auth middleware" {
		t.Errorf("Desc = %q", e.Desc)
	}
	if e.DoneWhen != "GET /protected returns 401 without token" {
		t.Errorf("DoneWhen = %q", e.DoneWhen)
	}
	if e.Scope != "internal/auth" {
		t.Errorf("Scope = %q", e.Scope)
	}
	if e.SpecRef != "specs/auth.md" {
		t.Errorf("SpecRef = %q", e.SpecRef)
	}
	if e.PlanRef != "specs/plans/plan-1.md" {
		t.Errorf("PlanRef = %q", e.PlanRef)
	}
	if e.ArchRef != "specs/arch-plan/arch-1.md" {
		t.Errorf("ArchRef = %q", e.ArchRef)
	}
	if len(e.Validation) != 2 || e.Validation[0] != "make -C services/auth test" || e.Validation[1] != "pre-commit run --files services/auth/README.md" {
		t.Errorf("Validation = %v", e.Validation)
	}
	if !e.DestructiveDB {
		t.Errorf("DestructiveDB = false, want true")
	}
	if len(e.DependsOn) != 2 || e.DependsOn[0] != "0" || e.DependsOn[1] != "2" {
		t.Errorf("DependsOn = %v", e.DependsOn)
	}
	if len(e.TaskDependsOn) != 1 || e.TaskDependsOn[0] != "github-175-runtime-capture-artifact-coverage" {
		t.Errorf("TaskDependsOn = %v", e.TaskDependsOn)
	}
}

func TestOutputEntry_KindYAMLRoundTrip(t *testing.T) {
	t.Run("populated kind round-trips", func(t *testing.T) {
		original := OutputEntry{
			Desc:     "x",
			DoneWhen: "y",
			Scope:    "z",
			SpecRef:  "s",
			Kind:     "bootstrap-precommit",
		}
		data, err := yaml.Marshal(&original)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded OutputEntry
		if err := yaml.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if !reflect.DeepEqual(original, decoded) {
			t.Errorf("round-trip mismatch:\noriginal = %#v\ndecoded  = %#v", original, decoded)
		}
	})

	t.Run("empty kind omitted from YAML", func(t *testing.T) {
		entry := OutputEntry{
			Desc:     "x",
			DoneWhen: "y",
			Scope:    "z",
			SpecRef:  "s",
			Kind:     "",
		}
		data, err := yaml.Marshal(&entry)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if strings.Contains(string(data), "kind:") {
			t.Errorf("empty Kind should be omitted from YAML, got:\n%s", string(data))
		}
	})
}

func TestOutputEntry_KindJSONRoundTrip(t *testing.T) {
	t.Run("populated kind round-trips", func(t *testing.T) {
		original := OutputEntry{
			Desc:     "x",
			DoneWhen: "y",
			Scope:    "z",
			SpecRef:  "s",
			Kind:     "bootstrap-precommit",
		}
		data, err := json.Marshal(&original)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded OutputEntry
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if !reflect.DeepEqual(original, decoded) {
			t.Errorf("round-trip mismatch:\noriginal = %#v\ndecoded  = %#v", original, decoded)
		}
	})

	t.Run("empty kind omitted from JSON", func(t *testing.T) {
		entry := OutputEntry{
			Desc:     "x",
			DoneWhen: "y",
			Scope:    "z",
			SpecRef:  "s",
			Kind:     "",
		}
		data, err := json.Marshal(&entry)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if strings.Contains(string(data), `"kind"`) {
			t.Errorf("empty Kind should be omitted from JSON, got: %s", string(data))
		}
	})
}

func TestOutputEntry_ValidationRoundTripAndOmitEmpty(t *testing.T) {
	rcaRequired := true
	original := OutputEntry{
		Desc:          "x",
		DoneWhen:      "y",
		Scope:         "z",
		SpecRef:       "s",
		Validation:    []string{"make test", "pre-commit run --files docs/USAGE.md"},
		DestructiveDB: true,
		RCARequired:   &rcaRequired,
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
	if !strings.Contains(string(yamlData), "rca_required: true") {
		t.Fatalf("YAML missing rca_required key:\n%s", string(yamlData))
	}
	var yamlDecoded OutputEntry
	if err := yaml.Unmarshal(yamlData, &yamlDecoded); err != nil {
		t.Fatalf("YAML Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(original.Validation, yamlDecoded.Validation) {
		t.Errorf("YAML Validation = %v, want %v", yamlDecoded.Validation, original.Validation)
	}
	if !yamlDecoded.DestructiveDB {
		t.Errorf("YAML DestructiveDB = false, want true")
	}
	if yamlDecoded.RCARequired == nil || !*yamlDecoded.RCARequired {
		t.Errorf("YAML RCARequired = %v, want pointer to true", yamlDecoded.RCARequired)
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
	if !strings.Contains(string(jsonData), `"rca_required":true`) || strings.Contains(string(jsonData), `"RCARequired"`) {
		t.Fatalf("JSON rca_required key mismatch: %s", string(jsonData))
	}
	var jsonDecoded OutputEntry
	if err := json.Unmarshal(jsonData, &jsonDecoded); err != nil {
		t.Fatalf("JSON Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(original.Validation, jsonDecoded.Validation) {
		t.Errorf("JSON Validation = %v, want %v", jsonDecoded.Validation, original.Validation)
	}
	if !jsonDecoded.DestructiveDB {
		t.Errorf("JSON DestructiveDB = false, want true")
	}
	if jsonDecoded.RCARequired == nil || !*jsonDecoded.RCARequired {
		t.Errorf("JSON RCARequired = %v, want pointer to true", jsonDecoded.RCARequired)
	}

	empty := OutputEntry{Desc: "x", DoneWhen: "y", Scope: "z", SpecRef: "s"}
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
	if strings.Contains(string(emptyYAML), "rca_required:") {
		t.Errorf("nil RCARequired should be omitted from YAML, got:\n%s", string(emptyYAML))
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
	if strings.Contains(string(emptyJSON), `"rca_required"`) || strings.Contains(string(emptyJSON), `"RCARequired"`) {
		t.Errorf("nil RCARequired should be omitted from JSON, got: %s", string(emptyJSON))
	}

	rcaNotRequired := false
	explicitFalse := OutputEntry{Desc: "x", DoneWhen: "y", Scope: "z", SpecRef: "s", RCARequired: &rcaNotRequired}
	falseYAML, err := yaml.Marshal(&explicitFalse)
	if err != nil {
		t.Fatalf("false YAML Marshal: %v", err)
	}
	if !strings.Contains(string(falseYAML), "rca_required: false") {
		t.Errorf("explicit false RCARequired should be retained in YAML, got:\n%s", string(falseYAML))
	}
	falseJSON, err := json.Marshal(&explicitFalse)
	if err != nil {
		t.Fatalf("false JSON Marshal: %v", err)
	}
	if !strings.Contains(string(falseJSON), `"rca_required":false`) {
		t.Errorf("explicit false RCARequired should be retained in JSON, got: %s", string(falseJSON))
	}
}

func TestOutputEntry_DecompositionYAMLRoundTrip(t *testing.T) {
	t.Run("populated decomposition round-trips", func(t *testing.T) {
		original := OutputEntry{
			Desc:          "Implement auth middleware",
			DoneWhen:      "middleware tests pass",
			Scope:         "internal/auth",
			SpecRef:       "specs/auth.md",
			Decomposition: testDecompositionManifest(),
		}
		data, err := yaml.Marshal(&original)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		assertYAMLDecompositionKeys(t, string(data))
		var decoded OutputEntry
		if err := yaml.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if !reflect.DeepEqual(original, decoded) {
			t.Errorf("round-trip mismatch:\noriginal = %#v\ndecoded  = %#v", original, decoded)
		}
	})

	t.Run("omitted decomposition remains absent", func(t *testing.T) {
		entry := OutputEntry{
			Desc:     "Implement auth middleware",
			DoneWhen: "middleware tests pass",
			Scope:    "internal/auth",
			SpecRef:  "specs/auth.md",
		}
		data, err := yaml.Marshal(&entry)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if strings.Contains(string(data), "decomposition:") {
			t.Errorf("nil Decomposition should be omitted from YAML, got:\n%s", string(data))
		}
		var decoded OutputEntry
		if err := yaml.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if decoded.Decomposition != nil {
			t.Errorf("decoded Decomposition = %#v, want nil", decoded.Decomposition)
		}
	})
}

func TestOutputEntry_DecompositionJSONRoundTrip(t *testing.T) {
	t.Run("populated decomposition round-trips", func(t *testing.T) {
		original := OutputEntry{
			Desc:          "Implement auth middleware",
			DoneWhen:      "middleware tests pass",
			Scope:         "internal/auth",
			SpecRef:       "specs/auth.md",
			Decomposition: testDecompositionManifest(),
		}
		data, err := json.Marshal(&original)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		assertJSONDecompositionKeys(t, string(data))
		var decoded OutputEntry
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if !reflect.DeepEqual(original, decoded) {
			t.Errorf("round-trip mismatch:\noriginal = %#v\ndecoded  = %#v", original, decoded)
		}
	})

	t.Run("omitted decomposition remains absent", func(t *testing.T) {
		entry := OutputEntry{
			Desc:     "Implement auth middleware",
			DoneWhen: "middleware tests pass",
			Scope:    "internal/auth",
			SpecRef:  "specs/auth.md",
		}
		data, err := json.Marshal(&entry)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if strings.Contains(string(data), `"decomposition"`) {
			t.Errorf("nil Decomposition should be omitted from JSON, got: %s", string(data))
		}
		var decoded OutputEntry
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if decoded.Decomposition != nil {
			t.Errorf("decoded Decomposition = %#v, want nil", decoded.Decomposition)
		}
	})
}
