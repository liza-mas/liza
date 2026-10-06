package commands

import (
	"context"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestD42ClaudeAwaitUsesLongForegroundInterval(t *testing.T) {
	fixture := setupBoundedAwaitFixture(t, models.TaskStatusReadyForReview,
		boundedAwaitCoderID, "coder", models.AgentStatusWaiting, models.TaskEventSubmittedForReview)
	if err := fixture.bb.Modify(func(state *models.State) error {
		agent := state.Agents[boundedAwaitCoderID]
		agent.Provider = "claude"
		state.Agents[boundedAwaitCoderID] = agent
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	previous := runAwaitVerdictWithAuthorityOptions
	t.Cleanup(func() { runAwaitVerdictWithAuthorityOptions = previous })
	var observed time.Duration
	runAwaitVerdictWithAuthorityOptions = func(_ context.Context, _, _ string, _ models.AgentAuthority,
		interval time.Duration, _ AwaitVerdictOptions) (*ops.AwaitVerdictResult, error) {
		observed = interval
		return &ops.AwaitVerdictResult{Verdict: ops.VerdictApproved}, nil
	}
	result, err := AwaitVerdictWithAuthority(fixture.projectRoot, boundedAwaitTaskID,
		models.AgentAuthority{ID: boundedAwaitCoderID}, ops.DefaultAwaitBudget)
	if err != nil || result.Verdict != ops.VerdictApproved {
		t.Fatalf("await = %+v, %v; want APPROVED", result, err)
	}
	if observed != 540*time.Second {
		t.Fatalf("Claude foreground interval = %s, want 9m", observed)
	}
}

func TestD42ResubmissionPollRetainsReviewOwnership(t *testing.T) {
	fixture := setupBoundedAwaitFixture(t, models.TaskStatusRejected,
		boundedAwaitReviewerID, "code-reviewer", models.AgentStatusIdle, models.TaskEventRejected)
	result, err := awaitResubmissionWithInterval(fixture.projectRoot, boundedAwaitTaskID,
		boundedAwaitReviewerID, 2*time.Second, 20*time.Millisecond)
	if err != nil || result.Verdict != ops.ResubmissionPoll {
		t.Fatalf("await = %+v, %v; want POLL", result, err)
	}
	state, err := fixture.bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	task, agent := state.FindTask(boundedAwaitTaskID), state.Agents[boundedAwaitReviewerID]
	if task.ReviewingBy == nil || *task.ReviewingBy != boundedAwaitReviewerID ||
		task.ReviewLeaseExpires == nil || !task.ReviewLeaseExpires.After(time.Now()) ||
		agent.Status != models.AgentStatusWaiting || agent.CurrentTask == nil || *agent.CurrentTask != boundedAwaitTaskID {
		t.Fatal("POLL released the review reservation; the same waiting session must retain it")
	}
	// The final expiry is different from POLL and still releases ownership.
	result, err = awaitResubmissionWithInterval(fixture.projectRoot, boundedAwaitTaskID,
		boundedAwaitReviewerID, 0, 20*time.Millisecond)
	if err != nil || result.Verdict != ops.ResubmissionTimeout {
		t.Fatalf("exhausted await = %+v, %v; want TIMEOUT", result, err)
	}
	assertBoundedAwaitOwnershipReleased(t, fixture, true)
}

func TestD42SubmissionPreparationSurvivesPollRetry(t *testing.T) {
	fixture := setupBoundedAwaitFixture(t, models.TaskStatusImplementing,
		boundedAwaitReviewerID, "code-reviewer", models.AgentStatusWaiting, models.TaskEventRejected)
	var request ops.LifecycleRequest
	if err := fixture.bb.Modify(func(state *models.State) error {
		task := state.FindTask(boundedAwaitTaskID)
		lease := time.Now().Add(10 * time.Minute)
		task.ReviewingBy, task.ReviewLeaseExpires = testhelpers.StringPtr(boundedAwaitReviewerID), &lease
		reviewer := state.Agents[boundedAwaitReviewerID]
		reviewer.CurrentTask = testhelpers.StringPtr(boundedAwaitTaskID)
		state.Agents[boundedAwaitReviewerID] = reviewer
		state.Agents[boundedAwaitCoderID] = models.Agent{Role: "coder", Status: models.AgentStatusWorking,
			CurrentTask: testhelpers.StringPtr(boundedAwaitTaskID), LeaseExpires: &lease}
		task.AssignedTo, task.LeaseExpires = testhelpers.StringPtr(boundedAwaitCoderID), &lease
		var err error
		request, err = ops.NewLifecycleRequest("submit-for-review", task, boundedAwaitCoderID, nil,
			ops.LifecycleRequestOptions{RequestID: "slow-resubmission", ExpectedTransition: models.TaskTransitionID(task)}, struct{}{})
		if err != nil {
			return err
		}
		return ops.PrepareLifecycleRequest(task, request, state.Agents)
	}); err != nil {
		t.Fatal(err)
	}
	for round := range 2 {
		result, err := awaitResubmissionWithInterval(fixture.projectRoot, boundedAwaitTaskID,
			boundedAwaitReviewerID, 2*time.Second, 20*time.Millisecond)
		if err != nil || result.Verdict != ops.ResubmissionPoll {
			t.Fatalf("round %d await = %+v, %v; want POLL", round, result, err)
		}
		state, err := fixture.bb.Read()
		if err != nil {
			t.Fatal(err)
		}
		if err := ops.ValidateLifecyclePreparation(state.FindTask(boundedAwaitTaskID), request); err != nil {
			t.Fatalf("round %d invalidated the doer's prepared submission across POLL: %v", round, err)
		}
	}
}

func TestD42AuthenticatedResubmissionIntervalAndCleanupPolicy(t *testing.T) {
	for _, tc := range []struct {
		cli         string
		budget, cap time.Duration
		poll        bool
	}{
		{"claude", ops.DefaultAwaitBudget, 540 * time.Second, true},
		{"codex", ops.DefaultAwaitBudget, 100 * time.Second, true},
		{"kimi", ops.DefaultAwaitBudget, 100 * time.Second, true},
		{"claude-acp", ops.DefaultAwaitBudget, 100 * time.Second, true},
		{"unknown", ops.DefaultAwaitBudget, 100 * time.Second, true},
		{"claude", time.Second, time.Second, false},
	} {
		t.Run(tc.cli+"/"+tc.budget.String(), func(t *testing.T) {
			fixture := setupBoundedAwaitFixture(t, models.TaskStatusRejected,
				boundedAwaitReviewerID, "code-reviewer", models.AgentStatusIdle, models.TaskEventRejected)
			if err := fixture.bb.Modify(func(s *models.State) error {
				a := s.Agents[boundedAwaitReviewerID]
				a.Provider = tc.cli
				s.Agents[boundedAwaitReviewerID] = a
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			authority := models.AgentAuthority{ID: boundedAwaitReviewerID}
			previous := runAwaitResubmissionWithAuthorityOptions
			t.Cleanup(func() { runAwaitResubmissionWithAuthorityOptions = previous })
			runAwaitResubmissionWithAuthorityOptions = func(_ context.Context, _, _ string, got models.AgentAuthority,
				interval time.Duration, opts AwaitResubmissionOptions) (*ops.AwaitResubmissionResult, error) {
				if got != authority || opts.PollOnTimeout != tc.poll {
					t.Fatal("operation received incorrect authority or interval-expiry cleanup policy")
				}
				if interval <= 0 || interval > tc.cap || (tc.poll && interval != tc.cap) {
					t.Fatalf("interval = %s, want cap %s within the remaining budget", interval, tc.cap)
				}
				verdict := ops.ResubmissionTimeout
				if opts.PollOnTimeout {
					verdict = ops.ResubmissionPoll
				}
				return &ops.AwaitResubmissionResult{Verdict: verdict}, nil
			}
			result, err := AwaitResubmissionWithAuthorityOptions(fixture.projectRoot, boundedAwaitTaskID,
				authority, tc.budget, AwaitResubmissionOptions{})
			if err != nil || result == nil {
				t.Fatalf("await = %+v, %v", result, err)
			}
			if tc.poll {
				if result.Verdict != ops.ResubmissionPoll || result.TimeoutSeconds <= 0 {
					t.Fatalf("await = %+v; want POLL with remaining budget", result)
				}
			} else if result.Verdict != ops.ResubmissionTimeout || result.TimeoutSeconds != 0 {
				t.Fatalf("await = %+v; want final TIMEOUT", result)
			}
		})
	}
}
