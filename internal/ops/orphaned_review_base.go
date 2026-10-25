package ops

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"time"

	"github.com/liza-mas/liza/internal/db"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// D-26 / ADR-0200: a reviewable task whose base_commit left the integration
// branch (a merge rolled back or redone, or a history rewrite) can never pass
// review-boundary validation again, and update-review-commit alone would pull
// the orphan's content into the review range. HealOrphanedReviewBase replays
// only the task's own commits onto integration and re-records the boundary,
// or routes the task to integration recovery when that is not provably safe.

const healOrphanedReviewBaseOperation = "heal-orphaned-review-base"

// History Extra keys only the heal writes, beside the shared review_commit_updated keys.
const (
	orphanedBaseHealExtraKey = "orphaned_base_heal"
	preparedByExtraKey       = "prepared_by"
)

// Test hooks. orphanHealAfterRebaseTestHook returning true simulates a
// process that ends as soon as its rebase returns: no restore, no retirement.
// orphanHealPublishTestHook wraps the publication transaction.
var (
	orphanHealAfterRebaseTestHook func() bool
	orphanHealPublishTestHook     func(bb *db.Blackboard, publish func() error) error
	errOrphanHealAbandonedForTest = stderrors.New("orphaned-base heal abandoned by test hook")
)

// OrphanedBaseHealOutcome is what one heal invocation did to the task.
type OrphanedBaseHealOutcome string

const (
	// OrphanedBaseHealNotApplicable: no orphaned base, or not this operation's
	// to repair; nothing was written.
	OrphanedBaseHealNotApplicable OrphanedBaseHealOutcome = "not_applicable"
	// OrphanedBaseHealed: the boundary now records the replayed commits.
	OrphanedBaseHealed OrphanedBaseHealOutcome = "healed"
	// OrphanedBaseHealIntegrationFailed: an unsafe case went to INTEGRATION_FAILED
	// with the manual repair in its diagnostic; the worktree is unchanged.
	OrphanedBaseHealIntegrationFailed OrphanedBaseHealOutcome = "integration_failed"
)

// OrphanedBaseHealInput names the reviewer performing the heal and the task.
type OrphanedBaseHealInput struct {
	ProjectRoot string
	AgentID     string
	Role        string
	Authority   *models.AgentAuthority
	TaskID      string
}

// OrphanedBaseHealFencedError reports a heal preparation whose effect is
// neither proven nor proven absent. Preparation and worktree are left for
// inspection (INVARIANTS §7 "Neither").
type OrphanedBaseHealFencedError struct {
	TaskID       string
	Reason       string
	RecoveryHint string
}

func (e *OrphanedBaseHealFencedError) Error() string {
	return fmt.Sprintf("task %s orphaned-base heal is fenced: %s — %s", e.TaskID, e.Reason, e.RecoveryHint)
}

// OrphanedBaseRestoreError reports a worktree that could not be returned to
// its submitted HEAD after a failed heal. The preparation is retained.
type OrphanedBaseRestoreError struct {
	TaskID  string
	Command string
	Err     error
}

func (e *OrphanedBaseRestoreError) Error() string {
	return fmt.Sprintf("task %s worktree not restored after a failed orphaned-base heal (%v); run %s", e.TaskID, e.Err, e.Command)
}

func (e *OrphanedBaseRestoreError) Unwrap() error { return e.Err }

// orphanHealPayload is the exact intended effect; its digest is the
// preparation's payload digest, which lets a later invocation prove a found
// effect is this one.
type orphanHealPayload struct {
	OldReviewCommit string `json:"old_review_commit"`
	OldBaseCommit   string `json:"old_base_commit"`
	Target          string `json:"target"`
}

func (p orphanHealPayload) digest() string {
	data, _ := json.Marshal(p) // Fixed string fields only.
	return lifecycleDigest(data)
}

// pendingOrphanedBaseHeal returns the task's heal preparation when it still
// fences the task's current boundary.
func pendingOrphanedBaseHeal(task *models.Task) *models.LifecyclePreparation {
	if task == nil || task.Lifecycle == nil || task.Lifecycle.Preparation == nil {
		return nil
	}
	p := task.Lifecycle.Preparation
	if p.Operation != healOrphanedReviewBaseOperation || p.Boundary != models.TaskTransitionID(task) {
		return nil
	}
	copied := *p
	return &copied
}

// orphanedBaseHealPending reports an unsettled heal on the task.
func orphanedBaseHealPending(task *models.Task) bool {
	return pendingOrphanedBaseHeal(task) != nil
}

// HealOrphanedReviewBase repairs one reviewable task whose base_commit is no
// longer on the integration branch. Lock order follows submission: project
// lifecycle (shared), then the per-task claim lock for the whole invocation,
// then the state lock per transaction. Holding the task lock throughout means
// a heal preparation found under it belongs to an invocation that has ended,
// and is settled from Git before anything else.
func HealOrphanedReviewBase(input OrphanedBaseHealInput) (OrphanedBaseHealOutcome, error) {
	if input.ProjectRoot == "" || input.TaskID == "" || input.AgentID == "" || input.Role == "" {
		return OrphanedBaseHealNotApplicable, &PreconditionError{Reason: "orphaned-base heal requires project root, task, agent and role"}
	}
	statePath := paths.New(input.ProjectRoot).StatePath()
	outcome := OrphanedBaseHealNotApplicable
	err := WithProjectLifecycleSharedLock(input.ProjectRoot, healOrphanedReviewBaseOperation, func() error {
		lock := filelock.New(claimTaskWorktreeLockPath(statePath, input.TaskID))
		return lock.WithLockOperation(healOrphanedReviewBaseOperation, func() error {
			h := &orphanHeal{in: input, bb: db.For(statePath), g: git.New(input.ProjectRoot)}
			var inner error
			outcome, inner = h.run()
			return inner
		})
	})
	return outcome, err
}

type orphanHeal struct {
	in          OrphanedBaseHealInput
	bb          *db.Blackboard
	g           *git.Git
	wtPath      string
	integration string
	submitted   models.TaskStatus
	partial     models.TaskStatus
	transitions map[models.TaskStatus][]models.TaskStatus
	reviewerOf  func(rolePair string) (string, error)
}

func (h *orphanHeal) run() (OrphanedBaseHealOutcome, error) {
	state, task, err := readTaskState(h.bb, h.in.TaskID)
	if err != nil {
		return OrphanedBaseHealNotApplicable, err
	}
	resolver, _, err := loadResolver(h.in.ProjectRoot)
	if err != nil {
		return OrphanedBaseHealNotApplicable, err
	}
	h.reviewerOf = resolver.ReviewerRole
	h.transitions = BuildPipelineTransitions(resolver)
	h.integration = state.Config.IntegrationBranch
	if task.RolePair == "" {
		return OrphanedBaseHealNotApplicable, nil
	}
	if h.submitted, err = resolver.SubmittedStatus(task.RolePair); err != nil {
		return OrphanedBaseHealNotApplicable, err
	}
	h.partial, _ = resolver.PartiallyApprovedStatus(task.RolePair)
	if err := h.admit(state, task); err != nil {
		return OrphanedBaseHealNotApplicable, err
	}
	if !h.inScope(task) {
		return OrphanedBaseHealNotApplicable, nil
	}
	if err := h.checkPath(task); err != nil {
		return OrphanedBaseHealNotApplicable, err
	}
	if prepared := pendingOrphanedBaseHeal(task); prepared != nil {
		return h.settle(task, prepared)
	}
	target, err := h.g.GetCommitSHA(h.integration)
	if err != nil {
		return OrphanedBaseHealNotApplicable, err
	}
	if !reviewBaseOrphaned(h.g, *task.BaseCommit, *task.ReviewCommit, target) {
		return OrphanedBaseHealNotApplicable, nil
	}
	return h.fresh(task, nil, target)
}

// admit is the pre-effect authorization, repeated inside every transaction
// that commits a new effect: current generation, a registered reviewer of the
// task's role pair, and work admission (INVARIANTS §5).
func (h *orphanHeal) admit(state *models.State, task *models.Task) error {
	if h.in.Authority != nil {
		if err := RequireAgentAuthority(state, *h.in.Authority); err != nil {
			return err
		}
	}
	if task == nil {
		return &lizaerrors.NotFoundError{Entity: "task", ID: h.in.TaskID}
	}
	if _, err := requireRegisteredClaimAgent(state, h.in.AgentID, h.in.Role); err != nil {
		return err
	}
	if role, err := h.reviewerOf(task.RolePair); err != nil || role != h.in.Role {
		return &PreconditionError{Reason: fmt.Sprintf("agent role %s cannot review task %s", h.in.Role, task.ID)}
	}
	return RequireWorkAdmitted(state, healOrphanedReviewBaseOperation)
}

func (h *orphanHeal) inScope(task *models.Task) bool {
	if task.Worktree == nil || task.ReviewCommit == nil || task.BaseCommit == nil {
		return false
	}
	return task.Status == h.submitted || (h.partial != "" && task.Status == h.partial)
}

// checkPath proves the recorded worktree is the canonical one for the task
// before any Git write in it.
func (h *orphanHeal) checkPath(task *models.Task) error {
	h.wtPath = h.g.GetWorktreePath(task.ID)
	if filepath.Clean(filepath.Join(h.in.ProjectRoot, *task.Worktree)) != filepath.Clean(h.wtPath) {
		return &PreconditionError{Reason: fmt.Sprintf("task %s worktree %q is not its canonical path", task.ID, *task.Worktree)}
	}
	if _, err := os.Stat(h.wtPath); err != nil {
		return &lizaerrors.WorktreeContextError{Operation: healOrphanedReviewBaseOperation, TaskID: task.ID, Reason: "worktree directory is not accessible", Err: err}
	}
	return nil
}

func (h *orphanHeal) checkBranch() error {
	branch, err := h.g.GetWorktreeBranch(h.wtPath)
	if err != nil {
		return err
	}
	if branch != paths.TaskBranchPrefix+h.in.TaskID {
		return &PreconditionError{Reason: fmt.Sprintf("worktree for %s is on %q, want %s", h.in.TaskID, branch, paths.TaskBranchPrefix+h.in.TaskID)}
	}
	return nil
}

// ineligible names why an orphaned task must not be healed in place; "" means
// it may be. Every reason routes to integration recovery untouched.
func (h *orphanHeal) ineligible(task *models.Task) (string, error) {
	switch {
	case task.Status != h.submitted:
		return fmt.Sprintf("status %s holds approvals for the old commit", task.Status), nil
	case task.ReviewingBy != nil:
		return "a reviewer claim is recorded", nil
	case len(task.Approvals) > 0:
		return "approvals are recorded for the old commit", nil
	}
	if err := h.checkBranch(); err != nil {
		return "", err
	}
	operation, err := h.g.InterruptedOperation(h.wtPath)
	if err != nil {
		return "", err
	}
	if operation != "" {
		return "the worktree has an interrupted " + operation, nil
	}
	head, err := h.g.GetWorktreeHEAD(task.ID)
	if err != nil {
		return "", err
	}
	if head != *task.ReviewCommit {
		return fmt.Sprintf("worktree HEAD %s differs from review_commit %s", head, *task.ReviewCommit), nil
	}
	status, err := h.g.WorktreeStatusShort(h.wtPath)
	if err != nil {
		return "", err
	}
	if tracked := trackedStatusLines(status); len(tracked) > 0 {
		return "the worktree has tracked changes", nil
	}
	if _, merges, err := h.g.RangeCommits(*task.BaseCommit, head); err != nil {
		return "", err
	} else if merges {
		return "base_commit..review_commit contains a merge commit", nil
	}
	return "", nil
}

// fresh heals a task with no heal effect in its worktree. settled is the heal
// preparation that this invocation has just proven absent, retired in the same
// transaction that reserves the new attempt.
func (h *orphanHeal) fresh(task *models.Task, settled *models.LifecyclePreparation, target string) (OrphanedBaseHealOutcome, error) {
	reason, err := h.ineligible(task)
	if err != nil {
		return OrphanedBaseHealNotApplicable, err
	}
	if reason != "" {
		return h.routeIntegrationFailed(task, nil, settled, reason)
	}
	oldHead, base := *task.ReviewCommit, *task.BaseCommit
	request, err := h.prepare(task, settled, orphanHealPayload{OldReviewCommit: oldHead, OldBaseCommit: base, Target: target})
	if err != nil {
		return OrphanedBaseHealNotApplicable, err
	}
	rebaseErr := h.g.RebaseOntoUpstream(h.wtPath, target, base)
	if orphanHealAfterRebaseTestHook != nil && orphanHealAfterRebaseTestHook() {
		return OrphanedBaseHealNotApplicable, errOrphanHealAbandonedForTest
	}
	if rebaseErr != nil {
		var conflict *git.RebaseConflictError
		return h.fail(task, request, oldHead, stderrors.As(rebaseErr, &conflict), "rebase onto integration conflicts", rebaseErr)
	}
	newHead, err := h.g.GetWorktreeHEAD(task.ID)
	if err != nil {
		return h.fail(task, request, oldHead, false, "", err)
	}
	return h.finish(task, request, oldHead, target, newHead, "")
}

// settle resolves a heal preparation left by an ended invocation, from Git
// and the preparation's payload digest — never from the preparer's identity.
func (h *orphanHeal) settle(task *models.Task, prepared *models.LifecyclePreparation) (OrphanedBaseHealOutcome, error) {
	oldHead, base := *task.ReviewCommit, *task.BaseCommit
	fenced := func(reason string) (OrphanedBaseHealOutcome, error) {
		return OrphanedBaseHealNotApplicable, &OrphanedBaseHealFencedError{
			TaskID: task.ID, Reason: reason,
			RecoveryHint: fmt.Sprintf("inspect git -C %s status and log --oneline %s..HEAD, finish or abort any rebase in progress, then restore with git -C %s reset --hard %s or %s", optionalValue(task.Worktree), base, optionalValue(task.Worktree), oldHead, orphanedBaseRepairCommand(task, h.integration)),
		}
	}
	// An interrupted rebase is never aborted here. Raw Git takes no task lock,
	// and an operator may have resolved or edited some of its conflicts; Git
	// stages its own auto-merged paths too, so that work cannot be told apart
	// from the heal's untouched stop. The stop is fenced for inspection.
	if operation, err := h.g.InterruptedOperation(h.wtPath); err != nil || operation != "" {
		return fenced(fmt.Sprintf("the worktree has an interrupted %q (%v)", operation, err))
	}
	if err := h.checkBranch(); err != nil {
		return fenced(err.Error())
	}
	status, err := h.g.WorktreeStatusShort(h.wtPath)
	if err != nil {
		return OrphanedBaseHealNotApplicable, err
	}
	if tracked := trackedStatusLines(status); len(tracked) > 0 {
		return fenced("the worktree has tracked changes")
	}
	head, err := h.g.GetWorktreeHEAD(task.ID)
	if err != nil {
		return OrphanedBaseHealNotApplicable, err
	}
	integration, err := h.g.GetCommitSHA(h.integration)
	if err != nil {
		return OrphanedBaseHealNotApplicable, err
	}
	if head == oldHead {
		// Proven absent: run afresh, retiring the preparation in the same
		// transaction that reserves the new attempt.
		if !reviewBaseOrphaned(h.g, base, oldHead, integration) {
			return OrphanedBaseHealNotApplicable, h.retireSettled(prepared)
		}
		return h.fresh(task, prepared, integration)
	}

	// Proven: the found HEAD is the ended heal's result — same commit count,
	// a target the payload digest binds, the same per-commit patch identities.
	oldCommits, _, err := h.g.RangeCommits(base, oldHead)
	if err != nil {
		return OrphanedBaseHealNotApplicable, err
	}
	target, err := h.g.ResolveCommit(head + "~" + strconv.Itoa(len(oldCommits)))
	if err != nil || len(oldCommits) == 0 {
		return fenced(fmt.Sprintf("worktree HEAD %s is not a replay of the %d submitted commits", head, len(oldCommits)))
	}
	if (orphanHealPayload{OldReviewCommit: oldHead, OldBaseCommit: base, Target: target}).digest() != prepared.PayloadDigest {
		return fenced(fmt.Sprintf("worktree HEAD %s does not match the prepared heal", head))
	}
	if reason := h.contentChanged(base, oldHead, target, head); reason != "" {
		return fenced(reason)
	}
	onIntegration, err := h.g.IsAncestor(target, integration)
	if err != nil {
		return OrphanedBaseHealNotApplicable, err
	}
	request, err := h.prepare(task, prepared, orphanHealPayload{OldReviewCommit: oldHead, OldBaseCommit: base, Target: target})
	if err != nil {
		return OrphanedBaseHealNotApplicable, err
	}
	if !onIntegration {
		// Integration moved off the target since: the effect is ours, so undo
		// it; the next invocation heals onto the current integration.
		return h.fail(task, request, oldHead, false, "", fmt.Errorf("integration no longer contains heal target %s", target))
	}
	return h.finish(task, request, oldHead, target, head, prepared.Actor)
}

// prepare commits the durable reservation before any Git write or acceptance
// command. A settled earlier preparation is retired in the same transaction;
// its identity is compared exactly, so a replacement request is untouched.
func (h *orphanHeal) prepare(observed *models.Task, settled *models.LifecyclePreparation, payload orphanHealPayload) (LifecycleRequest, error) {
	var request LifecycleRequest
	err := modifyLifecycleState(h.bb, h.in.Authority, func(state *models.State) error {
		live := state.FindTask(h.in.TaskID)
		if err := h.admit(state, live); err != nil {
			return err
		}
		if err := sameOrphanHealBoundary(observed, live); err != nil {
			return err
		}
		if models.TaskTransitionID(live) != models.TaskTransitionID(observed) {
			return &PreconditionError{Reason: "task boundary changed before the heal was reserved"}
		}
		if settled != nil {
			current := pendingOrphanedBaseHeal(live)
			if current == nil || !sameLifecycleRequest(current.LifecycleIdentity, settled.LifecycleIdentity) || current.Boundary != settled.Boundary {
				return &PreconditionError{Reason: "the settled heal preparation changed"}
			}
			models.AdvanceLifecycle(live) // Inspected retirement; not completion or rollback.
		}
		var err error
		if request, err = NewLifecycleRequest(healOrphanedReviewBaseOperation, live, h.in.AgentID, h.in.Authority, LifecycleRequestOptions{}, payload); err != nil {
			return err
		}
		return PrepareLifecycleRequest(live, request, state.Agents)
	})
	return request, err
}

// finish proves content preservation, revalidates as submission would,
// executes fresh acceptance evidence and publishes the boundary.
func (h *orphanHeal) finish(observed *models.Task, request LifecycleRequest, oldHead, target, newHead, adoptedFrom string) (OrphanedBaseHealOutcome, error) {
	base := *observed.BaseCommit
	if reason := h.contentChanged(base, oldHead, target, newHead); reason != "" {
		return h.fail(observed, request, oldHead, true, reason, nil)
	}
	if err := h.revalidate(observed, target, newHead); err != nil {
		return h.fail(observed, request, oldHead, deterministicHealRefusal(err), "the replayed boundary fails submission validation", err)
	}
	state, err := h.bb.Read()
	if err != nil {
		return h.fail(observed, request, oldHead, false, "", err)
	}
	acceptance, err := loadAcceptanceInput(h.in.ProjectRoot, state, observed, target)
	if err != nil {
		return h.fail(observed, request, oldHead, deterministicHealRefusal(err), "acceptance input refused", err)
	}
	gate := runtimeInputGate{projectRoot: h.in.ProjectRoot, bb: h.bb, authority: h.in.Authority, actor: h.in.AgentID, operation: healOrphanedReviewBaseOperation}
	receipt, err := executeAcceptanceReceipt(context.Background(), h.in.ProjectRoot, observed, acceptance, newHead, gate, state.Config, nil, nil)
	if err != nil {
		return h.fail(observed, request, oldHead, deterministicHealRefusal(err), "fresh acceptance evidence refused", err)
	}
	publish := func() error { return h.publish(observed, request, target, newHead, acceptance, receipt, adoptedFrom) }
	if orphanHealPublishTestHook != nil {
		err = orphanHealPublishTestHook(h.bb, publish)
	} else {
		err = publish()
	}
	if err != nil {
		if h.published(target, newHead) {
			return OrphanedBaseHealed, nil // Lost acknowledgement: the write committed.
		}
		return h.fail(observed, request, oldHead, false, "", err)
	}
	return OrphanedBaseHealed, nil
}

// contentChanged compares base..oldHead with target..newHead commit by commit;
// "" means the reviewer's scope range carries the same changes.
func (h *orphanHeal) contentChanged(base, oldHead, target, newHead string) string {
	oldCommits, oldMerges, err := h.g.RangeCommits(base, oldHead)
	if err != nil {
		return err.Error()
	}
	newCommits, newMerges, err := h.g.RangeCommits(target, newHead)
	if err != nil {
		return err.Error()
	}
	if oldMerges || newMerges {
		return "a merge commit has no patch identity"
	}
	if len(oldCommits) != len(newCommits) {
		return fmt.Sprintf("replay changed the commit count from %d to %d", len(oldCommits), len(newCommits))
	}
	for i := range oldCommits {
		oldID, oldErr := h.g.PatchID(oldCommits[i])
		newID, newErr := h.g.PatchID(newCommits[i])
		if oldErr != nil || newErr != nil || oldID == "" || oldID != newID {
			return fmt.Sprintf("replayed commit %s does not carry the change of %s", shortSHA(newCommits[i]), shortSHA(oldCommits[i]))
		}
	}
	return ""
}

// revalidate runs the post-rebase checks submit-for-review runs.
func (h *orphanHeal) revalidate(task *models.Task, target, newHead string) error {
	if err := validatePlanningOutputAcceptance(h.in.ProjectRoot, task, newHead); err != nil {
		return err
	}
	if err := validateOutputRefFragments(h.in.ProjectRoot, task, newHead); err != nil {
		return err
	}
	if err := validateReviewCarriers(h.g, h.in.ProjectRoot, task.ID, target, newHead, target); err != nil {
		return err
	}
	boundary := *task
	boundary.BaseCommit = &target
	return validateReviewBoundaryCommit(h.in.ProjectRoot, &boundary, newHead, target)
}

func (h *orphanHeal) publish(observed *models.Task, request LifecycleRequest, target, newHead string, acceptance *acceptanceInput, receipt *models.AcceptanceReceipt, adoptedFrom string) error {
	now := time.Now().UTC()
	return modifyLifecycleState(h.bb, h.in.Authority, func(state *models.State) error {
		live := state.FindTask(h.in.TaskID)
		if live == nil {
			return &lizaerrors.NotFoundError{Entity: "task", ID: h.in.TaskID}
		}
		if err := ValidateLifecyclePreparation(live, request); err != nil {
			return err
		}
		if err := sameOrphanHealBoundary(observed, live); err != nil {
			return err
		}
		if err := recheckAcceptanceReceipt(h.in.ProjectRoot, state, live, acceptance, receipt); err != nil {
			return err
		}
		oldReview, oldBase := *live.ReviewCommit, *live.BaseCommit
		if receipt != nil {
			live.AcceptanceSource = &receipt.Source
		}
		live.AcceptanceReceipt = receipt
		live.ReviewCommit = &newHead
		live.BaseCommit = &target
		reason := fmt.Sprintf("orphaned base %s healed: task commits replayed onto integration %s", oldBase, target)
		extra := map[string]any{
			oldReviewCommitExtraKey:  oldReview,
			newReviewCommitExtraKey:  newHead,
			oldBaseCommitExtraKey:    oldBase,
			newBaseCommitExtraKey:    target,
			orphanedBaseHealExtraKey: true,
		}
		if adoptedFrom != "" && adoptedFrom != h.in.AgentID {
			extra[preparedByExtraKey] = adoptedFrom
		}
		agentID := h.in.AgentID
		live.History = append(live.History, models.TaskHistoryEntry{
			Time: now, Event: models.TaskEventReviewCommitUpdated, Agent: &agentID, Reason: &reason, Commit: &newHead, Extra: extra,
		})
		_, err := CompleteLifecycleRequest(live, request, models.LifecycleProjection{
			InputCommit: oldReview, ReviewCommit: newHead, BaseCommit: target, SourceStatus: live.Status,
		}, state.Agents)
		return err
	})
}

func (h *orphanHeal) published(target, newHead string) bool {
	state, err := h.bb.Read()
	if err != nil {
		return false
	}
	task := state.FindTask(h.in.TaskID)
	return task != nil && optionalValue(task.ReviewCommit) == newHead && optionalValue(task.BaseCommit) == target
}

// fail undoes this invocation's Git effect, then either routes a deterministic
// refusal to integration recovery under the same preparation or retires the
// preparation for a transient error. A failed restore keeps the preparation:
// the effect is uncertain.
func (h *orphanHeal) fail(observed *models.Task, request LifecycleRequest, oldHead string, deterministic bool, reason string, cause error) (OrphanedBaseHealOutcome, error) {
	if err := h.restore(oldHead); err != nil {
		return OrphanedBaseHealNotApplicable, err
	}
	if deterministic {
		if cause != nil {
			reason = fmt.Sprintf("%s: %s", reason, truncateForDiagnostics(cause.Error(), 500))
		}
		return h.routeIntegrationFailed(observed, &request, nil, reason)
	}
	preparation := models.LifecyclePreparation{LifecycleIdentity: request}
	if state, err := h.bb.Read(); err == nil {
		if live := pendingOrphanedBaseHeal(state.FindTask(h.in.TaskID)); live != nil && sameLifecycleRequest(live.LifecycleIdentity, request) {
			preparation = *live
		}
	}
	return OrphanedBaseHealNotApplicable, retireOrDeferFailedLifecyclePreparation(h.in.ProjectRoot, h.in.TaskID, h.in.Authority, &preparation, cause, LifecycleEffectsNone)
}

// restore returns the task branch to its submitted HEAD and proves it.
func (h *orphanHeal) restore(oldHead string) error {
	restoreErr := func(err error) error {
		return &OrphanedBaseRestoreError{TaskID: h.in.TaskID, Command: fmt.Sprintf("git -C %s reset --hard %s", h.wtPath, oldHead), Err: err}
	}
	// ineligible and settle leave no rebase in progress before this
	// invocation's own, and a worktree holds one rebase at a time.
	if rebase, err := h.g.GetInterruptedRebase(h.wtPath); err != nil {
		return restoreErr(err)
	} else if rebase != nil {
		if rebase.HeadName != "refs/heads/"+paths.TaskBranchPrefix+h.in.TaskID || rebase.OrigHead != oldHead {
			return restoreErr(fmt.Errorf("interrupted rebase rewrites %s from %s", rebase.HeadName, rebase.OrigHead))
		}
		if err := h.g.AbortRebase(h.wtPath); err != nil {
			return restoreErr(err)
		}
	}
	if err := h.checkBranch(); err != nil {
		return restoreErr(err)
	}
	if operation, err := h.g.InterruptedOperation(h.wtPath); err != nil || operation != "" {
		return restoreErr(fmt.Errorf("interrupted %q (%v)", operation, err))
	}
	if err := git.New(h.wtPath).ResetHard(oldHead); err != nil {
		return restoreErr(err)
	}
	head, err := h.g.GetWorktreeHEAD(h.in.TaskID)
	if err != nil || head != oldHead {
		return restoreErr(fmt.Errorf("HEAD after reset = %s (%v)", head, err))
	}
	status, err := h.g.WorktreeStatusShort(h.wtPath)
	if err != nil {
		return restoreErr(err)
	}
	if tracked := trackedStatusLines(status); len(tracked) > 0 {
		return restoreErr(fmt.Errorf("tracked changes after reset"))
	}
	return nil
}

// routeIntegrationFailed moves the task to INTEGRATION_FAILED with the manual
// repair in its diagnostic. With a request it finishes that prepared
// invocation; without one it is a new claim-triggered write and is admitted
// as such, retiring a settled preparation in the same transaction.
func (h *orphanHeal) routeIntegrationFailed(observed *models.Task, request *LifecycleRequest, settled *models.LifecyclePreparation, reason string) (OrphanedBaseHealOutcome, error) {
	cause := fmt.Errorf("base_commit %s is no longer on integration branch %s and the task was not healed (%s); repair: %s",
		optionalValue(observed.BaseCommit), h.integration, reason, orphanedBaseRepairCommand(observed, h.integration))
	err := modifyLifecycleState(h.bb, h.in.Authority, func(state *models.State) error {
		live := state.FindTask(h.in.TaskID)
		if request != nil {
			if err := ValidateLifecyclePreparation(live, *request); err != nil {
				return err
			}
		} else {
			if err := h.admit(state, live); err != nil {
				return err
			}
			if models.TaskTransitionID(live) != models.TaskTransitionID(observed) {
				return &PreconditionError{Reason: "task boundary changed before integration routing"}
			}
			if settled != nil {
				current := pendingOrphanedBaseHeal(live)
				if current == nil || !sameLifecycleRequest(current.LifecycleIdentity, settled.LifecycleIdentity) {
					return &PreconditionError{Reason: "the settled heal preparation changed"}
				}
				models.AdvanceLifecycle(live)
			}
		}
		if err := sameOrphanHealBoundary(observed, live); err != nil {
			return err
		}
		return markReviewBoundaryIntegrationFailed(state, live, h.in.AgentID, h.transitions, cause)
	})
	if err != nil {
		return OrphanedBaseHealNotApplicable, err
	}
	return OrphanedBaseHealIntegrationFailed, nil
}

// retireSettled retires a heal preparation proven absent whose task no longer
// needs healing.
func (h *orphanHeal) retireSettled(prepared *models.LifecyclePreparation) error {
	err := retireOrDeferFailedLifecyclePreparation(h.in.ProjectRoot, h.in.TaskID, h.in.Authority, prepared, errOrphanHealSettled, LifecycleEffectsNone)
	if retirementUncommitted(err) || !stderrors.Is(err, errOrphanHealSettled) {
		return err
	}
	return nil
}

var errOrphanHealSettled = stderrors.New("orphaned-base heal preparation settled")

// sameOrphanHealBoundary compares the fields this operation depends on that
// TaskTransitionID does not cover.
func sameOrphanHealBoundary(observed, live *models.Task) error {
	if live == nil {
		return &lizaerrors.NotFoundError{Entity: "task", ID: observed.ID}
	}
	if live.Status != observed.Status ||
		!reflect.DeepEqual(live.ReviewingBy, observed.ReviewingBy) ||
		!reflect.DeepEqual(live.ReviewCommit, observed.ReviewCommit) ||
		!reflect.DeepEqual(live.BaseCommit, observed.BaseCommit) ||
		!reflect.DeepEqual(live.Worktree, observed.Worktree) ||
		!reflect.DeepEqual(live.Approvals, observed.Approvals) ||
		!reflect.DeepEqual(live.Output, observed.Output) ||
		!reflect.DeepEqual(live.AcceptanceSource, observed.AcceptanceSource) ||
		!reflect.DeepEqual(live.AcceptanceReceipt, observed.AcceptanceReceipt) {
		return &PreconditionError{Reason: fmt.Sprintf("task %s review boundary changed during the orphaned-base heal", observed.ID)}
	}
	return nil
}

// deterministicHealRefusal separates content refusals, which route to
// integration recovery, from transient failures, which are retried. A full
// admission class is contention, so it is transient.
func deterministicHealRefusal(err error) bool {
	var evidence *AcceptanceEvidenceError
	var precondition *PreconditionError
	var repair *ReviewBoundaryRepairNeededError
	var worktree *lizaerrors.WorktreeContextError
	if stderrors.As(err, &evidence) && evidence.Class == AcceptanceFaultAdmissionSlot {
		return false
	}
	return stderrors.As(err, &evidence) || stderrors.As(err, &precondition) ||
		stderrors.As(err, &repair) || stderrors.As(err, &worktree)
}
