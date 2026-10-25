package ops

import (
	"errors"
	"fmt"
	"log"
	"slices"
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

// errManyToOneCohortIncomplete is returned while a many-to-one cohort still has
// live members short of the required status: a normal wait, not a failure.
var errManyToOneCohortIncomplete = errors.New("many-to-one cohort incomplete")

// collectNonTerminalByKind scans s.Tasks repo-wide (all goals, all sprints) and
// returns a map of non-empty Kind → canonical non-terminal task ID. "Canonical"
// = lexicographically smallest ID among candidates, for determinism across
// crash-recovery runs. Tasks with empty Kind are ignored. Tasks whose
// Status.IsTerminal() returns true (MERGED | ABANDONED | SUPERSEDED) are
// ignored; BLOCKED is non-terminal and counts as in-flight.
func collectNonTerminalByKind(s *models.State, retiring map[string]bool) map[string]string {
	return models.NonTerminalTasksByKind(s, retiring)
}

// resolvePerSubtaskSiblings returns the effective sibling ID for each output
// entry after applying kind-based deduplication.
func resolvePerSubtaskSiblings(entries []models.OutputEntry, inFlightByKind map[string]string, taskID, taskSlug string) ([]string, map[int]string, map[int]string) {
	return models.ResolveOutputSiblings(entries, inFlightByKind, taskID, taskSlug)
}

// ProceedResult contains the outcome of executing a manual inter-pair transition.
type ProceedResult struct {
	SourceTaskID   string
	TransitionName string
	ChildTaskIDs   []string
	CohortTaskIDs  []string // populated for many-to-one transitions
	// RetiredTaskIDs are the originals this transition's outputs superseded.
	RetiredTaskIDs []string

	replacements     *planReplacements
	recovering       bool
	retiredWorktrees []string
}

// transitionDef defines a manual transition between role pairs.
type transitionDef struct {
	// sourceRolePair is the role-pair allowed to trigger this transition.
	sourceRolePair string
	// requiredStatus is the source task status required for this transition.
	requiredStatus models.TaskStatus
	// targetStatus is the status assigned to child tasks.
	targetStatus models.TaskStatus
	// cardinality is how many children the transition creates per source task.
	cardinality models.TransitionCardinality
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
	resolver, cfg, err := loadResolverWithRuntimePolicy(projectRoot)
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
	newResult := func() *ProceedResult {
		return &ProceedResult{SourceTaskID: taskID, TransitionName: transitionName}
	}
	execute := func(s *models.State, result *ProceedResult) error {
		// Validate sprint is COMPLETED
		if s.Sprint.Status != models.SprintStatusCompleted {
			return fmt.Errorf("sprint must be COMPLETED before proceeding (current: %s)", s.Sprint.Status)
		}

		task := s.FindTask(taskID)
		var inheritedDeps inheritedDepSet
		if task != nil {
			if task.PlanHandoffRetired() {
				return fmt.Errorf("task %q handoff was retired by %s", task.ID, task.RetiredBy())
			}
			if err := canonicalizeTaskDependsOnForTransition(s, resolver, task); err != nil {
				return fmt.Errorf("canonicalize dependencies: %w", err)
			}
			inheritedDeps, err = computeInheritedDeps(s, task, transitionName, resolver)
			if err != nil {
				return fmt.Errorf("inherited deps: %w", err)
			}
		}

		if err := proceedTransaction(blackboard, s, projectRoot, taskID, transitionName, tDef, inheritedDeps, resolver, now, result); err != nil {
			return err
		}

		// Add children to sprint scope so they appear in the next sprint
		s.Sprint.Scope.Planned = append(s.Sprint.Scope.Planned, result.ChildTaskIDs...)

		return nil
	}

	preflightState, err := blackboard.Read()
	if err != nil {
		return nil, fmt.Errorf("proceed failed: read state preflight: %w", err)
	}
	allocationSnapshot := db.CloneState(preflightState).FindTask(taskID)
	allocationCheck := validateDirectCodingGeneration(projectRoot, preflightState, allocationSnapshot)
	if allocationCheck.err != nil {
		return nil, fmt.Errorf("proceed failed: %w", allocationCheck.err)
	}
	// Exercise the exact transition path against the read snapshot so invalid
	// requests retain their native errors before integration reconciliation. The
	// in-memory mutations are discarded; the transaction below rechecks them.
	// A replacing plan therefore runs its clone, retirements, validation and
	// planReplacementTestHooks twice.
	if err := execute(preflightState, newResult()); err != nil {
		return nil, fmt.Errorf("proceed failed: %w", err)
	}

	result := newResult()

	mutate := func(completionAuthorization *effectiveIntegrationCompletionAuthorization) error {
		return blackboard.Modify(func(s *models.State) error {
			if allocationSnapshot != nil && models.HasCodingAllocation(allocationSnapshot.Output) {
				current := s.FindTask(taskID)
				if err := allocationCheck.validateState(s, current); err != nil {
					return err
				}
			}
			if err := completionAuthorization.validateState(s, false); err != nil {
				return err
			}
			return execute(s, result)
		})
	}
	if allocationCheck.commit != "" {
		err = withEffectiveIntegrationCompletionLinearization(projectRoot, "proceed direct coding allocation", func() error {
			authorization, err := authorizeEffectiveIntegrationCompletion(projectRoot, false)
			if err != nil {
				return err
			}
			runBeforeEffectiveIntegrationProgressionMutationTestHook()
			if err := verifyDirectCodingIntegration(projectRoot, preflightState.Config.IntegrationBranch, map[string]directCodingGenerationCheck{taskID: allocationCheck}); err != nil {
				return err
			}
			return mutate(authorization)
		})
	} else {
		err = withEffectiveIntegrationCompletionAuthorization(projectRoot, "proceed", false, mutate)
	}

	if err != nil {
		return nil, fmt.Errorf("proceed failed: %w", err)
	}
	for _, warning := range cleanupRetiredOriginals(blackboard, projectRoot, []ProceedResult{*result}) {
		log.Printf("WARNING: proceed: %s", warning)
	}

	return result, nil
}

// proceedManyToOneInner handles many-to-one transition logic. The trigger task
// identifies the cohort (all siblings sharing a parent_task with the same role_pair).
// The transition fires only when ALL cohort members are at the required status
// (or MERGED, for the ExecuteAvailableTransitions path). Creates one child task
// linked to all N cohort members and sets transitions_executed on all of them.
func proceedManyToOneInner(s *models.State, taskID, transitionName string, tDef transitionDef, inheritedDeps inheritedDepSet, resolver *pipeline.Resolver, now time.Time, result *ProceedResult) error {
	task, err := selectedTransitionTask(s, taskID, transitionName, resolver)
	if err != nil {
		return err
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
	if err := requireCohortReady(cohort, tDef.requiredStatus); err != nil {
		return err
	}

	// Idempotency: a member already executed means a crash left the child, or
	// not. An existing child is repaired; a missing one falls through to creation.
	if cohortTransitionExecuted(cohort, transitionName) {
		if existing := s.FindTask(manyToOneChildID(sharedParentID, tDef.taskSlug)); existing != nil {
			return repairExistingCohortChild(s, resolver, existing, inheritedDeps, cohort, transitionName, sharedParentID)
		}
	}

	child, err := newCohortChild(s, resolver, cohort, sharedParentID, tDef, inheritedDeps, now)
	if err != nil {
		return err
	}
	// Set transitions_executed on ALL cohort members after child dependency validation.
	markCohortExecuted(cohort, transitionName)

	s.Tasks = append(s.Tasks, child)
	result.ChildTaskIDs = append(result.ChildTaskIDs, child.ID)
	result.CohortTaskIDs = cohortMemberIDs(cohort)

	// Record history on the trigger task
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventTransitionExecuted,
		Extra: map[string]any{
			"transition":    transitionName,
			"cardinality":   string(models.CardinalityManyToOne),
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
func proceedInner(s *models.State, taskID, transitionName string, tDef transitionDef, inheritedDeps inheritedDepSet, resolver *pipeline.Resolver, now time.Time, result *ProceedResult) error {
	if err := admitTransitionSource(s, taskID, transitionName, tDef, resolver); err != nil {
		return err
	}
	if tDef.cardinality == models.CardinalityManyToOne {
		return proceedManyToOneInner(s, taskID, transitionName, tDef, inheritedDeps, resolver, now, result)
	}
	task, err := selectedTransitionTask(s, taskID, transitionName, resolver)
	if err != nil {
		return err
	}

	if task.RolePair != tDef.sourceRolePair {
		return fmt.Errorf("task %q has role_pair %q, but transition %q requires source role_pair %q",
			taskID, task.RolePair, transitionName, tDef.sourceRolePair)
	}

	if task.Status != tDef.requiredStatus && task.Status != models.TaskStatusMerged {
		return fmt.Errorf("task %q must be at %s (or MERGED) for transition %q (current: %s)",
			taskID, tDef.requiredStatus, transitionName, task.Status)
	}

	if task.TransitionsExecuted[transitionName] {
		return recoverCrashedTransition(s, task, taskID, transitionName, tDef, inheritedDeps, resolver, now, result)
	}
	if task.PlanCheckVerdictOf() == models.PlanCheckHeld {
		return fmt.Errorf("task %q is held for human action (%s); an operator must run plan-check %s --clear first", taskID, task.PlanCheck.Ask, taskID)
	}

	gen, err := generateTransitionChildren(s, resolver, task, transitionName, tDef, inheritedDeps, now, result)
	if err != nil {
		return err
	}
	recordGeneratedTransition(s, task, transitionName, gen, now, result)
	return nil
}

// admitTransitionSource refuses a source task whose hand-off is invalid,
// fenced by a plan amendment or retired, before any cardinality-specific work.
func admitTransitionSource(s *models.State, taskID, transitionName string, tDef transitionDef, resolver *pipeline.Resolver) error {
	task := s.FindTask(taskID)
	if task == nil {
		return nil
	}
	if task.PlanCheck != nil && selectsValidationNotes(resolver, task, transitionName) {
		if err := models.ValidatePlanValidationNotes(task.PlanCheck.Notes, len(task.Output)); err != nil {
			return fmt.Errorf("task %s validation notes: %w", taskID, err)
		}
		if len(task.PlanCheck.Notes) > 0 && (task.PlanCheck.Verdict != models.PlanCheckPassed || tDef.cardinality != models.CardinalityPerSubtask) {
			return fmt.Errorf("validation notes require a passed per-subtask hand-off")
		}
	}
	if task.PlanGenerationFenced() {
		return fmt.Errorf("task %q is fenced by a reviewed plan amendment", task.ID)
	}
	if task.PlanHandoffRetired() {
		return fmt.Errorf("task %q handoff was retired by %s", task.ID, task.RetiredBy())
	}
	return nil
}

// selectedTransitionTask returns the source task if the transition is selected
// by its output and its direct coding allocation is valid.
func selectedTransitionTask(s *models.State, taskID, transitionName string, resolver *pipeline.Resolver) (*models.Task, error) {
	task := s.FindTask(taskID)
	if task == nil {
		return nil, fmt.Errorf("task %q not found", taskID)
	}
	if resolver != nil && !resolver.TransitionApplies(transitionName, task.Output) {
		return nil, fmt.Errorf("transition %q is not selected by task %q output", transitionName, taskID)
	}
	if resolver != nil {
		if err := validateDirectCodingAllocation(resolver, task, task.Output); err != nil {
			return nil, err
		}
	}
	return task, nil
}

// selectsValidationNotes reports whether the plan's passed hand-off notes
// reach the children of this transition.
func selectsValidationNotes(resolver *pipeline.Resolver, task *models.Task, transitionName string) bool {
	return resolver != nil && NewPlanHandoffDomain(resolver).GatesTransition(task, transitionName)
}

// pendingTx represents a transition queued for execution in ExecuteAvailableTransitions.
type pendingTx struct {
	taskID  string
	taskIdx int
	name    string
	tDef    transitionDef
	origIdx int // original collection index for stable topo-sort tie-breaking
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
//
// It returns only the executed transitions and logs the rest; callers that must
// surface skipped transitions use ExecuteAvailableTransitionsReport.
func ExecuteAvailableTransitions(projectRoot string, triggerFilter string) ([]ProceedResult, error) {
	report, err := ExecuteAvailableTransitionsReport(projectRoot, triggerFilter)
	if err != nil {
		return nil, err
	}
	for _, failure := range report.Failures {
		log.Printf("WARNING: ExecuteAvailableTransitions: %s", failure)
	}
	return report.Results, nil
}

// TransitionFailure names a transition that was due but did not execute.
type TransitionFailure struct {
	SourceTaskID string
	Transition   string // empty when no transition could be resolved for the task
	Error        string
}

func (f TransitionFailure) String() string {
	if f.Transition == "" {
		return fmt.Sprintf("task %s: %s", f.SourceTaskID, f.Error)
	}
	return fmt.Sprintf("task %s transition %s: %s", f.SourceTaskID, f.Transition, f.Error)
}

// TransitionReport is the outcome of one ExecuteAvailableTransitionsReport pass.
// Failures exclude normal waits: already-executed transitions and many-to-one
// cohorts whose live members have not reached the required status.
type TransitionReport struct {
	Results  []ProceedResult
	Failures []TransitionFailure
	Warnings []string
}

// admitsHandoff decides a gated transition under the state lock that also
// creates its children, so a plan merged or re-dispositioned since any review
// is judged on current state.
func admitsHandoff(domain PlanHandoffDomain, s *models.State, task *models.Task, admission TransitionAdmission) bool {
	class, _ := domain.Classify(s, task)
	if admission == AdmitReviewed {
		return class == PlanHandoffPassed
	}
	return class != PlanHandoffHeld
}

// TransitionAdmission selects who authorizes the reviewed plan hand-off
// (PlanHandoffDomain) during a transition pass.
type TransitionAdmission int

const (
	// AdmitOperator: a human resumed or proceeded, which authorizes every
	// hand-off except a plan held for a human action.
	AdmitOperator TransitionAdmission = iota
	// AdmitReviewed: an automatic path (auto-resume, supervisor PreWork). A
	// reviewed hand-off runs only when its plan is classified passed.
	AdmitReviewed
)

// ExecuteAvailableTransitionsReport runs ExecuteAvailableTransitions' passes
// under operator admission and also reports every due transition that did not
// execute, so a stuck pipeline is visible instead of only logged.
func ExecuteAvailableTransitionsReport(projectRoot string, triggerFilter string) (TransitionReport, error) {
	return ExecuteTransitionsReportWith(projectRoot, triggerFilter, AdmitOperator)
}

// ExecuteTransitionsReportWith is ExecuteAvailableTransitionsReport with an
// explicit admission. Plans the admission does not authorize are skipped as a
// normal wait, not reported as failures: they stay eligible for their own
// orchestrator review or human action.
func ExecuteTransitionsReportWith(projectRoot string, triggerFilter string, admission TransitionAdmission) (TransitionReport, error) {
	resolver, _, err := loadResolverWithRuntimePolicy(projectRoot)
	if err != nil {
		return TransitionReport{}, fmt.Errorf("failed to load pipeline config: %w", err)
	}
	handoff := NewPlanHandoffDomain(resolver)

	statePath := paths.New(projectRoot).StatePath()
	blackboard := db.For(statePath)

	// Pre-read state to check if goal.base_commit needs snapshotting.
	// Git operation must happen outside Modify callback.
	var integrationHEAD string
	preState, preErr := blackboard.Read()
	directChecks := make(map[string]directCodingGenerationCheck)
	if preErr == nil {
		for i := range preState.Tasks {
			task := &preState.Tasks[i]
			if task.Status != models.TaskStatusMerged || !models.HasCodingAllocation(task.Output) || task.PlanGenerationFenced() || task.PlanHandoffRetired() || task.TransitionsExecuted["replanned"] {
				continue
			}
			needsGeneration := triggerFilter != "auto" && handoff.Pending(task)
			for name, executed := range task.TransitionsExecuted {
				needsGeneration = needsGeneration || (executed && isTransitionIncomplete(preState, task, name, resolver))
			}
			if needsGeneration {
				directChecks[task.ID] = validateDirectCodingGeneration(projectRoot, preState, task)
			}
		}
	}
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
	var failures []TransitionFailure
	var observedFailures []PlanHandoffFailure
	var skippedHandoffs []TransitionFailure
	fail := func(taskID, transition string, err error) {
		failures = append(failures, TransitionFailure{SourceTaskID: taskID, Transition: transition, Error: err.Error()})
	}

	mutate := func() error {
		return blackboard.Modify(func(s *models.State) error {
			now = time.Now().UTC()
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
				if task.TransitionsExecuted["replanned"] || task.PlanHandoffRetired() || task.PlanGenerationFenced() {
					continue
				}

				approvedStatus, err := resolver.ApprovedStatus(task.RolePair)
				if err != nil {
					fail(task.ID, "", fmt.Errorf("unknown role-pair %q: %w", task.RolePair, err))
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
					if !resolver.TransitionApplies(transitionName, task.Output) {
						continue
					}
					if admission == AdmitReviewed && handoff.TransitionFailure(s, task, transitionName) != nil {
						continue
					}
					if handoff.GatesTransition(task, transitionName) && !admitsHandoff(handoff, s, task, admission) {
						continue
					}
					tDef, err := buildTransitionDefFromPipeline(resolver, transitionName)
					if err != nil {
						fail(task.ID, transitionName, err)
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
				if task.TransitionsExecuted["replanned"] || task.PlanHandoffRetired() || task.PlanGenerationFenced() {
					continue
				}
				for transName := range task.TransitionsExecuted {
					if !isTransitionIncomplete(s, task, transName, resolver) {
						continue
					}
					tDef, err := buildTransitionDefFromPipeline(resolver, transName)
					if err != nil {
						fail(task.ID, transName, fmt.Errorf("crash recovery: %w", err))
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
			// Artifact reads ran outside Modify. Fence that checked allocation before
			// either ordinary generation or recovery can publish its coding children.
			admitted := pending[:0]
			for _, candidate := range pending {
				task := s.FindTask(candidate.taskID)
				if models.HasCodingAllocation(task.Output) {
					check, checked := directChecks[task.ID]
					if !checked || check.err != nil {
						if check.err == nil {
							check.err = fmt.Errorf("direct coding allocation was not checked before generation")
						}
						fail(task.ID, candidate.name, check.err)
						continue
					}
					if err := check.validateState(s, task); err != nil {
						fail(task.ID, candidate.name, err)
						continue
					}
				}
				admitted = append(admitted, candidate)
			}
			pending = admitted

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
					fail(p.taskID, p.name, fmt.Errorf("blocked by dependency cycle among %v", cycleMemberIDs))
				}
			}

			for _, p := range downstreamBlocked {
				fail(p.taskID, p.name, errors.New("blocked by an upstream dependency cycle"))
			}

			// Phase 3: Execute in sorted order
			planningAttemptStamped := false
			for _, p := range sorted {
				task := s.FindTask(p.taskID)
				// Earlier transitions in this pass may have repaired selected inputs.
				if admission == AdmitReviewed && handoff.TransitionFailure(s, task, p.name) != nil {
					continue
				}
				if !planningAttemptStamped && handoff.Pending(task) && !task.TransitionsExecuted[p.name] {
					// Use the pass start, so plans arriving after this attempt stay fresh.
					s.Sprint.Timeline.TransitionsAttemptedAt = &now
					planningAttemptStamped = true
				}
				var originalDeps []string
				var inheritedDeps inheritedDepSet
				if task != nil {
					originalDeps = task.DependsOn
					if err := canonicalizeTaskDependsOnForTransition(s, resolver, task); err != nil {
						fail(p.taskID, p.name, fmt.Errorf("dependency canonicalization: %w", err))
						continue
					}
					var depErr error
					inheritedDeps, depErr = computeInheritedDeps(s, task, p.name, resolver)
					if depErr != nil {
						fail(p.taskID, p.name, fmt.Errorf("inherited deps: %w", depErr))
						continue
					}
				}

				result := ProceedResult{
					SourceTaskID:   p.taskID,
					TransitionName: p.name,
				}

				if err := proceedTransaction(blackboard, s, projectRoot, p.taskID, p.name, p.tDef, inheritedDeps, resolver, now, &result); err != nil {
					var refusal *handoffInputError
					if task != nil && errors.As(err, &refusal) && handoff.GatesTransition(task, p.name) && !task.TransitionsExecuted[p.name] {
						// Initial pure refusals leave no child/output mutations published.
						// Undo the earlier source-dependency normalization as well.
						task.DependsOn = originalDeps
						if observation := handoff.recordFailure(s, task, p.name, refusal, now); observation != nil {
							observedFailures = append(observedFailures, *observation)
						}
					}
					if !errors.Is(err, errTransitionAlreadyExecuted) && !errors.Is(err, errManyToOneCohortIncomplete) {
						fail(p.taskID, p.name, err)
						// A passed plan's skip that no observation classifies stays
						// retryable, but must not live in a WARN line only (D-172).
						if task != nil && task.PlanCheckVerdictOf() == models.PlanCheckPassed && handoff.GatesTransition(task, p.name) &&
							!task.TransitionsExecuted[p.name] && handoff.TransitionFailure(s, task, p.name) == nil {
							skippedHandoffs = append(skippedHandoffs, failures[len(failures)-1])
						}
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
	}
	if len(directChecks) > 0 {
		err = withEffectiveIntegrationCompletionLinearization(projectRoot, "generate direct coding allocations", func() error {
			runBeforeEffectiveIntegrationProgressionMutationTestHook()
			if err := verifyDirectCodingIntegration(projectRoot, preState.Config.IntegrationBranch, directChecks); err != nil {
				return err
			}
			return mutate()
		})
	} else {
		err = mutate()
	}

	if err != nil {
		return TransitionReport{}, fmt.Errorf("execute available transitions failed: %w", err)
	}
	for _, warning := range cleanupRetiredOriginals(blackboard, projectRoot, results) {
		log.Printf("WARNING: ExecuteAvailableTransitions: %s", warning)
	}

	var warnings []string
	for _, observation := range observedFailures {
		if alertErr := writeHandoffFailureAlert(projectRoot, observation, now); alertErr != nil {
			warning := fmt.Sprintf("plan handoff failure alert write failed: %v", alertErr)
			warnings = append(warnings, warning)
			log.Printf("WARNING: ExecuteAvailableTransitions: %s", warning)
		}
	}
	for _, skipped := range skippedHandoffs {
		if alertErr := writeHandoffSkipAlert(projectRoot, skipped, now); alertErr != nil {
			warning := fmt.Sprintf("plan handoff skip alert write failed: %v", alertErr)
			warnings = append(warnings, warning)
			log.Printf("WARNING: ExecuteAvailableTransitions: %s", warning)
		}
	}
	return TransitionReport{Results: results, Failures: failures, Warnings: warnings}, nil
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
	sourcePair, err := resolver.TransitionSourceRolePair(transitionName)
	if err != nil {
		return transitionDef{}, fmt.Errorf("invalid source role-pair in transition %q: %w", transitionName, err)
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
		sourceRolePair:  sourcePair,
		requiredStatus:  fromStatus,
		targetStatus:    toStatus,
		cardinality:     td.Cardinality,
		targetRolePair:  targetPair,
		doerDisplayName: doerDisplay,
		taskType:        childTaskType,
		taskSlug:        td.TaskSlugOrName(),
	}, nil
}

// AvailableManualTransitions returns the available manual transitions for a task.
// Transitions are read from the frozen pipeline config.
// Returns nil if no transitions are available.
func AvailableManualTransitions(task *models.Task, projectRoot string) []string {
	resolver, _, err := loadResolverWithRuntimePolicy(projectRoot)
	if err != nil {
		return nil
	}
	var selected []string
	for _, name := range resolver.AvailableManualTransitions(task.Status, task.TransitionsExecuted) {
		if resolver.TransitionApplies(name, task.Output) {
			selected = append(selected, name)
		}
	}
	return selected
}

// resolveTransitionDefFrom validates and resolves a manual transition definition
// from an already-loaded resolver, avoiding double-loading in Proceed.
func resolveTransitionDefFrom(resolver *pipeline.Resolver, cfg *pipeline.PipelineConfig, transitionName string) (transitionDef, error) {
	if resolver.PipelineTransitionsSuppressed() && resolver.IsPipelineTransition(transitionName) {
		return transitionDef{}, fmt.Errorf("transition %q is a pipeline-transition disabled by config.no_follow_up", transitionName)
	}

	td, err := resolver.Transition(transitionName)
	if err != nil {
		names := allTransitionNames(cfg)
		return transitionDef{}, fmt.Errorf("unknown transition %q (available: %s)", transitionName, strings.Join(names, ", "))
	}

	// Only manual transitions are executable via liza proceed.
	// Auto transitions are reserved for supervisor execution.
	if td.Trigger != models.TransitionTriggerManual {
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
