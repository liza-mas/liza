package models

import (
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/referencecontract"
)

// HasCodingAllocation identifies an explicit request for the direct coding
// route. Invalid mixed manifests still select that route and fail validation;
// they must never silently fall back to code planning.
func HasCodingAllocation(output []OutputEntry) bool {
	for _, entry := range output {
		if entry.CodingAllocation {
			return true
		}
	}
	return false
}

// DirectCodingAllocation identifies a consistently marked single-scope
// allocation. Artifact and causal admission are checked separately.
func DirectCodingAllocation(output []OutputEntry) bool {
	if len(output) == 0 {
		return false
	}
	ref := output[0].ArchRef
	file, heading, present := strings.Cut(ref, "#")
	if !present || file == "" || strings.TrimSpace(heading) == "" {
		return false
	}
	for _, entry := range output {
		if !entry.CodingAllocation || entry.ArchRef != ref {
			return false
		}
	}
	return true
}

// ValidateCodingAllocationOutput checks explicit direct allocations before
// publication and generation. Defects retain their reviewed code-planning RCA;
// deferred writer waits must be authored as immediate provider dependencies.
func ValidateCodingAllocationOutput(task *Task, output []OutputEntry) error {
	if !HasCodingAllocation(output) {
		return nil
	}
	if task.EffectiveType() != TaskTypeArchitecture {
		return fmt.Errorf("coding_allocation requires an architecture task")
	}
	if !DirectCodingAllocation(output) {
		return fmt.Errorf("coding_allocation requires every output marked and one shared exact arch_ref Scope heading")
	}
	if task.RCARequired {
		return fmt.Errorf("coding_allocation cannot bypass required code-planning RCA")
	}
	if len(task.DescendantDependencies) > 0 {
		return fmt.Errorf("coding_allocation cannot carry deferred descendant_dependencies; declare immediate provider_dependencies on every coding unit")
	}
	refs := make(map[string]bool, len(output))
	for i, entry := range output {
		if entry.RCARequired != nil && *entry.RCARequired {
			return fmt.Errorf("output[%d].coding_allocation cannot bypass required code-planning RCA", i)
		}
		file, heading, present := strings.Cut(entry.PlanRef, "#")
		if !present || strings.TrimSpace(heading) == "" || referencecontract.ValidateAcceptancePath(file) != nil || refs[entry.PlanRef] {
			return fmt.Errorf("output[%d].plan_ref must name a distinct exact coding allocation heading", i)
		}
		refs[entry.PlanRef] = true
		if entry.Decomposition == nil || len(entry.Decomposition.OwnedFiles) == 0 {
			return fmt.Errorf("output[%d].decomposition.owned_files is required for coding_allocation", i)
		}
		for _, file := range entry.Decomposition.OwnedFiles {
			if err := referencecontract.ValidateAcceptancePath(file); err != nil {
				return fmt.Errorf("output[%d].decomposition.owned_files: %w", i, err)
			}
		}
		if len(entry.Validation) == 0 {
			return fmt.Errorf("output[%d].validation is required for coding_allocation", i)
		}
		if err := ValidateValidationSafety(fmt.Sprintf("output[%d].validation", i), entry.Validation, entry.DestructiveDB); err != nil {
			return err
		}
		if entry.Kind != "" || len(entry.DescendantDependencies) > 0 {
			return fmt.Errorf("output[%d].coding_allocation requires no kind or deferred descendant_dependencies", i)
		}
	}
	return nil
}
