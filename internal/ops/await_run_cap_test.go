package ops

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// ADR-0245: an agent blocked in await-verdict or await-resubmission holds its
// task or review claim, so it never reaches a task boundary while it waits. The
// wait itself gives the slot up when the run-wide budget needs it.

type awaitRunCapFixture struct {
	root     string
	bb       *db.Blackboard
	resolver *pipeline.Resolver
}

func (f awaitRunCapFixture) read(t *testing.T) *models.State {
	t.Helper()
	state, err := f.bb.Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	return state
}

func awaitRunCapState(t *testing.T, maxAgents int, mutate func(*models.State, time.Time)) awaitRunCapFixture {
	t.Helper()
	t.Cleanup(SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.MaxAgents = maxAgents
	state.Config.MaxInstances = 5
	mutate(state, now)
	bb := testhelpers.WriteInitialState(t, statePath, state)
	cfg, err := pipeline.LoadFrozen(root)
	if err != nil {
		t.Fatalf("load pipeline: %v", err)
	}
	return awaitRunCapFixture{root: root, bb: bb, resolver: pipeline.NewResolver(cfg)}
}

// writeReservation reserves a slot for role as pool auto-repair does.
func writeReservation(t *testing.T, f awaitRunCapFixture, role string) {
	t.Helper()
	if err := SetPoolReservations(f.root, []models.PoolReservation{reserve(role, time.Now().UTC())}); err != nil {
		t.Fatalf("write reservation: %v", err)
	}
}

// waitForAwaitOwnership waits, as waitForReviewOwnership does, until the
// await holds agentID's ownership of taskID, failing if the await ends first.
func waitForAwaitOwnership(t *testing.T, f awaitRunCapFixture, agentID, taskID string, done <-chan struct{}) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		state, err := f.bb.ReadCached()
		if err != nil {
			t.Fatalf("read state while waiting for await ownership: %v", err)
		}
		if current := state.Agents[agentID].CurrentTask; current != nil && *current == taskID {
			testhelpers.WaitForAsyncSetup()
			return
		}
		select {
		case <-done:
			t.Fatalf("await ended before %s took ownership of %s", agentID, taskID)
		case <-ticker.C:
		}
	}
	t.Fatalf("%s never took ownership of %s", agentID, taskID)
}

// waitingDoer is coder-1, registered a minute ago, with task-1 submitted for
// review since.
func waitingDoer(s *models.State, now time.Time) {
	s.Agents["coder-1"] = c6LiveAgent("coder", now.Add(-time.Minute))
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
	task.History = append(task.History, models.TaskHistoryEntry{Time: now, Event: models.TaskEventSubmittedForReview, Agent: strPtr("coder-1")})
	s.Tasks = append(s.Tasks, task)
}

func busyAgent(role, taskID string, now time.Time) models.Agent {
	agent := c6LiveAgent(role, now.Add(-time.Minute))
	agent.CurrentTask = &taskID
	return agent
}

// The issue's case: at the minimum of 3 the orchestrator, a doer waiting for
// its verdict and the reviewer it waits on all fit. The waiting doer keeps its
// session, and the reserved reviewer registers into the free slot.
func TestRunCapAwaitVerdict_MinimumFitsDoerAndReviewer(t *testing.T) {
	f := awaitRunCapState(t, models.MinMaxAgents, func(s *models.State, now time.Time) {
		s.Agents["orchestrator-1"] = c6LiveAgent("orchestrator", now)
		waitingDoer(s, now)
		s.PoolReservations = []models.PoolReservation{reserve("code-reviewer", now)}
	})
	if models.MinMaxAgents != 3 {
		t.Fatalf("MinMaxAgents = %d, want 3: orchestrator, doer and reviewer", models.MinMaxAgents)
	}

	result, err := AwaitVerdict(context.Background(), f.root, "task-1", "coder-1", 300*time.Millisecond)
	if err != nil || result.Verdict != string(VerdictTimeout) {
		t.Fatalf("AwaitVerdict = (%+v, %v), want TIMEOUT: nothing needs the waiting doer's slot", result, err)
	}
	if _, err := RegisterAgent(f.bb, c6Register("code-reviewer", "code-reviewer-1", f.resolver)); err != nil {
		t.Fatalf("reserved reviewer registration at the minimum: %v", err)
	}
	if f.read(t).Agents["code-reviewer-1"].Role != "code-reviewer" {
		t.Fatal("reviewer not registered")
	}
}

// A run whose slots are all held by doers waiting on a reviewer that has none:
// one wait ends and frees its slot; the doer that is still working keeps its.
func TestRunCapAwaitVerdict_WaitingDoerYieldsToReservedReviewer(t *testing.T) {
	f := awaitRunCapState(t, 3, func(s *models.State, now time.Time) {
		waitingDoer(s, now)
		s.Agents["coder-2"] = busyAgent("coder", "task-2", now)
	})

	// The watcher writes the reservation while the doer waits; that write is
	// the state event the wait reacts to.
	var result *AwaitVerdictResult
	var err error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, err = AwaitVerdict(context.Background(), f.root, "task-1", "coder-1", 10*time.Second)
	}()
	waitForAwaitOwnership(t, f, "coder-1", "task-1", done)
	writeReservation(t, f, "code-reviewer")
	<-done
	if err != nil {
		t.Fatalf("AwaitVerdict: %v", err)
	}
	if result.Verdict != string(VerdictAborted) || result.SafeAction != string(SafeActionStop) || !strings.Contains(result.Reason, "max_agents") {
		t.Fatalf("result = %+v, want ABORTED/stop naming the run cap", result)
	}
	state := f.read(t)
	if state.Agents["coder-1"].CurrentTask != nil {
		t.Fatal("the yielding wait kept its task")
	}
	if task := state.FindTask("task-1"); task.Status != models.TaskStatusReadyForReview {
		t.Fatalf("task-1 = %s, want its submission kept for review", task.Status)
	}
	resolver := f.resolver
	if !PoolSurplus(state, resolver, "coder-1", time.Now().UTC()) {
		t.Fatal("the doer that ended its wait does not retire at its next wait")
	}
	if PoolSurplus(state, resolver, "coder-2", time.Now().UTC()) {
		t.Fatal("the working doer was asked to give up its slot")
	}
}

// Without a reservation the full run asks nothing of a waiting doer.
func TestRunCapAwaitVerdict_NoYieldWithoutDemand(t *testing.T) {
	f := awaitRunCapState(t, 3, func(s *models.State, now time.Time) {
		waitingDoer(s, now)
		s.Agents["coder-2"] = busyAgent("coder", "task-2", now)
	})
	result, err := AwaitVerdict(context.Background(), f.root, "task-1", "coder-1", 300*time.Millisecond)
	if err != nil || result.Verdict != string(VerdictTimeout) {
		t.Fatalf("AwaitVerdict = (%+v, %v), want TIMEOUT", result, err)
	}
}

// The reviewer side: reviewers waiting for rework hold their review claims,
// so the coder role the rework needs gets a slot only if one wait yields.
func TestRunCapAwaitResubmission_WaitingReviewerYieldsToReservedCoder(t *testing.T) {
	f := awaitRunCapState(t, 3, func(s *models.State, now time.Time) {
		s.Agents["reviewer-1"] = c6LiveAgent("code-reviewer", now.Add(-time.Minute))
		s.Agents["code-reviewer-2"] = busyAgent("code-reviewer", "task-2", now)
		task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
		task.History = append(task.History, models.TaskHistoryEntry{Time: now, Event: models.TaskEventRejected, Agent: strPtr("reviewer-1")})
		s.Tasks = append(s.Tasks, task)
	})

	wait := startResubmissionWait(t, f.root, 10*time.Second)
	waitForReviewOwnership(t, f.bb, "task-1", "reviewer-1", wait, 10*time.Second)
	writeReservation(t, f, "coder")
	result, err := wait.finish()
	if err != nil {
		t.Fatalf("AwaitResubmission: %v", err)
	}
	if result.Verdict != string(ResubmissionAborted) || result.SafeAction != string(SafeActionStop) || !strings.Contains(result.Reason, "max_agents") {
		t.Fatalf("result = %+v, want ABORTED/stop naming the run cap", result)
	}
	state := f.read(t)
	task := state.FindTask("task-1")
	if task.ReviewingBy != nil || task.Status != models.TaskStatusRejected {
		t.Fatalf("task-1 = %s reviewing_by %v, want the rejection kept and the claim released", task.Status, task.ReviewingBy)
	}
	if !PoolSurplus(state, f.resolver, "reviewer-1", time.Now().UTC()) {
		t.Fatal("the reviewer that ended its wait does not retire at its next wait")
	}
}
