package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/functionalclusters"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/stacklit"
)

// orchestratorStrategy handles the orchestrator role.
type orchestratorStrategy struct {
	resolver         *pipeline.Resolver // pipeline resolver for context sections
	executionTimeout time.Duration      // from YAML; 0 = use type default
	yamlPollSec      int                // from YAML; 0 = use type default
	yamlMaxWaitSec   int                // from YAML; 0 = use type default
}

var (
	orchestratorScipRefresh               = scipsearch.RefreshIndexes
	orchestratorStacklitRefresh           = stacklit.RefreshIndex
	orchestratorFunctionalClustersRefresh = functionalclusters.RefreshIndex
	orchestratorWaitForWorkDetector       = DetectOrchestratorWakeTriggersForProject
)

const defaultOrchestratorTimeout = 4 * time.Hour

func (s *orchestratorStrategy) DefaultTimeout() time.Duration {
	if s.executionTimeout > 0 {
		return s.executionTimeout
	}
	return defaultOrchestratorTimeout
}

func (s *orchestratorStrategy) WaitConfig(state *models.State) (pollInterval, maxWait time.Duration) {
	poll := nonZeroOr(state.Config.OrchestratorPollInterval, nonZeroOr(s.yamlPollSec, models.DefaultOrchestratorPollInterval))
	max := nonZeroOr(state.Config.OrchestratorMaxWait, nonZeroOr(s.yamlMaxWaitSec, models.DefaultOrchestratorMaxWait))
	return time.Duration(poll) * time.Second, time.Duration(max) * time.Second
}

func (s *orchestratorStrategy) PreWork(_ context.Context, bb *db.Blackboard, config SupervisorConfig) (bool, error) {
	logger := GetLogger()

	state, err := bb.Read()
	if err != nil {
		logger.Warn("Failed to read state for transition check", "error", err)
		return false, nil
	}

	// Gate: checkpoint was for a pipeline transition AND sprint has been resumed.
	// The checkpoint trigger rules out manual/sprint-complete checkpoints.
	// status == IN_PROGRESS means the human reviewed and resumed.
	if state.Sprint.Status != models.SprintStatusInProgress ||
		!models.IsTransitionCheckpointTrigger(state.Sprint.CheckpointTrigger) {
		return false, nil
	}

	detCtx, detErr := ops.LoadDetectionContext(config.ProjectRoot)
	if detErr != nil {
		logger.Warn("Failed to load detection context", "error", detErr)
		return false, nil
	}

	planningReady := countMergedPlanningTasksWithOutput(state, detCtx.PlanningPairs) > 0
	m2oReady := countReadyManyToOneCohorts(state, detCtx.ManyToOneTransitions) > 0
	if planningReady || m2oReady {
		if err := handleAvailableTransitions(config.ProjectRoot); err != nil {
			logger.Warn("Transition handler error", "error", err)
		}
	}

	// Clear trigger even if transitions failed — the human approved, so don't
	// re-checkpoint. Transition errors are logged; retry is manual.
	if err := bb.Modify(func(s *models.State) error {
		s.Sprint.CheckpointTrigger = ""
		return nil
	}); err != nil {
		logger.Warn("Failed to clear checkpoint trigger", "error", err)
	}

	return false, nil
}

func (s *orchestratorStrategy) WaitForWork(ctx context.Context, bb *db.Blackboard, config SupervisorConfig, pollInterval, maxWait time.Duration) (bool, error) {
	detCtx, detErr := ops.LoadDetectionContext(config.ProjectRoot)
	var pipelineTerminals []models.TaskStatus
	var planningPairs map[string]bool
	var m2oTransitions []ops.ManyToOneTransitionInfo
	if detErr == nil {
		pipelineTerminals = detCtx.SprintTerminals
		planningPairs = detCtx.PlanningPairs
		m2oTransitions = detCtx.ManyToOneTransitions
	}

	return waitForWorkEventDriven(ctx, bb, config.ProjectRoot, pollInterval, maxWait,
		func(state *models.State) (bool, string) {
			result := orchestratorWaitForWorkDetector(config.ProjectRoot, state, pipelineTerminals, planningPairs, m2oTransitions)
			if result.ShouldWake() {
				return true, fmt.Sprintf("Orchestrator wake trigger: %s (count: %d)", result.Trigger, result.Count)
			}
			if result.Trigger != WakeTriggerNone {
				return false, fmt.Sprintf("Orchestrator stable integration outcome: %s (%s)", result.Trigger, result.Integration.ReasonCode)
			}
			return false, ""
		})
}

func (s *orchestratorStrategy) ClaimTask(_ SupervisorConfig, _ *db.Blackboard) (string, string, error) {
	return "", "", nil
}

func (s *orchestratorStrategy) PreExecution(bb *db.Blackboard, config SupervisorConfig) error {
	if err := setAgentToOrchestratingStatus(bb, config.AgentID); err != nil {
		return err
	}
	refreshOrchestratorProjectRootScipIndexes(bb, config)
	refreshOrchestratorProjectRootStacklitIndex(config)
	refreshOrchestratorProjectRootFunctionalClustersIndex(bb, config)
	return nil
}

func (s *orchestratorStrategy) BuildPrompt(state *models.State, config SupervisorConfig, _ string) (string, error) {
	return buildOrchestratorPromptContext(state, config, s.resolver)
}

func (s *orchestratorStrategy) PostExecution(bb *db.Blackboard, config SupervisorConfig, _, _ string, stateBefore *models.State) error {
	detCtx, detErr := ops.LoadDetectionContext(config.ProjectRoot)
	var pipelineTerminals []models.TaskStatus
	var planningPairs map[string]bool
	var m2oTransitions []ops.ManyToOneTransitionInfo
	if detErr != nil {
		GetLogger().Warn("Failed to load detection context", "error", detErr)
	} else {
		pipelineTerminals = detCtx.SprintTerminals
		planningPairs = detCtx.PlanningPairs
		m2oTransitions = detCtx.ManyToOneTransitions
	}

	if err := verifyOrchestratorStateChanges(bb, stateBefore, pipelineTerminals, planningPairs, m2oTransitions); err != nil {
		GetLogger().Warn("Orchestrator state verification failed",
			"error", err,
			"hint", "Agent may not have executed required commands - attempting self-heal")

		// Self-healing: for mechanical checkpoint operations, perform the
		// expected state change directly instead of relying on the LLM.
		// This breaks the re-wake loop where the orchestrator keeps
		// executing without calling sprint_checkpoint.
		trigger := DetectOrchestratorWakeTriggersForProject(config.ProjectRoot, stateBefore, pipelineTerminals, planningPairs, m2oTransitions)
		if healed := selfHealCheckpoint(config.ProjectRoot, trigger.Trigger); healed {
			GetLogger().Info("Self-healed: checkpoint created after agent failed to do so",
				"trigger", trigger.Trigger)
		}
	}
	return nil
}

// selfHealCheckpoint calls sprint_checkpoint directly when the orchestrator
// agent failed to do so. Returns true if a checkpoint was successfully created.
// Only acts on checkpoint triggers (SPRINT_COMPLETE, PLANNING_COMPLETE,
// MANY_TO_ONE_READY) — these are mechanical operations that don't require
// LLM creativity.
func selfHealCheckpoint(projectRoot string, trigger OrchestratorWakeTrigger) bool {
	switch trigger {
	case WakeTriggerSprintComplete, WakeTriggerPlanningComplete, WakeTriggerManyToOneReady:
	default:
		return false
	}

	triggerStr := ""
	switch trigger {
	case WakeTriggerPlanningComplete:
		triggerStr = models.CheckpointTriggerPlanningComplete
	case WakeTriggerManyToOneReady:
		triggerStr = models.CheckpointTriggerManyToOneReady
	}
	_, err := ops.SprintCheckpoint(projectRoot, triggerStr)
	if err != nil {
		if errors.Is(err, ops.ErrSprintAlreadyCheckpoint) {
			return true // already done, count as healed
		}
		GetLogger().Warn("Self-heal checkpoint failed", "error", err)
		return false
	}
	return true
}

func refreshOrchestratorProjectRootScipIndexes(bb *db.Blackboard, config SupervisorConfig) {
	logger := GetLogger()
	state, err := bb.Read()
	if err != nil {
		logger.Warn("Failed to read state for orchestrator SCIP refresh", "error", err)
		return
	}

	configuredLanguages := state.Config.ScipSearch
	if !scipsearch.RuntimeEnabled(configuredLanguages) {
		return
	}

	result, err := orchestratorScipRefresh(scipsearch.RefreshOptions{
		TargetRoot:          config.ProjectRoot,
		TargetKind:          scipsearch.TargetKindProjectRoot,
		ConfiguredLanguages: configuredLanguages,
	})
	if err != nil {
		logger.Warn("Orchestrator SCIP refresh failed", "error", err)
		return
	}
	for _, failure := range result.Failures {
		logger.Warn("Orchestrator SCIP indexer failed",
			"language", failure.Language,
			"diagnostic", failure.Diagnostic)
	}
}

func refreshOrchestratorProjectRootStacklitIndex(config SupervisorConfig) {
	if !stacklit.RuntimeEnabled() {
		return
	}

	logger := GetLogger()
	result, err := orchestratorStacklitRefresh(stacklit.RefreshOptions{
		TargetRoot: config.ProjectRoot,
		TargetKind: stacklit.TargetKindProjectRoot,
	})
	if err != nil {
		logger.Warn("Orchestrator Stacklit refresh failed", "error", err)
		return
	}
	for _, failure := range result.Failures {
		logger.Warn("Orchestrator Stacklit indexer failed",
			"diagnostic", failure.Diagnostic)
	}
}

func refreshOrchestratorProjectRootFunctionalClustersIndex(bb *db.Blackboard, config SupervisorConfig) {
	logger := GetLogger()
	state, err := bb.Read()
	if err != nil {
		logger.Warn("Failed to read state for orchestrator Functional Clusters refresh", "error", err)
		return
	}

	configuredLanguages := state.Config.ScipSearch
	if !functionalclusters.RefreshEnabled(configuredLanguages) {
		return
	}

	result, err := orchestratorFunctionalClustersRefresh(functionalclusters.RefreshOptions{
		TargetRoot:          config.ProjectRoot,
		TargetKind:          functionalclusters.TargetKindProjectRoot,
		ConfiguredLanguages: configuredLanguages,
	})
	if err != nil {
		logger.Warn("Orchestrator Functional Clusters refresh failed", "error", err)
		return
	}
	for _, failure := range result.Failures {
		logger.Warn("Orchestrator Functional Clusters build failed",
			"diagnostic", failure.Diagnostic)
	}
}
