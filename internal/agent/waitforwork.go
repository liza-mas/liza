package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// loadResolver loads the pipeline resolver for work detection, logging a warning
// on failure. Returns nil on error (callers treat nil as "no work visible").
func loadResolver(projectRoot string) models.PipelineResolver {
	pr, err := ops.LoadResolverForModels(projectRoot)
	if err != nil {
		GetLogger().Warn("Failed to load pipeline resolver for work detection", "error", err)
	}
	return pr
}

// nonZeroOr returns val if positive, otherwise fallback.
func nonZeroOr(val, fallback int) int {
	if val > 0 {
		return val
	}
	return fallback
}

// errPoolSurplus ends a wait because the agent's role is over its
// max-instances and the agent holds no in-flight work; the supervisor then
// tries to retire it under the state lock.
var errPoolSurplus = errors.New("agent role over max-instances")

// surplusCheckFunc reports, on a state snapshot, whether the waiting agent is
// surplus to its role. Nil disables the check.
type surplusCheckFunc func(*models.State) bool

// workCheckFunc checks if work is available for an agent role.
// Returns (hasWork, logMessage). If logMessage is non-empty, it will be printed when work is found.
type workCheckFunc func(*models.State) (hasWork bool, logMessage string)

// stateWatcher abstracts file-change notification so tests can inject
// a silent watcher to exercise the abortTicker fallback path.
type stateWatcher interface {
	Events() <-chan struct{}
	Errors() <-chan error
	Close() error
}

// abortTickInterval is the period of the ABORT/TOCTOU fallback tick in
// waitForWorkEventDriven. Both ABORT and new work are state.yaml changes that
// fsnotify already delivers, so this tick only covers the gap between the
// initial check and watcher setup, and events fsnotify misses. Every tick
// re-reads state in every waiting supervisor, so it is kept well above the
// one-second scale that made idle supervisors a dominant CPU cost.
// Overridable in tests.
var abortTickInterval = 5 * time.Second

// newStateWatcher creates a watcher for state file changes.
// Overridable in tests to inject a silent (no-event) watcher.
var newStateWatcher = func(bb *db.Blackboard) (stateWatcher, error) {
	return bb.WatchForChanges()
}

// waitForWorkEventDriven is a generic event-driven wait implementation for all agent roles.
// It uses fsnotify to detect state changes and wake immediately when work becomes available.
func waitForWorkEventDriven(
	ctx context.Context,
	bb *db.Blackboard,
	projectRoot string,
	pollInterval, maxWait time.Duration,
	checkWork workCheckFunc,
	surplus surplusCheckFunc,
) (bool, error) {
	// Check context cancellation before doing any work.
	// If we skip this check, and work is available, we'd return true immediately
	// even if the context was already cancelled, causing the supervisor to
	// continue running when it should stop.
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	deadline := time.Now().Add(maxWait)

	if done, hasWork, err := observeWaitState(bb.ReadCached, checkWork, surplus, true); done {
		return hasWork, err
	}

	// Try to set up event-driven watching
	watcher, err := newStateWatcher(bb)
	if err != nil {
		// Fallback to polling if watcher fails
		return waitForWorkPolling(ctx, bb, projectRoot, pollInterval, maxWait, checkWork, surplus)
	}
	defer watcher.Close()

	return watchForWork(ctx, bb, projectRoot, pollInterval, maxWait, deadline, watcher, checkWork, surplus)
}

// watchForWork is the watcher and timer lifetime of the event-driven wait: it
// re-evaluates state on every watcher event and on the fallback tick until the
// deadline, falling back to polling when the watcher reports an error.
func watchForWork(
	ctx context.Context,
	bb *db.Blackboard,
	projectRoot string,
	pollInterval, maxWait time.Duration,
	deadline time.Time,
	watcher stateWatcher,
	checkWork workCheckFunc,
	surplus surplusCheckFunc,
) (bool, error) {
	// Add ticker for periodic ABORT checks (file-based fallback).
	// Keep this well below typical maxWait to avoid racing with context deadlines.
	abortTicker := time.NewTicker(abortTickInterval)
	defer abortTicker.Stop()

	// Deadline timer — created once to avoid timer leak in the select loop
	deadlineTimer := time.NewTimer(time.Until(deadline))
	defer deadlineTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()

		case <-abortTicker.C:
			// Also check for work: covers the TOCTOU race where state
			// changed between the initial check and watcher setup.
			if done, hasWork, err := observeWaitState(bb.ReadCached, checkWork, surplus, false); done {
				return hasWork, err
			}
			if time.Now().After(deadline) {
				return false, nil
			}

		case <-watcher.Events():
			if done, hasWork, err := observeWaitState(bb.ReadCached, checkWork, surplus, true); done {
				return hasWork, err
			}
			if time.Now().After(deadline) {
				return false, nil
			}

		case err := <-watcher.Errors():
			// Watcher error, fallback to polling
			GetLogger().Warn("Watcher error, falling back to polling", "error", err)
			watcher.Close()
			return waitForWorkPolling(ctx, bb, projectRoot, pollInterval, maxWait, checkWork, surplus)

		case <-deadlineTimer.C:
			return false, nil
		}
	}
}

// observeWaitState reads one state snapshot with read and evaluates it. done
// reports that the wait ends with hasWork and err.
func observeWaitState(read func() (*models.State, error), checkWork workCheckFunc, surplus surplusCheckFunc, diagnoseIdle bool) (done, hasWork bool, err error) {
	state, err := read()
	if err != nil {
		return true, false, fmt.Errorf("failed to read state: %w", err)
	}
	return evaluateWaitState(state, checkWork, surplus, diagnoseIdle)
}

// evaluateWaitState is the snapshot predicate of every wait. Precedence: a
// stopped system ends the wait without work, then a surplus agent ends it with
// errPoolSurplus, then available work ends it with work. A snapshot with none
// of them keeps the wait going; when diagnoseIdle is set and diagnostic logging
// is on, the work check's message is logged for it.
func evaluateWaitState(state *models.State, checkWork workCheckFunc, surplus surplusCheckFunc, diagnoseIdle bool) (done, hasWork bool, err error) {
	logger := GetLogger()

	// Check for ABORT before checking for work
	if stopped, reason := isSystemStopped(state); stopped {
		logger.Info("ABORT detected", "reason", reason)
		return true, false, nil
	}
	if surplus != nil && surplus(state) {
		return true, false, errPoolSurplus
	}

	hasWork, logMsg := checkWork(state)
	if hasWork {
		if logMsg != "" {
			logger.Info(logMsg)
		}
		return true, true, nil
	}
	if diagnoseIdle && state.Config.DiagnosticLogging && logMsg != "" {
		// Only show "no work" diagnostics if enabled
		logger.Info(logMsg)
	}
	return false, false, nil
}

// waitForWorkPolling is a generic polling wait implementation for all agent roles.
// This is used as a fallback when fsnotify is unavailable or encounters errors.
func waitForWorkPolling(
	ctx context.Context,
	bb *db.Blackboard,
	projectRoot string,
	pollInterval, maxWait time.Duration,
	checkWork workCheckFunc,
	surplus surplusCheckFunc,
) (bool, error) {
	deadline := time.Now().Add(maxWait)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-ticker.C:
			if done, hasWork, err := observeWaitState(bb.ReadSnapshot, checkWork, surplus, true); done {
				return hasWork, err
			}
			if time.Now().After(deadline) {
				return false, nil
			}
		}
	}
}

func isResumableHandoff(task *models.Task, agentID string, pr models.PipelineResolver) bool {
	return models.IsExecutingStatus(task, pr) &&
		task.HandoffPending &&
		task.AssignedTo != nil &&
		*task.AssignedTo == agentID
}

func countResumableHandoffTasks(state *models.State, agentID string, pr models.PipelineResolver) int {
	count := 0
	for i := range state.Tasks {
		if isResumableHandoff(&state.Tasks[i], agentID, pr) {
			count++
		}
	}
	return count
}
