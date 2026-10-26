package commands

import (
	stderrors "errors"
	"fmt"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
)

// inspectTasksOptions contains options for task inspection
type inspectTasksOptions struct {
	Format           inspectFormat // Output format: json, yaml, table, value
	StatusFilter     string        // Filter by status
	AssignedToFilter string        // Filter by assignee
	BlockedFilter    bool          // Show only blocked tasks
	Internal         bool          // Return structured data for composition
	Summary          bool          // Return compact task summaries
	OutputSummary    bool          // Return compact output entry summaries
	Active           bool          // Show only non-terminal tasks
	Fields           []string      // Requested task fields
	ProjectRoot      string        // Project root, used for filesystem-aware diagnostics
	PipelineResolver models.PipelineResolver
	State            *models.State
	HandoffDomain    *ops.PlanHandoffDomain
}

// inspectTasks lists all tasks or filters by criteria
func inspectTasks(state *models.State, opts inspectTasksOptions) (any, error) {
	opts.State = state
	waits, err := inspectionDependencyWaits(state, &opts)
	if err != nil {
		return nil, err
	}
	filtered, err := filterInspectionTasks(state.Tasks, opts)
	if err != nil {
		return nil, err
	}
	if len(opts.Fields) > 0 {
		// Validate the schema even when filters (or an empty run) yield no tasks.
		if _, err := projectTaskFields(opts.ProjectRoot, &models.Task{}, opts.Fields); err != nil {
			return nil, err
		}
		projection := make([]map[string]any, 0, len(filtered))
		for i := range filtered {
			fields, err := projectTaskFields(opts.ProjectRoot, &filtered[i], opts.Fields)
			if err != nil {
				return nil, err
			}
			projection = append(projection, fields)
		}
		if opts.Internal {
			return projection, nil
		}
		return formatOutput(projection, opts.Format)
	}

	if opts.OutputSummary {
		summaries := make([]taskOutputSummaryInfo, len(filtered))
		for i, task := range filtered {
			summaries[i] = buildTaskOutputSummaryInfo(&task)
		}
		if opts.Internal {
			return summaries, nil
		}
		return formatTaskOutputSummariesOutput(summaries, opts.Format)
	}

	if opts.Summary {
		summaries := make([]taskSummaryInfo, len(filtered))
		for i := range filtered {
			summaries[i] = buildTaskSummaryInfo(&filtered[i])
			summaries[i].WaitingOn = buildDependencyWaitInfo(waits, &filtered[i])
		}
		if opts.Internal {
			return summaries, nil
		}
		return formatTasksSummaryOutput(summaries, opts.Format)
	}

	taskInfos := make([]taskInfo, len(filtered))
	for i := range filtered {
		task, err := restoreArchivedForOutput(&filtered[i], opts)
		if err != nil {
			return nil, err
		}
		taskInfos[i] = buildTaskInfo(task, opts.ProjectRoot)
		taskInfos[i].WaitingOn = buildDependencyWaitInfo(waits, &filtered[i])
	}

	if opts.Internal {
		return taskInfos, nil
	}
	return formatTasksOutput(taskInfos, opts.Format)
}

// inspectTask shows details for a single task
func inspectTask(state *models.State, taskID string, opts inspectTasksOptions) (any, error) {
	foundTask := state.FindTask(taskID)
	if foundTask == nil {
		return nil, &errors.NotFoundError{Entity: string(inspectRootTask), ID: taskID}
	}
	if len(opts.Fields) > 0 {
		projection, err := projectTaskFields(opts.ProjectRoot, foundTask, opts.Fields)
		if err != nil {
			return nil, err
		}
		filtered, err := filterInspectionTasks([]models.Task{*foundTask}, opts)
		if err != nil {
			return nil, err
		}
		var result any
		if len(filtered) > 0 {
			result = projection
		}
		if opts.Internal {
			return result, nil
		}
		return formatOutput(result, opts.Format)
	}

	waits, err := inspectionDependencyWaits(state, &opts)
	if err != nil {
		return nil, err
	}
	if opts.Summary {
		info := buildTaskSummaryInfo(foundTask)
		info.WaitingOn = buildDependencyWaitInfo(waits, foundTask)
		if opts.Internal {
			return info, nil
		}
		return formatTaskSummaryOutput(info, opts.Format)
	}

	if opts.OutputSummary {
		info := buildTaskOutputSummaryInfo(foundTask)
		if opts.Internal {
			return info, nil
		}
		return formatTaskOutputSummaryOutput(info, opts.Format)
	}

	restored, err := restoreArchivedForOutput(foundTask, opts)
	if err != nil {
		return nil, err
	}
	info := buildTaskInfo(restored, opts.ProjectRoot)
	info.WaitingOn = buildDependencyWaitInfo(waits, foundTask)
	if opts.Internal {
		return info, nil
	}
	return formatTaskOutput(info, opts.Format)
}

func filterInspectionTasks(tasks []models.Task, opts inspectTasksOptions) ([]models.Task, error) {
	if opts.Active && opts.PipelineResolver == nil && opts.ProjectRoot != "" {
		cfg, err := pipeline.LoadFrozen(opts.ProjectRoot)
		if err != nil {
			return nil, fmt.Errorf("load pipeline for active task filtering: %w", err)
		}
		opts.PipelineResolver = pipeline.NewResolver(cfg)
	}
	if opts.Active {
		if resolver, ok := opts.PipelineResolver.(*pipeline.Resolver); ok {
			domain := ops.NewPlanHandoffDomain(resolver)
			opts.HandoffDomain = &domain
		}
	}
	return filterTasks(tasks, opts), nil
}

// inspectionDependencyWaits returns one wait resolver per inspection call for
// full and summary views that carry structured output. It stores a loaded
// pipeline in opts so active filtering reuses it. Without a project root or a
// workspace pipeline it returns nil: waits are unavailable, not satisfied.
func inspectionDependencyWaits(state *models.State, opts *inspectTasksOptions) (*models.DependencyWaitResolver, error) {
	structured := opts.Internal || opts.Format.isStructured()
	if !structured || len(opts.Fields) > 0 || opts.OutputSummary {
		return nil, nil
	}
	if opts.PipelineResolver == nil {
		if opts.ProjectRoot == "" {
			return nil, nil
		}
		cfg, err := pipeline.LoadFrozen(opts.ProjectRoot)
		if stderrors.Is(err, pipeline.ErrConfigNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("load pipeline for dependency waits: %w", err)
		}
		opts.PipelineResolver = pipeline.NewResolver(cfg)
	}
	return models.NewDependencyWaitResolver(state, opts.PipelineResolver), nil
}

// filterTasks applies filters to task list
func filterTasks(tasks []models.Task, opts inspectTasksOptions) []models.Task {
	var filtered []models.Task

	for _, task := range tasks {
		if opts.Active && !taskVisibleInActiveList(&task, opts) {
			continue
		}
		if opts.StatusFilter != "" && string(task.Status) != opts.StatusFilter {
			continue
		}
		if opts.AssignedToFilter != "" {
			if task.AssignedTo == nil || *task.AssignedTo != opts.AssignedToFilter {
				continue
			}
		}
		if opts.BlockedFilter {
			if task.Status != models.TaskStatusBlocked {
				continue
			}
		}

		filtered = append(filtered, task)
	}

	return filtered
}

// Inspection visibility includes repairable handoffs, even when an operator
// hold or an undecided disposition prevents waking work. Claim semantics stay
// with IsOperationallyTerminal; this policy never makes a task assignable.
func taskVisibleInActiveList(task *models.Task, opts inspectTasksOptions) bool {
	if !models.IsOperationallyTerminal(task, opts.PipelineResolver) {
		return true
	}
	if task.Status != models.TaskStatusMerged || task.TransitionsExecuted[models.ReplannedTransition] || task.PlanHandoffRetired() {
		return false
	}
	if task.AmendsPlan != "" {
		if opts.State == nil {
			return false
		}
		original := opts.State.FindTask(task.AmendsPlan)
		return original != nil && original.PlanAmendment != nil && original.PlanAmendment.Pending == task.ID &&
			original.Status == models.TaskStatusMerged && !original.TransitionsExecuted[models.ReplannedTransition] && !original.PlanHandoffRetired()
	}
	if task.PlanAmendment != nil && task.PlanAmendment.Pending != "" {
		return true
	}
	return opts.HandoffDomain != nil && opts.HandoffDomain.Pending(task)
}
