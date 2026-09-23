package ops

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/liza-mas/liza/internal/models"
)

// AwaitedTasksExtraKey stores, on the latest orchestrator_assessment only, the
// existing unfinished tasks a BLOCKED task waits for. It is not a dependency
// edge: the role-pair direction rule does not apply, so a planner can wait on
// later-stage work. While the set is recorded, the assessment fingerprint
// tracks these tasks instead of every descendant of the task's dependencies.
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

// validateAwaitedTasks rejects an awaited set the task could not usefully or
// safely wait on: unknown IDs, the task itself, work that is no longer pending,
// and waits that lead back to the task.
func validateAwaitedTasks(state *models.State, task *models.Task, awaited []string) error {
	resolver := models.NewDependencyResolver(state)
	var settled []string
	for _, id := range awaited {
		if id == task.ID {
			return &PreconditionError{Reason: fmt.Sprintf("task %s cannot await itself", task.ID)}
		}
		if state.FindTask(id) == nil {
			return &PreconditionError{Reason: fmt.Sprintf("awaited task %q does not exist", id)}
		}
		if !awaitedTaskPending(state, resolver, id) {
			settled = append(settled, fmt.Sprintf("%s (%s)", id, state.FindTask(id).Status))
		}
	}
	if len(settled) > 0 {
		return &PreconditionError{Reason: fmt.Sprintf("awaited tasks are no longer pending: %s; drop them from the awaited set", strings.Join(settled, ", "))}
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
// through them. A stale awaited set on a task that is no longer BLOCKED is not
// followed. The returned path starts at the awaited task and ends at waitingID.
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
				if waits, ok := awaitedTasksFrom(lastOrchestratorAssessment(candidate)); ok {
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
