package ops

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// SetTaskOutputInput contains the parameters for setting output entries on a task.
type SetTaskOutputInput struct {
	TaskID  string
	AgentID string
	Output  []models.OutputEntry
}

// SetTaskOutput sets the output[] entries on a task. The task must exist, be assigned
// to the given agent, and be in an executing state (IMPLEMENTING, CODE_PLANNING, or
// a pipeline-defined executing status). Overwrites any existing output (idempotent).
func SetTaskOutput(projectRoot string, input *SetTaskOutputInput) error {
	if input.TaskID == "" {
		return &PreconditionError{Reason: "task_id is required"}
	}
	if input.AgentID == "" {
		return &PreconditionError{Reason: "agent_id is required"}
	}
	for i, entry := range input.Output {
		if entry.Desc == "" {
			return &PreconditionError{Reason: fmt.Sprintf("output[%d].desc is required", i)}
		}
		if entry.DoneWhen == "" {
			return &PreconditionError{Reason: fmt.Sprintf("output[%d].done_when is required", i)}
		}
		if entry.Scope == "" {
			return &PreconditionError{Reason: fmt.Sprintf("output[%d].scope is required", i)}
		}
		if err := models.ValidateKind(entry.Kind); err != nil {
			return &PreconditionError{Reason: fmt.Sprintf("output[%d].%s", i, err.Error())}
		}
		if err := models.ValidateDependsOn(entry.DependsOn, i, len(input.Output)); err != nil {
			return &PreconditionError{Reason: err.Error()}
		}
		if err := validateTaskDependsOn(entry.TaskDependsOn, i); err != nil {
			return &PreconditionError{Reason: err.Error()}
		}
		for _, ref := range []struct {
			field string
			value string
		}{
			{field: "spec_ref", value: entry.SpecRef},
			{field: "epic_ref", value: entry.EpicRef},
			{field: "plan_ref", value: entry.PlanRef},
			{field: "arch_ref", value: entry.ArchRef},
		} {
			if err := statevalidate.ValidateArtifactRefScalar(fmt.Sprintf("output[%d].%s", i, ref.field), ref.value, input.TaskID); err != nil {
				return &PreconditionError{Reason: err.Error()}
			}
		}
	}

	// Normalize spec_ref and plan_ref on each output entry to strip worktree prefixes.
	// Mutates input.Output in-place; callers do not reuse the slice.
	for i := range input.Output {
		input.Output[i].SpecRef = paths.NormalizeSpecRef(input.Output[i].SpecRef)
		input.Output[i].EpicRef = paths.NormalizeSpecRef(input.Output[i].EpicRef)
		input.Output[i].PlanRef = paths.NormalizeSpecRef(input.Output[i].PlanRef)
		input.Output[i].ArchRef = paths.NormalizeSpecRef(input.Output[i].ArchRef)
		input.Output[i].TaskDependsOn = normalizeTaskDependsOn(input.Output[i].TaskDependsOn)
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	// Collect pipeline executing statuses
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return fmt.Errorf("failed to load pipeline config: %w", err)
	}
	var pipelineExecuting []models.TaskStatus
	for _, rpName := range resolver.RolePairNames() {
		if es, err := resolver.ExecutingStatus(rpName); err == nil {
			pipelineExecuting = append(pipelineExecuting, es)
		}
	}

	return bb.Modify(func(state *models.State) error {
		task := state.FindTask(input.TaskID)
		if task == nil {
			return fmt.Errorf("task %s not found", input.TaskID)
		}

		if !isExecutingStatus(task.Status, pipelineExecuting) {
			return &PreconditionError{Reason: fmt.Sprintf("task %s is not in an executing state (current status: %s)", input.TaskID, task.Status)}
		}

		if task.AssignedTo == nil || *task.AssignedTo != input.AgentID {
			currentAgent := "none"
			if task.AssignedTo != nil {
				currentAgent = *task.AssignedTo
			}
			return &PreconditionError{Reason: fmt.Sprintf("task %s is not assigned to agent %s (currently assigned to: %s)", input.TaskID, input.AgentID, currentAgent)}
		}

		if err := validateDecompositionRootOutput(state, resolver, task.RolePair, input.Output); err != nil {
			return err
		}

		for i, entry := range input.Output {
			for _, depID := range entry.TaskDependsOn {
				depTask := state.FindTask(depID)
				if depTask == nil {
					return &PreconditionError{Reason: fmt.Sprintf("output[%d].task_depends_on references non-existent task %q", i, depID)}
				}
				if depTask.Status.IsTerminal() && depTask.Status != models.TaskStatusMerged {
					return &PreconditionError{Reason: fmt.Sprintf("output[%d].task_depends_on references terminal non-MERGED task %q (status: %s)", i, depID, depTask.Status)}
				}
			}
		}

		consumerRolePairs, err := resolver.OutputConsumerRolePairs(task.RolePair)
		if err != nil {
			return err
		}
		for _, consumerRolePair := range consumerRolePairs {
			for i, entry := range input.Output {
				if err := validateDependencyDirection(state, resolver, fmt.Sprintf("%s output[%d]", task.ID, i), consumerRolePair, entry.TaskDependsOn); err != nil {
					return err
				}
			}
		}

		task.Output = input.Output
		return nil
	})
}

func validateDecompositionRootOutput(state *models.State, resolver decompositionRootResolver, rolePair string, output []models.OutputEntry) error {
	isRoot, err := resolver.IsDecompositionRoot(rolePair)
	if err != nil {
		return err
	}
	if !isRoot {
		return nil
	}

	refField, err := resolver.DecompositionOutputRef(rolePair)
	if err != nil {
		return err
	}
	requiredRef, err := requiredDecompositionRootOutputRef(refField)
	if err != nil {
		return err
	}

	ownedFiles := map[string]int{}
	interfacesOwned := map[string]int{}
	for i, entry := range output {
		if strings.TrimSpace(requiredRef.value(entry)) == "" {
			return &PreconditionError{Reason: fmt.Sprintf("output[%d].%s is required for decomposition-root role-pair %q", i, requiredRef.field, rolePair)}
		}
		if entry.Decomposition == nil {
			return &PreconditionError{Reason: fmt.Sprintf("output[%d].decomposition is required for decomposition-root role-pair %q", i, rolePair)}
		}
		if err := validateOwnershipDeclaration(i, entry.Decomposition); err != nil {
			return err
		}
		if err := rejectDuplicateOwnership("owned_files", i, entry.Decomposition.OwnedFiles, ownedFiles); err != nil {
			return err
		}
		if err := rejectDuplicateOwnership("interfaces_owned", i, entry.Decomposition.InterfacesOwned, interfacesOwned); err != nil {
			return err
		}
		if err := validateReadOnlyDependsOn(i, len(output), entry.DependsOn, entry.Decomposition.ReadOnlyDependsOn); err != nil {
			return err
		}
		if err := validateReadOnlyTaskDependsOn(state, i, entry.TaskDependsOn, entry.Decomposition.ReadOnlyTaskDependsOn); err != nil {
			return err
		}
	}

	return validateDependsOnAcyclic(output)
}

type decompositionRootResolver interface {
	IsDecompositionRoot(rolePair string) (bool, error)
	DecompositionOutputRef(rolePair string) (string, error)
}

type decompositionRootOutputRef struct {
	field string
	value func(models.OutputEntry) string
}

func requiredDecompositionRootOutputRef(refField string) (decompositionRootOutputRef, error) {
	switch refField {
	case "plan_ref":
		return decompositionRootOutputRef{
			field: "plan_ref",
			value: func(entry models.OutputEntry) string { return entry.PlanRef },
		}, nil
	case "arch_ref":
		return decompositionRootOutputRef{
			field: "arch_ref",
			value: func(entry models.OutputEntry) string { return entry.ArchRef },
		}, nil
	case "epic_ref":
		return decompositionRootOutputRef{
			field: "epic_ref",
			value: func(entry models.OutputEntry) string { return entry.EpicRef },
		}, nil
	case "spec_ref":
		return decompositionRootOutputRef{
			field: "spec_ref",
			value: func(entry models.OutputEntry) string { return entry.SpecRef },
		}, nil
	default:
		return decompositionRootOutputRef{}, &PreconditionError{Reason: fmt.Sprintf("decomposition-root output ref %q is unsupported", refField)}
	}
}

func validateOwnershipDeclaration(entryIndex int, manifest *models.DecompositionManifest) error {
	ownershipFields := [][]string{
		manifest.OwnedFiles,
		manifest.OwnedModules,
		manifest.InterfacesOwned,
	}
	hasOwnership := false
	for _, values := range ownershipFields {
		for _, value := range values {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			if isCatchAllOwnership(trimmed) {
				return &PreconditionError{Reason: fmt.Sprintf("output[%d].decomposition contains catch-all ownership declaration %q", entryIndex, value)}
			}
			hasOwnership = true
		}
	}
	if !hasOwnership {
		return &PreconditionError{Reason: fmt.Sprintf("output[%d].decomposition must declare ownership in owned_files, owned_modules, or interfaces_owned", entryIndex)}
	}
	return nil
}

func isCatchAllOwnership(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "*", "everything", "everything else", "all", "all files", "all remaining", "remaining":
		return true
	default:
		return false
	}
}

func rejectDuplicateOwnership(field string, entryIndex int, values []string, seen map[string]int) error {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if previousIndex, ok := seen[trimmed]; ok && previousIndex != entryIndex {
			return &PreconditionError{Reason: fmt.Sprintf("output[%d].decomposition.%s duplicates output[%d] ownership %q", entryIndex, field, previousIndex, trimmed)}
		}
		if _, ok := seen[trimmed]; !ok {
			seen[trimmed] = entryIndex
		}
	}
	return nil
}

func validateReadOnlyDependsOn(entryIndex, outputCount int, dependsOn []string, readOnlyDependsOn []int) error {
	schedulerDeps := map[string]struct{}{}
	for _, dep := range dependsOn {
		schedulerDeps[dep] = struct{}{}
	}
	for _, dep := range readOnlyDependsOn {
		if dep < 0 || dep >= outputCount {
			return &PreconditionError{Reason: fmt.Sprintf("output[%d].decomposition.read_only_depends_on reference %d out of range [0, %d)", entryIndex, dep, outputCount)}
		}
		if dep == entryIndex {
			return &PreconditionError{Reason: fmt.Sprintf("output[%d].decomposition.read_only_depends_on references itself", entryIndex)}
		}
		depRef := strconv.Itoa(dep)
		if _, ok := schedulerDeps[depRef]; !ok {
			return &PreconditionError{Reason: fmt.Sprintf("output[%d].decomposition.read_only_depends_on reference %d must also appear in depends_on", entryIndex, dep)}
		}
	}
	return nil
}

func validateReadOnlyTaskDependsOn(state *models.State, entryIndex int, taskDependsOn []string, readOnlyTaskDependsOn []string) error {
	taskDeps := map[string]struct{}{}
	for _, dep := range taskDependsOn {
		taskDeps[dep] = struct{}{}
	}
	for _, depID := range readOnlyTaskDependsOn {
		trimmed := strings.TrimSpace(depID)
		if trimmed == "" {
			return &PreconditionError{Reason: fmt.Sprintf("output[%d].decomposition.read_only_task_depends_on contains invalid task ID %q", entryIndex, depID)}
		}
		if err := paths.ValidateTaskID(trimmed); err != nil {
			return &PreconditionError{Reason: fmt.Sprintf("output[%d].decomposition.read_only_task_depends_on contains invalid task ID %q: %v", entryIndex, depID, err)}
		}
		if state.FindTask(trimmed) == nil {
			return &PreconditionError{Reason: fmt.Sprintf("output[%d].decomposition.read_only_task_depends_on references non-existent task %q", entryIndex, trimmed)}
		}
		if _, ok := taskDeps[trimmed]; !ok {
			return &PreconditionError{Reason: fmt.Sprintf("output[%d].decomposition.read_only_task_depends_on reference %q must also appear in task_depends_on", entryIndex, trimmed)}
		}
	}
	return nil
}

func validateDependsOnAcyclic(output []models.OutputEntry) error {
	visiting := make([]bool, len(output))
	visited := make([]bool, len(output))

	var visit func(int) error
	visit = func(index int) error {
		if visiting[index] {
			return &PreconditionError{Reason: fmt.Sprintf("output[%d].depends_on cycle detected", index)}
		}
		if visited[index] {
			return nil
		}
		visiting[index] = true
		for _, depRef := range output[index].DependsOn {
			depIndex, err := strconv.Atoi(depRef)
			if err != nil {
				return err
			}
			if depIndex < 0 || depIndex >= len(output) {
				return &PreconditionError{Reason: fmt.Sprintf("output[%d].depends_on reference %q out of range [0, %d)", index, depRef, len(output))}
			}
			if err := visit(depIndex); err != nil {
				return err
			}
		}
		visiting[index] = false
		visited[index] = true
		return nil
	}

	for i := range output {
		if err := visit(i); err != nil {
			return err
		}
	}
	return nil
}

func validateTaskDependsOn(deps []string, entryIndex int) error {
	for _, depID := range deps {
		trimmed := strings.TrimSpace(depID)
		if trimmed == "" {
			continue
		}
		if err := paths.ValidateTaskID(trimmed); err != nil {
			return fmt.Errorf("output[%d].task_depends_on contains invalid task ID %q: %w", entryIndex, depID, err)
		}
	}
	return nil
}

func normalizeTaskDependsOn(deps []string) []string {
	normalized := make([]string, 0, len(deps))
	for _, depID := range deps {
		trimmed := strings.TrimSpace(depID)
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	return normalized
}
