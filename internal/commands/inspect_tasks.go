package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/render"
)

// inspectTasksOptions contains options for task inspection
type inspectTasksOptions struct {
	Format           string // Output format: json, yaml, table, value
	StatusFilter     string // Filter by status
	AssignedToFilter string // Filter by assignee
	BlockedFilter    bool   // Show only blocked tasks
	Internal         bool   // Return structured data for composition
	Summary          bool   // Return compact task summaries
	OutputSummary    bool   // Return compact output entry summaries
	Active           bool   // Show only non-terminal tasks
	ProjectRoot      string // Project root, used for filesystem-aware diagnostics
}

// taskInfo represents task information with computed fields
type taskInfo struct {
	ID                 string                        `json:"id" yaml:"id"`
	Description        string                        `json:"description" yaml:"description"`
	Status             string                        `json:"status" yaml:"status"`
	Priority           int                           `json:"priority" yaml:"priority"`
	AssignedTo         *string                       `json:"assigned_to,omitempty" yaml:"assigned_to,omitempty"`
	ReviewingBy        *string                       `json:"reviewing_by,omitempty" yaml:"reviewing_by,omitempty"`
	DependsOn          []string                      `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	Age                string                        `json:"age" yaml:"age"`                       // Computed: time since created
	TimeInStatus       string                        `json:"time_in_status" yaml:"time_in_status"` // Computed: time in current status
	BlockedReason      *string                       `json:"blocked_reason,omitempty" yaml:"blocked_reason,omitempty"`
	BlockedQuestions   []string                      `json:"blocked_questions,omitempty" yaml:"blocked_questions,omitempty"`
	RepairRequest      *models.RepairRequest         `json:"repair_request,omitempty" yaml:"repair_request,omitempty"`
	Iteration          int                           `json:"iteration,omitempty" yaml:"iteration,omitempty"`
	ReviewCycles       int                           `json:"review_cycles,omitempty" yaml:"review_cycles,omitempty"`
	LeaseExpires       *string                       `json:"lease_expires,omitempty" yaml:"lease_expires,omitempty"`
	Worktree           *string                       `json:"worktree,omitempty" yaml:"worktree,omitempty"`
	DoneWhen           string                        `json:"done_when,omitempty" yaml:"done_when,omitempty"`
	Scope              string                        `json:"scope,omitempty" yaml:"scope,omitempty"`
	SpecRef            string                        `json:"spec_ref,omitempty" yaml:"spec_ref,omitempty"`
	MergeCommit        *string                       `json:"merge_commit,omitempty" yaml:"merge_commit,omitempty"`
	PRURL              *string                       `json:"pr_url,omitempty" yaml:"pr_url,omitempty"`
	RejectionReason    *string                       `json:"rejection_reason,omitempty" yaml:"rejection_reason,omitempty"`
	IntegrationFailure map[string]any                `json:"integration_failure,omitempty" yaml:"integration_failure,omitempty"`
	Output             []models.OutputEntry          `json:"output,omitempty" yaml:"output,omitempty"`
	Decomposition      *models.DecompositionManifest `json:"decomposition,omitempty" yaml:"decomposition,omitempty"`
	AttemptNum         int                           `json:"attempt_num,omitempty" yaml:"attempt_num,omitempty"`
}

// taskSummaryInfo is a compact task projection for agent orchestration.
type taskSummaryInfo struct {
	ID               string                `json:"id" yaml:"id"`
	Status           string                `json:"status" yaml:"status"`
	RolePair         string                `json:"role_pair,omitempty" yaml:"role_pair,omitempty"`
	Priority         int                   `json:"priority" yaml:"priority"`
	AssignedTo       *string               `json:"assigned_to,omitempty" yaml:"assigned_to,omitempty"`
	ReviewingBy      *string               `json:"reviewing_by,omitempty" yaml:"reviewing_by,omitempty"`
	DependsOn        []string              `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	Attempt          int                   `json:"attempt" yaml:"attempt"`
	ReviewCycles     int                   `json:"review_cycles,omitempty" yaml:"review_cycles,omitempty"`
	LeaseExpires     *string               `json:"lease_expires,omitempty" yaml:"lease_expires,omitempty"`
	BlockedReason    *string               `json:"blocked_reason,omitempty" yaml:"blocked_reason,omitempty"`
	BlockedQuestions []string              `json:"blocked_questions,omitempty" yaml:"blocked_questions,omitempty"`
	RepairRequest    *models.RepairRequest `json:"repair_request,omitempty" yaml:"repair_request,omitempty"`
	RejectionReason  *string               `json:"rejection_reason,omitempty" yaml:"rejection_reason,omitempty"`
	FailedBy         []string              `json:"failed_by,omitempty" yaml:"failed_by,omitempty"`
	OutputCount      int                   `json:"output_count,omitempty" yaml:"output_count,omitempty"`
	OutputKinds      []string              `json:"output_kinds,omitempty" yaml:"output_kinds,omitempty"`
}

// taskOutputSummaryInfo is a compact projection of output[] for downstream orientation.
type taskOutputSummaryInfo struct {
	ID       string                   `json:"id" yaml:"id"`
	Status   string                   `json:"status" yaml:"status"`
	RolePair string                   `json:"role_pair,omitempty" yaml:"role_pair,omitempty"`
	Output   []outputEntrySummaryInfo `json:"output" yaml:"output"`
}

type outputEntrySummaryInfo struct {
	Index         int                           `json:"index" yaml:"index"`
	Desc          string                        `json:"desc,omitempty" yaml:"desc,omitempty"`
	Kind          string                        `json:"kind,omitempty" yaml:"kind,omitempty"`
	SpecRef       string                        `json:"spec_ref,omitempty" yaml:"spec_ref,omitempty"`
	EpicRef       string                        `json:"epic_ref,omitempty" yaml:"epic_ref,omitempty"`
	PlanRef       string                        `json:"plan_ref,omitempty" yaml:"plan_ref,omitempty"`
	ArchRef       string                        `json:"arch_ref,omitempty" yaml:"arch_ref,omitempty"`
	DependsOn     []string                      `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	TaskDependsOn []string                      `json:"task_depends_on,omitempty" yaml:"task_depends_on,omitempty"`
	Decomposition *models.DecompositionManifest `json:"decomposition,omitempty" yaml:"decomposition,omitempty"`
}

// inspectTasks lists all tasks or filters by criteria
func inspectTasks(state *models.State, opts inspectTasksOptions) (any, error) {
	filtered := filterTasks(state.Tasks, opts)

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
		for i, task := range filtered {
			summaries[i] = buildTaskSummaryInfo(&task)
		}
		if opts.Internal {
			return summaries, nil
		}
		return formatTasksSummaryOutput(summaries, opts.Format)
	}

	taskInfos := make([]taskInfo, len(filtered))
	for i, task := range filtered {
		taskInfos[i] = buildTaskInfo(&task, opts.ProjectRoot)
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
		return nil, &errors.NotFoundError{Entity: "task", ID: taskID}
	}

	if opts.Summary {
		info := buildTaskSummaryInfo(foundTask)
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

	info := buildTaskInfo(foundTask, opts.ProjectRoot)
	if opts.Internal {
		return info, nil
	}
	return formatTaskOutput(info, opts.Format)
}

// buildTaskInfo converts a Task to taskInfo with computed fields
func buildTaskInfo(task *models.Task, projectRoot string) taskInfo {
	info := taskInfo{
		ID:                 task.ID,
		Description:        task.Description,
		Status:             string(task.Status),
		Priority:           task.Priority,
		AssignedTo:         task.AssignedTo,
		ReviewingBy:        task.ReviewingBy,
		DependsOn:          task.DependsOn,
		BlockedReason:      task.BlockedReason,
		BlockedQuestions:   task.BlockedQuestions,
		RepairRequest:      task.RepairRequest,
		Iteration:          task.Iteration,
		ReviewCycles:       task.ReviewCyclesCurrent,
		Worktree:           task.Worktree,
		DoneWhen:           task.DoneWhen,
		Scope:              task.Scope,
		SpecRef:            task.SpecRef,
		MergeCommit:        task.MergeCommit,
		PRURL:              latestIntegrationPRURL(task),
		RejectionReason:    task.RejectionReason,
		IntegrationFailure: latestIntegrationFailureDiagnostic(task, projectRoot),
		Output:             task.Output,
		Decomposition:      task.Decomposition,
		AttemptNum:         task.EffectiveAttempt(),
	}

	info.Age = render.FormatDuration(calculateTaskAge(task))
	info.TimeInStatus = render.FormatDuration(calculateTimeInStatus(task))

	if task.LeaseExpires != nil {
		remaining := time.Until(*task.LeaseExpires)
		formatted := render.FormatDuration(remaining)
		info.LeaseExpires = &formatted
	}

	return info
}

func buildTaskSummaryInfo(task *models.Task) taskSummaryInfo {
	info := taskSummaryInfo{
		ID:               task.ID,
		Status:           string(task.Status),
		RolePair:         task.RolePair,
		Priority:         task.Priority,
		AssignedTo:       task.AssignedTo,
		ReviewingBy:      task.ReviewingBy,
		DependsOn:        task.DependsOn,
		Attempt:          task.EffectiveAttempt(),
		ReviewCycles:     task.ReviewCyclesCurrent,
		BlockedReason:    task.BlockedReason,
		BlockedQuestions: task.BlockedQuestions,
		RepairRequest:    task.RepairRequest,
		RejectionReason:  task.RejectionReason,
		FailedBy:         task.FailedBy,
		OutputCount:      len(task.Output),
		OutputKinds:      outputKinds(task.Output),
	}

	if task.LeaseExpires != nil {
		remaining := time.Until(*task.LeaseExpires)
		formatted := render.FormatDuration(remaining)
		info.LeaseExpires = &formatted
	}

	return info
}

func buildTaskOutputSummaryInfo(task *models.Task) taskOutputSummaryInfo {
	info := taskOutputSummaryInfo{
		ID:       task.ID,
		Status:   string(task.Status),
		RolePair: task.RolePair,
		Output:   make([]outputEntrySummaryInfo, 0, len(task.Output)),
	}

	for i, entry := range task.Output {
		info.Output = append(info.Output, outputEntrySummaryInfo{
			Index:         i,
			Desc:          entry.Desc,
			Kind:          entry.Kind,
			SpecRef:       entry.SpecRef,
			EpicRef:       entry.EpicRef,
			PlanRef:       entry.PlanRef,
			ArchRef:       entry.ArchRef,
			DependsOn:     entry.DependsOn,
			TaskDependsOn: entry.TaskDependsOn,
			Decomposition: entry.Decomposition,
		})
	}

	return info
}

func outputKinds(output []models.OutputEntry) []string {
	kinds := make([]string, 0, len(output))
	seen := make(map[string]bool)
	for _, entry := range output {
		if entry.Kind == "" || seen[entry.Kind] {
			continue
		}
		seen[entry.Kind] = true
		kinds = append(kinds, entry.Kind)
	}
	return kinds
}

// calculateTimeInStatus calculates how long the task has been in its current status
func calculateTimeInStatus(task *models.Task) time.Duration {
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		switch entry.Event {
		case models.TaskEventClaimed, models.TaskEventSubmittedForReview, models.TaskEventRejected, models.TaskEventApproved,
			models.TaskEventMerged, models.TaskEventBlocked, models.TaskEventAbandoned, models.TaskEventSuperseded, models.TaskEventIntegrationFailed:
			return time.Since(entry.Time)
		}
	}

	return time.Since(task.Created)
}

// filterTasks applies filters to task list
func filterTasks(tasks []models.Task, opts inspectTasksOptions) []models.Task {
	var filtered []models.Task

	for _, task := range tasks {
		if opts.Active && task.Status.IsTerminal() {
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

func formatTasksSummaryOutput(tasks []taskSummaryInfo, format string) (string, error) {
	if format == "" {
		format = "table"
	}

	switch format {
	case "json":
		return render.FormatJSON(tasks)
	case "yaml":
		return render.FormatYAML(tasks)
	case "table":
		return formatTaskSummariesTable(tasks), nil
	case "value":
		return "", fmt.Errorf("value format not supported for task summaries (use json, yaml, or table)")
	default:
		return "", fmt.Errorf("invalid format: %s", format)
	}
}

func formatTaskOutputSummariesOutput(tasks []taskOutputSummaryInfo, format string) (string, error) {
	if format == "" {
		format = "table"
	}

	switch format {
	case "json":
		return render.FormatJSON(tasks)
	case "yaml":
		return render.FormatYAML(tasks)
	case "table":
		return formatTaskOutputSummariesTable(tasks), nil
	case "value":
		return "", fmt.Errorf("value format not supported for task output summaries (use json, yaml, or table)")
	default:
		return "", fmt.Errorf("invalid format: %s", format)
	}
}

func formatTaskSummaryOutput(task taskSummaryInfo, format string) (string, error) {
	if format == "" {
		format = "value"
	}

	switch format {
	case "json":
		return render.FormatJSON(task)
	case "yaml":
		return render.FormatYAML(task)
	case "value":
		return formatTaskSummaryValue(task), nil
	case "table":
		return formatTaskSummariesTable([]taskSummaryInfo{task}), nil
	default:
		return "", fmt.Errorf("invalid format: %s", format)
	}
}

func formatTaskOutputSummaryOutput(task taskOutputSummaryInfo, format string) (string, error) {
	if format == "" {
		format = "value"
	}

	switch format {
	case "json":
		return render.FormatJSON(task)
	case "yaml":
		return render.FormatYAML(task)
	case "value":
		return formatTaskOutputSummaryValue(task), nil
	case "table":
		return formatTaskOutputSummariesTable([]taskOutputSummaryInfo{task}), nil
	default:
		return "", fmt.Errorf("invalid format: %s", format)
	}
}

func formatTaskSummariesTable(tasks []taskSummaryInfo) string {
	if len(tasks) == 0 {
		return "No tasks found"
	}

	headers := []string{"ID", "STATUS", "ROLE_PAIR", "ATTEMPT", "PRIORITY", "ASSIGNED_TO", "REVIEWING_BY", "DEPS", "OUTPUTS"}
	var rows [][]string
	for _, task := range tasks {
		assignedTo := "-"
		if task.AssignedTo != nil {
			assignedTo = *task.AssignedTo
		}

		reviewingBy := "-"
		if task.ReviewingBy != nil {
			reviewingBy = *task.ReviewingBy
		}

		deps := "-"
		if len(task.DependsOn) > 0 {
			deps = fmt.Sprintf("%d", len(task.DependsOn))
		}

		outputs := "-"
		if task.OutputCount > 0 {
			outputs = fmt.Sprintf("%d", task.OutputCount)
		}

		rows = append(rows, []string{
			task.ID,
			task.Status,
			task.RolePair,
			fmt.Sprintf("%d", task.Attempt),
			fmt.Sprintf("%d", task.Priority),
			assignedTo,
			reviewingBy,
			deps,
			outputs,
		})
	}

	return render.FormatTable(headers, rows)
}

func formatTaskOutputSummariesTable(tasks []taskOutputSummaryInfo) string {
	if len(tasks) == 0 {
		return "No tasks found"
	}

	headers := []string{"ID", "STATUS", "ROLE_PAIR", "INDEX", "KIND", "SPEC_REF", "DESC"}
	var rows [][]string
	for _, task := range tasks {
		if len(task.Output) == 0 {
			rows = append(rows, []string{task.ID, task.Status, task.RolePair, "-", "-", "-", "-"})
			continue
		}
		for _, output := range task.Output {
			desc := output.Desc
			if len(desc) > 50 {
				desc = desc[:47] + "..."
			}
			rows = append(rows, []string{
				task.ID,
				task.Status,
				task.RolePair,
				fmt.Sprintf("%d", output.Index),
				output.Kind,
				output.SpecRef,
				desc,
			})
		}
	}

	return render.FormatTable(headers, rows)
}

func formatTaskSummaryValue(task taskSummaryInfo) string {
	lines := []string{
		fmt.Sprintf("ID: %s", task.ID),
		fmt.Sprintf("Status: %s", task.Status),
		fmt.Sprintf("Role Pair: %s", task.RolePair),
		fmt.Sprintf("Priority: %d", task.Priority),
		fmt.Sprintf("Attempt: %d", task.Attempt),
	}

	if task.AssignedTo != nil {
		lines = append(lines, fmt.Sprintf("Assigned To: %s", *task.AssignedTo))
	} else {
		lines = append(lines, "Assigned To: -")
	}
	if task.ReviewingBy != nil {
		lines = append(lines, fmt.Sprintf("Reviewing By: %s", *task.ReviewingBy))
	} else {
		lines = append(lines, "Reviewing By: -")
	}
	if len(task.DependsOn) > 0 {
		lines = append(lines, fmt.Sprintf("Depends On: %s", strings.Join(task.DependsOn, ", ")))
	}
	if task.BlockedReason != nil {
		lines = append(lines, fmt.Sprintf("Blocked Reason: %s", *task.BlockedReason))
	}
	if task.RejectionReason != nil {
		lines = append(lines, fmt.Sprintf("Rejection Reason: %s", *task.RejectionReason))
	}
	if len(task.FailedBy) > 0 {
		lines = append(lines, fmt.Sprintf("Failed By: %s", strings.Join(task.FailedBy, ", ")))
	}
	if task.OutputCount > 0 {
		lines = append(lines, fmt.Sprintf("Output Count: %d", task.OutputCount))
	}

	return strings.Join(lines, "\n")
}

func formatTaskOutputSummaryValue(task taskOutputSummaryInfo) string {
	lines := []string{
		fmt.Sprintf("ID: %s", task.ID),
		fmt.Sprintf("Status: %s", task.Status),
		fmt.Sprintf("Role Pair: %s", task.RolePair),
	}

	if len(task.Output) == 0 {
		lines = append(lines, "Output: none")
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "Output:")
	for _, output := range task.Output {
		parts := []string{fmt.Sprintf("[%d]", output.Index)}
		if output.Kind != "" {
			parts = append(parts, fmt.Sprintf("kind=%s", output.Kind))
		}
		if output.SpecRef != "" {
			parts = append(parts, fmt.Sprintf("spec_ref=%s", output.SpecRef))
		}
		if output.EpicRef != "" {
			parts = append(parts, fmt.Sprintf("epic_ref=%s", output.EpicRef))
		}
		if output.PlanRef != "" {
			parts = append(parts, fmt.Sprintf("plan_ref=%s", output.PlanRef))
		}
		if output.ArchRef != "" {
			parts = append(parts, fmt.Sprintf("arch_ref=%s", output.ArchRef))
		}
		if len(output.DependsOn) > 0 {
			parts = append(parts, fmt.Sprintf("depends_on=%s", strings.Join(output.DependsOn, ",")))
		}
		if len(output.TaskDependsOn) > 0 {
			parts = append(parts, fmt.Sprintf("task_depends_on=%s", strings.Join(output.TaskDependsOn, ",")))
		}
		if output.Desc != "" {
			parts = append(parts, output.Desc)
		}
		lines = append(lines, "- "+strings.Join(parts, " "))
	}

	return strings.Join(lines, "\n")
}

// formatTasksOutput formats a list of tasks for output
func formatTasksOutput(tasks []taskInfo, format string) (string, error) {
	if format == "" {
		format = "table"
	}

	switch format {
	case "json":
		return render.FormatJSON(tasks)
	case "yaml":
		return render.FormatYAML(tasks)
	case "table":
		return formatTasksTable(tasks), nil
	case "value":
		return "", fmt.Errorf("value format not supported for task lists (use json, yaml, or table)")
	default:
		return "", fmt.Errorf("invalid format: %s", format)
	}
}

// formatTaskOutput formats a single task for output
func formatTaskOutput(task taskInfo, format string) (string, error) {
	if format == "" {
		format = "value"
	}

	switch format {
	case "json":
		return render.FormatJSON(task)
	case "yaml":
		return render.FormatYAML(task)
	case "value":
		return formatTaskValue(task), nil
	case "table":
		return formatTasksTable([]taskInfo{task}), nil
	default:
		return "", fmt.Errorf("invalid format: %s", format)
	}
}

// formatTasksTable formats tasks as a table
func formatTasksTable(tasks []taskInfo) string {
	if len(tasks) == 0 {
		return "No tasks found"
	}

	headers := []string{"ID", "STATUS", "ATTEMPT", "PRIORITY", "ASSIGNED_TO", "REVIEWING_BY", "DEPS", "AGE", "TIME_IN_STATUS", "DESCRIPTION"}
	var rows [][]string

	for _, task := range tasks {
		assignedTo := "-"
		if task.AssignedTo != nil {
			assignedTo = *task.AssignedTo
		}

		reviewingBy := "-"
		if task.ReviewingBy != nil {
			reviewingBy = *task.ReviewingBy
		}

		deps := "-"
		if len(task.DependsOn) > 0 {
			deps = fmt.Sprintf("%d", len(task.DependsOn))
		}

		desc := task.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}

		attempt := fmt.Sprintf("%d.%d", task.AttemptNum, task.Iteration)

		rows = append(rows, []string{
			task.ID,
			task.Status,
			attempt,
			fmt.Sprintf("%d", task.Priority),
			assignedTo,
			reviewingBy,
			deps,
			task.Age,
			task.TimeInStatus,
			desc,
		})
	}

	return render.FormatTable(headers, rows)
}

// formatTaskValue formats a single task as key-value pairs
func formatTaskValue(task taskInfo) string {
	lines := []string{
		fmt.Sprintf("ID: %s", task.ID),
		fmt.Sprintf("Description: %s", task.Description),
		fmt.Sprintf("Status: %s", task.Status),
		fmt.Sprintf("Priority: %d", task.Priority),
	}

	if task.AssignedTo != nil {
		lines = append(lines, fmt.Sprintf("Assigned To: %s", *task.AssignedTo))
	} else {
		lines = append(lines, "Assigned To: -")
	}

	if task.ReviewingBy != nil {
		lines = append(lines, fmt.Sprintf("Reviewing By: %s", *task.ReviewingBy))
	} else {
		lines = append(lines, "Reviewing By: -")
	}

	lines = append(lines, fmt.Sprintf("Age: %s", task.Age))
	lines = append(lines, fmt.Sprintf("Time in Status: %s", task.TimeInStatus))

	if len(task.DependsOn) > 0 {
		lines = append(lines, fmt.Sprintf("Dependencies: %s", strings.Join(task.DependsOn, ", ")))
	} else {
		lines = append(lines, "Dependencies: none")
	}

	if task.BlockedReason != nil {
		lines = append(lines, fmt.Sprintf("Blocked Reason: %s", *task.BlockedReason))
	}

	if task.Iteration > 0 {
		lines = append(lines, fmt.Sprintf("Iteration: %d", task.Iteration))
	}

	if task.ReviewCycles > 0 {
		lines = append(lines, fmt.Sprintf("Review Cycles: %d", task.ReviewCycles))
	}

	if task.LeaseExpires != nil {
		lines = append(lines, fmt.Sprintf("Lease Expires: %s", *task.LeaseExpires))
	}

	if task.Worktree != nil {
		lines = append(lines, fmt.Sprintf("Worktree: %s", *task.Worktree))
	}

	if task.DoneWhen != "" {
		lines = append(lines, fmt.Sprintf("Done When: %s", task.DoneWhen))
	}

	if task.Scope != "" {
		lines = append(lines, fmt.Sprintf("Scope: %s", task.Scope))
	}

	if task.SpecRef != "" {
		lines = append(lines, fmt.Sprintf("Spec Ref: %s", task.SpecRef))
	}

	if task.RejectionReason != nil {
		lines = append(lines, fmt.Sprintf("Rejection Reason: %s", *task.RejectionReason))
	}

	if len(task.Output) > 0 {
		lines = append(lines, fmt.Sprintf("Output: %d entries", len(task.Output)))
	}

	var result strings.Builder
	for _, line := range lines {
		result.WriteString(line)
		result.WriteString("\n")
	}
	return result.String()
}
