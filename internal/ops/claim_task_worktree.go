package ops

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

type claimWorktreePhaseResult struct {
	created bool
	deleted bool
}

// claimSetupWarnings accumulate in the order the setup steps run; all() keeps
// that order for the claim result.
type claimSetupWarnings struct {
	env, semble, scip, stacklit, functionalClusters []string
}

func (w claimSetupWarnings) all() []string {
	warnings := append([]string{}, w.env...)
	warnings = append(warnings, w.semble...)
	warnings = append(warnings, w.scip...)
	warnings = append(warnings, w.stacklit...)
	return append(warnings, w.functionalClusters...)
}

// claimPreparedWorktree is what phase two produced outside the state lock.
type claimPreparedWorktree struct {
	phase     claimWorktreePhaseResult
	warnings  claimSetupWarnings
	preflight *ValidationPreflight
}

// prepareWorktree runs phase two: the strategy's worktree handling, setup and
// index refreshes, and the validation preflight. It holds no state lock and
// writes no task or agent state.
func (op *claimOperation) prepareWorktree() (claimPreparedWorktree, error) {
	var prepared claimPreparedWorktree
	phase, err := op.strategy.handleWorktree(op.bb, op.git, &op.ctx)
	if err != nil {
		return prepared, err
	}
	prepared.phase = phase
	if op.config.copyWorktreeEnvFiles {
		prepared.warnings.env = ProvisionWorktreeEnvFiles(op.projectRoot, op.ctx.worktreeDir)
		op.logWarnings(prepared.warnings.env)
	}
	if err := op.runPostWorktreeCmd(phase); err != nil {
		return prepared, err
	}
	op.refreshIndexes(&prepared.warnings)
	prepared.preflight, err = PrepareValidationPreflight(op.projectRoot, op.taskID, op.agentID, op.ctx.worktreeDir, op.session)
	return prepared, err
}

// runPostWorktreeCmd runs after worktree provisioning on fresh claims, rejection
// reclaims (including same-coder) and integration-fix. The command is
// idempotent — safe on existing worktrees, catches prior failures.
//
// Fail closed: abort here, before assignment commits any task or agent state, so
// no claim exists for a worktree that is not build-ready. The worktree is left
// on disk so the failure can be reproduced against it; a later fresh claim
// treats it as a stale resource. The caller classifies this error as an
// infrastructure failure and degrades the agent, which bounds retries.
func (op *claimOperation) runPostWorktreeCmd(phase claimWorktreePhaseResult) error {
	if op.config.postWorktreeCmd == nil || !op.strategy.shouldRunPostWorktreeCmd(phase) {
		return nil
	}
	postErr := RunPostWorktreeCmd(op.projectRoot, *op.config.postWorktreeCmd, op.ctx.worktreeDir, op.config.postWorktreeCmdTimeout, op.authority)
	if postErr != nil {
		log.Printf("ERROR: claim-task %s: %v", op.taskID, postErr)
	}
	return postErr
}

func (op *claimOperation) refreshIndexes(warnings *claimSetupWarnings) {
	dir, languages := op.ctx.worktreeDir, op.config.scipSearchLanguages
	warnings.semble = PrepareSembleWorktreeIgnore(dir)
	op.logWarnings(warnings.semble)
	warnings.scip = refreshTaskWorktreeScipIndexes(op.projectRoot, dir, languages)
	op.logWarnings(warnings.scip)
	warnings.stacklit = refreshTaskWorktreeStacklitIndex(op.projectRoot, dir)
	op.logWarnings(warnings.stacklit)
	warnings.functionalClusters = refreshTaskWorktreeFunctionalClustersIndex(op.projectRoot, dir, languages)
	op.logWarnings(warnings.functionalClusters)
}

func (op *claimOperation) logWarnings(warnings []string) {
	for _, warning := range warnings {
		log.Printf("WARNING: claim-task %s: %s", op.taskID, warning)
	}
}

func handleReadyClaimWorktree(
	bb *db.Blackboard,
	gitWrapper *git.Git,
	taskID string, initialStatus models.TaskStatus,
	capturedBaseCommit, worktreeDir, worktreeRel string,
	cleanupAllowed bool,
) error {
	branchName := paths.TaskBranchPrefix + taskID
	if _, err := gitWrapper.CreateWorktree(taskID, capturedBaseCommit); err == nil {
		return nil
	} else if !isCreateWorktreeConflict(err) {
		return fmt.Errorf("failed to create worktree: %w", err)
	}
	if !cleanupAllowed {
		return fmt.Errorf("race condition: concurrent claim already provisioned worktree for READY task")
	}

	// Re-read task state before cleanup: if a concurrent claimer already won
	// (task no longer in initial status), the worktree belongs to them.
	_, task, readErr := readTaskState(bb, taskID)
	if readErr != nil && !errors.IsNotFound(readErr) {
		// I/O error — can't verify whether another agent claimed the task.
		// Aborting cleanup is safer than deleting a potentially active worktree.
		return fmt.Errorf("cannot verify task state before cleanup: %w", readErr)
	}
	if readErr == nil && task.Status != initialStatus {
		return fmt.Errorf("race condition: task %s claimed concurrently (status: %s)", taskID, task.Status)
	}

	branchExists, err := gitWrapper.BranchExists(branchName)
	if err != nil {
		return fmt.Errorf("failed to check branch existence: %w", err)
	}
	if _, statErr := os.Stat(worktreeDir); statErr == nil {
		log.Printf("WARNING: claim-task %s: removing stale worktree %s for READY task", taskID, worktreeRel)
		if cleanupErr := gitWrapper.RemoveWorktree(taskID); cleanupErr != nil {
			return fmt.Errorf("failed to remove stale worktree %s: %w", worktreeRel, cleanupErr)
		}
		_ = gitWrapper.DeleteBranch(branchName)
	} else if branchExists {
		log.Printf("WARNING: claim-task %s: removing stale branch %s for READY task", taskID, branchName)
		if cleanupErr := gitWrapper.RemoveWorktree(taskID); cleanupErr != nil {
			return fmt.Errorf("failed to clean up stale worktree/branch for %s: %w", branchName, cleanupErr)
		}
	}

	if _, err := gitWrapper.CreateWorktree(taskID, capturedBaseCommit); err != nil {
		if isCreateWorktreeConflict(err) {
			return fmt.Errorf("race condition: concurrent claim won after stale cleanup")
		}
		return fmt.Errorf("failed to create worktree after stale cleanup: %w", err)
	}

	return nil
}

func isCreateWorktreeConflict(err error) bool {
	errMsg := err.Error()
	return strings.Contains(errMsg, "worktree already exists") ||
		strings.Contains(errMsg, "already exists") ||
		strings.Contains(errMsg, "already checked out")
}

func readyClaimHasStaleResources(gitWrapper *git.Git, taskID, worktreeDir string) (bool, error) {
	branchExists, err := gitWrapper.BranchExists(paths.TaskBranchPrefix + taskID)
	if err != nil {
		return false, fmt.Errorf("failed to check branch existence: %w", err)
	}
	if _, err := os.Stat(worktreeDir); err == nil {
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("failed to stat worktree %s: %w", worktreeDir, err)
	}
	return branchExists, nil
}

func ensureRejectedWorktreeExists(
	gitWrapper *git.Git,
	ctx *claimContext,
) (claimWorktreePhaseResult, error) {
	result := claimWorktreePhaseResult{}
	branchName := paths.TaskBranchPrefix + ctx.taskID

	worktreeDirExists := false
	if _, err := os.Stat(ctx.worktreeDir); err == nil {
		worktreeDirExists = true
	} else if !os.IsNotExist(err) {
		return result, fmt.Errorf("failed to stat worktree %s: %w", ctx.worktreeDir, err)
	}

	branchExists, err := gitWrapper.BranchExists(branchName)
	if err != nil {
		return result, fmt.Errorf("failed to check branch %s: %w", branchName, err)
	}

	if worktreeDirExists && branchExists {
		if err := validateRejectedWorktree(gitWrapper, ctx); err != nil {
			return result, err
		}
		return result, rebaseRejectedWorktree(gitWrapper, ctx)
	}

	if worktreeDirExists {
		return result, &PreconditionError{Reason: fmt.Sprintf(
			"rejected task artifacts are unclassifiable: worktree %s exists but branch %s is missing; preserved without cleanup",
			ctx.worktreeRel,
			branchName,
		)}
	}

	if branchExists {
		if err := gitWrapper.RemoveWorktreeDir(ctx.taskID); err != nil {
			return result, fmt.Errorf("failed to clear stale worktree registration for %s: %w", ctx.worktreeRel, err)
		}
		if err := gitWrapper.AttachWorktree(ctx.taskID, branchName); err != nil {
			return result, fmt.Errorf("failed to reattach rejected task branch %s: %w", branchName, err)
		}
		if err := validateRejectedWorktree(gitWrapper, ctx); err != nil {
			return result, err
		}
		return result, rebaseRejectedWorktree(gitWrapper, ctx)
	}

	createdBase, err := gitWrapper.CreateWorktree(ctx.taskID, ctx.integrationBranch)
	if err != nil {
		return result, fmt.Errorf("failed to recreate worktree for REJECTED task from integration branch: %w", err)
	}
	ctx.baseCommit = createdBase
	result.created = true
	return result, nil
}

func ensureIntegrationFailedWorktreeExists(worktreeDir, worktreeRel string) error {
	if _, err := os.Stat(worktreeDir); os.IsNotExist(err) {
		return fmt.Errorf("worktree %s missing for INTEGRATION_FAILED task", worktreeRel)
	}
	return nil
}

func validateRejectedWorktree(
	gitWrapper *git.Git,
	ctx *claimContext,
) error {
	if _, err := os.Stat(ctx.worktreeDir); os.IsNotExist(err) {
		return fmt.Errorf("worktree %s missing for REJECTED task", ctx.worktreeRel)
	}

	branchName := paths.TaskBranchPrefix + ctx.taskID
	branchExists, err := gitWrapper.BranchExists(branchName)
	if err != nil {
		return fmt.Errorf("failed to check branch %s for REJECTED task: %w", branchName, err)
	}
	if !branchExists {
		return fmt.Errorf("branch %s missing for REJECTED task", branchName)
	}

	branch, err := gitWrapper.GetWorktreeBranch(ctx.worktreeDir)
	if err != nil {
		return fmt.Errorf("worktree %s invalid for REJECTED task: %w", ctx.worktreeRel, err)
	}
	if branch != branchName {
		return &PreconditionError{Reason: fmt.Sprintf("rejected worktree branch = %q, want %q", branch, branchName)}
	}

	head, err := gitWrapper.GetWorktreeHEAD(ctx.taskID)
	if err != nil {
		return fmt.Errorf("worktree %s invalid for REJECTED task: %w", ctx.worktreeRel, err)
	}
	baseCommit := ctx.preservedBaseCommit
	if baseCommit == "" {
		baseCommit, err = gitWrapper.GetMergeBase(head, ctx.integrationBranch)
		if err != nil {
			return fmt.Errorf("failed to derive rejected worktree base commit: %w", err)
		}
	} else {
		resolvedBase, resolveErr := gitWrapper.GetCommitSHA(baseCommit)
		if resolveErr != nil {
			return &PreconditionError{Reason: fmt.Sprintf("rejected base_commit %s does not resolve: %v", baseCommit, resolveErr)}
		}
		baseCommit = resolvedBase
	}
	ancestor, err := gitWrapper.IsAncestor(baseCommit, head)
	if err != nil {
		return fmt.Errorf("failed to validate rejected worktree lineage: %w", err)
	}
	if !ancestor {
		return &PreconditionError{Reason: fmt.Sprintf("rejected base_commit %s is not an ancestor of worktree HEAD %s", baseCommit, head)}
	}
	ctx.baseCommit = baseCommit

	return nil
}
