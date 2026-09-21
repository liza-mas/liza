package payloadschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// SetTaskOutputOperation names the task-output manifest write on the operation
// and schema registers.
const SetTaskOutputOperation = "set-task-output"

// setTaskOutputVersion is bumped only when a manifest accepted by this version
// would have to be rewritten.
const setTaskOutputVersion = 1

func init() {
	Register(Schema{
		Operation: SetTaskOutputOperation,
		Version:   setTaskOutputVersion,
		Validate:  validateSetTaskOutputPayload,
	})
}

// SetTaskOutputPayload builds the canonical object for a task-output manifest.
// A nil manifest is the empty one, not an absent key: callers that hold typed
// entries have already supplied the field, however many entries it holds.
func SetTaskOutputPayload(output []models.OutputEntry) map[string]any {
	if output == nil {
		output = []models.OutputEntry{}
	}
	return map[string]any{"output": output}
}

// Constraints never interpolate payload content: a rejected value reaches the
// caller only as a value class. Where a models validator's message is
// value-free by construction it is used verbatim, so the failing sub-rule
// survives; every other constraint states the rule this schema enforces.
func validateSetTaskOutputPayload(payload any) []models.FieldDiagnostic {
	entries, diagnostics := decodeSetTaskOutputManifest(payload)
	if diagnostics != nil {
		return diagnostics
	}

	ownedFiles := map[string]int{}
	interfacesOwned := map[string]int{}
	for i, entry := range entries {
		diagnostics = append(diagnostics, validateOutputEntryScalars(i, entry, len(entries))...)
		diagnostics = append(diagnostics, validateOutputEntryRefs(i, entry)...)
		diagnostics = append(diagnostics, validateOutputEntryDecomposition(i, entry, len(entries), ownedFiles, interfacesOwned)...)
	}
	return diagnostics
}

func decodeSetTaskOutputManifest(payload any) ([]models.OutputEntry, []models.FieldDiagnostic) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, []models.FieldDiagnostic{{
			Field:      "/",
			Constraint: "must be one JSON object holding the task-output manifest",
			ValueClass: models.FieldValueClassWrongType,
			SafeAction: models.FieldDiagnosticCorrectInput,
		}}
	}
	// A pointer separates an absent or null output from an empty manifest:
	// clearing a task's output is a manifest of zero entries, not a missing one.
	var manifest struct {
		Output *[]models.OutputEntry `json:"output"`
	}
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		return nil, []models.FieldDiagnostic{{
			Field:      "/output",
			Constraint: "must be an array of task-output entries",
			ValueClass: models.FieldValueClassWrongType,
			SafeAction: models.FieldDiagnosticCorrectInput,
		}}
	}
	if manifest.Output == nil {
		return nil, []models.FieldDiagnostic{{
			Field:      "/output",
			Constraint: "is required; pass the manifest array, empty to clear the task output",
			ValueClass: models.FieldValueClassMissing,
			SafeAction: models.FieldDiagnosticCorrectInput,
		}}
	}
	return *manifest.Output, nil
}

func validateOutputEntryScalars(index int, entry models.OutputEntry, total int) []models.FieldDiagnostic {
	var diagnostics []models.FieldDiagnostic
	for _, required := range []struct {
		field      string
		value      string
		constraint string
	}{
		{"desc", entry.Desc, "is required: describe the child task"},
		{"done_when", entry.DoneWhen, "is required: state the observable completion criteria"},
		{"scope", entry.Scope, "is required: bound what the child may touch"},
		// State validation and transition generation have always required
		// spec_ref; requiring it here fails the write instead of the next read.
		{"spec_ref", entry.SpecRef, "is required: name the spec section the child implements"},
	} {
		if required.value == "" {
			diagnostics = append(diagnostics, outputDiagnostic(index, required.field, required.constraint, models.FieldValueClassMissing))
		}
	}

	if err := models.ValidateKind(entry.Kind); err != nil {
		diagnostics = append(diagnostics, outputDiagnostic(index, "kind", "must be empty or a registered task kind", models.FieldValueClassUnknownEnum))
	}
	if err := models.ValidateValidationSafety("validation", entry.Validation, entry.DestructiveDB); err != nil {
		diagnostics = append(diagnostics, outputDiagnostic(index, "validation", err.Error(), models.FieldValueClassMalformed))
	}
	if err := models.ValidateValidationPrerequisites(entry.Validation, entry.ValidationPrerequisites); err != nil {
		diagnostics = append(diagnostics, outputDiagnostic(index, "validation_prerequisites", err.Error(), models.FieldValueClassMalformed))
	}
	if err := models.ValidateDependsOn(entry.DependsOn, index, total); err != nil {
		diagnostics = append(diagnostics, outputDiagnostic(index, "depends_on", "must hold sibling output indexes other than this entry's own", models.FieldValueClassOutOfRange))
	}
	if err := models.ValidateInheritInputs(entry.InheritInputs, index); err != nil {
		diagnostics = append(diagnostics, outputDiagnostic(index, "inherit_inputs", "mode must be all, selected or none; only selected carries selections, each naming one upstream task and at least one distinct output index", models.FieldValueClassMalformed))
	}
	return diagnostics
}

func validateOutputEntryRefs(index int, entry models.OutputEntry) []models.FieldDiagnostic {
	var diagnostics []models.FieldDiagnostic
	for _, ref := range []struct {
		field string
		value string
	}{
		{"spec_ref", entry.SpecRef},
		{"epic_ref", entry.EpicRef},
		{"plan_ref", entry.PlanRef},
		{"arch_ref", entry.ArchRef},
	} {
		// The artifact-ref error quotes the rejected ref; only its cause, a
		// closed vocabulary, crosses into a diagnostic.
		err := statevalidate.ValidateArtifactRefScalar(ref.field, ref.value, "")
		var refErr *statevalidate.ArtifactRefError
		if errors.As(err, &refErr) {
			diagnostics = append(diagnostics, outputDiagnostic(index, ref.field,
				fmt.Sprintf("must be one clean repo-relative ref (%s)", refErr.Cause), models.FieldValueClassMalformed))
		}
	}
	return diagnostics
}

// validateOutputEntryDecomposition checks the shape a decomposition block must
// have wherever it appears. Whether a block is required at all depends on the
// producing role-pair, which is pipeline configuration, so requiredness stays
// at the mutation boundary and an absent block is accepted here.
func validateOutputEntryDecomposition(index int, entry models.OutputEntry, total int, ownedFiles, interfacesOwned map[string]int) []models.FieldDiagnostic {
	if entry.Decomposition == nil {
		return nil
	}

	var diagnostics []models.FieldDiagnostic
	hasOwnership := false
	for _, owned := range []struct {
		field  string
		values []string
	}{
		{"owned_files", entry.Decomposition.OwnedFiles},
		{"owned_modules", entry.Decomposition.OwnedModules},
		{"interfaces_owned", entry.Decomposition.InterfacesOwned},
	} {
		for _, value := range owned.values {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			if isCatchAllOwnership(trimmed) {
				diagnostics = append(diagnostics, outputDecompositionDiagnostic(index, owned.field,
					"must name concrete units, not a catch-all claim over everything remaining", models.FieldValueClassMalformed))
				continue
			}
			hasOwnership = true
		}
	}
	if !hasOwnership {
		diagnostics = append(diagnostics, outputDiagnostic(index, "decomposition",
			"must declare ownership in owned_files, owned_modules or interfaces_owned", models.FieldValueClassMissing))
	}

	diagnostics = append(diagnostics, rejectDuplicateOwnership(index, "owned_files", entry.Decomposition.OwnedFiles, ownedFiles)...)
	diagnostics = append(diagnostics, rejectDuplicateOwnership(index, "interfaces_owned", entry.Decomposition.InterfacesOwned, interfacesOwned)...)
	diagnostics = append(diagnostics, validateReadOnlyDependsOn(index, entry, total)...)
	return diagnostics
}

func isCatchAllOwnership(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "*", "everything", "everything else", "all", "all files", "all remaining", "remaining":
		return true
	default:
		return false
	}
}

// rejectDuplicateOwnership keeps two entries from claiming the same unit. seen
// carries the first claimant across entries, so the second one is the conflict.
func rejectDuplicateOwnership(index int, field string, values []string, seen map[string]int) []models.FieldDiagnostic {
	var diagnostics []models.FieldDiagnostic
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		previousIndex, claimed := seen[trimmed]
		if claimed && previousIndex != index {
			diagnostics = append(diagnostics, outputDecompositionDiagnostic(index, field,
				"must not claim ownership another output entry already declared", models.FieldValueClassConflict))
			continue
		}
		if !claimed {
			seen[trimmed] = index
		}
	}
	return diagnostics
}

// validateReadOnlyDependsOn checks that a read-only sibling reference is a real
// sibling this entry already waits for: a read-only edge narrows a scheduling
// dependency, it does not add one.
func validateReadOnlyDependsOn(index int, entry models.OutputEntry, total int) []models.FieldDiagnostic {
	if len(entry.Decomposition.ReadOnlyDependsOn) == 0 {
		return nil
	}
	schedulerDeps := make(map[string]struct{}, len(entry.DependsOn))
	for _, dep := range entry.DependsOn {
		schedulerDeps[dep] = struct{}{}
	}

	var diagnostics []models.FieldDiagnostic
	for _, dep := range entry.Decomposition.ReadOnlyDependsOn {
		if dep < 0 || dep >= total || dep == index {
			diagnostics = append(diagnostics, outputDecompositionDiagnostic(index, "read_only_depends_on",
				"must reference a sibling output index other than this entry's own", models.FieldValueClassOutOfRange))
			continue
		}
		if _, scheduled := schedulerDeps[strconv.Itoa(dep)]; !scheduled {
			diagnostics = append(diagnostics, outputDecompositionDiagnostic(index, "read_only_depends_on",
				"must also appear in depends_on", models.FieldValueClassOutOfRange))
		}
	}
	return diagnostics
}

func outputDiagnostic(index int, field, constraint, valueClass string) models.FieldDiagnostic {
	return models.FieldDiagnostic{
		Field:      fmt.Sprintf("/output/%d/%s", index, field),
		Constraint: constraint,
		ValueClass: valueClass,
		SafeAction: models.FieldDiagnosticCorrectInput,
	}
}

func outputDecompositionDiagnostic(index int, field, constraint, valueClass string) models.FieldDiagnostic {
	return outputDiagnostic(index, "decomposition/"+field, constraint, valueClass)
}
