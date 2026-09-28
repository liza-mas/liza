package ops

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D106: when the reviewer that prepared a merge is gone, merge ownership passes
// to a surviving reviewer. These tests cover what that new owner finds under the
// task review lock: an orphaned wt-merge preparation whose effect is either
// proven by a receipt, provably absent, or unattributable.

// registerMergeReviewer registers a live code reviewer with the given
// generation and returns its authority.
func registerMergeReviewer(t *testing.T, stateFile, agentID, generation string) models.AgentAuthority {
	t.Helper()
	if err := db.For(stateFile).Modify(func(state *models.State) error {
		agent := state.Agents[agentID]
		agent.Role = "code-reviewer"
		agent.Status = models.AgentStatusIdle
		agent.Generation = generation
		state.Agents[agentID] = agent
		return nil
	}); err != nil {
		t.Fatalf("register reviewer %s: %v", agentID, err)
	}
	return models.AgentAuthority{ID: agentID, Generation: generation}
}

// unregisterMergeReviewer removes an agent row, as a reviewer that exited or
// was killed and cleaned up leaves the registry.
func unregisterMergeReviewer(t *testing.T, stateFile, agentID string) {
	t.Helper()
	if err := db.For(stateFile).Modify(func(state *models.State) error {
		delete(state.Agents, agentID)
		return nil
	}); err != nil {
		t.Fatalf("unregister reviewer %s: %v", agentID, err)
	}
}

// orphanMergePreparation records the wt-merge preparation a reviewer leaves
// when it dies after reserving the merge and before any integration receipt.
func orphanMergePreparation(t *testing.T, stateFile, taskID string, preparer models.AgentAuthority) models.LifecyclePreparation {
	t.Helper()
	var prepared models.LifecyclePreparation
	if err := db.For(stateFile).Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		request, err := NewLifecycleRequest(integrationOperationWTMerge, task, preparer.ID, &preparer, LifecycleRequestOptions{}, []map[string]any(nil))
		if err != nil {
			return err
		}
		if err := PrepareLifecycleRequest(task, request, state.Agents); err != nil {
			return err
		}
		prepared = *task.Lifecycle.Preparation
		return nil
	}); err != nil {
		t.Fatalf("record orphaned merge preparation: %v", err)
	}
	return prepared
}

func mergedEventCount(task *models.Task) int {
	count := 0
	for _, entry := range task.History {
		if entry.Event == models.TaskEventMerged {
			count++
		}
	}
	return count
}

// TestMergeTakeoverFinishesProvenInterruptedMerge is the D106 instance: the
// preparer moved integration and recorded the receipt, then died before the
// MERGED write. The surviving reviewer that inherits the merge must finish it
// from the receipt, without a second integration write.
func TestMergeTakeoverFinishesProvenInterruptedMerge(t *testing.T) {
	const taskID = "takeover-proven"
	projectRoot, stateFile := setupMergeTestRepo(t, taskID, "code-reviewer-1")
	preparer := registerMergeReviewer(t, stateFile, "code-reviewer-1", "preparer-generation")
	owner := registerMergeReviewer(t, stateFile, "code-reviewer-2", "owner-generation")

	reviewCommit := interruptMergeAfterReceipt(t, projectRoot, stateFile, taskID, preparer)
	unregisterMergeReviewer(t, stateFile, preparer.ID)
	headAfterInterrupt := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration")

	result, err := MergeWorktreeWithAuthority(projectRoot, taskID, owner)
	if err != nil {
		t.Fatalf("inheriting owner merge = %v, want the proven merge finished", err)
	}
	if result == nil || result.MergeCommit != reviewCommit {
		t.Fatalf("merge result = %+v, want merge_commit %s", result, reviewCommit)
	}

	merged := readStateForTest(t, stateFile).FindTask(taskID)
	if merged.Status != models.TaskStatusMerged {
		t.Fatalf("status = %s, want MERGED", merged.Status)
	}
	if merged.MergeCommit == nil || *merged.MergeCommit != reviewCommit {
		t.Fatalf("merge_commit = %v, want %s", merged.MergeCommit, reviewCommit)
	}
	if got := mergedEventCount(merged); got != 1 {
		t.Fatalf("merged history events = %d, want exactly 1", got)
	}
	if head := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration"); head != headAfterInterrupt {
		t.Fatalf("integration HEAD = %s, want %s: a proven merge must not be written twice", head, headAfterInterrupt)
	}
}

// TestMergeTakeoverRetriesOrphanedPreparationWithoutEffect covers death after
// the preparation and before any integration write: no receipt exists and
// integration does not reach the approved commit, so nothing of this task is
// published and the inheriting owner must run the merge.
func TestMergeTakeoverRetriesOrphanedPreparationWithoutEffect(t *testing.T) {
	const taskID = "takeover-absent"
	projectRoot, stateFile := setupMergeTestRepo(t, taskID, "code-reviewer-1")
	preparer := registerMergeReviewer(t, stateFile, "code-reviewer-1", "preparer-generation")
	owner := registerMergeReviewer(t, stateFile, "code-reviewer-2", "owner-generation")
	reviewCommit := *readStateForTest(t, stateFile).FindTask(taskID).ReviewCommit

	orphanMergePreparation(t, stateFile, taskID, preparer)
	unregisterMergeReviewer(t, stateFile, preparer.ID)

	if _, err := MergeWorktreeWithAuthority(projectRoot, taskID, owner); err != nil {
		t.Fatalf("inheriting owner merge = %v, want the unpublished merge run", err)
	}

	merged := readStateForTest(t, stateFile).FindTask(taskID)
	if merged.Status != models.TaskStatusMerged {
		t.Fatalf("status = %s, want MERGED", merged.Status)
	}
	if got := mergedEventCount(merged); got != 1 {
		t.Fatalf("merged history events = %d, want exactly 1", got)
	}
	testhelpers.MustGit(t, projectRoot, "merge-base", "--is-ancestor", reviewCommit, "refs/heads/integration")
}

// TestMergeTakeoverKeepsUnattributableEffectFenced is the case the takeover
// must not guess at: integration already reaches the approved commit, but no
// receipt attributes that to this task, so neither the merge commit nor the
// rollback baseline is known. The inheriting owner is refused and nothing
// changes.
func TestMergeTakeoverKeepsUnattributableEffectFenced(t *testing.T) {
	const taskID = "takeover-unattributable"
	projectRoot, stateFile := setupMergeTestRepo(t, taskID, "code-reviewer-1")
	preparer := registerMergeReviewer(t, stateFile, "code-reviewer-1", "preparer-generation")
	owner := registerMergeReviewer(t, stateFile, "code-reviewer-2", "owner-generation")
	reviewCommit := *readStateForTest(t, stateFile).FindTask(taskID).ReviewCommit

	prepared := orphanMergePreparation(t, stateFile, taskID, preparer)
	unregisterMergeReviewer(t, stateFile, preparer.ID)
	// The integration ref reaches the approved commit with no receipt for it.
	testhelpers.MustGit(t, projectRoot, "update-ref", "refs/heads/integration", reviewCommit)
	before := readStateForTest(t, stateFile).FindTask(taskID)

	_, err := MergeWorktreeWithAuthority(projectRoot, taskID, owner)
	var lifecycleErr *LifecycleError
	if !errors.As(err, &lifecycleErr) || lifecycleErr.Outcome.Outcome != models.LifecycleStateChanged {
		t.Fatalf("inheriting owner merge = %T %v, want STATE_CHANGED", err, err)
	}
	// The refusal must tell the operator whose merge to inspect, and why.
	if msg := err.Error(); !strings.Contains(msg, preparer.ID) || !strings.Contains(msg, "inspect before recovery") {
		t.Fatalf("refusal = %q, want it to name preparer %s and ask for inspection", msg, preparer.ID)
	}

	after := readStateForTest(t, stateFile).FindTask(taskID)
	if after.Status != models.TaskStatusApproved {
		t.Fatalf("status = %s, want APPROVED", after.Status)
	}
	if after.Lifecycle == nil || after.Lifecycle.Preparation == nil || !reflect.DeepEqual(*after.Lifecycle.Preparation, prepared) {
		t.Fatalf("preparation = %+v, want the orphaned preparation %+v kept", after.Lifecycle, prepared)
	}
	if !reflect.DeepEqual(after.Lifecycle, before.Lifecycle) {
		t.Fatalf("lifecycle changed on a refused takeover: before %+v, after %+v", before.Lifecycle, after.Lifecycle)
	}
	if head := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration"); head != reviewCommit {
		t.Fatalf("integration HEAD = %s, want it untouched at %s", head, reviewCommit)
	}
}
