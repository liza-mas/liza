package ops

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

// errTransitionAlreadyExecuted is returned by proceedInner when the transition
// has already been fully executed (idempotency guard). This is an expected
// condition in ExecuteAvailableTransitions, not a configuration error.
var errTransitionAlreadyExecuted = errors.New("transition already executed")

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

// collectNonTerminalByKind scans s.Tasks repo-wide (all goals, all sprints) and
// returns a map of non-empty Kind → canonical non-terminal task ID. "Canonical"
// = lexicographically smallest ID among candidates, for determinism across
// crash-recovery runs. Tasks with empty Kind are ignored. Tasks whose
// Status.IsTerminal() returns true (MERGED | ABANDONED | SUPERSEDED) are
// ignored; BLOCKED is non-terminal and counts as in-flight.
func collectNonTerminalByKind(s *models.State) map[string]string {
	byKind := map[string][]string{}
	for i := range s.Tasks {
		t := &s.Tasks[i]
		if t.Kind == "" {
			continue
		}
		if t.Status.IsTerminal() {
			continue
		}
		byKind[t.Kind] = append(byKind[t.Kind], t.ID)
	}
	out := map[string]string{}
	for k, ids := range byKind {
		slices.Sort(ids)
		out[k] = ids[0]
	}
	return out
}

// resolveKindDedup walks output entries in order and classifies each by Kind:
//   - empty Kind                    -> no effect.
//   - Kind in inFlight              -> skipped; remap[i] points at the in-flight task.
//   - Kind seen earlier in batch    -> skipped; remap[i] points at the first
//     occurrence's deterministic child ID.
//
// taskID and transitionName let the helper synthesize the first-occurrence
// sibling ID via perSubtaskChildID, so within-batch duplicates remap to the
// child that WILL be created this batch (not yet in s.Tasks when this runs).
// Return keys in skip and remap always match: every skipped entry is remapped.
func resolveKindDedup(
	entries []models.OutputEntry,
	inFlight map[string]string,
	taskID, transitionName string,
) (skip map[int]string, remap map[int]string) {
	skip = map[int]string{}
	remap = map[int]string{}
	emittedThisBatch := map[string]string{}
	for i, e := range entries {
		if e.Kind == "" {
			continue
		}
		if existingID, hit := inFlight[e.Kind]; hit {
			skip[i] = fmt.Sprintf("kind %q already in flight on task %s", e.Kind, existingID)
			remap[i] = existingID
			continue
		}
		if firstID, dup := emittedThisBatch[e.Kind]; dup {
			skip[i] = fmt.Sprintf("kind %q emitted earlier in same output[] at sibling %s", e.Kind, firstID)
			remap[i] = firstID
			continue
		}
		emittedThisBatch[e.Kind] = perSubtaskChildID(taskID, transitionName, i)
	}
	return
}

// ProceedResult contains the outcome of executing a manual inter-pair transition.
type ProceedResult struct {
	SourceTaskID   string
	TransitionName string
	ChildTaskIDs   []string
	CohortTaskIDs  []string // populated for many-to-one transitions
}

// transitionDef defines a manual transition between role pairs.
type transitionDef struct {
	// requiredStatus is the source task status required for this transition.
	requiredStatus models.TaskStatus
	// targetStatus is the status assigned to child tasks.
	targetStatus models.TaskStatus
	// cardinality is "per-subtask" or "one-to-one".
	cardinality string
	// targetRolePair is the role-pair set on child tasks (pipeline goals only).
	targetRolePair string
	// doerDisplayName is the display name of the target role-pair's doer (pipeline only).
	// Used for generating child task descriptions in one-to-one transitions.
	doerDisplayName string
	// taskType is the task type for child tasks, derived from the target role-pair.
	taskType models.TaskType
	// taskSlug is the segment used in child task IDs (falls back to transition name).
	taskSlug string
}

// Proceed executes a manual inter-pair transition on a source task.
// It creates child tasks from the source's output[] entries and records
// the transition in the source's transitions_executed map.
// Children are added to Sprint.Scope.Planned so they appear in the next sprint.
//
// Preconditions:
//   - Sprint must be COMPLETED
//   - Task must be at the transition's required status
//   - Transition must not already be executed (idempotency guard)
//   - For per-subtask: output[] must be non-empty with valid entries
//
// Crash recovery: if the transition key is already set but some children
// are missing, only the missing children are created.
func Proceed(projectRoot, taskID, transitionName string) (*ProceedResult, error) {
	resolver, cfg, err := loadResolver(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}

	tDef, err := resolveTransitionDefFrom(resolver, cfg, transitionName)
	if err != nil {
		return nil, err
	}

	statePath := paths.New(projectRoot).StatePath()
	blackboard := db.For(statePath)

	now := time.Now().UTC()
	result := &ProceedResult{
		SourceTaskID:   taskID,
		TransitionName: transitionName,
	}

	err = blackboard.Modify(func(s *models.State) error {
		// Validate sprint is COMPLETED
		if s.Sprint.Status != models.SprintStatusCompleted {
			return fmt.Errorf("sprint must be COMPLETED before proceeding (current: %s)", s.Sprint.Status)
		}

		task := s.FindTask(taskID)
		var inheritedDeps []string
		if task != nil {
			inheritedDeps, err = computeInheritedDeps(s, task, transitionName, resolver)
			if err != nil {
				return fmt.Errorf("inherited deps: %w", err)
			}
		}

		if err := proceedInner(s, taskID, transitionName, tDef, inheritedDeps, now, result); err != nil {
			return err
		}

		// Add children to sprint scope so they appear in the next sprint
		s.Sprint.Scope.Planned = append(s.Sprint.Scope.Planned, result.ChildTaskIDs...)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("proceed failed: %w", err)
	}

	return result, nil
}

// findManyToOneCohort finds all sibling tasks that form a many-to-one cohort
// with the trigger task. Siblings share a parent_task and the same role_pair.
// Returns the cohort members (as pointers into state.Tasks) and the shared parent ID.
func findManyToOneCohort(s *models.State, triggerTask *models.Task) ([]*models.Task, string, error) {
	sharedParentID := triggerTask.CohortParentID()
	if sharedParentID == "" {
		return nil, "", fmt.Errorf("trigger task %q has no parent_task — cannot determine many-to-one cohort", triggerTask.ID)
	}

	var cohort []*models.Task
	for i := range s.Tasks {
		task := &s.Tasks[i]
		if task.RolePair != triggerTask.RolePair {
			continue
		}
		taskParents := task.EffectiveParentTasks()
		if slices.Contains(taskParents, sharedParentID) {
			cohort = append(cohort, task)
		}
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
	epicRef := paths.SplitRefFile(cohort[0].EpicRef) // doc-only: strip section anchor at many-to-one boundary

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
		DoneWhen:    fmt.Sprintf("Complete %s work based on %d parent tasks from %s", doerName, len(cohort), sharedParentID),
		Scope:       fmt.Sprintf("Consolidation of %d tasks from %s", len(cohort), sharedParentID),
		DependsOn:   inheritedDeps,
		Created:     now,
		History:     []models.TaskHistoryEntry{},
	}
}

// proceedManyToOneInner handles many-to-one transition logic. The trigger task
// identifies the cohort (all siblings sharing a parent_task with the same role_pair).
// The transition fires only when ALL cohort members are at the required status
// (or MERGED, for the ExecuteAvailableTransitions path). Creates one child task
// linked to all N cohort members and sets transitions_executed on all of them.
func proceedManyToOneInner(s *models.State, taskID, transitionName string, tDef transitionDef, inheritedDeps []string, now time.Time, result *ProceedResult) error {
	task := s.FindTask(taskID)
	if task == nil {
		return fmt.Errorf("task %q not found", taskID)
	}

	// Accept both the required status and MERGED (see Design Decision D3)
	if task.Status != tDef.requiredStatus && task.Status != models.TaskStatusMerged {
		return fmt.Errorf("task %q must be at %s (or MERGED) for many-to-one transition %q (current: %s)",
			taskID, tDef.requiredStatus, transitionName, task.Status)
	}

	cohort, sharedParentID, err := findManyToOneCohort(s, task)
	if err != nil {
		return fmt.Errorf("many-to-one cohort detection failed: %w", err)
	}

	// Validate all cohort members are ready
	for _, member := range cohort {
		if member.Status != tDef.requiredStatus && member.Status != models.TaskStatusMerged {
			return fmt.Errorf("many-to-one cohort incomplete: member %q is at %s (need %s or MERGED)",
				member.ID, member.Status, tDef.requiredStatus)
		}
	}

	childID := manyToOneChildID(sharedParentID, tDef.taskSlug)

	// Idempotency: check if any cohort member already has transition executed
	anyExecuted := false
	for _, member := range cohort {
		if member.TransitionsExecuted[transitionName] {
			anyExecuted = true
			break
		}
	}

	if anyExecuted {
		// Crash recovery: check if child exists
		if existing := s.FindTask(childID); existing != nil {
			// Ensure all cohort members have transitions_executed set (repair partial)
			for _, member := range cohort {
				if member.TransitionsExecuted == nil {
					member.TransitionsExecuted = make(map[string]bool)
				}
				member.TransitionsExecuted[transitionName] = true
			}
			patchInheritedDeps(existing, inheritedDeps)
			return fmt.Errorf("%w: %q on cohort (parent %s)", errTransitionAlreadyExecuted, transitionName, sharedParentID)
		}
		// Child missing — fall through to create it (crash recovery)
	}

	// Set transitions_executed on ALL cohort members BEFORE child creation (crash recovery)
	for _, member := range cohort {
		if member.TransitionsExecuted == nil {
			member.TransitionsExecuted = make(map[string]bool)
		}
		member.TransitionsExecuted[transitionName] = true
	}

	child := buildManyToOneChild(childID, cohort, sharedParentID, tDef, inheritedDeps, now)
	s.Tasks = append(s.Tasks, child)
	result.ChildTaskIDs = append(result.ChildTaskIDs, childID)

	// Populate CohortTaskIDs
	cohortIDs := make([]string, len(cohort))
	for i, member := range cohort {
		cohortIDs[i] = member.ID
	}
	result.CohortTaskIDs = cohortIDs

	// Record history on the trigger task
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventTransitionExecuted,
		Extra: map[string]any{
			"transition":    transitionName,
			"cardinality":   "many-to-one",
			"cohort_size":   len(cohort),
			"cohort_parent": sharedParentID,
			"children":      1,
		},
	})

	return nil
}

// proceedInner is the core transition logic, operating on *models.State directly.
// It has no blackboard dependency and no sprint status check, making it usable
// both from Proceed (human-initiated, with sprint gate) and from
// ExecuteAvailableTransitions (supervisor-initiated, no sprint gate).
//
// The result.ChildTaskIDs slice is appended to with created child task IDs.
func proceedInner(s *models.State, taskID, transitionName string, tDef transitionDef, inheritedDeps []string, now time.Time, result *ProceedResult) error {
	if tDef.cardinality == "many-to-one" {
		return proceedManyToOneInner(s, taskID, transitionName, tDef, inheritedDeps, now, result)
	}
	task := s.FindTask(taskID)
	if task == nil {
		return fmt.Errorf("task %q not found", taskID)
	}

	if task.Status != tDef.requiredStatus {
		return fmt.Errorf("task %q must be at %s for transition %q (current: %s)",
			taskID, tDef.requiredStatus, transitionName, task.Status)
	}

	if task.TransitionsExecuted[transitionName] {
		return recoverCrashedTransition(s, task, taskID, transitionName, tDef, inheritedDeps, now, result)
	}

	switch tDef.cardinality {
	case "per-subtask":
		if len(task.Output) == 0 {
			return fmt.Errorf("task %q has no output[] entries for per-subtask transition %q", taskID, transitionName)
		}
		for i, entry := range task.Output {
			if err := validateOutputEntry(entry, i, len(task.Output)); err != nil {
				return err
			}
		}
	case "one-to-one":
		if task.SpecRef == "" {
			return fmt.Errorf("task %q has empty spec_ref for one-to-one transition %q", taskID, transitionName)
		}
	default:
		return fmt.Errorf("unsupported cardinality %q for transition %q", tDef.cardinality, transitionName)
	}

	// Compute Kind-based dedup decision for per-subtask. skipEntry[i] present =>
	// entry i is dedup'd; remapSibling[i] is the incumbent or first-occurrence
	// task ID that takes entry i's place for downstream dep resolution.
	var skipEntry map[int]string
	var remapSibling map[int]string
	if tDef.cardinality == "per-subtask" {
		inFlightByKind := collectNonTerminalByKind(s)
		skipEntry, remapSibling = resolveKindDedup(task.Output, inFlightByKind, taskID, transitionName)
	}

	// Write this first for crash recovery
	if task.TransitionsExecuted == nil {
		task.TransitionsExecuted = make(map[string]bool)
	}
	task.TransitionsExecuted[transitionName] = true

	// Pre-compute sibling IDs for DependsOn resolution in per-subtask transitions.
	siblingIDs := make([]string, len(task.Output))
	for i := range task.Output {
		siblingIDs[i] = perSubtaskChildID(taskID, tDef.taskSlug, i)
	}

	switch tDef.cardinality {
	case "per-subtask":
		for i, entry := range task.Output {
			if reID, dedupd := remapSibling[i]; dedupd {
				// Dep resolution still needs an ID; point at the incumbent so
				// that downstream siblings with depends_on: [i] resolve to the
				// real in-flight (or first-of-batch) task. Do NOT append a
				// child for i.
				siblingIDs[i] = reID
				continue
			}
			child := buildChildTask(siblingIDs[i], taskID, entry, tDef.targetStatus, tDef.targetRolePair, tDef.taskType, siblingIDs, inheritedDeps, task.EpicRef, task.ArchRef, now)
			s.Tasks = append(s.Tasks, child)
			result.ChildTaskIDs = append(result.ChildTaskIDs, siblingIDs[i])
		}
	case "one-to-one":
		childID := oneToOneChildID(taskID, tDef.taskSlug)
		child := buildOneToOneChild(childID, taskID, task, tDef, inheritedDeps, now)
		s.Tasks = append(s.Tasks, child)
		result.ChildTaskIDs = append(result.ChildTaskIDs, childID)
	}

	histExtra := map[string]any{
		"transition": transitionName,
		"children":   len(result.ChildTaskIDs),
	}
	if len(skipEntry) > 0 {
		skipped := make([]map[string]any, 0, len(skipEntry))
		for i, reason := range skipEntry {
			skipped = append(skipped, map[string]any{
				"output_index": i,
				"kind":         task.Output[i].Kind,
				"reason":       reason,
				"remapped_to":  remapSibling[i],
			})
		}
		slices.SortFunc(skipped, func(a, b map[string]any) int {
			return a["output_index"].(int) - b["output_index"].(int)
		})
		histExtra["skipped_entries"] = skipped
	}
	// Re-fetch the task pointer: s.Tasks appends above may have reallocated
	// the backing array, invalidating the original pointer.
	task = s.FindTask(taskID)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventTransitionExecuted,
		Extra: histExtra,
	})

	return nil
}

// recoverCrashedTransition handles crash recovery when a transition was already
// marked as executed but some child tasks are missing. Returns
// errTransitionAlreadyExecuted if all children already exist.
func recoverCrashedTransition(s *models.State, task *models.Task, taskID, transitionName string, tDef transitionDef, inheritedDeps []string, now time.Time, result *ProceedResult) error {
	switch tDef.cardinality {
	case "per-subtask":
		// Pre-compute sibling IDs for DependsOn resolution.
		siblingIDs := make([]string, len(task.Output))
		for i := range task.Output {
			siblingIDs[i] = perSubtaskChildID(taskID, tDef.taskSlug, i)
		}
		// Re-compute dedup decision against current repo-wide state. For skipped
		// entries, siblingIDs[i] must point at the foreign incumbent so downstream
		// dep resolution remaps correctly — mirrors proceedInner's per-subtask path.
		inFlightByKind := collectNonTerminalByKind(s)
		skipEntry, remapSibling := resolveKindDedup(task.Output, inFlightByKind, taskID, transitionName)
		for i, reID := range remapSibling {
			siblingIDs[i] = reID
		}
		var missingChildren []int
		for i := range task.Output {
			// (1) SKIPPED — short-circuit BEFORE any FindTask / patchInheritedDeps.
			//     siblingIDs[i] points at a FOREIGN incumbent for skipped entries.
			//     Touching its DependsOn would corrupt a cross-goal task.
			if _, skipped := skipEntry[i]; skipped {
				continue
			}
			// (2) EXISTING — patch our OWN child's inherited deps.
			existing := s.FindTask(siblingIDs[i])
			if existing == nil {
				// (3) CREATE — flagged for creation below.
				missingChildren = append(missingChildren, i)
			} else {
				patchInheritedDeps(existing, inheritedDeps)
			}
		}
		if len(missingChildren) == 0 {
			return fmt.Errorf("%w: %q on task %q", errTransitionAlreadyExecuted, transitionName, taskID)
		}
		for _, idx := range missingChildren {
			child := buildChildTask(siblingIDs[idx], taskID, task.Output[idx], tDef.targetStatus, tDef.targetRolePair, tDef.taskType, siblingIDs, inheritedDeps, task.EpicRef, task.ArchRef, now)
			s.Tasks = append(s.Tasks, child)
			result.ChildTaskIDs = append(result.ChildTaskIDs, siblingIDs[idx])
		}
		// Re-fetch: the appends above may have reallocated s.Tasks, leaving
		// the incoming task pointer stale.
		task = s.FindTask(taskID)
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventTransitionCrashRecov,
			Extra: map[string]any{
				"transition":         transitionName,
				"recovered_children": len(missingChildren),
			},
		})
		return nil

	case "one-to-one":
		childID := oneToOneChildID(taskID, tDef.taskSlug)
		if existing := s.FindTask(childID); existing != nil {
			patchInheritedDeps(existing, inheritedDeps)
			return fmt.Errorf("%w: %q on task %q", errTransitionAlreadyExecuted, transitionName, taskID)
		}
		child := buildOneToOneChild(childID, taskID, task, tDef, inheritedDeps, now)
		s.Tasks = append(s.Tasks, child)
		result.ChildTaskIDs = append(result.ChildTaskIDs, childID)
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventTransitionCrashRecov,
			Extra: map[string]any{
				"transition":         transitionName,
				"recovered_children": 1,
			},
		})
		return nil

	case "many-to-one":
		cohort, sharedParentID, err := findManyToOneCohort(s, task)
		if err != nil {
			return fmt.Errorf("crash recovery cohort detection: %w", err)
		}
		childID := manyToOneChildID(sharedParentID, tDef.taskSlug)
		if existing := s.FindTask(childID); existing != nil {
			for _, member := range cohort {
				if member.TransitionsExecuted == nil {
					member.TransitionsExecuted = make(map[string]bool)
				}
				member.TransitionsExecuted[transitionName] = true
			}
			patchInheritedDeps(existing, inheritedDeps)
			return fmt.Errorf("%w: %q on cohort (parent %s)", errTransitionAlreadyExecuted, transitionName, sharedParentID)
		}
		child := buildManyToOneChild(childID, cohort, sharedParentID, tDef, inheritedDeps, now)
		s.Tasks = append(s.Tasks, child)
		result.ChildTaskIDs = append(result.ChildTaskIDs, childID)
		for _, member := range cohort {
			if member.TransitionsExecuted == nil {
				member.TransitionsExecuted = make(map[string]bool)
			}
			member.TransitionsExecuted[transitionName] = true
		}
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventTransitionCrashRecov,
			Extra: map[string]any{
				"transition":         transitionName,
				"cardinality":        "many-to-one",
				"cohort_parent":      sharedParentID,
				"recovered_children": 1,
			},
		})
		return nil

	default:
		return fmt.Errorf("%w: %q on task %q", errTransitionAlreadyExecuted, transitionName, taskID)
	}
}

// pendingTx represents a transition queued for execution in ExecuteAvailableTransitions.
type pendingTx struct {
	taskID  string
	taskIdx int
	name    string
	tDef    transitionDef
	origIdx int // original collection index for stable topo-sort tie-breaking
}

// isTransitionIncomplete checks if an executed transition has missing children.
// Used to detect crash recovery needs in ExecuteAvailableTransitions phase 1b.
func isTransitionIncomplete(s *models.State, task *models.Task, transName string, resolver *pipeline.Resolver) bool {
	td, err := resolver.Transition(transName)
	if err != nil {
		return false
	}
	slug := td.TaskSlugOrName()
	switch td.Cardinality {
	case "per-subtask":
		for i := 0; i < len(task.Output); i++ {
			if s.FindTask(perSubtaskChildID(task.ID, slug, i)) == nil {
				return true
			}
		}
	case "one-to-one":
		if s.FindTask(oneToOneChildID(task.ID, slug)) == nil {
			return true
		}
	case "many-to-one":
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

// topoSortPending sorts pending transitions by task DependsOn relationships using
// Kahn's algorithm. Tie-breaker is original collection index for deterministic ordering.
// Returns (sorted, cycles, blocked) where cycles are true SCC members and blocked
// are nodes downstream of those cycles.
func topoSortPending(pending []pendingTx, s *models.State) (sorted []pendingTx, cycles [][]pendingTx, blocked []pendingTx) {
	n := len(pending)
	if n == 0 {
		return nil, nil, nil
	}

	// Build task ID → pending indices map
	taskToPending := make(map[string][]int)
	for i, p := range pending {
		taskToPending[p.taskID] = append(taskToPending[p.taskID], i)
	}

	// Compute in-degrees: if pending[i]'s task depends on pending[j]'s task, j→i edge
	inDegree := make([]int, n)
	edges := make([][]int, n) // edges[j] = indices that j blocks
	for i, p := range pending {
		task := s.FindTask(p.taskID)
		if task == nil {
			continue
		}
		for _, depID := range task.DependsOn {
			if depIndices, ok := taskToPending[depID]; ok {
				for _, depIdx := range depIndices {
					edges[depIdx] = append(edges[depIdx], i)
					inDegree[i]++
				}
			}
		}
	}

	// Kahn's: process zero-in-degree nodes, stable by origIdx
	var queue []int
	for i := range pending {
		if inDegree[i] == 0 {
			queue = append(queue, i)
		}
	}
	slices.SortFunc(queue, func(a, b int) int {
		return pending[a].origIdx - pending[b].origIdx
	})

	for len(queue) > 0 {
		idx := queue[0]
		queue = queue[1:]
		sorted = append(sorted, pending[idx])
		for _, blockedIdx := range edges[idx] {
			inDegree[blockedIdx]--
			if inDegree[blockedIdx] == 0 {
				queue = append(queue, blockedIdx)
				slices.SortFunc(queue, func(a, b int) int {
					return pending[a].origIdx - pending[b].origIdx
				})
			}
		}
	}

	// Remaining nodes have inDegree > 0. Some are in true cycles; others are
	// merely downstream of those cycles and cannot be ordered yet.
	var remaining []int
	for i := range pending {
		if inDegree[i] > 0 {
			remaining = append(remaining, i)
		}
	}
	if len(remaining) == 0 {
		return sorted, nil, nil
	}

	cycleIndices := make(map[int]bool)
	for _, scc := range findSCCs(remaining, edges) {
		if len(scc) < 2 && !hasSelfLoop(scc[0], edges) {
			continue
		}
		cycle := make([]pendingTx, len(scc))
		for i, idx := range scc {
			cycle[i] = pending[idx]
			cycleIndices[idx] = true
		}
		cycles = append(cycles, cycle)
	}

	for _, idx := range remaining {
		if !cycleIndices[idx] {
			blocked = append(blocked, pending[idx])
		}
	}

	return sorted, cycles, blocked
}

// findSCCs returns strongly connected components among the given node indices
// using Tarjan's algorithm, considering only edges within the subgraph.
func findSCCs(indices []int, edges [][]int) [][]int {
	inSubgraph := make(map[int]bool, len(indices))
	for _, i := range indices {
		inSubgraph[i] = true
	}

	var (
		stack   []int
		onStack = make(map[int]bool)
		disc    = make(map[int]int)
		low     = make(map[int]int)
		counter int
		sccs    [][]int
	)

	var visit func(v int)
	visit = func(v int) {
		disc[v] = counter
		low[v] = counter
		counter++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range edges[v] {
			if !inSubgraph[w] {
				continue
			}
			if _, seen := disc[w]; !seen {
				visit(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] && disc[w] < low[v] {
				low[v] = disc[w]
			}
		}

		if low[v] == disc[v] {
			var scc []int
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			sccs = append(sccs, scc)
		}
	}

	for _, v := range indices {
		if _, seen := disc[v]; !seen {
			visit(v)
		}
	}

	return sccs
}

func hasSelfLoop(idx int, edges [][]int) bool {
	for _, next := range edges[idx] {
		if next == idx {
			return true
		}
	}
	return false
}

// hasCycleBlockedEvent checks if a task already has a transition_cycle_blocked
// history entry for the given transition and cycle members (idempotency guard).
func hasCycleBlockedEvent(task *models.Task, transitionName string, cycleMembers []string) bool {
	for _, h := range task.History {
		if h.Event != models.TaskEventTransitionCycleBlocked {
			continue
		}
		if trans, ok := h.Extra["transition"].(string); ok && trans == transitionName {
			if slices.Equal(extraToStringSlice(h.Extra["cycle_members"]), cycleMembers) {
				return true
			}
		}
	}
	return false
}

// extraToStringSlice normalizes an Extra field value to []string.
// After YAML round-trip, []string becomes []any — this handles both.
func extraToStringSlice(v any) []string {
	switch typed := v.(type) {
	case []string:
		return typed
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

// ExecuteAvailableTransitions auto-executes pipeline transitions for merged tasks.
// Three-phase approach:
//  1. Collect: (a) available transitions + (b) incomplete transitions (crash recovery)
//  2. Topological sort by DependsOn for phase-gate ordering
//  3. Execute in sorted order — upstream transitions fire first
//
// Cycle detection: true cycle members get a transition_cycle_blocked history event
// and are skipped from execution. Tasks downstream of those cycles are skipped
// until the upstream cycle is resolved.
func ExecuteAvailableTransitions(projectRoot string, triggerFilter string) ([]ProceedResult, error) {
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}

	statePath := paths.New(projectRoot).StatePath()
	blackboard := db.For(statePath)

	// Pre-read state to check if goal.base_commit needs snapshotting.
	// Git operation must happen outside Modify callback.
	var integrationHEAD string
	preState, preErr := blackboard.Read()
	if preErr == nil && preState.Goal.BaseCommit == nil {
		branchName := preState.Config.IntegrationBranch
		if branchName == "" {
			branchName = "integration"
		}
		gw := git.New(projectRoot)
		integrationHEAD, _ = gw.GetCommitSHA(branchName)
	}

	now := time.Now().UTC()
	var results []ProceedResult

	err = blackboard.Modify(func(s *models.State) error {
		var pending []pendingTx
		origIdx := 0

		// Phase 1a: Collect available transitions based on trigger filter.
		for i := range s.Tasks {
			task := &s.Tasks[i]
			if task.RolePair == "" {
				continue
			}

			// All tasks fan out from MERGED (after merge handler).
			// Integration tasks previously bypassed merge, but that left
			// orphaned worktrees. No-diff merges are handled gracefully by
			// performCASMerge (ancestor check → fast-forward no-op).
			if task.Status != models.TaskStatusMerged {
				continue
			}

			// Replanned tasks must not spawn children — the replan replacement
			// owns the downstream pipeline. Replan.go marks real transitions as
			// executed (preventive), but this is a defensive second layer.
			if task.TransitionsExecuted["replanned"] {
				continue
			}

			approvedStatus, err := resolver.ApprovedStatus(task.RolePair)
			if err != nil {
				log.Printf("WARNING: ExecuteAvailableTransitions: task %s has unknown role-pair %q: %v", task.ID, task.RolePair, err)
				continue
			}

			var available []string
			switch triggerFilter {
			case "auto":
				available = resolver.AvailableAutoTransitions(approvedStatus, task.TransitionsExecuted)
			case "manual":
				available = resolver.AvailableManualTransitions(approvedStatus, task.TransitionsExecuted)
			default: // "" — both
				available = resolver.AvailableManualTransitions(approvedStatus, task.TransitionsExecuted)
				available = append(available, resolver.AvailableAutoTransitions(approvedStatus, task.TransitionsExecuted)...)
			}
			for _, transitionName := range available {
				tDef, err := buildTransitionDefFromPipeline(resolver, transitionName)
				if err != nil {
					log.Printf("WARNING: ExecuteAvailableTransitions: task %s transition %q: %v", task.ID, transitionName, err)
					continue
				}
				tDef.requiredStatus = models.TaskStatusMerged

				pending = append(pending, pendingTx{
					taskID: task.ID, taskIdx: i, name: transitionName,
					tDef: tDef, origIdx: origIdx,
				})
				origIdx++
			}
		}

		// Phase 1b: Collect incomplete transitions (crash recovery)
		for i := range s.Tasks {
			task := &s.Tasks[i]
			if task.RolePair == "" {
				continue
			}
			// All tasks recover from MERGED (same as Phase 1a).
			if task.Status != models.TaskStatusMerged {
				continue
			}
			// Replanned tasks must not spawn children (same guard as Phase 1a).
			if task.TransitionsExecuted["replanned"] {
				continue
			}
			for transName := range task.TransitionsExecuted {
				if !isTransitionIncomplete(s, task, transName, resolver) {
					continue
				}
				tDef, err := buildTransitionDefFromPipeline(resolver, transName)
				if err != nil {
					continue
				}
				tDef.requiredStatus = models.TaskStatusMerged
				pending = append(pending, pendingTx{
					taskID: task.ID, taskIdx: i, name: transName,
					tDef: tDef, origIdx: origIdx,
				})
				origIdx++
			}
		}

		// Dedup by (taskID, transitionName) — same transition may appear in both scans
		seen := make(map[string]bool)
		deduped := pending[:0]
		for _, p := range pending {
			key := p.taskID + "\x00" + p.name
			if !seen[key] {
				seen[key] = true
				deduped = append(deduped, p)
			}
		}
		pending = deduped

		// Phase 2: Topological sort by DependsOn
		sorted, cycles, downstreamBlocked := topoSortPending(pending, s)

		// Handle true cycles: add one history event per SCC (idempotent), log error
		for _, cycle := range cycles {
			cycleMemberIDs := make([]string, len(cycle))
			for i, p := range cycle {
				cycleMemberIDs[i] = p.taskID
			}
			slices.Sort(cycleMemberIDs)

			for _, p := range cycle {
				task := &s.Tasks[p.taskIdx]
				if !hasCycleBlockedEvent(task, p.name, cycleMemberIDs) {
					task.History = append(task.History, models.TaskHistoryEntry{
						Time:  now,
						Event: models.TaskEventTransitionCycleBlocked,
						Extra: map[string]any{
							"transition":    p.name,
							"cycle_members": cycleMemberIDs,
						},
					})
				}
				log.Printf("ERROR: ExecuteAvailableTransitions: cycle-blocked task %s transition %s (cycle: %v)", p.taskID, p.name, cycleMemberIDs)
			}
		}

		for _, p := range downstreamBlocked {
			log.Printf("WARN: ExecuteAvailableTransitions: task %s transition %s blocked by upstream cycle, skipping", p.taskID, p.name)
		}

		// Phase 3: Execute in sorted order
		for _, p := range sorted {
			task := s.FindTask(p.taskID)
			var inheritedDeps []string
			if task != nil {
				var depErr error
				inheritedDeps, depErr = computeInheritedDeps(s, task, p.name, resolver)
				if depErr != nil {
					log.Printf("WARNING: ExecuteAvailableTransitions: task %s inherited deps: %v", p.taskID, depErr)
					continue
				}
			}

			result := ProceedResult{
				SourceTaskID:   p.taskID,
				TransitionName: p.name,
			}

			if err := proceedInner(s, p.taskID, p.name, p.tDef, inheritedDeps, now, &result); err != nil {
				if !errors.Is(err, errTransitionAlreadyExecuted) {
					log.Printf("WARNING: ExecuteAvailableTransitions: task %s transition %q: %v", p.taskID, p.name, err)
				}
				continue
			}

			results = append(results, result)
		}

		// Snapshot goal.base_commit if this is the first time coding-pair children are created.
		if integrationHEAD != "" && s.Goal.BaseCommit == nil {
			for _, r := range results {
				tDef, tdErr := buildTransitionDefFromPipeline(resolver, r.TransitionName)
				if tdErr == nil && tDef.targetRolePair == "coding-pair" && len(r.ChildTaskIDs) > 0 {
					s.Goal.BaseCommit = &integrationHEAD
					break
				}
			}
		}

		// Add children to sprint scope (dedup guard for crash recovery idempotency)
		for _, r := range results {
			for _, childID := range r.ChildTaskIDs {
				if !slices.Contains(s.Sprint.Scope.Planned, childID) {
					s.Sprint.Scope.Planned = append(s.Sprint.Scope.Planned, childID)
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("execute available transitions failed: %w", err)
	}

	return results, nil
}

// buildTransitionDefFromPipeline resolves a transition definition from pipeline config.
// This is the shared helper used by both resolveTransitionDefFrom (with trigger check)
// and ExecuteAvailableTransitions (without trigger check).
func buildTransitionDefFromPipeline(resolver *pipeline.Resolver, transitionName string) (transitionDef, error) {
	td, err := resolver.Transition(transitionName)
	if err != nil {
		return transitionDef{}, err
	}

	fromStatus, err := resolvePhaseRef(resolver, td.From)
	if err != nil {
		return transitionDef{}, fmt.Errorf("invalid from reference in transition %q: %w", transitionName, err)
	}
	toStatus, err := resolvePhaseRef(resolver, td.To)
	if err != nil {
		return transitionDef{}, fmt.Errorf("invalid to reference in transition %q: %w", transitionName, err)
	}
	targetPair, err := resolver.TransitionTargetRolePair(transitionName)
	if err != nil {
		return transitionDef{}, fmt.Errorf("invalid target role-pair in transition %q: %w", transitionName, err)
	}

	// Resolve doer display name and task type from the target role-pair.
	// Task type is derived from the doer role via the taskWorkflows registry,
	// falling back to TaskTypeCoding for unknown roles.
	var doerDisplay string
	childTaskType := models.TaskTypeCoding
	rp, rpErr := resolver.RolePair(targetPair)
	if rpErr == nil {
		doerDisplay = resolver.RoleDisplayName(rp.Doer)
		childTaskType = models.TaskTypeForRole(rp.Doer)
	}

	return transitionDef{
		requiredStatus:  fromStatus,
		targetStatus:    toStatus,
		cardinality:     td.Cardinality,
		targetRolePair:  targetPair,
		doerDisplayName: doerDisplay,
		taskType:        childTaskType,
		taskSlug:        td.TaskSlugOrName(),
	}, nil
}

// buildChildTask creates a child task from an output entry.
// siblingIDs maps output entry indices to their generated task IDs,
// used to resolve DependsOn index references to actual task IDs.
// inheritedDeps are phase-gate dependencies from upstream tasks' children.
func buildChildTask(childID, parentID string, entry models.OutputEntry, targetStatus models.TaskStatus, targetRolePair string, taskType models.TaskType, siblingIDs, inheritedDeps []string, parentEpicRef, parentArchRef string, now time.Time) models.Task {
	var deps []string
	for _, ref := range entry.DependsOn {
		idx, _ := strconv.Atoi(ref) // validated upstream in validateOutputEntry
		deps = append(deps, siblingIDs[idx])
	}
	deps = append(deps, inheritedDeps...)

	epicRef := entry.EpicRef
	if epicRef == "" {
		epicRef = parentEpicRef
	}

	archRef := entry.ArchRef
	if archRef == "" {
		archRef = parentArchRef
	}

	return models.Task{
		ID:          childID,
		Type:        taskType,
		RolePair:    targetRolePair,
		Description: entry.Desc,
		Status:      targetStatus,
		Priority:    1,
		ParentTasks: []string{parentID},
		SpecRef:     paths.NormalizeSpecRef(entry.SpecRef),
		EpicRef:     paths.NormalizeSpecRef(epicRef),
		PlanRef:     paths.NormalizeSpecRef(entry.PlanRef),
		ArchRef:     paths.NormalizeSpecRef(archRef),
		Kind:        entry.Kind,
		DoneWhen:    entry.DoneWhen,
		Scope:       entry.Scope,
		DependsOn:   deps,
		Created:     now,
		History:     []models.TaskHistoryEntry{},
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
		DoneWhen:    fmt.Sprintf("Complete %s work based on parent task %s", doerName, parentID),
		Scope:       fmt.Sprintf("Based on parent task %s", parentID),
		DependsOn:   inheritedDeps,
		Created:     now,
		History:     []models.TaskHistoryEntry{},
	}
}

// patchInheritedDeps adds any missing inherited deps to an existing child task's DependsOn.
// Used during crash recovery when a child was created before inherited deps were computed.
func patchInheritedDeps(task *models.Task, inheritedDeps []string) {
	for _, dep := range inheritedDeps {
		if !slices.Contains(task.DependsOn, dep) {
			task.DependsOn = append(task.DependsOn, dep)
		}
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
	return models.ValidateDependsOn(entry.DependsOn, index, totalEntries)
}

// computeInheritedDeps derives phase-gate dependencies from upstream dependency tasks.
// For each task in task.DependsOn that has executed the same transition, it collects
// the child task IDs that were created by that transition. These become additional
// DependsOn entries on downstream children, enforcing phase-gate ordering.
//
// Returns error if an upstream transition is marked executed but expected children
// are missing (crash inconsistency that must be recovered first).
func computeInheritedDeps(s *models.State, task *models.Task, transitionName string, resolver *pipeline.Resolver) ([]string, error) {
	td, err := resolver.Transition(transitionName)
	if err != nil {
		return nil, fmt.Errorf("cannot compute inherited deps: unknown transition %q: %w", transitionName, err)
	}
	slug := td.TaskSlugOrName()

	var inherited []string
	for _, depID := range task.DependsOn {
		depTask := s.FindTask(depID)
		if depTask == nil || !depTask.TransitionsExecuted[transitionName] {
			continue
		}
		switch td.Cardinality {
		case "per-subtask":
			for i := 0; i < len(depTask.Output); i++ {
				childID := perSubtaskChildID(depID, slug, i)
				if s.FindTask(childID) == nil {
					return nil, fmt.Errorf("upstream task %s has transition %q executed but child %s missing (needs crash recovery)", depID, transitionName, childID)
				}
				inherited = append(inherited, childID)
			}
		case "one-to-one":
			childID := oneToOneChildID(depID, slug)
			if s.FindTask(childID) == nil {
				return nil, fmt.Errorf("upstream task %s has transition %q executed but child %s missing (needs crash recovery)", depID, transitionName, childID)
			}
			inherited = append(inherited, childID)
		case "many-to-one":
			cohortParentID := depTask.CohortParentID()
			if cohortParentID == "" {
				continue
			}
			// Skip intra-cohort deps: when the dep task shares the same cohort
			// parent, the many-to-one child is the same task being created,
			// which would produce a circular self-dependency.
			if cohortParentID == task.CohortParentID() {
				continue
			}
			childID := manyToOneChildID(cohortParentID, slug)
			if s.FindTask(childID) == nil {
				return nil, fmt.Errorf("upstream task %s has transition %q executed but child %s missing (needs crash recovery)", depID, transitionName, childID)
			}
			if !slices.Contains(inherited, childID) {
				inherited = append(inherited, childID)
			}
		}
	}
	return inherited, nil
}

// AvailableManualTransitions returns the available manual transitions for a task.
// Transitions are read from the frozen pipeline config.
// Returns nil if no transitions are available.
func AvailableManualTransitions(task *models.Task, projectRoot string) []string {
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil
	}
	return resolver.AvailableManualTransitions(task.Status, task.TransitionsExecuted)
}

// resolveTransitionDefFrom validates and resolves a manual transition definition
// from an already-loaded resolver, avoiding double-loading in Proceed.
func resolveTransitionDefFrom(resolver *pipeline.Resolver, cfg *pipeline.PipelineConfig, transitionName string) (transitionDef, error) {
	td, err := resolver.Transition(transitionName)
	if err != nil {
		names := allTransitionNames(cfg)
		return transitionDef{}, fmt.Errorf("unknown transition %q (available: %s)", transitionName, strings.Join(names, ", "))
	}

	// Only manual transitions are executable via liza proceed.
	// Auto transitions are reserved for supervisor execution.
	if td.Trigger != "manual" {
		return transitionDef{}, fmt.Errorf("transition %q has trigger %q; only manual transitions can be executed via proceed", transitionName, td.Trigger)
	}

	return buildTransitionDefFromPipeline(resolver, transitionName)
}

// resolvePhaseRef resolves a phase reference to a concrete TaskStatus.
// Handles both 2-part refs ("role-pair.phase") and 3-part refs
// ("sub-pipeline.role-pair.phase"). For 3-part refs, the sub-pipeline
// prefix is stripped since role-pair names are globally unique.
func resolvePhaseRef(resolver *pipeline.Resolver, ref string) (models.TaskStatus, error) {
	parts := strings.Split(ref, ".")
	var pair, phase string
	switch len(parts) {
	case 2:
		pair, phase = parts[0], parts[1]
	case 3:
		// 3-part ref: sub-pipeline.role-pair.phase — strip sub-pipeline prefix
		pair, phase = parts[1], parts[2]
	default:
		return "", fmt.Errorf("invalid transition reference %q (expected pair.phase or sub-pipeline.pair.phase)", ref)
	}

	if pair == "" || phase == "" {
		return "", fmt.Errorf("invalid transition reference %q: empty components", ref)
	}

	switch phase {
	case "initial":
		return resolver.InitialStatus(pair)
	case "executing":
		return resolver.ExecutingStatus(pair)
	case "submitted":
		return resolver.SubmittedStatus(pair)
	case "reviewing":
		return resolver.ReviewingStatus(pair)
	case "approved":
		return resolver.ApprovedStatus(pair)
	case "rejected":
		return resolver.RejectedStatus(pair)
	default:
		return "", fmt.Errorf("unknown phase %q in reference %q", phase, ref)
	}
}

// allTransitionNames collects all transition names from the pipeline config,
// including both sub-pipeline transitions and pipeline-transitions.
func allTransitionNames(cfg *pipeline.PipelineConfig) []string {
	var names []string
	for _, sp := range cfg.Pipeline.SubPipelines {
		for _, t := range sp.Transitions {
			names = append(names, t.Name)
		}
	}
	for _, t := range cfg.Pipeline.PipelineTransitions {
		names = append(names, t.Name)
	}
	slices.Sort(names)
	return names
}
