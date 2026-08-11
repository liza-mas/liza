package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/precommit"
)

// SupervisorConfig contains all configuration for the agent supervisor
type SupervisorConfig struct {
	AgentID          string
	Role             string // runtime role name from pipeline YAML (e.g. "coder", "code-reviewer", "orchestrator")
	ProjectRoot      string
	StatePath        string
	LogPath          string
	SpecsDir         string // For prompt building
	CLIName          string // "claude", "codex", "gemini", "mistral", "kimi"
	ProfileName      string // Optional structured launch profile name
	ProfileVars      map[string]string
	Interactive      bool          // Print prompt location, don't execute
	InitialTask      string        // Optional task ID to resume
	LLMAgent         LLMAgent      // Agent execution backend (CLI-based or future ACP-based)
	Executor         CLIExecutor   // Legacy field name, preserved for backward compatibility. Deprecated: Use LLMAgent.
	ExecutionTimeout time.Duration // Max time for agent execution before timeout
	// ExecutionProgressTimeout is the maximum time an executing task may go
	// without observable state, worktree, or provider-output progress.
	ExecutionProgressTimeout time.Duration
}

var waitWhilePausedForSupervisor = waitWhilePaused

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

func (t *exit42RestartTracker) Handle(bb *db.Blackboard, projectRoot, role, taskID, agentID string) (exit42RestartOutcome, error) {
	state, err := bb.Read()
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
	if err := bb.Modify(func(s *models.State) error {
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

	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Sprintf("%s|%t|%d", task.Status, task.HandoffPending, len(task.Output))
	}
	return string(payload)
}

// orchestratorProgressSignature returns a string capturing the state dimensions
// the orchestrator is expected to change. Includes sprint metadata, task-status
// distribution, and discovery count so that legitimate progress like resolving
// blocked tasks, superseding exhausted tasks, or triaging discoveries is
// recognized as a signature change and resets the spinning counter.
func orchestratorProgressSignature(state *models.State) string {
	// Task-status distribution: count tasks per status so any status
	// transition (block→ready, ready→superseded, etc.) changes the signature.
	statusCounts := make(map[models.TaskStatus]int)
	for i := range state.Tasks {
		statusCounts[state.Tasks[i].Status]++
	}
	// Sort keys for deterministic output.
	keys := make([]string, 0, len(statusCounts))
	for k := range statusCounts {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	var dist strings.Builder
	for _, k := range keys {
		if dist.Len() > 0 {
			dist.WriteByte(',')
		}
		fmt.Fprintf(&dist, "%s=%d", k, statusCounts[models.TaskStatus(k)])
	}

	// Unconverted immediate discovery count — changes when orchestrator triages.
	immediateDisc := 0
	for _, d := range state.Discovered {
		if d.Urgency == "immediate" && d.ConvertedToTask == nil {
			immediateDisc++
		}
	}

	return fmt.Sprintf("sprint:%s:%d:planned:%d:dist:%s:disc:%d",
		state.Sprint.Status, state.Sprint.Number,
		len(state.Sprint.Scope.Planned), dist.String(), immediateDisc)
}

// isReviewingStatus checks if a task is in a reviewing state.
func isReviewingStatus(task *models.Task, pr models.PipelineResolver) bool {
	if task.RolePair == "" || pr == nil {
		return false
	}
	reviewing, err := pr.ReviewingStatus(task.RolePair)
	return err == nil && task.Status == reviewing
}

// blockTaskFromSupervisor marks a task as BLOCKED and releases the assignment.
// Best-effort: logs warnings on failure but does not return errors.
func blockTaskFromSupervisor(bb *db.Blackboard, projectRoot, taskID, agentID, reason string) {
	pipelineTransitions, ptErr := ops.LoadPipelineTransitions(projectRoot)
	if ptErr != nil {
		GetLogger().Warn("Failed to load pipeline transitions for blocking", "error", ptErr)
		return
	}

	if err := bb.Modify(func(s *models.State) error {
		task := s.FindTask(taskID)
		if task == nil {
			return nil
		}
		if err := task.TransitionWith(models.TaskStatusBlocked, pipelineTransitions); err != nil {
			return err
		}
		now := time.Now().UTC()
		task.BlockedReason = &reason
		task.BlockedQuestions = []string{
			"Is the task spec unclear or incomplete?",
			"Is there an environment or tooling issue preventing progress?",
		}
		task.AssignedTo = nil
		task.LeaseExpires = nil
		task.ReviewingBy = nil
		task.ReviewLeaseExpires = nil
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventBlocked,
			Agent:  &agentID,
			Reason: &reason,
		})
		return nil
	}); err != nil {
		GetLogger().Warn("Failed to block task from supervisor", "error", err, "task_id", taskID)
		return
	}

	result, err := ops.DeleteWorktree(projectRoot, taskID)
	if err != nil {
		GetLogger().Warn("Failed to clean blocked task worktree", "error", err, "task_id", taskID)
		return
	}
	for _, warning := range result.Warnings {
		GetLogger().Warn("Blocked task worktree cleanup warning", "warning", warning, "task_id", taskID)
	}
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

const unknownLizaJSONCommand = "liza-json-envelope"

type observedRuntimeFailure struct {
	Command string
	Code    string
}

func (f observedRuntimeFailure) key() string {
	return f.Command + "\x00" + f.Code
}

type runtimeFailureState struct {
	Count int
	Key   string
}

type runtimeFailureTracker struct {
	byTask map[string]runtimeFailureState
}

func newRuntimeFailureTracker() *runtimeFailureTracker {
	return &runtimeFailureTracker{byTask: make(map[string]runtimeFailureState)}
}

func (t *runtimeFailureTracker) Track(taskID string, failure observedRuntimeFailure) int {
	prev := t.byTask[taskID]
	key := failure.key()
	if prev.Key != "" && prev.Key != key {
		prev.Count = 0
	}
	prev.Count++
	prev.Key = key
	t.byTask[taskID] = prev
	return prev.Count
}

func (t *runtimeFailureTracker) reset(taskID string) {
	delete(t.byTask, taskID)
}

func detectObservedRuntimeFailure(output string) *observedRuntimeFailure {
	if strings.Contains(output, "submit_verdict_failed") {
		return &observedRuntimeFailure{Command: "submit-verdict", Code: "submit_verdict_failed"}
	}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		if failure := detectFailedCommandEvent(line); failure != nil {
			return failure
		}
		env, ok := parseFailedJSONEnvelope(line)
		if !ok {
			continue
		}
		return &observedRuntimeFailure{
			Command: detectLizaCommandContext(output),
			Code:    env.Error.Code,
		}
	}
	return nil
}

type runtimeFailureEnvelope struct {
	OK    *bool `json:"ok"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func parseFailedJSONEnvelope(line string) (runtimeFailureEnvelope, bool) {
	var env runtimeFailureEnvelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		return runtimeFailureEnvelope{}, false
	}
	if env.OK == nil || *env.OK || env.Error == nil || env.Error.Code == "" {
		return runtimeFailureEnvelope{}, false
	}
	return env, true
}

type commandEventEnvelope struct {
	Item *struct {
		Command          string `json:"command"`
		AggregatedOutput string `json:"aggregated_output"`
		ExitCode         *int   `json:"exit_code"`
	} `json:"item"`
}

func detectFailedCommandEvent(line string) *observedRuntimeFailure {
	var event commandEventEnvelope
	if err := json.Unmarshal([]byte(line), &event); err != nil || event.Item == nil {
		return nil
	}

	command := detectLizaCommandContext(event.Item.Command)
	output := event.Item.AggregatedOutput
	if strings.Contains(output, "submit_verdict_failed") {
		return &observedRuntimeFailure{Command: "submit-verdict", Code: "submit_verdict_failed"}
	}
	for _, outputLine := range strings.Split(output, "\n") {
		outputLine = strings.TrimSpace(outputLine)
		if !strings.HasPrefix(outputLine, "{") {
			continue
		}
		env, ok := parseFailedJSONEnvelope(outputLine)
		if !ok {
			continue
		}
		return &observedRuntimeFailure{Command: command, Code: env.Error.Code}
	}
	if event.Item.ExitCode != nil && *event.Item.ExitCode != 0 && command != unknownLizaJSONCommand {
		return &observedRuntimeFailure{Command: command, Code: fmt.Sprintf("exit_%d", *event.Item.ExitCode)}
	}
	return nil
}

func detectLizaCommandContext(output string) string {
	for _, command := range []string{
		"submit-verdict",
		"submit-review",
		"await-verdict",
		"await-resubmission",
		"mark-blocked",
		"set-task-output",
		"handoff",
	} {
		if strings.Contains(output, command) {
			return command
		}
	}
	return unknownLizaJSONCommand
}

func successfulTurnProgressSignature(_, _, taskSnapshot string) string {
	return taskSnapshot
}

func handleObservedRuntimeFailureRetry(
	bb *db.Blackboard,
	config SupervisorConfig,
	taskID string,
	runtimeConfig models.Config,
	failure observedRuntimeFailure,
	runtimeTracker *runtimeFailureTracker,
	spinTracker *spinningTracker,
) bool {
	if taskID == "" {
		return false
	}

	spinTracker.reset(taskID)

	count := runtimeTracker.Track(taskID, failure)
	threshold := effectiveSpinningRestartThreshold(runtimeConfig)
	if count <= threshold {
		GetLogger().Warn("Observed structured CLI/runtime failure; suppressing no-progress spin count",
			"task_id", taskID,
			"agent_id", config.AgentID,
			"command", failure.Command,
			"code", failure.Code,
			"count", count,
			"threshold", threshold)
		return false
	}

	reason := fmt.Sprintf("tool failure retry loop detected: %d consecutive observed CLI/runtime failures for task %s without progress (threshold=%d, command=%s, code=%s)",
		count, taskID, threshold, failure.Command, failure.Code)
	GetLogger().Error("Tool failure retry loop detected, blocking task",
		"task_id", taskID,
		"agent_id", config.AgentID,
		"command", failure.Command,
		"code", failure.Code,
		"count", count)
	if alertErr := LogAlert(config.ProjectRoot, "🚨", "TOOL FAILURE RETRY LOOP", reason); alertErr != nil {
		GetLogger().Warn("Failed to write tool failure retry alert", "error", alertErr)
	}
	blockTaskFromSupervisor(bb, config.ProjectRoot, taskID, config.AgentID, reason)
	runtimeTracker.reset(taskID)
	spinTracker.reset(taskID)
	return true
}

// --- Effective config helpers ---

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

func effectiveAgentProgressTimeout(cfg models.Config) time.Duration {
	seconds := cfg.AgentProgressTimeout
	if seconds <= 0 {
		seconds = models.DefaultAgentProgressTimeoutSec
	}
	return time.Duration(seconds) * time.Second
}

// RunSupervisor is the main entry point for the agent supervisor
func RunSupervisor(ctx context.Context, config SupervisorConfig) error {
	bb := db.For(config.StatePath)
	lizaPaths := paths.New(config.ProjectRoot)
	supervisorCtx, cancelSupervisor := context.WithCancel(ctx)
	defer cancelSupervisor()

	// Validate identity
	if err := validateIdentity(config.AgentID, config.Role); err != nil {
		return err
	}

	// Load pipeline resolver for role type classification.
	// The resolver reads role definitions from the pipeline YAML,
	// enabling custom YAML-defined roles without Go code changes.
	pipelineCfg, pipelineErr := pipeline.LoadFrozen(config.ProjectRoot)
	if pipelineErr != nil {
		return fmt.Errorf("loading pipeline config for strategy selection: %w", pipelineErr)
	}
	resolver := pipeline.NewResolver(pipelineCfg)

	roleType, err := resolver.RoleType(config.Role)
	if err != nil {
		return fmt.Errorf("resolving role type for %q: %w", config.Role, err)
	}

	strategy, err := NewRoleStrategy(config.Role, resolver)
	if err != nil {
		return err
	}

	// Apply YAML-sourced timeouts from pipeline config to the strategy.
	if timeouts, tErr := resolver.RoleTimeouts(config.Role); tErr == nil {
		ApplyYAMLTimeouts(strategy, timeouts.Execution, timeouts.PollInterval, timeouts.MaxWait)
	}

	if err := registerAgent(bb, config.ProjectRoot, config.AgentID, config.Role, "terminal-1", 1800, config.CLIName, resolver); err != nil {
		return err
	}
	defer unregisterAgent(bb, config.AgentID, config.ProjectRoot)

	state, err := bb.Read()
	if err != nil {
		return fmt.Errorf("failed to read state: %w", err)
	}

	// Start supervisor-lifetime heartbeat to keep the lease alive across
	// the entire loop (including IDLE wait-for-work periods, not just
	// during CLI execution). Without this, an IDLE agent's lease can
	// expire, causing auto-assigned ID collision with new agents.
	heartbeatCtx, cancelHeartbeat := context.WithCancel(supervisorCtx)
	defer cancelHeartbeat()
	heartbeatErrCh := make(chan error, 1)

	hb := NewHeartbeat(HeartbeatConfig{
		AgentID:   config.AgentID,
		StatePath: config.StatePath,
		State:     state,
	})

	go func() {
		if err := hb.Start(heartbeatCtx); err != nil && err != context.Canceled {
			GetLogger().Error("Heartbeat error", "error", err, "agent_id", config.AgentID)
			select {
			case heartbeatErrCh <- err:
			default:
			}
			cancelSupervisor()
		}
	}()
	checkHeartbeat := func() error {
		select {
		case err := <-heartbeatErrCh:
			return fmt.Errorf("heartbeat stopped for agent %s: %w", config.AgentID, err)
		default:
			return nil
		}
	}
	waitForSupervisorDelay := func(delay time.Duration) error {
		timer := time.NewTimer(delay)
		defer timer.Stop()

		select {
		case <-supervisorCtx.Done():
			if hbErr := checkHeartbeat(); hbErr != nil {
				return hbErr
			}
			if ctx.Err() != nil {
				return nil
			}
			return supervisorCtx.Err()
		case <-timer.C:
			return nil
		}
	}

	pollInterval, maxWait := strategy.WaitConfig(state)

	// Set execution timeout if not configured
	if config.ExecutionTimeout == 0 {
		config.ExecutionTimeout = strategy.DefaultTimeout()
	}
	if config.ExecutionProgressTimeout == 0 {
		config.ExecutionProgressTimeout = effectiveAgentProgressTimeout(state.Config)
	}

	exit42Tracker := newExit42RestartTracker()
	crashTracker := newCrashRestartTracker()
	spinTracker := newSpinningTracker()
	runtimeFailureTracker := newRuntimeFailureTracker()

	for {
		if err := checkHeartbeat(); err != nil {
			return err
		}
		// Check context cancellation (signal received)
		if supervisorCtx.Err() != nil {
			if hbErr := checkHeartbeat(); hbErr != nil {
				return hbErr
			}
			if ctx.Err() == nil {
				return supervisorCtx.Err()
			}
			GetLogger().Info("Signal received, shutting down")
			return nil
		}

		// Check ABORT
		if checkAbort(config.ProjectRoot) {
			GetLogger().Info("ABORT signal received, system shutting down")
			return nil
		}

		if handleProviderUnavailableSignal(config) {
			return nil
		}

		if handleQuotaSignal(config) {
			return nil
		}

		// Wait while PAUSE/CHECKPOINT
		if err := waitWhilePausedForSupervisor(supervisorCtx, config.ProjectRoot, roleType); err != nil {
			if hbErr := checkHeartbeat(); hbErr != nil {
				return hbErr
			}
			if errors.Is(err, errGoalComplete) {
				GetLogger().Info("Goal complete, supervisor exiting")
				return nil
			}
			return err
		}

		// Pre-work (reviewer: merge handling; others: no-op)
		shouldContinue, err := strategy.PreWork(supervisorCtx, bb, config)
		if err != nil {
			if hbErr := checkHeartbeat(); hbErr != nil {
				return hbErr
			}
			return err
		}
		if shouldContinue {
			continue
		}

		// Wait for work
		hasWork, err := strategy.WaitForWork(supervisorCtx, bb, config, pollInterval, maxWait)
		if err != nil {
			if hbErr := checkHeartbeat(); hbErr != nil {
				return hbErr
			}
			return err
		}
		if !hasWork {
			GetLogger().Info("No work available, supervisor exiting")
			return nil
		}

		// Claim task
		taskID, claimedTaskID, err := strategy.ClaimTask(config, bb)
		if err != nil {
			if errors.Is(err, ErrAgentDegraded) {
				GetLogger().Error("Agent marked degraded, exiting supervisor", "agent_id", config.AgentID, "role", config.Role, "error", err)
				return nil
			}
			if err := waitForSupervisorDelay(5 * time.Second); err != nil {
				return err
			}
			continue
		}

		// Pre-execution (orchestrator: PLANNING status; others: no-op)
		if err := strategy.PreExecution(bb, config); err != nil {
			GetLogger().Warn("Pre-execution failed", "error", err, "agent_id", config.AgentID)
		}

		// Build and save prompt
		stateBefore, err := bb.Read()
		if err != nil {
			return fmt.Errorf("failed to read state for prompt: %w", err)
		}

		// Spinning detection: track re-executions for the same task.
		effectiveTask := claimedTaskID
		if effectiveTask == "" {
			effectiveTask = taskID
		}
		if effectiveTask != "" {
			// Prefer the worktree-aware snapshot: genuine iteration on a task
			// often progresses only in the worktree, and task-field signatures
			// alone would count it as spinning (DEV-667).
			sig, snapshotEligible, err := readSuccessfulTurnProgressSnapshot(config.ProjectRoot, bb, effectiveTask, config.AgentID, resolver)
			if err != nil {
				GetLogger().Warn("Successful no-progress snapshot failed", "error", err, "task_id", effectiveTask)
			}
			if !snapshotEligible || sig == "" {
				if task := stateBefore.FindTask(effectiveTask); task != nil {
					sig = exit42TaskProgressSignature(task)
				}
			}
			spinCount := spinTracker.Track(effectiveTask, sig)
			spinThreshold := effectiveSpinningRestartThreshold(stateBefore.Config)
			if spinCount > spinThreshold {
				reason := fmt.Sprintf("spinning detected: %d consecutive executions for task %s without progress (threshold=%d)",
					spinCount, effectiveTask, spinThreshold)
				GetLogger().Error("Spinning detected, blocking task",
					"task_id", effectiveTask,
					"agent_id", config.AgentID,
					"count", spinCount)
				if alertErr := LogAlert(config.ProjectRoot, "🚨", "SPINNING", reason); alertErr != nil {
					GetLogger().Warn("Failed to write spinning alert", "error", alertErr)
				}
				blockTaskFromSupervisor(bb, config.ProjectRoot, effectiveTask, config.AgentID, reason)
				spinTracker.reset(effectiveTask)
				continue
			}
		} else {
			// Orchestrator spinning detection: no task ID, so track by state
			// signature. If the orchestrator keeps executing without changing
			// sprint status, sprint number, or task count, it is spinning.
			sig := orchestratorProgressSignature(stateBefore)
			spinCount := spinTracker.Track("orchestrator", sig)
			spinThreshold := effectiveSpinningRestartThreshold(stateBefore.Config)
			if spinCount > spinThreshold {
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
				return nil
			}
		}

		if reviewer, ok := strategy.(*reviewerStrategy); ok {
			if err := ensureReviewerPromptClaimed(stateBefore, config.AgentID, taskID, reviewer.resolver); err != nil {
				return fmt.Errorf("stale reviewer claim before prompt launch: %w", err)
			}
		}

		prompt, err := strategy.BuildPrompt(stateBefore, config, taskID)
		if err != nil {
			if claimedTaskID != "" && errors.Is(err, precommit.ErrContextBuild) {
				reason := fmt.Sprintf("prompt context build failed: %v", err)
				GetLogger().Warn("Task blocked due to prompt-context build failure",
					"agent_id", config.AgentID,
					"task_id", claimedTaskID,
					"error", err)
				blockTaskFromSupervisor(bb, config.ProjectRoot, claimedTaskID, config.AgentID, reason)
				spinTracker.reset(effectiveTask)
				continue
			}
			return fmt.Errorf("failed to build prompt: %w", err)
		}

		promptFile, err := savePrompt(lizaPaths.AgentPromptsDir(), config.AgentID, prompt)
		if err != nil {
			return fmt.Errorf("failed to save prompt: %w", err)
		}
		GetLogger().Info("Prompt saved", "file", promptFile)

		// Execute agent
		exitCode, currentOutput, err := executeAgent(supervisorCtx, config, prompt, nil, effectiveTask, stateBefore.Config)
		if err != nil {
			if hbErr := checkHeartbeat(); hbErr != nil {
				return hbErr
			}
			return fmt.Errorf("agent execution error: %w", err)
		}

		if exitCode == 0 && effectiveTask != "" {
			if failure := detectObservedRuntimeFailure(currentOutput); failure != nil {
				handleObservedRuntimeFailureRetry(bb, config, effectiveTask, stateBefore.Config, *failure, runtimeFailureTracker, spinTracker)
				if err := resetAgentAfterExit(bb, config.AgentID, config.ProjectRoot); err != nil {
					GetLogger().Warn("Failed to reset agent status after runtime failure", "error", err, "agent_id", config.AgentID)
				}
				exit42Tracker.reset(taskID)
				crashTracker.reset(effectiveTask)
				continue
			}
		}

		// Reset runtime status after CLI exits, but preserve explicit command-driven
		// states such as WAITING and HANDOFF.
		if err := resetAgentAfterExit(bb, config.AgentID, config.ProjectRoot); err != nil {
			GetLogger().Warn("Failed to reset agent status after exit", "error", err, "agent_id", config.AgentID)
		}

		handleProviderAuditDegraded(bb, config, effectiveTask, currentOutput)

		// Handle exit code
		switch exitCode {
		case 0:
			if effectiveTask != "" {
				runtimeFailureTracker.reset(effectiveTask)
			}

			GetLogger().Info("Agent completed, checking for more work")
			if err := strategy.PostExecution(bb, config, taskID, claimedTaskID, stateBefore); err != nil {
				GetLogger().Warn("Post-execution error", "error", err)
			}
			exit42Tracker.reset(taskID)
			crashTracker.reset(effectiveTask)
		case 42:
			restartTaskID := claimedTaskID
			if restartTaskID == "" {
				restartTaskID = taskID
			}
			runtimeFailureTracker.reset(restartTaskID)

			outcome, trackErr := exit42Tracker.Handle(bb, config.ProjectRoot, config.Role, restartTaskID, config.AgentID)
			if trackErr != nil {
				GetLogger().Warn("Exit-42 tracker failed, using default retry delay",
					"error", trackErr,
					"task_id", restartTaskID)
				if err := waitForSupervisorDelay(2 * time.Second); err != nil {
					return err
				}
				break
			}

			if outcome.BlockedTask {
				GetLogger().Warn("Task transitioned to BLOCKED after repeated exit 42 restarts",
					"task_id", restartTaskID,
					"restart_count", outcome.RestartCount)
				break
			}

			GetLogger().Info("Agent aborted gracefully, restarting",
				"exit_code", 42,
				"task_id", restartTaskID,
				"restart_count", outcome.RestartCount,
				"delay_seconds", int(outcome.Delay/time.Second))
			if err := waitForSupervisorDelay(outcome.Delay); err != nil {
				return err
			}
		default:
			exit42Tracker.reset(taskID)
			runtimeFailureTracker.reset(effectiveTask)

			// Check if crash was caused by a provider-scoped failure.
			if handleClassifiedProviderCrash(config, currentOutput) {
				return nil
			}

			// Track crash restarts per task.
			if effectiveTask != "" {
				var sig string
				if s, rErr := bb.Read(); rErr == nil {
					if task := s.FindTask(effectiveTask); task != nil {
						sig = exit42TaskProgressSignature(task)
					}
				}
				crashCount := crashTracker.Increment(effectiveTask, sig)
				crashThreshold := effectiveCrashRestartThreshold(stateBefore.Config)
				if crashCount > crashThreshold {
					reason := fmt.Sprintf("crash restart loop detected: %d consecutive crashes for task %s without progress (threshold=%d, last exit code=%d)",
						crashCount, effectiveTask, crashThreshold, exitCode)
					GetLogger().Error("Crash restart loop detected, blocking task",
						"task_id", effectiveTask,
						"agent_id", config.AgentID,
						"crash_count", crashCount,
						"exit_code", exitCode)
					if alertErr := LogAlert(config.ProjectRoot, "🚨", "CRASH RESTART LOOP", reason); alertErr != nil {
						GetLogger().Warn("Failed to write crash alert", "error", alertErr)
					}
					blockTaskFromSupervisor(bb, config.ProjectRoot, effectiveTask, config.AgentID, reason)
					crashTracker.reset(effectiveTask)
					continue
				}
			}

			GetLogger().Error("Agent crashed, restarting", "exit_code", exitCode, "delay_seconds", 5)
			if err := waitForSupervisorDelay(5 * time.Second); err != nil {
				return err
			}
		}

		// Clear initial task after first run
		config.InitialTask = ""
	}
}

func handleQuotaSignal(config SupervisorConfig) bool {
	if !CheckQuotaSignal(config.ProjectRoot, config.CLIName) {
		return false
	}

	GetLogger().Info("Provider quota exhausted, shutting down",
		"provider", config.CLIName,
		"agent_id", config.AgentID)
	return true
}

func handleProviderUnavailableSignal(config SupervisorConfig) bool {
	if !CheckProviderUnavailableSignal(config.ProjectRoot, config.CLIName) {
		return false
	}

	GetLogger().Info("Provider unavailable, shutting down",
		"provider", config.CLIName,
		"agent_id", config.AgentID)
	return true
}

func handleClassifiedProviderCrash(config SupervisorConfig, output string) bool {
	if pu := DetectProviderUnavailable(output, config.CLIName); pu != nil {
		GetLogger().Error("Provider unavailable, terminating",
			"provider", pu.Provider,
			"agent_id", config.AgentID,
			"message", pu.Message)
		if alertErr := LogProviderUnavailableAlert(config.ProjectRoot, pu); alertErr != nil {
			GetLogger().Warn("Failed to write provider unavailable alert", "error", alertErr)
		}
		if err := WriteProviderUnavailableSignal(config.ProjectRoot, pu.Provider, pu.Message); err != nil {
			GetLogger().Warn("Failed to write provider unavailable signal", "error", err)
		}
		return true
	}

	if qe := DetectQuotaExhaustion(output, config.CLIName); qe != nil {
		GetLogger().Error("Provider quota exhausted, terminating",
			"provider", qe.Provider,
			"agent_id", config.AgentID,
			"message", qe.Message)
		if err := RaiseQuotaExhaustion(config.ProjectRoot, qe); err != nil {
			GetLogger().Warn("Failed to raise quota exhaustion", "error", err)
		}
		return true
	}

	return false
}
