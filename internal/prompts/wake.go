package prompts

import (
	"fmt"
	"sort"
	"strings"

	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
)

// planningTaskData holds a merged planning task's output for the PLANNING_COMPLETE template.
type planningTaskData struct {
	TaskID string
	Output []models.OutputEntry
}

// wakeEntryPointData describes an available entry-point for the orchestrator template.
type wakeEntryPointData struct {
	Name               string // e.g., "general-objective"
	SimpleRolePair     string // e.g., "epic-planning-pair"
	SimpleDisplayName  string // simple target doer's display name, e.g., "Epic Planner"
	SimpleTaskType     string // e.g., "epic-planning", "architecture"
	SimpleTaskIDPrefix string // configured task ID prefix, e.g., "ep"
	HasFanOutTarget    bool
	FanOutRolePair     string // e.g., "epic-planning-main-pair"
	FanOutDisplayName  string // fan-out target doer's display name
	FanOutTaskType     string
	FanOutTaskIDPrefix string
}

// wakeTemplateData is used by wake trigger templates that need GoalSpecRef
type wakeTemplateData struct {
	AgentID                    string
	GoalSpecRef                string
	GoalEntryPoint             string             // set if --entry-point was specified
	ResolvedRolePair           string             // role-pair resolved from GoalEntryPoint
	ResolvedDisplayName        string             // display name of the resolved role-pair's doer
	ResolvedTaskIDPrefix       string             // configured task ID prefix, e.g., "ep"
	ResolvedTaskType           string             // resolved from doer role → TaskTypeForRole
	ResolvedEntryPoint         wakeEntryPointData // resolved route data for GoalEntryPoint
	ResolvedHasFanOutTarget    bool
	ResolvedFanOutRolePair     string
	ResolvedFanOutDisplayName  string
	ResolvedFanOutTaskIDPrefix string
	ResolvedFanOutTaskType     string
	EntryPoints                []wakeEntryPointData // available entry-points for LLM classification
	Integration                EffectiveIntegrationCompletion
}

// EffectiveIntegrationCompletion is the read-only wake projection of the
// authoritative integration progress and reconciliation outcomes.
type EffectiveIntegrationCompletion struct {
	WakeTrigger    string
	Status         string
	ReasonCode     string
	TaskIDs        []string
	RequestKeys    []string
	CreatedTaskIDs []string
	Guidance       string
}

// ProjectEffectiveIntegrationCompletion maps authoritative integration
// progress and reconciliation outcomes into deterministic wake vocabulary.
func ProjectEffectiveIntegrationCompletion(
	decision ops.IntegrationProgressDecision,
	reconciliation *ops.ReconcileIntegrationAnalysesResult,
	evaluationErr error,
) EffectiveIntegrationCompletion {
	if evaluationErr != nil {
		return EffectiveIntegrationCompletion{
			WakeTrigger: "INTEGRATION_UNAVAILABLE",
			Status:      "unavailable",
			ReasonCode:  "integration_progress_unavailable",
			Guidance:    evaluationErr.Error(),
		}
	}
	if decision.IntegrationComplete {
		return EffectiveIntegrationCompletion{
			WakeTrigger: "SPRINT_COMPLETE",
			Status:      "complete",
		}
	}

	reason := decision.Blocked
	if reconciliation != nil && reconciliation.Reason != nil {
		reason = reconciliation.Reason
	}
	if reason != nil {
		trigger := "INTEGRATION_BLOCKED"
		status := "blocked"
		if decision.Exhausted {
			trigger = "INTEGRATION_EXHAUSTED"
			status = "exhausted"
		}
		return EffectiveIntegrationCompletion{
			WakeTrigger: trigger,
			Status:      status,
			ReasonCode:  reason.Code,
			TaskIDs:     sortedStringsCopy(reason.TaskIDs),
			Guidance:    reason.Guidance,
		}
	}

	requestKeys := integrationRequestKeys(decision)
	var createdTaskIDs []string
	if reconciliation != nil {
		createdTaskIDs = sortedStringsCopy(reconciliation.CreatedTaskIDs)
	}
	if decision.FreezeContributingSet || len(requestKeys) > 0 {
		return EffectiveIntegrationCompletion{
			WakeTrigger:    "CODING_COMPLETE",
			Status:         "reconciliation_needed",
			RequestKeys:    requestKeys,
			CreatedTaskIDs: createdTaskIDs,
		}
	}
	if decision.Waiting != nil {
		return EffectiveIntegrationCompletion{
			WakeTrigger:    "INTEGRATION_WAITING",
			Status:         "waiting",
			ReasonCode:     decision.Waiting.Code,
			TaskIDs:        sortedStringsCopy(decision.Waiting.TaskIDs),
			CreatedTaskIDs: createdTaskIDs,
			Guidance:       decision.Waiting.Guidance,
		}
	}
	return EffectiveIntegrationCompletion{
		WakeTrigger: "INTEGRATION_UNAVAILABLE",
		Status:      "unavailable",
		ReasonCode:  "integration_progress_incomplete",
	}
}

func integrationRequestKeys(decision ops.IntegrationProgressDecision) []string {
	keys := make([]string, 0, len(decision.SliceRequests)+1)
	for _, request := range decision.SliceRequests {
		keys = append(keys, request.Key)
	}
	if decision.GlobalRequest != nil {
		keys = append(keys, decision.GlobalRequest.Key)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringsCopy(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func evaluateEffectiveIntegrationCompletion(state *models.State, projectRoot string) EffectiveIntegrationCompletion {
	cfg, err := pipeline.LoadFrozen(projectRoot)
	if err != nil {
		return ProjectEffectiveIntegrationCompletion(ops.IntegrationProgressDecision{}, nil, fmt.Errorf("load frozen pipeline: %w", err))
	}
	resolver := pipeline.NewResolver(cfg)
	integrationHEAD, err := gitpkg.New(projectRoot).GetCommitSHA(state.Config.IntegrationBranch)
	if err != nil {
		return ProjectEffectiveIntegrationCompletion(ops.IntegrationProgressDecision{}, nil, fmt.Errorf("read integration HEAD: %w", err))
	}
	decision, err := ops.EvaluateIntegrationProgress(state, resolver.SlicedIntegrationCapability(), integrationHEAD)
	return ProjectEffectiveIntegrationCompletion(decision, nil, err)
}

func writeIntegrationProgressDiagnostic(b *strings.Builder, projection EffectiveIntegrationCompletion) {
	if projection.Status == "" {
		return
	}
	b.WriteString("\nINTEGRATION PROGRESS:\n")
	b.WriteString(fmt.Sprintf("- Status: %s\n", projection.Status))
	if projection.ReasonCode != "" {
		b.WriteString(fmt.Sprintf("- Reason: %s\n", projection.ReasonCode))
	}
	if len(projection.TaskIDs) > 0 {
		b.WriteString(fmt.Sprintf("- Related tasks: %s\n", strings.Join(projection.TaskIDs, ", ")))
	}
	if len(projection.RequestKeys) > 0 {
		b.WriteString(fmt.Sprintf("- Requested analyses: %s\n", strings.Join(projection.RequestKeys, ", ")))
	}
	if len(projection.CreatedTaskIDs) > 0 {
		b.WriteString(fmt.Sprintf("- Reconciled tasks: %s\n", strings.Join(projection.CreatedTaskIDs, ", ")))
	}
	if projection.Guidance != "" {
		b.WriteString(fmt.Sprintf("- Guidance: %s\n", projection.Guidance))
	}
}

func integrationOutcomeInstructions(projection EffectiveIntegrationCompletion) string {
	var b strings.Builder
	b.WriteString("Authoritative integration completion is not effective.\n")
	if projection.ReasonCode != "" {
		b.WriteString(fmt.Sprintf("Reason: %s\n", projection.ReasonCode))
	}
	if len(projection.TaskIDs) > 0 {
		b.WriteString(fmt.Sprintf("Related tasks: %s\n", strings.Join(projection.TaskIDs, ", ")))
	}
	if projection.Guidance != "" {
		b.WriteString(fmt.Sprintf("Guidance: %s\n", projection.Guidance))
	}
	b.WriteString("Preserve this outcome; do not create analysis tasks or advance terminal state manually.")
	return b.String()
}

// wakePlanningCompleteData is used by the PLANNING_COMPLETE wake template
type wakePlanningCompleteData struct {
	AgentID       string
	PlanningTasks []planningTaskData
}

// collectMergedPlanningTasks returns merged planning tasks with output for PLANNING_COMPLETE detection.
// Only transition-source role-pairs qualify — coding tasks with output are ignored.
// Uses the same IsPlanningPair predicate as workdetection to avoid classification drift.
func collectMergedPlanningTasks(state *models.State, planningPairs map[string]bool) []planningTaskData {
	var result []planningTaskData
	for _, taskID := range state.Sprint.Scope.Planned {
		task := state.FindTask(taskID)
		if !ops.IsPlanningCompleteEligible(task, planningPairs, state) {
			continue
		}
		result = append(result, planningTaskData{
			TaskID: task.ID,
			Output: task.Output,
		})
	}
	return result
}

func determineWakeTrigger(totalTasks, blocked, hypothesisExhausted, immediateDiscoveries int, sprintComplete, codingComplete bool, planningTasks []planningTaskData, m2oReadyCount int) string {
	if totalTasks == 0 {
		return "INITIAL_PLANNING"
	}
	if blocked > 0 {
		return "BLOCKED_TASKS"
	}
	if hypothesisExhausted > 0 {
		return "HYPOTHESIS_EXHAUSTED"
	}
	if immediateDiscoveries > 0 {
		return "IMMEDIATE_DISCOVERY"
	}
	if len(planningTasks) > 0 {
		return "PLANNING_COMPLETE"
	}
	if m2oReadyCount > 0 {
		return "MANY_TO_ONE_READY"
	}
	if sprintComplete && codingComplete {
		return "CODING_COMPLETE"
	}
	if sprintComplete {
		return "SPRINT_COMPLETE"
	}
	return "UNKNOWN"
}

// buildWakeTemplateData constructs entry-point-aware template data for the
// INITIAL_PLANNING wake trigger. Resolves entry-points from the pipeline config
// to role-pairs and display names.
//
// Returns an error if the pipeline config is missing/malformed, or if an
// explicit entry-point was specified but not found in the config.
func buildWakeTemplateData(goalSpecRef, goalEntryPoint, projectRoot string) (wakeTemplateData, error) {
	data := wakeTemplateData{
		GoalSpecRef:    goalSpecRef,
		GoalEntryPoint: goalEntryPoint,
	}

	cfg, err := pipeline.LoadFrozen(projectRoot)
	if err != nil {
		return data, err
	}
	resolver := pipeline.NewResolver(cfg)

	// Build sorted entry-point list for deterministic template output.
	var eps []wakeEntryPointData
	entryPointsByName := make(map[string]wakeEntryPointData, len(cfg.Pipeline.EntryPoints))
	for epName, epValue := range cfg.Pipeline.EntryPoints {
		rolePair, ok := entryPointRolePair(epValue)
		if !ok {
			continue
		}
		ep, err := buildWakeEntryPointData(resolver, epName, rolePair)
		if err != nil {
			return data, err
		}
		eps = append(eps, ep)
		entryPointsByName[epName] = ep
	}
	sort.Slice(eps, func(i, j int) bool { return eps[i].Name < eps[j].Name })
	data.EntryPoints = eps

	// If entry-point is explicitly set, resolve it.
	if goalEntryPoint != "" {
		ep, ok := entryPointsByName[goalEntryPoint]
		if !ok {
			return data, fmt.Errorf("unknown entry-point %q; available: %v", goalEntryPoint, entryPointNames(cfg))
		}
		data.ResolvedEntryPoint = ep
		data.ResolvedRolePair = ep.SimpleRolePair
		data.ResolvedDisplayName = ep.SimpleDisplayName
		data.ResolvedTaskIDPrefix = ep.SimpleTaskIDPrefix
		data.ResolvedTaskType = ep.SimpleTaskType
		data.ResolvedHasFanOutTarget = ep.HasFanOutTarget
		data.ResolvedFanOutRolePair = ep.FanOutRolePair
		data.ResolvedFanOutDisplayName = ep.FanOutDisplayName
		data.ResolvedFanOutTaskIDPrefix = ep.FanOutTaskIDPrefix
		data.ResolvedFanOutTaskType = ep.FanOutTaskType
	}

	return data, nil
}

func entryPointRolePair(entryPointValue string) (string, bool) {
	parts := strings.SplitN(entryPointValue, ".", 2)
	if len(parts) != 2 {
		return "", false
	}
	return parts[1], true
}

func buildWakeEntryPointData(resolver *pipeline.Resolver, name, simpleRolePair string) (wakeEntryPointData, error) {
	simpleDisplayName := resolveDoerDisplayName(resolver, simpleRolePair)
	simpleTaskType := resolveTaskType(resolver, simpleRolePair)
	simpleTaskIDPrefix, err := taskIDPrefixForRolePair(resolver, simpleRolePair)
	if err != nil {
		return wakeEntryPointData{}, fmt.Errorf("entry-point %q target %q: %w", name, simpleRolePair, err)
	}
	ep := wakeEntryPointData{
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
	ep.FanOutDisplayName = resolveDoerDisplayName(resolver, fanOutRolePair)
	ep.FanOutTaskType = resolveTaskType(resolver, fanOutRolePair)
	ep.FanOutTaskIDPrefix, err = taskIDPrefixForRolePair(resolver, fanOutRolePair)
	if err != nil {
		return ep, fmt.Errorf("entry-point %q fan-out target %q: %w", name, fanOutRolePair, err)
	}
	return ep, nil
}

func taskIDPrefixForRolePair(resolver *pipeline.Resolver, rolePair string) (string, error) {
	rp, err := resolver.RolePair(rolePair)
	if err != nil {
		return "", err
	}
	return rp.TaskSlugOrName(rolePair), nil
}

// entryPointNames returns sorted entry-point names from a pipeline config for error messages.
func entryPointNames(cfg *pipeline.PipelineConfig) []string {
	names := make([]string, 0, len(cfg.Pipeline.EntryPoints))
	for name := range cfg.Pipeline.EntryPoints {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// resolveDoerDisplayName looks up the doer's display name for a role-pair
// using the Resolver to access the roles section.
func resolveDoerDisplayName(resolver *pipeline.Resolver, rolePair string) string {
	rp, err := resolver.RolePair(rolePair)
	if err != nil {
		return rolePair
	}
	return resolver.RoleDisplayName(rp.Doer)
}

// resolveTaskType looks up the task type for a role-pair's doer role.
func resolveTaskType(resolver *pipeline.Resolver, rolePair string) string {
	rp, err := resolver.RolePair(rolePair)
	if err != nil {
		return "coding" // safe default
	}
	tt := models.TaskTypeForRole(rp.Doer)
	if tt == "" {
		return "coding"
	}
	return string(tt)
}

func buildInstructionsForWakeTrigger(wakeTrigger, agentID string, wakeData wakeTemplateData, planningTasks []planningTaskData) (string, error) {
	agentData := wakeTemplateData{AgentID: agentID}
	switch wakeTrigger {
	case "INITIAL_PLANNING":
		wakeData.AgentID = agentID
		return executeTemplate("wake_initial_planning", wakeData)
	case "BLOCKED_TASKS":
		return executeTemplate("wake_blocked_tasks", agentData)
	case "HYPOTHESIS_EXHAUSTED":
		return executeTemplate("wake_hypothesis_exhausted", agentData)
	case "IMMEDIATE_DISCOVERY":
		return executeTemplate("wake_immediate_discovery", agentData)
	case "PLANNING_COMPLETE":
		return executeTemplate("wake_planning_complete", wakePlanningCompleteData{
			AgentID:       agentID,
			PlanningTasks: planningTasks,
		})
	case "MANY_TO_ONE_READY":
		return executeTemplate("wake_many_to_one_ready", agentData)
	case "CODING_COMPLETE":
		wakeData.AgentID = agentID
		return executeTemplate("wake_coding_complete", wakeData)
	case "INTEGRATION_WAITING", "INTEGRATION_BLOCKED", "INTEGRATION_EXHAUSTED", "INTEGRATION_UNAVAILABLE":
		return integrationOutcomeInstructions(wakeData.Integration), nil
	case "SPRINT_COMPLETE":
		return executeTemplate("wake_sprint_complete", agentData)
	default:
		return "", nil
	}
}
