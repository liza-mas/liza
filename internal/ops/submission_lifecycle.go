package ops

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// preparedSubmission keeps warning-only indexing outside task locking while
// retaining the captured candidate and final transaction in one call frame.
type preparedSubmission struct {
	replay   *SubmitForReviewResult
	refresh  func() []string
	complete func() (*SubmitForReviewResult, error)
}

type submissionInvocation struct {
	task        *models.Task
	effects     bool
	preparation *models.LifecyclePreparation
}

// SubmitForReviewWithAuthorityAndOptions binds retries to the caller's original
// inspected transition. Existing entry points retain their legacy signatures.
func SubmitForReviewWithAuthorityAndOptions(projectRoot, taskID, commitRef string, authority models.AgentAuthority, opts LifecycleRequestOptions) (*SubmitForReviewResult, error) {
	return submitForReviewLifecycle(projectRoot, taskID, commitRef, authority.ID, &authority, opts)
}

func submitForReviewLifecycle(projectRoot, taskID, commitRef, agentID string, authority *models.AgentAuthority, opts LifecycleRequestOptions) (result *SubmitForReviewResult, err error) {
	invocation := &submissionInvocation{}
	observation := NewLifecycleInvocation(projectRoot)
	defer func() {
		if err != nil {
			// Finalization may lose a race after Git or indexing. Report a fresh
			// authorized observation, never a callback's uncommitted candidate.
			invocation.task = readLifecycleTask(projectRoot, taskID, authority)
			outcome, action, effects := models.LifecycleStateChanged, "requery", "none"
			var invalid *PreconditionError
			if !invocation.effects && errors.As(err, &invalid) {
				outcome, action = models.LifecycleInvalidInput, "correct_input"
			}
			if invocation.effects {
				effects = "unknown"
			}
			err = WrapLifecycleError(integrationOperationSubmitForReview, invocation.task, err, outcome, action, effects)
		}
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if result != nil {
			outcome, warnings = result.LifecycleOutcome, &result.Warnings
		}
		observation.FinishResult(integrationOperationSubmitForReview, outcome, &err, warnings)
	}()
	if validateErr := ValidateLifecycleRequestOptions(opts); validateErr != nil {
		return nil, &LifecycleError{Outcome: NewLifecycleOutcome(integrationOperationSubmitForReview, nil, models.LifecycleInvalidInput, "correct_input", "none"), Err: validateErr}
	}
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if commitRef == "" {
		return nil, &PreconditionError{Reason: "commit ref is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: fmt.Sprintf("%s is required", brand.EnvName("AGENT_ID"))}
	}
	err = WithProjectLifecycleSharedLock(projectRoot, integrationOperationSubmitForReview, func() error {
		lock := filelock.New(claimTaskWorktreeLockPath(paths.New(projectRoot).StatePath(), taskID))
		var prepared *preparedSubmission
		if prepareErr := lock.WithLockOperation(integrationOperationSubmitForReview, func() error {
			var inner error
			prepared, inner = prepareSubmitForReview(projectRoot, taskID, commitRef, agentID, authority, opts, invocation)
			return inner
		}); prepareErr != nil {
			return retireFailedLifecyclePreparation(db.For(paths.New(projectRoot).StatePath()), taskID, authority, invocation.preparation, prepareErr, "unknown")
		}
		if prepared.replay != nil {
			result = prepared.replay
			return nil
		}
		warnings := prepared.refresh()
		commitErr := lock.WithLockOperation("submit-for-review-finalize", func() error {
			var inner error
			result, inner = prepared.complete()
			return inner
		})
		if result != nil {
			result.Warnings = append(result.Warnings, warnings...)
		}
		return retireFailedLifecyclePreparation(db.For(paths.New(projectRoot).StatePath()), taskID, authority, invocation.preparation, commitErr, "unknown")
	})
	return result, err
}

func fullSubmissionSHA(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// submissionRequest proves implicit legacy replay only with a full immutable
// input SHA from a retained receipt at the same attempt/iteration boundary.
func submissionRequest(task *models.Task, commitRef, actor string, authority *models.AgentAuthority, opts LifecycleRequestOptions, agents map[string]models.Agent) (LifecycleRequest, *models.LifecycleReceipt, error) {
	if fullSubmissionSHA(commitRef) {
		commitRef = strings.ToLower(commitRef)
	}
	request, err := NewLifecycleRequest(integrationOperationSubmitForReview, task, actor, authority, opts, map[string]string{"input_commit": commitRef})
	if err != nil {
		return request, nil, err
	}
	implicit := opts.RequestID == "" && fullSubmissionSHA(commitRef)
	if implicit {
		request.RequestID = "sha:" + commitRef
		if task.Lifecycle != nil {
			for _, receipt := range task.Lifecycle.Receipts {
				if receipt.Operation == request.Operation && receipt.Actor == actor &&
					receipt.GenerationDigest == request.GenerationDigest && receipt.Projection.InputCommit == commitRef &&
					receipt.Projection.Attempt == task.EffectiveAttempt() && receipt.Projection.Iteration == task.Iteration {
					request.ExpectedTransition = receipt.ExpectedTransition
					break
				}
			}
		}
	}
	receipt, err := CheckLifecycleRequest(task, request, agents)
	if err != nil || receipt != nil {
		return request, receipt, err
	}
	if implicit {
		for _, event := range task.History {
			if event.Event == models.TaskEventSubmittedForReview && event.SubmissionInputCommit == commitRef {
				return request, nil, &LifecycleError{
					Outcome: NewLifecycleOutcome(request.Operation, task, models.LifecycleStateChanged, "requery", "none"),
					Err:     fmt.Errorf("input SHA was submitted previously; no exact receipt matches this request and authority; inspect before choosing a new request"),
				}
			}
		}
	}
	return request, nil, nil
}

func replaySubmission(task *models.Task, receipt *models.LifecycleReceipt, actor string) *preparedSubmission {
	return &preparedSubmission{replay: &SubmitForReviewResult{
		LifecycleOutcome: LifecycleReplayOutcome(task, receipt, actor),
		TaskID:           task.ID, ReviewCommit: receipt.Projection.ReviewCommit, AgentID: actor,
	}}
}
