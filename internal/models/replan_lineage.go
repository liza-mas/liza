package models

import "slices"

// ReplacementTaskIDExtra is the history extra key naming the task a
// replace-task source was replaced by, on its superseded and
// replacement_committed entries.
const ReplacementTaskIDExtra = "replacement_task_id"

// ReplanSuccessor follows replan lineage from id to the task that now carries
// its work. Replan marks the original "replanned" and creates one replacement
// whose Supersedes names it, with the same role_pair and parents; the walk
// repeats while the reached task is itself replanned, so chained replans
// resolve to the last one.
//
// It fails closed (false) when id is missing or not replanned, a step has no
// or several candidates, or the walk revisits a task. It does not judge the
// successor's own retirement: callers keep applying their retirement checks.
func ReplanSuccessor(state *State, id string) (*Task, bool) {
	return followLineage(state, id, replanStep)
}

// ProviderChildSuccessor is ReplanSuccessor for a selected provider child,
// whose slot also follows a same-pair replace-task successor (D-70): the
// replaced child was never MERGED, so no consumer consumed it, and the
// successor keeps its parent and role_pair. Any mix of replans and such
// replacements is followed. Supersession, cancellation and plan-declared
// replacement stay permanent retirements; the walk stops at such a task.
func ProviderChildSuccessor(state *State, id string) (*Task, bool) {
	return followLineage(state, id, func(state *State, task *Task) (*Task, bool) {
		if task.TransitionsExecuted["replanned"] {
			return replanStep(state, task)
		}
		return replacementStep(state, task)
	})
}

// lineageStep reports whether task was retired into a lineage successor and,
// if so, that unique successor (nil when there is none or several).
type lineageStep func(state *State, task *Task) (next *Task, retired bool)

// followLineage walks step from id while each reached task was retired into a
// successor. It fails closed when id is missing or not retired, a step has no
// unique successor, or the walk revisits a task.
func followLineage(state *State, id string, step lineageStep) (*Task, bool) {
	if state == nil {
		return nil, false
	}
	current := state.FindTask(id)
	if current == nil {
		return nil, false
	}
	visited := map[string]bool{}
	for {
		next, retired := step(state, current)
		if !retired {
			if len(visited) == 0 {
				return nil, false
			}
			return current, true
		}
		visited[current.ID] = true
		if next == nil || visited[next.ID] {
			return nil, false
		}
		current = next
	}
}

func replanStep(state *State, task *Task) (*Task, bool) {
	if !task.TransitionsExecuted["replanned"] {
		return nil, false
	}
	var next *Task
	for i := range state.Tasks {
		candidate := &state.Tasks[i]
		if candidate.Supersedes == nil || *candidate.Supersedes != task.ID || !sameLineageSlot(candidate, task) {
			continue
		}
		if next != nil {
			return nil, true
		}
		next = candidate
	}
	return next, true
}

// replacementStep follows a replace-task: task is SUPERSEDED by exactly the
// successor its history records (replace-task's superseded entry, or
// replacement_committed for replacements predating it), in its role_pair and
// with its parents. Without that record the supersession is not a replace-task.
func replacementStep(state *State, task *Task) (*Task, bool) {
	if task.Status != TaskStatusSuperseded {
		return nil, false
	}
	idx := slices.IndexFunc(task.History, func(entry TaskHistoryEntry) bool {
		id, ok := entry.Extra[ReplacementTaskIDExtra].(string)
		return ok && id != "" && (entry.Event == TaskEventSuperseded || entry.Event == TaskEventReplacementCommitted)
	})
	if idx < 0 {
		return nil, false
	}
	successorID := task.History[idx].Extra[ReplacementTaskIDExtra].(string)
	next := state.FindTask(successorID)
	if next == nil || !slices.Equal(task.SupersededBy, []string{successorID}) || !sameLineageSlot(next, task) {
		return nil, true
	}
	return next, true
}

func sameLineageSlot(successor, original *Task) bool {
	return successor.RolePair == original.RolePair &&
		slices.Equal(successor.EffectiveParentTasks(), original.EffectiveParentTasks())
}
