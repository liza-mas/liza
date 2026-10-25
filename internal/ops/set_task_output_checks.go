package ops

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

// checkSetTaskOutputInput runs the checks that need neither state nor the
// pipeline: identity presence, the manifest schema shared with the preflight,
// and per-entry reference syntax. It runs before any state is read or locked.
func checkSetTaskOutputInput(input *SetTaskOutputInput) error {
	if input.TaskID == "" {
		return &PreconditionError{Reason: "task_id is required"}
	}
	if input.AgentID == "" {
		return &PreconditionError{Reason: "agent_id is required"}
	}
	// The manifest's structure is validated once, by the schema the preflight
	// uses, before any state is read or locked. Only rules that need a task ID,
	// live state or the pipeline resolver remain in the locked checks.
	if err := validateSetTaskOutputManifest(input.Output); err != nil {
		return err
	}
	for i, entry := range input.Output {
		if err := validateTaskDependsOn(entry.TaskDependsOn, i); err != nil {
			return &PreconditionError{Reason: err.Error()}
		}
		if entry.Supersedes != "" {
			if err := paths.ValidateTaskID(entry.Supersedes); err != nil {
				return &PreconditionError{Reason: fmt.Sprintf("output[%d].supersedes: %v", i, err)}
			}
		}
	}
	return nil
}

// normalizeSetTaskOutputRefs strips worktree prefixes from the artifact refs of
// each output entry and trims its task_depends_on.
// Mutates output in-place; callers do not reuse the slice.
func normalizeSetTaskOutputRefs(output []models.OutputEntry) {
	for i := range output {
		output[i].SpecRef = paths.NormalizeSpecRef(output[i].SpecRef)
		output[i].EpicRef = paths.NormalizeSpecRef(output[i].EpicRef)
		output[i].PlanRef = paths.NormalizeSpecRef(output[i].PlanRef)
		output[i].ArchRef = paths.NormalizeSpecRef(output[i].ArchRef)
		output[i].TaskDependsOn = normalizeTaskDependsOn(output[i].TaskDependsOn)
	}
}

// loadSetTaskOutputPipeline resolves the pipeline and the executing status of
// each of its role pairs, the statuses in which a task may accept output.
func loadSetTaskOutputPipeline(projectRoot string) (*pipeline.Resolver, []models.TaskStatus, error) {
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}
	var executing []models.TaskStatus
	for _, rpName := range resolver.RolePairNames() {
		if es, err := resolver.ExecutingStatus(rpName); err == nil {
			executing = append(executing, es)
		}
	}
	return resolver, executing, nil
}

// checkSetTaskOutputWriter refuses a task that is not executing or not
// assigned to the writing agent. Both refusals carry lifecycle results.
func checkSetTaskOutputWriter(task *models.Task, input *SetTaskOutputInput, pipelineExecuting []models.TaskStatus) error {
	if !isExecutingStatus(task.Status, pipelineExecuting) {
		return WrapLifecycleError(setTaskOutputOperation, task, &PreconditionError{Reason: fmt.Sprintf("task %s is not in an executing state (current status: %s)", input.TaskID, task.Status)}, models.LifecycleAlreadyTransitioned, LifecycleSafeActionStop, LifecycleEffectsNone)
	}

	if task.AssignedTo == nil || *task.AssignedTo != input.AgentID {
		currentAgent := "none"
		if task.AssignedTo != nil {
			currentAgent = *task.AssignedTo
		}
		return WrapLifecycleError(setTaskOutputOperation, task, &PreconditionError{Reason: fmt.Sprintf("task %s is not assigned to agent %s (currently assigned to: %s)", input.TaskID, input.AgentID, currentAgent)}, models.LifecycleStaleCaller, LifecycleSafeActionStop, LifecycleEffectsNone)
	}
	return nil
}

// checkSetTaskOutputAgainstState runs the checks that read live state or the
// resolver: allocation, decomposition root, output cap, task dependencies,
// inheritance, dependency direction, supersession and runtime inputs.
func checkSetTaskOutputAgainstState(state *models.State, resolver *pipeline.Resolver, task *models.Task, output []models.OutputEntry) error {
	if err := validateDirectCodingAllocation(resolver, task, output); err != nil {
		return err
	}
	if err := validateDecompositionRootOutput(state, resolver, task.RolePair, output); err != nil {
		return err
	}
	if task.MaxOutputs > 0 && len(output) > task.MaxOutputs {
		return &PreconditionError{Reason: fmt.Sprintf("task %s max_outputs is %d, so %d output entries are refused: writers selecting its outputs by index would miss the extra ones (D-80)", task.ID, task.MaxOutputs, len(output))}
	}
	if err := checkOutputTaskDependsOnState(state, output); err != nil {
		return err
	}

	if err := validateInheritInputsAgainstState(resolver, task, output); err != nil {
		return err
	}

	consumerRolePairs, err := resolver.OutputConsumerRolePairsForOutput(task.RolePair, output)
	if err != nil {
		return err
	}
	for _, consumerRolePair := range consumerRolePairs {
		for i, entry := range output {
			if err := validateDependencyDirection(state, resolver, fmt.Sprintf("%s output[%d]", task.ID, i), consumerRolePair, entry.TaskDependsOn); err != nil {
				return err
			}
		}
	}
	for i, entry := range output {
		if err := validateOutputSupersedes(state, task, consumerRolePairs, i, entry); err != nil {
			return err
		}
	}
	return checkOutputRuntimeInputs(state, resolver, task, output)
}

// checkOutputTaskDependsOnState refuses a task_depends_on naming a missing task
// or a terminal task other than MERGED.
func checkOutputTaskDependsOnState(state *models.State, output []models.OutputEntry) error {
	for i, entry := range output {
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
	return nil
}

// prepareCheckedSetTaskOutput applies the owner's descendant declarations to the
// output and refuses stale declarations and an output the task's amendment mode
// does not allow. It returns the output to store and any misplaced-gate warnings.
func prepareCheckedSetTaskOutput(state *models.State, resolver *pipeline.Resolver, task *models.Task, input *SetTaskOutputInput) ([]models.OutputEntry, []string, error) {
	output, gateWarnings, err := prepareDescendantOutput(resolver, task, input.Output)
	if err != nil {
		return nil, nil, err
	}
	// Draft output may keep a stale declaration (ADR-0188), so validation
	// accepts it; new output must never author one.
	if stale := staleOutputDeclaration(state, resolver, output); stale != "" {
		return nil, nil, &PreconditionError{Reason: fmt.Sprintf("task %s %s; select the replacement's output instead", input.TaskID, stale)}
	}
	if task.AmendsPlan != "" {
		original := state.FindTask(task.AmendsPlan)
		if original == nil || original.PlanAmendment == nil || original.PlanAmendment.Pending != task.ID {
			return nil, nil, &PreconditionError{Reason: "correction is not the original's pending amendment"}
		}
		if err := models.ValidateAmendmentOutputForMode(original.Output, output, task.AmendmentMode); err != nil {
			return nil, nil, &PreconditionError{Reason: err.Error()}
		}
	}
	return output, gateWarnings, nil
}
