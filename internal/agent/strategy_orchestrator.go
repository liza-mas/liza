package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/functionalclusters"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/stacklit"
)

// orchestratorStrategy handles the orchestrator role.
type orchestratorStrategy struct {
	wake             *OrchestratorWakeResult // selection for this invocation only
	resolver         *pipeline.Resolver      // pipeline resolver for context sections
	executionTimeout time.Duration           // from YAML; 0 = use type default
	yamlPollSec      int                     // from YAML; 0 = use type default
	yamlMaxWaitSec   int                     // from YAML; 0 = use type default
}

var (
	orchestratorScipRefresh               = scipsearch.RefreshIndexes
	orchestratorStacklitRefresh           = stacklit.RefreshIndex
	orchestratorFunctionalClustersRefresh = functionalclusters.RefreshIndex
	orchestratorWaitForWorkDetector       = DetectOrchestratorWakeTriggersForProject
)

const defaultOrchestratorTimeout = 4 * time.Hour

// orchestratorProgressSignature returns a string capturing the state dimensions
// the orchestrator is expected to change. Includes sprint metadata, task-status
// distribution, and discovery count so that legitimate progress like resolving
// blocked tasks, superseding exhausted tasks, or triaging discoveries is
// recognized as a signature change and resets the spinning counter.
func orchestratorProgressSignature(state *models.State) string {
	// Task-status distribution: count tasks per status so any status
	// transition (block→ready, ready→superseded, etc.) changes the signature.
	statusCounts := make(map[models.TaskStatus]int)
	for i := range state.Tasks {
		statusCounts[state.Tasks[i].Status]++
	}
	// Sort keys for deterministic output.
	keys := make([]string, 0, len(statusCounts))
	for k := range statusCounts {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	var dist strings.Builder
	for _, k := range keys {
		if dist.Len() > 0 {
			dist.WriteByte(',')
		}
		fmt.Fprintf(&dist, "%s=%d", k, statusCounts[models.TaskStatus(k)])
	}

	// Unconverted immediate discovery count — changes when orchestrator triages.
	immediateDisc := 0
	for _, d := range state.Discovered {
		if d.Urgency == "immediate" && d.ConvertedToTask == nil {
			immediateDisc++
		}
	}

	return fmt.Sprintf("sprint:%s:%d:planned:%d:dist:%s:disc:%d",
		state.Sprint.Status, state.Sprint.Number,
		len(state.Sprint.Scope.Planned), dist.String(), immediateDisc)
}

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
	if err := ops.ModifyWithAgentAuthority(bb, config.Authority, func(s *models.State) error {
		s.Sprint.CheckpointTrigger = ""
		return nil
	}); err != nil {
		return false, fmt.Errorf("clear checkpoint trigger: %w", err)
	}

	return false, nil
}

func (s *orchestratorStrategy) WaitForWork(ctx context.Context, bb *db.Blackboard, config SupervisorConfig, pollInterval, maxWait time.Duration) (bool, error) {
	s.wake = nil
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
			// This closure is the orchestrator's only look at fresh state
			// while it waits, and a checkpoint suppresses the wake triggers
			// below — so without this the wait runs to timeout and the
			// supervisor exits having never observed the checkpoint.
			maybeEmitCheckpointSummary(bb, config.ProjectRoot, "orchestrator", state)

			result := orchestratorWaitForWorkDetector(config.ProjectRoot, state, pipelineTerminals, planningPairs, m2oTransitions)
			if result.ShouldWake() {
				s.wake = &result
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
	if err := setAgentToOrchestratingStatus(bb, config.Authority); err != nil {
		return err
	}
	refreshOrchestratorProjectRootScipIndexes(bb, config)
	refreshOrchestratorProjectRootStacklitIndex(config)
	refreshOrchestratorProjectRootFunctionalClustersIndex(bb, config)
	return nil
}

func (s *orchestratorStrategy) BuildPrompt(state *models.State, config SupervisorConfig, _ string) (string, error) {
	return buildOrchestratorPromptForWake(state, config, s.resolver, s.wake)
}

// RevalidateWake runs after indexing and before any turn/spin accounting. Gate
// effects remain owned by the supervisor loop; cancellation just returns there.
func (s *orchestratorStrategy) RevalidateWake(ctx context.Context, bb *db.Blackboard, state *models.State, config SupervisorConfig) (launch bool, err error) {
	defer func() {
		if !launch {
			err = errors.Join(err, resetAgentAfterExit(bb, config.Authority, config.ProjectRoot))
		}
	}()
	selected := OrchestratorWakeResult{Trigger: WakeTriggerNone}
	if s.wake != nil {
		selected = *s.wake
	}
	s.wake = nil
	det, err := ops.LoadDetectionContext(config.ProjectRoot)
	if err != nil {
		GetLogger().Warn("Failed to load detection context", "error", err)
		det = &ops.PipelineDetectionContext{}
	}
	var projection *prompts.EffectiveIntegrationCompletion
	result, fresh := revalidateOrchestratorWake(state, selected, det.SprintTerminals, det.PlanningPairs, det.ManyToOneTransitions, func() prompts.EffectiveIntegrationCompletion {
		if projection == nil {
			decision, evaluationErr := ops.EvaluateLiveIntegrationProgress(state, config.ProjectRoot)
			value := prompts.ProjectEffectiveIntegrationCompletion(decision, nil, evaluationErr)
			projection = &value
		}
		return *projection
	})
	stopped, _ := isSystemStopped(state)
	if ctx.Err() != nil || stopped || rolePauseReason(state, "orchestrator") != "" ||
		CheckProviderUnavailableSignal(config.ProjectRoot, config.CLIName) || CheckQuotaSignal(config.ProjectRoot, config.CLIName) || !result.ShouldWake() {
		GetLogger().Info("Skipping orchestrator launch after revalidation", "selected_trigger", selected.Trigger, "fresh_trigger", fresh.Trigger)
		return false, nil
	}
	s.wake = &result
	return true, nil
}

func (s *orchestratorStrategy) PostExecution(bb *db.Blackboard, config SupervisorConfig, _, _ string, stateBefore *models.State) error {
	defer func() { s.wake = nil }()
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
	var result OrchestratorWakeResult
	if s.wake != nil {
		result = *s.wake
	} else {
		// Snapshot-only callers have no live invocation to retain.
		result = DetectOrchestratorWakeTriggersForProject(config.ProjectRoot, stateBefore, pipelineTerminals, planningPairs, m2oTransitions)
	}

	// A HUMAN_NOTE turn rendered every pre-turn note. Any other turn consumed only the notes whose
	// target it assessed: the blocked-task instructions read human_notes before
	// recording an assessment, so re-rendering those as fresh requests would
	// execute them twice. Only notes that existed when the prompt was built
	// qualify. A turn that exits non-zero never reaches here and the notes
	// wake the orchestrator again.
	if ops.CountUnseenHumanNotes(stateBefore) > 0 {
		trigger := result.Trigger
		rendered := len(stateBefore.HumanNotes)
		if err := ops.ModifyWithAgentAuthority(bb, config.Authority, func(state *models.State) error {
			consumed := func(*models.HumanNote) bool { return true }
			if trigger != WakeTriggerHumanNote {
				assessed := ops.TasksAssessedBetween(stateBefore, state)
				consumed = func(note *models.HumanNote) bool {
					return assessed[note.For] || (note.For == "all" && len(assessed) > 0)
				}
			}
			if stamped := ops.MarkHumanNotesSeen(state, time.Now().UTC(), rendered, consumed); stamped > 0 {
				GetLogger().Info("Marked operator notes as seen by orchestrator", "count", stamped, "trigger", trigger)
			}
			return nil
		}); err != nil {
			GetLogger().Warn("Failed to mark operator notes as seen", "error", err)
		}
	}

	if err := verifyOrchestratorWakeChanges(bb, stateBefore, result); err != nil {
		GetLogger().Warn("Orchestrator state verification failed",
			"error", err,
			"hint", "Agent may not have executed required commands - attempting self-heal")

		// Self-healing: for mechanical checkpoint operations, perform the
		// expected state change directly instead of relying on the LLM.
		// This breaks the re-wake loop where the orchestrator keeps
		// executing without calling sprint_checkpoint.
		if healed := selfHealCheckpoint(config.ProjectRoot, result.Trigger); healed {
			GetLogger().Info("Self-healed: checkpoint created after agent failed to do so",
				"trigger", result.Trigger)
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
