package ops

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// Plan-declared replacement (D62, ADR-0161): an output[] entry's supersedes
// names the existing task its generated child replaces. The transition that
// generates the child also retires the original and retargets its consumers,
// in one validated candidate state, or changes nothing.

// Per-blackboard barriers let tests fail a plan-replacement transaction after
// its first retirement without changing the shared transition primitives.
var planReplacementTestHooks sync.Map

type planReplacementTestHook struct {
	afterRetire func(originalID string) error
}

// planReplacementActor records engine-driven retirements, like the other
// supervisor repairs that have no acting agent.
const planReplacementActor = "system"

// planReplacements maps each original, in output order, to the children
// generated for it. One original may be split across several outputs.
type planReplacements struct {
	originals []string
	children  map[string][]string
}

// declaredOriginals returns the tasks a plan's outputs supersede.
func declaredOriginals(entries []models.OutputEntry) map[string]bool {
	originals := make(map[string]bool)
	for _, entry := range entries {
		if entry.Supersedes != "" {
			originals[entry.Supersedes] = true
		}
	}
	return originals
}

// excludeRetiring drops the originals a plan's outputs retire from a
// replacement's inherited phase-gate dependencies. A corrective plan usually
// depends on the plan whose children it replaces, so default inheritance would
// make each replacement wait on the tasks being retired; supersession would
// then rewrite those edges into a self-dependency and sibling cycles. Order
// among the replacements stays expressed by their sibling depends_on. A
// non-replacing output keeps them: supersession retargets its edges to the
// replacements, so it still waits for the work that takes their place.
func excludeRetiring(deps []string, entry models.OutputEntry, retiring map[string]bool) []string {
	if entry.Supersedes == "" || len(retiring) == 0 {
		return deps
	}
	return slices.DeleteFunc(slices.Clone(deps), func(id string) bool { return retiring[id] })
}

// groupPlanReplacements pairs every superseding output with its child ID. It
// runs before any child is appended: an output deduplicated onto another
// in-flight task has no child of its own to take the original's place.
func groupPlanReplacements(entries []models.OutputEntry, siblingIDs []string, skipped map[int]string) (*planReplacements, error) {
	set := &planReplacements{children: make(map[string][]string)}
	for i, entry := range entries {
		if entry.Supersedes == "" {
			continue
		}
		if reason, ok := skipped[i]; ok {
			return nil, fmt.Errorf("output[%d] supersedes %s but generates no child of its own (%s)", i, entry.Supersedes, reason)
		}
		if _, seen := set.children[entry.Supersedes]; !seen {
			set.originals = append(set.originals, entry.Supersedes)
		}
		set.children[entry.Supersedes] = append(set.children[entry.Supersedes], siblingIDs[i])
	}
	if len(set.originals) == 0 {
		return nil, nil
	}
	return set, nil
}

// replacementEligible reports whether a task may be retired by a replacement:
// its role-pair initial or rejected status, BLOCKED, or INTEGRATION_FAILED.
// replace-task applies the same rule to its source.
func replacementEligible(task *models.Task, resolver *pipeline.Resolver) bool {
	if task.Status == models.TaskStatusBlocked || task.Status == models.TaskStatusIntegrationFailed {
		return true
	}
	if task.RolePair == "" {
		return task.Status == models.TaskStatusReady || task.Status == models.TaskStatusRejected
	}
	if resolver == nil {
		return false
	}
	initial, err := resolver.InitialStatus(task.RolePair)
	if err == nil && task.Status == initial {
		return true
	}
	rejected, err := resolver.RejectedStatus(task.RolePair)
	return err == nil && task.Status == rejected
}

// retiredByChildren reports an original already retired by exactly these
// children, which is how a completed replacement looks to crash recovery.
func retiredByChildren(task *models.Task, children []string) bool {
	if task.Status != models.TaskStatusSuperseded {
		return false
	}
	for _, child := range children {
		if !slices.Contains(task.SupersededBy, child) {
			return false
		}
	}
	return true
}

// proceedTransaction runs one transition. A plan whose outputs supersede
// existing tasks runs on a deep copy of the state that is validated whole and
// adopted only on success: the shared transition pass records a failed
// transition and keeps going, so a retirement applied before a later failure
// would otherwise persist as the half-applied replacement D62 describes.
//
// Validation skips spec-file disk checks: a generation pass must not refuse on
// artifact files the transition does not touch.
func proceedTransaction(bb *db.Blackboard, s *models.State, projectRoot, taskID, transitionName string, tDef transitionDef, inheritedDeps inheritedDepSet, resolver *pipeline.Resolver, now time.Time, result *ProceedResult) error {
	plan := s.FindTask(taskID)
	if plan == nil || tDef.cardinality != "per-subtask" || len(declaredOriginals(plan.Output)) == 0 {
		return proceedInner(s, taskID, transitionName, tDef, inheritedDeps, resolver, now, result)
	}
	candidate := db.CloneState(s)
	err := proceedInner(candidate, taskID, transitionName, tDef, inheritedDeps, resolver, now, result)
	alreadyExecuted := errors.Is(err, errTransitionAlreadyExecuted)
	if err != nil && !alreadyExecuted {
		return err
	}
	// A refused candidate is discarded; so is everything it reported.
	discard := func(refusal error) error {
		result.ChildTaskIDs, result.RetiredTaskIDs, result.retiredWorktrees = nil, nil, nil
		return refusal
	}
	if result.replacements != nil {
		if applyErr := applyPlanReplacements(bb, candidate, taskID, resolver, result, now); applyErr != nil {
			return discard(applyErr)
		}
	}
	if validateErr := statevalidate.ValidateState(candidate, projectRoot, true, io.Discard); validateErr != nil {
		return discard(fmt.Errorf("plan replacement leaves an invalid state: %w", validateErr))
	}
	*s = *candidate
	return err
}

// applyPlanReplacements retires each declared original with the children
// generated for it, through the audited supersession path that also prunes
// its illegal edges and retargets its consumers. An original already retired
// by those children is left alone; any other ineligible original refuses the
// whole transition. Under crash recovery the marker proves the retirement was
// committed with it, so a live original there is refused too.
func applyPlanReplacements(bb *db.Blackboard, s *models.State, planID string, resolver *pipeline.Resolver, result *ProceedResult, now time.Time) error {
	var hook planReplacementTestHook
	if loaded, ok := planReplacementTestHooks.Load(bb); ok {
		hook = loaded.(planReplacementTestHook)
	}
	pb := &pipelineBundle{pr: resolver, resolver: resolver, transitions: BuildPipelineTransitions(resolver)}
	set := result.replacements
	for _, originalID := range set.originals {
		children := set.children[originalID]
		original := s.FindTask(originalID)
		if original == nil {
			return fmt.Errorf("output supersedes %s, which does not exist", originalID)
		}
		if retiredByChildren(original, children) {
			continue
		}
		if result.recovering {
			return fmt.Errorf("output supersedes %s (%s), but the transition already executed without retiring it; the state was edited by hand", originalID, original.Status)
		}
		child := s.FindTask(children[0])
		if !replacementEligible(original, resolver) || child == nil || original.RolePair != child.RolePair {
			return fmt.Errorf("output supersedes %s (%s, role pair %s), which cannot be retired by a %s replacement", originalID, original.Status, original.RolePair, rolePairOf(child))
		}
		if original.Worktree != nil {
			result.retiredWorktrees = append(result.retiredWorktrees, originalID)
		}
		reason := fmt.Sprintf("replaced by plan %s", planID)
		if _, err := supersedeTaskInState(s, pb, original, children, reason, planReplacementActor, nil, now); err != nil {
			return fmt.Errorf("retire %s: %w", originalID, err)
		}
		result.RetiredTaskIDs = append(result.RetiredTaskIDs, originalID)
		if hook.afterRetire != nil {
			if err := hook.afterRetire(originalID); err != nil {
				return err
			}
		}
	}
	recordPlanReplacements(s.FindTask(planID), set, result.RetiredTaskIDs)
	return nil
}

// recordPlanReplacements adds the retirements to the plan's own transition
// event, the audit record of the generation they were committed with.
func recordPlanReplacements(plan *models.Task, set *planReplacements, retiredIDs []string) {
	if len(retiredIDs) == 0 {
		return
	}
	retired := make(map[string]any, len(retiredIDs))
	for _, id := range retiredIDs {
		retired[id] = slices.Clone(set.children[id])
	}
	for i := len(plan.History) - 1; i >= 0; i-- {
		event := plan.History[i].Event
		if event == models.TaskEventTransitionExecuted || event == models.TaskEventTransitionCrashRecov {
			if plan.History[i].Extra == nil {
				plan.History[i].Extra = make(map[string]any)
			}
			plan.History[i].Extra["superseded"] = retired
			return
		}
	}
}

func rolePairOf(task *models.Task) string {
	if task == nil {
		return "missing"
	}
	return task.RolePair
}

// cleanupRetiredOriginals removes retired originals' worktree directories and
// releases predecessor branches after the state commit, like supersede-task.
// Failures are warnings: the retirement is already durable.
func cleanupRetiredOriginals(bb *db.Blackboard, projectRoot string, results []ProceedResult) []string {
	var warnings []string
	gw := git.New(projectRoot)
	for _, result := range results {
		for _, id := range result.retiredWorktrees {
			if err := gw.RemoveWorktreeDir(id); err != nil {
				warnings = append(warnings, fmt.Sprintf("failed to remove worktree directory of retired %s: %v", id, err))
			}
		}
		for _, id := range result.RetiredTaskIDs {
			warnings = append(warnings, cleanupPredecessorBranches(bb, gw, id)...)
		}
	}
	return warnings
}
