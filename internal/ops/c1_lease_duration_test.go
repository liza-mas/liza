package ops

import (
	"context"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// C1 lease cases: the configured lease_duration, and the unset path (0), which
// must give the 1800 s default. Expected deltas are literals, independent of
// the resolver under test.
var c1LeaseCases = []struct {
	name       string
	configured int
	want       time.Duration
}{
	{"configured_600", 600, 600 * time.Second},
	{"default_unset", 0, 1800 * time.Second},
}

func TestC1LeaseDurationSecondsResolver(t *testing.T) {
	for _, tc := range []struct{ configured, want int }{
		{600, 600},
		{1, 1},
		{1800, 1800},
		{0, 1800},
		{-5, 1800},
	} {
		if got := LeaseDurationSeconds(models.Config{LeaseDuration: tc.configured}); got != tc.want {
			t.Errorf("LeaseDurationSeconds(lease_duration=%d) = %d, want %d", tc.configured, got, tc.want)
		}
	}
}

// requireC1ReviewLease asserts the review lease lies within the operation's
// bracketing instants plus want, with a small slack for timestamp encoding.
func requireC1ReviewLease(t *testing.T, bb *db.Blackboard, before, after time.Time, want time.Duration) {
	t.Helper()
	s, err := bb.Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	task := s.FindTask("task-1")
	if task.ReviewLeaseExpires == nil {
		t.Fatal("ReviewLeaseExpires not set after reclaim")
	}
	const slack = 2 * time.Second
	lo, hi := before.Add(want-slack), after.Add(want+slack)
	if got := *task.ReviewLeaseExpires; got.Before(lo) || got.After(hi) {
		t.Errorf("review lease = %v, want within [%v, %v] (%v after the reclaim)", got, lo, hi, want)
	}
}

// Early resubmission: the task is already resubmitted at entry, so the reclaim
// runs without a reservation.
func TestC1ReclaimEarlyResubmissionHonoursConfiguredLease(t *testing.T) {
	for _, tc := range c1LeaseCases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, root)
			now := time.Now().UTC()
			state := testhelpers.CreateValidState()
			state.Config.LeaseDuration = tc.configured
			task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
			task.History = append(task.History, models.TaskHistoryEntry{Time: now.Add(-time.Minute), Event: models.TaskEventRejected, Agent: strPtr("reviewer-1")})
			commit := "c1earlycommit"
			task.ReviewCommit, task.Worktree = &commit, nil
			state.Tasks = []models.Task{task}
			state.Agents["reviewer-1"] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusIdle}
			bb := testhelpers.WriteInitialState(t, stateFile, state)

			before := time.Now()
			result, err := AwaitResubmission(context.Background(), root, "task-1", "reviewer-1", 10*time.Second)
			after := time.Now()
			if err != nil || result.Verdict != string(ResubmissionResubmitted) {
				t.Fatalf("AwaitResubmission = %+v, %v; want RESUBMITTED", result, err)
			}
			requireC1ReviewLease(t, bb, before, after, tc.want)
		})
	}
}

// Reservation-held resubmission: the wait holds the reservation when the
// doer resubmits.
func TestC1ReclaimReservationHeldHonoursConfiguredLease(t *testing.T) {
	for _, tc := range c1LeaseCases {
		t.Run(tc.name, func(t *testing.T) {
			root, bb := setupReservationFixture(t)
			if err := bb.Modify(func(s *models.State) error {
				s.Config.LeaseDuration = tc.configured
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			wait := startResubmissionWait(t, root, 10*time.Second)
			waitForReviewOwnership(t, bb, "task-1", "reviewer-1", wait, 5*time.Second)

			before := time.Now()
			if err := bb.Modify(func(s *models.State) error {
				task := s.FindTask("task-1")
				task.Status = models.TaskStatusReadyForReview
				commit := "c1heldcommit"
				task.ReviewCommit, task.Worktree = &commit, nil
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			result, err := wait.finish()
			after := time.Now()
			if err != nil || result.Verdict != string(ResubmissionResubmitted) {
				t.Fatalf("AwaitResubmission = %+v, %v; want RESUBMITTED", result, err)
			}
			requireC1ReviewLease(t, bb, before, after, tc.want)
		})
	}
}
