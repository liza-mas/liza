package agent

import (
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// OrchestratorWakeTrigger represents what triggered the orchestrator to wake
type OrchestratorWakeTrigger string

const (
	WakeTriggerInitialPlanning     OrchestratorWakeTrigger = "INITIAL_PLANNING"
	WakeTriggerBlocked             OrchestratorWakeTrigger = "BLOCKED_TASKS"
	WakeTriggerHypothesisExhausted OrchestratorWakeTrigger = "HYPOTHESIS_EXHAUSTED"
	WakeTriggerImmediateDiscovery  OrchestratorWakeTrigger = "IMMEDIATE_DISCOVERY"
	WakeTriggerPlanningComplete    OrchestratorWakeTrigger = "PLANNING_COMPLETE"
	WakeTriggerManyToOneReady      OrchestratorWakeTrigger = "MANY_TO_ONE_READY"
	WakeTriggerCodingComplete      OrchestratorWakeTrigger = "CODING_COMPLETE"
	WakeTriggerSprintComplete      OrchestratorWakeTrigger = "SPRINT_COMPLETE"
	WakeTriggerNone                OrchestratorWakeTrigger = "NONE"
)

// OrchestratorWakeResult contains the wake trigger and count
type OrchestratorWakeResult struct {
	Trigger OrchestratorWakeTrigger
	Count   int
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
		Count: func(state *models.State) int {
			return countActionableBlockedTasks(state)
		},
	},
	{
		Trigger:     WakeTriggerHypothesisExhausted,
		Description: "Tasks with repeated coder failures need orchestrator intervention.",
		Count:       countHypothesisExhaustedTasks,
	},
	{
		Trigger:     WakeTriggerImmediateDiscovery,
		Description: "Immediate discoveries need orchestrator triage.",
		Count:       countImmediateDiscoveries,
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
// 1. No tasks (initial planning)
// 2. Blocked tasks
// 3. Hypothesis exhausted (2+ failed_by)
// 4. Immediate discoveries (not yet converted to tasks)
// 5. Planning complete (merged planning tasks have output[])
// 6. Many-to-one transition ready
// 7. Sprint complete (all planned tasks terminal)
func DetectOrchestratorWakeTriggers(state *models.State, pipelineTerminals []models.TaskStatus, planningPairs map[string]bool, m2oTransitions []ops.ManyToOneTransitionInfo) OrchestratorWakeResult {
	for _, triggerSpec := range orchestratorWakeTriggerSpecs {
		if count := triggerSpec.Count(state); count > 0 {
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
		if n := countMergedPlanningTasksWithOutput(state, planningPairs); n > 0 {
			return OrchestratorWakeResult{
				Trigger: WakeTriggerPlanningComplete,
				Count:   n,
			}
		}
		if n := countReadyManyToOneCohorts(state, m2oTransitions); n > 0 {
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
	if state.AllPlannedTasksTerminalWith(pipelineTerminals) {
		if state.Sprint.Status == models.SprintStatusCheckpoint ||
			state.Sprint.Status == models.SprintStatusCompleted {
			return OrchestratorWakeResult{Trigger: WakeTriggerNone}
		}
		// Detect coding completion: all tasks terminal, coding happened (base_commit set),
		// but no integration task exists yet.
		if state.Goal.BaseCommit != nil && !hasIntegrationTask(state) {
			return OrchestratorWakeResult{
				Trigger: WakeTriggerCodingComplete,
				Count:   1,
			}
		}
		return OrchestratorWakeResult{
			Trigger: WakeTriggerSprintComplete,
			Count:   len(state.Sprint.Scope.Planned),
		}
	}

	return OrchestratorWakeResult{Trigger: WakeTriggerNone}
}

// countActionableBlockedTasks counts BLOCKED tasks that the orchestrator should
// wake up for. A blocked task is actionable if it has never been assessed, or if
// new activity has occurred since the last assessment (on the task itself, its
// dependencies, or via human notes).
func countActionableBlockedTasks(state *models.State) int {
	count := 0
	for i := range state.Tasks {
		if state.Tasks[i].Status == models.TaskStatusBlocked && isTaskActionableSinceAssessment(&state.Tasks[i], state) {
			count++
		}
	}
	return count
}

// isTaskActionableSinceAssessment determines whether a task should trigger an
// orchestrator wake. Returns true if:
//   - No orchestrator_assessment history entry exists (never triaged)
//   - The task itself has a non-assessment history entry after the last assessment
//   - Any dependency has a non-assessment history entry after the last assessment
//   - A human note targets this task (by ID or "all") after the last assessment
//
// Used by both BLOCKED and HYPOTHESIS_EXHAUSTED wake triggers.
func isTaskActionableSinceAssessment(task *models.Task, state *models.State) bool {
	// Find the last orchestrator_assessment (reverse scan).
	var lastAssessment *models.TaskHistoryEntry
	for i := len(task.History) - 1; i >= 0; i-- {
		if task.History[i].Event == models.TaskEventOrchestratorAssessment {
			lastAssessment = &task.History[i]
			break
		}
	}

	// Never assessed → actionable.
	if lastAssessment == nil {
		return true
	}

	// Check own history for non-assessment activity after last assessment.
	for i := range task.History {
		if task.History[i].Event != models.TaskEventOrchestratorAssessment &&
			task.History[i].Time.After(lastAssessment.Time) {
			return true
		}
	}

	// Check dependency history for non-assessment activity after last assessment.
	for _, depID := range task.DependsOn {
		dep := state.FindTask(depID)
		if dep == nil {
			continue // orphan reference, skip gracefully
		}
		for i := range dep.History {
			if dep.History[i].Event != models.TaskEventOrchestratorAssessment &&
				dep.History[i].Time.After(lastAssessment.Time) {
				return true
			}
		}
	}

	// Check human notes targeting this task (by ID or "all") after last assessment.
	// Design choice: notes on dependency tasks do NOT re-activate this blocked task.
	// Rationale: human targets specific tasks by ID; if they want to wake all
	// dependents, they add notes to each or use for:"all". This avoids surprising
	// cascade wakes when a human annotates a dependency for unrelated reasons.
	for i := range state.HumanNotes {
		note := &state.HumanNotes[i]
		if (note.For == task.ID || note.For == "all") && note.Timestamp.After(lastAssessment.Time) {
			return true
		}
	}

	return false
}

func countHypothesisExhaustedTasks(state *models.State) int {
	count := 0
	for i := range state.Tasks {
		if len(state.Tasks[i].FailedBy) >= 2 &&
			state.Tasks[i].Status != models.TaskStatusBlocked &&
			!state.Tasks[i].Status.IsTerminal() &&
			isTaskActionableSinceAssessment(&state.Tasks[i], state) {
			count++
		}
	}
	return count
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
