package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/functionalclusters"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/stacklit"
)

// errTaskBlocked is a sentinel indicating that blockReviewerTask already
// handled claim release and agent state cleanup. The supervisor must NOT
// call releaseReviewerClaimQuietly when this error is returned.
var errTaskBlocked = errors.New("task blocked")

var (
	reviewerWorktreeRefreshIndexes                 = scipsearch.RefreshIndexes
	reviewerWorktreeRefreshStacklitIndex           = stacklit.RefreshIndex
	reviewerWorktreeRefreshFunctionalClustersIndex = functionalclusters.RefreshIndex
	reviewerWorktreePrepareSembleIgnore            = ops.PrepareSembleWorktreeIgnore
)

// ensureReviewerWorktree verifies the worktree exists for a reviewer task and
// that its configured setup command succeeds, so the reviewer session starts
// against a build-ready checkout.
// Returns (true, nil) if the worktree was recovered from an existing branch.
// Returns (false, nil) if the worktree already exists and setup succeeded.
// Returns (false, error) if the task was blocked, recovery failed, or setup failed.
func ensureReviewerWorktree(projectRoot string, bb *db.Blackboard, taskID string, authority models.AgentAuthority) (recovered bool, err error) {
	// Intact-worktree fast path, deliberately outside the lifecycle lock: it
	// creates and recovers nothing, which is what that lock guards. Setup can run
	// for minutes, and holding the lock across it would stall project cleanup on
	// every review round. Trade-off: cleanup may now delete the worktree while
	// setup runs, degrading the reviewer — acceptable, since cleanup is tearing
	// the run down regardless.
	wtPath := filepath.Join(projectRoot, paths.WorktreesDirName, taskID)
	if _, statErr := os.Stat(wtPath); statErr == nil {
		// Still run the configured setup command before the reviewer session: it
		// is idempotent, and a reviewer that builds and tests needs the same
		// prepared checkout the doer had.
		return false, runReviewerWorktreeSetup(bb, taskID, wtPath)
	}

	err = ops.WithProjectLifecycleSharedLock(projectRoot, "reviewer-worktree-recover", func() error {
		var recoverErr error
		recovered, recoverErr = ensureReviewerWorktreeLocked(projectRoot, bb, taskID, authority)
		return recoverErr
	})
	return recovered, err
}

func ensureReviewerWorktreeLocked(projectRoot string, bb *db.Blackboard, taskID string, authority models.AgentAuthority) (recovered bool, err error) {
	agentID := authority.ID
	wtPath := filepath.Join(projectRoot, paths.WorktreesDirName, taskID)
	if _, statErr := os.Stat(wtPath); statErr == nil {
		// Raced: a concurrent claim created the worktree between the fast-path
		// stat above and this lock. Same handling, re-checked under the lock.
		return false, runReviewerWorktreeSetup(bb, taskID, wtPath)
	}

	logger := GetLogger()
	logger.Warn("Worktree missing for reviewer task", "task_id", taskID)

	// Check if already recovered once.
	state, err := bb.Read()
	if err != nil {
		return false, fmt.Errorf("read state: %w", err)
	}
	task := state.FindTask(taskID)
	if task == nil {
		return false, fmt.Errorf("task %s not found", taskID)
	}

	for _, h := range task.History {
		if h.Event == models.TaskEventWorktreeRecovered {
			logger.Error("Blocking task: worktree still missing after prior recovery", "task_id", taskID)
			if blockErr := blockReviewerTask(bb, taskID, authority, "worktree missing after prior recovery attempt"); blockErr != nil {
				return false, blockErr
			}
			return false, fmt.Errorf("task %s: unrecoverable worktree: %w", taskID, errTaskBlocked)
		}
	}

	// Check if the task branch still exists.
	gitWrapper := git.New(projectRoot)
	branchName := paths.TaskBranchPrefix + taskID
	branchExists, branchErr := gitWrapper.BranchExists(branchName)
	if branchErr != nil {
		return false, fmt.Errorf("check branch: %w", branchErr)
	}
	if !branchExists {
		logger.Error("Cannot recover: branch also missing", "task_id", taskID, "branch", branchName)
		if blockErr := blockReviewerTask(bb, taskID, authority, "worktree and branch both missing — unrecoverable"); blockErr != nil {
			return false, blockErr
		}
		return false, fmt.Errorf("task %s: branch missing: %w", taskID, errTaskBlocked)
	}

	// Recreate worktree from existing branch.
	if attachErr := gitWrapper.AttachWorktree(taskID, branchName); attachErr != nil {
		logger.Error("Failed to recreate worktree", "task_id", taskID, "error", attachErr)
		return false, fmt.Errorf("worktree recreation failed: %w", attachErr)
	}

	if state.Config.CopyWorktreeEnvFiles {
		for _, warning := range ops.ProvisionWorktreeEnvFiles(projectRoot, wtPath) {
			logger.Warn("copy worktree env files warning after worktree recovery", "task_id", taskID, "warning", warning)
		}
	}

	// Run post-worktree command to ensure recovered worktree is build-ready.
	// Fail closed: an unprepared worktree must not reach a provider session. The
	// error is returned plain — the caller releases the reviewer claim back to
	// reviewable and degrades this reviewer, preserving the doer's work.
	if state.Config.PostWorktreeCmd != nil {
		if postErr := ops.RunPostWorktreeCmd(*state.Config.PostWorktreeCmd, wtPath); postErr != nil {
			logger.Error("post-worktree-cmd failed after worktree recovery", "task_id", taskID, "error", postErr)
			return false, postErr
		}
	}

	for _, warning := range reviewerWorktreePrepareSembleIgnore(wtPath) {
		logger.Warn("semble .sembleignore preparation warning after worktree recovery", "task_id", taskID, "warning", warning)
	}

	refreshResult, refreshErr := reviewerWorktreeRefreshIndexes(scipsearch.RefreshOptions{
		TargetRoot:          wtPath,
		TargetKind:          scipsearch.TargetKindTaskWorktree,
		ConfiguredLanguages: state.Config.ScipSearch,
	})
	if refreshErr != nil {
		logger.Warn("scip-search refresh failed after worktree recovery", "task_id", taskID, "error", refreshErr)
	}
	for _, failure := range refreshResult.Failures {
		logger.Warn(
			"scip-search indexer failed after worktree recovery",
			"task_id", taskID,
			"language", failure.Language,
			"diagnostic", failure.Diagnostic,
		)
	}
	stacklitResult, stacklitErr := reviewerWorktreeRefreshStacklitIndex(stacklit.RefreshOptions{
		TargetRoot: wtPath,
		TargetKind: stacklit.TargetKindTaskWorktree,
	})
	if stacklitErr != nil {
		logger.Warn("stacklit refresh failed after worktree recovery", "task_id", taskID, "error", stacklitErr)
	}
	for _, failure := range stacklitResult.Failures {
		logger.Warn(
			"stacklit indexer failed after worktree recovery",
			"task_id", taskID,
			"diagnostic", failure.Diagnostic,
		)
	}
	functionalClustersResult, functionalClustersErr := reviewerWorktreeRefreshFunctionalClustersIndex(functionalclusters.RefreshOptions{
		TargetRoot:          wtPath,
		TargetKind:          functionalclusters.TargetKindTaskWorktree,
		ConfiguredLanguages: state.Config.ScipSearch,
	})
	if functionalClustersErr != nil {
		logger.Warn("functional-clusters refresh failed after worktree recovery", "task_id", taskID, "error", functionalClustersErr)
	}
	for _, failure := range functionalClustersResult.Failures {
		logger.Warn(
			"functional-clusters build failed after worktree recovery",
			"task_id", taskID,
			"diagnostic", failure.Diagnostic,
		)
	}

	// Record recovery in history.
	if modErr := ops.ModifyWithAgentAuthority(bb, authority, func(s *models.State) error {
		t := s.FindTask(taskID)
		if t != nil {
			agentPtr := agentID
			t.History = append(t.History, models.TaskHistoryEntry{
				Time:  time.Now().UTC(),
				Event: models.TaskEventWorktreeRecovered,
				Agent: &agentPtr,
			})
		}
		return nil
	}); modErr != nil {
		logger.Warn("Failed to record worktree recovery in history", "task_id", taskID, "error", modErr)
	}

	logger.Info("Worktree recovered from branch", "task_id", taskID, "branch", branchName)
	return true, nil
}

// runReviewerWorktreeSetup runs the configured post_worktree_cmd against an
// intact reviewer worktree. Returns the setup error unchanged; the caller owns
// claim release and agent degradation.
func runReviewerWorktreeSetup(bb *db.Blackboard, taskID, wtPath string) error {
	state, err := bb.Read()
	if err != nil {
		return fmt.Errorf("read state: %w", err)
	}
	if state.Config.PostWorktreeCmd == nil {
		return nil
	}
	if task := state.FindTask(taskID); task != nil && len(task.ValidationPrerequisites) > 0 {
		// Protected reviewer claims ran setup before preflight and assignment.
		return nil
	}
	postErr := ops.RunPostWorktreeCmd(*state.Config.PostWorktreeCmd, wtPath)
	if postErr != nil {
		GetLogger().Error("post-worktree-cmd failed for reviewer worktree", "task_id", taskID, "error", postErr)
	}
	return postErr
}

// blockReviewerTask forces a task into BLOCKED status, bypassing normal
// transition validation. This handles the exceptional case where a reviewer
// task's worktree is unrecoverable and no valid transition path to BLOCKED exists.
//
// Reserved for lost work. A task blocked here is restored by unblock-task to the
// doer's executing status (unblock_task.go), which discards review readiness —
// acceptable when the worktree is gone, not when it merely failed to prepare.
func blockReviewerTask(bb *db.Blackboard, taskID string, authority models.AgentAuthority, reason string) error {
	agentID := authority.ID
	if err := ops.ModifyWithAgentAuthority(bb, authority, func(state *models.State) error {
		t := state.FindTask(taskID)
		if t == nil {
			return nil
		}

		// Force status to BLOCKED — no valid transition exists from REVIEWING.
		t.Status = models.TaskStatusBlocked
		t.BlockedReason = &reason
		t.BlockedQuestions = []string{
			"Is the task branch recoverable from a remote or backup?",
			"Should this task be recreated with a fresh worktree?",
		}

		// Release the reviewer agent state before clearing claim fields.
		if t.ReviewingBy != nil {
			state.ReleaseAgent(*t.ReviewingBy)
		}

		// Clear reviewer claim.
		t.ReviewingBy = nil
		t.ReviewLeaseExpires = nil

		now := time.Now().UTC()
		t.History = append(t.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventBlocked,
			Agent:  &agentID,
			Reason: &reason,
		})

		return nil
	}); err != nil {
		return err
	}
	return nil
}
