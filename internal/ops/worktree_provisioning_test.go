package ops

import (
	stderrors "errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/stacklit"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func newProvisioningFixture(t *testing.T, postCmd string) worktreeProvisioning {
	t.Helper()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	requireFeatureGateUnset(t, stacklit.EnvEnableStacklit)
	gitWrapper := git.New(tmpDir)
	if _, err := gitWrapper.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("CreateWorktree() setup error: %v", err)
	}
	return worktreeProvisioning{
		projectRoot:    tmpDir,
		worktreeDir:    filepath.Join(tmpDir, ".worktrees", "task-1"),
		taskID:         "task-1",
		gitWrapper:     gitWrapper,
		postCmd:        &postCmd,
		postCmdTimeout: 30 * time.Second,
	}
}

// The post-worktree command runs after the hook install and before the Semble
// ignore payload, on the reuse and fresh paths alike (one shared sequence).
func TestProvisionWorktreeRunsStepsInOrder(t *testing.T) {
	t.Parallel()
	hook := filepath.Join(worktreeHooksDirName(), "pre-commit")
	p := newProvisioningFixture(t, "test -f '"+hook+"' && test ! -e "+sembleWorktreeIgnoreFile+" && echo ordered > order.txt")

	if _, err := provisionWorktree(p); err != nil {
		t.Fatalf("provisionWorktree() error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.worktreeDir, "order.txt")); err != nil {
		t.Fatal("post-worktree command did not run after the hook install and before the Semble ignore payload")
	}
	if _, err := os.Stat(filepath.Join(p.worktreeDir, sembleWorktreeIgnoreFile)); err != nil {
		t.Fatalf("Semble ignore payload missing after the post-worktree command: %v", err)
	}
}

// A failed post-worktree command ends the sequence: no later step runs and no
// warning is returned.
func TestProvisionWorktreePostCmdFailureEndsSequence(t *testing.T) {
	t.Parallel()
	p := newProvisioningFixture(t, "exit 3")

	warnings, err := provisionWorktree(p)
	var setupErr *PostWorktreeSetupError
	if !stderrors.As(err, &setupErr) {
		t.Fatalf("provisionWorktree() error = %v, want *PostWorktreeSetupError", err)
	}
	if warnings != nil {
		t.Errorf("warnings = %v, want nil on failure", warnings)
	}
	if _, err := os.Stat(filepath.Join(p.worktreeDir, sembleWorktreeIgnoreFile)); !os.IsNotExist(err) {
		t.Fatal("Semble ignore payload was written after a failed post-worktree command")
	}
}
