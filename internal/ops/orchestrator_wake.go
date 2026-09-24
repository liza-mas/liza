package ops

import (
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// CountActionableBlockedTasks counts BLOCKED tasks that the orchestrator should
// wake up for. A blocked task is actionable when its current assessment
// fingerprint differs from the recorded one, or no valid baseline exists.
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
// orchestrator wake. BLOCKED tasks compare the writer's content fingerprint.
// For other tasks, returns true if:
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

	if task.Status == models.TaskStatusBlocked {
		recorded, valid := IsAssessmentFingerprint(lastAssessment.Extra[AssessmentFingerprintExtraKey])
		if !valid {
			return true
		}
		candidate := currentBlockerCandidate(state, task, lastAssessment)
		// Disposition belongs to the same assessment that carries the digest;
		// the blocker triple belongs to the task's current canonical state.
		if lastAssessment.Note != nil {
			candidate.Note = *lastAssessment.Note
		}
		return BuildAssessmentFingerprint(state, task, candidate) != recorded
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

// currentBlockerCandidate is a BLOCKED task's current blocker triple plus the
// awaited set of its latest assessment, without the assessment's own note.
func currentBlockerCandidate(state *models.State, task *models.Task, lastAssessment *models.TaskHistoryEntry) AssessmentFingerprintCandidate {
	candidate := AssessmentFingerprintCandidate{
		Questions: task.BlockedQuestions, RepairRequest: task.RepairRequest,
	}
	if task.BlockedReason != nil {
		candidate.Reason = *task.BlockedReason
	}
	// A wait that now leads back to this task would suppress its wakes
	// forever. Dropping the set changes the digest, so the task wakes; the
	// wake does not say why, but recording the same set again is rejected
	// with the cycle. A malformed set also wakes the task.
	if awaited, ok := awaitedTasksFrom(lastAssessment); ok {
		if _, deadlocked := awaitLeadsBackTo(state, task.ID, awaited); !deadlocked {
			candidate.Awaited = awaited
		}
	}
	return candidate
}

// BlockedTasksAwaitPlanningOutput reports whether an actionable BLOCKED task
// waits on planning output that only a PLANNING_COMPLETE checkpoint can
// materialize: a dependency or awaited task (either followed through
// supersession) that is a planned, PLANNING_COMPLETE-eligible planner merged after the last
// transition attempt. A BLOCKED_TASKS turn cannot checkpoint, so such a wake
// is wasted. The attempt bound keeps a planner whose transition already failed
// from outranking blocked triage again in this sprint.
func BlockedTasksAwaitPlanningOutput(state *models.State, planningPairs map[string]bool) bool {
	if state.Sprint.Status == models.SprintStatusCheckpoint || state.Sprint.Status == models.SprintStatusCompleted {
		return false
	}
	resolver := models.NewDependencyResolver(state)
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.Status != models.TaskStatusBlocked || !isTaskActionableSinceAssessment(task, state) {
			continue
		}
		waitsOn := task.DependsOn
		if awaited, ok := awaitedTasksFrom(lastOrchestratorAssessment(task)); ok {
			waitsOn = append(slices.Clone(waitsOn), awaited...)
		}
		// Both lists follow supersession to the replacement that will merge.
		for _, id := range waitsOn {
			for _, candidate := range append([]string{id}, resolver.Resolve(id).Path...) {
				if awaitsPlanningTransition(state, candidate, planningPairs) {
					return true
				}
			}
		}
	}
	return false
}

func awaitsPlanningTransition(state *models.State, plannerID string, planningPairs map[string]bool) bool {
	if !slices.Contains(state.Sprint.Scope.Planned, plannerID) {
		return false
	}
	planner := state.FindTask(plannerID)
	if !IsPlanningCompleteEligible(planner, planningPairs, state) {
		return false
	}
	attempted := state.Sprint.Timeline.TransitionsAttemptedAt
	if attempted == nil {
		return true
	}
	for i := len(planner.History) - 1; i >= 0; i-- {
		if planner.History[i].Event == models.TaskEventMerged {
			return planner.History[i].Time.After(*attempted)
		}
	}
	return false
}

// BlockedMaterialIdentity identifies the blocker state of every BLOCKED task:
// its current blocker triple, awaited set, dependency and descendant outcomes,
// structure and targeted human notes, through the assessment fingerprint. The
// orchestrator's own assessment note is excluded, so a turn that only
// rephrases a hold leaves the identity unchanged.
func BlockedMaterialIdentity(state *models.State) string {
	var lines []string
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.Status != models.TaskStatusBlocked {
			continue
		}
		candidate := currentBlockerCandidate(state, task, lastOrchestratorAssessment(task))
		lines = append(lines, task.ID+"="+BuildAssessmentFingerprint(state, task, candidate))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
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
