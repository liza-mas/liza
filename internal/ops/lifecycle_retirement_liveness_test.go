package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D60: a lifecycle invocation known to have failed must release its own
// preparation, or every later claim of the task is refused with "an unresolved
// preparation remains at this ownership boundary" until the preparing agent
// restarts. None of these tests is parallel: they shorten the package-wide
// state-lock timeout and install package test hooks.

const livenessLockTimeout = 200 * time.Millisecond

// retirementLockHold saturates the state lock from a pre-write hook, then
// releases it well after the first authorized lifecycle write has started to
// wait. A retirement on the ordinary budget gives up first; one on the patient
// budget outlasts the hold.
type retirementLockHold struct {
	release func()
}

func newRetirementLockHold(t *testing.T) *retirementLockHold {
	t.Helper()
	h := &retirementLockHold{}
	scheduled := false
	previous := lifecycleBeforeModifyTestHook
	t.Cleanup(func() { lifecycleBeforeModifyTestHook = previous })
	lifecycleBeforeModifyTestHook = func() {
		if h.release == nil || scheduled {
			return
		}
		scheduled = true
		time.AfterFunc(5*livenessLockTimeout, h.release)
	}
	return h
}

func (h *retirementLockHold) hold(t *testing.T, statePath string) {
	if h.release == nil {
		h.release = testhelpers.HoldFileLock(t, statePath)
	}
}

func (h *retirementLockHold) done() {
	if h.release != nil {
		h.release()
	}
}

func requireNoPreparation(t *testing.T, bb *db.Blackboard, taskID, context string) *models.Task {
	t.Helper()
	task, err := bb.GetTask(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Lifecycle != nil && task.Lifecycle.Preparation != nil {
		p := task.Lifecycle.Preparation
		t.Fatalf("%s retained its preparation (operation %s, actor %s)", context, p.Operation, p.Actor)
	}
	return task
}

// RT1: the D60 live case. The claim's phase-3 write times out on the state
// lock and its retirement runs under the same contention.
func TestFailedClaimRetiresPreparationUnderStateLockContention(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(livenessLockTimeout))
	root, statePath, bb, authority := setupOwnershipLifecycleClaim(t)
	hold := newRetirementLockHold(t)
	previousHooks := testClaimTaskHooks
	t.Cleanup(func() { testClaimTaskHooks = previousHooks })
	testClaimTaskHooks = &claimTaskTestHooks{beforePhase3Modify: func() { hold.hold(t, statePath) }}

	_, err := ClaimTaskWithAuthority(root, "task-1", authority)
	testClaimTaskHooks = previousHooks
	hold.done()
	if err == nil || !filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		t.Fatalf("first claim error = %v, want the phase-3 state-lock timeout", err)
	}
	requireNoPreparation(t, bb, "task-1", "failed claim")

	result, err := ClaimTaskWithAuthority(root, "task-1", authority)
	if err != nil {
		t.Fatalf("same-generation claim after a failed claim = %v, want success", err)
	}
	if result.Outcome != models.LifecycleCompleted {
		t.Fatalf("retry outcome = %s, want %s", result.Outcome, models.LifecycleCompleted)
	}
}

// RT3: submit-for-review's finalization times out on the state lock after its
// rebase; its retirement runs under the same contention.
func TestFailedSubmitRetiresPreparationUnderStateLockContention(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(livenessLockTimeout))
	projectRoot, taskID, commit, agentID, bb := setupSuccessfulSubmitScenario(t)
	statePath := bbStatePath(projectRoot)
	setLifecycleAgentGeneration(t, bb, agentID, lifecycleGenerationA)
	authority := models.AgentAuthority{ID: agentID, Generation: lifecycleGenerationA}
	hold := newRetirementLockHold(t)
	previousHook := submitReviewBeforeModifyTestHook
	t.Cleanup(func() { submitReviewBeforeModifyTestHook = previousHook })
	submitReviewBeforeModifyTestHook = func() { hold.hold(t, statePath) }

	_, err := SubmitForReviewWithAuthority(projectRoot, taskID, commit, authority)
	submitReviewBeforeModifyTestHook = previousHook
	hold.done()
	if err == nil || !filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		t.Fatalf("first submit error = %v, want the finalization state-lock timeout", err)
	}
	requireNoPreparation(t, bb, taskID, "failed submit-for-review")

	if _, err := SubmitForReviewWithAuthority(projectRoot, taskID, "HEAD", authority); err != nil {
		t.Fatalf("same-generation resubmission after a failed submit = %v, want success", err)
	}
}

func bbStatePath(projectRoot string) string {
	return paths.New(projectRoot).StatePath()
}

// setupDirtyPreservedRecovery mirrors the 2026-09-25 cpr-4-code-1 state: an
// unassigned initial-status task whose preserved worktree holds uncommitted work.
func setupDirtyPreservedRecovery(t *testing.T) (root, statePath string, bb *db.Blackboard, g *git.Git) {
	t.Helper()
	root, statePath, bb, _ = setupOwnershipLifecycleClaim(t)
	g = git.New(root)
	baseCommit, err := g.CreateWorktree("task-1", "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	worktree := g.GetWorktreeRelPath("task-1")
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("task-1")
		task.Worktree = &worktree
		task.BaseCommit = &baseCommit
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.GetWorktreePath("task-1"), "wip.txt"), []byte("interrupted work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, statePath, bb, g
}

// RT5: a recover-task refused before any Git effect must not leave its
// generation-less reservation behind; a fresh request then recovers the task,
// while the refused request stays stale.
func TestRefusedPreserveRecoveryRetiresItsPreparation(t *testing.T) {
	root, _, bb, g := setupDirtyPreservedRecovery(t)
	stale := ownershipRequestOptions(t, bb, "recover-dirty")

	_, err := RecoverTaskWithOptions(root, "task-1", "dirty preserved worktree", RecoverTaskOptions{RequestOptions: stale})
	if err == nil || !strings.Contains(err.Error(), "preserved worktree is dirty") {
		t.Fatalf("RecoverTaskWithOptions() error = %v, want dirty worktree refusal", err)
	}
	task := requireNoPreparation(t, bb, "task-1", "refused preserve recovery")
	if models.TaskTransitionID(task) == stale.ExpectedTransition {
		t.Fatal("refused recovery left the original transition current; its exact replay would be ambiguous")
	}

	wt := g.GetWorktreePath("task-1")
	testhelpers.MustGit(t, wt, "add", "wip.txt")
	testhelpers.MustGit(t, wt, "commit", "-m", "preserve interrupted work")

	_, err = RecoverTaskWithOptions(root, "task-1", "dirty preserved worktree", RecoverTaskOptions{RequestOptions: stale})
	requireLifecycleError(t, err, models.LifecycleStateChanged, "requery", "none")
	fresh := ownershipRequestOptions(t, bb, "recover-clean")
	if _, err := RecoverTaskWithOptions(root, "task-1", "worktree committed", RecoverTaskOptions{RequestOptions: fresh}); err != nil {
		t.Fatalf("fresh recovery after the refusal = %v, want success", err)
	}
	requireNoPreparation(t, bb, "task-1", "completed recovery")
}

// seedMissingWorktreeRecovery leaves the task branch, one commit ahead of its
// base, without its worktree, so preserve recovery must attach it before any
// refusal.
func seedMissingWorktreeRecovery(t *testing.T) (root, baseCommit string, bb *db.Blackboard, g *git.Git) {
	t.Helper()
	root, _, bb, _ = setupOwnershipLifecycleClaim(t)
	g = git.New(root)
	baseCommit, err := g.CreateWorktree("task-1", "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	worktree := g.GetWorktreeRelPath("task-1")
	wt := g.GetWorktreePath("task-1")
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("committed work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wt, "add", "work.txt")
	testhelpers.MustGit(t, wt, "commit", "-m", "task work")
	testhelpers.MustGit(t, root, "worktree", "remove", "--force", wt)
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("task-1")
		task.Worktree = &worktree
		task.BaseCommit = &baseCommit
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return root, baseCommit, bb, g
}

func requirePreparationRetained(t *testing.T, bb *db.Blackboard, context string) {
	t.Helper()
	task, err := bb.GetTask("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Lifecycle == nil || task.Lifecycle.Preparation == nil || task.Lifecycle.Preparation.Operation != "recover-task" {
		t.Fatalf("%s: recovery fence was not retained", context)
	}
}

// RT5b (characterization): a successful attach is a Git effect, so a refusal
// after it keeps the fence.
func TestPreserveRecoveryRefusedAfterAttachKeepsPreparation(t *testing.T) {
	root, otherCommit, bb, g := seedMissingWorktreeRecovery(t)
	if branchHead := testhelpers.MustGit(t, root, "rev-parse", "task/task-1"); otherCommit == branchHead {
		t.Fatal("test setup needs a review commit that differs from the branch HEAD")
	}
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("task-1")
		task.Status = models.TaskStatusReviewing
		reviewer := "code-reviewer-1"
		task.ReviewingBy = &reviewer
		expired := time.Now().UTC().Add(-time.Hour)
		task.ReviewLeaseExpires = &expired
		task.ReviewCommit = &otherCommit
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	opts := ownershipRequestOptions(t, bb, "recover-after-attach")

	_, err := RecoverTaskWithOptions(root, "task-1", "attach then refuse", RecoverTaskOptions{RequestOptions: opts, Force: true})
	if err == nil || !strings.Contains(err.Error(), "does not match worktree HEAD") {
		t.Fatalf("RecoverTaskWithOptions() error = %v, want review-commit mismatch after attach", err)
	}
	if _, statErr := os.Stat(g.GetWorktreePath("task-1")); statErr != nil {
		t.Fatalf("attach did not run before the refusal: %v", statErr)
	}
	requirePreparationRetained(t, bb, "refusal after attach")
}

// RT5c (characterization): a failed attach may have created the directory or
// worktree metadata, so its refusal keeps the fence.
func TestPreserveRecoveryFailedAttachKeepsPreparation(t *testing.T) {
	root, _, bb, _ := seedMissingWorktreeRecovery(t)
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	testhelpers.MustGit(t, root, "worktree", "add", elsewhere, "task/task-1")
	opts := ownershipRequestOptions(t, bb, "recover-failed-attach")

	_, err := RecoverTaskWithOptions(root, "task-1", "attach fails", RecoverTaskOptions{RequestOptions: opts})
	if err == nil || !strings.Contains(err.Error(), "reattach worktree") {
		t.Fatalf("RecoverTaskWithOptions() error = %v, want reattach failure", err)
	}
	requirePreparationRetained(t, bb, "failed attach")
}

// RT6 (characterization): a generation-less recover-task marker blocks claims,
// and a fresh inspected recovery request clears it.
func TestFreshRecoveryClearsGenerationlessPreparation(t *testing.T) {
	root, _, bb, authority := setupOwnershipLifecycleClaim(t)
	stale := ownershipRequestOptions(t, bb, "abandoned-recovery")
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("task-1")
		request, err := NewLifecycleRequest("recover-task", task, "human", nil, stale, struct {
			Force, Fresh bool
			Reason       string
		}{false, false, "abandoned"})
		if err != nil {
			return err
		}
		return prepareOwnerEndingRequest(task, request)
	}); err != nil {
		t.Fatal(err)
	}

	_, err := ClaimTaskWithAuthority(root, "task-1", authority)
	if err == nil || !strings.Contains(err.Error(), "unresolved preparation remains") {
		t.Fatalf("claim over a generation-less marker = %v, want an unresolved-preparation refusal", err)
	}
	fresh := ownershipRequestOptions(t, bb, "inspected-recovery")
	if _, err := RecoverTaskWithOptions(root, "task-1", "inspected", RecoverTaskOptions{RequestOptions: fresh}); err != nil {
		t.Fatalf("fresh recovery over a generation-less marker = %v, want success", err)
	}
	requireNoPreparation(t, bb, "task-1", "fresh recovery")
	if _, err := ClaimTaskWithAuthority(root, "task-1", authority); err != nil {
		t.Fatalf("claim after fresh recovery = %v, want success", err)
	}
}

// innerLockTimeout returns a genuine state-lock-style timeout produced after
// the wrapper's own lock was acquired.
func innerLockTimeout(t *testing.T) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "inner")
	release := testhelpers.HoldFileLock(t, path)
	defer release()
	err := filelock.New(path).WithTimeout(10*time.Millisecond).WithLockOperation("inner", func() error { return nil })
	if !filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		t.Fatalf("inner lock error = %v, want timeout", err)
	}
	return err
}

// RT7: a wrapper names its own lock only when that lock's acquisition timed
// out; a timeout returned by the callback is reported as itself.
func TestLockWrappersDoNotRelabelCallbackTimeouts(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	inner := innerLockTimeout(t)
	cases := map[string]struct {
		label string
		run   func(fn func() error) error
	}{
		"project lifecycle": {"project lifecycle operation", func(fn func() error) error {
			return WithProjectLifecycleSharedLock(projectRoot, "task-claim-worktree", fn)
		}},
		"integration mutation": {"integration mutation lock operation", func(fn func() error) error {
			return withIntegrationMutationLockTimeout(projectRoot, "merge", time.Second, fn)
		}},
		"agent lifecycle": {"agent lifecycle operation", func(fn func() error) error {
			return WithAgentLifecycleLock(context.Background(), projectRoot, "coder-1", "register", fn)
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.run(func() error { return inner })
			if !errors.Is(err, inner) || !filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
				t.Fatalf("callback timeout lost its chain or classification: %v", err)
			}
			if strings.Contains(err.Error(), tc.label) {
				t.Fatalf("callback timeout relabelled as the wrapper's own lock: %q", err)
			}
		})
	}
}
