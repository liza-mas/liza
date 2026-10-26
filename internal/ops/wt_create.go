package ops

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
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
	return paths.ProjectDirName() + worktreeHooksDirSuffix
}

// Named values of the provisioning steps (K7); the bytes and modes are the
// ones the steps always used.
const (
	claudeConfigDirName       = ".claude"
	claudeSettingsFileName    = "settings.json"
	claudeLocalSettingsName   = "settings.local.json"
	claudeHooksDirName        = "hooks"
	preCommitHookFileName     = "pre-commit"
	worktreeDirMode           = 0o755
	worktreeHookFileMode      = 0o755
	provisionClaudeConfigWarn = "provision-claude-config"
	copyWorktreeEnvFilesWarn  = "copy-worktree-env-files"
	installPreCommitHookWarn  = "install-pre-commit-hook"
	// worktreeCreateOperation labels the shared project lifecycle hold of
	// worktree creation.
	worktreeCreateOperation = "worktree-create"
	// worktreeHooksDirSuffix follows the project directory name in the name of
	// the per-worktree hooks directory.
	worktreeHooksDirSuffix = "-hooks"

	// The file names ProvisionWorktreeEnvFiles recognises as environment
	// files: the plain name, a dotted variant, an extension and the direnv name.
	worktreeEnvFileName   = ".env"
	worktreeEnvFilePrefix = ".env."
	worktreeEnvFileSuffix = ".env"
	worktreeEnvrcFileName = ".envrc"
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
	var result *CreateWorktreeResult
	err := WithProjectLifecycleSharedLock(projectRoot, worktreeCreateOperation, func() error {
		var createErr error
		result, createErr = createWorktree(projectRoot, taskID, fresh)
		return createErr
	})
	return result, err
}

func createWorktree(projectRoot, taskID string, fresh bool) (*CreateWorktreeResult, error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}

	lp := paths.New(projectRoot)
	worktreeDir := filepath.Join(lp.ProjectRoot(), paths.WorktreesDirName, taskID)

	bb := db.For(lp.StatePath())
	state, err := readExecutingTaskState(bb, projectRoot, taskID)
	if err != nil {
		return nil, err
	}

	gitWrapper := git.New(lp.ProjectRoot())
	provisioning := worktreeProvisioning{
		projectRoot:         lp.ProjectRoot(),
		worktreeDir:         worktreeDir,
		taskID:              taskID,
		gitWrapper:          gitWrapper,
		copyEnvFiles:        state.Config.CopyWorktreeEnvFiles,
		postCmd:             state.Config.PostWorktreeCmd,
		postCmdTimeout:      models.EffectivePostWorktreeCmdTimeout(state.Config),
		scipSearchLanguages: append([]string(nil), state.Config.ScipSearch...),
	}

	if _, err := os.Stat(worktreeDir); err == nil && !fresh {
		if err := gitWrapper.ValidateWorktreeHealth(taskID); err != nil {
			return nil, fmt.Errorf("existing worktree not healthy: %w", err)
		}
		warnings, err := provisionWorktree(provisioning)
		if err != nil {
			return nil, err
		}
		return &CreateWorktreeResult{
			TaskID:         taskID,
			WorktreeDir:    worktreeDir,
			AlreadyExisted: true,
			Warnings:       warnings,
		}, nil
	}

	baseCommit, err := createWorktreeBranch(gitWrapper, taskID, state.Config.IntegrationBranch, fresh)
	if err != nil {
		return nil, fmt.Errorf("failed to create worktree: %w", err)
	}
	if err := recordWorktreeBaseCommit(bb, gitWrapper, taskID, baseCommit); err != nil {
		return nil, err
	}

	warnings, err := provisionWorktree(provisioning)
	if err != nil {
		return nil, err
	}
	return &CreateWorktreeResult{
		TaskID:      taskID,
		WorktreeDir: worktreeDir,
		BaseCommit:  baseCommit,
		Warnings:    warnings,
	}, nil
}

// readExecutingTaskState reads the state and refuses unless the task is in an
// executing status.
func readExecutingTaskState(bb *db.Blackboard, projectRoot, taskID string) (*models.State, error) {
	state, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}
	pr, _ := LoadResolverForModels(projectRoot)
	if !models.IsExecutingStatus(task, pr) {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s is not in an executing state (status: %s)", taskID, task.Status)}
	}
	return state, nil
}

// createWorktreeBranch creates the task worktree from the integration branch
// outside any lock. fresh deletes an existing worktree first (reassignment).
func createWorktreeBranch(gitWrapper *git.Git, taskID, integrationBranch string, fresh bool) (string, error) {
	if fresh {
		return gitWrapper.CreateWorktreeFresh(taskID, integrationBranch)
	}
	return gitWrapper.CreateWorktree(taskID, integrationBranch)
}

// recordWorktreeBaseCommit persists the task's base_commit in one transaction
// that runs no Git. On a write failure the just-created worktree is removed.
func recordWorktreeBaseCommit(bb *db.Blackboard, gitWrapper *git.Git, taskID, baseCommit string) error {
	err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: notFoundEntityTask, ID: taskID}
		}
		task.BaseCommit = &baseCommit
		return nil
	})
	if err != nil {
		_ = gitWrapper.RemoveWorktree(taskID)
		return fmt.Errorf("failed to update state: %w", err)
	}
	return nil
}

// ProvisionClaudeConfig copies Claude Code configuration files from the project
// root into a worktree so that agents launched there have correct settings.
// Files that don't exist in the project root are silently skipped.
// Returns warnings for any copy failures (non-fatal).
func ProvisionClaudeConfig(projectRoot, worktreeDir string) []string {
	var warnings []string

	// Individual files to copy (relative to project root).
	files := []string{
		filepath.Join(claudeConfigDirName, claudeSettingsFileName),
		filepath.Join(claudeConfigDirName, claudeLocalSettingsName),
	}

	for _, rel := range files {
		src := filepath.Join(projectRoot, rel)
		dst := filepath.Join(worktreeDir, rel)
		if err := copyFilePreserveMode(src, dst); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			warnings = append(warnings, fmt.Sprintf(provisionClaudeConfigWarn+": %s: %v", rel, err))
		}
	}

	// Copy hook scripts (may have execute bits).
	hooksDir := filepath.Join(projectRoot, claudeConfigDirName, claudeHooksDirName)
	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		// No hooks directory — nothing to copy.
		return warnings
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		rel := filepath.Join(claudeConfigDirName, claudeHooksDirName, entry.Name())
		src := filepath.Join(projectRoot, rel)
		dst := filepath.Join(worktreeDir, rel)
		if err := copyFilePreserveMode(src, dst); err != nil {
			warnings = append(warnings, fmt.Sprintf(provisionClaudeConfigWarn+": %s: %v", rel, err))
		}
	}

	return warnings
}

// ProvisionWorktreeEnvFiles copies explicitly authorized, ignored root env files
// from the project root into a task worktree. It never walks subdirectories and
// never follows symlinks; only root-level regular files matching the candidate
// predicate are eligible. Destination ignore setup is verified before any copy.
//
// A file mentioning a withheld variable (a runtime input, ADR-0169, or a
// config.agent_env_deny name, ADR-0213) is not copied at all: runtime-input
// values reach commands only through the ledger, and withheld names never
// reach sessions.
func ProvisionWorktreeEnvFiles(projectRoot, worktreeDir string) []string {
	entries, err := os.ReadDir(projectRoot)
	if err != nil {
		return []string{fmt.Sprintf(copyWorktreeEnvFilesWarn+": read project root: %v", err)}
	}
	deny, err := worktreeEnvFileDenyNames(projectRoot)
	if err != nil {
		return []string{fmt.Sprintf(copyWorktreeEnvFilesWarn+": read runtime-input declarations: %v; no env file copied", err)}
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
			warnings = append(warnings, fmt.Sprintf(copyWorktreeEnvFilesWarn+": %s: inspect source: %v", rel, err))
			continue
		}
		if !info.Mode().IsRegular() {
			warnings = append(warnings, fmt.Sprintf(copyWorktreeEnvFilesWarn+": %s: source is not a regular file", rel))
			continue
		}

		if err := checkIgnored(projectRoot, rel); err != nil {
			warnings = append(warnings, fmt.Sprintf(copyWorktreeEnvFilesWarn+": %s: source is not ignored by git", rel))
			continue
		}
		// A user-owned core.excludesFile blocks the private exclude; a destination
		// the effective rules already ignore is as safe to copy into.
		if err := worktreeexclude.EnsurePrivateExclude(worktreeDir, rel); err != nil && checkIgnored(worktreeDir, rel) != nil {
			warnings = append(warnings, fmt.Sprintf(copyWorktreeEnvFilesWarn+": %s: configure worktree ignore: %v", rel, err))
			continue
		}
		if err := checkIgnored(worktreeDir, rel); err != nil {
			warnings = append(warnings, fmt.Sprintf(copyWorktreeEnvFilesWarn+": %s: destination is not ignored by git", rel))
			continue
		}

		dst := filepath.Join(worktreeDir, rel)
		if _, err := os.Lstat(dst); err == nil {
			// Preserve existing worktree-local env files; ignore was verified above.
			continue
		} else if !os.IsNotExist(err) {
			warnings = append(warnings, fmt.Sprintf(copyWorktreeEnvFilesWarn+": %s: inspect destination: %v", rel, err))
			continue
		}

		reserved, err := copyEnvFileUnlessReserved(src, dst, info.Mode().Perm(), deny)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf(copyWorktreeEnvFilesWarn+": %s: copy: %v", rel, err))
		} else if len(reserved) > 0 {
			warnings = append(warnings, fmt.Sprintf(copyWorktreeEnvFilesWarn+": %s: not copied: it mentions withheld variables %s (runtime inputs or config.agent_env_deny); move them out of the file", rel, strings.Join(reserved, ", ")))
		}
	}
	return warnings
}

func worktreeEnvFileDenyNames(projectRoot string) (map[string]bool, error) {
	statePath := paths.New(projectRoot).StatePath()
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		return nil, nil
	}
	state, err := db.For(statePath).Read()
	if err != nil {
		return nil, err
	}
	return models.ReservedEnvNames(state), nil
}

// copyEnvFileUnlessReserved copies src to a new dst unless the file mentions
// a denied name anywhere, as a whole word; then it copies nothing and returns
// the names found. Shell env files allow several assignments per line,
// continuation lines and multi-line quoted values, so dropping lines cannot
// reliably remove a value: a file mentioning a runtime-input name is refused
// whole (ADR-0169).
func copyEnvFileUnlessReserved(src, dst string, mode os.FileMode, deny map[string]bool) ([]string, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, word := range strings.FieldsFunc(string(data), func(r rune) bool {
		return r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if models.EnvNameIn(deny, word) && !slices.Contains(found, word) {
			found = append(found, word)
		}
	}
	if len(found) > 0 {
		sort.Strings(found)
		return found, nil
	}
	file, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return nil, err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return nil, writeErr
	}
	return nil, closeErr
}

func isWorktreeEnvFileCandidate(name string) bool {
	return name == worktreeEnvFileName ||
		strings.HasPrefix(name, worktreeEnvFilePrefix) ||
		strings.HasSuffix(name, worktreeEnvFileSuffix) ||
		name == worktreeEnvrcFileName
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
		warnings = append(warnings, fmt.Sprintf(installPreCommitHookWarn+": enable worktreeConfig: %v", err))
		return warnings
	}

	hooksDir := filepath.Join(worktreeDir, worktreeHooksDirName())
	if err := os.MkdirAll(hooksDir, worktreeDirMode); err != nil {
		warnings = append(warnings, fmt.Sprintf(installPreCommitHookWarn+": mkdir: %v", err))
		return warnings
	}

	hookPath := filepath.Join(hooksDir, preCommitHookFileName)
	content := embedded.RenderWorktreePreCommitHook(lizaBin, taskID)
	if err := os.WriteFile(hookPath, content, worktreeHookFileMode); err != nil {
		warnings = append(warnings, fmt.Sprintf(installPreCommitHookWarn+": write: %v", err))
		return warnings
	}

	hooksAbs, err := filepath.Abs(hooksDir)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf(installPreCommitHookWarn+": resolve abs: %v", err))
		return warnings
	}

	if err := gitWrapper.SetWorktreeHooksPath(worktreeDir, hooksAbs); err != nil {
		warnings = append(warnings, fmt.Sprintf(installPreCommitHookWarn+": set hooksPath: %v", err))
		return warnings
	}

	// Verify the config actually took. If extensions.worktreeConfig is not
	// honored (old git, odd repo state), git writes the --worktree value to
	// a config file it never reads, producing a ghost hook. Surface that as
	// a warning so silent misconfiguration is visible at create time.
	got, err := gitWrapper.GetWorktreeHooksPath(worktreeDir)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf(installPreCommitHookWarn+": verify hooksPath: %v", err))
		return warnings
	}
	if got != hooksAbs {
		warnings = append(warnings, fmt.Sprintf(
			installPreCommitHookWarn+": hooksPath verification mismatch (got %q, want %q) — hook may not fire",
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

	if err := os.MkdirAll(filepath.Dir(dst), worktreeDirMode); err != nil {
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
