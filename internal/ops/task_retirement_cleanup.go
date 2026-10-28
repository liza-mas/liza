package ops

import (
	"fmt"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/paths"
)

// deleteOwnTaskBranch deletes a retired task's own branch when it exists. The
// retirement is already committed, so a failure is a warning, not an error.
func deleteOwnTaskBranch(gw *git.Git, taskID string) []string {
	branchName := paths.TaskBranchPrefix + taskID
	exists, err := gw.BranchExists(branchName)
	if err != nil {
		return []string{fmt.Sprintf("failed to check branch %s: %v", branchName, err)}
	}
	if !exists {
		return nil
	}
	if err := gw.DeleteBranch(branchName); err != nil {
		return []string{fmt.Sprintf("failed to delete branch %s: %v", branchName, err)}
	}
	return nil
}
