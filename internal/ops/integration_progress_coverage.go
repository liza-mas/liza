package ops

import (
	"fmt"
	"sort"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// Slice coverage and approval attestations of the integration progress
// evaluator: which frozen scope is covered by what immutable evidence.

// evaluateCoverage projects the evidence each frozen scope needs once the
// cohort holds two or more scopes: an approval attestation for a single-root
// scope, an analyst slice report for a multi-root one.
func (e *integrationProgressEvaluator) evaluateCoverage(
	decision *models.IntegrationProgressDecision,
	lifecycle *models.IntegrationLifecycle,
	capability pipeline.SlicedIntegrationCapability,
) (bool, error) {
	if decision.ContributingSet == nil || len(decision.ContributingSet.Scopes) < slicedCohortMinScopes {
		return true, nil
	}
	records, err := coverageRecordsByPlan(lifecycle)
	if err != nil {
		return false, err
	}
	cohortPlans := make(map[string]bool, len(decision.ContributingSet.Scopes))
	allReady := true
	for _, scope := range decision.ContributingSet.Scopes {
		cohortPlans[scope.PlanTaskID] = true
		coverage := models.IntegrationScopeCoverage{
			PlanTaskID: scope.PlanTaskID, RootTaskIDs: append([]string(nil), scope.RootTaskIDs...),
		}
		record := records[scope.PlanTaskID]
		var ready bool
		if len(scope.RootTaskIDs) == 1 {
			ready, err = e.attestedScopeCoverage(&coverage, scope, record)
		} else {
			ready, err = e.sliceScopeCoverage(decision, &coverage, scope, record)
		}
		if err != nil {
			return false, err
		}
		allReady = allReady && ready
		decision.Coverage = append(decision.Coverage, coverage)
	}
	if err := rejectNonContributingCoverage(records, cohortPlans); err != nil {
		return false, err
	}
	return coverageVerdict(decision, capability, allReady), nil
}

// coverageRecordsByPlan indexes the persisted coverage, refusing two records
// for one plan.
func coverageRecordsByPlan(lifecycle *models.IntegrationLifecycle) (map[string]*models.IntegrationCoverageRecord, error) {
	records := make(map[string]*models.IntegrationCoverageRecord, len(lifecycle.Coverage))
	for i := range lifecycle.Coverage {
		record := &lifecycle.Coverage[i]
		if records[record.PlanTaskID] != nil {
			return nil, fmt.Errorf("integration progress: duplicate coverage for plan %q", record.PlanTaskID)
		}
		records[record.PlanTaskID] = record
	}
	return records, nil
}

// attestedScopeCoverage covers a single-root scope with the approval facts of
// its merged lineage; it needs no analyst and is always ready.
func (e *integrationProgressEvaluator) attestedScopeCoverage(
	coverage *models.IntegrationScopeCoverage,
	scope models.IntegrationScopeSnapshot,
	record *models.IntegrationCoverageRecord,
) (bool, error) {
	attestations, err := e.approvalAttestations(scope.RootTaskIDs[0], record)
	if err != nil {
		return false, err
	}
	coverage.Kind = models.IntegrationCoverageApprovalAttestation
	coverage.ApprovalAttestations = attestations
	coverage.Effective = true
	coverage.Resolved = true
	return true, nil
}

// sliceScopeCoverage covers a multi-root scope with the slice analysis of its
// plan: requested while absent, validated and judged by verdict once recorded.
func (e *integrationProgressEvaluator) sliceScopeCoverage(
	decision *models.IntegrationProgressDecision,
	coverage *models.IntegrationScopeCoverage,
	scope models.IntegrationScopeSnapshot,
	record *models.IntegrationCoverageRecord,
) (bool, error) {
	key := sliceAnalysisKey(scope.PlanTaskID)
	coverage.Kind = models.IntegrationCoverageSliceReport
	coverage.AnalysisKey = key
	if record == nil {
		e.requestSliceAnalysis(decision, scope, key)
		return false, nil
	}
	if record.Kind != models.IntegrationCoverageSliceReport || record.SliceReport == nil {
		return false, fmt.Errorf("integration progress: plan %q requires slice coverage", scope.PlanTaskID)
	}
	if err := e.validateSliceReport(scope, record.SliceReport, key); err != nil {
		return false, err
	}
	coverage.Effective = true
	return e.sliceReportResolved(decision, coverage, record.SliceReport, key)
}

// requestSliceAnalysis asks for the missing slice analysis, or blocks when the
// existing one can no longer finish.
func (e *integrationProgressEvaluator) requestSliceAnalysis(
	decision *models.IntegrationProgressDecision,
	scope models.IntegrationScopeSnapshot,
	key string,
) {
	analysis := e.analysisKeys[key]
	if analysis == nil {
		decision.SliceRequests = append(decision.SliceRequests, models.IntegrationAnalysisRequest{
			Key: key, Phase: models.IntegrationAnalysisPhaseSlice,
			OriginatingPlanTaskID: scope.PlanTaskID,
			RootTaskIDs:           append([]string(nil), scope.RootTaskIDs...),
		})
		return
	}
	if analysis.Status == models.TaskStatusBlocked || analysis.Status == models.TaskStatusAbandoned {
		decision.Blocked = progressReasonWithTasks(integrationProgressBlockedAnalysis, analysis.ID)
	}
}

// sliceReportResolved judges a recorded slice report: a clean verdict
// resolves the scope, findings resolve it once their repairs settle.
func (e *integrationProgressEvaluator) sliceReportResolved(
	decision *models.IntegrationProgressDecision,
	coverage *models.IntegrationScopeCoverage,
	report *models.IntegrationSliceReport,
	key string,
) (bool, error) {
	switch report.Verdict {
	case models.IntegrationAnalysisVerdictClean:
		coverage.Resolved = true
		return true, nil
	case models.IntegrationAnalysisVerdictFindings:
		resolution, err := e.resolveAnalysisRepairs(report.AnalysisTaskID)
		if err != nil {
			return false, err
		}
		if resolution.blocked {
			decision.Blocked = progressReasonWithTasks(integrationProgressBlockedRepair, resolution.taskIDs...)
			return false, nil
		}
		if !resolution.settled {
			decision.Waiting = progressReasonWithTasks(integrationProgressWaitingRepairs, resolution.taskIDs...)
			return false, nil
		}
		coverage.Resolved = true
		return true, nil
	default:
		return false, fmt.Errorf("integration progress: slice %q has invalid verdict %q", key, report.Verdict)
	}
}

// rejectNonContributingCoverage refuses persisted coverage for a plan outside
// the frozen cohort.
func rejectNonContributingCoverage(records map[string]*models.IntegrationCoverageRecord, cohortPlans map[string]bool) error {
	extra := make([]string, 0)
	for planID := range records {
		if !cohortPlans[planID] {
			extra = append(extra, planID)
		}
	}
	if len(extra) == 0 {
		return nil
	}
	sort.Strings(extra)
	return fmt.Errorf("integration progress: coverage references non-contributing plan %q", extra[0])
}

// coverageVerdict decides whether coverage is complete: slice requests need a
// pipeline that supports sliced integration, and an incomplete scope without a
// more specific reason waits on slice coverage.
func coverageVerdict(decision *models.IntegrationProgressDecision, capability pipeline.SlicedIntegrationCapability, allReady bool) bool {
	if len(decision.SliceRequests) > 0 && !capability.Available {
		code := capability.Code
		if code == "" {
			code = string(integrationProgressBlockedCapability)
		}
		decision.Blocked = &models.IntegrationProgressReason{Code: code, Guidance: capability.Guidance}
		return false
	}
	if !allReady && decision.Blocked == nil && decision.Waiting == nil {
		ids := make([]string, 0, len(decision.SliceRequests))
		for _, request := range decision.SliceRequests {
			ids = append(ids, request.Key)
		}
		decision.Waiting = progressReasonWithTasks(integrationProgressWaitingSliceCoverage, ids...)
	}
	return allReady
}

func (e *integrationProgressEvaluator) approvalAttestations(
	rootID string,
	record *models.IntegrationCoverageRecord,
) ([]models.IntegrationApprovalAttestation, error) {
	mergedLeaves, err := e.mergedLineageLeaves(rootID)
	if err != nil {
		return nil, err
	}
	if len(mergedLeaves) == 0 {
		return nil, fmt.Errorf("integration progress: root lineage %q has no merged leaves", rootID)
	}

	if record != nil {
		if record.Kind != models.IntegrationCoverageApprovalAttestation || len(record.ApprovalAttestations) == 0 {
			return nil, fmt.Errorf("integration progress: root %q requires approval attestation coverage", rootID)
		}
		reviewedTaskIDs := make([]string, 0, len(record.ApprovalAttestations))
		for _, attestation := range record.ApprovalAttestations {
			reviewedID := attestation.ReviewedTaskID
			task := e.tasks[reviewedID]
			if task == nil || task.Status != models.TaskStatusMerged || !e.lineageContains(rootID, reviewedID) {
				return nil, fmt.Errorf("integration progress: approval attestation task %q is outside merged root lineage %q", reviewedID, rootID)
			}
			reviewedTaskIDs = append(reviewedTaskIDs, reviewedID)
		}
		if len(uniqueSortedStrings(reviewedTaskIDs)) != len(reviewedTaskIDs) || !equalStringSets(reviewedTaskIDs, mergedLeaves) {
			return nil, fmt.Errorf("integration progress: approval attestation tasks %v do not exactly cover merged root lineage %q leaves %v", reviewedTaskIDs, rootID, mergedLeaves)
		}
		attestations := cloneApprovalAttestations(record.ApprovalAttestations)
		sort.Slice(attestations, func(i, j int) bool {
			return attestations[i].ReviewedTaskID < attestations[j].ReviewedTaskID
		})
		return attestations, nil
	}

	attestations := make([]models.IntegrationApprovalAttestation, 0, len(mergedLeaves))
	for _, leafID := range mergedLeaves {
		attestation, err := approvalAttestationFor(e.tasks[leafID])
		if err != nil {
			return nil, err
		}
		attestations = append(attestations, attestation)
	}
	return attestations, nil
}

// approvalAttestationFor reads the immutable approval facts of one merged task.
func approvalAttestationFor(task *models.Task) (models.IntegrationApprovalAttestation, error) {
	if task.ReviewCommit == nil || *task.ReviewCommit == "" || task.ApprovedBy == nil || *task.ApprovedBy == "" || task.MergeCommit == nil || *task.MergeCommit == "" || task.DoneWhen == "" {
		return models.IntegrationApprovalAttestation{}, fmt.Errorf("integration progress: merged task %q lacks approval attestation facts", task.ID)
	}
	return models.IntegrationApprovalAttestation{
		ReviewedTaskID:     task.ID,
		AcceptanceCriteria: task.DoneWhen,
		ReviewedCommit:     *task.ReviewCommit,
		Approver:           *task.ApprovedBy,
		Validation:         append([]string(nil), task.Validation...),
		MergeCommit:        *task.MergeCommit,
	}, nil
}

func (e *integrationProgressEvaluator) validateSliceReport(
	scope models.IntegrationScopeSnapshot,
	report *models.IntegrationSliceReport,
	key string,
) error {
	if report.AnalysisKey != key {
		return fmt.Errorf("integration progress: slice report key %q does not match %q", report.AnalysisKey, key)
	}
	analysis := e.tasks[report.AnalysisTaskID]
	if analysis == nil || analysis.IntegrationAnalysis == nil {
		return fmt.Errorf("integration progress: slice report %q references missing analysis task %q", key, report.AnalysisTaskID)
	}
	metadata := analysis.IntegrationAnalysis
	if metadata.Key != key || metadata.Phase != models.IntegrationAnalysisPhaseSlice || metadata.OriginatingPlanTaskID != scope.PlanTaskID || !equalStringSets(metadata.RootTaskIDs, scope.RootTaskIDs) || metadata.SourceCommit != report.SourceCommit {
		return fmt.Errorf("integration progress: slice report %q contradicts analysis metadata", key)
	}
	return nil
}
