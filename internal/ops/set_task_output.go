package ops

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// SetTaskOutputInput contains the parameters for setting output entries on a task.
type SetTaskOutputInput struct {
	Request LifecycleRequestOptions
	TaskID  string
	AgentID string
	Output  []models.OutputEntry
}

type SetTaskOutputResult struct {
	models.LifecycleOutcome
	TaskID      string   `json:"task_id"`
	OutputCount int      `json:"output_count"`
	StatePath   string   `json:"state_path"`
	Warnings    []string `json:"warnings,omitempty"`
}

// SetTaskOutput sets the output[] entries on a task. The task must exist, be assigned
// to the given agent, and be in an executing state (IMPLEMENTING, CODE_PLANNING, or
// a pipeline-defined executing status). Replaces output and appends a write receipt
// to task history in the same transaction, including when output is unchanged.
func SetTaskOutput(projectRoot string, input *SetTaskOutputInput) error {
	_, err := SetTaskOutputWithOptions(projectRoot, input)
	return err
}

func SetTaskOutputWithOptions(projectRoot string, input *SetTaskOutputInput) (*SetTaskOutputResult, error) {
	return setTaskOutputResult(projectRoot, input, nil)
}

// SetTaskOutputWithAuthority fences the output write with the caller's
// registration generation.
func SetTaskOutputWithAuthority(projectRoot string, input *SetTaskOutputInput, authority models.AgentAuthority) error {
	_, err := SetTaskOutputWithAuthorityAndOptions(projectRoot, input, authority)
	return err
}

func SetTaskOutputWithAuthorityAndOptions(projectRoot string, input *SetTaskOutputInput, authority models.AgentAuthority) (*SetTaskOutputResult, error) {
	return setTaskOutputResult(projectRoot, input, &authority)
}

func setTaskOutputResult(projectRoot string, input *SetTaskOutputInput, authority *models.AgentAuthority) (result *SetTaskOutputResult, retErr error) {
	if input == nil {
		return nil, WrapLifecycleError("set-task-output", nil, fmt.Errorf("output input is required"), models.LifecycleInvalidInput, "correct_input", "none")
	}
	invocation := NewLifecycleInvocation(projectRoot)
	var outcome models.LifecycleOutcome
	defer func() {
		failure, action := models.LifecycleStateChanged, "requery"
		var invalid *PreconditionError
		if errors.As(retErr, &invalid) {
			failure, action = models.LifecycleInvalidInput, "correct_input"
		}
		retErr = WrapLifecycleError("set-task-output", nil, retErr, failure, action, "none")
		// The requested task ID remains useful when storage fails before a task
		// observation is available; do not invent its status or ownership.
		var lifecycleErr *LifecycleError
		if errors.As(retErr, &lifecycleErr) && lifecycleErr.Outcome.TaskID == "" {
			lifecycleErr.Outcome.TaskID = input.TaskID
		}
		var warnings *[]string
		if result != nil {
			warnings = &result.Warnings
		}
		invocation.FinishResult("set-task-output", outcome, &retErr, warnings)
	}()
	if authority != nil {
		if err := requireAuthorityActor(*authority, input.AgentID); err != nil {
			return nil, err
		}
	}
	if err := ValidateLifecycleRequestOptions(input.Request); err != nil {
		return nil, &PreconditionError{Reason: err.Error()}
	}
	if err := setTaskOutputWithOptionalAuthority(projectRoot, input, authority, &outcome); err != nil {
		return nil, err
	}
	return &SetTaskOutputResult{LifecycleOutcome: outcome, TaskID: input.TaskID, OutputCount: len(input.Output), StatePath: paths.New(projectRoot).StatePath()}, nil
}

func setTaskOutputWithOptionalAuthority(projectRoot string, input *SetTaskOutputInput, authority *models.AgentAuthority, outcome *models.LifecycleOutcome) (retErr error) {
	phase := "validate-output"
	defer func() {
		if retErr == nil {
			return
		}
		details := map[string]any{
			"operation": "set-task-output", "task_id": input.TaskID,
			"state_path": paths.New(projectRoot).StatePath(), "output_count": len(input.Output),
			"recovery_hint": "Inspect the task state and history before retrying; a later reset may clear a previously saved output.",
		}
		// Preserve opted-in diagnostics (including generation fencing) while
		// exposing only the OS error text, never arbitrary wrapped payloads.
		var detailed interface{ SafeDetails() map[string]any }
		if errors.As(retErr, &detailed) {
			for key, value := range detailed.SafeDetails() {
				if _, exists := details[key]; !exists {
					details[key] = value
				}
			}
		}
		var errno syscall.Errno
		if errors.As(retErr, &errno) {
			details["cause"] = errno.Error()
		}
		retErr = &OperationalError{Phase: phase, Message: "failed to set task output", Details: details, Err: retErr}
	}()
	if input.TaskID == "" {
		return &PreconditionError{Reason: "task_id is required"}
	}
	if input.AgentID == "" {
		return &PreconditionError{Reason: "agent_id is required"}
	}
	// The manifest's structure is validated once, by the schema the preflight
	// uses, before any state is read or locked. Only rules that need a task ID,
	// live state or the pipeline resolver remain below.
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
	phase = "load-pipeline"
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

	phase = "persist-output"
	err = lifecycleMutation(bb, authority)(func(state *models.State) error {
		task := state.FindTask(input.TaskID)
		if task == nil {
			return &PreconditionError{Reason: fmt.Sprintf("task %s not found", input.TaskID)}
		}
		request, err := NewLifecycleRequest("set-task-output", task, input.AgentID, authority, input.Request, input.Output)
		if err != nil {
			return err
		}
		receipt, err := CheckLifecycleRequest(task, request, state.Agents)
		if err != nil {
			return err
		}
		if receipt != nil {
			*outcome = LifecycleReplayOutcome(task, receipt, input.AgentID)
			return errLifecycleReplay
		}

		if !isExecutingStatus(task.Status, pipelineExecuting) {
			return WrapLifecycleError("set-task-output", task, &PreconditionError{Reason: fmt.Sprintf("task %s is not in an executing state (current status: %s)", input.TaskID, task.Status)}, models.LifecycleAlreadyTransitioned, "stop", "none")
		}

		if task.AssignedTo == nil || *task.AssignedTo != input.AgentID {
			currentAgent := "none"
			if task.AssignedTo != nil {
				currentAgent = *task.AssignedTo
			}
			return WrapLifecycleError("set-task-output", task, &PreconditionError{Reason: fmt.Sprintf("task %s is not assigned to agent %s (currently assigned to: %s)", input.TaskID, input.AgentID, currentAgent)}, models.LifecycleStaleCaller, "stop", "none")
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

		if err := validateInheritInputsAgainstState(resolver, task, input.Output); err != nil {
			return err
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
		for i, entry := range input.Output {
			if err := validateOutputSupersedes(state, task, consumerRolePairs, i, entry); err != nil {
				return err
			}
		}

		previousCount := len(task.Output)
		task.Output = input.Output
		task.History = append(task.History, models.TaskHistoryEntry{
			Time: time.Now().UTC(), Event: models.TaskEventOutputSet, Agent: &input.AgentID,
			Extra: map[string]any{"previous_output_count": previousCount, "output_count": len(input.Output)},
		})
		*outcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{}, state.Agents)
		return err
	})
	if isLifecycleReplay(err) {
		return nil
	}
	return err
}

// validateOutputArtifactRefScalars keeps the artifact-ref syntax rule available
// to boundaries that revalidate a task's stored output.
func validateOutputArtifactRefScalars(taskID string, output []models.OutputEntry) error {
	for i, entry := range output {
		for _, ref := range []struct {
			field string
			value string
		}{
			{field: "spec_ref", value: entry.SpecRef},
			{field: "epic_ref", value: entry.EpicRef},
			{field: "plan_ref", value: entry.PlanRef},
			{field: "arch_ref", value: entry.ArchRef},
		} {
			if err := statevalidate.ValidateArtifactRefScalar(fmt.Sprintf("output[%d].%s", i, ref.field), ref.value, taskID); err != nil {
				return &PreconditionError{Reason: err.Error()}
			}
		}
	}
	return nil
}

// validateSetTaskOutputManifest rejects a structurally invalid manifest with
// the diagnostics the preflight returns for the same canonical object, so the
// two boundaries cannot disagree about a manifest's shape.
func validateSetTaskOutputManifest(output []models.OutputEntry) error {
	_, diagnostics, err := payloadschema.Validate(payloadschema.SetTaskOutputOperation, payloadschema.SetTaskOutputPayload(output))
	if err != nil {
		return err
	}
	if len(diagnostics) == 0 {
		return nil
	}
	// The cause is a precondition message naming only field paths, so a
	// text-mode caller still learns what to correct; the values stay out, as
	// in the diagnostics themselves.
	fields := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		fields = append(fields, diagnostic.Field)
	}
	return NewLifecycleInvalidInputError("set-task-output", nil, diagnostics, &PreconditionError{
		Reason: fmt.Sprintf("task output manifest rejected by the %s schema at %s", payloadschema.SetTaskOutputOperation, strings.Join(fields, ", ")),
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
	if err := validateDecompositionRootRCAClassificationForRoot(resolver, rolePair, output); err != nil {
		return err
	}

	// A present block's shape is the schema's; only its requiredness and the
	// references it makes into live state are decided here.
	for i, entry := range output {
		if strings.TrimSpace(requiredRef.value(entry)) == "" {
			return &PreconditionError{Reason: fmt.Sprintf("output[%d].%s is required for decomposition-root role-pair %q", i, requiredRef.field, rolePair)}
		}
		if entry.Decomposition == nil {
			return &PreconditionError{Reason: fmt.Sprintf("output[%d].decomposition is required for decomposition-root role-pair %q", i, rolePair)}
		}
		if err := validateReadOnlyTaskDependsOn(state, i, entry.TaskDependsOn, entry.Decomposition.ReadOnlyTaskDependsOn); err != nil {
			return err
		}
	}

	return validateDependsOnAcyclic(output)
}

func validateDecompositionRootRCAClassification(resolver decompositionRootResolver, rolePair string, output []models.OutputEntry) error {
	isRoot, err := resolver.IsDecompositionRoot(rolePair)
	if err != nil {
		return err
	}
	if !isRoot {
		return nil
	}
	return validateDecompositionRootRCAClassificationForRoot(resolver, rolePair, output)
}

func validateDecompositionRootRCAClassificationForRoot(resolver decompositionRootResolver, rolePair string, output []models.OutputEntry) error {
	requiresRCAClassification, err := decompositionOutputsRequireRCAClassification(resolver, rolePair)
	if err != nil {
		return err
	}
	if !requiresRCAClassification {
		return nil
	}
	for i, entry := range output {
		if entry.RCARequired == nil {
			return &PreconditionError{Reason: fmt.Sprintf("output[%d].rca_required is required because decomposition-root role-pair %q routes to code planning", i, rolePair)}
		}
	}
	return nil
}

func decompositionOutputsRequireRCAClassification(resolver decompositionRootResolver, rolePair string) (bool, error) {
	consumerRolePairs, err := resolver.OutputConsumerRolePairs(rolePair)
	if err != nil {
		return false, err
	}
	for _, consumerRolePair := range consumerRolePairs {
		doerRole, err := resolver.DoerRole(consumerRolePair)
		if err != nil {
			return false, err
		}
		if doerRole == models.RoleCodePlanner {
			return true, nil
		}
	}
	return false, nil
}

type decompositionRootResolver interface {
	IsDecompositionRoot(rolePair string) (bool, error)
	DecompositionOutputRef(rolePair string) (string, error)
	OutputConsumerRolePairs(sourceRolePair string) ([]string, error)
	DoerRole(rolePair string) (string, error)
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

// validateInheritInputsAgainstState checks the parts of a selective
// inherit_inputs that need the producing task and the pipeline topology.
//
// Index bounds are deliberately not checked here. A selection names positions
// in an upstream task's output[], and that upstream has usually not produced
// its output when this runs — bounds are enforced at generation, where the
// real upstream output exists.
func validateInheritInputsAgainstState(resolver *pipeline.Resolver, task *models.Task, entries []models.OutputEntry) error {
	selective := false
	for _, entry := range entries {
		if entry.InheritInputs.IsSelective() {
			selective = true
			break
		}
	}
	if !selective {
		return nil
	}

	// OutputConsumerRolePairs only reports per-subtask consumers. No consumer
	// means no transition from this task fans out, so there is nothing for a
	// selection to narrow and the planner has misunderstood the topology.
	consumers, err := resolver.OutputConsumerRolePairs(task.RolePair)
	if err != nil {
		return err
	}
	if len(consumers) == 0 {
		return &PreconditionError{Reason: fmt.Sprintf(
			"task %s role_pair %q has no per-subtask consumer transition, so inherit_inputs mode %q selects from nothing; "+
				"omit inherit_inputs to inherit the whole upstream phase",
			task.ID, task.RolePair, models.InheritModeSelected)}
	}

	for i, entry := range entries {
		if !entry.InheritInputs.IsSelective() {
			continue
		}
		for selectionIndex, selection := range entry.InheritInputs.Selections {
			if !slices.Contains(task.DependsOn, selection.UpstreamTask) {
				return &PreconditionError{Reason: fmt.Sprintf(
					"output[%d].inherit_inputs.selections[%d]: upstream_task %q is not a dependency of %s; "+
						"a child can only select from phases its producing task waits for",
					i, selectionIndex, selection.UpstreamTask, task.ID)}
			}
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

// validateOutputSupersedes rejects a supersedes target the generating
// transition could never retire (ADR-0161). Generation re-checks eligibility
// under its own lock; this only fails the planner early. A live original that
// is not yet supersedable is accepted: it may become so before generation.
func validateOutputSupersedes(state *models.State, plan *models.Task, consumerRolePairs []string, index int, entry models.OutputEntry) error {
	id := entry.Supersedes
	if id == "" {
		return nil
	}
	original := state.FindTask(id)
	switch {
	case original == nil:
		return &PreconditionError{Reason: fmt.Sprintf("output[%d].supersedes references non-existent task %q", index, id)}
	case id == plan.ID:
		return &PreconditionError{Reason: fmt.Sprintf("output[%d].supersedes cannot name the planning task itself %q", index, id)}
	case original.Status.IsTerminal():
		return &PreconditionError{Reason: fmt.Sprintf("output[%d].supersedes references terminal task %q (status: %s); delivered or retired work is not replaced", index, id, original.Status)}
	case !slices.Contains(consumerRolePairs, original.RolePair):
		return &PreconditionError{Reason: fmt.Sprintf("output[%d].supersedes references %q in role pair %q; this output generates %v children", index, id, original.RolePair, consumerRolePairs)}
	case slices.Contains(entry.TaskDependsOn, id):
		return &PreconditionError{Reason: fmt.Sprintf("output[%d].supersedes %q also appears in its task_depends_on; a child cannot depend on the task it replaces", index, id)}
	}
	return nil
}
