package ops

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// AcceptanceFault classifies where an acceptance refusal came from, decided at
// the site that refuses. The zero value covers Git read failures and every site
// whose determinism is not established; a supervisor never escalates it.
type AcceptanceFault string

const (
	// AcceptanceFaultContent is a pure function of bytes and state that were
	// read successfully, so an unchanged claim is refused identically.
	AcceptanceFaultContent AcceptanceFault = "content"
	// AcceptanceFaultAllocation is a parent-allocation or ancestry refusal. The
	// helpers behind it read Git "false" for both a mismatch and a failed read,
	// so repetition can be observed but determinism cannot.
	AcceptanceFaultAllocation AcceptanceFault = "allocation"
	// AcceptanceFaultRuntimeInput is a declared runtime input with no usable
	// ledger instance (ADR-0169). Only an operator can record one, so it
	// escalates at once like a content refusal, and no agent retry helps.
	AcceptanceFaultRuntimeInput AcceptanceFault = "runtime_input"
	// AcceptanceFaultAdmissionSlot is an admission limit whose class had no
	// free slot in time (ADR-0209). No command ran; a later retry can pass.
	AcceptanceFaultAdmissionSlot AcceptanceFault = "admission_slot"
	// AcceptanceFaultBatchTimeout is a batch that reached its own budget under
	// an admission timeout retry policy whose retry also timed out or could
	// not run (ADR-0216). The submit then blocks the task for a budget ruling.
	AcceptanceFaultBatchTimeout AcceptanceFault = "batch_timeout"
)

// AcceptanceClaimObservation is what a refused claim validated against: the
// allocation reference, the integration commit and AcceptanceObservation of
// the task and state. It marks the error as claim-stage.
type AcceptanceClaimObservation struct {
	AllocationRef     string
	IntegrationCommit string
	Digest            string
}

// AcceptanceEvidenceError is a repairable admission precondition, never an
// integration failure. It identifies the task and field requiring correction.
type AcceptanceEvidenceError struct {
	TaskID string
	Field  string
	Reason string
	Class  AcceptanceFault
	// Claim is set only when the refusal came from a claim, whose task is
	// unsubmitted and whose acceptance fields only the orchestrator can change.
	Claim *AcceptanceClaimObservation
	// Input names the first refused runtime input of a runtime-input refusal.
	Input string
	// Blocked reports that the refusing submission also moved the task to
	// BLOCKED; BlockFailed that it tried and could not.
	Blocked     bool
	BlockFailed bool
	// Ruling names the policy of a batch-timeout refusal; BlockSkipped says
	// why its submit did not block the task.
	Ruling       string
	BlockSkipped string
}

func (e *AcceptanceEvidenceError) Error() string {
	message := fmt.Sprintf("task %s %s: %s — correct the evidence and submit, or run %s for a submitted task", e.TaskID, e.Field, e.Reason, brand.Command("update-review-commit", e.TaskID))
	if e.Class == AcceptanceFaultRuntimeInput {
		message = fmt.Sprintf("task %s %s: %s — operator action required: %s. No command ran and no review cycle was spent. Do not retry, change evidence or run %s: an agent cannot provide this input",
			e.TaskID, e.Field, e.Reason, runtimeInputOperatorAction(e.TaskID, e.Input), brand.Command("update-review-commit"))
		switch {
		case e.Blocked:
			message += "; the task is now BLOCKED until the operator records the input and runs " + brand.Command("unblock-task", e.TaskID)
		case e.BlockFailed:
			message += fmt.Sprintf("; blocking the task failed, so run %s with this reason and stop", brand.Command("mark-blocked", e.TaskID))
		}
		return acceptanceExecutionMask(os.Environ())(message)
	}
	if e.Class == AcceptanceFaultBatchTimeout {
		message = fmt.Sprintf("task %s %s: %s — admission_timeout_retry (ruling %s) settled this timeout; do not resubmit or change the evidence", e.TaskID, e.Field, e.Reason, e.Ruling)
		switch {
		case e.Blocked:
			message += fmt.Sprintf("; the task is now BLOCKED for a budget ruling (%s) or a timeout_seconds correction", brand.Command("unblock-task", e.TaskID, "--budget-exception", "<seconds>", "--budget-ruling", "<decision-id>"))
		case e.BlockFailed:
			message += fmt.Sprintf("; blocking the task failed, so run %s with this reason and stop", brand.Command("mark-blocked", e.TaskID))
		case e.BlockSkipped != "":
			message += "; the task was not blocked: " + e.BlockSkipped
		}
		return acceptanceExecutionMask(os.Environ())(message)
	}
	if e.Class == AcceptanceFaultAdmissionSlot {
		message = fmt.Sprintf("task %s %s: %s — the operator's admission limit was full. No command ran and no review cycle was spent; do not change the evidence: retry the same commit", e.TaskID, e.Field, e.Reason)
		return acceptanceExecutionMask(os.Environ())(message)
	}
	if e.Claim != nil {
		message = fmt.Sprintf("task %s %s (%s): %s — the task's acceptance allocation must be corrected by the orchestrator (%s) or its integration-side cause repaired; claiming cannot fix it", e.TaskID, e.Field, e.Claim.AllocationRef, e.Reason, brand.Command("replace-task"))
	}
	return acceptanceExecutionMask(os.Environ())(message)
}

// Unwrap preserves the existing precondition classification for callers that
// do not need the more specific acceptance field diagnostic.
func (e *AcceptanceEvidenceError) Unwrap() error {
	return &PreconditionError{Reason: e.Error()}
}

func acceptanceError(taskID, field, reason string) error {
	return &AcceptanceEvidenceError{TaskID: taskID, Field: field, Reason: reason}
}

// acceptanceReceiptVersion is the layout version of the receipt submission
// publishes.
const acceptanceReceiptVersion = 1

type acceptanceInput struct {
	source        models.AcceptanceSource
	contract      *referencecontract.AcceptanceContract
	specRef       string
	destructiveDB bool
	// batchTimeoutSeconds is the admission batch budget: the reviewed
	// timeout_seconds raised by any recorded budget exception (ADR-0207).
	// contract keeps the reviewed value for receipts and deferrals.
	batchTimeoutSeconds int
	// deferred is derived from the contract and the current operator rulings;
	// admission skips these commands (ADR-0204).
	deferred []models.AcceptanceDeferredCommand
}

// executedCommands returns the canonical commands admission runs, in order,
// with each one's canonical index so diagnostics name the reviewed position.
func (input *acceptanceInput) executedCommands() (commands []string, indices []int) {
	skipped := make(map[int]bool, len(input.deferred))
	for _, deferral := range input.deferred {
		skipped[deferral.Index] = true
	}
	for i, command := range input.contract.Validation {
		if !skipped[i] {
			commands = append(commands, command)
			indices = append(indices, i)
		}
	}
	return commands, indices
}

// acceptanceDeferrals derives the commands admission skips: the contract's
// reviewed deferred indices, plus commands an operator ruling defers. A
// command a runtime input lists runs only in the gate (ADR-0169): the contract
// may not defer it, and a ruling does not apply to it.
func acceptanceDeferrals(config models.Config, contract *referencecontract.AcceptanceContract, runtimeInputs []models.RuntimeInput) ([]models.AcceptanceDeferredCommand, error) {
	gateOnly := map[string]bool{}
	for _, input := range runtimeInputs {
		for _, command := range input.Commands {
			gateOnly[command] = true
		}
	}
	mask := acceptanceExecutionMask(os.Environ())
	var deferred []models.AcceptanceDeferredCommand
	for i, command := range contract.Validation {
		authority := models.AcceptanceDeferralAuthority("")
		switch {
		case contract.IsDeferred(i):
			if gateOnly[command] {
				return nil, fmt.Errorf("acceptance.deferred: validation[%d] is used by a runtime input and must run in the gate", i)
			}
			authority = models.AcceptanceDeferralContract
		case !gateOnly[command] && models.DeferredValidationRulingFor(config, command) != nil:
			authority = models.AcceptanceDeferralOperator
		default:
			continue
		}
		deferred = append(deferred, models.AcceptanceDeferredCommand{
			Index:          i,
			Command:        mask(command),
			CommandSHA256:  fmt.Sprintf("%x", sha256.Sum256([]byte(command))),
			TimeoutSeconds: contract.TimeoutSeconds,
			Authority:      authority,
		})
	}
	return deferred, nil
}

// sameDeferrals compares deferral identity. The masked display text depends
// on the comparing process's environment, so it is not identity.
func sameDeferrals(want, got []models.AcceptanceDeferredCommand) bool {
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		w, g := want[i], got[i]
		if w.Index != g.Index || w.CommandSHA256 != g.CommandSHA256 || w.TimeoutSeconds != g.TimeoutSeconds || w.Authority != g.Authority {
			return false
		}
	}
	return true
}

// loadAcceptanceInput derives authority from the allocated, independently
// reviewed source at integration. Candidate worktree bytes cannot supply it.
// A task that declares runtime inputs must turn out strict: only the strict
// gate runs the canonical commands that consume them (ADR-0169).
func loadAcceptanceInput(root string, state *models.State, task *models.Task, integrationCommit string) (*acceptanceInput, error) {
	input, err := loadDeclaredAcceptanceInput(root, state, task, integrationCommit)
	if err == nil && input == nil && len(task.RuntimeInputs) > 0 {
		return nil, &AcceptanceEvidenceError{TaskID: task.ID, Field: "runtime_inputs", Reason: runtimeInputsStrictReason, Class: AcceptanceFaultContent}
	}
	return input, err
}

func loadDeclaredAcceptanceInput(root string, state *models.State, task *models.Task, integrationCommit string) (*acceptanceInput, error) {
	// Planning can consume documents containing coding allocations. Do not
	// adopt those contracts, but never let a type change erase prior adoption.
	if task.EffectiveType() != models.TaskTypeCoding && task.AcceptanceSource == nil {
		return nil, nil
	}
	if original := models.PendingPlanAmendment(state, task); original != "" {
		return nil, acceptanceSourceRefusal(task, AcceptanceFaultAllocation, "reviewed plan amendment pending for "+original)
	}
	declared, err := readAcceptanceDeclaration(root, state, task, integrationCommit)
	if err != nil || declared == nil {
		return nil, err
	}
	g := git.New(root)
	source, err := reviewDeclaredAllocation(g, root, state, task, declared, integrationCommit)
	if err != nil {
		return nil, err
	}
	if err := keepAdoptedSourceCommit(g, task, source, integrationCommit); err != nil {
		return nil, err
	}
	return &acceptanceInput{
		source: *source, contract: declared.contract,
		specRef: task.SpecRef, destructiveDB: task.DestructiveDB,
		batchTimeoutSeconds: admissionTimeoutSeconds(declared.contract.TimeoutSeconds, task.History),
		deferred:            declared.deferred,
	}, nil
}

func prepareAcceptanceReceipt(root string, task *models.Task, input *acceptanceInput, commit string) (*models.AcceptanceReceipt, error) {
	if input == nil {
		return nil, nil
	}
	path, heading, _ := strings.Cut(input.source.Ref, "#")
	candidateContent, _, err := readAcceptanceBlob(root, commit, path)
	if err != nil {
		return nil, acceptanceError(task.ID, "acceptance.source", "candidate changes the independently reviewed source")
	}
	// Same identity as allocation: the candidate may carry an unrelated edit to
	// the carrier it rebased onto, but not a change to the allocation itself.
	candidateIdentity, ok := carrierSpanIdentity(candidateContent, heading)
	if !ok || candidateIdentity != input.source.Blob {
		return nil, acceptanceError(task.ID, "acceptance.source", "candidate changes the independently reviewed source")
	}
	content, blob, err := readAcceptanceBlob(root, commit, input.contract.Manifest)
	if err != nil {
		return nil, acceptanceError(task.ID, "acceptance.manifest", err.Error())
	}
	manifest, err := referencecontract.ParseAcceptanceManifest(content)
	if err == nil {
		err = referencecontract.ValidateAcceptanceManifest(input.contract, manifest)
	}
	if err != nil {
		return nil, acceptanceError(task.ID, "acceptance.mappings", err.Error())
	}
	for _, mapping := range manifest.Mappings {
		if mapping.File == "" {
			continue
		}
		mode, present, err := git.New(root).TreePathMode(commit, mapping.File)
		if err != nil || !present || (mode != gitModeRegularFile && mode != gitModeExecutableFile) {
			return nil, acceptanceError(task.ID, "acceptance.mappings["+mapping.ObligationID+"].file", "must name a regular committed file")
		}
	}
	return &models.AcceptanceReceipt{Version: acceptanceReceiptVersion, ReviewCommit: commit, Source: input.source, ManifestPath: input.contract.Manifest, ManifestBlob: blob, Mappings: manifest.Mappings, Deferred: slices.Clone(input.deferred)}, nil
}

func checkAcceptanceWorktree(root, taskID, commit string) error {
	g := git.New(root)
	head, err := g.GetWorktreeHEAD(taskID)
	if err != nil || head != commit {
		return acceptanceError(taskID, "acceptance.review_commit", "worktree HEAD changed during validation")
	}
	status, err := g.WorktreeStatusShort(g.GetWorktreePath(taskID))
	if err != nil || status != "" {
		return acceptanceError(taskID, "acceptance.worktree", "validation requires no staged, unstaged or untracked changes")
	}
	return nil
}

// executeAcceptanceReceipt runs the strict gate. Runtime inputs are acquired
// here, after every non-executing precondition, so a refused manifest or a
// dirty worktree never spends a single_use input (ADR-0169). Admission slots
// of config's limits are taken just before, so a full class spends none
// either; slots already in held (submit's pre-admission) stay the caller's to
// release, otherwise they are released on return (ADR-0209). The batch budget
// starts once they are held. Cancelling ctx stops the wait or the command.
// A batch reaching its own budget under retry's policy re-runs once in full
// within the same slots and grant, or is refused as a batch timeout
// (ADR-0216); a nil retry (no policy, or a caller without one) never re-runs.
func executeAcceptanceReceipt(ctx context.Context, root string, task *models.Task, input *acceptanceInput, commit string, gate runtimeInputGate, config models.Config, held *admissionSlots, retry *timeoutRetryPlan) (*models.AcceptanceReceipt, error) {
	receipt, err := prepareAcceptanceReceipt(root, task, input, commit)
	if err != nil || receipt == nil {
		return receipt, err
	}
	if err := checkAcceptanceWorktree(root, task.ID, commit); err != nil {
		return nil, err
	}
	commands, indices := input.executedCommands()
	if held == nil {
		held = newAdmissionSlots(input.batchTimeoutSeconds)
		defer held.release()
	}
	if err := held.acquire(ctx, root, models.AdmissionLimitsFor(config, commands)); err != nil {
		return nil, admissionSlotRefusal(task.ID, err)
	}
	grant, err := gate.acquire(task)
	if err != nil {
		return nil, err
	}
	budget := acceptanceBatchTimeout(input.batchTimeoutSeconds)
	held.progress.set(fmt.Sprintf("running canonical validation, batch budget %s", budget), time.Now().Add(budget))
	worktree := git.New(root).GetWorktreePath(task.ID)
	receipt.Commands, err = executeAcceptanceCommandsWith(ctx, task.ID, worktree, commands, indices, input.batchTimeoutSeconds, grant, held.files())
	var timeout *acceptanceBatchTimeoutError
	if errors.As(err, &timeout) && retry.applies() {
		first := err
		refuse := func(why string) error {
			return &AcceptanceEvidenceError{TaskID: task.ID, Field: "acceptance.execution", Reason: fmt.Sprintf("%s after %s; %s", first, budget, why), Class: AcceptanceFaultBatchTimeout, Ruling: retry.ruling}
		}
		if retry.seconds == 0 {
			return nil, refuse("no retry: " + retry.noRetry)
		}
		if checkAcceptanceWorktree(root, task.ID, commit) != nil {
			return nil, refuse("no retry: the timed-out batch left the worktree changed")
		}
		retryBudget := acceptanceBatchTimeout(retry.seconds)
		retryLabel := retryBudget.String()
		if retry.clippedBy != "" {
			retryLabel += ", clipped by " + retry.clippedBy
		}
		held.progress.set(fmt.Sprintf("batch timed out at command[%d]; re-running the whole batch once, budget %s (admission_timeout_retry, ruling %s)", timeout.Index, retryLabel, retry.ruling), time.Now().Add(retryBudget))
		receipt.Commands, err = executeAcceptanceCommandsWith(ctx, task.ID, worktree, commands, indices, retry.seconds, grant, held.files())
		if errors.Is(err, errAcceptanceBatchTimedOut) {
			return nil, refuse(fmt.Sprintf("the whole-batch retry at %s also timed out", retryLabel))
		}
		if err == nil {
			receipt.TimeoutRetry = retry.record(timeout.Index, input.batchTimeoutSeconds)
		}
	}
	if err != nil {
		reason := err.Error()
		if errors.Is(err, errAcceptanceBatchTimedOut) && retry != nil && retry.invalid != nil {
			reason += "; admission_timeout_retry is set but invalid (" + retry.invalid.Error() + "): no retry"
		}
		return nil, acceptanceError(task.ID, "acceptance.execution", reason)
	}
	if err := checkAcceptanceWorktree(root, task.ID, commit); err != nil {
		return nil, err
	}
	return receipt, nil
}

// Recheck the source, task allocation and candidate identity in the final
// transaction. Commands ran outside the lock; this closes the stale-input gap.
func recheckAcceptanceReceipt(root string, state *models.State, task *models.Task, input *acceptanceInput, receipt *models.AcceptanceReceipt) error {
	integrationCommit, err := git.New(root).GetCommitSHA(state.Config.IntegrationBranch)
	if err != nil {
		return acceptanceError(task.ID, "acceptance.source", "cannot resolve integration commit")
	}
	current, err := loadAcceptanceInput(root, state, task, integrationCommit)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, input) {
		return acceptanceError(task.ID, "acceptance.source", "allocation or reviewed source changed during validation")
	}
	if receipt == nil {
		return nil
	}
	expected, err := prepareAcceptanceReceipt(root, task, current, receipt.ReviewCommit)
	if err != nil {
		return err
	}
	expected.Commands = receipt.Commands
	expected.TimeoutRetry = receipt.TimeoutRetry
	if sameDeferrals(expected.Deferred, receipt.Deferred) {
		expected.Deferred = receipt.Deferred
	}
	if !reflect.DeepEqual(expected, receipt) {
		return acceptanceError(task.ID, "acceptance.receipt", "candidate manifest changed during validation")
	}
	return checkAcceptanceWorktree(root, task.ID, receipt.ReviewCommit)
}

func validateAcceptanceForAssignment(root string, state *models.State, task *models.Task) error {
	if task.AcceptanceSource == nil && task.PlanRef == "" && task.SpecRef == "" {
		return nil
	}
	commit, err := git.New(root).GetCommitSHA(state.Config.IntegrationBranch)
	if err != nil {
		return acceptanceError(task.ID, "acceptance.source", "cannot resolve integration commit")
	}
	input, err := loadAcceptanceInput(root, state, task, commit)
	if err != nil || input == nil {
		return err
	}
	receipt := task.AcceptanceReceipt
	if task.ReviewCommit == nil || receipt == nil || receipt.ReviewCommit != *task.ReviewCommit || task.AcceptanceSource == nil || !reflect.DeepEqual(*task.AcceptanceSource, input.source) {
		return acceptanceError(task.ID, "acceptance.receipt", "missing or stale receipt at review commit")
	}
	expected, err := prepareAcceptanceReceipt(root, task, input, *task.ReviewCommit)
	if err != nil {
		return err
	}
	expected.Commands = receipt.Commands
	expected.TimeoutRetry = receipt.TimeoutRetry
	expected.Deferred = receipt.Deferred
	if !reflect.DeepEqual(expected, receipt) {
		return acceptanceError(task.ID, "acceptance.receipt", "receipt does not match validated manifest and canonical commands")
	}
	return validateReceiptExecutions(task.ID, input, receipt)
}

// validateReceiptExecutions binds every canonical command to either a
// successful execution or a deferral that current authority still grants. A
// command executed although a later ruling defers it remains valid evidence,
// so a new ruling never strands an already-submitted receipt; a deferral whose
// ruling was withdrawn does not.
func validateReceiptExecutions(taskID string, input *acceptanceInput, receipt *models.AcceptanceReceipt) error {
	granted := make(map[int]models.AcceptanceDeferredCommand, len(input.deferred))
	for _, deferral := range input.deferred {
		granted[deferral.Index] = deferral
	}
	skipped := make(map[int]bool, len(receipt.Deferred))
	for i, deferral := range receipt.Deferred {
		current, ok := granted[deferral.Index]
		if !ok || (i > 0 && deferral.Index <= receipt.Deferred[i-1].Index) || !sameDeferrals([]models.AcceptanceDeferredCommand{current}, []models.AcceptanceDeferredCommand{deferral}) {
			return acceptanceError(taskID, "acceptance.receipt.deferred", "deferral is not granted by the reviewed contract or a current operator ruling")
		}
		skipped[deferral.Index] = true
	}
	for index := range granted {
		if granted[index].Authority == models.AcceptanceDeferralContract && !skipped[index] {
			return acceptanceError(taskID, "acceptance.receipt.deferred", "receipt omits a deferral the reviewed contract declares")
		}
	}
	if len(receipt.Commands)+len(receipt.Deferred) != len(input.contract.Validation) {
		return acceptanceError(taskID, "acceptance.receipt", "receipt does not match validated manifest and canonical commands")
	}
	next := 0
	for i, command := range input.contract.Validation {
		if skipped[i] {
			continue
		}
		result := receipt.Commands[next]
		next++
		commandHash := fmt.Sprintf("%x", sha256.Sum256([]byte(command)))
		if result.CommandSHA256 != commandHash || result.ExitCode != 0 || result.StartedAt.IsZero() || result.FinishedAt.Before(result.StartedAt) {
			return acceptanceError(taskID, "acceptance.receipt.commands", "missing or unsuccessful canonical execution")
		}
	}
	executed := make(map[int]bool, len(receipt.Commands))
	for i := range input.contract.Validation {
		executed[i] = !skipped[i]
	}
	return validateTimeoutRetryRecord(taskID, receipt, executed)
}
