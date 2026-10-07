package statevalidate

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// ValidateProviderDependencies checks explicit provider references and the
// dependency graph including children that have not yet been generated.
// It does not require future children to exist: admission holds them instead.
func ValidateProviderDependencies(state *models.State, resolver *pipeline.Resolver) error {
	return collectErr(func(v *violations) { validateProviderDependencies(v, state, resolver) })
}

func validateProviderDependencies(v *violations, state *models.State, resolver *pipeline.Resolver) {
	if state != nil && !models.HasProviderDependencies(state) {
		return
	}
	if state == nil || resolver == nil {
		v.add(fmt.Errorf("provider dependency validation requires state and pipeline"))
		return
	}
	graph := projectedProviderGraph(state, resolver)
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if !task.Status.IsTerminal() {
			unstarted := func() bool { return models.UnstartedProviderConsumer(task, resolver) }
			validateProviderOwner(v, state, resolver, task.ID, task.ProviderDependencies, unstarted)
			if executing, err := resolver.ExecutingStatus(task.RolePair); err == nil && task.Status == executing {
				for _, unmet := range models.UnmetProviderDependencies(task, state.Tasks, resolver) {
					v.addID("executing provider prerequisite "+task.ID+" "+unmet.DependencyID,
						fmt.Errorf("executing task %s has unmet provider dependency: %s", task.ID, unmet.Summary()))
				}
			}
		}
		if !providerOutputIsLive(state, resolver, task) {
			continue
		}
		mayGoStale := func() bool { return models.OutputMayGoStale(state, task, resolver) }
		for index, output := range task.Output {
			validateProviderOwner(v, state, resolver, fmt.Sprintf("%s output[%d]", task.ID, index), output.ProviderDependencies, mayGoStale)
		}
	}
	// Identify each cyclic effective edge, as ordinary dependency validation
	// does. A new edge can close another cycle through an already-cyclic provider
	// declaration; identifying only that declaration would hide the new damage.
	owners := make([]string, 0, len(graph.edges))
	for owner := range graph.edges {
		owners = append(owners, owner)
	}
	slices.Sort(owners)
	for _, owner := range owners {
		for _, target := range graph.edges[owner] {
			if path := shortestDependencyPath(graph.edges, target, owner); path != nil {
				cycle := &DependencyCycleError{CyclePath: append([]string{owner}, path...)}
				v.addID("provider dependency cycle edge "+owner+" -> "+target,
					fmt.Errorf("provider dependency cycle %s: %w", strings.Join(cycle.CyclePath, " -> "), cycle))
			}
		}
	}
}

// mayGoStale, when non-nil and true, marks reviewed output of a plan that has
// generated nothing, draft output, or a task-level declaration of a task
// nobody has started: a retired provider or selected child there is reconcile
// evidence, refused by hand-off classification, generation, claim and unblock,
// or by draft authoring, submission and approval, instead (ADR-0185, ADR-0186,
// ADR-0187, ADR-0188). Provenance, bounds and cycles are still validated.
func validateProviderOwner(v *violations, state *models.State, resolver *pipeline.Resolver, owner string, deps []models.ProviderDependency, mayGoStale func() bool) {
	if err := models.ValidateProviderDependencies(deps); err != nil {
		v.add(fmt.Errorf("task %s: %w", owner, err))
		return
	}
	stale := func() bool { return mayGoStale != nil && mayGoStale() }
	for index, dep := range deps {
		label := fmt.Sprintf("task %s provider_dependencies[%d]", owner, index)
		provider := state.FindTask(dep.ProviderTask)
		if provider == nil {
			v.add(fmt.Errorf("%s references non-existent provider %s", label, dep.ProviderTask))
			continue
		}
		transition, children, err := models.EffectiveProviderChildren(dep, state, resolver)
		if err != nil {
			v.add(fmt.Errorf("%s: %w", label, err))
			continue
		}
		if provider.RolePair != transition.SourceRolePair {
			v.add(fmt.Errorf("%s transition %s does not originate from provider role_pair %s", label, dep.Transition, provider.RolePair))
		}
		if models.ProviderRetired(provider) && !stale() {
			v.add(fmt.Errorf("%s references retired provider %s", label, provider.ID))
		}
		for position, output := range dep.Outputs {
			if output >= len(provider.Output) {
				if len(provider.Output) > 0 || provider.Status == models.TaskStatusMerged {
					v.add(fmt.Errorf("%s output %d is outside provider %s output range", label, output, provider.ID))
				}
			} else if provider.Output[output].Kind != "" {
				v.add(fmt.Errorf("%s output %d has Kind and may be deduplicated; its child identity is not stable", label, output))
			}
			if child := state.FindTask(children[position]); child != nil {
				parents := child.EffectiveParentTasks()
				if child.RolePair != transition.TargetRolePair || len(parents) != 1 || parents[0] != provider.ID {
					v.add(fmt.Errorf("%s child %s has incorrect provider provenance or role_pair", label, child.ID))
				}
				if models.ProviderRetired(child) && !stale() {
					v.add(fmt.Errorf("%s references retired provider child %s", label, child.ID))
				}
			}
		}
	}
}

// Live output is generation input, including a partially recovered MERGED
// producer. Retired output and fully materialized output are audit records.
func providerOutputIsLive(state *models.State, resolver *pipeline.Resolver, task *models.Task) bool {
	if len(task.Output) == 0 || task.Status == models.TaskStatusSuperseded || task.Status == models.TaskStatusAbandoned ||
		task.TransitionsExecuted["replanned"] || task.PlanHandoffRetired() {
		return false
	}
	if task.Status != models.TaskStatusMerged {
		return true
	}
	for _, transition := range resolver.AllTransitions() {
		source, err := resolver.TransitionSourceRolePair(transition.Name)
		if err != nil || source != task.RolePair || transition.Cardinality != "per-subtask" {
			continue
		}
		if !task.TransitionsExecuted[transition.Name] {
			return true
		}
		ids, skipped, _ := models.ResolveOutputSiblings(task.Output, models.NonTerminalTasksByKind(state, nil), task.ID, transition.TaskSlugOrName())
		for index, id := range ids {
			child := state.FindTask(id)
			if child == nil {
				return true
			}
			if _, dedup := skipped[index]; dedup && id != fmt.Sprintf("%s-%s-%d", task.ID, transition.TaskSlugOrName(), index) {
				continue
			}
			if !child.Status.IsTerminal() && !models.ProviderDependenciesEqual(child.ProviderDependencies, task.Output[index].ProviderDependencies) {
				return true
			}
		}
	}
	return false
}

type providerGraph struct {
	edges map[string][]string
}

func (g *providerGraph) add(from, to string) {
	if !slices.Contains(g.edges[from], to) {
		g.edges[from] = append(g.edges[from], to)
	}
}

func (g *providerGraph) declare(state *models.State, resolver *pipeline.Resolver, owner string, deps []models.ProviderDependency) {
	for _, dep := range deps {
		_, children, err := models.EffectiveProviderChildren(dep, state, resolver)
		if err != nil {
			continue // The declaration validator reports this malformed edge.
		}
		provider := state.FindTask(dep.ProviderTask)
		if provider == nil {
			continue
		}
		if provider.Status != models.TaskStatusMerged {
			g.add(owner, provider.ID)
		}
		for _, id := range children {
			g.add(owner, id)
			if state.FindTask(id) == nil && provider.Status != models.TaskStatusMerged {
				g.add(id, provider.ID)
			}
		}
	}
}

func projectedProviderGraph(state *models.State, resolver *pipeline.Resolver) providerGraph {
	g := providerGraph{edges: map[string][]string{}}
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.Status.IsTerminal() {
			continue
		}
		for _, id := range task.DependsOn {
			g.add(task.ID, id)
		}
		g.declare(state, resolver, task.ID, task.ProviderDependencies)
	}
	for i := range state.Tasks {
		parent := &state.Tasks[i]
		if !providerOutputIsLive(state, resolver, parent) {
			continue
		}
		retiring := map[string]bool{}
		for _, output := range parent.Output {
			retiring[output.Supersedes] = output.Supersedes != ""
		}
		for _, transition := range resolver.AllTransitions() {
			source, err := resolver.TransitionSourceRolePair(transition.Name)
			if err != nil || source != parent.RolePair || transition.Cardinality != "per-subtask" {
				continue
			}
			ids, skipped, _ := models.ResolveOutputSiblings(parent.Output, models.NonTerminalTasksByKind(state, retiring), parent.ID, transition.TaskSlugOrName())
			for index, entry := range parent.Output {
				if _, dedup := skipped[index]; dedup {
					continue
				}
				id := ids[index]
				if state.FindTask(id) != nil {
					continue // Actual child dependencies are authoritative after generation.
				}
				if parent.Status != models.TaskStatusMerged {
					g.add(id, parent.ID)
				}
				for _, sibling := range entry.DependsOn {
					position, err := strconv.Atoi(sibling)
					if err == nil && position >= 0 && position < len(ids) {
						g.add(id, ids[position])
					}
				}
				for _, dep := range entry.TaskDependsOn {
					g.add(id, dep)
				}
				g.declare(state, resolver, id, entry.ProviderDependencies)
				projectInheritedProviderEdges(&g, state, parent, entry, transition, id)
			}
		}
	}
	return g
}

func projectInheritedProviderEdges(g *providerGraph, state *models.State, parent *models.Task, entry models.OutputEntry, transition pipeline.TransitionDef, child string) {
	if entry.InheritInputs.InheritsNothing() {
		return
	}
	for _, upstreamID := range parent.DependsOn {
		upstream := state.FindTask(upstreamID)
		if upstream == nil || upstream.RolePair != parent.RolePair || !upstream.TransitionsExecuted[transition.Name] || upstream.TransitionsExecuted["replanned"] || upstream.PlanHandoffRetired() {
			continue
		}
		ids, _, _ := models.ResolveOutputSiblings(upstream.Output, models.NonTerminalTasksByKind(state, nil), upstream.ID, transition.TaskSlugOrName())
		addInherited := func(id string) {
			if entry.Supersedes != "" {
				for _, output := range parent.Output {
					if output.Supersedes == id {
						return
					}
				}
			}
			g.add(child, id)
		}
		if entry.InheritInputs.IsSelective() {
			for _, selection := range entry.InheritInputs.Selections {
				if selection.UpstreamTask != upstreamID {
					continue
				}
				for _, position := range selection.Outputs {
					if position >= 0 && position < len(ids) {
						addInherited(ids[position])
					}
				}
			}
		} else {
			for _, id := range ids {
				addInherited(id)
			}
		}
	}
}
