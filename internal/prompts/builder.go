package prompts

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// BasePromptConfig contains configuration for building the base prompt
type BasePromptConfig struct {
	Role        string
	AgentID     string
	TaskID      string // empty for orchestrator
	SpecsDir    string
	ProjectRoot string
	StatePath   string
	GoalDesc    string
	GoalSpecRef string
}

// SiblingTaskSummary provides minimal context about sibling tasks in the same sprint
type SiblingTaskSummary struct {
	ID          string
	Description string
	Status      string
	PlanRef     string
	RolePair    string
}

// BuildBasePrompt creates the base bootstrap prompt for all agents
func BuildBasePrompt(config BasePromptConfig) (string, error) {
	return executeTemplate("base_prompt", config)
}

// RenderOrchestratorDashboard pre-renders the orchestrator dashboard and wake instruction
// strings for use as RoleContextData.DashboardOutput and RoleContextData.WakeInstruction.
func RenderOrchestratorDashboard(state *models.State, projectRoot, agentID string) (dashboard, wakeInstruction string, err error) {
	totalTasks := len(state.Tasks)
	merged := countTasksByStatus(state.Tasks, models.TaskStatusMerged)
	blocked := countTasksByStatus(state.Tasks, models.TaskStatusBlocked)
	integrationFailed := countTasksByStatus(state.Tasks, models.TaskStatusIntegrationFailed)
	unclaimed := countTasksByStatus(state.Tasks, models.TaskStatusReady)

	inProgress := countTasksByStatus(state.Tasks, models.TaskStatusImplementing) +
		countTasksByStatus(state.Tasks, models.TaskStatusReadyForReview) +
		countTasksByStatus(state.Tasks, models.TaskStatusApproved)

	hypothesisExhausted := countHypothesisExhausted(state.Tasks)
	immediateDiscoveries := countImmediateDiscoveries(state.Discovered)

	detCtx, detErr := ops.LoadDetectionContext(projectRoot)
	var sprintTerminals []models.TaskStatus
	var planningPairs map[string]bool
	var m2oTransitions []ops.ManyToOneTransitionInfo
	if detErr == nil {
		sprintTerminals = detCtx.SprintTerminals
		planningPairs = detCtx.PlanningPairs
		m2oTransitions = detCtx.ManyToOneTransitions
	}

	cycleBlocked := countCycleBlockedPlanning(state.Tasks, planningPairs)

	sprintComplete := state.AllPlannedTasksTerminalWith(sprintTerminals)

	var planningTasks []planningTaskData
	if sprintComplete {
		planningTasks = collectMergedPlanningTasks(state, planningPairs)
	}

	codingComplete := state.Goal.BaseCommit != nil && !hasIntegrationTaskInSprint(state)

	var m2oReadyCount int
	if sprintComplete {
		m2oReadyCount = countReadyM2OCohorts(state, m2oTransitions)
	}

	wakeTrigger := determineWakeTrigger(totalTasks, blocked, hypothesisExhausted, immediateDiscoveries, sprintComplete, codingComplete, planningTasks, m2oReadyCount)

	wakeData, wakeErr := buildWakeTemplateData(state.Goal.SpecRef, state.Goal.EntryPoint, projectRoot)
	if wakeErr != nil {
		return "", "", fmt.Errorf("building wake template data: %w", wakeErr)
	}

	wakeInstructions, instrErr := buildInstructionsForWakeTrigger(wakeTrigger, agentID, wakeData, planningTasks)
	if instrErr != nil {
		return "", "", fmt.Errorf("building wake instructions: %w", instrErr)
	}

	// Build the dashboard string (replaces orchestrator_context.tmpl rendering)
	var b strings.Builder
	b.WriteString("\n\n=== ORCHESTRATOR CONTEXT ===\n")
	b.WriteString(fmt.Sprintf("WAKE TRIGGER: %s\n", wakeTrigger))
	b.WriteString("\nSPRINT STATE:\n")
	if state.Sprint.Number > 0 {
		b.WriteString(fmt.Sprintf("- Sprint number: %d\n", state.Sprint.Number))
	}
	if len(state.SprintHistory) > 0 {
		b.WriteString(fmt.Sprintf("- Previous sprints: %d\n", len(state.SprintHistory)))
		for _, sh := range state.SprintHistory {
			b.WriteString(fmt.Sprintf("  - %s: %s (%d tasks done)\n", sh.ID, sh.Status, sh.TasksDone))
		}
	}
	b.WriteString(fmt.Sprintf("- Total tasks: %d\n", totalTasks))
	b.WriteString(fmt.Sprintf("- Merged: %d\n", merged))
	b.WriteString(fmt.Sprintf("- In progress: %d\n", inProgress))
	b.WriteString(fmt.Sprintf("- Unclaimed: %d\n", unclaimed))
	b.WriteString(fmt.Sprintf("- Blocked: %d\n", blocked))
	b.WriteString(fmt.Sprintf("- Integration failed: %d\n", integrationFailed))
	b.WriteString(fmt.Sprintf("- Hypothesis exhausted: %d\n", hypothesisExhausted))
	b.WriteString(fmt.Sprintf("- Immediate discoveries: %d\n", immediateDiscoveries))
	if cycleBlocked > 0 {
		b.WriteString(fmt.Sprintf("- Cycle-blocked planning: %d\n", cycleBlocked))
	}

	b.WriteString("\nORCHESTRATOR COMMANDS:\n")
	b.WriteString(fmt.Sprintf(`- liza add-tasks — Add one or more tasks to blackboard (atomic per task, with validation)
  liza add-tasks --tasks-file <path.json> --agent-id "%s" --json
- liza supersede-task — Supersede task (replacement_ids optional — omit when work completed externally)
  liza supersede-task <task-id> --replacement-ids "..." --reason "..." --agent-id "%s" --json
- liza assess-blocked — Record orchestrator assessment of a BLOCKED task (prevents re-wake loops)
  liza assess-blocked <task-id> --note "..." --agent-id "%s" --json
- liza wt-delete — Delete worktree for abandoned/superseded/blocked tasks
  liza wt-delete <task-id> --agent-id "%s" --json
- liza sprint-checkpoint — Create sprint checkpoint for human review (pauses all agents)
  liza sprint-checkpoint --json
- liza update-sprint-metrics — Recompute sprint metrics from current state
  liza update-sprint-metrics --json

ANOMALY LOGGING:
| Event | Type | Required Fields |
|-------|------|-----------------|
| Two coders failed same task | hypothesis_exhaustion | — |
| Spec gap discovered | spec_gap | — |
| Review stuck in cycles | review_deadlock | — |
| Multiple reviewers failed | review_exhaustion | reviewers_failed, common_blocker |
| Protocol ambiguity | system_ambiguity | protocol_section, question |
Format: anomalies: [{id, type, task, reporter, timestamp (ISO 8601), details: {<fields>}}]

SELF-VALIDATION GATES (verify before adding each task):
| Gate | Requirement |
|------|-------------|
| Spec reference | Each task must cite spec |
| Success criteria | Each task must have falsifiable done |
| Scope boundary | IN scope stated (functional area, not file names) |
| Dependency check | Dependencies stated if any |
| TDD inclusion | Code tasks include tests |

FIELD FORMAT GUIDELINES:
- done: observable behavior, specific, falsifiable. Bad: "works correctly". Good: "GET /users returns 200"
- spec: path to spec optionally with #anchor

TASK CREATION ORDER:
When adding multiple tasks with dependencies, create them in topological order — dependency-free tasks first, then tasks that depend on them. liza add-tasks validates that all `+"`depends`"+` IDs already exist; creating a task that references a not-yet-created dependency will fail.

ERROR RECOVERY:
On CLI command errors, diagnose the root cause before retrying. Read the error message, investigate the constraint that failed (e.g. missing dependency, invalid state), and fix the underlying issue. Do NOT retry the same call blindly.

MULTIPLE BLOCKED TASKS: Process sequentially by priority (lowest number first), then by timestamp.
Work unit = all planned state changes executed. Do NOT exit until all commands have been run.
`, agentID, agentID, agentID, agentID))

	// Wake instruction is rendered separately by the wake-instructions block
	wakeInstr := fmt.Sprintf("INSTRUCTIONS:\n%s", wakeInstructions)

	return b.String(), wakeInstr, nil
}

func countTasksByStatus(tasks []models.Task, status models.TaskStatus) int {
	count := 0
	for _, task := range tasks {
		if task.Status == status {
			count++
		}
	}
	return count
}

// countHypothesisExhausted counts non-terminal tasks that have been failed by 2+ reviewers.
func countHypothesisExhausted(tasks []models.Task) int {
	count := 0
	for _, task := range tasks {
		if len(task.FailedBy) >= 2 && !task.Status.IsTerminal() {
			count++
		}
	}
	return count
}

// countImmediateDiscoveries counts unresolved discoveries with "immediate" urgency.
func countImmediateDiscoveries(discovered []models.Discovery) int {
	count := 0
	for _, disc := range discovered {
		if disc.Urgency == "immediate" && disc.ConvertedToTask == nil {
			count++
		}
	}
	return count
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

// countReadyM2OCohorts delegates to ops.CountReadyManyToOneCohorts.
func countReadyM2OCohorts(state *models.State, m2oTransitions []ops.ManyToOneTransitionInfo) int {
	return ops.CountReadyManyToOneCohorts(state, m2oTransitions)
}

// hasIntegrationTaskInSprint checks if any planned task uses the integration-pair role-pair.
func hasIntegrationTaskInSprint(state *models.State) bool {
	for _, taskID := range state.Sprint.Scope.Planned {
		task := state.FindTask(taskID)
		if task != nil && task.RolePair == "integration-pair" {
			return true
		}
	}
	return false
}

// BuildRoleContext assembles role-specific context by rendering the named template
// blocks in order and concatenating their output. Each block is a modular .tmpl file
// in templates/blocks/ that receives a unified RoleContextData.
//
// Block boundaries are normalized here rather than relying on template whitespace
// control ({{- -}} trimming), which is fragile and linter-hostile. Each non-empty
// block is TrimSpace'd and joined with a blank-line separator.
func BuildRoleContext(role string, sectionNames []string, data *RoleContextData) (string, error) {
	var blocks []string
	for _, section := range sectionNames {
		var sectionBuf bytes.Buffer
		if err := blockTmpl.ExecuteTemplate(&sectionBuf, section, data); err != nil {
			return "", fmt.Errorf("block template %q for role %q: %w", section, role, err)
		}
		rendered := strings.TrimSpace(sectionBuf.String())
		if rendered == "" {
			continue
		}
		blocks = append(blocks, rendered)
	}
	if len(blocks) == 0 {
		return "", nil
	}
	// Leading \n\n separates from base prompt; \n\n between blocks = one blank line.
	return "\n\n" + strings.Join(blocks, "\n\n") + "\n", nil
}
