package ops

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

const (
	sliceIntegrationRolePair  = "slice-integration-pair"
	globalIntegrationRolePair = "integration-pair"
)

// ReconcileIntegrationAnalysesResult reports the durable projection made by
// ReconcileIntegrationAnalyses.
type ReconcileIntegrationAnalysesResult struct {
	Changed        bool                       `json:"changed"`
	CreatedTaskIDs []string                   `json:"created_task_ids,omitempty"`
	Reason         *IntegrationProgressReason `json:"reason,omitempty"`
}

type reconcileIntegrationAnalysesTestHooks struct {
	capability       *pipeline.SlicedIntegrationCapability
	beforeValidation func(*models.State)
}

var testReconcileIntegrationAnalysesHooks *reconcileIntegrationAnalysesTestHooks

// ReconcileIntegrationAnalyses atomically projects the immutable cohort,
// coverage, analysis work, and non-clean closure requested by the authoritative
// integration progress decision.
func ReconcileIntegrationAnalyses(projectRoot string) (*ReconcileIntegrationAnalysesResult, error) {
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("load integration reconciliation pipeline: %w", err)
	}
	capability := resolver.SlicedIntegrationCapability()
	if hooks := testReconcileIntegrationAnalysesHooks; hooks != nil && hooks.capability != nil {
		capability = *hooks.capability
	}

	result := &ReconcileIntegrationAnalysesResult{}
	gitWrapper := gitpkg.New(projectRoot)
	bb := db.For(paths.New(projectRoot).StatePath())
	err = bb.Modify(func(state *models.State) error {
		previous := snapshotIntegrationLifecycleState(state)
		integrationHEAD, headErr := gitWrapper.GetCommitSHA(state.Config.IntegrationBranch)
		if headErr != nil {
			return fmt.Errorf("read integration HEAD: %w", headErr)
		}
		decision, decisionErr := EvaluateIntegrationProgress(state, capability, integrationHEAD)
		if decisionErr != nil {
			return decisionErr
		}

		changed, projectionErr := projectIntegrationProgressDecision(
			state,
			decision,
			integrationHEAD,
			projectRoot,
			resolver,
			gitWrapper,
			result,
		)
		if projectionErr != nil {
			return projectionErr
		}
		result.Changed = changed
		if hooks := testReconcileIntegrationAnalysesHooks; hooks != nil && hooks.beforeValidation != nil {
			hooks.beforeValidation(state)
		}
		if validationErr := validateIntegrationLifecycleCandidate(projectRoot, previous, state); validationErr != nil {
			return validationErr
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reconcile integration analyses: %w", err)
	}
	return result, nil
}

func projectIntegrationProgressDecision(
	state *models.State,
	decision IntegrationProgressDecision,
	integrationHEAD string,
	projectRoot string,
	resolver *pipeline.Resolver,
	gitWrapper *gitpkg.Git,
	result *ReconcileIntegrationAnalysesResult,
) (bool, error) {
	changed := false
	needsLifecycle := decision.FreezeContributingSet || len(decision.Coverage) > 0 ||
		len(decision.SliceRequests) > 0 || decision.GlobalRequest != nil || decision.Blocked != nil
	if state.Goal.Integration == nil && needsLifecycle {
		state.Goal.Integration = &models.IntegrationLifecycle{}
		changed = true
	}
	lifecycle := state.Goal.Integration

	if decision.FreezeContributingSet {
		if lifecycle.ContributingSet != nil {
			return false, fmt.Errorf("integration contributing set already exists while freeze was requested")
		}
		lifecycle.ContributingSet = cloneIntegrationContributingSet(decision.ContributingSet)
		changed = true
	}

	if lifecycle != nil {
		coverageChanged, err := appendApprovalCoverage(lifecycle, decision.Coverage)
		if err != nil {
			return false, err
		}
		changed = changed || coverageChanged
	}

	requests := slices.Clone(decision.SliceRequests)
	if decision.GlobalRequest != nil {
		requests = append(requests, *decision.GlobalRequest)
	}
	if decision.Blocked != nil {
		requests = nil
	}
	sort.Slice(requests, func(i, j int) bool { return requests[i].Key < requests[j].Key })
	now := time.Now().UTC()
	for _, request := range requests {
		createdID, created, err := materializeIntegrationAnalysis(
			state,
			decision,
			request,
			integrationHEAD,
			projectRoot,
			resolver,
			gitWrapper,
			now,
		)
		if err != nil {
			return false, err
		}
		if created {
			result.CreatedTaskIDs = append(result.CreatedTaskIDs, createdID)
			changed = true
		}
	}

	if lifecycle != nil {
		closureChanged := projectNonCleanIntegrationClosure(lifecycle, decision, result)
		changed = changed || closureChanged
	}
	return changed, nil
}

func appendApprovalCoverage(
	lifecycle *models.IntegrationLifecycle,
	coverage []IntegrationScopeCoverage,
) (bool, error) {
	existing := make(map[string]models.IntegrationCoverageRecord, len(lifecycle.Coverage))
	for _, record := range lifecycle.Coverage {
		existing[record.PlanTaskID] = record
	}
	pending := make([]models.IntegrationCoverageRecord, 0)
	for _, scope := range coverage {
		if scope.Kind != models.IntegrationCoverageApprovalAttestation {
			continue
		}
		record := models.IntegrationCoverageRecord{
			PlanTaskID:           scope.PlanTaskID,
			Kind:                 models.IntegrationCoverageApprovalAttestation,
			ApprovalAttestations: cloneApprovalAttestations(scope.ApprovalAttestations),
		}
		if persisted, ok := existing[scope.PlanTaskID]; ok {
			if !reflect.DeepEqual(persisted, record) {
				return false, fmt.Errorf("integration coverage collision for plan %q", scope.PlanTaskID)
			}
			continue
		}
		pending = append(pending, record)
	}
	if len(pending) == 0 {
		return false, nil
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].PlanTaskID < pending[j].PlanTaskID })
	lifecycle.Coverage = append(lifecycle.Coverage, pending...)
	return true, nil
}

func materializeIntegrationAnalysis(
	state *models.State,
	decision IntegrationProgressDecision,
	request IntegrationAnalysisRequest,
	integrationHEAD string,
	projectRoot string,
	resolver *pipeline.Resolver,
	gitWrapper *gitpkg.Git,
	now time.Time,
) (string, bool, error) {
	taskID := integrationAnalysisTaskID(request.Key)
	rolePair, err := analysisRolePair(request.Phase)
	if err != nil {
		return "", false, err
	}
	initialStatus, err := resolver.InitialStatus(rolePair)
	if err != nil {
		return "", false, fmt.Errorf("resolve initial status for %s: %w", rolePair, err)
	}

	metadata, refs, parents, err := buildIntegrationAnalysisProjection(
		state,
		decision,
		request,
		integrationHEAD,
		projectRoot,
		gitWrapper,
	)
	if err != nil {
		return "", false, err
	}
	if existing := state.FindTask(taskID); existing != nil {
		if existing.Type != models.TaskTypeIntegration || existing.RolePair != rolePair ||
			existing.Status != initialStatus || !reflect.DeepEqual(existing.IntegrationAnalysis, metadata) ||
			!reflect.DeepEqual(uniqueSortedStrings(existing.ParentTasks), parents) || len(existing.DependsOn) != 0 {
			return "", false, fmt.Errorf("integration analysis task collision for key %q and id %q", request.Key, taskID)
		}
		if !slices.Contains(state.Sprint.Scope.Planned, taskID) {
			return "", false, fmt.Errorf("integration analysis task %q is missing sprint planned registration", taskID)
		}
		return taskID, false, nil
	}

	task := models.Task{
		ID:                  taskID,
		Type:                models.TaskTypeIntegration,
		RolePair:            rolePair,
		Description:         analysisDescription(request),
		Status:              initialStatus,
		Priority:            1,
		SpecRef:             refs.spec,
		PlanRef:             refs.plan,
		ArchRef:             refs.arch,
		DoneWhen:            analysisDoneWhen(request),
		Scope:               analysisScope(request),
		ParentTasks:         parents,
		DependsOn:           nil,
		Created:             now,
		History:             []models.TaskHistoryEntry{},
		IntegrationAnalysis: metadata,
	}
	state.Tasks = append(state.Tasks, task)
	if !slices.Contains(state.Sprint.Scope.Planned, taskID) {
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, taskID)
	}
	return taskID, true, nil
}

type analysisRefs struct {
	spec string
	plan string
	arch string
}

func buildIntegrationAnalysisProjection(
	state *models.State,
	decision IntegrationProgressDecision,
	request IntegrationAnalysisRequest,
	integrationHEAD string,
	projectRoot string,
	gitWrapper *gitpkg.Git,
) (*models.IntegrationAnalysisMetadata, analysisRefs, []string, error) {
	switch request.Phase {
	case models.IntegrationAnalysisPhaseSlice:
		plan := state.FindTask(request.OriginatingPlanTaskID)
		if plan == nil {
			return nil, analysisRefs{}, nil, fmt.Errorf("slice analysis %q references missing plan %q", request.Key, request.OriginatingPlanTaskID)
		}
		changes, affected, snapshot, err := sliceAnalysisSurface(state, request.RootTaskIDs, integrationHEAD, projectRoot, gitWrapper)
		if err != nil {
			return nil, analysisRefs{}, nil, fmt.Errorf("build slice analysis %q: %w", request.Key, err)
		}
		metadata := &models.IntegrationAnalysisMetadata{
			Key:                   request.Key,
			Phase:                 request.Phase,
			OriginatingPlanTaskID: request.OriginatingPlanTaskID,
			RootTaskIDs:           uniqueSortedStrings(request.RootTaskIDs),
			DescendantChanges:     changes,
			SourceCommit:          integrationHEAD,
			AffectedPaths:         affected,
			SourceSnapshotPaths:   snapshot,
		}
		refs := analysisRefs{
			spec: paths.NormalizeSpecRef(plan.SpecRef),
			plan: paths.NormalizeSpecRef(plan.PlanRef),
			arch: paths.NormalizeSpecRef(plan.ArchRef),
		}
		return metadata, refs, uniqueSortedStrings(request.RootTaskIDs), nil
	case models.IntegrationAnalysisPhaseGlobal:
		metadata := &models.IntegrationAnalysisMetadata{
			Key: request.Key, Phase: request.Phase, Generation: request.Generation, SourceCommit: request.SourceCommit,
		}
		parents, err := globalAnalysisParents(state, decision, request.Generation)
		if err != nil {
			return nil, analysisRefs{}, nil, err
		}
		return metadata, analysisRefs{spec: paths.NormalizeSpecRef(state.Goal.SpecRef)}, parents, nil
	default:
		return nil, analysisRefs{}, nil, fmt.Errorf("unsupported integration analysis phase %q", request.Phase)
	}
}

func sliceAnalysisSurface(
	state *models.State,
	rootTaskIDs []string,
	sourceCommit string,
	projectRoot string,
	gitWrapper *gitpkg.Git,
) ([]models.IntegrationDescendantChange, []string, []string, error) {
	evaluator, err := newIntegrationProgressEvaluator(state)
	if err != nil {
		return nil, nil, nil, err
	}
	descendantIDs := make([]string, 0)
	for _, rootID := range uniqueSortedStrings(rootTaskIDs) {
		leaves, leafErr := evaluator.mergedLineageLeaves(rootID)
		if leafErr != nil {
			return nil, nil, nil, leafErr
		}
		descendantIDs = append(descendantIDs, leaves...)
	}
	descendantIDs = uniqueSortedStrings(descendantIDs)
	changes := make([]models.IntegrationDescendantChange, 0, len(descendantIDs))
	affectedPaths := make([]string, 0)
	for _, taskID := range descendantIDs {
		task := state.FindTask(taskID)
		if task == nil || task.MergeCommit == nil || *task.MergeCommit == "" ||
			task.BaseCommit == nil || *task.BaseCommit == "" || task.ReviewCommit == nil || *task.ReviewCommit == "" {
			return nil, nil, nil, fmt.Errorf("merged descendant %q lacks reviewed change attribution", taskID)
		}
		changes = append(changes, models.IntegrationDescendantChange{TaskID: taskID, Commit: *task.MergeCommit})
		paths, diffErr := gitWrapper.DiffFiles(projectRoot, *task.BaseCommit, *task.ReviewCommit)
		if diffErr != nil {
			return nil, nil, nil, fmt.Errorf("diff descendant %q review range: %w", taskID, diffErr)
		}
		affectedPaths = append(affectedPaths, paths...)
	}
	affectedPaths = uniqueSortedStrings(affectedPaths)
	snapshotPaths := make([]string, 0, len(affectedPaths))
	for _, path := range affectedPaths {
		mode, present, modeErr := gitWrapper.TreePathMode(sourceCommit, path)
		if modeErr != nil {
			return nil, nil, nil, modeErr
		}
		if present && (mode == "100644" || mode == "100755") {
			snapshotPaths = append(snapshotPaths, path)
		}
	}
	return changes, affectedPaths, snapshotPaths, nil
}

func globalAnalysisParents(
	state *models.State,
	decision IntegrationProgressDecision,
	generation int,
) ([]string, error) {
	cohort := decision.ContributingSet
	if cohort == nil {
		return nil, fmt.Errorf("global integration request has no contributing set")
	}
	parents := make([]string, 0, len(cohort.Scopes)+1)
	if len(cohort.Scopes) < 2 {
		for _, scope := range cohort.Scopes {
			parents = append(parents, scope.RootTaskIDs...)
		}
	} else {
		coverageByPlan := make(map[string]IntegrationScopeCoverage, len(decision.Coverage))
		for _, coverage := range decision.Coverage {
			coverageByPlan[coverage.PlanTaskID] = coverage
		}
		for _, scope := range cohort.Scopes {
			coverage, ok := coverageByPlan[scope.PlanTaskID]
			if !ok || !coverage.Resolved {
				return nil, fmt.Errorf("global integration request lacks resolved coverage for plan %q", scope.PlanTaskID)
			}
			switch coverage.Kind {
			case models.IntegrationCoverageApprovalAttestation:
				attestations := cloneApprovalAttestations(coverage.ApprovalAttestations)
				sort.Slice(attestations, func(i, j int) bool { return attestations[i].ReviewedTaskID < attestations[j].ReviewedTaskID })
				if len(attestations) == 0 {
					return nil, fmt.Errorf("global integration request lacks approval witness for plan %q", scope.PlanTaskID)
				}
				parents = append(parents, attestations[0].ReviewedTaskID)
			case models.IntegrationCoverageSliceReport:
				record := integrationCoverageByPlan(state.Goal.Integration, scope.PlanTaskID)
				if record == nil || record.SliceReport == nil || record.SliceReport.AnalysisTaskID == "" {
					return nil, fmt.Errorf("global integration request lacks slice witness for plan %q", scope.PlanTaskID)
				}
				parents = append(parents, record.SliceReport.AnalysisTaskID)
			default:
				return nil, fmt.Errorf("global integration request has invalid coverage for plan %q", scope.PlanTaskID)
			}
		}
	}
	if generation > 1 {
		generations := state.Goal.Integration.GlobalGenerations
		if len(generations) < generation-1 {
			return nil, fmt.Errorf("global generation %d lacks generation %d provenance", generation, generation-1)
		}
		parents = append(parents, generations[generation-2].AnalysisTaskID)
	}
	return uniqueSortedStrings(parents), nil
}

func integrationCoverageByPlan(lifecycle *models.IntegrationLifecycle, planID string) *models.IntegrationCoverageRecord {
	if lifecycle == nil {
		return nil
	}
	for i := range lifecycle.Coverage {
		if lifecycle.Coverage[i].PlanTaskID == planID {
			return &lifecycle.Coverage[i]
		}
	}
	return nil
}

func projectNonCleanIntegrationClosure(
	lifecycle *models.IntegrationLifecycle,
	decision IntegrationProgressDecision,
	result *ReconcileIntegrationAnalysesResult,
) bool {
	if decision.Blocked == nil {
		if lifecycle.Closure != nil && lifecycle.Closure.Status != models.IntegrationClosureStatusClean {
			lifecycle.Closure = nil
			return true
		}
		return false
	}
	status := models.IntegrationClosureStatusBlocked
	if decision.Exhausted {
		status = models.IntegrationClosureStatusExhausted
	}
	want := &models.IntegrationClosure{Status: status, Reason: decision.Blocked.Code}
	reasonCopy := *decision.Blocked
	reasonCopy.TaskIDs = slices.Clone(decision.Blocked.TaskIDs)
	result.Reason = &reasonCopy
	if reflect.DeepEqual(lifecycle.Closure, want) {
		return false
	}
	lifecycle.Closure = want
	return true
}

func integrationAnalysisTaskID(key string) string {
	return "integration-" + strings.ReplaceAll(key, ":", "-")
}

func analysisRolePair(phase models.IntegrationAnalysisPhase) (string, error) {
	switch phase {
	case models.IntegrationAnalysisPhaseSlice:
		return sliceIntegrationRolePair, nil
	case models.IntegrationAnalysisPhaseGlobal:
		return globalIntegrationRolePair, nil
	default:
		return "", fmt.Errorf("unsupported integration analysis phase %q", phase)
	}
}

func analysisDescription(request IntegrationAnalysisRequest) string {
	if request.Phase == models.IntegrationAnalysisPhaseSlice {
		return fmt.Sprintf("Analyze integration composition for plan %s.", request.OriginatingPlanTaskID)
	}
	return fmt.Sprintf("Analyze global integration generation %d.", request.Generation)
}

func analysisDoneWhen(request IntegrationAnalysisRequest) string {
	if request.Phase == models.IntegrationAnalysisPhaseSlice {
		return "The slice analysis is reviewed and its immutable verdict is recorded."
	}
	return "The global integration analysis is reviewed and its immutable verdict is recorded."
}

func analysisScope(request IntegrationAnalysisRequest) string {
	if request.Phase == models.IntegrationAnalysisPhaseSlice {
		return fmt.Sprintf("Frozen integration slice for plan %s.", request.OriginatingPlanTaskID)
	}
	return fmt.Sprintf("Goal-wide integration generation %d.", request.Generation)
}
