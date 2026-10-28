package ops

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

type awaitVerdictWatcher interface {
	Events() <-chan struct{}
	Errors() <-chan error
	Close() error
}

var newAwaitVerdictWatcher = func(bb *db.Blackboard) (awaitVerdictWatcher, error) {
	return bb.WatchForChanges()
}

// AwaitVerdictOptions controls the periodic checks used while waiting. The
// zero value uses the project's shared await polling policy.
type AwaitVerdictOptions struct {
	AbortPollInterval    time.Duration
	FallbackPollInterval time.Duration
}

func (opts AwaitVerdictOptions) normalized(config models.Config) (AwaitVerdictOptions, error) {
	var err error
	opts.AbortPollInterval, opts.FallbackPollInterval, err = normalizeAwaitPolling(config, opts.AbortPollInterval, opts.FallbackPollInterval)
	return opts, err
}

var awaitVerdictNow = time.Now

var runAwaitVerdictPolling = awaitVerdictPolling

// AwaitVerdict blocks until a review verdict arrives for a submitted task.
// It validates preconditions, acquires ownership (agent status=WAITING,
// CurrentTask=taskID), checks budget, then blocks on an event loop until
// the task status leaves the submitted/reviewing/partially-approved set.
func AwaitVerdict(ctx context.Context, projectRoot, taskID, agentID string, timeout time.Duration) (*AwaitVerdictResult, error) {
	return awaitVerdictWithOptions(ctx, projectRoot, taskID, agentID, nil, timeout, AwaitVerdictOptions{})
}

// AwaitVerdictWithOptions is AwaitVerdict with configurable polling intervals.
func AwaitVerdictWithOptions(ctx context.Context, projectRoot, taskID, agentID string, timeout time.Duration, opts AwaitVerdictOptions) (*AwaitVerdictResult, error) {
	return awaitVerdictWithOptions(ctx, projectRoot, taskID, agentID, nil, timeout, opts)
}

// AwaitVerdictWithAuthority is the authenticated command entry point.
func AwaitVerdictWithAuthority(ctx context.Context, projectRoot, taskID string, authority models.AgentAuthority, timeout time.Duration) (*AwaitVerdictResult, error) {
	return awaitVerdictWithOptions(ctx, projectRoot, taskID, authority.ID, &authority, timeout, AwaitVerdictOptions{})
}

// AwaitVerdictWithAuthorityOptions is AwaitVerdictWithAuthority with
// configurable polling intervals.
func AwaitVerdictWithAuthorityOptions(ctx context.Context, projectRoot, taskID string, authority models.AgentAuthority, timeout time.Duration, opts AwaitVerdictOptions) (*AwaitVerdictResult, error) {
	return awaitVerdictWithOptions(ctx, projectRoot, taskID, authority.ID, &authority, timeout, opts)
}

// awaitVerdictRun is the entry snapshot one await carries from ownership
// through the waiting loops to its finish.
type awaitVerdictRun struct {
	bb              *db.Blackboard
	projectRoot     string
	taskID          string
	agentID         string
	authority       *models.AgentAuthority
	resolver        *pipeline.Resolver
	rolePair        string
	submissionIndex int
}

// awaitVerdictEntry is what the entry preconditions yield: either the final
// result or error that ends the call before ownership is taken, or the run and
// snapshot the wait proceeds with.
type awaitVerdictEntry struct {
	run    awaitVerdictRun
	task   *models.Task
	config models.Config
	opts   AwaitVerdictOptions
	done   bool
	result *AwaitVerdictResult
	err    error
}

func (e awaitVerdictEntry) end(result *AwaitVerdictResult, err error) awaitVerdictEntry {
	e.done, e.result, e.err = true, result, err
	return e
}

func awaitVerdictWithOptions(ctx context.Context, projectRoot, taskID, agentID string, authority *models.AgentAuthority, timeout time.Duration, opts AwaitVerdictOptions) (*AwaitVerdictResult, error) {
	entry := enterAwaitVerdict(projectRoot, taskID, agentID, authority, opts)
	if entry.done {
		return entry.result, entry.err
	}
	run := entry.run

	// Acquire ownership atomically.
	if err := acquireAwaitOwnership(run.bb, agentID, taskID, authority); err != nil {
		return nil, err
	}

	// Budget gate: release ownership and return immediately rather than
	// blocking for the full budget only to discover we can't iterate.
	if awaitBudgetExhausted(entry.task, entry.config) {
		return run.finish(nil, ErrBudgetExhausted)
	}

	return run.wait(ctx, entry.task.Status, awaitVerdictNow().Add(timeout), entry.opts)
}

// enterAwaitVerdict runs the preconditions of one await against a state
// snapshot: option and agent checks, then the decided-task, fast-reviewer and
// awaitable-status checks. It takes no ownership.
func enterAwaitVerdict(projectRoot, taskID, agentID string, authority *models.AgentAuthority, opts AwaitVerdictOptions) awaitVerdictEntry {
	var entry awaitVerdictEntry
	if taskID == "" {
		return entry.end(nil, &PreconditionError{Reason: "task ID is required"})
	}
	if agentID == "" {
		return entry.end(nil, &PreconditionError{Reason: "agent ID is required"})
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	// Read state and find task.
	state, task, err := readTaskState(bb, taskID)
	if err != nil {
		return entry.end(nil, err)
	}
	opts, err = opts.normalized(state.Config)
	if err != nil {
		return entry.end(nil, err)
	}

	// Verify agent exists in state (early exits below bypass acquireAwaitOwnership
	// which would otherwise catch nonexistent agents).
	if _, ok := state.Agents[agentID]; !ok {
		return entry.end(nil, &errors.NotFoundError{Entity: notFoundEntityAgent, ID: agentID})
	}
	submission, _ := latestSubmissionByAgent(task, agentID)
	entry = awaitVerdictEntry{
		run: awaitVerdictRun{
			bb: bb, projectRoot: projectRoot, taskID: taskID, agentID: agentID, authority: authority,
			rolePair: task.RolePair, submissionIndex: submission.index,
		},
		task: task, config: state.Config, opts: opts,
	}
	return entry.admit()
}

// admit ends the entry when the task needs no wait: the system halted work, the
// task was already decided, the verdict already arrived, or the task is not
// awaitable. Otherwise the entry carries the loaded pipeline resolver on.
func (e awaitVerdictEntry) admit() awaitVerdictEntry {
	run, task := e.run, e.task
	if e.config.Mode.HaltsWork() {
		if err := checkLastSubmitter(task, run.agentID); err != nil {
			return e.end(nil, err)
		}
		return e.end(run.finishPaused(task, e.config.Mode))
	}

	// If the task was already decided (BLOCKED or terminal) before we got here,
	// return immediately so the coder can exit cleanly.
	if task.Status == models.TaskStatusBlocked || task.Status.IsTerminal() {
		return e.end(decidedAwaitVerdict(task, run.agentID))
	}

	// Resolve pipeline statuses for the task's role-pair.
	if task.RolePair == "" {
		return e.end(nil, &PreconditionError{Reason: fmt.Sprintf("task %s has no role_pair set", task.ID)})
	}
	resolver, _, resolverErr := loadResolver(run.projectRoot)
	if resolverErr != nil {
		return e.end(nil, &OperationalError{Message: "failed to load pipeline config", Err: resolverErr})
	}
	e.run.resolver = resolver

	// Fast-reviewer race: if verdict already arrived, handle it directly
	// instead of entering the event loop.
	approved, _ := resolver.ApprovedStatus(task.RolePair)
	rejected, _ := resolver.RejectedStatus(task.RolePair)
	if task.Status == approved || task.Status == rejected {
		if err := checkLastSubmitter(task, run.agentID); err != nil {
			return e.endRecovered(err)
		}
		return e.end(e.run.handle(task))
	}

	// Check task status is in the awaitable set.
	if err := checkAwaitableStatus(task, resolver); err != nil {
		if result, ok := recoveredVerdictFromHistory(task, run.agentID); ok {
			return e.end(result, nil)
		}
		if submitterErr := checkLastSubmitter(task, run.agentID); submitterErr != nil {
			return e.end(nil, submitterErr)
		}
		return e.end(nil, err)
	}

	if err := checkLastSubmitter(task, run.agentID); err != nil {
		return e.endRecovered(err)
	}
	return e
}

// endRecovered ends the entry with the verdict recovered from history, or with
// refusal when history holds none.
func (e awaitVerdictEntry) endRecovered(refusal error) awaitVerdictEntry {
	if result, ok := recoveredVerdictFromHistory(e.task, e.run.agentID); ok {
		return e.end(result, nil)
	}
	return e.end(nil, refusal)
}

// wait blocks on the state watcher, the abort check and the deadline until the
// task leaves the awaitable set, falling back to polling when the watcher is
// unavailable or fails.
func (r awaitVerdictRun) wait(ctx context.Context, taskStatus models.TaskStatus, deadline time.Time, opts AwaitVerdictOptions) (*AwaitVerdictResult, error) {
	watcher, watchErr := newAwaitVerdictWatcher(r.bb)
	if watchErr != nil {
		return r.poll(ctx, deadline, taskStatus, opts.FallbackPollInterval)
	}
	defer watcher.Close()

	deadlineTimer := time.NewTimer(time.Until(deadline))
	defer deadlineTimer.Stop()

	abortTicker := time.NewTicker(opts.AbortPollInterval)
	defer abortTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return r.finish(nil, ctx.Err())

		case <-abortTicker.C:
			if obs := r.observe(taskStatus); obs.final {
				return obs.result, obs.err
			}

		case <-watcher.Events():
			if obs := r.observe(taskStatus); obs.final {
				return obs.result, obs.err
			}

		case watcherErr := <-watcher.Errors():
			log.Printf("Watcher error, falling back to polling: %v", watcherErr)
			watcher.Close()
			return r.poll(ctx, deadline, taskStatus, opts.FallbackPollInterval)

		case <-deadlineTimer.C:
			return finishAwaitVerdictTimeout(r.projectRoot, r.bb, r.taskID, r.agentID, r.authority, taskStatus, r.submissionIndex)
		}
	}
}

func (r awaitVerdictRun) poll(ctx context.Context, deadline time.Time, taskStatus models.TaskStatus, pollInterval time.Duration) (*AwaitVerdictResult, error) {
	return runAwaitVerdictPolling(ctx, r.bb, r.taskID, r.agentID, r.authority, deadline, taskStatus, r.resolver, r.rolePair, r.projectRoot, pollInterval, r.submissionIndex)
}

// awaitObservation is what one look at the cached state yields: a final result
// or error, or the live task that is still awaiting its verdict. A state that
// cannot be read yields neither.
type awaitObservation struct {
	task   *models.Task
	final  bool
	result *AwaitVerdictResult
	err    error
}

func endedAwaitObservation(result *AwaitVerdictResult, err error) awaitObservation {
	return awaitObservation{final: true, result: result, err: err}
}

// observe applies one reading of the cached state to the wait: a stopped system
// aborts it (reporting abortedStatus), a vanished task or a halted system ends
// it, and a verdict is classified.
func (r awaitVerdictRun) observe(abortedStatus models.TaskStatus) awaitObservation {
	state, err := r.bb.ReadCached()
	if err != nil {
		return awaitObservation{}
	}
	if state.Config.Mode == models.SystemModeStopped {
		return endedAwaitObservation(r.finish(&AwaitVerdictResult{Verdict: string(VerdictAborted), TaskStatus: abortedStatus}, nil))
	}
	currentTask := state.FindTask(r.taskID)
	if currentTask == nil {
		return endedAwaitObservation(r.finish(disappearedVerdictResult(), nil))
	}
	if state.Config.Mode.HaltsWork() {
		return endedAwaitObservation(r.finishPaused(currentTask, state.Config.Mode))
	}
	if vc := checkVerdictStatus(currentTask, r.resolver, r.rolePair); vc != nil {
		return endedAwaitObservation(r.handle(currentTask))
	}
	return awaitObservation{task: currentTask}
}

// handle classifies the verdict an observed task carries.
func (r awaitVerdictRun) handle(task *models.Task) (*AwaitVerdictResult, error) {
	return handleVerdictResult(r.bb, task, r.agentID, r.authority, r.projectRoot, r.resolver, r.rolePair, r.submissionIndex)
}

// checkAwaitableStatus verifies the task is in a status where awaiting a
// verdict is valid: submitted, reviewing, or partially-approved.
func checkAwaitableStatus(task *models.Task, resolver interface {
	SubmittedStatus(string) (models.TaskStatus, error)
	ReviewingStatus(string) (models.TaskStatus, error)
	PartiallyApprovedStatus(string) (models.TaskStatus, error)
}) error {
	submitted, err := resolver.SubmittedStatus(task.RolePair)
	if err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q — check pipeline.yaml config", task.RolePair)}
	}
	reviewing, err := resolver.ReviewingStatus(task.RolePair)
	if err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q — check pipeline.yaml config", task.RolePair)}
	}

	if task.Status == submitted || task.Status == reviewing {
		return nil
	}

	// PartiallyApproved may not exist for all role-pairs — that's fine.
	partiallyApproved, paErr := resolver.PartiallyApprovedStatus(task.RolePair)
	if paErr == nil && task.Status == partiallyApproved {
		return nil
	}

	return &PreconditionError{
		Reason: fmt.Sprintf("task %s is not in an awaitable status (current: %s, expected: %s or %s)",
			task.ID, task.Status, submitted, reviewing),
	}
}

// checkLastSubmitter verifies the agent was the last to submit this task for review.
func checkLastSubmitter(task *models.Task, agentID string) error {
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		if entry.Event == models.TaskEventSubmittedForReview {
			if entry.Agent != nil && *entry.Agent == agentID {
				return nil
			}
			return &PreconditionError{
				Reason: fmt.Sprintf("agent %s is not the last submitter of task %s", agentID, task.ID),
			}
		}
	}
	return &PreconditionError{
		Reason: fmt.Sprintf("task %s has no submission history", task.ID),
	}
}

// acquireAwaitOwnership atomically sets the agent's status to WAITING and
// CurrentTask to taskID. This prevents other supervisors from claiming the
// task if it gets rejected while we're waiting.
func acquireAwaitOwnership(bb *db.Blackboard, agentID, taskID string, authority *models.AgentAuthority) error {
	return modifyLifecycleState(bb, authority, func(s *models.State) error {
		agent, ok := s.Agents[agentID]
		if !ok {
			return &errors.NotFoundError{Entity: notFoundEntityAgent, ID: agentID}
		}
		agent.Status = models.AgentStatusWaiting
		agent.CurrentTask = &taskID
		s.Agents[agentID] = agent
		return nil
	})
}

// checkVerdictStatus determines if the task has left the awaitable set
// (submitted/reviewing/partially-approved). Returns nil if still waiting;
// returns the observed status if a verdict has arrived.
func checkVerdictStatus(task *models.Task, resolver *pipeline.Resolver, rolePair string) *verdictCheck {
	submitted, _ := resolver.SubmittedStatus(rolePair)
	reviewing, _ := resolver.ReviewingStatus(rolePair)

	if task.Status == submitted || task.Status == reviewing {
		return nil
	}

	// PartiallyApproved keeps waiting — quorum not yet met.
	partiallyApproved, paErr := resolver.PartiallyApprovedStatus(rolePair)
	if paErr == nil && task.Status == partiallyApproved {
		return nil
	}

	return &verdictCheck{status: task.Status}
}

// handleVerdictResult maps the final task status to an AwaitVerdictResult.
// For rejections within budget, it attempts auto-reclaim via ClaimTask.
func handleVerdictResult(bb *db.Blackboard, task *models.Task, agentID string, authority *models.AgentAuthority, projectRoot string, resolver *pipeline.Resolver, rolePair string, submissionIndex int) (*AwaitVerdictResult, error) {
	run := awaitVerdictRun{
		bb: bb, projectRoot: projectRoot, taskID: task.ID, agentID: agentID, authority: authority,
		resolver: resolver, rolePair: rolePair, submissionIndex: submissionIndex,
	}
	approved, _ := resolver.ApprovedStatus(rolePair)
	rejected, _ := resolver.RejectedStatus(rolePair)

	switch task.Status {
	case approved:
		return run.finish(approvedAwaitResult(task), nil)
	case rejected:
		return run.reclaimRejected(task, rejected)
	default:
		return run.finishNonAwaitable(task)
	}
}

// awaitVerdictPolling is the polling fallback for when fsnotify is unavailable.
// It checks state at the resolved interval until a verdict or the deadline.
func awaitVerdictPolling(ctx context.Context, bb *db.Blackboard, taskID, agentID string, authority *models.AgentAuthority, deadline time.Time, taskStatus models.TaskStatus, resolver *pipeline.Resolver, rolePair, projectRoot string, pollInterval time.Duration, submissionIndex int) (*AwaitVerdictResult, error) {
	run := awaitVerdictRun{
		bb: bb, projectRoot: projectRoot, taskID: taskID, agentID: agentID, authority: authority,
		resolver: resolver, rolePair: rolePair, submissionIndex: submissionIndex,
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	deadlineTimer := time.NewTimer(time.Until(deadline))
	defer deadlineTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return run.finish(nil, ctx.Err())

		case <-ticker.C:
			obs := run.observe("")
			if obs.final {
				return obs.result, obs.err
			}
			if obs.task != nil {
				taskStatus = obs.task.Status
			}

		case <-deadlineTimer.C:
			return finishAwaitVerdictTimeout(projectRoot, bb, taskID, agentID, authority, taskStatus, submissionIndex)
		}
	}
}
