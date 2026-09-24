package ops

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// PlanHandoffClass classifies a merged planning task for the orchestrator's
// plan-to-children hand-off. One classification feeds wake rendering, the
// PLANNING_COMPLETE verifier, plan-check admission, and transition admission,
// so none of them can disagree about what a plan still needs.
type PlanHandoffClass string

const (
	// PlanHandoffNotSource: no hand-off left to run (see Pending).
	PlanHandoffNotSource PlanHandoffClass = ""
	// PlanHandoffOutOfDomain: a source with no reviewed hand-off (auto-only,
	// many-to-one or empty output). Behaves as before D73.
	PlanHandoffOutOfDomain PlanHandoffClass = "out_of_domain"
	// PlanHandoffNeedsReview: in domain, no disposition yet.
	PlanHandoffNeedsReview PlanHandoffClass = "needs_review"
	// PlanHandoffPassed: passed and every in-domain upstream is admissible.
	PlanHandoffPassed PlanHandoffClass = "passed"
	// PlanHandoffWaitingUpstream: passed, but an in-domain upstream still
	// awaits its own review.
	PlanHandoffWaitingUpstream PlanHandoffClass = "waiting_upstream"
	// PlanHandoffNeedsReconciliation: passed, but an in-domain upstream was
	// replanned or held afterwards; the plan must be replanned or held.
	PlanHandoffNeedsReconciliation PlanHandoffClass = "needs_reconciliation"
	// PlanHandoffHeld: held for a human action.
	PlanHandoffHeld PlanHandoffClass = "held"
)

// PlanHandoffDomain identifies the reviewed hand-off: manual per-subtask or
// one-to-one transitions out of a planning pair whose task has output[]. Only
// these transitions need an orchestrator disposition before they run
// automatically. The zero domain with only planning pairs (PlanningPairsOnly)
// gates nothing and classifies every source as before D73.
type PlanHandoffDomain struct {
	planningPairs    map[string]bool
	gatedTransitions map[string]bool
	gatedByPair      map[string][]string
}

// PlanningPairsOnly is a domain with no reviewed hand-off, for callers that
// know only the planning pairs.
func PlanningPairsOnly(planningPairs map[string]bool) PlanHandoffDomain {
	return PlanHandoffDomain{planningPairs: planningPairs}
}

// NewPlanHandoffDomain derives the reviewed hand-off domain from the pipeline.
func NewPlanHandoffDomain(resolver *pipeline.Resolver) PlanHandoffDomain {
	domain := PlanHandoffDomain{
		planningPairs:    resolver.TransitionSourcePairs(),
		gatedTransitions: make(map[string]bool),
		gatedByPair:      make(map[string][]string),
	}
	for _, td := range resolver.AllTransitions() {
		if td.Trigger != "manual" || (td.Cardinality != "per-subtask" && td.Cardinality != "one-to-one") {
			continue
		}
		source, err := resolver.TransitionSourceRolePair(td.Name)
		if err != nil {
			continue
		}
		domain.gatedTransitions[td.Name] = true
		domain.gatedByPair[source] = append(domain.gatedByPair[source], td.Name)
	}
	return domain
}

// LoadPlanHandoffDomain loads the project's pipeline and derives its domain.
func LoadPlanHandoffDomain(projectRoot string) (PlanHandoffDomain, error) {
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return PlanHandoffDomain{}, err
	}
	return NewPlanHandoffDomain(resolver), nil
}

// InDomain reports whether task's hand-off requires an orchestrator
// disposition. A replanned task stays in domain so its consumers still see it
// as an inadmissible upstream.
func (d PlanHandoffDomain) InDomain(task *models.Task) bool {
	return task != nil && len(task.Output) > 0 && len(d.gatedByPair[task.RolePair]) > 0
}

// Pending reports whether task still has a hand-off to run. In the domain,
// that is a gated transition not yet executed, so another outgoing transition
// (an auto one) firing first does not consume it; elsewhere it is unconsumed
// planning output. A replanned task has none.
func (d PlanHandoffDomain) Pending(task *models.Task) bool {
	if !d.InDomain(task) {
		return IsUnconsumedPlanningOutput(task, d.planningPairs)
	}
	if task.Status != models.TaskStatusMerged || task.TransitionsExecuted["replanned"] {
		return false
	}
	for _, name := range d.gatedByPair[task.RolePair] {
		if !task.TransitionsExecuted[name] {
			return true
		}
	}
	return false
}

// PlanningCompleteEligible reports whether task wakes PLANNING_COMPLETE: a
// pending hand-off that is not held for a human and not cycle-blocked,
// directly or through an upstream. A passed task stays eligible until it
// transitions, so a crash between its pass and the checkpoint re-wakes it.
func (d PlanHandoffDomain) PlanningCompleteEligible(state *models.State, task *models.Task) bool {
	return d.Pending(task) &&
		task.PlanCheckVerdictOf() != models.PlanCheckHeld &&
		!IsTransitionCycleBlocked(task) &&
		!HasCycleBlockedDependency(task, state)
}

// HasHeldPlan reports whether a planned task is held for a human action. A
// held plan keeps the sprint open: its children do not exist yet.
func HasHeldPlan(state *models.State) bool {
	for _, id := range state.Sprint.Scope.Planned {
		task := state.FindTask(id)
		if task != nil && task.Status == models.TaskStatusMerged &&
			task.PlanCheckVerdictOf() == models.PlanCheckHeld && !task.TransitionsExecuted["replanned"] {
			return true
		}
	}
	return false
}

// GatesTransition reports whether running transitionName from task needs an
// orchestrator disposition.
func (d PlanHandoffDomain) GatesTransition(task *models.Task, transitionName string) bool {
	return d.InDomain(task) && d.gatedTransitions[transitionName]
}

// Classify returns task's hand-off class and, for classes that name one, the
// blocker (the hold's ask, or the upstream chain that prevents admission).
// A needs_review task also reports the upstream blocker that would refuse a
// pass.
func (d PlanHandoffDomain) Classify(state *models.State, task *models.Task) (PlanHandoffClass, string) {
	if !d.Pending(task) {
		return PlanHandoffNotSource, ""
	}
	if !d.InDomain(task) {
		return PlanHandoffOutOfDomain, ""
	}
	switch task.PlanCheckVerdictOf() {
	case models.PlanCheckHeld:
		return PlanHandoffHeld, task.PlanCheck.Ask
	case models.PlanCheckPassed:
		switch kind, blocker := d.upstreamBlocker(state, task, map[string]bool{task.ID: true}); kind {
		case upstreamReconcile:
			return PlanHandoffNeedsReconciliation, blocker
		case upstreamWaiting:
			return PlanHandoffWaitingUpstream, blocker
		default:
			return PlanHandoffPassed, ""
		}
	default:
		_, blocker := d.upstreamBlocker(state, task, map[string]bool{task.ID: true})
		return PlanHandoffNeedsReview, blocker
	}
}

type upstreamKind int

const (
	upstreamAdmissible upstreamKind = iota
	upstreamWaiting
	upstreamReconcile
)

// upstreamBlocker walks task's in-domain planning dependencies. A replanned
// or held upstream, or a passed one blocked by either, needs reconciliation;
// an upstream awaiting review makes the task wait. Dependencies outside the
// domain, non-merged ones and already-transitioned ones are admissible, as
// before D73; so is one whose gated hand-off already ran. The worst finding
// wins; the first of that kind is reported.
func (d PlanHandoffDomain) upstreamBlocker(state *models.State, task *models.Task, visiting map[string]bool) (upstreamKind, string) {
	worst, blocker := upstreamAdmissible, ""
	note := func(kind upstreamKind, text string) {
		if kind > worst {
			worst, blocker = kind, text
		}
	}
	for _, depID := range task.DependsOn {
		dep := state.FindTask(depID)
		if visiting[depID] || !d.InDomain(dep) || dep.Status != models.TaskStatusMerged {
			continue
		}
		if dep.TransitionsExecuted["replanned"] {
			note(upstreamReconcile, fmt.Sprintf("upstream %s was replanned", depID))
			continue
		}
		if !d.Pending(dep) {
			continue
		}
		switch dep.PlanCheckVerdictOf() {
		case models.PlanCheckHeld:
			note(upstreamReconcile, fmt.Sprintf("upstream %s is held: %s", depID, dep.PlanCheck.Ask))
		case models.PlanCheckPassed:
			visiting[depID] = true
			kind, inner := d.upstreamBlocker(state, dep, visiting)
			delete(visiting, depID)
			if kind != upstreamAdmissible {
				note(kind, fmt.Sprintf("upstream %s: %s", depID, inner))
			}
		default:
			note(upstreamWaiting, fmt.Sprintf("upstream %s awaits plan review", depID))
		}
	}
	return worst, blocker
}
