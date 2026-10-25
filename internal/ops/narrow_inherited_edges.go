package ops

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// narrowPass is one locked narrowing of a producer's generated children. It
// holds the producer, its authored selections and the phase-gate edges they
// were computed against.
type narrowPass struct {
	state      *models.State
	resolver   *pipeline.Resolver
	producer   *models.Task
	transition string
	slug       string
	inherited  inheritedDepSet
	// wholePhase is every child of every upstream: what a child inherited
	// before it declared intent.
	wholePhase []string
	now        time.Time
	agentID    string
	reason     string
}

// newNarrowPass checks the producer is narrowable, authors the selections onto
// it and computes the inheritance they leave, in that order: the persisted
// selection is what the rewrite is computed from.
func newNarrowPass(state *models.State, producer *models.Task, transition string, resolver *pipeline.Resolver, req narrowRequest, now time.Time) (*narrowPass, error) {
	slug, err := checkNarrowProducer(producer, transition, resolver)
	if err != nil {
		return nil, err
	}
	if err := authorNarrowSelections(producer, req.selections); err != nil {
		return nil, err
	}
	inherited, err := computeInheritedDeps(state, producer, transition, resolver)
	if err != nil {
		return nil, &PreconditionError{Reason: err.Error()}
	}
	return &narrowPass{
		state: state, resolver: resolver, producer: producer, transition: transition, slug: slug,
		inherited: inherited, wholePhase: dedupeStrings(inherited.all),
		now: now, agentID: req.agentID, reason: req.reason,
	}, nil
}

// checkNarrowProducer refuses a producer whose children cannot be narrowed and
// returns the slug its per-subtask children are named by.
func checkNarrowProducer(producer *models.Task, transition string, resolver *pipeline.Resolver) (string, error) {
	if producer.Status != models.TaskStatusMerged {
		return "", &PreconditionError{Reason: fmt.Sprintf("producer %s must be MERGED to narrow its generated children (status %s)", producer.ID, producer.Status)}
	}
	if producer.TransitionsExecuted[models.ReplannedTransition] {
		return "", &PreconditionError{Reason: fmt.Sprintf("producer %s was replanned; narrow the replacement planning task instead", producer.ID)}
	}
	if !producer.TransitionsExecuted[transition] {
		return "", &PreconditionError{Reason: fmt.Sprintf("producer %s has not executed transition %q; there are no generated children to narrow", producer.ID, transition)}
	}
	if len(producer.Output) == 0 {
		return "", &PreconditionError{Reason: fmt.Sprintf("producer %s has no output[] entries", producer.ID)}
	}
	td, err := resolver.Transition(transition)
	if err != nil {
		return "", &PreconditionError{Reason: fmt.Sprintf("unknown transition %q: %v", transition, err)}
	}
	if td.Cardinality != models.CardinalityPerSubtask {
		return "", &PreconditionError{Reason: fmt.Sprintf("transition %q has cardinality %q; only per-subtask fan-out can be narrowed", transition, td.Cardinality)}
	}
	return td.TaskSlugOrName(), nil
}

// authorNarrowSelections persists each selection's intent on the producer's
// output entry. Any failure fails the whole callback, so no earlier selection
// survives.
func authorNarrowSelections(producer *models.Task, selections []NarrowSelection) error {
	for _, selection := range selections {
		if selection.Index >= len(producer.Output) {
			return &PreconditionError{Reason: fmt.Sprintf("selection index %d is out of range: producer %s has %d output(s)", selection.Index, producer.ID, len(producer.Output))}
		}
		if err := models.ValidateInheritInputs(selection.InheritInputs, selection.Index); err != nil {
			return &PreconditionError{Reason: err.Error()}
		}
		producer.Output[selection.Index].InheritInputs = cloneInheritInputs(selection.InheritInputs)
	}
	return nil
}

// run narrows every selected child and reports each one.
func (p *narrowPass) run(selections []NarrowSelection) (NarrowInheritedDependenciesResult, error) {
	result := NarrowInheritedDependenciesResult{ProducerID: p.producer.ID, Transition: p.transition}
	selected := make(map[int]bool, len(selections))
	for _, selection := range selections {
		selected[selection.Index] = true
	}
	for i, entry := range p.producer.Output {
		if !selected[i] {
			continue
		}
		report, err := p.narrowChild(i, entry)
		if err != nil {
			return result, err
		}
		result.Children = append(result.Children, report)
		switch narrowAction(report.Action) {
		case narrowActionNarrowed:
			result.Narrowed++
		case narrowActionUnchanged:
			result.Unchanged++
		case narrowActionSkipped:
			result.Skipped++
		}
	}
	return result, nil
}

// narrowChild skips, leaves or rewrites the child generated for output i. A
// skipped child is never planned, so its edges are never read.
func (p *narrowPass) narrowChild(i int, entry models.OutputEntry) (NarrowedChild, error) {
	childID := perSubtaskChildID(p.producer.ID, p.slug, i)
	child := p.state.FindTask(childID)
	if child == nil {
		return NarrowedChild{}, &PreconditionError{Reason: fmt.Sprintf("producer %s has transition %q executed but child %s is missing (needs crash recovery)", p.producer.ID, p.transition, childID)}
	}
	report := NarrowedChild{TaskID: childID, OutputIndex: i, Status: string(child.Status)}
	if skip := narrowSkipReason(child, p.resolver, p.now); skip != "" {
		report.Action = string(narrowActionSkipped)
		report.SkipReason = skip
		return report, nil
	}
	plan, err := p.planChild(child, entry, i)
	if err != nil {
		return report, err
	}
	report.CanonicalDependencies = append([]string(nil), plan.canonical...)
	if plan.unchangedFor(child) {
		report.Action = string(narrowActionUnchanged)
		return report, nil
	}
	sort.Strings(plan.removed)
	sort.Strings(plan.added)
	report.Action = string(narrowActionNarrowed)
	report.RemovedDependencies = plan.removed
	report.AddedDependencies = plan.added
	p.adoptChild(child, i, plan)
	return report, nil
}

// narrowChildPlan is one child's rewritten dependency list.
type narrowChildPlan struct {
	removed   []string
	added     []string
	canonical []string
}

func (plan narrowChildPlan) unchangedFor(child *models.Task) bool {
	return len(plan.removed) == 0 && len(plan.added) == 0 && slices.Equal(plan.canonical, child.DependsOn)
}

// planChild computes the child's narrowed, canonical and direction-checked
// dependency list without touching the child.
func (p *narrowPass) planChild(child *models.Task, entry models.OutputEntry, i int) (narrowChildPlan, error) {
	keep, err := p.inherited.forEntry(entry, i)
	if err != nil {
		return narrowChildPlan{}, &PreconditionError{Reason: err.Error()}
	}
	keep = dedupeStrings(keep)
	protected, err := p.protectedEdges(entry, keep)
	if err != nil {
		return narrowChildPlan{}, err
	}
	removable, err := p.removableEdges(protected)
	if err != nil {
		return narrowChildPlan{}, err
	}

	var plan narrowChildPlan
	var next []string
	for _, dep := range child.DependsOn {
		if removable[dep] {
			plan.removed = append(plan.removed, dep)
			continue
		}
		next = append(next, dep)
	}
	for _, dep := range keep {
		if !slices.Contains(next, dep) {
			next = append(next, dep)
			plan.added = append(plan.added, dep)
		}
	}
	next = dedupeStrings(next)
	plan.canonical, _, err = canonicalizeConcreteDependencyList(p.state, p.resolver, child.ID, child.RolePair, next)
	if err != nil {
		return narrowChildPlan{}, err
	}
	if err := validateDependencyDirection(p.state, p.resolver, child.ID, child.RolePair, plan.canonical); err != nil {
		return narrowChildPlan{}, err
	}
	return plan, nil
}

// protectedEdges is every edge the entry keeps or declares itself. Sibling
// indexes and concrete task_depends_on are protected even when the same ID also
// arrived through inheritance: the planner named them.
func (p *narrowPass) protectedEdges(entry models.OutputEntry, keep []string) (map[string]bool, error) {
	protected := make(map[string]bool, len(keep)+len(entry.DependsOn)+len(entry.TaskDependsOn))
	for _, dep := range keep {
		protected[dep] = true
	}
	for _, ref := range entry.DependsOn {
		idx, convErr := strconv.Atoi(ref)
		if convErr == nil && idx >= 0 && idx < len(p.producer.Output) {
			protected[perSubtaskChildID(p.producer.ID, p.slug, idx)] = true
		}
	}
	for _, dep := range entry.TaskDependsOn {
		protected[dep] = true
		canonical, _, err := canonicalizeDependencyID(p.state, dep, nil)
		if err != nil {
			return nil, &PreconditionError{Reason: fmt.Sprintf("cannot canonicalize concrete dependency %s: %v", dep, err)}
		}
		for _, c := range canonical {
			protected[c] = true
		}
	}
	return protected, nil
}

// removableEdges are exactly the whole-phase inherited children the entry does
// not protect, in both their generated and their canonical (post-supersession)
// spellings. Nothing else on the child is touched.
func (p *narrowPass) removableEdges(protected map[string]bool) (map[string]bool, error) {
	removable := make(map[string]bool)
	for _, dep := range p.wholePhase {
		if protected[dep] {
			continue
		}
		canonical, _, err := canonicalizeDependencyID(p.state, dep, nil)
		if err != nil {
			return nil, &PreconditionError{Reason: fmt.Sprintf("cannot canonicalize inherited dependency %s: %v", dep, err)}
		}
		if slices.ContainsFunc(canonical, func(c string) bool { return protected[c] }) {
			continue
		}
		removable[dep] = true
		for _, c := range canonical {
			removable[c] = true
		}
	}
	return removable, nil
}

// adoptChild assigns the planned list and appends the child's audit entry.
func (p *narrowPass) adoptChild(child *models.Task, outputIndex int, plan narrowChildPlan) {
	child.DependsOn = plan.canonical
	note := fmt.Sprintf("narrowed inherited dependencies from %s output[%d]: removed %d, added %d", p.producer.ID, outputIndex, len(plan.removed), len(plan.added))
	child.History = append(child.History, models.TaskHistoryEntry{
		Time:   p.now,
		Event:  models.TaskEventDependenciesRewritten,
		Agent:  &p.agentID,
		Reason: &p.reason,
		Note:   &note,
		Extra: map[string]any{
			"manual":                 true,
			"operation":              narrowInheritedDependenciesOperation,
			"producer":               p.producer.ID,
			"transition":             p.transition,
			"output_index":           outputIndex,
			"removed_dependencies":   append([]string(nil), plan.removed...),
			"added_dependencies":     append([]string(nil), plan.added...),
			"canonical_dependencies": append([]string(nil), plan.canonical...),
		},
	})
}

// auditProducer records the authored selections on the producer's history.
func (p *narrowPass) auditProducer(result NarrowInheritedDependenciesResult, selections []NarrowSelection) {
	note := fmt.Sprintf("authored inherit_inputs on %d output(s); narrowed %d, unchanged %d, skipped %d",
		len(selections), result.Narrowed, result.Unchanged, result.Skipped)
	p.producer.History = append(p.producer.History, models.TaskHistoryEntry{
		Time:   p.now,
		Event:  models.TaskEventDependenciesRewritten,
		Agent:  &p.agentID,
		Reason: &p.reason,
		Note:   &note,
		Extra: map[string]any{
			"manual":     true,
			"operation":  narrowInheritedDependenciesOperation,
			"transition": p.transition,
			"outputs":    selectedIndexList(selections),
		},
	})
}

// narrowSkipReason returns why a generated child must not be rewritten, or ""
// when it is safe to narrow. Only children still in their role-pair's initial
// status with no live doer claim are rewritten: anything claimed, executing,
// under review, blocked, or terminal has scheduler state that a dependency
// rewrite would race with.
func narrowSkipReason(child *models.Task, resolver *pipeline.Resolver, now time.Time) string {
	if child.Status.IsTerminal() {
		return fmt.Sprintf("terminal status %s", child.Status)
	}
	initial, err := resolver.InitialStatus(child.RolePair)
	if err != nil {
		return fmt.Sprintf("cannot resolve initial status for role_pair %q: %v", child.RolePair, err)
	}
	if child.Status != initial {
		return fmt.Sprintf("status %s is not the initial status %s", child.Status, initial)
	}
	if child.AssignedTo != nil && !strings.HasPrefix(*child.AssignedTo, "$") &&
		child.LeaseExpires != nil && child.LeaseExpires.After(now) {
		return fmt.Sprintf("claimed by %s with a live lease", *child.AssignedTo)
	}
	return ""
}
