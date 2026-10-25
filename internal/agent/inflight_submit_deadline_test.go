//go:build !windows

package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D-38 (scaled): a doer that submits late in its session, whose quiet canonical
// validation outlasts both the remaining execution budget and the progress
// interval, must still publish before the supervisor ends the session. A
// marker of the session's own submit is written before the session starts and
// released as soon as the submit returns, so the deferral does not depend on
// when the real submit gets to write its own; that the real submit writes its
// marker is asserted in internal/ops (budget_exception_test.go,
// submission_admission_test.go).
func TestExecuteAgentLetsLateSubmitPublishPastSupervisorClocks(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stall time.Duration
	}{
		{"prompt start", 0},
		// The submit's preparation outlasts both the 1 s progress and the 2 s
		// execution clocks.
		{"stalled preparation", 2500 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN a quiet 4 s canonical command, a 2 s execution budget and a 1 s
			// progress timeout; the session spends 0.5 s before submitting
			scenario := testhelpers.SetupAcceptanceSubmitScenario(t, "sleep 4\nprintf 'PASS identity assertion\\n'\n", 30)
			previous := executionDeadlineRecheckInterval
			executionDeadlineRecheckInterval = 100 * time.Millisecond
			t.Cleanup(func() { executionDeadlineRecheckInterval = previous })
			shortenSubmitHandback(t, 500*time.Millisecond)
			releaseMarker := ops.WriteInflightSubmitMarkerForTest(scenario.Root, scenario.Authority, scenario.TaskID, time.Now().Add(10*time.Minute))
			t.Cleanup(releaseMarker)

			var (
				cancelledBeforeSubmit      bool
				cancelledBeforePublication bool
				cancelAtPublication        string
				submitErr                  error
				submitReturned             time.Time
			)
			mock := &MockLLMAgent{
				OnExecute: func(ctx context.Context, _ string, _ string, _ string, _ string, _ []string) error {
					select {
					case <-time.After(500 * time.Millisecond):
					case <-ctx.Done():
						cancelledBeforeSubmit = true
						return ctx.Err()
					}
					if tc.stall > 0 {
						holdProjectLifecycleLock(t, scenario.Root, tc.stall)
					}
					submitted := make(chan error, 1)
					go func() {
						_, err := ops.SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)
						submitted <- err
					}()
					select {
					case submitErr = <-submitted:
						submitReturned = time.Now()
						releaseMarker()
					case <-ctx.Done():
						// The supervisor may see the marker gone before the submit
						// returns; that is legal only once it published (ADR-0176).
						_, inflight := ops.InflightSubmitDeadline(scenario.Root, scenario.Authority, scenario.TaskID, time.Now())
						published, readErr := false, error(nil)
						if snap, err := scenario.BB.ReadSnapshot(); err != nil {
							readErr = err
						} else if task := snap.FindTask(scenario.TaskID); task != nil {
							published = task.AcceptanceReceipt != nil
						}
						cancelledBeforePublication = inflight || !published
						cancelAtPublication = fmt.Sprintf("marker=%v receipt=%v read error=%v", inflight, published, readErr)
						submitErr = <-submitted
						submitReturned = time.Now()
						releaseMarker()
						return ctx.Err()
					}
					// The agent is still writing its reply when the session ends.
					<-ctx.Done()
					return ctx.Err()
				},
			}
			state, err := scenario.BB.Read()
			if err != nil {
				t.Fatal(err)
			}
			config := SupervisorConfig{
				AgentID:                  scenario.AgentID,
				Authority:                scenario.Authority,
				Role:                     models.RoleCoder,
				ProjectRoot:              scenario.Root,
				StatePath:                scenario.StatePath,
				CLIName:                  "codex",
				LLMAgent:                 mock,
				ExecutionTimeout:         2 * time.Second,
				ExecutionProgressTimeout: time.Second,
			}

			// WHEN the supervisor runs the session
			exitCode, _, execErr := executeAgent(context.Background(), config, "prompt", nil, scenario.TaskID, state.Config)
			returned := time.Now()

			// THEN the session was not cancelled before the submit published at its
			// candidate
			if cancelledBeforeSubmit {
				t.Fatalf("supervisor cancelled the session before the submit started")
			}
			if cancelledBeforePublication {
				t.Fatalf("supervisor cancelled the session before the submit published (%s, submit error: %v)", cancelAtPublication, submitErr)
			}
			if submitErr != nil {
				t.Fatalf("submit error = %v, want publication", submitErr)
			}
			after, err := scenario.BB.Read()
			if err != nil {
				t.Fatal(err)
			}
			task := after.FindTask(scenario.TaskID)
			if task.ReviewCommit == nil || task.AcceptanceReceipt == nil || task.AcceptanceReceipt.ReviewCommit != *task.ReviewCommit {
				t.Fatalf("task status=%s review_commit=%v receipt=%v, want a receipt at the review commit", task.Status, task.ReviewCommit, task.AcceptanceReceipt)
			}
			if last := task.History[len(task.History)-1]; last.Event != models.TaskEventSubmittedForReview {
				t.Fatalf("last task event = %s (status %s), want %s", last.Event, task.Status, models.TaskEventSubmittedForReview)
			}
			for _, event := range task.History {
				if event.Event == models.TaskEventClaimReleased || event.Event == models.TaskEventBlocked {
					t.Fatalf("task history has %s before or around publication: %+v", event.Event, event)
				}
			}
			// AND the session still ends, unblocked, once the hand-back after the
			// submit is over
			if execErr != nil || exitCode != 1 {
				t.Fatalf("executeAgent = (%d, %v), want execution timeout exit 1", exitCode, execErr)
			}
			if wait := returned.Sub(submitReturned); wait > 1500*time.Millisecond {
				t.Fatalf("session ended %s after submit returned, want within the 500ms hand-back plus 1s", wait)
			}
			entries, err := os.ReadDir(paths.New(scenario.Root).InflightSubmitDir())
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("in-flight submit markers left behind: %v", entries)
			}
		})
	}
}

// holdProjectLifecycleLock holds the project lifecycle lock exclusively for d
// and returns once it is held. A submit started meanwhile waits for the lock
// before it prepares and writes its marker, which stalls its preparation for
// d whatever the host's speed. The test waits for the holder at cleanup.
func holdProjectLifecycleLock(t *testing.T, root string, d time.Duration) {
	t.Helper()
	held := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- ops.WithProjectLifecycleExclusiveLock(root, "test stalled preparation", func() error {
			close(held)
			<-time.After(d)
			return nil
		})
	}()
	t.Cleanup(func() {
		if err := <-done; err != nil {
			t.Errorf("project lifecycle lock holder: %v", err)
		}
	})
	select {
	case <-held:
	case <-time.After(10 * time.Second):
		t.Errorf("project lifecycle lock not acquired within 10s")
	}
}

// lateSubmitSession runs a coder session whose submit starts after startDelay,
// with a 1 s execution budget, and records whether the session was still live
// when the submit returned. A non-zero stall holds the project lifecycle lock
// for that long first, so the submit's preparation outlasts the supervisor's
// clocks. The agent then waits for the session's end, as an agent that never
// settles the outcome would. With holdMarker, a marker of the session's own
// submit is written before the session starts, so the deferral of the
// execution timeout does not depend on when the submit gets to write its own;
// the agent removes it right after recording liveAtReturn, which starts the
// hand-back.
func lateSubmitSession(t *testing.T, scenario *testhelpers.AcceptanceSubmitScenario, progressTimeout, startDelay, stall time.Duration, holdMarker bool) (exitCode int, liveAtReturn bool, handback time.Duration, submitErr error) {
	t.Helper()
	previous := executionDeadlineRecheckInterval
	executionDeadlineRecheckInterval = 50 * time.Millisecond
	t.Cleanup(func() { executionDeadlineRecheckInterval = previous })
	releaseMarker := func() {}
	if holdMarker {
		releaseMarker = ops.WriteInflightSubmitMarkerForTest(scenario.Root, scenario.Authority, scenario.TaskID, time.Now().Add(10*time.Minute))
		t.Cleanup(releaseMarker)
	}
	var returned time.Time
	mock := &MockLLMAgent{
		OnExecute: func(ctx context.Context, _ string, _ string, _ string, _ string, _ []string) error {
			select {
			case <-time.After(startDelay):
			case <-ctx.Done():
				return ctx.Err()
			}
			if stall > 0 {
				holdProjectLifecycleLock(t, scenario.Root, stall)
			}
			_, submitErr = ops.SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)
			returned = time.Now()
			liveAtReturn = ctx.Err() == nil
			releaseMarker()
			<-ctx.Done()
			handback = time.Since(returned)
			return ctx.Err()
		},
	}
	state, err := scenario.BB.Read()
	if err != nil {
		t.Fatal(err)
	}
	config := SupervisorConfig{
		AgentID:                  scenario.AgentID,
		Authority:                scenario.Authority,
		Role:                     models.RoleCoder,
		ProjectRoot:              scenario.Root,
		StatePath:                scenario.StatePath,
		CLIName:                  "codex",
		LLMAgent:                 mock,
		ExecutionTimeout:         time.Second,
		ExecutionProgressTimeout: progressTimeout,
	}
	exitCode, _, execErr := executeAgent(context.Background(), config, "prompt", nil, scenario.TaskID, state.Config)
	if execErr != nil {
		t.Fatalf("executeAgent error = %v", execErr)
	}
	return exitCode, liveAtReturn, handback, submitErr
}

func requireSupervisorBlocked(t *testing.T, scenario *testhelpers.AcceptanceSubmitScenario, reasonPart string) {
	t.Helper()
	after, err := scenario.BB.Read()
	if err != nil {
		t.Fatal(err)
	}
	task := after.FindTask(scenario.TaskID)
	if task.Status != models.TaskStatusBlocked || task.BlockedReason == nil || !strings.Contains(*task.BlockedReason, reasonPart) {
		t.Fatalf("task status=%s reason=%v, want BLOCKED with %q", task.Status, task.BlockedReason, reasonPart)
	}
	for _, event := range task.History {
		if event.Event == models.TaskEventClaimReleased {
			t.Fatalf("task history has %s, want the claim settled by the block: %+v", event.Event, event)
		}
	}
}

// D-174 (scaled): a late submit that fails after the execution timeout hands
// the session back to the agent; when the agent does not settle the task, the
// supervisor blocks it instead of releasing it for a blind re-claim. The held
// marker stands for the session's submit; the real submit's own marker is
// asserted in internal/ops (budget_exception_test.go, submission_admission_test.go).
func TestExecuteAgentBlocksUnadmittedLateSubmitAfterHandback(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stall time.Duration
	}{
		{"prompt start", 0},
		// The submit's preparation outlasts the 1 s execution timeout.
		{"stalled preparation", 1500 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN a canonical command that fails after the 1 s execution budget
			scenario := testhelpers.SetupAcceptanceSubmitScenario(t, "sleep 1\nexit 3\n", 30)
			shortenSubmitHandback(t, 500*time.Millisecond)

			// WHEN the agent submits late and then idles
			exitCode, liveAtReturn, handback, submitErr := lateSubmitSession(t, &scenario, time.Hour, 500*time.Millisecond, tc.stall, true)

			// THEN the agent saw the failure in a live session, kept the hand-back,
			// and the task ended BLOCKED with the hand-back reason
			if submitErr == nil {
				t.Fatal("submit with a failing canonical command succeeded")
			}
			if !liveAtReturn {
				t.Fatal("the session was cancelled before the agent saw its submit's outcome")
			}
			if handback < 400*time.Millisecond {
				t.Fatalf("session ended %s after the submit returned, want the 500ms hand-back", handback)
			}
			if exitCode != 0 {
				t.Fatalf("exit code = %d, want 0 after a supervisor block", exitCode)
			}
			requireSupervisorBlocked(t, &scenario, "no longer observable")
		})
	}
}

// The hand-back does not suspend the progress watchdog: an idle agent with a
// progress timeout shorter than the hand-back is blocked by the watchdog,
// still never released.
func TestExecuteAgentProgressWatchdogStillBlocksDuringHandback(t *testing.T) {
	for _, tc := range []struct {
		name       string
		startDelay time.Duration
	}{
		{"prompt start", 500 * time.Millisecond},
		// The agent's first action comes after the 1 s execution timeout.
		{"late start", 1500 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scenario := testhelpers.SetupAcceptanceSubmitScenario(t, "sleep 1\nexit 3\n", 30)
			shortenSubmitHandback(t, time.Hour)

			exitCode, liveAtReturn, _, _ := lateSubmitSession(t, &scenario, 2*time.Second, tc.startDelay, 0, true) // above the idle time; the held marker exempts the watchdog until the agent releases it

			if !liveAtReturn {
				t.Fatal("the session was cancelled before the agent saw its submit's outcome")
			}
			if exitCode != 0 {
				t.Fatalf("exit code = %d, want 0 after the watchdog block", exitCode)
			}
			requireSupervisorBlocked(t, &scenario, "progress timeout")
		})
	}
}

// A submit still running when the grace cap ends is interrupted, and the task
// is blocked as an interrupted admission. The cap bounds the deferral even for
// a marker whose own deadline is far later; the marker's deadline semantics are
// asserted by TestExecutionDeadlineMarkerPastItsDeadlineWithLiveProcessIsInterrupted.
func TestExecuteAgentBlocksSubmitStillRunningWhenGraceEnds(t *testing.T) {
	for _, tc := range []struct {
		name       string
		startDelay time.Duration
	}{
		{"prompt start", 0},
		// The agent's first action comes after the 200 ms execution timeout.
		{"late start", time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scenario := testhelpers.SetupAcceptanceSubmitScenario(t, "printf 'PASS identity assertion\\n'\n", 30)
			shortenSubmitHandback(t, time.Hour)
			previous := executionDeadlineRecheckInterval
			executionDeadlineRecheckInterval = 50 * time.Millisecond
			t.Cleanup(func() { executionDeadlineRecheckInterval = previous })
			previousGrace := maxInflightSubmitGrace
			maxInflightSubmitGrace = 500 * time.Millisecond
			t.Cleanup(func() { maxInflightSubmitGrace = previousGrace })
			// A submit of this process that never returns, held for the whole
			// session: its marker is there when the execution timeout elapses,
			// whenever the agent gets to act.
			t.Cleanup(ops.WriteInflightSubmitMarkerForTest(scenario.Root, scenario.Authority, scenario.TaskID, time.Now().Add(time.Hour)))
			mock := &MockLLMAgent{
				OnExecute: func(ctx context.Context, _ string, _ string, _ string, _ string, _ []string) error {
					select {
					case <-time.After(tc.startDelay):
					case <-ctx.Done():
						return ctx.Err()
					}
					<-ctx.Done()
					return ctx.Err()
				},
			}
			state, err := scenario.BB.Read()
			if err != nil {
				t.Fatal(err)
			}
			config := SupervisorConfig{
				AgentID:          scenario.AgentID,
				Authority:        scenario.Authority,
				Role:             models.RoleCoder,
				ProjectRoot:      scenario.Root,
				StatePath:        scenario.StatePath,
				CLIName:          "codex",
				LLMAgent:         mock,
				ExecutionTimeout: 200 * time.Millisecond,
			}

			start := time.Now()
			exitCode, _, execErr := executeAgent(context.Background(), config, "prompt", nil, scenario.TaskID, state.Config)

			if execErr != nil || exitCode != 0 {
				t.Fatalf("executeAgent = (%d, %v), want exit 0 after a supervisor block", exitCode, execErr)
			}
			if elapsed := time.Since(start); elapsed > 3*time.Second {
				t.Fatalf("executeAgent took %s, want the session to end at the grace cap", elapsed)
			}
			requireSupervisorBlocked(t, &scenario, "admission interrupted by session deadline")
		})
	}
}

// A task that moves between the supervisor's observation and its transaction
// is left to whoever moved it, and reported as not blocked.
func TestBlockExecutingTaskFromSupervisorLeavesAMovedTask(t *testing.T) {
	scenario := testhelpers.SetupAcceptanceSubmitScenario(t, "printf 'PASS identity assertion\\n'\n", 30)
	supervisorBlockBeforeModifyTestHook = func() {
		if err := scenario.BB.Modify(func(s *models.State) error {
			models.AdvanceLifecycle(s.FindTask(scenario.TaskID))
			return nil
		}); err != nil {
			t.Errorf("move task: %v", err)
		}
	}
	t.Cleanup(func() { supervisorBlockBeforeModifyTestHook = nil })

	blocked, err := blockExecutingTaskFromSupervisor(scenario.BB, scenario.Root, scenario.TaskID, scenario.Authority, "test reason")

	if err != nil || blocked {
		t.Fatalf("blockExecutingTaskFromSupervisor = (%v, %v), want (false, nil) for a moved task", blocked, err)
	}
	after, err := scenario.BB.Read()
	if err != nil {
		t.Fatal(err)
	}
	if task := after.FindTask(scenario.TaskID); task.Status == models.TaskStatusBlocked {
		t.Fatal("a task moved before the transaction was blocked")
	}
}
