package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D59: a preserved continuation whose worktree still holds the previous
// owner's uncommitted work must be claimable by any doer. The claim adopts that
// work as one WIP commit on the task branch instead of blocking on it, unless
// Git is mid-operation there, where adopting would commit an unresolved state.

const (
	adoptedTrackedContent   = "dirty task work\n"
	adoptedUntrackedName    = "new-file.txt"
	adoptedUntrackedContent = "untracked work\n"
)

// leaveUncommittedWork dirties the preserved worktree with a tracked edit and a
// new untracked file, as a doer terminated mid-edit leaves it.
func leaveUncommittedWork(t *testing.T, fixture *preservedInitialClaimFixture) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(fixture.worktreeDir, "task.txt"), []byte(adoptedTrackedContent), 0o644); err != nil {
		t.Fatalf("dirty tracked file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fixture.worktreeDir, adoptedUntrackedName), []byte(adoptedUntrackedContent), 0o644); err != nil {
		t.Fatalf("add untracked file: %v", err)
	}
}

func claimedHistoryEntry(t *testing.T, task *models.Task) *models.TaskHistoryEntry {
	t.Helper()
	for i := len(task.History) - 1; i >= 0; i-- {
		if task.History[i].Event == models.TaskEventClaimed {
			return &task.History[i]
		}
	}
	t.Fatalf("no claimed history entry: %+v", task.History)
	return nil
}

// assertAdoptedWIP checks the uncommitted work became exactly one commit on top
// of the preserved head, the worktree is clean, and the claim records it.
func assertAdoptedWIP(t *testing.T, fixture *preservedInitialClaimFixture, claimant string) {
	t.Helper()
	assertGitStatusClean(t, fixture.worktreeDir)
	head := testhelpers.MustGit(t, fixture.worktreeDir, "rev-parse", "HEAD")
	if parent := testhelpers.MustGit(t, fixture.worktreeDir, "rev-parse", "HEAD^"); parent != fixture.preservedHead {
		t.Fatalf("adopted commit parent = %s, want the preserved head %s", parent, fixture.preservedHead)
	}
	if got := testhelpers.MustGit(t, fixture.worktreeDir, "show", "HEAD:task.txt"); got+"\n" != adoptedTrackedContent {
		t.Fatalf("adopted task.txt = %q, want %q", got, adoptedTrackedContent)
	}
	if got := testhelpers.MustGit(t, fixture.worktreeDir, "show", "HEAD:"+adoptedUntrackedName); got+"\n" != adoptedUntrackedContent {
		t.Fatalf("adopted %s = %q, want %q", adoptedUntrackedName, got, adoptedUntrackedContent)
	}

	state := readClaimStateForTest(t, fixture.stateFile)
	task := state.FindTask(fixture.taskID)
	if task.Status != models.TaskStatusImplementing || task.AssignedTo == nil || *task.AssignedTo != claimant {
		t.Fatalf("task = status %s assigned_to %v, want IMPLEMENTING for %s", task.Status, task.AssignedTo, claimant)
	}
	entry := claimedHistoryEntry(t, task)
	if got, _ := entry.Extra["adopted_wip_commit"].(string); got != head {
		t.Fatalf("claimed history adopted_wip_commit = %v, want %s", entry.Extra["adopted_wip_commit"], head)
	}
}

// assertPreservedWorkUntouched checks a refused adoption lost nothing: HEAD
// did not move and the uncommitted work is still on disk.
func assertPreservedWorkUntouched(t *testing.T, fixture *preservedInitialClaimFixture, trackedContent string) {
	t.Helper()
	if head := testhelpers.MustGit(t, fixture.worktreeDir, "rev-parse", "HEAD"); head != fixture.preservedHead {
		t.Fatalf("worktree HEAD = %s, want untouched %s", head, fixture.preservedHead)
	}
	if got, err := os.ReadFile(filepath.Join(fixture.worktreeDir, "task.txt")); err != nil || string(got) != trackedContent {
		t.Fatalf("task.txt = %q, %v; want %q", got, err, trackedContent)
	}
	if got, err := os.ReadFile(filepath.Join(fixture.worktreeDir, adoptedUntrackedName)); err != nil || string(got) != adoptedUntrackedContent {
		t.Fatalf("%s = %q, %v; want %q", adoptedUntrackedName, got, err, adoptedUntrackedContent)
	}
}

func TestClaimTask_PreservedInitialAdoptsUncommittedWork(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	leaveUncommittedWork(t, fixture)

	if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, "coder-2"); err != nil {
		t.Fatalf("ClaimTask(coder-2) on a dirty preserved worktree = %v, want the work adopted", err)
	}
	assertAdoptedWIP(t, fixture, "coder-2")
}

// TestClaimTask_StrandedDirtyClaimIsTakenOverAndAdopted is the D75 + D59 path
// end to end: the holder died mid-edit and its lease expired.
func TestClaimTask_StrandedDirtyClaimIsTakenOverAndAdopted(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	strandPreservedTask(t, fixture, time.Now().UTC().Add(-time.Minute))
	leaveUncommittedWork(t, fixture)

	if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, "coder-2"); err != nil {
		t.Fatalf("ClaimTask(coder-2) on a stranded dirty claim = %v, want takeover and adoption", err)
	}
	assertAdoptedWIP(t, fixture, "coder-2")
}

// TestClaimTask_PreservedInitialInterruptedMergeIsNotAdopted keeps the
// fail-closed gate where adoption would commit conflict markers as resolved.
func TestClaimTask_PreservedInitialInterruptedMergeIsNotAdopted(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	// A branch that adds task.txt differently, merged into the task worktree,
	// leaves MERGE_HEAD and an unmerged task.txt.
	testhelpers.MustGit(t, fixture.projectRoot, "branch", "conflicting", fixture.originalBase)
	conflictDir := filepath.Join(t.TempDir(), "conflicting")
	testhelpers.MustGit(t, fixture.projectRoot, "worktree", "add", conflictDir, "conflicting")
	writeAndCommit(t, conflictDir, "task.txt", "conflicting content\n", "Conflicting change")
	if out, err := exec.Command("git", "-C", fixture.worktreeDir, "merge", "conflicting").CombinedOutput(); err == nil {
		t.Fatalf("fixture merge did not conflict: %s", out)
	}
	if err := os.WriteFile(filepath.Join(fixture.worktreeDir, adoptedUntrackedName), []byte(adoptedUntrackedContent), 0o644); err != nil {
		t.Fatalf("add untracked file: %v", err)
	}
	conflicted, err := os.ReadFile(filepath.Join(fixture.worktreeDir, "task.txt"))
	if err != nil {
		t.Fatalf("read conflicted task.txt: %v", err)
	}

	_, err = ClaimTask(fixture.projectRoot, fixture.taskID, "coder-2")
	if err == nil {
		t.Fatal("ClaimTask(coder-2) adopted a worktree with an interrupted merge")
	}
	assertPreservedInitialRecoveryState(t, fixture, "dirty")
	assertPreservedWorkUntouched(t, fixture, string(conflicted))
	if unmerged := testhelpers.MustGit(t, fixture.worktreeDir, "diff", "--name-only", "--diff-filter=U"); !strings.Contains(unmerged, "task.txt") {
		t.Fatalf("unmerged paths = %q, want task.txt still unresolved", unmerged)
	}
	testhelpers.MustGit(t, fixture.worktreeDir, "rev-parse", "-q", "--verify", "MERGE_HEAD")
}

// TestClaimTask_PreservedInitialFailedAdoptionPreservesWork makes the WIP
// commit fail after staging (the task branch ref is locked). Nothing may be
// lost: HEAD stays, the work stays on disk, and the task is blocked for repair.
func TestClaimTask_PreservedInitialFailedAdoptionPreservesWork(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	leaveUncommittedWork(t, fixture)
	commonDir := testhelpers.MustGit(t, fixture.worktreeDir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	refLock := filepath.Join(commonDir, "refs", "heads", "task", fixture.taskID+".lock")
	if err := os.WriteFile(refLock, nil, 0o644); err != nil {
		t.Fatalf("lock the task branch ref: %v", err)
	}

	_, err := ClaimTask(fixture.projectRoot, fixture.taskID, "coder-2")
	if err == nil {
		t.Fatal("ClaimTask(coder-2) succeeded although the task branch ref could not move")
	}
	// The refusal must come from the commit after staging, not an earlier gate.
	assertPreservedInitialRecoveryState(t, fixture, "adopting it failed")
	assertPreservedWorkUntouched(t, fixture, adoptedTrackedContent)
}
