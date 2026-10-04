package ops

import (
	stderrors "errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D75 residual: a doer that dies holding an executing claim, without its exit
// release, leaves the task assigned to it after the lease expires. Any other
// doer must be able to take that stranded claim over into the preserved
// continuation — worktree and branch reused — while a claim whose holder is
// still registered stays protected.

const strandedHolder = "coder-1"

// strandPreservedTask turns the preserved-continuation fixture into the stranded
// shape: executing, assigned to coder-1 with an expired lease, coder-1's row
// still WORKING on it. holderLease is coder-1's registration lease.
func strandPreservedTask(t *testing.T, fixture *preservedInitialClaimFixture, holderLease time.Time) {
	t.Helper()
	expired := time.Now().UTC().Add(-time.Minute)
	if err := db.For(fixture.stateFile).Modify(func(state *models.State) error {
		task := state.FindTask(fixture.taskID)
		holder := strandedHolder
		task.Status = models.TaskStatusImplementing
		task.AssignedTo = &holder
		task.LeaseExpires = &expired
		task.Iteration = 1
		task.Attempt = 1
		agent := state.Agents[strandedHolder]
		agent.Status = models.AgentStatusWorking
		agent.CurrentTask = &task.ID
		agent.Heartbeat = holderLease
		agent.LeaseExpires = &holderLease
		agent.PID = 0
		state.Agents[strandedHolder] = agent
		return nil
	}); err != nil {
		t.Fatalf("strand preserved task: %v", err)
	}
}

type claimOwnershipSnapshot struct {
	task   models.Task
	agents map[string]models.Agent
}

func snapshotClaimOwnership(t *testing.T, stateFile, taskID string) claimOwnershipSnapshot {
	t.Helper()
	state := readClaimStateForTest(t, stateFile)
	return claimOwnershipSnapshot{task: *state.FindTask(taskID), agents: state.Agents}
}

func assertClaimOwnershipUnchanged(t *testing.T, fixture *preservedInitialClaimFixture, before claimOwnershipSnapshot) {
	t.Helper()
	after := snapshotClaimOwnership(t, fixture.stateFile, fixture.taskID)
	if !reflect.DeepEqual(after.task, before.task) {
		t.Errorf("task changed on a refused claim:\nbefore %+v\nafter  %+v", before.task, after.task)
	}
	if !reflect.DeepEqual(after.agents, before.agents) {
		t.Errorf("agents changed on a refused claim:\nbefore %+v\nafter  %+v", before.agents, after.agents)
	}
	if head := testhelpers.MustGit(t, fixture.worktreeDir, "rev-parse", "HEAD"); head != fixture.preservedHead {
		t.Errorf("worktree HEAD = %s, want untouched %s", head, fixture.preservedHead)
	}
}

func countHistoryEvents(task *models.Task, event models.TaskEventName) int {
	count := 0
	for _, entry := range task.History {
		if entry.Event == event {
			count++
		}
	}
	return count
}

// assertStrandedTakeover checks the claim continued the dead holder's branch.
func assertStrandedTakeover(t *testing.T, fixture *preservedInitialClaimFixture, claimant string) {
	t.Helper()
	state := readClaimStateForTest(t, fixture.stateFile)
	task := state.FindTask(fixture.taskID)
	if task.Status != models.TaskStatusImplementing || task.AssignedTo == nil || *task.AssignedTo != claimant {
		t.Fatalf("task = status %s assigned_to %v, want IMPLEMENTING for %s", task.Status, task.AssignedTo, claimant)
	}
	if task.Iteration != 1 || task.Continuation {
		t.Fatalf("stranded takeover charged an interrupted cycle: iteration=%d continuation=%v", task.Iteration, task.Continuation)
	}
	if task.LeaseExpires == nil || !task.LeaseExpires.After(time.Now().UTC()) {
		t.Fatalf("lease_expires = %v, want a fresh lease", task.LeaseExpires)
	}
	if head := testhelpers.MustGit(t, fixture.worktreeDir, "rev-parse", "HEAD"); head != fixture.preservedHead {
		t.Fatalf("worktree HEAD = %s, want the preserved branch head %s", head, fixture.preservedHead)
	}
	if got, err := os.ReadFile(filepath.Join(fixture.worktreeDir, "task.txt")); err != nil || string(got) != fixture.taskContent {
		t.Fatalf("preserved content = %q, %v; want %q", got, err, fixture.taskContent)
	}
	if holder := state.Agents[strandedHolder]; holder.CurrentTask != nil {
		t.Fatalf("dead holder still points at the task: %+v", holder)
	}
	if got := countHistoryEvents(task, models.TaskEventDoerClaimReleased); got != 1 {
		t.Fatalf("doer_claim_released events = %d, want exactly 1", got)
	}
	var release *models.TaskHistoryEntry
	for i := range task.History {
		if task.History[i].Event == models.TaskEventDoerClaimReleased {
			release = &task.History[i]
		}
	}
	if release.PreviousAssignee == nil || *release.PreviousAssignee != strandedHolder {
		t.Fatalf("release previous_assignee = %v, want %s", release.PreviousAssignee, strandedHolder)
	}
	if got := countHistoryEvents(task, models.TaskEventClaimed); got != 1 {
		t.Fatalf("claimed events = %d, want exactly 1", got)
	}
}

func TestClaimTask_TakesOverStrandedExecutingClaim(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	strandPreservedTask(t, fixture, time.Now().UTC().Add(-time.Minute))

	if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, "coder-2"); err != nil {
		t.Fatalf("ClaimTask(coder-2) on a stranded claim = %v, want takeover", err)
	}
	assertStrandedTakeover(t, fixture, "coder-2")
}

func TestClaimTask_StrandedTakeoverRetiresDeadHoldersPreparation(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	strandPreservedTask(t, fixture, time.Now().UTC().Add(-time.Minute))
	// The holder died mid-submit: its submit-for-review reservation remains.
	if err := db.For(fixture.stateFile).Modify(func(state *models.State) error {
		task := state.FindTask(fixture.taskID)
		holder := models.AgentAuthority{ID: strandedHolder, Generation: state.Agents[strandedHolder].Generation}
		request, err := NewLifecycleRequest("submit-for-review", task, holder.ID, &holder, LifecycleRequestOptions{}, nil)
		if err != nil {
			return err
		}
		return PrepareLifecycleRequest(task, request, state.Agents)
	}); err != nil {
		t.Fatalf("record the dead holder's preparation: %v", err)
	}

	if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, "coder-2"); err != nil {
		t.Fatalf("ClaimTask(coder-2) with the dead holder's preparation = %v, want takeover", err)
	}
	assertStrandedTakeover(t, fixture, "coder-2")
}

func TestClaimTask_StrandedClaimWithLiveHolderIsRefused(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	strandPreservedTask(t, fixture, time.Now().UTC().Add(time.Hour))
	before := snapshotClaimOwnership(t, fixture.stateFile, fixture.taskID)

	if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, "coder-2"); err == nil {
		t.Fatal("ClaimTask(coder-2) took over a claim whose holder is registered")
	}
	assertClaimOwnershipUnchanged(t, fixture, before)
}

func TestClaimTask_StrandedTakeoverRefusesBusyClaimant(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	strandPreservedTask(t, fixture, time.Now().UTC().Add(-time.Minute))
	if err := db.For(fixture.stateFile).Modify(func(state *models.State) error {
		other := testhelpers.BuildTaskByStatus("other-task", models.TaskStatusImplementing, time.Now().UTC())
		claimant := "coder-2"
		lease := time.Now().UTC().Add(time.Hour)
		other.AssignedTo = &claimant
		other.LeaseExpires = &lease
		other.Worktree = testhelpers.StringPtr(".worktrees/other-task")
		other.BaseCommit = testhelpers.StringPtr(fixture.originalBase)
		state.Tasks = append(state.Tasks, other)
		agent := state.Agents[claimant]
		agent.Status = models.AgentStatusWorking
		agent.CurrentTask = &other.ID
		state.Agents[claimant] = agent
		return nil
	}); err != nil {
		t.Fatalf("make the claimant busy: %v", err)
	}
	before := snapshotClaimOwnership(t, fixture.stateFile, fixture.taskID)

	if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, "coder-2"); err == nil {
		t.Fatal("ClaimTask(coder-2) took over while coder-2 works on another task")
	}
	assertClaimOwnershipUnchanged(t, fixture, before)
}

func TestClaimTask_PinnedRequestOnStrandedClaimChangesNothing(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	strandPreservedTask(t, fixture, time.Now().UTC().Add(-time.Minute))
	before := snapshotClaimOwnership(t, fixture.stateFile, fixture.taskID)
	opts := LifecycleRequestOptions{RequestID: "pinned-takeover", ExpectedTransition: models.TaskTransitionID(&before.task)}

	_, err := ClaimTaskWithRequest(fixture.projectRoot, fixture.taskID, "coder-2", nil, opts)
	var lifecycleErr *LifecycleError
	if !stderrors.As(err, &lifecycleErr) || lifecycleErr.Outcome.Outcome != models.LifecycleStateChanged {
		t.Fatalf("pinned claim on a stranded task = %T %v, want STATE_CHANGED", err, err)
	}
	assertClaimOwnershipUnchanged(t, fixture, before)
}

// TestClaimTask_ConcurrentStrandedTakeoverReleasesOnce races two doers on the
// same stranded claim: exactly one takes it over, and the release is recorded
// once.
func TestClaimTask_ConcurrentStrandedTakeoverReleasesOnce(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	strandPreservedTask(t, fixture, time.Now().UTC().Add(-time.Minute))
	if err := db.For(fixture.stateFile).Modify(func(state *models.State) error {
		state.Agents["coder-3"] = testhelpers.RegisteredTestAgent("coder")
		return nil
	}); err != nil {
		t.Fatalf("register coder-3: %v", err)
	}

	claimants := []string{"coder-2", "coder-3"}
	errs := make([]error, len(claimants))
	var wg sync.WaitGroup
	for i, claimant := range claimants {
		wg.Add(1)
		go func(i int, claimant string) {
			defer wg.Done()
			_, errs[i] = ClaimTask(fixture.projectRoot, fixture.taskID, claimant)
		}(i, claimant)
	}
	wg.Wait()

	winner := ""
	for i, err := range errs {
		if err == nil {
			if winner != "" {
				t.Fatalf("both %s and %s claimed the stranded task", winner, claimants[i])
			}
			winner = claimants[i]
		}
	}
	if winner == "" {
		t.Fatalf("no claimant took over the stranded task: %v", errs)
	}
	assertStrandedTakeover(t, fixture, winner)
}

// TestTakeOverStrandedDoerClaim_OverlappingObservationReleasesOnce forces the
// overlap the concurrent test only makes likely: both claimants observed the
// stranded claim before either took the task lock. The locked recheck must
// refuse the second one without a second release.
func TestTakeOverStrandedDoerClaim_OverlappingObservationReleasesOnce(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	strandPreservedTask(t, fixture, time.Now().UTC().Add(-time.Minute))
	bb := db.For(fixture.stateFile)
	if err := bb.Modify(func(state *models.State) error {
		state.Agents["coder-3"] = testhelpers.RegisteredTestAgent("coder")
		return nil
	}); err != nil {
		t.Fatalf("register coder-3: %v", err)
	}
	resolver, _, err := loadResolver(fixture.projectRoot)
	if err != nil {
		t.Fatalf("loadResolver() error = %v", err)
	}

	// GIVEN both claimants observed the stranded claim
	observedState, observedTask, err := readTaskState(bb, fixture.taskID)
	if err != nil {
		t.Fatalf("readTaskState() error = %v", err)
	}
	takeOver := func(agentID string) error {
		request, err := NewLifecycleRequest("claim-task", observedTask, agentID, nil, LifecycleRequestOptions{}, nil)
		if err != nil {
			t.Fatalf("NewLifecycleRequest() error = %v", err)
		}
		invocation := &ownershipInvocation{operation: "claim-task", request: request}
		_, _, err = takeOverStrandedDoerClaim(bb, fixture.stateFile, observedState, observedTask, agentID, "coder", resolver, nil, invocation)
		return err
	}
	if err := takeOver("coder-2"); err != nil {
		t.Fatalf("first takeover error = %v", err)
	}
	released := snapshotClaimOwnership(t, fixture.stateFile, fixture.taskID)

	// WHEN the second acts on its stale observation
	err = takeOver("coder-3")

	// THEN it is refused and nothing changes
	var lifecycleErr *LifecycleError
	if !stderrors.As(err, &lifecycleErr) || lifecycleErr.Outcome.Outcome != models.LifecycleStateChanged {
		t.Fatalf("second takeover = %T %v, want STATE_CHANGED", err, err)
	}
	after := snapshotClaimOwnership(t, fixture.stateFile, fixture.taskID)
	if !reflect.DeepEqual(after, released) {
		t.Fatalf("state changed on the refused takeover:\nbefore %+v\nafter  %+v", released, after)
	}
	if got := countHistoryEvents(&after.task, models.TaskEventDoerClaimReleased); got != 1 {
		t.Fatalf("doer_claim_released events = %d, want exactly 1", got)
	}
}
