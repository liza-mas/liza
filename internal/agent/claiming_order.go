package agent

import (
	"sort"

	"github.com/liza-mas/liza/internal/models"
)

// orderDoerCandidates keeps explicit priority authoritative, then prefers owned
// rejection, other rework, and active dependency demand. Shuffling before the
// stable sort preserves random contention spreading among equivalent ranks.
// Every top-tier candidate remains available for the claim loop's fallback.
func orderDoerCandidates(candidates []*models.Task, state *models.State, agentID string, pr models.PipelineResolver) []*models.Task {
	tier := shuffledByPriorityTier(candidates)
	if len(tier) < 2 {
		return tier
	}
	impact := doerDependencyImpact(state, pr, tier)
	classes := make(map[string]int, len(tier))
	for _, task := range tier {
		class := 2
		rejected, err := pr.RejectedStatus(task.RolePair)
		if err == nil && task.Status == rejected {
			class = 1
			if task.AssignedTo != nil && *task.AssignedTo == agentID {
				class = 0
			}
		} else if task.Status == models.TaskStatusIntegrationFailed {
			class = 1
		}
		classes[task.ID] = class
	}
	sort.SliceStable(tier, func(i, j int) bool {
		a, b := tier[i].ID, tier[j].ID
		if classes[a] != classes[b] {
			return classes[a] < classes[b]
		}
		return impact[a] > impact[b]
	})
	return tier
}

// doerDependencyImpact counts unique active, materialized consumers behind each
// candidate. This is a demand heuristic, not a duration-based critical path.
// Build one reverse graph; each candidate walk is O(V+E), cycle-safe, and does
// not count hypothetical outputs or historical terminal tasks as extra demand.
func doerDependencyImpact(state *models.State, pr models.PipelineResolver, candidates []*models.Task) map[string]int {
	impact := make(map[string]int, len(candidates))
	if state == nil {
		return impact
	}
	active := make(map[string]*models.Task, len(state.Tasks))
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if !models.IsOperationallyTerminal(task, pr) {
			active[task.ID] = task
		}
	}
	reverse := make(map[string][]string)
	add := func(provider, consumer string) {
		if active[provider] != nil {
			reverse[provider] = append(reverse[provider], consumer)
		}
	}
	deps := models.NewDependencyResolver(state)
	for _, consumer := range active {
		for _, id := range consumer.DependsOn {
			resolved := deps.Resolve(id)
			if resolved.Satisfied() || resolved.Invalid() {
				continue
			}
			for _, blocker := range resolved.BlockingIDs {
				add(blocker, consumer.ID)
			}
		}
		// The readiness resolver validates retirement, bounds, Kind and child
		// provenance, but reports only the first pending child per declaration.
		// Expand each valid pending declaration to all selected active children.
		pending := make(map[string]bool)
		invalid := false
		for _, resolved := range models.UnmetProviderDependencies(consumer, state.Tasks, pr) {
			invalid = invalid || resolved.Invalid()
			pending[resolved.DependencyID] = true
		}
		if invalid {
			continue // A refused declaration set cannot contribute typed demand.
		}
		for _, declaration := range consumer.ProviderDependencies {
			if !pending[declaration.ProviderTask] {
				continue
			}
			_, children, err := models.ProviderDependencyChildren(declaration, pr)
			if err != nil {
				continue
			}
			add(declaration.ProviderTask, consumer.ID)
			for _, child := range children {
				add(child, consumer.ID)
			}
		}
	}
	for _, candidate := range candidates {
		seen := map[string]bool{candidate.ID: true}
		queue := append([]string(nil), reverse[candidate.ID]...)
		for len(queue) > 0 {
			id := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			if seen[id] {
				continue
			}
			seen[id] = true
			queue = append(queue, reverse[id]...)
		}
		impact[candidate.ID] = len(seen) - 1
	}
	return impact
}
