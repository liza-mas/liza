package ops

import (
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

type claimContext struct {
	authority           *models.AgentAuthority
	request             *LifecycleRequest
	taskID              string
	agentID             string
	taskStatus          models.TaskStatus
	targetStatus        models.TaskStatus
	worktreeDir         string
	worktreeRel         string
	integrationBranch   string
	previousAssignee    string
	baseCommit          string
	preservedBaseCommit string
	worktreeHead        string
	adoptedWIP          string // WIP commit adopting a preserved worktree's uncommitted work
	integrationCommit   string // integration commit captured before the worktree phase
	rebaseOldHead       string // rejected worktree HEAD before a claim-time rebase
	rebaseSkipped       string // why a rejected claim kept its old base
	continuation        bool   // the claim resumes the current iteration instead of starting one
	leaseExpires        time.Time
	pipelineTransitions map[models.TaskStatus][]models.TaskStatus
}

type claimStrategy interface {
	validate(*models.Task, *models.State, string, string, *claimContext) error
	enforceIterationLimit() bool
	requiresDependencyRecheck() bool
	handleWorktree(*db.Blackboard, *git.Git, *claimContext) (claimWorktreePhaseResult, error)
	shouldRunPostWorktreeCmd(claimWorktreePhaseResult) bool
	mutateTask(*models.Task, *claimContext)
	historyEntry(time.Time, *claimContext) models.TaskHistoryEntry
}

type freshClaimStrategy struct{}

func (freshClaimStrategy) validate(task *models.Task, state *models.State, runtimeRole, doerRole string, ctx *claimContext) error {
	if runtimeRole != doerRole {
		return fmt.Errorf("task %s is %s (not claimable by %s)", task.ID, task.Status, runtimeRole)
	}
	if unmet := unmetDependencies(task, state); len(unmet) > 0 {
		return fmt.Errorf("task has unmet dependencies: %s", formatDependencyResults(unmet))
	}
	return nil
}

func (freshClaimStrategy) enforceIterationLimit() bool {
	return false
}

func (freshClaimStrategy) requiresDependencyRecheck() bool {
	return true
}

func (freshClaimStrategy) handleWorktree(
	bb *db.Blackboard,
	gitWrapper *git.Git,
	ctx *claimContext,
) (claimWorktreePhaseResult, error) {
	result := claimWorktreePhaseResult{}
	cleanupAllowed, err := readyClaimHasStaleResources(gitWrapper, ctx.taskID, ctx.worktreeDir)
	if err != nil {
		return result, err
	}
	if err := handleReadyClaimWorktree(
		bb,
		gitWrapper,
		ctx.taskID,
		ctx.taskStatus,
		ctx.baseCommit,
		ctx.worktreeDir,
		ctx.worktreeRel,
		cleanupAllowed,
	); err != nil {
		return result, err
	}
	ctx.baseCommit, err = gitWrapper.GetWorktreeHEAD(ctx.taskID)
	if err != nil {
		return result, err
	}
	result.created = true
	return result, nil
}

func (freshClaimStrategy) shouldRunPostWorktreeCmd(phase claimWorktreePhaseResult) bool {
	return phase.created
}

func (freshClaimStrategy) mutateTask(task *models.Task, ctx *claimContext) {
	task.Worktree = &ctx.worktreeRel
	task.BaseCommit = &ctx.baseCommit
	if task.Attempt == 0 {
		task.Attempt = 1
	}
}

func (freshClaimStrategy) historyEntry(now time.Time, ctx *claimContext) models.TaskHistoryEntry {
	agentPtr := &ctx.agentID
	return models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventClaimed,
		Agent: agentPtr,
	}
}

type preservedInitialClaimStrategy struct{}

func (preservedInitialClaimStrategy) validate(task *models.Task, state *models.State, runtimeRole, doerRole string, ctx *claimContext) error {
	if runtimeRole != doerRole {
		return fmt.Errorf("task %s is %s (not claimable by %s)", task.ID, task.Status, runtimeRole)
	}
	if task.Worktree == nil || *task.Worktree == "" {
		return &PreconditionError{Reason: fmt.Sprintf("task %s preserved claim requires worktree metadata", task.ID)}
	}
	if *task.Worktree != ctx.worktreeRel {
		return &PreconditionError{Reason: fmt.Sprintf("task %s worktree = %q, want %q", task.ID, *task.Worktree, ctx.worktreeRel)}
	}
	if task.BaseCommit == nil || *task.BaseCommit == "" {
		return &PreconditionError{Reason: fmt.Sprintf("task %s preserved claim requires base_commit", task.ID)}
	}
	if unmet := unmetDependencies(task, state); len(unmet) > 0 {
		return fmt.Errorf("task has unmet dependencies: %s", formatDependencyResults(unmet))
	}
	return nil
}

func (preservedInitialClaimStrategy) enforceIterationLimit() bool {
	return false
}

func (preservedInitialClaimStrategy) requiresDependencyRecheck() bool {
	return true
}

func (preservedInitialClaimStrategy) handleWorktree(
	bb *db.Blackboard,
	gitWrapper *git.Git,
	ctx *claimContext,
) (claimWorktreePhaseResult, error) {
	result := claimWorktreePhaseResult{}
	if err := gitWrapper.ValidateWorktreeHealth(ctx.taskID); err != nil {
		return result, &PreconditionError{Reason: fmt.Sprintf("preserved worktree not healthy: %v", err)}
	}
	branch, err := gitWrapper.GetWorktreeBranch(ctx.worktreeDir)
	if err != nil {
		return result, err
	}
	expectedBranch := paths.TaskBranchPrefix + ctx.taskID
	if branch != expectedBranch {
		return result, &PreconditionError{Reason: fmt.Sprintf("preserved worktree branch = %q, want %q", branch, expectedBranch)}
	}
	head, err := gitWrapper.GetWorktreeHEAD(ctx.taskID)
	if err != nil {
		return result, err
	}
	if _, err := gitWrapper.GetCommitSHA(ctx.preservedBaseCommit + "^{commit}"); err != nil {
		return result, blockPreservedClaimBase(bb, ctx, fmt.Sprintf("preserved base_commit %s does not resolve: %v", ctx.preservedBaseCommit, err))
	}
	ancestor, err := gitWrapper.IsAncestor(ctx.preservedBaseCommit, head)
	if err != nil {
		return result, err
	}
	if !ancestor {
		return result, blockPreservedClaimBase(bb, ctx, fmt.Sprintf("preserved base_commit %s is not an ancestor of worktree HEAD %s", ctx.preservedBaseCommit, head))
	}

	status, err := gitWrapper.WorktreeStatusShort(ctx.worktreeDir)
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(status) != "" {
		if err := adoptPreservedWIP(bb, gitWrapper, ctx, status); err != nil {
			return result, err
		}
		if head, err = gitWrapper.GetWorktreeHEAD(ctx.taskID); err != nil {
			return result, err
		}
	}

	baseOnIntegration, err := gitWrapper.IsAncestor(ctx.preservedBaseCommit, ctx.baseCommit)
	if err != nil {
		return result, err
	}
	if !baseOnIntegration {
		if head != ctx.preservedBaseCommit {
			return result, blockPreservedClaimBase(bb, ctx, fmt.Sprintf("preserved base_commit %s no longer belongs to captured integration %s; task commits require manual repair", ctx.preservedBaseCommit, ctx.baseCommit))
		}
		operation, err := gitWrapper.InterruptedOperation(ctx.worktreeDir)
		if err != nil {
			return result, err
		}
		if operation != "" {
			return result, blockPreservedClaimBase(bb, ctx, fmt.Sprintf("preserved worktree has an interrupted %s; cannot replace its removed integration base", operation))
		}
		// HEAD == recorded base and the clean worktree has no task commits.
		// Reset only this task branch; rebasing would replay removed integration.
		if err := git.New(ctx.worktreeDir).ResetHard(ctx.baseCommit); err != nil {
			return result, blockPreservedClaimBase(bb, ctx, fmt.Sprintf("could not move zero-work preserved branch to captured integration %s: %v", ctx.baseCommit, err))
		}
		head = ctx.baseCommit
	}

	targetAncestor, err := gitWrapper.IsAncestor(ctx.baseCommit, head)
	if err != nil {
		return result, err
	}
	if !targetAncestor {
		if err := gitWrapper.RebaseOnto(ctx.worktreeDir, ctx.baseCommit); err != nil {
			kind := "retry_preserved_claim_rebase"
			reason := fmt.Sprintf("preserved worktree rebase failed onto %s: %v", ctx.baseCommit, err)
			var conflict *git.RebaseConflictError
			if stderrors.As(err, &conflict) {
				kind = "resolve_preserved_claim_rebase_conflict"
				reason = fmt.Sprintf("preserved worktree rebase conflict onto %s: %v", ctx.baseCommit, err)
			}
			if abortErr := gitWrapper.AbortRebase(ctx.worktreeDir); abortErr != nil {
				reason = fmt.Sprintf("%s; abort failed: %v", reason, abortErr)
			}
			if markErr := markPreservedInitialClaimRecovery(bb, ctx, kind, reason, err.Error()); markErr != nil {
				return result, fmt.Errorf("%s; failed to record recovery state: %w", reason, markErr)
			}
			return result, &PreconditionError{Reason: reason}
		}
		head, err = gitWrapper.GetWorktreeHEAD(ctx.taskID)
		if err != nil {
			return result, err
		}
	}

	ancestor, err = gitWrapper.IsAncestor(ctx.baseCommit, head)
	if err != nil {
		return result, err
	}
	if !ancestor {
		return result, &PreconditionError{Reason: fmt.Sprintf("preserved worktree HEAD %s does not descend from captured integration commit %s", head, ctx.baseCommit)}
	}
	if ctx.adoptedWIP != "" {
		ctx.adoptedWIP = head // The WIP commit is the tip, including after a rebase.
	}
	ctx.worktreeHead = head
	return result, nil
}

// Invalid preserved ancestry needs an operator decision; repeatedly claiming
// the same boundary cannot repair it and must not discard task commits.
func blockPreservedClaimBase(bb *db.Blackboard, ctx *claimContext, reason string) error {
	if err := markPreservedInitialClaimRecovery(bb, ctx, "repair_preserved_claim_base", reason, reason); err != nil {
		return fmt.Errorf("%s; failed to record recovery state: %w", reason, err)
	}
	return &PreconditionError{Reason: reason}
}

// adoptPreservedWIP commits the uncommitted work a previous owner left in the
// preserved worktree, so the claim continues it instead of blocking on it. A
// Git operation in progress is refused: committing would record unresolved
// state as resolved. Every refusal blocks the task with the clean-worktree
// repair and deletes nothing. Before staging the worktree is untouched; a
// failed commit may leave the work staged; residue after the commit keeps the
// WIP commit on the task branch.
func adoptPreservedWIP(bb *db.Blackboard, gitWrapper *git.Git, ctx *claimContext, status string) error {
	block := func(reason string) error {
		if markErr := markPreservedInitialClaimRecovery(bb, ctx, "clean_preserved_claim_worktree", reason, reason); markErr != nil {
			return fmt.Errorf("%s; failed to record recovery state: %w", reason, markErr)
		}
		return &PreconditionError{Reason: reason}
	}
	dirt := strings.Join(strings.Fields(status), " ")
	operation, err := gitWrapper.InterruptedOperation(ctx.worktreeDir)
	if err != nil {
		return block(fmt.Sprintf("preserved worktree is dirty and its Git state cannot be inspected: %s: %v", dirt, err))
	}
	if operation != "" {
		return block(fmt.Sprintf("preserved worktree is dirty with an interrupted %s: %s", operation, dirt))
	}
	message := fmt.Sprintf("WIP: adopt uncommitted work preserved in task worktree — auto-committed at claim, hooks skipped\n\nTask: %s\nClaimed by: %s\n", ctx.taskID, ctx.agentID)
	sha, _, err := gitWrapper.CommitAllWIP(ctx.worktreeDir, message)
	if err != nil {
		return block(fmt.Sprintf("preserved worktree is dirty and adopting it failed: %s: %v", dirt, err))
	}
	residual, err := gitWrapper.WorktreeStatusShort(ctx.worktreeDir)
	if err != nil {
		return err
	}
	if strings.TrimSpace(residual) != "" {
		return block(fmt.Sprintf("preserved worktree is still dirty after adopting %s: %s", shortSHA(sha), strings.Join(strings.Fields(residual), " ")))
	}
	ctx.adoptedWIP = sha
	return nil
}

func markPreservedInitialClaimRecovery(
	bb *db.Blackboard,
	ctx *claimContext,
	operation,
	reason,
	evidence string,
) error {
	now := time.Now().UTC()
	question := "Repair the preserved worktree, then use unblock-task to restore it for a new claim."
	statusCommand := fmt.Sprintf("git -C %s status --short", ctx.worktreeRel)
	repairCommand := statusCommand
	validation := []string{statusCommand}
	if operation == "repair_preserved_claim_base" {
		repairCommand = brand.Command("recover-task", ctx.taskID) + " --fresh"
		question = "Inspect the preserved task branch and repair its base metadata, or deliberately discard the preserved work with " + repairCommand + "."
	} else if operation != "clean_preserved_claim_worktree" {
		repairCommand = fmt.Sprintf("git -C %s rebase %s", ctx.worktreeRel, ctx.baseCommit)
		validation = append(validation, repairCommand)
	}
	return lifecycleMutation(bb, ctx.authority)(func(state *models.State) error {
		task := state.FindTask(ctx.taskID)
		if task == nil {
			return fmt.Errorf("task %s not found while recording preserved claim recovery", ctx.taskID)
		}
		if task.Status != ctx.taskStatus {
			return fmt.Errorf("race condition: task status changed from %s to %s", ctx.taskStatus, task.Status)
		}
		if ctx.request != nil {
			if err := ValidateLifecyclePreparation(task, *ctx.request); err != nil {
				return err
			}
		}
		if err := task.TransitionWith(models.TaskStatusBlocked, ctx.pipelineTransitions); err != nil {
			return err
		}
		task.AssignedTo = nil
		task.LeaseExpires = nil
		models.AdvanceLifecycle(task)
		task.BlockedReason = &reason
		task.BlockedQuestions = []string{question}
		task.RepairRequest = &models.RepairRequest{
			Operation:  operation,
			Target:     ctx.taskID,
			Command:    repairCommand,
			Evidence:   []string{truncateForDiagnostics(evidence, 2000)},
			Validation: validation,
		}
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventBlocked,
			Agent:  &ctx.agentID,
			Reason: &reason,
			Extra: map[string]any{
				"preserved_worktree": true,
				"rebase_target_sha":  ctx.baseCommit,
			},
		})
		return nil
	})
}

func (preservedInitialClaimStrategy) shouldRunPostWorktreeCmd(claimWorktreePhaseResult) bool {
	return true
}

func (preservedInitialClaimStrategy) mutateTask(task *models.Task, ctx *claimContext) {
	task.Worktree = &ctx.worktreeRel
	task.BaseCommit = &ctx.baseCommit
	if task.Attempt == 0 {
		task.Attempt = 1
	}
}

func (preservedInitialClaimStrategy) historyEntry(now time.Time, ctx *claimContext) models.TaskHistoryEntry {
	agentPtr := &ctx.agentID
	extra := map[string]any{
		"preserved_worktree": true,
	}
	if ctx.adoptedWIP != "" {
		extra["adopted_wip_commit"] = ctx.adoptedWIP
	}
	if ctx.continuation {
		extra["continuation"] = true
	}
	return models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventClaimed,
		Agent: agentPtr,
		Extra: extra,
	}
}

type rejectedClaimStrategy struct{}

func (rejectedClaimStrategy) validate(task *models.Task, _ *models.State, runtimeRole, doerRole string, ctx *claimContext) error {
	if runtimeRole != doerRole {
		return fmt.Errorf("task %s is %s (not claimable by %s)", task.ID, task.Status, runtimeRole)
	}
	if task.Worktree != nil && *task.Worktree != ctx.worktreeRel {
		return &PreconditionError{Reason: fmt.Sprintf("task %s worktree = %q, want %q", task.ID, *task.Worktree, ctx.worktreeRel)}
	}
	if task.AssignedTo != nil {
		ctx.previousAssignee = *task.AssignedTo
	}
	return nil
}

func (rejectedClaimStrategy) enforceIterationLimit() bool {
	return true
}

func (rejectedClaimStrategy) requiresDependencyRecheck() bool {
	return false
}

func (rejectedClaimStrategy) handleWorktree(
	_ *db.Blackboard,
	gitWrapper *git.Git,
	ctx *claimContext,
) (claimWorktreePhaseResult, error) {
	return ensureRejectedWorktreeExists(gitWrapper, ctx)
}

func (rejectedClaimStrategy) shouldRunPostWorktreeCmd(claimWorktreePhaseResult) bool {
	return true
}

func (rejectedClaimStrategy) mutateTask(task *models.Task, ctx *claimContext) {
	task.Worktree = &ctx.worktreeRel
	task.BaseCommit = &ctx.baseCommit
}

func (rejectedClaimStrategy) historyEntry(now time.Time, ctx *claimContext) models.TaskHistoryEntry {
	agentPtr := &ctx.agentID
	entry := models.TaskHistoryEntry{
		Time:  now,
		Agent: agentPtr,
	}
	if ctx.rebaseOldHead != "" {
		entry.Extra = map[string]any{"rebase_old_head": ctx.rebaseOldHead, "rebase_target_sha": ctx.baseCommit}
	} else if ctx.rebaseSkipped != "" {
		entry.Extra = map[string]any{"rebase_skipped": ctx.rebaseSkipped}
	}
	if ctx.previousAssignee == ctx.agentID {
		entry.Event = models.TaskEventReclaimedAfterRejection
		return entry
	}

	entry.Event = models.TaskEventReassignedAfterRejection
	if ctx.previousAssignee != "" {
		entry.PreviousAssignee = &ctx.previousAssignee
	}
	return entry
}

// rebaseRejectedWorktree moves reused rejected work onto the integration commit
// captured for this claim, so rework starts from current integration instead of
// meeting integration's movement only at submission. It is best effort: a
// refused or conflicting rebase keeps the branch and base_commit, records why,
// and leaves the conflict to the doer, who meets it again at submission. The
// claim fails closed only when it cannot prove the worktree is as it found it.
func rebaseRejectedWorktree(gitWrapper *git.Git, ctx *claimContext) error {
	target := ctx.integrationCommit
	if target == "" || target == ctx.baseCommit {
		return nil
	}
	// Rebasing only onto a descendant of the old base keeps that base an
	// ancestor of HEAD, which the next claim validates if this one fails later.
	advanced, err := gitWrapper.IsAncestor(ctx.baseCommit, target)
	if err != nil {
		return err
	}
	if !advanced {
		ctx.rebaseSkipped = fmt.Sprintf("integration %s does not descend from base_commit %s", shortSHA(target), shortSHA(ctx.baseCommit))
		return nil
	}
	head, err := gitWrapper.GetWorktreeHEAD(ctx.taskID)
	if err != nil {
		return err
	}
	current, err := gitWrapper.IsAncestor(target, head)
	if err != nil {
		return err
	}
	if current {
		ctx.baseCommit = target
		return nil
	}
	operation, err := gitWrapper.InterruptedOperation(ctx.worktreeDir)
	if err != nil {
		return failClosedRejectedRebase(ctx, fmt.Sprintf("cannot inspect Git state: %v", err))
	}
	if operation != "" {
		return failClosedRejectedRebase(ctx, "interrupted "+operation)
	}
	status, err := gitWrapper.WorktreeStatusShort(ctx.worktreeDir)
	if err != nil {
		return err
	}
	if tracked := trackedStatusLines(status); len(tracked) > 0 {
		ctx.rebaseSkipped = "worktree has tracked changes: " + strings.Join(tracked, "; ")
		return nil
	}

	rebaseErr := gitWrapper.RebaseOnto(ctx.worktreeDir, target)
	if rebaseErr == nil {
		ctx.rebaseOldHead = head
		ctx.baseCommit = target
		return nil
	}
	if err := restoreRejectedWorktreeAfterRebase(gitWrapper, ctx, head); err != nil {
		return err
	}
	var conflict *git.RebaseConflictError
	if stderrors.As(rebaseErr, &conflict) {
		ctx.rebaseSkipped = fmt.Sprintf("rebase conflict onto %s", shortSHA(target))
	} else {
		ctx.rebaseSkipped = fmt.Sprintf("rebase onto %s failed: %s", shortSHA(target), truncateForDiagnostics(rebaseErr.Error(), 500))
	}
	return nil
}

// restoreRejectedWorktreeAfterRebase aborts only a rebase this claim started
// (a refusal before start leaves none) and proves the pre-rebase boundary.
func restoreRejectedWorktreeAfterRebase(gitWrapper *git.Git, ctx *claimContext, preHead string) error {
	operation, err := gitWrapper.InterruptedOperation(ctx.worktreeDir)
	if err != nil {
		return failClosedRejectedRebase(ctx, fmt.Sprintf("cannot inspect Git state: %v", err))
	}
	switch operation {
	case "":
	case "rebase":
		if err := gitWrapper.AbortRebase(ctx.worktreeDir); err != nil {
			return failClosedRejectedRebase(ctx, fmt.Sprintf("rebase abort failed: %v", err))
		}
		if operation, err = gitWrapper.InterruptedOperation(ctx.worktreeDir); err != nil || operation != "" {
			return failClosedRejectedRebase(ctx, fmt.Sprintf("Git state after rebase abort: %q, %v", operation, err))
		}
	default:
		return failClosedRejectedRebase(ctx, "interrupted "+operation)
	}
	branch, err := gitWrapper.GetWorktreeBranch(ctx.worktreeDir)
	if err != nil || branch != paths.TaskBranchPrefix+ctx.taskID {
		return failClosedRejectedRebase(ctx, fmt.Sprintf("branch after rebase = %q, %v", branch, err))
	}
	head, err := gitWrapper.GetWorktreeHEAD(ctx.taskID)
	if err != nil || head != preHead {
		return failClosedRejectedRebase(ctx, fmt.Sprintf("HEAD after rebase = %s, %v; want %s", head, err, preHead))
	}
	status, err := gitWrapper.WorktreeStatusShort(ctx.worktreeDir)
	if err != nil {
		return failClosedRejectedRebase(ctx, fmt.Sprintf("cannot read status: %v", err))
	}
	if tracked := trackedStatusLines(status); len(tracked) > 0 {
		return failClosedRejectedRebase(ctx, "tracked changes after rebase: "+strings.Join(tracked, "; "))
	}
	return nil
}

func failClosedRejectedRebase(ctx *claimContext, detail string) error {
	return &PreconditionError{Reason: fmt.Sprintf(
		"rejected worktree %s is not in a known state for claim-time rebase onto %s: %s; inspect with git -C %s status",
		ctx.worktreeRel, shortSHA(ctx.integrationCommit), detail, ctx.worktreeRel,
	)}
}

type integrationFixClaimStrategy struct{}

func (integrationFixClaimStrategy) validate(task *models.Task, _ *models.State, runtimeRole, doerRole string, ctx *claimContext) error {
	if runtimeRole != doerRole {
		return fmt.Errorf("task %s is %s (not claimable by %s)", task.ID, task.Status, runtimeRole)
	}
	if task.AssignedTo != nil {
		ctx.previousAssignee = *task.AssignedTo
	}
	return nil
}

func (integrationFixClaimStrategy) enforceIterationLimit() bool {
	return false
}

func (integrationFixClaimStrategy) requiresDependencyRecheck() bool {
	return false
}

func (integrationFixClaimStrategy) handleWorktree(
	_ *db.Blackboard,
	_ *git.Git,
	ctx *claimContext,
) (claimWorktreePhaseResult, error) {
	result := claimWorktreePhaseResult{}
	if err := ensureIntegrationFailedWorktreeExists(ctx.worktreeDir, ctx.worktreeRel); err != nil {
		return result, err
	}
	return result, nil
}

func (integrationFixClaimStrategy) shouldRunPostWorktreeCmd(claimWorktreePhaseResult) bool {
	return true
}

func (integrationFixClaimStrategy) mutateTask(task *models.Task, _ *claimContext) {
	clearAttemptState(task, attemptStateIntegrationFixClaim)
	// FailedBy is audit/escalation history, not stale attempt state.
	task.IntegrationFix = true
}

func (integrationFixClaimStrategy) historyEntry(now time.Time, ctx *claimContext) models.TaskHistoryEntry {
	agentPtr := &ctx.agentID
	return models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventClaimedForIntegrationFix,
		Agent: agentPtr,
	}
}
