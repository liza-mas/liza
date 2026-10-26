package ops

import (
	"fmt"
	"slices"
	"sort"
	"strconv"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// integrationProgressCode is the finite set of wait and block reasons the
// evaluator emits. models.IntegrationProgressReason.Code stays a string;
// constants convert where assigned.
type integrationProgressCode string

const (
	integrationProgressWaitingPlanning       integrationProgressCode = "planning_unsettled"
	integrationProgressWaitingSliceCoverage  integrationProgressCode = "slice_coverage_pending"
	integrationProgressWaitingCoding         integrationProgressCode = "coding_work_pending"
	integrationProgressWaitingRepairs        integrationProgressCode = "integration_repairs_pending"
	integrationProgressWaitingGlobalAnalysis integrationProgressCode = "global_analysis_pending"
	integrationProgressWaitingClosure        integrationProgressCode = "closure_projection_pending"

	integrationProgressBlockedRepair     integrationProgressCode = "integration_repair_blocked"
	integrationProgressBlockedAnalysis   integrationProgressCode = "integration_analysis_blocked"
	integrationProgressBlockedExhausted  integrationProgressCode = "global_generations_exhausted"
	integrationProgressBlockedCapability integrationProgressCode = pipeline.SlicedIntegrationUpgradeRequired
)

// slicedCohortMinScopes is how many scopes a contributing set needs before its
// scopes are covered by slice evidence: a smaller set needs no coverage, and
// its global analysis takes the scopes' root tasks as parents.
const slicedCohortMinScopes = 2

// EvaluateIntegrationProgress derives integration lifecycle progress without
// mutating state, creating tasks, reading prompts, or inspecting Git.
func EvaluateIntegrationProgress(
	state *models.State,
	capability pipeline.SlicedIntegrationCapability,
	integrationHEAD string,
) (models.IntegrationProgressDecision, error) {
	evaluator, err := newIntegrationProgressEvaluator(state)
	if err != nil {
		return models.IntegrationProgressDecision{}, err
	}
	return evaluator.evaluate(capability, integrationHEAD)
}

// UnsettledPreIntegrationPlanningTasks returns sorted upstream planning task IDs
// that can still produce coding work. It ignores persisted cohort state and
// integration repair ancestry, so recovery can inspect a premature freeze.
// A settled graph returns nil; malformed task graphs return an error.
func UnsettledPreIntegrationPlanningTasks(state *models.State, capability pipeline.SlicedIntegrationCapability) ([]string, error) {
	evaluator, err := newIntegrationProgressEvaluator(state)
	if err != nil {
		return nil, err
	}
	return evaluator.unsettledPreIntegrationPlanningTasks(capability)
}

// EvaluateLiveIntegrationProgress loads the frozen pipeline capability and
// current integration branch HEAD before evaluating integration progress.
func EvaluateLiveIntegrationProgress(state *models.State, projectRoot string) (models.IntegrationProgressDecision, error) {
	cfg, err := pipeline.LoadFrozen(projectRoot)
	if err != nil {
		return models.IntegrationProgressDecision{}, fmt.Errorf("load frozen pipeline: %w", err)
	}
	capability, err := pipeline.NewResolver(cfg).SlicedIntegrationCapability()
	if err != nil {
		return models.IntegrationProgressDecision{}, fmt.Errorf("resolve sliced integration capability: %w", err)
	}
	integrationHEAD, err := ResolveIntegrationHEAD(projectRoot, state.Config.IntegrationBranch)
	if err != nil {
		return models.IntegrationProgressDecision{}, fmt.Errorf("read integration HEAD: %w", err)
	}
	return EvaluateIntegrationProgress(state, capability, integrationHEAD)
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
) (models.IntegrationProgressDecision, error) {
	decision := models.IntegrationProgressDecision{}
	lifecycle := e.state.Goal.Integration
	if lifecycle == nil {
		lifecycle = &models.IntegrationLifecycle{}
	}

	cohort, freeze, holding, err := e.contributingSet(
		lifecycle.ContributingSet,
		capability,
	)
	if err != nil {
		return decision, err
	}
	decision.PlanningSettled = len(holding) == 0
	decision.FreezeContributingSet = freeze
	decision.ContributingSet = cloneIntegrationContributingSet(cohort)
	if !decision.PlanningSettled {
		decision.Waiting = progressReasonWithTasks(integrationProgressWaitingPlanning, holding...)
		decision.Waiting.Guidance = planningUnsettledGuidance()
		return decision, nil
	}

	coverageReady, err := e.evaluateCoverage(&decision, lifecycle, capability)
	if err != nil {
		return models.IntegrationProgressDecision{}, err
	}
	if decision.Blocked != nil || !coverageReady {
		return decision, nil
	}

	codingReady, err := e.evaluateCodingBarrier(&decision)
	if err != nil {
		return models.IntegrationProgressDecision{}, err
	}
	if decision.Blocked != nil || !codingReady {
		return decision, nil
	}

	return e.evaluateGlobal(decision, lifecycle, integrationHEAD)
}

// contributingSet returns the integration cohort once planning has settled,
// or the task IDs still holding it.
func (e *integrationProgressEvaluator) contributingSet(
	persisted *models.IntegrationContributingSet,
	capability pipeline.SlicedIntegrationCapability,
) (*models.IntegrationContributingSet, bool, []string, error) {
	pending, err := e.unsettledPreIntegrationPlanningTasks(capability)
	if err != nil {
		return nil, false, nil, err
	}
	if len(pending) > 0 {
		return nil, false, pending, nil
	}

	if persisted != nil {
		cohort := cloneIntegrationContributingSet(persisted)
		if err := e.validateCohort(cohort); err != nil {
			return nil, false, nil, err
		}
		return cohort, false, nil, nil
	}

	plans, err := e.preIntegrationPlans()
	if err != nil {
		return nil, false, nil, err
	}
	cohort := &models.IntegrationContributingSet{Scopes: []models.IntegrationScopeSnapshot{}}
	for _, plan := range plans {
		if plan.PlanHandoffRetired() {
			continue
		}
		resolution, err := e.resolveLineage(plan.ID, false)
		if err != nil {
			return nil, false, nil, err
		}
		if !resolution.settled {
			return nil, false, []string{plan.ID}, nil
		}
		if plan.Status == models.TaskStatusMerged && len(plan.Output) > 0 && !hasExecutedOutputTransition(plan.TransitionsExecuted) {
			return nil, false, []string{plan.ID}, nil
		}

		roots := e.directCodingRoots(plan.ID)
		mergedRoots := make([]string, 0, len(roots))
		for _, rootID := range roots {
			rootResolution, err := e.resolveLineage(rootID, false)
			if err != nil {
				return nil, false, nil, err
			}
			if !rootResolution.settled {
				return nil, false, []string{rootID}, nil
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
	return cohort, true, nil, nil
}

// planningUnsettledGuidance names the ways out of a planning wait, including
// the D-225 lever for a source that can never run its transition.
func planningUnsettledGuidance() string {
	return "listed tasks must finish and run their outgoing transitions; for a merged, outputless source that can never run its transition and whose downstream work merged, an operator runs " +
		brand.Command("plan-check", "TASK", "--retire", "--ruling", "DECISION")
}

func (e *integrationProgressEvaluator) unsettledPreIntegrationPlanningTasks(capability pipeline.SlicedIntegrationCapability) ([]string, error) {
	var pending []string
	for i := range e.state.Tasks {
		task := &e.state.Tasks[i]
		transitions, upstream := capability.PreIntegrationPlanningTransitions[task.RolePair]
		if task.PlanHandoffRetired() {
			continue
		}
		if capability.PreIntegrationPlanningTransitions == nil {
			// Preserve the legacy pure evaluator input when no topology is supplied.
			upstream = task.RolePair == codePlanningRolePair ||
				(capability.PreIntegrationDecompositionRoot != "" && task.RolePair == capability.PreIntegrationDecompositionRoot)
		}
		if !upstream || task.IntegrationAnalysis != nil {
			continue
		}
		repair, err := e.hasAnalysisAncestor(task.ID)
		if err != nil {
			return nil, err
		}
		if repair {
			continue
		}
		resolution, err := e.resolveLineage(task.ID, false)
		if err != nil {
			return nil, err
		}
		if !resolution.settled {
			pending = append(pending, task.ID)
			continue
		}
		if task.Status != models.TaskStatusMerged {
			continue
		}
		if transitions == nil && len(task.Output) > 0 && !hasExecutedOutputTransition(task.TransitionsExecuted) {
			pending = append(pending, task.ID)
		}
		// Exclusive `when` routes: only the route the output selects must run.
		// A selected route suppressed by policy still counts as supported.
		supported := false
		for _, transition := range transitions {
			if !transition.AppliesTo(task.Output) {
				continue
			}
			supported = true
			if e.state.Config.NoFollowUp && capability.PreIntegrationFollowUpTransitions[transition.Name] {
				continue
			}
			if transition.Cardinality == models.CardinalityPerSubtask && len(task.Output) == 0 {
				continue
			}
			if !task.TransitionsExecuted[transition.Name] {
				pending = append(pending, task.ID)
				break
			}
		}
		if len(transitions) > 0 && !supported && len(task.Output) > 0 {
			// No route consumes this output mode; fail closed.
			pending = append(pending, task.ID)
		}
	}
	sort.Strings(pending)
	return pending, nil
}

func (e *integrationProgressEvaluator) preIntegrationPlans() ([]*models.Task, error) {
	plans := make([]*models.Task, 0)
	for i := range e.state.Tasks {
		task := &e.state.Tasks[i]
		if task.RolePair != codePlanningRolePair {
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
	replacementIDs := make(map[string]bool)
	for i := range e.state.Tasks {
		task := &e.state.Tasks[i]
		if task.Supersedes != nil && *task.Supersedes != "" {
			replacementIDs[task.ID] = true
		}
		for _, replacementID := range task.SupersededBy {
			replacementIDs[replacementID] = true
		}
	}

	roots := make([]string, 0)
	for i := range e.state.Tasks {
		task := &e.state.Tasks[i]
		if task.RolePair == codingRolePair &&
			slices.Contains(task.EffectiveParentTasks(), planID) &&
			!replacementIDs[task.ID] {
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

func (e *integrationProgressEvaluator) evaluateCodingBarrier(decision *models.IntegrationProgressDecision) (bool, error) {
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
		if task.RolePair != codingRolePair && !(repair && task.RolePair == codePlanningRolePair) {
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

func hasExecutedOutputTransition(transitions map[string]bool) bool {
	for name, executed := range transitions {
		if executed && name != models.ReplannedTransition {
			return true
		}
	}
	return false
}

// Analysis keys identify one immutable analysis: one per frozen plan slice and
// one per global generation.
const (
	sliceAnalysisKeyPrefix  = "slice:"
	globalAnalysisKeyPrefix = "global:"
)

func sliceAnalysisKey(planTaskID string) string {
	return sliceAnalysisKeyPrefix + planTaskID
}

func globalAnalysisKey(generation int) string {
	return globalAnalysisKeyPrefix + strconv.Itoa(generation)
}

func progressReason(code integrationProgressCode) *models.IntegrationProgressReason {
	return &models.IntegrationProgressReason{Code: string(code)}
}

func progressReasonWithTasks(code integrationProgressCode, taskIDs ...string) *models.IntegrationProgressReason {
	return &models.IntegrationProgressReason{Code: string(code), TaskIDs: uniqueSortedStrings(taskIDs)}
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
