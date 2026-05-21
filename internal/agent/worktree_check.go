package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/scipsearch"
)

// errTaskBlocked is a sentinel indicating that blockReviewerTask already
// handled claim release and agent state cleanup. The supervisor must NOT
// call releaseReviewerClaimQuietly when this error is returned.
var errTaskBlocked = errors.New("task blocked")

var reviewerWorktreeRefreshIndexes = scipsearch.RefreshIndexes

// ensureReviewerWorktree verifies the worktree exists for a reviewer task.
// Returns (true, nil) if the worktree was recovered from an existing branch.
// Returns (false, nil) if the worktree already exists.
// Returns (false, error) if the task was blocked or recovery failed.
func ensureReviewerWorktree(projectRoot string, bb *db.Blackboard, taskID, agentID string) (recovered bool, err error) {
	wtPath := filepath.Join(projectRoot, paths.WorktreesDirName, taskID)
	if _, statErr := os.Stat(wtPath); statErr == nil {
		return false, nil // exists, nothing to do
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
			blockReviewerTask(bb, taskID, agentID, "worktree missing after prior recovery attempt")
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
		blockReviewerTask(bb, taskID, agentID, "worktree and branch both missing — unrecoverable")
		return false, fmt.Errorf("task %s: branch missing: %w", taskID, errTaskBlocked)
	}

	// Recreate worktree from existing branch.
	if attachErr := gitWrapper.AttachWorktree(taskID, branchName); attachErr != nil {
		logger.Error("Failed to recreate worktree", "task_id", taskID, "error", attachErr)
		return false, fmt.Errorf("worktree recreation failed: %w", attachErr)
	}

	// Run post-worktree command to ensure recovered worktree is build-ready.
	if state.Config.PostWorktreeCmd != nil {
		if postErr := ops.RunPostWorktreeCmd(*state.Config.PostWorktreeCmd, wtPath); postErr != nil {
			logger.Warn("post-worktree-cmd failed after worktree recovery", "task_id", taskID, "error", postErr)
		}
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

	// Record recovery in history.
	if modErr := bb.Modify(func(s *models.State) error {
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

// blockReviewerTask forces a task into BLOCKED status, bypassing normal
// transition validation. This handles the exceptional case where a reviewer
// task's worktree is unrecoverable and no valid transition path to BLOCKED exists.
func blockReviewerTask(bb *db.Blackboard, taskID, agentID, reason string) {
	if err := bb.Modify(func(state *models.State) error {
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
		GetLogger().Error("Failed to block reviewer task", "task_id", taskID, "error", err)
	}
}
