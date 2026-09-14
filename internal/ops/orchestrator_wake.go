package ops

import (
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// CountActionableBlockedTasks counts BLOCKED tasks that the orchestrator should
// wake up for. A blocked task is actionable if it has never been assessed, or if
// new activity has occurred since the last assessment on the task itself or via
// human notes. Dependency activity is actionable only when it changes dependency
// satisfaction.
func CountActionableBlockedTasks(state *models.State) int {
	count := 0
	for i := range state.Tasks {
		if state.Tasks[i].Status == models.TaskStatusBlocked && isTaskActionableSinceAssessment(&state.Tasks[i], state) {
			count++
		}
	}
	return count
}

// isTaskActionableSinceAssessment determines whether a task should trigger an
// orchestrator wake. Returns true if:
//   - No orchestrator_assessment history entry exists (never triaged)
//   - The task itself has a non-assessment history entry after the last assessment
//   - Any dependency became satisfied after the last assessment
//   - A human note targets this task (by ID or "all") after the last assessment
//
// Used by both BLOCKED and HYPOTHESIS_EXHAUSTED wake triggers.
func isTaskActionableSinceAssessment(task *models.Task, state *models.State) bool {
	lastAssessment := lastOrchestratorAssessment(task)

	// Never assessed → actionable.
	if lastAssessment == nil {
		return true
	}

	// Check own history for non-assessment activity after last assessment.
	for i := range task.History {
		if task.History[i].Event != models.TaskEventOrchestratorAssessment &&
			task.History[i].Time.After(lastAssessment.Time) {
			return true
		}
	}

	// Check whether dependency satisfaction changed after the last assessment.
	for _, depID := range task.DependsOn {
		if dependencySatisfactionChangedAfterAssessment(state, state.ResolveDependency(depID), lastAssessment.Time) {
			return true
		}
	}

	// Only BLOCKED tasks use dependency-descendant cursors. Hypothesis exhaustion
	// shares this predicate but retains its existing direct-dependency behavior.
	if task.Status == models.TaskStatusBlocked {
		recorded, hasSnapshot := lastAssessment.Extra[DependencyDescendantWakeSnapshotExtraKey]
		if hasSnapshot {
			if DependencyDescendantWakeSnapshotChanged(state, task, recorded) {
				return true
			}
		} else if DependencyDescendantChangedAfter(state, task, lastAssessment.Time) {
			return true
		}
	}

	// Check human notes targeting this task (by ID or "all") after last assessment.
	// Design choice: notes on dependency tasks do NOT re-activate this blocked task.
	// Rationale: human targets specific tasks by ID; if they want to wake all
	// dependents, they add notes to each or use for:"all". This avoids surprising
	// cascade wakes when a human annotates a dependency for unrelated reasons.
	for i := range state.HumanNotes {
		note := &state.HumanNotes[i]
		if (note.For == task.ID || note.For == "all") && note.Timestamp.After(lastAssessment.Time) {
			return true
		}
	}

	return false
}

// lastOrchestratorAssessment returns the task's most recent
// orchestrator_assessment history entry, or nil if it was never assessed.
func lastOrchestratorAssessment(task *models.Task) *models.TaskHistoryEntry {
	for i := len(task.History) - 1; i >= 0; i-- {
		if task.History[i].Event == models.TaskEventOrchestratorAssessment {
			return &task.History[i]
		}
	}
	return nil
}

func dependencySatisfactionChangedAfterAssessment(state *models.State, result models.DependencySatisfaction, after time.Time) bool {
	if state == nil || !result.Satisfied() {
		return false
	}
	for _, taskID := range result.Path {
		task := state.FindTask(taskID)
		if task == nil {
			continue
		}
		for i := len(task.History) - 1; i >= 0; i-- {
			entry := task.History[i]
			if !entry.Time.After(after) {
				break
			}
			switch entry.Event {
			case models.TaskEventMerged, models.TaskEventSuperseded:
				return true
			}
		}
	}
	return false
}

// CountActionableHypothesisExhaustedTasks counts nonterminal, nonblocked tasks
// with repeated coder failures and new information since their last assessment.
func CountActionableHypothesisExhaustedTasks(state *models.State) int {
	count := 0
	for i := range state.Tasks {
		if len(state.Tasks[i].FailedBy) >= 2 &&
			state.Tasks[i].Status != models.TaskStatusBlocked &&
			!state.Tasks[i].Status.IsTerminal() &&
			isTaskActionableSinceAssessment(&state.Tasks[i], state) {
			count++
		}
	}
	return count
}

// CountUnseenHumanNotes counts operator notes no completed orchestrator turn
// has rendered yet. Each such note wakes an idle orchestrator once; the turn
// that renders it marks it seen on completion, so a note whose instructions
// were not carried out is shown again rather than silently dropped.
func CountUnseenHumanNotes(state *models.State) int {
	count := 0
	for i := range state.HumanNotes {
		if !state.HumanNotes[i].SeenByOrchestrator() {
			count++
		}
	}
	return count
}

// UnseenHumanNotes returns the notes CountUnseenHumanNotes counts, in
// recording order.
func UnseenHumanNotes(state *models.State) []models.HumanNote {
	var notes []models.HumanNote
	for i := range state.HumanNotes {
		if !state.HumanNotes[i].SeenByOrchestrator() {
			notes = append(notes, state.HumanNotes[i])
		}
	}
	return notes
}

// TasksAssessedBetween returns the IDs of tasks that gained an orchestrator
// assessment between before and after. History is append-only, so an entry
// count comparison is immune to same-instant re-assessments.
func TasksAssessedBetween(before, after *models.State) map[string]bool {
	assessed := map[string]bool{}
	for i := range after.Tasks {
		task := &after.Tasks[i]
		previous := 0
		if beforeTask := before.FindTask(task.ID); beforeTask != nil {
			previous = countOrchestratorAssessments(beforeTask)
		}
		if countOrchestratorAssessments(task) > previous {
			assessed[task.ID] = true
		}
	}
	return assessed
}

func countOrchestratorAssessments(task *models.Task) int {
	count := 0
	for i := range task.History {
		if task.History[i].Event == models.TaskEventOrchestratorAssessment {
			count++
		}
	}
	return count
}

// MarkHumanNotesSeen stamps the unseen notes among the first rendered entries
// (state.HumanNotes is append-only, so notes added after the prompt was built
// sit past that index) that consumed accepts, and returns how many it stamped.
// A HUMAN_NOTE turn consumes every rendered note; any other turn only the
// notes an assessment it recorded consumed.
func MarkHumanNotesSeen(state *models.State, at time.Time, rendered int, consumed func(*models.HumanNote) bool) int {
	count := 0
	for i := 0; i < rendered && i < len(state.HumanNotes); i++ {
		if state.HumanNotes[i].SeenByOrchestrator() || !consumed(&state.HumanNotes[i]) {
			continue
		}
		state.HumanNotes[i].MarkSeenByOrchestrator(at)
		count++
	}
	return count
}
