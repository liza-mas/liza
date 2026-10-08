package ops

import (
	stderrors "errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
)

const (
	retargetDependencyOperation              = "retarget-dependency"
	retargetDependencyRejectedAction         = "retarget_dependency_rejected"
	retargetDependencyCandidateValidation    = "candidate-state-validation"
	retargetDependencyRejectedDetailMaxBytes = 2048
)

// RetargetDependencyResult contains the outcome of retargeting one task
// dependency edge.
type RetargetDependencyResult struct {
	models.LifecycleOutcome
	TaskID                string   `json:"task_id"`
	OldDependency         string   `json:"old_dependency"`
	NewDependencies       []string `json:"new_dependencies"`
	CanonicalDependencies []string `json:"canonical_dependencies"`
	RepairRequestCleared  bool     `json:"repair_request_cleared"`
	Warnings              []string `json:"warnings,omitempty"`
}

func (r *RetargetDependencyResult) GetWarnings() []string {
	if r == nil {
		return nil
	}
	return r.Warnings
}

// RetargetDependency replaces one non-terminal task's direct depends_on edge
// with one or more direct dependencies. It is a metadata repair operation: it
// does not unblock the task or execute repair validation commands.
func RetargetDependency(projectRoot, taskID, oldDependency string, newDependencies []string, reason, agentID string) (*RetargetDependencyResult, error) {
	return retargetDependencyWithOptionalAuthority(projectRoot, taskID, oldDependency, newDependencies, reason, agentID, nil)
}

// RetargetDependencyWithAuthority fences the dependency rewrite with the
// orchestrator's registration generation.
func RetargetDependencyWithAuthority(projectRoot, taskID, oldDependency string, newDependencies []string, reason string, authority models.AgentAuthority) (*RetargetDependencyResult, error) {
	return retargetDependencyWithOptionalAuthority(projectRoot, taskID, oldDependency, newDependencies, reason, authority.ID, &authority)
}

// RetargetDependencyWithOptions identifies a dependency replacement invocation.
func RetargetDependencyWithOptions(projectRoot, taskID, oldDependency string, newDependencies []string, reason, agentID string, opts LifecycleRequestOptions) (*RetargetDependencyResult, error) {
	return retargetDependencyWithOptionalAuthority(projectRoot, taskID, oldDependency, newDependencies, reason, agentID, nil, opts)
}

// RetargetDependencyWithAuthorityAndOptions fences an identified replacement.
func RetargetDependencyWithAuthorityAndOptions(projectRoot, taskID, oldDependency string, newDependencies []string, reason string, authority models.AgentAuthority, opts LifecycleRequestOptions) (*RetargetDependencyResult, error) {
	return retargetDependencyWithOptionalAuthority(projectRoot, taskID, oldDependency, newDependencies, reason, authority.ID, &authority, opts)
}

func retargetDependencyWithOptionalAuthority(projectRoot, taskID, oldDependency string, newDependencies []string, reason, agentID string, authority *models.AgentAuthority, options ...LifecycleRequestOptions) (returned *RetargetDependencyResult, retErr error) {
	invocation := NewLifecycleInvocation(projectRoot)
	var opts LifecycleRequestOptions
	if len(options) > 0 {
		opts = options[0]
	}
	var observed *models.Task
	effects := "none"
	defer func() {
		retErr = WrapLifecycleError(retargetDependencyOperation, observed, retErr, models.LifecycleInvalidInput, "correct_input", "none")
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if returned != nil {
			outcome = returned.LifecycleOutcome
			warnings = &returned.Warnings
		}
		invocation.FinishResult(retargetDependencyOperation, outcome, &retErr, warnings)
	}()
	if err := ValidateLifecycleRequestOptions(opts); err != nil {
		return nil, err
	}
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if oldDependency == "" {
		return nil, &PreconditionError{Reason: "old dependency is required"}
	}
	if reason == "" {
		return nil, &PreconditionError{Reason: "reason is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "orchestrator agent ID is required"}
	}

	normalizedNewDeps, err := normalizeRetargetNewDependencies(newDependencies)
	if err != nil {
		return nil, err
	}

	lp := paths.New(projectRoot)
	bb := RequestBlackboard(lp.StatePath(), authority, opts)
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, WrapLifecycleError(retargetDependencyOperation, nil, fmt.Errorf("failed to load pipeline config: %w", err), models.LifecycleStateChanged, "requery", "none")
	}

	var result RetargetDependencyResult
	now := time.Now().UTC()

	err = lifecycleMutation(bb, authority)(func(state *models.State) (callbackErr error) {
		defer func() {
			callbackErr = WrapLifecycleError(retargetDependencyOperation, observed, callbackErr, models.LifecycleInvalidInput, "correct_input", "none")
		}()
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		copy := *task
		observed = &copy
		request, err := NewLifecycleRequest(retargetDependencyOperation, task, agentID, authority, opts, struct {
			OldDependency   string
			NewDependencies []string
			Reason          string
		}{oldDependency, normalizedNewDeps, reason})
		if err != nil {
			return err
		}
		receipt, err := CheckLifecycleRequest(task, request, state.Agents)
		if err != nil {
			return err
		}
		if receipt != nil {
			result = RetargetDependencyResult{TaskID: taskID, LifecycleOutcome: LifecycleReplayOutcome(task, receipt, agentID)}
			return errLifecycleReplay
		}
		lineageRepair := false
		if task.Status.IsTerminal() {
			if repairErr := mergedPlanLineageRepair(state, resolver, task, oldDependency, normalizedNewDeps); repairErr != nil {
				return WrapLifecycleError(retargetDependencyOperation, task, &PreconditionError{Reason: fmt.Sprintf("cannot retarget dependencies on terminal task %s (%s): %v", taskID, task.Status, repairErr)}, models.LifecycleAlreadyTransitioned, "stop", "none")
			}
			lineageRepair = true
		}
		if !slices.Contains(task.DependsOn, oldDependency) {
			return WrapLifecycleError(retargetDependencyOperation, task, &PreconditionError{Reason: fmt.Sprintf("task %s does not depend on %s", taskID, oldDependency)}, models.LifecycleStateChanged, "requery", "none")
		}
		for _, depID := range normalizedNewDeps {
			if depID == task.ID {
				return &PreconditionError{Reason: fmt.Sprintf("task %s cannot depend on itself", task.ID)}
			}
			if state.FindTask(depID) == nil {
				return &PreconditionError{Reason: fmt.Sprintf("new dependency %q does not exist", depID)}
			}
		}

		replaced := replaceDependency(task.DependsOn, oldDependency, normalizedNewDeps)
		canonical, _, err := canonicalizeConcreteDependencyList(state, resolver, task.ID, task.RolePair, replaced)
		if err != nil {
			return err
		}
		if len(canonical) == 0 {
			return &PreconditionError{Reason: "retarget-dependency cannot remove all dependencies; use an explicit dependency-removal operation instead"}
		}
		if err := rejectUnmetDependencyOnExecutingConsumer(state, resolver, retargetDependencyOperation, task, canonical); err != nil {
			return err
		}

		repairExtra := matchingRetargetRepairExtra(task.RepairRequest, taskID, oldDependency, canonical)
		repairRequestCleared := repairExtra != nil
		if repairRequestCleared {
			task.RepairRequest = nil
		}

		task.DependsOn = canonical
		extra := map[string]any{
			"manual":                 true,
			"operation":              retargetDependencyOperation,
			"old_dependency":         oldDependency,
			"new_dependencies":       append([]string(nil), normalizedNewDeps...),
			"canonical_dependencies": append([]string(nil), canonical...),
			"repair_request_cleared": repairRequestCleared,
		}
		if lineageRepair {
			extra["merged_plan_lineage_repair"] = true
		}
		for key, value := range repairExtra {
			extra[key] = value
		}
		note := fmt.Sprintf("retargeted dependency %s -> %s", oldDependency, strings.Join(normalizedNewDeps, ", "))
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventDependenciesRewritten,
			Agent:  &agentID,
			Reason: &reason,
			Note:   &note,
			Extra:  extra,
		})

		if err := statevalidate.ValidateCandidate(state, bb.ReadSnapshot, projectRoot, false, os.Stderr); err != nil {
			return err
		}

		result = RetargetDependencyResult{
			TaskID:                taskID,
			OldDependency:         oldDependency,
			NewDependencies:       append([]string(nil), normalizedNewDeps...),
			CanonicalDependencies: append([]string(nil), canonical...),
			RepairRequestCleared:  repairRequestCleared,
		}
		result.LifecycleOutcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{}, state.Agents)
		if err == nil {
			effects = "unknown"
		}
		return err
	})
	if isLifecycleReplay(err) {
		return &result, nil
	}
	if err != nil {
		var cycleErr *statevalidate.DependencyCycleError
		if stderrors.As(err, &cycleErr) {
			cyclePath := slices.Clone(cycleErr.CyclePath)
			recordRetargetDependencyRejection(lp.LogPath(), now, agentID, taskID, oldDependency, normalizedNewDeps, cyclePath, reason, err)
			return nil, WrapLifecycleError(retargetDependencyOperation, observed, &OperationalError{
				Code:    "validation",
				Phase:   retargetDependencyCandidateValidation,
				Message: "retarget dependency rejected because the candidate state contains a dependency cycle",
				Details: map[string]any{
					"operation":         retargetDependencyOperation,
					"task_id":           taskID,
					"old_dependency":    oldDependency,
					"new_dependencies":  append([]string(nil), normalizedNewDeps...),
					"cycle_path":        cyclePath,
					"diagnostic_action": retargetDependencyRejectedAction,
				},
				Err: err,
			}, models.LifecycleInvalidInput, "correct_input", "none")
		}
		return nil, WrapLifecycleError(retargetDependencyOperation, observed, fmt.Errorf("failed to retarget dependency: %w", err), models.LifecycleStateChanged, "requery", effects)
	}

	logger := log.New(lp.LogPath())
	logEntry := log.Entry{
		Timestamp: now,
		Agent:     agentID,
		Action:    retargetDependencyOperation,
		Task:      &taskID,
		Detail:    fmt.Sprintf("%s -> %s: %s", oldDependency, strings.Join(normalizedNewDeps, ","), reason),
	}
	if err := logger.Append(logEntry); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("activity log write failed: %v", err))
	}

	return &result, nil
}

func recordRetargetDependencyRejection(logPath string, timestamp time.Time, agentID, taskID, oldDependency string, newDependencies, cyclePath []string, reason string, err error) {
	detail := fmt.Sprintf(
		"operation=%s phase=%s task_id=%s old_dependency=%s new_dependencies=%s cycle_path=%s reason=%s error=%v",
		retargetDependencyOperation,
		retargetDependencyCandidateValidation,
		taskID,
		oldDependency,
		strings.Join(newDependencies, ","),
		strings.Join(cyclePath, " -> "),
		reason,
		err,
	)
	// boundedString appends this marker after applying its byte limit, so reserve
	// the marker bytes and discard any partial UTF-8 rune at the cut point.
	const truncationMarker = "... [truncated]"
	detail = strings.ToValidUTF8(
		boundedMaskedString(detail, retargetDependencyRejectedDetailMaxBytes-len(truncationMarker)),
		"",
	)
	// The candidate mutation is already rejected; a secondary audit-log failure
	// must not replace the validation result.
	_ = log.New(logPath).Append(log.Entry{
		Timestamp: timestamp,
		Agent:     agentID,
		Action:    retargetDependencyRejectedAction,
		Task:      &taskID,
		Detail:    detail,
	})
}

func normalizeRetargetNewDependencies(values []string) ([]string, error) {
	var normalized []string
	seen := make(map[string]bool)
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			depID := strings.TrimSpace(part)
			if depID == "" {
				return nil, &PreconditionError{Reason: "new dependency entries cannot be empty"}
			}
			if seen[depID] {
				return nil, &PreconditionError{Reason: fmt.Sprintf("duplicate new dependency %q", depID)}
			}
			seen[depID] = true
			normalized = append(normalized, depID)
		}
	}
	if len(normalized) == 0 {
		return nil, &PreconditionError{Reason: "at least one new dependency is required"}
	}
	return normalized, nil
}

// mergedPlanLineageRepair admits the one terminal retarget that repairs replan
// lineage (D-60): a MERGED plan whose hand-off has not started keeps an edge to
// an upstream replanned while it was MERGED (Replan skips terminal consumers).
// The edge moves only to that upstream's MERGED successor, and only when the
// reviewed output already targets the successor and no longer names the
// retired lineage — otherwise the plan predates the replan and must be
// reconciled by replan, not by metadata. Naming a direct child counts as
// naming its parent plan (D-77). Plan-check state is left untouched.
func mergedPlanLineageRepair(state *models.State, resolver *pipeline.Resolver, task *models.Task, oldDependency string, newDependencies []string) error {
	if task.Status != models.TaskStatusMerged || !IsPlanningPair(task.RolePair, resolver.TransitionSourcePairs()) || len(task.Output) == 0 {
		return fmt.Errorf("only a MERGED planning task with output can have a replan lineage edge repaired")
	}
	if len(task.TransitionsExecuted) > 0 || task.PlanHandoffRetired() {
		return fmt.Errorf("its hand-off already ran, was replanned or was retired")
	}
	successor, ok := liveReplanSuccessor(state, oldDependency)
	if !ok {
		return fmt.Errorf("%s has no live replan successor", oldDependency)
	}
	if !slices.Equal(newDependencies, []string{successor.ID}) {
		return fmt.Errorf("a replanned dependency can only be retargeted to its replan successor %s", successor.ID)
	}
	if successor.Status != models.TaskStatusMerged {
		return fmt.Errorf("replan successor %s is %s, not MERGED", successor.ID, successor.Status)
	}
	targetsSuccessor := false
	for index, entry := range task.Output {
		for _, ref := range outputDependencyReferences(entry) {
			// A reference targets itself and, for a generated child, each of
			// its parents. A child exists only once its parent's hand-off ran,
			// so naming the successor's child postdates the replan; every parent
			// is checked first, since one may be the retired lineage.
			referenced := []string{ref}
			if refTask := state.FindTask(ref); refTask != nil {
				referenced = append(referenced, refTask.EffectiveParentTasks()...)
			}
			for i, id := range referenced {
				if lineage, ok := liveReplanSuccessor(state, id); ok && lineage.ID == successor.ID {
					if i == 0 {
						return fmt.Errorf("output[%d] still names %s, replanned into %s; replan the plan instead", index, ref, successor.ID)
					}
					return fmt.Errorf("output[%d] still names %s, a child of %s replanned into %s; replan the plan instead", index, ref, id, successor.ID)
				}
			}
			if slices.Contains(referenced, successor.ID) {
				targetsSuccessor = true
			}
		}
	}
	if !targetsSuccessor {
		return fmt.Errorf("no output names replan successor %s; replan the plan instead", successor.ID)
	}
	return nil
}

// outputDependencyReferences lists the upstream tasks an output entry names.
func outputDependencyReferences(entry models.OutputEntry) []string {
	refs := slices.Clone(entry.TaskDependsOn)
	for _, dep := range entry.ProviderDependencies {
		refs = append(refs, dep.ProviderTask)
	}
	if entry.InheritInputs != nil {
		for _, selection := range entry.InheritInputs.Selections {
			refs = append(refs, selection.UpstreamTask)
		}
	}
	return refs
}

func replaceDependency(deps []string, oldDependency string, newDependencies []string) []string {
	replaced := make([]string, 0, len(deps)+len(newDependencies)-1)
	for _, depID := range deps {
		if depID == oldDependency {
			replaced = append(replaced, newDependencies...)
			continue
		}
		replaced = append(replaced, depID)
	}
	return dedupeStrings(replaced)
}

func matchingRetargetRepairExtra(request *models.RepairRequest, taskID, oldDependency string, canonicalDependencies []string) map[string]any {
	if request == nil || request.Operation != retargetDependencyOperation || request.Target != taskID {
		return nil
	}
	if slices.Contains(canonicalDependencies, oldDependency) {
		return nil
	}
	if !repairCommandMatchesRetargetEdge(request.Command, taskID, oldDependency) {
		return nil
	}

	return map[string]any{
		"repair_operation":  request.Operation,
		"repair_target":     request.Target,
		"repair_command":    request.Command,
		"repair_evidence":   append([]string(nil), request.Evidence...),
		"repair_validation": append([]string(nil), request.Validation...),
	}
}

func repairCommandMatchesRetargetEdge(command, taskID, oldDependency string) bool {
	fields := strings.Fields(command)
	for i := 0; i+2 < len(fields); i++ {
		if fields[i] != retargetDependencyOperation {
			continue
		}
		if fields[i+1] != taskID || fields[i+2] != oldDependency {
			continue
		}
		return true
	}
	return false
}
