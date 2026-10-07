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
// Exceptions (ADR-0185, ADR-0186, ADR-0187, ADR-0188): an unexpanded plan's or
// draft output holds neither the provider it names directly nor a selected
// child retired permanently. That declaration goes stale; hand-off
// classification then requires the plan's replan, and generation refuses it
// (staleProviderReference); a draft is refused authoring, submission and
// approval until re-authored (staleOutputDeclaration). An unstarted consumer's
// task-level declaration holds nothing retired permanently either.
// routeStaleProviderConsumers then sends the consumers to re-authoring. A child
// retired by replan still holds: lineage would silently resolve the slot to
// the successor, hiding the staleness.
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
			released := func(direct bool) bool { return outputDeclarationReleased(state, resolver, task, direct, permanent) }
			for index, output := range task.Output {
				if holds(output.ProviderDependencies, released) {
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

// outputDeclarationReleased reports whether retiring a provider task's output
// declaration names (directly, or as a selected child) leaves it stale rather
// than held. It fails closed for draft output without a resolver.
func outputDeclarationReleased(state *models.State, resolver *pipeline.Resolver, task *models.Task, direct, permanent bool) bool {
	return (direct || permanent) && (models.PlanUnexpanded(state, task) || resolver != nil && models.DraftOutput(task, resolver))
}

// staleDeclaration describes dep declaring targetID, retired: directly, or as
// its selected child.
func staleDeclaration(dep models.ProviderDependency, targetID string, direct bool) string {
	if direct {
		return "retired provider " + targetID
	}
	return fmt.Sprintf("provider %s whose selected child %s was retired", dep.ProviderTask, targetID)
}

// routeStaleProviderConsumers sends each consumer a retirement of targetID
// leaves stale to re-authoring. Call it once the retirement passed
// rejectReferencedProviderRetirement, in the same transaction.
//   - A BLOCKED consumer whose draft output declares targetID gains a
//     re-authoring question and a provider_declaration_stale record. Both
//     change its assessment fingerprint, so the BLOCKED_TASKS wake re-triages
//     it even without an ordinary edge on targetID (ADR-0188). Every other
//     draft owner meets the set-task-output, submission or approval refusal on
//     its own path.
//   - On permanent retirement, unstarted task-level consumers are blocked
//     (blockStaleProviderConsumers, ADR-0187).
func routeStaleProviderConsumers(state *models.State, resolver *pipeline.Resolver, targetID string, retirement providerRetirement, actor string, now time.Time) error {
	permanent := retirement == retirePermanently
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.ID == targetID || task.Status != models.TaskStatusBlocked {
			continue
		}
		var declarations []string
		var indexes []int
		for index, output := range task.Output {
			for position, dep := range output.ProviderDependencies {
				direct, named := declaresProvider(state, resolver, dep, targetID)
				if !named || !outputDeclarationReleased(state, resolver, task, direct, permanent) {
					continue
				}
				declaration := fmt.Sprintf("output[%d].provider_dependencies[%d] declares %s", index, position, staleDeclaration(dep, targetID, direct))
				question := fmt.Sprintf("Re-author %s output[%d]: provider_dependencies[%d] declares %s; after unblocking, %s must select the replacement's output (submission and approval refuse the stale declaration).",
					task.ID, index, position, staleDeclaration(dep, targetID, direct), brand.Command("set-task-output"))
				if !slices.Contains(task.BlockedQuestions, question) {
					task.BlockedQuestions = append(task.BlockedQuestions, question)
				}
				declarations = append(declarations, declaration)
				indexes = append(indexes, index)
			}
		}
		if len(declarations) == 0 {
			continue
		}
		reason := strings.Join(declarations, "; ") + "; output indexes do not transfer to a replacement"
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventProviderDeclarationStale,
			Agent:  &actor,
			Reason: &reason,
			Extra:  map[string]any{"provider_retirement": targetID, "output_indexes": indexes},
		})
	}
	if !permanent {
		return nil
	}
	return blockStaleProviderConsumers(state, resolver, targetID, actor, now)
}

// staleOutputDeclaration names the first declaration in output that names a
// retired provider or retired selected child, or "". New output must not
// author one, and draft output carrying one must be re-authored before it is
// submitted or approved (ADR-0188).
func staleOutputDeclaration(state *models.State, resolver *pipeline.Resolver, output []models.OutputEntry) string {
	for index, entry := range output {
		for position, dep := range entry.ProviderDependencies {
			if id, child := staleProviderReference(state, resolver, dep); id != "" {
				return fmt.Sprintf("output[%d].provider_dependencies[%d] declares %s", index, position, staleDeclaration(dep, id, !child))
			}
		}
	}
	return ""
}

// rejectStaleDraftOutput refuses submitting output that still declares a
// retired provider or selected child (ADR-0188).
func rejectStaleDraftOutput(state *models.State, resolver *pipeline.Resolver, task *models.Task) error {
	if stale := staleOutputDeclaration(state, resolver, task.Output); stale != "" {
		return &PreconditionError{Reason: fmt.Sprintf("task %s %s; re-author it with %s before submitting", task.ID, stale, brand.Command("set-task-output", task.ID))}
	}
	return nil
}

// blockStaleProviderConsumers blocks each unstarted consumer in its initial
// status whose task-level declaration names targetID, which is being retired
// permanently, so the orchestrator is woken to re-author it (ADR-0187). An
// already BLOCKED one is in triage already, and unblocking it is refused while
// the declaration is stale.
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
				reason = fmt.Sprintf("provider_dependencies[%d] declares %s", position, staleDeclaration(dep, targetID, direct))
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

// staleProviderConsumerWarnings names each unexpanded plan or draft output
// that declares providerID directly: what retiring it leaves to be
// re-authored, by a replan of the plan or by the draft's author.
func staleProviderConsumerWarnings(state *models.State, resolver *pipeline.Resolver, providerID string) []string {
	var warnings []string
	for i := range state.Tasks {
		task := &state.Tasks[i]
		for index, output := range task.Output {
			if !slices.ContainsFunc(output.ProviderDependencies, func(dep models.ProviderDependency) bool { return dep.ProviderTask == providerID }) {
				continue
			}
			switch {
			case models.PlanUnexpanded(state, task):
				warnings = append(warnings, fmt.Sprintf("task %s output[%d] declares replanned provider %s; replan %s to re-author it", task.ID, index, providerID, task.ID))
			case resolver != nil && models.DraftOutput(task, resolver):
				warnings = append(warnings, fmt.Sprintf("task %s output[%d] declares replanned provider %s; re-author %s's output before it is submitted or approved", task.ID, index, providerID, task.ID))
			}
		}
	}
	return warnings
}
