package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestAttemptFinalizationTimeoutPreservesReviewedGitEvidence(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(10 * time.Millisecond))
	root, statePath := setupTransitionTest(t)
	testhelpers.CreateTestWorktree(t, root, "task-1")
	wtPath := filepath.Join(root, ".worktrees", "task-1")
	if err := os.WriteFile(filepath.Join(wtPath, "candidate.txt"), []byte("reviewed work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "candidate.txt")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "reviewed candidate")
	g := git.New(root)
	commit, err := g.GetWorktreeHEAD("task-1")
	if err != nil {
		t.Fatal(err)
	}
	base, err := g.GetCommitSHA("integration")
	if err != nil {
		t.Fatal(err)
	}
	bb := db.For(statePath)
	if err := bb.Modify(func(s *models.State) error {
		task := s.FindTask("task-1")
		wt := ".worktrees/task-1"
		task.Worktree, task.BaseCommit = &wt, &base
		// The shared rollover fixture intentionally carries stale metadata.
		// Model a real rejected candidate and keep its reviewed SHA in history.
		clearAttemptState(task, attemptStateReviewRejection)
		task.History = append(task.History, models.TaskHistoryEntry{Event: models.TaskEventRejected, Commit: &commit})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var unlock func()
	previous := testTransitionHooks
	testTransitionHooks = &transitionTestHooks{afterPhase1: func() { unlock = holdPendingVerdictState(t, bb) }}
	t.Cleanup(func() { testTransitionHooks = previous })
	_, err = TransitionToNewAttempt(root, "task-1", "review cycle limit")
	if !filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		t.Fatalf("expected final-write timeout, got %v", err)
	}
	unlock()
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatal("failed finalization removed reviewed worktree")
	}
	exists, err := g.BranchExists("task/task-1")
	if err != nil || !exists {
		t.Fatal("failed finalization removed reviewed branch")
	}
	if head, err := g.GetWorktreeHEAD("task-1"); err != nil || head != commit {
		t.Fatal("reviewed commit boundary was lost")
	}
	state, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	task := state.FindTask("task-1")
	if task.AssignedTo == nil || *task.AssignedTo != transitioning || task.Worktree == nil {
		t.Fatal("interrupted finalization lost its recoverable marker/substrate")
	}
	// Exercise the documented preserve recovery, rather than just assuming a
	// sentinel with an intact branch is recoverable.
	recovered, err := RecoverTaskWithOptions(root, "task-1", "interrupted attempt finalization", RecoverTaskOptions{})
	if err != nil {
		t.Fatalf("preserve recovery after finalization timeout:\n%s", strings.ReplaceAll(err.Error(), ": ", "\n"))
	}
	if !recovered.PreservedWorktree || !recovered.ClaimReleased || recovered.WorktreeRemoved || recovered.BranchRemoved {
		t.Fatal("sentinel recovery did not preserve reviewed Git evidence")
	}
	state, err = bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	task = state.FindTask("task-1")
	if task.AssignedTo != nil || task.Worktree == nil || task.Attempt != 2 {
		t.Fatal("preserve recovery did not clear the interrupted sentinel")
	}
	if head, err := g.GetWorktreeHEAD("task-1"); err != nil || head != commit {
		t.Fatal("sentinel recovery changed the reviewed commit")
	}
}
