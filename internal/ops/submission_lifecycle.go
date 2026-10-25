package ops

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

const (
	// submissionSHA1HexLength and submissionSHA256HexLength are the lengths of a
	// full immutable commit SHA in each Git object format.
	submissionSHA1HexLength   = 40
	submissionSHA256HexLength = 64
	// submitTimeoutRequestIDLength is how much of the settled transition names
	// the request of the batch-timeout block.
	submitTimeoutRequestIDLength = 16
)

// preparedSubmission keeps warning-only indexing outside task locking while
// retaining the captured candidate and final transaction in one call frame.
type preparedSubmission struct {
	replay   *SubmitForReviewResult
	refresh  func() []string
	complete func() (*SubmitForReviewResult, error)
	// acceptanceBatch is the time complete may spend: the canonical batch
	// budget, plus any admission slot wait still ahead of it.
	acceptanceBatch time.Duration
}

type submissionInvocation struct {
	task        *models.Task
	effects     bool
	preparation *models.LifecyclePreparation
	// admission is what pre-admission settled; nil when state was unreadable.
	admission *submissionAdmission
	progress  *submitProgress
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
			invocation.task = readLifecycleTask(projectRoot, taskID, authority, opts)
			outcome, action, effects := models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone
			var invalid *PreconditionError
			if !invocation.effects && errors.As(err, &invalid) {
				outcome, action = models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput
			}
			if invocation.effects {
				effects = LifecycleEffectsUnknown
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
		return nil, &LifecycleError{Outcome: NewLifecycleOutcome(integrationOperationSubmitForReview, nil, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone), Err: validateErr}
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
	invocation.progress = startSubmitProgress(taskID)
	defer invocation.progress.finish()
	hold, err := holdSubmitTask(projectRoot, taskID, agentID, authority, opts)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := hold.Release(); err != nil {
			log.Printf("WARNING: submit hold for task %s not released: %v", taskID, err)
		}
	}()
	invocation.admission, err = preAdmitSubmission(projectRoot, taskID, agentID, authority, opts, invocation.progress)
	if err != nil {
		return nil, err
	}
	defer invocation.admission.release()
	invocation.progress.set("preparing the submission (rebase, index refresh)", time.Time{})
	err = WithProjectLifecycleSharedLock(projectRoot, integrationOperationSubmitForReview, func() error {
		lock := filelock.New(claimTaskWorktreeLockPath(paths.New(projectRoot).StatePath(), taskID))
		// Retirement usually follows a state-lock timeout, so it waits the
		// patient budget; this short-lived CLI process cannot retry it later.
		retirementBB := RequestBlackboard(paths.New(projectRoot).StatePath(), authority, opts).Patient()
		var prepared *preparedSubmission
		if prepareErr := lock.WithLockOperation(integrationOperationSubmitForReview, func() error {
			var inner error
			prepared, inner = prepareSubmitForReview(projectRoot, taskID, commitRef, agentID, authority, opts, invocation)
			return inner
		}); prepareErr != nil {
			return retireFailedLifecyclePreparation(retirementBB, taskID, authority, invocation.preparation, prepareErr, LifecycleEffectsUnknown)
		}
		if prepared.replay != nil {
			result = prepared.replay
			return nil
		}
		if authority != nil && (invocation.admission == nil || invocation.admission.removeMarker == nil) {
			// Lets the agent's own supervisor hold its timeouts while this
			// submit runs, up to the marker's fixed deadline (D-38).
			// Pre-admission already wrote one covering its slot wait.
			removeMarker := writeInflightSubmitMarker(projectRoot, *authority, taskID, prepared.acceptanceBatch, time.Now().UTC())
			defer removeMarker()
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
		return retireFailedLifecyclePreparation(retirementBB, taskID, authority, invocation.preparation, commitErr, LifecycleEffectsUnknown)
	})
	blockRuntimeInputRefusal(projectRoot, taskID, agentID, authority, err)
	blockBatchTimeoutRefusal(projectRoot, taskID, agentID, authority, err)
	return result, err
}

// submitBatchTimeoutBlockTestHook runs after a batch-timeout submit released
// its locks and before it blocks the task; tests use it to race the block.
var submitBatchTimeoutBlockTestHook func()

// blockBatchTimeoutRefusal moves a task whose submit settled a batch timeout
// under the admission timeout retry policy to BLOCKED for a budget ruling,
// after the submission's locks are released (ADR-0216). The block is fenced
// to the transition the submit's own retirement produced, so a task released,
// unblocked or reclaimed meanwhile is never blocked by this stale outcome.
func blockBatchTimeoutRefusal(projectRoot, taskID, agentID string, authority *models.AgentAuthority, err error) {
	var refusal *AcceptanceEvidenceError
	if !errors.As(err, &refusal) || refusal.Class != AcceptanceFaultBatchTimeout {
		return
	}
	var retired *LifecycleError
	if !errors.As(err, &retired) || retired.Outcome.TransitionID == "" {
		refusal.BlockSkipped = "the task's state after this submit could not be confirmed; inspect it before blocking"
		return
	}
	if submitBatchTimeoutBlockTestHook != nil {
		submitBatchTimeoutBlockTestHook()
	}
	transition := retired.Outcome.TransitionID
	reason := fmt.Sprintf("canonical admission timed out under admission_timeout_retry (ruling %s): %s", refusal.Ruling, refusal.Reason)
	question := fmt.Sprintf("Raise the canonical admission budget for %s with a budget exception ruling, or correct timeout_seconds?", taskID)
	opts := MarkBlockedOptions{Request: LifecycleRequestOptions{RequestID: "admission-timeout:" + transition[:submitTimeoutRequestIDLength], ExpectedTransition: transition}}
	var blockErr error
	if authority != nil {
		_, blockErr = MarkBlockedWithAuthority(projectRoot, taskID, reason, []string{question}, *authority, opts)
	} else {
		_, blockErr = MarkBlockedWithOptions(projectRoot, taskID, reason, []string{question}, agentID, opts)
	}
	// Only an observed different transition proves the task moved on; any
	// other refusal leaves the block to the doer.
	var moved *LifecycleError
	switch {
	case blockErr == nil:
		refusal.Blocked = true
	case errors.As(blockErr, &moved) && moved.Outcome.TransitionID != "" && moved.Outcome.TransitionID != transition:
		refusal.BlockSkipped = "the task moved on after this submit"
	default:
		refusal.BlockFailed = true
	}
}

// blockRuntimeInputRefusal moves a task whose submission gate refused a
// runtime input to BLOCKED with the operator question, after the submission's
// locks are released. The refusal and its anomaly are already durable; a
// failed block is reported on the refusal so the doer can block by hand.
func blockRuntimeInputRefusal(projectRoot, taskID, agentID string, authority *models.AgentAuthority, err error) {
	var refusal *AcceptanceEvidenceError
	if !errors.As(err, &refusal) || refusal.Class != AcceptanceFaultRuntimeInput || refusal.Claim != nil {
		return
	}
	reason, question := runtimeInputBlockText(refusal)
	// Only the operator can provision the input, so the question is the ask.
	opts := MarkBlockedOptions{HumanAction: runtimeInputHumanAsk(refusal)}
	var blockErr error
	if authority != nil {
		_, blockErr = MarkBlockedWithAuthority(projectRoot, taskID, reason, []string{question}, *authority, opts)
	} else {
		_, blockErr = MarkBlockedWithOptions(projectRoot, taskID, reason, []string{question}, agentID, opts)
	}
	refusal.Blocked = blockErr == nil
	refusal.BlockFailed = blockErr != nil
}

func fullSubmissionSHA(value string) bool {
	if len(value) != submissionSHA1HexLength && len(value) != submissionSHA256HexLength {
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
					Outcome: NewLifecycleOutcome(request.Operation, task, models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone),
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
