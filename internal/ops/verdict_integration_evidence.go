package ops

import (
	"fmt"
	"os"
	"slices"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// This file holds the integration-analysis evidence a clean or findings verdict
// appends and the candidate validation that guards it. The candidate validation
// runs inside the verdict transaction (K1-E14).

func validateIntegrationAnalysisRolePair(task *models.Task) error {
	if task == nil || task.IntegrationAnalysis == nil {
		return nil
	}
	wantRolePair, err := analysisRolePair(task.IntegrationAnalysis.Phase)
	if err != nil {
		return fmt.Errorf("task %s has invalid integration analysis phase %q", task.ID, task.IntegrationAnalysis.Phase)
	}
	if task.RolePair != wantRolePair {
		return fmt.Errorf("task %s integration analysis phase %q requires role_pair %q, got %q", task.ID, task.IntegrationAnalysis.Phase, wantRolePair, task.RolePair)
	}
	return nil
}

func appendIntegrationVerdictEvidence(state *models.State, task *models.Task, verification *cleanIntegrationSourceVerification) error {
	metadata := task.IntegrationAnalysis
	if metadata == nil || task.ReviewCommit == nil {
		return fmt.Errorf("task %s integration analysis verdict requires metadata and review commit", task.ID)
	}
	if state.Goal.Integration == nil {
		state.Goal.Integration = &models.IntegrationLifecycle{}
	}
	verdict := models.IntegrationAnalysisVerdictFindings
	if len(task.Output) == 0 {
		verdict = models.IntegrationAnalysisVerdictClean
	}

	switch metadata.Phase {
	case models.IntegrationAnalysisPhaseSlice:
		state.Goal.Integration.Coverage = append(state.Goal.Integration.Coverage, models.IntegrationCoverageRecord{
			PlanTaskID: metadata.OriginatingPlanTaskID,
			Kind:       models.IntegrationCoverageSliceReport,
			SliceReport: &models.IntegrationSliceReport{
				AnalysisTaskID: task.ID,
				AnalysisKey:    metadata.Key,
				Verdict:        verdict,
				SourceCommit:   metadata.SourceCommit,
				ReportCommit:   *task.ReviewCommit,
			},
		})
	case models.IntegrationAnalysisPhaseGlobal:
		if verdict == models.IntegrationAnalysisVerdictClean && (verification == nil || verification.SourceCommit != metadata.SourceCommit) {
			return fmt.Errorf("task %s clean integration source was not verified", task.ID)
		}
		state.Goal.Integration.GlobalGenerations = append(state.Goal.Integration.GlobalGenerations, models.IntegrationGlobalGeneration{
			Generation:     metadata.Generation,
			AnalysisTaskID: task.ID,
			AnalysisKey:    metadata.Key,
			Verdict:        verdict,
			SourceCommit:   metadata.SourceCommit,
			ReportCommit:   *task.ReviewCommit,
		})
		if verdict == models.IntegrationAnalysisVerdictClean && verification.Effective {
			state.Goal.Integration.Closure = &models.IntegrationClosure{
				Status:       models.IntegrationClosureStatusClean,
				Generation:   metadata.Generation,
				AnalysisKey:  metadata.Key,
				SourceCommit: metadata.SourceCommit,
			}
		}
	default:
		return fmt.Errorf("task %s has invalid integration analysis phase %q", task.ID, metadata.Phase)
	}
	return nil
}

// validateIntegrationLifecycleCandidate refuses violations the verdict adds,
// judged against baseline, the locked pre-image. previous is only the
// lifecycle snapshot for the append-only transition check: its Agents map
// aliases the candidate's, so it cannot serve as the delta baseline.
func validateIntegrationLifecycleCandidate(projectRoot string, baseline func() (*models.State, error), previous, candidate *models.State) error {
	if err := statevalidate.ValidateCandidate(candidate, baseline, projectRoot, false, os.Stderr); err != nil {
		return fmt.Errorf("invalid integration lifecycle candidate: %w", err)
	}
	normalizeEmptyIntegrationPrefixes(previous, candidate)
	if err := statevalidate.ValidateIntegrationLifecycleTransition(previous, candidate); err != nil {
		return fmt.Errorf("invalid integration lifecycle transition: %w", err)
	}
	return nil
}

func normalizeEmptyIntegrationPrefixes(previous, candidate *models.State) {
	if previous.Goal.Integration == nil || candidate.Goal.Integration == nil {
		return
	}
	// reflect.DeepEqual distinguishes nil and empty slices. Normalize only an
	// append's zero-length before image so the transition validator compares
	// evidence values rather than their in-memory slice representation.
	if previous.Goal.Integration.Coverage == nil && len(candidate.Goal.Integration.Coverage) > 0 {
		previous.Goal.Integration.Coverage = []models.IntegrationCoverageRecord{}
	}
	if previous.Goal.Integration.GlobalGenerations == nil && len(candidate.Goal.Integration.GlobalGenerations) > 0 {
		previous.Goal.Integration.GlobalGenerations = []models.IntegrationGlobalGeneration{}
	}
	if previous.Goal.Integration.MutationReceipts == nil && len(candidate.Goal.Integration.MutationReceipts) > 0 {
		previous.Goal.Integration.MutationReceipts = []models.IntegrationMutationReceipt{}
	}
}

func snapshotIntegrationLifecycleState(state *models.State) *models.State {
	previous := *state
	previous.Goal = state.Goal
	previous.Tasks = slices.Clone(state.Tasks)
	for i := range previous.Tasks {
		metadata := state.Tasks[i].IntegrationAnalysis
		if metadata == nil {
			continue
		}
		metadataCopy := *metadata
		metadataCopy.RootTaskIDs = slices.Clone(metadata.RootTaskIDs)
		metadataCopy.DescendantChanges = slices.Clone(metadata.DescendantChanges)
		metadataCopy.AffectedPaths = slices.Clone(metadata.AffectedPaths)
		metadataCopy.SourceSnapshotPaths = slices.Clone(metadata.SourceSnapshotPaths)
		previous.Tasks[i].IntegrationAnalysis = &metadataCopy
	}

	lifecycle := state.Goal.Integration
	if lifecycle == nil {
		return &previous
	}
	lifecycleCopy := *lifecycle
	if lifecycle.PrematureRecovery != nil {
		recoveryCopy := *lifecycle.PrematureRecovery
		recoveryCopy.PreviousContributingSet.Scopes = slices.Clone(lifecycle.PrematureRecovery.PreviousContributingSet.Scopes)
		lifecycleCopy.PrematureRecovery = &recoveryCopy
	}
	if lifecycle.ContributingSet != nil {
		setCopy := *lifecycle.ContributingSet
		setCopy.Scopes = slices.Clone(lifecycle.ContributingSet.Scopes)
		for i := range setCopy.Scopes {
			setCopy.Scopes[i].RootTaskIDs = slices.Clone(lifecycle.ContributingSet.Scopes[i].RootTaskIDs)
		}
		lifecycleCopy.ContributingSet = &setCopy
	}
	lifecycleCopy.Coverage = slices.Clone(lifecycle.Coverage)
	for i := range lifecycleCopy.Coverage {
		lifecycleCopy.Coverage[i].ApprovalAttestations = slices.Clone(lifecycle.Coverage[i].ApprovalAttestations)
		for j := range lifecycleCopy.Coverage[i].ApprovalAttestations {
			lifecycleCopy.Coverage[i].ApprovalAttestations[j].Validation = slices.Clone(lifecycle.Coverage[i].ApprovalAttestations[j].Validation)
		}
		if lifecycle.Coverage[i].SliceReport != nil {
			reportCopy := *lifecycle.Coverage[i].SliceReport
			lifecycleCopy.Coverage[i].SliceReport = &reportCopy
		}
	}
	lifecycleCopy.GlobalGenerations = slices.Clone(lifecycle.GlobalGenerations)
	lifecycleCopy.MutationReceipts = slices.Clone(lifecycle.MutationReceipts)
	if lifecycle.Closure != nil {
		closureCopy := *lifecycle.Closure
		lifecycleCopy.Closure = &closureCopy
	}
	previous.Goal.Integration = &lifecycleCopy
	return &previous
}
