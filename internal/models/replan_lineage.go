package models

import "slices"

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
	if state == nil {
		return nil, false
	}
	current := state.FindTask(id)
	if current == nil || !current.TransitionsExecuted["replanned"] {
		return nil, false
	}
	visited := map[string]bool{current.ID: true}
	for current.TransitionsExecuted["replanned"] {
		var next *Task
		for i := range state.Tasks {
			candidate := &state.Tasks[i]
			if candidate.Supersedes == nil || *candidate.Supersedes != current.ID ||
				candidate.RolePair != current.RolePair ||
				!slices.Equal(candidate.EffectiveParentTasks(), current.EffectiveParentTasks()) {
				continue
			}
			if next != nil {
				return nil, false
			}
			next = candidate
		}
		if next == nil || visited[next.ID] {
			return nil, false
		}
		visited[next.ID] = true
		current = next
	}
	return current, true
}
