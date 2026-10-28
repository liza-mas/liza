package ops

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const c6BlockReason = "watchdog stalled"

func c6ReviewedTask(t *testing.T) *c6Fixture {
	t.Helper()
	now := time.Now().UTC()
	return newC6Fixture(t, func(s *models.State) {
		task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
		s.Tasks = []models.Task{task}
		doer := c6LiveAgent("coder", now)
		doer.Status = models.AgentStatusWaiting
		doer.CurrentTask = testhelpers.StringPtr("task-1")
		s.Agents["coder-1"] = doer
		reviewer := c6LiveAgent("code-reviewer", now)
		reviewer.Status = models.AgentStatusReviewing
		reviewer.CurrentTask = testhelpers.StringPtr("task-1")
		s.Agents["code-reviewer-1"] = reviewer
	})
}

func TestC6SupervisorTaskSlotDoerFirst(t *testing.T) {
	task := &models.Task{AssignedTo: testhelpers.StringPtr("a"), ReviewingBy: testhelpers.StringPtr("a")}
	if got := SupervisorTaskSlot(task, "a"); got != roles.ClaimDoer {
		t.Fatalf("slot with both claims = %q, want the doer slot first", got)
	}
	task.AssignedTo = nil
	if got := SupervisorTaskSlot(task, "a"); got != roles.ClaimReviewer {
		t.Fatalf("slot = %q, want reviewer", got)
	}
	if got := SupervisorTaskSlot(task, "b"); got != "" {
		t.Fatalf("slot for another agent = %q, want none", got)
	}
}

func TestC6BlockSupervisorTaskAppliesForDoerAndReleasesOwners(t *testing.T) {
	f := c6WorkingDoer(t, 1, nil)
	task := f.read(t).FindTask("task-1")
	expected := SupervisorBlockExpectation{Owner: SupervisorBlockOwnerDoer, TransitionID: models.TaskTransitionID(task)}

	outcome, err := BlockSupervisorTask(f.bb, f.release.Transitions, c6Authority("coder-1"), "task-1", c6BlockReason, expected)
	if err != nil || outcome != SupervisorBlockApplied {
		t.Fatalf("BlockSupervisorTask = (%v, %v), want applied", outcome, err)
	}
	state := f.read(t)
	blocked := state.FindTask("task-1")
	if blocked.Status != models.TaskStatusBlocked || blocked.AssignedTo != nil || blocked.LeaseExpires != nil ||
		blocked.ReviewingBy != nil || blocked.BlockedReason == nil || *blocked.BlockedReason != c6BlockReason || len(blocked.BlockedQuestions) != 2 {
		t.Fatalf("blocked task = %+v", blocked)
	}
	last := blocked.History[len(blocked.History)-1]
	if last.Event != models.TaskEventBlocked || last.Agent == nil || *last.Agent != "coder-1" || last.Reason == nil || *last.Reason != c6BlockReason {
		t.Fatalf("last history entry = %+v, want the blocked event by the supervisor", last)
	}
	// Agent-side claims on the task are released with it (ReleaseAgentsForTask).
	if agent := state.Agents["coder-1"]; agent.CurrentTask != nil {
		t.Fatalf("agent row still names the blocked task: %+v", agent)
	}
}

func TestC6BlockSupervisorTaskReviewerOwnerBlocksOnlyWhileReviewing(t *testing.T) {
	f := c6ReviewedTask(t)
	task := f.read(t).FindTask("task-1")
	// The review fixture's doer assignment is retained attribution; the slot
	// decision is "doer first", so only a doer-less task matches the reviewer slot.
	if got := SupervisorTaskSlot(task, "code-reviewer-1"); got != roles.ClaimReviewer {
		t.Fatalf("reviewer slot = %q", got)
	}
	expected := SupervisorBlockExpectation{Owner: SupervisorBlockOwnerReviewer, TransitionID: models.TaskTransitionID(task)}
	before := f.bytes(t)

	// The doer slot does not match a reviewer's claim: stale, nothing changes.
	outcome, err := BlockSupervisorTask(f.bb, f.release.Transitions, c6Authority("code-reviewer-1"), "task-1", c6BlockReason,
		SupervisorBlockExpectation{Owner: SupervisorBlockOwnerDoer, TransitionID: expected.TransitionID})
	if err != nil || outcome != SupervisorBlockStale {
		t.Fatalf("doer expectation on a reviewer = (%v, %v), want stale", outcome, err)
	}
	f.requireUnchanged(t, before, "stale slot")

	outcome, err = BlockSupervisorTask(f.bb, f.release.Transitions, c6Authority("code-reviewer-1"), "task-1", c6BlockReason, expected)
	if err != nil || outcome != SupervisorBlockApplied {
		t.Fatalf("reviewer block = (%v, %v), want applied", outcome, err)
	}
	blocked := f.read(t).FindTask("task-1")
	if blocked.Status != models.TaskStatusBlocked || blocked.ReviewingBy != nil || blocked.ReviewLeaseExpires != nil {
		t.Fatalf("blocked review task = %+v", blocked)
	}
}

func TestC6BlockSupervisorTaskStaleObservationsAreNoOps(t *testing.T) {
	for _, tc := range []struct {
		name     string
		owner    SupervisorBlockOwner
		agent    string
		mutate   func(*models.Task)
		token    func(*models.Task) string
		wantStat SupervisorBlockOutcome
	}{
		{name: "lifecycle token moved", owner: SupervisorBlockOwnerDoer, agent: "coder-1", token: func(*models.Task) string { return "moved" }, wantStat: SupervisorBlockStale},
		{name: "owner changed", owner: SupervisorBlockOwnerDoer, agent: "coder-1", mutate: func(task *models.Task) { task.AssignedTo = testhelpers.StringPtr("coder-2") }, wantStat: SupervisorBlockStale},
		{name: "released task now owned", owner: SupervisorBlockOwnerReleased, agent: "coder-1", token: func(task *models.Task) string { return models.TaskTransitionID(task) }, wantStat: SupervisorBlockStale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := c6WorkingDoer(t, 1, nil)
			task := f.read(t).FindTask("task-1")
			token := models.TaskTransitionID(task)
			if tc.token != nil {
				token = tc.token(task)
			}
			if tc.mutate != nil {
				state := f.read(t)
				tc.mutate(state.FindTask("task-1"))
				if err := f.bb.Write(state); err != nil {
					t.Fatalf("rewrite state: %v", err)
				}
			}
			before := f.bytes(t)
			outcome, err := BlockSupervisorTask(f.bb, f.release.Transitions, c6Authority(tc.agent), "task-1", c6BlockReason,
				SupervisorBlockExpectation{Owner: tc.owner, TransitionID: token})
			if err != nil || outcome != tc.wantStat {
				t.Fatalf("BlockSupervisorTask = (%v, %v), want %v", outcome, err, tc.wantStat)
			}
			f.requireUnchanged(t, before, "stale supervisor block")
		})
	}
}

func TestC6BlockSupervisorTaskReleasedContinuationAndMissingTask(t *testing.T) {
	f := c6WorkingDoer(t, 1, nil)
	released, err := ReleaseAgentAfterExit(f.bb, c6Authority("coder-1"), f.release)
	if err != nil {
		t.Fatalf("ReleaseAgentAfterExit: %v", err)
	}
	expected := SupervisorBlockExpectation{Owner: SupervisorBlockOwnerReleased, TransitionID: released.TransitionID}

	before := f.bytes(t)
	outcome, err := BlockSupervisorTask(f.bb, f.release.Transitions, c6Authority("coder-1"), "task-gone", c6BlockReason, expected)
	if err != nil || outcome != SupervisorBlockTaskMissing {
		t.Fatalf("missing task = (%v, %v), want task-missing", outcome, err)
	}
	f.requireUnchanged(t, before, "missing task")

	outcome, err = BlockSupervisorTask(f.bb, f.release.Transitions, c6Authority("coder-1"), "task-1", c6BlockReason, expected)
	if err != nil || outcome != SupervisorBlockApplied {
		t.Fatalf("released continuation = (%v, %v), want applied", outcome, err)
	}
	if blocked := f.read(t).FindTask("task-1"); blocked.Status != models.TaskStatusBlocked || blocked.AssignedTo != nil {
		t.Fatalf("blocked continuation = %+v", blocked)
	}
}

func TestC6BlockSupervisorTaskStaleAuthorityAndIllegalTransition(t *testing.T) {
	f := c6WorkingDoer(t, 1, nil)
	task := f.read(t).FindTask("task-1")
	expected := SupervisorBlockExpectation{Owner: SupervisorBlockOwnerDoer, TransitionID: models.TaskTransitionID(task)}
	before := f.bytes(t)

	_, err := BlockSupervisorTask(f.bb, f.release.Transitions, models.AgentAuthority{ID: "coder-1", Generation: "replaced"}, "task-1", c6BlockReason, expected)
	requireAuthorityError(t, err, "coder-1")
	f.requireUnchanged(t, before, "fenced-out block")

	// An owner match with a transition map that forbids BLOCKED surfaces the
	// transition error and writes nothing.
	_, err = BlockSupervisorTask(f.bb, map[models.TaskStatus][]models.TaskStatus{}, c6Authority("coder-1"), "task-1", c6BlockReason, expected)
	if err == nil || !strings.Contains(err.Error(), "BLOCKED") {
		t.Fatalf("block without a legal transition = %v, want a transition error", err)
	}
	f.requireUnchanged(t, before, "illegal transition")
}
