package ops

import (
	"log"

	"github.com/liza-mas/liza/internal/paths"
)

// cleanupFailedClaim removes the worktree and branch this invocation created
// when the assignment failed. It never runs for resources the claim found
// already present, and it leaves a worktree alone when another claimer won.
// Cleanup errors are logged as warnings; the returned claim error conveys the
// failure. Material inconsistency (orphaned worktree/branch) is flagged for
// operator attention.
func (op *claimOperation) cleanupFailedClaim(worktreeCreated bool) {
	if !worktreeCreated {
		return
	}
	// Guard: if another claimer won (task status changed), the worktree
	// belongs to them — don't remove it. This prevents a race where a
	// losing claimer's cleanup deletes the worktree the winner depends on.
	readState, readErr := op.bb.Read()
	if readErr != nil {
		log.Printf("WARNING: claim-task %s: cannot verify winner before cleanup: %v", op.taskID, readErr)
	} else if currentTask := readState.FindTask(op.taskID); currentTask != nil && currentTask.Status != op.taskStatus {
		log.Printf("claim-task %s: skipping worktree cleanup — another claimer won (status: %s)", op.taskID, currentTask.Status)
		return
	}
	if cleanupErr := op.git.RemoveWorktree(op.taskID); cleanupErr != nil {
		log.Printf("WARNING: claim-task %s: failed to cleanup worktree after claim failure: %v", op.taskID, cleanupErr)
	}
	if cleanupErr := op.git.DeleteBranch(paths.TaskBranchPrefix + op.taskID); cleanupErr != nil {
		log.Printf("WARNING: claim-task %s: failed to cleanup branch after claim failure: %v", op.taskID, cleanupErr)
	}
}
