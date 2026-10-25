package ops

import (
	"fmt"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// checkEligibility admits the retarget against the task's effective lineage: a
// terminal task only through the replan lineage repair, then the old edge's
// presence and the new dependencies' existence. It reports whether the
// retarget is a lineage repair.
func (r *retargetRun) checkEligibility(state *models.State, task *models.Task) (lineageRepair bool, err error) {
	if task.Status.IsTerminal() {
		if repairErr := mergedPlanLineageRepair(state, r.resolver, task, r.oldDependency, r.newDependencies); repairErr != nil {
			return false, WrapLifecycleError(retargetDependencyOperation, task, &PreconditionError{Reason: fmt.Sprintf("cannot retarget dependencies on terminal task %s (%s): %v", r.taskID, task.Status, repairErr)}, models.LifecycleAlreadyTransitioned, LifecycleSafeActionStop, LifecycleEffectsNone)
		}
		lineageRepair = true
	}
	if !slices.Contains(task.DependsOn, r.oldDependency) {
		return false, WrapLifecycleError(retargetDependencyOperation, task, &PreconditionError{Reason: fmt.Sprintf("task %s does not depend on %s", r.taskID, r.oldDependency)}, models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone)
	}
	for _, depID := range r.newDependencies {
		if depID == task.ID {
			return false, &PreconditionError{Reason: fmt.Sprintf("task %s cannot depend on itself", task.ID)}
		}
		if state.FindTask(depID) == nil {
			return false, &PreconditionError{Reason: fmt.Sprintf("new dependency %q does not exist", depID)}
		}
	}
	return lineageRepair, nil
}

// planRewrite replaces the old edge and canonicalises the list, refusing an
// empty result and an executing consumer that would gain an unmet dependency.
func (r *retargetRun) planRewrite(state *models.State, task *models.Task) ([]string, error) {
	replaced := replaceDependency(task.DependsOn, r.oldDependency, r.newDependencies)
	canonical, _, err := canonicalizeConcreteDependencyList(state, r.resolver, task.ID, task.RolePair, replaced)
	if err != nil {
		return nil, err
	}
	if len(canonical) == 0 {
		return nil, &PreconditionError{Reason: "retarget-dependency cannot remove all dependencies; use an explicit dependency-removal operation instead"}
	}
	if err := rejectUnmetDependencyOnExecutingConsumer(state, r.resolver, retargetDependencyOperation, task, canonical); err != nil {
		return nil, err
	}
	return canonical, nil
}

// adopt clears a matching repair request, assigns the canonical list and
// appends the audit entry.
func (r *retargetRun) adopt(task *models.Task, canonical []string, lineageRepair bool) {
	repairExtra := matchingRetargetRepairExtra(task.RepairRequest, r.taskID, r.oldDependency, canonical)
	r.repairRequestCleared = repairExtra != nil
	if r.repairRequestCleared {
		task.RepairRequest = nil
	}

	task.DependsOn = canonical
	extra := map[string]any{
		"manual":                 true,
		"operation":              retargetDependencyOperation,
		"old_dependency":         r.oldDependency,
		"new_dependencies":       append([]string(nil), r.newDependencies...),
		"canonical_dependencies": append([]string(nil), canonical...),
		"repair_request_cleared": r.repairRequestCleared,
	}
	if lineageRepair {
		extra["merged_plan_lineage_repair"] = true
	}
	for key, value := range repairExtra {
		extra[key] = value
	}
	note := fmt.Sprintf("retargeted dependency %s -> %s", r.oldDependency, strings.Join(r.newDependencies, ", "))
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:   r.now,
		Event:  models.TaskEventDependenciesRewritten,
		Agent:  &r.agentID,
		Reason: &r.reason,
		Note:   &note,
		Extra:  extra,
	})
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
