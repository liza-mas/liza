package commands

import (
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/render"
)

func buildDependencyWaitInfo(waits *models.DependencyWaitResolver, task *models.Task) *dependencyWaitInfo {
	if waits == nil {
		return nil
	}
	wait := waits.Wait(task)
	if wait == nil {
		return nil
	}
	info := &dependencyWaitInfo{
		Unmet: make([]dependencyWaitUnmetInfo, len(wait.Unmet)),
		Heads: make([]dependencyWaitHeadInfo, len(wait.Heads)),
	}
	for i, unmet := range wait.Unmet {
		info.Unmet[i] = dependencyWaitUnmetInfo{Dependency: unmet.DependencyID, Kind: string(unmet.Kind), Blocking: unmet.BlockingIDs, Reason: unmet.Reason}
	}
	for i, head := range wait.Heads {
		info.Heads[i] = dependencyWaitHeadInfo{ID: head.ID, Status: string(head.Status), Claimable: head.Claimable, Reason: head.Reason}
	}
	return info
}

func projectTaskFields(projectRoot string, task *models.Task, fields []string) (map[string]any, error) {
	projection := make(map[string]any, len(fields))
	for _, field := range fields {
		value, err := taskInspectionField(projectRoot, task, field)
		if err != nil {
			return nil, err
		}
		projection[field] = value
	}
	return projection, nil
}

// restoreArchivedForOutput returns a copy of task with its archived fields
// restored when the output will carry them: structured results and JSON/YAML.
// Table and value formats never print receipts, so they do no archive I/O.
func restoreArchivedForOutput(task *models.Task, opts inspectTasksOptions) (*models.Task, error) {
	if len(task.Archived) == 0 || !(opts.Internal || opts.Format.isStructured()) {
		return task, nil
	}
	restored := *task
	if err := ops.HydrateArchivedFields(opts.ProjectRoot, &restored); err != nil {
		return nil, err
	}
	return &restored, nil
}

// buildTaskInfo converts a Task to taskInfo with computed fields
func buildTaskInfo(task *models.Task, projectRoot string) taskInfo {
	info := taskInfo{
		ID:                 task.ID,
		TransitionID:       models.TaskTransitionID(task),
		Description:        task.Description,
		Status:             string(task.Status),
		Priority:           task.Priority,
		AssignedTo:         task.AssignedTo,
		ReviewingBy:        task.ReviewingBy,
		DependsOn:          task.DependsOn,
		BlockedReason:      task.BlockedReason,
		BlockedQuestions:   task.BlockedQuestions,
		RepairRequest:      task.RepairRequest,
		DependencyReceipt:  latestDependencyRepairReceipt(task),
		Iteration:          task.Iteration,
		ReviewCycles:       task.ReviewCyclesCurrent,
		Worktree:           task.Worktree,
		DoneWhen:           task.DoneWhen,
		Validation:         task.Validation,
		ValidationNotes:    task.ValidationNotes,
		AmendsPlan:         task.AmendsPlan,
		AmendmentMode:      task.AmendmentMode,
		PlanAmendment:      task.PlanAmendment,
		PlanningChange:     task.PlanningChange,
		AcceptanceSource:   task.AcceptanceSource,
		AcceptanceReceipt:  task.AcceptanceReceipt,
		Archived:           task.Archived,
		DestructiveDB:      task.DestructiveDB,
		RCARequired:        task.RCARequired,
		Scope:              task.Scope,
		SpecRef:            task.SpecRef,
		PlanRef:            task.PlanRef,
		ParentTasks:        task.EffectiveParentTasks(),
		MergeCommit:        task.MergeCommit,
		PRURL:              latestIntegrationPRURL(task),
		RejectionReason:    task.RejectionReason,
		RejectionRCA:       task.RejectionRCA,
		IntegrationFailure: latestIntegrationFailureDiagnostic(task, projectRoot),
		Output:             task.Output,
		Decomposition:      task.Decomposition,
		AttemptNum:         task.EffectiveAttempt(),
	}

	info.Age = render.FormatDuration(calculateTaskAge(task))
	info.TimeInStatus = render.FormatDuration(models.TimeInStatus(task, time.Now()))

	if task.LeaseExpires != nil {
		remaining := time.Until(*task.LeaseExpires)
		formatted := render.FormatDuration(remaining)
		info.LeaseExpires = &formatted
	}

	return info
}

func latestDependencyRepairReceipt(task *models.Task) *dependencyRepairReceipt {
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		if entry.Event != models.TaskEventDependencyRepairApplied &&
			(entry.Event != models.TaskEventDependenciesRewritten || entry.Extra["repair_source_task"] != task.ID) {
			continue
		}
		if entry.Extra["operation"] != models.RepairOperationApplyDependencyRepair ||
			entry.Extra["repair_request_cleared"] != true {
			continue
		}
		affectedTaskIDs, ok := historyExtraStringSlice(entry.Extra["affected_task_ids"])
		if !ok || len(affectedTaskIDs) == 0 {
			continue
		}
		validation, ok := historyExtraStringSlice(entry.Extra["repair_validation"])
		if !ok || len(validation) == 0 {
			continue
		}
		return &dependencyRepairReceipt{
			Operation:       models.RepairOperationApplyDependencyRepair,
			AffectedTaskIDs: affectedTaskIDs,
			Validation:      validation,
		}
	}
	return nil
}

func historyExtraStringSlice(value any) ([]string, bool) {
	switch values := value.(type) {
	case []string:
		return append([]string{}, values...), true
	case []any:
		result := make([]string, len(values))
		for i, value := range values {
			stringValue, ok := value.(string)
			if !ok {
				return nil, false
			}
			result[i] = stringValue
		}
		return result, true
	default:
		return nil, false
	}
}

func buildTaskSummaryInfo(task *models.Task) taskSummaryInfo {
	info := taskSummaryInfo{
		ID:               task.ID,
		TransitionID:     models.TaskTransitionID(task),
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
		ID:           task.ID,
		TransitionID: models.TaskTransitionID(task),
		Status:       string(task.Status),
		RolePair:     task.RolePair,
		Output:       make([]outputEntrySummaryInfo, 0, len(task.Output)),
	}

	for i, entry := range task.Output {
		info.Output = append(info.Output, outputEntrySummaryInfo{
			Index:            i,
			CodingAllocation: entry.CodingAllocation,
			Desc:             entry.Desc,
			Kind:             entry.Kind,
			SpecRef:          entry.SpecRef,
			EpicRef:          entry.EpicRef,
			PlanRef:          entry.PlanRef,
			ArchRef:          entry.ArchRef,
			Validation:       entry.Validation,
			DestructiveDB:    entry.DestructiveDB,
			DependsOn:        entry.DependsOn,
			TaskDependsOn:    entry.TaskDependsOn,
			Decomposition:    entry.Decomposition,
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
