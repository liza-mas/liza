package agent

import (
	"fmt"
	"path/filepath"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/prompts"
)

// verifyOrchestratorStateChanges checks if orchestrator made expected state changes after completion
func verifyOrchestratorStateChanges(bb *db.Blackboard, stateBefore *models.State, pipelineTerminals []models.TaskStatus, planningPairs map[string]bool, m2oTransitions []ops.ManyToOneTransitionInfo) error {
	projectRoot := filepath.Dir(filepath.Dir(bb.GetStatePath()))
	result := DetectOrchestratorWakeTriggersForProject(projectRoot, stateBefore, pipelineTerminals, planningPairs, m2oTransitions)
	return verifyOrchestratorWakeChanges(bb, stateBefore, result)
}

// orchestratorTurnView is what every trigger check reads: the state the wake was
// computed on, the state the turn left, and the wake projection.
type orchestratorTurnView struct {
	bb          *db.Blackboard
	projectRoot string
	before      *models.State
	after       *models.State
	result      OrchestratorWakeResult
}

// verifyOrchestratorWakeChanges dispatches to the check of the wake's trigger.
// A check returns an error only where the trigger's expectation is a refusal;
// a missed expectation that is only advisory is logged.
func verifyOrchestratorWakeChanges(bb *db.Blackboard, stateBefore *models.State, result OrchestratorWakeResult) error {
	// Read state after agent execution
	stateAfter, err := bb.ReadCached()
	if err != nil {
		return fmt.Errorf("failed to read state after agent execution: %w", err)
	}
	turn := orchestratorTurnView{
		bb:          bb,
		projectRoot: filepath.Dir(filepath.Dir(bb.GetStatePath())),
		before:      stateBefore,
		after:       stateAfter,
		result:      result,
	}

	switch result.Trigger {
	case WakeTriggerInitialPlanning:
		return turn.verifyInitialPlanning()
	case WakeTriggerBlocked:
		turn.reportBlockedResolution()
	case WakeTriggerHypothesisExhausted:
		return turn.verifyHypothesisExhausted()
	case WakeTriggerHumanNote:
		turn.reportHumanNoteTurn()
	case WakeTriggerImmediateDiscovery:
		return turn.verifyImmediateDiscovery()
	case WakeTriggerPlanningComplete:
		return turn.verifyPlanningComplete()
	case WakeTriggerManyToOneReady:
		return turn.verifyCheckpointedSprint(WakeTriggerManyToOneReady, "Orchestrator checkpointed many-to-one cohort readiness")
	case WakeTriggerCodingComplete:
		return turn.verifyCodingComplete()
	case WakeTriggerIntegrationBlocked, WakeTriggerIntegrationExhausted,
		WakeTriggerIntegrationWaiting, WakeTriggerIntegrationUnavailable:
		return verifyEffectiveIntegrationOutcome(turn.after, result.Integration, nil)
	case WakeTriggerSprintComplete:
		return turn.verifyCheckpointedSprint(WakeTriggerSprintComplete, "Orchestrator completed sprint")
	}

	return nil
}

// verifyInitialPlanning expects tasks to be created.
func (turn orchestratorTurnView) verifyInitialPlanning() error {
	if len(turn.after.Tasks) == 0 {
		return fmt.Errorf("orchestrator completed with INITIAL_PLANNING trigger but no tasks were created")
	}
	GetLogger().Info("Orchestrator created tasks", "task_count", len(turn.after.Tasks))
	return nil
}

// reportBlockedResolution expects blocked tasks to be unblocked or superseded;
// an unresolved block may need a human, so it is reported, never refused.
func (turn orchestratorTurnView) reportBlockedResolution() {
	blockedBefore := countTasksWithStatus(turn.before, models.TaskStatusBlocked)
	blockedAfter := countTasksWithStatus(turn.after, models.TaskStatusBlocked)
	if blockedAfter < blockedBefore {
		GetLogger().Info("Orchestrator resolved blocked tasks", "before", blockedBefore, "after", blockedAfter)
		return
	}
	GetLogger().Info("Orchestrator could not resolve blocked tasks",
		"before", blockedBefore, "after", blockedAfter,
		"hint", "Blocks may require human intervention")
}

// verifyHypothesisExhausted expects exhausted tasks to be blocked, superseded,
// or otherwise made non-claimable.
func (turn orchestratorTurnView) verifyHypothesisExhausted() error {
	exhaustedBefore := countUnresolvedHypothesisExhausted(turn.before)
	exhaustedAfter := countUnresolvedHypothesisExhausted(turn.after)
	if exhaustedAfter >= exhaustedBefore {
		return fmt.Errorf("orchestrator completed with HYPOTHESIS_EXHAUSTED trigger but unresolved exhausted count didn't decrease (before: %d, after: %d)", exhaustedBefore, exhaustedAfter)
	}
	GetLogger().Info("Orchestrator handled exhausted hypotheses", "before", exhaustedBefore, "after", exhaustedAfter)
	return nil
}

// reportHumanNoteTurn logs for the audit trail only: the note carries the
// operator's request, so there is no single expected state change.
func (turn orchestratorTurnView) reportHumanNoteTurn() {
	GetLogger().Info("Orchestrator completed an operator-note turn",
		"unseen_before", ops.CountUnseenHumanNotes(turn.before))
}

// verifyImmediateDiscovery expects immediate discoveries to be converted to tasks.
func (turn orchestratorTurnView) verifyImmediateDiscovery() error {
	immediateBefore := countUnconvertedImmediateDiscoveries(turn.before)
	immediateAfter := countUnconvertedImmediateDiscoveries(turn.after)
	if immediateAfter >= immediateBefore {
		return fmt.Errorf("orchestrator completed with IMMEDIATE_DISCOVERY trigger but unconverted count didn't decrease (before: %d, after: %d)", immediateBefore, immediateAfter)
	}
	GetLogger().Info("Orchestrator handled immediate discoveries", "before", immediateBefore, "after", immediateAfter)
	return nil
}

// verifyPlanningComplete expects every wake-time plan to be dispositioned,
// replanned or held, and admissible plans to be checkpointed. Child tasks are
// created after resume, automatically only for passed plans.
func (turn orchestratorTurnView) verifyPlanningComplete() error {
	if err := verifyPlanningCompleteTurn(turn.projectRoot, turn.before, turn.after); err != nil {
		return err
	}
	if turn.after.Sprint.Status == models.SprintStatusCheckpoint &&
		turn.after.Sprint.CheckpointTrigger != models.CheckpointTriggerPlanningComplete {
		GetLogger().Warn("Orchestrator checkpointed but checkpoint_trigger is not PLANNING_COMPLETE — transitions may not execute after resume",
			"actual_trigger", turn.after.Sprint.CheckpointTrigger)
	}
	GetLogger().Info("Orchestrator completed plan hand-off review")
	return nil
}

// verifyCheckpointedSprint expects the sprint checkpointed (or completed) so
// transitions execute after the human resume.
func (turn orchestratorTurnView) verifyCheckpointedSprint(trigger OrchestratorWakeTrigger, completed string) error {
	sprint := turn.after.Sprint
	if sprint.Status != models.SprintStatusCheckpoint && sprint.Status != models.SprintStatusCompleted {
		return fmt.Errorf("orchestrator completed with %s trigger but sprint status is %s (expected CHECKPOINT or COMPLETED)", trigger, sprint.Status)
	}
	if sprint.Timeline.CheckpointAt == nil {
		return fmt.Errorf("orchestrator completed with %s trigger but checkpoint_at is not set", trigger)
	}
	if trigger == WakeTriggerSprintComplete {
		GetLogger().Info(completed, "status", sprint.Status)
		return nil
	}
	GetLogger().Info(completed)
	return nil
}

// verifyCodingComplete reconciles the integration analyses the turn requested.
func (turn orchestratorTurnView) verifyCodingComplete() error {
	if turn.result.Integration.Status != prompts.IntegrationViewReconciliationNeeded {
		return fmt.Errorf("orchestrator completed with CODING_COMPLETE trigger but integration status is %q", turn.result.Integration.Status)
	}
	return reconcileEffectiveIntegrationOutcome(turn.projectRoot, turn.bb, turn.result.Integration, ops.ReconcileIntegrationAnalyses)
}

func countTasksWithStatus(state *models.State, status models.TaskStatus) int {
	count := 0
	for _, task := range state.Tasks {
		if task.Status == status {
			count++
		}
	}
	return count
}

func countUnconvertedImmediateDiscoveries(state *models.State) int {
	count := 0
	for _, disc := range state.Discovered {
		if disc.Urgency == models.DiscoveryUrgencyImmediate && disc.ConvertedToTask == nil {
			count++
		}
	}
	return count
}

type integrationReconcileFunc func(projectRoot string) (*models.ReconcileIntegrationAnalysesResult, error)

func reconcileEffectiveIntegrationOutcome(projectRoot string, bb *db.Blackboard, projection prompts.EffectiveIntegrationCompletion, reconcile integrationReconcileFunc) error {
	reconciliation, err := reconcile(projectRoot)
	if err != nil {
		return fmt.Errorf("reconcile requested integration analyses: %w", err)
	}
	state, err := bb.ReadSnapshot()
	if err != nil {
		return fmt.Errorf("read reconciled integration state: %w", err)
	}
	return verifyEffectiveIntegrationOutcome(state, projection, reconciliation)
}

func verifyEffectiveIntegrationOutcome(state *models.State, projection prompts.EffectiveIntegrationCompletion, reconciliation *models.ReconcileIntegrationAnalysesResult) error {
	switch projection.Status {
	case prompts.IntegrationViewBlocked, prompts.IntegrationViewExhausted:
		return nil
	case prompts.IntegrationViewReconciliationNeeded:
		if reconciliation != nil && reconciliation.Reason != nil {
			lifecycle := state.Goal.Integration
			if lifecycle == nil || lifecycle.Closure == nil ||
				(lifecycle.Closure.Status != models.IntegrationClosureStatusBlocked && lifecycle.Closure.Status != models.IntegrationClosureStatusExhausted) ||
				lifecycle.Closure.Reason != reconciliation.Reason.Code {
				return fmt.Errorf("integration reconciliation returned %q without matching blocked or exhausted closure", reconciliation.Reason.Code)
			}
			return nil
		}
		if len(projection.RequestKeys) == 0 {
			return fmt.Errorf("integration reconciliation projected no requested analysis keys")
		}
		for _, key := range projection.RequestKeys {
			taskCount := 0
			plannedCount := 0
			for i := range state.Tasks {
				if state.Tasks[i].IntegrationAnalysis != nil && state.Tasks[i].IntegrationAnalysis.Key == key {
					taskCount++
					for _, plannedID := range state.Sprint.Scope.Planned {
						if plannedID == state.Tasks[i].ID {
							plannedCount++
						}
					}
				}
			}
			if taskCount != 1 || plannedCount != 1 {
				return fmt.Errorf("requested integration analysis %q has duplicate or missing membership: tasks=%d planned=%d", key, taskCount, plannedCount)
			}
		}
		return nil
	default:
		return fmt.Errorf("integration outcome %q (%s) is not an accepted post-run result", projection.Status, projection.ReasonCode)
	}
}

// hypothesisExhaustionMinFailures is the number of failed attempts at which a
// task counts as hypothesis-exhausted. It mirrors the operations' own threshold,
// which is unexported there.
const hypothesisExhaustionMinFailures = 2

func countUnresolvedHypothesisExhausted(state *models.State) int {
	count := 0
	for _, task := range state.Tasks {
		if len(task.FailedBy) >= hypothesisExhaustionMinFailures && task.Status != models.TaskStatusBlocked && !task.Status.IsTerminal() {
			count++
		}
	}
	return count
}
