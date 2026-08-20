package ops

import (
	"errors"
	"fmt"
	"log"

	"github.com/liza-mas/liza/internal/db"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

type effectiveIntegrationCompletionAuthorization struct {
	cohortFrozen         bool
	integrationComplete  bool
	generation           int
	analysisKey          string
	sourceCommit         string
	mutationReceiptCount int
}

type effectiveIntegrationCompletionSnapshot struct {
	decision             IntegrationProgressDecision
	cohortFrozen         bool
	closure              *models.IntegrationClosure
	mutationReceiptCount int
}

var (
	reconcileIntegrationAnalysesForProgression                = ReconcileIntegrationAnalyses
	readEffectiveIntegrationCompletion                        = readEffectiveIntegrationCompletionSnapshot
	beforeEffectiveIntegrationProgressionMutationTestHook     func()
	beforeEffectiveIntegrationCompletionLinearizationTestHook func(string)
)

// withEffectiveIntegrationCompletionLinearization orders sprint finalization
// with integration ref mutation without extending the integration mutation lock
// across a blackboard write. The lock order is completion -> mutation -> read;
// both receipt and progression writes happen after the mutation lock is released.
func withEffectiveIntegrationCompletionLinearization(projectRoot, operation string, fn func() error) error {
	if beforeEffectiveIntegrationCompletionLinearizationTestHook != nil {
		beforeEffectiveIntegrationCompletionLinearizationTestHook(operation)
	}
	lock, err := projectFileLock(projectRoot, "integration-completion")
	if err != nil {
		return err
	}
	return lock.WithTimeout(integrationMutationLockTimeout).WithLockOperation(operation, fn)
}

func withEffectiveIntegrationCompletionAuthorization(
	projectRoot, operation string,
	requireSettled bool,
	fn func(*effectiveIntegrationCompletionAuthorization) error,
) error {
	if !requireSettled {
		state, err := db.For(paths.New(projectRoot).StatePath()).Read()
		if err != nil {
			return fmt.Errorf("read integration completion precondition: %w", err)
		}
		cohortFrozen := state.Goal.Integration != nil && state.Goal.Integration.ContributingSet != nil
		if !cohortFrozen {
			runBeforeEffectiveIntegrationProgressionMutationTestHook()
			return fn(&effectiveIntegrationCompletionAuthorization{})
		}
	}
	return withEffectiveIntegrationCompletionLinearization(projectRoot, "progression "+operation, func() error {
		authorization, err := authorizeEffectiveIntegrationCompletion(projectRoot, requireSettled)
		if err != nil {
			return err
		}
		runBeforeEffectiveIntegrationProgressionMutationTestHook()
		return fn(authorization)
	})
}

// authorizeEffectiveIntegrationCompletion projects pending integration work,
// then evaluates completion against live integration HEAD under the integration
// mutation lock. A nil contributing set remains available to pre-integration
// phase handoffs, but never authorizes an explicit sprint-complete claim.
func authorizeEffectiveIntegrationCompletion(projectRoot string, requireSettled bool) (*effectiveIntegrationCompletionAuthorization, error) {
	state, err := db.For(paths.New(projectRoot).StatePath()).Read()
	if err != nil {
		return nil, fmt.Errorf("read integration completion precondition: %w", err)
	}
	cohortFrozen := state.Goal.Integration != nil && state.Goal.Integration.ContributingSet != nil
	if !cohortFrozen && !requireSettled {
		return &effectiveIntegrationCompletionAuthorization{}, nil
	}
	if _, err := reconcileIntegrationAnalysesForProgression(projectRoot); err != nil {
		return nil, fmt.Errorf("reconcile integration completion precondition: %w", err)
	}

	snapshot, err := readEffectiveIntegrationCompletion(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("evaluate integration completion precondition: %w", err)
	}
	if !snapshot.cohortFrozen {
		if requireSettled {
			return nil, integrationCompletionPreconditionError(progressReason(integrationProgressWaitingPlanning))
		}
		return &effectiveIntegrationCompletionAuthorization{}, nil
	}
	if !snapshot.decision.IntegrationComplete {
		reason := snapshot.decision.Blocked
		if reason == nil {
			reason = snapshot.decision.Waiting
		}
		if reason == nil && snapshot.decision.GlobalRequest != nil {
			reason = progressReason(integrationProgressWaitingGlobalAnalysis)
		}
		if reason == nil {
			reason = &IntegrationProgressReason{Code: "integration_incomplete"}
		}
		return nil, integrationCompletionPreconditionError(reason)
	}
	if snapshot.closure == nil {
		return nil, integrationCompletionPreconditionError(progressReason(integrationProgressWaitingClosure))
	}
	return &effectiveIntegrationCompletionAuthorization{
		cohortFrozen:         true,
		integrationComplete:  true,
		generation:           snapshot.closure.Generation,
		analysisKey:          snapshot.closure.AnalysisKey,
		sourceCommit:         snapshot.closure.SourceCommit,
		mutationReceiptCount: snapshot.mutationReceiptCount,
	}, nil
}

func readEffectiveIntegrationCompletionSnapshot(projectRoot string) (effectiveIntegrationCompletionSnapshot, error) {
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return effectiveIntegrationCompletionSnapshot{}, err
	}
	capability := resolver.SlicedIntegrationCapability()
	blackboard := db.For(paths.New(projectRoot).StatePath())
	var snapshot effectiveIntegrationCompletionSnapshot
	err = withIntegrationMutationLock(projectRoot, "verify effective integration completion", func() error {
		state, readErr := blackboard.Read()
		if readErr != nil {
			return fmt.Errorf("read integration state: %w", readErr)
		}
		head, headErr := ResolveIntegrationHEAD(projectRoot, state.Config.IntegrationBranch)
		if headErr != nil {
			return fmt.Errorf("read live integration HEAD: %w", headErr)
		}
		decision, decisionErr := EvaluateIntegrationProgress(state, capability, head)
		if decisionErr != nil {
			return decisionErr
		}
		snapshot.decision = decision
		snapshot.cohortFrozen = state.Goal.Integration != nil && state.Goal.Integration.ContributingSet != nil
		if state.Goal.Integration != nil {
			snapshot.mutationReceiptCount = len(state.Goal.Integration.MutationReceipts)
		}
		if state.Goal.Integration != nil && state.Goal.Integration.Closure != nil {
			closure := *state.Goal.Integration.Closure
			snapshot.closure = &closure
		}
		return nil
	})
	return snapshot, err
}

func integrationCompletionPreconditionError(reason *IntegrationProgressReason) error {
	details := map[string]any{"code": reason.Code}
	if len(reason.TaskIDs) > 0 {
		details["task_ids"] = append([]string(nil), reason.TaskIDs...)
	}
	if reason.Guidance != "" {
		details["guidance"] = reason.Guidance
	}
	return &PreconditionError{
		Reason:  "integration is not effectively complete: " + reason.Code,
		Details: details,
	}
}

func (authorization *effectiveIntegrationCompletionAuthorization) validateState(state *models.State, requireSettled bool) error {
	frozen := state.Goal.Integration != nil && state.Goal.Integration.ContributingSet != nil
	if !frozen {
		if requireSettled || authorization.cohortFrozen {
			return integrationCompletionPreconditionError(progressReason(integrationProgressWaitingPlanning))
		}
		return nil
	}
	if !authorization.cohortFrozen || !authorization.integrationComplete {
		return integrationCompletionPreconditionError(&IntegrationProgressReason{Code: "integration_state_changed"})
	}
	closure := state.Goal.Integration.Closure
	// The outer completion lock excludes cooperating ref mutations here. Recheck
	// closure identity and receipt count to reject any durable state drift before
	// the progression transaction commits.
	if closure == nil || closure.Status != models.IntegrationClosureStatusClean ||
		closure.Generation != authorization.generation || closure.AnalysisKey != authorization.analysisKey ||
		closure.SourceCommit != authorization.sourceCommit ||
		len(state.Goal.Integration.MutationReceipts) != authorization.mutationReceiptCount {
		return integrationCompletionPreconditionError(&IntegrationProgressReason{Code: "integration_state_changed"})
	}
	return nil
}

func runBeforeEffectiveIntegrationProgressionMutationTestHook() {
	if beforeEffectiveIntegrationProgressionMutationTestHook != nil {
		beforeEffectiveIntegrationProgressionMutationTestHook()
	}
}

// loadResolver loads the frozen pipeline config for the given project root.
func loadResolver(projectRoot string) (*pipeline.Resolver, *pipeline.PipelineConfig, error) {
	cfg, err := pipeline.LoadFrozen(projectRoot)
	if err != nil {
		return nil, nil, &lizaerrors.PipelineConfigError{Operation: "load resolver", Err: err}
	}
	return pipeline.NewResolver(cfg), cfg, nil
}

// loadResolverWithRuntimePolicy loads the frozen pipeline config with runtime
// transition policies from state.yaml applied. Structural topology methods still
// see the full frozen pipeline; policies only affect runtime availability.
func loadResolverWithRuntimePolicy(projectRoot string) (*pipeline.Resolver, *pipeline.PipelineConfig, error) {
	cfg, err := pipeline.LoadFrozen(projectRoot)
	if err != nil {
		return nil, nil, &lizaerrors.PipelineConfigError{Operation: "load resolver", Err: err}
	}
	return pipeline.NewResolver(cfg, resolverOptionsFromState(projectRoot)...), cfg, nil
}

func resolverOptionsFromState(projectRoot string) []pipeline.ResolverOption {
	state, err := db.For(paths.New(projectRoot).StatePath()).Read()
	if err != nil {
		return nil
	}
	if state.Config.NoFollowUp {
		return []pipeline.ResolverOption{pipeline.WithNoFollowUp()}
	}
	return nil
}

// warnSkipRolePair logs a warning when a role-pair is skipped due to a resolver
// error during transition map construction. Should not happen on validated configs.
func warnSkipRolePair(rpName string, err error) {
	log.Printf("WARNING: BuildPipelineTransitions: skipping role-pair %q: %v", rpName, err)
}

// lifecycleStatuses holds the resolved statuses for a single role-pair's lifecycle.
type lifecycleStatuses struct {
	initial   models.TaskStatus
	executing models.TaskStatus
	submitted models.TaskStatus
	reviewing models.TaskStatus
	rejected  models.TaskStatus
	approved  models.TaskStatus
}

// resolveLifecycleStatuses resolves all lifecycle statuses for a role-pair in one call.
func resolveLifecycleStatuses(r *pipeline.Resolver, rpName string) (lifecycleStatuses, error) {
	initial, err := r.InitialStatus(rpName)
	if err != nil {
		return lifecycleStatuses{}, err
	}
	executing, err := r.ExecutingStatus(rpName)
	if err != nil {
		return lifecycleStatuses{}, err
	}
	submitted, err := r.SubmittedStatus(rpName)
	if err != nil {
		return lifecycleStatuses{}, err
	}
	reviewing, err := r.ReviewingStatus(rpName)
	if err != nil {
		return lifecycleStatuses{}, err
	}
	rejected, err := r.RejectedStatus(rpName)
	if err != nil {
		return lifecycleStatuses{}, err
	}
	approved, err := r.ApprovedStatus(rpName)
	if err != nil {
		return lifecycleStatuses{}, err
	}
	return lifecycleStatuses{initial, executing, submitted, reviewing, rejected, approved}, nil
}

// BuildPipelineTransitions creates a complete transition map by merging the
// resolver's intra-pair transitions with cross-cutting meta-state transitions.
func BuildPipelineTransitions(r *pipeline.Resolver) map[models.TaskStatus][]models.TaskStatus {
	tm := r.TransitionMap()

	var executingStatuses []models.TaskStatus
	for _, rpName := range r.RolePairNames() {
		ls, err := resolveLifecycleStatuses(r, rpName)
		if err != nil {
			warnSkipRolePair(rpName, err)
			continue
		}
		executingStatuses = append(executingStatuses, ls.executing)

		// Cross-cutting additions per lifecycle phase:
		tm[ls.initial] = append(tm[ls.initial], models.TaskStatusAbandoned, models.TaskStatusBlocked, models.TaskStatusSuperseded)
		tm[ls.executing] = append(tm[ls.executing], models.TaskStatusBlocked, ls.initial, models.TaskStatusIntegrationFailed, models.TaskStatusAbandoned)
		tm[ls.submitted] = append(tm[ls.submitted], models.TaskStatusIntegrationFailed, models.TaskStatusAbandoned)
		tm[ls.reviewing] = append(tm[ls.reviewing], ls.submitted, models.TaskStatusBlocked, models.TaskStatusAbandoned)
		tm[ls.rejected] = append(tm[ls.rejected], ls.executing, models.TaskStatusBlocked, models.TaskStatusSuperseded, models.TaskStatusAbandoned)
		tm[ls.approved] = append(tm[ls.approved], models.TaskStatusMerged, models.TaskStatusIntegrationFailed)

		// Clean state cross-cutting transition (reviewing → clean).
		cleanStatus, cleanErr := r.CleanStatus(rpName)
		if cleanErr == nil {
			tm[ls.reviewing] = append(tm[ls.reviewing], cleanStatus)
		}

		// Quorum state cross-cutting transitions.
		partiallyApproved, paErr := r.PartiallyApprovedStatus(rpName)
		reviewing2, r2Err := r.Reviewing2Status(rpName)
		if paErr == nil && r2Err == nil {
			tm[reviewing2] = append(tm[reviewing2], partiallyApproved, models.TaskStatusAbandoned) // stale revert or operator cancel
			tm[partiallyApproved] = append(tm[partiallyApproved], models.TaskStatusAbandoned, models.TaskStatusSuperseded, models.TaskStatusIntegrationFailed)
		}
	}

	// Meta-state transitions
	tm[models.TaskStatusBlocked] = append([]models.TaskStatus{
		models.TaskStatusSuperseded,
		models.TaskStatusAbandoned,
	}, executingStatuses...)
	tm[models.TaskStatusIntegrationFailed] = append([]models.TaskStatus{
		models.TaskStatusAbandoned,
		models.TaskStatusBlocked,
		models.TaskStatusSuperseded,
		models.TaskStatusMerged,
	}, executingStatuses...)
	tm[models.TaskStatusMerged] = []models.TaskStatus{}
	tm[models.TaskStatusAbandoned] = []models.TaskStatus{}
	tm[models.TaskStatusSuperseded] = []models.TaskStatus{}

	return tm
}

// ManyToOneTransitionInfo holds pre-resolved data for a many-to-one transition.
type ManyToOneTransitionInfo struct {
	Name           string
	SourceRolePair string
}

// PipelineDetectionContext holds pipeline-derived data needed for orchestrator
// wake detection. Computed once from a single config load via LoadDetectionContext.
type PipelineDetectionContext struct {
	SprintTerminals          []models.TaskStatus
	PlanningPairs            map[string]bool
	PlanningApprovedStatuses map[string]models.TaskStatus
	ManyToOneTransitions     []ManyToOneTransitionInfo
}

// LoadDetectionContext loads pipeline config once and returns both sprint-terminal
// states and transition-source pairs.
func LoadDetectionContext(projectRoot string) (*PipelineDetectionContext, error) {
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, err
	}
	var m2oInfos []ManyToOneTransitionInfo
	for _, td := range resolver.AllTransitions() {
		if td.Cardinality == "many-to-one" {
			srcPair, err := resolver.TransitionSourceRolePair(td.Name)
			if err != nil {
				continue
			}
			m2oInfos = append(m2oInfos, ManyToOneTransitionInfo{
				Name:           td.Name,
				SourceRolePair: srcPair,
			})
		}
	}

	planningPairs := resolver.TransitionSourcePairs()
	planningApprovedStatuses := make(map[string]models.TaskStatus, len(planningPairs))
	for rolePair := range planningPairs {
		approved, err := resolver.ApprovedStatus(rolePair)
		if err != nil {
			return nil, fmt.Errorf("approved status for transition-source role-pair %q: %w", rolePair, err)
		}
		planningApprovedStatuses[rolePair] = approved
	}

	return &PipelineDetectionContext{
		SprintTerminals:          resolver.SprintTerminalStates(),
		PlanningPairs:            planningPairs,
		PlanningApprovedStatuses: planningApprovedStatuses,
		ManyToOneTransitions:     m2oInfos,
	}, nil
}

// LoadPhaseHandoffDetectionContext loads the pipeline context used to decide
// whether planning output can advance to the next phase. A missing pipeline
// config retains the legacy code-planning-pair policy used by completed-sprint
// advance; malformed configs fail closed instead of silently using that policy.
func LoadPhaseHandoffDetectionContext(projectRoot string) (*PipelineDetectionContext, error) {
	detCtx, err := LoadDetectionContext(projectRoot)
	if err != nil {
		if errors.Is(err, pipeline.ErrConfigNotFound) {
			return &PipelineDetectionContext{
				PlanningApprovedStatuses: map[string]models.TaskStatus{
					"code-planning-pair": models.TaskStatusCodingPlanApproved,
				},
			}, nil
		}
		return nil, fmt.Errorf("pipeline config failed to load: %w", err)
	}
	return detCtx, nil
}

// SprintTerminalStates returns pipeline-defined sprint-terminal states for a project.
func SprintTerminalStates(projectRoot string) ([]models.TaskStatus, error) {
	ctx, err := LoadDetectionContext(projectRoot)
	if err != nil {
		return nil, err
	}
	return ctx.SprintTerminals, nil
}

// TransitionSourcePairs returns the set of role-pair names that are transition
// sources in the pipeline config.
func TransitionSourcePairs(projectRoot string) (map[string]bool, error) {
	ctx, err := LoadDetectionContext(projectRoot)
	if err != nil {
		return nil, err
	}
	return ctx.PlanningPairs, nil
}

// IsPlanningPair reports whether a role-pair is a transition source ("planning pair").
// planningPairs is the set from TransitionSourcePairs / LoadDetectionContext.
// When planningPairs is nil (legacy projects without pipeline config), falls back
// to recognizing "code-planning-pair" as the only planning pair.
func IsPlanningPair(rolePair string, planningPairs map[string]bool) bool {
	if planningPairs == nil {
		return rolePair == "code-planning-pair"
	}
	return planningPairs[rolePair]
}

// allPlannedTasksTerminalForProject checks if all planned tasks are sprint-terminal.
func allPlannedTasksTerminalForProject(s *models.State, projectRoot string) (bool, error) {
	terminals, err := SprintTerminalStates(projectRoot)
	if err != nil {
		return false, err
	}
	return s.AllPlannedTasksTerminalWith(terminals), nil
}

// LoadResolverForModels loads the pipeline resolver as a models.PipelineResolver.
func LoadResolverForModels(projectRoot string) (models.PipelineResolver, error) {
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, err
	}
	return resolver, nil
}

// pipelineBundle holds the resolver, interface, and transition map from a single config load.
// Used to avoid double-parsing pipeline.yaml within a single operation.
type pipelineBundle struct {
	pr          models.PipelineResolver
	resolver    *pipeline.Resolver // concrete resolver for policy queries (ProviderDiversity, etc.)
	transitions map[models.TaskStatus][]models.TaskStatus
}

// loadPipelineBundle loads the pipeline config once and returns the resolver interface
// and pre-built transition map.
func loadPipelineBundle(projectRoot string) (*pipelineBundle, error) {
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, err
	}
	return &pipelineBundle{
		pr:          resolver,
		resolver:    resolver,
		transitions: BuildPipelineTransitions(resolver),
	}, nil
}

// LoadPipelineTransitions loads the pipeline config and builds the transition map.
// Exported for use by the agent package.
func LoadPipelineTransitions(projectRoot string) (map[models.TaskStatus][]models.TaskStatus, error) {
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, err
	}
	return BuildPipelineTransitions(resolver), nil
}
