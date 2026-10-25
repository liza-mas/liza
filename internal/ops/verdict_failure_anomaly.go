package ops

import (
	stderrors "errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	activitylog "github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/secretmask"
)

// Bounds on what a failed verdict records: masked error text and stack in the
// anomaly and activity log, and the error text of a failed anomaly write.
const (
	verdictFailureErrorLimit        = 2048
	verdictFailureStackLimit        = 4096
	verdictFailureAnomalyErrorLimit = 1024
)

// This file projects verdict failures and stale verdicts into anomalies and the
// activity log after the verdict transaction ended or never ran.

// projectFailure is the failure projection of one verdict attempt. Invalid
// requests and lifecycle recovery responses carry their own outcome; they must
// not release claims or create failure anomalies.
func (a *verdictAttempt) projectFailure(err error) {
	var lifecycleErr *LifecycleError
	if isSubmitVerdictPreconditionError(err) || IsAgentAuthorityError(err) || stderrors.As(err, &lifecycleErr) {
		return
	}
	if err != nil && a.recordFailure {
		recordSubmitVerdictFailure(a.bb, a.logPath, a.taskID, a.agentID, a.authority, string(a.verdict), err)
	}
}

func recordSubmitVerdictFailure(bb *db.Blackboard, logPath, taskID, agentID string, authority *models.AgentAuthority, verdict string, err error) {
	errorText := boundedMaskedErrorString(err, verdictFailureErrorLimit)
	stack := boundedMaskedString(string(debug.Stack()), verdictFailureStackLimit)
	anomalyErr := recordSubmitVerdictFailureAnomaly(bb, taskID, agentID, authority, verdict, err, errorText)
	logSubmitVerdictError(logPath, taskID, agentID, verdict, errorText, stack, boundedMaskedErrorString(anomalyErr, verdictFailureAnomalyErrorLimit))
}

func logSubmitVerdictError(logPath, taskID, agentID, verdict string, errorText string, stack string, anomalyErrText string) {
	if logPath == "" || errorText == "" {
		return
	}
	detail := fmt.Sprintf("verdict=%s error=%s", verdict, errorText)
	if anomalyErrText != "" {
		detail += fmt.Sprintf(" anomaly_recording_error=%s", anomalyErrText)
	}
	if stack != "" {
		detail += fmt.Sprintf(" stack=%s", stack)
	}
	_ = activitylog.New(logPath).Append(activitylog.Entry{
		Agent:  agentIDOrSystem(agentID),
		Action: "submit_verdict_failed",
		Task:   optionalTaskID(taskID),
		Detail: detail,
	})
}

func recordSubmitVerdictFailureAnomaly(bb *db.Blackboard, taskID, agentID string, authority *models.AgentAuthority, verdict string, err error, errorText string) error {
	if bb == nil || err == nil || isSubmitVerdictPreconditionError(err) {
		return nil
	}

	return modifyLifecycleState(bb, authority, func(state *models.State) error {
		state.Anomalies = append(state.Anomalies, models.Anomaly{
			Timestamp: time.Now().UTC(),
			Task:      taskID,
			Reporter:  agentIDOrSystem(agentID),
			Type:      models.AnomalyTypeSubmitVerdictFailed,
			Details: map[string]any{
				"verdict": verdict,
				"error":   errorText,
			},
		})
		return nil
	})
}

func isSubmitVerdictPreconditionError(err error) bool {
	var precondition *PreconditionError
	return stderrors.As(err, &precondition)
}

func boundedMaskedErrorString(err error, limit int) string {
	if err == nil {
		return ""
	}
	return boundedMaskedString(err.Error(), limit)
}

func boundedMaskedString(text string, limit int) string {
	return boundedString(secretmask.New().MaskText(text), limit)
}

func boundedString(text string, limit int) string {
	if limit <= 0 || len([]byte(text)) <= limit {
		return text
	}
	bytes := []byte(text)
	return string(bytes[:limit]) + "... [truncated]"
}

func agentIDOrSystem(agentID string) string {
	if agentID == "" {
		return "system"
	}
	return agentID
}

func optionalTaskID(taskID string) *string {
	if taskID == "" {
		return nil
	}
	return &taskID
}

func recordStaleVerdictAnomaly(bb *db.Blackboard, taskID, agentID string, authority *models.AgentAuthority, verdict, reason, impact string, expectedReviewingStatus, expectedReviewing2Status models.TaskStatus) error {
	now := time.Now().UTC()
	return modifyLifecycleState(bb, authority, func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		if isReviewingStatus(task.Status, expectedReviewingStatus, expectedReviewing2Status) {
			return nil
		}

		appendStaleVerdictAnomaly(state, task, taskID, agentID, verdict, reason, impact, now)

		return nil
	})
}

func appendStaleVerdictAnomaly(state *models.State, task *models.Task, taskID, agentID, verdict, reason, impact string, now time.Time) {
	details := map[string]any{
		"attempted_verdict": verdict,
		"current_status":    string(task.Status),
	}
	if reason != "" {
		details["reason"] = reason
	}
	if impact != "" {
		details["impact"] = impact
	}
	if task.ReviewCommit != nil {
		details["review_commit"] = *task.ReviewCommit
	}

	state.Anomalies = append(state.Anomalies, models.Anomaly{
		Timestamp: now,
		Task:      taskID,
		Reporter:  agentID,
		Type:      models.AnomalyTypeStaleVerdict,
		Details:   details,
	})

	if agent, ok := state.Agents[agentID]; ok && agent.CurrentTask != nil && *agent.CurrentTask == taskID {
		state.ReleaseAgent(agentID)
	}
}
