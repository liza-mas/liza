package commands

import (
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/render"
)

func formatTasksSummaryOutput(tasks []taskSummaryInfo, format inspectFormat) (string, error) {
	if format == "" {
		format = inspectFormatTable
	}

	switch format {
	case inspectFormatJSON:
		return render.FormatJSON(tasks)
	case inspectFormatYAML:
		return render.FormatYAML(tasks)
	case inspectFormatTable:
		return formatTaskSummariesTable(tasks), nil
	case inspectFormatValue:
		return "", fmt.Errorf("value format not supported for task summaries (use json, yaml, or table)")
	default:
		return "", fmt.Errorf("invalid format: %s", format)
	}
}

func formatTaskOutputSummariesOutput(tasks []taskOutputSummaryInfo, format inspectFormat) (string, error) {
	if format == "" {
		format = inspectFormatTable
	}

	switch format {
	case inspectFormatJSON:
		return render.FormatJSON(tasks)
	case inspectFormatYAML:
		return render.FormatYAML(tasks)
	case inspectFormatTable:
		return formatTaskOutputSummariesTable(tasks), nil
	case inspectFormatValue:
		return "", fmt.Errorf("value format not supported for task output summaries (use json, yaml, or table)")
	default:
		return "", fmt.Errorf("invalid format: %s", format)
	}
}

func formatTaskSummaryOutput(task taskSummaryInfo, format inspectFormat) (string, error) {
	if format == "" {
		format = inspectFormatValue
	}

	switch format {
	case inspectFormatJSON:
		return render.FormatJSON(task)
	case inspectFormatYAML:
		return render.FormatYAML(task)
	case inspectFormatValue:
		return formatTaskSummaryValue(task), nil
	case inspectFormatTable:
		return formatTaskSummariesTable([]taskSummaryInfo{task}), nil
	default:
		return "", fmt.Errorf("invalid format: %s", format)
	}
}

func formatTaskOutputSummaryOutput(task taskOutputSummaryInfo, format inspectFormat) (string, error) {
	if format == "" {
		format = inspectFormatValue
	}

	switch format {
	case inspectFormatJSON:
		return render.FormatJSON(task)
	case inspectFormatYAML:
		return render.FormatYAML(task)
	case inspectFormatValue:
		return formatTaskOutputSummaryValue(task), nil
	case inspectFormatTable:
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
		assignedTo := emptyCell
		if task.AssignedTo != nil {
			assignedTo = *task.AssignedTo
		}

		reviewingBy := emptyCell
		if task.ReviewingBy != nil {
			reviewingBy = *task.ReviewingBy
		}

		deps := emptyCell
		if len(task.DependsOn) > 0 {
			deps = fmt.Sprintf("%d", len(task.DependsOn))
		}

		outputs := emptyCell
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
			rows = append(rows, []string{task.ID, task.Status, task.RolePair, emptyCell, emptyCell, emptyCell, emptyCell})
			continue
		}
		for _, output := range task.Output {
			desc := truncateTableDescription(output.Desc)
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
		fmt.Sprintf("Transition ID: %s", task.TransitionID),
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
		fmt.Sprintf("Transition ID: %s", task.TransitionID),
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
		if len(output.Validation) > 0 {
			parts = append(parts, fmt.Sprintf("validation=%s", strings.Join(output.Validation, "; ")))
		}
		if output.DestructiveDB {
			parts = append(parts, "destructive_db=true")
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
func formatTasksOutput(tasks []taskInfo, format inspectFormat) (string, error) {
	if format == "" {
		format = inspectFormatTable
	}

	switch format {
	case inspectFormatJSON:
		return render.FormatJSON(tasks)
	case inspectFormatYAML:
		return render.FormatYAML(tasks)
	case inspectFormatTable:
		return formatTasksTable(tasks), nil
	case inspectFormatValue:
		return "", fmt.Errorf("value format not supported for task lists (use json, yaml, or table)")
	default:
		return "", fmt.Errorf("invalid format: %s", format)
	}
}

// formatTaskOutput formats a single task for output
func formatTaskOutput(task taskInfo, format inspectFormat) (string, error) {
	if format == "" {
		format = inspectFormatValue
	}

	switch format {
	case inspectFormatJSON:
		return render.FormatJSON(task)
	case inspectFormatYAML:
		return render.FormatYAML(task)
	case inspectFormatValue:
		return formatTaskValue(task), nil
	case inspectFormatTable:
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
		assignedTo := emptyCell
		if task.AssignedTo != nil {
			assignedTo = *task.AssignedTo
		}

		reviewingBy := emptyCell
		if task.ReviewingBy != nil {
			reviewingBy = *task.ReviewingBy
		}

		deps := emptyCell
		if len(task.DependsOn) > 0 {
			deps = fmt.Sprintf("%d", len(task.DependsOn))
		}

		desc := truncateTableDescription(task.Description)

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
		fmt.Sprintf("Transition ID: %s", task.TransitionID),
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

	if len(task.Validation) > 0 {
		lines = append(lines, fmt.Sprintf("Validation: %s", strings.Join(task.Validation, "; ")))
	}
	if task.DestructiveDB {
		lines = append(lines, "Destructive DB: true")
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
