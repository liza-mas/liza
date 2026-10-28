//go:build linux

package artifact

import (
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleA2WorktreeExclude(t *testing.T) {
	e := isolateEnv(t, true)
	repo := initRepo(t, e)
	gitRun(t, e, repo, "commit", "-q", "--allow-empty", "-m", "fixture repository")
	linked := filepath.Join(e.work, "linked")
	gitRun(t, e, repo, "worktree", "add", "-q", "--detach", linked)
	for range 2 {
		if err := writeRepoExclude(linked); err != nil {
			t.Fatal("write repository exclude")
		}
		if err := writePrivateExclude(linked); err != nil {
			t.Fatal("write linked-worktree private exclude")
		}
	}
	oracle.Check(t, oracle.Artifact, "worktree-exclude/repo-exclude", readFile(t, repo, ".git/info/exclude"))
	oracle.Check(t, oracle.Artifact, "worktree-exclude/private-exclude", readFile(t, repo, ".git/worktrees/linked/info/exclude"))
}
