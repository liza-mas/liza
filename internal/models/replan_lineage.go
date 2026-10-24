package models

import "slices"

// ReplacementTaskIDExtra is the history extra key naming the task a
// replace-task source was replaced by, on its superseded and
// replacement_committed entries.
const ReplacementTaskIDExtra = "replacement_task_id"

// ReplannedTransition is the TransitionsExecuted marker replan sets on the
// original task. It is a marker key, not a pipeline transition name.
const ReplannedTransition = "replanned"

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
// Reservation provider aliasing and validation-note attribution use it; a
// selected child slot uses SelectedChildSuccessor.
func ProviderChildSuccessor(state *State, id string) (*Task, bool) {
	return followLineage(state, id, providerChildStep)
}

// SelectedChildSuccessor is ProviderChildSuccessor for a consumer's selected
// child slot, which also follows a unique same-pair plan-declared replacement
// (D-167): the correcting plan's reviewed output names the child in
// supersedes and generated the successor, which delivers the same work under
// the plan's provenance. The child was never MERGED (replacement eligibility),
// so no consumer consumed it. A split, or a successor that is not provably
// the declaring output's generated child, stays a permanent retirement.
func SelectedChildSuccessor(state *State, id string, pr PipelineResolver) (*Task, bool) {
	return followLineage(state, id, func(state *State, task *Task) (*Task, bool) {
		if next, retired := providerChildStep(state, task); retired {
			return next, true
		}
		return planReplacementStep(state, task, pr)
	})
}

func providerChildStep(state *State, task *Task) (*Task, bool) {
	if task.TransitionsExecuted[ReplannedTransition] {
		return replanStep(state, task)
	}
	return replacementStep(state, task)
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
	if !task.TransitionsExecuted[ReplannedTransition] {
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

// planReplacementStep follows a plan-declared replacement: task is SUPERSEDED
// by exactly one successor naming it in supersedes, which only plan
// generation writes across parents (replan keeps them). That successor
// carries the slot only when it has task's role_pair and a single parent
// plan, other than task's parents, whose one output superseding task
// generated it at an executed transition. Otherwise the retirement resolves
// nothing.
func planReplacementStep(state *State, task *Task, pr PipelineResolver) (*Task, bool) {
	if task.Status != TaskStatusSuperseded || len(task.SupersededBy) != 1 {
		return nil, false
	}
	next := state.FindTask(task.SupersededBy[0])
	if next == nil || next.Supersedes == nil || *next.Supersedes != task.ID {
		return nil, false
	}
	parents := next.EffectiveParentTasks()
	if next.RolePair != task.RolePair || len(parents) != 1 || slices.Contains(task.EffectiveParentTasks(), parents[0]) {
		return nil, true
	}
	plan := state.FindTask(parents[0])
	if plan == nil {
		return nil, true
	}
	index := -1
	for i, entry := range plan.Output {
		if entry.Supersedes != task.ID {
			continue
		}
		if index >= 0 {
			return nil, true
		}
		index = i
	}
	if index < 0 {
		return nil, true
	}
	for transition, executed := range plan.TransitionsExecuted {
		if !executed {
			continue
		}
		td, ids, err := ProviderDependencyChildren(ProviderDependency{ProviderTask: plan.ID, Transition: transition, Outputs: []int{index}}, pr)
		if err == nil && td.SourceRolePair == plan.RolePair && td.TargetRolePair == next.RolePair && ids[0] == next.ID {
			return next, true
		}
	}
	return nil, true
}

func sameLineageSlot(successor, original *Task) bool {
	return successor.RolePair == original.RolePair &&
		slices.Equal(successor.EffectiveParentTasks(), original.EffectiveParentTasks())
}
