package paths

// WorktreeSetupDir returns the branded private runtime directory holding, per
// worktree, the last failed post_worktree_cmd output tail and the latest setup
// outcome record.
func (p LizaPaths) WorktreeSetupDir() string {
	return p.get("worktree-setup")
}
