package ops

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// phaseHookCount counts the lifecycle transactions that reach the generation
// fence of fixture, i.e. authority-path transactions on its shared blackboard.
func phaseHookCount(t *testing.T, fixture lifecycleGenerationFixture) *int {
	t.Helper()
	count := new(int)
	t.Cleanup(setLifecycleMutationTestHook(db.For(fixture.statePath), func() { *count++ }))
	return count
}

// P8: the blocked-episode operations select their transaction by caller
// identity. An identified caller always goes through the generation-fenced
// transaction (one lifecycle hook per transaction); a nil-authority operator
// keeps the plain bb.Modify and never reaches the fence. A replaced generation
// is refused with no write and the same call then succeeds for the current one.
func TestPhaseEpisodeOperationsSelectTransactionByCaller(t *testing.T) {
	t.Parallel()
	exhaustedTask := func(t *testing.T) lifecycleGenerationFixture {
		fixture := newOrchestratorLifecycleFixture(t, false, models.TaskStatusImplementing)
		phaseRewriteState(t, fixture, func(state *models.State) {
			state.FindTask("task-1").FailedBy = []string{"coder-1", "coder-2"}
		})
		return fixture
	}
	markBlockedTask := func(t *testing.T) lifecycleGenerationFixture {
		return newLifecycleGenerationFixture(t, "coder-1", "coder", false, []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC()),
		})
	}
	assessTask := func(t *testing.T) lifecycleGenerationFixture {
		return newOrchestratorLifecycleFixture(t, false, models.TaskStatusBlocked)
	}
	for _, tc := range []struct {
		name       string
		newFixture func(*testing.T) lifecycleGenerationFixture
		operator   func(lifecycleGenerationFixture) error
		identified func(lifecycleGenerationFixture, models.AgentAuthority) error
	}{
		{"mark-blocked", markBlockedTask,
			func(f lifecycleGenerationFixture) error {
				_, err := MarkBlocked(f.projectRoot, "task-1", "blocked", []string{"what now?"}, f.authorityA.ID)
				return err
			},
			func(f lifecycleGenerationFixture, a models.AgentAuthority) error {
				_, err := MarkBlockedWithAuthority(f.projectRoot, "task-1", "blocked", []string{"what now?"}, a, MarkBlockedOptions{})
				return err
			}},
		{"assess-blocked", assessTask,
			func(f lifecycleGenerationFixture) error {
				_, err := AssessBlocked(f.projectRoot, "task-1", "assessed", f.authorityA.ID)
				return err
			},
			func(f lifecycleGenerationFixture, a models.AgentAuthority) error {
				_, err := AssessBlockedWithAuthority(f.projectRoot, "task-1", "assessed", a, AssessBlockedOptions{})
				return err
			}},
		{"assess-hypothesis-exhausted", exhaustedTask,
			func(f lifecycleGenerationFixture) error {
				_, err := AssessHypothesisExhausted(f.projectRoot, "task-1", "assessed", f.authorityA.ID)
				return err
			},
			func(f lifecycleGenerationFixture, a models.AgentAuthority) error {
				_, err := AssessHypothesisExhaustedWithAuthority(f.projectRoot, "task-1", "assessed", a)
				return err
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			t.Run("operator-never-reaches-the-fence", func(t *testing.T) {
				t.Parallel()
				fixture := tc.newFixture(t)
				count := phaseHookCount(t, fixture)
				if err := tc.operator(fixture); err != nil {
					t.Fatalf("operator call: %v", err)
				}
				if *count != 0 {
					t.Fatalf("operator call reached the generation fence %d times", *count)
				}
			})
			t.Run("identified-runs-one-fenced-transaction", func(t *testing.T) {
				t.Parallel()
				fixture := tc.newFixture(t)
				count := phaseHookCount(t, fixture)
				if err := tc.identified(fixture, fixture.authorityA); err != nil {
					t.Fatalf("identified call: %v", err)
				}
				if *count != 1 {
					t.Fatalf("identified call ran %d fenced transactions, want 1", *count)
				}
			})
			t.Run("replaced-generation-writes-nothing", func(t *testing.T) {
				t.Parallel()
				fixture := tc.newFixture(t)
				interleaving := fixture.replaceAtLifecycleWrite(t, 1)
				interleaving.requireStaleAndUnchanged(t, tc.identified(fixture, fixture.authorityA))
				if err := tc.identified(fixture, fixture.authorityB); err != nil {
					t.Fatalf("current generation: %v", err)
				}
			})
		})
	}
}

// P8: a carried human ask or awaited set does not let a replaced generation
// publish; the same re-assessment then reports NO_CHANGE for the current
// generation and keeps what the episode carried.
func TestPhaseAssessBlockedCarriedStateUnderReplacedGeneration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		first AssessBlockedOptions
		check func(*testing.T, *AssessBlockedResult)
	}{
		{"human-ask", AssessBlockedOptions{HumanAction: "Inspect the lock holder"}, func(t *testing.T, r *AssessBlockedResult) {
			if r.HumanAction != "Inspect the lock holder" {
				t.Fatalf("carried human action = %q", r.HumanAction)
			}
		}},
		{"awaited-set", AssessBlockedOptions{AwaitedTasks: []string{"awaited-1"}}, func(t *testing.T, r *AssessBlockedResult) {
			if !r.AwaitedCarried || len(r.AwaitedTasks) != 1 || r.AwaitedTasks[0] != "awaited-1" {
				t.Fatalf("carried awaited set = %v carried=%t", r.AwaitedTasks, r.AwaitedCarried)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture := newOrchestratorLifecycleFixture(t, false, models.TaskStatusBlocked)
			phaseRewriteState(t, fixture, func(state *models.State) {
				state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus("awaited-1", models.TaskStatusImplementing, time.Now().UTC()))
			})
			opts := tc.first
			if _, err := AssessBlockedWithAuthority(fixture.projectRoot, "task-1", "assessed", fixture.authorityA, opts); err != nil {
				t.Fatalf("first assessment: %v", err)
			}
			interleaving := fixture.replaceAtLifecycleWrite(t, 1)
			_, err := AssessBlockedWithAuthority(fixture.projectRoot, "task-1", "assessed", fixture.authorityA, AssessBlockedOptions{})
			interleaving.requireStaleAndUnchanged(t, err)
			result, err := AssessBlockedWithAuthority(fixture.projectRoot, "task-1", "assessed", fixture.authorityB, AssessBlockedOptions{})
			if err != nil || result.Outcome != models.LifecycleNoChange {
				t.Fatalf("current-generation re-assessment = %+v, %v; want NO_CHANGE", result, err)
			}
			tc.check(t, result)
		})
	}
}

// P9: an identified unblock under state-lock contention retries only lock
// acquisition. The held lock times out at least once per wait and the
// operation then completes exactly once: two lifecycle publications (the
// preparation and the final or conflict write), one unblocked entry and one
// receipt, and an exact replay afterwards is a replay, not a second unblock.
// Not parallel: it overrides the default lock timeout process-wide.
func TestPhaseIdentifiedUnblockRetriesAcquisitionOnly(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(50 * time.Millisecond))
	for _, conflict := range []bool{false, true} {
		name := "clean-rebase"
		if conflict {
			name = "conflicting-rebase"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newPhaseRebaseFixture(t, conflict)
			request := phaseRequestFor(t, fixture, "phase-retry")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			request.RetryContext = ctx
			sequenceBefore := readStateForTest(t, fixture.statePath).MutationSequence
			var timeouts atomic.Int32
			release := testhelpers.HoldFileLock(t, fixture.statePath)
			t.Cleanup(filelock.SetAcquisitionTimeoutHookForTest(fixture.statePath, func() {
				timeouts.Add(1)
				release()
			}))
			result, err := UnblockTaskWithAuthority(fixture.projectRoot, "task-1", "phase repair", fixture.authorityA, UnblockTaskOptions{RebaseOn: "integration", Request: request})
			if timeouts.Load() < 1 {
				t.Fatal("the held state lock never timed an acquisition out")
			}
			state := readStateForTest(t, fixture.statePath)
			task := state.FindTask("task-1")
			unblocked, blocked := 0, 0
			for _, entry := range task.History {
				switch entry.Event {
				case models.TaskEventUnblocked:
					unblocked++
				case models.TaskEventBlocked:
					blocked++
				}
			}
			if conflict {
				if err == nil || !strings.Contains(err.Error(), "conflict") {
					t.Fatalf("conflicting rebase = %v, want a conflict refusal", err)
				}
				if task.Status != models.TaskStatusBlocked || unblocked != 0 {
					t.Fatalf("conflict left task %s with %d unblocked entries", task.Status, unblocked)
				}
			} else {
				if err != nil || result == nil || result.Rebase == nil {
					t.Fatalf("clean rebase unblock = %+v, %v", result, err)
				}
				if unblocked != 1 || task.Lifecycle == nil || len(task.Lifecycle.Receipts) != 1 {
					t.Fatalf("clean rebase recorded %d unblocked entries and %v receipts, want one of each", unblocked, task.Lifecycle)
				}
			}
			if got := state.MutationSequence - sequenceBefore; got != 2 {
				t.Fatalf("publications = %d, want the preparation and the final/conflict write only (blocked entries %d)", got, blocked)
			}
			if !conflict {
				replay, err := UnblockTaskWithAuthority(fixture.projectRoot, "task-1", "phase repair", fixture.authorityA, UnblockTaskOptions{RebaseOn: "integration", Request: request})
				if err != nil || replay == nil || replay.Outcome != models.LifecycleAlreadyCompleted {
					t.Fatalf("replay = %+v, %v; want ALREADY_COMPLETED", replay, err)
				}
				if after := readStateForTest(t, fixture.statePath); after.MutationSequence != state.MutationSequence {
					t.Fatal("the replay published state")
				}
			}
		})
	}
}
