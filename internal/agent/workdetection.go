package agent

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/prompts"
)

// OrchestratorWakeTrigger represents what triggered the orchestrator to wake
type OrchestratorWakeTrigger string

const (
	WakeTriggerInitialPlanning        OrchestratorWakeTrigger = "INITIAL_PLANNING"
	WakeTriggerBlocked                OrchestratorWakeTrigger = "BLOCKED_TASKS"
	WakeTriggerHypothesisExhausted    OrchestratorWakeTrigger = "HYPOTHESIS_EXHAUSTED"
	WakeTriggerImmediateDiscovery     OrchestratorWakeTrigger = "IMMEDIATE_DISCOVERY"
	WakeTriggerHumanNote              OrchestratorWakeTrigger = "HUMAN_NOTE"
	WakeTriggerPlanningComplete       OrchestratorWakeTrigger = "PLANNING_COMPLETE"
	WakeTriggerManyToOneReady         OrchestratorWakeTrigger = "MANY_TO_ONE_READY"
	WakeTriggerCodingComplete         OrchestratorWakeTrigger = "CODING_COMPLETE"
	WakeTriggerSprintComplete         OrchestratorWakeTrigger = "SPRINT_COMPLETE"
	WakeTriggerIntegrationWaiting     OrchestratorWakeTrigger = "INTEGRATION_WAITING"
	WakeTriggerIntegrationBlocked     OrchestratorWakeTrigger = "INTEGRATION_BLOCKED"
	WakeTriggerIntegrationExhausted   OrchestratorWakeTrigger = "INTEGRATION_EXHAUSTED"
	WakeTriggerIntegrationUnavailable OrchestratorWakeTrigger = "INTEGRATION_UNAVAILABLE"
	WakeTriggerNone                   OrchestratorWakeTrigger = "NONE"
)

// OrchestratorWakeResult contains the wake trigger and count
type OrchestratorWakeResult struct {
	Trigger     OrchestratorWakeTrigger
	Count       int
	Integration prompts.EffectiveIntegrationCompletion
}

// ShouldWake reports whether the projected result needs orchestrator work.
// Stable integration outcomes remain visible to diagnostics without causing a
// restart loop.
func (result OrchestratorWakeResult) ShouldWake() bool {
	switch result.Trigger {
	case WakeTriggerNone,
		WakeTriggerIntegrationWaiting,
		WakeTriggerIntegrationBlocked,
		WakeTriggerIntegrationExhausted,
		WakeTriggerIntegrationUnavailable:
		return false
	default:
		return true
	}
}

type orchestratorWakeTriggerSpec struct {
	Trigger     OrchestratorWakeTrigger
	Description string
	Count       func(state *models.State) int
}

var orchestratorWakeTriggerSpecs = []orchestratorWakeTriggerSpec{
	{
		Trigger:     WakeTriggerInitialPlanning,
		Description: "No tasks exist yet, so initial planning is required.",
		Count: func(state *models.State) int {
			if len(state.Tasks) == 0 {
				return 1
			}
			return 0
		},
	},
	{
		Trigger:     WakeTriggerBlocked,
		Description: "Blocked tasks need orchestrator intervention.",
		Count:       ops.CountActionableBlockedTasks,
	},
	{
		Trigger:     WakeTriggerHypothesisExhausted,
		Description: "Tasks with repeated coder failures need orchestrator intervention.",
		Count:       ops.CountActionableHypothesisExhaustedTasks,
	},
	{
		Trigger:     WakeTriggerImmediateDiscovery,
		Description: "Immediate discoveries need orchestrator triage.",
		Count:       countImmediateDiscoveries,
	},
	{
		Trigger:     WakeTriggerHumanNote,
		Description: "Operator notes not yet rendered in a completed orchestrator turn.",
		Count:       ops.CountUnseenHumanNotes,
	},
	// WakeTriggerSprintComplete is handled separately in DetectOrchestratorWakeTriggers
	// because it requires pipeline-aware terminal state checking.
}

// DetectOrchestratorWakeTriggers detects conditions that should wake the orchestrator.
// pipelineTerminals provides pipeline-defined sprint-terminal states (from ops.SprintTerminalStates).
// planningPairs provides role-pairs that are transition sources (from ops.TransitionSourcePairs).
// Pass nil for either to use fallback behavior.
//
// Returns the highest-priority trigger and count of items for that trigger.
// Priority order:
//  1. No tasks (initial planning)
//  2. Blocked tasks — or planning complete instead, when an actionable blocked
//     task waits on a planner's untransitioned output (ops.BlockedTasksAwaitPlanningOutput)
//  3. Hypothesis exhausted (2+ failed_by)
//  4. Immediate discoveries (not yet converted to tasks)
//  5. Operator notes not yet rendered in a completed turn
//  6. Planning complete (merged planning tasks have output[])
//  7. Many-to-one transition ready
//  8. Sprint complete (all planned tasks terminal)
func DetectOrchestratorWakeTriggers(state *models.State, pipelineTerminals []models.TaskStatus, planningPairs map[string]bool, m2oTransitions []ops.ManyToOneTransitionInfo) OrchestratorWakeResult {
	return detectOrchestratorWakeTriggers(state, pipelineTerminals, planningPairs, m2oTransitions, nil, WakeTriggerNone)
}

// DetectOrchestratorWakeTriggersForProject evaluates terminal integration
// state through the authoritative progress decision and prompt projection.
func DetectOrchestratorWakeTriggersForProject(projectRoot string, state *models.State, pipelineTerminals []models.TaskStatus, planningPairs map[string]bool, m2oTransitions []ops.ManyToOneTransitionInfo) OrchestratorWakeResult {
	return detectOrchestratorWakeTriggers(state, pipelineTerminals, planningPairs, m2oTransitions, func() prompts.EffectiveIntegrationCompletion {
		decision, evaluationErr := ops.EvaluateLiveIntegrationProgress(state, projectRoot)
		return prompts.ProjectEffectiveIntegrationCompletion(decision, nil, evaluationErr)
	}, WakeTriggerNone)
}

// DetectOrchestratorWakeTriggersWithIntegrationProjection exposes the pure
// adapter used by tests and read-only consumers that already hold an
// authoritative projection.
func DetectOrchestratorWakeTriggersWithIntegrationProjection(state *models.State, pipelineTerminals []models.TaskStatus, planningPairs map[string]bool, m2oTransitions []ops.ManyToOneTransitionInfo, projection prompts.EffectiveIntegrationCompletion) OrchestratorWakeResult {
	return detectOrchestratorWakeTriggers(state, pipelineTerminals, planningPairs, m2oTransitions, func() prompts.EffectiveIntegrationCompletion {
		return projection
	}, WakeTriggerNone)
}

// revalidateOrchestratorWake retains a selected invocation only while its own
// predicate still holds. Ordinary priority applies again on the next wait.
// The caller owns launch gates; this function only projects work from state.
func revalidateOrchestratorWake(state *models.State, selected OrchestratorWakeResult, pipelineTerminals []models.TaskStatus, planningPairs map[string]bool, m2oTransitions []ops.ManyToOneTransitionInfo, integrationProjection func() prompts.EffectiveIntegrationCompletion) (result, fresh OrchestratorWakeResult) {
	fresh = detectOrchestratorWakeTriggers(state, pipelineTerminals, planningPairs, m2oTransitions, integrationProjection, WakeTriggerNone)
	if selected.Trigger == fresh.Trigger || selected.Trigger == "" || !selected.ShouldWake() {
		return fresh, fresh
	}
	retained := detectOrchestratorWakeTriggers(state, pipelineTerminals, planningPairs, m2oTransitions, integrationProjection, selected.Trigger)
	if retained.ShouldWake() {
		return retained, fresh
	}
	return fresh, fresh
}

// only selects a single predicate for revalidation; NONE uses normal priority.
func detectOrchestratorWakeTriggers(state *models.State, pipelineTerminals []models.TaskStatus, planningPairs map[string]bool, m2oTransitions []ops.ManyToOneTransitionInfo, integrationProjection func() prompts.EffectiveIntegrationCompletion, only OrchestratorWakeTrigger) OrchestratorWakeResult {
	for _, triggerSpec := range orchestratorWakeTriggerSpecs {
		if only != WakeTriggerNone && only != triggerSpec.Trigger {
			continue
		}
		if count := triggerSpec.Count(state); count > 0 {
			// Revalidation (only set) keeps the selected predicate instead.
			if triggerSpec.Trigger == WakeTriggerBlocked && only == WakeTriggerNone &&
				ops.BlockedTasksAwaitPlanningOutput(state, planningPairs) {
				return OrchestratorWakeResult{
					Trigger: WakeTriggerPlanningComplete,
					Count:   countMergedPlanningTasksWithOutput(state, planningPairs),
				}
			}
			return OrchestratorWakeResult{
				Trigger: triggerSpec.Trigger,
				Count:   count,
			}
		}
	}

	// Partial handoff: a merged planning task with unconsumed output is enough
	// to wake the orchestrator, even when unrelated planned tasks are still
	// active. The checkpoint/resume gate still controls when transitions run,
	// but ready design work no longer waits for the entire sprint to finish.
	if state.Sprint.Status != models.SprintStatusCheckpoint &&
		state.Sprint.Status != models.SprintStatusCompleted {
		if n := countMergedPlanningTasksWithOutput(state, planningPairs); n > 0 && (only == WakeTriggerNone || only == WakeTriggerPlanningComplete) {
			return OrchestratorWakeResult{
				Trigger: WakeTriggerPlanningComplete,
				Count:   n,
			}
		}
		if n := countReadyManyToOneCohorts(state, m2oTransitions); n > 0 && (only == WakeTriggerNone || only == WakeTriggerManyToOneReady) {
			return OrchestratorWakeResult{
				Trigger: WakeTriggerManyToOneReady,
				Count:   n,
			}
		}
	}

	// Sprint-complete check: pipeline-aware when terminals are provided,
	// falls back to universal terminals when nil.
	// Guard: suppress when sprint is already CHECKPOINT or COMPLETED to prevent
	// the re-wake loop (supervisor sets COMPLETED → state change fires detection
	// → orchestrator wakes → calls sprint_checkpoint → rejected).
	if (only == WakeTriggerNone || only == WakeTriggerCodingComplete || only == WakeTriggerSprintComplete) && state.AllPlannedTasksTerminalWith(pipelineTerminals) {
		if state.Sprint.Status == models.SprintStatusCheckpoint ||
			state.Sprint.Status == models.SprintStatusCompleted {
			return OrchestratorWakeResult{Trigger: WakeTriggerNone}
		}
		if integrationProjection != nil && (state.Goal.BaseCommit != nil || state.Goal.Integration != nil) {
			result := projectIntegrationWakeResult(integrationProjection(), len(state.Sprint.Scope.Planned))
			if only == WakeTriggerNone || only == result.Trigger {
				return result
			}
			return OrchestratorWakeResult{Trigger: WakeTriggerNone}
		}
		// Detect coding completion: all tasks terminal, coding happened (base_commit set),
		// but no integration task exists yet.
		if state.Goal.BaseCommit != nil && !hasIntegrationTask(state) {
			if only == WakeTriggerSprintComplete {
				return OrchestratorWakeResult{Trigger: WakeTriggerNone}
			}
			return OrchestratorWakeResult{
				Trigger: WakeTriggerCodingComplete,
				Count:   1,
			}
		}
		if only == WakeTriggerCodingComplete {
			return OrchestratorWakeResult{Trigger: WakeTriggerNone}
		}
		return OrchestratorWakeResult{
			Trigger: WakeTriggerSprintComplete,
			Count:   len(state.Sprint.Scope.Planned),
		}
	}

	return OrchestratorWakeResult{Trigger: WakeTriggerNone}
}

func projectIntegrationWakeResult(projection prompts.EffectiveIntegrationCompletion, plannedCount int) OrchestratorWakeResult {
	switch OrchestratorWakeTrigger(projection.WakeTrigger) {
	case WakeTriggerCodingComplete,
		WakeTriggerSprintComplete,
		WakeTriggerIntegrationWaiting,
		WakeTriggerIntegrationBlocked,
		WakeTriggerIntegrationExhausted,
		WakeTriggerIntegrationUnavailable:
	default:
		projection = prompts.ProjectEffectiveIntegrationCompletion(
			ops.IntegrationProgressDecision{},
			nil,
			fmt.Errorf("unknown integration wake projection %q", projection.WakeTrigger),
		)
	}
	count := len(projection.RequestKeys)
	if count == 0 {
		count = len(projection.TaskIDs)
	}
	if projection.Status == "complete" {
		count = plannedCount
	} else if count == 0 {
		count = 1
	}
	return OrchestratorWakeResult{
		Trigger:     OrchestratorWakeTrigger(projection.WakeTrigger),
		Count:       count,
		Integration: projection,
	}
}

func countImmediateDiscoveries(state *models.State) int {
	count := 0
	for _, disc := range state.Discovered {
		if disc.Urgency == "immediate" && disc.ConvertedToTask == nil {
			count++
		}
	}
	return count
}

// hasIntegrationTask checks if any planned task uses the integration-pair role-pair.
func hasIntegrationTask(state *models.State) bool {
	for _, taskID := range state.Sprint.Scope.Planned {
		task := state.FindTask(taskID)
		if task != nil && task.RolePair == "integration-pair" {
			return true
		}
	}
	return false
}

// countReadyManyToOneCohorts delegates to ops.CountReadyManyToOneCohorts.
func countReadyManyToOneCohorts(state *models.State, m2oTransitions []ops.ManyToOneTransitionInfo) int {
	return ops.CountReadyManyToOneCohorts(state, m2oTransitions)
}

// countMergedPlanningTasksWithOutput counts planned tasks with unconsumed
// planning output, indicating tasks ready to be expanded into coding tasks.
// Uses the shared predicate ops.IsPlanningCompleteEligible.
func countMergedPlanningTasksWithOutput(state *models.State, planningPairs map[string]bool) int {
	count := 0
	for _, taskID := range state.Sprint.Scope.Planned {
		task := state.FindTask(taskID)
		if ops.IsPlanningCompleteEligible(task, planningPairs, state) {
			count++
		}
	}
	return count
}
