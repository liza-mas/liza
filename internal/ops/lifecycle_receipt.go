package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// LifecycleRequestOptions identifies one invocation against the boundary the
// caller inspected. Both values must survive unchanged across retries.
type LifecycleRequestOptions struct {
	RequestID          string
	ExpectedTransition string
}

type LifecycleRequest = models.LifecycleIdentity

var lifecycleRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

// ValidateLifecycleRequestOptions performs pure validation before any effects.
func ValidateLifecycleRequestOptions(opts LifecycleRequestOptions) error {
	if (opts.RequestID == "") != (opts.ExpectedTransition == "") {
		return fmt.Errorf("request-id and expected-transition must be supplied together")
	}
	if opts.RequestID != "" && (len(opts.RequestID) > models.LifecycleIdentifierMaxBytes || !lifecycleRequestIDPattern.MatchString(opts.RequestID)) {
		return fmt.Errorf("request-id must be at most %d bytes using letters, digits, dot, underscore, colon or hyphen", models.LifecycleIdentifierMaxBytes)
	}
	if opts.ExpectedTransition != "" && !lifecycleDigestValid(opts.ExpectedTransition) {
		return fmt.Errorf("expected-transition must be a full lowercase SHA-256 transition token")
	}
	return nil
}

// NewLifecycleRequest hashes only caller-normalized payloads. It does not
// authorize the caller: current authority and operation eligibility must be
// checked inside the existing state transaction before using these helpers.
func NewLifecycleRequest(operation string, task *models.Task, actor string, authority *models.AgentAuthority, opts LifecycleRequestOptions, normalizedPayload any) (LifecycleRequest, error) {
	invalid := func(err error) (LifecycleRequest, error) {
		return LifecycleRequest{}, &LifecycleError{Outcome: NewLifecycleOutcome(operation, task, models.LifecycleInvalidInput, "correct_input", "none"), Err: err}
	}
	if err := ValidateLifecycleRequestOptions(opts); err != nil {
		return invalid(err)
	}
	if !models.IsLifecycleOperation(operation) {
		return invalid(fmt.Errorf("unknown lifecycle operation"))
	}
	if task == nil {
		return invalid(fmt.Errorf("task is required for lifecycle identity"))
	}
	if actor == "" || len(actor) > models.LifecycleIdentifierMaxBytes || !lifecycleRequestIDPattern.MatchString(actor) {
		return invalid(fmt.Errorf("invalid lifecycle actor identifier"))
	}
	if authority != nil && authority.ID != actor {
		return invalid(fmt.Errorf("lifecycle actor does not match authority"))
	}
	payload, err := json.Marshal(normalizedPayload)
	if err != nil {
		return invalid(fmt.Errorf("lifecycle payload cannot be encoded: %w", err))
	}
	expected := opts.ExpectedTransition
	if expected == "" {
		expected = models.TaskTransitionID(task)
	}
	request := LifecycleRequest{Operation: operation, Actor: actor, RequestID: opts.RequestID, ExpectedTransition: expected, PayloadDigest: lifecycleDigest(payload)}
	if authority != nil && authority.Generation != "" {
		request.GenerationDigest = lifecycleDigest([]byte(authority.Generation))
	}
	return request, nil
}

func lifecycleDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func lifecycleDigestValid(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func sameLifecycleKey(a, b models.LifecycleIdentity) bool {
	return a.Operation == b.Operation && a.Actor == b.Actor &&
		a.GenerationDigest == b.GenerationDigest && a.RequestID == b.RequestID &&
		a.ExpectedTransition == b.ExpectedTransition
}

func sameLifecycleRequest(a, b models.LifecycleIdentity) bool {
	return sameLifecycleKey(a, b) && a.PayloadDigest == b.PayloadDigest
}

func lifecycleRequestError(task *models.Task, request LifecycleRequest, outcome, action, effects, reason string) error {
	o := NewLifecycleOutcome(request.Operation, task, outcome, action, effects)
	o.RequestID = request.RequestID
	return &LifecycleError{Outcome: o, Err: fmt.Errorf("%s", reason)}
}

// preparationStillCurrent never infers process death without two authenticated
// generations. The caller must have validated current authority and eligibility.
func preparationStillCurrent(task *models.Task, request LifecycleRequest) bool {
	p := task.Lifecycle.Preparation
	if p.Boundary != models.TaskTransitionID(task) {
		return false
	}
	return !lifecycleGenerationSuperseded(p.LifecycleIdentity, request)
}

func lifecycleGenerationSuperseded(prepared, current models.LifecycleIdentity) bool {
	return prepared.Actor == current.Actor && prepared.GenerationDigest != "" && current.GenerationDigest != "" && prepared.GenerationDigest != current.GenerationDigest
}

// CheckLifecycleRequest is read-only and must precede domain mutation. Retained
// exact receipts win over current-token rejection. Empty IDs never prove replay.
// Obsolete preparations do not block an authorized fresh boundary/generation.
func CheckLifecycleRequest(task *models.Task, request LifecycleRequest) (*models.LifecycleReceipt, error) {
	if task == nil {
		return nil, lifecycleRequestError(task, request, models.LifecycleStateChanged, "requery", "none", "task state is unavailable")
	}
	if task.Lifecycle != nil && request.RequestID != "" {
		for _, receipt := range task.Lifecycle.Receipts {
			if !sameLifecycleKey(receipt.LifecycleIdentity, request) {
				continue
			}
			if receipt.PayloadDigest != request.PayloadDigest {
				return nil, lifecycleRequestError(task, request, models.LifecycleInvalidInput, "correct_input", "none", "request identity was already used with a different payload")
			}
			copy := receipt
			return &copy, nil
		}
	}
	if task.Lifecycle != nil && task.Lifecycle.Preparation != nil {
		p := task.Lifecycle.Preparation
		if request.RequestID != "" && sameLifecycleKey(p.LifecycleIdentity, request) && p.PayloadDigest != request.PayloadDigest {
			return nil, lifecycleRequestError(task, request, models.LifecycleInvalidInput, "correct_input", "none", "prepared request identity has a different payload")
		}
	}
	if task.Lifecycle != nil && task.Lifecycle.Preparation != nil && preparationStillCurrent(task, request) {
		return nil, lifecycleRequestError(task, request, models.LifecycleStateChanged, "requery", "unknown", "an unresolved preparation remains at this ownership boundary")
	}
	if request.ExpectedTransition != models.TaskTransitionID(task) {
		return nil, lifecycleRequestError(task, request, models.LifecycleStateChanged, "requery", "none", "original task transition no longer matches; inspect before choosing a new request")
	}
	return nil, nil
}

// PrepareLifecycleRequest reserves external work in an existing authorized
// transaction. Retiring an obsolete marker is not completion or rollback.
// All validation happens on a copy, so a helper error leaves metadata unchanged.
func PrepareLifecycleRequest(task *models.Task, request LifecycleRequest) error {
	receipt, err := CheckLifecycleRequest(task, request)
	if err != nil {
		return err
	}
	if receipt != nil {
		return &LifecycleError{Outcome: LifecycleReplayOutcome(task, receipt, request.Actor), Err: fmt.Errorf("request is already completed")}
	}
	copy := *task
	copy.Lifecycle = cloneTaskLifecycle(task.Lifecycle)
	if copy.Lifecycle.Preparation != nil {
		models.AdvanceLifecycle(&copy)
	}
	copy.Lifecycle.Preparation = &models.LifecyclePreparation{LifecycleIdentity: request, Boundary: models.TaskTransitionID(&copy)}
	if err := statevalidate.ValidateTaskLifecycle(&copy); err != nil {
		return lifecycleRequestError(task, request, models.LifecycleInvalidInput, "correct_input", "none", err.Error())
	}
	task.Lifecycle = copy.Lifecycle
	return nil
}

// ValidateLifecyclePreparation runs before final domain mutation, after any
// unlocked external work. CompleteLifecycleRequest deliberately cannot perform
// this comparison after the caller has changed history/status/ownership.
func ValidateLifecyclePreparation(task *models.Task, request LifecycleRequest) error {
	if task == nil || task.Lifecycle == nil || task.Lifecycle.Preparation == nil ||
		!sameLifecycleRequest(task.Lifecycle.Preparation.LifecycleIdentity, request) ||
		task.Lifecycle.Preparation.Boundary != models.TaskTransitionID(task) {
		return lifecycleRequestError(task, request, models.LifecycleStateChanged, "requery", "unknown", "preparation or task boundary changed before finalization")
	}
	return nil
}

// CompleteLifecycleRequest follows the caller's checked domain mutation in the
// SAME transaction. For metadata-only calls CheckLifecycleRequest must run
// first; external-effect calls use ValidateLifecyclePreparation before mutation.
func CompleteLifecycleRequest(task *models.Task, request LifecycleRequest, projection models.LifecycleProjection) (models.LifecycleOutcome, error) {
	if task == nil {
		return models.LifecycleOutcome{}, lifecycleRequestError(task, request, models.LifecycleStateChanged, "requery", "unknown", "task disappeared before completion")
	}
	if task.Lifecycle != nil && task.Lifecycle.Preparation != nil {
		p := task.Lifecycle.Preparation
		// CheckLifecycleRequest established the original boundary before the
		// caller's metadata-only mutation. Do not compare the changed task here.
		if !sameLifecycleRequest(p.LifecycleIdentity, request) && p.Boundary == request.ExpectedTransition && !lifecycleGenerationSuperseded(p.LifecycleIdentity, request) {
			return models.LifecycleOutcome{}, lifecycleRequestError(task, request, models.LifecycleStateChanged, "requery", "unknown", "another request owns the preparation")
		}
	}
	copy := *task
	copy.Lifecycle = cloneTaskLifecycle(task.Lifecycle)
	models.AdvanceLifecycle(&copy)
	copy.Lifecycle.CompletionSequence++
	receipt := models.LifecycleReceipt{LifecycleIdentity: request, Sequence: copy.Lifecycle.CompletionSequence, TransitionID: models.TaskTransitionID(&copy), Projection: projection}
	copy.Lifecycle.Receipts = append(copy.Lifecycle.Receipts, receipt)
	copy.Lifecycle.Receipts = pruneLifecycleReceipts(copy.Lifecycle.Receipts, copy.Lifecycle.CompletionSequence)
	if err := statevalidate.ValidateTaskLifecycle(&copy); err != nil {
		return models.LifecycleOutcome{}, lifecycleRequestError(nil, request, models.LifecycleInvalidInput, "correct_input", "unknown", err.Error())
	}
	task.Lifecycle = copy.Lifecycle
	outcome := NewLifecycleOutcome(request.Operation, task, models.LifecycleCompleted, "continue", "committed")
	outcome.CompletedTransitionID = receipt.TransitionID
	outcome.RequestID = request.RequestID
	return outcome, nil
}

func cloneTaskLifecycle(lifecycle *models.TaskLifecycle) *models.TaskLifecycle {
	if lifecycle == nil {
		return &models.TaskLifecycle{}
	}
	copy := *lifecycle
	copy.Receipts = append([]models.LifecycleReceipt(nil), lifecycle.Receipts...)
	return &copy
}

func pruneLifecycleReceipts(receipts []models.LifecycleReceipt, sequence uint64) []models.LifecycleReceipt {
	counts := make(map[string]int)
	keep := make([]models.LifecycleReceipt, 0, models.LifecycleReceiptsPerTask)
	for i := len(receipts) - 1; i >= 0 && len(keep) < models.LifecycleReceiptsPerTask; i-- {
		receipt := receipts[i]
		if sequence >= models.LifecycleReceiptsPerTask && receipt.Sequence <= sequence-models.LifecycleReceiptsPerTask {
			continue
		}
		if counts[receipt.Operation] >= models.LifecycleReceiptsPerOperation {
			continue
		}
		counts[receipt.Operation]++
		keep = append(keep, receipt)
	}
	for i, j := 0, len(keep)-1; i < j; i, j = i+1, j-1 {
		keep[i], keep[j] = keep[j], keep[i]
	}
	return keep
}

// retireFailedLifecyclePreparation releases only a reservation committed by this
// invocation after its external work has returned. It proves neither completion
// nor rollback. Crashes and callers with uncertain unfinished effects retain it.
// The project lock remains held; the exact identity and boundary are compared
// under the blackboard lock, so a concurrent or replacement request is untouched.
func retireFailedLifecyclePreparation(bb *db.Blackboard, taskID string, authority *models.AgentAuthority, preparation *models.LifecyclePreparation, originalErr error, effects string) error {
	if originalErr == nil || preparation == nil {
		return originalErr
	}
	var observed *models.Task
	cleanupErr := modifyLifecycleState(bb, authority, func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil || task.Lifecycle == nil || task.Lifecycle.Preparation == nil {
			return errLifecycleReplay // Abort serialization for a no-op.
		}
		current := task.Lifecycle.Preparation
		if !sameLifecycleRequest(current.LifecycleIdentity, preparation.LifecycleIdentity) ||
			current.Boundary != preparation.Boundary || models.TaskTransitionID(task) != preparation.Boundary {
			return errLifecycleReplay
		}
		models.AdvanceLifecycle(task)
		observed = task
		return nil
	})
	if errors.Is(cleanupErr, errLifecycleReplay) {
		return originalErr
	}
	outcome, action := models.LifecycleStateChanged, "requery"
	var existing *LifecycleError
	if errors.As(originalErr, &existing) {
		outcome, action = existing.Outcome.Outcome, existing.Outcome.SafeAction
	}
	if cleanupErr != nil {
		observed = nil // A failed write's candidate is not committed evidence.
		effects = "unknown"
		originalErr = errors.Join(originalErr, fmt.Errorf("failed to retire lifecycle preparation: %w", cleanupErr))
	}
	if IsAgentAuthorityError(originalErr) {
		observed = nil
		outcome, action = models.LifecycleStaleCaller, "stop"
	}
	result := NewLifecycleOutcome(preparation.Operation, observed, outcome, action, effects)
	result.RequestID = preparation.RequestID
	return &LifecycleError{Outcome: result, Err: originalErr}
}
