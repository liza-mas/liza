package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

// backgroundTaskStoppedStatus is the provider's report for a background job
// killed because its session ended, as opposed to one that ran to an answer.
const backgroundTaskStoppedStatus = "stopped"

// abandonedBackgroundTask identifies a background job the session started and
// never finished.
type abandonedBackgroundTask struct {
	TaskID  string
	Summary string
}

type backgroundTaskEnvelope struct {
	Subtype string `json:"subtype"`
	TaskID  string `json:"task_id"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

// detectAbandonedBackgroundTask reports a background job that was still live
// when the session ended.
//
// The provider kills such jobs at session end and reports them as "stopped",
// so the completion notice the agent said it was waiting for never arrives.
// Treating that session as complete releases a claim whose work is unfinished
// and leaves the worktree dirty, which blocks the task on the next claim. A
// job reported "completed" or "failed" ran to an answer and is not abandoned.
//
// Only the last status observed for a task id counts: a job may be reported
// stopped and then completed within one session.
//
// A job the agent killed on purpose reports the same "stopped" status, so
// those are excluded: deliberately abandoning a job the agent no longer needs
// and then finishing the work is healthy, and refusing to complete such a
// session would block a task whose work is done.
//
// Fails closed: a provider that emits no background-task events yields no
// detection, leaving session completion exactly as it was.
func detectAbandonedBackgroundTask(output string) *abandonedBackgroundTask {
	lastStatus := make(map[string]string)
	summaries := make(map[string]string)
	order := make([]string, 0)
	deliberatelyStopped := deliberatelyStoppedTaskIDs(output)

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") || !strings.Contains(line, "task_notification") {
			continue
		}
		var env backgroundTaskEnvelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}
		if env.Subtype != "task_notification" || env.TaskID == "" || env.Status == "" {
			continue
		}
		if _, seen := lastStatus[env.TaskID]; !seen {
			order = append(order, env.TaskID)
		}
		lastStatus[env.TaskID] = env.Status
		if env.Summary != "" {
			summaries[env.TaskID] = env.Summary
		}
	}

	for _, taskID := range order {
		if lastStatus[taskID] != backgroundTaskStoppedStatus {
			continue
		}
		if _, deliberate := deliberatelyStopped[taskID]; deliberate {
			continue
		}
		return &abandonedBackgroundTask{TaskID: taskID, Summary: summaries[taskID]}
	}
	return nil
}

// deliberateStopPattern matches the provider's acknowledgement of an agent
// request to kill a background job. It is matched against tool-result payloads
// only: the same sentence in agent prose, a quoted log, or a transcript that
// discusses this behaviour must not mask a real abandonment.
var deliberateStopPattern = regexp.MustCompile(`Successfully stopped task: ([A-Za-z0-9_-]+)`)

// deliberatelyStoppedTaskIDs collects the background jobs the agent asked to
// stop, which the provider then reports with the same status as a job killed
// at session end.
func deliberatelyStoppedTaskIDs(output string) map[string]struct{} {
	stopped := make(map[string]struct{})
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") || !strings.Contains(line, "tool_result") {
			continue
		}
		for _, match := range deliberateStopPattern.FindAllStringSubmatch(line, -1) {
			stopped[match[1]] = struct{}{}
		}
	}
	return stopped
}

// handleAbandonedBackgroundTaskRetry preserves the claim of a session that
// ended with a live background job so the next session can finish the work,
// and blocks the task once that has repeated past the spin threshold.
//
// Returns true when the task was blocked, matching
// handleObservedRuntimeFailureRetry.
func handleAbandonedBackgroundTaskRetry(
	bb *db.Blackboard,
	config SupervisorConfig,
	taskID string,
	runtimeConfig models.Config,
	abandoned abandonedBackgroundTask,
	backgroundTracker *runtimeFailureTracker,
	spinTracker *spinningTracker,
) bool {
	if taskID == "" {
		return false
	}

	// A preserved claim is progress of a kind the spin counter cannot see: the
	// session is re-run on the same task deliberately, not because it failed to
	// move. This tracker's own count is the bound.
	spinTracker.reset(taskID)

	count := backgroundTracker.Track(taskID, observedRuntimeFailure{
		Command: "background-task",
		Code:    backgroundTaskStoppedStatus,
	})
	// Deliberately shares the spinning threshold: both bound how many times one
	// task may re-run without completing. Split this out if the two need to be
	// tuned apart.
	threshold := effectiveSpinningRestartThreshold(runtimeConfig)
	if count <= threshold {
		GetLogger().Warn("Session ended with a live background job; preserving claim instead of completing",
			"task_id", taskID,
			"agent_id", config.AgentID,
			"background_task_id", abandoned.TaskID,
			"summary", abandoned.Summary,
			"count", count,
			"threshold", threshold)
		return false
	}

	reason := fmt.Sprintf("abandoned background job loop detected: %d consecutive sessions ended with a live background task for %s without progress (threshold=%d, last job=%s)",
		count, taskID, threshold, abandoned.Summary)
	GetLogger().Error("Abandoned background job loop detected, blocking task",
		"task_id", taskID,
		"agent_id", config.AgentID,
		"background_task_id", abandoned.TaskID,
		"count", count)
	if alertErr := LogAlert(config.ProjectRoot, "🚨", "ABANDONED BACKGROUND JOB LOOP", reason); alertErr != nil {
		GetLogger().Warn("Failed to write abandoned background job alert", "error", alertErr)
	}
	if err := blockTaskFromSupervisor(bb, config.ProjectRoot, taskID, config.Authority, reason); err != nil {
		GetLogger().Warn("Failed to block task from supervisor", "error", err, "task_id", taskID)
	}
	backgroundTracker.reset(taskID)
	spinTracker.reset(taskID)
	return true
}

// preserveClaimForBackgroundJob decides whether a session that exited 0 has
// really finished. A session that ends while one of its own background jobs
// is still live has not: the provider kills the job at session end, so the
// completion the agent said it was waiting for never arrives. Completing it
// would release the claim and leave the worktree dirty, blocking the task on
// the next claim. Returns true when the claim was preserved and the task
// should be re-run rather than completed.
func preserveClaimForBackgroundJob(
	bb *db.Blackboard,
	config SupervisorConfig,
	taskID string,
	runtimeConfig models.Config,
	output string,
	backgroundTracker *runtimeFailureTracker,
	spinTracker *spinningTracker,
) bool {
	if taskID == "" {
		return false
	}
	abandoned := detectAbandonedBackgroundTask(output)
	if abandoned == nil {
		backgroundTracker.reset(taskID)
		return false
	}
	handleAbandonedBackgroundTaskRetry(bb, config, taskID, runtimeConfig, *abandoned, backgroundTracker, spinTracker)
	return true
}
