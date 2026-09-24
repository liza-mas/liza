package ops

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// ReplanInput holds the parameters for a replan operation.
type ReplanInput struct {
	TaskID    string // optional — auto-detect if empty
	ChangedBy string // required — actor metadata for history/logs
	// Reason is optional. When set it is appended to the replacement's
	// description, so the planner and plan reviewer see what must change.
	Reason string
}

// ReplanResult contains the outcome of a replan operation.
type ReplanResult struct {
	OriginalTaskID string
	NewTaskID      string
	RolePair       string
	SpecRef        string
	// Resumed reports that the sprint left CHECKPOINT for IN_PROGRESS.
	Resumed  bool
	Warnings []string
}

// Replan invalidates a merged planning task's output and creates a new planning
// task so the planner agent re-reads the amended plan. At CHECKPOINT the sprint
// is set back to IN_PROGRESS so agents resume. At IN_PROGRESS (the orchestrator's
// PLANNING_COMPLETE turn, or after an automatic resume) status and checkpoint
// trigger are left alone, so the sprint's other plans still transition. What
// makes replan safe is that no child exists yet, checked under the lock.
func Replan(projectRoot string, input *ReplanInput) (*ReplanResult, error) {
	if input.ChangedBy == "" {
		return nil, &PreconditionError{Reason: "changed_by is required"}
	}

	lp := paths.New(projectRoot)
	statePath := lp.StatePath()

	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}

	planningPairs, err := loadPlanningPairsForAdvance(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load planning pairs: %w", err)
	}

	bb := db.For(statePath)

	var result ReplanResult

	err = bb.Modify(func(state *models.State) error {
		// Resolve target task
		task, resolveErr := resolveReplanTarget(state, input.TaskID, planningPairs)
		if resolveErr != nil {
			return resolveErr
		}

		// Validate sprint status
		atCheckpoint := state.Sprint.Status == models.SprintStatusCheckpoint
		if !atCheckpoint && state.Sprint.Status != models.SprintStatusInProgress {
			return &PreconditionError{Reason: fmt.Sprintf(
				"sprint must be at CHECKPOINT or IN_PROGRESS, got %s", state.Sprint.Status)}
		}

		// Validate task state
		if task.Status != models.TaskStatusMerged {
			return &PreconditionError{Reason: fmt.Sprintf(
				"task %s must be MERGED, got %s", task.ID, task.Status)}
		}
		if len(task.Output) == 0 {
			return &PreconditionError{Reason: fmt.Sprintf(
				"task %s has no output to replan", task.ID)}
		}
		if len(task.TransitionsExecuted) > 0 {
			return &PreconditionError{Reason: fmt.Sprintf(
				"cannot replan — child tasks already created from task %s output. Cancel children first", task.ID)}
		}
		if !IsPlanningPair(task.RolePair, planningPairs) {
			return &PreconditionError{Reason: fmt.Sprintf(
				"task %s role_pair %q is not a planning pair", task.ID, task.RolePair)}
		}
		// A human hold is released only by an operator clear; a replacement
		// would otherwise mint an unheld identity for the same plan.
		if task.PlanCheckVerdictOf() == models.PlanCheckHeld {
			return &PreconditionError{Reason: fmt.Sprintf(
				"task %s is held for human action (%s); an operator must run plan-check %s --clear before replanning",
				task.ID, task.PlanCheck.Ask, task.ID)}
		}

		// Compute new task ID: <original-id>-replan-N
		newTaskID := computeReplanID(state, task.ID)

		// Resolve initial status for the role pair
		initialStatus, statusErr := resolver.InitialStatus(task.RolePair)
		if statusErr != nil {
			return fmt.Errorf("failed to resolve initial status for %q: %w", task.RolePair, statusErr)
		}

		// Invalidate old task output and block all outbound transitions.
		// The "replanned" marker alone doesn't prevent real transitions
		// (e.g. "epic-to-us") from firing, because availableTransitionsByTrigger
		// checks transitionsExecuted[transitionName], not "replanned".
		// We must also mark each real transition as executed.
		if task.TransitionsExecuted == nil {
			task.TransitionsExecuted = make(map[string]bool)
		}
		task.TransitionsExecuted["replanned"] = true

		approvedStatus, approvedErr := resolver.ApprovedStatus(task.RolePair)
		if approvedErr == nil {
			for _, txName := range resolver.AvailableManualTransitions(approvedStatus, nil) {
				task.TransitionsExecuted[txName] = true
			}
			for _, txName := range resolver.AvailableAutoTransitions(approvedStatus, nil) {
				task.TransitionsExecuted[txName] = true
			}
		}

		now := time.Now().UTC()
		reason := strings.TrimSpace(input.Reason)
		note := fmt.Sprintf("replaced by %s", newTaskID)
		if reason != "" {
			note += ": " + reason
		}
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventReplanned,
			Note:  &note,
		})

		// Create new task inheriting fields from original
		originalID := task.ID
		newTask := models.Task{
			ID:          newTaskID,
			Type:        task.Type,
			RolePair:    task.RolePair,
			Description: replanDescription(task.Description, input.ChangedBy, reason),
			Status:      initialStatus,
			Priority:    task.Priority,
			ParentTask:  task.ParentTask,
			ParentTasks: slices.Clone(task.ParentTasks),
			SpecRef:     task.SpecRef,
			EpicRef:     task.EpicRef,
			PlanRef:     task.PlanRef,
			ArchRef:     task.ArchRef,
			Kind:        task.Kind,
			RCARequired: task.RCARequired,
			DoneWhen:    task.DoneWhen,
			Scope:       task.Scope,
			DependsOn:   slices.Clone(task.DependsOn),
			Supersedes:  &originalID,
			Created:     now,
			History:     []models.TaskHistoryEntry{},
		}
		state.Tasks = append(state.Tasks, newTask)

		// Retarget downstream non-terminal tasks' DependsOn from old → new ID
		for i := range state.Tasks {
			if state.Tasks[i].Status.IsTerminal() {
				continue
			}
			changed := false
			for j := range state.Tasks[i].DependsOn {
				if state.Tasks[i].DependsOn[j] == task.ID {
					state.Tasks[i].DependsOn[j] = newTaskID
					changed = true
				}
			}
			if changed {
				state.Tasks[i].DependsOn = dedupeStrings(state.Tasks[i].DependsOn)
			}
		}

		var warnings []string

		// Retire selective inherit_inputs that name the replanned task.
		//
		// Deliberately NOT filtered by IsTerminal(), unlike the retarget loop
		// above. That loop concerns a task's own DependsOn, and a terminal
		// task will not be claimed again. Output[].InheritInputs on a MERGED
		// task is different: MERGED is terminal *and* is the status children
		// are generated from, so a merged producer's output is live input to
		// generation, not historical audit data.
		//
		// Indexes are not retargeted onto the replacement. Replanning exists
		// to change the upstream's output[], so index k no longer denotes
		// what the planner selected; carrying it across would silently
		// reinterpret the selection, which is worse than losing it because
		// nothing would detect it. Degrading to the whole-phase barrier is
		// the safe direction — it is a superset of any selection, it cannot
		// fail, and it is the same behavior as omitted intent.
		//
		// This makes the state honest; it does not recover the prerequisite.
		// When the producer is terminal its DependsOn still names the
		// replanned task (the retarget loop skipped it), that task has no
		// children by design, and the barrier is lost either way. The warning
		// below is the existing mitigation for that; see TECH_DEBT.md.
		for i := range state.Tasks {
			candidate := &state.Tasks[i]
			for entryIndex := range candidate.Output {
				entry := &candidate.Output[entryIndex]
				retired, named := entry.InheritInputs.SelectionFor(task.ID)
				if !named {
					continue
				}
				entry.InheritInputs = &models.InheritInputs{Mode: models.InheritModeAll}

				// Persisted on the producer, not only returned to the caller:
				// the seam was chosen for a durable record at the moment of
				// cause, and after this command returns the state would
				// otherwise show Mode "all" with no trace it was ever
				// selective or why.
				note := fmt.Sprintf("output[%d] selection into replanned task %s retired to whole-phase inheritance",
					entryIndex, task.ID)
				candidate.History = append(candidate.History, models.TaskHistoryEntry{
					Time:  now,
					Event: models.TaskEventDependenciesRewritten,
					Agent: &input.ChangedBy,
					Note:  &note,
					Extra: map[string]any{
						"output_index":           entryIndex,
						"replanned_task":         task.ID,
						"replacement_task":       newTaskID,
						"retired_output_index":   retired,
						"rewrote_inherit_inputs": true,
					},
				})
				warnings = append(warnings, fmt.Sprintf(
					"task %s output[%d] selected specific outputs of replanned task %s; "+
						"selection retired to whole-phase inheritance (replacement %s)",
					candidate.ID, entryIndex, task.ID, newTaskID))
			}
		}

		// Warn about terminal tasks that still depend on the old ID
		for i := range state.Tasks {
			if !state.Tasks[i].Status.IsTerminal() {
				continue
			}
			if slices.Contains(state.Tasks[i].DependsOn, task.ID) {
				warnings = append(warnings,
					fmt.Sprintf("task %s is %s and depends on replanned task %s — consider replanning %s too",
						state.Tasks[i].ID, state.Tasks[i].Status, task.ID, state.Tasks[i].ID))
			}
		}

		// Add to sprint scope
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, newTaskID)

		if atCheckpoint {
			// Resume sprint
			state.Sprint.Status = models.SprintStatusInProgress
			state.Sprint.CheckpointTrigger = ""
		}

		// Alignment history
		state.Goal.AlignmentHistory = append(state.Goal.AlignmentHistory, models.AlignmentHistory{
			Timestamp: now,
			Event:     "replan",
			Summary: fmt.Sprintf("Replanned task %s → %s (role_pair: %s, spec: %s)%s",
				task.ID, newTaskID, task.RolePair, task.SpecRef, replanReasonSuffix(reason)),
		})

		result = ReplanResult{
			OriginalTaskID: task.ID,
			NewTaskID:      newTaskID,
			RolePair:       task.RolePair,
			SpecRef:        task.SpecRef,
			Resumed:        atCheckpoint,
			Warnings:       warnings,
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("replan failed: %w", err)
	}

	// Activity log (best-effort)
	logger := log.New(lp.LogPath())
	logEntry := log.Entry{
		Timestamp: time.Now().UTC(),
		Agent:     input.ChangedBy,
		Action:    "task_replanned",
		Task:      &result.NewTaskID,
		Detail:    fmt.Sprintf("Replanned %s → %s", result.OriginalTaskID, result.NewTaskID),
	}
	if logErr := logger.Append(logEntry); logErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("activity log write failed: %v", logErr))
	}

	// Post-write state validation
	if valErr := statevalidate.ValidateStateFile(statePath, false, io.Discard); valErr != nil {
		return nil, &PostWriteValidationError{Err: valErr}
	}

	return &result, nil
}

// resolveReplanTarget finds the task to replan. If taskID is provided, looks it
// up directly. Otherwise scans sprint.scope.planned for a single unconsumed
// planning task.
func resolveReplanTarget(state *models.State, taskID string, planningPairs map[string]bool) (*models.Task, error) {
	if taskID != "" {
		task := state.FindTask(taskID)
		if task == nil {
			return nil, &PreconditionError{Reason: fmt.Sprintf("task %q not found", taskID)}
		}
		return task, nil
	}

	// Auto-detect: scan planned tasks for unconsumed planning output
	var matches []*models.Task
	for _, id := range state.Sprint.Scope.Planned {
		task := state.FindTask(id)
		if IsUnconsumedPlanningOutput(task, planningPairs) {
			matches = append(matches, task)
		}
	}

	switch len(matches) {
	case 0:
		return nil, &PreconditionError{Reason: "no planning task with unconsumed output found in current sprint"}
	case 1:
		return matches[0], nil
	default:
		ids := make([]string, len(matches))
		for i, t := range matches {
			ids[i] = t.ID
		}
		return nil, &PreconditionError{Reason: fmt.Sprintf(
			"multiple planning tasks found — specify task ID: %s", strings.Join(ids, ", "))}
	}
}

// replanDescription carries the replan reason to the replacement's planner and
// reviewer, who see the task description but not the original's history.
func replanDescription(description, changedBy, reason string) string {
	if reason == "" {
		return description
	}
	return fmt.Sprintf("%s\n\nReplan reason (from %s): %s", description, changedBy, reason)
}

func replanReasonSuffix(reason string) string {
	if reason == "" {
		return ""
	}
	return ": " + reason
}

// dedupeStrings returns a new slice with duplicates removed, preserving order.
func dedupeStrings(s []string) []string {
	seen := make(map[string]bool, len(s))
	result := make([]string, 0, len(s))
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

// computeReplanID generates <original-id>-replan-N by counting existing
// replan tasks for the same original.
func computeReplanID(state *models.State, originalID string) string {
	prefix := originalID + "-replan-"
	count := 0
	for _, task := range state.Tasks {
		if strings.HasPrefix(task.ID, prefix) {
			count++
		}
	}
	return fmt.Sprintf("%s%d", prefix, count+1)
}
