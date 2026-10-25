package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

type dependencyPatch struct {
	taskID                 string
	dependsOn              []string
	providerDependencies   []models.ProviderDependency
	descendantDependencies []models.DescendantDependency
}

// perSubtaskRecovery is the outcome of comparing a per-subtask transition's
// output with the children that survive a crash: the entries without a child,
// the patches the surviving children need, and whether a patch restores
// provider metadata (which makes the recovery itself the result).
type perSubtaskRecovery struct {
	missing                   []int
	patches                   []dependencyPatch
	providerMetadataRecovered bool
}

// recoverCrashedTransition handles crash recovery when a transition was already
// marked as executed but some child tasks are missing. Returns
// errTransitionAlreadyExecuted if all children already exist.
func recoverCrashedTransition(s *models.State, task *models.Task, taskID, transitionName string, tDef transitionDef, inheritedDeps inheritedDepSet, resolver *pipeline.Resolver, now time.Time, result *ProceedResult) error {
	if task.PlanGenerationFenced() {
		return fmt.Errorf("task %q is fenced by a reviewed plan amendment", task.ID)
	}
	switch tDef.cardinality {
	case models.CardinalityPerSubtask:
		return recoverPerSubtask(s, task, transitionName, tDef, inheritedDeps, resolver, now, result)
	case models.CardinalityOneToOne:
		return recoverOneToOne(s, task, transitionName, tDef, inheritedDeps, resolver, now, result)
	case models.CardinalityManyToOne:
		return recoverManyToOne(s, task, transitionName, tDef, inheritedDeps, resolver, now, result)
	default:
		return fmt.Errorf("%w: %q on task %q", errTransitionAlreadyExecuted, transitionName, taskID)
	}
}

// crashRecoveryEntry is the history entry of a recovery that recreated children.
func crashRecoveryEntry(now time.Time, transitionName string, recovered int, extra map[string]any) models.TaskHistoryEntry {
	if extra == nil {
		extra = map[string]any{}
	}
	extra["transition"] = transitionName
	extra["recovered_children"] = recovered
	return models.TaskHistoryEntry{Time: now, Event: models.TaskEventTransitionCrashRecov, Extra: extra}
}

// recoverPerSubtask recreates the missing children of a per-subtask transition
// and patches the surviving ones, deciding everything before any state changes.
func recoverPerSubtask(s *models.State, task *models.Task, transitionName string, tDef transitionDef, inheritedDeps inheritedDepSet, resolver *pipeline.Resolver, now time.Time, result *ProceedResult) error {
	canonicalOutput, outputChanged, err := canonicalizedOutputTaskDependsOnForTarget(s, resolver, task, tDef.targetRolePair)
	if err != nil {
		return err
	}
	if err := refuseDescendantAndRCA(resolver, task, canonicalOutput, transitionName); err != nil {
		return err
	}
	// Re-compute dedup decision against current repo-wide state. For skipped
	// entries, siblings.ids[i] must point at the foreign incumbent so downstream
	// dep resolution remaps correctly — mirrors fresh generation.
	siblings, err := planOutputSiblings(s, canonicalOutput, task.ID, tDef.taskSlug)
	if err != nil {
		return err
	}
	result.replacements, result.recovering = siblings.replacements, true
	gen := perSubtaskChildren{
		s: s, resolver: resolver, task: task, tDef: tDef, inheritedDeps: inheritedDeps, output: canonicalOutput, siblings: siblings,
		selectedNotes: selectsValidationNotes(resolver, task, transitionName), now: now,
	}
	recovery, err := gen.classifyRecovery()
	if err != nil {
		return err
	}
	if len(recovery.missing) == 0 {
		applyDependencyPatches(s, recovery.patches)
		if recovery.providerMetadataRecovered {
			return nil
		}
		return fmt.Errorf("%w: %q on task %q", errTransitionAlreadyExecuted, transitionName, task.ID)
	}
	children, err := gen.recreate(recovery.missing)
	if err != nil {
		return err
	}
	if outputChanged {
		task.Output = canonicalOutput
	}
	applyDependencyPatches(s, recovery.patches)
	taskID := task.ID
	for _, child := range children {
		s.Tasks = append(s.Tasks, child)
		result.ChildTaskIDs = append(result.ChildTaskIDs, child.ID)
	}
	// Re-fetch: the appends above may have reallocated s.Tasks, leaving
	// the incoming task pointer stale.
	task = s.FindTask(taskID)
	task.History = append(task.History, crashRecoveryEntry(now, transitionName, len(recovery.missing), nil))
	return nil
}

// classifyRecovery walks the output and decides, per entry, whether its child
// is missing or survives and needs a patch. Foreign Kind incumbents stay
// untouched; deduplication that points at our own child still recovers that
// child's missing declaration.
func (g perSubtaskChildren) classifyRecovery() (perSubtaskRecovery, error) {
	var recovery perSubtaskRecovery
	for i := range g.output {
		if _, skipped := g.siblings.skipped[i]; skipped {
			if g.siblings.ids[i] != perSubtaskChildID(g.task.ID, g.tDef.taskSlug, i) {
				if err := g.refuseDeduplicatedNotes(i); err != nil {
					return perSubtaskRecovery{}, err
				}
				continue
			}
			own := g.s.FindTask(g.siblings.ids[i])
			if own == nil || own.Status.IsTerminal() || models.GeneratedDeclarationsMatch(own, g.output[i]) {
				continue
			}
		}
		existing := g.s.FindTask(g.siblings.ids[i])
		if existing == nil {
			recovery.missing = append(recovery.missing, i)
			continue
		}
		patch, metadataRecovered, err := g.patchSurvivor(i, existing)
		if err != nil {
			return perSubtaskRecovery{}, err
		}
		recovery.patches = append(recovery.patches, patch)
		recovery.providerMetadataRecovered = recovery.providerMetadataRecovered || metadataRecovered
	}
	return recovery, nil
}

// patchSurvivor decides the dependency and declaration patch of a surviving
// child. It inherits exactly what the uninterrupted generation would have given
// it: merging the whole-phase set would silently widen a selective child back to
// the full barrier and leave it inconsistent with siblings recreated through
// the same inherited step.
func (g perSubtaskChildren) patchSurvivor(i int, existing *models.Task) (dependencyPatch, bool, error) {
	canonicalDeps, _, err := canonicalizeConcreteDependencyList(g.s, g.resolver, existing.ID, existing.RolePair, existing.DependsOn)
	if err != nil {
		return dependencyPatch{}, false, err
	}
	entryInherited, err := g.inherited(i)
	if err != nil {
		return dependencyPatch{}, false, err
	}
	mergedDeps, _, err := canonicalizeChildDependsOn(g.s, g.resolver, existing.ID, existing.RolePair, mergeInheritedDeps(canonicalDeps, entryInherited), stringSet(g.siblings.ids))
	if err != nil {
		return dependencyPatch{}, false, err
	}
	if err := validateDependencyDirection(g.s, g.resolver, existing.ID, existing.RolePair, mergedDeps); err != nil {
		return dependencyPatch{}, false, err
	}
	wanted := g.output[i]
	declared := len(existing.ProviderDependencies) > 0 || len(existing.DescendantDependencies) > 0
	if len(wanted.ProviderDependencies) > 0 || len(wanted.DescendantDependencies) > 0 || declared {
		parents := existing.EffectiveParentTasks()
		if existing.RolePair != g.tDef.targetRolePair || len(parents) != 1 || parents[0] != g.task.ID {
			return dependencyPatch{}, false, fmt.Errorf("cannot recover provider_dependencies on child %s with incorrect transition provenance", existing.ID)
		}
	}
	// A matching child keeps its own declarations: an authorized
	// deferral (ADR-0193) must not be re-imposed, claimed or not.
	patch := dependencyPatch{taskID: existing.ID, dependsOn: mergedDeps,
		providerDependencies:   models.CloneProviderDependencies(existing.ProviderDependencies),
		descendantDependencies: models.CloneDescendantDependencies(existing.DescendantDependencies)}
	if models.GeneratedDeclarationsMatch(existing, wanted) {
		return patch, false, nil
	}
	unclaimedInitial := existing.Status == g.tDef.targetStatus && existing.AssignedTo == nil && existing.LeaseExpires == nil
	if declared || !unclaimedInitial {
		return dependencyPatch{}, false, fmt.Errorf("cannot recover provider_dependencies on conflicting or claimed child %s", existing.ID)
	}
	patch.providerDependencies = models.CloneProviderDependencies(wanted.ProviderDependencies)
	patch.descendantDependencies = models.CloneDescendantDependencies(wanted.DescendantDependencies)
	return patch, true, nil
}

// recreate rebuilds the missing children through the same inherited and
// validated-child steps as fresh generation, so a repaired child's depends_on
// is identical to the one the uninterrupted transition would have produced.
func (g perSubtaskChildren) recreate(missing []int) ([]models.Task, error) {
	var children []models.Task
	for _, i := range missing {
		inherited, err := g.inherited(i)
		if err != nil {
			return nil, err
		}
		child, err := g.child(i, inherited)
		if err != nil {
			return nil, err
		}
		children = append(children, child)
	}
	return children, nil
}

// applyDependencyPatches adopts the decided patches on the surviving children.
func applyDependencyPatches(s *models.State, patches []dependencyPatch) {
	for _, patch := range patches {
		if existing := s.FindTask(patch.taskID); existing != nil {
			existing.DependsOn = patch.dependsOn
			existing.ProviderDependencies = patch.providerDependencies
			existing.DescendantDependencies = patch.descendantDependencies
		}
	}
}

// recoverOneToOne recreates the single child of a one-to-one transition, or
// repairs its dependencies when it already exists.
func recoverOneToOne(s *models.State, task *models.Task, transitionName string, tDef transitionDef, inheritedDeps inheritedDepSet, resolver *pipeline.Resolver, now time.Time, result *ProceedResult) error {
	childID := oneToOneChildID(task.ID, tDef.taskSlug)
	if existing := s.FindTask(childID); existing != nil {
		deps, err := repairedChildDeps(s, resolver, existing, inheritedDeps.all)
		if err != nil {
			return err
		}
		existing.DependsOn = deps
		return fmt.Errorf("%w: %q on task %q", errTransitionAlreadyExecuted, transitionName, task.ID)
	}
	child, err := validatedChild(s, resolver, buildOneToOneChild(childID, task.ID, task, tDef, inheritedDeps.all, now))
	if err != nil {
		return err
	}
	s.Tasks = append(s.Tasks, child)
	result.ChildTaskIDs = append(result.ChildTaskIDs, childID)
	task.History = append(task.History, crashRecoveryEntry(now, transitionName, 1, nil))
	return nil
}

// recoverManyToOne recreates the consolidation child of a cohort, or repairs the
// existing child and the cohort markers.
func recoverManyToOne(s *models.State, task *models.Task, transitionName string, tDef transitionDef, inheritedDeps inheritedDepSet, resolver *pipeline.Resolver, now time.Time, result *ProceedResult) error {
	cohort, sharedParentID, err := findManyToOneCohort(s, task)
	if err != nil {
		return fmt.Errorf("crash recovery cohort detection: %w", err)
	}
	childID := manyToOneChildID(sharedParentID, tDef.taskSlug)
	if existing := s.FindTask(childID); existing != nil {
		return repairExistingCohortChild(s, resolver, existing, inheritedDeps, cohort, transitionName, sharedParentID)
	}
	child, err := newCohortChild(s, resolver, cohort, sharedParentID, tDef, inheritedDeps, now)
	if err != nil {
		return err
	}
	s.Tasks = append(s.Tasks, child)
	result.ChildTaskIDs = append(result.ChildTaskIDs, childID)
	markCohortExecuted(cohort, transitionName)
	task.History = append(task.History, crashRecoveryEntry(now, transitionName, 1, map[string]any{
		"cardinality":   string(models.CardinalityManyToOne),
		"cohort_parent": sharedParentID,
	}))
	return nil
}

// isTransitionIncomplete checks if an executed transition has missing children.
// Used to detect crash recovery needs in ExecuteAvailableTransitions phase 1b.
func isTransitionIncomplete(s *models.State, task *models.Task, transName string, resolver *pipeline.Resolver) bool {
	if !resolver.TransitionApplies(transName, task.Output) {
		return false
	}
	if task.PlanHandoffRetired() {
		return false
	}
	td, err := resolver.Transition(transName)
	if err != nil {
		return false
	}
	slug := td.TaskSlugOrName()
	switch td.Cardinality {
	case models.CardinalityPerSubtask:
		for i := 0; i < len(task.Output); i++ {
			child := s.FindTask(perSubtaskChildID(task.ID, slug, i))
			if child == nil || (!child.Status.IsTerminal() && !models.GeneratedDeclarationsMatch(child, task.Output[i])) {
				return true
			}
		}
	case models.CardinalityOneToOne:
		if s.FindTask(oneToOneChildID(task.ID, slug)) == nil {
			return true
		}
	case models.CardinalityManyToOne:
		cohortParentID := task.CohortParentID()
		if cohortParentID == "" {
			return false
		}
		if s.FindTask(manyToOneChildID(task.ID, slug)) == nil &&
			s.FindTask(manyToOneChildID(cohortParentID, slug)) == nil {
			return true
		}
	}
	return false
}
