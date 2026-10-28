package ops

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func c6ReviewingFixture(t *testing.T) *c6Fixture {
	t.Helper()
	now := time.Now().UTC()
	return newC6Fixture(t, func(s *models.State) {
		task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
		task.ReviewingBy = testhelpers.StringPtr("code-reviewer-1")
		lease := now.Add(time.Hour)
		task.ReviewLeaseExpires = &lease
		s.Tasks = []models.Task{task}
		reviewer := c6LiveAgent("code-reviewer", now)
		reviewer.Status = models.AgentStatusReviewing
		reviewer.CurrentTask = testhelpers.StringPtr("task-1")
		s.Agents["code-reviewer-1"] = reviewer
	})
}

func TestC6RecordWorktreeRecoveredAppendsOneHistoryEntry(t *testing.T) {
	f := c6ReviewingFixture(t)
	before := f.read(t).FindTask("task-1")
	started := time.Now().UTC().Add(-time.Second)

	if err := RecordWorktreeRecovered(f.bb, c6Authority("code-reviewer-1"), "task-1"); err != nil {
		t.Fatalf("RecordWorktreeRecovered: %v", err)
	}

	after := f.read(t).FindTask("task-1")
	if len(after.History) != len(before.History)+1 {
		t.Fatalf("history length = %d, want %d", len(after.History), len(before.History)+1)
	}
	last := after.History[len(after.History)-1]
	if last.Event != models.TaskEventWorktreeRecovered || last.Agent == nil || *last.Agent != "code-reviewer-1" ||
		last.Reason != nil || last.Time.Before(started) {
		t.Fatalf("last history entry = %+v, want worktree_recovered by code-reviewer-1 stamped under the lock", last)
	}
	// Only the history grew: status, claims and lifecycle are the recovery's business, not this record's.
	if after.Status != before.Status || after.ReviewingBy == nil || *after.ReviewingBy != "code-reviewer-1" || after.Lifecycle != nil {
		t.Fatalf("task changed beyond its history: status %s, reviewing_by %v, lifecycle %v", after.Status, after.ReviewingBy, after.Lifecycle)
	}
}

func TestC6RecordWorktreeRecoveredMissingTaskAndStaleAuthority(t *testing.T) {
	f := c6ReviewingFixture(t)
	before := f.bytes(t)

	if err := RecordWorktreeRecovered(f.bb, c6Authority("code-reviewer-1"), "no-such-task"); err != nil {
		t.Fatalf("missing task = %v, want a no-op", err)
	}
	f.requireUnchanged(t, before, "recording for a missing task")

	stale := models.AgentAuthority{ID: "code-reviewer-1", Generation: "replaced"}
	requireAuthorityError(t, RecordWorktreeRecovered(f.bb, stale, "task-1"), "code-reviewer-1")
	f.requireUnchanged(t, before, "recording by a replaced generation")
}

func TestC6ForceBlockReviewerTaskBlocksOutsideTheTransitionTable(t *testing.T) {
	f := c6ReviewingFixture(t)
	const reason = "worktree and branch both missing — unrecoverable"
	started := time.Now().UTC().Add(-time.Second)

	if err := ForceBlockReviewerTask(f.bb, c6Authority("code-reviewer-1"), "task-1", reason); err != nil {
		t.Fatalf("ForceBlockReviewerTask: %v", err)
	}

	state := f.read(t)
	task := state.FindTask("task-1")
	if task.Status != models.TaskStatusBlocked || task.BlockedReason == nil || *task.BlockedReason != reason {
		t.Fatalf("task = %+v, want BLOCKED with the reason", task)
	}
	if len(task.BlockedQuestions) != 2 || task.BlockedQuestions[0] != "Is the task branch recoverable from a remote or backup?" ||
		task.BlockedQuestions[1] != "Should this task be recreated with a fresh worktree?" {
		t.Fatalf("blocked questions = %q", task.BlockedQuestions)
	}
	if task.ReviewingBy != nil || task.ReviewLeaseExpires != nil {
		t.Fatalf("reviewer claim not cleared: %+v", task)
	}
	if task.Lifecycle == nil || task.Lifecycle.Revision != 1 || task.Lifecycle.Preparation != nil {
		t.Fatalf("lifecycle = %+v, want one advance with no preparation", task.Lifecycle)
	}
	reviewer := state.Agents["code-reviewer-1"]
	if reviewer.CurrentTask != nil || reviewer.Status != models.AgentStatusIdle {
		t.Fatalf("reviewer agent row = %+v, want released", reviewer)
	}
	last := task.History[len(task.History)-1]
	if last.Event != models.TaskEventBlocked || last.Agent == nil || *last.Agent != "code-reviewer-1" ||
		last.Reason == nil || *last.Reason != reason || last.Time.Before(started) {
		t.Fatalf("last history entry = %+v, want the blocked event by the reviewer", last)
	}
}

func TestC6ForceBlockReviewerTaskMissingTaskAndStaleAuthority(t *testing.T) {
	f := c6ReviewingFixture(t)
	before := f.bytes(t)

	if err := ForceBlockReviewerTask(f.bb, c6Authority("code-reviewer-1"), "no-such-task", "lost"); err != nil {
		t.Fatalf("missing task = %v, want a no-op", err)
	}
	f.requireUnchanged(t, before, "forced block of a missing task")

	stale := models.AgentAuthority{ID: "code-reviewer-1", Generation: "replaced"}
	requireAuthorityError(t, ForceBlockReviewerTask(f.bb, stale, "task-1", "lost"), "code-reviewer-1")
	f.requireUnchanged(t, before, "forced block by a replaced generation")
}
