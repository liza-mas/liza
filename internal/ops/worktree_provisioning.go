package ops

import (
	"time"

	"github.com/liza-mas/liza/internal/git"
)

// worktreeProvisioning is the input of the provisioning sequence shared by the
// reuse and fresh paths of createWorktree.
type worktreeProvisioning struct {
	projectRoot         string
	worktreeDir         string
	taskID              string
	gitWrapper          *git.Git
	copyEnvFiles        bool
	postCmd             *string
	postCmdTimeout      time.Duration
	scipSearchLanguages []string
}

// provisionWorktree runs the worktree setup steps in their fixed order and
// returns their warnings in that order: Claude Code config, per-worktree
// pre-commit hook, env files (when authorized), the post-worktree command,
// the Semble ignore payload, then the SCIP, Stacklit and functional-clusters
// index refreshes. The post-worktree command fails closed: an unprepared
// worktree must not be reported as ready, so its error ends the sequence and
// no warning is returned. The steps are idempotent, which lets the reuse path
// catch prior failures and upgrades.
func provisionWorktree(p worktreeProvisioning) ([]string, error) {
	var warnings []string
	warnings = append(warnings, ProvisionClaudeConfig(p.projectRoot, p.worktreeDir)...)
	// Non-fatal: the submit-verdict HEAD check remains the second line of
	// defense against post-submission mutations.
	warnings = append(warnings, InstallWorktreePreCommitHook(p.gitWrapper, p.worktreeDir, p.taskID)...)
	if p.copyEnvFiles {
		warnings = append(warnings, ProvisionWorktreeEnvFiles(p.projectRoot, p.worktreeDir)...)
	}
	if p.postCmd != nil {
		if err := RunPostWorktreeCmd(p.projectRoot, *p.postCmd, p.worktreeDir, p.postCmdTimeout, nil); err != nil {
			return nil, err
		}
	}
	warnings = append(warnings, PrepareSembleWorktreeIgnore(p.worktreeDir)...)
	warnings = append(warnings, refreshTaskWorktreeScipIndexes(p.projectRoot, p.worktreeDir, p.scipSearchLanguages)...)
	warnings = append(warnings, refreshTaskWorktreeStacklitIndex(p.projectRoot, p.worktreeDir)...)
	warnings = append(warnings, refreshTaskWorktreeFunctionalClustersIndex(p.projectRoot, p.worktreeDir, p.scipSearchLanguages)...)
	return warnings, nil
}
