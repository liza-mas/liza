package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/ops"
)

// executionDeadlineRecheckInterval is how often a deferred execution timeout
// re-reads the in-flight submit marker; tests shorten it.
var executionDeadlineRecheckInterval = 5 * time.Second

// maxInflightSubmitGrace caps any deferral regardless of marker content; tests
// shorten it.
var maxInflightSubmitGrace = ops.MaxInflightSubmitGrace

// submitHandbackWindow is how long a session held past its execution timeout
// for its own submit stays live once that submit is no longer running, so the
// agent can act on the outcome (D-174); tests shorten it.
var submitHandbackWindow = 10 * time.Minute

var (
	// errInflightSubmitInterrupted: a submit was still running when the
	// session's deadline ended it.
	errInflightSubmitInterrupted = fmt.Errorf("in-flight submit still running at the session deadline: %w", context.DeadlineExceeded)
	// errHandbackSubmitInterrupted: the submit running at the end of the
	// hand-back was started during it.
	errHandbackSubmitInterrupted = fmt.Errorf("submit started during the hand-back: %w", errInflightSubmitInterrupted)
	// errSubmitHandbackEnded: the hand-back after the session's in-flight
	// submit stopped running ran out.
	errSubmitHandbackEnded = fmt.Errorf("hand-back after the in-flight submit ended: %w", context.DeadlineExceeded)
)

// inflightSubmit reports the fixed deadline of the session's own submit, if
// one is running (ops.InflightSubmitDeadline bound to one session).
type inflightSubmit func(now time.Time) (time.Time, bool)

// submitRunning reports whether the session's own submit is still running,
// past its marker's deadline included (ops.InflightSubmitRunning bound to one
// session).
type submitRunning func() bool

func sessionInflightSubmit(config SupervisorConfig, taskID string) inflightSubmit {
	return func(now time.Time) (time.Time, bool) {
		return ops.InflightSubmitDeadline(config.ProjectRoot, config.Authority, taskID, now)
	}
}

func sessionSubmitRunning(config SupervisorConfig, taskID string) submitRunning {
	return func() bool {
		return ops.InflightSubmitRunning(config.ProjectRoot, config.Authority, taskID)
	}
}

// withExecutionDeadline is context.WithTimeout whose expiry waits for the
// session's own in-flight submit (D-38). When timeout elapses with a valid
// marker, cancellation is deferred to that marker's deadline as first
// observed, never past timeout+maxInflightSubmitGrace. Once the submit is no
// longer running, the session gets submitHandbackWindow more to act on its
// outcome (D-174); later markers never extend either wait. A submit still
// running when the deferral ends cancels with errInflightSubmitInterrupted
// (errHandbackSubmitInterrupted for one started during the hand-back), an
// expired hand-back with errSubmitHandbackEnded, a plain expiry with
// context.DeadlineExceeded; all are execution timeouts. cancel cancels with
// context.Canceled.
func withExecutionDeadline(parent context.Context, timeout time.Duration, taskID string, inflight inflightSubmit, running submitRunning) (ctx context.Context, cancel context.CancelFunc) {
	ctx, cancelCause := context.WithCancelCause(parent)
	// Read before the goroutine starts: tests restore these while a cancelled
	// goroutine may still be running.
	recheck, grace, window := executionDeadlineRecheckInterval, maxInflightSubmitGrace, submitHandbackWindow
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		deadline, ok := inflight(time.Now())
		if !ok {
			cancelCause(context.DeadlineExceeded)
			return
		}
		end := minTime(deadline, time.Now().Add(grace))
		GetLogger().Info("Deferring execution timeout for in-flight submit", "task_id", taskID, "timeout", timeout, "until", end)
		var handback time.Time
		for {
			now := time.Now()
			live := running()
			switch {
			case handback.IsZero() && !live:
				handback = now.Add(window)
				end = handback
				GetLogger().Info("Handing the session back after its in-flight submit stopped running", "task_id", taskID, "until", handback)
			case !now.Before(end) && handback.IsZero():
				cancelCause(errInflightSubmitInterrupted)
				return
			case !now.Before(end) && live:
				cancelCause(errHandbackSubmitInterrupted)
				return
			case !now.Before(end):
				cancelCause(errSubmitHandbackEnded)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(min(recheck, end.Sub(now))):
			}
		}
	}()
	return ctx, func() { cancelCause(context.Canceled) }
}

// executionTimedOut reports whether ctx ended through its execution deadline,
// its own or a parent's.
func executionTimedOut(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), context.DeadlineExceeded)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// endSessionAfterInflightSubmit settles a session that ended past its
// execution timeout around its own submit (D-174): a task still executing
// under this agent is blocked with the cause, since the submit's outcome is
// not recorded on the task and a blind re-claim would repeat the batch. A task
// already admitted or moved ends the session as before. It returns the
// session's exit code.
func endSessionAfterInflightSubmit(config SupervisorConfig, taskID string, cause error) int {
	logger := GetLogger()
	reason := submitDeadlineBlockReason(cause)
	blocked, err := blockExecutingTaskFromSupervisor(db.For(config.StatePath), config.ProjectRoot, taskID, config.Authority, reason)
	switch {
	case err != nil:
		logger.Error("Failed to block task after the session deadline", "agent_id", config.AgentID, "task_id", taskID, "reason", reason, "error", err)
		return 1
	case !blocked:
		logger.Info("Session ended after its in-flight submit; task no longer executing under this agent", "agent_id", config.AgentID, "task_id", taskID, "cause", cause)
		return 1
	}
	logger.Error("Session deadline ended an unsettled submit; task blocked", "agent_id", config.AgentID, "task_id", taskID, "reason", reason)
	return 0
}

func submitDeadlineBlockReason(cause error) string {
	switch {
	case errors.Is(cause, errHandbackSubmitInterrupted):
		return fmt.Sprintf("admission interrupted by session deadline: a submit started during the %s hand-back after the execution timeout was still running when the hand-back ended; its outcome is unknown", submitHandbackWindow)
	case errors.Is(cause, errInflightSubmitInterrupted):
		return "admission interrupted by session deadline: the in-flight submit was still running when its grace after the execution timeout ended (its marker's deadline or the grace cap); its outcome is unknown"
	}
	return fmt.Sprintf("session deadline: the in-flight submit is no longer observable and the task is still executing unadmitted; the agent did not settle it within the %s hand-back after the execution timeout; admission outcome unknown (see the agent output)", submitHandbackWindow)
}
