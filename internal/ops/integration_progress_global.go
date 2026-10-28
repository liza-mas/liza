package ops

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
)

// Global analysis stage of the integration progress evaluator: generation
// provenance, deferred-validation gate and the next global request.

func (e *integrationProgressEvaluator) evaluateGlobal(
	decision models.IntegrationProgressDecision,
	lifecycle *models.IntegrationLifecycle,
	integrationHEAD string,
) (models.IntegrationProgressDecision, error) {
	if err := e.validateGlobalGenerations(lifecycle.GlobalGenerations, lifecycle.FirstGlobalGeneration()); err != nil {
		return models.IntegrationProgressDecision{}, err
	}
	if len(lifecycle.GlobalGenerations) > 0 {
		latest := lifecycle.GlobalGenerations[len(lifecycle.GlobalGenerations)-1]
		if latest.Verdict == models.IntegrationAnalysisVerdictFindings {
			resolution, err := e.resolveAnalysisRepairs(latest.AnalysisTaskID)
			if err != nil {
				return models.IntegrationProgressDecision{}, err
			}
			if resolution.blocked {
				decision.Blocked = progressReasonWithTasks(integrationProgressBlockedRepair, resolution.taskIDs...)
				return decision, nil
			}
			if !resolution.settled {
				decision.Waiting = progressReasonWithTasks(integrationProgressWaitingRepairs, resolution.taskIDs...)
				return decision, nil
			}
		}
	}

	// Deferred commands bind only completion: they must pass at the final
	// HEAD before closure counts, while a generation may be requested and the
	// batch runs alongside it (ADR-0204, D-230).
	deferredWaiting, deferredBlocked := deferredValidationGate(e.state, integrationHEAD)
	if deferredWaiting != nil {
		decision.DeferredValidationRequest = integrationHEAD
	}

	decision.GlobalReady = true
	if len(lifecycle.GlobalGenerations) > 0 {
		latest := lifecycle.GlobalGenerations[len(lifecycle.GlobalGenerations)-1]
		if latest.Verdict == models.IntegrationAnalysisVerdictClean && latest.SourceCommit == integrationHEAD {
			if deferredBlocked != nil {
				decision.Blocked = deferredBlocked
				return decision, nil
			}
			if deferredWaiting != nil {
				decision.Waiting = deferredWaiting
				return decision, nil
			}
			closure := lifecycle.Closure
			decision.IntegrationComplete = closure != nil &&
				closure.Status == models.IntegrationClosureStatusClean &&
				closure.Generation == latest.Generation &&
				closure.AnalysisKey == latest.AnalysisKey &&
				closure.SourceCommit == integrationHEAD
			if !decision.IntegrationComplete {
				decision.Waiting = progressReason(integrationProgressWaitingClosure)
			}
			return decision, nil
		}
	}

	limit := models.NormalizeGlobalIntegrationGenerationLimit(e.state.Config.MaxGlobalIntegrationGenerations)
	nextGeneration := len(lifecycle.GlobalGenerations) + lifecycle.FirstGlobalGeneration()
	if len(lifecycle.GlobalGenerations)+1 > limit {
		decision.Exhausted = true
		decision.Blocked = &models.IntegrationProgressReason{
			Code:     string(integrationProgressBlockedExhausted),
			Guidance: fmt.Sprintf("global integration generation limit %d reached", limit),
		}
		return decision, nil
	}
	if integrationHEAD == "" {
		return models.IntegrationProgressDecision{}, fmt.Errorf("integration progress: live integration HEAD is empty for global generation %d", nextGeneration)
	}
	key := globalAnalysisKey(nextGeneration)
	if analysis := e.analysisKeys[key]; analysis != nil {
		if analysis.Status == models.TaskStatusBlocked || analysis.Status == models.TaskStatusAbandoned {
			decision.Blocked = progressReasonWithTasks(integrationProgressBlockedAnalysis, analysis.ID)
		} else {
			decision.Waiting = progressReasonWithTasks(integrationProgressWaitingGlobalAnalysis, analysis.ID)
		}
		return decision, nil
	}
	decision.GlobalRequest = &models.IntegrationAnalysisRequest{
		Key: key, Phase: models.IntegrationAnalysisPhaseGlobal,
		Generation: nextGeneration, SourceCommit: integrationHEAD,
	}
	return decision, nil
}

func (e *integrationProgressEvaluator) validateGlobalGenerations(generations []models.IntegrationGlobalGeneration, firstGeneration int) error {
	for i, generation := range generations {
		wantGeneration := i + firstGeneration
		wantKey := globalAnalysisKey(wantGeneration)
		if generation.Generation != wantGeneration || generation.AnalysisKey != wantKey || generation.SourceCommit == "" {
			return fmt.Errorf("integration progress: malformed global generation at index %d", i)
		}
		analysis := e.tasks[generation.AnalysisTaskID]
		if analysis == nil || analysis.IntegrationAnalysis == nil {
			return fmt.Errorf("integration progress: global generation %d references missing analysis task %q", wantGeneration, generation.AnalysisTaskID)
		}
		metadata := analysis.IntegrationAnalysis
		if metadata.Key != wantKey || metadata.Phase != models.IntegrationAnalysisPhaseGlobal || metadata.Generation != wantGeneration || metadata.SourceCommit != generation.SourceCommit {
			return fmt.Errorf("integration progress: global generation %d contradicts analysis metadata", wantGeneration)
		}
		if !generation.Verdict.IsValid() {
			return fmt.Errorf("integration progress: global generation %d has invalid verdict %q", wantGeneration, generation.Verdict)
		}
	}
	return nil
}
