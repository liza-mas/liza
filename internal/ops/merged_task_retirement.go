package ops

import (
	"fmt"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/paths"
)

// retireMergedTask removes the worktree directory and branch of a task whose
// MERGED state is already committed, then the predecessor branches it was the
// last active successor of. Shared by the merge and by external-merge
// reconciliation. Every failure is non-fatal and returned as a warning, in the
// order the steps run. Sprint metrics stay with the callers: their authority
// differs.
func retireMergedTask(bb *db.Blackboard, gw *git.Git, taskID string) []string {
	var warnings []string
	if err := gw.RemoveWorktreeDir(taskID); err != nil {
		warnings = append(warnings, fmt.Sprintf("failed to remove worktree directory: %v", err))
	}
	taskBranch := paths.TaskBranchPrefix + taskID
	if exists, err := gw.BranchExists(taskBranch); err != nil {
		warnings = append(warnings, fmt.Sprintf("failed to check branch %s: %v", taskBranch, err))
	} else if exists {
		if err := gw.DeleteBranch(taskBranch); err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to delete branch %s: %v", taskBranch, err))
		}
	}
	// Check if this merged task was the last active successor of a superseded
	// predecessor — if so, clean up the predecessor's branch.
	return append(warnings, cleanupPredecessorBranches(bb, gw, taskID)...)
}
