package ops

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

// findManyToOneCohort finds all sibling tasks that form a many-to-one cohort
// with the trigger task. Siblings share a parent_task and the same role_pair.
// Returns the cohort members (as pointers into state.Tasks) and the shared parent ID.
func findManyToOneCohort(s *models.State, triggerTask *models.Task) ([]*models.Task, string, error) {
	sharedParentID := triggerTask.CohortParentID()
	if sharedParentID == "" {
		return nil, "", fmt.Errorf("trigger task %q has no parent_task — cannot determine many-to-one cohort", triggerTask.ID)
	}

	cohort, err := manyToOneCohortMembers(s, sharedParentID, triggerTask.RolePair)
	if err != nil {
		return nil, "", err
	}
	if len(cohort) == 0 {
		return nil, "", fmt.Errorf("no cohort members found for trigger task %q", triggerTask.ID)
	}

	// Sort for deterministic ordering
	slices.SortFunc(cohort, func(a, b *models.Task) int {
		return strings.Compare(a.ID, b.ID)
	})

	return cohort, sharedParentID, nil
}

// manyToOneCohortMembers returns the tasks of rolePair whose parents include
// parentID, with each SUPERSEDED member represented by its successors. Every
// supersession chain must stay within the cohort lineage and end at members
// that are not superseded; otherwise the superseded work would silently drop
// out of the fan-in, so the cohort is rejected.
func manyToOneCohortMembers(s *models.State, parentID, rolePair string) ([]*models.Task, error) {
	var members, superseded []*models.Task
	lineage := make(map[string]*models.Task)
	for i := range s.Tasks {
		task := &s.Tasks[i]
		if task.AmendsPlan != "" || task.RolePair != rolePair || !slices.Contains(task.EffectiveParentTasks(), parentID) {
			continue
		}
		lineage[task.ID] = task
		if task.Status == models.TaskStatusSuperseded {
			superseded = append(superseded, task)
			continue
		}
		members = append(members, task)
	}
	for _, task := range superseded {
		if err := checkSupersessionChain(task, task.ID, lineage, map[string]bool{}, parentID, rolePair); err != nil {
			return nil, err
		}
	}
	return members, nil
}

// checkSupersessionChain walks task's successors depth-first and requires every
// path to reach a live cohort member without revisiting a task on the path.
func checkSupersessionChain(task *models.Task, origin string, lineage map[string]*models.Task, path map[string]bool, parentID, rolePair string) error {
	if task.Status != models.TaskStatusSuperseded {
		return nil
	}
	if path[task.ID] {
		return fmt.Errorf("many-to-one cohort supersession chain from %q cycles back to %q", origin, task.ID)
	}
	if len(task.SupersededBy) == 0 {
		return fmt.Errorf("many-to-one cohort member %q is SUPERSEDED without a successor", task.ID)
	}
	path[task.ID] = true
	defer delete(path, task.ID)
	for _, successorID := range task.SupersededBy {
		successor := lineage[successorID]
		if successor == nil {
			return fmt.Errorf("many-to-one cohort member %q was superseded by %q, which does not carry cohort lineage (parent %s, role_pair %s)", task.ID, successorID, parentID, rolePair)
		}
		if err := checkSupersessionChain(successor, origin, lineage, path, parentID, rolePair); err != nil {
			return err
		}
	}
	return nil
}

// buildManyToOneChild creates a single child task from N parent cohort members.
// The child's ParentTasks lists all cohort member IDs. SpecRef is inherited
// from the cohort (all members share the same spec_ref via the pipeline chain).
func buildManyToOneChild(childID string, cohort []*models.Task, sharedParentID string, tDef transitionDef, inheritedDeps []string, now time.Time) models.Task {
	parentIDs := make([]string, len(cohort))
	for i, member := range cohort {
		parentIDs[i] = member.ID
	}

	doerName := tDef.doerDisplayName
	if doerName == "" {
		doerName = tDef.targetRolePair
	}

	specRef := cohort[0].SpecRef
	// Doc-only: strip the section anchor at the many-to-one boundary. Members
	// share the epic document, but a replacement may not carry epic_ref.
	var epicRef string
	for _, member := range cohort {
		if member.EpicRef != "" {
			epicRef = paths.SplitRefFile(member.EpicRef)
			break
		}
	}

	// Fan-in keeps the defect classification if any member carries it: a cohort
	// mixing defect and feature parents still consolidates work whose root cause
	// must be established downstream.
	rcaRequired := false
	for _, member := range cohort {
		if member.RCARequired {
			rcaRequired = true
			break
		}
	}

	return models.Task{
		ID:          childID,
		Type:        tDef.taskType,
		RolePair:    tDef.targetRolePair,
		Description: fmt.Sprintf("%s task consolidating %d approved tasks from %s", doerName, len(cohort), sharedParentID),
		Status:      tDef.targetStatus,
		Priority:    cohort[0].Priority,
		ParentTasks: parentIDs,
		SpecRef:     paths.NormalizeSpecRef(specRef),
		EpicRef:     paths.NormalizeSpecRef(epicRef),
		RCARequired: rcaRequired,
		DoneWhen:    fmt.Sprintf("Complete %s work based on %d parent tasks from %s", doerName, len(cohort), sharedParentID),
		Scope:       fmt.Sprintf("Consolidation of %d tasks from %s", len(cohort), sharedParentID),
		DependsOn:   inheritedDeps,
		Created:     now,
		History:     []models.TaskHistoryEntry{},
	}
}

// requireCohortReady checks that every cohort member can complete the fan-in.
// A member that can never merge is a failure wherever it sorts; only live
// members make this a normal wait.
func requireCohortReady(cohort []*models.Task, requiredStatus models.TaskStatus) error {
	var pending *models.Task
	for _, member := range cohort {
		if member.PlanGenerationFenced() {
			return fmt.Errorf("task %q is fenced by a reviewed plan amendment", member.ID)
		}
		if member.Status == requiredStatus || member.Status == models.TaskStatusMerged {
			continue
		}
		if member.Status.IsTerminal() {
			return fmt.Errorf("many-to-one cohort cannot complete: member %q is %s", member.ID, member.Status)
		}
		if pending == nil {
			pending = member
		}
	}
	if pending != nil {
		return fmt.Errorf("%w: member %q is at %s (need %s or MERGED)",
			errManyToOneCohortIncomplete, pending.ID, pending.Status, requiredStatus)
	}
	return nil
}

// cohortTransitionExecuted reports whether any cohort member already carries
// the transition marker, which makes the fan-in a crash-recovery case.
func cohortTransitionExecuted(cohort []*models.Task, transitionName string) bool {
	for _, member := range cohort {
		if member.TransitionsExecuted[transitionName] {
			return true
		}
	}
	return false
}

// markCohortExecuted sets the transition marker on every cohort member.
func markCohortExecuted(cohort []*models.Task, transitionName string) {
	for _, member := range cohort {
		if member.TransitionsExecuted == nil {
			member.TransitionsExecuted = make(map[string]bool)
		}
		member.TransitionsExecuted[transitionName] = true
	}
}

// cohortMemberIDs lists the cohort members in cohort order.
func cohortMemberIDs(cohort []*models.Task) []string {
	ids := make([]string, len(cohort))
	for i, member := range cohort {
		ids[i] = member.ID
	}
	return ids
}

// repairExistingCohortChild completes a many-to-one transition whose child
// already exists: it merges the inherited dependencies into the child, repairs
// a partially recorded marker on every member and reports the transition as
// already executed. Fresh generation and crash recovery share this repair.
func repairExistingCohortChild(s *models.State, resolver *pipeline.Resolver, existing *models.Task, inheritedDeps inheritedDepSet, cohort []*models.Task, transitionName, sharedParentID string) error {
	deps, err := repairedChildDeps(s, resolver, existing, inheritedDeps.all)
	if err != nil {
		return err
	}
	markCohortExecuted(cohort, transitionName)
	existing.DependsOn = deps
	return fmt.Errorf("%w: %q on cohort (parent %s)", errTransitionAlreadyExecuted, transitionName, sharedParentID)
}

// newCohortChild builds the validated consolidation child of a cohort.
func newCohortChild(s *models.State, resolver *pipeline.Resolver, cohort []*models.Task, sharedParentID string, tDef transitionDef, inheritedDeps inheritedDepSet, now time.Time) (models.Task, error) {
	childID := manyToOneChildID(sharedParentID, tDef.taskSlug)
	return validatedChild(s, resolver, buildManyToOneChild(childID, cohort, sharedParentID, tDef, inheritedDeps.all, now))
}
