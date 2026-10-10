package models

// PendingPlanAmendment follows explicit input/parent edges to a pending
// correction. Correction work itself must remain runnable to clear its fence.
func PendingPlanAmendment(state *State, task *Task) string {
	if state == nil || task == nil || task.AmendsPlan != "" {
		return ""
	}
	seen := map[string]bool{}
	var visit func(*Task) string
	visit = func(current *Task) string {
		if current == nil || seen[current.ID] {
			return ""
		}
		seen[current.ID] = true
		if current.PlanAmendment != nil && current.PlanAmendment.Pending != "" {
			return current.ID
		}
		ids := append([]string(nil), current.DependsOn...)
		ids = append(ids, current.EffectiveParentTasks()...)
		for _, dep := range current.ProviderDependencies {
			ids = append(ids, dep.ProviderTask)
		}
		for _, reservation := range current.ProviderReservations {
			ids = append(ids, EffectiveReservationProvider(state, reservation.ProviderTask))
		}
		for _, id := range ids {
			if blocked := visit(state.FindTask(id)); blocked != "" {
				return blocked
			}
		}
		return ""
	}
	return visit(task)
}
