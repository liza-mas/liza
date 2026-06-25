package ops

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/worktreeexclude"
)

// worktreeHooksDirName returns the directory, relative to each worktree root, where
// the per-worktree git hooks live. Git resolves core.hooksPath (set via
// --worktree) to this directory. See InstallWorktreePreCommitHook.
func worktreeHooksDirName() string {
	return paths.ProjectDirName() + "-hooks"
}

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
	copyEnvFiles := state.Config.CopyWorktreeEnvFiles
	scipSearchLanguages := append([]string(nil), state.Config.ScipSearch...)

	gitWrapper := git.New(lp.ProjectRoot())

	// Check if worktree already exists
	if _, err := os.Stat(worktreeDir); err == nil {
		if !fresh {
			if err := gitWrapper.ValidateWorktreeHealth(taskID); err != nil {
				return nil, fmt.Errorf("existing worktree not healthy: %w", err)
			}
			result := &CreateWorktreeResult{
				TaskID:         taskID,
				WorktreeDir:    worktreeDir,
				AlreadyExisted: true,
			}
			// Provision Claude Code config — idempotent, catches prior failures or upgrades.
			result.Warnings = append(result.Warnings, ProvisionClaudeConfig(lp.ProjectRoot(), worktreeDir)...)
			// Install the pre-commit hook — idempotent, upgrades pre-hook-era worktrees
			// without requiring fresh=true.
			result.Warnings = append(result.Warnings, InstallWorktreePreCommitHook(gitWrapper, worktreeDir, taskID)...)
			if copyEnvFiles {
				result.Warnings = append(result.Warnings, ProvisionWorktreeEnvFiles(lp.ProjectRoot(), worktreeDir)...)
			}
			// Run post-worktree command even on existing worktrees — idempotent, catches prior failures.
			if postCmd != nil {
				if err := RunPostWorktreeCmd(*postCmd, worktreeDir); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("post-worktree-cmd: %v", err))
				}
			}
			result.Warnings = append(result.Warnings, PrepareSembleWorktreeIgnore(worktreeDir)...)
			result.Warnings = append(result.Warnings, refreshTaskWorktreeScipIndexes(worktreeDir, scipSearchLanguages)...)
			result.Warnings = append(result.Warnings, refreshTaskWorktreeStacklitIndex(worktreeDir)...)
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

	// Install the per-worktree pre-commit hook (git-level guard against
	// post-submission mutations). Non-fatal: the submit-verdict HEAD check
	// remains as the second line of defense.
	result.Warnings = append(result.Warnings, InstallWorktreePreCommitHook(gitWrapper, worktreeDir, taskID)...)

	if copyEnvFiles {
		result.Warnings = append(result.Warnings, ProvisionWorktreeEnvFiles(lp.ProjectRoot(), worktreeDir)...)
	}

	// Run post-worktree command so the worktree is build/test-ready.
	// Non-fatal: agents can run the command manually.
	if postCmd != nil {
		if err := RunPostWorktreeCmd(*postCmd, worktreeDir); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("post-worktree-cmd: %v", err))
		}
	}
	result.Warnings = append(result.Warnings, PrepareSembleWorktreeIgnore(worktreeDir)...)
	result.Warnings = append(result.Warnings, refreshTaskWorktreeScipIndexes(worktreeDir, scipSearchLanguages)...)
	result.Warnings = append(result.Warnings, refreshTaskWorktreeStacklitIndex(worktreeDir)...)

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

// ProvisionWorktreeEnvFiles copies explicitly authorized, ignored root env files
// from the project root into a task worktree. It never walks subdirectories and
// never follows symlinks; only root-level regular files matching the candidate
// predicate are eligible. Destination ignore setup is verified before any copy.
func ProvisionWorktreeEnvFiles(projectRoot, worktreeDir string) []string {
	entries, err := os.ReadDir(projectRoot)
	if err != nil {
		return []string{fmt.Sprintf("copy-worktree-env-files: read project root: %v", err)}
	}

	var warnings []string
	for _, entry := range entries {
		rel := entry.Name()
		if !isWorktreeEnvFileCandidate(rel) {
			continue
		}

		src := filepath.Join(projectRoot, rel)
		info, err := os.Lstat(src)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("copy-worktree-env-files: %s: inspect source: %v", rel, err))
			continue
		}
		if !info.Mode().IsRegular() {
			warnings = append(warnings, fmt.Sprintf("copy-worktree-env-files: %s: source is not a regular file", rel))
			continue
		}

		if err := checkIgnored(projectRoot, rel); err != nil {
			warnings = append(warnings, fmt.Sprintf("copy-worktree-env-files: %s: source is not ignored by git", rel))
			continue
		}
		if err := worktreeexclude.EnsurePrivateExclude(worktreeDir, rel); err != nil {
			warnings = append(warnings, fmt.Sprintf("copy-worktree-env-files: %s: configure worktree ignore: %v", rel, err))
			continue
		}
		if err := checkIgnored(worktreeDir, rel); err != nil {
			warnings = append(warnings, fmt.Sprintf("copy-worktree-env-files: %s: destination is not ignored by git", rel))
			continue
		}

		dst := filepath.Join(worktreeDir, rel)
		if _, err := os.Lstat(dst); err == nil {
			// Preserve existing worktree-local env files; ignore was verified above.
			continue
		} else if !os.IsNotExist(err) {
			warnings = append(warnings, fmt.Sprintf("copy-worktree-env-files: %s: inspect destination: %v", rel, err))
			continue
		}

		if err := copyFilePreserveMode(src, dst); err != nil {
			warnings = append(warnings, fmt.Sprintf("copy-worktree-env-files: %s: copy: %v", rel, err))
		}
	}
	return warnings
}

func isWorktreeEnvFileCandidate(name string) bool {
	return name == ".env" ||
		strings.HasPrefix(name, ".env.") ||
		strings.HasSuffix(name, ".env") ||
		name == ".envrc"
}

func checkIgnored(dir, rel string) error {
	_, err := gitenv.CombinedOutput(dir, "check-ignore", "--quiet", "--", rel)
	return err
}

// InstallWorktreePreCommitHook writes the rendered pre-commit hook into the
// worktree and configures the worktree's git to look for hooks there. Returns
// warnings for any non-fatal errors (install failures don't abort worktree
// creation — the submit-verdict HEAD check remains as the authoritative
// guard).
//
// Mechanism:
//  1. Enable extensions.worktreeConfig on the main repo (idempotent).
//  2. Write the rendered hook to <worktreeDir>/<brand-hooks>/pre-commit (chmod 0755).
//  3. git config --worktree core.hooksPath <abs-path-to-brand-hooks> in the
//     worktree.
//
// The hook dir lives inside the worktree rather than under .git/worktrees/...
// so it's visible to humans inspecting the worktree and survives worktree
// repair. Agents can still bypass via git commit --no-verify; this is a
// guard, not a lock.
func InstallWorktreePreCommitHook(gitWrapper *git.Git, worktreeDir, taskID string) []string {
	var warnings []string

	lizaBin, err := os.Executable()
	if err != nil {
		// Fall back to PATH lookup at hook exec time.
		lizaBin = brand.RuntimeValues().BinaryName
	}

	if err := gitWrapper.EnableWorktreeConfigExtension(); err != nil {
		warnings = append(warnings, fmt.Sprintf("install-pre-commit-hook: enable worktreeConfig: %v", err))
		return warnings
	}

	hooksDir := filepath.Join(worktreeDir, worktreeHooksDirName())
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		warnings = append(warnings, fmt.Sprintf("install-pre-commit-hook: mkdir: %v", err))
		return warnings
	}

	hookPath := filepath.Join(hooksDir, "pre-commit")
	content := embedded.RenderWorktreePreCommitHook(lizaBin, taskID)
	if err := os.WriteFile(hookPath, content, 0755); err != nil {
		warnings = append(warnings, fmt.Sprintf("install-pre-commit-hook: write: %v", err))
		return warnings
	}

	hooksAbs, err := filepath.Abs(hooksDir)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("install-pre-commit-hook: resolve abs: %v", err))
		return warnings
	}

	if err := gitWrapper.SetWorktreeHooksPath(worktreeDir, hooksAbs); err != nil {
		warnings = append(warnings, fmt.Sprintf("install-pre-commit-hook: set hooksPath: %v", err))
		return warnings
	}

	// Verify the config actually took. If extensions.worktreeConfig is not
	// honored (old git, odd repo state), git writes the --worktree value to
	// a config file it never reads, producing a ghost hook. Surface that as
	// a warning so silent misconfiguration is visible at create time.
	got, err := gitWrapper.GetWorktreeHooksPath(worktreeDir)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("install-pre-commit-hook: verify hooksPath: %v", err))
		return warnings
	}
	if got != hooksAbs {
		warnings = append(warnings, fmt.Sprintf(
			"install-pre-commit-hook: hooksPath verification mismatch (got %q, want %q) — hook may not fire",
			got, hooksAbs,
		))
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
