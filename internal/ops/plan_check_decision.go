package ops

import (
	stderrors "errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// planCheckChange is a decided disposition change. check is the new
// disposition (nil clears it), note the history entry that records it, and
// changed false a replay or no-op that persists nothing.
type planCheckChange struct {
	check   *models.PlanCheck
	note    string
	changed bool
}

// decidePlanCheck is the read-side half of a plan-check request: it takes the
// domain classification of the admitted task and returns the change to
// persist. Only retirement routes stale provider consumers, so it is the one
// branch that writes state; every other branch leaves state untouched.
func decidePlanCheck(state *models.State, resolver *pipeline.Resolver, domain PlanHandoffDomain, task *models.Task, class PlanHandoffClass, blocker string, input PlanCheckInput, actor string) (planCheckChange, error) {
	retiring := input.Action == PlanCheckActionReplace || input.Action == PlanCheckActionRetire
	switch {
	case input.Action == PlanCheckActionRetire && task.PlanCheckVerdictOf() == models.PlanCheckRetired &&
		strings.TrimSpace(task.PlanCheck.Ruling) == "":
		return restoredRetirementRuling(task, input), nil
	case retiring:
		return decideRetirement(state, resolver, domain, task, input, actor)
	case input.Action == PlanCheckActionClear && task.PlanCheckVerdictOf() == models.PlanCheckHeld &&
		(class == PlanHandoffNotSource || class == PlanHandoffOutOfDomain):
		// A hold placed while the plan still awaited hand-off outlives it
		// once the plan expands or its fence lifts. Clearing only drops the
		// disposition: a plan that awaits no hand-off generates nothing.
		return planCheckChange{note: "hold cleared (plan no longer awaits hand-off)", changed: true}, nil
	}
	return decideDisposition(state, resolver, domain, task, class, blocker, input, actor)
}

// restoredRetirementRuling handles a binary predating the ruling field, which
// dropped it on rewrite. The hand-off is already retired, so this only
// restores the ruling and keeps the retirement's actor and time; the history
// entry records who restored it and when.
func restoredRetirementRuling(task *models.Task, input PlanCheckInput) planCheckChange {
	ruling := strings.TrimSpace(input.Ruling)
	check := *task.PlanCheck
	check.Ruling = ruling
	return planCheckChange{check: &check, note: "retirement ruling restored: " + ruling, changed: true}
}

// decideRetirement ends an unused hand-off, then routes the consumers of any
// provider the retirement leaves stale.
func decideRetirement(state *models.State, resolver *pipeline.Resolver, domain PlanHandoffDomain, task *models.Task, input PlanCheckInput, actor string) (planCheckChange, error) {
	if err := rejectReferencedProviderRetirement(state, resolver, task.ID, retirePermanently); err != nil {
		return planCheckChange{}, err
	}
	next, err := retirePlanHandoff(state, domain, task, input, actor)
	if err == nil && next.changed {
		err = routeStaleProviderConsumers(state, resolver, task.ID, retirePermanently, actor, time.Now().UTC())
	}
	return next, err
}

// decideDisposition judges a pass, hold or clear against the plan's class:
// a plan outside the reviewed hand-off carries no disposition, otherwise the
// transition of the current verdict decides, and a pass additionally meets
// the replacement barrier.
func decideDisposition(state *models.State, resolver *pipeline.Resolver, domain PlanHandoffDomain, task *models.Task, class PlanHandoffClass, blocker string, input PlanCheckInput, actor string) (planCheckChange, error) {
	switch class {
	case PlanHandoffNotSource:
		return planCheckChange{}, &PreconditionError{Reason: fmt.Sprintf(
			"task %s has no plan awaiting hand-off: it must be a MERGED planning task with output whose children do not exist yet and that was not replanned", task.ID)}
	case PlanHandoffOutOfDomain:
		return planCheckChange{}, &PreconditionError{Reason: fmt.Sprintf(
			"task %s has no reviewed hand-off (its transitions are automatic, many-to-one, or it has no output); it transitions without a plan-check", task.ID)}
	}
	next, err := planCheckTransition(task, class, blocker, input, actor)
	var refusal *PreconditionError
	if input.Action == PlanCheckActionPass && stderrors.As(err, &refusal) {
		refusal.Reason += lineageRepairHint(state, resolver, task)
	}
	if input.Action == PlanCheckActionPass && err == nil && next.changed {
		err = planReplacementPassBarrier(state, resolver, domain, task)
	}
	return next, err
}

// retirePlanHandoff ends an unused hand-off by a merged correction (replace)
// or by a ruling that leaves it with no replacement (retire). Both share the
// unused-hand-off checks; only retire may end a hold, and only replace names
// a successor.
func retirePlanHandoff(state *models.State, domain PlanHandoffDomain, original *models.Task, input PlanCheckInput, actor string) (planCheckChange, error) {
	refuse := func(reason string) (planCheckChange, error) {
		return planCheckChange{}, &PreconditionError{Reason: reason}
	}
	retire := input.Action == PlanCheckActionRetire
	ruling := strings.TrimSpace(input.Ruling)
	if original.PlanHandoffRetired() {
		verdict := original.PlanCheckVerdictOf()
		if retire && verdict == models.PlanCheckRetired && original.PlanCheck.Ruling == ruling ||
			!retire && verdict == models.PlanCheckReplaced && original.PlanCheck.ReplacedBy == input.ReplacedBy {
			return planCheckChange{}, nil
		}
		return refuse(fmt.Sprintf("task %s handoff was already retired by %s", original.ID, original.RetiredBy()))
	}
	if !domain.InDomain(original) || !domain.Pending(original) {
		if !retire {
			return refuse(fmt.Sprintf("task %s must have an unused MERGED reviewed handoff", original.ID))
		}
		if reason := deliveredCohortlessSourceRefusal(state, domain.resolver, original); reason != "" {
			return refuse(fmt.Sprintf("task %s must have an unused MERGED reviewed handoff, or be an outputless many-to-one source outside any cohort whose downstream work merged: %s", original.ID, reason))
		}
	}
	if original.PlanCheckVerdictOf() == models.PlanCheckHeld && !retire {
		return refuse(fmt.Sprintf("task %s is held; operator plan-check %s --clear is required first", original.ID, original.ID))
	}
	for name, executed := range original.TransitionsExecuted {
		if executed {
			return refuse(fmt.Sprintf("task %s already executed %s; delivered output cannot be retired", original.ID, name))
		}
	}
	for i := range state.Tasks {
		candidate := &state.Tasks[i]
		if slices.Contains(candidate.EffectiveParentTasks(), original.ID) {
			return refuse(fmt.Sprintf("task %s already generated child %s", original.ID, candidate.ID))
		}
		if candidate.PlanHandoffRetired() || candidate.TransitionsExecuted[models.ReplannedTransition] ||
			(candidate.Status.IsTerminal() && !domain.Pending(candidate)) {
			continue
		}
		if candidate.ID != input.ReplacedBy && candidate.RolePair == original.RolePair &&
			slices.Contains(candidate.DependsOn, original.ID) {
			// Its transition inherits only from same-pair upstreams that ran it: the
			// original never will, and nothing redirects the edge to the correction.
			if retire {
				return refuse(fmt.Sprintf("task %s depends on %s and would inherit no phase gate once it is retired; retarget or replan it before retirement", candidate.ID, original.ID))
			}
			return refuse(fmt.Sprintf("task %s depends on %s and would inherit no phase gate from %s; retarget or replan it before retirement", candidate.ID, original.ID, input.ReplacedBy))
		}
		for index, output := range candidate.Output {
			if output.InheritInputs == nil {
				continue
			}
			for selection, selected := range output.InheritInputs.Selections {
				if selected.UpstreamTask == original.ID {
					return refuse(fmt.Sprintf("task %s output[%d].inherit_inputs.selections[%d] still selects %s; retarget and review that plan before retirement", candidate.ID, index, selection, original.ID))
				}
			}
		}
	}
	if retire {
		return planCheckChange{
			check: &models.PlanCheck{Verdict: models.PlanCheckRetired, Ruling: ruling, By: actor, At: time.Now().UTC()},
			note:  "unused handoff retired by ruling " + ruling, changed: true,
		}, nil
	}
	correction := state.FindTask(input.ReplacedBy)
	if correction == nil || correction.ID == original.ID || correction.Status != models.TaskStatusMerged ||
		correction.RolePair != original.RolePair || !domain.InDomain(correction) || correction.PlanHandoffRetired() ||
		correction.TransitionsExecuted[models.ReplannedTransition] || correction.PlanCheckVerdictOf() == models.PlanCheckHeld {
		return refuse("replacement must be a distinct MERGED correction with output in the same reviewed role-pair, neither held, replanned nor retired")
	}
	return planCheckChange{
		check: &models.PlanCheck{Verdict: models.PlanCheckReplaced, ReplacedBy: correction.ID, By: actor, At: time.Now().UTC()},
		note:  "unused handoff replaced by " + correction.ID, changed: true,
	}, nil
}

// deliveredCohortlessSourceRefusal admits, for retire only, an outputless
// source of a many-to-one transition that has no cohort parent: the engine can
// never run that transition for it (D-225). The ruling judges delivery; its
// direct downstream tasks in other role-pairs are the evidence it rests on, at
// least one merged and none live. It returns why task does not qualify, or "".
func deliveredCohortlessSourceRefusal(state *models.State, resolver *pipeline.Resolver, task *models.Task) string {
	switch {
	case task.Status != models.TaskStatusMerged:
		return "it is not MERGED"
	case len(task.Output) > 0:
		return "it has output"
	case task.CohortParentID() != "":
		return "it belongs to the cohort of " + task.CohortParentID()
	case !hasUnexecutedManyToOne(resolver, task):
		return "no unexecuted many-to-one transition leaves " + task.RolePair
	}
	delivered := false
	for i := range state.Tasks {
		dependent := &state.Tasks[i]
		if dependent.RolePair == task.RolePair || !slices.Contains(dependent.DependsOn, task.ID) {
			continue
		}
		if !dependent.Status.IsTerminal() {
			return fmt.Sprintf("downstream task %s is not terminal", dependent.ID)
		}
		delivered = delivered || dependent.Status == models.TaskStatusMerged
	}
	if !delivered {
		return fmt.Sprintf("no MERGED downstream task outside %s depends on it", task.RolePair)
	}
	return ""
}

func hasUnexecutedManyToOne(resolver *pipeline.Resolver, task *models.Task) bool {
	if resolver == nil {
		return false
	}
	for _, transition := range resolver.AllTransitions() {
		if transition.Cardinality != models.CardinalityManyToOne || task.TransitionsExecuted[transition.Name] || !transition.AppliesTo(task.Output) {
			continue
		}
		if source, err := resolver.TransitionSourceRolePair(transition.Name); err == nil && source == task.RolePair {
			return true
		}
	}
	return false
}

func planCheckTransition(task *models.Task, class PlanHandoffClass, blocker string, input PlanCheckInput, actor string) (planCheckChange, error) {
	current := task.PlanCheckVerdictOf()
	now := time.Now().UTC()
	switch input.Action {
	case PlanCheckActionPass:
		switch {
		case current == models.PlanCheckHeld:
			return planCheckChange{}, &PreconditionError{Reason: fmt.Sprintf(
				"task %s is held for human action (%s); only an operator plan-check %s --clear releases it", task.ID, task.PlanCheck.Ask, task.ID)}
		case current == models.PlanCheckPassed:
			if !slices.Equal(task.PlanCheck.Notes, input.Notes) {
				return planCheckChange{}, &PreconditionError{Reason: "passed plan has different validation notes; an operator must clear and recheck it before generation"}
			}
			return planCheckChange{}, nil
		case class == PlanHandoffNeedsReview && blocker != "":
			return planCheckChange{}, &PreconditionError{Reason: fmt.Sprintf(
				"task %s cannot pass while %s; replan or hold it instead", task.ID, blocker)}
		}
		return planCheckChange{
			check:   &models.PlanCheck{Verdict: models.PlanCheckPassed, By: actor, At: now, Notes: slices.Clone(input.Notes)},
			note:    "passed",
			changed: true,
		}, nil
	case PlanCheckActionHold:
		ask := strings.TrimSpace(input.Ask)
		if current == models.PlanCheckHeld {
			if task.PlanCheck.Ask == ask {
				return planCheckChange{}, nil
			}
			return planCheckChange{}, &PreconditionError{Reason: fmt.Sprintf(
				"task %s is already held for %q; an operator must clear it before a different hold", task.ID, task.PlanCheck.Ask)}
		}
		return planCheckChange{
			check:   &models.PlanCheck{Verdict: models.PlanCheckHeld, Ask: ask, By: actor, At: now},
			note:    "held: " + ask,
			changed: true,
		}, nil
	default: // clear
		if current == "" {
			return planCheckChange{}, nil
		}
		return planCheckChange{note: "cleared " + string(current), changed: true}, nil
	}
}

// lineageRepairHint names the retarget-dependency repair for each direct edge
// to a replanned upstream that mergedPlanLineageRepair would admit, so a
// refused pass points at the metadata repair instead of a replan.
func lineageRepairHint(state *models.State, resolver *pipeline.Resolver, task *models.Task) string {
	var hints []string
	for _, dep := range task.DependsOn {
		successor, ok := liveReplanSuccessor(state, dep)
		if ok && mergedPlanLineageRepair(state, resolver, task, dep, []string{successor.ID}) == nil {
			hints = append(hints, fmt.Sprintf("retarget-dependency %s %s %s", task.ID, dep, successor.ID))
		}
	}
	if len(hints) == 0 {
		return ""
	}
	return "; its output already targets the replan successor, so repair the stale edge instead: " + strings.Join(hints, " and ")
}
