package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/functionalclusters"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/stacklit"
)

// errTaskBlocked is a sentinel indicating that ops.ForceBlockReviewerTask already
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
		return false, runReviewerWorktreeSetup(projectRoot, bb, taskID, wtPath, authority)
	}

	err = ops.WithProjectLifecycleSharedLock(projectRoot, "reviewer-worktree-recover", func() error {
		var recoverErr error
		recovered, recoverErr = ensureReviewerWorktreeLocked(projectRoot, bb, taskID, authority)
		return recoverErr
	})
	return recovered, err
}

func ensureReviewerWorktreeLocked(projectRoot string, bb *db.Blackboard, taskID string, authority models.AgentAuthority) (recovered bool, err error) {
	wtPath := filepath.Join(projectRoot, paths.WorktreesDirName, taskID)
	if _, statErr := os.Stat(wtPath); statErr == nil {
		// Raced: a concurrent claim created the worktree between the fast-path
		// stat above and this lock. Same handling, re-checked under the lock.
		return false, runReviewerWorktreeSetup(projectRoot, bb, taskID, wtPath, authority)
	}

	GetLogger().Warn("Worktree missing for reviewer task", "task_id", taskID)

	state, branchName, err := attachReviewerWorktree(projectRoot, bb, taskID, authority)
	if err != nil {
		return false, err
	}

	if err := prepareRecoveredReviewerWorktree(projectRoot, taskID, wtPath, state.Config, authority); err != nil {
		return false, err
	}

	recordReviewerWorktreeRecovery(bb, taskID, authority)

	GetLogger().Info("Worktree recovered from branch", "task_id", taskID, "branch", branchName)
	return true, nil
}

// attachReviewerWorktree decides whether the missing worktree is recoverable
// and, when it is, recreates it from the task branch. A worktree already
// recovered once, or a missing branch, blocks the task (errTaskBlocked). It
// returns the state it read and the branch it attached.
func attachReviewerWorktree(projectRoot string, bb *db.Blackboard, taskID string, authority models.AgentAuthority) (*models.State, string, error) {
	logger := GetLogger()

	// Check if already recovered once.
	state, err := bb.ReadSnapshot()
	if err != nil {
		return nil, "", fmt.Errorf("read state: %w", err)
	}
	task := state.FindTask(taskID)
	if task == nil {
		return nil, "", fmt.Errorf("task %s not found", taskID)
	}

	for _, h := range task.History {
		if h.Event == models.TaskEventWorktreeRecovered {
			logger.Error("Blocking task: worktree still missing after prior recovery", "task_id", taskID)
			if blockErr := ops.ForceBlockReviewerTask(bb, authority, taskID, "worktree missing after prior recovery attempt"); blockErr != nil {
				return nil, "", blockErr
			}
			return nil, "", fmt.Errorf("task %s: unrecoverable worktree: %w", taskID, errTaskBlocked)
		}
	}

	// Check if the task branch still exists.
	gitWrapper := git.New(projectRoot)
	branchName := paths.TaskBranchPrefix + taskID
	branchExists, branchErr := gitWrapper.BranchExists(branchName)
	if branchErr != nil {
		return nil, "", fmt.Errorf("check branch: %w", branchErr)
	}
	if !branchExists {
		logger.Error("Cannot recover: branch also missing", "task_id", taskID, "branch", branchName)
		if blockErr := ops.ForceBlockReviewerTask(bb, authority, taskID, "worktree and branch both missing — unrecoverable"); blockErr != nil {
			return nil, "", blockErr
		}
		return nil, "", fmt.Errorf("task %s: branch missing: %w", taskID, errTaskBlocked)
	}

	// Recreate worktree from existing branch.
	if attachErr := gitWrapper.AttachWorktree(taskID, branchName); attachErr != nil {
		logger.Error("Failed to recreate worktree", "task_id", taskID, "error", attachErr)
		return nil, "", fmt.Errorf("worktree recreation failed: %w", attachErr)
	}
	return state, branchName, nil
}

// prepareRecoveredReviewerWorktree makes a recovered worktree build-ready: env
// files, the configured setup command (fail closed), the semble ignore file,
// then the index refreshes (warnings only).
func prepareRecoveredReviewerWorktree(projectRoot, taskID, wtPath string, cfg models.Config, authority models.AgentAuthority) error {
	logger := GetLogger()

	if cfg.CopyWorktreeEnvFiles {
		for _, warning := range ops.ProvisionWorktreeEnvFiles(projectRoot, wtPath) {
			logger.Warn("copy worktree env files warning after worktree recovery", "task_id", taskID, "warning", warning)
		}
	}

	// Run post-worktree command to ensure recovered worktree is build-ready.
	// Fail closed: an unprepared worktree must not reach a provider session. The
	// error is returned plain — the caller releases the reviewer claim back to
	// reviewable and degrades this reviewer, preserving the doer's work.
	if cfg.PostWorktreeCmd != nil {
		if postErr := ops.RunPostWorktreeCmd(projectRoot, *cfg.PostWorktreeCmd, wtPath, models.EffectivePostWorktreeCmdTimeout(cfg), &authority); postErr != nil {
			logger.Error("post-worktree-cmd failed after worktree recovery", "task_id", taskID, "error", postErr)
			return postErr
		}
	}

	for _, warning := range reviewerWorktreePrepareSembleIgnore(wtPath) {
		logger.Warn("semble .sembleignore preparation warning after worktree recovery", "task_id", taskID, "warning", warning)
	}

	refreshRecoveredReviewerIndexes(projectRoot, taskID, wtPath, cfg)
	return nil
}

// refreshRecoveredReviewerIndexes refreshes the scip-search, stacklit and
// functional-clusters indexes of a recovered worktree, in that order. A failure
// is a warning: the reviewer can work without the indexes.
func refreshRecoveredReviewerIndexes(projectRoot, taskID, wtPath string, cfg models.Config) {
	logger := GetLogger()

	refreshResult, refreshErr := reviewerWorktreeRefreshIndexes(scipsearch.RefreshOptions{
		ProjectRoot:         projectRoot,
		TargetRoot:          wtPath,
		ConfiguredLanguages: cfg.ScipSearch,
	})
	if refreshErr != nil {
		logger.Warn("scip-search refresh failed after worktree recovery", "task_id", taskID, "error", refreshErr)
	}
	for _, failure := range refreshResult.Failures {
		logger.Warn(
			"scip-search index copy failed after worktree recovery",
			"task_id", taskID,
			"language", failure.Language,
			"diagnostic", failure.Diagnostic,
		)
	}
	stacklitResult, stacklitErr := reviewerWorktreeRefreshStacklitIndex(stacklit.RefreshOptions{
		ProjectRoot: projectRoot,
		TargetRoot:  wtPath,
	})
	if stacklitErr != nil {
		logger.Warn("stacklit refresh failed after worktree recovery", "task_id", taskID, "error", stacklitErr)
	}
	for _, failure := range stacklitResult.Failures {
		logger.Warn(
			"stacklit index copy failed after worktree recovery",
			"task_id", taskID,
			"diagnostic", failure.Diagnostic,
		)
	}
	functionalClustersResult, functionalClustersErr := reviewerWorktreeRefreshFunctionalClustersIndex(functionalclusters.RefreshOptions{
		ProjectRoot:         projectRoot,
		TargetRoot:          wtPath,
		ConfiguredLanguages: cfg.ScipSearch,
	})
	if functionalClustersErr != nil {
		logger.Warn("functional-clusters refresh failed after worktree recovery", "task_id", taskID, "error", functionalClustersErr)
	}
	for _, failure := range functionalClustersResult.Failures {
		logger.Warn(
			"functional-clusters artifact copy failed after worktree recovery",
			"task_id", taskID,
			"diagnostic", failure.Diagnostic,
		)
	}
}

// recordReviewerWorktreeRecovery records the recovery in the task history. A
// failed write is a warning: the worktree is already back.
func recordReviewerWorktreeRecovery(bb *db.Blackboard, taskID string, authority models.AgentAuthority) {
	if modErr := ops.RecordWorktreeRecovered(bb, authority, taskID); modErr != nil {
		GetLogger().Warn("Failed to record worktree recovery in history", "task_id", taskID, "error", modErr)
	}
}

// runReviewerWorktreeSetup runs the configured post_worktree_cmd against an
// intact reviewer worktree. Returns the setup error unchanged; the caller owns
// claim release and agent degradation.
func runReviewerWorktreeSetup(projectRoot string, bb *db.Blackboard, taskID, wtPath string, authority models.AgentAuthority) error {
	state, err := bb.ReadSnapshot()
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
	postErr := ops.RunPostWorktreeCmd(projectRoot, *state.Config.PostWorktreeCmd, wtPath, models.EffectivePostWorktreeCmdTimeout(state.Config), &authority)
	if postErr != nil {
		GetLogger().Error("post-worktree-cmd failed for reviewer worktree", "task_id", taskID, "error", postErr)
	}
	return postErr
}
