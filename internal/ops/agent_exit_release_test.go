package ops

import (
	"errors"
	"strings"
	"testing"
	"time"

	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// c6WorkingDoer is coder-1 holding task-1 in the executing status.
func c6WorkingDoer(t *testing.T, iteration int, mutate func(*models.State)) *c6Fixture {
	t.Helper()
	now := time.Now().UTC()
	return newC6Fixture(t, func(s *models.State) {
		task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
		task.Iteration = iteration
		s.Tasks = []models.Task{task}
		agent := c6LiveAgent("coder", now)
		agent.Status = models.AgentStatusWorking
		agent.CurrentTask = testhelpers.StringPtr("task-1")
		s.Agents["coder-1"] = agent
		if mutate != nil {
			mutate(s)
		}
	})
}

func claimReleasedEntries(task *models.Task) int {
	count := 0
	for _, entry := range task.History {
		if entry.Event == models.TaskEventClaimReleased {
			count++
		}
	}
	return count
}

func TestC6ReleaseAgentAfterExitReleasesDoerAndReturnsWitness(t *testing.T) {
	f := c6WorkingDoer(t, 2, nil)

	released, err := ReleaseAgentAfterExit(f.bb, c6Authority("coder-1"), f.release)
	if err != nil {
		t.Fatalf("ReleaseAgentAfterExit: %v", err)
	}
	state := f.read(t)
	task := state.FindTask("task-1")
	if task.Status != models.TaskStatusReady || task.AssignedTo != nil || task.LeaseExpires != nil || !task.Continuation {
		t.Fatalf("released task = status %s assigned %v lease %v continuation %v, want READY unowned continuation",
			task.Status, task.AssignedTo, task.LeaseExpires, task.Continuation)
	}
	if claimReleasedEntries(task) != 1 {
		t.Fatalf("history = %+v, want exactly one claim_released entry", task.History)
	}
	if want := (ReleasedTaskClaim{TaskID: "task-1", TransitionID: models.TaskTransitionID(task)}); released != want {
		t.Fatalf("witness = %+v, want the task's post-release token %+v", released, want)
	}
	agent := state.Agents["coder-1"]
	if agent.Status != models.AgentStatusIdle || agent.CurrentTask != nil {
		t.Fatalf("agent = %+v, want IDLE with no task", agent)
	}
}

func TestC6ReleaseAgentAfterExitFirstIterationIsNotAContinuation(t *testing.T) {
	f := c6WorkingDoer(t, 0, nil)
	if _, err := ReleaseAgentAfterExit(f.bb, c6Authority("coder-1"), f.release); err != nil {
		t.Fatalf("ReleaseAgentAfterExit: %v", err)
	}
	if f.read(t).FindTask("task-1").Continuation {
		t.Fatal("a release before the first completed iteration must not mark a continuation")
	}
}

func TestC6ReleaseAgentAfterExitHandoffKeepsClaimAndWaitingReviewerReleases(t *testing.T) {
	t.Run("handoff with a task only renews the heartbeat", func(t *testing.T) {
		f := c6WorkingDoer(t, 1, func(s *models.State) {
			agent := s.Agents["coder-1"]
			agent.Status = models.AgentStatusHandoff
			agent.Heartbeat = time.Now().UTC().Add(-time.Minute)
			s.Agents["coder-1"] = agent
		})
		stale := f.read(t).Agents["coder-1"].Heartbeat
		released, err := ReleaseAgentAfterExit(f.bb, c6Authority("coder-1"), f.release)
		if err != nil || released != (ReleasedTaskClaim{}) {
			t.Fatalf("handoff exit = (%+v, %v), want an empty witness", released, err)
		}
		state := f.read(t)
		agent := state.Agents["coder-1"]
		if agent.Status != models.AgentStatusHandoff || agent.CurrentTask == nil || !agent.Heartbeat.After(stale) {
			t.Fatalf("agent = %+v, want HANDOFF kept with a renewed heartbeat", agent)
		}
		if task := state.FindTask("task-1"); task.AssignedTo == nil || claimReleasedEntries(task) != 0 {
			t.Fatal("a handoff exit must keep the doer claim")
		}
	})
	t.Run("waiting reviewer releases its review claim", func(t *testing.T) {
		now := time.Now().UTC()
		f := newC6Fixture(t, func(s *models.State) {
			task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
			s.Tasks = []models.Task{task}
			agent := c6LiveAgent("code-reviewer", now)
			agent.Status = models.AgentStatusWaiting
			agent.CurrentTask = testhelpers.StringPtr("task-1")
			s.Agents["code-reviewer-1"] = agent
		})
		released, err := ReleaseAgentAfterExit(f.bb, c6Authority("code-reviewer-1"), f.release)
		if err != nil {
			t.Fatalf("ReleaseAgentAfterExit: %v", err)
		}
		state := f.read(t)
		task := state.FindTask("task-1")
		if task.ReviewingBy != nil || task.ReviewLeaseExpires != nil || task.Status != models.TaskStatusReadyForReview {
			t.Fatalf("task = status %s reviewing_by %v, want a released submitted review", task.Status, task.ReviewingBy)
		}
		if released.TaskID != "task-1" || released.TransitionID != models.TaskTransitionID(task) {
			t.Fatalf("witness = %+v", released)
		}
		if state.Agents["code-reviewer-1"].Status != models.AgentStatusReviewing && state.Agents["code-reviewer-1"].CurrentTask != nil {
			t.Fatalf("reviewer row = %+v", state.Agents["code-reviewer-1"])
		}
	})
}

func TestC6ReleaseAgentAfterExitMissingAgentAndStaleAuthorityChangeNothing(t *testing.T) {
	f := c6WorkingDoer(t, 1, nil)
	before := f.bytes(t)

	_, err := ReleaseAgentAfterExit(f.bb, models.AgentAuthority{ID: "coder-1", Generation: "replaced"}, f.release)
	requireAuthorityError(t, err, "coder-1")
	f.requireUnchanged(t, before, "stale exit release")

	_, err = ReleaseAgentAfterExit(f.bb, c6Authority("coder-9"), f.release)
	requireAuthorityError(t, err, "coder-9")

	// Fail closed: missing transitions leave both task-side and agent-side ownership.
	_, err = ReleaseAgentAfterExit(f.bb, c6Authority("coder-1"), ClaimReleasePipeline{Resolver: f.resolver})
	if err == nil || !strings.Contains(err.Error(), "pipeline transitions not loaded") {
		t.Fatalf("release without transitions = %v, want a fail-closed refusal", err)
	}
	f.requireUnchanged(t, before, "release without transitions")
}

func TestC6ReleaseClaimForHaltReleasesOnlyTheNamedHeldClaim(t *testing.T) {
	t.Run("doer claim becomes a continuation and the agent idles", func(t *testing.T) {
		f := c6WorkingDoer(t, 1, nil)
		if err := ReleaseClaimForHalt(f.bb, c6Authority("coder-1"), f.release, "task-1"); err != nil {
			t.Fatalf("ReleaseClaimForHalt: %v", err)
		}
		state := f.read(t)
		task := state.FindTask("task-1")
		if task.Status != models.TaskStatusReady || task.AssignedTo != nil || !task.Continuation || claimReleasedEntries(task) != 1 {
			t.Fatalf("task = %+v, want a released continuation with one claim_released entry", task)
		}
		if agent := state.Agents["coder-1"]; agent.Status != models.AgentStatusIdle || agent.CurrentTask != nil {
			t.Fatalf("agent = %+v, want IDLE", agent)
		}
	})
	t.Run("another task name leaves the claim and still idles the agent", func(t *testing.T) {
		f := c6WorkingDoer(t, 1, nil)
		if err := ReleaseClaimForHalt(f.bb, c6Authority("coder-1"), f.release, "task-other"); err != nil {
			t.Fatalf("ReleaseClaimForHalt: %v", err)
		}
		state := f.read(t)
		if task := state.FindTask("task-1"); task.AssignedTo == nil || claimReleasedEntries(task) != 0 {
			t.Fatal("a claim on another task must not be released")
		}
		if agent := state.Agents["coder-1"]; agent.Status != models.AgentStatusIdle || agent.CurrentTask != nil {
			t.Fatalf("agent = %+v, want IDLE", agent)
		}
	})
	t.Run("missing agent is not found and stale authority is refused", func(t *testing.T) {
		f := c6WorkingDoer(t, 1, nil)
		before := f.bytes(t)
		requireAuthorityError(t, ReleaseClaimForHalt(f.bb, models.AgentAuthority{ID: "coder-1", Generation: "replaced"}, f.release, "task-1"), "coder-1")
		f.requireUnchanged(t, before, "stale halt release")
	})
}

func TestC6ResetAgentToIdleAndEnterOrchestration(t *testing.T) {
	now := time.Now().UTC().Add(-time.Hour)
	f := newC6Fixture(t, func(s *models.State) {
		s.Config.LeaseDuration = 600
		agent := c6LiveAgent("orchestrator", now)
		agent.Status = models.AgentStatusWorking
		agent.CurrentTask = testhelpers.StringPtr("task-1")
		s.Agents["orchestrator-1"] = agent
	})
	authority := c6Authority("orchestrator-1")

	if err := ResetAgentToIdle(f.bb, authority); err != nil {
		t.Fatalf("ResetAgentToIdle: %v", err)
	}
	agent := f.read(t).Agents["orchestrator-1"]
	if agent.Status != models.AgentStatusIdle || agent.CurrentTask != nil || !agent.Heartbeat.After(now) {
		t.Fatalf("agent after reset = %+v", agent)
	}

	if err := EnterOrchestration(f.bb, authority); err != nil {
		t.Fatalf("EnterOrchestration: %v", err)
	}
	agent = f.read(t).Agents["orchestrator-1"]
	if agent.Status != models.AgentStatusPlanning || agent.CurrentTask == nil || *agent.CurrentTask != "planning" {
		t.Fatalf("agent after orchestration = %+v", agent)
	}
	// The renewed lease uses the configured duration, defaulting when unset.
	if agent.LeaseExpires == nil || agent.LeaseExpires.Sub(agent.Heartbeat) != 600*time.Second {
		t.Fatalf("lease %v is not heartbeat + the configured 600s", agent.LeaseExpires)
	}
	state := f.read(t)
	state.Config.LeaseDuration = 0
	if err := f.bb.Write(state); err != nil {
		t.Fatalf("rewrite state: %v", err)
	}
	if err := EnterOrchestration(f.bb, authority); err != nil {
		t.Fatalf("EnterOrchestration with the default lease: %v", err)
	}
	agent = f.read(t).Agents["orchestrator-1"]
	if want := time.Duration(models.DefaultLeaseDurationSeconds) * time.Second; agent.LeaseExpires.Sub(agent.Heartbeat) != want {
		t.Fatalf("default lease = %v, want %v", agent.LeaseExpires.Sub(agent.Heartbeat), want)
	}

	before := f.bytes(t)
	stale := models.AgentAuthority{ID: "orchestrator-1", Generation: "replaced"}
	requireAuthorityError(t, ResetAgentToIdle(f.bb, stale), "orchestrator-1")
	requireAuthorityError(t, EnterOrchestration(f.bb, stale), "orchestrator-1")
	f.requireUnchanged(t, before, "stale reset/orchestration")
	missing := EnterOrchestration(f.bb, c6Authority("orchestrator-9"))
	var notFound *lizaerrors.NotFoundError
	if !errors.As(missing, &notFound) && !IsAgentAuthorityError(missing) {
		t.Fatalf("missing agent error = %v", missing)
	}
}

func TestC6UnregisterAgentReleasesEveryClaimAndRemovesRow(t *testing.T) {
	now := time.Now().UTC()
	f := newC6Fixture(t, func(s *models.State) {
		doing := testhelpers.BuildTaskByStatus("task-doing", models.TaskStatusImplementing, now)
		doing.Iteration = 1
		reviewing := testhelpers.BuildTaskByStatus("task-reviewing", models.TaskStatusReviewing, now)
		reviewing.AssignedTo = testhelpers.StringPtr("coder-2")
		reviewing.ReviewingBy = testhelpers.StringPtr("code-reviewer-1")
		s.Tasks = []models.Task{doing, reviewing}
		agent := c6LiveAgent("coder", now)
		agent.Status = models.AgentStatusWorking
		agent.CurrentTask = testhelpers.StringPtr("task-doing")
		s.Agents["coder-1"] = agent
		s.Agents["coder-2"] = c6LiveAgent("coder", now)
		reviewer := c6LiveAgent("code-reviewer", now)
		reviewer.Status = models.AgentStatusReviewing
		reviewer.CurrentTask = testhelpers.StringPtr("task-reviewing")
		s.Agents["code-reviewer-1"] = reviewer
	})
	before := f.bytes(t)

	requireAuthorityError(t, UnregisterAgent(f.bb, models.AgentAuthority{ID: "coder-1", Generation: "replaced"}, f.release), "coder-1")
	f.requireUnchanged(t, before, "stale unregister")

	if err := UnregisterAgent(f.bb.Patient(), c6Authority("coder-1"), f.release); err != nil {
		t.Fatalf("UnregisterAgent: %v", err)
	}
	state := f.read(t)
	if _, ok := state.Agents["coder-1"]; ok {
		t.Fatal("the agent row survived unregistration")
	}
	if _, ok := state.Agents["coder-2"]; !ok {
		t.Fatal("unregistration removed a peer")
	}
	doing := state.FindTask("task-doing")
	if doing.Status != models.TaskStatusReady || doing.AssignedTo != nil || !doing.Continuation || claimReleasedEntries(doing) != 1 {
		t.Fatalf("doer task = %+v, want a released continuation", doing)
	}
	if state.FindTask("task-reviewing").ReviewingBy == nil {
		t.Fatal("unregistering the doer released a reviewer's claim")
	}
	if err := UnregisterAgent(f.bb, c6Authority("code-reviewer-1"), f.release); err != nil {
		t.Fatalf("UnregisterAgent (reviewer): %v", err)
	}
	state = f.read(t)
	reviewing := state.FindTask("task-reviewing")
	if reviewing.ReviewingBy != nil || reviewing.Status != models.TaskStatusReadyForReview || claimReleasedEntries(reviewing) != 1 {
		t.Fatalf("review task = %+v, want the review claim released to the submitted status", reviewing)
	}

	// With the row gone, the old generation holds no authority any more.
	requireAuthorityError(t, UnregisterAgent(f.bb, c6Authority("coder-1"), f.release), "coder-1")
}

func TestC6UnregisterAgentFailsClosedWhenTransitionsMissing(t *testing.T) {
	f := c6WorkingDoer(t, 1, nil)
	before := f.bytes(t)
	err := UnregisterAgent(f.bb, c6Authority("coder-1"), ClaimReleasePipeline{Resolver: f.resolver})
	if err == nil || !strings.Contains(err.Error(), "pipeline transitions not loaded") {
		t.Fatalf("unregister without transitions = %v, want a fail-closed refusal", err)
	}
	f.requireUnchanged(t, before, "unregister without transitions")
}
