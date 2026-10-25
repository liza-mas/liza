package ops

import (
	"fmt"
	"os"
	"strings"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

type recoverTaskArtifactSnapshot struct {
	WorktreePath string
	WorktreeRel  string
	BranchName   string
	Worktree     bool
	Branch       bool
	BaseCommit   string
}

func inspectRecoverTaskArtifacts(gitWrapper *git.Git, taskID string, result *RecoverTaskResult) recoverTaskArtifactSnapshot {
	wtPath := gitWrapper.GetWorktreePath(taskID)
	branchName := paths.TaskBranchPrefix + taskID
	artifact := recoverTaskArtifactSnapshot{
		WorktreePath: wtPath,
		WorktreeRel:  fmt.Sprintf("%s/%s", paths.WorktreesDirName, taskID),
		BranchName:   branchName,
	}
	if _, statErr := os.Stat(wtPath); statErr == nil {
		artifact.Worktree = true
	}
	branchExisted, branchErr := gitWrapper.BranchExists(branchName)
	if branchErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("branch existence check: %v", branchErr))
	}
	artifact.Branch = branchExisted
	return artifact
}

func recoverTaskCleanupGitArtifacts(gitWrapper *git.Git, taskID string, artifact recoverTaskArtifactSnapshot, result *RecoverTaskResult) error {
	if artifact.Worktree || artifact.Branch {
		if testRecoverTaskHooks != nil && testRecoverTaskHooks.cleanupGitArtifacts != nil {
			if err := testRecoverTaskHooks.cleanupGitArtifacts(); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("worktree removal: %v", err))
				return err
			}
		}
		if err := gitWrapper.RemoveWorktree(taskID); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("worktree removal: %v", err))
			return err
		}
		result.WorktreeRemoved = artifact.Worktree
		result.BranchRemoved = artifact.Branch
	}
	return nil
}

func recoverTaskPreserveArtifacts(gitWrapper *git.Git, task *models.Task, statuses recoverTaskStatusSet, artifact *recoverTaskArtifactSnapshot, result *RecoverTaskResult) (bool, error) {
	if artifact.Worktree && !artifact.Branch {
		return false, fmt.Errorf("task %s: worktree exists but branch %s is missing; preserve recovery fails closed (use --fresh to discard)", task.ID, artifact.BranchName)
	}
	if !artifact.Worktree && !artifact.Branch {
		if task.Status == statuses.Executing {
			return false, nil
		}
		if isRecoverTaskReviewCandidate(task.Status, statuses) {
			return false, fmt.Errorf("task %s: submitted candidate is unrecoverable because worktree and branch are both missing", task.ID)
		}
		if task.Status == models.TaskStatusIntegrationFailed {
			return false, fmt.Errorf("task %s: integration-failed repair substrate is unrecoverable because worktree and branch are both missing", task.ID)
		}
		return false, nil
	}
	if !artifact.Worktree && artifact.Branch {
		if err := gitWrapper.AttachWorktree(task.ID, artifact.BranchName); err != nil {
			return false, fmt.Errorf("reattach worktree from %s: %w", artifact.BranchName, err)
		}
		result.WorktreeRecovered = true
		artifact.Worktree = true
	}
	if err := gitWrapper.ValidateWorktreeHealth(task.ID); err != nil {
		return false, fmt.Errorf("preserved worktree not healthy: %w", err)
	}
	status, err := gitWrapper.WorktreeStatusShort(artifact.WorktreePath)
	if err != nil {
		return false, fmt.Errorf("preserved worktree status: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return false, fmt.Errorf("preserved worktree is dirty; recover-task refuses to redispatch ambiguous work (use --fresh to discard)")
	}
	baseCommit := ""
	if task.BaseCommit != nil {
		baseCommit = *task.BaseCommit
	}
	if baseCommit == "" {
		head, err := gitWrapper.GetWorktreeHEAD(task.ID)
		if err != nil {
			return false, err
		}
		baseCommit = head
	}
	artifact.BaseCommit = baseCommit
	if isRecoverTaskReviewCandidate(task.Status, statuses) {
		if task.ReviewCommit == nil || *task.ReviewCommit == "" {
			return false, fmt.Errorf("task %s has no review_commit; cannot preserve submitted review boundary", task.ID)
		}
		head, err := gitWrapper.GetWorktreeHEAD(task.ID)
		if err != nil {
			return false, err
		}
		if head != *task.ReviewCommit {
			return false, fmt.Errorf("review_commit %s does not match worktree HEAD %s; recover-task refuses to review a different candidate", *task.ReviewCommit, head)
		}
	}
	return true, nil
}
