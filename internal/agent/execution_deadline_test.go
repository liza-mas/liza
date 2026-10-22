package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func shortenExecutionDeadlineRecheck(t *testing.T) {
	t.Helper()
	previous := executionDeadlineRecheckInterval
	executionDeadlineRecheckInterval = 10 * time.Millisecond
	t.Cleanup(func() { executionDeadlineRecheckInterval = previous })
}

func shortenSubmitHandback(t *testing.T, window time.Duration) {
	t.Helper()
	previous := submitHandbackWindow
	submitHandbackWindow = window
	t.Cleanup(func() { submitHandbackWindow = previous })
}

func noInflightSubmit(time.Time) (time.Time, bool) { return time.Time{}, false }

func noSubmitRunning() bool { return false }

func alwaysSubmitRunning() bool { return true }

// waitDone returns how long ctx took to end, failing after limit.
func waitDone(t *testing.T, ctx context.Context, limit time.Duration) time.Duration {
	t.Helper()
	start := time.Now()
	select {
	case <-ctx.Done():
		return time.Since(start)
	case <-time.After(limit):
		t.Fatalf("context still live after %s", limit)
		return 0
	}
}

// requireLive fails if ctx ends within d.
func requireLive(t *testing.T, ctx context.Context, d time.Duration, what string) {
	t.Helper()
	select {
	case <-ctx.Done():
		t.Fatalf("%s: context ended early (cause %v)", what, context.Cause(ctx))
	case <-time.After(d):
	}
}

func requireCause(t *testing.T, ctx context.Context, want error) {
	t.Helper()
	if cause := context.Cause(ctx); cause != want {
		t.Fatalf("cause = %v, want %v", cause, want)
	}
	if !executionTimedOut(ctx) {
		t.Fatalf("cause %v is not classified as an execution timeout", context.Cause(ctx))
	}
}

func TestExecutionDeadlineExpiresWithoutInflightSubmit(t *testing.T) {
	ctx, cancel := withExecutionDeadline(context.Background(), 50*time.Millisecond, "task-1", noInflightSubmit, noSubmitRunning)
	defer cancel()

	waitDone(t, ctx, time.Second)

	requireCause(t, ctx, context.DeadlineExceeded)
}

func TestExecutionDeadlineCancelIsNotATimeout(t *testing.T) {
	ctx, cancel := withExecutionDeadline(context.Background(), time.Hour, "task-1", noInflightSubmit, noSubmitRunning)

	cancel()
	waitDone(t, ctx, time.Second)

	if executionTimedOut(ctx) {
		t.Fatal("explicit cancel was classified as an execution timeout")
	}
}

func TestExecutionDeadlineParentTimeoutIsATimeout(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelParent()
	ctx, cancel := withExecutionDeadline(parent, time.Hour, "task-1", noInflightSubmit, noSubmitRunning)
	defer cancel()

	waitDone(t, ctx, time.Second)

	if !executionTimedOut(ctx) {
		t.Fatalf("cause = %v, want the parent's deadline to count as a timeout", context.Cause(ctx))
	}
}

// D-174: once the submit the session was held for stops running, the agent
// keeps the hand-back window to act on its outcome, then the session ends.
func TestExecutionDeadlineHandsBackAfterInflightSubmitEnds(t *testing.T) {
	shortenExecutionDeadlineRecheck(t)
	shortenSubmitHandback(t, 300*time.Millisecond)
	var running atomic.Bool
	running.Store(true)
	inflight := func(now time.Time) (time.Time, bool) { return now.Add(time.Hour), running.Load() }
	ctx, cancel := withExecutionDeadline(context.Background(), 20*time.Millisecond, "task-1", inflight, running.Load)
	defer cancel()

	requireLive(t, ctx, 200*time.Millisecond, "while the submit was in flight")
	running.Store(false)
	ended := time.Now()
	requireLive(t, ctx, 200*time.Millisecond, "during the hand-back")
	waitDone(t, ctx, time.Second)

	if waited := time.Since(ended); waited < 300*time.Millisecond || waited > 600*time.Millisecond {
		t.Fatalf("session ended %s after the submit, want about the 300ms hand-back", waited)
	}
	requireCause(t, ctx, errSubmitHandbackEnded)
}

func TestExecutionDeadlineDeferralIsFixedAtFirstObservation(t *testing.T) {
	shortenExecutionDeadlineRecheck(t)
	shortenSubmitHandback(t, time.Hour)
	start := time.Now()
	first := start.Add(150 * time.Millisecond)
	var calls atomic.Int32
	// A later submit with a later deadline must not extend the deferral.
	inflight := func(now time.Time) (time.Time, bool) {
		if calls.Add(1) == 1 {
			return first, true
		}
		return now.Add(time.Hour), true
	}
	ctx, cancel := withExecutionDeadline(context.Background(), 20*time.Millisecond, "task-1", inflight, alwaysSubmitRunning)
	defer cancel()

	waitDone(t, ctx, 2*time.Second)

	if ended := time.Since(start); ended > first.Sub(start)+150*time.Millisecond {
		t.Fatalf("deferral ended %s after start, want near the first observed deadline %s", ended, first.Sub(start))
	}
	requireCause(t, ctx, errInflightSubmitInterrupted)
}

func TestExecutionDeadlineHardCapBoundsAnyMarker(t *testing.T) {
	shortenExecutionDeadlineRecheck(t)
	shortenSubmitHandback(t, time.Hour)
	previous := maxInflightSubmitGrace
	maxInflightSubmitGrace = 100 * time.Millisecond
	t.Cleanup(func() { maxInflightSubmitGrace = previous })
	inflight := func(now time.Time) (time.Time, bool) { return now.Add(24 * time.Hour), true }
	ctx, cancel := withExecutionDeadline(context.Background(), 20*time.Millisecond, "task-1", inflight, alwaysSubmitRunning)
	defer cancel()

	waitDone(t, ctx, 2*time.Second)

	requireCause(t, ctx, errInflightSubmitInterrupted)
}

// A submit that stops just before the hard cap still gets its whole hand-back:
// the bound is timeout + grace + hand-back.
func TestExecutionDeadlineSubmitEndingAtTheCapGetsFullHandback(t *testing.T) {
	shortenExecutionDeadlineRecheck(t)
	shortenSubmitHandback(t, 300*time.Millisecond)
	previous := maxInflightSubmitGrace
	maxInflightSubmitGrace = 200 * time.Millisecond
	t.Cleanup(func() { maxInflightSubmitGrace = previous })
	start := time.Now()
	inflight := func(now time.Time) (time.Time, bool) { return now.Add(time.Hour), true }
	running := func() bool { return time.Since(start) < 200*time.Millisecond }
	ctx, cancel := withExecutionDeadline(context.Background(), 20*time.Millisecond, "task-1", inflight, running)
	defer cancel()

	waitDone(t, ctx, 2*time.Second)

	// The submit stops at ~200ms, before the ~220ms cap.
	if ended := time.Since(start); ended < 450*time.Millisecond || ended > 900*time.Millisecond {
		t.Fatalf("session ended %s after start, want the submit's ~200ms plus the 300ms hand-back", ended)
	}
	requireCause(t, ctx, errSubmitHandbackEnded)
}

// A submit started during the hand-back neither extends it nor reads as the
// original submit's interruption.
func TestExecutionDeadlineResubmitDoesNotExtendHandback(t *testing.T) {
	shortenExecutionDeadlineRecheck(t)
	shortenSubmitHandback(t, 200*time.Millisecond)
	var running atomic.Bool
	inflight := func(now time.Time) (time.Time, bool) { return now.Add(time.Hour), true }
	ctx, cancel := withExecutionDeadline(context.Background(), 20*time.Millisecond, "task-1", inflight, running.Load)
	defer cancel()

	requireLive(t, ctx, 100*time.Millisecond, "early in the hand-back")
	running.Store(true) // the agent resubmits; its marker has an hour left
	waitDone(t, ctx, time.Second)

	requireCause(t, ctx, errHandbackSubmitInterrupted)
	if !errors.Is(context.Cause(ctx), errInflightSubmitInterrupted) {
		t.Fatal("a resubmit interrupted at the hand-back's end is not an interrupted submit")
	}
}

func TestExecutionDeadlineCancelDuringHandbackIsImmediate(t *testing.T) {
	for name, viaParent := range map[string]bool{"explicit cancel": false, "parent cancel": true} {
		t.Run(name, func(t *testing.T) {
			shortenExecutionDeadlineRecheck(t)
			shortenSubmitHandback(t, time.Hour)
			parent, cancelParent := context.WithCancel(context.Background())
			defer cancelParent()
			inflight := func(now time.Time) (time.Time, bool) { return now.Add(time.Hour), true }
			ctx, cancel := withExecutionDeadline(parent, 20*time.Millisecond, "task-1", inflight, noSubmitRunning)
			defer cancel()
			requireLive(t, ctx, 100*time.Millisecond, "in the hand-back")

			if viaParent {
				cancelParent()
			} else {
				cancel()
			}

			if waited := waitDone(t, ctx, time.Second); waited > 50*time.Millisecond {
				t.Fatalf("cancel took %s during the hand-back, want immediate", waited)
			}
			if executionTimedOut(ctx) {
				t.Fatalf("cancel during the hand-back was classified as an execution timeout (cause %v)", context.Cause(ctx))
			}
		})
	}
}

// Real marker semantics: a marker past its own deadline reads as not in
// flight, but its live process still counts as a running submit, so the end
// of the grace is an interruption, not a hand-back.
func TestExecutionDeadlineMarkerPastItsDeadlineWithLiveProcessIsInterrupted(t *testing.T) {
	shortenExecutionDeadlineRecheck(t)
	shortenSubmitHandback(t, time.Hour)
	root := t.TempDir()
	config := SupervisorConfig{ProjectRoot: root, Authority: models.AgentAuthority{ID: "coder-1", Generation: "generation-1"}}
	start := time.Now()
	remove := ops.WriteInflightSubmitMarkerForTest(root, config.Authority, "task-1", start.Add(200*time.Millisecond))
	defer remove()
	ctx, cancel := withExecutionDeadline(context.Background(), 20*time.Millisecond, "task-1", sessionInflightSubmit(config, "task-1"), sessionSubmitRunning(config, "task-1"))
	defer cancel()

	waitDone(t, ctx, 2*time.Second)

	if ended := time.Since(start); ended > 500*time.Millisecond {
		t.Fatalf("session ended %s after start, want at the marker's 200ms deadline", ended)
	}
	requireCause(t, ctx, errInflightSubmitInterrupted)
}

func TestExecutionDeadlineRemovedRealMarkerHandsBack(t *testing.T) {
	shortenExecutionDeadlineRecheck(t)
	shortenSubmitHandback(t, 200*time.Millisecond)
	root := t.TempDir()
	config := SupervisorConfig{ProjectRoot: root, Authority: models.AgentAuthority{ID: "coder-1", Generation: "generation-1"}}
	remove := ops.WriteInflightSubmitMarkerForTest(root, config.Authority, "task-1", time.Now().Add(time.Hour))
	ctx, cancel := withExecutionDeadline(context.Background(), 20*time.Millisecond, "task-1", sessionInflightSubmit(config, "task-1"), sessionSubmitRunning(config, "task-1"))
	defer cancel()
	requireLive(t, ctx, 100*time.Millisecond, "while the marker was present")

	remove()
	requireLive(t, ctx, 100*time.Millisecond, "during the hand-back")
	waitDone(t, ctx, time.Second)

	requireCause(t, ctx, errSubmitHandbackEnded)
}

// Without an in-flight submit, a session that outlives its execution timeout
// is still stopped and reported as a timeout.
func TestExecuteAgentTimesOutWithoutInflightSubmit(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	taskID, agentID := "task-1", "coder-1"
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, time.Now().UTC())
	task.AssignedTo = &agentID
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, statePath, state)
	mock := &MockLLMAgent{
		OnExecute: func(ctx context.Context, _ string, _ string, _ string, _ string, _ []string) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	config := SupervisorConfig{
		AgentID:          agentID,
		Role:             models.RoleCoder,
		ProjectRoot:      tmpDir,
		StatePath:        statePath,
		CLIName:          "codex",
		LLMAgent:         mock,
		ExecutionTimeout: 100 * time.Millisecond,
	}

	start := time.Now()
	exitCode, _, err := executeAgent(context.Background(), config, "prompt", nil, taskID, state.Config)

	if err != nil || exitCode != 1 {
		t.Fatalf("executeAgent = (%d, %v), want execution timeout exit 1", exitCode, err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("executeAgent took %s, want about the 100ms timeout", elapsed)
	}
}
