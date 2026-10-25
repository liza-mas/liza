package ops

import (
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// replacementAudit is what the candidate edit leaves for the projection: the
// source's status before its supersession, the consumers it rewrote, and the
// audit entry's Extra, whose new transition token the projection fills in.
type replacementAudit struct {
	originalStatus models.TaskStatus
	consumers      []string
	extra          map[string]any
}

// decide applies the eligibility and consumer decisions to the locked state and
// returns the replacement it admits. The held pre-lock verdicts are reported
// here, in today's order, after replay had its chance.
func (p *replacePass) decide(candidate *models.State, source *models.Task) (models.Task, error) {
	if p.baseErr != nil {
		return models.Task{}, p.baseErr
	}
	if !replacementEligible(source, p.pb.resolver) {
		return models.Task{}, WrapLifecycleError(replaceTaskOperation, p.observed, fmt.Errorf("source is no longer eligible for replacement"), models.LifecycleAlreadyTransitioned, LifecycleSafeActionStop, LifecycleEffectsNone)
	}
	if candidate.FindTask(p.input.Replacement.ID) != nil {
		return models.Task{}, replacementConflict(source, p.request, replaceFieldReplacementID, fmt.Errorf("replacement task ID already exists"))
	}
	replacement, err := buildReplacementTask(&p.input.Replacement, p.pb.resolver)
	if err != nil {
		return models.Task{}, err
	}
	inheritReplacementLineage(&replacement, source)
	// The acceptance check ran on a pre-lock snapshot. Its result holds
	// only while the parent evidence it read is unchanged.
	if p.acceptance.parentFence != "" {
		current, err := acceptanceParentFence(candidate, &replacement)
		if err != nil || current != p.acceptance.parentFence {
			return models.Task{}, WrapLifecycleError(replaceTaskOperation, p.observed, fmt.Errorf("replacement allocation evidence changed during its acceptance check"), models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone)
		}
	}
	if err := p.acceptance.lifecycleError(replaceTaskOperation, p.observed); err != nil {
		return models.Task{}, err
	}
	if err := p.runtimeInputVerdict(replaceTaskOperation, p.observed); err != nil {
		return models.Task{}, err
	}
	return replacement, nil
}

// replace inserts the replacement, rewrites the consumers and supersedes the
// source in the candidate, then records the replacement_committed audit entry.
// It returns the candidate's source task, which insertion may have moved.
func (p *replacePass) replace(candidate *models.State, replacement models.Task) (*models.Task, replacementAudit, error) {
	if p.base != nil {
		replacement.BaseCommit = &p.base.BaseCommit
		replacement.Worktree = &p.base.Worktree
	}
	if err := insertTaskInState(candidate, p.projectRoot, replacement, &p.input.Replacement, p.pb.resolver); err != nil {
		return nil, replacementAudit{}, err
	}
	// Appending to Tasks may move its backing array.
	source := candidate.FindTask(p.input.SourceTaskID)
	now := time.Now().UTC()
	// Both cores append dependency-rewrite events, including output-only
	// canonicalization. Snapshot cursors so old rewrites cannot leak in.
	historyStarts := make(map[string]int, len(candidate.Tasks))
	for _, task := range candidate.Tasks {
		historyStarts[task.ID] = len(task.History)
	}
	if _, err := applyDependencyUpdatesInState(candidate, p.pb.resolver, source, replaceTaskOperation, p.input.Consumers, p.authority.ID, p.input.Reason, now, map[string]any{"operation": replaceTaskOperation}); err != nil {
		return nil, replacementAudit{}, err
	}
	if hook := replaceTestHooks(p.bb); hook.afterUpdates != nil {
		if err := hook.afterUpdates(candidate); err != nil {
			return nil, replacementAudit{}, err
		}
	}
	p.hadWorktree = source.Worktree != nil
	originalStatus := source.Status
	// Recording the successor on the supersession itself lets the
	// retirement barrier and consumer routing inside it see the
	// selected-child slot follow the replacement (D-70).
	replacedBy := map[string]any{models.ReplacementTaskIDExtra: replacement.ID}
	if _, err := supersedeTaskInState(candidate, p.pb, source, []string{p.input.Replacement.ID}, p.input.Reason, p.input.Changed, p.authority.ID, replacedBy, now); err != nil {
		return nil, replacementAudit{}, err
	}
	consumers := replacementRewrittenConsumers(candidate, source.ID, historyStarts)
	extra := map[string]any{"source_task_id": source.ID, models.ReplacementTaskIDExtra: replacement.ID, "source_prior_transition_id": p.request.ExpectedTransition, "retargeted_consumers": consumers, "request_id": p.request.RequestID}
	if p.base != nil {
		extra["preserved_base_commit"] = p.base.BaseCommit
	}
	source.History = append(source.History, models.TaskHistoryEntry{Time: now, Event: models.TaskEventReplacementCommitted, Agent: &p.authority.ID, Extra: extra})
	return source, replacementAudit{originalStatus: originalStatus, consumers: consumers, extra: extra}, nil
}

// project validates the complete candidate, completes the lifecycle request
// and reports the committed replacement graph.
func (p *replacePass) project(candidate *models.State, source *models.Task, replacementID string, audit replacementAudit) error {
	if err := statevalidate.ValidateCandidate(candidate, p.bb.ReadSnapshot, p.projectRoot, false, os.Stderr); err != nil {
		return err
	}
	outcome, err := CompleteLifecycleRequest(source, p.request, models.LifecycleProjection{SourceStatus: audit.originalStatus}, candidate.Agents)
	if err != nil {
		return err
	}
	// Extra is excluded from the transition digest; append the event before
	// completion, then fill its new token without changing the boundary.
	audit.extra["source_new_transition_id"] = outcome.TransitionID
	p.result = &ReplaceTaskResult{LifecycleOutcome: outcome, SourceTaskID: source.ID, ReplacementTaskID: replacementID, SourceOriginalStatus: audit.originalStatus, RetargetedConsumers: audit.consumers}
	return nil
}

// inheritReplacementLineage applies the replacement's allocation lineage. A
// same-pair replacement continues the source's allocation: keep its parent
// lineage so fan-in cohorts and parent-scoped checks still see the work, and
// its arch_ref, the architecture scope the payload cannot restate (D-67).
// epic_ref is not inherited; replacement is how a broken one is dropped.
// The successor takes over the source's writer placement (D-80): its
// provider_reservations, so it cannot race the writers placed before it, and,
// for the same pair, a max_outputs the payload omits.
func inheritReplacementLineage(replacement, source *models.Task) {
	replacement.ProviderReservations = slices.Clone(source.ProviderReservations)
	if replacement.RolePair == source.RolePair {
		replacement.ParentTask = cloneStringPtr(source.ParentTask)
		replacement.ParentTasks = slices.Clone(source.ParentTasks)
		replacement.ArchRef = source.ArchRef
		if replacement.MaxOutputs == 0 {
			replacement.MaxOutputs = source.MaxOutputs
		}
	}
}

func replacementRewrittenConsumers(state *models.State, sourceID string, historyStarts map[string]int) []string {
	consumers := []string{}
	for _, task := range state.Tasks {
		if task.ID == sourceID {
			continue
		}
		for _, entry := range task.History[historyStarts[task.ID]:] {
			if entry.Event == models.TaskEventDependenciesRewritten {
				consumers = append(consumers, task.ID)
				break // Explicit updates and supersession may rewrite the same task.
			}
		}
	}
	slices.Sort(consumers)
	return consumers
}
