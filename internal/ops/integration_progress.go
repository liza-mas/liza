package ops

import (
	"fmt"
	"slices"
	"sort"
	"strconv"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

const (
	integrationProgressWaitingPlanning       = "planning_unsettled"
	integrationProgressWaitingSliceCoverage  = "slice_coverage_pending"
	integrationProgressWaitingCoding         = "coding_work_pending"
	integrationProgressWaitingRepairs        = "integration_repairs_pending"
	integrationProgressWaitingGlobalAnalysis = "global_analysis_pending"
	integrationProgressWaitingClosure        = "closure_projection_pending"

	integrationProgressBlockedRepair     = "integration_repair_blocked"
	integrationProgressBlockedAnalysis   = "integration_analysis_blocked"
	integrationProgressBlockedExhausted  = "global_generations_exhausted"
	integrationProgressBlockedCapability = pipeline.SlicedIntegrationUpgradeRequired
)

// IntegrationProgressDecision is the pure projection consumed by integration
// reconciliation and completion gates.
type IntegrationProgressDecision struct {
	PlanningSettled       bool
	FreezeContributingSet bool
	ContributingSet       *models.IntegrationContributingSet
	Coverage              []IntegrationScopeCoverage
	SliceRequests         []IntegrationAnalysisRequest
	GlobalReady           bool
	GlobalRequest         *IntegrationAnalysisRequest
	IntegrationComplete   bool
	Exhausted             bool
	Waiting               *IntegrationProgressReason
	Blocked               *IntegrationProgressReason
}

// IntegrationScopeCoverage projects the bounded evidence required for one
// frozen contributing scope.
type IntegrationScopeCoverage struct {
	PlanTaskID           string
	RootTaskIDs          []string
	Kind                 models.IntegrationCoverageKind
	AnalysisKey          string
	ApprovalAttestations []models.IntegrationApprovalAttestation
	Effective            bool
	Resolved             bool
}

// IntegrationAnalysisRequest identifies one missing immutable analysis.
type IntegrationAnalysisRequest struct {
	Key                   string
	Phase                 models.IntegrationAnalysisPhase
	Generation            int
	OriginatingPlanTaskID string
	RootTaskIDs           []string
	SourceCommit          string
}

// IntegrationProgressReason is a stable machine-readable wait or stop reason.
type IntegrationProgressReason struct {
	Code     string
	TaskIDs  []string
	Guidance string
}

// EvaluateIntegrationProgress derives integration lifecycle progress without
// mutating state, creating tasks, reading prompts, or inspecting Git.
func EvaluateIntegrationProgress(
	state *models.State,
	capability pipeline.SlicedIntegrationCapability,
	integrationHEAD string,
) (IntegrationProgressDecision, error) {
	evaluator, err := newIntegrationProgressEvaluator(state)
	if err != nil {
		return IntegrationProgressDecision{}, err
	}
	return evaluator.evaluate(capability, integrationHEAD)
}

type integrationProgressEvaluator struct {
	state        *models.State
	tasks        map[string]*models.Task
	children     map[string][]string
	parents      map[string][]string
	analysisKeys map[string]*models.Task
}

func newIntegrationProgressEvaluator(state *models.State) (*integrationProgressEvaluator, error) {
	if state == nil {
		return nil, fmt.Errorf("integration progress: state is nil")
	}
	evaluator := &integrationProgressEvaluator{
		state:        state,
		tasks:        make(map[string]*models.Task, len(state.Tasks)),
		children:     make(map[string][]string),
		parents:      make(map[string][]string),
		analysisKeys: make(map[string]*models.Task),
	}
	tasks := make([]*models.Task, 0, len(state.Tasks))
	for i := range state.Tasks {
		tasks = append(tasks, &state.Tasks[i])
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	for _, task := range tasks {
		if task.ID == "" {
			return nil, fmt.Errorf("integration progress: task has empty id")
		}
		if _, exists := evaluator.tasks[task.ID]; exists {
			return nil, fmt.Errorf("integration progress: duplicate task id %q", task.ID)
		}
		evaluator.tasks[task.ID] = task
		if task.IntegrationAnalysis != nil {
			key := task.IntegrationAnalysis.Key
			if key == "" {
				return nil, fmt.Errorf("integration progress: analysis task %q has empty key", task.ID)
			}
			if existing := evaluator.analysisKeys[key]; existing != nil {
				return nil, fmt.Errorf("integration progress: analysis key %q is reused by %q and %q", key, existing.ID, task.ID)
			}
			evaluator.analysisKeys[key] = task
		}
	}
	for _, task := range tasks {
		parents := append([]string(nil), task.EffectiveParentTasks()...)
		if task.Supersedes != nil && *task.Supersedes != "" {
			parents = append(parents, *task.Supersedes)
		}
		for _, parentID := range uniqueSortedStrings(parents) {
			if evaluator.tasks[parentID] == nil {
				return nil, fmt.Errorf("integration progress: task %q references missing parent %q", task.ID, parentID)
			}
			evaluator.addEdge(parentID, task.ID)
		}
		for _, replacementID := range uniqueSortedStrings(task.SupersededBy) {
			if evaluator.tasks[replacementID] == nil {
				return nil, fmt.Errorf("integration progress: task %q references missing replacement %q", task.ID, replacementID)
			}
			evaluator.addEdge(task.ID, replacementID)
		}
	}
	for parentID := range evaluator.children {
		evaluator.children[parentID] = uniqueSortedStrings(evaluator.children[parentID])
	}
	for childID := range evaluator.parents {
		evaluator.parents[childID] = uniqueSortedStrings(evaluator.parents[childID])
	}
	if err := evaluator.validateGraphAcyclic(); err != nil {
		return nil, err
	}
	return evaluator, nil
}

func (e *integrationProgressEvaluator) addEdge(parentID, childID string) {
	e.children[parentID] = append(e.children[parentID], childID)
	e.parents[childID] = append(e.parents[childID], parentID)
}

func (e *integrationProgressEvaluator) validateGraphAcyclic() error {
	visiting := make(map[string]bool)
	visited := make(map[string]bool)
	var walk func(string) error
	walk = func(taskID string) error {
		if visiting[taskID] {
			return fmt.Errorf("integration progress: ancestry cycle includes %q", taskID)
		}
		if visited[taskID] {
			return nil
		}
		visiting[taskID] = true
		defer delete(visiting, taskID)
		for _, childID := range e.children[taskID] {
			if err := walk(childID); err != nil {
				return err
			}
		}
		visited[taskID] = true
		return nil
	}
	for _, taskID := range e.sortedTaskIDs() {
		if err := walk(taskID); err != nil {
			return err
		}
	}
	return nil
}

func (e *integrationProgressEvaluator) sortedTaskIDs() []string {
	ids := make([]string, 0, len(e.tasks))
	for taskID := range e.tasks {
		ids = append(ids, taskID)
	}
	sort.Strings(ids)
	return ids
}

func (e *integrationProgressEvaluator) evaluate(
	capability pipeline.SlicedIntegrationCapability,
	integrationHEAD string,
) (IntegrationProgressDecision, error) {
	decision := IntegrationProgressDecision{}
	lifecycle := e.state.Goal.Integration
	if lifecycle == nil {
		lifecycle = &models.IntegrationLifecycle{}
	}

	cohort, freeze, settled, err := e.contributingSet(lifecycle.ContributingSet)
	if err != nil {
		return decision, err
	}
	decision.PlanningSettled = settled
	decision.FreezeContributingSet = freeze
	decision.ContributingSet = cloneIntegrationContributingSet(cohort)
	if !settled {
		decision.Waiting = progressReason(integrationProgressWaitingPlanning)
		return decision, nil
	}

	coverageReady, err := e.evaluateCoverage(&decision, lifecycle, capability)
	if err != nil {
		return IntegrationProgressDecision{}, err
	}
	if decision.Blocked != nil || !coverageReady {
		return decision, nil
	}

	codingReady, err := e.evaluateCodingBarrier(&decision)
	if err != nil {
		return IntegrationProgressDecision{}, err
	}
	if decision.Blocked != nil || !codingReady {
		return decision, nil
	}

	return e.evaluateGlobal(decision, lifecycle, integrationHEAD)
}

func (e *integrationProgressEvaluator) contributingSet(
	persisted *models.IntegrationContributingSet,
) (*models.IntegrationContributingSet, bool, bool, error) {
	if persisted != nil {
		cohort := cloneIntegrationContributingSet(persisted)
		if err := e.validateCohort(cohort); err != nil {
			return nil, false, false, err
		}
		return cohort, false, true, nil
	}

	plans, err := e.preIntegrationPlans()
	if err != nil {
		return nil, false, false, err
	}
	cohort := &models.IntegrationContributingSet{Scopes: []models.IntegrationScopeSnapshot{}}
	for _, plan := range plans {
		resolution, err := e.resolveLineage(plan.ID, false)
		if err != nil {
			return nil, false, false, err
		}
		if !resolution.settled {
			return nil, false, false, nil
		}
		if plan.Status == models.TaskStatusMerged && len(plan.Output) > 0 && !hasExecutedOutputTransition(plan.TransitionsExecuted) {
			return nil, false, false, nil
		}

		roots := e.directCodingRoots(plan.ID)
		mergedRoots := make([]string, 0, len(roots))
		for _, rootID := range roots {
			rootResolution, err := e.resolveLineage(rootID, false)
			if err != nil {
				return nil, false, false, err
			}
			if !rootResolution.settled {
				return nil, false, false, nil
			}
			if rootResolution.merged {
				mergedRoots = append(mergedRoots, rootID)
			}
		}
		if len(mergedRoots) > 0 {
			cohort.Scopes = append(cohort.Scopes, models.IntegrationScopeSnapshot{
				PlanTaskID: plan.ID, RootTaskIDs: uniqueSortedStrings(mergedRoots),
			})
		}
	}
	sort.Slice(cohort.Scopes, func(i, j int) bool { return cohort.Scopes[i].PlanTaskID < cohort.Scopes[j].PlanTaskID })
	return cohort, true, true, nil
}

func (e *integrationProgressEvaluator) preIntegrationPlans() ([]*models.Task, error) {
	plans := make([]*models.Task, 0)
	for i := range e.state.Tasks {
		task := &e.state.Tasks[i]
		if task.RolePair != "code-planning-pair" {
			continue
		}
		repair, err := e.hasAnalysisAncestor(task.ID)
		if err != nil {
			return nil, err
		}
		if !repair {
			plans = append(plans, task)
		}
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].ID < plans[j].ID })
	return plans, nil
}

func (e *integrationProgressEvaluator) hasAnalysisAncestor(taskID string) (bool, error) {
	visiting := make(map[string]bool)
	visited := make(map[string]bool)
	var walk func(string) (bool, error)
	walk = func(current string) (bool, error) {
		if visiting[current] {
			return false, fmt.Errorf("integration progress: ancestry cycle includes %q", current)
		}
		if visited[current] {
			return false, nil
		}
		visiting[current] = true
		defer delete(visiting, current)
		for _, parentID := range e.parents[current] {
			parent := e.tasks[parentID]
			if parent.IntegrationAnalysis != nil {
				return true, nil
			}
			found, err := walk(parentID)
			if err != nil || found {
				return found, err
			}
		}
		visited[current] = true
		return false, nil
	}
	return walk(taskID)
}

func (e *integrationProgressEvaluator) directCodingRoots(planID string) []string {
	roots := make([]string, 0)
	for i := range e.state.Tasks {
		task := &e.state.Tasks[i]
		if task.RolePair == "coding-pair" && slices.Contains(task.EffectiveParentTasks(), planID) {
			roots = append(roots, task.ID)
		}
	}
	return uniqueSortedStrings(roots)
}

func (e *integrationProgressEvaluator) validateCohort(cohort *models.IntegrationContributingSet) error {
	seenPlans := make(map[string]bool)
	seenRoots := make(map[string]bool)
	for i := range cohort.Scopes {
		scope := &cohort.Scopes[i]
		if scope.PlanTaskID == "" || seenPlans[scope.PlanTaskID] {
			return fmt.Errorf("integration progress: invalid or duplicate contributing plan %q", scope.PlanTaskID)
		}
		if e.tasks[scope.PlanTaskID] == nil {
			return fmt.Errorf("integration progress: contributing plan %q is missing", scope.PlanTaskID)
		}
		seenPlans[scope.PlanTaskID] = true
		scope.RootTaskIDs = uniqueSortedStrings(scope.RootTaskIDs)
		if len(scope.RootTaskIDs) == 0 {
			return fmt.Errorf("integration progress: contributing plan %q has no roots", scope.PlanTaskID)
		}
		for _, rootID := range scope.RootTaskIDs {
			if e.tasks[rootID] == nil || seenRoots[rootID] {
				return fmt.Errorf("integration progress: invalid or reused contributing root %q", rootID)
			}
			seenRoots[rootID] = true
		}
	}
	sort.Slice(cohort.Scopes, func(i, j int) bool { return cohort.Scopes[i].PlanTaskID < cohort.Scopes[j].PlanTaskID })
	return nil
}

func (e *integrationProgressEvaluator) evaluateCoverage(
	decision *IntegrationProgressDecision,
	lifecycle *models.IntegrationLifecycle,
	capability pipeline.SlicedIntegrationCapability,
) (bool, error) {
	if decision.ContributingSet == nil || len(decision.ContributingSet.Scopes) < 2 {
		return true, nil
	}
	records := make(map[string]*models.IntegrationCoverageRecord, len(lifecycle.Coverage))
	for i := range lifecycle.Coverage {
		record := &lifecycle.Coverage[i]
		if records[record.PlanTaskID] != nil {
			return false, fmt.Errorf("integration progress: duplicate coverage for plan %q", record.PlanTaskID)
		}
		records[record.PlanTaskID] = record
	}
	cohortPlans := make(map[string]bool, len(decision.ContributingSet.Scopes))
	allReady := true
	for _, scope := range decision.ContributingSet.Scopes {
		cohortPlans[scope.PlanTaskID] = true
		coverage := IntegrationScopeCoverage{
			PlanTaskID: scope.PlanTaskID, RootTaskIDs: append([]string(nil), scope.RootTaskIDs...),
		}
		record := records[scope.PlanTaskID]
		if len(scope.RootTaskIDs) == 1 {
			attestations, err := e.approvalAttestations(scope.RootTaskIDs[0], record)
			if err != nil {
				return false, err
			}
			coverage.Kind = models.IntegrationCoverageApprovalAttestation
			coverage.ApprovalAttestations = attestations
			coverage.Effective = true
			coverage.Resolved = true
			decision.Coverage = append(decision.Coverage, coverage)
			continue
		}

		key := sliceAnalysisKey(scope.PlanTaskID)
		coverage.Kind = models.IntegrationCoverageSliceReport
		coverage.AnalysisKey = key
		if record == nil {
			allReady = false
			if analysis := e.analysisKeys[key]; analysis != nil {
				if analysis.Status == models.TaskStatusBlocked || analysis.Status == models.TaskStatusAbandoned {
					decision.Blocked = progressReasonWithTasks(integrationProgressBlockedAnalysis, analysis.ID)
				}
			} else {
				decision.SliceRequests = append(decision.SliceRequests, IntegrationAnalysisRequest{
					Key: key, Phase: models.IntegrationAnalysisPhaseSlice,
					OriginatingPlanTaskID: scope.PlanTaskID,
					RootTaskIDs:           append([]string(nil), scope.RootTaskIDs...),
				})
			}
			decision.Coverage = append(decision.Coverage, coverage)
			continue
		}
		if record.Kind != models.IntegrationCoverageSliceReport || record.SliceReport == nil {
			return false, fmt.Errorf("integration progress: plan %q requires slice coverage", scope.PlanTaskID)
		}
		if err := e.validateSliceReport(scope, record.SliceReport, key); err != nil {
			return false, err
		}
		coverage.Effective = true
		switch record.SliceReport.Verdict {
		case models.IntegrationAnalysisVerdictClean:
			coverage.Resolved = true
		case models.IntegrationAnalysisVerdictFindings:
			resolution, err := e.resolveAnalysisRepairs(record.SliceReport.AnalysisTaskID)
			if err != nil {
				return false, err
			}
			if resolution.blocked {
				decision.Blocked = progressReasonWithTasks(integrationProgressBlockedRepair, resolution.taskIDs...)
				allReady = false
			} else if !resolution.settled {
				decision.Waiting = progressReasonWithTasks(integrationProgressWaitingRepairs, resolution.taskIDs...)
				allReady = false
			} else {
				coverage.Resolved = true
			}
		default:
			return false, fmt.Errorf("integration progress: slice %q has invalid verdict %q", key, record.SliceReport.Verdict)
		}
		decision.Coverage = append(decision.Coverage, coverage)
	}
	extraCoveragePlans := make([]string, 0)
	for planID := range records {
		if !cohortPlans[planID] {
			extraCoveragePlans = append(extraCoveragePlans, planID)
		}
	}
	if len(extraCoveragePlans) > 0 {
		sort.Strings(extraCoveragePlans)
		return false, fmt.Errorf("integration progress: coverage references non-contributing plan %q", extraCoveragePlans[0])
	}
	if len(decision.SliceRequests) > 0 && !capability.Available {
		code := capability.Code
		if code == "" {
			code = integrationProgressBlockedCapability
		}
		decision.Blocked = &IntegrationProgressReason{Code: code, Guidance: capability.Guidance}
		return false, nil
	}
	if !allReady && decision.Blocked == nil && decision.Waiting == nil {
		ids := make([]string, 0, len(decision.SliceRequests))
		for _, request := range decision.SliceRequests {
			ids = append(ids, request.Key)
		}
		decision.Waiting = progressReasonWithTasks(integrationProgressWaitingSliceCoverage, ids...)
	}
	return allReady, nil
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
		task := e.tasks[leafID]
		if task.ReviewCommit == nil || *task.ReviewCommit == "" || task.ApprovedBy == nil || *task.ApprovedBy == "" || task.MergeCommit == nil || *task.MergeCommit == "" || task.DoneWhen == "" {
			return nil, fmt.Errorf("integration progress: merged task %q lacks approval attestation facts", task.ID)
		}
		attestations = append(attestations, models.IntegrationApprovalAttestation{
			ReviewedTaskID:     task.ID,
			AcceptanceCriteria: task.DoneWhen,
			ReviewedCommit:     *task.ReviewCommit,
			Approver:           *task.ApprovedBy,
			Validation:         append([]string(nil), task.Validation...),
			MergeCommit:        *task.MergeCommit,
		})
	}
	return attestations, nil
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

func (e *integrationProgressEvaluator) evaluateCodingBarrier(decision *IntegrationProgressDecision) (bool, error) {
	pending := make([]string, 0)
	repairPending := make([]string, 0)
	blocked := make([]string, 0)
	for i := range e.state.Tasks {
		task := &e.state.Tasks[i]
		if task.IntegrationAnalysis != nil {
			continue
		}
		repair, err := e.hasAnalysisAncestor(task.ID)
		if err != nil {
			return false, err
		}
		if task.RolePair != "coding-pair" && !(repair && task.RolePair == "code-planning-pair") {
			continue
		}
		resolution, err := e.resolveLineage(task.ID, repair)
		if err != nil {
			return false, err
		}
		if resolution.blocked {
			blocked = append(blocked, resolution.taskIDs...)
		} else if !resolution.settled {
			if repair {
				repairPending = append(repairPending, resolution.taskIDs...)
			} else {
				pending = append(pending, resolution.taskIDs...)
			}
		}
	}
	if len(blocked) > 0 {
		decision.Blocked = progressReasonWithTasks(integrationProgressBlockedRepair, blocked...)
		return false, nil
	}
	if len(repairPending) > 0 {
		decision.Waiting = progressReasonWithTasks(integrationProgressWaitingRepairs, repairPending...)
		return false, nil
	}
	if len(pending) > 0 {
		decision.Waiting = progressReasonWithTasks(integrationProgressWaitingCoding, pending...)
		return false, nil
	}
	return true, nil
}

func (e *integrationProgressEvaluator) evaluateGlobal(
	decision IntegrationProgressDecision,
	lifecycle *models.IntegrationLifecycle,
	integrationHEAD string,
) (IntegrationProgressDecision, error) {
	if err := e.validateGlobalGenerations(lifecycle.GlobalGenerations); err != nil {
		return IntegrationProgressDecision{}, err
	}
	if len(lifecycle.GlobalGenerations) > 0 {
		latest := lifecycle.GlobalGenerations[len(lifecycle.GlobalGenerations)-1]
		if latest.Verdict == models.IntegrationAnalysisVerdictFindings {
			resolution, err := e.resolveAnalysisRepairs(latest.AnalysisTaskID)
			if err != nil {
				return IntegrationProgressDecision{}, err
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

	decision.GlobalReady = true
	if len(lifecycle.GlobalGenerations) > 0 {
		latest := lifecycle.GlobalGenerations[len(lifecycle.GlobalGenerations)-1]
		if latest.Verdict == models.IntegrationAnalysisVerdictClean && latest.SourceCommit == integrationHEAD {
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
	nextGeneration := len(lifecycle.GlobalGenerations) + 1
	if nextGeneration > limit {
		decision.Exhausted = true
		decision.Blocked = &IntegrationProgressReason{
			Code:     integrationProgressBlockedExhausted,
			Guidance: fmt.Sprintf("global integration generation limit %d reached", limit),
		}
		return decision, nil
	}
	if integrationHEAD == "" {
		return IntegrationProgressDecision{}, fmt.Errorf("integration progress: live integration HEAD is empty for global generation %d", nextGeneration)
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
	decision.GlobalRequest = &IntegrationAnalysisRequest{
		Key: key, Phase: models.IntegrationAnalysisPhaseGlobal,
		Generation: nextGeneration, SourceCommit: integrationHEAD,
	}
	return decision, nil
}

func (e *integrationProgressEvaluator) validateGlobalGenerations(generations []models.IntegrationGlobalGeneration) error {
	for i, generation := range generations {
		wantGeneration := i + 1
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

type lineageResolution struct {
	settled bool
	merged  bool
	blocked bool
	taskIDs []string
}

func (e *integrationProgressEvaluator) resolveLineage(rootID string, abandonedBlocks bool) (lineageResolution, error) {
	return e.resolveLineageWithStack(rootID, abandonedBlocks, make(map[string]bool))
}

func (e *integrationProgressEvaluator) resolveLineageWithStack(
	taskID string,
	abandonedBlocks bool,
	visiting map[string]bool,
) (lineageResolution, error) {
	if visiting[taskID] {
		return lineageResolution{}, fmt.Errorf("integration progress: replacement cycle includes %q", taskID)
	}
	task := e.tasks[taskID]
	if task == nil {
		return lineageResolution{}, fmt.Errorf("integration progress: replacement task %q is missing", taskID)
	}
	switch task.Status {
	case models.TaskStatusMerged:
		return lineageResolution{settled: true, merged: true}, nil
	case models.TaskStatusAbandoned:
		if abandonedBlocks {
			return lineageResolution{settled: true, blocked: true, taskIDs: []string{taskID}}, nil
		}
		return lineageResolution{settled: true}, nil
	case models.TaskStatusBlocked:
		if abandonedBlocks {
			return lineageResolution{settled: true, blocked: true, taskIDs: []string{taskID}}, nil
		}
		return lineageResolution{taskIDs: []string{taskID}}, nil
	case models.TaskStatusSuperseded:
		if len(task.SupersededBy) == 0 {
			return lineageResolution{}, fmt.Errorf("integration progress: superseded task %q has no replacements", taskID)
		}
		visiting[taskID] = true
		defer delete(visiting, taskID)
		combined := lineageResolution{settled: true}
		for _, replacementID := range uniqueSortedStrings(task.SupersededBy) {
			resolution, err := e.resolveLineageWithStack(replacementID, abandonedBlocks, visiting)
			if err != nil {
				return lineageResolution{}, err
			}
			combined.settled = combined.settled && resolution.settled
			combined.merged = combined.merged || resolution.merged
			combined.blocked = combined.blocked || resolution.blocked
			combined.taskIDs = append(combined.taskIDs, resolution.taskIDs...)
		}
		combined.taskIDs = uniqueSortedStrings(combined.taskIDs)
		return combined, nil
	default:
		return lineageResolution{taskIDs: []string{taskID}}, nil
	}
}

func (e *integrationProgressEvaluator) resolveAnalysisRepairs(analysisTaskID string) (lineageResolution, error) {
	children := make([]string, 0)
	for _, childID := range e.children[analysisTaskID] {
		if e.tasks[childID].IntegrationAnalysis == nil {
			children = append(children, childID)
		}
	}
	children = uniqueSortedStrings(children)
	if len(children) == 0 {
		return lineageResolution{taskIDs: []string{analysisTaskID}}, nil
	}

	leaves, err := e.repairLeaves(children)
	if err != nil {
		return lineageResolution{}, err
	}
	result := lineageResolution{settled: true, merged: true}
	for _, leafID := range leaves {
		leaf := e.tasks[leafID]
		if leaf.RolePair == "code-planning-pair" && len(leaf.Output) > 0 {
			result.settled = false
			result.merged = false
			result.taskIDs = append(result.taskIDs, leafID)
			continue
		}
		switch leaf.Status {
		case models.TaskStatusMerged:
		case models.TaskStatusBlocked, models.TaskStatusAbandoned:
			result.blocked = true
			result.taskIDs = append(result.taskIDs, leafID)
		default:
			result.settled = false
			result.merged = false
			result.taskIDs = append(result.taskIDs, leafID)
		}
	}
	result.taskIDs = uniqueSortedStrings(result.taskIDs)
	return result, nil
}

func (e *integrationProgressEvaluator) repairLeaves(roots []string) ([]string, error) {
	leaves := make([]string, 0)
	visiting := make(map[string]bool)
	visited := make(map[string]bool)
	var walk func(string) error
	walk = func(taskID string) error {
		if visiting[taskID] {
			return fmt.Errorf("integration progress: repair lineage cycle includes %q", taskID)
		}
		if visited[taskID] {
			return nil
		}
		visiting[taskID] = true
		defer delete(visiting, taskID)
		children := make([]string, 0)
		for _, childID := range e.children[taskID] {
			if e.tasks[childID].IntegrationAnalysis == nil {
				children = append(children, childID)
			}
		}
		children = uniqueSortedStrings(children)
		if len(children) == 0 {
			if task := e.tasks[taskID]; task.Status == models.TaskStatusSuperseded {
				return fmt.Errorf("integration progress: superseded repair %q has no replacement leaves", taskID)
			}
			leaves = append(leaves, taskID)
		} else {
			for _, childID := range children {
				if err := walk(childID); err != nil {
					return err
				}
			}
		}
		visited[taskID] = true
		return nil
	}
	for _, rootID := range roots {
		if err := walk(rootID); err != nil {
			return nil, err
		}
	}
	return uniqueSortedStrings(leaves), nil
}

func (e *integrationProgressEvaluator) mergedLineageLeaves(rootID string) ([]string, error) {
	leaves := make([]string, 0)
	var walk func(string, map[string]bool) error
	walk = func(taskID string, visiting map[string]bool) error {
		if visiting[taskID] {
			return fmt.Errorf("integration progress: replacement cycle includes %q", taskID)
		}
		task := e.tasks[taskID]
		if task == nil {
			return fmt.Errorf("integration progress: replacement task %q is missing", taskID)
		}
		if task.Status != models.TaskStatusSuperseded {
			if task.Status == models.TaskStatusMerged {
				leaves = append(leaves, taskID)
			}
			return nil
		}
		if len(task.SupersededBy) == 0 {
			return fmt.Errorf("integration progress: superseded task %q has no replacements", taskID)
		}
		visiting[taskID] = true
		defer delete(visiting, taskID)
		for _, replacementID := range uniqueSortedStrings(task.SupersededBy) {
			if err := walk(replacementID, visiting); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(rootID, make(map[string]bool)); err != nil {
		return nil, err
	}
	return uniqueSortedStrings(leaves), nil
}

func (e *integrationProgressEvaluator) lineageContains(rootID, candidateID string) bool {
	if rootID == candidateID {
		return true
	}
	visited := make(map[string]bool)
	queue := []string{rootID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if visited[current] {
			continue
		}
		visited[current] = true
		for _, childID := range e.children[current] {
			if !slices.Contains(e.tasks[current].SupersededBy, childID) {
				continue
			}
			if childID == candidateID {
				return true
			}
			queue = append(queue, childID)
		}
	}
	return false
}

func hasExecutedOutputTransition(transitions map[string]bool) bool {
	for name, executed := range transitions {
		if executed && name != "replanned" {
			return true
		}
	}
	return false
}

func sliceAnalysisKey(planTaskID string) string {
	return "slice:" + planTaskID
}

func globalAnalysisKey(generation int) string {
	return "global:" + strconv.Itoa(generation)
}

func progressReason(code string) *IntegrationProgressReason {
	return &IntegrationProgressReason{Code: code}
}

func progressReasonWithTasks(code string, taskIDs ...string) *IntegrationProgressReason {
	return &IntegrationProgressReason{Code: code, TaskIDs: uniqueSortedStrings(taskIDs)}
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := append([]string(nil), values...)
	sort.Strings(result)
	result = slices.Compact(result)
	return result
}

func equalStringSets(left, right []string) bool {
	return slices.Equal(uniqueSortedStrings(left), uniqueSortedStrings(right))
}

func cloneIntegrationContributingSet(set *models.IntegrationContributingSet) *models.IntegrationContributingSet {
	if set == nil {
		return nil
	}
	clone := &models.IntegrationContributingSet{Scopes: make([]models.IntegrationScopeSnapshot, len(set.Scopes))}
	copy(clone.Scopes, set.Scopes)
	for i := range clone.Scopes {
		clone.Scopes[i].RootTaskIDs = append([]string(nil), set.Scopes[i].RootTaskIDs...)
	}
	return clone
}

func cloneApprovalAttestations(attestations []models.IntegrationApprovalAttestation) []models.IntegrationApprovalAttestation {
	if attestations == nil {
		return nil
	}
	clones := make([]models.IntegrationApprovalAttestation, len(attestations))
	copy(clones, attestations)
	for i := range clones {
		clones[i].Validation = append([]string(nil), attestations[i].Validation...)
	}
	return clones
}
