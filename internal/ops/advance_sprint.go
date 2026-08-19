package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"gopkg.in/yaml.v3"
)

// AdvanceSprintResult contains the outcome of advancing to a new sprint.
type AdvanceSprintResult struct {
	ArchivedSprintID string
	NewSprintID      string
	NewSprintNumber  int
	CarriedTasks     []string
	ArchivePath      string
}

// sprintAdvancePlan holds validated, pre-computed data for a sprint advance.
// Created by planSprintAdvance (read-only), applied by applySprintAdvance (mutates state).
type sprintAdvancePlan struct {
	archivedSprint models.Sprint
	newSprintID    string
	newNumber      int
	carriedTasks   []string
}

// AdvanceSprint archives the current sprint and creates a new one.
// Non-terminal tasks are carried forward into the new sprint's planned scope.
//
// The archive file is written before state is mutated, so an archive write
// failure aborts the operation with no state change. This prevents data loss
// of full sprint detail.
//
// Precondition: sprint is at CHECKPOINT and all planned tasks are terminal.
// All precondition checks, archive write, and state mutation happen inside a
// single Modify to prevent TOCTOU races.
func AdvanceSprint(projectRoot string) (*AdvanceSprintResult, error) {
	lizaPaths := paths.New(projectRoot)
	statePath := lizaPaths.StatePath()
	blackboard := db.For(statePath)
	preflightState, err := blackboard.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read state before sprint advance: %w", err)
	}
	if _, err := planSprintAdvance(preflightState, time.Now().UTC(), projectRoot); err != nil {
		return nil, fmt.Errorf("failed to advance sprint: %w", err)
	}
	var result AdvanceSprintResult

	err = withEffectiveIntegrationCompletionAuthorization(projectRoot, "advance sprint", false, func(completionAuthorization *effectiveIntegrationCompletionAuthorization) error {
		return blackboard.Modify(func(s *models.State) error {
			if err := completionAuthorization.validateState(s, false); err != nil {
				return err
			}
			plan, err := planSprintAdvance(s, time.Now().UTC(), projectRoot)
			if err != nil {
				return err
			}

			archivePath := lizaPaths.SprintArchivePath(plan.archivedSprint.Number)

			// Write archive BEFORE mutating state. If this fails, Modify aborts
			// and state is unchanged — no data loss.
			if err := writeSprintArchive(archivePath, &plan.archivedSprint); err != nil {
				return fmt.Errorf("archive write failed (state unchanged): %w", err)
			}

			applySprintAdvance(s, plan)

			result = AdvanceSprintResult{
				ArchivedSprintID: plan.archivedSprint.ID,
				NewSprintID:      plan.newSprintID,
				NewSprintNumber:  plan.newNumber,
				CarriedTasks:     plan.carriedTasks,
				ArchivePath:      archivePath,
			}
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to advance sprint: %w", err)
	}

	return &result, nil
}

// planSprintAdvance validates preconditions and computes all derived values
// for a sprint advance without mutating state. Returns a plan that can be
// applied via applySprintAdvance.
func planSprintAdvance(s *models.State, now time.Time, projectRoot string) (*sprintAdvancePlan, error) {
	if s.Sprint.Status != models.SprintStatusCheckpoint {
		return nil, fmt.Errorf("cannot advance sprint: status is %s, expected CHECKPOINT", s.Sprint.Status)
	}
	allTerminal, termErr := allPlannedTasksTerminalForProject(s, projectRoot)
	if termErr != nil {
		return nil, fmt.Errorf("cannot advance sprint: %w", termErr)
	}
	if !allTerminal {
		return nil, fmt.Errorf("cannot advance sprint: not all planned tasks are terminal")
	}

	detCtx, ppErr := loadDetectionContextForAdvance(projectRoot)
	if ppErr != nil {
		return nil, fmt.Errorf("cannot advance sprint: %w", ppErr)
	}
	return buildSprintAdvancePlan(s, now, detCtx)
}

// applySprintAdvance mutates state to record the completed sprint in history
// and create a new sprint. Must be called only after the archive file has been
// successfully written.
func applySprintAdvance(s *models.State, plan *sprintAdvancePlan) {
	s.SprintHistory = append(s.SprintHistory, models.SprintSummary{
		ID:        plan.archivedSprint.ID,
		Number:    plan.archivedSprint.Number,
		Status:    models.SprintStatusCompleted,
		Started:   plan.archivedSprint.Timeline.Started,
		Ended:     *plan.archivedSprint.Timeline.Ended,
		TasksDone: plan.archivedSprint.Metrics.TasksDone,
	})

	s.Sprint = models.Sprint{
		ID:      plan.newSprintID,
		Number:  plan.newNumber,
		GoalRef: s.Goal.ID,
		Scope: models.SprintScope{
			Planned: plan.carriedTasks,
			Stretch: []string{},
		},
		Timeline: models.SprintTimeline{
			Started: *plan.archivedSprint.Timeline.Ended,
		},
		Status:  models.SprintStatusInProgress,
		Metrics: models.SprintMetrics{},
	}
}

// writeSprintArchive writes the full sprint struct to a YAML archive file.
func writeSprintArchive(archivePath string, sprint *models.Sprint) error {
	data, err := yaml.Marshal(sprint)
	if err != nil {
		return fmt.Errorf("failed to marshal sprint for archive: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(archivePath), 0755); err != nil {
		return fmt.Errorf("failed to create archive directory: %w", err)
	}

	if err := os.WriteFile(archivePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write archive file: %w", err)
	}

	return nil
}

// planSprintAdvanceFromCompleted validates preconditions and computes all
// derived values for advancing from a COMPLETED sprint. Unlike planSprintAdvance
// (which requires CHECKPOINT), this handles the sub-pipeline flow where the
// sprint was marked COMPLETED after all tasks reached sprint-terminal state.
func planSprintAdvanceFromCompleted(s *models.State, now time.Time, projectRoot string) (*sprintAdvancePlan, error) {
	if s.Sprint.Status != models.SprintStatusCompleted {
		return nil, fmt.Errorf("cannot advance sprint: status is %s, expected COMPLETED", s.Sprint.Status)
	}

	detCtx, err := loadDetectionContextForAdvance(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("cannot advance sprint: %w", err)
	}
	if blocked := ApprovedPlanningTasksWithUnmergedOutput(s, detCtx.planningPairs, detCtx.planningApprovedStatuses); len(blocked) > 0 {
		return nil, fmt.Errorf("cannot advance sprint: approved planning task(s) must be merged before output can be carried forward: %s (run `%s <task-id>` first)", strings.Join(blocked, ", "), brand.Command("wt-merge"))
	}
	return buildSprintAdvancePlan(s, now, detCtx)
}

// buildSprintAdvancePlan is the shared implementation for sprint advance planning.
// It snapshots the current sprint for archive, normalizes legacy numbering, and
// computes carried tasks. Callers validate preconditions before calling this.
//
// detCtx provides pipeline-aware detection data: sprint terminal states,
// planning pairs (transition sources), and many-to-one transitions.
// Tasks in pipeline-defined terminal states are NOT carried forward.
func buildSprintAdvancePlan(s *models.State, now time.Time, detCtx *advanceDetectionContext) (*sprintAdvancePlan, error) {
	archivedSprint := s.Sprint
	if archivedSprint.Number == 0 {
		archivedSprint.Number = 1
	}
	archivedSprint.Status = models.SprintStatusCompleted
	ended := now
	archivedSprint.Timeline.Ended = &ended

	newNumber := archivedSprint.Number + 1
	newSprintID := fmt.Sprintf("sprint-%d", newNumber)
	carriedTasks := collectNonTerminalTaskIDs(s, detCtx.sprintTerminals)
	carriedTasks = append(carriedTasks, collectMergedPlanningWithUnconsumedOutput(s, detCtx.planningPairs)...)
	carriedTasks = append(carriedTasks, collectMergedManyToOneWithUnfiredTransition(s, detCtx.m2oTransitions)...)

	return &sprintAdvancePlan{
		archivedSprint: archivedSprint,
		newSprintID:    newSprintID,
		newNumber:      newNumber,
		carriedTasks:   carriedTasks,
	}, nil
}

// collectNonTerminalTaskIDs returns IDs of tasks not in a terminal state.
// Uses pipeline-aware terminal check so that pipeline-defined terminal states
// (e.g. INTEGRATION_ANALYSIS_CLEAN) are recognized and not carried forward.
func collectNonTerminalTaskIDs(state *models.State, pipelineTerminals []models.TaskStatus) []string {
	var carried []string
	for _, task := range state.Tasks {
		if !task.Status.IsPipelineSprintTerminal(pipelineTerminals) {
			carried = append(carried, task.ID)
		}
	}
	return carried
}

// IsUnconsumedPlanningOutput reports whether a task is a merged planning task
// with output that has not yet been expanded into child tasks. Used by both
// sprint advance (carry-forward) and orchestrator wake detection (PLANNING_COMPLETE).
func IsUnconsumedPlanningOutput(task *models.Task, planningPairs map[string]bool) bool {
	if task == nil || task.Status != models.TaskStatusMerged || len(task.Output) == 0 {
		return false
	}
	if len(task.TransitionsExecuted) > 0 {
		return false // transitions already fired — children exist
	}
	return IsPlanningPair(task.RolePair, planningPairs)
}

// IsTransitionCycleBlocked checks if a task has a transition_cycle_blocked history event.
func IsTransitionCycleBlocked(task *models.Task) bool {
	for _, h := range task.History {
		if h.Event == models.TaskEventTransitionCycleBlocked {
			return true
		}
	}
	return false
}

// HasCycleBlockedDependency checks whether any upstream dependency is
// cycle-blocked, transitively, from the current task graph.
func HasCycleBlockedDependency(task *models.Task, state *models.State) bool {
	if task == nil || state == nil {
		return false
	}

	memo := make(map[string]bool)
	visiting := make(map[string]bool)

	var blocked func(taskID string) bool
	blocked = func(taskID string) bool {
		if result, ok := memo[taskID]; ok {
			return result
		}
		if visiting[taskID] {
			return false
		}

		dep := state.FindTask(taskID)
		if dep == nil {
			memo[taskID] = false
			return false
		}
		if IsTransitionCycleBlocked(dep) {
			memo[taskID] = true
			return true
		}

		visiting[taskID] = true
		defer delete(visiting, taskID)

		for _, upstreamID := range dep.DependsOn {
			if blocked(upstreamID) {
				memo[taskID] = true
				return true
			}
		}

		memo[taskID] = false
		return false
	}

	for _, depID := range task.DependsOn {
		if blocked(depID) {
			return true
		}
	}
	return false
}

// IsPlanningCompleteEligible returns true if a task has unconsumed planning output
// AND is not cycle-blocked directly or transitively via an upstream dependency.
// Used by wake detection and prompt rendering to exclude cycle-blocked tasks from
// PLANNING_COMPLETE triggering.
// IsUnconsumedPlanningOutput remains unchanged for carry-forward, replan, and checkpoint.
func IsPlanningCompleteEligible(task *models.Task, planningPairs map[string]bool, state *models.State) bool {
	return IsUnconsumedPlanningOutput(task, planningPairs) &&
		!IsTransitionCycleBlocked(task) &&
		!HasCycleBlockedDependency(task, state)
}

// collectMergedPlanningWithUnconsumedOutput returns IDs of planned tasks with
// unconsumed planning output. These need to be carried into the new sprint so
// the orchestrator can fire PLANNING_COMPLETE.
//
// Iterates state.Sprint.Scope.Planned (not all tasks) to avoid reintroducing
// tasks from prior sprints.
func collectMergedPlanningWithUnconsumedOutput(state *models.State, planningPairs map[string]bool) []string {
	var carried []string
	for _, taskID := range state.Sprint.Scope.Planned {
		task := state.FindTask(taskID)
		if IsUnconsumedPlanningOutput(task, planningPairs) {
			carried = append(carried, taskID)
		}
	}
	return carried
}

// ApprovedPlanningTasksWithUnmergedOutput returns sprint-scoped planning task
// IDs that have output and are approved but not yet merged. Results preserve
// sprint scope order.
func ApprovedPlanningTasksWithUnmergedOutput(state *models.State, planningPairs map[string]bool, approvedStatuses map[string]models.TaskStatus) []string {
	var blocked []string
	for _, taskID := range state.Sprint.Scope.Planned {
		task := state.FindTask(taskID)
		if task == nil || len(task.Output) == 0 {
			continue
		}
		if task.Status == models.TaskStatusMerged || !IsPlanningPair(task.RolePair, planningPairs) {
			continue
		}
		if task.Status == approvedStatuses[task.RolePair] {
			blocked = append(blocked, taskID)
		}
	}
	return blocked
}

// advanceDetectionContext holds the detection data needed for sprint advance.
type advanceDetectionContext struct {
	sprintTerminals          []models.TaskStatus
	planningPairs            map[string]bool
	planningApprovedStatuses map[string]models.TaskStatus
	m2oTransitions           []ManyToOneTransitionInfo
}

// IsManyToOneReady checks if a MERGED task is part of a complete many-to-one
// cohort whose transition has not yet been executed. Used by both carry-forward
// and wake detection. A cohort is complete when all siblings (same parent, same
// role_pair) are MERGED with no transitions_executed for the transition name.
func IsManyToOneReady(task *models.Task, state *models.State, m2oTransitions []ManyToOneTransitionInfo) bool {
	if task == nil || task.Status != models.TaskStatusMerged {
		return false
	}
	sharedParentID := task.CohortParentID()
	if sharedParentID == "" {
		return false
	}

	for _, m2o := range m2oTransitions {
		if task.RolePair != m2o.SourceRolePair {
			continue
		}
		if task.TransitionsExecuted[m2o.Name] {
			continue
		}

		// Check if ALL siblings in the cohort are MERGED with unfired transition
		allReady := true
		for i := range state.Tasks {
			sibling := &state.Tasks[i]
			if sibling.RolePair != task.RolePair {
				continue
			}
			siblingParents := sibling.EffectiveParentTasks()
			if !slices.Contains(siblingParents, sharedParentID) {
				continue
			}
			if sibling.Status != models.TaskStatusMerged {
				allReady = false
				break
			}
			if sibling.TransitionsExecuted[m2o.Name] {
				allReady = false // already executed — not "unfired"
				break
			}
		}
		if allReady {
			return true
		}
	}
	return false
}

// CountReadyManyToOneCohorts counts distinct many-to-one cohorts where all
// siblings are MERGED with unfired transitions. Deduplicates by (shared parent, transition).
func CountReadyManyToOneCohorts(state *models.State, m2oTransitions []ManyToOneTransitionInfo) int {
	type cohortKey struct {
		parentID       string
		transitionName string
	}
	seen := make(map[cohortKey]bool)
	for _, taskID := range state.Sprint.Scope.Planned {
		task := state.FindTask(taskID)
		if !IsManyToOneReady(task, state, m2oTransitions) {
			continue
		}
		cohortParent := task.CohortParentID()
		if cohortParent == "" {
			continue
		}
		for _, m2o := range m2oTransitions {
			if task.RolePair == m2o.SourceRolePair {
				seen[cohortKey{cohortParent, m2o.Name}] = true
			}
		}
	}
	return len(seen)
}

// collectMergedManyToOneWithUnfiredTransition returns IDs of planned MERGED tasks
// that are part of a complete many-to-one cohort with an unfired transition.
// These must be carried forward so the orchestrator can fire the transition.
// Returns deduplicated IDs (each cohort member appears at most once).
func collectMergedManyToOneWithUnfiredTransition(state *models.State, m2oTransitions []ManyToOneTransitionInfo) []string {
	seen := make(map[string]bool)
	var carried []string
	for _, taskID := range state.Sprint.Scope.Planned {
		if seen[taskID] {
			continue
		}
		task := state.FindTask(taskID)
		if IsManyToOneReady(task, state, m2oTransitions) {
			carried = append(carried, taskID)
			seen[taskID] = true
		}
	}
	return carried
}

// loadDetectionContextForAdvance loads detection context from pipeline config.
// Returns zero-value context (nil maps/slices) when the pipeline config is absent
// (legacy project) — IsPlanningPair falls back to recognizing "code-planning-pair".
// Returns a non-nil error when the config exists but cannot be loaded (parse or
// validation failure), preventing silent fallback that would drop non-legacy pairs.
func loadDetectionContextForAdvance(projectRoot string) (*advanceDetectionContext, error) {
	detCtx, err := LoadPhaseHandoffDetectionContext(projectRoot)
	if err != nil {
		return nil, err
	}
	return &advanceDetectionContext{
		sprintTerminals:          detCtx.SprintTerminals,
		planningPairs:            detCtx.PlanningPairs,
		planningApprovedStatuses: detCtx.PlanningApprovedStatuses,
		m2oTransitions:           detCtx.ManyToOneTransitions,
	}, nil
}

// loadPlanningPairsForAdvance loads planning pairs from pipeline config.
// Returns (nil, nil) when the pipeline config is absent (legacy project) —
// IsPlanningPair falls back to recognizing "code-planning-pair" as the only
// planning pair.
func loadPlanningPairsForAdvance(projectRoot string) (map[string]bool, error) {
	detCtx, err := loadDetectionContextForAdvance(projectRoot)
	if err != nil {
		return nil, err
	}
	return detCtx.planningPairs, nil
}
