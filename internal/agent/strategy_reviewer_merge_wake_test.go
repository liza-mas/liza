package agent

import (
	"context"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// pendingMergeReviewer builds a reviewer holding one approved task that has not
// merged — the OP-048 shape. The merge attempt inside PreWork fails (the task
// has no review commit), so the task stays pending across rounds.
func pendingMergeReviewer(t *testing.T, extraTasks ...models.Task) (*reviewerStrategy, *db.Blackboard, SupervisorConfig, models.PipelineResolver) {
	t.Helper()
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	pr, err := ops.LoadResolverForModels(tmpDir)
	if err != nil {
		t.Fatalf("LoadResolverForModels() error = %v", err)
	}

	const reviewerID = "code-reviewer-1"
	tasks := append([]models.Task{{
		ID:        "approved-unmerged",
		Status:    models.TaskStatusApproved,
		RolePair:  "coding-pair",
		Approvals: []models.Approval{{Agent: reviewerID, Provider: "claude"}},
	}}, extraTasks...)

	state := testhelpers.CreateValidState()
	state.Tasks = tasks
	registerEligibleMergeTestAgents(state, tasks, reviewerID, pr)
	testhelpers.WriteInitialState(t, statePath, state)

	bb := db.New(statePath)
	config := SupervisorConfig{
		AgentID:     reviewerID,
		Role:        models.RoleCodeReviewer,
		ProjectRoot: tmpDir,
		Authority:   testSupervisorAuthority(t, bb, reviewerID),
	}

	strategy, err := NewRoleStrategy("code-reviewer", testResolver(t))
	if err != nil {
		t.Fatalf("NewRoleStrategy() error = %v", err)
	}
	reviewer, ok := strategy.(*reviewerStrategy)
	if !ok {
		t.Fatalf("NewRoleStrategy() returned %T, want *reviewerStrategy", strategy)
	}
	if !hasPendingMerges(bb, reviewerID, pr) {
		t.Fatal("fixture does not present a pending merge")
	}
	return reviewer, bb, config, pr
}

func withPendingMergeWake(t *testing.T, interval time.Duration, rounds int) {
	t.Helper()
	originalInterval, originalRounds := pendingMergeWakeInterval, maxPendingMergeStallRounds
	pendingMergeWakeInterval, maxPendingMergeStallRounds = interval, rounds
	t.Cleanup(func() {
		pendingMergeWakeInterval, maxPendingMergeStallRounds = originalInterval, originalRounds
	})
}

// TestReviewerPreWork_PendingMergeRetriesInsteadOfParking is the OP-048
// regression: a reviewer that still owns an unmerged approved task after its
// quick retries must keep retrying on a bounded wake. Falling through to the
// role's normal wait parks it for reviewer_max_wait (5h by default) on a
// predicate that cannot see the merge, while every downstream task waits.
func TestReviewerPreWork_PendingMergeRetriesInsteadOfParking(t *testing.T) {
	withPendingMergeWake(t, 100*time.Millisecond, 20)
	reviewer, bb, config, _ := pendingMergeReviewer(t)

	// Quick retries already spent — this is the round that used to give up.
	reviewer.mergeRetries = reviewer.effectiveMaxRetries()

	start := time.Now()
	shouldContinue, err := reviewer.PreWork(context.Background(), bb, config)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("PreWork() error = %v", err)
	}
	if !shouldContinue {
		t.Error("PreWork() shouldContinue = false, want true — the reviewer parked instead of retrying the merge")
	}
	if reviewer.mergeStallRounds != 1 {
		t.Errorf("mergeStallRounds = %d, want 1", reviewer.mergeStallRounds)
	}
	if elapsed > 5*time.Second {
		t.Errorf("PreWork() took %v, want a bounded wake", elapsed)
	}
}

// TestReviewerPreWork_PendingMergeYieldsToReviewWork verifies the retry loop
// does not starve reviews: work this reviewer can take ends the wake early and
// hands control back to the normal wait/claim path.
func TestReviewerPreWork_PendingMergeYieldsToReviewWork(t *testing.T) {
	withPendingMergeWake(t, 30*time.Second, 20)
	reviewable := testhelpers.BuildTaskByStatus("needs-review", models.TaskStatusReadyForReview, time.Now().UTC())
	reviewer, bb, config, pr := pendingMergeReviewer(t, reviewable)

	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got := models.CountReviewableTasksForAgent(state, "code-reviewer", config.AgentID, pr); got == 0 {
		t.Fatal("fixture produced no reviewable task for this role")
	}

	reviewer.mergeRetries = reviewer.effectiveMaxRetries()

	start := time.Now()
	shouldContinue, err := reviewer.PreWork(context.Background(), bb, config)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("PreWork() error = %v", err)
	}
	if shouldContinue {
		t.Error("PreWork() shouldContinue = true, want false — review work should end the merge wake")
	}
	if elapsed >= 30*time.Second {
		t.Errorf("PreWork() waited %v, want an immediate yield", elapsed)
	}
	if reviewer.mergeRetries != 0 || reviewer.mergeStallRounds != 0 {
		t.Errorf("counters = (%d, %d), want reset on yield", reviewer.mergeRetries, reviewer.mergeStallRounds)
	}
}

// TestReviewerPreWork_PendingMergeStallRecordsAnomaly verifies the retry loop is
// itself bounded: a merge that never converges must leave durable evidence and
// release the reviewer rather than retrying forever.
func TestReviewerPreWork_PendingMergeStallRecordsAnomaly(t *testing.T) {
	withPendingMergeWake(t, 50*time.Millisecond, 1)
	reviewer, bb, config, _ := pendingMergeReviewer(t)

	reviewer.mergeRetries = reviewer.effectiveMaxRetries()
	reviewer.mergeStallRounds = maxPendingMergeStallRounds

	shouldContinue, err := reviewer.PreWork(context.Background(), bb, config)
	if err != nil {
		t.Fatalf("PreWork() error = %v", err)
	}
	if shouldContinue {
		t.Error("PreWork() shouldContinue = true, want false after the stall cap")
	}
	if reviewer.mergeRetries != 0 || reviewer.mergeStallRounds != 0 {
		t.Errorf("counters = (%d, %d), want reset after the stall cap", reviewer.mergeRetries, reviewer.mergeStallRounds)
	}

	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	var found *models.Anomaly
	for i := range state.Anomalies {
		if state.Anomalies[i].Type == pendingMergeStallAnomalyType && state.Anomalies[i].Reporter == config.AgentID {
			found = &state.Anomalies[i]
		}
	}
	if found == nil {
		t.Fatalf("no %s anomaly recorded; anomalies = %+v", pendingMergeStallAnomalyType, state.Anomalies)
	}
	if !found.IsValidType() {
		t.Errorf("anomaly type %q is not accepted by the state model", found.Type)
	}
}
