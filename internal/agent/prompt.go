package agent

import (
	"fmt"
	"path/filepath"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/precommit"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/roles"
)

// baseConfigFrom constructs the BasePromptConfig shared by all roles.
func baseConfigFrom(state *models.State, config SupervisorConfig, taskID string) prompts.BasePromptConfig {
	return prompts.BasePromptConfig{
		Role:        config.Role,
		AgentID:     config.AgentID,
		TaskID:      taskID,
		SpecsDir:    config.SpecsDir,
		ProjectRoot: config.ProjectRoot,
		StatePath:   config.StatePath,
		GoalDesc:    state.Goal.Description,
		GoalSpecRef: state.Goal.SpecRef,
	}
}

// buildPromptWithContext builds a complete prompt for any task-based role:
// base prompt + task lookup + role-specific context via BuildRoleContext + InitialTask suffix.
func buildPromptWithContext(state *models.State, config SupervisorConfig, taskID string, resolver *pipeline.Resolver) (string, error) {
	prompt, err := prompts.BuildBasePrompt(baseConfigFrom(state, config, taskID))
	if err != nil {
		return "", fmt.Errorf("building base prompt: %w", err)
	}

	task := state.FindTask(taskID)
	if task == nil {
		return "", &errors.NotFoundError{Entity: "task", ID: taskID}
	}

	sections, err := resolver.ContextSections(config.Role)
	if err != nil {
		return "", fmt.Errorf("context sections for role %q: %w", config.Role, err)
	}

	data, err := buildTaskRoleContextData(task, state, config, resolver)
	if err != nil {
		return "", err
	}

	context, err := prompts.BuildRoleContext(config.Role, sections, data)
	if err != nil {
		return "", err
	}
	prompt += context

	if config.InitialTask != "" {
		prompt += fmt.Sprintf("\n\n=== RESUME CONTEXT ===\nResuming task: %s\n", config.InitialTask)
	}

	return prompt, nil
}

// buildOrchestratorPromptContext builds the complete prompt for the orchestrator role.
// Unlike task-based roles, the orchestrator has no task to look up. Dashboard and wake
// instruction content is pre-rendered and passed through block templates.
func buildOrchestratorPromptContext(state *models.State, config SupervisorConfig, resolver *pipeline.Resolver) (string, error) {
	prompt, err := prompts.BuildBasePrompt(baseConfigFrom(state, config, ""))
	if err != nil {
		return "", fmt.Errorf("building base prompt: %w", err)
	}

	sections, err := resolver.ContextSections(config.Role)
	if err != nil {
		return "", fmt.Errorf("context sections for role %q: %w", config.Role, err)
	}

	dashboard, wakeInstruction, err := prompts.RenderOrchestratorDashboard(state, config.ProjectRoot, config.AgentID)
	if err != nil {
		return "", err
	}

	skills, _ := resolver.Skills(config.Role)
	mandatoryDocs, _ := resolver.MandatoryDocs(config.Role)

	data := &prompts.RoleContextData{
		Role:            config.Role,
		AgentID:         config.AgentID,
		RoleType:        "orchestrator",
		DashboardOutput: dashboard,
		WakeInstruction: wakeInstruction,
		ProjectRoot:     config.ProjectRoot,
		StatePath:       config.StatePath,
		SpecsDir:        config.SpecsDir,
		GoalDesc:        state.Goal.Description,
		Skills:          skills,
		MandatoryDocs:   mandatoryDocs,
	}

	context, err := prompts.BuildRoleContext(config.Role, sections, data)
	if err != nil {
		return "", err
	}
	prompt += context

	if config.InitialTask != "" {
		prompt += fmt.Sprintf("\n\n=== RESUME CONTEXT ===\nResuming task: %s\n", config.InitialTask)
	}

	return prompt, nil
}

// buildTaskRoleContextData constructs RoleContextData for task-based roles (doers and reviewers).
func buildTaskRoleContextData(task *models.Task, state *models.State, config SupervisorConfig, resolver *pipeline.Resolver) (*prompts.RoleContextData, error) {
	roleType, _ := resolver.RoleType(config.Role)

	siblingTasks, totalPlanTasks, taskOrdinal := collectSiblingTasks(state, task.ID)

	data := &prompts.RoleContextData{
		// Identity
		Role:     config.Role,
		AgentID:  config.AgentID,
		RoleType: roleType,

		// Task
		TaskID:       task.ID,
		Description:  task.Description,
		DoneWhen:     task.DoneWhen,
		Scope:        task.Scope,
		SpecRef:      task.SpecRef,
		EpicRef:      worktreeRelPath(paths.SplitRefFile(task.EpicRef), resolveWorktreePath(config.ProjectRoot, task.Worktree)),
		EpicSection:  paths.SplitRefFragment(task.EpicRef),
		EpicSlug:     paths.GoalSlug(paths.SplitRefFile(task.EpicRef)),
		PlanRef:      worktreeRelPath(paths.SplitRefFile(task.PlanRef), resolveWorktreePath(config.ProjectRoot, task.Worktree)),
		PlanSection:  paths.SplitRefFragment(task.PlanRef),
		ArchRef:      worktreeRelPath(task.ArchRef, resolveWorktreePath(config.ProjectRoot, task.Worktree)),
		Worktree:     resolveWorktreePath(config.ProjectRoot, task.Worktree),
		IterationNum: task.Iteration,
		AttemptNum:   task.EffectiveAttempt(),

		// Plan scoping
		GoalSpecRef:    state.Goal.SpecRef,
		SiblingTasks:   siblingTasks,
		TotalPlanTasks: totalPlanTasks,
		TaskOrdinal:    taskOrdinal,
		DependsOn:      task.DependsOn,
		TaskRolePair:   task.RolePair,

		// Config/state
		ProjectRoot: config.ProjectRoot,
		StatePath:   config.StatePath,
		SpecsDir:    config.SpecsDir,
		GoalDesc:    state.Goal.Description,
		GoalSlug:    paths.GoalSlug(state.Goal.SpecRef),
	}

	// Prior rejection
	if task.Iteration > 1 && task.RejectionReason != nil && *task.RejectionReason != "" && *task.RejectionReason != "null" {
		data.PriorRejection = *task.RejectionReason
	}

	// Prior attempt outcome (attempt 2 only)
	if data.AttemptNum == 2 {
		for i := len(task.History) - 1; i >= 0; i-- {
			if task.History[i].Event == models.TaskEventNewAttempt && task.History[i].Reason != nil {
				data.PriorAttemptOutcome = *task.History[i].Reason
				if task.History[i].Note != nil {
					data.PriorAttemptRejection = *task.History[i].Note
				}
				break
			}
		}
	}

	// Doer-specific: coder fields
	if roleType == "doer" && config.Role == "coder" {
		data.IntegrationBranch = state.Config.IntegrationBranch
		data.IntegrationFix = task.IntegrationFix
		// Find the last context_exhaustion HandoffEvent for resume context
		for i := len(task.HandoffEvents) - 1; i >= 0; i-- {
			if task.HandoffEvents[i].Trigger == models.HandoffTriggerContextExhaustion {
				evt := task.HandoffEvents[i]
				data.HandoffNote = &evt
				break
			}
		}
	}

	// Reviewer-specific fields
	if roleType == "reviewer" {
		data.BaseCommit = derefString(task.BaseCommit)
		data.ReviewCommit = derefString(task.ReviewCommit)
		data.AssignedTo = derefString(task.AssignedTo)
		if task.AssignedTo != nil {
			data.ScopeExtensions = ops.GetLatestScopeExtensions(task.History, *task.AssignedTo)
			data.ValidationPlan = ops.GetValidationPlan(task.History, *task.AssignedTo)
		}
	}

	// Integration-specific: branch context for analyst and reviewer
	if config.Role == roles.IntegrationAnalyst || config.Role == roles.IntegrationReviewer {
		if state.Goal.BaseCommit != nil {
			data.GoalBaseCommit = *state.Goal.BaseCommit
		}
		data.CompletedTasks = collectCompletedTasks(state)
	}

	// Declarative fields from pipeline YAML
	if skills, err := resolver.Skills(config.Role); err == nil {
		data.Skills = skills
	}
	if mandatoryDocs, err := resolver.MandatoryDocs(config.Role); err == nil {
		data.MandatoryDocs = mandatoryDocs
	}

	// Architect-specific: parent task contexts
	if config.Role == roles.Architect {
		for _, parentID := range task.EffectiveParentTasks() {
			parent := state.FindTask(parentID)
			if parent != nil {
				data.ParentTaskContexts = append(data.ParentTaskContexts, prompts.ParentTaskContext{
					ID:          parent.ID,
					Description: truncateDescription(parent.Description, 500),
					DoneWhen:    parent.DoneWhen,
					SpecRef:     parent.SpecRef,
					EpicRef:     parent.EpicRef,
					PlanRef:     parent.PlanRef,
				})
			}
		}
	}

	// Architect-specific: pre-commit bootstrap context
	if config.Role == roles.Architect {
		exists, err := precommit.ConfigExistsOnIntegration(config.ProjectRoot, state.Config.IntegrationBranch)
		if err != nil {
			return nil, fmt.Errorf("precommit config check: %w", err)
		}
		data.PreCommitConfigExists = exists
		data.PreCommitBootstrapInFlight = precommit.BootstrapInFlight(state)
		data.PreCommitKind = precommit.Kind
	}

	return data, nil
}

// collectCompletedTasks returns summaries of all MERGED tasks for integration context.
func collectCompletedTasks(state *models.State) []prompts.CompletedTaskSummary {
	var tasks []prompts.CompletedTaskSummary
	for _, t := range state.Tasks {
		if t.Status == models.TaskStatusMerged {
			tasks = append(tasks, prompts.CompletedTaskSummary{
				ID:          t.ID,
				Description: truncateDescription(t.Description, 200),
				DoneWhen:    t.DoneWhen,
				SpecRef:     t.SpecRef,
			})
		}
	}
	return tasks
}

// resolveWorktreePath returns the absolute worktree path, or "" if worktree is nil.
func resolveWorktreePath(projectRoot string, worktree *string) string {
	if worktree == nil {
		return ""
	}
	return fmt.Sprintf("%s/%s", projectRoot, *worktree)
}

// derefString returns the value pointed to by s, or "" if s is nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// collectSiblingTasks returns summaries of sibling tasks in the sprint plan (excluding currentTaskID),
// the total count of planned tasks, and the 1-based ordinal position of currentTaskID in the plan.
// Returns nil, 0, 0 if no planned tasks or if currentTaskID is not in the planned list
// (e.g. mid-sprint replacement tasks created outside the original plan).
//
// Note: tasks not found by FindTask are silently skipped. This assumes the orchestrator keeps
// Sprint.Scope.Planned in sync with the task list (archived/removed tasks are pruned from planned[]).
func collectSiblingTasks(state *models.State, currentTaskID string) ([]prompts.SiblingTaskSummary, int, int) {
	planned := state.Sprint.Scope.Planned
	if len(planned) == 0 {
		return nil, 0, 0
	}

	ordinal := 0
	var siblings []prompts.SiblingTaskSummary
	for i, id := range planned {
		if id == currentTaskID {
			ordinal = i + 1 // 1-based
			continue
		}
		task := state.FindTask(id)
		if task != nil {
			siblings = append(siblings, prompts.SiblingTaskSummary{
				ID:          task.ID,
				Description: truncateDescription(task.Description, 200),
				Status:      string(task.Status),
				PlanRef:     task.PlanRef,
				RolePair:    task.RolePair,
			})
		}
	}

	// Suppress scoping for tasks not in the plan (mid-sprint replacements).
	// Returning 0 for totalPlanTasks ensures the template condition is false.
	if ordinal == 0 {
		return nil, 0, 0
	}

	return siblings, len(planned), ordinal
}

// worktreeRelPath prefixes a relative path with the worktree path so agents
// resolve it inside the worktree rather than the main repo.
func worktreeRelPath(relPath, worktree string) string {
	if worktree == "" || relPath == "" {
		return relPath
	}
	return filepath.Join(worktree, relPath)
}

// truncateDescription shortens s to maxLen characters, appending "…" if truncated.
func truncateDescription(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

func savePrompt(promptDir, agentID, prompt string) (string, error) {
	return saveTimestampedFile(promptDir, agentID, "txt", prompt)
}
