package ops

import (
	"errors"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/referencecontract"
	"github.com/liza-mas/liza/internal/roles"
)

// errAcceptanceBatchTimedOut is the cause of a canonical batch reaching its
// own budget, as opposed to a cancelled caller or an output overflow.
var errAcceptanceBatchTimedOut = errors.New("command batch timed out")

// effectiveTimeoutRetry returns config's admission timeout retry policy: nil
// when unset, or an error naming the invalid field, under which admission
// behaves as without a policy.
func effectiveTimeoutRetry(config models.Config) (*models.AdmissionTimeoutRetry, error) {
	policy := config.AdmissionTimeoutRetry
	if policy == nil {
		return nil, nil
	}
	// A factor above the cap in seconds cannot raise anything further, and
	// bounding it keeps factor times budget from overflowing.
	if policy.BudgetFactor < 1 || policy.BudgetFactor > referencecontract.AcceptanceMaxTimeoutSeconds {
		return nil, fmt.Errorf("budget_factor must be 1..%d", referencecontract.AcceptanceMaxTimeoutSeconds)
	}
	if policy.MaxTimeoutSeconds < 0 || policy.MaxTimeoutSeconds > referencecontract.AcceptanceMaxTimeoutSeconds {
		return nil, fmt.Errorf("max_timeout_seconds must be 0..%d", referencecontract.AcceptanceMaxTimeoutSeconds)
	}
	if (BudgetException{TimeoutSeconds: 1, Ruling: policy.Ruling}).Validate() != nil {
		return nil, fmt.Errorf("ruling must be a nonblank single line of at most %d bytes", MaxBudgetRulingBytes)
	}
	return policy, nil
}

// timeoutRetryPlan is what one submit may do when its batch reaches its own
// budget, fixed before its in-flight marker is published so the marker covers
// it (ADR-0216). A nil plan means no policy is set.
type timeoutRetryPlan struct {
	// invalid is a set but invalid policy: no retry and no block.
	invalid error
	ruling  string
	// seconds is the retry budget; 0 means no retry, for the reason in noRetry.
	seconds    int
	configured int
	clippedBy  string
	noRetry    string
}

// planTimeoutRetry derives the retry for a batch budgeted budgetSeconds:
// factor times the budget, clipped by the policy's maximum, the engine cap
// and the submit envelope, so the slot wait (when limited), the batch and the
// retry together never exceed the cap plus the submit wait floor that the
// in-flight grace covers. A retry shorter than the budget that just ran out
// is not run. doerRole is the task's doer role; inputs its runtime inputs.
func planTimeoutRetry(config models.Config, doerRole string, inputs []models.RuntimeInput, budgetSeconds int, limited bool) *timeoutRetryPlan {
	policy, err := effectiveTimeoutRetry(config)
	if err != nil {
		return &timeoutRetryPlan{invalid: err}
	}
	if policy == nil {
		return nil
	}
	plan := &timeoutRetryPlan{ruling: policy.Ruling}
	if doerRole != roles.Coder {
		plan.noRetry = fmt.Sprintf("the policy applies to coding tasks only; the doer role is %q", doerRole)
		return plan
	}
	for _, input := range inputs {
		if input.Consumption == models.RuntimeInputSingleUse {
			plan.noRetry = fmt.Sprintf("runtime input %s is single_use, so only one run may use it", input.ID)
			return plan
		}
	}
	budget := acceptanceBatchTimeout(budgetSeconds)
	envelope := admissionSlotCap + submitSlotWaitFloor - budget
	if limited {
		envelope -= submitAdmissionSlotWait(budgetSeconds)
	}
	plan.configured = policy.BudgetFactor * int(budget/time.Second)
	retry := time.Duration(plan.configured) * time.Second
	if limit := time.Duration(policy.MaxTimeoutSeconds) * time.Second; limit > 0 && retry > limit {
		retry, plan.clippedBy = limit, models.AdmissionRetryClippedByMax
	}
	if retry > admissionSlotCap {
		retry, plan.clippedBy = admissionSlotCap, models.AdmissionRetryClippedByCap
	}
	if retry > envelope {
		retry, plan.clippedBy = max(0, envelope), models.AdmissionRetryClippedByEnvelope
	}
	if retry < budget {
		plan.noRetry = fmt.Sprintf("a retry of at least the %s budget does not fit: at most %s (%s)", budget, retry, plan.clippedBy)
		return plan
	}
	plan.seconds = int(retry / time.Second)
	return plan
}

// retrySeconds is the retry budget the plan reserves in the in-flight marker.
func (p *timeoutRetryPlan) retrySeconds() int {
	if p == nil {
		return 0
	}
	return p.seconds
}

// applies reports whether a batch timeout falls under a valid policy: the
// engine retries it or blocks the task.
func (p *timeoutRetryPlan) applies() bool {
	return p != nil && p.invalid == nil
}

// record is the receipt record of a passing retry.
func (p *timeoutRetryPlan) record(timedOutIndex, budgetSeconds int) *models.AcceptanceTimeoutRetry {
	return &models.AcceptanceTimeoutRetry{
		TimedOutIndex:            timedOutIndex,
		TimeoutSeconds:           int(acceptanceBatchTimeout(budgetSeconds) / time.Second),
		RetryTimeoutSeconds:      p.seconds,
		ConfiguredTimeoutSeconds: p.configured,
		ClippedBy:                p.clippedBy,
		Ruling:                   p.ruling,
	}
}

// submitAdmissionWindow is how long a submit's in-flight marker must cover:
// the slot wait when a class matched, the batch and the planned retry.
func submitAdmissionWindow(budgetSeconds int, limited bool, plan *timeoutRetryPlan) time.Duration {
	window := acceptanceBatchTimeout(budgetSeconds) + time.Duration(plan.retrySeconds())*time.Second
	if limited {
		window += submitAdmissionSlotWait(budgetSeconds)
	}
	return window
}

// validateTimeoutRetryRecord checks a receipt's retry record shape. The
// recorded ruling is evidence of the run, never re-checked against config.
func validateTimeoutRetryRecord(taskID string, receipt *models.AcceptanceReceipt, executed map[int]bool) error {
	r := receipt.TimeoutRetry
	if r == nil {
		return nil
	}
	clipped := r.ClippedBy == models.AdmissionRetryClippedByMax || r.ClippedBy == models.AdmissionRetryClippedByCap || r.ClippedBy == models.AdmissionRetryClippedByEnvelope
	if !executed[r.TimedOutIndex] || r.TimeoutSeconds < 1 || r.RetryTimeoutSeconds < r.TimeoutSeconds ||
		r.RetryTimeoutSeconds > referencecontract.AcceptanceMaxTimeoutSeconds || r.ConfiguredTimeoutSeconds < r.RetryTimeoutSeconds ||
		(r.ClippedBy == "") != (r.ConfiguredTimeoutSeconds == r.RetryTimeoutSeconds) || (r.ClippedBy != "" && !clipped) ||
		(BudgetException{TimeoutSeconds: 1, Ruling: r.Ruling}).Validate() != nil {
		return acceptanceError(taskID, "acceptance.receipt.timeout_retry", "malformed admission timeout retry record")
	}
	return nil
}
