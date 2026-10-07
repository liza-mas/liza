package ops

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// providerRetirement says whether retiring a task mints a replan successor.
// Only replan does; every other retirement (supersession, cancellation,
// plan-check replacement) is permanent: the task can never be replanned.
type providerRetirement bool

const (
	retirePermanently providerRetirement = false
	retireByReplan    providerRetirement = true
)

// Output indexes are identities in one reviewed provider, not transferable
// positions in a replacement. Retire live consumer declarations first. The
// refusal names every holder, so all of them can be handled at once.
//
// Exceptions (ADR-0185, ADR-0186, ADR-0187): an unexpanded plan's output holds
// neither the provider it names directly nor a selected child retired
// permanently. That declaration goes stale; hand-off classification then
// requires the plan's replan, and generation refuses it
// (staleProviderReference). An unstarted consumer's task-level declaration
// holds nothing retired permanently either; blockStaleProviderConsumers then
// sends it to re-authoring. A child retired by replan still holds: lineage
// would silently resolve the slot to the successor, hiding the staleness.
func rejectReferencedProviderRetirement(state *models.State, resolver *pipeline.Resolver, targetID string, retirement providerRetirement) error {
	permanent := retirement == retirePermanently
	holds := func(deps []models.ProviderDependency, released func(direct bool) bool) bool {
		return slices.ContainsFunc(deps, func(dep models.ProviderDependency) bool {
			direct, named := declaresProvider(state, resolver, dep, targetID)
			return named && !released(direct)
		})
	}
	var holders []string
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.ID == targetID {
			continue
		}
		if !task.Status.IsTerminal() && holds(task.ProviderDependencies, func(bool) bool {
			return permanent && unstartedProviderConsumer(task, resolver)
		}) {
			holders = append(holders, task.ID)
		}
		if !task.TransitionsExecuted["replanned"] && !task.PlanHandoffRetired() && operationalOutputMayBeConsumed(state, resolver, task) {
			unexpanded := func(direct bool) bool { return (direct || permanent) && models.PlanUnexpanded(state, task) }
			for index, output := range task.Output {
				if holds(output.ProviderDependencies, unexpanded) {
					holders = append(holders, fmt.Sprintf("%s output[%d]", task.ID, index))
				}
			}
		}
	}
	switch len(holders) {
	case 0:
		return nil
	case 1:
		return &PreconditionError{Reason: fmt.Sprintf("cannot retire provider %s while %s has a live provider_dependencies declaration; replace or retire that consumer declaration first", targetID, holders[0])}
	}
	return &PreconditionError{Reason: fmt.Sprintf("cannot retire provider %s while %s have live provider_dependencies declarations; replace or retire those consumer declarations first", targetID, strings.Join(holders, ", "))}
}

// declaresProvider reports whether dep names targetID, directly or as one of
// its selected effective children.
func declaresProvider(state *models.State, resolver *pipeline.Resolver, dep models.ProviderDependency, targetID string) (direct, named bool) {
	if dep.ProviderTask == targetID {
		return true, true
	}
	_, children, err := models.EffectiveProviderChildren(dep, state, resolver)
	return false, err == nil && slices.Contains(children, targetID)
}

// unstartedProviderConsumer fails closed without a resolver.
func unstartedProviderConsumer(task *models.Task, resolver *pipeline.Resolver) bool {
	return resolver != nil && models.UnstartedProviderConsumer(task, resolver)
}

// blockStaleProviderConsumers blocks each unstarted consumer in its initial
// status whose task-level declaration names targetID, which is being retired
// permanently, so the orchestrator is woken to re-author it (ADR-0187). An
// already BLOCKED one is in triage already, and unblocking it is refused while
// the declaration is stale. Call it only once the retirement passed
// rejectReferencedProviderRetirement, in the same transaction.
func blockStaleProviderConsumers(state *models.State, resolver *pipeline.Resolver, targetID, actor string, now time.Time) error {
	transitions := BuildPipelineTransitions(resolver)
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.ID == targetID || task.Status == models.TaskStatusBlocked || !unstartedProviderConsumer(task, resolver) {
			continue
		}
		index := -1
		var reason string
		for position, dep := range task.ProviderDependencies {
			if direct, named := declaresProvider(state, resolver, dep, targetID); named {
				index = position
				reason = fmt.Sprintf("provider_dependencies[%d] declares provider %s whose selected child %s was retired", position, dep.ProviderTask, targetID)
				if direct {
					reason = fmt.Sprintf("provider_dependencies[%d] declares retired provider %s", position, targetID)
				}
				break
			}
		}
		if index < 0 {
			continue
		}
		reason += "; output indexes do not transfer to a replacement"
		question := fmt.Sprintf("Re-author %s: %s it with a task whose provider_dependencies select the replacement's output, or %s it.",
			task.ID, brand.Command("replace-task"), brand.Command("cancel-task"))
		if err := task.TransitionWith(models.TaskStatusBlocked, transitions); err != nil {
			return err
		}
		task.BlockedReason = &reason
		task.BlockedQuestions = []string{question}
		models.AdvanceLifecycle(task)
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventBlocked,
			Agent:  &actor,
			Reason: &reason,
			Extra:  map[string]any{"provider_retirement": targetID},
		})
	}
	return nil
}

// staleProviderReference names what leaves an unexpanded plan's declaration
// stale: the declared provider when retired, else the first existing selected
// child that is (child true). Without a resolver, or for a selection that does
// not resolve, only the provider is judged; validation reports the rest.
func staleProviderReference(state *models.State, resolver *pipeline.Resolver, dep models.ProviderDependency) (id string, child bool) {
	if provider := state.FindTask(dep.ProviderTask); provider != nil && models.ProviderRetired(provider) {
		return provider.ID, false
	}
	if resolver == nil {
		return "", false
	}
	_, children, err := models.EffectiveProviderChildren(dep, state, resolver)
	if err != nil {
		return "", false
	}
	for _, childID := range children {
		if task := state.FindTask(childID); task != nil && models.ProviderRetired(task) {
			return childID, true
		}
	}
	return "", false
}

// staleProviderConsumerWarnings names each unexpanded plan output that
// declares providerID directly: what retiring it leaves to be re-authored.
func staleProviderConsumerWarnings(state *models.State, providerID string) []string {
	var warnings []string
	for i := range state.Tasks {
		task := &state.Tasks[i]
		for index, output := range task.Output {
			if slices.ContainsFunc(output.ProviderDependencies, func(dep models.ProviderDependency) bool { return dep.ProviderTask == providerID }) &&
				models.PlanUnexpanded(state, task) {
				warnings = append(warnings, fmt.Sprintf("task %s output[%d] declares replanned provider %s; replan %s to re-author it", task.ID, index, providerID, task.ID))
			}
		}
	}
	return warnings
}
