package ops

import (
	"fmt"
	"slices"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// inheritedDepSet is one transition's phase-gate dependencies, retained per
// upstream task so that a child can narrow them.
//
// The whole-phase set (all) is what every child inherited before selective
// inputs existed and is still what a child inherits when it declares no
// intent. byUpstream is indexed by upstream task ID, and within an upstream by
// that upstream's own output[] position — which is what an InputSelection
// names, since the upstream's children usually do not exist at authoring time.
type inheritedDepSet struct {
	all        []string
	byUpstream map[string][]string
	// selectable is false for cardinalities that produce a single edge per
	// upstream. There is no fan-out to narrow there, so a selection is inert
	// rather than an error: inheriting the whole edge is a superset.
	selectable bool
}

// forEntry returns the dependencies one generated child inherits.
//
// Fails closed. An unresolvable or out-of-range selection errors the
// transition rather than yielding a smaller dependency set, because a planner
// naming a prerequisite that cannot be resolved is a defect, and silently
// dropping it produces a child that runs without inputs it declared.
func (d inheritedDepSet) forEntry(entry models.OutputEntry, entryIndex int) ([]string, error) {
	if !entry.InheritInputs.IsSelective() || !d.selectable {
		return d.all, nil
	}
	if entry.InheritInputs.InheritsNothing() {
		return nil, nil
	}

	var selected []string
	for selectionIndex, selection := range entry.InheritInputs.Selections {
		children, known := d.byUpstream[selection.UpstreamTask]
		if !known {
			// The upstream contributed no children to this transition: it is
			// absent from depends_on, or it was replanned. Either way the
			// named prerequisite does not exist and cannot be waited for.
			return nil, fmt.Errorf(
				"output[%d].inherit_inputs.selections[%d]: upstream task %q supplies no inherited children for this transition; "+
					"it is not a dependency of the producing task, or its output was replaced",
				entryIndex, selectionIndex, selection.UpstreamTask)
		}
		for _, output := range selection.Outputs {
			if output >= len(children) {
				return nil, fmt.Errorf(
					"output[%d].inherit_inputs.selections[%d]: upstream task %q has %d output(s), cannot select index %d",
					entryIndex, selectionIndex, selection.UpstreamTask, len(children), output)
			}
			selected = append(selected, children[output])
		}
	}
	return selected, nil
}

// computeInheritedDeps derives phase-gate dependencies from upstream dependency tasks.
// For each task in task.DependsOn that has executed the same transition, it collects
// the child task IDs that were created by that transition. These become additional
// DependsOn entries on downstream children, enforcing phase-gate ordering.
//
// Replanned dependencies are skipped: their executed-transition markers are
// suppressive metadata rather than evidence of materialized children.
// Different direct/legacy architecture routes cannot supply the same phase
// barrier. Such inheritance requires explicit concrete writer ordering instead.
//
// Returns error if a non-replanned upstream transition is marked executed but
// expected children are missing (crash inconsistency that must be recovered first).
func computeInheritedDeps(s *models.State, task *models.Task, transitionName string, resolver *pipeline.Resolver) (inheritedDepSet, error) {
	td, err := resolver.Transition(transitionName)
	if err != nil {
		return inheritedDepSet{}, fmt.Errorf("cannot compute inherited deps: unknown transition %q: %w", transitionName, err)
	}
	slug := td.TaskSlugOrName()

	set := inheritedDepSet{
		byUpstream: map[string][]string{},
		selectable: td.Cardinality == models.CardinalityPerSubtask,
	}
	for _, depID := range task.DependsOn {
		depTask := s.FindTask(depID)
		if depTask == nil {
			continue
		}
		// Replanned tasks carry executed-transition markers with no children by
		// design: Replan marks every outbound transition to block it (replan.go
		// "Invalidate old task output and block all outbound transitions"), and
		// the replacement owns the downstream pipeline. Both transition-collection
		// phases already skip them — as a transition source and as a crash-recovery
		// target — so the marker must not be read here as evidence that children
		// were materialized. Without this guard a replanned upstream fails every
		// downstream transition permanently, and the same "replanned" marker
		// excludes it from the recovery pass that would otherwise repair it.
		// See GH #137.
		if depTask.TransitionsExecuted[models.ReplannedTransition] {
			continue
		}
		if err := refuseCrossRouteInheritance(task, depTask, depID, transitionName, resolver); err != nil {
			return inheritedDepSet{}, err
		}
		if !depTask.TransitionsExecuted[transitionName] {
			continue
		}
		if err := set.addUpstream(s, task, depTask, td, slug, transitionName); err != nil {
			return inheritedDepSet{}, err
		}
	}
	return set, nil
}

// refuseCrossRouteInheritance refuses a phase barrier between architecture
// tasks whose allocation routes differ: code-planning completion does not
// establish direct-coding writer completion.
func refuseCrossRouteInheritance(task, depTask *models.Task, depID, transitionName string, resolver *pipeline.Resolver) error {
	if task.EffectiveType() != models.TaskTypeArchitecture || depTask.EffectiveType() != models.TaskTypeArchitecture ||
		(!models.HasCodingAllocation(task.Output) && !models.HasCodingAllocation(depTask.Output)) {
		return nil
	}
	for _, upstreamRoute := range resolver.AllTransitions() {
		sourcePair, err := resolver.TransitionSourceRolePair(upstreamRoute.Name)
		if err != nil || sourcePair != depTask.RolePair || upstreamRoute.Name == transitionName ||
			!resolver.TransitionApplies(upstreamRoute.Name, depTask.Output) {
			continue
		}
		for i, entry := range task.Output {
			if entry.InheritInputs.IsSelective() {
				if _, selected := entry.InheritInputs.SelectionFor(depID); !selected {
					continue
				}
			}
			return fmt.Errorf("output[%d] cannot inherit prerequisite %q across different architecture allocation routes %q and %q; declare explicit task_depends_on on the prerequisite writer tasks and use inherit_inputs mode none (or select only compatible upstreams); code-planning completion does not establish writer completion", i, depID, upstreamRoute.Name, transitionName)
		}
	}
	return nil
}

// addUpstream records the children one upstream task generated for this
// transition. A missing child is a crash inconsistency that must be recovered
// before the downstream transition can run.
func (set *inheritedDepSet) addUpstream(s *models.State, task, depTask *models.Task, td *pipeline.TransitionDef, slug, transitionName string) error {
	depID := depTask.ID
	missing := func(childID string) error {
		return fmt.Errorf("upstream task %s has transition %q executed but child %s missing (needs crash recovery)", depID, transitionName, childID)
	}
	switch td.Cardinality {
	case models.CardinalityPerSubtask:
		for i := range depTask.Output {
			childID := perSubtaskChildID(depID, slug, i)
			if s.FindTask(childID) == nil {
				return missing(childID)
			}
			// Recorded by upstream output position: that is what an
			// InputSelection names.
			set.byUpstream[depID] = append(set.byUpstream[depID], childID)
			set.all = append(set.all, childID)
		}
	case models.CardinalityOneToOne:
		childID := oneToOneChildID(depID, slug)
		if s.FindTask(childID) == nil {
			return missing(childID)
		}
		set.byUpstream[depID] = append(set.byUpstream[depID], childID)
		set.all = append(set.all, childID)
	case models.CardinalityManyToOne:
		cohortParentID := depTask.CohortParentID()
		// Skip intra-cohort deps: when the dep task shares the same cohort
		// parent, the many-to-one child is the same task being created,
		// which would produce a circular self-dependency.
		if cohortParentID == "" || cohortParentID == task.CohortParentID() {
			return nil
		}
		childID := manyToOneChildID(cohortParentID, slug)
		if s.FindTask(childID) == nil {
			return missing(childID)
		}
		if !slices.Contains(set.all, childID) {
			set.byUpstream[depID] = append(set.byUpstream[depID], childID)
			set.all = append(set.all, childID)
		}
	}
	return nil
}
