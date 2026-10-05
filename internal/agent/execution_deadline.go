package agent

import (
	"context"
	"time"

	"github.com/liza-mas/liza/internal/ops"
)

// executionDeadlineRecheckInterval is how often a deferred execution timeout
// re-reads the in-flight submit marker; tests shorten it.
var executionDeadlineRecheckInterval = 5 * time.Second

// maxInflightSubmitGrace caps any deferral regardless of marker content; tests
// shorten it.
var maxInflightSubmitGrace = ops.MaxInflightSubmitGrace

// inflightSubmit reports the fixed deadline of the session's own submit, if
// one is running (ops.InflightSubmitDeadline bound to one session).
type inflightSubmit func(now time.Time) (time.Time, bool)

func sessionInflightSubmit(config SupervisorConfig, taskID string) inflightSubmit {
	return func(now time.Time) (time.Time, bool) {
		return ops.InflightSubmitDeadline(config.ProjectRoot, config.Authority, taskID, now)
	}
}

// withExecutionDeadline is context.WithTimeout whose expiry waits for the
// session's own in-flight submit (D-38). When timeout elapses with a valid
// marker, cancellation is deferred to that marker's deadline as first observed,
// never past timeout+maxInflightSubmitGrace, and happens as soon as the
// marker ends. Expiry cancels with cause context.DeadlineExceeded; cancel
// cancels with context.Canceled.
func withExecutionDeadline(parent context.Context, timeout time.Duration, taskID string, inflight inflightSubmit) (ctx context.Context, cancel context.CancelFunc) {
	ctx, cancelCause := context.WithCancelCause(parent)
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		var end time.Time
		hardCap := time.Now().Add(maxInflightSubmitGrace)
		for {
			now := time.Now()
			deadline, ok := inflight(now)
			if end.IsZero() && ok {
				end = minTime(deadline, hardCap)
				GetLogger().Info("Deferring execution timeout for in-flight submit", "task_id", taskID, "timeout", timeout, "until", end)
			}
			if !ok || !now.Before(end) {
				cancelCause(context.DeadlineExceeded)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(min(executionDeadlineRecheckInterval, end.Sub(now))):
			}
		}
	}()
	return ctx, func() { cancelCause(context.Canceled) }
}

// executionTimedOut reports whether ctx ended through its execution deadline,
// its own or a parent's.
func executionTimedOut(ctx context.Context) bool {
	return context.Cause(ctx) == context.DeadlineExceeded
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
