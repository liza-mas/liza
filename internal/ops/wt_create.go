package ops

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// CreateWorktreeResult contains the outcome of creating a worktree.
type CreateWorktreeResult struct {
	TaskID         string   `json:"task_id"`
	WorktreeDir    string   `json:"worktree_dir"`
	BaseCommit     string   `json:"base_commit"`
	AlreadyExisted bool     `json:"already_existed"` // true if worktree existed and fresh was false
	Warnings       []string `json:"warnings"`
}

// CreateWorktree provisions a git worktree from the integration branch for a
// task in an executing state and records its base_commit. When fresh is true,
// deletes any existing worktree first (for reassignment). No terminal I/O.
func CreateWorktree(projectRoot, taskID string, fresh bool) (*CreateWorktreeResult, error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}

	lp := paths.New(projectRoot)
	worktreeRel := filepath.Join(paths.WorktreesDirName, taskID)
	worktreeDir := filepath.Join(lp.ProjectRoot(), worktreeRel)

	bb := db.For(lp.StatePath())
	state, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}

	pr, _ := LoadResolverForModels(projectRoot)
	if !models.IsExecutingStatus(task, pr) {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s is not in an executing state (status: %s)", taskID, task.Status)}
	}

	integrationBranch := state.Config.IntegrationBranch
	postCmd := state.Config.PostWorktreeCmd

	gitWrapper := git.New(lp.ProjectRoot())

	// Check if worktree already exists
	if _, err := os.Stat(worktreeDir); err == nil {
		if !fresh {
			result := &CreateWorktreeResult{
				TaskID:         taskID,
				WorktreeDir:    worktreeDir,
				AlreadyExisted: true,
			}
			// Provision Claude Code config — idempotent, catches prior failures or upgrades.
			result.Warnings = append(result.Warnings, ProvisionClaudeConfig(lp.ProjectRoot(), worktreeDir)...)
			// Run post-worktree command even on existing worktrees — idempotent, catches prior failures.
			if postCmd != nil {
				if err := RunPostWorktreeCmd(*postCmd, worktreeDir); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("post-worktree-cmd: %v", err))
				}
			}
			return result, nil
		}
	}

	var baseCommit string
	if fresh {
		baseCommit, err = gitWrapper.CreateWorktreeFresh(taskID, integrationBranch)
	} else {
		baseCommit, err = gitWrapper.CreateWorktree(taskID, integrationBranch)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create worktree: %w", err)
	}

	err = bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		task.BaseCommit = &baseCommit
		return nil
	})

	if err != nil {
		_ = gitWrapper.RemoveWorktree(taskID)
		return nil, fmt.Errorf("failed to update state: %w", err)
	}

	result := &CreateWorktreeResult{
		TaskID:      taskID,
		WorktreeDir: worktreeDir,
		BaseCommit:  baseCommit,
	}

	// Provision Claude Code config for agents in worktrees.
	result.Warnings = append(result.Warnings, ProvisionClaudeConfig(lp.ProjectRoot(), worktreeDir)...)

	// Run post-worktree command so the worktree is build/test-ready.
	// Non-fatal: agents can run the command manually.
	if postCmd != nil {
		if err := RunPostWorktreeCmd(*postCmd, worktreeDir); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("post-worktree-cmd: %v", err))
		}
	}

	return result, nil
}

// ProvisionClaudeConfig copies Claude Code configuration files from the project
// root into a worktree so that agents launched there have correct settings.
// Files that don't exist in the project root are silently skipped.
// Returns warnings for any copy failures (non-fatal).
func ProvisionClaudeConfig(projectRoot, worktreeDir string) []string {
	var warnings []string

	// Individual files to copy (relative to project root).
	files := []string{
		filepath.Join(".claude", "settings.json"),
		filepath.Join(".claude", "settings.local.json"),
	}

	for _, rel := range files {
		src := filepath.Join(projectRoot, rel)
		dst := filepath.Join(worktreeDir, rel)
		if err := copyFilePreserveMode(src, dst); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			warnings = append(warnings, fmt.Sprintf("provision-claude-config: %s: %v", rel, err))
		}
	}

	// Copy hook scripts (may have execute bits).
	hooksDir := filepath.Join(projectRoot, ".claude", "hooks")
	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		// No hooks directory — nothing to copy.
		return warnings
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		rel := filepath.Join(".claude", "hooks", entry.Name())
		src := filepath.Join(projectRoot, rel)
		dst := filepath.Join(worktreeDir, rel)
		if err := copyFilePreserveMode(src, dst); err != nil {
			warnings = append(warnings, fmt.Sprintf("provision-claude-config: %s: %v", rel, err))
		}
	}

	return warnings
}

// copyFilePreserveMode copies a file from src to dst, creating parent
// directories as needed and preserving the source file's permission bits.
func copyFilePreserveMode(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	info, err := srcFile.Stat()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}

	_, err = io.Copy(dstFile, srcFile)
	if closeErr := dstFile.Close(); err == nil {
		err = closeErr
	}
	return err
}

// RunPostWorktreeCmd runs the configured post-worktree shell command in the given directory.
// It is idempotent and safe to call on both new and existing worktrees.
//
// Trust model: the command comes from state.yaml which lives inside .liza/ in
// the project root. Write access to state.yaml implies write access to the
// repo (same trust boundary as Makefile, .github/workflows/, package.json
// scripts). No additional confirmation gate is needed.
func RunPostWorktreeCmd(cmdStr, dir string) error {
	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}
