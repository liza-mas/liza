package ops

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestC6PersistExit42RestartRecordsCountWithoutBlockingBelowLimit(t *testing.T) {
	f := c6WorkingDoer(t, 1, nil)
	blocked, err := PersistExit42Restart(f.bb, c6Authority("coder-1"), f.root, "task-1", 2, 2)
	if err != nil || blocked {
		t.Fatalf("PersistExit42Restart at the limit = (%v, %v), want no block", blocked, err)
	}
	task := f.read(t).FindTask("task-1")
	if task.Exit42RestartCount != 2 || task.Status != models.TaskStatusImplementing || task.AssignedTo == nil {
		t.Fatalf("task = count %d status %s assigned %v, want the count persisted and the claim untouched",
			task.Exit42RestartCount, task.Status, task.AssignedTo)
	}
}

// This is the restart-loop BLOCKED branch that decision D40 leaves unpinned by
// the black-box oracle: no supervisor recipe reaches it, because the exit
// releases the claim before the count is persisted.
func TestC6PersistExit42RestartBlocksOwnedExecutingTaskPastTheLimit(t *testing.T) {
	f := c6WorkingDoer(t, 1, func(s *models.State) {
		// The agent row keeps naming the task: unlike a supervisor block, this
		// path releases no agent-side claim.
		agent := s.Agents["coder-1"]
		agent.CurrentTask = testhelpers.StringPtr("task-1")
		s.Agents["coder-1"] = agent
	})
	blocked, err := PersistExit42Restart(f.bb, c6Authority("coder-1"), f.root, "task-1", 3, 2)
	if err != nil || !blocked {
		t.Fatalf("PersistExit42Restart past the limit = (%v, %v), want blocked", blocked, err)
	}
	state := f.read(t)
	task := state.FindTask("task-1")
	wantReason := "exit code 42 restart loop detected: 3 consecutive restarts without progress (threshold=2)"
	if task.Status != models.TaskStatusBlocked || task.AssignedTo != nil || task.LeaseExpires != nil || task.ReviewingBy != nil ||
		task.BlockedReason == nil || *task.BlockedReason != wantReason || len(task.BlockedQuestions) != 2 || task.Exit42RestartCount != 3 {
		t.Fatalf("blocked task = %+v", task)
	}
	last := task.History[len(task.History)-1]
	if last.Event != models.TaskEventBlocked || last.Agent == nil || *last.Agent != "coder-1" || last.Reason == nil || *last.Reason != wantReason {
		t.Fatalf("last history entry = %+v", last)
	}
	if agent := state.Agents["coder-1"]; agent.CurrentTask == nil {
		t.Fatal("the exit-42 block must not release the agent row's claim")
	}
}

func TestC6PersistExit42RestartBlocksOwnedReviewingTask(t *testing.T) {
	f := c6ReviewedTask(t)
	blocked, err := PersistExit42Restart(f.bb, c6Authority("code-reviewer-1"), f.root, "task-1", 5, 1)
	if err != nil || !blocked {
		t.Fatalf("reviewer past the limit = (%v, %v), want blocked", blocked, err)
	}
	task := f.read(t).FindTask("task-1")
	if task.Status != models.TaskStatusBlocked || task.ReviewingBy != nil || task.ReviewLeaseExpires != nil {
		t.Fatalf("blocked reviewing task = %+v", task)
	}
}

func TestC6PersistExit42RestartLeavesUnownedAndInactiveTasksUnblocked(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name   string
		agent  string
		mutate func(*models.State)
	}{
		{name: "another agent's executing task", agent: "coder-2", mutate: func(s *models.State) { s.Agents["coder-2"] = c6LiveAgent("coder", now) }},
		{name: "owned task in a non-active status", agent: "coder-1", mutate: func(s *models.State) {
			s.Tasks[0].Status = models.TaskStatusReady
			s.Tasks[0].LeaseExpires = nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := c6WorkingDoer(t, 1, tc.mutate)
			blocked, err := PersistExit42Restart(f.bb, c6Authority(tc.agent), f.root, "task-1", 9, 1)
			if err != nil || blocked {
				t.Fatalf("PersistExit42Restart = (%v, %v), want no block", blocked, err)
			}
			task := f.read(t).FindTask("task-1")
			if task.Status == models.TaskStatusBlocked || task.Exit42RestartCount != 9 {
				t.Fatalf("task = status %s count %d, want the count recorded and no block", task.Status, task.Exit42RestartCount)
			}
		})
	}
}

func TestC6PersistExit42RestartEmptyAndMissingTaskStillFenced(t *testing.T) {
	f := c6WorkingDoer(t, 1, nil)
	before := f.bytes(t)
	for _, taskID := range []string{"", "task-gone"} {
		blocked, err := PersistExit42Restart(f.bb, c6Authority("coder-1"), f.root, taskID, 9, 1)
		if err != nil || blocked {
			t.Fatalf("PersistExit42Restart(%q) = (%v, %v), want a no-op", taskID, blocked, err)
		}
		f.requireUnchanged(t, before, "no-op restart persistence")
	}
	_, err := PersistExit42Restart(f.bb, models.AgentAuthority{ID: "coder-1", Generation: "replaced"}, f.root, "", 9, 1)
	requireAuthorityError(t, err, "coder-1")
	_, err = PersistExit42Restart(f.bb, models.AgentAuthority{ID: "coder-1", Generation: "replaced"}, f.root, "task-1", 9, 1)
	requireAuthorityError(t, err, "coder-1")
	f.requireUnchanged(t, before, "fenced-out restart persistence")
}

func TestC6PersistExit42RestartPipelineUnreadableSurfacesOnlyPastTheLimit(t *testing.T) {
	f := c6WorkingDoer(t, 1, nil)
	// A project root without a frozen pipeline: below the limit no pipeline is
	// needed, past it the load error comes back and nothing is written.
	if blocked, err := PersistExit42Restart(f.bb, c6Authority("coder-1"), t.TempDir(), "task-1", 1, 1); err != nil || blocked {
		t.Fatalf("below the limit = (%v, %v), want no pipeline access", blocked, err)
	}
	before := f.bytes(t)
	blocked, err := PersistExit42Restart(f.bb, c6Authority("coder-1"), t.TempDir(), "task-1", 2, 1)
	if err == nil || blocked || strings.TrimSpace(err.Error()) == "" {
		t.Fatalf("past the limit without a pipeline = (%v, %v), want the load error", blocked, err)
	}
	f.requireUnchanged(t, before, "unreadable pipeline")
}
