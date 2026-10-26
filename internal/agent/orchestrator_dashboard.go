package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/prompts"
)

// RenderOrchestratorDashboard resolves the orchestrator's inputs from the
// snapshot and the project, selects the standalone wake and renders the
// dashboard and wake instruction.
func RenderOrchestratorDashboard(state *models.State, projectRoot, agentID string) (dashboard, wakeInstruction string, err error) {
	return renderOrchestratorDashboard(state, projectRoot, agentID, nil)
}

// renderOrchestratorDashboardForWake renders an already selected invocation:
// the supplied trigger and integration projection are kept, never reselected.
func renderOrchestratorDashboardForWake(state *models.State, projectRoot, agentID string, wake OrchestratorWakeResult) (dashboard, wakeInstruction string, err error) {
	return renderOrchestratorDashboard(state, projectRoot, agentID, &wake)
}

func renderOrchestratorDashboard(state *models.State, projectRoot, agentID string, selected *OrchestratorWakeResult) (dashboard, wakeInstruction string, err error) {
	in := resolveOrchestratorDashboardInput(state, projectRoot, agentID)
	var decision prompts.OrchestratorWakeDecision
	if selected != nil {
		decision = prompts.OrchestratorWakeDecision{Trigger: prompts.WakeTrigger(selected.Trigger), Integration: selected.Integration}
	} else {
		decision = selectStandaloneWake(in, projectRoot)
	}
	// The integration outcome above is evaluated before entry points resolve,
	// so an entry-point error never hides it.
	routes, routesErr := resolveWakeRoutes(state.Goal.EntryPoint, projectRoot)
	if routesErr != nil {
		return "", "", fmt.Errorf("building wake template data: %w", routesErr)
	}
	in.Routes = routes
	return prompts.RenderOrchestratorDashboard(in, decision)
}

// resolveOrchestratorDashboardInput gathers every value the dashboard reads
// beyond the snapshot. A detection context that cannot load gates nothing
// here: the transition executor loads its own domain and still gates.
func resolveOrchestratorDashboardInput(state *models.State, projectRoot, agentID string) *prompts.OrchestratorDashboardInput {
	detCtx, detErr := ops.LoadDetectionContext(projectRoot)
	var planningPairs map[string]bool
	var m2oTransitions []ops.ManyToOneTransitionInfo
	domain := ops.PlanningPairsOnly(nil)
	in := &prompts.OrchestratorDashboardInput{
		State:   state,
		AgentID: agentID,
		WakeOpen: state.Sprint.Status != models.SprintStatusCheckpoint &&
			state.Sprint.Status != models.SprintStatusCompleted,
		HeldPlan:         ops.HasHeldPlan(state),
		UnseenNotes:      ops.UnseenHumanNotes(state),
		TestFilePatterns: SubmissionTestPatternsText(),
	}
	if detErr == nil {
		in.SprintTerminals = detCtx.SprintTerminals
		planningPairs = detCtx.PlanningPairs
		m2oTransitions = detCtx.ManyToOneTransitions
		domain = detCtx.PlanHandoff
	}
	in.CycleBlocked = countCycleBlockedPlanning(state.Tasks, planningPairs)
	if in.WakeOpen {
		in.PlanningTasks = collectMergedPlanningTasks(state, domain)
		in.ReadyManyToOne = ops.CountReadyManyToOneCohorts(state, m2oTransitions)
	}
	if len(in.PlanningTasks) > 0 {
		in.BlockedAwaitPlanningHandoff = ops.BlockedTasksAwaitPlanningHandoff(state, domain)
	}
	in.ActionableBlockedIDs = ops.ActionableBlockedTaskIDs(state)
	in.ActionableHypothesisExhausted = ops.CountActionableHypothesisExhaustedTasks(state)
	return in
}

// selectStandaloneWake picks the wake for a render nobody selected. Only a
// completion wake evaluates live integration progress, and the projected
// outcome then replaces it.
func selectStandaloneWake(in *prompts.OrchestratorDashboardInput, projectRoot string) prompts.OrchestratorWakeDecision {
	trigger := prompts.SelectOrchestratorWake(in)
	var integration prompts.EffectiveIntegrationCompletion
	if trigger == prompts.WakeCodingComplete || trigger == prompts.WakeSprintComplete {
		decision, evaluationErr := ops.EvaluateLiveIntegrationProgress(in.State, projectRoot)
		integration = prompts.ProjectEffectiveIntegrationCompletion(decision, nil, evaluationErr)
		trigger = integration.WakeTrigger
	}
	return prompts.OrchestratorWakeDecision{Trigger: trigger, Integration: integration}
}

// SubmissionTestPatternsText is the submission test-file matcher display
// text the prompts name; ops owns the matcher itself.
func SubmissionTestPatternsText() string {
	return strings.Join(ops.TestFileMatcherPatterns(), ", ")
}

// countCycleBlockedPlanning counts MERGED planning tasks with a transition_cycle_blocked history event.
func countCycleBlockedPlanning(tasks []models.Task, planningPairs map[string]bool) int {
	count := 0
	for i := range tasks {
		if tasks[i].Status == models.TaskStatusMerged &&
			ops.IsPlanningPair(tasks[i].RolePair, planningPairs) &&
			ops.IsTransitionCycleBlocked(&tasks[i]) {
			count++
		}
	}
	return count
}

// collectMergedPlanningTasks returns merged planning tasks with output for PLANNING_COMPLETE detection.
// Only transition-source role-pairs qualify — coding tasks with output are ignored.
// Uses the same IsPlanningPair predicate as workdetection to avoid classification drift,
// and the shared hand-off classifier for what each plan still needs.
func collectMergedPlanningTasks(state *models.State, domain ops.PlanHandoffDomain) []prompts.PlanningTask {
	var result []prompts.PlanningTask
	for _, taskID := range state.Sprint.Scope.Planned {
		task := state.FindTask(taskID)
		if !domain.PlanningCompleteEligible(state, task) {
			continue
		}
		class, blocker := domain.Classify(state, task)
		entry := prompts.PlanningTask{
			TaskID:   task.ID,
			Output:   task.Output,
			Class:    class,
			Blocker:  blocker,
			PlanRefs: prompts.DistinctPlanFiles(task.Output),
		}
		if correction := domain.ReadyPlanCorrection(state, task); correction != nil {
			entry.CorrectionID = correction.ID
			entry.CorrectionStatus = correction.Status
			entry.PlanRefs = prompts.DistinctPlanFiles(correction.Output)
		}
		result = append(result, entry)
	}
	return result
}

// resolveWakeRoutes resolves the pipeline's entry-points to role-pairs,
// display names, task types and ID prefixes for the INITIAL_PLANNING wake.
//
// Returns an error if the pipeline config is missing/malformed, or if an
// explicit entry-point was specified but not found in the config.
func resolveWakeRoutes(goalEntryPoint, projectRoot string) (prompts.WakeRoutes, error) {
	var routes prompts.WakeRoutes
	cfg, err := pipeline.LoadFrozen(projectRoot)
	if err != nil {
		return routes, err
	}
	resolver := pipeline.NewResolver(cfg)

	entryPointsByName := make(map[string]prompts.WakeEntryPoint, len(cfg.Pipeline.EntryPoints))
	for epName, epValue := range cfg.Pipeline.EntryPoints {
		rolePair, ok := wakeEntryPointRolePair(epValue)
		if !ok {
			continue
		}
		ep, err := buildWakeEntryPoint(resolver, epName, rolePair)
		if err != nil {
			return routes, err
		}
		routes.EntryPoints = append(routes.EntryPoints, ep)
		entryPointsByName[epName] = ep
	}
	// Sorted for deterministic template output.
	sort.Slice(routes.EntryPoints, func(i, j int) bool { return routes.EntryPoints[i].Name < routes.EntryPoints[j].Name })

	if goalEntryPoint != "" {
		ep, ok := entryPointsByName[goalEntryPoint]
		if !ok {
			return routes, fmt.Errorf("unknown entry-point %q; available: %v", goalEntryPoint, wakeEntryPointNames(cfg))
		}
		routes.Resolved = &ep
	}
	return routes, nil
}

// entryPointValueSeparator splits a goal entry-point value into its kind and
// the simple role pair it targets.
const entryPointValueSeparator = "."

func wakeEntryPointRolePair(entryPointValue string) (string, bool) {
	parts := strings.SplitN(entryPointValue, entryPointValueSeparator, 2)
	if len(parts) != 2 {
		return "", false
	}
	return parts[1], true
}

func buildWakeEntryPoint(resolver *pipeline.Resolver, name, simpleRolePair string) (prompts.WakeEntryPoint, error) {
	simpleDisplayName := resolveWakeDoerDisplayName(resolver, simpleRolePair)
	simpleTaskType := resolveWakeTaskType(resolver, simpleRolePair)
	simpleTaskIDPrefix, err := wakeTaskIDPrefix(resolver, simpleRolePair)
	if err != nil {
		return prompts.WakeEntryPoint{}, fmt.Errorf("entry-point %q target %q: %w", name, simpleRolePair, err)
	}
	ep := prompts.WakeEntryPoint{
		Name:               name,
		SimpleRolePair:     simpleRolePair,
		SimpleDisplayName:  simpleDisplayName,
		SimpleTaskType:     simpleTaskType,
		SimpleTaskIDPrefix: simpleTaskIDPrefix,
	}

	fanOutRolePair, found, err := resolver.DecompositionRootForTarget(simpleRolePair)
	if err != nil {
		return ep, fmt.Errorf("entry-point %q target %q: %w", name, simpleRolePair, err)
	}
	if !found {
		return ep, nil
	}

	ep.HasFanOutTarget = true
	ep.FanOutRolePair = fanOutRolePair
	ep.FanOutDisplayName = resolveWakeDoerDisplayName(resolver, fanOutRolePair)
	ep.FanOutTaskType = resolveWakeTaskType(resolver, fanOutRolePair)
	ep.FanOutTaskIDPrefix, err = wakeTaskIDPrefix(resolver, fanOutRolePair)
	if err != nil {
		return ep, fmt.Errorf("entry-point %q fan-out target %q: %w", name, fanOutRolePair, err)
	}
	return ep, nil
}

func wakeTaskIDPrefix(resolver *pipeline.Resolver, rolePair string) (string, error) {
	rp, err := resolver.RolePair(rolePair)
	if err != nil {
		return "", err
	}
	return rp.TaskSlugOrName(rolePair), nil
}

// wakeEntryPointNames returns sorted entry-point names from a pipeline config for error messages.
func wakeEntryPointNames(cfg *pipeline.PipelineConfig) []string {
	names := make([]string, 0, len(cfg.Pipeline.EntryPoints))
	for name := range cfg.Pipeline.EntryPoints {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// resolveWakeDoerDisplayName looks up the doer's display name for a role-pair
// using the Resolver to access the roles section.
func resolveWakeDoerDisplayName(resolver *pipeline.Resolver, rolePair string) string {
	rp, err := resolver.RolePair(rolePair)
	if err != nil {
		return rolePair
	}
	return resolver.RoleDisplayName(rp.Doer)
}

// resolveWakeTaskType looks up the task type for a role-pair's doer role.
func resolveWakeTaskType(resolver *pipeline.Resolver, rolePair string) string {
	rp, err := resolver.RolePair(rolePair)
	if err != nil {
		return string(models.TaskTypeCoding) // safe default
	}
	tt := models.TaskTypeForRole(rp.Doer)
	if tt == "" {
		return string(models.TaskTypeCoding)
	}
	return string(tt)
}
