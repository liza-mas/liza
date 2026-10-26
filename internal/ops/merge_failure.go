package ops

import (
	"errors"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/db"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/secretmask"
)

// maxIntegrationDiagnosticOutputExcerptBytes bounds test output persisted into
// task history. Full output remains transient on IntegrationFailedError.
const maxIntegrationDiagnosticOutputExcerptBytes = 4096

// appendUniqueAgentID adds an agent ID to failed_by if not already present
func appendUniqueAgentID(failedBy []string, agentID string) []string {
	if slices.Contains(failedBy, agentID) {
		return failedBy
	}
	return append(failedBy, agentID)
}

// pipelineTransition applies a status transition using the pipeline transition map.
func pipelineTransition(t *models.Task, to models.TaskStatus, pb *pipelineBundle) error {
	return t.TransitionWith(to, pb.transitions)
}

// markIntegrationFailed transitions a task to INTEGRATION_FAILED under lock.
// Re-validates the task is still in an approved state to prevent concurrent transitions.
// If mergeCommit is non-empty, it's recorded on both the task and the history entry.
func markIntegrationFailed(bb *db.Blackboard, taskID, agentID string, reason IntegrationReason, mergeCommit string, pb *pipelineBundle) error {
	return markIntegrationFailedWithDiagnostic(
		bb,
		taskID,
		agentID,
		reason,
		mergeCommit,
		pb,
		integrationFailureDiagnostic(reason, mergeCommit, "", nil),
	)
}

func markIntegrationFailedWithAuthority(bb *db.Blackboard, taskID string, authority models.AgentAuthority, reason IntegrationReason, mergeCommit string, pb *pipelineBundle) error {
	return markIntegrationFailedWithDiagnosticAuthority(
		bb,
		taskID,
		authority.ID,
		&authority,
		reason,
		mergeCommit,
		pb,
		integrationFailureDiagnostic(reason, mergeCommit, "", nil),
	)
}

func markIntegrationFailedWithDiagnostic(
	bb *db.Blackboard,
	taskID, agentID string,
	reason IntegrationReason,
	mergeCommit string,
	pb *pipelineBundle,
	diagnostic map[string]any,
) error {
	return markIntegrationFailedWithDiagnosticAuthority(bb, taskID, agentID, nil, reason, mergeCommit, pb, diagnostic)
}

func markIntegrationFailedWithDiagnosticAuthority(
	bb *db.Blackboard,
	taskID, agentID string,
	authority *models.AgentAuthority,
	reason IntegrationReason,
	mergeCommit string,
	pb *pipelineBundle,
	diagnostic map[string]any,
	request ...LifecycleRequest,
) error {
	var pr models.PipelineResolver
	if pb != nil {
		pr = pb.pr
	}
	reasonText := string(reason)
	return modifyLifecycleState(bb, authority, func(s *models.State) error {
		t := s.FindTask(taskID)
		if t == nil {
			return &lizaerrors.NotFoundError{Entity: "task", ID: taskID}
		}
		if !models.IsApprovedForMerge(t, pr) {
			return fmt.Errorf("task %s status changed concurrently (now %s)", taskID, t.Status)
		}
		if len(request) > 0 {
			if err := ValidateLifecyclePreparation(t, request[0]); err != nil {
				return err
			}
		}
		if err := pipelineTransition(t, models.TaskStatusIntegrationFailed, pb); err != nil {
			return err
		}
		models.AdvanceLifecycle(t)
		t.FailedBy = appendUniqueAgentID(t.FailedBy, agentID)
		now := time.Now().UTC()
		entry := models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventIntegrationFailed,
			Agent:  &agentID,
			Reason: &reasonText,
		}
		if mergeCommit != "" {
			t.MergeCommit = &mergeCommit
			entry.Commit = &mergeCommit
		}
		if len(diagnostic) > 0 {
			t.IntegrationFailure = cloneMapForTaskDiagnostic(diagnostic)
			entry.Extra = map[string]any{
				"diagnostic": cloneMapForTaskDiagnostic(diagnostic),
			}
		}
		t.History = append(t.History, entry)
		blocked, err := blockTaskForHypothesisExhaustion(s, t, agentID, pb.transitions, now)
		if err != nil {
			return err
		}

		if !blocked {
			// Refresh lease — task stays assigned to original coder for conflict resolution.
			renewLease(s, t)
			if t.AssignedTo != nil {
				if agent, ok := s.Agents[*t.AssignedTo]; ok {
					agent.LeaseExpires = t.LeaseExpires
					s.Agents[*t.AssignedTo] = agent
				}
			}
		}
		t.HandoffEvents = append(t.HandoffEvents, models.HandoffEvent{
			Timestamp: time.Now().UTC(),
			Agent:     agentID,
			Trigger:   models.HandoffTriggerSubmission,
			Failed:    []string{reasonText},
			NextStep:  integrationFailureRecoveryHint(reason),
		})

		return nil
	})
}

func integrationFailureDiagnostic(reason IntegrationReason, mergeCommit, testOutput string, rollbackErr error) map[string]any {
	diagnostic := map[string]any{
		"operation":     integrationOperationWTMerge,
		"reason":        string(reason),
		"recovery_hint": integrationFailureRecoveryHint(reason),
	}
	if mergeCommit != "" {
		diagnostic["merge_commit"] = mergeCommit
	}
	if testOutput != "" {
		outputExcerpt, truncated := persistedOutputExcerpt(secretmask.New().MaskText(testOutput), maxIntegrationDiagnosticOutputExcerptBytes)
		diagnostic["test_output_excerpt"] = outputExcerpt
		diagnostic["output_truncated"] = truncated
		// Raw byte count is pre-redaction; it describes the transient command output size.
		diagnostic["output_bytes"] = len([]byte(testOutput))
		diagnostic["failing_command"] = integrationTestCommand
	}
	if rollbackErr != nil {
		diagnostic["rollback_error"] = rollbackErr.Error()
	}
	return diagnostic
}

func cloneMapForTaskDiagnostic(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]any, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func integrationFailureDiagnosticWithDetail(reason IntegrationReason, detail, mergeCommit, testOutput string, rollbackErr error) map[string]any {
	diagnostic := integrationFailureDiagnostic(reason, mergeCommit, testOutput, rollbackErr)
	if detail != "" {
		diagnostic["detail"] = detail
	}
	return diagnostic
}

func persistedOutputExcerpt(output string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len([]byte(output)) <= maxBytes {
		return output, false
	}

	excerpt := []byte(output)
	excerpt = excerpt[:maxBytes]
	for !utf8.Valid(excerpt) && len(excerpt) > 0 {
		excerpt = excerpt[:len(excerpt)-1]
	}
	return string(excerpt), true
}

func integrationFailureRecoveryHint(reason IntegrationReason) string {
	switch reason {
	case IntegrationReasonHEADMismatch:
		return "verify the task worktree HEAD matches review_commit before retrying integration"
	case IntegrationReasonMergeConflict:
		return "resolve the integration conflict in the task worktree, resubmit, and retry merge"
	case IntegrationReasonTestsFailed:
		return "inspect the integration test output, fix the task branch, resubmit, and retry merge"
	case IntegrationReasonStateInvalid:
		return "restore or preserve referenced artifacts, resubmit, and retry merge"
	default:
		return "inspect the integration failure details before retrying merge"
	}
}

// recordFailure persists INTEGRATION_FAILED for this invocation. The
// preparation request is passed once the preparation exists, so the write also
// proves the invocation still owns it.
func (m *mergeInvocation) recordFailure(reason IntegrationReason, mergeCommit string, diagnostic map[string]any, request ...LifecycleRequest) error {
	if err := markIntegrationFailedWithDiagnosticAuthority(m.bb, m.taskID, m.agentID, m.authority, reason, mergeCommit, m.pb, diagnostic, request...); err != nil {
		return fmt.Errorf("failed to update state to INTEGRATION_FAILED: %w", err)
	}
	return nil
}

// wrapFailure projects an invocation's failure onto the lifecycle result,
// observing committed state rather than the invocation's in-memory task.
func (m *mergeInvocation) wrapFailure(err error) error {
	task := readLifecycleTask(m.projectRoot, m.taskID, m.authority, m.opts)
	outcome, action := models.LifecycleStateChanged, LifecycleSafeActionRequery
	var invalid *PreconditionError
	if m.effects == LifecycleEffectsNone && errors.As(err, &invalid) {
		outcome, action = models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput
	}
	return WrapLifecycleError(integrationOperationWTMerge, task, err, outcome, action, m.effects)
}

// recordHeadMismatch fails the merge because the task worktree no longer holds
// the approved commit. It runs before the preparation, so no request is passed.
func (m *mergeInvocation) recordHeadMismatch(wtHEAD string) error {
	detail := fmt.Sprintf("worktree HEAD (%s) does not match approved commit (%s)", shortSHA(wtHEAD), shortSHA(m.expectedCommit))
	diagnostic := integrationFailureDiagnosticWithDetail(IntegrationReasonHEADMismatch, detail, "", "", nil)
	if err := m.recordFailure(IntegrationReasonHEADMismatch, "", diagnostic); err != nil {
		return err
	}
	m.effects = LifecycleEffectsCommitted
	return &IntegrationFailedError{Reason: IntegrationReasonHEADMismatch}
}

// classifyPublicationFailure turns a failed forward publication into the
// merge's error. Two failures prove nothing was published: the forward lock
// timed out before its callback ran, or the guard, which runs before
// update-ref, could not read state. Neither says anything about the candidate,
// so the task stays approved (D78). Retiring the preparation advances the
// boundary, hence requery; only lock contention is RETRYABLE.
func (m *mergeInvocation) classifyPublicationFailure(err error) error {
	var unavailable *candidateArtifactGuardUnavailableError
	lockTimeout := filelock.IsLockErrorType(err, filelock.LockErrorTimeout)
	if errors.As(err, &unavailable) || (!m.casStarted && lockTimeout) {
		m.forwardUnpublished = true
		m.effects = LifecycleEffectsNone
		result := models.LifecycleStateChanged
		if lockTimeout {
			result = models.LifecycleRetryable
		}
		return &LifecycleError{Outcome: NewLifecycleOutcome(integrationOperationWTMerge, nil, result, "requery", m.effects), Err: err}
	}
	var artifactErr *candidateArtifactGuardError
	if errors.As(err, &artifactErr) {
		diagnostic := integrationFailureDiagnosticWithDetail(IntegrationReasonStateInvalid, err.Error(), "", "", nil)
		if updateErr := m.recordFailure(IntegrationReasonStateInvalid, "", diagnostic, m.request); updateErr != nil {
			return updateErr
		}
		return &IntegrationFailedError{Reason: IntegrationReasonStateInvalid, Cause: err}
	}
	return err
}

// recordConflict fails the merge on a true merge conflict found by the CAS.
func (m *mergeInvocation) recordConflict() error {
	diagnostic := integrationFailureDiagnostic(IntegrationReasonMergeConflict, "", "", nil)
	if updateErr := m.recordFailure(IntegrationReasonMergeConflict, "", diagnostic, m.request); updateErr != nil {
		return updateErr
	}
	return &IntegrationFailedError{Reason: IntegrationReasonMergeConflict}
}
