package ops

import (
	stderrors "errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/liza-mas/liza/internal/models"
)

// AwaitedTasksExtraKey stores, on the latest orchestrator_assessment only, the
// existing unfinished tasks a BLOCKED task waits for, all of them. It is not a
// dependency edge: the role-pair direction rule does not apply, so a planner
// can wait on later-stage work. While the set is recorded, the assessment
// fingerprint tracks the set's completion instead of every descendant of the
// task's dependencies, and instead of the dependency records the set covers.
const AwaitedTasksExtraKey = "awaited_tasks"

// normalizeAwaitedTasks trims the declared IDs and returns them as a sorted
// set, rejecting blank entries. Repeats are merged, as the payload schema
// accepts them.
func normalizeAwaitedTasks(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	normalized := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, &PreconditionError{Reason: "awaited task IDs must not be empty"}
		}
		if !slices.Contains(normalized, id) {
			normalized = append(normalized, id)
		}
	}
	sort.Strings(normalized)
	return normalized, nil
}

// awaitedTasksFrom reads the awaited set persisted on an assessment. A missing
// or malformed value reports false, so callers fall back to the default
// descendant projection.
func awaitedTasksFrom(entry *models.TaskHistoryEntry) ([]string, bool) {
	if entry == nil {
		return nil, false
	}
	var ids []string
	switch values := entry.Extra[AwaitedTasksExtraKey].(type) {
	case []string:
		ids = values
	case []any:
		for _, value := range values {
			id, ok := value.(string)
			if !ok {
				return nil, false
			}
			ids = append(ids, id)
		}
	default:
		return nil, false
	}
	normalized, err := normalizeAwaitedTasks(ids)
	if err != nil || len(normalized) == 0 || !slices.Equal(normalized, ids) {
		return nil, false
	}
	return normalized, true
}

// Awaited-member states. The set is one all-of wait: it completes when every
// member is satisfied and needs attention as soon as any member has failed.
const (
	awaitedPending   = "pending"
	awaitedSatisfied = "satisfied"
	awaitedFailed    = "failed"
)

// awaitedMemberState classifies one awaited ID over its replacement path.
// Failed work anywhere on the path (blocked, abandoned, integration-failed,
// unreplaced-superseded, missing, unknown status, or a replacement cycle)
// makes the member failed and is listed as "id (status)". Otherwise the member
// is satisfied only when the resolver satisfies it, i.e. every replacement of a
// split has merged, and pending before that. A BLOCKED or INTEGRATION_FAILED
// task can recover, so failed means "needs reassessment", not "can never merge".
func awaitedMemberState(state *models.State, resolver *models.DependencyResolver, id string) (string, []string) {
	resolution := resolver.Resolve(id)
	var failed []string
	for _, pathID := range resolution.Path {
		task := state.FindTask(pathID)
		switch {
		case task == nil:
			failed = append(failed, pathID+" (missing)")
		case assessmentDependencyOutcome(task) == "unknown":
			failed = append(failed, pathID+" (unknown status)")
		case assessmentDependencyOutcome(task) == "failed_or_blocked":
			failed = append(failed, fmt.Sprintf("%s (%s)", pathID, task.Status))
		}
	}
	if len(failed) == 0 && resolution.Kind == models.DependencyInvalidCycle {
		failed = append(failed, id+" (replacement cycle)")
	}
	switch {
	case len(failed) > 0:
		return awaitedFailed, failed
	case resolution.Satisfied():
		return awaitedSatisfied, nil
	default:
		return awaitedPending, nil
	}
}

// currentEpisodeAwaitedTasks returns the awaited set of the task's latest
// assessment only while no status transition follows that assessment in
// history. Order, not time, decides: an unblock and re-block recorded at the
// assessment's own instant still start a new episode. Unclassified events are
// treated as transitions so vocabulary drift drops a set rather than keeping
// a stale one.
func currentEpisodeAwaitedTasks(task *models.Task) ([]string, bool) {
	for i := len(task.History) - 1; i >= 0; i-- {
		event := task.History[i].Event
		if event == models.TaskEventOrchestratorAssessment {
			return awaitedTasksFrom(&task.History[i])
		}
		if transition, classified := models.IsStatusTransitionEvent(event); transition || !classified {
			return nil, false
		}
	}
	return nil, false
}

// carriedAwaitedTasks is the set a re-assessment without --awaits keeps: the
// current episode's set minus satisfied members. It is empty when nothing is
// left to wait for.
func carriedAwaitedTasks(state *models.State, task *models.Task) []string {
	previous, ok := currentEpisodeAwaitedTasks(task)
	if !ok {
		return nil
	}
	resolver := models.NewDependencyResolver(state)
	var carried []string
	for _, id := range previous {
		if memberState, _ := awaitedMemberState(state, resolver, id); memberState != awaitedSatisfied {
			carried = append(carried, id)
		}
	}
	return carried
}

// rejectCarriedAwaitedTasks explains a carried set's validation failure with
// the two ways out: restate only the members still pending, or clear the set.
func rejectCarriedAwaitedTasks(state *models.State, carried []string, err error) error {
	var precondition *PreconditionError
	if !stderrors.As(err, &precondition) {
		return err
	}
	resolver := models.NewDependencyResolver(state)
	var pending []string
	for _, id := range carried {
		if memberState, _ := awaitedMemberState(state, resolver, id); memberState == awaitedPending {
			pending = append(pending, id)
		}
	}
	remedy := "re-assess with --clear-awaits"
	if len(pending) > 0 && len(pending) < len(carried) {
		remedy = fmt.Sprintf("re-assess with --awaits %s or --clear-awaits", strings.Join(pending, ","))
	}
	return &PreconditionError{Reason: fmt.Sprintf("the awaited set carried from the previous assessment (%s) is no longer valid: %s; %s", strings.Join(carried, ","), precondition.Reason, remedy)}
}

// validateAwaitedTasks rejects an awaited set the task could not usefully or
// safely wait on: unknown IDs, the task itself, work that is no longer pending,
// members with failed work on their replacement path, and waits that lead back
// to the task.
func validateAwaitedTasks(state *models.State, task *models.Task, awaited []string) error {
	resolver := models.NewDependencyResolver(state)
	var settled, failed []string
	for _, id := range awaited {
		if id == task.ID {
			return &PreconditionError{Reason: fmt.Sprintf("task %s cannot await itself", task.ID)}
		}
		if state.FindTask(id) == nil {
			return &PreconditionError{Reason: fmt.Sprintf("awaited task %q does not exist", id)}
		}
		if !awaitedTaskPending(state, resolver, id) {
			settled = append(settled, fmt.Sprintf("%s (%s)", id, state.FindTask(id).Status))
			continue
		}
		if memberState, work := awaitedMemberState(state, resolver, id); memberState == awaitedFailed {
			failed = append(failed, fmt.Sprintf("%s: %s", id, strings.Join(work, ", ")))
		}
	}
	if len(settled) > 0 {
		return &PreconditionError{Reason: fmt.Sprintf("awaited tasks are no longer pending: %s; drop them from the awaited set", strings.Join(settled, ", "))}
	}
	// The set completes only when every member is satisfied, so a member with
	// failed work would hold the wait until that work is reassessed.
	if len(failed) > 0 {
		return &PreconditionError{Reason: fmt.Sprintf("awaited tasks have failed work on their replacement path: %s; resolve the failure or await the pending replacements directly", strings.Join(failed, "; "))}
	}
	if path, ok := awaitLeadsBackTo(state, task.ID, awaited); ok {
		cycle := append([]string{task.ID}, path...)
		return &PreconditionError{Reason: fmt.Sprintf("awaiting would deadlock: %s", strings.Join(cycle, " -> "))}
	}
	return nil
}

// awaitedTaskPending reports whether some task on the ID's replacement path is
// still pending in the fingerprint's outcome classes. A path that is wholly
// merged, failed, blocked or missing has nothing left to wait for.
func awaitedTaskPending(state *models.State, resolver *models.DependencyResolver, id string) bool {
	for _, pathID := range resolver.Resolve(id).Path {
		if candidate := state.FindTask(pathID); candidate != nil && assessmentDependencyOutcome(candidate) == "pending" {
			return true
		}
	}
	return false
}

// awaitLeadsBackTo reports whether any awaited task can reach waitingID
// through work that is still active: effective prerequisites followed through
// supersession, and the awaited sets of tasks that are BLOCKED now. Merged,
// abandoned and superseded tasks end a branch because nothing still waits
// through them. A set from an earlier episode, or on a task that is no longer
// BLOCKED, is not followed. The returned path starts at the awaited task and
// ends at waitingID.
func awaitLeadsBackTo(state *models.State, waitingID string, awaited []string) ([]string, bool) {
	resolver := models.NewDependencyResolver(state)
	type node struct {
		id   string
		path []string
	}
	visited := map[string]bool{}
	queue := make([]node, 0, len(awaited))
	for _, id := range awaited {
		queue = append(queue, node{id: id, path: []string{id}})
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, pathID := range resolver.Resolve(current.id).Path {
			if pathID == waitingID {
				// A dependency edge already queued the waiter; a supersession
				// that resolves to it has not.
				if current.path[len(current.path)-1] != waitingID {
					return append(current.path, waitingID), true
				}
				return current.path, true
			}
			if visited[pathID] {
				continue
			}
			visited[pathID] = true
			candidate := state.FindTask(pathID)
			if candidate == nil {
				continue
			}
			switch candidate.Status {
			case models.TaskStatusMerged, models.TaskStatusAbandoned, models.TaskStatusSuperseded:
				continue
			}
			next := candidate.DependsOn
			if candidate.Status == models.TaskStatusBlocked {
				if waits, ok := currentEpisodeAwaitedTasks(candidate); ok {
					next = append(append([]string(nil), next...), waits...)
				}
			}
			for _, id := range next {
				queue = append(queue, node{id: id, path: append(append([]string(nil), current.path...), id)})
			}
		}
	}
	return nil, false
}
