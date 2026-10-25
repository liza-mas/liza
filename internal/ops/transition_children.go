package ops

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

// perSubtaskChildID returns the deterministic child task ID for a per-subtask transition.
func perSubtaskChildID(parentID, transitionName string, index int) string {
	return fmt.Sprintf("%s-%s-%d", parentID, transitionName, index)
}

// oneToOneChildID returns the deterministic child task ID for a one-to-one transition.
func oneToOneChildID(parentID, transitionName string) string {
	return fmt.Sprintf("%s-%s", parentID, transitionName)
}

// manyToOneChildID returns the deterministic child task ID for a many-to-one transition.
// The cohort's shared parent ID is used as the anchor since all cohort members
// are siblings created from the same upstream task.
func manyToOneChildID(cohortParentID, transitionName string) string {
	return fmt.Sprintf("%s-%s", cohortParentID, transitionName)
}

// transitionGeneration is what a fresh transition generates before the
// transition marker, children and history are recorded.
type transitionGeneration struct {
	output        []models.OutputEntry
	outputChanged bool
	children      []models.Task
	siblings      outputSiblings
}

// outputSiblings is the kind-deduplication and replacement-grouping decision
// for a per-subtask output. ids holds the effective sibling ID of every entry;
// skipped and remapped are keyed by the entries kind deduplication removed.
type outputSiblings struct {
	retiring     map[string]bool
	ids          []string
	skipped      map[int]string
	remapped     map[int]string
	replacements *planReplacements
}

// planOutputSiblings decides, against the repo-wide in-flight tasks, which
// entries are deduplicated and which originals their replacements retire.
func planOutputSiblings(s *models.State, output []models.OutputEntry, taskID, taskSlug string) (outputSiblings, error) {
	retiring := declaredOriginals(output)
	ids, skipped, remapped := resolvePerSubtaskSiblings(output, collectNonTerminalByKind(s, retiring), taskID, taskSlug)
	replacements, err := groupPlanReplacements(output, ids, skipped)
	if err != nil {
		return outputSiblings{}, err
	}
	return outputSiblings{retiring: retiring, ids: ids, skipped: skipped, remapped: remapped, replacements: replacements}, nil
}

// perSubtaskChildren constructs the children of one per-subtask transition.
// Fresh generation and crash recovery build every child through it, so a
// recreated child carries what the uninterrupted transition would have given it.
type perSubtaskChildren struct {
	s             *models.State
	resolver      *pipeline.Resolver
	task          *models.Task
	tDef          transitionDef
	inheritedDeps inheritedDepSet
	output        []models.OutputEntry
	siblings      outputSiblings
	selectedNotes bool
	now           time.Time
}

// inherited returns the phase-gate dependencies output[i] inherits: the whole
// upstream phase, or the upstream children its inherit_inputs selection names.
func (g perSubtaskChildren) inherited(i int) ([]string, error) {
	deps, err := g.inheritedDeps.forEntry(g.output[i], i)
	if err != nil {
		return nil, err
	}
	return excludeRetiring(deps, g.output[i], g.siblings.retiring), nil
}

// child builds the validated child of output[i] with the given inherited dependencies.
func (g perSubtaskChildren) child(i int, inherited []string) (models.Task, error) {
	child := buildChildTask(g.siblings.ids[i], g.task.ID, g.output[i], g.tDef.targetStatus, g.tDef.targetRolePair, g.tDef.taskType, g.siblings.ids, inherited, g.task.EpicRef, g.task.ArchRef, g.task.RCARequired, g.now)
	if g.selectedNotes {
		child.ValidationNotes = g.task.ValidationNotesForOutput(i)
	}
	return validatedChild(g.s, g.resolver, child, stringSet(g.siblings.ids))
}

// refuseDeduplicatedNotes refuses a validation note selecting an entry that
// kind deduplication left without a child to receive it.
func (g perSubtaskChildren) refuseDeduplicatedNotes(i int) error {
	if g.selectedNotes && len(g.task.ValidationNotesForOutput(i)) > 0 {
		return fmt.Errorf("validation notes select deduplicated output[%d]; no new child would receive them", i)
	}
	return nil
}

// generate builds one child per entry kind deduplication did not remap. A
// deduplicated entry creates no child; its dependents resolve to the incumbent.
func (g perSubtaskChildren) generate() ([]models.Task, error) {
	var children []models.Task
	for i := range g.output {
		if _, dedupd := g.siblings.remapped[i]; dedupd {
			if err := g.refuseDeduplicatedNotes(i); err != nil {
				return nil, err
			}
			continue
		}
		inherited, err := g.inherited(i)
		if err != nil {
			return nil, &handoffInputError{class: handoffInputRefusal, index: i, err: err}
		}
		child, err := g.child(i, inherited)
		if err != nil {
			return nil, err
		}
		children = append(children, child)
	}
	return children, nil
}

// validatedChild canonicalizes a generated child's depends_on and checks the
// final list against the dependency direction. Every generated child, fresh or
// recreated by crash recovery, passes through it before adoption.
func validatedChild(s *models.State, resolver *pipeline.Resolver, child models.Task, allowedMissing ...map[string]bool) (models.Task, error) {
	canonicalDeps, _, err := canonicalizeChildDependsOn(s, resolver, child.ID, child.RolePair, child.DependsOn, allowedMissing...)
	if err != nil {
		return models.Task{}, err
	}
	child.DependsOn = canonicalDeps
	if err := validateDependencyDirection(s, resolver, child.ID, child.RolePair, child.DependsOn); err != nil {
		return models.Task{}, err
	}
	return child, nil
}

// repairedChildDeps merges the transition's whole-phase dependencies into an
// already created single child (one-to-one or many-to-one) and validates the
// result. The caller adopts the returned list.
func repairedChildDeps(s *models.State, resolver *pipeline.Resolver, existing *models.Task, inherited []string) ([]string, error) {
	canonicalDeps, _, err := canonicalizeChildDependsOn(s, resolver, existing.ID, existing.RolePair, mergeInheritedDeps(existing.DependsOn, inherited))
	if err != nil {
		return nil, err
	}
	if err := validateDependencyDirection(s, resolver, existing.ID, existing.RolePair, canonicalDeps); err != nil {
		return nil, err
	}
	return canonicalDeps, nil
}

// generateTransitionChildren admits the source's output for the transition's
// cardinality and builds its children without touching state.
func generateTransitionChildren(s *models.State, resolver *pipeline.Resolver, task *models.Task, transitionName string, tDef transitionDef, inheritedDeps inheritedDepSet, now time.Time, result *ProceedResult) (transitionGeneration, error) {
	switch tDef.cardinality {
	case models.CardinalityPerSubtask:
		return generatePerSubtask(s, resolver, task, transitionName, tDef, inheritedDeps, now, result)
	case models.CardinalityOneToOne:
		return generateOneToOne(s, resolver, task, transitionName, tDef, inheritedDeps, now)
	default:
		return transitionGeneration{}, fmt.Errorf("unsupported cardinality %q for transition %q", tDef.cardinality, transitionName)
	}
}

func generatePerSubtask(s *models.State, resolver *pipeline.Resolver, task *models.Task, transitionName string, tDef transitionDef, inheritedDeps inheritedDepSet, now time.Time, result *ProceedResult) (transitionGeneration, error) {
	output, outputChanged, err := admitPerSubtaskOutput(s, resolver, task, transitionName, tDef)
	if err != nil {
		return transitionGeneration{}, err
	}
	siblings, err := planOutputSiblings(s, output, task.ID, tDef.taskSlug)
	if err != nil {
		return transitionGeneration{}, err
	}
	result.replacements = siblings.replacements
	children, err := perSubtaskChildren{
		s: s, resolver: resolver, task: task, tDef: tDef, inheritedDeps: inheritedDeps, output: output, siblings: siblings,
		selectedNotes: selectsValidationNotes(resolver, task, transitionName), now: now,
	}.generate()
	if err != nil {
		return transitionGeneration{}, err
	}
	return transitionGeneration{output: output, outputChanged: outputChanged, children: children, siblings: siblings}, nil
}

func generateOneToOne(s *models.State, resolver *pipeline.Resolver, task *models.Task, transitionName string, tDef transitionDef, inheritedDeps inheritedDepSet, now time.Time) (transitionGeneration, error) {
	if task.SpecRef == "" {
		return transitionGeneration{}, fmt.Errorf("task %q has empty spec_ref for one-to-one transition %q", task.ID, transitionName)
	}
	child, err := validatedChild(s, resolver, buildOneToOneChild(oneToOneChildID(task.ID, tDef.taskSlug), task.ID, task, tDef, inheritedDeps.all, now))
	if err != nil {
		return transitionGeneration{}, err
	}
	return transitionGeneration{output: task.Output, children: []models.Task{child}}, nil
}

// admitPerSubtaskOutput refuses an output that cannot generate children and
// returns its canonical form. The refusals keep their order: non-empty output,
// canonical task dependencies, entry shape, stale providers, then the
// descendant and RCA-classification refusals shared with crash recovery.
func admitPerSubtaskOutput(s *models.State, resolver *pipeline.Resolver, task *models.Task, transitionName string, tDef transitionDef) ([]models.OutputEntry, bool, error) {
	if len(task.Output) == 0 {
		return nil, false, fmt.Errorf("task %q has no output[] entries for per-subtask transition %q", task.ID, transitionName)
	}
	canonicalOutput, changed, err := canonicalizedOutputTaskDependsOnForTarget(s, resolver, task, tDef.targetRolePair)
	if err != nil {
		return nil, false, err
	}
	for i, entry := range canonicalOutput {
		if err := validateOutputEntry(entry, i, len(task.Output)); err != nil {
			return nil, false, &handoffInputError{class: handoffOutputRefusal, index: i, err: err}
		}
		if err := refuseStaleProviders(s, resolver, task.ID, i, entry); err != nil {
			return nil, false, err
		}
	}
	if err := refuseDescendantAndRCA(resolver, task, canonicalOutput, transitionName); err != nil {
		return nil, false, err
	}
	return canonicalOutput, changed, nil
}

// refuseStaleProviders stops a stale declaration (ADR-0185, ADR-0186) from
// reaching a generated child.
func refuseStaleProviders(s *models.State, resolver *pipeline.Resolver, taskID string, index int, entry models.OutputEntry) error {
	for _, dep := range entry.ProviderDependencies {
		switch id, child := staleProviderReference(s, resolver, dep); {
		case child:
			return &handoffInputError{class: handoffOutputRefusal, index: index, err: fmt.Errorf(
				"output[%d] declares retired provider child %s; replan %s to re-author it", index, id, taskID)}
		case id != "":
			return &handoffInputError{class: handoffOutputRefusal, index: index, err: fmt.Errorf(
				"output[%d] declares retired provider %s; replan %s to re-author it", index, dep.ProviderTask, taskID)}
		}
	}
	return nil
}

// refuseDescendantAndRCA applies the output refusals generation and crash
// recovery share.
func refuseDescendantAndRCA(resolver *pipeline.Resolver, task *models.Task, output []models.OutputEntry, transitionName string) error {
	if err := descendantOutputRefusal(resolver, task, output, transitionName); err != nil {
		return err
	}
	if resolver != nil {
		return validateDecompositionRootRCAClassification(resolver, task.RolePair, output)
	}
	return nil
}

// recordGeneratedTransition adopts a fresh generation: the canonical output,
// the crash-recovery marker (only after every child validated), the children
// and the transition history entry.
func recordGeneratedTransition(s *models.State, task *models.Task, transitionName string, gen transitionGeneration, now time.Time, result *ProceedResult) {
	taskID := task.ID
	if gen.outputChanged {
		task.Output = gen.output
	}
	if task.TransitionsExecuted == nil {
		task.TransitionsExecuted = make(map[string]bool)
	}
	task.TransitionsExecuted[transitionName] = true
	for _, child := range gen.children {
		s.Tasks = append(s.Tasks, child)
		result.ChildTaskIDs = append(result.ChildTaskIDs, child.ID)
	}

	histExtra := map[string]any{
		"transition": transitionName,
		"children":   len(result.ChildTaskIDs),
	}
	if len(gen.siblings.skipped) > 0 {
		histExtra["skipped_entries"] = skippedEntriesHistory(gen.output, gen.siblings)
	}
	// Re-fetch the task pointer: s.Tasks appends above may have reallocated
	// the backing array, invalidating the original pointer.
	task = s.FindTask(taskID)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventTransitionExecuted,
		Extra: histExtra,
	})
}

// skippedEntriesHistory describes the entries kind deduplication removed, by output index.
func skippedEntriesHistory(output []models.OutputEntry, siblings outputSiblings) []map[string]any {
	skipped := make([]map[string]any, 0, len(siblings.skipped))
	for i, reason := range siblings.skipped {
		skipped = append(skipped, map[string]any{
			"output_index": i,
			"kind":         output[i].Kind,
			"reason":       reason,
			"remapped_to":  siblings.remapped[i],
		})
	}
	slices.SortFunc(skipped, func(a, b map[string]any) int {
		return cmp.Compare(a["output_index"].(int), b["output_index"].(int))
	})
	return skipped
}

// buildChildTask creates a child task from an output entry.
// siblingIDs maps output entry indices to their generated task IDs,
// used to resolve DependsOn index references to actual task IDs.
// inheritedDeps are phase-gate dependencies from upstream tasks' children.
func buildChildTask(childID, parentID string, entry models.OutputEntry, targetStatus models.TaskStatus, targetRolePair string, taskType models.TaskType, siblingIDs, inheritedDeps []string, parentEpicRef, parentArchRef string, parentRCARequired bool, now time.Time) models.Task {
	var deps []string
	for _, ref := range entry.DependsOn {
		idx, _ := strconv.Atoi(ref) // validated upstream in validateOutputEntry
		deps = append(deps, siblingIDs[idx])
	}
	deps = append(deps, entry.TaskDependsOn...)
	deps = append(deps, inheritedDeps...)
	deps = dedupeStrings(deps)

	epicRef := entry.EpicRef
	if epicRef == "" {
		epicRef = parentEpicRef
	}

	archRef := entry.ArchRef
	if archRef == "" {
		archRef = parentArchRef
	}
	rcaRequired := parentRCARequired
	if entry.RCARequired != nil {
		rcaRequired = *entry.RCARequired
	}
	var supersedes *string
	if entry.Supersedes != "" {
		supersedes = &entry.Supersedes
	}

	return models.Task{
		ID:                      childID,
		Type:                    taskType,
		RolePair:                targetRolePair,
		Description:             entry.Desc,
		Status:                  targetStatus,
		Priority:                1,
		ParentTasks:             []string{parentID},
		SpecRef:                 paths.NormalizeSpecRef(entry.SpecRef),
		EpicRef:                 paths.NormalizeSpecRef(epicRef),
		PlanRef:                 paths.NormalizeSpecRef(entry.PlanRef),
		ArchRef:                 paths.NormalizeSpecRef(archRef),
		Decomposition:           entry.Decomposition,
		Kind:                    entry.Kind,
		RCARequired:             rcaRequired,
		DoneWhen:                entry.DoneWhen,
		Validation:              slices.Clone(entry.Validation),
		ValidationPrerequisites: models.CloneValidationPrerequisites(entry.ValidationPrerequisites),
		RuntimeInputs:           models.CloneRuntimeInputs(entry.RuntimeInputs),
		DestructiveDB:           entry.DestructiveDB,
		Scope:                   entry.Scope,
		DependsOn:               deps,
		ProviderDependencies:    models.CloneProviderDependencies(entry.ProviderDependencies),
		DescendantDependencies:  models.CloneDescendantDependencies(entry.DescendantDependencies),
		Supersedes:              supersedes,
		Created:                 now,
		History:                 []models.TaskHistoryEntry{},
	}
}

// buildOneToOneChild creates a child task for a one-to-one transition.
// The parent task itself is the input — no output[] needed. The child's fields
// describe the next phase's work, with spec_ref pointing to the parent's artifact.
func buildOneToOneChild(childID, parentID string, parent *models.Task, tDef transitionDef, inheritedDeps []string, now time.Time) models.Task {
	doerName := tDef.doerDisplayName
	if doerName == "" {
		doerName = tDef.targetRolePair
	}

	return models.Task{
		ID:          childID,
		Type:        tDef.taskType,
		RolePair:    tDef.targetRolePair,
		Description: fmt.Sprintf("%s task for: %s", doerName, parent.Description),
		Status:      tDef.targetStatus,
		Priority:    parent.Priority,
		ParentTasks: []string{parentID},
		SpecRef:     paths.NormalizeSpecRef(parent.SpecRef),
		EpicRef:     parent.EpicRef,
		PlanRef:     parent.PlanRef,
		ArchRef:     parent.ArchRef,
		RCARequired: parent.RCARequired,
		DoneWhen:    fmt.Sprintf("Complete %s work based on parent task %s", doerName, parentID),
		Scope:       fmt.Sprintf("Based on parent task %s", parentID),
		DependsOn:   inheritedDeps,
		Created:     now,
		History:     []models.TaskHistoryEntry{},
	}
}

// validateOutputEntry checks that an output entry has all required fields
// and that DependsOn indices are valid references within [0, totalEntries).
func validateOutputEntry(entry models.OutputEntry, index, totalEntries int) error {
	if entry.Desc == "" {
		return fmt.Errorf("output[%d] missing desc", index)
	}
	if entry.DoneWhen == "" {
		return fmt.Errorf("output[%d] missing done_when", index)
	}
	if entry.Scope == "" {
		return fmt.Errorf("output[%d] missing scope", index)
	}
	if entry.SpecRef == "" {
		return fmt.Errorf("output[%d] missing spec_ref", index)
	}
	if err := models.ValidateKind(entry.Kind); err != nil {
		return fmt.Errorf("output[%d] %w", index, err)
	}
	if err := models.ValidateValidationSafety(fmt.Sprintf("output[%d].validation", index), entry.Validation, entry.DestructiveDB); err != nil {
		return err
	}
	if err := models.ValidateValidationPrerequisites(entry.Validation, entry.ValidationPrerequisites); err != nil {
		return fmt.Errorf("output[%d]: %w", index, err)
	}
	if err := models.ValidateRuntimeInputs(entry.Validation, entry.RuntimeInputs); err != nil {
		return fmt.Errorf("output[%d]: %w", index, err)
	}
	return models.ValidateDependsOn(entry.DependsOn, index, totalEntries)
}
