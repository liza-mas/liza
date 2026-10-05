package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func shortenExecutionDeadlineRecheck(t *testing.T) {
	t.Helper()
	previous := executionDeadlineRecheckInterval
	executionDeadlineRecheckInterval = 10 * time.Millisecond
	t.Cleanup(func() { executionDeadlineRecheckInterval = previous })
}

func noInflightSubmit(time.Time) (time.Time, bool) { return time.Time{}, false }

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

func TestExecutionDeadlineExpiresWithoutInflightSubmit(t *testing.T) {
	ctx, cancel := withExecutionDeadline(context.Background(), 50*time.Millisecond, "task-1", noInflightSubmit)
	defer cancel()

	waitDone(t, ctx, time.Second)

	if !executionTimedOut(ctx) {
		t.Fatalf("cause = %v, want execution timeout", context.Cause(ctx))
	}
}

func TestExecutionDeadlineCancelIsNotATimeout(t *testing.T) {
	ctx, cancel := withExecutionDeadline(context.Background(), time.Hour, "task-1", noInflightSubmit)

	cancel()
	waitDone(t, ctx, time.Second)

	if executionTimedOut(ctx) {
		t.Fatal("explicit cancel was classified as an execution timeout")
	}
}

func TestExecutionDeadlineParentTimeoutIsATimeout(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelParent()
	ctx, cancel := withExecutionDeadline(parent, time.Hour, "task-1", noInflightSubmit)
	defer cancel()

	waitDone(t, ctx, time.Second)

	if !executionTimedOut(ctx) {
		t.Fatalf("cause = %v, want the parent's deadline to count as a timeout", context.Cause(ctx))
	}
}

func TestExecutionDeadlineWaitsForInflightSubmitUntilItEnds(t *testing.T) {
	shortenExecutionDeadlineRecheck(t)
	var running atomic.Bool
	running.Store(true)
	inflight := func(now time.Time) (time.Time, bool) { return now.Add(time.Hour), running.Load() }
	ctx, cancel := withExecutionDeadline(context.Background(), 20*time.Millisecond, "task-1", inflight)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatal("deadline fired while the submit was in flight")
	case <-time.After(200 * time.Millisecond):
	}
	running.Store(false)
	if waited := waitDone(t, ctx, time.Second); waited > 100*time.Millisecond {
		t.Fatalf("deadline fired %s after the submit ended, want within one recheck", waited)
	}
	if !executionTimedOut(ctx) {
		t.Fatalf("cause = %v, want execution timeout", context.Cause(ctx))
	}
}

func TestExecutionDeadlineDeferralIsFixedAtFirstObservation(t *testing.T) {
	shortenExecutionDeadlineRecheck(t)
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
	ctx, cancel := withExecutionDeadline(context.Background(), 20*time.Millisecond, "task-1", inflight)
	defer cancel()

	waitDone(t, ctx, 2*time.Second)

	if ended := time.Since(start); ended > first.Sub(start)+150*time.Millisecond {
		t.Fatalf("deferral ended %s after start, want near the first observed deadline %s", ended, first.Sub(start))
	}
}

func TestExecutionDeadlineHardCapBoundsAnyMarker(t *testing.T) {
	shortenExecutionDeadlineRecheck(t)
	previous := maxInflightSubmitGrace
	maxInflightSubmitGrace = 100 * time.Millisecond
	t.Cleanup(func() { maxInflightSubmitGrace = previous })
	inflight := func(now time.Time) (time.Time, bool) { return now.Add(24 * time.Hour), true }
	ctx, cancel := withExecutionDeadline(context.Background(), 20*time.Millisecond, "task-1", inflight)
	defer cancel()

	waitDone(t, ctx, 2*time.Second)

	if !executionTimedOut(ctx) {
		t.Fatalf("cause = %v, want execution timeout at the hard cap", context.Cause(ctx))
	}
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
