package ops

import (
	stderrors "errors"
	"fmt"
	"strings"
	"time"

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
		ctx.integrationBranch,
		ctx.worktreeDir,
		ctx.worktreeRel,
		cleanupAllowed,
	); err != nil {
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
	if _, err := gitWrapper.GetCommitSHA(ctx.preservedBaseCommit); err != nil {
		return result, &PreconditionError{Reason: fmt.Sprintf("preserved base_commit %s does not resolve: %v", ctx.preservedBaseCommit, err)}
	}
	ancestor, err := gitWrapper.IsAncestor(ctx.preservedBaseCommit, head)
	if err != nil {
		return result, err
	}
	if !ancestor {
		return result, &PreconditionError{Reason: fmt.Sprintf("preserved base_commit %s is not an ancestor of worktree HEAD %s", ctx.preservedBaseCommit, head)}
	}

	status, err := gitWrapper.WorktreeStatusShort(ctx.worktreeDir)
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(status) != "" {
		reason := fmt.Sprintf("preserved worktree is dirty: %s", strings.Join(strings.Fields(status), " "))
		if markErr := markPreservedInitialClaimRecovery(bb, ctx, "clean_preserved_claim_worktree", reason, reason); markErr != nil {
			return result, fmt.Errorf("%s; failed to record recovery state: %w", reason, markErr)
		}
		return result, &PreconditionError{Reason: reason}
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
	ctx.worktreeHead = head
	return result, nil
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
	if operation != "clean_preserved_claim_worktree" {
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
	return models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventClaimed,
		Agent: agentPtr,
		Extra: map[string]any{
			"preserved_worktree": true,
		},
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
