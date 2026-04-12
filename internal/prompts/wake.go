package prompts

import (
	"fmt"
	"sort"
	"strings"

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
	Name        string // e.g., "general-objective"
	RolePair    string // e.g., "epic-planning-pair"
	DisplayName string // doer's display name, e.g., "Epic Planner"
	TaskType    string // e.g., "coding", "architecture"
}

// wakeTemplateData is used by wake trigger templates that need GoalSpecRef
type wakeTemplateData struct {
	AgentID              string
	GoalSpecRef          string
	GoalEntryPoint       string               // set if --entry-point was specified
	ResolvedRolePair     string               // role-pair resolved from GoalEntryPoint
	ResolvedDisplayName  string               // display name of the resolved role-pair's doer
	ResolvedTaskIDPrefix string               // task ID prefix, e.g., "epic-planning" (role-pair without "-pair" suffix)
	ResolvedTaskType     string               // resolved from doer role → TaskTypeForRole
	EntryPoints          []wakeEntryPointData // available entry-points for LLM classification
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
	if sprintComplete && len(planningTasks) > 0 {
		return "PLANNING_COMPLETE"
	}
	if sprintComplete && m2oReadyCount > 0 {
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
	for epName, epValue := range cfg.Pipeline.EntryPoints {
		parts := strings.SplitN(epValue, ".", 2)
		if len(parts) != 2 {
			continue
		}
		rolePair := parts[1]
		displayName := resolveDoerDisplayName(resolver, rolePair)
		taskType := resolveTaskType(resolver, rolePair)
		eps = append(eps, wakeEntryPointData{
			Name:        epName,
			RolePair:    rolePair,
			DisplayName: displayName,
			TaskType:    taskType,
		})
	}
	sort.Slice(eps, func(i, j int) bool { return eps[i].Name < eps[j].Name })
	data.EntryPoints = eps

	// If entry-point is explicitly set, resolve it.
	if goalEntryPoint != "" {
		epValue, ok := cfg.Pipeline.EntryPoints[goalEntryPoint]
		if !ok {
			return data, fmt.Errorf("unknown entry-point %q; available: %v", goalEntryPoint, entryPointNames(cfg))
		}
		parts := strings.SplitN(epValue, ".", 2)
		if len(parts) == 2 {
			data.ResolvedRolePair = parts[1]
			data.ResolvedDisplayName = resolveDoerDisplayName(resolver, parts[1])
			data.ResolvedTaskIDPrefix = strings.TrimSuffix(parts[1], "-pair")
			data.ResolvedTaskType = resolveTaskType(resolver, parts[1])
		}
	}

	return data, nil
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
		return executeTemplate("wake_coding_complete", agentData)
	case "SPRINT_COMPLETE":
		return executeTemplate("wake_sprint_complete", agentData)
	default:
		return "", nil
	}
}
