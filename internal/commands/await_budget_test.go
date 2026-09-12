package commands

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/paths"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const (
	boundedAwaitTaskID     = "task-1"
	boundedAwaitCoderID    = "coder-1"
	boundedAwaitReviewerID = "code-reviewer-1"

	// maxSafeForegroundAwaitInterval leaves scheduling headroom below the
	// provider threshold that backgrounds long-running foreground commands.
	maxSafeForegroundAwaitInterval = 120 * time.Second
)

type boundedAwaitFixture struct {
	projectRoot string
	bb          *db.Blackboard
}

type asyncAwaitResult[T any] struct {
	result T
	err    error
}

func TestMaxAwaitIntervalStaysBelowForegroundTransportLimit(t *testing.T) {
	if maxAwaitInterval >= maxSafeForegroundAwaitInterval {
		t.Fatalf("maxAwaitInterval = %s, must stay below foreground transport safety limit %s",
			maxAwaitInterval, maxSafeForegroundAwaitInterval)
	}
}

func TestAwaitResubmissionWithOptions_ForwardsOperationValidation(t *testing.T) {
	result, err := AwaitResubmissionWithOptions(
		t.TempDir(),
		"",
		boundedAwaitReviewerID,
		time.Second,
		AwaitResubmissionOptions{
			AbortPollInterval:    5 * time.Millisecond,
			FallbackPollInterval: 5 * time.Millisecond,
		},
	)
	if result != nil {
		t.Fatalf("result = %#v, want nil on precondition failure", result)
	}
	var preconditionErr *ops.PreconditionError
	if !errors.As(err, &preconditionErr) {
		t.Fatalf("error = %T %v, want *ops.PreconditionError", err, err)
	}
}

func TestAwaitVerdictWithBudget_DecreasesUntilExhausted(t *testing.T) {
	remaining := 250 * time.Second
	wantVerdicts := []string{ops.VerdictPoll, ops.VerdictPoll, ops.VerdictTimeout}
	wantRemaining := []int{150, 50, 0}
	wantIntervals := []time.Duration{100 * time.Second, 100 * time.Second, 50 * time.Second}

	for i, wantVerdict := range wantVerdicts {
		var gotInterval time.Duration
		result, err := awaitVerdictWithBudget(remaining, 100*time.Second, func(interval time.Duration) (*ops.AwaitVerdictResult, error) {
			gotInterval = interval
			return &ops.AwaitVerdictResult{Verdict: ops.VerdictTimeout}, nil
		})
		if err != nil {
			t.Fatalf("call %d: awaitVerdictWithBudget error: %v", i+1, err)
		}
		if gotInterval != wantIntervals[i] {
			t.Errorf("call %d: interval = %s, want %s", i+1, gotInterval, wantIntervals[i])
		}
		if result.Verdict != wantVerdict {
			t.Errorf("call %d: verdict = %q, want %q", i+1, result.Verdict, wantVerdict)
		}
		if result.TimeoutSeconds != wantRemaining[i] {
			t.Errorf("call %d: timeout_seconds = %d, want %d", i+1, result.TimeoutSeconds, wantRemaining[i])
		}
		remaining = time.Duration(result.TimeoutSeconds) * time.Second
	}
}

func TestAwaitResubmissionWithBudget_DecreasesUntilExhausted(t *testing.T) {
	remaining := 250 * time.Second
	wantVerdicts := []string{ops.ResubmissionPoll, ops.ResubmissionPoll, ops.ResubmissionTimeout}
	wantRemaining := []int{150, 50, 0}
	wantIntervals := []time.Duration{100 * time.Second, 100 * time.Second, 50 * time.Second}

	for i, wantVerdict := range wantVerdicts {
		var gotInterval time.Duration
		result, err := awaitResubmissionWithBudget(remaining, 100*time.Second, func(interval time.Duration) (*ops.AwaitResubmissionResult, error) {
			gotInterval = interval
			return &ops.AwaitResubmissionResult{Verdict: ops.ResubmissionTimeout}, nil
		})
		if err != nil {
			t.Fatalf("call %d: awaitResubmissionWithBudget error: %v", i+1, err)
		}
		if gotInterval != wantIntervals[i] {
			t.Errorf("call %d: interval = %s, want %s", i+1, gotInterval, wantIntervals[i])
		}
		if result.Verdict != wantVerdict {
			t.Errorf("call %d: verdict = %q, want %q", i+1, result.Verdict, wantVerdict)
		}
		if result.TimeoutSeconds != wantRemaining[i] {
			t.Errorf("call %d: timeout_seconds = %d, want %d", i+1, result.TimeoutSeconds, wantRemaining[i])
		}
		remaining = time.Duration(result.TimeoutSeconds) * time.Second
	}
}

func TestAwaitWithBudget_PreservesNonTimeoutOutcome(t *testing.T) {
	result, err := awaitVerdictWithBudget(250*time.Second, 100*time.Second, func(interval time.Duration) (*ops.AwaitVerdictResult, error) {
		return &ops.AwaitVerdictResult{Verdict: ops.VerdictApproved}, nil
	})
	if err != nil {
		t.Fatalf("awaitVerdictWithBudget error: %v", err)
	}
	if result.Verdict != ops.VerdictApproved {
		t.Errorf("verdict = %q, want %q", result.Verdict, ops.VerdictApproved)
	}
	if result.TimeoutSeconds != 0 {
		t.Errorf("timeout_seconds = %d, want 0 for non-POLL outcome", result.TimeoutSeconds)
	}
}

func TestAwaitResubmissionWithBudget_OmitsBudgetFromNonTimeoutOutcome(t *testing.T) {
	result, err := awaitResubmissionWithBudget(250*time.Second, 100*time.Second, func(interval time.Duration) (*ops.AwaitResubmissionResult, error) {
		return &ops.AwaitResubmissionResult{Verdict: ops.ResubmissionResubmitted}, nil
	})
	if err != nil {
		t.Fatalf("awaitResubmissionWithBudget error: %v", err)
	}
	if result.Verdict != ops.ResubmissionResubmitted {
		t.Errorf("verdict = %q, want %q", result.Verdict, ops.ResubmissionResubmitted)
	}
	if result.TimeoutSeconds != 0 {
		t.Errorf("timeout_seconds = %d, want 0 for non-POLL outcome", result.TimeoutSeconds)
	}
}

func TestAwaitCompositionWithInterval_BoundedBudgetLifecycle(t *testing.T) {
	verdictFixture := setupBoundedAwaitFixture(
		t,
		models.TaskStatusReadyForReview,
		boundedAwaitCoderID,
		"coder",
		models.AgentStatusWaiting,
		models.TaskEventSubmittedForReview,
	)
	resubmissionFixture := setupBoundedAwaitFixture(
		t,
		models.TaskStatusRejected,
		boundedAwaitReviewerID,
		"code-reviewer",
		models.AgentStatusIdle,
		models.TaskEventRejected,
	)

	const interval = time.Second
	firstStarted := time.Now()
	verdictCall := startAsyncAwait(func() (*AwaitVerdictResult, error) {
		return awaitVerdictWithInterval(
			verdictFixture.projectRoot, boundedAwaitTaskID, boundedAwaitCoderID, 2*time.Second, interval)
	})
	resubmissionCall := startAsyncAwait(func() (*AwaitResubmissionResult, error) {
		return awaitResubmissionWithInterval(
			resubmissionFixture.projectRoot, boundedAwaitTaskID, boundedAwaitReviewerID, 2*time.Second, interval)
	})

	verdictResult := receiveAsyncAwait(t, verdictCall, 3*time.Second)
	resubmissionResult := receiveAsyncAwait(t, resubmissionCall, 3*time.Second)
	if elapsed := time.Since(firstStarted); elapsed >= 3*time.Second {
		t.Fatalf("first bounded await round took %s, want < 3s", elapsed)
	}
	if verdictResult.Verdict != ops.VerdictPoll || verdictResult.TimeoutSeconds != 1 {
		t.Fatalf("AwaitVerdict result = (%q, %d), want (%q, 1)",
			verdictResult.Verdict, verdictResult.TimeoutSeconds, ops.VerdictPoll)
	}
	if resubmissionResult.Verdict != ops.ResubmissionPoll || resubmissionResult.TimeoutSeconds != 1 {
		t.Fatalf("AwaitResubmission result = (%q, %d), want (%q, 1)",
			resubmissionResult.Verdict, resubmissionResult.TimeoutSeconds, ops.ResubmissionPoll)
	}
	assertBoundedAwaitOwnershipReleased(t, verdictFixture, false)
	assertBoundedAwaitOwnershipReleased(t, resubmissionFixture, true)

	verdictCall = startAsyncAwait(func() (*AwaitVerdictResult, error) {
		return awaitVerdictWithInterval(
			verdictFixture.projectRoot,
			boundedAwaitTaskID,
			boundedAwaitCoderID,
			time.Duration(verdictResult.TimeoutSeconds)*time.Second,
			interval,
		)
	})
	resubmissionCall = startAsyncAwait(func() (*AwaitResubmissionResult, error) {
		return awaitResubmissionWithInterval(
			resubmissionFixture.projectRoot,
			boundedAwaitTaskID,
			boundedAwaitReviewerID,
			time.Duration(resubmissionResult.TimeoutSeconds)*time.Second,
			interval,
		)
	})

	verdictResult = receiveAsyncAwait(t, verdictCall, 3*time.Second)
	resubmissionResult = receiveAsyncAwait(t, resubmissionCall, 3*time.Second)
	if verdictResult.Verdict != ops.VerdictTimeout || verdictResult.TimeoutSeconds != 0 {
		t.Fatalf("final AwaitVerdict result = (%q, %d), want (%q, 0)",
			verdictResult.Verdict, verdictResult.TimeoutSeconds, ops.VerdictTimeout)
	}
	if resubmissionResult.Verdict != ops.ResubmissionTimeout || resubmissionResult.TimeoutSeconds != 0 {
		t.Fatalf("final AwaitResubmission result = (%q, %d), want (%q, 0)",
			resubmissionResult.Verdict, resubmissionResult.TimeoutSeconds, ops.ResubmissionTimeout)
	}
	assertBoundedAwaitOwnershipReleased(t, verdictFixture, false)
	assertBoundedAwaitOwnershipReleased(t, resubmissionFixture, true)
}

func setupBoundedAwaitFixture(
	t *testing.T,
	status models.TaskStatus,
	agentID string,
	role string,
	agentStatus models.AgentStatus,
	event models.TaskEventName,
) boundedAwaitFixture {
	t.Helper()

	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus(boundedAwaitTaskID, status, time.Now().UTC())
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  time.Now().UTC(),
		Event: event,
		Agent: testhelpers.StringPtr(agentID),
	})
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = models.Agent{Role: role, Status: agentStatus}

	return boundedAwaitFixture{
		projectRoot: projectRoot,
		bb:          testhelpers.WriteInitialState(t, statePath, state),
	}
}

func startAsyncAwait[T any](call func() (T, error)) <-chan asyncAwaitResult[T] {
	result := make(chan asyncAwaitResult[T], 1)
	go func() {
		awaitResult, err := call()
		result <- asyncAwaitResult[T]{result: awaitResult, err: err}
	}()
	return result
}

func receiveAsyncAwait[T any](
	t *testing.T,
	call <-chan asyncAwaitResult[T],
	guard time.Duration,
) T {
	t.Helper()
	select {
	case outcome := <-call:
		if outcome.err != nil {
			t.Fatalf("await returned error: %v", outcome.err)
		}
		return outcome.result
	case <-time.After(guard):
		t.Fatalf("await did not return within %s", guard)
		var zero T
		return zero
	}
}

func assertBoundedAwaitOwnershipReleased(
	t *testing.T,
	fixture boundedAwaitFixture,
	reviewer bool,
) {
	t.Helper()
	state, err := fixture.bb.Read()
	if err != nil {
		t.Fatalf("read state after await: %v", err)
	}
	task := state.FindTask(boundedAwaitTaskID)
	if task == nil {
		t.Fatal("task not found after await")
	}

	if reviewer {
		if state.Agents[boundedAwaitReviewerID].CurrentTask != nil {
			t.Error("reviewer current_task should be nil after await")
		}
		if task.ReviewingBy != nil {
			t.Error("task reviewing_by should be nil after await")
		}
		if task.ReviewLeaseExpires != nil {
			t.Error("task review_lease_expires should be nil after await")
		}
	} else if state.Agents[boundedAwaitCoderID].CurrentTask != nil {
		t.Error("doer current_task should be nil after await")
	}
}

// TestAwaitVerdict_BudgetExhaustionReleasesDoerClaim verifies that a doer whose
// wait budget runs out leaves no claim behind. The session ends at that point and
// never resumes, so a retained assigned_to would pin the task to a departed agent
// until its lease lapsed.
func TestAwaitVerdict_BudgetExhaustionReleasesDoerClaim(t *testing.T) {
	fixture := setupBoundedAwaitFixture(
		t,
		models.TaskStatusReadyForReview,
		boundedAwaitCoderID,
		"coder",
		models.AgentStatusWaiting,
		models.TaskEventSubmittedForReview,
	)

	// Give the task a live doer claim plus the submitted-attempt state a review
	// in flight depends on, as it would have after ClaimTask + submit-for-review.
	liveLease := time.Now().Add(30 * time.Minute)
	reviewLease := time.Now().Add(30 * time.Minute)
	if err := fixture.bb.Modify(func(s *models.State) error {
		task := s.FindTask(boundedAwaitTaskID)
		task.AssignedTo = testhelpers.StringPtr(boundedAwaitCoderID)
		task.LeaseExpires = &liveLease
		task.Worktree = testhelpers.StringPtr(".worktrees/" + boundedAwaitTaskID)
		task.BaseCommit = testhelpers.StringPtr("abc1234")
		task.ReviewCommit = testhelpers.StringPtr("def5678")
		task.Output = []models.OutputEntry{{Desc: "implementation notes"}}
		task.Iteration = 3
		task.ReviewingBy = testhelpers.StringPtr(boundedAwaitReviewerID)
		task.ReviewLeaseExpires = &reviewLease
		return nil
	}); err != nil {
		t.Fatalf("seed doer claim: %v", err)
	}

	// remaining == interval, so the single call exhausts the budget outright.
	result, err := awaitVerdictWithInterval(
		fixture.projectRoot, boundedAwaitTaskID, boundedAwaitCoderID, time.Second, time.Second)
	if err != nil {
		t.Fatalf("awaitVerdictWithInterval: %v", err)
	}
	if result.Verdict != ops.VerdictTimeout {
		t.Fatalf("verdict = %q, want %q", result.Verdict, ops.VerdictTimeout)
	}

	state, err := fixture.bb.Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	task := state.FindTask(boundedAwaitTaskID)
	if task == nil {
		t.Fatal("task not found after await")
	}
	if task.AssignedTo != nil {
		t.Errorf("assigned_to = %q, want nil after budget exhaustion", *task.AssignedTo)
	}
	if task.LeaseExpires != nil {
		t.Error("lease_expires should be nil after budget exhaustion")
	}
	if task.Status != models.TaskStatusReadyForReview {
		t.Errorf("status = %q, want it left submitted for the reviewer", task.Status)
	}

	// The submitted attempt belongs to the review in flight, not to the departed
	// doer. Clearing any of these strands the reviewer: submit-verdict rejects a
	// missing review_commit, and a missing worktree bypasses boundary validation.
	if task.Worktree == nil {
		t.Error("worktree cleared; the reviewer would be handed an empty worktree")
	}
	if task.BaseCommit == nil {
		t.Error("base_commit cleared; the review boundary is destroyed")
	}
	if task.ReviewCommit == nil {
		t.Error("review_commit cleared; a later verdict would fail validation")
	}
	if len(task.Output) != 1 {
		t.Errorf("output entries = %d, want 1 preserved; the submitted attempt lost its result", len(task.Output))
	}
	if task.Iteration != 3 {
		t.Errorf("iteration = %d, want 3 preserved", task.Iteration)
	}
	if task.ReviewingBy == nil || *task.ReviewingBy != boundedAwaitReviewerID {
		t.Errorf("reviewing_by = %v, want the active reviewer untouched", task.ReviewingBy)
	}
	if task.ReviewLeaseExpires == nil {
		t.Error("review_lease_expires cleared; the active review lost its lease")
	}
}

func TestAwaitVerdictWithAuthority_BudgetCleanupPropagatesGenerationFence(t *testing.T) {
	fixture := setupBoundedAwaitFixture(
		t,
		models.TaskStatusReadyForReview,
		boundedAwaitCoderID,
		"coder",
		models.AgentStatusWaiting,
		models.TaskEventSubmittedForReview,
	)

	liveLease := time.Now().Add(30 * time.Minute)
	if err := fixture.bb.Modify(func(state *models.State) error {
		task := state.FindTask(boundedAwaitTaskID)
		task.AssignedTo = testhelpers.StringPtr(boundedAwaitCoderID)
		task.LeaseExpires = &liveLease
		agent := state.Agents[boundedAwaitCoderID]
		agent.Generation = "generation-b"
		state.Agents[boundedAwaitCoderID] = agent
		return nil
	}); err != nil {
		t.Fatalf("seed generation-B claim: %v", err)
	}

	previousAwait := runAwaitVerdictWithAuthorityOptions
	runAwaitVerdictWithAuthorityOptions = func(
		context.Context,
		string,
		string,
		models.AgentAuthority,
		time.Duration,
		AwaitVerdictOptions,
	) (*ops.AwaitVerdictResult, error) {
		return &ops.AwaitVerdictResult{Verdict: ops.VerdictTimeout}, nil
	}
	t.Cleanup(func() { runAwaitVerdictWithAuthorityOptions = previousAwait })

	statePath := filepath.Join(fixture.projectRoot, paths.ProjectDirName(), "state.yaml")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state before stale cleanup: %v", err)
	}
	stale := models.AgentAuthority{ID: boundedAwaitCoderID, Generation: "generation-a"}
	result, err := AwaitVerdictWithAuthorityOptions(
		fixture.projectRoot, boundedAwaitTaskID, stale, time.Second, AwaitVerdictOptions{},
	)
	var authorityErr *ops.AgentAuthorityError
	if !errors.As(err, &authorityErr) {
		t.Fatalf("error = %T %v, want *ops.AgentAuthorityError", err, err)
	}
	for _, want := range []string{boundedAwaitCoderID, "stop"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want %q", err, want)
		}
	}
	for _, generation := range []string{"generation-a", "generation-b"} {
		for _, forbidden := range []string{generation, fmt.Sprintf("%x", sha256.Sum256([]byte(generation)))} {
			if strings.Contains(err.Error(), forbidden) {
				t.Fatal("budget cleanup diagnostic exposes generation or fingerprint")
			}
		}
	}
	if result == nil || result.Verdict != ops.VerdictTimeout {
		t.Fatalf("result = %#v, want TIMEOUT with authority error", result)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state after stale cleanup: %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("stale command budget cleanup changed generation-B state")
	}
}

// TestAwaitVerdict_NonThreadingCallerStillConverges is the closure condition for
// the loop-detection exemption: a caller that never passes the reported remainder
// back must still reach TIMEOUT rather than looping until the session ceiling.
// Each call here supplies the full default budget, exactly as an agent that omits
// --timeout-seconds would.
func TestAwaitVerdict_NonThreadingCallerStillConverges(t *testing.T) {
	fixture := setupBoundedAwaitFixture(
		t,
		models.TaskStatusReadyForReview,
		boundedAwaitCoderID,
		"coder",
		models.AgentStatusWaiting,
		models.TaskEventSubmittedForReview,
	)

	// Age the submission anchor past the budget the caller keeps re-sending.
	const budget = 30 * time.Minute
	if err := fixture.bb.Modify(func(s *models.State) error {
		task := s.FindTask(boundedAwaitTaskID)
		for i := range task.History {
			if task.History[i].Event == models.TaskEventSubmittedForReview {
				task.History[i].Time = time.Now().UTC().Add(-budget - time.Minute)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("age submission anchor: %v", err)
	}

	remaining := ops.AwaitVerdictRemainingBudget(
		fixture.projectRoot, boundedAwaitTaskID, boundedAwaitCoderID, budget)
	if remaining != 0 {
		t.Fatalf("derived remaining = %s, want 0 once the anchor predates the budget", remaining)
	}

	// The full budget is re-sent, as a non-threading caller would; the derived
	// remainder must still be exhausted.
	result, err := AwaitVerdict(fixture.projectRoot, boundedAwaitTaskID, boundedAwaitCoderID, budget)
	if err != nil {
		t.Fatalf("AwaitVerdict: %v", err)
	}
	if result.Verdict != ops.VerdictTimeout {
		t.Fatalf("verdict = %q, want %q — a non-threading caller must still converge",
			result.Verdict, ops.VerdictTimeout)
	}
	if result.TimeoutSeconds != 0 {
		t.Errorf("timeout_seconds = %d, want 0 on final exhaustion", result.TimeoutSeconds)
	}
}

// TestAwaitRemainingBudget_AnchorArithmetic covers the clamps around the derived
// remainder: a fresh anchor yields the full budget, a future anchor (clock skew)
// never yields more than it, and a missing anchor falls back rather than failing.
func TestAwaitRemainingBudget_AnchorArithmetic(t *testing.T) {
	fixture := setupBoundedAwaitFixture(
		t,
		models.TaskStatusRejected,
		boundedAwaitReviewerID,
		"code-reviewer",
		models.AgentStatusIdle,
		models.TaskEventRejected,
	)
	const budget = 30 * time.Minute

	setRejectionTime := func(t *testing.T, at time.Time) {
		t.Helper()
		if err := fixture.bb.Modify(func(s *models.State) error {
			task := s.FindTask(boundedAwaitTaskID)
			for i := range task.History {
				if task.History[i].Event == models.TaskEventRejected {
					task.History[i].Time = at
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("set rejection time: %v", err)
		}
	}

	setRejectionTime(t, time.Now().UTC().Add(-10*time.Minute))
	got := ops.AwaitResubmissionRemainingBudget(
		fixture.projectRoot, boundedAwaitTaskID, boundedAwaitReviewerID, budget)
	if got > 20*time.Minute || got < 19*time.Minute {
		t.Errorf("remaining after 10m elapsed = %s, want ~20m", got)
	}

	setRejectionTime(t, time.Now().UTC().Add(time.Hour))
	got = ops.AwaitResubmissionRemainingBudget(
		fixture.projectRoot, boundedAwaitTaskID, boundedAwaitReviewerID, budget)
	if got != budget {
		t.Errorf("remaining with a future anchor = %s, want the full budget %s", got, budget)
	}

	got = ops.AwaitResubmissionRemainingBudget(
		fixture.projectRoot, boundedAwaitTaskID, "code-reviewer-absent", budget)
	if got != budget {
		t.Errorf("remaining with no anchor for the agent = %s, want fallback to %s", got, budget)
	}
}

// TestAwaitRemainingBudget_CallerCannotExtendDeadline covers the second half of
// the loop bound: deriving from an anchor is only monotonic if the total is also
// fixed. A caller that raises --timeout-seconds by however much time has passed
// would otherwise hold the remainder constant forever.
func TestAwaitRemainingBudget_CallerCannotExtendDeadline(t *testing.T) {
	fixture := setupBoundedAwaitFixture(
		t,
		models.TaskStatusReadyForReview,
		boundedAwaitCoderID,
		"coder",
		models.AgentStatusWaiting,
		models.TaskEventSubmittedForReview,
	)

	setSubmissionAge := func(t *testing.T, age time.Duration) {
		t.Helper()
		if err := fixture.bb.Modify(func(s *models.State) error {
			task := s.FindTask(boundedAwaitTaskID)
			for i := range task.History {
				if task.History[i].Event == models.TaskEventSubmittedForReview {
					task.History[i].Time = time.Now().UTC().Add(-age)
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("set submission age: %v", err)
		}
	}

	// An escalating caller: each call asks for more than the last, by more than
	// the elapsed time. The derived remainder must still fall.
	var previous time.Duration
	for i, step := range []struct {
		age   time.Duration
		total time.Duration
	}{
		{5 * time.Minute, 30 * time.Minute},
		{15 * time.Minute, 2 * time.Hour},
		{29 * time.Minute, 24 * time.Hour},
	} {
		setSubmissionAge(t, step.age)
		got := ops.AwaitVerdictRemainingBudget(
			fixture.projectRoot, boundedAwaitTaskID, boundedAwaitCoderID, step.total)
		if got > ops.DefaultAwaitBudget {
			t.Fatalf("step %d: remaining = %s, exceeds the %s ceiling",
				i, got, ops.DefaultAwaitBudget)
		}
		if i > 0 && got >= previous {
			t.Fatalf("step %d: remaining = %s did not fall below previous %s; "+
				"a caller raising the total extended its own deadline", i, got, previous)
		}
		previous = got
	}

	// Past the ceiling, no total revives the wait.
	setSubmissionAge(t, 90*time.Minute)
	if got := ops.AwaitVerdictRemainingBudget(
		fixture.projectRoot, boundedAwaitTaskID, boundedAwaitCoderID, 24*time.Hour); got != 0 {
		t.Errorf("remaining = %s past the ceiling, want 0", got)
	}
}
