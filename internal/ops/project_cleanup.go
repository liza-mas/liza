package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/liza-mas/liza/internal/db"
	gitops "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/paths"
)

// projectCleanupLockOperation labels the exclusive lock a cleanup holds.
const projectCleanupLockOperation = "project-cleanup"

// ProjectCleanupWorktree is a registered task worktree owned by the project.
type ProjectCleanupWorktree struct {
	TaskID string
	Path   string
	Branch string
}

// ProjectCleanupPlan describes the destructive targets for a project cleanup.
// Planning is read-only so callers can show the exact targets before approval.
type ProjectCleanupPlan struct {
	ProjectRoot string
	Directories []string
	Worktrees   []ProjectCleanupWorktree
	LiveAgents  []string
}

// Empty reports whether cleanup has any filesystem or Git targets.
func (p ProjectCleanupPlan) Empty() bool {
	return len(p.Directories) == 0 && len(p.Worktrees) == 0
}

// PlanProjectCleanup discovers cleanup targets without modifying project state.
func PlanProjectCleanup(projectRoot string) (ProjectCleanupPlan, error) {
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return ProjectCleanupPlan{}, fmt.Errorf("resolve project root: %w", err)
	}
	root = filepath.Clean(root)
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return ProjectCleanupPlan{}, fmt.Errorf("resolve project root symlinks: %w", err)
	}
	lp := paths.New(root)
	worktreesDir := filepath.Join(root, paths.WorktreesDirName)
	plan := ProjectCleanupPlan{ProjectRoot: root}

	for _, dir := range []string{lp.LizaDir(), worktreesDir} {
		info, statErr := os.Lstat(dir)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return ProjectCleanupPlan{}, fmt.Errorf("inspect cleanup path %s: %w", dir, statErr)
		}
		if !info.IsDir() {
			return ProjectCleanupPlan{}, fmt.Errorf("cleanup path exists and is not a directory: %s", dir)
		}
		plan.Directories = append(plan.Directories, dir)
	}

	gitClient := gitops.New(root)
	registered, err := gitClient.ListWorktrees()
	if err != nil {
		return ProjectCleanupPlan{}, fmt.Errorf("inspect registered worktrees: %w", err)
	}
	for _, worktree := range registered {
		worktreePath, absErr := filepath.Abs(worktree.Path)
		if absErr != nil {
			return ProjectCleanupPlan{}, fmt.Errorf("resolve registered worktree %s: %w", worktree.Path, absErr)
		}
		worktreePath = filepath.Clean(worktreePath)
		resolvedWorktreePath, resolveErr := filepath.EvalSymlinks(worktreePath)
		if resolveErr == nil {
			worktreePath = filepath.Clean(resolvedWorktreePath)
		} else if !os.IsNotExist(resolveErr) {
			return ProjectCleanupPlan{}, fmt.Errorf("resolve registered worktree symlinks %s: %w", worktree.Path, resolveErr)
		}
		rel, relErr := filepath.Rel(worktreesDir, worktreePath)
		if relErr != nil {
			return ProjectCleanupPlan{}, fmt.Errorf("classify registered worktree %s: %w", worktree.Path, relErr)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		if rel == "." || filepath.Dir(rel) != "." {
			return ProjectCleanupPlan{}, fmt.Errorf("registered worktree under %s is not an owned task worktree: %s; relocate or unregister the worktree before retrying", worktreesDir, worktree.Path)
		}

		taskID := filepath.Base(worktreePath)
		if err := paths.ValidateTaskID(taskID); err != nil {
			return ProjectCleanupPlan{}, fmt.Errorf("registered worktree under %s has an invalid task directory %q: %w; relocate or unregister the worktree before retrying", worktreesDir, taskID, err)
		}
		expectedBranch := paths.TaskBranchPrefix + taskID
		if worktree.Branch != expectedBranch {
			return ProjectCleanupPlan{}, fmt.Errorf("registered worktree %s is not owned by task branch %s (found %q); relocate or unregister the worktree before retrying", worktree.Path, expectedBranch, worktree.Branch)
		}
		plan.Worktrees = append(plan.Worktrees, ProjectCleanupWorktree{
			TaskID: taskID,
			Path:   worktreePath,
			Branch: expectedBranch,
		})
	}
	sort.Slice(plan.Worktrees, func(i, j int) bool {
		return plan.Worktrees[i].Path < plan.Worktrees[j].Path
	})

	if _, statErr := os.Stat(lp.StatePath()); statErr == nil {
		state, readErr := db.For(lp.StatePath()).Read()
		if readErr != nil {
			return ProjectCleanupPlan{}, fmt.Errorf("read project state before cleanup: %w", readErr)
		}
		for agentID, agent := range state.Agents {
			status := AgentProcessStatus(agentID, agent)
			if status.IsLiveOrUnknown() {
				plan.LiveAgents = append(plan.LiveAgents, fmt.Sprintf("%s (pid %d)", agentID, agent.PID))
			}
		}
		sort.Strings(plan.LiveAgents)
	} else if !os.IsNotExist(statErr) {
		return ProjectCleanupPlan{}, fmt.Errorf("inspect project state before cleanup: %w", statErr)
	}

	return plan, nil
}

// ExecuteProjectCleanup revalidates an approved plan and removes its targets.
func ExecuteProjectCleanup(approved ProjectCleanupPlan) error {
	return executeProjectCleanup(approved, nil)
}

func executeProjectCleanup(approved ProjectCleanupPlan, afterRevalidation func()) error {
	return WithProjectLifecycleExclusiveLock(approved.ProjectRoot, projectCleanupLockOperation, func() error {
		current, err := PlanProjectCleanup(approved.ProjectRoot)
		if err != nil {
			return err
		}
		if len(current.LiveAgents) > 0 {
			return fmt.Errorf("cannot clean project while agents are still running: %s", strings.Join(current.LiveAgents, ", "))
		}
		if !sameProjectCleanupTargets(approved, current) {
			return fmt.Errorf("cleanup targets changed after confirmation; review the new targets and retry")
		}
		if afterRevalidation != nil {
			afterRevalidation()
		}

		gitClient := gitops.New(current.ProjectRoot)
		for _, worktree := range current.Worktrees {
			if err := gitClient.RemoveWorktree(worktree.TaskID); err != nil {
				return fmt.Errorf("remove task worktree %s and branch %s: %w", worktree.Path, worktree.Branch, err)
			}
		}
		for _, dir := range current.Directories {
			if err := os.RemoveAll(dir); err != nil {
				return fmt.Errorf("remove cleanup directory %s: %w", dir, err)
			}
		}
		return nil
	})
}

func sameProjectCleanupTargets(left, right ProjectCleanupPlan) bool {
	return left.ProjectRoot == right.ProjectRoot &&
		reflect.DeepEqual(left.Directories, right.Directories) &&
		reflect.DeepEqual(left.Worktrees, right.Worktrees)
}
