package ops

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

// proceedOperation is the lifecycle operation, completion-lock label and command
// name of the transition entry operations.
const proceedOperation = "proceed"

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

	run := proceedRun{
		projectRoot: projectRoot, taskID: taskID, transitionName: transitionName,
		tDef: tDef, resolver: resolver, blackboard: db.For(paths.New(projectRoot).StatePath()), now: time.Now().UTC(),
	}
	preflight, err := run.preflight()
	if err != nil {
		return nil, err
	}

	result := run.newResult()
	if err := run.commit(preflight, result); err != nil {
		return nil, fmt.Errorf("proceed failed: %w", err)
	}
	for _, warning := range cleanupRetiredOriginals(run.blackboard, projectRoot, []ProceedResult{*result}) {
		log.Printf("WARNING: proceed: %s", warning)
	}

	return result, nil
}

// proceedRun is one Proceed call: its request and the resolved pipeline.
type proceedRun struct {
	projectRoot, taskID, transitionName string
	tDef                                transitionDef
	resolver                            *pipeline.Resolver
	blackboard                          *db.Blackboard
	now                                 time.Time
}

// proceedPreflight is what the snapshot preflight hands to the transaction:
// the state it read and the source task's direct-coding allocation check.
type proceedPreflight struct {
	state              *models.State
	allocationSnapshot *models.Task
	allocationCheck    directCodingGenerationCheck
}

func (r *proceedRun) newResult() *ProceedResult {
	return &ProceedResult{SourceTaskID: r.taskID, TransitionName: r.transitionName}
}

// execute applies the transition to s: the sprint gate, the source's canonical
// dependencies and inherited phase-gate dependencies, then the transition itself.
func (r *proceedRun) execute(s *models.State, result *ProceedResult) error {
	// Validate sprint is COMPLETED
	if s.Sprint.Status != models.SprintStatusCompleted {
		return fmt.Errorf("sprint must be COMPLETED before proceeding (current: %s)", s.Sprint.Status)
	}

	task := s.FindTask(r.taskID)
	var inheritedDeps inheritedDepSet
	if task != nil {
		if task.PlanHandoffRetired() {
			return fmt.Errorf("task %q handoff was retired by %s", task.ID, task.RetiredBy())
		}
		if err := canonicalizeTaskDependsOnForTransition(s, r.resolver, task); err != nil {
			return fmt.Errorf("canonicalize dependencies: %w", err)
		}
		var err error
		inheritedDeps, err = computeInheritedDeps(s, task, r.transitionName, r.resolver)
		if err != nil {
			return fmt.Errorf("inherited deps: %w", err)
		}
	}

	if err := proceedTransaction(r.blackboard, s, r.projectRoot, r.taskID, r.transitionName, r.tDef, inheritedDeps, r.resolver, r.now, result); err != nil {
		return err
	}

	// Add children to sprint scope so they appear in the next sprint
	s.Sprint.Scope.Planned = append(s.Sprint.Scope.Planned, result.ChildTaskIDs...)

	return nil
}

// preflight reads a state snapshot, checks the source's direct-coding
// allocation and exercises the exact transition path against the snapshot so
// invalid requests retain their native errors before integration
// reconciliation. The in-memory mutations are discarded; the transaction
// rechecks them. A replacing plan therefore runs its clone, retirements,
// validation and planReplacementTestHooks twice.
func (r *proceedRun) preflight() (proceedPreflight, error) {
	state, err := r.blackboard.Read()
	if err != nil {
		return proceedPreflight{}, fmt.Errorf("proceed failed: read state preflight: %w", err)
	}
	snapshot := db.CloneState(state).FindTask(r.taskID)
	check := validateDirectCodingGeneration(r.projectRoot, state, snapshot)
	if check.err != nil {
		return proceedPreflight{}, fmt.Errorf("proceed failed: %w", check.err)
	}
	if err := r.execute(state, r.newResult()); err != nil {
		return proceedPreflight{}, fmt.Errorf("proceed failed: %w", err)
	}
	return proceedPreflight{state: state, allocationSnapshot: snapshot, allocationCheck: check}, nil
}

// commit runs the transition in one state transaction. The integration
// authorization depends on the allocation: a direct-coding allocation verifies
// its artifacts at the integration HEAD under completion linearization, any
// other transition only needs the effective completion authorization.
func (r *proceedRun) commit(pf proceedPreflight, result *ProceedResult) error {
	mutate := func(completionAuthorization *effectiveIntegrationCompletionAuthorization) error {
		return r.blackboard.Modify(func(s *models.State) error {
			if pf.allocationSnapshot != nil && models.HasCodingAllocation(pf.allocationSnapshot.Output) {
				current := s.FindTask(r.taskID)
				if err := pf.allocationCheck.validateState(s, current); err != nil {
					return err
				}
			}
			if err := completionAuthorization.validateState(s, false); err != nil {
				return err
			}
			return r.execute(s, result)
		})
	}
	if pf.allocationCheck.commit == "" {
		return withEffectiveIntegrationCompletionAuthorization(r.projectRoot, proceedOperation, false, mutate)
	}
	return withEffectiveIntegrationCompletionLinearization(r.projectRoot, "proceed direct coding allocation", func() error {
		authorization, err := authorizeEffectiveIntegrationCompletion(r.projectRoot, false)
		if err != nil {
			return err
		}
		runBeforeEffectiveIntegrationProgressionMutationTestHook()
		if err := verifyDirectCodingIntegration(r.projectRoot, pf.state.Config.IntegrationBranch, map[string]directCodingGenerationCheck{r.taskID: pf.allocationCheck}); err != nil {
			return err
		}
		return mutate(authorization)
	})
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
			transitionExtraKey: transitionName,
			"cardinality":      string(models.CardinalityManyToOne),
			"cohort_size":      len(cohort),
			"cohort_parent":    sharedParentID,
			"children":         1,
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
		return class == models.PlanHandoffPassed
	}
	return class != models.PlanHandoffHeld
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
	pass := &transitionPass{
		projectRoot: projectRoot, triggerFilter: triggerFilter, admission: admission,
		resolver: resolver, handoff: NewPlanHandoffDomain(resolver), blackboard: db.For(paths.New(projectRoot).StatePath()),
	}
	preState := pass.readInputs()

	mutate := func() error { return pass.blackboard.Modify(pass.execute) }
	if len(pass.directChecks) > 0 {
		err = withEffectiveIntegrationCompletionLinearization(projectRoot, "generate direct coding allocations", func() error {
			runBeforeEffectiveIntegrationProgressionMutationTestHook()
			if err := verifyDirectCodingIntegration(projectRoot, preState.Config.IntegrationBranch, pass.directChecks); err != nil {
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
	return pass.afterCommit(), nil
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

	switch pipeline.StateCategory(phase) {
	case pipeline.StateCategoryInitial:
		return resolver.InitialStatus(pair)
	case pipeline.StateCategoryExecuting:
		return resolver.ExecutingStatus(pair)
	case pipeline.StateCategorySubmitted:
		return resolver.SubmittedStatus(pair)
	case pipeline.StateCategoryReviewing:
		return resolver.ReviewingStatus(pair)
	case pipeline.StateCategoryApproved:
		return resolver.ApprovedStatus(pair)
	case pipeline.StateCategoryRejected:
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
