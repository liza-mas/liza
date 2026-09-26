package ops

import (
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

func rewriteActiveDependents(state *models.State, resolver *pipeline.Resolver, targetID string, replacements []string, agentID string, now time.Time) error {
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.ID == targetID {
			continue
		}

		depsChanged := false
		if !task.Status.IsTerminal() {
			rewritten, changed, err := canonicalizeConcreteDependencyList(state, resolver, task.ID, task.RolePair, task.DependsOn)
			if err != nil {
				return err
			}
			if changed {
				task.DependsOn = rewritten
				depsChanged = true
			}
		}

		outputChanged := false
		if operationalOutputMayBeConsumed(state, resolver, task) {
			changed, err := canonicalizeOutputTaskDependsOnForConsumers(state, resolver, task)
			if err != nil {
				return err
			}
			outputChanged = changed
		}

		if !depsChanged && !outputChanged {
			continue
		}

		note := fmt.Sprintf("rewrote dependencies after %s became terminal", targetID)
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventDependenciesRewritten,
			Agent: &agentID,
			Note:  &note,
			Extra: map[string]any{
				"removed_dependency":       targetID,
				"replacement_dependencies": replacements,
				"rewrote_depends_on":       depsChanged,
				"rewrote_task_depends_on":  outputChanged,
			},
		})
	}
	return nil
}

func pruneDownstreamDependencies(state *models.State, resolver *pipeline.Resolver, task *models.Task) ([]string, []string, error) {
	if task == nil {
		return nil, nil, nil
	}
	if state == nil || resolver == nil || task.RolePair == "" {
		return append([]string(nil), task.DependsOn...), nil, nil
	}

	retained := make([]string, 0, len(task.DependsOn))
	removed := make([]string, 0, len(task.DependsOn))
	depResolver := models.NewDependencyResolver(state)
	for _, depID := range task.DependsOn {
		path := depResolver.Resolve(depID).Path
		if len(path) == 0 {
			path = []string{depID}
		}
		resolvesDownstream := false
		for _, pathID := range path {
			depTask := state.FindTask(pathID)
			if depTask == nil || depTask.RolePair == "" {
				continue
			}
			downstream, err := resolver.IsRolePairDownstream(task.RolePair, depTask.RolePair)
			if err != nil {
				return nil, nil, err
			}
			if downstream {
				resolvesDownstream = true
				break
			}
		}
		if resolvesDownstream {
			removed = append(removed, depID)
			continue
		}
		retained = append(retained, depID)
	}
	return retained, removed, nil
}

func canonicalizeTaskDependsOnForTransition(state *models.State, resolver *pipeline.Resolver, task *models.Task) error {
	if task == nil || len(task.DependsOn) == 0 {
		return nil
	}
	rewritten, changed, err := canonicalizeConcreteDependencyList(state, resolver, task.ID, task.RolePair, task.DependsOn)
	if err != nil {
		return err
	}
	if changed {
		task.DependsOn = rewritten
	}
	return nil
}

func canonicalizedOutputTaskDependsOnForTarget(state *models.State, resolver *pipeline.Resolver, task *models.Task, targetRolePair string) ([]models.OutputEntry, bool, error) {
	if task == nil || len(task.Output) == 0 {
		return nil, false, nil
	}
	output := append([]models.OutputEntry(nil), task.Output...)
	changedAny := false
	for i := range output {
		dependentID := fmt.Sprintf("%s output[%d]", task.ID, i)
		rewritten, changed, err := canonicalizeConcreteDependencyList(state, resolver, dependentID, targetRolePair, output[i].TaskDependsOn)
		if err != nil {
			return nil, false, err
		}
		if changed {
			output[i].TaskDependsOn = rewritten
			changedAny = true
		}
	}
	return output, changedAny, nil
}

func canonicalizeOutputTaskDependsOnForConsumers(state *models.State, resolver *pipeline.Resolver, task *models.Task) (bool, error) {
	consumerRolePairs, err := resolver.OutputConsumerRolePairs(task.RolePair)
	if err != nil {
		return false, err
	}
	changedAny := false
	for i := range task.Output {
		dependentID := fmt.Sprintf("%s output[%d]", task.ID, i)
		rewritten := task.Output[i].TaskDependsOn
		entryChanged := false
		for _, consumerRolePair := range consumerRolePairs {
			var changed bool
			var err error
			rewritten, changed, err = canonicalizeConcreteDependencyList(state, resolver, dependentID, consumerRolePair, rewritten)
			if err != nil {
				return false, err
			}
			entryChanged = entryChanged || changed
		}
		if entryChanged {
			for _, consumerRolePair := range consumerRolePairs {
				if err := validateDependencyDirection(state, resolver, dependentID, consumerRolePair, rewritten); err != nil {
					return false, err
				}
			}
			task.Output[i].TaskDependsOn = rewritten
			changedAny = true
		}
	}
	return changedAny, nil
}

func canonicalizeConcreteDependencyList(state *models.State, resolver *pipeline.Resolver, dependentID, dependentRolePair string, deps []string) ([]string, bool, error) {
	if len(deps) == 0 {
		return deps, false, nil
	}
	rewritten := make([]string, 0, len(deps))
	changed := false
	for _, dep := range deps {
		canonical, depChanged, err := canonicalizeDependencyID(state, dep, nil)
		if err != nil {
			return nil, false, &PreconditionError{Reason: fmt.Sprintf("cannot rewrite dependency for %s: %v", dependentID, err)}
		}
		changed = changed || depChanged
		rewritten = append(rewritten, canonical...)
	}
	if !changed {
		return deps, false, nil
	}
	rewritten = dedupeStrings(rewritten)
	if err := validateDependencyDirection(state, resolver, dependentID, dependentRolePair, rewritten); err != nil {
		return nil, false, err
	}
	return rewritten, true, nil
}

func canonicalizeChildDependsOn(state *models.State, resolver *pipeline.Resolver, dependentID, dependentRolePair string, deps []string, allowedMissing ...map[string]bool) ([]string, bool, error) {
	if state == nil || resolver == nil || len(deps) == 0 {
		return deps, false, nil
	}
	var allowedMissingDeps map[string]bool
	if len(allowedMissing) > 0 {
		allowedMissingDeps = allowedMissing[0]
	}
	rewritten := make([]string, 0, len(deps))
	changed := false
	for _, depID := range deps {
		candidates, depChanged, err := canonicalizeDependencyIDForChild(state, depID, allowedMissingDeps, nil)
		if err != nil {
			return nil, false, &PreconditionError{Reason: fmt.Sprintf("cannot rewrite dependency for %s: %v", dependentID, err)}
		}
		changed = changed || depChanged
		if !depChanged {
			for _, candidate := range candidates {
				rewritten = append(rewritten, candidate.id)
			}
			continue
		}
		for _, candidate := range candidates {
			if err := validateDependencyDirection(state, resolver, dependentID, dependentRolePair, []string{candidate.id}); err != nil {
				if candidate.satisfied {
					continue
				}
				return nil, false, err
			}
			rewritten = append(rewritten, candidate.id)
		}
	}
	rewritten = dedupeStrings(rewritten)
	if err := validateDependencyDirection(state, resolver, dependentID, dependentRolePair, rewritten); err != nil {
		return nil, false, err
	}
	return rewritten, changed, nil
}

type childDependencyCandidate struct {
	id        string
	satisfied bool
}

func canonicalizeDependencyIDForChild(state *models.State, depID string, allowedMissing map[string]bool, visiting map[string]bool) ([]childDependencyCandidate, bool, error) {
	depTask := state.FindTask(depID)
	if depTask == nil {
		if allowedMissing[depID] {
			return []childDependencyCandidate{{id: depID}}, false, nil
		}
		return nil, false, fmt.Errorf("dependency %s does not exist", depID)
	}
	switch depTask.Status {
	case models.TaskStatusMerged:
		return []childDependencyCandidate{{id: depID, satisfied: true}}, false, nil
	case models.TaskStatusAbandoned:
		return nil, true, nil
	case models.TaskStatusSuperseded:
		if visiting == nil {
			visiting = make(map[string]bool)
		}
		if visiting[depID] {
			return nil, false, fmt.Errorf("supersession cycle at %s", depID)
		}
		if len(depTask.SupersededBy) == 0 {
			return nil, true, nil
		}
		visiting[depID] = true
		var rewritten []childDependencyCandidate
		for _, replacementID := range depTask.SupersededBy {
			if state.FindTask(replacementID) == nil {
				return nil, false, fmt.Errorf("replacement dependency %s does not exist", replacementID)
			}
			canonical, _, err := canonicalizeDependencyIDForChild(state, replacementID, allowedMissing, visiting)
			if err != nil {
				return nil, false, err
			}
			rewritten = append(rewritten, canonical...)
		}
		delete(visiting, depID)
		return dedupeChildDependencyCandidates(rewritten), true, nil
	default:
		return []childDependencyCandidate{{id: depID}}, false, nil
	}
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func dedupeChildDependencyCandidates(candidates []childDependencyCandidate) []childDependencyCandidate {
	if len(candidates) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(candidates))
	deduped := make([]childDependencyCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if seen[candidate.id] {
			continue
		}
		seen[candidate.id] = true
		deduped = append(deduped, candidate)
	}
	return deduped
}

func canonicalizeDependencyID(state *models.State, depID string, visiting map[string]bool) ([]string, bool, error) {
	if state == nil {
		return []string{depID}, false, nil
	}
	depTask := state.FindTask(depID)
	if depTask == nil {
		return []string{depID}, false, nil
	}
	switch depTask.Status {
	case models.TaskStatusAbandoned:
		return nil, true, nil
	case models.TaskStatusSuperseded:
		if visiting == nil {
			visiting = make(map[string]bool)
		}
		if visiting[depID] {
			return nil, false, fmt.Errorf("supersession cycle at %s", depID)
		}
		if len(depTask.SupersededBy) == 0 {
			return nil, true, nil
		}
		visiting[depID] = true
		var rewritten []string
		for _, replacementID := range depTask.SupersededBy {
			if state.FindTask(replacementID) == nil {
				return nil, false, fmt.Errorf("replacement dependency %s does not exist", replacementID)
			}
			canonical, _, err := canonicalizeDependencyID(state, replacementID, visiting)
			if err != nil {
				return nil, false, err
			}
			rewritten = append(rewritten, canonical...)
		}
		delete(visiting, depID)
		return dedupeStrings(rewritten), true, nil
	default:
		return []string{depID}, false, nil
	}
}

func operationalOutputMayBeConsumed(state *models.State, resolver *pipeline.Resolver, task *models.Task) bool {
	if task == nil || len(task.Output) == 0 || task.Status == models.TaskStatusSuperseded || task.Status == models.TaskStatusAbandoned {
		return false
	}
	if task.Status != models.TaskStatusMerged {
		return true
	}
	for _, transition := range resolver.AllTransitions() {
		if transition.Cardinality != "per-subtask" {
			continue
		}
		sourceRolePair, err := resolver.TransitionSourceRolePair(transition.Name)
		if err != nil || sourceRolePair != task.RolePair {
			continue
		}
		if !task.TransitionsExecuted[transition.Name] {
			return true
		}
		slug := transition.TaskSlugOrName()
		for i := range task.Output {
			if state.FindTask(perSubtaskChildID(task.ID, slug, i)) == nil {
				return true
			}
		}
	}
	return false
}

// rejectUnmetDependencyOnExecutingConsumer refuses a dependency update that
// would give an executing task an unmet dependency (D13). State validation
// rejects that candidate too, but only after mutation and with a class that
// invites a correction or retry which cannot succeed while the task executes.
// The executing set is every role pair's, as in the state invariant.
func rejectUnmetDependencyOnExecutingConsumer(state *models.State, resolver *pipeline.Resolver, operation string, task *models.Task, dependencies []string) error {
	if resolver == nil {
		return nil
	}
	var executing []models.TaskStatus
	for _, rolePair := range resolver.RolePairNames() {
		if status, err := resolver.ExecutingStatus(rolePair); err == nil {
			executing = append(executing, status)
		}
	}
	if !isExecutingStatus(task.Status, executing) {
		return nil
	}
	depResolver := models.NewDependencyResolver(state)
	var unmet []string
	for _, dependency := range dependencies {
		if !depResolver.Resolve(dependency).Satisfied() {
			unmet = append(unmet, dependency)
		}
	}
	if len(unmet) == 0 {
		return nil
	}
	return WrapLifecycleError(operation, task, &PreconditionError{
		Reason: fmt.Sprintf("task %s is executing (%s): a dependency update cannot add unmet dependencies %s; wait until it leaves execution or block it first", task.ID, task.Status, strings.Join(unmet, ", ")),
		Details: map[string]any{
			"prerequisite":       "consumer_not_executing",
			"task_id":            task.ID,
			"status":             string(task.Status),
			"unmet_dependencies": unmet,
		},
	}, models.LifecycleAlreadyTransitioned, "stop", "none")
}
