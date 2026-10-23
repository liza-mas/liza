package agent

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

type exit42RestartState struct {
	RestartCount int
	Signature    string
}

type exit42RestartOutcome struct {
	Delay        time.Duration
	RestartCount int
	BlockedTask  bool
}

type exit42RestartTracker struct {
	byKey map[string]exit42RestartState
}

func newExit42RestartTracker() *exit42RestartTracker {
	return &exit42RestartTracker{
		byKey: make(map[string]exit42RestartState),
	}
}

func (t *exit42RestartTracker) reset(taskID string) {
	if taskID == "" {
		return
	}
	delete(t.byKey, "task:"+taskID)
}

func (t *exit42RestartTracker) Handle(bb *db.Blackboard, projectRoot, role, taskID string, authority models.AgentAuthority) (exit42RestartOutcome, error) {
	agentID := authority.ID
	state, err := bb.ReadSnapshot()
	if err != nil {
		return exit42RestartOutcome{}, fmt.Errorf("read state for exit-42 tracking: %w", err)
	}

	maxBackoff := effectiveExit42MaxBackoff(state.Config)
	restartLimit := effectiveExit42RestartLimit(state.Config)
	key := exit42TrackerKey(taskID, role, agentID)
	prev := t.byKey[key]

	var signature string
	if taskID != "" {
		task := state.FindTask(taskID)
		if task != nil {
			signature = exit42TaskProgressSignature(task)
		}
	}

	if prev.Signature != "" && signature != "" && prev.Signature != signature {
		prev.RestartCount = 0
	}

	prev.RestartCount++
	prev.Signature = signature

	outcome := exit42RestartOutcome{
		Delay:        computeExit42BackoffDelay(prev.RestartCount, maxBackoff),
		RestartCount: prev.RestartCount,
	}

	blockedTask := false
	if err := ops.ModifyWithAgentAuthority(bb, authority, func(s *models.State) error {
		if taskID == "" {
			return nil
		}

		task := s.FindTask(taskID)
		if task == nil {
			return nil
		}

		task.Exit42RestartCount = outcome.RestartCount

		if outcome.RestartCount <= restartLimit {
			return nil
		}
		// Check if task is in an active state owned by this agent.
		pr, prErr := ops.LoadResolverForModels(projectRoot)
		if prErr != nil {
			return prErr
		}
		isActive := models.IsExecutingStatus(task, pr) || isReviewingStatus(task, pr)
		if !isActive {
			return nil
		}
		ownedByAgent := (task.AssignedTo != nil && *task.AssignedTo == agentID) ||
			(task.ReviewingBy != nil && *task.ReviewingBy == agentID)
		if !ownedByAgent {
			return nil
		}

		reason := fmt.Sprintf(
			"exit code 42 restart loop detected: %d consecutive restarts without progress (threshold=%d)",
			outcome.RestartCount,
			restartLimit,
		)
		questions := []string{
			"What task/environment issue is causing repeated exit code 42 without progress?",
			"Should this task be decomposed or the spec clarified before retrying?",
		}

		pipelineTransitions, ptErr := ops.LoadPipelineTransitions(projectRoot)
		if ptErr != nil {
			return ptErr
		}
		if err := task.TransitionWith(models.TaskStatusBlocked, pipelineTransitions); err != nil {
			return err
		}

		now := time.Now().UTC()
		task.BlockedReason = &reason
		task.BlockedQuestions = questions
		task.AssignedTo = nil
		task.LeaseExpires = nil
		models.AdvanceLifecycle(task)
		task.ReviewingBy = nil
		task.ReviewLeaseExpires = nil
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventBlocked,
			Agent:  &agentID,
			Reason: &reason,
		})
		blockedTask = true
		return nil
	}); err != nil {
		return exit42RestartOutcome{}, fmt.Errorf("persist exit-42 tracking state: %w", err)
	}

	outcome.BlockedTask = blockedTask
	if blockedTask {
		outcome.Delay = 0
		delete(t.byKey, key)
		return outcome, nil
	}

	t.byKey[key] = prev
	return outcome, nil
}

func exit42TrackerKey(taskID, role, agentID string) string {
	if taskID != "" {
		return "task:" + taskID
	}
	return "agent:" + role + ":" + agentID
}

func effectiveExit42MaxBackoff(cfg models.Config) time.Duration {
	seconds := cfg.Exit42MaxBackoffSeconds
	if seconds <= 0 {
		seconds = models.DefaultExit42MaxBackoffSec
	}
	return time.Duration(seconds) * time.Second
}

func effectiveExit42RestartLimit(cfg models.Config) int {
	limit := cfg.Exit42RestartThreshold
	if limit <= 0 {
		limit = models.DefaultExit42RestartLimit
	}
	return limit
}

func computeExit42BackoffDelay(restartCount int, maxBackoff time.Duration) time.Duration {
	if restartCount <= 0 {
		restartCount = 1
	}
	if maxBackoff <= 0 {
		maxBackoff = time.Duration(models.DefaultExit42MaxBackoffSec) * time.Second
	}

	delay := 2 * time.Second
	if delay > maxBackoff {
		return maxBackoff
	}

	for i := 1; i < restartCount; i++ {
		if delay >= maxBackoff {
			return maxBackoff
		}
		if delay > maxBackoff/2 {
			return maxBackoff
		}
		delay *= 2
	}

	if delay > maxBackoff {
		return maxBackoff
	}
	return delay
}

func exit42TaskProgressSignature(task *models.Task) string {
	snapshot := *task
	snapshot.AssignedTo = nil
	snapshot.LeaseExpires = nil
	snapshot.ReviewingBy = nil
	snapshot.ReviewLeaseExpires = nil
	// Iteration is bumped by every ClaimTask, including the no-progress
	// re-claims this signature exists to detect; keeping it would reset the
	// spin/crash counters on each cycle (DEV-667).
	snapshot.Iteration = 0
	snapshot.Exit42RestartCount = 0
	snapshot.History = nil
	// Claim/release receipts describe ownership bookkeeping, not task progress.
	snapshot.Lifecycle = nil

	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Sprintf("%s|%t", task.Status, task.HandoffPending)
	}
	return string(payload)
}

func successfulTurnTaskProgressSignature(task *models.Task) string {
	snapshot := *task
	snapshot.AssignedTo = nil
	snapshot.LeaseExpires = nil
	snapshot.ReviewingBy = nil
	snapshot.ReviewLeaseExpires = nil
	snapshot.Iteration = 0
	snapshot.Exit42RestartCount = 0
	snapshot.History = nil
	// Claim/release receipts describe ownership bookkeeping, not task progress.
	snapshot.Lifecycle = nil

	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Sprintf("%s|%t|%d", task.Status, task.HandoffPending, len(task.Output))
	}
	return string(payload)
}

// isReviewingStatus checks if a task is in a reviewing state.
func isReviewingStatus(task *models.Task, pr models.PipelineResolver) bool {
	if task.RolePair == "" || pr == nil {
		return false
	}
	reviewing, err := pr.ReviewingStatus(task.RolePair)
	return err == nil && task.Status == reviewing
}

// --- Crash restart tracker ---

type crashRestartState struct {
	Count     int
	Signature string
}

type crashRestartTracker struct {
	byTask map[string]crashRestartState
}

func newCrashRestartTracker() *crashRestartTracker {
	return &crashRestartTracker{byTask: make(map[string]crashRestartState)}
}

func (t *crashRestartTracker) reset(taskID string) {
	delete(t.byTask, taskID)
}

// Increment records a crash for the task and returns the new count.
// Resets the counter if task state has changed (progress detected).
func (t *crashRestartTracker) Increment(taskID string, currentSignature string) int {
	prev := t.byTask[taskID]
	if prev.Signature != "" && currentSignature != "" && prev.Signature != currentSignature {
		prev.Count = 0
	}
	prev.Count++
	prev.Signature = currentSignature
	t.byTask[taskID] = prev
	return prev.Count
}

// Crash restarts back off per supervisor, not per task: a persistent failure
// the classifiers miss (an unrecognised provider limit) fails every task the
// same way, and each restart pays a full claim cycle.
const (
	crashRestartBaseDelay = 5 * time.Second
	crashRestartMaxDelay  = 5 * time.Minute
)

// crashRestartDelay is the wait before the next claim after the supervisor's
// consecutiveCrashes-th crash in a row.
func crashRestartDelay(consecutiveCrashes int) time.Duration {
	delay := crashRestartBaseDelay
	for i := 1; i < consecutiveCrashes && delay < crashRestartMaxDelay; i++ {
		delay *= 2
	}
	return min(delay, crashRestartMaxDelay)
}

// --- Spinning (same-task re-execution) tracker ---

type spinningState struct {
	Count     int
	Signature string
}

type spinningTracker struct {
	byTask map[string]spinningState
}

func newSpinningTracker() *spinningTracker {
	return &spinningTracker{byTask: make(map[string]spinningState)}
}

// Track records an execution for the task and returns the new count.
// Resets when task ID changes (caller responsibility) or task state progresses.
func (t *spinningTracker) Track(taskID string, currentSignature string) int {
	prev := t.byTask[taskID]
	if prev.Signature != "" && currentSignature != "" && prev.Signature != currentSignature {
		prev.Count = 0
	}
	prev.Count++
	prev.Signature = currentSignature
	t.byTask[taskID] = prev
	return prev.Count
}

func (t *spinningTracker) reset(taskID string) {
	delete(t.byTask, taskID)
}

// blockSpinningTask records an execution of taskID and blocks the task once it
// has run more than the spinning threshold without progress. It reports
// whether the task was blocked.
func blockSpinningTask(bb *db.Blackboard, config SupervisorConfig, pr models.PipelineResolver, stateBefore *models.State, taskID string, spinTracker *spinningTracker) bool {
	// Prefer the worktree-aware snapshot: genuine iteration on a task
	// often progresses only in the worktree, and task-field signatures
	// alone would count it as spinning (DEV-667).
	sig, snapshotEligible, err := readSuccessfulTurnProgressSnapshot(config.ProjectRoot, bb, taskID, config.AgentID, pr)
	if err != nil {
		GetLogger().Warn("Successful no-progress snapshot failed", "error", err, "task_id", taskID)
	}
	if !snapshotEligible || sig == "" {
		if task := stateBefore.FindTask(taskID); task != nil {
			sig = exit42TaskProgressSignature(task)
		}
	}
	spinCount := spinTracker.Track(taskID, sig)
	spinThreshold := effectiveSpinningRestartThreshold(stateBefore.Config)
	if spinCount <= spinThreshold {
		return false
	}
	reason := fmt.Sprintf("spinning detected: %d consecutive executions for task %s without progress (threshold=%d)",
		spinCount, taskID, spinThreshold)
	GetLogger().Error("Spinning detected, blocking task",
		"task_id", taskID,
		"agent_id", config.AgentID,
		"count", spinCount)
	if alertErr := LogAlert(config.ProjectRoot, "🚨", "SPINNING", reason); alertErr != nil {
		GetLogger().Warn("Failed to write spinning alert", "error", alertErr)
	}
	if blockErr := blockTaskFromSupervisor(bb, config.ProjectRoot, taskID, config.Authority, reason); blockErr != nil {
		GetLogger().Warn("Failed to block task from supervisor", "error", blockErr, "task_id", taskID)
	}
	spinTracker.reset(taskID)
	return true
}

// stopSpinningOrchestrator records an orchestrator execution and stops the
// system once it has run more than the spinning threshold without progress.
// It reports whether the system was stopped. With no task ID, progress is the
// state signature: sprint status, sprint number and task count.
func stopSpinningOrchestrator(config SupervisorConfig, stateBefore *models.State, spinTracker *spinningTracker) bool {
	sig := orchestratorProgressSignature(stateBefore)
	spinCount := spinTracker.Track("orchestrator", sig)
	spinThreshold := effectiveSpinningRestartThreshold(stateBefore.Config)
	if spinCount <= spinThreshold {
		return false
	}
	reason := fmt.Sprintf("orchestrator spinning detected: %d consecutive executions without state progress (threshold=%d)",
		spinCount, spinThreshold)
	GetLogger().Error("Orchestrator spinning detected, stopping system",
		"agent_id", config.AgentID,
		"count", spinCount)
	if alertErr := LogAlert(config.ProjectRoot, "🚨", "ORCHESTRATOR_SPINNING", reason); alertErr != nil {
		GetLogger().Warn("Failed to write spinning alert", "error", alertErr)
	}
	if _, stopErr := ops.Stop(config.ProjectRoot, reason, config.AgentID); stopErr != nil {
		GetLogger().Warn("Failed to stop system after orchestrator spinning", "error", stopErr)
	}
	return true
}

func effectiveCrashRestartThreshold(cfg models.Config) int {
	if cfg.CrashRestartThreshold > 0 {
		return cfg.CrashRestartThreshold
	}
	return models.DefaultCrashRestartThreshold
}

func effectiveSpinningRestartThreshold(cfg models.Config) int {
	if cfg.SpinningRestartThreshold > 0 {
		return cfg.SpinningRestartThreshold
	}
	return models.DefaultSpinningRestartThreshold
}
