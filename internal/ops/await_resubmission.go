package ops

import (
	"context"
	stderrors "errors"
	"fmt"
	"log"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

// AwaitResubmissionOutcome is the closed set of AwaitResubmissionResult.Verdict
// values. The result field stays a string; a constant reaches it by conversion.
type AwaitResubmissionOutcome string

// Resubmission verdict constants for AwaitResubmissionResult.Verdict.
const (
	ResubmissionResubmitted AwaitResubmissionOutcome = "RESUBMITTED"
	ResubmissionTerminal    AwaitResubmissionOutcome = "TERMINAL"
	ResubmissionTimeout     AwaitResubmissionOutcome = "TIMEOUT"
	ResubmissionAborted     AwaitResubmissionOutcome = "ABORTED"
	ResubmissionPoll        AwaitResubmissionOutcome = "POLL" // Blocking interval expired, caller should retry
	ResubmissionPaused      AwaitResubmissionOutcome = "PAUSED"
)

// Operation labels the work-admission gate reports for an await-resubmission
// wait and for its reclaim.
const (
	awaitResubmissionOperation        = "await-resubmission"
	awaitResubmissionReclaimOperation = "await-resubmission reclaim"
)

// AwaitResubmissionResult holds the outcome of blocking on a doer resubmission.
type AwaitResubmissionResult struct {
	Verdict      string            `json:"verdict"`       // One of the Resubmission* constants
	TaskStatus   models.TaskStatus `json:"task_status"`   // Final observed task status
	Reason       string            `json:"reason"`        // Terminal explanation (empty on RESUBMITTED)
	BaseCommit   string            `json:"base_commit"`   // Fresh review base SHA to use on RESUBMITTED
	ReviewCommit string            `json:"review_commit"` // New commit SHA to review (on RESUBMITTED)
	ReviewCycle  int               `json:"review_cycle"`  // Current review cycle count
	SafeAction   string            `json:"safe_action,omitempty"`
}

type awaitResubmissionWatcher interface {
	Events() <-chan struct{}
	Errors() <-chan error
	Close() error
}

var newAwaitResubmissionWatcher = func(bb *db.Blackboard) (awaitResubmissionWatcher, error) {
	return bb.WatchForChanges()
}

// AwaitResubmissionOptions controls the periodic checks used while waiting.
// The zero value uses the project's shared await polling policy.
type AwaitResubmissionOptions struct {
	AbortPollInterval    time.Duration
	FallbackPollInterval time.Duration
	// PollOnTimeout distinguishes a foreground slice from final budget expiry.
	// Only the command layer sets it after deriving the remaining total budget.
	PollOnTimeout bool
}

func (opts AwaitResubmissionOptions) normalized(config models.Config) (AwaitResubmissionOptions, error) {
	var err error
	opts.AbortPollInterval, opts.FallbackPollInterval, err = normalizeAwaitPolling(config, opts.AbortPollInterval, opts.FallbackPollInterval)
	return opts, err
}

// AwaitResubmission blocks until a doer resubmits after a rejection.
// It validates preconditions, acquires review ownership (ReviewingBy on task,
// agent status=WAITING), then blocks on an event loop until the task status
// leaves the waiting set (rejected/implementing/executing).
func AwaitResubmission(ctx context.Context, projectRoot, taskID, agentID string, timeout time.Duration) (*AwaitResubmissionResult, error) {
	return awaitResubmissionWithOptions(ctx, projectRoot, taskID, agentID, nil, timeout, AwaitResubmissionOptions{})
}

// AwaitResubmissionWithOptions is AwaitResubmission with configurable polling intervals.
func AwaitResubmissionWithOptions(ctx context.Context, projectRoot, taskID, agentID string, timeout time.Duration, opts AwaitResubmissionOptions) (*AwaitResubmissionResult, error) {
	return awaitResubmissionWithOptions(ctx, projectRoot, taskID, agentID, nil, timeout, opts)
}

// AwaitResubmissionWithAuthority is the authenticated command entry point.
func AwaitResubmissionWithAuthority(ctx context.Context, projectRoot, taskID string, authority models.AgentAuthority, timeout time.Duration) (*AwaitResubmissionResult, error) {
	return awaitResubmissionWithOptions(ctx, projectRoot, taskID, authority.ID, &authority, timeout, AwaitResubmissionOptions{})
}

// AwaitResubmissionWithAuthorityOptions is AwaitResubmissionWithAuthority with
// configurable polling intervals.
func AwaitResubmissionWithAuthorityOptions(ctx context.Context, projectRoot, taskID string, authority models.AgentAuthority, timeout time.Duration, opts AwaitResubmissionOptions) (*AwaitResubmissionResult, error) {
	return awaitResubmissionWithOptions(ctx, projectRoot, taskID, authority.ID, &authority, timeout, opts)
}

func awaitResubmissionWithOptions(ctx context.Context, projectRoot, taskID, agentID string, authority *models.AgentAuthority, timeout time.Duration, opts AwaitResubmissionOptions) (result *AwaitResubmissionResult, resultErr error) {
	entry, settled, err := enterAwaitResubmission(projectRoot, taskID, agentID, authority, opts)
	if entry == nil {
		return settled, err
	}

	// Early resubmission: if the task is already in submitted status, skip the
	// wait loop and validate evidence before acquiring any ownership.
	submitted, _ := entry.resolver.SubmittedStatus(entry.task.RolePair)
	if entry.task.Status == submitted {
		return entry.reclaimEarly(projectRoot, agentID, authority)
	}

	// Acquire ownership only when actually waiting for the doer's submission.
	ownership, settled, err := entry.acquireReservation(agentID, authority, timeout)
	if ownership == nil {
		return settled, err
	}
	defer func() {
		var evidenceErr *AcceptanceEvidenceError
		if stderrors.As(resultErr, &evidenceErr) {
			cleanupErr := ownership.undoAfterEvidenceRefusal(entry.bb, agentID, taskID, authority)
			resultErr = joinAwaitCleanupError(resultErr, cleanupErr)
		}
	}()

	wait := &awaitedResubmission{
		projectRoot: projectRoot,
		bb:          entry.bb,
		taskID:      taskID,
		agentID:     agentID,
		authority:   authority,
		reservation: ownership.reservation,
		resolver:    entry.resolver,
		rolePair:    entry.task.RolePair,
		status:      entry.task.Status,
		opts:        entry.opts,
	}
	return wait.run(ctx, time.Now().Add(timeout))
}

// resubmissionEntry is what the preconditions of an await-resubmission settle:
// the observed task, the pipeline that classifies its statuses, the polling
// options and the reservation the agent already holds, if any.
type resubmissionEntry struct {
	bb       *db.Blackboard
	task     *models.Task
	resolver *pipeline.Resolver
	retained *reviewReservation
	opts     AwaitResubmissionOptions
}

// enterAwaitResubmission checks the preconditions of an await-resubmission from
// one state snapshot, before any write. It returns the entry to wait on, or a nil
// entry with the outcome that already settles the call.
func enterAwaitResubmission(projectRoot, taskID, agentID string, authority *models.AgentAuthority, opts AwaitResubmissionOptions) (*resubmissionEntry, *AwaitResubmissionResult, error) {
	if taskID == "" {
		return nil, nil, &PreconditionError{Reason: "task ID is required"}
	}
	if agentID == "" {
		return nil, nil, &PreconditionError{Reason: "agent ID is required"}
	}

	bb := db.For(paths.New(projectRoot).StatePath())

	// Read state and find task.
	state, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, nil, err
	}
	opts, err = opts.normalized(state.Config)
	if err != nil {
		return nil, nil, err
	}

	// Verify agent exists in state (early exits below bypass acquireReviewOwnership
	// which would otherwise catch nonexistent agents).
	if _, ok := state.Agents[agentID]; !ok {
		return nil, nil, &errors.NotFoundError{Entity: notFoundEntityAgent, ID: agentID}
	}

	// Verify agent was the last rejecting reviewer (even for early exits).
	if err := checkLastRejectingReviewer(task, agentID); err != nil {
		return nil, nil, err
	}
	retained := retainedReservation(state, task, agentID)
	if state.Config.Mode.HaltsWork() {
		result, err := finishPausedAwaitResubmission(bb, task, agentID, authority, retained, state.Config.Mode)
		return nil, result, err
	}

	// If the task was escalated (BLOCKED or terminal) by the verdict, return
	// immediately so the reviewer can exit cleanly without parsing verdict details.
	if task.Status == models.TaskStatusBlocked || task.Status.IsTerminal() {
		return nil, &AwaitResubmissionResult{
			Verdict:    string(ResubmissionAborted),
			TaskStatus: task.Status,
			Reason:     fmt.Sprintf("task already %s — no resubmission expected", task.Status),
		}, nil
	}

	// Resolve pipeline statuses for the task's role-pair.
	if task.RolePair == "" {
		return nil, nil, &PreconditionError{Reason: fmt.Sprintf("task %s has no role_pair set", taskID)}
	}
	resolver, _, resolverErr := loadResolver(projectRoot)
	if resolverErr != nil {
		return nil, nil, &OperationalError{Message: "failed to load pipeline config", Err: resolverErr}
	}

	// Check task status is rejected or already submitted (fast-doer edge case).
	if err := checkResubmissionPrecondition(task, resolver); err != nil {
		return nil, nil, err
	}
	return &resubmissionEntry{bb: bb, task: task, resolver: resolver, retained: retained, opts: opts}, nil, nil
}

// reclaimEarly reclaims a task the doer resubmitted before the reviewer called
// await. An evidence refusal releases the reservation a retained wait still holds.
func (e *resubmissionEntry) reclaimEarly(projectRoot, agentID string, authority *models.AgentAuthority) (*AwaitResubmissionResult, error) {
	result, err := reclaimForReview(projectRoot, e.bb, e.task.ID, agentID, authority, e.resolver, e.task.RolePair, e.retained)
	var evidenceErr *AcceptanceEvidenceError
	if e.retained != nil && stderrors.As(err, &evidenceErr) {
		return finishAwaitResubmission(e.bb, agentID, e.task.ID, authority, e.retained, result, err)
	}
	return result, err
}

// acquireReservation takes the review ownership the wait holds. A nil snapshot
// means the acquisition already settled the call with the returned outcome.
func (e *resubmissionEntry) acquireReservation(agentID string, authority *models.AgentAuthority, timeout time.Duration) (*reviewOwnershipSnapshot, *AwaitResubmissionResult, error) {
	ownership, err := acquireReviewOwnershipSnapshotChecked(e.bb, agentID, e.task.ID, authority, timeout, e.retained)
	if err == nil {
		return ownership, nil, nil
	}
	var halted *SystemHaltedError
	if stderrors.As(err, &halted) {
		result, pausedErr := finishPausedAwaitResubmission(e.bb, e.task, agentID, authority, e.retained, halted.Mode)
		return nil, result, pausedErr
	}
	if stderrors.Is(err, errReviewReservationLost) {
		return nil, reviewOwnershipLostResult(e.task.Status), nil
	}
	return nil, nil, &OperationalError{Message: "failed to acquire review ownership", Err: err}
}

// awaitedResubmission is one wait on a doer's resubmission while holding a
// reservation: it observes state until the task leaves the waiting set, the
// system stops or halts, the reservation is lost or the deadline passes.
type awaitedResubmission struct {
	projectRoot string
	bb          *db.Blackboard
	taskID      string
	agentID     string
	authority   *models.AgentAuthority
	reservation *reviewReservation
	resolver    *pipeline.Resolver
	rolePair    string
	// status is the task status the interval's result reports on expiry.
	status models.TaskStatus
	opts   AwaitResubmissionOptions
}

// resubmissionEnd is the outcome that ends a wait.
type resubmissionEnd struct {
	result *AwaitResubmissionResult
	err    error
}

func endResubmission(result *AwaitResubmissionResult, err error) *resubmissionEnd {
	return &resubmissionEnd{result: result, err: err}
}

func (w *awaitedResubmission) finish(result *AwaitResubmissionResult, err error) (*AwaitResubmissionResult, error) {
	return finishAwaitResubmission(w.bb, w.agentID, w.taskID, w.authority, w.reservation, result, err)
}

// observe classifies one observed state: nil keeps waiting. abortedStatus is the
// task status an ABORTED result reports when the system has stopped.
func (w *awaitedResubmission) observe(state *models.State, abortedStatus models.TaskStatus) *resubmissionEnd {
	if state.Config.Mode == models.SystemModeStopped {
		return endResubmission(w.finish(&AwaitResubmissionResult{Verdict: string(ResubmissionAborted), TaskStatus: abortedStatus}, nil))
	}
	task := state.FindTask(w.taskID)
	if task == nil {
		return endResubmission(w.finish(&AwaitResubmissionResult{
			Verdict: string(ResubmissionTerminal),
			Reason:  "task disappeared from state",
		}, nil))
	}
	if state.Config.Mode.HaltsWork() {
		return endResubmission(finishPausedAwaitResubmission(w.bb, task, w.agentID, w.authority, w.reservation, state.Config.Mode))
	}
	if rc := checkResubmissionStatus(task, w.resolver, w.rolePair); rc != nil {
		return endResubmission(handleResubmissionResult(w.projectRoot, w.bb, task, w.agentID, w.authority, w.resolver, w.rolePair, w.reservation))
	}
	if reservationLost(w.bb, w.reservation, state, task, w.agentID) {
		return endResubmission(w.finish(reviewOwnershipLostResult(task.Status), nil))
	}
	return nil
}

// run blocks on state-file events, with a periodic abort check, until the wait
// ends. A watcher that cannot start or fails hands over to polling.
func (w *awaitedResubmission) run(ctx context.Context, deadline time.Time) (*AwaitResubmissionResult, error) {
	watcher, watchErr := newAwaitResubmissionWatcher(w.bb)
	if watchErr != nil {
		return w.poll(ctx, deadline)
	}
	defer watcher.Close()

	deadlineTimer := time.NewTimer(time.Until(deadline))
	defer deadlineTimer.Stop()

	abortTicker := time.NewTicker(w.opts.AbortPollInterval)
	defer abortTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return w.finish(nil, ctx.Err())

		case <-abortTicker.C:
			state, err := w.bb.Read()
			if err != nil {
				continue
			}
			if end := w.observe(state, w.status); end != nil {
				return end.result, end.err
			}

		case <-watcher.Events():
			state, err := w.bb.ReadCached()
			if err != nil {
				continue
			}
			if end := w.observe(state, w.status); end != nil {
				return end.result, end.err
			}

		case watcherErr := <-watcher.Errors():
			log.Printf("Watcher error, falling back to polling: %v", watcherErr)
			watcher.Close()
			return w.poll(ctx, deadline)

		case <-deadlineTimer.C:
			return finishResubmissionInterval(w.bb, w.taskID, w.agentID, w.authority, w.reservation, w.status, w.opts.PollOnTimeout)
		}
	}
}

// poll is the polling fallback for when fsnotify is unavailable.
// It checks state at the resolved interval until a resubmission or the deadline.
func (w *awaitedResubmission) poll(ctx context.Context, deadline time.Time) (*AwaitResubmissionResult, error) {
	ticker := time.NewTicker(w.opts.FallbackPollInterval)
	defer ticker.Stop()
	deadlineTimer := time.NewTimer(time.Until(deadline))
	defer deadlineTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return w.finish(nil, ctx.Err())

		case <-ticker.C:
			state, err := w.bb.ReadCached()
			if err != nil {
				continue
			}
			if end := w.observe(state, ""); end != nil {
				return end.result, end.err
			}
			w.status = state.FindTask(w.taskID).Status

		case <-deadlineTimer.C:
			return finishResubmissionInterval(w.bb, w.taskID, w.agentID, w.authority, w.reservation, w.status, w.opts.PollOnTimeout)
		}
	}
}

// checkResubmissionPrecondition verifies the task is in a status where
// awaiting resubmission is valid: rejected, executing (fast-coder claimed
// before reviewer called await), or already submitted (fast-doer).
func checkResubmissionPrecondition(task *models.Task, resolver *pipeline.Resolver) error {
	rejected, err := resolver.RejectedStatus(task.RolePair)
	if err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q — check pipeline.yaml config", task.RolePair)}
	}
	submitted, err := resolver.SubmittedStatus(task.RolePair)
	if err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q — check pipeline.yaml config", task.RolePair)}
	}
	executing, err := resolver.ExecutingStatus(task.RolePair)
	if err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q — check pipeline.yaml config", task.RolePair)}
	}

	if task.Status == rejected || task.Status == submitted || task.Status == executing {
		return nil
	}

	return &PreconditionError{
		Reason: fmt.Sprintf("task %s is not in a rejected, executing, or submitted status (current: %s, expected: %s, %s, or %s)",
			task.ID, task.Status, rejected, executing, submitted),
	}
}

// checkLastRejectingReviewer verifies the agent was the last to reject this task.
func checkLastRejectingReviewer(task *models.Task, agentID string) error {
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		if entry.Event == models.TaskEventRejected {
			if entry.Agent != nil && *entry.Agent == agentID {
				return nil
			}
			return &PreconditionError{
				Reason: fmt.Sprintf("agent %s is not the last rejecting reviewer of task %s", agentID, task.ID),
			}
		}
	}
	return &PreconditionError{
		Reason: fmt.Sprintf("task %s has no rejection history", task.ID),
	}
}

// resubmissionCheck holds the result of checking whether a resubmission has arrived.
type resubmissionCheck struct {
	status models.TaskStatus
}

// checkResubmissionStatus determines if the task has left the waiting set.
// Returns nil if still waiting; returns the observed status if a resubmission
// or terminal state has been reached.
func checkResubmissionStatus(task *models.Task, resolver *pipeline.Resolver, rolePair string) *resubmissionCheck {
	submitted, _ := resolver.SubmittedStatus(rolePair)
	approved, _ := resolver.ApprovedStatus(rolePair)

	// Resubmission detected.
	if task.Status == submitted {
		return &resubmissionCheck{status: task.Status}
	}

	// Terminal states.
	if task.Status == approved ||
		task.Status == models.TaskStatusBlocked ||
		task.Status == models.TaskStatusSuperseded ||
		task.Status == models.TaskStatusAbandoned ||
		task.Status == models.TaskStatusIntegrationFailed ||
		task.Status == models.TaskStatusMerged {
		return &resubmissionCheck{status: task.Status}
	}

	// All other statuses (rejected, implementing, executing, etc.) — keep waiting.
	return nil
}

// handleResubmissionResult maps the observed task status to an AwaitResubmissionResult.
func handleResubmissionResult(projectRoot string, bb *db.Blackboard, task *models.Task, agentID string, authority *models.AgentAuthority, resolver *pipeline.Resolver, rolePair string, reservation *reviewReservation) (*AwaitResubmissionResult, error) {
	submitted, _ := resolver.SubmittedStatus(rolePair)

	if task.Status == submitted {
		return reclaimForReview(projectRoot, bb, task.ID, agentID, authority, resolver, rolePair, reservation)
	}

	// Terminal state — release ownership and report.
	return finishAwaitResubmission(bb, agentID, task.ID, authority, reservation, &AwaitResubmissionResult{
		Verdict:    string(ResubmissionTerminal),
		TaskStatus: task.Status,
		Reason:     fmt.Sprintf("task entered terminal status: %s", task.Status),
	}, nil)
}

// latestRejectionByAgent returns when this agent last rejected the task.
func latestRejectionByAgent(task *models.Task, agentID string) (time.Time, bool) {
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		if entry.Event != models.TaskEventRejected {
			continue
		}
		if entry.Agent == nil || *entry.Agent != agentID {
			continue
		}
		return entry.Time, true
	}
	return time.Time{}, false
}

// AwaitResubmissionRemainingBudget reports how much of total is left for this
// wait, measured from the reviewer's most recent rejection. Mirrors
// AwaitVerdictRemainingBudget: the bound holds without the caller carrying a
// value between invocations.
func AwaitResubmissionRemainingBudget(projectRoot, taskID, agentID string, total time.Duration) time.Duration {
	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	_, task, err := readTaskState(bb, taskID)
	if err != nil || task == nil {
		return cappedAwaitBudget(total)
	}
	rejectedAt, ok := latestRejectionByAgent(task, agentID)
	if !ok {
		return cappedAwaitBudget(total)
	}
	return remainingFromAnchor(rejectedAt, total)
}
