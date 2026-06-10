package git

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/paths"
)

const maxProgressSignatureFileBytes = 1 << 20

// CreateWorktree creates a worktree for the given task from the specified branch.
// Returns the base commit (full SHA) for drift tracking.
func (g *Git) CreateWorktree(taskID, fromBranch string) (string, error) {
	if err := paths.ValidateTaskID(taskID); err != nil {
		return "", fmt.Errorf("invalid task ID: %w", err)
	}
	worktreeRel := filepath.Join(paths.WorktreesDirName, taskID)
	worktreePath := filepath.Join(g.projectRoot, worktreeRel)
	branchName := paths.TaskBranchPrefix + taskID

	// Check if worktree already exists
	if _, err := os.Stat(worktreePath); err == nil {
		return "", fmt.Errorf("worktree already exists: %s", worktreePath)
	}

	// Ensure .worktrees directory exists
	worktreesDir := filepath.Join(g.projectRoot, paths.WorktreesDirName)
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create .worktrees directory: %w", err)
	}

	// Get base commit before creating worktree
	baseCommit, err := g.GetCommitSHA(fromBranch)
	if err != nil {
		return "", fmt.Errorf("failed to get base commit: %w", err)
	}

	// Create worktree with new branch
	_, err = g.exec("worktree", "add", worktreePath, fromBranch, "-b", branchName)
	if err != nil {
		return "", fmt.Errorf("failed to create worktree: %w", err)
	}

	return baseCommit, nil
}

// AttachWorktree creates a worktree for an already-existing branch.
// Unlike CreateWorktree, this does not create a new branch with -b.
func (g *Git) AttachWorktree(taskID, existingBranch string) error {
	if err := paths.ValidateTaskID(taskID); err != nil {
		return fmt.Errorf("invalid task ID: %w", err)
	}
	worktreePath := filepath.Join(g.projectRoot, paths.WorktreesDirName, taskID)

	if _, err := os.Stat(worktreePath); err == nil {
		return fmt.Errorf("worktree already exists: %s", worktreePath)
	}

	worktreesDir := filepath.Join(g.projectRoot, paths.WorktreesDirName)
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		return fmt.Errorf("failed to create .worktrees directory: %w", err)
	}

	_, err := g.exec("worktree", "add", worktreePath, existingBranch)
	if err != nil {
		return fmt.Errorf("failed to attach worktree: %w", err)
	}

	return nil
}

// CreateWorktreeFresh creates a worktree, deleting any existing one first
// This is used for task reassignment scenarios
func (g *Git) CreateWorktreeFresh(taskID, fromBranch string) (string, error) {
	// Remove existing worktree if it exists (ignore errors)
	_ = g.RemoveWorktree(taskID)

	// Create new worktree
	return g.CreateWorktree(taskID, fromBranch)
}

// RemoveWorktreeDir removes a worktree directory and git metadata without
// deleting the associated branch. Use this when the branch must survive
// (e.g. superseded tasks whose branch successors still need).
func (g *Git) RemoveWorktreeDir(taskID string) error {
	if err := paths.ValidateTaskID(taskID); err != nil {
		return fmt.Errorf("invalid task ID: %w", err)
	}
	worktreePath := filepath.Join(g.projectRoot, paths.WorktreesDirName, taskID)

	metadataDir := filepath.Join(g.projectRoot, ".git", "worktrees", taskID)

	// If the directory is already gone, Git may still have a stale worktree
	// registration that keeps the task branch "checked out". Clear only this
	// task's metadata; global prune can interfere with concurrent worktree adds.
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		_ = os.RemoveAll(metadataDir)
		return nil
	}

	// Remove worktree (force to handle any uncommitted changes)
	_, err := g.exec("worktree", "remove", "--force", worktreePath)
	if err != nil {
		// If git worktree remove fails, try manual cleanup
		if err := os.RemoveAll(worktreePath); err != nil {
			return fmt.Errorf("failed to remove worktree directory: %w", err)
		}
	}
	_ = os.RemoveAll(metadataDir)

	return nil
}

// RemoveWorktree removes a worktree and its associated branch.
func (g *Git) RemoveWorktree(taskID string) error {
	if err := g.RemoveWorktreeDir(taskID); err != nil {
		return err
	}

	branchName := paths.TaskBranchPrefix + taskID
	exists, err := g.BranchExists(branchName)
	if err != nil {
		return err
	}
	if exists {
		return g.DeleteBranch(branchName)
	}
	return nil
}

// ListWorktrees returns a list of all worktrees
func (g *Git) ListWorktrees() ([]Worktree, error) {
	// git worktree list --porcelain gives us machine-readable output
	output, err := g.exec("worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}

	var worktrees []Worktree
	var current Worktree

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			// Empty line separates worktree entries
			if current.Path != "" {
				worktrees = append(worktrees, current)
				current = Worktree{}
			}
			continue
		}

		if strings.HasPrefix(line, "worktree ") {
			current.Path = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "HEAD ") {
			current.Commit = strings.TrimPrefix(line, "HEAD ")
		} else if strings.HasPrefix(line, "branch ") {
			branchRef := strings.TrimPrefix(line, "branch ")
			// Extract branch name from refs/heads/...
			current.Branch = strings.TrimPrefix(branchRef, "refs/heads/")
		}
	}

	// Add the last entry if present
	if current.Path != "" {
		worktrees = append(worktrees, current)
	}

	return worktrees, nil
}

// GetWorktreePath returns the absolute path for a task's worktree
// Callers must validate taskID before calling this — see paths.ValidateTaskID.
func (g *Git) GetWorktreePath(taskID string) string {
	return filepath.Join(g.projectRoot, paths.WorktreesDirName, taskID)
}

// GetWorktreeRelPath returns the relative path for a task's worktree
func (g *Git) GetWorktreeRelPath(taskID string) string {
	return filepath.Join(paths.WorktreesDirName, taskID)
}

// WorktreeProgressSignature returns a compact signature for meaningful changes
// inside a task worktree: HEAD movement, porcelain status changes, and dirty
// content changes for tracked and untracked files.
func (g *Git) WorktreeProgressSignature(taskID string) (string, error) {
	if err := paths.ValidateTaskID(taskID); err != nil {
		return "", fmt.Errorf("invalid task ID: %w", err)
	}
	worktreePath := g.GetWorktreePath(taskID)
	head, err := g.execInDir(worktreePath, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	status, err := g.execInDir(worktreePath, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	contentSig, err := g.worktreeDirtyContentSignature(worktreePath)
	if err != nil {
		return "", err
	}
	return head + "\n" + status + "\ncontent:" + contentSig, nil
}

func (g *Git) worktreeDirtyContentSignature(worktreePath string) (string, error) {
	h := sha256.New()

	tracked, err := gitenv.Output(worktreePath, "diff", "--name-only", "-z", "HEAD", "--")
	if err != nil {
		return "", fmt.Errorf("git diff failed: %w\nOutput: %s", err, tracked)
	}
	h.Write([]byte("tracked\x00"))
	h.Write(tracked)
	for _, relPath := range nulRecords(tracked) {
		if err := writeWorktreeFileSignature(h, worktreePath, relPath); err != nil {
			return "", err
		}
	}

	untracked, err := gitenv.Output(worktreePath, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return "", fmt.Errorf("git ls-files failed: %w\nOutput: %s", err, untracked)
	}
	h.Write([]byte("\x00untracked\x00"))
	h.Write(untracked)
	for _, relPath := range nulRecords(untracked) {
		if err := writeWorktreeFileSignature(h, worktreePath, relPath); err != nil {
			return "", err
		}
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func nulRecords(output []byte) []string {
	if len(output) == 0 {
		return nil
	}
	records := strings.Split(strings.TrimSuffix(string(output), "\x00"), "\x00")
	nonEmpty := records[:0]
	for _, record := range records {
		if record != "" {
			nonEmpty = append(nonEmpty, record)
		}
	}
	return nonEmpty
}

func writeWorktreeFileSignature(w io.Writer, worktreePath, relPath string) error {
	fullPath := filepath.Join(worktreePath, filepath.FromSlash(relPath))
	info, statErr := os.Lstat(fullPath)
	fmt.Fprintf(w, "%s\x00", relPath)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			fmt.Fprint(w, "missing\x00")
			return nil
		}
		return fmt.Errorf("stat worktree file %s: %w", relPath, statErr)
	}

	fmt.Fprintf(w, "mode=%s\x00size=%d\x00", info.Mode().String(), info.Size())
	if info.Mode()&os.ModeSymlink != 0 {
		target, readlinkErr := os.Readlink(fullPath)
		if readlinkErr != nil {
			return fmt.Errorf("readlink worktree file %s: %w", relPath, readlinkErr)
		}
		fmt.Fprintf(w, "symlink\x00%s\x00", target)
		return nil
	}
	if !info.Mode().IsRegular() {
		fmt.Fprint(w, "nonregular\x00")
		return nil
	}

	file, openErr := os.Open(fullPath)
	if openErr != nil {
		return fmt.Errorf("open worktree file %s: %w", relPath, openErr)
	}
	defer file.Close()

	written, copyErr := io.Copy(w, io.LimitReader(file, maxProgressSignatureFileBytes))
	if copyErr != nil {
		return fmt.Errorf("hash worktree file %s: %w", relPath, copyErr)
	}
	fmt.Fprintf(w, "\x00sampled=%d\x00", written)
	if info.Size() > maxProgressSignatureFileBytes {
		fmt.Fprint(w, "truncated\x00")
	}
	return nil
}

// WorktreeStatusShort returns git status --short output for a worktree path.
func (g *Git) WorktreeStatusShort(worktreePath string) (string, error) {
	return g.execInDir(worktreePath, "status", "--short")
}

// WorktreeUntrackedFiles returns untracked, non-ignored paths relative to a worktree.
func (g *Git) WorktreeUntrackedFiles(worktreePath string) ([]string, error) {
	output, err := g.execInDir(worktreePath, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	if output == "" {
		return nil, nil
	}

	records := strings.Split(strings.TrimSuffix(output, "\x00"), "\x00")
	files := make([]string, 0, len(records))
	for _, record := range records {
		if record != "" {
			files = append(files, record)
		}
	}
	return files, nil
}

// UntrackedFilesOverwrittenBy reports untracked files that are present in targetTreeish.
func (g *Git) UntrackedFilesOverwrittenBy(worktreePath, targetTreeish string) ([]string, error) {
	untracked, err := g.WorktreeUntrackedFiles(worktreePath)
	if err != nil {
		return nil, err
	}

	var overwritten []string
	for _, path := range untracked {
		_, present, err := g.TreePathMode(targetTreeish, path)
		if err != nil {
			return nil, err
		}
		if present {
			overwritten = append(overwritten, path)
		}
	}
	return overwritten, nil
}

// EnableWorktreeConfigExtension ensures the main repo has
// extensions.worktreeConfig enabled so each worktree can override config
// values (notably core.hooksPath) without polluting the shared repo config.
// Idempotent: safe to call repeatedly.
func (g *Git) EnableWorktreeConfigExtension() error {
	_, err := g.exec("config", "extensions.worktreeConfig", "true")
	return err
}

// SetWorktreeHooksPath sets core.hooksPath in the per-worktree config for the
// worktree at worktreeDir, pointing git at hooksAbsPath. Requires
// extensions.worktreeConfig=true in the main repo.
func (g *Git) SetWorktreeHooksPath(worktreeDir, hooksAbsPath string) error {
	_, err := g.execInDir(worktreeDir, "config", "--worktree", "core.hooksPath", hooksAbsPath)
	return err
}

// GetWorktreeHooksPath returns the effective core.hooksPath git will use for
// the worktree at worktreeDir (i.e. the value honoring extensions.worktreeConfig
// merging). Used to verify that a prior SetWorktreeHooksPath actually took.
func (g *Git) GetWorktreeHooksPath(worktreeDir string) (string, error) {
	return g.execInDir(worktreeDir, "config", "--get", "core.hooksPath")
}

// ValidateWorktreeHealth checks that a worktree directory and its .git link
// file both exist and are accessible. A worktree without its .git file is an
// orphan that git cannot operate on — this can happen when concurrent
// RemoveWorktree operations interfere with in-flight worktree creation.
// Callers must validate taskID before calling this — see paths.ValidateTaskID.
func (g *Git) ValidateWorktreeHealth(taskID string) error {
	worktreePath := g.GetWorktreePath(taskID)

	if _, err := os.Stat(worktreePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("worktree directory missing: %s", worktreePath)
		}
		return fmt.Errorf("worktree directory inaccessible: %w", err)
	}

	gitFile := filepath.Join(worktreePath, ".git")
	if _, err := os.Stat(gitFile); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("worktree .git link file missing: %s (orphaned worktree)", gitFile)
		}
		return fmt.Errorf("worktree .git link file inaccessible: %w", err)
	}

	if _, err := g.execInDir(worktreePath, "rev-parse", "--verify", "HEAD^{commit}"); err != nil {
		return fmt.Errorf("worktree HEAD cannot resolve to a commit: %w", err)
	}

	return nil
}
