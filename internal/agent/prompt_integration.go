package agent

import (
	"fmt"
	"slices"
	"sort"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/prompts"
)

// integrationDescriptionLimit truncates, in runes, a task description shown in
// the integration context.
const integrationDescriptionLimit = 200

const (
	// sliceAnalysisGeneration is the generation a slice analysis carries; only
	// a global analysis has a positive generation.
	sliceAnalysisGeneration = 0
	// firstGlobalGeneration is the lowest valid generation of a global analysis.
	firstGlobalGeneration = 1
	// multiPlanScopeCount is the contributing-set size from which every plan
	// must have coverage; a single-plan set may omit it.
	multiPlanScopeCount = 2
)

func populateIntegrationContext(task *models.Task, state *models.State, resolver *pipeline.Resolver, projectRoot string, data *prompts.RoleContextData) error {
	metadata := task.IntegrationAnalysis
	if metadata == nil {
		if state.Goal.BaseCommit != nil {
			data.GoalBaseCommit = *state.Goal.BaseCommit
		}
		data.CompletedTasks = collectCompletedTasks(state)
		return nil
	}
	if !metadata.Phase.IsValid() {
		return fmt.Errorf("integration context for task %s has invalid phase %q", task.ID, metadata.Phase)
	}
	if metadata.SourceCommit == "" {
		return fmt.Errorf("integration context for task %s has empty source commit", task.ID)
	}

	data.IntegrationPhase = metadata.Phase
	data.IntegrationGeneration = metadata.Generation
	data.IntegrationSourceCommit = metadata.SourceCommit

	switch metadata.Phase {
	case models.IntegrationAnalysisPhaseSlice:
		return populateSliceIntegrationContext(task, state, resolver, data)
	case models.IntegrationAnalysisPhaseGlobal:
		return populateGlobalIntegrationContext(task, state, projectRoot, data)
	default:
		return fmt.Errorf("integration context for task %s has unsupported phase %q", task.ID, metadata.Phase)
	}
}

func populateSliceIntegrationContext(task *models.Task, state *models.State, resolver *pipeline.Resolver, data *prompts.RoleContextData) error {
	metadata := task.IntegrationAnalysis
	capability, err := resolver.SlicedIntegrationCapability()
	if err != nil {
		return fmt.Errorf("resolve sliced integration capability: %w", err)
	}
	if !capability.Available {
		return fmt.Errorf("slice integration context unavailable (%s): %s", capability.Code, capability.Guidance)
	}
	if metadata.Generation != sliceAnalysisGeneration {
		return fmt.Errorf("slice integration context for task %s has generation %d", task.ID, metadata.Generation)
	}
	if metadata.OriginatingPlanTaskID == "" {
		return fmt.Errorf("slice integration context for task %s has no originating plan", task.ID)
	}
	originatingPlan := state.FindTask(metadata.OriginatingPlanTaskID)
	if originatingPlan == nil {
		return fmt.Errorf("slice integration context for task %s references missing plan %q", task.ID, metadata.OriginatingPlanTaskID)
	}
	data.IntegrationOriginatingPlan = &prompts.IntegrationPlanSummary{
		ID:          originatingPlan.ID,
		Description: originatingPlan.Description,
		DoneWhen:    originatingPlan.DoneWhen,
		SpecRef:     originatingPlan.SpecRef,
		PlanRef:     paths.SplitRefFile(originatingPlan.PlanRef),
		ArchRef:     paths.SplitRefFile(originatingPlan.ArchRef),
	}

	rootIDs, err := sortedUniquePromptStrings(metadata.RootTaskIDs, "root task")
	if err != nil {
		return fmt.Errorf("slice integration context for task %s: %w", task.ID, err)
	}
	if len(rootIDs) == 0 {
		return fmt.Errorf("slice integration context for task %s has no root tasks", task.ID)
	}
	for _, rootID := range rootIDs {
		if state.FindTask(rootID) == nil {
			return fmt.Errorf("slice integration context for task %s references missing root task %q", task.ID, rootID)
		}
	}
	data.IntegrationRootTaskIDs = rootIDs

	changes := slices.Clone(metadata.DescendantChanges)
	sort.Slice(changes, func(i, j int) bool { return changes[i].TaskID < changes[j].TaskID })
	seenDescendants := make(map[string]struct{}, len(changes))
	for _, change := range changes {
		if change.TaskID == "" || change.Commit == "" {
			return fmt.Errorf("slice integration context for task %s has incomplete descendant attribution", task.ID)
		}
		if _, duplicate := seenDescendants[change.TaskID]; duplicate {
			return fmt.Errorf("slice integration context for task %s repeats descendant %q", task.ID, change.TaskID)
		}
		seenDescendants[change.TaskID] = struct{}{}
		descendant := state.FindTask(change.TaskID)
		if descendant == nil {
			return fmt.Errorf("slice integration context for task %s references missing descendant task %q", task.ID, change.TaskID)
		}
		if descendant.MergeCommit == nil || *descendant.MergeCommit != change.Commit {
			return fmt.Errorf("slice integration context for task %s descendant %q commit contradicts persisted task evidence", task.ID, change.TaskID)
		}
		dependsOn := slices.Clone(descendant.DependsOn)
		sort.Strings(dependsOn)
		data.IntegrationDescendants = append(data.IntegrationDescendants, prompts.IntegrationDescendantSummary{
			ID:            descendant.ID,
			Description:   prompts.TruncateText(descendant.Description, integrationDescriptionLimit),
			DoneWhen:      descendant.DoneWhen,
			SpecRef:       descendant.SpecRef,
			Commit:        change.Commit,
			DependsOn:     dependsOn,
			Decomposition: cloneDecompositionManifest(descendant.Decomposition),
		})
	}

	affectedPaths, err := sortedUniquePromptStrings(metadata.AffectedPaths, "affected path")
	if err != nil {
		return fmt.Errorf("slice integration context for task %s: %w", task.ID, err)
	}
	snapshotPaths, err := sortedUniquePromptStrings(metadata.SourceSnapshotPaths, "snapshot path")
	if err != nil {
		return fmt.Errorf("slice integration context for task %s: %w", task.ID, err)
	}
	affected := make(map[string]struct{}, len(affectedPaths))
	for _, path := range affectedPaths {
		affected[path] = struct{}{}
	}
	for _, path := range snapshotPaths {
		if _, ok := affected[path]; !ok {
			return fmt.Errorf("slice integration context for task %s snapshot path %q is not attributable", task.ID, path)
		}
	}
	data.IntegrationAffectedPaths = affectedPaths
	data.IntegrationSnapshotPaths = snapshotPaths
	return nil
}

func populateGlobalIntegrationContext(task *models.Task, state *models.State, projectRoot string, data *prompts.RoleContextData) error {
	metadata := task.IntegrationAnalysis
	if metadata.Generation < firstGlobalGeneration {
		return fmt.Errorf("global integration context for task %s has invalid generation %d", task.ID, metadata.Generation)
	}
	if metadata.OriginatingPlanTaskID != "" || len(metadata.RootTaskIDs) != 0 || len(metadata.DescendantChanges) != 0 || len(metadata.AffectedPaths) != 0 || len(metadata.SourceSnapshotPaths) != 0 {
		return fmt.Errorf("global integration context for task %s contains slice-only metadata", task.ID)
	}
	if state.Goal.BaseCommit == nil || *state.Goal.BaseCommit == "" {
		return fmt.Errorf("global integration context for task %s has no goal base commit", task.ID)
	}
	data.GoalBaseCommit = *state.Goal.BaseCommit
	if state.Goal.Integration == nil || state.Goal.Integration.ContributingSet == nil {
		return fmt.Errorf("global integration context for task %s has no frozen contributing set", task.ID)
	}

	coverageByPlan := make(map[string]models.IntegrationCoverageRecord, len(state.Goal.Integration.Coverage))
	for _, record := range state.Goal.Integration.Coverage {
		if record.PlanTaskID == "" {
			return fmt.Errorf("global integration context for task %s has coverage with no plan", task.ID)
		}
		if _, duplicate := coverageByPlan[record.PlanTaskID]; duplicate {
			return fmt.Errorf("global integration context for task %s repeats coverage plan %q", task.ID, record.PlanTaskID)
		}
		coverageByPlan[record.PlanTaskID] = record
	}

	scopes := slices.Clone(state.Goal.Integration.ContributingSet.Scopes)
	sort.Slice(scopes, func(i, j int) bool { return scopes[i].PlanTaskID < scopes[j].PlanTaskID })
	seenPlans := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		if scope.PlanTaskID == "" {
			return fmt.Errorf("global integration context for task %s has contributing scope with no plan", task.ID)
		}
		if _, duplicate := seenPlans[scope.PlanTaskID]; duplicate {
			return fmt.Errorf("global integration context for task %s repeats contributing plan %q", task.ID, scope.PlanTaskID)
		}
		seenPlans[scope.PlanTaskID] = struct{}{}
		if state.FindTask(scope.PlanTaskID) == nil {
			return fmt.Errorf("global integration context for task %s references missing contributing plan %q", task.ID, scope.PlanTaskID)
		}
		record, ok := coverageByPlan[scope.PlanTaskID]
		if !ok {
			if len(scopes) >= multiPlanScopeCount {
				return fmt.Errorf("global integration context for task %s lacks coverage for plan %q", task.ID, scope.PlanTaskID)
			}
			continue
		}
		summary, err := integrationCoverageSummary(state, scope, record)
		if err != nil {
			return fmt.Errorf("global integration context for task %s: %w", task.ID, err)
		}
		data.IntegrationCoverage = append(data.IntegrationCoverage, summary)
	}
	for planTaskID := range coverageByPlan {
		if _, ok := seenPlans[planTaskID]; !ok {
			return fmt.Errorf("global integration context for task %s contains coverage outside the frozen contributing set", task.ID)
		}
	}

	surface, err := ops.BuildGlobalIntegrationSurface(state, projectRoot, metadata.Generation)
	if err != nil {
		return fmt.Errorf("global integration context for task %s: %w", task.ID, err)
	}
	for _, plan := range surface.Plans {
		data.IntegrationPlanSurfaces = append(data.IntegrationPlanSurfaces, prompts.IntegrationPlanSurfaceSummary{
			PlanTaskID:         plan.PlanTaskID,
			Tasks:              integrationTaskChangeSummaries(plan.Tasks),
			PathCount:          plan.PathCount,
			InterfacesOwned:    plan.InterfacesOwned,
			InterfacesConsumed: plan.InterfacesConsumed,
		})
	}
	data.IntegrationPriorRepairs = integrationTaskChangeSummaries(surface.PriorRepairs)
	data.IntegrationSeamPaths = integrationSeamSummaries(surface.SeamPaths)
	data.IntegrationSeamInterfaces = integrationSeamSummaries(surface.SeamInterfaces)
	if previous := surface.PreviousGeneration; previous != nil {
		data.IntegrationPreviousGeneration = previous.Generation
		data.IntegrationPreviousVerdict = string(previous.Verdict)
		data.IntegrationPreviousSourceCommit = previous.SourceCommit
		for _, repair := range previous.Repairs {
			data.IntegrationFixRepairs = append(data.IntegrationFixRepairs, integrationApprovalSummary(repair))
		}
	}
	return nil
}

func integrationApprovalSummary(attestation models.IntegrationApprovalAttestation) prompts.IntegrationApprovalSummary {
	return prompts.IntegrationApprovalSummary{
		ReviewedTaskID:     attestation.ReviewedTaskID,
		AcceptanceCriteria: attestation.AcceptanceCriteria,
		ReviewedCommit:     attestation.ReviewedCommit,
		Approver:           attestation.Approver,
		Validation:         slices.Clone(attestation.Validation),
		MergeCommit:        attestation.MergeCommit,
	}
}

func integrationTaskChangeSummaries(changes []ops.GlobalIntegrationTaskChange) []prompts.IntegrationTaskChangeSummary {
	summaries := make([]prompts.IntegrationTaskChangeSummary, 0, len(changes))
	for _, change := range changes {
		summaries = append(summaries, prompts.IntegrationTaskChangeSummary{
			ID:           change.TaskID,
			Description:  prompts.TruncateText(change.Description, integrationDescriptionLimit),
			BaseCommit:   change.BaseCommit,
			ReviewCommit: change.ReviewCommit,
			PathCount:    len(change.Paths),
		})
	}
	return summaries
}

func integrationSeamSummaries(seams []ops.GlobalIntegrationSeam) []prompts.IntegrationSeamSummary {
	summaries := make([]prompts.IntegrationSeamSummary, 0, len(seams))
	for _, seam := range seams {
		summaries = append(summaries, prompts.IntegrationSeamSummary{Name: seam.Name, PlanTaskIDs: seam.PlanTaskIDs})
	}
	return summaries
}

func integrationCoverageSummary(state *models.State, scope models.IntegrationScopeSnapshot, record models.IntegrationCoverageRecord) (prompts.IntegrationCoverageSummary, error) {
	summary := prompts.IntegrationCoverageSummary{PlanTaskID: record.PlanTaskID, Kind: string(record.Kind)}
	switch record.Kind {
	case models.IntegrationCoverageApprovalAttestation:
		if len(record.ApprovalAttestations) == 0 || record.SliceReport != nil {
			return prompts.IntegrationCoverageSummary{}, fmt.Errorf("approval coverage for plan %q has contradictory payload", record.PlanTaskID)
		}
		attestations := slices.Clone(record.ApprovalAttestations)
		sort.Slice(attestations, func(i, j int) bool { return attestations[i].ReviewedTaskID < attestations[j].ReviewedTaskID })
		for _, attestation := range attestations {
			if attestation.ReviewedTaskID == "" || state.FindTask(attestation.ReviewedTaskID) == nil {
				return prompts.IntegrationCoverageSummary{}, fmt.Errorf("approval coverage for plan %q references missing task %q", record.PlanTaskID, attestation.ReviewedTaskID)
			}
			summary.ApprovalAttestations = append(summary.ApprovalAttestations, integrationApprovalSummary(attestation))
		}
	case models.IntegrationCoverageSliceReport:
		if record.SliceReport == nil || len(record.ApprovalAttestations) != 0 {
			return prompts.IntegrationCoverageSummary{}, fmt.Errorf("slice coverage for plan %q has contradictory payload", record.PlanTaskID)
		}
		report := record.SliceReport
		analysis := state.FindTask(report.AnalysisTaskID)
		if analysis == nil || analysis.IntegrationAnalysis == nil {
			return prompts.IntegrationCoverageSummary{}, fmt.Errorf("slice coverage for plan %q references missing analysis task %q", record.PlanTaskID, report.AnalysisTaskID)
		}
		analysisMetadata := analysis.IntegrationAnalysis
		if analysisMetadata.Phase != models.IntegrationAnalysisPhaseSlice || analysisMetadata.Key != report.AnalysisKey || analysisMetadata.OriginatingPlanTaskID != scope.PlanTaskID || analysisMetadata.SourceCommit != report.SourceCommit {
			return prompts.IntegrationCoverageSummary{}, fmt.Errorf("slice coverage for plan %q contradicts analysis task %q", record.PlanTaskID, report.AnalysisTaskID)
		}
		summary.SliceReport = &prompts.IntegrationSliceReportSummary{
			AnalysisTaskID: report.AnalysisTaskID,
			AnalysisKey:    report.AnalysisKey,
			Verdict:        string(report.Verdict),
			SourceCommit:   report.SourceCommit,
			ReportCommit:   report.ReportCommit,
		}
	default:
		return prompts.IntegrationCoverageSummary{}, fmt.Errorf("coverage for plan %q has invalid kind %q", record.PlanTaskID, record.Kind)
	}
	return summary, nil
}

func sortedUniquePromptStrings(values []string, label string) ([]string, error) {
	result := slices.Clone(values)
	sort.Strings(result)
	for i, value := range result {
		if value == "" {
			return nil, fmt.Errorf("%s is empty", label)
		}
		if i > 0 && value == result[i-1] {
			return nil, fmt.Errorf("duplicate %s %q", label, value)
		}
	}
	return result, nil
}

func cloneDecompositionManifest(manifest *models.DecompositionManifest) *models.DecompositionManifest {
	if manifest == nil {
		return nil
	}
	clone := *manifest
	clone.OwnedFiles = slices.Clone(manifest.OwnedFiles)
	clone.OwnedModules = slices.Clone(manifest.OwnedModules)
	clone.ReadOnlyDependsOn = slices.Clone(manifest.ReadOnlyDependsOn)
	clone.ReadOnlyTaskDependsOn = slices.Clone(manifest.ReadOnlyTaskDependsOn)
	clone.InterfacesOwned = slices.Clone(manifest.InterfacesOwned)
	clone.InterfacesConsumed = slices.Clone(manifest.InterfacesConsumed)
	return &clone
}

// collectCompletedTasks returns summaries of all MERGED tasks for integration context.
func collectCompletedTasks(state *models.State) []prompts.CompletedTaskSummary {
	var tasks []prompts.CompletedTaskSummary
	for _, t := range state.Tasks {
		if t.Status == models.TaskStatusMerged {
			tasks = append(tasks, prompts.CompletedTaskSummary{
				ID:          t.ID,
				Description: prompts.TruncateText(t.Description, integrationDescriptionLimit),
				DoneWhen:    t.DoneWhen,
				SpecRef:     t.SpecRef,
			})
		}
	}
	return tasks
}
