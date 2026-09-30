package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// newRejectedRebaseFixture reuses the preserved-claim fixture (task branch with
// one committed change on top of integration) and turns the task into REJECTED
// work still owned by coder-1, the shape await-verdict auto-reclaims.
func newRejectedRebaseFixture(t *testing.T, baseFile, baseContent string) *preservedInitialClaimFixture {
	t.Helper()
	fixture := newPreservedInitialClaimFixtureWithBaseFile(t, baseFile, baseContent)
	state := readClaimStateForTest(t, fixture.stateFile)
	rejected := testhelpers.BuildTaskByStatus(fixture.taskID, models.TaskStatusRejected, time.Now().UTC())
	rejected.Worktree = state.FindTask(fixture.taskID).Worktree
	rejected.BaseCommit = &fixture.originalBase
	state.Tasks = []models.Task{rejected}
	testhelpers.WriteInitialState(t, fixture.stateFile, state)
	return fixture
}

func lastRejectedClaimHistory(t *testing.T, fixture *preservedInitialClaimFixture) (*models.Task, models.TaskHistoryEntry) {
	t.Helper()
	task := readClaimStateForTest(t, fixture.stateFile).FindTask(fixture.taskID)
	if task == nil {
		t.Fatal("task not found")
	}
	for i := len(task.History) - 1; i >= 0; i-- {
		if task.History[i].Event == models.TaskEventReclaimedAfterRejection {
			return task, task.History[i]
		}
	}
	t.Fatalf("no %s history entry: %+v", models.TaskEventReclaimedAfterRejection, task.History)
	return nil, models.TaskHistoryEntry{}
}

// assertRejectedRebaseSkipped checks the claim kept the rejected branch and its
// base untouched, left no Git operation behind, and recorded why.
func assertRejectedRebaseSkipped(t *testing.T, fixture *preservedInitialClaimFixture, reasonContains string) {
	t.Helper()
	if head := testhelpers.MustGit(t, fixture.worktreeDir, "rev-parse", "HEAD"); head != fixture.preservedHead {
		t.Fatalf("HEAD = %s, want preserved %s", head, fixture.preservedHead)
	}
	operation, err := git.New(fixture.projectRoot).InterruptedOperation(fixture.worktreeDir)
	if err != nil || operation != "" {
		t.Fatalf("interrupted operation = %q, %v; want none", operation, err)
	}
	task, entry := lastRejectedClaimHistory(t, fixture)
	if task.Status != models.TaskStatusImplementing {
		t.Fatalf("status = %s, want IMPLEMENTING", task.Status)
	}
	if task.BaseCommit == nil || *task.BaseCommit != fixture.originalBase {
		t.Fatalf("base_commit = %v, want unchanged %s", task.BaseCommit, fixture.originalBase)
	}
	reason, _ := entry.Extra["rebase_skipped"].(string)
	if !strings.Contains(reason, reasonContains) {
		t.Fatalf("history rebase_skipped = %q, want it to contain %q (extra %v)", reason, reasonContains, entry.Extra)
	}
	if _, ok := entry.Extra["rebase_target_sha"]; ok {
		t.Fatalf("skipped rebase recorded rebase_target_sha: %v", entry.Extra)
	}
}

func TestClaimTask_RejectedReclaimRebasesOntoAdvancedIntegration(t *testing.T) {
	t.Parallel()
	// GIVEN rejected work based on an integration commit that has since advanced
	fixture := newRejectedRebaseFixture(t, "", "")
	targetSHA := advancePreservedClaimIntegration(t, fixture.projectRoot, "dependency.txt", "merged dependency\n", "Merge dependency")

	// WHEN the doer re-claims it
	result, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID)
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}

	// THEN the rejected work sits on current integration and base_commit follows
	if result.BaseCommit != targetSHA {
		t.Fatalf("result BaseCommit = %s, want integration %s", result.BaseCommit, targetSHA)
	}
	head := testhelpers.MustGit(t, fixture.worktreeDir, "rev-parse", "HEAD")
	ancestor, err := git.New(fixture.projectRoot).IsAncestor(targetSHA, head)
	if err != nil || !ancestor {
		t.Fatalf("HEAD %s descends from integration %s = %v, %v; want true", head, targetSHA, ancestor, err)
	}
	if got := testhelpers.MustGit(t, fixture.worktreeDir, "log", "-1", "--format=%s"); got != fixture.commitMessage {
		t.Fatalf("tip commit = %q, want rejected work %q", got, fixture.commitMessage)
	}
	if got, err := os.ReadFile(filepath.Join(fixture.worktreeDir, "task.txt")); err != nil || string(got) != fixture.taskContent {
		t.Fatalf("task content = %q, %v; want %q", got, err, fixture.taskContent)
	}
	task, entry := lastRejectedClaimHistory(t, fixture)
	if task.BaseCommit == nil || *task.BaseCommit != targetSHA {
		t.Fatalf("base_commit = %v, want %s", task.BaseCommit, targetSHA)
	}
	if entry.Extra["rebase_target_sha"] != targetSHA || entry.Extra["rebase_old_head"] != fixture.preservedHead {
		t.Fatalf("history extra = %v, want rebase_target_sha %s and rebase_old_head %s", entry.Extra, targetSHA, fixture.preservedHead)
	}
}

func TestClaimTask_RejectedReclaimRebaseConflictKeepsBaseAndClaims(t *testing.T) {
	t.Parallel()
	// GIVEN rejected work whose file integration has since changed differently
	fixture := newRejectedRebaseFixture(t, "task.txt", "base content\n")
	advancePreservedClaimIntegration(t, fixture.projectRoot, "task.txt", "integration content\n", "Conflicting integration change")

	// WHEN the doer re-claims it
	if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID); err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}

	// THEN the claim proceeds on the old base and the conflict is left to the doer
	assertRejectedRebaseSkipped(t, fixture, "conflict")
	if status := testhelpers.MustGit(t, fixture.worktreeDir, "status", "--short"); status != "" {
		t.Fatalf("worktree status = %q, want clean", status)
	}
}

func TestClaimTask_RejectedReclaimRebaseRefusedBeforeStartSkips(t *testing.T) {
	t.Parallel()
	// GIVEN integration advanced and a pre-rebase hook that refuses every rebase
	fixture := newRejectedRebaseFixture(t, "", "")
	advancePreservedClaimIntegration(t, fixture.projectRoot, "dependency.txt", "merged dependency\n", "Merge dependency")
	hooksDir := testhelpers.MustGit(t, fixture.projectRoot, "rev-parse", "--path-format=absolute", "--git-path", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "pre-rebase"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// WHEN the doer re-claims it
	if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID); err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}

	// THEN the refusal is a skip, not a claim failure
	assertRejectedRebaseSkipped(t, fixture, "rebase")
}

func TestClaimTask_RejectedReclaimTrackedDirtySkipsAndPreserves(t *testing.T) {
	t.Parallel()
	// GIVEN integration advanced and uncommitted tracked work in the rejected worktree
	fixture := newRejectedRebaseFixture(t, "", "")
	advancePreservedClaimIntegration(t, fixture.projectRoot, "dependency.txt", "merged dependency\n", "Merge dependency")
	dirty := "uncommitted rework\n"
	if err := os.WriteFile(filepath.Join(fixture.worktreeDir, "task.txt"), []byte(dirty), 0o644); err != nil {
		t.Fatal(err)
	}

	// WHEN the doer re-claims it
	if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID); err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}

	// THEN the uncommitted work is untouched and the rebase is skipped
	assertRejectedRebaseSkipped(t, fixture, "tracked changes")
	if got, err := os.ReadFile(filepath.Join(fixture.worktreeDir, "task.txt")); err != nil || string(got) != dirty {
		t.Fatalf("task content = %q, %v; want uncommitted %q", got, err, dirty)
	}
	if status := testhelpers.MustGit(t, fixture.worktreeDir, "status", "--short"); status != "M task.txt" {
		t.Fatalf("worktree status = %q, want unstaged modification of task.txt", status)
	}
}

// Not parallel: installs the package-level claim test hook.
func TestClaimTask_RejectedReclaimRebasedThenPhase3FailureRetries(t *testing.T) {
	// GIVEN integration advanced and a first claim that fails after its rebase
	fixture := newRejectedRebaseFixture(t, "", "")
	targetSHA := advancePreservedClaimIntegration(t, fixture.projectRoot, "dependency.txt", "merged dependency\n", "Merge dependency")
	// The coder's generation rotates between the rebase and Phase 3, so the
	// stale-authority claim fails and the current generation retries.
	bb := db.For(fixture.stateFile)
	setLifecycleAgentGeneration(t, bb, fixture.agentID, lifecycleGenerationA)
	previousHooks := testClaimTaskHooks
	testClaimTaskHooks = &claimTaskTestHooks{beforePhase3Modify: func() {
		setLifecycleAgentGeneration(t, bb, fixture.agentID, lifecycleGenerationB)
	}}
	t.Cleanup(func() { testClaimTaskHooks = previousHooks })

	// WHEN the first claim fails in Phase 3
	_, err := ClaimTaskWithAuthority(fixture.projectRoot, fixture.taskID, models.AgentAuthority{
		ID: fixture.agentID, Generation: lifecycleGenerationA,
	})
	assertLifecycleAuthorityError(t, err, fixture.agentID)

	// THEN the rebase stays, base_commit is still the old base, and that base is still an ancestor
	rebasedHead := testhelpers.MustGit(t, fixture.worktreeDir, "rev-parse", "HEAD")
	gitWrapper := git.New(fixture.projectRoot)
	if ancestor, err := gitWrapper.IsAncestor(targetSHA, rebasedHead); err != nil || !ancestor {
		t.Fatalf("HEAD %s descends from integration %s = %v, %v; want rebased", rebasedHead, targetSHA, ancestor, err)
	}
	task := readClaimStateForTest(t, fixture.stateFile).FindTask(fixture.taskID)
	if task.Status != models.TaskStatusRejected || task.BaseCommit == nil || *task.BaseCommit != fixture.originalBase {
		t.Fatalf("after failed claim: status %s base_commit %v; want REJECTED at %s", task.Status, task.BaseCommit, fixture.originalBase)
	}
	if ancestor, err := gitWrapper.IsAncestor(fixture.originalBase, rebasedHead); err != nil || !ancestor {
		t.Fatalf("old base %s ancestor of %s = %v, %v; want true", fixture.originalBase, rebasedHead, ancestor, err)
	}

	// WHEN the current generation retries
	testClaimTaskHooks = previousHooks
	if _, err := ClaimTaskWithAuthority(fixture.projectRoot, fixture.taskID, models.AgentAuthority{
		ID: fixture.agentID, Generation: lifecycleGenerationB,
	}); err != nil {
		t.Fatalf("retry ClaimTaskWithAuthority() error: %v", err)
	}

	// THEN no second rebase happens but base_commit catches up to integration
	if head := testhelpers.MustGit(t, fixture.worktreeDir, "rev-parse", "HEAD"); head != rebasedHead {
		t.Fatalf("retry moved HEAD from %s to %s, want no rebase", rebasedHead, head)
	}
	task, entry := lastRejectedClaimHistory(t, fixture)
	if task.BaseCommit == nil || *task.BaseCommit != targetSHA {
		t.Fatalf("retry base_commit = %v, want integration %s", task.BaseCommit, targetSHA)
	}
	if _, ok := entry.Extra["rebase_old_head"]; ok {
		t.Fatalf("no-op retry recorded a rebase: %v", entry.Extra)
	}
	if _, ok := entry.Extra["rebase_skipped"]; ok {
		t.Fatalf("no-op retry recorded a skip: %v", entry.Extra)
	}
}
