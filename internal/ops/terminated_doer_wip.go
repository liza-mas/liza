package ops

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

const terminatedDoerWIPOperation = "terminated-doer-wip"

// TerminatedDoerWIPResult reports whether CommitTerminatedDoerWIP recorded a
// WIP commit and, when it did not, why.
type TerminatedDoerWIPResult struct {
	Committed  bool
	SHA        string
	SkipReason string
}

// CommitTerminatedDoerWIP commits the uncommitted work of a doer session the
// provider terminated, before the supervisor releases the claim. Released with
// a dirty worktree, the task would block on its next claim.
//
// Locks are taken in recover-agent's order: project lifecycle (shared), this
// agent's lifecycle lock, then the task's claim-worktree lock. The first
// fences registration and recover-agent, so the generation checked below
// cannot change before the commit; the last is held by every claim, release,
// recovery, unblock, cancel, supersede, replace and submission of this task,
// so none of them can interleave. Heartbeat and lease renewal take neither but
// change no field checked here.
//
// Every refusal leaves the worktree, its index and HEAD untouched and is
// returned as a SkipReason: the dirty worktree then blocks on its next claim,
// as before, for a human to repair.
func CommitTerminatedDoerWIP(ctx context.Context, projectRoot string, authority models.AgentAuthority, taskID, message string) (*TerminatedDoerWIPResult, error) {
	if err := paths.ValidateTaskID(taskID); err != nil {
		return nil, err
	}
	var result *TerminatedDoerWIPResult
	err := WithProjectLifecycleSharedLock(projectRoot, terminatedDoerWIPOperation, func() error {
		return WithAgentLifecycleLock(ctx, projectRoot, authority.ID, terminatedDoerWIPOperation, func() error {
			lock := filelock.New(claimTaskWorktreeLockPath(paths.New(projectRoot).StatePath(), taskID))
			return lock.WithLockOperation(terminatedDoerWIPOperation, func() error {
				var inner error
				result, inner = commitTerminatedDoerWIPLocked(projectRoot, authority, taskID, message)
				return inner
			})
		})
	})
	return result, err
}

func commitTerminatedDoerWIPLocked(projectRoot string, authority models.AgentAuthority, taskID, message string) (*TerminatedDoerWIPResult, error) {
	skip := func(format string, args ...any) (*TerminatedDoerWIPResult, error) {
		return &TerminatedDoerWIPResult{SkipReason: fmt.Sprintf(format, args...)}, nil
	}

	state, err := db.For(paths.New(projectRoot).StatePath()).Read()
	if err != nil {
		return nil, err
	}
	if err := RequireAgentAuthority(state, authority); err != nil {
		return skip("%v", err)
	}
	task := state.FindTask(taskID)
	if task == nil {
		return skip("task %s not found", taskID)
	}
	if task.AssignedTo == nil || *task.AssignedTo != authority.ID {
		return skip("task %s is not assigned to %s", taskID, authority.ID)
	}
	resolver, err := LoadResolverForModels(projectRoot)
	if err != nil {
		return nil, err
	}
	if !models.IsExecutingStatus(task, resolver) {
		return skip("task %s status %s is not its executing status", taskID, task.Status)
	}
	if task.Worktree == nil || *task.Worktree == "" {
		return skip("task %s has no worktree", taskID)
	}

	gitWrapper := git.New(projectRoot)
	worktreeDir := gitWrapper.GetWorktreePath(taskID)
	if recorded := filepath.Join(projectRoot, *task.Worktree); filepath.Clean(recorded) != filepath.Clean(worktreeDir) {
		return skip("task %s worktree %s is not its task worktree %s", taskID, recorded, worktreeDir)
	}
	if err := gitWrapper.ValidateWorktreeHealth(taskID); err != nil {
		return skip("task %s worktree not healthy: %v", taskID, err)
	}
	branch, err := gitWrapper.GetWorktreeBranch(worktreeDir)
	if err != nil {
		return nil, err
	}
	if want := paths.TaskBranchPrefix + taskID; branch != want {
		return skip("task %s worktree is on branch %q, want %q", taskID, branch, want)
	}
	operation, err := gitWrapper.InterruptedOperation(worktreeDir)
	if err != nil {
		return nil, err
	}
	if operation != "" {
		return skip("task %s worktree is mid-%s", taskID, operation)
	}

	sha, committed, err := gitWrapper.CommitAllWIP(worktreeDir, message)
	if err != nil {
		return nil, err
	}
	if !committed {
		return skip("task %s worktree has nothing to commit", taskID)
	}
	return &TerminatedDoerWIPResult{Committed: true, SHA: sha}, nil
}
