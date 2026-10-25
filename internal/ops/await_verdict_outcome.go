package ops

import (
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

// AwaitVerdictOutcome is the closed set of AwaitVerdictResult.Verdict values.
// The result field stays a string; a constant reaches it by conversion.
type AwaitVerdictOutcome string

// Verdict constants for AwaitVerdictResult.Verdict.
const (
	VerdictApproved            AwaitVerdictOutcome = "APPROVED"
	VerdictRejected            AwaitVerdictOutcome = "REJECTED"
	VerdictNewAttempt          AwaitVerdictOutcome = "NEW_ATTEMPT"
	VerdictAlreadyTransitioned AwaitVerdictOutcome = "ALREADY_TRANSITIONED"
	VerdictTerminal            AwaitVerdictOutcome = "TERMINAL"
	VerdictTimeout             AwaitVerdictOutcome = "TIMEOUT"
	VerdictAborted             AwaitVerdictOutcome = "ABORTED"
	VerdictPoll                AwaitVerdictOutcome = "POLL" // Blocking interval expired, caller should retry
	VerdictPaused              AwaitVerdictOutcome = "PAUSED"
)

// AwaitSafeAction is the closed set of AwaitVerdictResult.SafeAction values. The
// result field stays a string; a constant reaches it by conversion.
type AwaitSafeAction string

const (
	SafeActionRevise AwaitSafeAction = "revise"
	SafeActionStop   AwaitSafeAction = "stop"
)

// ErrBudgetExhausted is returned when the iteration/review-cycle budget
// would be exceeded on rejection — the agent should exit normally.
var ErrBudgetExhausted = stderrors.New("budget exhausted: iteration or review-cycle limit reached")

// AwaitVerdictResult holds the outcome of blocking on a review verdict.
type AwaitVerdictResult struct {
	Verdict         string            `json:"verdict"`                    // One of the Verdict* constants
	Reason          string            `json:"reason"`                     // Rejection reason or terminal explanation
	ReviewerAgent   string            `json:"reviewer_agent"`             // Agent ID that issued the verdict (empty if timeout/abort)
	TaskStatus      models.TaskStatus `json:"task_status"`                // Final observed task status
	Iteration       int               `json:"iteration"`                  // Current iteration number (post-reclaim if rejected)
	Guidance        string            `json:"guidance"`                   // Inline guidance for the agent on rejection
	ReviewCommit    string            `json:"review_commit,omitempty"`    // Commit associated with the submitted review, when known
	CurrentAssignee string            `json:"current_assignee,omitempty"` // Current task assignee when task already moved onward
	SafeAction      string            `json:"safe_action,omitempty"`      // revise or stop, when the system can determine it
}

// verdictCheck holds the result of checking whether a verdict has arrived.
type verdictCheck struct {
	status models.TaskStatus
}

// decidedAwaitVerdict answers an await that finds its task already decided
// (BLOCKED or terminal): the durable verdict record when the task moved onward
// before the submitter consumed it, otherwise a TERMINAL stop for the last
// submitter.
func decidedAwaitVerdict(task *models.Task, agentID string) (*AwaitVerdictResult, error) {
	if result, ok := recoveredVerdictFromHistory(task, agentID); ok {
		return result, nil
	}
	if err := checkLastSubmitter(task, agentID); err != nil {
		return nil, err
	}
	return &AwaitVerdictResult{
		Verdict:    string(VerdictTerminal),
		TaskStatus: task.Status,
		Reason:     fmt.Sprintf("task already %s — no verdict expected", task.Status),
		Guidance:   stopVerdictGuidance(task.Status),
		SafeAction: string(SafeActionStop),
	}, nil
}

// approvedAwaitResult is the APPROVED outcome: the doer stops.
func approvedAwaitResult(task *models.Task) *AwaitVerdictResult {
	return &AwaitVerdictResult{
		Verdict:       string(VerdictApproved),
		TaskStatus:    task.Status,
		ReviewerAgent: extractReviewerFromHistory(task),
		Guidance:      stopVerdictGuidance(task.Status),
		SafeAction:    string(SafeActionStop),
	}
}

// rejectedVerdict is the reviewer and reason a rejection carries into the
// reclaim result.
type rejectedVerdict struct {
	reviewer string
	reason   string
}

// reclaimRejected maps a rejected task to its result. It attempts the doer's
// auto-reclaim via ClaimTask, which classifies the iteration and review limits
// itself, so no same-versus-new-attempt detection is needed here. A successful
// reclaim keeps the ownership the claim just assigned.
func (r awaitVerdictRun) reclaimRejected(task *models.Task, rejected models.TaskStatus) (*AwaitVerdictResult, error) {
	verdict := rejectedVerdict{reviewer: extractReviewerFromHistory(task)}
	if task.RejectionReason != nil {
		verdict.reason = *task.RejectionReason
	}

	var claimErr error
	if r.authority != nil {
		_, claimErr = ClaimTaskWithAuthority(r.projectRoot, task.ID, *r.authority)
	} else {
		_, claimErr = ClaimTask(r.projectRoot, task.ID, r.agentID)
	}
	if claimErr != nil {
		return r.reclaimFailureResult(task, claimErr, rejected, verdict)
	}

	// Reclaim succeeded — re-read task to get updated iteration.
	_, updatedTask, readErr := readTaskState(r.bb, task.ID)
	iteration := task.Iteration
	if readErr == nil && updatedTask != nil {
		iteration = updatedTask.Iteration
	}

	return &AwaitVerdictResult{
		Verdict:       string(VerdictRejected),
		Reason:        verdict.reason,
		ReviewerAgent: verdict.reviewer,
		TaskStatus:    task.Status,
		Iteration:     iteration,
		Guidance:      buildRejectionGuidance(verdict.reason, task),
	}, nil
}

// reclaimFailureResult maps a failed auto-reclaim to its result: a halted
// system pauses, a transition to a new attempt and a limit exhaustion report
// through the verdict, and an infrastructure error surfaces unless the verdict
// can be recovered from history.
func (r awaitVerdictRun) reclaimFailureResult(task *models.Task, claimErr error, rejected models.TaskStatus, verdict rejectedVerdict) (*AwaitVerdictResult, error) {
	var halted *SystemHaltedError
	if stderrors.As(claimErr, &halted) {
		// The task stays rejected and is reclaimed after resume; this
		// session must not start another iteration while halted.
		return r.finishPaused(task, halted.Mode)
	}
	var pe *PreconditionError
	if stderrors.As(claimErr, &pe) {
		if strings.Contains(pe.Reason, "transitioned to attempt") {
			return r.finish(&AwaitVerdictResult{
				Verdict:       string(VerdictNewAttempt),
				Reason:        verdict.reason,
				ReviewerAgent: verdict.reviewer,
				TaskStatus:    task.Status,
			}, nil)
		}
		// Blocked or other limit exhaustion.
		if result, ok := recoveredVerdictAfterReclaimFailure(r.bb, task.ID, r.agentID, rejected); ok {
			return r.finish(result, nil)
		}
		return r.finish(&AwaitVerdictResult{
			Verdict:       string(VerdictTerminal),
			Reason:        verdict.reason,
			ReviewerAgent: verdict.reviewer,
			TaskStatus:    task.Status,
			Guidance:      stopVerdictGuidance(task.Status),
			SafeAction:    string(SafeActionStop),
		}, nil)
	}
	// A degraded claim already recorded agent health and bounded retries;
	// surface it instead of converting it into a verdict, so the failure
	// stays visible and this agent stops retrying the failing path.
	if stderrors.Is(claimErr, ErrAgentDegraded) {
		return r.finish(nil, fmt.Errorf("auto-reclaim failed: %w", claimErr))
	}

	// Infrastructure error during reclaim.
	if result, ok := recoveredVerdictAfterReclaimFailure(r.bb, task.ID, r.agentID, rejected); ok {
		return r.finish(result, nil)
	}
	return r.finish(nil, fmt.Errorf("auto-reclaim failed: %w", claimErr))
}

// finishNonAwaitable maps a task that left the awaitable set for a status that
// is neither approved nor rejected: the recovered verdict when history holds
// one, otherwise a TERMINAL stop.
func (r awaitVerdictRun) finishNonAwaitable(task *models.Task) (*AwaitVerdictResult, error) {
	if result, ok := recoveredVerdictFromHistory(task, r.agentID); ok {
		return r.finish(result, nil)
	}
	return r.finish(&AwaitVerdictResult{
		Verdict:    string(VerdictTerminal),
		TaskStatus: task.Status,
		Reason:     fmt.Sprintf("task entered non-awaitable status: %s", task.Status),
		Guidance:   stopVerdictGuidance(task.Status),
		SafeAction: string(SafeActionStop),
	}, nil)
}

// buildRejectionGuidance constructs inline guidance for the agent on rejection,
// equivalent to the prior_rejection.tmpl content.
func buildRejectionGuidance(reason string, task *models.Task) string {
	var b strings.Builder
	b.WriteString("## Rejection Feedback\n\n")
	b.WriteString("You MUST ADDRESS the following rejection feedback before resubmitting:\n\n")
	b.WriteString(reason)
	b.WriteString("\n\n")
	if task.Scope != "" {
		b.WriteString("If the fix requires changes outside your declared scope, use scope_extensions in your checkpoint.\n\n")
	}
	fmt.Fprintf(&b, "Current iteration: %d\n", task.Iteration)
	return b.String()
}

// extractReviewerFromHistory scans task history in reverse for the most recent
// review verdict and returns the reviewer agent ID.
func extractReviewerFromHistory(task *models.Task) string {
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		if entry.Event == models.TaskEventApproved ||
			entry.Event == models.TaskEventRejected {
			if entry.Agent != nil {
				return *entry.Agent
			}
			return ""
		}
	}
	return ""
}

type submittedReview struct {
	index  int
	commit string
	at     time.Time
}

type recoveredVerdict struct {
	event    models.TaskEventName
	reason   string
	reviewer string
	commit   string
}

func latestSubmissionByAgent(task *models.Task, agentID string) (submittedReview, bool) {
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		if entry.Event != models.TaskEventSubmittedForReview {
			continue
		}
		if entry.Agent == nil || *entry.Agent != agentID {
			continue
		}
		commit := ""
		if entry.Commit != nil {
			commit = *entry.Commit
		}
		return submittedReview{index: i, commit: commit, at: entry.Time}, true
	}
	return submittedReview{}, false
}

func recoveredVerdictFromHistory(task *models.Task, agentID string) (*AwaitVerdictResult, bool) {
	submission, ok := latestSubmissionByAgent(task, agentID)
	if !ok {
		return nil, false
	}
	verdict, ok := findVerdictAfterSubmission(task, submission)
	if !ok {
		return nil, false
	}

	currentAssignee := ""
	if task.AssignedTo != nil {
		currentAssignee = *task.AssignedTo
	}

	result := &AwaitVerdictResult{
		Verdict:         string(VerdictAlreadyTransitioned),
		Reason:          verdict.reason,
		ReviewerAgent:   verdict.reviewer,
		TaskStatus:      task.Status,
		ReviewCommit:    verdict.commit,
		CurrentAssignee: currentAssignee,
		SafeAction:      string(safeActionForRecoveredVerdict(task, agentID, verdict.event)),
	}
	result.Guidance = recoveredVerdictGuidance(result)
	return result, true
}

func findVerdictAfterSubmission(task *models.Task, submission submittedReview) (recoveredVerdict, bool) {
	commit := submission.commit
	for i := submission.index + 1; i < len(task.History); i++ {
		entry := task.History[i]
		if entry.Event == models.TaskEventSubmittedForReview {
			return recoveredVerdict{}, false
		}
		if entry.Event == models.TaskEventReviewCommitUpdated {
			commit = updatedReviewCommit(entry, commit)
			continue
		}
		if entry.Event != models.TaskEventApproved && entry.Event != models.TaskEventRejected {
			continue
		}
		if commit != "" && entry.Commit != nil && *entry.Commit != commit {
			continue
		}
		if commit != "" && entry.Commit == nil {
			continue
		}

		reason := ""
		if entry.Reason != nil {
			reason = *entry.Reason
		} else if entry.Event == models.TaskEventRejected && task.RejectionReason != nil {
			reason = *task.RejectionReason
		}
		reviewer := ""
		if entry.Agent != nil {
			reviewer = *entry.Agent
		}
		if commit == "" && entry.Commit != nil {
			commit = *entry.Commit
		}
		return recoveredVerdict{
			event:    entry.Event,
			reason:   reason,
			reviewer: reviewer,
			commit:   commit,
		}, true
	}
	return recoveredVerdict{}, false
}

func updatedReviewCommit(entry models.TaskHistoryEntry, current string) string {
	oldCommit, _ := entry.Extra["old_review_commit"].(string)
	newCommit, _ := entry.Extra["new_review_commit"].(string)
	if newCommit != "" && (current == "" || oldCommit == "" || oldCommit == current) {
		return newCommit
	}
	if entry.Commit != nil && (current == "" || oldCommit == current) {
		return *entry.Commit
	}
	return current
}

func recoveredVerdictAfterReclaimFailure(bb *db.Blackboard, taskID, agentID string, rejectedStatus models.TaskStatus) (*AwaitVerdictResult, bool) {
	_, currentTask, err := readTaskState(bb, taskID)
	if err != nil || currentTask == nil {
		return nil, false
	}
	if currentTask.Status == rejectedStatus {
		return nil, false
	}
	return recoveredVerdictFromHistory(currentTask, agentID)
}

func safeActionForRecoveredVerdict(task *models.Task, agentID string, verdictEvent models.TaskEventName) AwaitSafeAction {
	if verdictEvent == models.TaskEventApproved {
		return SafeActionStop
	}
	if task.AssignedTo != nil && *task.AssignedTo == agentID {
		return SafeActionRevise
	}
	return SafeActionStop
}

func stopVerdictGuidance(status models.TaskStatus) string {
	if status == "" {
		return "Stop this session; do not run more worktree commands for this submission."
	}
	return fmt.Sprintf(
		"Stop this session; do not run more worktree commands for this submission. Task status is %s, and %s may clean up the task worktree after terminal merge.",
		status, brand.NameTitle,
	)
}

func disappearedVerdictResult() *AwaitVerdictResult {
	return &AwaitVerdictResult{
		Verdict:    string(VerdictTerminal),
		Reason:     "task disappeared from state",
		Guidance:   stopVerdictGuidance(""),
		SafeAction: string(SafeActionStop),
	}
}

func recoveredVerdictGuidance(result *AwaitVerdictResult) string {
	if result.SafeAction == string(SafeActionRevise) {
		return fmt.Sprintf(
			"Review verdict was recovered after the task already moved to %s. Address the rejection feedback before resubmitting.",
			result.TaskStatus,
		)
	}
	if result.CurrentAssignee != "" {
		return fmt.Sprintf(
			"Review verdict was recovered after the task already moved to %s and is assigned to %s. Stop this session; do not resubmit from stale context or run more worktree commands.",
			result.TaskStatus,
			result.CurrentAssignee,
		)
	}
	return fmt.Sprintf(
		"Review verdict was recovered after the task already moved to %s. Stop this session; do not retry await-verdict or run more worktree commands for this submission.",
		result.TaskStatus,
	)
}
