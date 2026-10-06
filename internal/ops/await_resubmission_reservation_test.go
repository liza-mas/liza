package ops

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// A cancelled provider can leave its await-resubmission running. The same
// registration then releases that wait and claims the task again; the old
// await must not touch the newer claim.

func setupReservationFixture(t *testing.T) (string, *db.Blackboard) {
	t.Helper()
	root := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.History = append(task.History, models.TaskHistoryEntry{Time: now, Event: models.TaskEventRejected, Agent: strPtr("reviewer-1")})
	state.Tasks = []models.Task{task}
	state.Agents["reviewer-1"] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusIdle}
	return root, testhelpers.WriteInitialState(t, stateFile, state)
}

// replaceWithSameAgentClaim applies the supervisor's post-exit release, then
// the same reviewer's next-turn claim of the resubmitted task.
func replaceWithSameAgentClaim(t *testing.T, bb *db.Blackboard) {
	t.Helper()
	agentID := "reviewer-1"
	if err := bb.Modify(func(s *models.State) error {
		task := s.FindTask("task-1")
		task.ReviewingBy, task.ReviewLeaseExpires = nil, nil
		s.ReleaseAgent(agentID)
		task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventClaimReleased, Agent: &agentID})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lease := time.Now().UTC().Add(30 * time.Minute)
	if err := bb.Modify(func(s *models.State) error {
		task := s.FindTask("task-1")
		task.Status = models.TaskStatusReviewing
		task.ReviewingBy, task.ReviewLeaseExpires = &agentID, &lease
		task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventClaimed, Agent: &agentID})
		a := s.Agents[agentID]
		a.Status, a.CurrentTask = models.AgentStatusReviewing, strPtr("task-1")
		s.Agents[agentID] = a
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func requireSameAgentClaimIntact(t *testing.T, bb *db.Blackboard) {
	t.Helper()
	s, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	task, agent := s.FindTask("task-1"), s.Agents["reviewer-1"]
	if task.Status != models.TaskStatusReviewing || task.ReviewingBy == nil || *task.ReviewingBy != "reviewer-1" || task.ReviewLeaseExpires == nil {
		t.Fatalf("old await changed the newer claim: status=%s reviewing_by=%v lease=%v", task.Status, task.ReviewingBy, task.ReviewLeaseExpires)
	}
	if agent.Status != models.AgentStatusReviewing || agent.CurrentTask == nil || *agent.CurrentTask != "task-1" {
		t.Fatalf("old await changed the newer claim's agent: status=%s current_task=%v", agent.Status, agent.CurrentTask)
	}
}

// clearStaleReservation expires the wait's review lease and runs the real
// stale-claim cleanup, which clears ownership without a history event.
func clearStaleReservation(t *testing.T, root string, bb *db.Blackboard) {
	t.Helper()
	if err := bb.Modify(func(s *models.State) error {
		expired := time.Now().UTC().Add(-time.Minute)
		s.FindTask("task-1").ReviewLeaseExpires = &expired
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if cleared, err := ClearStaleReviewClaims(root); err != nil || cleared != 1 {
		t.Fatalf("ClearStaleReviewClaims = %d, %v; want the wait's claim cleared", cleared, err)
	}
}

func requireUnclaimed(t *testing.T, bb *db.Blackboard) {
	t.Helper()
	s, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	task, agent := s.FindTask("task-1"), s.Agents["reviewer-1"]
	if task.ReviewingBy != nil || (agent.CurrentTask != nil && *agent.CurrentTask == "task-1") {
		t.Fatalf("old await reacquired a cleared claim: reviewing_by=%v agent=%s/%v", task.ReviewingBy, agent.Status, agent.CurrentTask)
	}
}

// Each way a wait loses its reservation, with how the aftermath must look.
var reservationLosses = []struct {
	name   string
	lose   func(*testing.T, string, *db.Blackboard)
	verify func(*testing.T, *db.Blackboard)
}{
	{"same_agent_claim", func(t *testing.T, _ string, bb *db.Blackboard) { replaceWithSameAgentClaim(t, bb) }, requireSameAgentClaimIntact},
	{"stale_cleanup", clearStaleReservation, requireUnclaimed},
}

func requireOwnershipLost(t *testing.T, result *AwaitResubmissionResult, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("await error = %v, want final ABORTED", err)
	}
	if result == nil || result.Verdict != ResubmissionAborted {
		t.Fatalf("result = %+v, want final ABORTED (never a retryable TIMEOUT)", result)
	}
}

func startOptionsWait(t *testing.T, root string, timeout time.Duration, opts AwaitResubmissionOptions) (*resubmissionWait, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	wait := &resubmissionWait{done: make(chan struct{})}
	t.Cleanup(func() {
		cancel()
		<-wait.done
	})
	go func() {
		defer close(wait.done)
		wait.result, wait.err = AwaitResubmissionWithOptions(ctx, root, "task-1", "reviewer-1", timeout, opts)
	}()
	return wait, cancel
}

func TestAwaitResubmission_OldAwaitEndsOnLostReservation(t *testing.T) {
	for _, loss := range reservationLosses {
		for _, path := range []string{"watcher", "polling"} {
			t.Run(loss.name+"/"+path, func(t *testing.T) {
				testOldAwaitEndsOnLostReservation(t, path, loss.lose, loss.verify)
			})
		}
	}
}

func testOldAwaitEndsOnLostReservation(t *testing.T, path string, lose func(*testing.T, string, *db.Blackboard), verify func(*testing.T, *db.Blackboard)) {
	root, bb := setupReservationFixture(t)
	if path == "polling" {
		previousWatcher := newAwaitResubmissionWatcher
		t.Cleanup(func() { newAwaitResubmissionWatcher = previousWatcher })
		newAwaitResubmissionWatcher = func(*db.Blackboard) (awaitResubmissionWatcher, error) {
			return nil, errors.New("watcher unavailable")
		}
	}
	wait, _ := startOptionsWait(t, root, 3*time.Second, AwaitResubmissionOptions{FallbackPollInterval: 50 * time.Millisecond})
	waitForReviewOwnership(t, bb, "task-1", "reviewer-1", wait, 10*time.Second)

	lose(t, root, bb)
	lostAt := time.Now()

	result, err := wait.finish()
	requireOwnershipLost(t, result, err)
	verify(t, bb)
	// The wait observes the loss, rather than lingering until its deadline.
	if elapsed := time.Since(lostAt); elapsed > 2*time.Second {
		t.Fatalf("old await ended %v after losing its reservation", elapsed)
	}
}

// The loss lands after the last observation, so only the cleanup on timeout or
// cancellation sees it.
func TestAwaitResubmission_OwnershipLostBeforeCleanupIsFinal(t *testing.T) {
	for _, loss := range reservationLosses {
		for _, exit := range []string{"timeout", "cancellation"} {
			t.Run(loss.name+"/"+exit, func(t *testing.T) {
				testOwnershipLostBeforeCleanup(t, exit, loss.lose, loss.verify)
			})
		}
	}
}

func testOwnershipLostBeforeCleanup(t *testing.T, exit string, lose func(*testing.T, string, *db.Blackboard), verify func(*testing.T, *db.Blackboard)) {
	root, bb := setupReservationFixture(t)
	previousWatcher := newAwaitResubmissionWatcher
	t.Cleanup(func() { newAwaitResubmissionWatcher = previousWatcher })
	newAwaitResubmissionWatcher = func(*db.Blackboard) (awaitResubmissionWatcher, error) {
		return silentAwaitVerdictWatcher{}, nil
	}
	timeout := time.Minute
	if exit == "timeout" {
		timeout = 1500 * time.Millisecond
	}
	wait, cancel := startOptionsWait(t, root, timeout, AwaitResubmissionOptions{AbortPollInterval: time.Hour})
	waitForReviewOwnership(t, bb, "task-1", "reviewer-1", wait, 10*time.Second)

	lose(t, root, bb)
	if exit == "cancellation" {
		cancel()
	}

	result, err := wait.finish()
	requireOwnershipLost(t, result, err)
	verify(t, bb)
}

// A terminal transition may release the waiting reviewer itself (mark-blocked
// does); that is no later claim, so the wait keeps its TERMINAL outcome.
func TestAwaitResubmission_TerminalReleaseKeepsOutcome(t *testing.T) {
	root, bb := setupReservationFixture(t)
	wait := startResubmissionWait(t, root, 10*time.Second)
	waitForReviewOwnership(t, bb, "task-1", "reviewer-1", wait, 10*time.Second)

	if err := bb.Modify(func(s *models.State) error {
		s.FindTask("task-1").Status = models.TaskStatusBlocked
		ReleaseAgentsForTask(s, "task-1")
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	result, err := wait.finish()
	if err != nil || result.Verdict != ResubmissionTerminal || result.TaskStatus != models.TaskStatusBlocked {
		t.Fatalf("blocked wait = %+v, %v; want TERMINAL on BLOCKED", result, err)
	}
}

// A terminal outcome still yields to a later claim: no cleanup of it.
func TestAwaitResubmission_TerminalAfterLaterClaimIsLost(t *testing.T) {
	root, bb := setupReservationFixture(t)
	wait := startResubmissionWait(t, root, 10*time.Second)
	waitForReviewOwnership(t, bb, "task-1", "reviewer-1", wait, 10*time.Second)

	agentID := "reviewer-1"
	if err := bb.Modify(func(s *models.State) error {
		task := s.FindTask("task-1")
		now := time.Now().UTC()
		task.History = append(task.History,
			models.TaskHistoryEntry{Time: now, Event: models.TaskEventClaimReleased, Agent: &agentID},
			models.TaskHistoryEntry{Time: now, Event: models.TaskEventClaimed, Agent: &agentID})
		task.Status = models.TaskStatusBlocked
		a := s.Agents[agentID]
		a.Status = models.AgentStatusReviewing
		s.Agents[agentID] = a
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	result, err := wait.finish()
	requireOwnershipLost(t, result, err)
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatal(readErr)
	}
	task, agent := s.FindTask("task-1"), s.Agents[agentID]
	if task.ReviewingBy == nil || *task.ReviewingBy != agentID || agent.CurrentTask == nil || *agent.CurrentTask != "task-1" {
		t.Fatalf("terminal cleanup cleared the later claim: reviewing_by=%v agent=%v", task.ReviewingBy, agent.CurrentTask)
	}
}

// Stale-claim cleanup leaves no history event; the old reservation must still
// not reclaim the doer's resubmission.
func TestReclaimForReview_StaleClearedReservationCannotReclaim(t *testing.T) {
	root, bb := setupReservationFixture(t)
	ownership, err := acquireReviewOwnershipSnapshot(bb, "reviewer-1", "task-1", nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	clearStaleReservation(t, root, bb)
	if err := bb.Modify(func(s *models.State) error {
		task := s.FindTask("task-1")
		task.Status, task.ReviewCommit, task.Worktree = models.TaskStatusReadyForReview, strPtr("newcommit456"), nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	resolver, _, err := loadResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}

	result, err := reclaimForReview(root, bb, "task-1", "reviewer-1", nil, resolver, s.FindTask("task-1").RolePair, ownership.reservation)

	requireOwnershipLost(t, result, err)
	requireUnclaimed(t, bb)
}

// An operator's forced release ends the old wait for good: it must not report a
// retryable outcome and take the review back.
func TestAwaitResubmission_ForcedReleaseEndsOldAwait(t *testing.T) {
	root, bb := setupReservationFixture(t)
	wait, _ := startOptionsWait(t, root, 3*time.Second, AwaitResubmissionOptions{})
	waitForReviewOwnership(t, bb, "task-1", "reviewer-1", wait, 10*time.Second)

	if _, err := ReleaseClaim(root, "task-1", roles.ClaimReviewer, true, "operator release", "operator"); err != nil {
		t.Fatal(err)
	}
	releasedAt := time.Now()

	result, err := wait.finish()
	requireOwnershipLost(t, result, err)
	if elapsed := time.Since(releasedAt); elapsed > 2*time.Second {
		t.Fatalf("old await ended %v after the forced release", elapsed)
	}
	s, readErr := bb.Read()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if owner := s.FindTask("task-1").ReviewingBy; owner != nil {
		t.Fatalf("old await took the review back: reviewing_by=%s", *owner)
	}
}

// Heartbeat renewal and doer activity during a legitimate wait keep the
// reservation: the doer's own claim events are not reviewer ownership changes.
func TestAwaitResubmission_ReservationSurvivesHeartbeatAndDoerActivity(t *testing.T) {
	root, bb := setupReservationFixture(t)
	wait := startResubmissionWait(t, root, 10*time.Second)
	waitForReviewOwnership(t, bb, "task-1", "reviewer-1", wait, 10*time.Second)

	doer := "coder-1"
	for _, step := range []func(*models.Task){
		func(task *models.Task) {
			renewed := time.Now().UTC().Add(time.Hour)
			task.ReviewLeaseExpires = &renewed
		},
		func(task *models.Task) {
			task.Status, task.AssignedTo = models.TaskStatusImplementing, &doer
			task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventClaimed, Agent: &doer})
		},
		func(task *models.Task) {
			task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventClaimReleased, Agent: &doer})
			task.Status, task.ReviewCommit, task.Worktree = models.TaskStatusReadyForReview, strPtr("newcommit456"), nil
		},
	} {
		if err := bb.Modify(func(s *models.State) error {
			step(s.FindTask("task-1"))
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	result, err := wait.finish()
	if err != nil || result.Verdict != ResubmissionResubmitted {
		t.Fatalf("legitimate wait = %+v, %v; want RESUBMITTED", result, err)
	}
	requireSameAgentClaimIntact(t, bb)
}

// armBeforeReclaim applies change just before the next authority-fenced
// transaction: after the reclaim's pre-read, inside its race window.
func armBeforeReclaim(t *testing.T, change func()) {
	t.Helper()
	previousHook := lifecycleBeforeModifyTestHook
	t.Cleanup(func() { lifecycleBeforeModifyTestHook = previousHook })
	lifecycleBeforeModifyTestHook = func() {
		lifecycleBeforeModifyTestHook = nil
		change()
	}
}

func reservationAuthority(t *testing.T, bb *db.Blackboard) *models.AgentAuthority {
	t.Helper()
	setLifecycleAgentGeneration(t, bb, "reviewer-1", lifecycleGenerationA)
	return &models.AgentAuthority{ID: "reviewer-1", Generation: lifecycleGenerationA}
}

// The newer turn rejected and re-awaited, and the doer resubmitted: its claim
// fields match the old reservation exactly, so only history tells them apart.
// Each invalidating event is checked on its own, so none can regress unnoticed.
func TestReclaimForReview_OldReservationCannotTakeNewerAwait(t *testing.T) {
	for _, tc := range []struct {
		event models.TaskEventName
		actor string
	}{
		{models.TaskEventClaimReleased, "reviewer-1"},
		{models.TaskEventClaimed, "reviewer-1"},
		{models.TaskEventReviewClaimReleased, "operator"},
	} {
		t.Run(string(tc.event), func(t *testing.T) {
			testOldReservationCannotTakeNewerAwait(t, tc.event, tc.actor)
		})
	}
}

func testOldReservationCannotTakeNewerAwait(t *testing.T, event models.TaskEventName, actor string) {
	root, bb := setupReservationFixture(t)
	authority := reservationAuthority(t, bb)
	ownership, err := acquireReviewOwnershipSnapshot(bb, authority.ID, "task-1", authority, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	agentID := authority.ID
	armBeforeReclaim(t, func() {
		if err := bb.Modify(func(s *models.State) error {
			task := s.FindTask("task-1")
			task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: event, Agent: &actor})
			task.Status, task.ReviewCommit = models.TaskStatusReadyForReview, strPtr("newcommit456")
			return nil
		}); err != nil {
			t.Error(err)
		}
	})
	before, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	resolver, _, err := loadResolver(root)
	if err != nil {
		t.Fatal(err)
	}

	result, err := reclaimForReview(root, bb, "task-1", agentID, authority, resolver, before.FindTask("task-1").RolePair, ownership.reservation)

	requireOwnershipLost(t, result, err)
	after, readErr := bb.Read()
	if readErr != nil {
		t.Fatal(readErr)
	}
	task, agent := after.FindTask("task-1"), after.Agents[agentID]
	if task.Status != models.TaskStatusReadyForReview || task.ReviewingBy == nil || *task.ReviewingBy != agentID ||
		agent.Status != models.AgentStatusWaiting || agent.CurrentTask == nil || *agent.CurrentTask != "task-1" {
		t.Fatalf("old reservation changed the newer await: status=%s reviewing_by=%v agent=%s/%v", task.Status, task.ReviewingBy, agent.Status, agent.CurrentTask)
	}
}

// Early resubmission acquires nothing before reclaim, so a failed reclaim has
// nothing to release: a claim that appeared meanwhile is not its own.
func TestAwaitResubmission_EarlyReclaimFailureReleasesNothing(t *testing.T) {
	root, bb := setupReservationFixture(t)
	authority := reservationAuthority(t, bb)
	if err := bb.Modify(func(s *models.State) error {
		s.FindTask("task-1").Status = models.TaskStatusReadyForReview
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	armBeforeReclaim(t, func() {
		lease := time.Now().UTC().Add(30 * time.Minute)
		if err := bb.Modify(func(s *models.State) error {
			task := s.FindTask("task-1")
			task.Status = models.TaskStatusReviewing
			task.ReviewingBy, task.ReviewLeaseExpires = &authority.ID, &lease
			a := s.Agents[authority.ID]
			a.Status, a.CurrentTask = models.AgentStatusReviewing, strPtr("task-1")
			s.Agents[authority.ID] = a
			return nil
		}); err != nil {
			t.Error(err)
		}
	})

	if _, err := AwaitResubmissionWithAuthority(context.Background(), root, "task-1", *authority, time.Minute); err == nil {
		t.Fatal("reclaim of a task claimed meanwhile succeeded")
	}
	requireSameAgentClaimIntact(t, bb)
}

func TestD42RetainedReviewReservationSafety(t *testing.T) {
	for _, exit := range []string{"cancellation", "expired", "stale-generation", "newer-claim", "paused", "stopped", "final-timeout"} {
		t.Run(exit, func(t *testing.T) {
			root, bb := setupReservationFixture(t)
			authority := reservationAuthority(t, bb)
			opts := AwaitResubmissionOptions{PollOnTimeout: true, AbortPollInterval: time.Millisecond}
			result, err := AwaitResubmissionWithAuthorityOptions(context.Background(), root, "task-1", *authority, 20*time.Millisecond, opts)
			if err != nil || result.Verdict != ResubmissionPoll {
				t.Fatalf("first interval = %+v, %v; want retained POLL", result, err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch exit {
			case "cancellation":
				cancel()
			case "expired":
				if err := bb.Modify(func(s *models.State) error {
					past := time.Now().Add(-time.Minute)
					s.FindTask("task-1").ReviewLeaseExpires = &past
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			case "stale-generation":
				setLifecycleAgentGeneration(t, bb, authority.ID, lifecycleGenerationB)
			case "newer-claim":
				replaceWithSameAgentClaim(t, bb)
			case "paused", "stopped":
				if err := bb.Modify(func(s *models.State) error {
					s.Config.Mode = models.SystemModePaused
					if exit == "stopped" {
						s.Config.Mode = models.SystemModeStopped
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			case "final-timeout":
				opts.PollOnTimeout = false
			}
			before, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			result, err = AwaitResubmissionWithAuthorityOptions(ctx, root, "task-1", *authority, 20*time.Millisecond, opts)
			unchanged := false
			switch exit {
			case "expired":
				requireOwnershipLost(t, result, err)
				unchanged = true
			case "stale-generation":
				var fence *AgentAuthorityError
				if !errors.As(err, &fence) {
					t.Fatalf("stale retry error = %T; want authority fence", err)
				}
				unchanged = true
			case "newer-claim":
				if err == nil {
					t.Fatal("retry adopted a newer review claim")
				}
				unchanged = true
			case "cancellation":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled retry error = %v", err)
				}
			default:
				want := ResubmissionTimeout
				if exit == "paused" {
					want = ResubmissionPaused
				} else if exit == "stopped" {
					want = ResubmissionAborted
				}
				if err != nil || result.Verdict != want {
					t.Fatalf("retry = %+v, %v; want %s", result, err, want)
				}
			}
			after, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			if unchanged {
				if !reflect.DeepEqual(before, after) {
					t.Fatal("refused retry changed the expired, fenced, or newer claim")
				}
			} else if after.FindTask("task-1").ReviewingBy != nil || after.Agents[authority.ID].CurrentTask != nil {
				t.Fatal("final exit retained the previous POLL reservation")
			}
		})
	}
}

func TestD42PausedRetainedCleanupCannotReleaseLaterClaim(t *testing.T) {
	root, bb := setupReservationFixture(t)
	authority := reservationAuthority(t, bb)
	result, err := AwaitResubmissionWithAuthorityOptions(context.Background(), root, "task-1", *authority,
		20*time.Millisecond, AwaitResubmissionOptions{PollOnTimeout: true})
	if err != nil || result.Verdict != ResubmissionPoll {
		t.Fatalf("first interval = %+v, %v; want POLL", result, err)
	}
	if err := bb.Modify(func(s *models.State) error { s.Config.Mode = models.SystemModePaused; return nil }); err != nil {
		t.Fatal(err)
	}
	armBeforeReclaim(t, func() { replaceWithSameAgentClaim(t, bb) })
	result, err = AwaitResubmissionWithAuthorityOptions(context.Background(), root, "task-1", *authority,
		time.Second, AwaitResubmissionOptions{PollOnTimeout: true})
	requireOwnershipLost(t, result, err)
	requireSameAgentClaimIntact(t, bb)
}

func TestD42PollRetentionChecksGenerationAtDeadline(t *testing.T) {
	root, bb := setupReservationFixture(t)
	authority := reservationAuthority(t, bb)
	var afterReplacement *models.State
	previous := newAwaitResubmissionWatcher
	t.Cleanup(func() { newAwaitResubmissionWatcher = previous })
	newAwaitResubmissionWatcher = func(*db.Blackboard) (awaitResubmissionWatcher, error) {
		armBeforeReclaim(t, func() {
			setLifecycleAgentGeneration(t, bb, authority.ID, lifecycleGenerationB)
			var err error
			afterReplacement, err = bb.Read()
			if err != nil {
				t.Fatal(err)
			}
		})
		return silentAwaitVerdictWatcher{}, nil
	}
	_, err := AwaitResubmissionWithAuthorityOptions(context.Background(), root, "task-1", *authority,
		20*time.Millisecond, AwaitResubmissionOptions{PollOnTimeout: true, AbortPollInterval: time.Hour})
	var fence *AgentAuthorityError
	if !errors.As(err, &fence) || afterReplacement == nil {
		t.Fatalf("deadline error = %T; want a fenced POLL after replacement", err)
	}
	after, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterReplacement, after) {
		t.Fatal("stale POLL validation changed the replacement registration's reservation")
	}
}
