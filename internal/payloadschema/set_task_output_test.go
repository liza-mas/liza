package payloadschema_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/payloadschema"
)

// validEntry is the smallest manifest entry the schema accepts. Each table case
// mutates one field, so a rejection names the rule under test and nothing else.
func validEntry() models.OutputEntry {
	return models.OutputEntry{
		Desc:     "Implement the parser",
		DoneWhen: "TestParser passes",
		Scope:    "internal/parser",
		SpecRef:  "specs/protocols/parser.md#Grammar",
	}
}

func withEntry(mutate func(*models.OutputEntry)) []models.OutputEntry {
	entry := validEntry()
	mutate(&entry)
	return []models.OutputEntry{entry}
}

const inheritInputsConstraint = "mode must be all, selected or none; only selected carries selections, each naming one upstream task and at least one distinct output index"

func TestSetTaskOutputSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		output     []models.OutputEntry
		field      string
		constraint string
		valueClass string
	}{
		{name: "desc is required", output: withEntry(func(e *models.OutputEntry) { e.Desc = "" }),
			field: "/output/0/desc", constraint: "is required: describe the child task", valueClass: models.FieldValueClassMissing},
		{name: "done_when is required", output: withEntry(func(e *models.OutputEntry) { e.DoneWhen = "" }),
			field: "/output/0/done_when", constraint: "is required: state the observable completion criteria", valueClass: models.FieldValueClassMissing},
		{name: "scope is required", output: withEntry(func(e *models.OutputEntry) { e.Scope = "" }),
			field: "/output/0/scope", constraint: "is required: bound what the child may touch", valueClass: models.FieldValueClassMissing},
		{name: "spec_ref is required", output: withEntry(func(e *models.OutputEntry) { e.SpecRef = "" }),
			field: "/output/0/spec_ref", constraint: "is required: name the spec section the child implements", valueClass: models.FieldValueClassMissing},
		{name: "kind must be registered", output: withEntry(func(e *models.OutputEntry) { e.Kind = "not-a-kind" }),
			field: "/output/0/kind", constraint: "must be empty or a registered task kind", valueClass: models.FieldValueClassUnknownEnum},
		{name: "validation command must not be empty", output: withEntry(func(e *models.OutputEntry) { e.Validation = []string{""} }),
			field: "/output/0/validation", constraint: "validation[0] must not be empty", valueClass: models.FieldValueClassMalformed},
		{name: "destructive validation needs the break-glass marker", output: withEntry(func(e *models.OutputEntry) {
			e.Validation = []string{"psql -c 'drop table t'"}
			e.DestructiveDB = true
		}), field: "/output/0/validation",
			constraint: "validation[0] destructive_db requires command to start with " + models.CurrentDestructiveDBAllowMarker() + " or env " + models.CurrentDestructiveDBAllowMarker(),
			valueClass: models.FieldValueClassMalformed},
		{name: "validation_prerequisites need one declaration per command", output: withEntry(func(e *models.OutputEntry) {
			e.Validation = []string{"go test ./..."}
			e.ValidationPrerequisites = []models.ValidationPrerequisite{
				{Command: "go test ./...", Executables: []string{"go"}},
				{Command: "go vet ./...", Executables: []string{"go"}},
			}
		}), field: "/output/0/validation_prerequisites",
			constraint: "validation_prerequisites requires one declaration per validation command (maximum 64)",
			valueClass: models.FieldValueClassMalformed},
		{name: "depends_on must reference a sibling index", output: withEntry(func(e *models.OutputEntry) { e.DependsOn = []string{"3"} }),
			field: "/output/0/depends_on", constraint: "must hold sibling output indexes other than this entry's own", valueClass: models.FieldValueClassOutOfRange},
		{name: "depends_on must not self-reference", output: withEntry(func(e *models.OutputEntry) { e.DependsOn = []string{"0"} }),
			field: "/output/0/depends_on", constraint: "must hold sibling output indexes other than this entry's own", valueClass: models.FieldValueClassOutOfRange},
		{name: "inherit_inputs mode must be known", output: withEntry(func(e *models.OutputEntry) {
			e.InheritInputs = &models.InheritInputs{Mode: "some"}
		}), field: "/output/0/inherit_inputs", constraint: inheritInputsConstraint, valueClass: models.FieldValueClassMalformed},
		{name: "inherit_inputs selected needs a selection", output: withEntry(func(e *models.OutputEntry) {
			e.InheritInputs = &models.InheritInputs{Mode: models.InheritModeSelected}
		}), field: "/output/0/inherit_inputs", constraint: inheritInputsConstraint, valueClass: models.FieldValueClassMalformed},
		{name: "spec_ref must be one ref", output: withEntry(func(e *models.OutputEntry) { e.SpecRef = "specs/a.md;specs/b.md" }),
			field: "/output/0/spec_ref", constraint: "must be one clean repo-relative ref (multiple_refs_not_supported)", valueClass: models.FieldValueClassMalformed},
		{name: "epic_ref must not carry annotations", output: withEntry(func(e *models.OutputEntry) { e.EpicRef = "specs/epics/e1.md (section 2)" }),
			field: "/output/0/epic_ref", constraint: "must be one clean repo-relative ref (invalid_path_syntax)", valueClass: models.FieldValueClassMalformed},
		{name: "plan_ref must not be a bare fragment", output: withEntry(func(e *models.OutputEntry) { e.PlanRef = "#Section" }),
			field: "/output/0/plan_ref", constraint: "must be one clean repo-relative ref (empty_ref_path)", valueClass: models.FieldValueClassMalformed},
		{name: "arch_ref must be one ref", output: withEntry(func(e *models.OutputEntry) { e.ArchRef = "specs/a.md;specs/b.md" }),
			field: "/output/0/arch_ref", constraint: "must be one clean repo-relative ref (multiple_refs_not_supported)", valueClass: models.FieldValueClassMalformed},
		{name: "decomposition must declare ownership", output: withEntry(func(e *models.OutputEntry) {
			e.Decomposition = &models.DecompositionManifest{CoverageNotes: "everything the parser needs"}
		}), field: "/output/0/decomposition", constraint: "must declare ownership in owned_files, owned_modules or interfaces_owned", valueClass: models.FieldValueClassMissing},
		{name: "decomposition must not claim everything", output: withEntry(func(e *models.OutputEntry) {
			e.Decomposition = &models.DecompositionManifest{OwnedFiles: []string{"everything else"}}
		}), field: "/output/0/decomposition/owned_files",
			constraint: "must name concrete units, not a catch-all claim over everything remaining", valueClass: models.FieldValueClassMalformed},
		{name: "decomposition read_only_depends_on must be a sibling", output: withEntry(func(e *models.OutputEntry) {
			e.Decomposition = &models.DecompositionManifest{OwnedFiles: []string{"internal/parser/lex.go"}, ReadOnlyDependsOn: []int{4}}
		}), field: "/output/0/decomposition/read_only_depends_on",
			constraint: "must reference a sibling output index other than this entry's own", valueClass: models.FieldValueClassOutOfRange},
		{name: "decomposition read_only_depends_on must also be scheduled", output: []models.OutputEntry{
			validEntry(),
			func() models.OutputEntry {
				entry := validEntry()
				entry.Decomposition = &models.DecompositionManifest{OwnedFiles: []string{"internal/parser/lex.go"}, ReadOnlyDependsOn: []int{0}}
				return entry
			}(),
		}, field: "/output/1/decomposition/read_only_depends_on", constraint: "must also appear in depends_on", valueClass: models.FieldValueClassOutOfRange},
		{name: "two entries must not own the same file", output: []models.OutputEntry{
			func() models.OutputEntry {
				entry := validEntry()
				entry.Decomposition = &models.DecompositionManifest{OwnedFiles: []string{"internal/parser/lex.go"}}
				return entry
			}(),
			func() models.OutputEntry {
				entry := validEntry()
				entry.Decomposition = &models.DecompositionManifest{OwnedFiles: []string{"internal/parser/lex.go"}}
				return entry
			}(),
		}, field: "/output/1/decomposition/owned_files",
			constraint: "must not claim ownership another output entry already declared", valueClass: models.FieldValueClassConflict},
		{name: "two entries must not own the same interface", output: []models.OutputEntry{
			func() models.OutputEntry {
				entry := validEntry()
				entry.Decomposition = &models.DecompositionManifest{InterfacesOwned: []string{"Parser"}}
				return entry
			}(),
			func() models.OutputEntry {
				entry := validEntry()
				entry.Decomposition = &models.DecompositionManifest{InterfacesOwned: []string{"Parser"}}
				return entry
			}(),
		}, field: "/output/1/decomposition/interfaces_owned",
			constraint: "must not claim ownership another output entry already declared", valueClass: models.FieldValueClassConflict},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			version, diagnostics, err := payloadschema.Validate(payloadschema.SetTaskOutputOperation, payloadschema.SetTaskOutputPayload(test.output))
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if version != 1 {
				t.Fatalf("schema version = %d, want 1", version)
			}
			index := slices.IndexFunc(diagnostics, func(d models.FieldDiagnostic) bool { return d.Field == test.field })
			if index < 0 {
				t.Fatalf("no diagnostic on %s: %#v", test.field, diagnostics)
			}
			diagnostic := diagnostics[index]
			if diagnostic.Constraint != test.constraint {
				t.Errorf("constraint = %q, want %q", diagnostic.Constraint, test.constraint)
			}
			if diagnostic.ValueClass != test.valueClass {
				t.Errorf("value_class = %q, want %q", diagnostic.ValueClass, test.valueClass)
			}
			if diagnostic.SchemaVersion != 1 {
				t.Errorf("schema_version = %d, want 1", diagnostic.SchemaVersion)
			}
			if diagnostic.SafeAction != models.FieldDiagnosticCorrectInput {
				t.Errorf("safe_action = %q, want %q", diagnostic.SafeAction, models.FieldDiagnosticCorrectInput)
			}
		})
	}
}

func TestSetTaskOutputSchemaAccepts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output []models.OutputEntry
	}{
		{name: "a minimal entry", output: []models.OutputEntry{validEntry()}},
		{name: "an empty manifest clears the output", output: []models.OutputEntry{}},
		// Requiredness of a decomposition block depends on the producing
		// role-pair, so its absence is a mutation-boundary question.
		{name: "an absent decomposition block", output: withEntry(func(e *models.OutputEntry) { e.Decomposition = nil })},
		{name: "a well-formed decomposition block", output: withEntry(func(e *models.OutputEntry) {
			e.Decomposition = &models.DecompositionManifest{
				OwnedFiles:      []string{"internal/parser/lex.go"},
				InterfacesOwned: []string{"Lexer"},
			}
		})},
		{name: "a scheduled read-only sibling", output: []models.OutputEntry{
			validEntry(),
			func() models.OutputEntry {
				entry := validEntry()
				entry.DependsOn = []string{"0"}
				entry.Decomposition = &models.DecompositionManifest{OwnedFiles: []string{"internal/parser/parse.go"}, ReadOnlyDependsOn: []int{0}}
				return entry
			}(),
		}},
		// task_depends_on syntax needs the task-ID rules, which live outside
		// the schema's dependency boundary and stay at the mutation boundary.
		{name: "a task_depends_on entry the schema does not parse", output: withEntry(func(e *models.OutputEntry) {
			e.TaskDependsOn = []string{"cpm-1-cp-3-code-1"}
		})},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, diagnostics, err := payloadschema.Validate(payloadschema.SetTaskOutputOperation, payloadschema.SetTaskOutputPayload(test.output))
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if len(diagnostics) > 0 {
				t.Fatalf("valid manifest rejected: %#v", diagnostics)
			}
		})
	}
}

func TestSetTaskOutputSchemaRejectsMalformedManifest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		payload    any
		field      string
		valueClass string
	}{
		{name: "a missing output key", payload: map[string]any{}, field: "/output", valueClass: models.FieldValueClassMissing},
		{name: "a null output", payload: map[string]any{"output": nil}, field: "/output", valueClass: models.FieldValueClassMissing},
		{name: "an output that is not an array", payload: map[string]any{"output": "one entry"}, field: "/output", valueClass: models.FieldValueClassWrongType},
		{name: "an entry field of the wrong type", payload: map[string]any{"output": []any{map[string]any{"desc": 7}}}, field: "/output", valueClass: models.FieldValueClassWrongType},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, diagnostics, err := payloadschema.Validate(payloadschema.SetTaskOutputOperation, test.payload)
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if len(diagnostics) != 1 {
				t.Fatalf("want one decode diagnostic, got %#v", diagnostics)
			}
			if diagnostics[0].Field != test.field || diagnostics[0].ValueClass != test.valueClass {
				t.Fatalf("diagnostic = %#v, want field %s and value class %s", diagnostics[0], test.field, test.valueClass)
			}
		})
	}
}

func TestSetTaskOutputSchemaIsListed(t *testing.T) {
	t.Parallel()

	if !slices.Contains(payloadschema.List(), payloadschema.Descriptor{Operation: payloadschema.SetTaskOutputOperation, Version: 1}) {
		t.Fatalf("set-task-output version 1 is not discoverable: %#v", payloadschema.List())
	}
}

// A diagnostic reports what the manifest had to satisfy, never what it held: a
// validation command can carry a credential.
func TestSetTaskOutputSchemaKeepsRejectedValuesOut(t *testing.T) {
	t.Parallel()

	const secret = "API_TOKEN=s3cr3t-do-not-echo"
	output := withEntry(func(e *models.OutputEntry) {
		e.Kind = secret
		e.Validation = []string{secret + " "}
		e.SpecRef = "specs/a.md;" + secret
	})

	_, diagnostics, err := payloadschema.Validate(payloadschema.SetTaskOutputOperation, payloadschema.SetTaskOutputPayload(output))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(diagnostics) == 0 {
		t.Fatal("expected the malformed manifest to be rejected")
	}
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Constraint, "s3cr3t") || strings.Contains(diagnostic.Field, "s3cr3t") {
			t.Fatalf("diagnostic echoed the rejected value: %#v", diagnostic)
		}
	}
}
